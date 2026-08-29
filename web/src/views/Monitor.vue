<template>
  <div class="monitor-page">
    <!-- 监控设置栏 -->
    <el-card shadow="never" class="monitor-control">
      <div class="control-row">
        <div class="control-item">
          <span class="control-label">开启监控</span>
          <el-switch v-model="config.enabled" active-text="开启" inactive-text="关闭" @change="saveConfig" />
        </div>
        <div class="control-item">
          <span class="control-label">保存天数</span>
          <el-input-number v-model="config.save_days" :min="1" :max="365" size="small" controls-position="right" @change="saveConfig" />
          <span class="control-hint">天</span>
        </div>
        <div class="control-item">
          <span class="control-label">监控日志大小</span>
          <el-tag type="info" size="small">{{ formatSize(logSize) }}</el-tag>
        </div>
        <div class="control-actions">
          <el-button type="primary" size="small" @click="alertVisible = true">告警设置</el-button>
          <el-button type="danger" size="small" :icon="Delete" @click="clearHistory">清空记录</el-button>
        </div>
      </div>
    </el-card>

    <!-- 实时指标卡片 -->
    <el-row :gutter="16" class="stat-row">
      <el-col :span="6">
        <el-card shadow="never">
          <div class="gauge">
            <el-progress type="dashboard" :percentage="current.cpu" :width="100" :color="cpuColor(current.cpu)">
              <template #default>
                <div class="gauge-value">{{ current.cpu.toFixed(1) }}%</div>
                <div class="gauge-label">CPU</div>
              </template>
            </el-progress>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never">
          <div class="gauge">
            <el-progress type="dashboard" :percentage="current.mem" :width="100" color="#67c23a">
              <template #default>
                <div class="gauge-value">{{ current.mem.toFixed(1) }}%</div>
                <div class="gauge-label">内存</div>
              </template>
            </el-progress>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never">
          <div class="gauge">
            <el-progress type="dashboard" :percentage="current.disk" :width="100" color="#e6a23c">
              <template #default>
                <div class="gauge-value">{{ current.disk.toFixed(1) }}%</div>
                <div class="gauge-label">磁盘 /</div>
              </template>
            </el-progress>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never">
          <div class="gauge">
            <div class="load-box">
              <div class="load-value">{{ current.load1.toFixed(2) }}</div>
              <div class="gauge-label">系统负载 (1min)</div>
              <div class="load-sub">网速 ↓{{ current.net_in.toFixed(0) }} ↑{{ current.net_out.toFixed(0) }} KB/s</div>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 平均负载 -->
    <el-card shadow="never" class="chart-card">
      <template #header>
        <div class="card-header">
          <span>平均负载</span>
          <div class="range-bar">
            <el-radio-group v-model="rangeType" size="small" @change="onRangeChange">
              <el-radio-button value="today">今天</el-radio-button>
              <el-radio-button value="yesterday">昨天</el-radio-button>
              <el-radio-button value="week">最近七天</el-radio-button>
              <el-radio-button value="custom">自定义时间</el-radio-button>
            </el-radio-group>
            <el-date-picker
              v-if="rangeType === 'custom'"
              v-model="customRange"
              type="datetimerange"
              size="small"
              range-separator="至"
              start-placeholder="开始时间"
              end-placeholder="结束时间"
              value-format="x"
              @change="onRangeChange"
            />
            <el-button link :icon="Refresh" size="small" @click="loadHistory" title="刷新" />
          </div>
        </div>
      </template>
      <LineChart
        :data="loadPoints.map(p => p.load1)"
        :second="loadPoints.map(p => p.load5)"
        :third="loadPoints.map(p => p.load15)"
        :max="loadMax"
        color="#409eff"
        second-color="#67c23a"
        third-color="#e6a23c"
        :labels="loadLabels"
      />
      <div class="legend-row">
        <span class="legend-item"><i class="dot" style="background:#409eff" /> 1分钟</span>
        <span class="legend-item"><i class="dot" style="background:#67c23a" /> 5分钟</span>
        <span class="legend-item"><i class="dot" style="background:#e6a23c" /> 15分钟</span>
      </div>
    </el-card>

    <!-- 资源使用率 -->
    <el-card shadow="never" class="chart-card">
      <template #header>
        <div class="card-header">
          <span>资源使用率</span>
        </div>
      </template>
      <el-row :gutter="16">
        <el-col :span="12">
          <div class="chart-title">CPU 使用率 (%)</div>
          <LineChart :data="points.map(p => p.cpu)" :max="100" color="#409eff" :labels="labels" />
        </el-col>
        <el-col :span="12">
          <div class="chart-title">内存使用率 (%)</div>
          <LineChart :data="points.map(p => p.mem)" :max="100" color="#67c23a" :labels="labels" />
        </el-col>
        <el-col :span="12">
          <div class="chart-title">磁盘使用率 (%)</div>
          <LineChart :data="points.map(p => p.disk)" :max="100" color="#e6a23c" :labels="labels" />
        </el-col>
        <el-col :span="12">
          <div class="chart-title">网络流量 (KB/s) — 入站(红) / 出站(灰)</div>
          <LineChart :data="points.map(p => p.net_in)" :max="netMax" color="#f56c6c" :labels="labels" :second="points.map(p => p.net_out)" second-color="#909399" />
        </el-col>
      </el-row>
    </el-card>

    <p class="update-hint">{{ config.enabled ? '每 5 秒自动刷新，最近约 10 分钟数据' : '监控已关闭，仅显示历史数据' }}</p>

    <!-- 告警设置弹窗 -->
    <el-dialog v-model="alertVisible" title="告警通知设置" width="720px" :close-on-click-modal="false">
      <AlertPanel />
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { Delete, Refresh } from '@element-plus/icons-vue'
import request from '../utils/request'
import { formatBytes as _fmtBytes } from '../utils/format'
import LineChart from '../components/LineChart.vue'
import AlertPanel from '../components/AlertPanel.vue'
import { ElMessage, ElMessageBox } from 'element-plus'

const alertVisible = ref(false)
const config = ref({ enabled: true, save_days: 30 })
const logSize = ref(0)
const points = ref([])
const loadPoints = ref([])
const rangeType = ref('today')
const customRange = ref(null)
let timer = null

const current = computed(() => {
  const last = points.value[points.value.length - 1]
  if (!last) return { cpu: 0, mem: 0, disk: 0, load1: 0, load5: 0, load15: 0, net_in: 0, net_out: 0 }
  return {
    cpu: last.cpu ?? 0,
    mem: last.mem ?? 0,
    disk: last.disk ?? 0,
    load1: last.load1 ?? last.load ?? 0,
    load5: last.load5 ?? 0,
    load15: last.load15 ?? 0,
    net_in: last.net_in ?? 0,
    net_out: last.net_out ?? 0
  }
})

const labels = computed(() => points.value.map((p) => formatTime(p.time)))
const loadLabels = computed(() => loadPoints.value.map((p) => formatTime(p.time)))
const netMax = computed(() => {
  let m = 10
  points.value.forEach((p) => { m = Math.max(m, p.net_in, p.net_out) })
  return Math.ceil(m * 1.2)
})
const loadMax = computed(() => {
  let m = 1
  loadPoints.value.forEach((p) => { m = Math.max(m, p.load1, p.load5, p.load15) })
  return Math.ceil(m * 1.2)
})

function formatTime(ts) {
  const d = new Date(ts * 1000)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

function cpuColor(v) {
  if (v < 50) return '#67c23a'
  if (v < 80) return '#e6a23c'
  return '#f56c6c'
}

function formatSize(bytes) {
  return _fmtBytes(bytes, { empty: '0 B' })
}

async function loadConfig() {
  const res = await request.get('/monitor/config')
  config.value = res.data
}

async function saveConfig() {
  try {
    await request.post('/monitor/config', config.value)
    ElMessage.success('监控设置已保存')
  } catch (e) {
    ElMessage.error('保存失败')
    loadConfig()
  }
}

async function loadSize() {
  const res = await request.get('/monitor/size')
  logSize.value = res.data?.bytes || 0
}

async function loadHistory() {
  const params = { range: rangeType.value }
  if (rangeType.value === 'custom' && customRange.value && customRange.value.length === 2) {
    params.start = Math.floor(customRange.value[0] / 1000)
    params.end = Math.floor(customRange.value[1] / 1000)
  }
  const res = await request.get('/monitor/history', { params })
  loadPoints.value = res.data || []
  // 实时图表仍使用最近内存数据，避免资源图表因自定义时间而跳变
  if (rangeType.value === 'today' || rangeType.value === 'week') {
    points.value = res.data || []
  } else {
    await loadCurrent()
  }
}

async function loadCurrent() {
  const res = await request.get('/monitor/current')
  if (res.data) {
    points.value = [res.data]
  }
}

async function clearHistory() {
  try {
    await ElMessageBox.confirm('确定要清空所有监控历史记录吗？', '提示', { type: 'warning' })
    await request.post('/monitor/clear')
    ElMessage.success('已清空')
    loadHistory()
    loadSize()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('清空失败')
  }
}

function onRangeChange() {
  loadHistory()
}

async function loadAll() {
  await Promise.all([loadConfig(), loadSize(), loadHistory(), loadCurrent()])
}

onMounted(() => {
  loadAll()
  timer = setInterval(() => {
    if (config.value.enabled) {
      loadCurrent()
      if (rangeType.value === 'today') {
        loadHistory()
      }
    }
    loadSize()
  }, 5000)
})

onUnmounted(() => {
  if (timer) clearInterval(timer)
})

watch(rangeType, () => {
  if (rangeType.value !== 'custom') loadHistory()
})
</script>

<style scoped>
.monitor-page { /* 间距由 AdminLayout.lp-main 统一控制（16px），<600px 时为 0 */ }
.monitor-control { margin-bottom: 16px; }
.control-row { display: flex; align-items: center; flex-wrap: wrap; gap: 24px; }
.control-item { display: flex; align-items: center; gap: 8px; }
.control-label { color: #606266; font-size: 14px; }
.control-hint { color: #909399; font-size: 12px; margin-left: -4px; }
.control-actions { margin-left: auto; }
.stat-row { margin-bottom: 16px; }
.gauge { display: flex; justify-content: center; padding: 8px 0; }
.gauge-value { font-size: 22px; font-weight: 700; color: #303133; }
.gauge-label { font-size: 12px; color: #909399; margin-top: 4px; }
.load-box { text-align: center; }
.load-value { font-size: 28px; font-weight: 700; color: #303133; padding-top: 22px; }
.load-sub { font-size: 12px; color: #909399; margin-top: 6px; }
.chart-card { margin-bottom: 16px; }
.card-header { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.range-bar { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.chart-title { font-size: 13px; color: #606266; margin-bottom: 8px; }
.legend-row { display: flex; justify-content: center; gap: 24px; margin-top: 8px; font-size: 12px; color: #606266; }
.legend-item { display: inline-flex; align-items: center; gap: 6px; }
.dot { width: 8px; height: 8px; border-radius: 50%; display: inline-block; }
.update-hint { color: #909399; font-size: 12px; margin-top: 12px; text-align: center; }
</style>
