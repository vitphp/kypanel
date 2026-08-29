<template>
  <!-- 全屏浮窗终端：直接覆盖当前页面，无需跳路由 -->
  <transition name="ft-fade">
    <div v-if="visible" class="ft-overlay" @keydown.esc.stop="close()" tabindex="-1" ref="overlayEl">
      <div class="ft-window" :style="windowStyle" @click.stop>
        <div class="ft-header">
          <div class="ft-header-left">
            <span class="ft-status" :class="{ online: connected }"></span>
            <el-icon class="ft-icon"><Monitor /></el-icon>
            <span class="ft-title-text">Web 终端</span>
            <span class="ft-cwd" v-if="store.cwd" :title="store.cwd">
              <el-icon><FolderOpened /></el-icon>
              <span class="ft-cwd-text">{{ store.cwd }}</span>
            </span>
          </div>
          <div class="ft-header-right">
            <el-button size="small" text @click="clearScreen" title="清屏">
              <el-icon><Delete /></el-icon>
            </el-button>
            <el-button size="small" text @click="reconnect" title="重新连接">
              <el-icon><RefreshRight /></el-icon>
            </el-button>
            <el-divider direction="vertical" />
            <el-button size="small" text class="ft-close-btn" @click="close" title="关闭 (Esc)">
              <el-icon><Close /></el-icon>
            </el-button>
          </div>
        </div>
        <div class="ft-body">
          <div ref="termContainer" class="ft-xterm"></div>
        </div>
        <div class="ft-footer">
          <span class="ft-tip">{{ connected ? '已连接 · ' + (store.cwd || '默认目录') : '正在连接…' }}</span>
          <span class="ft-tip">{{ term ? term.cols + ' × ' + term.rows : '' }}</span>
        </div>
      </div>
    </div>
  </transition>
</template>

<script setup>
import { ref, onMounted, onBeforeUnmount, computed, watch, nextTick } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { Monitor, FolderOpened, Close, Delete, RefreshRight } from '@element-plus/icons-vue'
import { useAuthStore } from '@/stores/auth'
import { useFloatingTerminalStore } from '@/stores/floatingTerminal'

const auth = useAuthStore()
const store = useFloatingTerminalStore()

const overlayEl = ref(null)
const termContainer = ref(null)

let term = null
let fitAddon = null
let ws = null
let resizeHandler = null
let scrollLockTimer = null
// 命令执行中空闲检测：用户按 enter 或终端有输出时持续刷新，800ms 内无新数据视为命令完成
let runningIdleTimer = null
function markRunning() {
  store.setRunning(true)
  if (runningIdleTimer) clearTimeout(runningIdleTimer)
  runningIdleTimer = setTimeout(() => {
    store.setRunning(false)
    runningIdleTimer = null
  }, 800)
}
const connected = ref(false)
const collapsed = ref(false)

const visible = computed(() => store.visible)

// 80% 屏宽高，居中
const windowStyle = ref({})

function buildUrl() {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  let url = `${proto}://${location.host}/api/terminal?token=${encodeURIComponent(auth.token || '')}`
  if (store.cwd) url += `&cwd=${encodeURIComponent(store.cwd)}`
  return url
}

function connect() {
  if (!term) return
  if (ws) {
    try { ws.close() } catch (e) {}
    ws = null
  }
  connected.value = false
  try {
    ws = new WebSocket(buildUrl())
  } catch (e) {
    term.writeln('\r\n\x1b[31m无法创建 WebSocket 连接：' + (e?.message || e) + '\x1b[0m')
    return
  }
  ws.binaryType = 'arraybuffer'
  ws.onopen = () => {
    connected.value = true
    store.setConnected(true)
    sendResize()
    term.focus()
  }
  ws.onmessage = (ev) => {
    if (typeof ev.data === 'string') {
      term.write(ev.data)
    } else {
      term.write(new Uint8Array(ev.data))
    }
    // 终端有输出 → 命令仍在执行，重置空闲定时器
    markRunning()
  }
  ws.onclose = () => {
    connected.value = false
    store.setConnected(false)
    if (visible.value) {
      term.writeln('\r\n\x1b[31m[开猿运维] 连接已断开，可点击右上角「重新连接」\x1b[0m')
    }
  }
  ws.onerror = () => {
    connected.value = false
    store.setConnected(false)
  }
}

function sendResize() {
  if (ws && ws.readyState === WebSocket.OPEN && term) {
    ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }))
  }
}

function reconnect() {
  if (!term) return
  term.clear()
  connect()
}

function clearScreen() {
  if (term) term.clear()
}

function close() {
  // 完全释放资源，否则 xterm 仍渲染到已 detach 的旧 DOM 容器上，
  // 下次打开（v-if 重建容器）时屏幕会一直空白。
  // 关闭 WebSocket + 释放 xterm 实例，下次 open 时 ensureTerm 会重新创建。
  if (ws) {
    try { ws.close() } catch (e) {}
    ws = null
  }
  if (term) {
    try { term.dispose() } catch (e) {}
    term = null
  }
  if (fitAddon) {
    try { fitAddon.dispose() } catch (e) {}
    fitAddon = null
  }
  connected.value = false
  store.setConnected(false)
  store.setRunning(false)
  store.close()
}

// 第一次 visible=true 时初始化 term；后续 open 只调整 cwd 并重连。
watch(visible, (v, prev) => {
  if (v && !prev) {
    nextTick(() => {
      ensureTerm()
      setTimeout(() => {
        reconnect()
      }, 30)
    })
  }
})

function ensureTerm() {
  if (term) {
    requestAnimationFrame(() => {
      try { fitAddon && fitAddon.fit(); sendResize() } catch (e) {}
    })
    return
  }
  term = new Terminal({
    cursorBlink: true,
    fontSize: 13,
    fontFamily: 'Consolas, "Courier New", monospace',
    theme: { background: '#0d1117' },
    scrollback: 8000,
    convertEol: true
  })
  fitAddon = new FitAddon()
  term.loadAddon(fitAddon)
  term.open(termContainer.value)
  fitAddon.fit()
  term.onData((data) => {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(data)
      // 用户提交了回车（命令提交），进入执行态
      if (data.indexOf('\r') >= 0 || data.indexOf('\n') >= 0) {
        markRunning()
      }
    }
  })
  term.onResize(() => sendResize())

  resizeHandler = () => {
    if (!fitAddon || !visible.value) return
    try {
      fitAddon.fit()
      sendResize()
    } catch (e) {
      // 容器隐藏中 fit 可能抛错
    }
  }
  window.addEventListener('resize', resizeHandler)
  // 也监听 overlay 自己的尺寸变化（比如窗口拖拽）
  if (window.ResizeObserver && overlayEl.value) {
    const ro = new ResizeObserver(() => {
      if (!visible.value) return
      try { fitAddon.fit(); sendResize() } catch (e) {}
    })
    ro.observe(overlayEl.value)
  }
}

// 当 cwd 变化时按需重连（用户在 FileManager 切换目录再开终端）
watch(() => store.cwd, () => {
  if (visible.value) {
    nextTick(() => reconnect())
  }
})

onMounted(() => {
  // 计算初始窗口尺寸
  setWindowSize()
  window.addEventListener('resize', setWindowSize)
})

onBeforeUnmount(() => {
  if (resizeHandler) window.removeEventListener('resize', resizeHandler)
  if (scrollLockTimer) clearTimeout(scrollLockTimer)
  if (runningIdleTimer) {
    clearTimeout(runningIdleTimer)
    runningIdleTimer = null
  }
  if (ws) {
    try { ws.close() } catch (e) {}
    ws = null
  }
  if (term) {
    try { term.dispose() } catch (e) {}
    term = null
  }
  store.setRunning(false)
  store.setConnected(false)
})

function setWindowSize() {
  // 桌面：75% 屏宽 + 70% 屏高，最小 720×360
  // 手机：100% 屏宽 + 80% 屏高，CSS 同步覆盖
  const isMobile = window.innerWidth < 768
  if (isMobile) {
    windowStyle.value = {
      width: '100%',
      height: Math.floor(window.innerHeight * 0.8) + 'px'
    }
    return
  }
  const w = Math.max(720, Math.floor(window.innerWidth * 0.75))
  const h = Math.max(360, Math.floor(window.innerHeight * 0.7))
  windowStyle.value = { width: w + 'px', height: h + 'px' }
}
</script>

<style scoped>
.ft-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  z-index: 9000;
  display: flex;
  align-items: center;
  justify-content: center;
  outline: none;
}
.ft-window {
  background: #0d1117;
  color: #c9d1d9;
  border-radius: 10px;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  box-shadow: 0 18px 60px rgba(0, 0, 0, 0.45), 0 4px 12px rgba(0, 0, 0, 0.25);
  border: 1px solid #30363d;
  min-width: 720px;
  min-height: 360px;
}
/* 移动端 (<768px)：终端窗口占满整个屏幕宽度，关闭按钮不再被截断 */
@media (max-width: 767px) {
  .ft-window {
    width: 100% !important;
    max-width: 100vw;
    height: 80vh !important;
    min-width: 0;
    border-radius: 0;
    border-left: none;
    border-right: none;
  }
  .ft-header {
    padding: 6px 10px;
  }
  .ft-title-text {
    font-size: 13px;
  }
  .ft-cwd { max-width: 30vw; }
  .ft-footer {
    padding: 4px 10px;
  }
}
.ft-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 14px;
  background: #161b22;
  border-bottom: 1px solid #30363d;
  color: #c9d1d9;
  -webkit-user-select: none;
  user-select: none;
}
.ft-header-left {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 13px;
  min-width: 0;
}
.ft-icon { color: #58a6ff; }
.ft-title-text {
  font-weight: 600;
  color: #e6edf3;
}
.ft-cwd {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  border-radius: 999px;
  background: #21262d;
  color: #58a6ff;
  max-width: 50vw;
  min-width: 0;
  font-size: 12px;
}
.ft-cwd .el-icon {
  flex: 0 0 auto;
  font-size: 12px;
}
.ft-cwd-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ft-header-right {
  display: flex;
  align-items: center;
  gap: 2px;
}
.ft-header-right :deep(.el-button) {
  color: #c9d1d9;
  background-color: transparent;
  border-color: transparent;
}
.ft-header-right :deep(.el-button:hover),
.ft-header-right :deep(.el-button:focus-visible) {
  background-color: rgba(255, 255, 255, 0.10);
  color: #fff;
  border-color: transparent;
}
.ft-header-right :deep(.el-button:active) {
  background-color: rgba(255, 255, 255, 0.16);
}
.ft-header-right :deep(.el-divider--vertical) {
  background-color: #30363d;
}
/* 关闭按钮单独红色 hover，更醒目 */
.ft-header-right :deep(.ft-close-btn:hover),
.ft-header-right :deep(.ft-close-btn:focus-visible) {
  background-color: #f56c6c;
  color: #fff;
}
.ft-header-right :deep(.ft-close-btn:active) {
  background-color: #e64545;
}
.ft-status {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #f56c6c;
}
.ft-status.online {
  background: #67c23a;
  box-shadow: 0 0 6px #67c23a80;
}
.ft-body {
  flex: 1;
  background: #0d1117;
  padding: 6px;
  min-height: 0;
  overflow: hidden;
}
.ft-xterm {
  width: 100%;
  height: 100%;
}
.ft-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 14px;
  font-size: 12px;
  background: #161b22;
  border-top: 1px solid #30363d;
  color: #8b949e;
}

/* 浮窗动画 */
.ft-fade-enter-active, .ft-fade-leave-active {
  transition: opacity 0.18s ease;
}
.ft-fade-enter-from, .ft-fade-leave-to {
  opacity: 0;
}
.ft-fade-enter-active .ft-window,
.ft-fade-leave-active .ft-window {
  transition: transform 0.18s ease;
}
.ft-fade-enter-from .ft-window,
.ft-fade-leave-to .ft-window {
  transform: scale(0.97);
}
</style>
