<template>
  <div class="dashboard">
    <!-- 顶部统计卡片（可拖拽排序） -->
    <div class="stat-grid">
      <div
        v-for="cid in layout"
        :key="cid"
        class="stat-col"
        :class="{ dragging: draggingId === cid }"
        draggable="true"
        @dragstart="onDragStart(cid)"
        @dragover.prevent
        @drop="onDrop(cid)"
        @dragend="onDragEnd"
      >
        <template v-if="cid === 'cpu'">
          <div class="stat-card">
            <div class="stat-card-body">
              <div class="stat-label">CPU 使用率</div>
              <div class="stat-value">{{ info.cpu_percent?.toFixed(1) || '-' }}%</div>
            </div>
            <div class="stat-card-icon" style="background: #ecfdf5; color: #10b981;">
              <el-icon :size="22"><Cpu /></el-icon>
            </div>
          </div>
        </template>
        <template v-if="cid === 'mem'">
          <div class="stat-card">
            <div class="stat-card-body">
              <div class="stat-label">内存使用率</div>
              <div class="stat-value">{{ info.mem_percent?.toFixed(1) || '-' }}%</div>
            </div>
            <div class="stat-card-icon" style="background: #ecfdf5; color: #10b981;">
              <el-icon :size="22"><Memo /></el-icon>
            </div>
          </div>
        </template>
        <template v-if="cid === 'disk'">
          <div class="stat-card">
            <div class="stat-card-body">
              <div class="stat-label">磁盘使用率</div>
              <div class="stat-value">{{ rootDiskPercent }}%</div>
            </div>
            <div class="stat-card-icon" style="background: #ecfdf5; color: #10b981;">
              <el-icon :size="22"><Coin /></el-icon>
            </div>
          </div>
        </template>
        <template v-if="cid === 'diskio'">
          <div class="stat-card">
            <div class="stat-card-body">
              <div class="stat-label">磁盘 IO</div>
              <div class="stat-value" style="font-size: 16px;">
                <span style="color:#10b981;">读</span> {{ diskReadText }}
              </div>
              <div class="stat-value" style="font-size: 16px;">
                <span style="color:#f56c6c;">写</span> {{ diskWriteText }}
              </div>
            </div>
            <div class="stat-card-icon" style="background: #ecfdf5; color: #10b981;">
              <el-icon :size="22"><Files /></el-icon>
            </div>
          </div>
        </template>
        <template v-if="cid === 'net'">
          <div class="stat-card">
            <div class="stat-card-body">
              <div class="stat-label">网络流量</div>
              <div class="stat-value" style="font-size: 16px;">
                <span style="color:#10b981;">↓</span> {{ netInText }}
              </div>
              <div class="stat-value" style="font-size: 16px;">
                <span style="color:#f56c6c;">↑</span> {{ netOutText }}
              </div>
            </div>
            <div class="stat-card-icon" style="background: #ecfdf5; color: #10b981;">
              <el-icon :size="22"><Connection /></el-icon>
            </div>
          </div>
        </template>
        <template v-if="cid === 'total'">
          <div class="stat-card">
            <div class="stat-card-body">
              <div class="stat-label">累计流量</div>
              <div class="stat-value" style="font-size: 16px;">
                <span style="color:#10b981;">↓</span> {{ totalInText }}
              </div>
              <div class="stat-value" style="font-size: 16px;">
                <span style="color:#f56c6c;">↑</span> {{ totalOutText }}
              </div>
            </div>
            <div class="stat-card-icon" style="background: #ecfdf5; color: #10b981;">
              <el-icon :size="22"><DataLine /></el-icon>
            </div>
          </div>
        </template>
        <template v-if="cid === 'alert'">
          <div class="stat-card" style="cursor: pointer;" @click="$router.push('/monitor')">
            <div class="stat-card-body">
              <div class="stat-label">告警</div>
              <div class="stat-value" :style="{ color: summary.alert_count_24h > 0 ? '#f56c6c' : '#10b981' }">
                {{ summary.alert_count_24h || 0 }} <span class="net-unit">条/24h</span>
              </div>
              <div class="stat-sub" :style="{ color: summary.alert_enabled ? '#10b981' : '#94a3b8' }">
                {{ summary.alert_enabled ? '告警已开启' : '告警未开启' }}
              </div>
            </div>
            <div class="stat-card-icon" :style="summary.alert_count_24h > 0 ? 'background:#fef2f2;color:#f56c6c;' : 'background:#ecfdf5;color:#10b981;'">
              <el-icon :size="22"><Warning /></el-icon>
            </div>
          </div>
        </template>
        <template v-if="cid === 'uptime'">
          <div class="stat-card">
            <div class="stat-card-body">
              <div class="stat-label">运行时长</div>
              <div class="stat-value" style="font-size: 22px;">{{ uptimeText }}</div>
            </div>
            <div class="stat-card-icon" style="background: #ecfdf5; color: #10b981;">
              <el-icon :size="22"><Timer /></el-icon>
            </div>
          </div>
        </template>
      </div>
    </div>

    <!-- 系统负载 + 磁盘分区 -->
    <el-row :gutter="16" class="row-gap">
      <el-col :xs="24" :md="10">
        <el-card shadow="never" class="fill-card">
          <template #header>
            <div class="card-head">
              <span>系统负载</span>
              <el-tag size="small" type="success">实时</el-tag>
            </div>
          </template>
          <el-descriptions :column="1" border>
            <el-descriptions-item label="1 分钟负载">{{ info.load1?.toFixed(2) || '-' }}</el-descriptions-item>
            <el-descriptions-item label="5 分钟负载">{{ info.load5?.toFixed(2) || '-' }}</el-descriptions-item>
            <el-descriptions-item label="15 分钟负载">{{ info.load15?.toFixed(2) || '-' }}</el-descriptions-item>
          </el-descriptions>
          <div class="mem-bar-wrap">
            <div class="mem-bar-label">内存使用（{{ formatBytes(info.mem_used) }} / {{ formatBytes(info.mem_total) }}）</div>
            <el-progress :percentage="Number(info.mem_percent?.toFixed(1) || 0)" :stroke-width="12" :color="memColor" />
          </div>
          <div class="mem-bar-wrap">
            <div class="mem-bar-label">Swap 使用（{{ formatBytes(info.swap_used) }} / {{ formatBytes(info.swap_total) }}）</div>
            <el-progress :percentage="swapPercent" :stroke-width="12" :color="swapColor" />
          </div>
        </el-card>
      </el-col>

      <el-col :xs="24" :md="14">
        <el-card shadow="never" class="fill-card">
          <template #header>
            <div class="card-head">
              <span>磁盘分区</span>
              <span class="muted">{{ (info.disks || []).length }} 个挂载点</span>
            </div>
          </template>
          <div class="disk-list">
            <div v-for="d in info.disks || []" :key="d.mount" class="disk-item">
              <div class="disk-item-head">
                <span class="disk-mount">{{ d.mount }}</span>
                <span class="muted">{{ d.fs_type }} · {{ formatBytes(d.used) }} / {{ formatBytes((d.used || 0) + (d.free || 0)) }}</span>
              </div>
              <el-progress :percentage="Number(d.percent.toFixed(1))" :stroke-width="10" :color="diskColor(d.percent)" />
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 进程 TOP + 服务状态 -->
    <el-row :gutter="16" class="row-gap">
      <el-col :xs="24" :md="14">
        <el-card shadow="never" class="fill-card">
          <template #header>
            <div class="card-head">
              <span>进程占用 TOP</span>
              <el-tag size="small" type="success">按 CPU 排序</el-tag>
            </div>
          </template>
          <el-table :data="summary.top_procs || []" size="small" max-height="320">
            <el-table-column prop="pid" label="PID" width="80" />
            <el-table-column prop="user" label="用户" width="90" />
            <el-table-column label="CPU" width="80">
              <template #default="{ row }"><span :style="{ color: diskColor(parseFloat(row.cpu) || 0) }">{{ row.cpu }}%</span></template>
            </el-table-column>
            <el-table-column label="内存" width="80">
              <template #default="{ row }">{{ row.mem }}%</template>
            </el-table-column>
            <el-table-column label="命令" show-overflow-tooltip>
              <template #default="{ row }">{{ row.command }}</template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>

      <el-col :xs="24" :md="10">
        <el-card shadow="never" class="fill-card">
          <template #header>
            <div class="card-head">
              <span>服务状态</span>
              <el-tag size="small" type="info">{{ runningServiceCount }} / {{ (summary.services || []).length }} 运行中</el-tag>
            </div>
          </template>
          <div class="service-list">
            <div v-for="s in summary.services || []" :key="s.key" class="service-item">
              <span class="service-name" :title="s.service">{{ s.name }}</span>
              <div class="service-right">
                <el-tag :type="s.running ? 'success' : (s.active === 'not-found' ? 'info' : 'danger')" size="small">
                  {{ s.running ? '运行中' : (s.active === 'not-found' ? '未安装' : '已停止') }}
                </el-tag>
                <template v-if="s.active !== 'not-found'">
                  <el-dropdown trigger="click" @command="(cmd) => svcAction(s, cmd)">
                    <el-button size="small" plain>
                      操作<i class="el-icon--right"><el-icon><ArrowDown /></el-icon></i>
                    </el-button>
                    <template #dropdown>
                      <el-dropdown-menu>
                        <el-dropdown-item v-if="!s.running" command="start" :disabled="actingKey === s.key + ':start'">启动</el-dropdown-item>
                        <el-dropdown-item v-if="s.running" command="stop" :disabled="actingKey === s.key + ':stop'">停止</el-dropdown-item>
                        <el-dropdown-item command="restart" :disabled="actingKey === s.key + ':restart'">重启</el-dropdown-item>
                      </el-dropdown-menu>
                    </template>
                  </el-dropdown>
                </template>
              </div>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 主机信息（最底部） -->
    <el-card shadow="never" class="row-gap">
      <template #header>
        <div class="card-head">
          <span>主机信息</span>
          <el-tag size="small" type="success">实时</el-tag>
        </div>
      </template>
      <el-descriptions :column="2" border>
        <el-descriptions-item label="主机名">{{ info.hostname || '-' }}</el-descriptions-item>
        <el-descriptions-item label="操作系统">{{ info.os || '-' }}</el-descriptions-item>
        <el-descriptions-item label="内核版本">{{ info.kernel || '-' }}</el-descriptions-item>
        <el-descriptions-item label="架构">{{ info.arch || '-' }}</el-descriptions-item>
        <el-descriptions-item label="CPU 型号">{{ info.cpu_model || '-' }}</el-descriptions-item>
        <el-descriptions-item label="CPU 核数">{{ info.cpu_cores || '-' }}</el-descriptions-item>
        <el-descriptions-item label="面板版本">{{ info.panel_version || '-' }}</el-descriptions-item>
        <el-descriptions-item label="Go 版本">{{ info.go_version || '-' }}</el-descriptions-item>
      </el-descriptions>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import request from '../utils/request'
import { formatBytes as _fmtBytes } from '../utils/format'
import { Cpu, Memo, Coin, Connection, DataLine, Warning, Files, Timer, ArrowDown } from '@element-plus/icons-vue'

const router = useRouter()

const defaultLayout = ['uptime', 'cpu', 'mem', 'disk', 'total', 'net', 'diskio', 'alert']
const layout = ref([...defaultLayout])
const draggingId = ref('')

const info = ref({})
const summary = ref({ net_in_rate: 0, net_out_rate: 0, net_in_total: 0, net_out_total: 0, disk_read_rate: 0, disk_write_rate: 0, alert_enabled: false, alert_count_24h: 0, top_procs: [], services: [] })

const runningServiceCount = computed(() =>
  (summary.value.services || []).filter((s) => s.running).length
)

// 网络实时速率文本
const netInText = computed(() => rateText(summary.value.net_in_rate))
const netOutText = computed(() => rateText(summary.value.net_out_rate))

// 磁盘 IO 文本
const diskReadText = computed(() => rateText(summary.value.disk_read_rate))
const diskWriteText = computed(() => rateText(summary.value.disk_write_rate))

// 累计流量文本
const totalInText = computed(() => bytesText(summary.value.net_in_total))
const totalOutText = computed(() => bytesText(summary.value.net_out_total))

function rateText(v) {
  if (!v || v < 0.01) return '0 KB/s'
  if (v < 1024) return v.toFixed(1) + ' KB/s'
  return (v / 1024).toFixed(2) + ' MB/s'
}

function bytesText(bytes) {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0, v = bytes
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++ }
  return v.toFixed(1) + ' ' + units[i]
}

const rootDiskPercent = computed(() => {
  const root = (info.value.disks || []).find((d) => d.mount === '/')
  return root ? root.percent.toFixed(1) : '-'
})

const swapPercent = computed(() => {
  const s = info.value
  if (!s.swap_total) return 0
  return Number(((s.swap_used / s.swap_total) * 100).toFixed(1))
})

const memColor = computed(() => diskColor(info.value.mem_percent || 0))
const swapColor = computed(() => diskColor(swapPercent.value))

function diskColor(p) {
  if (p >= 85) return '#ef4444'
  if (p >= 70) return '#f59e0b'
  return '#10b981'
}

const uptimeText = computed(() => {
  const sec = info.value.uptime || 0
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  return `${d}天${h}时${m}分`
})

function formatBytes(bytes) {
  return _fmtBytes(bytes, { withTB: true })
}

const actingKey = ref('') // 正在操作的服务

const svcActionLabel = { start: '启动', stop: '停止', restart: '重启' }

async function svcAction(s, action) {
  const label = svcActionLabel[action] || action
  if (action === 'stop' || action === 'restart') {
    const tip = action === 'stop'
      ? `确定停止 ${s.name} 服务吗？停止后依赖此服务的功能将不可用。`
      : `确定重启 ${s.name} 服务吗？服务将短暂中断后自动恢复。`
    try {
      await ElMessageBox.confirm(tip, '操作确认', {
        type: 'warning',
        confirmButtonText: '确定',
        cancelButtonText: '取消'
      })
    } catch {
      return
    }
  }
  actingKey.value = `${s.key}:${action}`
  try {
    await request.post('/apps/service/action', { key: s.key, action })
    ElMessage.success(`${s.name}${label}成功`)
    await loadSummary()
  } catch (e) {
    // 错误提示已由请求拦截器统一处理
  } finally {
    actingKey.value = ''
  }
}

let timer = null
async function loadSummary() {
  try {
    const res = await request.get('/dashboard/summary')
    summary.value = res.data || summary.value
  } catch (e) { /* 静默 */ }
}

async function loadLayout() {
  try {
    const res = await request.get('/dashboard/layout')
    const saved = res.data?.layout
    if (Array.isArray(saved) && saved.length === defaultLayout.length) {
      const set = new Set(saved)
      if (defaultLayout.every(k => set.has(k))) {
        layout.value = saved
      }
    }
  } catch (e) { /* 静默 */ }
}

async function saveLayout(newLayout) {
  try {
    await request.put('/dashboard/layout', { layout: newLayout })
  } catch (e) { /* 错误由拦截器提示 */ }
}

function onDragStart(cid) {
  draggingId.value = cid
}

function onDrop(targetId) {
  const sourceId = draggingId.value
  if (!sourceId || sourceId === targetId) return
  const from = layout.value.indexOf(sourceId)
  const to = layout.value.indexOf(targetId)
  if (from < 0 || to < 0) return
  const newLayout = [...layout.value]
  newLayout.splice(from, 1)
  newLayout.splice(to, 0, sourceId)
  layout.value = newLayout
  saveLayout(newLayout)
}

function onDragEnd() {
  draggingId.value = ''
}

onMounted(async () => {
  const res = await request.get('/system/info')
  info.value = res.data
  await loadLayout()
  await loadSummary()
  // 每 5 秒刷新网络速率 + 进程 TOP + 服务状态
  timer = setInterval(loadSummary, 5000)
})

onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<style scoped>
.dashboard {
  display: flex;
  flex-direction: column;
}

.row-gap {
  margin-top: 16px;
}

/* 顶部统计卡片网格（可拖拽） */
.stat-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
  margin-bottom: 16px;
}
.stat-col {
  cursor: grab;
}
.stat-col.dragging {
  opacity: 0.5;
  cursor: grabbing;
}
@media (max-width: 768px) {
  .stat-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}

/* 同行卡片等高填充 */
.fill-card {
  height: 100%;
  box-sizing: border-box;
}

/* 统计卡：白底圆角浅边框，左侧大数字 + 右侧绿色方块 icon */
.stat-card {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 20px;
  height: 96px;
  box-sizing: border-box;
  background: #ffffff;
  border: 1px solid #e5e7eb;
  border-radius: 12px;
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.04);
  transition: box-shadow 0.2s, transform 0.2s;
}
.stat-card:hover {
  box-shadow: 0 4px 16px rgba(15, 23, 42, 0.08);
  transform: translateY(-1px);
}
.stat-card-body { min-width: 0; }
.stat-label {
  color: #94a3b8;
  font-size: 13px;
  margin-bottom: 6px;
}
.stat-value {
  font-size: 26px;
  font-weight: 700;
  color: #0f172a;
  line-height: 1.1;
  word-break: break-all;
}
.stat-card-icon {
  width: 48px;
  height: 48px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
}
.muted { color: #94a3b8; font-size: 13px; font-weight: normal; }

.mem-bar-wrap {
  margin-top: 16px;
}

/* 磁盘分区卡片 */
.disk-list {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.disk-item-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 6px;
  font-size: 13px;
}
.disk-mount {
  font-weight: 600;
  color: #0f172a;
}

/* 网络流量卡单位 */
.net-unit { font-size: 12px; color: #94a3b8; font-weight: normal; }
.stat-sub { font-size: 12px; color: #94a3b8; margin-top: 2px; }

/* 服务状态列表 */
.service-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 420px;
  overflow-y: auto;
}
.service-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 8px 12px;
  background: #f8fafc;
  border-radius: 8px;
  flex-shrink: 0;
}
.service-name {
  font-size: 13px;
  color: #334155;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
  min-width: 0;
}
.service-right {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
}
.service-right :deep(.el-button + .el-button) {
  margin-left: 0;
}
.mem-bar-label {
  font-size: 13px;
  color: #606266;
  margin-bottom: 6px;
}
</style>
