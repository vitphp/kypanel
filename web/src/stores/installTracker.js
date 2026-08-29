import { defineStore } from 'pinia'

// 全局安装任务追踪：右下角浮窗 + 安装中心大弹窗（左列表右日志）+ 音效提示
// 任何页面触发 install/uninstall 时，把任务写到本 store，InstallFloater 自动展示

// 用户主动"移除/清空"的 key 持久化到 localStorage，刷新页面 / 重新进入页面后，
// 不会再被后端轮询的 status 变化（installing/failed 等）复活。
const REMOVED_KEY = 'lp_removed_task_keys'
function loadRemovedSet() {
  try {
    const raw = localStorage.getItem(REMOVED_KEY)
    if (!raw) return new Set()
    const arr = JSON.parse(raw)
    return new Set(Array.isArray(arr) ? arr : [])
  } catch (e) {
    return new Set()
  }
}
function saveRemovedSet(set) {
  try {
    localStorage.setItem(REMOVED_KEY, JSON.stringify(Array.from(set)))
  } catch (e) { /* 忽略 */ }
}

export const useInstallTrackerStore = defineStore('installTracker', {
  state: () => ({
    // 任务列表：{ key, name, action, status, version, message, paused, startedAt, finishedAt }
    // status: 'queued' | 'installing' | 'uninstalling' | 'installed' | 'failed' | 'uninstalled'
    tasks: [],
    // 安装中心大弹窗开关
    batchVisible: false,
    // 右侧日志当前显示的 key
    activeLogKey: '',
    // 用户主动移除过的 key（用普通 Set 即可，不需要响应式；每次操作后镜像回 _removedList）
    _removedSet: typeof window !== 'undefined' ? loadRemovedSet() : new Set(),
    _removedList: [],
  }),
  getters: {
    // 正在进行的任务数（用于浮窗角标）
    activeCount: (s) => s.tasks.filter((t) => !t.finishedAt && t.status !== 'paused' && (t.status === 'installing' || t.status === 'queued' || t.status === 'uninstalling')).length,
    // 排队中的数量
    queuedCount: (s) => s.tasks.filter((t) => t.status === 'queued').length,
    // 安装中的数量
    installingCount: (s) => s.tasks.filter((t) => t.status === 'installing' || t.status === 'uninstalling').length,
    // 失败数量
    failedCount: (s) => s.tasks.filter((t) => t.status === 'failed').length,
    // 是否全部结束
    allFinished: (s) => s.tasks.length > 0 && s.tasks.every((t) => t.finishedAt || t.status === 'paused'),
    // 是否有失败
    hasFailure: (s) => s.tasks.some((t) => t.status === 'failed'),
  },
  actions: {
    // 用户是否主动移除过这个 key
    isRemoved(key) {
      return this._removedSet.has(key)
    },
    // 记录用户主动移除
    markRemoved(key) {
      if (!key) return
      this._removedSet.add(key)
      this._removedList = Array.from(this._removedSet)
      saveRemovedSet(this._removedSet)
    },
    // 取消移除标记（用户重新触发安装 / 卸载时调用，让任务能再次进入队列）
    unmarkRemoved(key) {
      if (!key) return
      this._removedSet.delete(key)
      this._removedList = Array.from(this._removedSet)
      saveRemovedSet(this._removedSet)
    },
    // 创建/更新一个任务
    upsert(task) {
      if (!task?.key) return
      // 终态：用户移除过该 key，拒绝复活（避免后端 status 长时间停留在 failed 时被轮询再次写回）
      // 进行中：即使移除过，也允许写回（后端真的在跑，必须显示在浮窗上）
      const activeStatuses = ['installing', 'uninstalling', 'queued', 'paused']
      if (this.isRemoved(task.key) && !activeStatuses.includes(task.status)) return
      // 标记为"活跃"时，顺手清掉被移除的记录，保证后续状态也能继续写回
      if (activeStatuses.includes(task.status)) this.unmarkRemoved(task.key)
      const i = this.tasks.findIndex((t) => t.key === task.key && t.action === (task.action || 'install'))
      const now = Date.now()
      const isFinished = ['installed', 'failed', 'uninstalled'].includes(task.status)
      const old = i < 0 ? null : this.tasks[i]
      const newTask = {
        ...old,
        ...task,
        startedAt: task.startedAt || old?.startedAt || now,
        finishedAt: isFinished ? now : old?.finishedAt ?? null,
      }
      if (i < 0) this.tasks.push(newTask)
      else this.tasks[i] = newTask
    },
    // 标记为暂停（保留在列表）
    markPaused(key) {
      if (this.isRemoved(key)) return
      const t = this.tasks.find((x) => x.key === key)
      if (t) {
        t.paused = true
        t.status = 'paused'
        t.finishedAt = null
      }
    },
    // 继续安装（重新入队）
    markResumed(key) {
      const t = this.tasks.find((x) => x.key === key)
      if (t) {
        t.paused = false
        t.status = 'queued'
        t.finishedAt = null
      }
    },
    // 打开安装中心大弹窗
    open(key) {
      this.batchVisible = true
      if (key) this.setActiveLogKey(key)
    },
    close() {
      this.batchVisible = false
    },
    setActiveLogKey(k) {
      this.activeLogKey = k
    },
    // 用户点"清空已完成"：只清成功的，失败的保留（用户要求失败不移除列表，需手动"移除"）
    clearFinished() {
      // 不动 _removedSet（已移除的不在 tasks 里，但被 markRemoved 记录）
      this.tasks = this.tasks.filter((t) => t.status === 'failed')
    },
    // 全部清空（用户主动确认后会同步清掉 removed 集合，避免再次刷新又被旧记录阻挡新的安装）
    clearAll() {
      this.tasks = []
      this.activeLogKey = ''
      this._removedSet.clear()
      this._removedList = []
      saveRemovedSet(this._removedSet)
    },
    // 移除单个任务：写入 removed 集合，刷新/重新进入后该 key 不会自动复活
    remove(key) {
      this.tasks = this.tasks.filter((t) => t.key !== key)
      if (this.activeLogKey === key) this.activeLogKey = ''
      this.markRemoved(key)
    },
  },
})
