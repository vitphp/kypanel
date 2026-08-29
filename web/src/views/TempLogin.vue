<template>
  <div class="temp-login-page">
    <div class="temp-card">
      <div class="logo">🛡️ {{ panelName }}</div>
      <template v-if="state === 'loading'">
        <el-icon class="loading-icon" :size="32"><Loading /></el-icon>
        <p class="tip">正在验证临时链接…</p>
      </template>
      <template v-else-if="state === 'error'">
        <el-icon class="error-icon" :size="40"><CircleClose /></el-icon>
        <p class="tip error">{{ errorMsg }}</p>
        <el-button type="primary" @click="goLogin">返回登录页</el-button>
      </template>
      <template v-else>
        <el-icon class="ok-icon" :size="40"><CircleCheck /></el-icon>
        <p class="tip">验证成功，正在进入面板…</p>
      </template>
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import request from '../utils/request'

const route = useRoute()

// 面板名称（后端注入 window.__PANEL_NAME__，未设置则用默认「开猿运维」）
const panelName = (window.__PANEL_NAME__ || '').trim() || '开猿运维'
const router = useRouter()
const auth = useAuthStore()

const state = ref('loading')
const errorMsg = ref('')

onMounted(async () => {
  const token = route.query.token
  if (!token) {
    state.value = 'error'
    errorMsg.value = '链接无效：缺少访问令牌'
    return
  }
  try {
    // 校验临时 token 是否有效：调一个轻量公共接口触发鉴权。
    // 临时 token 走 ApiTokenOrAuth("api") 中间件链，会被 SecurityGuard 检查 IP 白名单等。
    // 如果 token 已过期/失效，会返回 401 并附带具体原因。
    const res = await request.get('/system/info', { headers: { Authorization: `Bearer ${token}` } })
    if (res.code !== 0) {
      throw new Error(res.msg || '链接校验失败')
    }
    // 存 token 到 localStorage，进入后台
    auth.setLogin(token, '临时访问')
    auth.setPermissions(null) // 超管全部权限
    setTimeout(() => router.replace('/dashboard'), 300)
  } catch (e) {
    state.value = 'error'
    // 展示详细错误（帮助排查：未登录 / 过期 / IP 白名单 / 已禁用 / 次数用完等）
    const status = e?.response?.status
    const apiMsg = e?.response?.data?.msg
    const errMsg = e?.message || ''
    const parts = []
    if (status) parts.push(`HTTP ${status}`)
    if (apiMsg) parts.push(apiMsg)
    if (!apiMsg && errMsg) parts.push(errMsg)
    errorMsg.value = parts.length ? parts.join('：') : '链接已失效'
    // 调试日志（浏览器控制台可见）
    console.error('[TempLogin] 验证失败', { status, apiMsg, errMsg, token: token?.slice(0, 12) + '...' })
  }
})

function goLogin() {
  // 临时登录链接失败时跳到安全入口（登录页），/login 已不可访问
  const entrance = (window.__SECURITY_ENTRANCE__ || '').trim()
  router.replace(entrance ? '/' + entrance : '/')
}
</script>

<style scoped>
.temp-login-page {
  min-height: 100vh; display: flex; align-items: center; justify-content: center;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
}
.temp-card {
  background: #fff; border-radius: 16px; padding: 48px 40px;
  text-align: center; min-width: 340px;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.2);
}
.logo { font-size: 22px; font-weight: 700; color: #1f2937; margin-bottom: 28px; }
.tip { color: #6b7280; font-size: 14px; margin: 16px 0 20px; }
.tip.error { color: #dc2626; }
.loading-icon { color: #6366f1; animation: spin 1s linear infinite; }
.error-icon { color: #dc2626; }
.ok-icon { color: #10b981; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>
