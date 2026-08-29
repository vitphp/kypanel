import { defineStore } from 'pinia'
import { ElMessage, ElMessageBox } from 'element-plus'
import request from '../utils/request'
import { langOf } from '../utils/highlight'

// 待打开文件路径的 sessionStorage key（用于从外部跳转到编辑器而不污染 URL；不持久化到 LS）
const SS_PENDING = 'panel_file_editor_pending'

// 撤销历史上限（超出丢弃最早的快照，防止大文件长期编辑内存膨胀）
const MAX_UNDO = 100
// 连续编辑合并窗口（毫秒）：窗口内的输入/删除合并为一次撤销单元，类似 VSCode
const UNDO_MERGE_MS = 1000

function genId() {
  return 'fe_' + Date.now().toString(36) + '_' + Math.random().toString(36).slice(2, 8)
}

// 可编辑的文本扩展名（双击目录树中的文件时判断）
const TEXT_EXTS = new Set([
  'php', 'html', 'htm', 'css', 'scss', 'less', 'js', 'mjs', 'cjs', 'ts', 'tsx', 'jsx', 'vue',
  'json', 'md', 'txt', 'log', 'ini', 'conf', 'yaml', 'yml', 'toml', 'sh', 'bash', 'zsh',
  'py', 'go', 'java', 'c', 'h', 'cpp', 'cc', 'hpp', 'cs', 'rb', 'rs', 'swift', 'kt', 'sql',
  'xml', 'svg', 'yml', 'env', 'gitignore', 'dockerfile', 'editorconfig', 'bat', 'ps1',
  'pl', 'pm', 'lua', 'vue', 'svg', 'map',
  'lock', 'sum', 'mod', 'gradle', 'properties', 'ini', 'cfg', 'cnf', 'dist', 'md5'
])

export function isEditableFile(name) {
  const ext = (name.split('.').pop() || '').toLowerCase()
  if (/dockerfile/i.test(name) || name === '.env' || name === '.gitignore' || name === '.editorconfig') return true
  return TEXT_EXTS.has(ext)
}

export const useFileEditorStore = defineStore('fileEditor', {
  state: () => ({
    root: '/',
    tabs: [], // [{id, path, name, lang, content, savedContent, loaded, dirty, undoStack, redoStack, lastEditAt, lastCursor}]
    activeId: null,
    expanded: {}, // 目录树展开状态 {path: true}
    pendingPath: null // 等待编辑器首次挂载消费的"待打开文件"路径（不写入 URL）
  }),

  getters: {
    activeTab(s) {
      return s.tabs.find(t => t.id === s.activeId) || null
    },
    activeContent(s) {
      const t = s.tabs.find(t => t.id === s.activeId)
      return t ? t.content : ''
    },
    activePath(s) {
      const t = s.tabs.find(t => t.id === s.activeId)
      return t ? t.path : ''
    },
    activeLang(s) {
      const t = s.tabs.find(t => t.id === s.activeId)
      return t ? t.lang : ''
    },
    dirtyCount(s) {
      return s.tabs.filter(t => t.dirty).length
    }
  },

  actions: {
    // 占位：保留扩展位。当前不再从 localStorage 恢复任何 tab/active/root，
    // 每次进入编辑器都是干净状态；退出时由 FileEditor.onBeforeUnmount 调 clearAll。
    init() {
      this.tabs = []
      this.activeId = null
    },

    // 清空所有状态 + 待打开路径（路由切换 / 关闭编辑器窗口时调用）
    clearAll() {
      this.tabs = []
      this.activeId = null
      this.expanded = {}
      this.root = '/'
      this.pendingPath = null
      try { sessionStorage.removeItem(SS_PENDING) } catch { /* 忽略 */ }
    },

    // 把要打开的文件"暂存"起来（sessionStorage 同步备份），
    // 由 FileEditor 在挂载后通过 consumePending 消费并清掉 URL 上的 ?path=。
    // 这样浏览器地址栏不会暴露本地路径。
    stageOpen(path) {
      if (!path) return
      this.pendingPath = path
      try { sessionStorage.setItem(SS_PENDING, path) } catch { /* 隐私模式可能禁用 */ }
    },

    // 同步从 sessionStorage 拉回（防止用户刷新后状态丢失），
    // 同时覆盖 pendingPath。
    rehydratePending() {
      try {
        const p = sessionStorage.getItem(SS_PENDING)
        if (p) this.pendingPath = p
      } catch { /* noop */ }
      return this.pendingPath
    },

    // 编辑器挂载后调用：取出待打开路径并清空。
    // 返回值是路径（消费一次即失效，避免重复打开）。
    consumePending() {
      const p = this.pendingPath
      this.pendingPath = null
      try { sessionStorage.removeItem(SS_PENDING) } catch { /* noop */ }
      return p
    },

    // 打开文件：已打开则激活；否则建 tab 并读取内容
    async openFile(path) {
      const exist = this.tabs.find(t => t.path === path)
      if (exist) {
        this.activeId = exist.id
        if (!exist.loaded) await this.loadTab(exist)
        
        return exist
      }
      const name = path.split('/').filter(Boolean).pop() || path
      const tab = {
        id: genId(),
        path,
        name,
        lang: langOf(name),
        content: '',
        savedContent: null,
        loaded: false,
        loading: false,
        dirty: false,
        undoStack: [],
        redoStack: [],
        lastEditAt: 0,
        lastCursor: 0
      }
      this.tabs.push(tab)
      this.activeId = tab.id
      // 展开其所在目录
      this.expanded[dirOf(path)] = true
      await this.loadTab(tab)
      
      return tab
    },

    async loadTab(tab) {
      // 兼容：调用方传入的 tab 可能是 openFile 建 tab 时留下的"原始对象"（未响应式代理）。
      // 直接对它读写属性不会触发 Vue 响应式更新 → 界面会永远卡在"正在加载"且内容同步不到编辑器。
      // 因此统一从 this.tabs 按 id 取回响应式代理对象后再操作。
      const t = tab && this.tabs.find(x => x.id === tab.id)
      if (!t || t.loaded) return
      // 并发锁：同一 tab 只允许一个读取请求在途，防止并发请求互相覆盖内容
      if (t.loading) return
      t.loading = true
      try {
        const res = await request.get('/file/read', { params: { path: t.path } })
        this.applyLoaded(t, res.data.content)
      } catch (e) {
        // 瞬时故障（超时 / 后端重启 / 网络抖动）自动重试一次，避免"打开即空白"
        try {
          await new Promise(r => setTimeout(r, 600))
          const res = await request.get('/file/read', { params: { path: t.path } })
          this.applyLoaded(t, res.data.content)
        } catch (e2) {
          // 失败时【不】置 loaded：用户再次点击该 tab / 重新打开时仍会重试，而不是永久空白
          ElMessage.error('读取文件失败: ' + (e2.message || '未知错误'))
        }
      } finally {
        t.loading = false
      }
    },

    // 把读取结果写入 tab（写入前校验 tab 仍存在于列表中，防止 clearAll 后写回脏数据）
    applyLoaded(tab, content) {
      if (!tab || !this.tabs.includes(tab)) return
      tab.content = content || ''
      tab.savedContent = tab.content
      tab.dirty = false
      tab.loaded = true
    },

    async setActive(id) {
      this.activeId = id
      const t = this.tabs.find(x => x.id === id)
      if (t) this.expanded[dirOf(t.path)] = true
      // 防止"切换 tab 后内容空"：若该 tab 内容尚未加载，立即按需读取
      if (t && !t.loaded && !t.loading) {
        await this.loadTab(t)
      }
    },

    setContent(id, content) {
      const t = this.tabs.find(x => x.id === id)
      if (!t || t.content === content) return
      t.content = content
      t.dirty = t.content !== t.savedContent
    },

    // 用户每次编辑前调用：把当前内容快照压入撤销栈。
    // 1 秒内的连续编辑合并为一次撤销单元（VSCode 式合并输入）。
    recordEdit(id, { cursor = null } = {}) {
      const t = this.tabs.find(x => x.id === id)
      if (!t) return
      const now = Date.now()
      // 与上次编辑间隔在窗口内 → 合并（不新开快照，只续时间窗）
      if (now - (t.lastEditAt || 0) < UNDO_MERGE_MS) {
        t.lastEditAt = now
        t.lastCursor = cursor
        return
      }
      t.undoStack.push({ content: t.content, cursor })
      if (t.undoStack.length > MAX_UNDO) t.undoStack.shift()
      t.redoStack = []
      t.lastEditAt = now
      t.lastCursor = cursor
    },

    // 撤销当前标签：恢复上一快照，返回恢复后光标；无历史返回 null
    undo(id) {
      const t = this.tabs.find(x => x.id === id)
      if (!t || !t.undoStack.length) return null
      const snap = t.undoStack.pop()
      t.redoStack.push({ content: t.content, cursor: t.lastCursor })
      t.content = snap.content
      t.dirty = t.content !== t.savedContent
      t.lastEditAt = 0
      t.lastCursor = snap.cursor
      return { cursor: snap.cursor }
    },

    // 重做当前标签：返回恢复后光标；无历史返回 null
    redo(id) {
      const t = this.tabs.find(x => x.id === id)
      if (!t || !t.redoStack.length) return null
      const snap = t.redoStack.pop()
      t.undoStack.push({ content: t.content, cursor: t.lastCursor })
      t.content = snap.content
      t.dirty = t.content !== t.savedContent
      t.lastEditAt = 0
      t.lastCursor = snap.cursor
      return { cursor: snap.cursor }
    },

    markDirty(id, dirty) {
      const t = this.tabs.find(x => x.id === id)
      if (t) t.dirty = dirty
    },

    async save(id) {
      const t = this.tabs.find(x => x.id === id)
      if (!t) return
      // 防误存：内容还没从磁盘读出来（加载中 / 加载失败）时禁止保存，
      // 防止把空白内容写回文件造成数据丢失
      if (!t.loaded || t.loading) {
        ElMessage.warning('文件内容尚未加载完成，已阻止保存（防止把空白内容写回文件）')
        return false
      }
      try {
        await request.post('/file/write', { path: t.path, content: t.content })
        t.savedContent = t.content
        t.dirty = false
        
        return true
      } catch (e) {
        ElMessage.error('保存失败: ' + (e.message || '未知错误'))
        return false
      }
    },

    // 刷新：从磁盘重新读取（丢弃本地修改）
    async reload(id) {
      const t = this.tabs.find(x => x.id === id)
      if (!t) return
      if (t.dirty) {
        try {
          await ElMessageBox.confirm('文件有未保存的修改，重新加载将丢失这些修改，确定继续？', '重新加载', {
            type: 'warning', confirmButtonText: '重新加载', cancelButtonText: '取消'
          })
        } catch {
          return
        }
      }
      try {
        const res = await request.get('/file/read', { params: { path: t.path } })
        this.applyLoaded(t, res.data.content)
        // 重新加载后丢弃本地撤销/重做历史
        t.undoStack = []
        t.redoStack = []
        t.lastEditAt = 0
        t.lastCursor = 0
        ElMessage.success('已重新加载')
      } catch (e) {
        ElMessage.error('重新加载失败: ' + (e.message || '未知错误'))
      }
    },

    async closeTab(id, { force = false } = {}) {
      const idx = this.tabs.findIndex(t => t.id === id)
      if (idx === -1) return
      const t = this.tabs[idx]
      if (t.dirty && !force) {
        try {
          const act = await ElMessageBox.confirm(
            `文件「${t.name}」有未保存的修改，要保存吗？`,
            '未保存的修改',
            { type: 'warning', confirmButtonText: '保存', cancelButtonText: '不保存', distinguishCancelAndClose: true }
          )
          await this.save(id)
        } catch (action) {
          if (action !== 'cancel') return // 点了对话框右上角关闭，取消关闭操作
        }
      }
      this.tabs.splice(idx, 1)
      if (this.activeId === id) {
        const next = this.tabs[Math.min(idx, this.tabs.length - 1)]
        this.activeId = next ? next.id : null
      }
      
    },

    closeAll() {
      this.tabs = []
      this.activeId = null
      
    },

    setRoot(path) {
      this.root = path || '/'
      this.expanded[this.root] = true
      
    },

    // 把 root 切到上一层目录
    goUpRoot() {
      const r = this.root
      if (!r || r === '/') return false
      const i = r.lastIndexOf('/')
      this.setRoot(i <= 0 ? '/' : r.substring(0, i))
      return true
    },

    // 当前 root 是否为最顶层（不能再上）
    canGoUpRoot() {
      const r = this.root
      if (!r || r === '/') return false
      const segs = r.split('/').filter(Boolean)
      return segs.length > 1
    },

    toggleDir(path) {
      this.expanded[path] = !this.expanded[path]
      
    },

    // 重命名 / 移动时同步更新已打开的 tab 路径
    renameTabPath(oldPath, newPath) {
      for (const t of this.tabs) {
        if (t.path === oldPath) {
          t.path = newPath
          t.name = newPath.split('/').filter(Boolean).pop() || newPath
          t.lang = langOf(t.name)
        } else if (t.path.startsWith(oldPath + '/')) {
          t.path = newPath + t.path.substring(oldPath.length)
        }
      }
    },

    // 删除文件 / 文件夹时，关闭所有相关 tab
    closeTabsByPrefix(path) {
      const remain = this.tabs.filter(t => t.path !== path && !t.path.startsWith(path + '/'))
      const removed = this.tabs.length - remain.length
      this.tabs = remain
      if (this.activeId && !this.tabs.find(t => t.id === this.activeId)) {
        this.activeId = this.tabs.length ? this.tabs[this.tabs.length - 1].id : null
      }
    },

    // 目录树刷新后清理不存在的展开状态（简单保留即可）
    resetExpanded() {
      this.expanded = { [this.root]: true }
    }
  }
})

function dirOf(path) {
  const i = path.lastIndexOf('/')
  if (i <= 0) return '/'
  return path.slice(0, i)
}
