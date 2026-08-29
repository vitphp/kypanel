<template>
  <div class="fe" @keydown.esc="onEsc">
    <!-- 顶部菜单栏 -->
    <div class="fe-topbar">
      <button
        class="fe-btn fe-btn-icon"
        :title="sideCollapsed ? '展开文件列表' : '收起文件列表'"
        @click="toggleSide"
      >
        <el-icon><Fold v-if="!sideCollapsed" /><Expand v-else /></el-icon>
      </button>
      <span class="fe-sep"></span>
      <button
        class="fe-btn"
        title="保存当前文件 (Ctrl+S)"
        :disabled="!activeTab || !activeTab.loaded || activeTab.loading"
        @click="onSave"
      >
        保存
      </button>
      <button class="fe-btn" title="查找 (Ctrl+F / 再按关闭)" :disabled="!activeTab" @click="toggleFind">
        查找
      </button>
      <button class="fe-btn" title="替换 (Ctrl+H / 再按关闭)" :disabled="!activeTab" @click="toggleReplace">
        替换
      </button>
      <span class="fe-sep"></span>
      <div class="fe-topbar-right">
        <span v-if="store.dirtyCount" class="fe-dirty-badge">{{ store.dirtyCount }} 个未保存</span>
        <!-- 关闭当前文件 -->
        <button
          v-if="activeTab"
          class="fe-btn fe-btn-icon fe-close-current"
          title="关闭当前文件"
          @click="store.closeTab(activeTab.id)"
        >
          <el-icon><Close /></el-icon>
        </button>
        <button class="fe-btn fe-btn-icon" :title="props.fullscreen ? '还原为弹窗' : '切换到全屏'" @click="emit('toggle-fullscreen')">
          <el-icon><FullScreen /></el-icon>
        </button>
        <button class="fe-btn" title="关闭编辑器" @click="onClose">关闭</button>
      </div>
    </div>

    <div class="fe-body">
      <!-- 左侧目录树 -->
      <div class="fe-side" :class="{ collapsed: sideCollapsed }">
        <div class="fe-side-tree">
          <FileTree
            v-if="treeReady"
            :root="store.root"
            :active-path="activeTab ? activeTab.path : ''"
            :collapsed="sideCollapsed"
            @open="onOpenTreeFile"
            @toggle-collapsed="toggleSide"
          />
        </div>
      </div>

      <!-- 右侧主区域 -->
      <div class="fe-main">
        <!-- 文件 Tab 栏 -->
        <div class="fe-tabs" v-if="store.tabs.length">
          <div
            v-for="t in store.tabs"
            :key="t.id"
            class="fe-tab"
            :class="{ active: t.id === store.activeId, dirty: t.dirty }"
            :title="t.path"
            @click="store.setActive(t.id)"
            @auxclick="onMiddleClick($event, t)"
          >
            <span class="fe-tab-name">
              {{ t.name }}
              <!-- 黄点：固定 8px 宽占位，dirty 时显示圆点，saved 时占位透明但保留空间 -->
              <span class="fe-tab-dirty" :class="{ active: t.dirty }"></span>
            </span>
            <!-- 关闭按钮：常驻 ×，hover 时变红，圆点不动 -->
            <span
              class="fe-tab-close"
              :title="t.dirty ? '未保存（点击关闭此标签，会提示是否保存）' : '关闭此标签'"
              @click="store.closeTab(t.id)"
            >×</span>
          </div>
          <div class="fe-tabs-empty"></div>
        </div>

        <!-- 查找 / 替换面板 -->
        <div v-if="findVisible" class="fe-findbar" :class="{ 'has-replace': replaceVisible }">
          <div class="fe-find-main">
            <input
              ref="findInput"
              v-model="searchTerm"
              class="fe-input"
              placeholder="查找"
              spellcheck="false"
              @keydown.enter.prevent="onFindNext"
              @keydown.shift.enter.prevent="onFindPrev"
              @keydown.esc.stop="closeFind"
            />
            <span v-if="searchCount" class="fe-find-count">{{ searchIndex + 1 }}/{{ searchCount }}</span>
            <button class="fe-btn small" title="上一个 (Shift+Enter)" @click="onFindPrev">
              <el-icon><ArrowUp /></el-icon>
            </button>
            <button class="fe-btn small" title="下一个 (Enter)" @click="onFindNext">
              <el-icon><ArrowDown /></el-icon>
            </button>
          </div>
          <div v-if="replaceVisible" class="fe-find-main">
            <input
              v-model="replaceTerm"
              class="fe-input"
              placeholder="替换为"
              spellcheck="false"
              @keydown.enter.prevent="onReplaceCurrent"
            />
            <button class="fe-btn small" title="替换当前" @click="onReplaceCurrent">替换</button>
            <button class="fe-btn small" title="全部替换" @click="onReplaceAll">全部替换</button>
          </div>
          <button class="fe-find-close" title="关闭查找/替换 (Esc)" @click="closeFind">
            <el-icon><Close /></el-icon>
          </button>
        </div>

        <!-- 报错结果面板 -->
        <div v-if="syntaxPanel" class="fe-syntax" :class="{ ok: syntaxOk }">
          <div class="fe-syntax-head">
            <el-icon><CircleCheckFilled v-if="syntaxOk" /><WarningFilled v-else /></el-icon>
            <span>{{ syntaxTitle }}</span>
            <button class="fe-btn small" @click="syntaxPanel = null"><el-icon><Close /></el-icon></button>
          </div>
          <div v-if="syntaxErrors.length" class="fe-syntax-list">
            <div
              v-for="(e, i) in syntaxErrors"
              :key="i"
              class="fe-syntax-item"
              @click="gotoError(e)"
            >
              <span class="fe-syntax-line">L{{ e.line || '?' }}</span>
              <span class="fe-syntax-msg">{{ e.message }}</span>
            </div>
          </div>
          <div v-else class="fe-syntax-msg">未发现语法错误</div>
        </div>

        <!-- 编辑器：仅内容加载成功后才渲染可编辑区域，加载中/失败显示占位，杜绝"空白可编辑"误保存 -->
        <div class="fe-editor">
          <CodeEditor
            v-if="activeTab && activeTab.loaded"
            ref="editorRef"
            v-model="editorModel"
            :lang="activeTab.lang"
            :fill="true"
            @save="onSave"
            @close="onEsc"
            @cursor="onCursor"
          />
          <div v-else-if="activeTab && activeTab.loading" class="fe-placeholder">
            <el-icon class="fe-placeholder-ico is-spin"><Loading /></el-icon>
            <p>正在加载 {{ activeTab.name }}…</p>
          </div>
          <div v-else-if="activeTab" class="fe-placeholder">
            <el-icon class="fe-placeholder-ico"><Warning /></el-icon>
            <p>文件内容加载失败，为避免误操作已禁止保存</p>
            <button class="fe-btn" @click="onRetryLoad(activeTab)">
              <el-icon><Refresh /></el-icon><span>重新加载</span>
            </button>
          </div>
          <div v-else class="fe-empty">
            <el-icon class="fe-empty-ico"><Files /></el-icon>
            <p>在左侧目录树选择文件打开</p>
          </div>
        </div>
      </div>
    </div>

    <!-- 状态栏 -->
    <div class="fe-statusbar">
      <span class="fe-st-item">{{ activeTab ? activeTab.lang : '--' }}</span>
      <span class="fe-st-item">行 {{ cursorPos.line }}, 列 {{ cursorPos.col }}</span>
      <span class="fe-st-item">UTF-8</span>
      <span class="fe-st-item">{{ fileSize }}</span>
      <span class="fe-st-item fe-st-path" :title="activeTab ? activeTab.path : ''">{{ activeTab ? activeTab.path : '' }}</span>
      <span v-if="activeTab && activeTab.dirty" class="fe-st-item fe-st-dirty">● 未保存</span>
    </div>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  ArrowLeft, ArrowDown, ArrowUp, Check, CircleCheckFilled,
  Close, EditPen, Expand, Files, Fold, FullScreen, Loading, Refresh, Search, Warning, WarningFilled
} from '@element-plus/icons-vue'
import CodeEditor from '../components/CodeEditor.vue'
import FileTree from '../components/FileTree.vue'
import { useFileEditorStore, isEditableFile } from '../stores/fileEditor'
import request from '../utils/request'

const props = defineProps({
  initialPath: { type: String, default: '' },
  fullscreen: { type: Boolean, default: false }
})
const emit = defineEmits(['close', 'toggle-fullscreen'])
const router = useRouter()
const route = useRoute()
const store = useFileEditorStore()

const editorRef = ref(null)
const findInput = ref(null)

// 编辑器内容双向绑定（到 store 的当前 tab）
const editorModel = computed({
  get: () => store.activeContent,
  set: v => store.setContent(store.activeId, v)
})
const activeTab = computed(() => store.activeTab)

/* 移动端 (< 768px) 默认收起左侧文件列表，桌面默认展开 */
const MOBILE_BP = 768
const sideCollapsed = ref(typeof window !== 'undefined' && window.innerWidth < MOBILE_BP)
function toggleSide() {
  sideCollapsed.value = !sideCollapsed.value
}
function onResize() {
  /* 跨过断点时强制收起到适合状态：进入 mobile 自动收起，回到桌面时保留用户上次的折叠/展开 */
  const w = window.innerWidth
  if (w < MOBILE_BP) sideCollapsed.value = true
}

// 查找 / 替换
const findVisible = ref(false)
const replaceVisible = ref(false)
const searchTerm = ref('')
const replaceTerm = ref('')
const searchCount = ref(0)
const searchIndex = ref(-1)
let searchTimer = null

const cursorPos = ref({ line: 1, col: 1 })
const fileSize = computed(() => {
  const c = store.activeContent
  if (!c) return ''
  const bytes = new TextEncoder().encode(c).length
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / 1024 / 1024).toFixed(2) + ' MB'
})

// 报错结果
const syntaxPanel = ref(null)
const syntaxOk = ref(false)
const syntaxTitle = ref('')
const syntaxErrors = ref([])

// 左侧目录树初始化完成后再挂载，避免 root 尚未确定时请求 '/' 造成竞态
const treeReady = ref(false)

onMounted(() => {
  store.init()
  let target = props.initialPath
  if (!target) {
    store.rehydratePending()
    target = store.consumePending() || ''
  }
  if (target) {
    store.setRoot(dirOf(target))
    store.openFile(target)
  } else {
    if (!store.root) store.root = '/'
  }
  treeReady.value = true
  window.addEventListener('keydown', onGlobalKeydown)
  window.addEventListener('resize', onResize)
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onGlobalKeydown)
  window.removeEventListener('resize', onResize)
  if (searchTimer) clearTimeout(searchTimer)
  // 离开编辑器时彻底清空 tab / 缓存，避免下次进入时残留空 tab
  store.clearAll()
})

function onGlobalKeydown(e) {
  if (!(e.ctrlKey || e.metaKey)) return
  const k = e.key.toLowerCase()
  if (k === 's') {
    e.preventDefault()
    onSave()
  } else if (k === 'f') {
    e.preventDefault()
    toggleFind()
  } else if (k === 'h') {
    e.preventDefault()
    toggleReplace()
  }
}

// ============ 文件操作 ============
function onOpenTreeFile(path) {
  const name = path.split('/').filter(Boolean).pop() || ''
  if (!isEditableFile(name)) {
    ElMessage.info('暂不支持编辑该文件类型')
    return
  }
  store.openFile(path)
}

async function onSave() {
  const t = activeTab.value
  if (!t) return
  if (!t.loaded || t.loading) {
    ElMessage.warning('文件内容尚未加载完成，已阻止保存（防止把空白内容写回文件）')
    return
  }
  const ok = await store.save(store.activeId)
  if (ok) ElMessage.success('已保存')
}

// 加载失败后手动重试读取
function onRetryLoad(t) {
  if (!t || t.loading) return
  store.loadTab(t)
}

async function onReload() {
  if (!activeTab.value) return
  store.reload(store.activeId)
}

// ============ 查找 / 替换 ============
// 用工具栏按钮 / Ctrl+F 或 Ctrl+H 触发：面板已开则关闭，未开则打开（按面板类型分别处理）
function toggleFind() {
  // 仅当"纯查找"模式（未开替换）下关闭；否则只切换到不带替换
  if (findVisible.value && !replaceVisible.value) {
    closeFind()
    return
  }
  openFind()
}
function toggleReplace() {
  if (findVisible.value && replaceVisible.value) {
    closeFind()
    return
  }
  openReplace()
}
function openFind() {
  replaceVisible.value = false
  findVisible.value = true
  nextTick(() => {
    findInput.value && findInput.value.focus()
    findInput.value && findInput.value.select()
    if (searchTerm.value) refreshFind()
  })
}
function openReplace() {
  replaceVisible.value = true
  findVisible.value = true
  nextTick(() => findInput.value && findInput.value.focus())
}
function closeFind() {
  findVisible.value = false
  replaceVisible.value = false
  refreshFind(true)
}
function refreshFind(clear = false) {
  const ed = editorRef.value
  if (!ed) return
  if (clear || !searchTerm.value) {
    ed.findAll('')
    searchCount.value = 0
    searchIndex.value = -1
    return
  }
  const count = ed.findAll(searchTerm.value)
  const info = ed.getMatchInfo()
  searchCount.value = count
  searchIndex.value = count ? info.index : -1
}
watch(searchTerm, () => {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    const ed = editorRef.value
    if (!ed) return
    if (!searchTerm.value) {
      ed.findAll('')
      searchCount.value = 0
      searchIndex.value = -1
      return
    }
    const count = ed.findAll(searchTerm.value)
    const info = ed.getMatchInfo()
    searchCount.value = count
    searchIndex.value = count ? info.index : -1
  }, 150)
})
function onFindNext() {
  const ed = editorRef.value
  if (!ed || !searchTerm.value) return
  ed.findNext()
  const info = ed.getMatchInfo()
  searchIndex.value = info.index
}
function onFindPrev() {
  const ed = editorRef.value
  if (!ed || !searchTerm.value) return
  ed.findPrev()
  const info = ed.getMatchInfo()
  searchIndex.value = info.index
}
function onReplaceCurrent() {
  const ed = editorRef.value
  if (!ed || !searchTerm.value) return
  if (ed.replaceCurrent(replaceTerm.value)) {
    const info = ed.getMatchInfo()
    searchCount.value = info.count
    searchIndex.value = info.count ? info.index : -1
  }
}
function onReplaceAll() {
  const ed = editorRef.value
  if (!ed || !searchTerm.value) return
  const n = ed.replaceAll(searchTerm.value, replaceTerm.value)
  const info = ed.getMatchInfo()
  searchCount.value = info.count
  searchIndex.value = -1
  if (n > 0) ElMessage.success(`已替换 ${n} 处`)
  else ElMessage.info('没有可替换的内容')
}

// ============ 报错检查 ============
async function onSyntaxCheck() {
  const t = activeTab.value
  if (!t) return
  try {
    const res = await request.post('/file/syntax_check', {
      path: t.path,
      content: t.content,
      lang: t.lang
    })
    if (!res.data.supported) {
      syntaxPanel.value = null
      ElMessage.info(res.data.message || '暂不支持该语言的语法检查')
      return
    }
    syntaxPanel.value = true
    syntaxOk.value = res.data.ok
    syntaxTitle.value = res.data.ok ? '语法检查通过' : `发现 ${(res.data.errors || []).length} 个错误`
    syntaxErrors.value = res.data.errors || []
    if (res.data.ok) {
      editorRef.value && editorRef.value.setErrorLines([])
      ElMessage.success('语法检查通过')
    } else {
      const lines = syntaxErrors.value.map(e => e.line).filter(l => l > 0)
      editorRef.value && editorRef.value.setErrorLines(lines)
    }
  } catch (e) {
    ElMessage.error('语法检查失败: ' + (e.message || '未知错误'))
  }
}
function gotoError(e) {
  if (e.line > 0) {
    editorRef.value && editorRef.value.gotoLine(e.line)
  }
}

// ============ 其他 ============
function onCursor(pos) {
  cursorPos.value = pos
}
function onEsc() {
  if (findVisible.value) {
    closeFind()
    return
  }
  if (syntaxPanel.value) {
    syntaxPanel.value = null
    return
  }
}
function onMiddleClick(e, t) {
  if (e.button === 1) {
    e.preventDefault()
    store.closeTab(t.id)
  }
}
function goBack() {
  if (store.dirtyCount > 0) {
    ElMessageBox.confirm(`有 ${store.dirtyCount} 个文件未保存，离开后将丢失修改，确定离开？`, '未保存的修改', {
      type: 'warning',
      confirmButtonText: '离开',
      cancelButtonText: '取消'
    }).then(() => router.push('/files')).catch(() => {})
  } else {
    router.push('/files')
  }
}
async function onClose() {
  // 有关未保存的修改时先确认，避免直接关闭丢失修改
  if (store.dirtyCount > 0) {
    try {
      const action = await ElMessageBox.confirm(
        `有 ${store.dirtyCount} 个文件未保存，关闭后将丢失修改，是否保存？`,
        '未保存的修改',
        {
          type: 'warning',
          confirmButtonText: '保存并关闭',
          cancelButtonText: '不保存',
          distinguishCancelAndClose: true
        }
      )
      // 用户点了「保存并关闭」（resolve('confirm')）
      if (action === 'confirm') {
        for (const t of store.tabs) {
          if (t.dirty) await store.save(t.id)
        }
      }
      // action === 'cancel'（不保存）也继续关闭
    } catch (action) {
      // X / Esc（reject 参数为 'close'）→ 取消关闭
      // 其它（理论上不应出现，但兜底）也取消关闭
      if (action !== 'cancel') return
    }
  }
  // 作为内嵌组件时通知父组件隐藏；作为独立页面时再返回路由
  emit('close')
  if (route.path === '/files/editor') {
    goBack()
  }
}
function dirOf(path) {
  const i = (path || '').lastIndexOf('/')
  if (i <= 0) return '/'
  return path.slice(0, i)
}
</script>

<style scoped>
.fe {
  display: flex;
  flex-direction: column;
  height: 100%;
  background: #1e1e1e;
  color: #cccccc;
  font-size: 13px;
  overflow: hidden;
}

/* ===== 顶部菜单栏 ===== */
.fe-topbar {
  display: flex;
  align-items: center;
  height: 40px;
  padding: 0 10px;
  gap: 4px;
  background: #3c3c3c;
  border-bottom: 1px solid #1e1e1e;
  flex: none;
  user-select: none;
}
.fe-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 28px;
  padding: 0 10px;
  background: transparent;
  border: 1px solid transparent;
  border-radius: 4px;
  color: #cccccc;
  font-size: 13px;
  cursor: pointer;
  white-space: nowrap;
  transition: background-color 0.12s, border-color 0.12s, color 0.12s;
}
.fe-btn:hover { background: #505050; color: #fff; }
.fe-btn:disabled { opacity: 0.35; cursor: not-allowed; }
.fe-btn.small { height: 24px; padding: 0 8px; }
.fe-btn-icon { padding: 0 6px; min-width: 28px; justify-content: center; }
.fe-sep { width: 1px; height: 20px; background: #525252; margin: 0 4px; }
.fe-topbar-right { margin-left: auto; display: flex; align-items: center; gap: 8px; }
.fe-mobile-only { display: none; }
/* 顶栏按钮文字：默认隐藏（图标+文字结构，桌面只显示图标），移动端显示文字 */
.fe-btn-text { display: none; }
/* 仅桌面端显示：tab 行内的 × 关闭（移动端由右上角的关闭按钮接管） */
.fe-pc-only { display: inline-flex; }
/* 移动端右上角"关闭当前文件"按钮：tab 上已有 × 按钮，无需重复，永久隐藏 */
.fe-close-current { display: none; }
@media (max-width: 767px) {
  .fe-mobile-only { display: inline-flex; }
  .fe-pc-only { display: none; }
  .fe-btn-text { display: inline; }
  .fe-topbar { padding: 0 6px; gap: 2px; }
  .fe-btn { padding: 0 8px; font-size: 13px; }
  .fe-btn-icon { padding: 0 6px; }
  .fe-sep { margin: 0 2px; }
  /* 移动端：文字按钮也显示文字（顶栏空间够），过长时省略号截断 */
  .fe-btn:not(.fe-btn-icon) {
    gap: 4px;
    min-width: 0;
    white-space: nowrap;
  }
  .fe-btn:not(.fe-btn-icon) > span {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  /* 极窄屏（< 380px）按钮文字也压缩 */
  @media (max-width: 379px) {
    .fe-btn { padding: 0 4px; font-size: 12px; }
  }
  /* 目录树展开时占更多宽度（全宽抽屉式） */
  .fe-side { width: 78vw; max-width: 300px; }
  /* 状态栏：隐藏文件路径和部分次要项，保留核心信息 */
  .fe-statusbar { gap: 10px; padding: 0 8px; }
  .fe-st-path { display: none; }
  .fe-st-item:nth-child(3) { display: none; } /* 隐藏 UTF-8 */
}
.fe-dirty-badge {
  background: #b4524d;
  color: #fff;
  font-size: 12px;
  padding: 2px 8px;
  border-radius: 10px;
}

/* ===== 主体 ===== */
.fe-body {
  display: flex;
  flex: 1;
  min-height: 0;
}

/* ===== 左侧目录树 ===== */
.fe-side {
  width: 240px;
  flex: none;
  background: #252526;
  border-right: 1px solid #1a1a1a;
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
  transition: width 0.18s ease;
}
.fe-side.collapsed {
  width: 0;
  border-right: none;
}
.fe-side-tree { flex: 1; min-height: 0; overflow: hidden; }

/* ===== 右侧主区域 ===== */
.fe-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
}

/* ===== 文件 Tab ===== */
.fe-tabs {
  display: flex;
  align-items: stretch;
  height: 36px;
  background: #2d2d2d;
  border-bottom: 1px solid #1a1a1a;
  overflow-x: auto;
  flex: none;
  user-select: none;
}
.fe-tabs::-webkit-scrollbar { height: 4px; }
.fe-tab {
  display: flex;
  align-items: center;
  gap: 0;
  padding: 0 8px;
  min-width: 90px;
  max-width: 200px;
  border-right: 1px solid #252526;
  color: #969696;
  cursor: pointer;
  background: #2d2d2d;
  position: relative;
  transition: background-color 0.12s, color 0.12s;
}
.fe-tab:hover { background: #353535; color: #e0e0e0; }
.fe-tab.active {
  background: #1e1e1e;
  color: #ffffff;
}
.fe-tab.active::before {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 2px;
  background: #007acc;
}
.fe-tab-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
}
/* 黄点：固定 8px 宽占位，dirty 时显示圆点，saved 时占位仍存在（透明），
   这样 dirty 切换时标签不抖动 */
.fe-tab-dirty {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: transparent;
  margin-left: 4px;
  vertical-align: middle;
  flex: none;
}
.fe-tab-dirty.active { background: #d19a66; }

/* 关闭按钮：常驻 ×，独立位置，hover 自身变红，黄点不动 */
.fe-tab-close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
  margin-left: 2px;
  border-radius: 3px;
  font-size: 13px;
  line-height: 1;
  color: #969696;
  cursor: pointer;
  flex: none;
  transition: background-color 0.12s, color 0.12s;
}
.fe-tab-close:hover {
  background: #c44a3a;
  color: #fff;
}
.fe-tabs-empty { flex: 1; background: #252526; border-bottom: 1px solid #1a1a1a; }

/* ===== 查找 / 替换面板 ===== */
.fe-findbar {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 6px 40px 6px 10px;
  background: #1e1e1e;
  border-bottom: 1px solid #3a3d41;
  flex: none;
}
.fe-findbar.has-replace { gap: 4px; }
.fe-find-main {
  display: flex;
  align-items: center;
  gap: 6px;
}
.fe-input {
  flex: 1;
  min-width: 180px;
  max-width: 420px;
  height: 26px;
  padding: 0 8px;
  background: #3c3c3c;
  border: 1px solid #3c3c3c;
  border-radius: 3px;
  color: #ddd;
  font-size: 13px;
  outline: none;
}
@media (max-width: 767px) {
  .fe-input {
    min-width: 0;
    font-size: 16px; /* 防 iOS 缩放 */
  }
  .fe-findbar { padding: 6px 36px 6px 8px; }
}
.fe-input:focus { border-color: #007acc; }
.fe-find-count { color: #969696; font-size: 12px; min-width: 34px; text-align: center; }
.fe-find-close {
  position: absolute;
  top: 6px;
  right: 8px;
  width: 24px;
  height: 24px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: transparent;
  border: 1px solid transparent;
  border-radius: 3px;
  color: #969696;
  cursor: pointer;
  padding: 0;
  font-size: 14px;
  transition: background-color .12s, color .12s, border-color .12s;
}
.fe-find-close:hover {
  background: #4a4d51;
  color: #fff;
  border-color: #5a5d61;
}

/* ===== 报错面板 ===== */
.fe-syntax {
  background: #2d2d2d;
  border-bottom: 1px solid #3a3d41;
  flex: none;
  max-height: 40%;
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.fe-syntax.ok { border-left: 3px solid #4ec9b0; }
.fe-syntax:not(.ok) { border-left: 3px solid #e51400; }
.fe-syntax-head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  font-weight: 600;
  color: #e8e8e8;
}
.fe-syntax.ok .fe-syntax-head { color: #4ec9b0; }
.fe-syntax:not(.ok) .fe-syntax-head { color: #e51400; }
.fe-syntax-head .fe-btn { margin-left: auto; }
.fe-syntax-list { overflow: auto; padding: 0 8px 8px; }
.fe-syntax-item {
  display: flex;
  gap: 10px;
  padding: 5px 8px;
  border-radius: 3px;
  cursor: pointer;
  font-size: 12px;
}
.fe-syntax-item:hover { background: #3a3d41; }
.fe-syntax-line {
  color: #e51400;
  font-weight: 700;
  flex: none;
  min-width: 42px;
}
.fe-syntax-msg { color: #cccccc; word-break: break-all; }
.fe-syntax .fe-syntax-msg:not(.fe-syntax-line) { padding: 0 12px 10px; }

/* ===== 编辑器 ===== */
.fe-editor { flex: 1; min-height: 0; position: relative; }
.fe-empty {
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: #6b6b6b;
}
.fe-empty-ico { font-size: 44px; }
.fe-placeholder {
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  color: #6b6b6b;
  font-size: 14px;
}
.fe-placeholder-ico { font-size: 40px; color: #909399; }
.fe-placeholder-ico.is-spin { animation: fe-spin 1s linear infinite; }
.fe-placeholder .fe-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
@keyframes fe-spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

/* ===== 状态栏 ===== */
.fe-statusbar {
  display: flex;
  align-items: center;
  gap: 14px;
  height: 24px;
  padding: 0 12px;
  background: #1a1a1a;
  color: #969696;
  border-top: 1px solid #2d2d2d;
  font-size: 12px;
  flex: none;
  user-select: none;
  overflow: hidden;
}
.fe-st-item { display: inline-flex; align-items: center; gap: 4px; white-space: nowrap; }
.fe-st-path {
  overflow: hidden;
  text-overflow: ellipsis;
  margin-left: auto;
  opacity: 0.9;
  direction: rtl;
  text-align: left;
  color: #cccccc;
}
.fe-st-dirty { font-weight: 700; color: #d19a66; }
</style>
