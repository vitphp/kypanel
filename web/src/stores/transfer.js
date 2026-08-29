import { defineStore } from 'pinia'

let taskSeq = 1

const MAX_CHUNK_RETRIES = 3 // 单片重试次数（字节级续传的单次请求重试上限）

function makeFileID(file) {
  // 用 name|size|lastModified 作为浏览器内稳定的续传标识
  const t = (file.lastModified || 0).toString(36)
  return `${file.name}|${file.size}|${t}`.replace(/[^a-zA-Z0-9_\-]/g, '_').slice(0, 64)
}

// 全局上传/下载任务中心：所有页面共享，集中弹窗展示。
// 上传采用串行队列：同时只有一个上传任务在跑（running），其他排队的 pending。
export const useTransferStore = defineStore('transfer', {
  state: () => ({
    // 任务列表: { id, type, name, dir, total, loaded, status, error,
    //           fileID, controller, paused, startFn, _file }
    // status: running | pending | paused | done | error | canceled
    tasks: []
  }),
  getters: {
    runningCount(state) {
      return state.tasks.filter((t) => t.status === 'running').length
    },
    hasTasks(state) {
      return state.tasks.length > 0
    },
    // 是否有真正在传输的上传（用于关闭弹窗时确认）
    hasActiveUploads(state) {
      return state.tasks.some((t) => t.type === 'upload' && (t.status === 'running' || t.status === 'pending'))
    },
    // 所有进行中任务的总速度（bytes/s）
    totalSpeed(state) {
      return state.tasks
        .filter((t) => t.status === 'running')
        .reduce((sum, t) => sum + (t.speed || 0), 0)
    }
  },
  actions: {
    addTask(payload) {
      const id = 't' + Date.now().toString(36) + '_' + taskSeq++
      const t = {
        id,
        type: payload.type || 'upload',
        name: payload.name || '',
        dir: payload.dir || '',
        total: payload.total || 0,
        loaded: 0,
        // 串行队列：先加入的都是 pending，由 runQueue 依次启动
        status: payload.status || 'pending',
        error: '',
        // 字节级续传上传相关
        fileID: payload.fileID || '',
        paused: false,
        retries: 0,
        speed: 0,
        remoteId: payload.remoteId || ''
      }
      this.tasks.push(t)
      if (payload.controller) t.controller = payload.controller
      return id
    },
    /**
     * 取消任务：对上传任务，会 abort 当前正在进行的 XHR
     */
    cancelTask(id) {
      const t = this.tasks.find((x) => x.id === id)
      if (!t) return
      const ctrl = t.controller
      if (ctrl && typeof ctrl.abort === 'function') {
        try { ctrl.abort() } catch (e) { /* ignore */ }
      } else if (typeof ctrl === 'function') {
        try { ctrl() } catch (e) { /* ignore */ }
      }
      if (t.status === 'running' || t.status === 'paused' || t.status === 'pending') {
        t.status = 'canceled'
        t.paused = false
      }
    },
    pauseTask(id) {
      const t = this.tasks.find((x) => x.id === id)
      if (!t) return
      if (t.status !== 'running') return
      t.paused = true
    },
    async resumeTask(id) {
      const t = this.tasks.find((x) => x.id === id)
      if (!t) return
      if (!t.startFn) return
      t.paused = false
      t.status = 'running'
      t.error = ''
      try {
        await t.startFn()
      } catch (e) { /* ignore */ }
    },
    retryTask(id) {
      const t = this.tasks.find((x) => x.id === id)
      if (!t) return
      if (!t.startFn) return
      t.error = ''
      t.retries = 0
      t.status = 'pending'
      t.paused = false
    },
    updateTask(id, loaded) {
      const t = this.tasks.find((x) => x.id === id)
      if (!t) return
      const now = Date.now()
      if (t._lastSpeedAt && loaded >= t.loaded) {
        const dt = now - t._lastSpeedAt
        if (dt >= 200) {
          const bytes = loaded - (t.loaded || 0)
          t.speed = Math.round((bytes / dt) * 1000)
          t._lastSpeedAt = now
        }
      } else if (!t._lastSpeedAt) {
        t._lastSpeedAt = now
      }
      if (t._speedTimer) clearTimeout(t._speedTimer)
      t._speedTimer = setTimeout(() => {
        if (t.status === 'running' && t.speed) t.speed = 0
      }, 1500)
      t.loaded = loaded
    },
    setTaskMeta(id, patch) {
      const t = this.tasks.find((x) => x.id === id)
      if (!t) return
      Object.assign(t, patch)
    },
    setTaskStatus(id, status, error = '') {
      const t = this.tasks.find((x) => x.id === id)
      if (!t) return
      const prevStatus = t.status
      t.status = status
      if (error) t.error = error
      // 已完成的任务 1.5s 后自动移除；若状态在计时期间变更则取消
      if (status === 'done') {
        if (t._doneTimer) clearTimeout(t._doneTimer)
        t._doneTimer = setTimeout(() => {
          this.removeTask(id)
        }, 1500)
      } else if (t._doneTimer) {
        clearTimeout(t._doneTimer)
        delete t._doneTimer
      }
      // pending → running：自动启动该任务的 startFn（串行队列用）
      if (status === 'running' && prevStatus === 'pending' && t.startFn) {
        // 异步调用，避免 setTaskStatus 同步栈中递归
        Promise.resolve().then(() => {
          if (t.status === 'running' && t.startFn) {
            try { t.startFn() } catch (e) { /* ignore */ }
          }
        })
      }
      // 任务从 running → 终态（done/error/canceled）后通知队列调度
      if (prevStatus === 'running' && status !== 'running' && status !== 'paused') {
        if (typeof window !== 'undefined' && window.__lpTransferQueueTick) {
          window.__lpTransferQueueTick()
        }
      }
    },
    registerStartFn(id, fn) {
      const t = this.tasks.find((x) => x.id === id)
      if (t) t.startFn = fn
    },
    registerController(id, controller) {
      const t = this.tasks.find((x) => x.id === id)
      if (t) t.controller = controller
    },
    removeTask(id) {
      this.tasks = this.tasks.filter((x) => x.id !== id)
      if (typeof window !== 'undefined' && window.__lpTransferQueueTick) {
        window.__lpTransferQueueTick()
      }
    },
    clearFinished() {
      this.tasks = this.tasks.filter(
        (t) => t.status === 'running' || t.status === 'paused' || t.status === 'pending'
      )
    }
  }
})

export { MAX_CHUNK_RETRIES, makeFileID }