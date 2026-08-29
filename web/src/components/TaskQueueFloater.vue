<template>
  <!-- 右下角浮窗：有任务时显示，全部完成后显示"已全部安装完成"3 秒后消失 -->
  <div v-show="count > 0 || done" class="task-floater" @click="openList">
    <div class="floater-pill" :class="{ done }" :title="done ? '全部安装已完成' : `正在执行 ${count} 个安装/卸载任务，点击查看详情`">
      <el-icon v-if="!done" class="floater-icon"><Loading /></el-icon>
      <el-icon v-else class="floater-done-icon"><SuccessFilled /></el-icon>
      <span class="floater-text">{{ done ? '已全部安装完成' : `安装任务 (${count})` }}</span>
    </div>
  </div>

  <!-- 任务队列 + 实时日志（左右布局） -->
  <el-dialog
    v-model="listOpen"
    title="应用安装 / 卸载任务"
    width="920px"
    align-center
    :close-on-click-modal="false"
    @close="onDialogClose"
  >
    <div class="task-layout">
      <!-- 左边：任务列表 -->
      <div class="task-list">
        <div class="list-header">
          <span>当前有 <b>{{ count }}</b> 个任务</span>
          <el-button size="small" link @click="loadTasks">
            <el-icon><Refresh /></el-icon>刷新
          </el-button>
        </div>
        <div class="list-body">
          <div
            v-for="t in tasks"
            :key="t.key"
            class="list-item"
            :class="{ active: selectedKey === t.key }"
            @click="selectTask(t.key)"
          >
            <div class="item-name">
              {{ t.name }}
              <el-button
                size="small"
                type="danger"
                plain
                class="item-stop"
                @click.stop="stopTask(t)"
              >
                <el-icon><VideoPause /></el-icon>停止
              </el-button>
            </div>
            <div class="item-meta">
              <el-tag size="small" :type="t.queued ? 'info' : (t.status === 'installing' ? 'primary' : 'warning')" effect="light">
                {{ t.queued ? '排队中' : (t.status === 'installing' ? '安装中' : '卸载中') }}
              </el-tag>
              <span class="item-time">{{ formatTime(t.started_at) }}</span>
            </div>
            <div class="item-elapsed">已用时 {{ elapsed(t.started_at) }}</div>
          </div>
          <div v-if="!tasks.length" class="empty">暂无任务</div>
        </div>
      </div>

      <!-- 右边：实时日志 -->
      <div class="task-log">
        <div class="log-header">
          <span>{{ selectedKey ? `日志：${selectedKey}` : '实时日志' }}</span>
          <el-button v-if="selectedKey" size="small" link @click="openFullLog">
            完整日志
          </el-button>
        </div>
        <pre class="log-body" ref="logEl">{{ selectedKey ? logText : '从左侧选择一个任务查看实时日志输出' }}</pre>
      </div>
    </div>

    <template #footer>
      <el-button @click="listOpen = false">关闭</el-button>
    </template>
  </el-dialog>

  <!-- 完整日志抽屉（保留入口） -->
  <el-drawer
    v-model="logOpen"
    :title="`安装日志：${logKey}`"
    size="70%"
    direction="btt"
  >
    <pre class="drawer-log">{{ logText }}</pre>
  </el-drawer>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount, watch, nextTick } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Loading, Refresh, SuccessFilled, VideoPause } from '@element-plus/icons-vue'
import request from '../utils/request'

const tasks = ref([])
const listOpen = ref(false)
const logOpen = ref(false)
const logKey = ref('')
const logText = ref('')
const selectedKey = ref('')
const logEl = ref(null)
const done = ref(false) // 全部完成提示状态

let pollTimer = null
let logTimer = null
let doneTimer = null
let audioCtx = null
let prevCount = 0 // 跟踪上一次任务数，用于判断从"有"到"无"的瞬间

const count = computed(() => tasks.value.length)

// ===== 音频（成功提示音：C-E-G 大三和弦 + 高八度） =====
// 来源参考 devcode/ui-design-preview/beep-tester.html 中的 success 音效
function unlockAudio() {
  try {
    const AC = window.AudioContext || window.webkitAudioContext
    if (!AC) return
    if (!audioCtx) audioCtx = new AC()
    if (audioCtx.state === 'suspended') audioCtx.resume()
  } catch (e) {}
}
function playSuccess() {
  try {
    if (!audioCtx) return
    const c = audioCtx
    const t0 = c.currentTime
    const note = (freq, start, dur, type = 'sine', peak = 0.18) => {
      const osc = c.createOscillator()
      const g = c.createGain()
      osc.type = type
      osc.frequency.value = freq
      osc.connect(g).connect(c.destination)
      g.gain.setValueAtTime(0, t0 + start)
      g.gain.linearRampToValueAtTime(peak, t0 + start + 0.015)
      g.gain.exponentialRampToValueAtTime(0.0001, t0 + start + dur)
      osc.start(t0 + start)
      osc.stop(t0 + start + dur + 0.02)
    }
    // 上行大三和弦 + 高八度点缀，模拟"叮咚当"成功感
    note(523.25, 0.00, 0.30, 'sine', 0.18) // C5
    note(659.25, 0.10, 0.30, 'sine', 0.18) // E5
    note(783.99, 0.20, 0.50, 'sine', 0.18) // G5
    note(1046.50, 0.32, 0.50, 'triangle', 0.12) // C6
  } catch (e) {}
}

const loadTasks = async () => {
  try {
    const { data } = await request.get('/apps/tasks')
    tasks.value = Array.isArray(data) ? data : []
    // 选中任务被移除时自动回退到第一个
    if (tasks.value.length && !tasks.value.find((t) => t.key === selectedKey.value)) {
      selectTask(tasks.value[0].key)
    }
    if (!tasks.value.length) {
      selectedKey.value = ''
      logText.value = ''
    }
  } catch (e) {}
}

function selectTask(key) {
  selectedKey.value = key
  loadLog()
}

// 停止正在执行的安装/卸载任务
async function stopTask(t) {
  try {
    await ElMessageBox.confirm(
      `确定停止「${t.name}」任务吗？\n将立即终止当前的${t.status === 'installing' ? '安装' : '卸载'}过程。`,
      '停止任务',
      { confirmButtonText: '停止任务', cancelButtonText: '取消', type: 'warning' }
    )
  } catch (_) {
    return
  }
  try {
    await request.post('/apps/tasks/cancel', { key: t.key })
    ElMessage.success('任务已停止')
    loadTasks()
  } catch (e) { /* 错误已由拦截器提示 */ }
}

async function loadLog() {
  if (!selectedKey.value) {
    logText.value = ''
    return
  }
  try {
    const res = await request.get('/apps/log', { params: { key: selectedKey.value } })
    logText.value = (res && res.data && res.data.log) || '(暂无日志)'
    await nextTick()
    if (logEl.value) logEl.value.scrollTop = logEl.value.scrollHeight
  } catch (e) {
    logText.value = '加载日志失败: ' + (e?.message || '未知错误')
  }
}

function openFullLog() {
  if (!selectedKey.value) return
  logKey.value = selectedKey.value
  logOpen.value = true
}

function onDialogClose() {
  selectedKey.value = ''
  logText.value = ''
}

function openList() {
  // 首次点击时解锁音频（浏览器自动播放策略要求用户手势）
  unlockAudio()
  listOpen.value = true
  loadTasks()
}

function formatTime(ts) {
  if (!ts) return '-'
  const d = new Date(ts * 1000)
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}
function elapsed(ts) {
  if (!ts) return '-'
  const s = Math.max(0, Math.floor(Date.now() / 1000 - ts))
  if (s < 60) return s + ' 秒'
  if (s < 3600) return Math.floor(s / 60) + ' 分 ' + (s % 60) + ' 秒'
  return Math.floor(s / 3600) + ' 时 ' + Math.floor((s % 3600) / 60) + ' 分'
}

// 监听任务数变化：所有任务完成（从有变 0）→ 播音 + 浮窗变"已全部安装完成"3 秒后消失
// 弹窗（listOpen）由用户手动开关，不自动关闭
watch(count, (n) => {
  if (prevCount > 0 && n === 0) {
    playSuccess()
    done.value = true
    if (doneTimer) clearTimeout(doneTimer)
    doneTimer = setTimeout(() => {
      done.value = false
    }, 3000)
  }
  prevCount = n
})

onMounted(() => {
  loadTasks()
  pollTimer = setInterval(loadTasks, 3000) // 任务队列
  logTimer = setInterval(() => {
    if (listOpen.value && selectedKey.value) loadLog()
  }, 2000) // 实时日志
})
onBeforeUnmount(() => {
  if (pollTimer) clearInterval(pollTimer)
  if (logTimer) clearInterval(logTimer)
  if (doneTimer) clearTimeout(doneTimer)
})
</script>

<style scoped>
.task-floater {
  position: fixed;
  right: 24px;
  bottom: 24px;
  z-index: 2000;
  cursor: pointer;
  user-select: none;
}
.floater-pill {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 9px 16px 9px 14px;
  background: linear-gradient(135deg, #409eff, #66b1ff);
  color: #fff;
  border-radius: 22px;
  box-shadow: 0 4px 18px rgba(64, 158, 255, 0.4);
  font-size: 13px;
  font-weight: 500;
  transition: transform .15s, box-shadow .15s;
}
.floater-pill:hover {
  transform: translateY(-2px);
  box-shadow: 0 6px 22px rgba(64, 158, 255, 0.55);
}
.floater-pill.done {
  background: linear-gradient(135deg, #67c23a, #85ce61);
  box-shadow: 0 4px 18px rgba(103, 194, 58, 0.45);
}
.floater-pill.done:hover {
  box-shadow: 0 6px 22px rgba(103, 194, 58, 0.6);
}
.floater-icon {
  font-size: 16px;
  animation: spin 1.4s linear infinite;
}
.floater-done-icon {
  font-size: 16px;
}
@keyframes spin {
  to { transform: rotate(360deg); }
}

/* 左右布局 */
.task-layout {
  display: flex;
  gap: 12px;
  height: 460px;
}
.task-list {
  width: 38%;
  border: 1px solid #ebeef5;
  border-radius: 6px;
  background: #fafbfc;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.list-header {
  padding: 10px 12px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid #ebeef5;
  font-size: 13px;
  color: #606266;
  background: #fff;
}
.list-body {
  flex: 1;
  overflow-y: auto;
}
.list-item {
  padding: 10px 12px;
  border-bottom: 1px solid #f0f1f2;
  cursor: pointer;
  transition: background .15s;
}
.list-item:hover {
  background: #f0f7ff;
}
.list-item.active {
  background: #ecf5ff;
  border-left: 3px solid #409eff;
}
.item-name {
  font-size: 14px;
  font-weight: 500;
  color: #303133;
  margin-bottom: 6px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.item-stop {
  margin-left: 0;
  flex-shrink: 0;
}
.item-meta {
  display: flex;
  gap: 8px;
  align-items: center;
  font-size: 12px;
  color: #909399;
}
.item-elapsed {
  font-size: 12px;
  color: #67c23a;
  margin-top: 4px;
}
.empty {
  padding: 30px 10px;
  text-align: center;
  color: #c0c4cc;
  font-size: 13px;
}

.task-log {
  flex: 1;
  border: 1px solid #ebeef5;
  border-radius: 6px;
  background: #1e1e1e;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.log-header {
  padding: 8px 12px;
  background: #2d2d2d;
  color: #e0e0e0;
  font-size: 13px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid #1a1a1a;
}
.log-header :deep(.el-button) {
  color: #e0e0e0;
}
.log-body {
  flex: 1;
  margin: 0;
  padding: 12px 14px;
  color: #d4d4d4;
  font-family: 'Consolas', 'Monaco', 'Courier New', monospace;
  font-size: 12.5px;
  line-height: 1.6;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
}
.drawer-log {
  margin: 0;
  padding: 16px;
  background: #1e1e1e;
  color: #d4d4d4;
  font-family: 'Consolas', 'Monaco', 'Courier New', monospace;
  font-size: 12.5px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
  min-height: 100%;
}
</style>
