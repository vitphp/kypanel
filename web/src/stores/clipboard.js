import { defineStore } from 'pinia'

// 文件剪贴板：跨标签页共享（用 localStorage + storage 事件）。
// mode: 'copy' 复制 | 'cut' 移动 | null 空
// paths: 绝对路径列表
// sourceDir: 来源目录（用于在粘贴时显示「来自 xxx」）
const KEY = 'panel_file_clipboard'

function loadFromStorage() {
  try {
    const raw = localStorage.getItem(KEY)
    if (!raw) return null
    const obj = JSON.parse(raw)
    if (!obj || !Array.isArray(obj.paths) || obj.paths.length === 0) return null
    if (obj.mode !== 'copy' && obj.mode !== 'cut') return null
    return { mode: obj.mode, paths: obj.paths, sourceDir: obj.sourceDir || '' }
  } catch (e) {
    return null
  }
}

function saveToStorage(payload) {
  try {
    if (!payload) localStorage.removeItem(KEY)
    else localStorage.setItem(KEY, JSON.stringify(payload))
  } catch (e) { /* ignore */ }
}

export const useClipboardStore = defineStore('clipboard', {
  state: () => {
    const init = loadFromStorage()
    return {
      mode: init?.mode || null, // 'copy' | 'cut' | null
      paths: init?.paths || [],
      sourceDir: init?.sourceDir || ''
    }
  },
  getters: {
    hasItems(state) {
      return state.mode && state.paths && state.paths.length > 0
    },
    count(state) {
      return state.paths?.length || 0
    },
    isCopy(state) {
      return state.mode === 'copy'
    },
    isCut(state) {
      return state.mode === 'cut'
    }
  },
  actions: {
    setCopy(paths, sourceDir = '') {
      this.mode = 'copy'
      this.paths = [...paths]
      this.sourceDir = sourceDir
      saveToStorage({ mode: this.mode, paths: this.paths, sourceDir: this.sourceDir })
    },
    setCut(paths, sourceDir = '') {
      this.mode = 'cut'
      this.paths = [...paths]
      this.sourceDir = sourceDir
      saveToStorage({ mode: this.mode, paths: this.paths, sourceDir: this.sourceDir })
    },
    clear() {
      this.mode = null
      this.paths = []
      this.sourceDir = ''
      saveToStorage(null)
    },
    /**
     * 跨标签页同步：监听 storage 事件，其它标签页改剪贴板时同步到本地
     */
    syncFromStorage() {
      const v = loadFromStorage()
      if (!v) {
        this.mode = null
        this.paths = []
        this.sourceDir = ''
      } else if (v.mode !== this.mode || v.sourceDir !== this.sourceDir ||
                 v.paths.length !== this.paths.length) {
        // 简化判断：长度或 mode 变了就整体覆盖
        this.mode = v.mode
        this.paths = v.paths
        this.sourceDir = v.sourceDir
      }
    }
  }
})
