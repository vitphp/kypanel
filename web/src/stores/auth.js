import { defineStore } from 'pinia'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    token: localStorage.getItem('panel_token') || '',
    username: localStorage.getItem('panel_username') || '',
    // 权限模块列表：null 表示全部权限（超管），[] 表示无权限
    // 用 try 兜底：localStorage 若被脚本/异常写成非法 JSON，直接回落为 null（视为全权限并清脏数据），
    // 避免 JSON.parse 抛错导致 store 初始化失败、整页白屏。
    permissions: (() => {
      const raw = localStorage.getItem('panel_permissions')
      if (!raw) return null
      try {
        return JSON.parse(raw)
      } catch (e) {
        localStorage.removeItem('panel_permissions')
        return null
      }
    })()
  }),

  actions: {
    setLogin(token, username) {
      this.token = token
      this.username = username
      localStorage.setItem('panel_token', token)
      localStorage.setItem('panel_username', username)
    },
    setPermissions(perms) {
      // perms 为 null 表示全部权限
      this.permissions = perms
      if (perms === null) {
        localStorage.removeItem('panel_permissions')
      } else {
        localStorage.setItem('panel_permissions', JSON.stringify(perms))
      }
    },
    hasPermission(module) {
      // null = 超管（全部权限）
      if (this.permissions === null) return true
      return Array.isArray(this.permissions) && this.permissions.includes(module)
    },
    logout() {
      this.token = ''
      this.username = ''
      this.permissions = null
      localStorage.removeItem('panel_token')
      localStorage.removeItem('panel_username')
      localStorage.removeItem('panel_permissions')
    },
    isLoggedIn() {
      return !!this.token
    }
  }
})
