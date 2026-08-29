import { defineStore } from 'pinia'

// 跨页面传递一次性"上下文路径"（terminal 的 cwd / files 的初始 path），
// 不写入 URL，避免污染浏览器地址栏历史。
// 每个字段 consumeX() 读一次就清空，防止下次刷新页时残留错误。
export const useNavStore = defineStore('nav', {
  state: () => ({
    pendingCwd: '',
    pendingFilePath: ''
  }),
  actions: {
    setTerminalCwd(p) {
      this.pendingCwd = typeof p === 'string' ? p : ''
    },
    consumeTerminalCwd() {
      const v = this.pendingCwd
      this.pendingCwd = ''
      return v
    },
    setFilePath(p) {
      this.pendingFilePath = typeof p === 'string' ? p : ''
    },
    consumeFilePath() {
      const v = this.pendingFilePath
      this.pendingFilePath = ''
      return v
    }
  }
})
