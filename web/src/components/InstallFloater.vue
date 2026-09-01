<template>
  <Teleport to="body">
    <!-- 收起态：右下角小胶囊 -->
    <transition name="lp-fade">
      <div
        v-if="!tracker.batchVisible && tracker.tasks.length"
        class="install-mini"
        :class="{ 'is-active': hasActive, 'is-failed': hasFailure }"
        @click="openBatch"
      >
        <el-icon class="mini-icon" :class="{ spin: hasActive }" :size="18">
          <component :is="hasFailure && !hasActive ? CircleClose : (hasActive ? Loading : Check)" />
        </el-icon>
        <span class="mini-text">
          <template v-if="hasActive">安装中 {{ tracker.activeCount }}</template>
          <template v-else-if="hasFailure">{{ tracker.failedCount }} 个失败</template>
          <template v-else>已完成</template>
        </span>
      </div>
    </transition>

    <!-- 安装中心大弹窗：左列表 + 右命令行日志 -->
    <el-dialog
      :model-value="tracker.batchVisible"
      :title="batchTitle"
      width="min(980px, 96vw)"
      top="5vh"
      class="install-center-dialog"
      :close-on-click-modal="false"
      @update:model-value="onClose"
    >
      <div class="batch-layout">
        <!-- 左：安装列表 -->
        <div class="batch-left">
          <div class="batch-list">
            <div
              v-for="t in sortedTasks"
              :key="t.key + '-' + t.action"
              class="batch-row"
              :class="[
                'status-' + t.status,
                { 'is-active': t.key === tracker.activeLogKey, 'is-paused': t.paused },
              ]"
              @click="selectTask(t)"
            >
              <div class="row-main">
                <el-icon class="row-icon" :class="{ spin: isRunning(t) }" :size="16">
                  <component :is="statusIcon(t)" />
                </el-icon>
                <div class="row-text">
                  <div class="row-name">
                    <span>{{ t.name }}</span>
                    <span v-if="t.version" class="row-version">v{{ t.version }}</span>
                  </div>
                  <div class="row-status">{{ statusText(t) }}</div>
                </div>
              </div>
              <div class="row-actions">
                <el-button
                  v-if="isRunning(t)"
                  size="small"
                  type="warning"
                  plain
                  @click.stop="cancelRunning(t)"
                >取消</el-button>
                <el-button
                  v-else-if="t.paused"
                  size="small"
                  type="primary"
                  plain
                  @click.stop="resumeTask(t)"
                >继续</el-button>
                <el-button
                  v-if="canRemove(t)"
                  size="small"
                  text
                  @click.stop="removeTask(t)"
                >移除</el-button>
              </div>
            </div>
            <div v-if="!tracker.tasks.length" class="batch-empty">暂无安装任务</div>
          </div>
          <div class="batch-list-footer">
            <span class="stats">
              <template v-if="tracker.queuedCount">排队 {{ tracker.queuedCount }}</template>
              <template v-if="tracker.installingCount">{{ tracker.queuedCount ? ' · ' : '' }}安装中 {{ tracker.installingCount }}</template>
              <template v-if="tracker.failedCount">{{ (tracker.queuedCount || tracker.installingCount) ? ' · ' : '' }}失败 {{ tracker.failedCount }}</template>
            </span>
            <el-button v-if="hasFinished" size="small" text @click="tracker.clearFinished()">清空已完成</el-button>
          </div>
        </div>

        <!-- 右：命令行日志（终端） -->
        <div class="batch-right">
          <div class="term-head">
            <span class="term-dots"><i></i><i></i><i></i></span>
            <span class="term-title">{{ logTitle }}</span>
            <span class="term-live" :class="{ on: watchingLog }">● {{ watchingLog ? '实时' : '已停止' }}</span>
          </div>
          <pre ref="logBoxRef" class="terminal" :class="{ 'is-empty': !logText }">{{ logText || emptyLogText }}</pre>
        </div>
      </div>

      <template #footer>
        <div class="batch-footer">
          <span v-if="!hasActive && hasFailure" class="footer-tip tip-fail">有安装失败，其余任务已跳过，请查看日志重试</span>
          <span v-else-if="!hasActive && tracker.tasks.length" class="footer-tip tip-ok">全部任务已完成</span>
          <span v-else class="footer-tip tip-info">队列串行执行，装完一个自动开始下一个</span>
          <div>
            <el-button @click="tracker.close()">收起</el-button>
          </div>
        </div>
      </template>
    </el-dialog>
  </Teleport>
</template>

<script setup>
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { storeToRefs } from 'pinia'
import { ElMessage } from 'element-plus'
import {
  Loading, Check, Warning, CircleClose, Close, VideoPause, VideoPlay, Delete,
} from '@element-plus/icons-vue'
import { useInstallTrackerStore } from '../stores/installTracker'
import request from '../utils/request'
import { playSuccessSound, playFailSound } from '../utils/sound'

const tracker = useInstallTrackerStore()
const { tasks } = storeToRefs(tracker)

const logText = ref('')
const logBoxRef = ref(null)
const stickToBottom = ref(true)
const watchingLog = ref(false)

// ============ 派生 ============
const hasActive = computed(() => tracker.activeCount > 0)
const hasFailure = computed(() => tracker.failedCount > 0)
const hasFinished = computed(() => tasks.value.some((t) => t.finishedAt))

const batchTitle = computed(() => {
  if (hasActive.value) return `软件安装中心（队列执行中）`
  if (hasFailure.value) return `软件安装中心（部分失败）`
  if (tasks.value.length) return `软件安装中心`
  return `软件安装中心`
})

const logTitle = computed(() => {
  const t = tasks.value.find((x) => x.key === tracker.activeLogKey)
  if (!t) return '选择左侧任务查看日志'
  const act = t.action === 'uninstall' ? '卸载' : '安装'
  return `${t.name} ${act}日志`
})

// 空态提示：跟随当前选中任务的 action，区分排队中 / 执行中
const emptyLogText = computed(() => {
  const t = tasks.value.find((x) => x.key === tracker.activeLogKey)
  if (!t) return '（请在左侧选择一个任务查看日志）'
  const act = t.action === 'uninstall' ? '卸载' : '安装'
  if (t.status === 'queued') return `（排队等待中，尚未开始${act}，开始后这里会实时输出日志...）`
  return `（暂无日志，请等待${act}脚本输出...）`
})

const sortedTasks = computed(() => {
  const order = { failed: 0, installing: 1, uninstalling: 1, queued: 2, paused: 3, installed: 4, uninstalled: 4 }
  const list = [...tasks.value]
  list.sort((a, b) => {
    const ao = order[a.status] ?? 5
    const bo = order[b.status] ?? 5
    if (ao !== bo) return ao - bo
    // 同一状态组内按入队时间正序（先点安装的排上面，与串行执行顺序一致）
    return (a.startedAt || 0) - (b.startedAt || 0)
  })
  return list
})

function isRunning(t) {
  return t.status === 'installing' || t.status === 'uninstalling'
}
function statusIcon(t) {
  if (t.status === 'queued') return Loading
  if (isRunning(t)) return Loading
  if (t.status === 'failed') return CircleClose
  if (t.status === 'paused') return VideoPause
  return Check
}
function statusText(t) {
  const act = t.action === 'uninstall' ? '卸载' : '安装'
  if (t.status === 'queued') return '排队等待中...'
  if (t.status === 'installing') return '正在安装，实时输出日志...'
  if (t.status === 'uninstalling') return '正在卸载，实时输出日志...'
  if (t.status === 'failed') return t.message || `${act}失败，点击右侧查看日志`
  if (t.status === 'paused') return '已暂停，点击「继续」重新入队'
  if (t.status === 'uninstalled') return '已卸载'
  return '安装完成，已自动移除'
}
function canRemove(t) {
  return t.status === 'queued' || t.status === 'paused' || t.status === 'failed'
}

// ============ 弹窗开关 ============
function openBatch() {
  tracker.open(tracker.tasks.find((t) => isRunning(t) || t.status === 'queued')?.key || tracker.tasks[0]?.key || '')
}
function onClose(v) {
  if (!v) tracker.close()
}
function selectTask(t) {
  tracker.setActiveLogKey(t.key)
  fetchLog()
}

// ============ 任务同步（队列 + 终态） ============
let syncTimer = null
let logTimer = null
// 任务驱动轮询：只有存在进行中任务时才继续调度，无任务时完全停止（零请求）
const SYNC_DELAY = 2500
// 防重入：syncTasks 执行期间再次被触发时标记，跑完后补跑一轮
let syncInFlight = false
let syncAgain = false
// 用于声音判断：记录上一轮是否存在进行中任务
let prevHadActive = false
let sawTransition = false
// 全部成功完成后的「5 秒自动隐藏」定时器
let autoHideTimer = null
// ghost 判定宽限：记录任务「疑似失联」的连续轮询次数，避免 ListApps 缓存竞态导致的一次性误判为「已暂停」
const stallSeen = new Map()

function stopSyncTimer() {
  if (syncTimer) {
    clearTimeout(syncTimer)
    syncTimer = null
  }
}

// 全部成功完成后：5 秒后自动隐藏右下角小胶囊（清除成功/已完成的任务）。
// 期间若有新任务进来，取消隐藏计时，保持小胶囊可见。
function scheduleAutoHide() {
  if (autoHideTimer) clearTimeout(autoHideTimer)
  autoHideTimer = setTimeout(() => {
    autoHideTimer = null
    // 只清除成功终态任务；若期间又有新任务/失败任务，则保留
    if (tracker.activeCount > 0 || tracker.failedCount > 0) return
    const done = tracker.tasks.filter((t) => t.status === 'installed' || t.status === 'uninstalled')
    for (const d of done) tracker.remove(d.key)
  }, 5000)
}

function cancelAutoHide() {
  if (autoHideTimer) {
    clearTimeout(autoHideTimer)
    autoHideTimer = null
  }
}

function scheduleSync() {
  if (syncTimer) return // 已有定时器在跑，避免重复
  syncTimer = setTimeout(() => {
    syncTimer = null
    syncTasks()
  }, SYNC_DELAY)
}

async function syncTasks() {
  if (syncInFlight) {
    syncAgain = true
    return
  }
  syncInFlight = true
  syncAgain = false
  let hasActiveNow = false
  try {
    const [taskRes, listRes] = await Promise.all([
      request.get('/apps/tasks').catch(() => null),
      request.get('/apps/list').catch(() => null),
    ])
    const backendTasks = taskRes?.data || []
    const list = listRes?.data || []
    // 是否有进行中任务（后端任务 or 本地队列）→ 决定轮询频率
    hasActiveNow = backendTasks.length > 0 || tasks.value.some((t) => isRunning(t) || t.status === 'queued')

    // 1) 后端正在执行/排队中的任务 -> upsert
    for (const t of backendTasks) {
      const existing = tasks.value.find((x) => x.key === t.key)
      const action = t.status === 'uninstalling' ? 'uninstall' : 'install'
      if (existing?.paused) continue // 已被用户暂停的不覆盖
      // queued 优先：排队中（无论安装还是卸载）都先显示「排队等待中」，
      // 避免卸载任务在排队阶段被误显示成「正在卸载」却无日志输出
      const isQueued = !!t.queued
      const isUninstall = t.status === 'uninstalling'
      const status = isQueued ? 'queued' : (isUninstall ? 'uninstalling' : 'installing')
      const message = isQueued
        ? '排队等待中...'
        : (isUninstall ? '正在卸载，实时输出日志...' : '正在安装，实时输出日志...')
      tracker.upsert({
        key: t.key,
        name: t.name || t.key,
        action,
        version: t.version || '',
        status,
        message,
        startedAt: t.started_at || 0, // 用后端真实入队时间作为排序基准，避免本地 Date.now() 与刷新场景顺序不一致
      })
    }

    // 2) 本 store 里的任务：根据 /apps/list 终态收尾
    const statusOf = (k) => list.find((a) => a.key === k)?.status || ''
    let anyFinishedNow = false
    const endedKeys = new Set() // 本轮刚收尾（完成/失败/暂停）的任务，用于自动切换右侧日志
    for (const t of [...tasks.value]) {
      if (t.paused) continue
      if (t.finishedAt) continue
      const cur = statusOf(t.key)
      const isBackendActive = backendTasks.some((b) => b.key === t.key)
      if (isBackendActive) {
        stallSeen.delete(t.key)
        continue
      }
      if (t.status === 'installing' || t.status === 'queued') {
        if (cur === 'installed') {
          tracker.upsert({ key: t.key, name: t.name, action: 'install', version: t.version, status: 'installed', message: '安装完成' })
          anyFinishedNow = true
          endedKeys.add(t.key)
          stallSeen.delete(t.key)
        } else if (cur === 'failed') {
          tracker.upsert({ key: t.key, name: t.name, action: 'install', version: t.version, status: 'failed', message: list.find((a) => a.key === t.key)?.error || '安装失败' })
          anyFinishedNow = true
          endedKeys.add(t.key)
          stallSeen.delete(t.key)
        } else if ((cur === 'not_installed' || cur === 'installing') && !t.paused && t.status !== 'queued') {
          // 被取消/手动中断，或 DB 状态残留为 installing 但后端已无此任务（ghost）→ 暂停。
          // 需连续两轮都失联才判定，避免 ListApps 缓存竞态（安装刚完成但列表仍短暂返回 installing）误判。
          const n = (stallSeen.get(t.key) || 0) + 1
          if (n >= 2) {
            tracker.markPaused(t.key)
            endedKeys.add(t.key)
            stallSeen.delete(t.key)
          } else {
            stallSeen.set(t.key, n)
          }
        }
      } else if (t.status === 'uninstalling') {
        if (cur === 'not_installed' || cur === '') {
          tracker.upsert({ key: t.key, name: t.name, action: 'uninstall', status: 'uninstalled', message: '卸载完成' })
          anyFinishedNow = true
          endedKeys.add(t.key)
          stallSeen.delete(t.key)
        } else if (cur === 'failed') {
          tracker.upsert({ key: t.key, name: t.name, action: 'uninstall', status: 'failed', message: '卸载失败' })
          anyFinishedNow = true
          endedKeys.add(t.key)
          stallSeen.delete(t.key)
        } else if (cur === 'uninstalling' && !t.paused) {
          // DB 状态残留为 uninstalling 但后端已无此任务（ghost）→ 暂停（同样需连续两轮确认）
          const n = (stallSeen.get(t.key) || 0) + 1
          if (n >= 2) {
            tracker.markPaused(t.key)
            endedKeys.add(t.key)
            stallSeen.delete(t.key)
          } else {
            stallSeen.set(t.key, n)
          }
        }
      }
    }

    // 自动切换：当前右侧选中的任务本轮刚结束，优先切到「执行中」任务，其次「排队中」任务继续看日志
    if (endedKeys.size && endedKeys.has(tracker.activeLogKey)) {
      const next = tasks.value.find((x) => isRunning(x)) || tasks.value.find((x) => x.status === 'queued')
      tracker.setActiveLogKey(next ? next.key : '')
    }

    // 3) 声音判断：之前有进行中 -> 现在没有 -> 全部结束（先播完音效再决定是否延迟隐藏）
    if (prevHadActive && !hasActiveNow && !sawTransition) {
      const hasFailed = tasks.value.some((t) => t.status === 'failed')
      if (hasFailed) {
        // 有失败：播放失败音效，小胶囊保留显示「N 个失败」，不自动隐藏
        playFailSound()
      } else if (anyFinishedNow || tasks.value.some((t) => t.finishedAt)) {
        // 全部成功：播放成功音效，5 秒后自动隐藏小胶囊（清除成功任务）
        playSuccessSound()
        scheduleAutoHide()
      }
      sawTransition = true
    }
    // 新任务开始（之前无 active -> 现在有 active）：重置 transition 标记，让下一批任务完成时能再次播音效
    if (!prevHadActive && hasActiveNow) {
      sawTransition = false
    }
    prevHadActive = hasActiveNow
  } catch (e) {
    /* 网络波动忽略 */
  } finally {
    syncInFlight = false
    // 5) 任务驱动：有任务继续高频轮询；无任务完全停止（零请求），等待外部事件唤醒
    if (syncAgain) {
      syncAgain = false
      syncTasks()
    } else if (hasActiveNow) {
      scheduleSync()
    }
  }
}

// ============ 日志同步 ============
async function fetchLog() {
  if (!tracker.activeLogKey) {
    logText.value = ''
    watchingLog.value = false
    return
  }
  try {
    const res = await request.get('/apps/log', { params: { key: tracker.activeLogKey } })
    logText.value = res.data.log || ''
    watchingLog.value = true
    await nextTick()
    if (stickToBottom.value && logBoxRef.value) {
      logBoxRef.value.scrollTop = logBoxRef.value.scrollHeight
    }
  } catch (e) {
    watchingLog.value = false
  }
}

// ============ 任务操作 ============
async function pauseTask(t) {
  // 旧 API 保留调用点：执行中无意义，统一走 cancelRunning
  return cancelRunning(t)
}

// 取消正在执行的安装/卸载（后端会真停掉子进程，无法"暂停"）
async function cancelRunning(t) {
  let cancelled = false
  try {
    await request.post('/apps/tasks/cancel', { key: t.key })
    cancelled = true
  } catch (e) {
    // 后端找不到任务（可能 install 协程刚退出但 list 状态未刷，或 apt-get 子进程残留状态导致脱钩），
    // 前端宽容处理：把 store 任务标记为已取消，不阻塞用户操作
    cancelled = true
  }
  if (cancelled) {
    tracker.upsert({ key: t.key, name: t.name, action: t.action || 'install', status: 'failed', message: '用户已取消' })
    ElMessage.info(`${t.name} 已取消`)
  }
}
async function resumeTask(t) {
  try {
    await request.post('/apps/install', { key: t.key, version: t.version || undefined })
    tracker.markResumed(t.key)
    ElMessage.success(`${t.name} 已重新入队`)
    // 强制立即同步一次，让队列状态更新
    syncTasks()
  } catch (e) {
    ElMessage.error('重新入队失败：' + (e?.response?.data?.message || e?.message || '未知错误'))
  }
}
async function removeTask(t) {
  // 还在执行中的先取消
  if (isRunning(t) || t.status === 'queued') {
    try { await request.post('/apps/tasks/cancel', { key: t.key }) } catch (e) {}
  }
  stallSeen.delete(t.key) // 清理失联计数，避免该 key 下次重新入队后被残留计数误判为暂停
  tracker.remove(t.key)
  ElMessage.success(`已从队列移除 ${t.name}`)
}

// ============ 生命周期 ============
// 切回本标签页时同步一次：感知其他标签页/其他入口发起的安装任务
function onVisibilityChange() {
  if (document.visibilityState === 'visible') syncTasks()
}

// 本地队列出现新任务（任何页面点了安装/卸载）→ 立即启动同步与轮询，并取消自动隐藏
watch(
  () => tracker.activeCount,
  (n) => {
    if (n > 0) {
      cancelAutoHide()
      syncTasks()
    }
  }
)

onMounted(() => {
  // 进入页面同步一次：发现后端/队列有任务则自动开始轮询，无任务则保持零请求
  syncTasks()
  document.addEventListener('visibilitychange', onVisibilityChange)
})
onUnmounted(() => {
  stopSyncTimer()
  cancelAutoHide()
  document.removeEventListener('visibilitychange', onVisibilityChange)
  if (logTimer) clearInterval(logTimer)
})

// 日志轮询：仅当安装中心弹窗打开且选中了任务时轮询，否则完全停止
watch(
  [() => tracker.batchVisible, () => tracker.activeLogKey],
  () => {
    if (logTimer) {
      clearInterval(logTimer)
      logTimer = null
    }
    if (tracker.batchVisible && tracker.activeLogKey) {
      fetchLog()
      logTimer = setInterval(fetchLog, 2000)
    } else {
      watchingLog.value = false
    }
  },
  { immediate: true }
)
</script>

<style scoped>
/* ===== 收起态小胶囊 ===== */
.install-mini {
  position: fixed;
  right: 24px;
  bottom: 24px;
  z-index: 3000;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 8px 16px;
  background: #fff;
  border-radius: 22px;
  box-shadow: 0 6px 20px rgba(15, 23, 42, 0.15);
  cursor: pointer;
  border: 1px solid #ebeef5;
  transition: all 0.18s;
  user-select: none;
}
.install-mini:hover { transform: translateY(-2px); box-shadow: 0 10px 24px rgba(15, 23, 42, 0.2); }
.install-mini.is-active { border-color: #e6a23c; }
.install-mini.is-failed { border-color: #f56c6c; background: #fef0f0; }
.mini-icon { color: #67c23a; }
.install-mini.is-active .mini-icon { color: #e6a23c; }
.install-mini.is-failed .mini-icon { color: #f56c6c; }
.mini-text { color: #303133; font-weight: 500; }
.spin { animation: lp-spin 1s linear infinite; }
@keyframes lp-spin { to { transform: rotate(360deg); } }
.lp-fade-enter-active, .lp-fade-leave-active { transition: all 0.25s; }
.lp-fade-enter-from, .lp-fade-leave-to { opacity: 0; transform: translateY(20px) scale(0.95); }

/* ===== 大弹窗 ===== */
:deep(.install-center-dialog .el-dialog__body) {
  padding: 12px 16px 8px;
}
:deep(.install-center-dialog .el-dialog__footer) {
  padding-top: 8px;
}
.batch-layout {
  display: flex;
  gap: 12px;
  height: min(560px, 66vh);
}

/* 左列表 */
.batch-left {
  width: 300px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  border: 1px solid #ebeef5;
  border-radius: 8px;
  overflow: hidden;
}
.batch-list {
  flex: 1;
  overflow-y: auto;
  padding: 6px;
}
.batch-empty {
  padding: 40px 0;
  text-align: center;
  color: #c0c4cc;
  font-size: 13px;
}
.batch-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 8px 10px;
  border-radius: 6px;
  cursor: pointer;
  transition: background 0.15s;
  margin-bottom: 2px;
}
.batch-row:hover { background: #f5f7fa; }
.batch-row.is-active { background: #ecf5ff; }
.batch-row.is-paused { opacity: 0.75; }
.row-main { display: flex; align-items: center; gap: 8px; min-width: 0; }
.row-icon { color: #909399; flex-shrink: 0; }
.batch-row.status-installing .row-icon,
.batch-row.status-queued .row-icon { color: #e6a23c; }
.batch-row.status-failed .row-icon { color: #f56c6c; }
.batch-row.status-installed .row-icon,
.batch-row.status-uninstalled .row-icon { color: #67c23a; }
.row-text { min-width: 0; }
.row-name { display: flex; align-items: center; gap: 6px; color: #303133; font-weight: 500; font-size: 13px; }
.row-version { font-size: 11px; color: #909399; background: #f4f4f5; padding: 0 5px; border-radius: 3px; }
.row-status { font-size: 12px; color: #909399; margin-top: 1px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.batch-row.status-failed .row-status { color: #f56c6c; }
.row-actions { display: flex; align-items: center; gap: 2px; flex-shrink: 0; }
.batch-list-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 10px;
  border-top: 1px solid #f0f2f5;
}
.stats { font-size: 12px; color: #909399; }

/* 右终端 */
.batch-right {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  border-radius: 8px;
  overflow: hidden;
  border: 1px solid #1e2a3a;
}
.term-head {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 12px;
  background: #1e2a3a;
  color: #9aa7b8;
  font-size: 12px;
}
.term-dots { display: inline-flex; gap: 5px; }
.term-dots i { width: 10px; height: 10px; border-radius: 50%; background: #3d4c5f; }
.term-title { flex: 1; color: #cbd5e1; }
.term-live { color: #909399; }
.term-live.on { color: #67c23a; }
.terminal {
  flex: 1;
  margin: 0;
  padding: 10px 12px;
  background: #0d1520;
  color: #d7e3f0;
  font-family: 'Cascadia Code', Consolas, 'Courier New', monospace;
  font-size: 12.5px;
  line-height: 1.55;
  overflow-y: auto;
  white-space: pre-wrap;
  word-break: break-all;
}
.terminal.is-empty { color: #51606f; }

/* 底部 */
.batch-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
}
.footer-tip { font-size: 12.5px; }
.tip-info { color: #909399; }
.tip-ok { color: #67c23a; }
.tip-fail { color: #f56c6c; }

@media (max-width: 720px) {
  .batch-layout { flex-direction: column; height: auto; }
  .batch-left { width: 100%; max-height: 200px; }
  .batch-right { height: 320px; }
}
</style>
