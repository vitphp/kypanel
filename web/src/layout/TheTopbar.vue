<template>
  <header class="lp-topbar">
    <div class="lp-topbar-left">
      <button class="lp-collapse-btn" type="button" @click="$emit('toggle-collapse')" :title="collapsed ? '展开侧栏' : '折叠侧栏'">
        <el-icon :size="18"><Expand v-if="collapsed" /><Fold v-else /></el-icon>
      </button>
      <div class="lp-brand">
        <img class="lp-brand-logo" src="/logo.png" alt="logo" />
        <div class="lp-brand-text">
          <div class="lp-brand-name">{{ panel.name }}</div>
          <div class="lp-brand-sub">{{ panel.sub }}</div>
        </div>
      </div>
    </div>

    <div class="lp-topbar-right">
      <el-tooltip content="面板文档" placement="bottom">
        <button class="lp-icon-btn" type="button" @click="openDocs">
          <el-icon :size="16"><QuestionFilled /></el-icon>
        </button>
      </el-tooltip>
      <el-tooltip :content="floatTerm.running ? '终端（命令执行中…）' : '打开终端'" placement="bottom">
        <button class="lp-terminal-btn" :class="{ 'is-running': floatTerm.running, 'is-offline': floatTerm.visible && !floatTerm.connected }" type="button" @click="onTerminalClick">
          <span class="lp-terminal-icon-wrap" :class="{ spinning: floatTerm.running }">
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M4 6 L10 12 L4 18"/>
              <line x1="13" y1="18" x2="20" y2="18"/>
            </svg>
          </span>
          <span>终端</span>
          <span class="lp-terminal-dot"></span>
        </button>
      </el-tooltip>

      <el-dropdown trigger="hover" @command="onRestartCommand" :hide-on-click="false">
        <button class="lp-restart-btn" type="button" :disabled="restarting" title="重启面板 / 服务器">
          <el-icon :size="16" :class="{ spinning: restarting }"><SwitchButton /></el-icon>
          <span>重启</span>
        </button>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item command="panel">
              <el-icon style="color:#10b981;"><SwitchButton /></el-icon>重启面板
            </el-dropdown-item>
            <el-dropdown-item command="server" divided>
              <el-icon style="color:#f56c6c;"><SwitchButton /></el-icon>重启服务器
            </el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>

      <el-dropdown trigger="click" @command="onCommand">
        <div class="lp-user">
          <div class="lp-avatar">{{ userText.charAt(0).toUpperCase() }}</div>
          <div class="lp-user-meta">
            <span class="lp-user-name">{{ userText }}</span>
            <span class="lp-user-role">管理员</span>
          </div>
          <el-icon class="lp-user-caret" :size="12"><ArrowDown /></el-icon>
        </div>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item command="settings">
              <el-icon><Setting /></el-icon>面板设置
            </el-dropdown-item>
            <el-dropdown-item command="logout" divided>
              <el-icon><SwitchButton /></el-icon>退出登录
            </el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
    </div>
  </header>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessageBox, ElMessage, ElNotification } from 'element-plus'
import { useAuthStore } from '../stores/auth'
import { useFloatingTerminalStore } from '../stores/floatingTerminal'
import { usePanelStore } from '../stores/panel'
import request from '../utils/request'
import { OFFICIAL_SITE } from '../config'

defineProps({
  collapsed: { type: Boolean, default: false }
})

defineEmits(['toggle-collapse'])

const router = useRouter()
const auth = useAuthStore()
const floatTerm = useFloatingTerminalStore()
const panel = usePanelStore()

function onTerminalClick() {
  // 打开/关闭浮窗终端（不再跳路由，所有页面都能用）
  floatTerm.toggle()
}

const userText = computed(() => auth.username || 'admin')

function openDocs() {
  window.open(OFFICIAL_SITE, '_blank')
}

function onCommand(cmd) {
  if (cmd === 'settings') {
    router.push('/settings')
  } else if (cmd === 'logout') {
    ElMessageBox.confirm('确定要退出登录吗？', '提示', {
      confirmButtonText: '退出',
      cancelButtonText: '取消',
      type: 'warning'
    }).then(async () => {
      auth.logout()
      try { await request.post('/auth/logout') } catch (e) { /* 忽略 */ }
      // 退登后跳到安全入口（登录页），而非 /login（/login 已不可访问）
      const entrance = (window.__SECURITY_ENTRANCE__ || '').trim()
      router.push(entrance ? '/' + entrance : '/')
    }).catch(() => {})
  }
}

const restarting = ref(false)

// ping 探测：每 1s 试一次，5xx / 5xx / 网络错误视为离线，200 视为恢复。
// 用于面板/服务器重启后自动刷新页面。
async function waitForServerBack(timeoutMs = 120000) {
  const startedAt = Date.now()
  const interval = 1000
  while (Date.now() - startedAt < timeoutMs) {
    try {
      // 强制走原生 fetch（不带 token），避免鉴权头依赖干扰
      const res = await fetch('/api/ping', { method: 'GET', cache: 'no-store' })
      if (res.ok) {
        // 后端已恢复，再做一次 success toast + reload
        return true
      }
    } catch (_) { /* 离线中，继续轮询 */ }
    await new Promise(r => setTimeout(r, interval))
  }
  return false
}

async function onRestartCommand(cmd) {
  if (restarting.value) return
  if (cmd === 'panel') {
    // 二次确认
    try {
      await ElMessageBox.confirm(
        '重启面板会断开所有当前连接，约 3-10 秒后自动恢复。是否继续？',
        '重启面板',
        { confirmButtonText: '重启', cancelButtonText: '取消', type: 'warning' }
      )
    } catch { return }
    restarting.value = true
    try {
      await request.post('/system/restart-panel')
      ElNotification({ title: '面板重启中', message: '页面将在后端恢复后自动刷新', type: 'warning', duration: 0 })
    } catch (e) {
      ElMessage.error('重启指令发送失败：' + (e?.message || e))
      restarting.value = false
      return
    }
    const back = await waitForServerBack()
    if (back) {
      ElMessage.success('重启成功，即将刷新…')
      setTimeout(() => location.reload(), 2000)
    } else {
      ElMessage.error('面板长时间未恢复，请稍后手动刷新')
      restarting.value = false
    }
  } else if (cmd === 'server') {
    // 服务器重启：强确认
    try {
      await ElMessageBox.confirm(
        '重启服务器将中断所有服务和网站，请确认已做好数据备份。\n服务器恢复约需 30 秒 ~ 2 分钟，页面会自动刷新。',
        '重启服务器',
        {
          confirmButtonText: '我已备份，立即重启',
          cancelButtonText: '取消',
          type: 'warning',
          confirmButtonClass: 'el-button--danger'
        }
      )
    } catch { return }
    restarting.value = true
    try {
      await request.post('/system/restart-server')
      ElNotification({ title: '服务器重启中', message: '正在轮询服务器是否恢复（最多等待 2 分钟）…', type: 'error', duration: 0 })
    } catch (e) {
      ElMessage.error('重启指令发送失败：' + (e?.message || e))
      restarting.value = false
      return
    }
    const back = await waitForServerBack(180000)
    if (back) {
      ElMessage.success('重启成功，即将刷新…')
      setTimeout(() => location.reload(), 2000)
    } else {
      ElMessage.error('服务器长时间未恢复，请稍后手动刷新')
      restarting.value = false
    }
  }
}
</script>

<style scoped>
.lp-topbar {
  height: 64px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 20px;
  background: #ffffff;
  border-bottom: 1px solid #e5e7eb;
  z-index: 20;
}

.lp-topbar-left {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}

.lp-collapse-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border: none;
  background: transparent;
  border-radius: 8px;
  color: #475569;
  cursor: pointer;
  transition: background 0.2s, color 0.2s;
}
.lp-collapse-btn:hover {
  background: #f1f5f9;
  color: #10b981;
}

.lp-brand {
  display: flex;
  align-items: center;
  gap: 10px;
}
/* 窄屏（手机 / 小屏平板 < 900px）：隐藏品牌区，只留汉堡按钮作为侧栏入口；
   顶栏空间让给终端、重启等更多功能按钮，避免被挤压 */
@media (max-width: 899px) {
  .lp-brand { display: none; }
}
/* 进一步窄屏（< 700px）：隐藏文档按钮（保留终端/重启/账号下拉等核心操作，避免顶栏被压成两行） */
@media (max-width: 699px) {
  .lp-topbar-right .lp-icon-btn { display: none; }
  .lp-topbar-right { gap: 4px; }
}
/* 极窄屏（< 380px）：账号下拉的「管理员」副标题隐藏，主名仍保留；按钮文字保留 */
@media (max-width: 379px) {
  .lp-user-role { display: none; }
  .lp-topbar-right { gap: 2px; }
  .lp-terminal-btn,
  .lp-restart-btn { padding-left: 6px; padding-right: 6px; font-size: 12px; }
}
.lp-brand-logo {
  width: 34px;
  height: 34px;
  border-radius: 10px;
  object-fit: contain;
  display: block;
}
.lp-brand-text {
  line-height: 1.2;
}
.lp-brand-name {
  font-size: 15px;
  font-weight: 700;
  color: #0f172a;
}
.lp-brand-sub {
  font-size: 11px;
  color: #94a3b8;
}

.lp-topbar-right {
  display: flex;
  align-items: center;
  gap: 6px;
}

.lp-icon-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 34px;
  height: 34px;
  border: none;
  background: transparent;
  border-radius: 8px;
  color: #64748b;
  cursor: pointer;
  transition: background 0.2s, color 0.2s;
}
.lp-icon-btn:hover {
  background: #f1f5f9;
  color: #10b981;
}
.lp-icon-btn:disabled {
  cursor: not-allowed;
  opacity: .7;
}
.lp-icon-btn .spinning {
  animation: lp-term-spin 1s linear infinite;
}

/* 终端按钮：图标+文字，无描边无背景色（按要求简化） */
.lp-terminal-btn {
  display: flex;
  align-items: center;
  gap: 6px;
  height: 34px;
  padding: 0 10px;
  margin: 0 2px;
  border: none;
  background: transparent;
  border-radius: 8px;
  color: #334155;
  cursor: pointer;
  font-size: 13px;
  font-weight: 600;
  transition: background 0.2s, color 0.2s;
}
.lp-terminal-btn:hover {
  background: #f1f5f9;
  color: #10b981;
}

/* 重启按钮：与终端按钮风格一致（图标+文字、无描边无背景色） */
.lp-restart-btn {
  display: flex;
  align-items: center;
  gap: 6px;
  height: 34px;
  padding: 0 10px;
  margin: 0 2px;
  border: none;
  background: transparent;
  border-radius: 8px;
  color: #334155;
  cursor: pointer;
  font-size: 13px;
  font-weight: 600;
  transition: background 0.2s, color 0.2s;
  outline: none; /* 去掉浏览器/Element Plus 默认 hover/focus 黑色描边 */
}
.lp-restart-btn:hover,
.lp-restart-btn:focus,
.lp-restart-btn:focus-visible {
  background: #f1f5f9;
  color: #10b981;
  outline: none;
  box-shadow: none;
}
.lp-terminal-icon-wrap {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
  line-height: 1;
}
.lp-terminal-icon-wrap.spinning svg {
  animation: lp-term-spin 1s linear infinite;
  transform-origin: 50% 50%;
}
@keyframes lp-term-spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}
.lp-terminal-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #94a3b8;
  transition: background 0.2s;
}
.lp-terminal-btn.is-running {
  color: #ea580c;
  background: rgba(234, 88, 12, 0.08);
}
.lp-terminal-btn.is-running .lp-terminal-dot {
  background: #ea580c;
  animation: lp-term-pulse 0.9s ease-in-out infinite;
}
.lp-terminal-btn.is-offline .lp-terminal-dot {
  background: #f56c6c;
}
@keyframes lp-term-pulse {
  0%, 100% { opacity: 1; transform: scale(1); }
  50% { opacity: 0.4; transform: scale(0.7); }
}

.lp-user {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 4px 8px;
  border-radius: 10px;
  cursor: pointer;
  transition: background 0.2s;
  outline: none;
}
.lp-user:hover {
  background: #f1f5f9;
}
.lp-avatar {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: #10b981;
  color: #fff;
  font-weight: 700;
  font-size: 14px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.lp-user-meta {
  display: flex;
  flex-direction: column;
  line-height: 1.2;
  text-align: left;
}
.lp-user-name {
  font-size: 13px;
  font-weight: 600;
  color: #0f172a;
}
.lp-user-role {
  font-size: 11px;
  color: #94a3b8;
}
.lp-user-caret {
  color: #94a3b8;
}
</style>
