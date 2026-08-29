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
</template>

<script setup>
import { ref, computed, h } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
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

async function onUpdateClick() {
  try {
    await ElMessageBox.confirm(
      h('div', { style: 'line-height:1.6' }, [
        h('p', { style: 'margin:0 0 8px;font-weight:600' }, `发现新版本 v${panel.updateVersion}，是否立即升级？`),
        h('pre', {
          style: 'margin:0;padding:10px;background:#f8fafc;border-radius:6px;max-height:240px;overflow:auto;white-space:pre-wrap;word-break:break-word;font-size:13px;color:#334155'
        }, panel.updateChangelog || '暂无更新日志')
      ]),
      '面板升级确认',
      {
        confirmButtonText: '立即升级',
        cancelButtonText: '取消',
        type: 'warning',
        closeOnClickModal: false
      }
    )
  } catch {
    return
  }

  ElMessage.info('正在下载更新包并准备重启，请稍候…')
  try {
    await panel.doUpgrade()
    startPolling()
  } catch (err) {
    ElMessage.error(err?.response?.data?.msg || err.message || '升级失败')
  }
}

function startPolling() {
  let attempts = 0
  const maxAttempts = 60
  const timer = setInterval(async () => {
    attempts++
    try {
      // 轮询 /api/ping，通则面板已重启完成
      await fetch('/api/ping', { method: 'GET' })
      clearInterval(timer)
      panel.upgrading = false
      ElMessage.success('升级完成，页面即将刷新')
      setTimeout(() => window.location.reload(), 1500)
    } catch {
      if (attempts >= maxAttempts) {
        clearInterval(timer)
        panel.upgrading = false
        ElMessage.warning('升级状态未知，请手动刷新页面查看')
      }
    }
  }, 2000)
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
</style>
