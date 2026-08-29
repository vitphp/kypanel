import { defineStore } from 'pinia'

// 全局浮窗终端：在所有页面都能打开，无需跳转到 /terminal 路由。
export const useFloatingTerminalStore = defineStore('floatingTerminal', {
  state: () => ({
    visible: false,
    cwd: '',
    title: '终端',
    // 命令执行中标识：用户提交回车或终端有输出时为 true，800ms 无新数据回到 false
    running: false,
    // WebSocket 连接状态
    connected: false
  }),
  actions: {
    open(payload = {}) {
      this.cwd = typeof payload.cwd === 'string' ? payload.cwd : ''
      this.title = typeof payload.title === 'string' && payload.title ? payload.title : (this.cwd || '终端')
      this.visible = true
    },
    close() {
      this.visible = false
    },
    toggle() {
      this.visible = !this.visible
    },
    setRunning(v) {
      this.running = !!v
    },
    setConnected(v) {
      this.connected = !!v
    }
  }
})
