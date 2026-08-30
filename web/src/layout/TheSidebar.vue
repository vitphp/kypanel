<template>
  <aside class="lp-sidebar" :class="{ 'is-collapsed': collapsed }">
    <nav class="lp-sidebar-nav">
      <div class="lp-sidebar-section">
        <template v-for="item in filteredMenu" :key="item.path">
          <!-- 无子菜单：直接跳转 -->
          <router-link
            v-if="!item.children || !item.children.length"
            :to="item.path"
            class="lp-nav-item"
            :class="{ active: isActive(item) }"
          >
            <el-icon class="lp-nav-icon" :size="18"><component :is="item.icon || 'Menu'" /></el-icon>
            <span class="lp-nav-label">{{ item.title }}</span>
          </router-link>

          <!-- 有子菜单：手风琴分组 -->
          <div v-else class="lp-nav-group">
            <button
              type="button"
              class="lp-nav-item lp-nav-group-trigger"
              :class="{ active: isGroupActive(item) }"
              @click="toggleGroup(item.path)"
            >
              <el-icon class="lp-nav-icon" :size="18"><component :is="item.icon || 'Menu'" /></el-icon>
              <span class="lp-nav-label">{{ item.title }}</span>
              <el-icon class="lp-nav-caret" :size="12" :class="{ open: openGroups.includes(item.path) }">
                <ArrowDown />
              </el-icon>
            </button>
            <div
              class="lp-nav-sub"
              :class="{ open: openGroups.includes(item.path) }"
            >
              <router-link
                v-for="child in item.children"
                :key="child.path"
                :to="item.path + '/' + child.path"
                class="lp-nav-item lp-nav-child"
                :class="{ active: isActive({ path: item.path + '/' + child.path }) }"
              >
                <span class="lp-nav-dot" />
                <span class="lp-nav-label">{{ child.title }}</span>
              </router-link>
            </div>
          </div>
        </template>
      </div>
    </nav>

    <div class="lp-sidebar-foot">
      <div class="lp-version-row">
        <span
          class="lp-version-text"
          style="cursor:pointer"
          :title="panel.checking ? '正在检查更新…' : '点击检查更新'"
          @click="panel.checkForUpdate(true)"
        >v{{ panel.version || '0.1.0' }}</span>
        <template v-if="panel.hasUpdate">
          <el-badge is-dot class="lp-update-dot" />
          <el-button
            v-if="!collapsed"
            type="primary"
            size="small"
            class="lp-update-btn"
            :loading="panel.checking"
            @click="onUpdateClick"
          >
            更新
          </el-button>
        </template>
        <el-icon v-else-if="panel.checking" class="lp-version-spin" :size="12"><component is="Loading" /></el-icon>
      </div>
    </div>
  </aside>

  <!-- 升级确认弹窗：告知新版本信息与更新内容，确认后进入升级进度弹窗 -->
  <el-dialog
    v-model="upgradeConfirmVisible"
    class="lp-upgrade-confirm"
    width="min(680px, 94vw)"
    :close-on-click-modal="false"
    :close-on-press-escape="false"
    align-center
  >
    <template #header>
      <div class="lp-uc-head">
        <div class="lp-uc-icon">
          <el-icon :size="22"><Promotion /></el-icon>
        </div>
        <div class="lp-uc-head-text">
          <div class="lp-uc-title">发现新版本 v{{ panel.updateVersion }}</div>
          <div class="lp-uc-sub">当前版本 v{{ panel.version }} · 开猿运维面板</div>
        </div>
      </div>
    </template>

    <div class="lp-uc-body">
      <div class="lp-uc-tags">
        <span class="lp-uc-tag">后端程序</span>
        <span v-if="panel.updateWebURL" class="lp-uc-tag">前端界面</span>
        <span v-if="panel.updateXdbURL" class="lp-uc-tag">离线IP库</span>
      </div>
      <div class="lp-uc-log-head">更新日志</div>
      <pre class="lp-uc-log">{{ panel.updateChangelog || '暂无更新日志' }}</pre>
    </div>

    <template #footer>
      <div class="lp-uc-foot">
        <span class="lp-uc-foot-tip">升级将自动备份数据，失败自动回滚恢复，期间请勿关闭页面</span>
        <div class="lp-uc-btns">
          <el-button @click="upgradeConfirmVisible = false">取消</el-button>
          <el-button type="primary" :loading="panel.upgrading" @click="onUpgradeConfirm">立即升级</el-button>
        </div>
      </div>
    </template>
  </el-dialog>

  <!-- 升级进度弹窗：升级过程中不可关闭，实时展示各阶段进度，完成后自动刷新 -->
  <el-dialog
    v-model="upgradeDialogVisible"
    class="lp-upgrade-progress"
    title="正在升级面板"
    width="min(520px, 92vw)"
    :show-close="upgradePhase === 'failed'"
    :close-on-click-modal="upgradePhase === 'failed'"
    :close-on-press-escape="upgradePhase === 'failed'"
    :before-close="onUpgradeDialogClose"
    align-center
  >
    <div class="lp-upgrade-body">
      <el-progress
        :percentage="upgradePercent ?? 0"
        :indeterminate="upgradePercent === null"
        :stroke-width="8"
        :duration="1.2"
        color="#10b981"
      />
      <p class="lp-upgrade-status" :class="{ 'is-error': upgradePhase === 'failed' }">
        {{ upgradeMessage }}
      </p>
      <p v-if="upgradePhase === 'downloading'" class="lp-upgrade-sub">{{ upgradeDownloadInfo }}</p>
      <p v-else-if="upgradePhase === 'rebooting'" class="lp-upgrade-sub">请勿关闭此窗口，面板重启完成后会自动刷新页面</p>
      <p v-if="upgradePhase === 'failed' && upgradeError" class="lp-upgrade-error">{{ upgradeError }}</p>
    </div>
    <template #footer>
      <template v-if="upgradePhase === 'failed'">
        <el-button @click="copyUpgradeError">复制错误信息</el-button>
        <el-button type="primary" @click="upgradeDialogVisible = false">知道了</el-button>
      </template>
      <span v-else class="lp-upgrade-tip">升级期间请保持页面打开，不要刷新或关闭</span>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Promotion } from '@element-plus/icons-vue'
import { getUpgradeProgress } from '../api/update'
import { useAuthStore } from '../stores/auth'
import { usePanelStore } from '../stores/panel'

const props = defineProps({
  collapsed: { type: Boolean, default: false }
})

defineEmits(['toggle-collapse'])

const route = useRoute()
const auth = useAuthStore()
const panel = usePanelStore()
const openGroups = ref(['system'])

// 按权限过滤菜单：无 perm 的项始终显示；有 perm 的项需有对应权限
const filteredMenu = computed(() => menu.filter((item) => !item.perm || auth.hasPermission(item.perm)))

const menu = [
  { path: '/dashboard', title: '概览', icon: 'Odometer', perm: 'dashboard' },
  { path: '/websites', title: '网站', icon: 'Monitor', perm: 'site' },
  { path: '/database', title: '数据库', icon: 'Coin', perm: 'database' },
  { path: '/ftp', title: 'FTP', icon: 'UploadFilled', perm: 'ftp' },
  { path: '/container', title: 'Docker', icon: 'Box', perm: 'container' },
  { path: '/monitor', title: '监控', icon: 'TrendCharts', perm: 'monitor' },
  { path: '/logs', title: '日志', icon: 'Document', perm: 'log' },
  { path: '/firewall', title: '防火墙', icon: 'Lock', perm: 'firewall' },
  { path: '/apps', title: '应用商店', icon: 'Grid', perm: 'appstore' },
  { path: '/cron', title: '计划任务', icon: 'Timer', perm: 'cron' },
  { path: '/files', title: '文件管理', icon: 'FolderOpened', perm: 'file' },
  { path: '/process', title: '进程管理', icon: 'Cpu', perm: 'process' },
  { path: '/backup', title: '备份中心', icon: 'Files', perm: 'backup' },
  { path: '/users', title: '用户管理', icon: 'User', perm: 'settings' },
  { path: '/settings', title: '设置', icon: 'Setting', perm: 'settings' }
]

const isActive = computed(() => (item) => {
  if (item.path === '/dashboard') return route.path === item.path || route.path === '/'
  return route.path === item.path || route.path.startsWith(item.path + '/')
})

function isGroupActive(item) {
  return route.path.startsWith(item.path)
}

function toggleGroup(path) {
  const i = openGroups.value.indexOf(path)
  if (i >= 0) openGroups.value.splice(i, 1)
  else openGroups.value.push(path)
}

// ============ 升级确认弹窗状态 ============
const upgradeConfirmVisible = ref(false)

async function onUpgradeConfirm() {
  if (panel.upgrading) return
  try {
    upgradeConfirmVisible.value = false
    await panel.doUpgrade()
    startUpgradeProgress()
  } catch (err) {
    ElMessage.error(err?.response?.data?.msg || err.message || '升级失败')
  }
}

// ============ 升级进度弹窗状态 ============
const upgradeDialogVisible = ref(false)
const upgradePhase = ref('downloading') // downloading / applying / rebooting / done / failed
const upgradeMessage = ref('正在启动升级…')
const upgradePercent = ref(null) // null = 不确定进度（流动条），数字 = 下载百分比
const upgradeDownloadInfo = ref('')
const upgradeError = ref('')
let upgradeTimer = null
let upgradePollFails = 0

function fmtSize(bytes) {
  if (!bytes || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  const i = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)))
  return (bytes / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1) + ' ' + units[i]
}

// 升级过程中不允许关闭弹窗，仅失败时可关闭
function onUpgradeDialogClose() {
  return upgradePhase.value === 'failed'
}

// 复制失败错误信息，方便用户粘贴给客服/去社区求助
async function copyUpgradeError() {
  const text = upgradeError.value || upgradeMessage.value || ''
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('错误信息已复制')
  } catch {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    try {
      document.execCommand('copy')
      ElMessage.success('错误信息已复制')
    } catch {
      ElMessage.error('复制失败，请长按手动复制')
    }
    document.body.removeChild(ta)
  }
}

function onUpdateClick() {
  upgradeConfirmVisible.value = true
}

// 更新进度 UI
function updateUpgradeUI(d) {
  if (d?.phase) upgradePhase.value = d.phase
  if (d?.message) upgradeMessage.value = d.message
  if (d?.phase === 'downloading') {
    if (d.total > 0) {
      upgradePercent.value = Math.min(100, Math.max(0, Math.round((d.downloaded / d.total) * 100)))
      upgradeDownloadInfo.value = `${d.file || '文件'}  ${fmtSize(d.downloaded)} / ${fmtSize(d.total)}`
    } else {
      upgradePercent.value = null
      upgradeDownloadInfo.value = `正在下载 ${d.file || '文件'}…`
    }
  } else {
    upgradePercent.value = null
    upgradeDownloadInfo.value = ''
  }
}

// 打开升级进度弹窗并轮询后端进度，直到完成或失败
function startUpgradeProgress() {
  upgradeDialogVisible.value = true
  upgradePhase.value = 'downloading'
  upgradeMessage.value = '正在启动升级…'
  upgradePercent.value = null
  upgradeDownloadInfo.value = ''
  upgradeError.value = ''
  upgradePollFails = 0
  clearInterval(upgradeTimer)

  upgradeTimer = setInterval(async () => {
    try {
      const { data } = await getUpgradeProgress()
      upgradePollFails = 0
      updateUpgradeUI(data)

      // 兜底：面板重启后新进程状态已重置（phase 为空/idle）。若此前已进入 rebooting
      // 且后端不再报 running，说明重启已完成，直接刷新加载最新版本
      if (
        upgradePhase.value === 'rebooting' &&
        data?.running === false &&
        !['downloading', 'applying', 'rebooting', 'done', 'failed'].includes(data?.phase)
      ) {
        clearInterval(upgradeTimer)
        upgradePhase.value = 'done'
        upgradeMessage.value = data?.message || '面板已重启完成，正在刷新…'
        panel.upgrading = false
        setTimeout(() => window.location.reload(), 800)
        return
      }

      if (data?.phase === 'done') {
        clearInterval(upgradeTimer)
        upgradePhase.value = 'done'
        upgradeMessage.value = data?.message || '更新完成！页面即将刷新'
        panel.upgrading = false
        setTimeout(() => window.location.reload(), 1500)
        return
      }
      if (data?.phase === 'failed') {
        clearInterval(upgradeTimer)
        upgradePhase.value = 'failed'
        upgradeMessage.value = data?.message || '升级失败'
        upgradeError.value = data?.error || ''
        panel.upgrading = false
      }
    } catch (e) {
      // 请求失败通常意味着面板正在重启（连接断开），继续轮询等待恢复
      upgradePollFails++
      if (upgradePollFails > 2) {
        upgradePhase.value = 'rebooting'
        upgradeMessage.value = '面板正在重启，请稍候…'
        upgradePercent.value = null
      }
      if (upgradePollFails >= 90) {
        clearInterval(upgradeTimer)
        upgradePhase.value = 'failed'
        upgradeMessage.value = '等待面板恢复超时，请手动刷新页面查看'
        panel.upgrading = false
      }
    }
  }, 1000)
}
</script>

<style scoped>
.lp-sidebar {
  width: 224px;
  height: 100%;
  display: flex;
  flex-direction: column;
  background: #ffffff;
  border-right: 1px solid #e5e7eb;
  transition: width 0.2s ease;
  overflow: hidden;
}

.lp-sidebar.is-collapsed {
  width: 64px;
}

.lp-sidebar-nav {
  flex: 1;
  overflow-y: auto;
  overflow-x: hidden;
  padding: 12px 10px;
}

.lp-sidebar-section {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.lp-nav-item {
  display: flex;
  align-items: center;
  gap: 10px;
  height: 40px;
  padding: 0 12px;
  border-radius: 8px;
  color: #475569;
  font-size: 14px;
  text-decoration: none;
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
  white-space: nowrap;
  width: 100%;
  border: none;
  background: transparent;
  text-align: left;
}
.lp-nav-item:hover {
  background: #f1f5f9;
  color: #0f172a;
}
.lp-nav-item.active {
  background: #ecfdf5;
  color: #059669;
  font-weight: 600;
}
.lp-nav-item.active .lp-nav-icon {
  color: #10b981;
}

.lp-nav-icon {
  flex-shrink: 0;
  color: #94a3b8;
}
.lp-nav-item.active .lp-nav-icon {
  color: #10b981;
}

.lp-nav-label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}

.lp-nav-caret {
  flex-shrink: 0;
  color: #94a3b8;
  transition: transform 0.2s;
}
.lp-nav-caret.open {
  transform: rotate(180deg);
}

/* 子菜单 */
.lp-nav-sub {
  max-height: 0;
  overflow: hidden;
  transition: max-height 0.25s ease;
}
.lp-nav-sub.open {
  max-height: 160px;
}
.lp-nav-child {
  height: 36px;
  padding-left: 32px;
  font-size: 13px;
}
.lp-nav-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #cbd5e1;
  flex-shrink: 0;
}
.lp-nav-child.active .lp-nav-dot {
  background: #10b981;
}

/* 折叠态：只显示图标 */
.lp-sidebar.is-collapsed .lp-nav-label,
.lp-sidebar.is-collapsed .lp-nav-caret,
.lp-sidebar.is-collapsed .lp-nav-dot {
  display: none;
}
.lp-sidebar.is-collapsed .lp-nav-item {
  justify-content: center;
  padding: 0;
}
.lp-sidebar.is-collapsed .lp-nav-sub {
  display: none;
}

.lp-sidebar-foot {
  flex-shrink: 0;
  padding: 12px 16px;
  border-top: 1px solid #f1f5f9;
}
.lp-version-row {
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 24px;
}
.lp-version-text {
  font-size: 12px;
  color: #94a3b8;
  white-space: nowrap;
}
.lp-update-dot :deep(.el-badge__content) {
  top: 4px;
  right: -2px;
}
.lp-update-btn {
  padding: 1px 8px;
  height: 22px;
  font-size: 12px;
  border-radius: 4px;
}
.lp-version-spin {
  font-size: 12px;
  color: #94a3b8;
  animation: lp-spin 1s linear infinite;
}
@keyframes lp-spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}
.lp-sidebar.is-collapsed .lp-version-row {
  justify-content: center;
}
.lp-sidebar.is-collapsed .lp-version-text,
.lp-sidebar.is-collapsed .lp-update-btn {
  display: none;
}

/* ============ 升级确认弹窗 ============ */
.lp-upgrade-confirm {
  border-radius: 14px;
}
.lp-upgrade-confirm :deep(.el-dialog__header) {
  margin-right: 0;
  padding: 20px 24px 0;
}
.lp-upgrade-confirm :deep(.el-dialog__body) {
  padding: 16px 24px 0;
}
.lp-upgrade-confirm :deep(.el-dialog__footer) {
  padding: 16px 24px 20px;
}
.lp-uc-head {
  display: flex;
  align-items: center;
  gap: 12px;
}
.lp-uc-icon {
  width: 44px;
  height: 44px;
  border-radius: 12px;
  background: linear-gradient(135deg, #10b981, #059669);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.lp-uc-head-text {
  min-width: 0;
}
.lp-uc-title {
  font-size: 17px;
  font-weight: 700;
  color: #0f172a;
  line-height: 1.4;
}
.lp-uc-sub {
  font-size: 12.5px;
  color: #94a3b8;
  margin-top: 2px;
}
.lp-uc-body {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.lp-uc-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.lp-uc-tag {
  font-size: 12px;
  padding: 3px 10px;
  border-radius: 999px;
  background: #ecfdf5;
  color: #047857;
  border: 1px solid #a7f3d0;
}
.lp-uc-log-head {
  font-size: 13px;
  font-weight: 600;
  color: #475569;
}
.lp-uc-log {
  width: 100%;
  box-sizing: border-box;
  margin: 0;
  padding: 14px 16px;
  background: #f8fafc;
  border: 1px solid #e2e8f0;
  border-radius: 10px;
  max-height: 300px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-word;
  font-size: 13px;
  line-height: 1.75;
  color: #334155;
}
.lp-uc-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.lp-uc-foot-tip {
  font-size: 12px;
  color: #94a3b8;
}
.lp-uc-btns {
  display: flex;
  gap: 10px;
}
@media (max-width: 599px) {
  .lp-upgrade-confirm :deep(.el-dialog__header) {
    padding: 16px 16px 0;
  }
  .lp-upgrade-confirm :deep(.el-dialog__body) {
    padding: 12px 16px 0;
  }
  .lp-upgrade-confirm :deep(.el-dialog__footer) {
    padding: 14px 16px 16px;
  }
  .lp-uc-foot {
    flex-direction: column;
    align-items: stretch;
  }
  .lp-uc-foot-tip {
    text-align: center;
  }
  .lp-uc-btns {
    justify-content: flex-end;
  }
}
.lp-upgrade-progress {
  border-radius: 14px;
}

/* ============ 升级进度弹窗 ============ */
.lp-upgrade-body {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 6px 2px 2px;
}
.lp-upgrade-status {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  color: #0f172a;
  line-height: 1.5;
}
.lp-upgrade-status.is-error {
  color: #dc2626;
}
.lp-upgrade-sub {
  margin: 0;
  font-size: 13px;
  color: #64748b;
  line-height: 1.5;
}
.lp-upgrade-error {
  margin: 0;
  padding: 8px 10px;
  background: #fef2f2;
  border: 1px solid #fecaca;
  border-radius: 6px;
  font-size: 12px;
  color: #b91c1c;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 160px;
  overflow: auto;
}
.lp-upgrade-tip {
  font-size: 12px;
  color: #94a3b8;
}
</style>
