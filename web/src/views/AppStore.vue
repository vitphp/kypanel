<template>
  <div class="app-store">
    <!-- 分类 Tab + 搜索框：同行布局（左 tab，右搜索） -->
    <div class="app-store-toolbar">
      <el-tabs v-model="activeCat" class="cat-tabs">
        <el-tab-pane label="全部" name="all" />
        <el-tab-pane v-for="cat in categories" :key="cat.key" :name="cat.key">
          <template #label>
            <span class="cat-label">{{ cat.label }}</span>
          </template>
        </el-tab-pane>
        <el-tab-pane name="installed">
          <template #label>
            <span class="cat-label">已安装<span v-if="installedCount" class="cat-badge">{{ installedCount }}</span></span>
          </template>
        </el-tab-pane>
      </el-tabs>

      <!-- 搜索框：在当前选中 Tab 分类下搜索应用 -->
      <el-input
        v-model="searchKeyword"
        placeholder="搜索当前分类下的应用"
        clearable
        :prefix-icon="Search"
        class="app-search-input"
      />
    </div>

    <!-- 运行时环境的语言二级分类 -->
    <div v-if="activeCat === 'runtime' && currentCat?.sub_categories?.length" class="sub-tabs">
      <span :class="['sub-pill', { active: !activeSubcat }]" @click="activeSubcat = ''">全部</span>
      <span v-for="sc in currentCat.sub_categories" :key="sc.key"
            :class="['sub-pill', { active: activeSubcat === sc.key }]"
            @click="activeSubcat = sc.key">
        <el-icon><component :is="sc.icon" /></el-icon>
        {{ sc.label }}
      </span>
    </div>

    <div v-if="activeCat !== 'all' && activeCat !== 'installed' && currentCatDesc" class="cat-desc">
      {{ currentCatDesc }}
    </div>
    <div v-else-if="activeCat === 'installed'" class="cat-desc">
      共 {{ installedApps.length }} 个已安装应用
    </div>

    <!-- 应用卡片 -->
    <Skeleton v-if="loading" type="card" :rows="8" />
    <el-empty v-else-if="filteredApps.length === 0" :description="searchKeyword ? '没有匹配的应用' : '暂无应用'" :image-size="90" />
    <div v-else class="app-grid">
      <div v-for="app in filteredApps" :key="app.key" class="app-col">
        <el-card shadow="hover" class="app-card">
          <div class="app-head">
            <div class="app-icon" :style="{ background: iconColor(app.icon) }">
              <el-icon :size="26" color="#fff"><component :is="app.icon" /></el-icon>
            </div>
            <div class="app-title">
              <div class="app-name">{{ app.name }}</div>
              <el-tag size="small" :type="statusTag(app.status)">{{ statusText(app.status) }}</el-tag>
              <el-tag v-if="app.system_default" size="small" type="info" effect="plain">系统自带</el-tag>
            </div>
          </div>
          <el-alert
            v-show="app.error && !dismissedErrors.has(app.key)"
            :title="app.error"
            type="error"
            :closable="true"
            show-icon
            class="app-error"
            @close="dismissError(app.key)"
          >
            <template #title>
              <span class="app-error-text" :title="app.error">{{ app.error }}</span>
            </template>
          </el-alert>
          <div class="app-desc">{{ app.description }}</div>
          <div class="app-meta">
            <span class="cat-tag">{{ categoryText(app.category) }}</span>
          </div>
          <div class="app-actions">
            <template v-if="app.status === 'not_installed'">
              <el-button type="primary" size="small" @click="install(app)">一键安装</el-button>
            </template>
            <template v-else-if="app.status === 'queued' || app.status === 'installing' || app.status === 'uninstalling'">
              <el-button type="warning" size="small" disabled>
                {{ app.status === 'queued' ? '排队中...' : (app.status === 'installing' ? '安装中...' : '卸载中...') }}
              </el-button>
              <el-button size="small" @click="openLog(app)">查看日志</el-button>
            </template>
            <template v-else-if="app.status === 'failed'">
              <el-button type="primary" size="small" @click="install(app)">重新安装</el-button>
              <el-button size="small" @click="openLog(app)">日志</el-button>
            </template>
            <template v-else>
              <template v-if="app.service_name">
                <el-tag size="small" :type="runningKeys.has(app.key) ? 'success' : 'info'" effect="light" class="status-tag">
                  {{ runningKeys.has(app.key) ? '运行中' : '已停止' }}
                </el-tag>
                <el-button v-if="!runningKeys.has(app.key)" type="primary" size="small"
                  :loading="isActing(app.key, 'start')" :disabled="isActing(app.key)"
                  @click="serviceAction(app, 'start')">启动</el-button>
                <el-button v-else size="small"
                  :loading="isActing(app.key, 'stop')" :disabled="isActing(app.key)"
                  @click="serviceAction(app, 'stop')">停止</el-button>
                <el-button v-if="runningKeys.has(app.key)" size="small"
                  :loading="isActing(app.key, 'restart')" :disabled="isActing(app.key)"
                  @click="serviceAction(app, 'restart')">重启</el-button>
              </template>
              <el-button
                v-for="act in app.installed_actions || []"
                :key="act.key"
                size="small"
                :type="act.type || 'default'"
                @click="onCustomAction(app, act)"
              >{{ act.label }}</el-button>
              <el-tooltip
                v-if="app.system_default && isLangRuntimeApp(app)"
                content="系统自带运行环境，卸载会误伤系统依赖，不支持卸载"
                placement="top"
              >
                <span>
                  <el-button size="small" type="danger" plain disabled>卸载</el-button>
                </span>
              </el-tooltip>
              <el-button v-else size="small" type="danger" :disabled="isActing(app.key)" @click="uninstall(app)">卸载</el-button>
            </template>
          </div>
          <div v-if="app.remarks" class="app-remarks">
            <el-icon><InfoFilled /></el-icon>
            <span class="remarks-text">{{ app.remarks }}</span>
          </div>
        </el-card>
      </div>
    </div>

    <!-- 网站搬家（迁移）弹窗 -->
    <MigrateDialog v-model="migrateVisible" />

    <!-- 日志抽屉 -->
    <el-drawer v-model="logVisible" :title="`安装日志 - ${logKey}`" size="55%">
      <div class="log-wrap">
        <pre ref="logBoxRef" class="log-box" @scroll="onLogScroll">{{ logText }}</pre>
        <div v-if="!stickToBottom && hasLog" class="log-follow-tip">
          <el-button size="small" type="primary" @click="scrollLogToBottom">回到最新 ↓</el-button>
        </div>
      </div>
    </el-drawer>

    <!-- 选择 PHP 版本 -->
    <el-dialog v-model="phpDialogVisible" :title="`选择 PHP 版本 - ${phpDialogApp?.name || ''}`" width="420px">
      <el-form label-width="80px">
        <el-form-item label="PHP 版本">
          <el-select v-model="selectedPhpVersion" style="width: 100%" :disabled="phpVersions.length === 0">
            <el-option v-if="phpVersions.length === 0" label="自动安装 PHP 7.4" value="7.4" />
            <el-option v-for="v in phpVersions" :key="v" :label="'PHP ' + v" :value="v" />
          </el-select>
        </el-form-item>
        <el-form-item label="说明">
          <div class="php-dialog-tip">
            <template v-if="phpVersions.length > 0">默认使用已安装的最高 PHP 版本；</template>
            <template v-else>未检测到已安装的 PHP 版本，安装时将自动安装 PHP 7.4 兜底；</template>
            安装后通过「数据库」- MySQL 的「管理」免密进入 phpMyAdmin。
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="phpDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="phpInstallLoading" @click="confirmPhpInstall">开始安装</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, watch, nextTick, reactive } from 'vue'
import { ElMessage, ElMessageBox, ElLoading } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import request from '../utils/request'
import { playFailSound, playSuccessSound, playStartSound } from '../utils/sound'
import { useInstallTrackerStore } from '../stores/installTracker'
import MigrateDialog from '../components/MigrateDialog.vue'

const apps = ref([])
const loading = ref(true)
// 记录各应用上一次的安装状态，用于检测 installing/uninstalling -> failed 的状态切换
const prevAppStatus = {}
const categories = ref([])
// 应用商店 Tab 选择持久化（localStorage）：
// 下次进入 /apps 时停留在上次选中的 Tab（顶级分类 + 二级分组）。
// 已安装 Tab 也参与持久化；不存在时由 loadCategories 完成后的合法性校验回退到 'all'。
const ACTIVE_CAT_KEY = 'lp_appstore_active_cat'
const ACTIVE_SUBCAT_KEY = 'lp_appstore_active_subcat'
function readPersistedTab(key) {
  try {
    const v = localStorage.getItem(key)
    return v == null ? null : v
  } catch (e) { return null }
}
// 注意：activeCat / activeSubcat 初始值固定为默认，持久化值由 loadCategories 完成后覆盖，
// 避免空渲染时引用不存在的分类导致 el-tabs 短暂异常。
const activeCat = ref('all')
const activeSubcat = ref('')
const searchKeyword = ref('')
// 响应式卡片列数改用 CSS grid 媒体查询（见 .app-grid），无需 JS 监听

const runningKeys = ref(new Set())
// 用户已关闭过的应用错误提示（持久化到 localStorage，刷新/重进不再显示）
const DISMISSED_ERR_KEY = 'lp_dismissed_app_errors'
function loadDismissedErrors() {
  try {
    const raw = localStorage.getItem(DISMISSED_ERR_KEY)
    if (!raw) return new Set()
    const arr = JSON.parse(raw)
    return new Set(Array.isArray(arr) ? arr : [])
  } catch (e) { return new Set() }
}
const dismissedErrors = ref(loadDismissedErrors())
function dismissError(key) {
  if (!key) return
  dismissedErrors.value.add(key)
  // 触发响应式更新（Set 本身不是深度响应，用 triggerRef 显式通知）
  dismissedErrors.value = new Set(dismissedErrors.value)
  try {
    localStorage.setItem(DISMISSED_ERR_KEY, JSON.stringify(Array.from(dismissedErrors.value)))
  } catch (e) { /* 忽略 */ }
}
const logVisible = ref(false)
const logKey = ref('')
const logText = ref('')
const logBoxRef = ref(null)
// 安装 phpMyAdmin 等需要选择 PHP 版本的应用
const phpDialogVisible = ref(false)
const phpDialogApp = ref(null)
const phpVersions = ref([])
const migrateVisible = ref(false)
const selectedPhpVersion = ref('')
const phpInstallLoading = ref(false)
// 是否贴底：只在用户停在底部时自动跟新日志滚到底；用户主动上滑查看历史时不拽回
const stickToBottom = ref(true)
const hasLog = ref(false)

// 「已安装」Tab 仅展示真正安装成功的应用（status=installed）。
// failed 应用回归到原分类（带红色错误提示引导重新安装），避免在「已安装」里出现
// 「已失败但被归类为已安装」的矛盾体验。
const installedApps = computed(() =>
  apps.value.filter((a) => a.status === 'installed')
)
const installedCount = computed(() => installedApps.value.length)
const currentCatDesc = computed(() => {
  const c = categories.value.find((x) => x.key === activeCat.value)
  return c ? c.desc : ''
})
const currentCat = computed(() => categories.value.find((x) => x.key === activeCat.value))
const filteredApps = computed(() => {
  let list
  if (activeCat.value === 'all') list = apps.value
  else if (activeCat.value === 'installed') list = installedApps.value
  else {
    list = apps.value.filter((a) => a.category === activeCat.value)
    if (activeCat.value === 'runtime' && activeSubcat.value) {
      list = list.filter((a) => a.sub_category === activeSubcat.value)
    }
  }
  // 搜索过滤：按名称 / 描述 / 分类名模糊匹配
  const kw = searchKeyword.value.trim().toLowerCase()
  if (kw) {
    list = list.filter((a) => {
      const name = (a.name || '').toLowerCase()
      const desc = (a.description || '').toLowerCase()
      const cat = (categoryText(a.category) || '').toLowerCase()
      return name.includes(kw) || desc.includes(kw) || cat.includes(kw)
    })
  }
  return list
})

let listTimer = null
let logTimer = null

const categoryMap = computed(() => {
  const m = {}
  categories.value.forEach((c) => { m[c.key] = c.label })
  return m
})
const statusMap = {
  not_installed: '未安装',
  queued: '排队中',
  installing: '安装中',
  installed: '已安装',
  uninstalling: '卸载中',
  failed: '安装失败'
}
const statusTypeMap = {
  not_installed: 'info',
  queued: 'warning',
  installing: 'warning',
  installed: 'success',
  uninstalling: 'warning',
  failed: 'danger'
}
const iconColors = ['#409eff', '#67c23a', '#e6a23c', '#f56c6c', '#909399', '#9c27b0']

function categoryText(c) { return categoryMap.value[c] || c }
function statusText(s) { return statusMap[s] || s }
function statusTag(s) { return statusTypeMap[s] || 'info' }
function iconColor(icon) {
  let h = 0
  for (const ch of (icon || '')) h = (h * 31 + ch.charCodeAt(0)) % 997
  return iconColors[h % iconColors.length]
}

async function loadCategories() {
  try {
    const res = await request.get('/apps/categories')
    categories.value = res.data
    // 持久化的 activeCat 在新分类列表里不存在 → 回退到 'all'（分类被删除/改 key 时）
    const validCats = new Set(['all', 'installed'])
    categories.value.forEach((c) => validCats.add(c.key))
    // 首次进入时 activeCat 是 'all'（避免空渲染时引用不存在的分类）。
    // 若 localStorage 里有持久化值且合法，覆盖回去（实现 Tab 持久化）。
    const persisted = readPersistedTab(ACTIVE_CAT_KEY)
    const persistedSub = readPersistedTab(ACTIVE_SUBCAT_KEY) || ''
    if (persisted && validCats.has(persisted)) {
      activeCat.value = persisted
    }
    // 持久化的 activeSubcat 在当前分类的 sub_categories 里不存在 → 清空
    const cur = categories.value.find((x) => x.key === activeCat.value)
    const validSubs = new Set((cur && cur.sub_categories) ? cur.sub_categories.map((s) => s.key) : [])
    if (persistedSub && validSubs.has(persistedSub)) {
      activeSubcat.value = persistedSub
    } else {
      activeSubcat.value = ''
    }
  } catch (e) { /* ignore */ }
}

async function loadApps() {
  try {
    const res = await request.get('/apps/list')
    apps.value = res.data
    // 检测状态变化 -> 仅用于 AppStore 卡片内的 ElMessage 错误提示
    // 注意：浮窗（installTracker）的任务列表完全以 /apps/tasks 为真相源（见 InstallFloater.syncTasks），
    // 这里不再写 tracker，避免 list 里 status 字段残留（apt-get 子进程已结束但 status 未刷）造成
    // 「浮窗显示任务 / 取消时报『该应用没有正在执行的任务』」的脱钩问题。
    const firstLoad = Object.keys(prevAppStatus).length === 0 // 首次进入本页面：不响音效
    res.data.forEach((a) => {
      const prev = prevAppStatus[a.key]
      const cur = a.status
      if (cur === 'failed' && (prev === 'installing' || prev === 'uninstalling' || prev === 'queued')) {
        if (!firstLoad) playFailSound()
        ElMessage.error(`${a.name} ${prev === 'installing' || prev === 'queued' ? '安装' : '卸载'}失败，请查看日志后重试`)
      }
      prevAppStatus[a.key] = cur
    })
    // 刷新运行状态（已安装的）：内部 10s 节流，避免每 3s 对每个已装应用打 service/status
    loadRunningKeys()
  } finally {
    loading.value = false
  }
}

// 运行状态探测节流：/apps/service/status 后端是 systemctl is-active，
// 在 systemctl 异常的环境下每个请求可能挂起数秒，不能随 3s 列表轮询反复轰炸，
// 只允许每 10s 刷新一次；手动操作服务后 force 立即刷新。
let lastRunningCheck = 0
const RUNNING_CHECK_INTERVAL = 10000

async function loadRunningKeys(force = false) {
  const now = Date.now()
  if (!force && now - lastRunningCheck < RUNNING_CHECK_INTERVAL) return
  lastRunningCheck = now
  const running = new Set()
  await Promise.all(
    apps.value
      .filter((a) => a.status === 'installed' && a.source !== 'system')
      .map(async (a) => {
        try {
          const r = await request.get('/apps/service/status', { params: { key: a.key } })
          if (r.data.status === 'active') running.add(a.key)
        } catch (e) { /* ignore */ }
      })
  )
  runningKeys.value = running
}

async function install(app) {
  // Nginx 与 Apache 互斥：只能安装其中一个（两者抢占 80/443 端口）
  if (app.key === 'nginx' || app.key === 'apache') {
    if (await webServerConflicted(app.key)) return
  }
  // 依赖检查：PHP/Go/Node/Python 等运行环境需要 Web 服务器（Nginx），没有则先装 Nginx
  if (needsWebServer(app)) {
    const ok = await ensureWebServer(app)
    if (!ok) return // 用户取消，或 Nginx 安装失败
  }
  if (app.select_php_version) {
    return openPhpVersionDialog(app)
  }
  // Python 多版本走源码编译（pyenv），耗时显著长于其他运行时，明确告知预期时长
  const pythonCompileTip = app.key.startsWith('python')
    ? '\nPython 为源码编译安装，预计需要 10~20 分钟，期间请勿关闭或刷新页面。'
    : ''
  try {
    await ElMessageBox.confirm(`确定安装 ${app.name} 吗？${app.remarks ? '注意：' + app.remarks : ''}${pythonCompileTip}`, '安装应用', {
      confirmButtonText: '开始安装',
      cancelButtonText: '取消',
      type: 'info'
    })
  } catch (_) { return }
  try {
    await request.post('/apps/install', { key: app.key })
    // 记录发起安装，供轮询检测 installing -> failed 的失败切换
    prevAppStatus[app.key] = 'installing'
    // 写入全局任务队列，右下角浮窗会立即显示（不再自动弹大弹窗，避免遮挡）
    const tracker = useInstallTrackerStore()
    tracker.unmarkRemoved(app.key) // 用户主动触发：先清除"被移除"标记
    tracker.upsert({ key: app.key, name: app.name, action: 'install', status: 'queued', message: '排队等待中...' })
    ElMessage.success('已开始安装，请稍候...')
    startPolling()
  } catch (e) {
    // 安装请求失败（错误提示由 request 拦截器统一弹出），播放卖点提示音效
    playFailSound()
  }
}

// 需要 Web 服务器配合的应用（运行时环境、phpMyAdmin）
function needsWebServer(app) {
  return app.category === 'runtime' || app.key === 'phpmyadmin'
}

// Nginx / Apache 互斥检测：对方已安装（含安装中）时提示并返回 true
async function webServerConflicted(key) {
  if (!apps.value.length) {
    try { await loadApps() } catch (e) { /* ignore */ }
  }
  const otherKey = key === 'nginx' ? 'apache' : 'nginx'
  const otherName = key === 'nginx' ? 'Apache' : 'Nginx'
  const other = apps.value.find((a) => a.key === otherKey)
  if (other && (other.status === 'installed' || other.status === 'installing')) {
    ElMessage.warning(
      `检测到系统已安装 ${otherName}。Nginx 与 Apache 同时安装会抢占 80/443 端口导致冲突，请先在「应用商店」卸载 ${otherName} 后再安装`
    )
    return true
  }
  return false
}

// 确保已安装 Web 服务器：未安装 Nginx 时提示先装，装完后再继续安装目标应用
async function ensureWebServer(app) {
  // 页面应用列表可能尚未加载，先拉一次
  if (!apps.value.length) {
    try { await loadApps() } catch (e) { /* ignore */ }
  }
  const nginx = apps.value.find((a) => a.key === 'nginx')
  if (nginx && (nginx.status === 'installed' || nginx.status === 'installing')) {
    return true // 已有 Nginx（含正在安装中）
  }
  // 互斥检查：已装 Apache 时不能再装 Nginx
  const apache = apps.value.find((a) => a.key === 'apache')
  if (apache && (apache.status === 'installed' || apache.status === 'installing')) {
    ElMessage.warning(
      `检测到系统已安装 Apache。Nginx 与 Apache 同时安装会抢占 80/443 端口导致冲突，请先在「应用商店」卸载 Apache 后再安装 ${app.name}`
    )
    return false
  }
  // 未检测到 Nginx：提示是否先安装
  try {
    await ElMessageBox.confirm(
      `当前服务器还没有安装 Nginx。\nPHP、Go 等运行环境需要 Web 服务器配合才能正常工作。\n是否先安装 Nginx，再安装 ${app.name}？`,
      '缺少 Web 服务器',
      { confirmButtonText: '先装 Nginx', cancelButtonText: '取消', type: 'warning' }
    )
  } catch (_) {
    return false // 用户取消安装
  }
  await request.post('/apps/install', { key: 'nginx' })
  ElMessage.success('已开始安装 Nginx，完成后将自动继续安装 ' + app.name)
  startPolling()
  // 等待 Nginx 安装完成后再安装目标应用
  const item = await waitAppDone('nginx')
  if (!item || item.status !== 'installed') {
    ElMessage.error('Nginx 安装未成功，请查看日志后重试')
    return false
  }
  return true
}

// 轮询等待指定应用安装完成（最久 40 分钟）
function waitAppDone(key, timeoutMs = 40 * 60 * 1000) {
  return new Promise((resolve) => {
    const start = Date.now()
    const timer = setInterval(async () => {
      try {
        const res = await request.get('/apps/list')
        const item = (res.data || []).find((a) => a.key === key)
        if (item && ['installed', 'failed', 'not_installed'].includes(item.status)) {
          clearInterval(timer)
          resolve(item)
          return
        }
      } catch (e) { /* ignore */ }
      if (Date.now() - start > timeoutMs) {
        clearInterval(timer)
        resolve(null)
      }
    }, 3000)
  })
}

// phpMyAdmin 安装前选择 PHP 版本（默认最高版本）
async function openPhpVersionDialog(app) {
  phpDialogApp.value = app
  phpDialogVisible.value = true
  phpVersions.value = []
  selectedPhpVersion.value = '7.4'
  try {
    const res = await request.get('/apps/php-versions')
    phpVersions.value = res.data || []
    selectedPhpVersion.value = phpVersions.value[0] || '7.4'
  } catch (e) { /* 忽略 */ }
}

async function confirmPhpInstall() {
  phpInstallLoading.value = true
  try {
    await request.post('/apps/install', { key: 'phpmyadmin', php_version: selectedPhpVersion.value })
    phpDialogVisible.value = false
    ElMessage.success('已开始安装，请稍候...')
    startPolling()
  } catch (e) { /* msg shown by interceptor */ } finally {
    phpInstallLoading.value = false
  }
}

function openMigrate() {
  migrateVisible.value = true
}

function onCustomAction(app, act) {
  if (act.action === 'open-migrate') {
    openMigrate()
    return
  }
  if (act.action === 'open-url' && act.url) {
    window.open(act.url, '_blank')
    return
  }
  // 预留：后续支持 exec-service、open-path 等动作
  ElMessage.info(`动作 ${act.action} 暂未实现`)
}

// 语言运行时应用（Node/Python/Go/PHP 等）：系统自带的才禁用卸载（会误伤系统依赖）；
// 基础服务（nginx/mysql/redis 等）系统自带允许卸载，不在此列。
function isLangRuntimeApp(app) {
  return app.category === 'runtime' && (app.sub_category || '').startsWith('lang:')
}

function uninstall(app) {
  ElMessageBox.confirm(`确定卸载 ${app.name} 吗？该操作会删除对应软件包。`, '卸载应用', {
    confirmButtonText: '卸载',
    cancelButtonText: '取消',
    type: 'warning'
  }).then(async () => {
    try {
      await request.post('/apps/uninstall', { key: app.key })
      // 记录发起卸载，供轮询检测 uninstalling -> failed 的失败切换
      prevAppStatus[app.key] = 'uninstalling'
      // 用户主动卸载：先清除"被移除"标记，确保 uninstalling 状态能写回任务列表；右下角浮窗立即显示
      const tracker = useInstallTrackerStore()
      tracker.unmarkRemoved(app.key)
      tracker.upsert({ key: app.key, name: app.name, action: 'uninstall', status: 'uninstalling', message: '正在卸载...' })
      ElMessage.success('已开始卸载...')
      startPolling()
    } catch (e) {
      playFailSound()
    }
  }).catch(() => {})
}

// 正在执行某项操作：`actingMap[${key}:${action||'*'}] = true`
const actingMap = reactive({})
function isActing(key, action) {
  if (action) return !!actingMap[`${key}:${action}`]
  // 任意一个动作在跑就认为是 acting（用于 disable 其它按钮）
  for (const k in actingMap) if (actingMap[k] && k.startsWith(`${key}:`)) return true
  return !!actingMap[`${key}:*`]
}
function setActing(key, action, v) {
  if (action) {
    const k = `${key}:${action}`
    if (v) actingMap[k] = true
    else delete actingMap[k]
  }
}

async function serviceAction(app, action) {
  // 停止 / 重启 是有副作用的操作（会导致正在运行的网站/接口短暂中断），弹出确认框
  if (action === 'stop' || action === 'restart') {
    try {
      const tip = action === 'stop'
        ? `确定停止 ${app.name} 服务吗？\n停止后依赖此服务的网站将无法访问。`
        : `确定重启 ${app.name} 服务吗？\n服务将在短暂中断后自动恢复。`
      await ElMessageBox.confirm(tip, `${actionLabel(action)}服务`, {
        confirmButtonText: actionLabel(action),
        cancelButtonText: '取消',
        type: 'warning'
      })
    } catch (_) {
      return // 用户取消
    }
  }
  setActing(app.key, action, true)
  // 操作可能耗时（systemctl 重启最坏 30s），给用户直观的 loading 反馈
  const loading = ElLoading.service({ lock: true, text: `正在${actionLabel(action)} ${app.name || app.key}…` })
  try {
    await request.post('/apps/service/action', { key: app.key, action })
    ElMessage.success(`操作成功：${actionLabel(action)}`)
    await loadApps()
    await loadRunningKeys(true) // 操作后强制刷新运行状态
  } catch (e) { /* msg shown by interceptor */ } finally {
    loading.close()
    setActing(app.key, action, false)
  }
}

function actionLabel(a) {
  return { start: '启动', stop: '停止', restart: '重启' }[a] || a
}

function openLog(app) {
  logKey.value = app.key
  logText.value = '正在加载...'
  hasLog.value = false
  stickToBottom.value = true
  logVisible.value = true
  fetchLog()
}

async function fetchLog() {
  if (!logKey.value) return
  const res = await request.get('/apps/log', { params: { key: logKey.value } })
  logText.value = res.data.log || '（暂无日志）'
  hasLog.value = !!(logText.value && logText.value !== '（暂无日志）')
  // 用户停留在底部时自动跟新日志滚到底；用户主动上滑查看历史则不拽回
  await nextTick()
  if (stickToBottom.value) scrollLogToBottom()
}

function scrollLogToBottom() {
  const el = logBoxRef.value
  if (!el) return
  el.scrollTop = el.scrollHeight
}

function onLogScroll(e) {
  const el = e.target
  // 容忍 20px 误差，避免一直被拉回到底
  stickToBottom.value = el.scrollHeight - el.clientHeight - el.scrollTop < 20
}

// 任务驱动轮询：仅在有安装/卸载任务时轮询，任务全部结束后自动停止（空闲零请求）
function startPolling() {
  stopPolling()
  listTimer = setInterval(async () => {
    await loadApps()
    // 无进行中任务（列表无 installing/uninstalling 且全局队列无活跃任务）→ 停止轮询
    const hasRunningApp = apps.value.some((a) => a.status === 'installing' || a.status === 'uninstalling')
    const tracker = useInstallTrackerStore()
    if (!hasRunningApp && tracker.activeCount === 0) stopPolling()
  }, 3000)
}

function stopPolling() {
  if (listTimer) { clearInterval(listTimer); listTimer = null }
}

watch(logVisible, (v) => {
  if (v) {
    stickToBottom.value = true
    fetchLog()
    logTimer = setInterval(fetchLog, 2000)
  } else if (logTimer) {
    clearInterval(logTimer)
    logTimer = null
  }
})

// 切换顶级分类时清空二级分类选择；同时把当前 Tab 选择持久化到 localStorage
watch(activeCat, () => {
  activeSubcat.value = ''
  try { localStorage.setItem(ACTIVE_CAT_KEY, activeCat.value) } catch (e) { /* 忽略 */ }
})
watch(activeSubcat, () => {
  try { localStorage.setItem(ACTIVE_SUBCAT_KEY, activeSubcat.value) } catch (e) { /* 忽略 */ }
})

// 切回本标签页时刷新一次列表：感知其他标签页/入口发起的安装（非轮询，仅一次请求）
function onVisibilityChange() {
  if (document.visibilityState === 'visible') loadApps()
}

onMounted(async () => {
  // 进入/刷新应用商店页：强制刷新一次官网远程列表缓存（官网后台变更后立即生效）。
  // 刷新失败不阻塞页面加载，沿用上次已缓存的数据。
  try { await request.post('/apps/refresh') } catch (e) { /* ignore */ }
  loadCategories()
  await loadApps()
  // 任务驱动：页面加载时已有安装/卸载在进行，或全局队列有活跃任务 → 开始轮询；否则不轮询
  const hasRunningApp = apps.value.some((a) => a.status === 'installing' || a.status === 'uninstalling')
  const tracker = useInstallTrackerStore()
  if (hasRunningApp || tracker.activeCount > 0) startPolling()
  document.addEventListener('visibilitychange', onVisibilityChange)
  // 接收 InstallFloater 的"打开日志"事件（用户在浮窗点"日志"按钮会跳过来）
  const openKey = sessionStorage.getItem('lp-installfloater-openlog')
  if (openKey) {
    sessionStorage.removeItem('lp-installfloater-openlog')
    const tryOpen = () => {
      const a = apps.value.find((x) => x.key === openKey)
      if (a) { openLog(a); return true }
      return false
    }
    if (!tryOpen()) {
      // apps 还没加载完，等下次 loadApps 完成
      const stop = watch(apps, () => { if (tryOpen()) stop() }, { deep: true })
    }
  }
})

onUnmounted(() => {
  stopPolling()
  document.removeEventListener('visibilitychange', onVisibilityChange)
  if (logTimer) clearInterval(logTimer)
})
</script>

<style scoped>
.app-store-toolbar {
  display: flex;
  align-items: center;
  gap: 16px;
  background: #fff;
  border-radius: 6px;
  padding: 0 16px;
  margin-bottom: 12px;
  min-height: 48px;
}
.cat-tabs {
  flex: 1;
  min-width: 0;
  background: transparent;
  padding: 0;
  margin: 0;
}
/* el-tabs 自身会渲染 .el-tabs__header，需要去掉它的下边框，让 toolbar 视觉统一 */
.cat-tabs :deep(.el-tabs__header) {
  margin-bottom: 0;
  border-bottom: none;
}
.cat-label { font-size: 14px; display: inline-flex; align-items: center; gap: 6px; }
.cat-badge {
  background: #67c23a;
  color: #fff;
  border-radius: 10px;
  padding: 0 6px;
  min-width: 18px;
  height: 18px;
  font-size: 11px;
  line-height: 18px;
  text-align: center;
}
.cat-desc { color: #909399; font-size: 12px; margin: -4px 0 12px; padding-left: 4px; }

.app-search-input {
  width: 280px;
  flex-shrink: 0;
}

.sub-tabs {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
  padding: 0 0 10px;
}
.sub-pill {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 5px 14px;
  border-radius: 18px;
  background: #f3f3f5;
  color: #606266;
  font-size: 13px;
  cursor: pointer;
  user-select: none;
  transition: all .15s;
}
.sub-pill:hover { color: #409eff; background: #e6f1ff; }
.sub-pill.active {
  background: #409eff;
  color: #fff;
}

.app-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));   /* 桌面默认 4 列 */
  gap: 14px;                                           /* 上下左右统一空隙 */
}
/* 大屏（≥1600px）一行 5 个；平板 3 列；手机 2 / 1 列 */
@media (min-width: 1600px) {
  .app-grid { grid-template-columns: repeat(5, minmax(0, 1fr)); }
}
@media (max-width: 1199px) {
  .app-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); }
}
@media (max-width: 899px) {
  .app-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (max-width: 599px) {
  .app-grid { grid-template-columns: 1fr; }
}

.app-card {
  width: 100%;
  height: 100%;
  display: flex; flex-direction: column;
}
.app-card :deep(.el-card__body) { padding: 14px; }  /* 紧凑卡片内边距 */
.app-col { display: flex; }
.app-card :deep(.el-card__body) { flex: 1; display: flex; flex-direction: column; }
.app-head { display: flex; align-items: center; gap: 10px; margin-bottom: 8px; }
.app-icon {
  width: 40px; height: 40px; border-radius: 10px;  /* 图标缩小 */
  display: flex; align-items: center; justify-content: center; flex-shrink: 0;
}
.app-title { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.app-name { font-size: 14px; font-weight: 600; color: #303133; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
/* 描述固定 1 行（≈21px）：紧凑卡片不再用 2 行 */
.app-desc {
  color: #606266; font-size: 12px; line-height: 1.5;
  height: 18px; margin-bottom: 8px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.app-meta { display: flex; align-items: center; gap: 6px; margin-bottom: 8px; }
.cat-tag {
  background: #ecf5ff; color: #409eff; border-radius: 4px;
  padding: 1px 6px; font-size: 11px;
}
/* app-error 区域：固定单行高度（约 28px），保证有无红字时卡片整体高度一致 */
.app-error {
  margin: 0 0 6px;
  min-height: 28px;
  padding: 4px 8px;
}
.app-error :deep(.el-alert__content) { padding-left: 4px; }
.app-error :deep(.el-alert__title) { font-size: 12px; }
.app-error .app-error-text {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.php-dialog-tip { font-size: 13px; color: #909399; line-height: 1.6; }
/* 操作按钮：固定 1 行高度（≈28px），按钮多时省略号截断。已用 flex-wrap 允许换行 */
.app-actions {
  display: flex; flex-wrap: wrap; align-items: center; gap: 6px;
  margin-bottom: 6px;
  min-height: 28px;
}
.app-actions .el-button { padding: 4px 10px; font-size: 12px; }  /* 紧凑按钮 */
.status-tag { margin-right: 0; }
/* 备注：固定 1 行（≈18px），紧凑卡片只用 1 行 */
.app-remarks {
  display: flex; align-items: flex-start; gap: 4px;
  color: #909399; font-size: 11px; line-height: 1.5;
  height: 18px;
  overflow: hidden;
}
.app-remarks .remarks-text {
  flex: 1; min-width: 0;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.log-wrap { position: relative; }
.log-box {
  background: #1e1e1e; color: #d4d4d4; padding: 12px;
  border-radius: 6px; font-size: 12px; line-height: 1.7;
  height: calc(100vh - 120px); overflow: auto; white-space: pre-wrap;
  word-break: break-all; margin: 0;
}
.log-follow-tip {
  position: absolute; right: 20px; bottom: 20px; z-index: 5;
  box-shadow: 0 2px 8px rgba(0,0,0,0.18);
}
</style>
