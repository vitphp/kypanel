<template>
  <div class="process-page">
    <el-card shadow="never" class="process-card">
      <!-- 状态概览行：内嵌在卡片顶部，不再单独成卡（用自定义 flex 排版，避开 el-col 全局复写冲突） -->
      <div class="status-row">
        <div class="status-item status-item--start">
          <div class="status-label">进程总数</div>
          <div class="status-value"><span class="big-num">{{ summary.total }}</span></div>
        </div>
        <div class="status-item status-item--center">
          <div class="status-label">显示 / 上限</div>
          <div class="status-value"><span class="big-num">{{ procs.length }} / {{ effectiveLimit }}</span></div>
        </div>
        <div class="status-item status-item--end">
          <div class="status-label">自动刷新</div>
          <div class="status-value">
            <el-switch v-model="autoRefresh" inline-prompt active-text="开" inactive-text="关" />
            <el-select v-if="autoRefresh" v-model="intervalSec" size="small" style="width: 90px; margin-left: 8px">
              <el-option v-for="s in [3, 5, 10, 30]" :key="s" :label="s + 's'" :value="s" />
            </el-select>
          </div>
        </div>
      </div>

      <el-divider style="margin: 16px 0" />

      <!-- 工具栏 -->
      <div class="toolbar">
        <div class="toolbar-left">
          <el-input
            v-model="keyword"
            placeholder="按 PID / 用户 / 命令过滤"
            clearable
            style="width: 240px"
            @keyup.enter="reload"
            @clear="reload"
          >
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
          <el-select v-model="sortKey" style="width: 140px" @change="reload">
            <el-option label="按 CPU 占用" value="cpu" />
            <el-option label="按内存占比" value="mem" />
            <el-option label="按内存大小" value="rss" />
            <el-option label="按 PID" value="pid" />
            <el-option label="按用户" value="user" />
            <el-option label="按命令名" value="command" />
          </el-select>
          <el-select v-model.number="effectiveLimit" style="width: 110px" @change="reload">
            <el-option v-for="n in limitOptions" :key="n" :label="limitLabel(n)" :value="n" />
          </el-select>
        </div>
        <div class="toolbar-right">
          <el-button :icon="Refresh" @click="reload">刷新</el-button>
          <el-button
            type="danger"
            :icon="Delete"
            :disabled="selected.length === 0"
            @click="batchKill"
          >
            结束选中 ({{ selected.length }})
          </el-button>
        </div>
      </div>

      <!-- 表格 -->
      <Skeleton v-if="loading" type="table" :rows="12" :columns="[{width:'40px'},{width:'80px'},{flex:1},{width:'140px'},{width:'80px'},{width:'80px'},{width:'80px'},{width:'160px'}]" />
      <el-table
        v-else
        :data="procs"
        size="small"
        height="calc(100vh - 280px)"
        stripe
        @selection-change="(rows) => (selected = rows)"
        :row-class-name="rowClass"
      >
        <el-table-column type="selection" width="40" :selectable="canSelect" />
        <el-table-column prop="pid" label="PID" width="80" sortable>
          <template #default="{ row }">
            <span class="mono">{{ row.pid }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="user" label="用户" width="100">
          <template #default="{ row }">
            <el-tag size="small" :type="row.user === 'root' ? 'danger' : 'info'">{{ row.user }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="cpu" label="CPU %" width="110" sortable>
          <template #default="{ row }">
            <div class="bar-cell">
              <el-progress
                :percentage="Math.min(100, parseFloat(row.cpu) * 2)"
                :stroke-width="8"
                :show-text="false"
                :status="cpuStatus(row.cpu)"
                class="bar"
              />
              <span class="mono" :class="cpuTextClass(row.cpu)">{{ row.cpu }}%</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="mem" label="内存 %" width="110" sortable>
          <template #default="{ row }">
            <div class="bar-cell">
              <el-progress
                :percentage="Math.min(100, parseFloat(row.mem) * 2)"
                :stroke-width="8"
                :show-text="false"
                :status="memStatus(row.mem)"
                class="bar"
              />
              <span class="mono">{{ row.mem }}%</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="rss_kb" label="内存占用" width="120">
          <template #default="{ row }">
            <span class="mono">{{ formatBytes(row.rss_kb * 1024) }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="command" label="命令" min-width="280">
          <template #default="{ row }">
            <el-tooltip :content="row.command" placement="top">
              <span class="mono cmd-cell">{{ shortCmd(row.command) }}</span>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column label="操作" :width="isMobile ? 'auto' : 70" align="right" fixed="right" class-name="ops-col">
          <template #default="{ row }">
            <div class="ops-cell">
              <el-button
                size="small"
                type="danger"
                text
                :disabled="!canKill(row)"
                @click="kill(row)"
              >
                结束
              </el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Search, Refresh, Delete } from '@element-plus/icons-vue'
import request from '../utils/request'
import { formatBytes as _fmtBytes } from '../utils/format'

const procs = ref([])
const loading = ref(true)
const keyword = ref('')
const sortKey = ref('cpu')
const effectiveLimit = ref(200)
const limitOptions = [50, 100, 200, 500]
function limitLabel(n) {
  return '前 ' + n
}
const selected = ref([])

const autoRefresh = ref(false)
const intervalSec = ref(5)
let timer = null

const summary = reactive({ total: 0 })

function canSelect(row) {
  return !isProtectedProcess(row)
}
function isProtectedProcess(row) {
  // PID 0/1（kernel/init）和名为 systemd/init 的不让选
  if (!row || row.pid === undefined) return false
  if (row.pid <= 1) return true
  const base = (row.command || '').split(' ')[0]
  return ['systemd', 'init', 'kthreadd'].includes(base)
}
function canKill(row) {
  if (row.pid <= 1) return false
  if (row.user === 'root' && isProtectedProcess(row)) return false
  return true
}

function rowClass({ row }) {
  if (parseFloat(row.cpu) >= 50) return 'row-hot'
  if (parseFloat(row.mem) >= 30) return 'row-mem-hot'
  return ''
}

function cpuStatus(v) {
  const n = parseFloat(v)
  if (n >= 80) return 'exception'
  if (n >= 40) return 'warning'
  return 'success'
}
function cpuTextClass(v) {
  const n = parseFloat(v)
  if (n >= 80) return 'text-danger'
  if (n >= 40) return 'text-warning'
  return 'text-success'
}
function memStatus(v) {
  const n = parseFloat(v)
  if (n >= 50) return 'exception'
  if (n >= 20) return 'warning'
  return 'success'
}
function formatBytes(n) {
  return _fmtBytes(n, { empty: '0 B', withTB: true })
}
function shortCmd(cmd) {
  if (!cmd) return ''
  if (cmd.length <= 60) return cmd
  return cmd.slice(0, 57) + '...'
}

async function reload() {
  loading.value = true
  try {
    const res = await request.get('/process/list', {
      params: {
        sort: sortKey.value,
        q: keyword.value || undefined,
        limit: effectiveLimit.value,
      },
    })
    procs.value = res.data || []
    summary.total = procs.value.length
  } catch (e) {
    ElMessage.error(e.message || '加载进程失败')
  } finally {
    loading.value = false
  }
}

async function kill(row) {
  try {
    await ElMessageBox.confirm(
      `确认结束进程 ${row.pid}（${shortCmd(row.command)}）？\n使用 kill -9，强制终止。`,
      '危险操作',
      { type: 'warning', confirmButtonText: '结束进程', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  try {
    await request.post('/process/kill', { pid: row.pid })
    ElMessage.success(`PID ${row.pid} 已结束`)
    reload()
  } catch (e) {
    ElMessage.error(e.message || '结束失败')
  }
}

async function batchKill() {
  const list = selected.value.filter(canKill)
  if (list.length === 0) {
    ElMessage.warning('没有可结束的进程（含不可结束的 PID）')
    return
  }
  try {
    await ElMessageBox.confirm(
      `将结束 ${list.length} 个进程（已过滤系统关键进程）。\n继续？`,
      '批量结束',
      { type: 'warning', confirmButtonText: '继续', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  let ok = 0, fail = 0
  for (const p of list) {
    try {
      await request.post('/process/kill', { pid: p.pid })
      ok++
    } catch {
      fail++
    }
  }
  ElMessage.success(`完成：成功 ${ok}，失败 ${fail}`)
  reload()
}

function startTimer() {
  stopTimer()
  if (!autoRefresh.value) return
  timer = setInterval(reload, intervalSec.value * 1000)
}
function stopTimer() {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
}

watch(autoRefresh, startTimer)
watch(intervalSec, startTimer)

onMounted(() => {
  reload()
})
onBeforeUnmount(stopTimer)
</script>

<style scoped>
.process-card { margin-bottom: 16px; }
/* 状态概览行：内嵌在卡片顶部（自定义 flex，避开 el-col 全局复写） */
.status-row {
  margin-bottom: 0;
  display: flex;
  align-items: center;
  gap: 12px;
}
.status-item {
  flex: 1 1 0;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 4px;
}
/* 第一个靠左、中间居中、第三个靠右 */
.status-item--start { justify-content: flex-start; }
.status-item--center { justify-content: center; }
.status-item--end { justify-content: flex-end; }
/* 窄屏紧凑两列网格：让"进程总数 / 显示上限 / 自动刷新"更紧凑 */
@media (max-width: 767px) {
  .status-row {
    flex-wrap: wrap;
    gap: 4px 12px;
  }
  .status-item { flex: 0 0 calc(50% - 6px); justify-content: flex-start; padding: 4px; }
  .big-num { font-size: 18px; }
  .status-label { font-size: 12px; }
}
.status-label {
  color: #909399;
  font-size: 13px;
  white-space: nowrap;
}
.status-value {
  display: flex;
  align-items: center;
  gap: 6px;
}
.big-num {
  font-size: 22px;
  font-weight: 600;
  color: #303133;
}
/* 工具栏：在卡片内部，不再自绘背景/阴影 */
.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 10px;
}
.toolbar-left,
.toolbar-right {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.mono {
  font-family: Consolas, Menlo, monospace;
  font-size: 12.5px;
}
.bar-cell {
  display: flex;
  align-items: center;
  gap: 8px;
}
.bar {
  flex: 1;
  min-width: 0;
}
.text-danger {
  color: #f56c6c;
  font-weight: 600;
}
.text-warning {
  color: #e6a23c;
  font-weight: 600;
}
.text-success {
  color: #67c23a;
}
.cmd-cell {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  vertical-align: middle;
}
:deep(.el-table .row-hot) {
  background: rgba(245, 108, 108, 0.06) !important;
}
:deep(.el-table .row-mem-hot) {
  background: rgba(230, 162, 60, 0.06) !important;
}
</style>
