<template>
  <el-dialog
    v-model="visible"
    width="960"
    top="5vh"
    :close-on-click-modal="false"
    :title="`站点访问统计 — ${siteName || ''}`"
    destroy-on-close
    append-to-body
  >
    <div v-loading="loading" class="ss-dialog">
      <!-- 时间段 -->
      <el-radio-group v-model="rangeKind" size="default" @change="onRangeChange">
        <el-radio-button label="today">今日</el-radio-button>
        <el-radio-button label="yesterday">昨天</el-radio-button>
        <el-radio-button label="before_yesterday">前天</el-radio-button>
        <el-radio-button label="7d">最近 7 天</el-radio-button>
        <el-radio-button label="30d">最近 30 天</el-radio-button>
        <el-radio-button label="custom">自定义</el-radio-button>
      </el-radio-group>
      <div class="ss-custom-range" v-if="rangeKind === 'custom'">
        <el-date-picker v-model="customRange" type="daterange" range-separator="至"
          start-placeholder="开始日期" end-placeholder="结束日期"
          format="YYYY-MM-DD" value-format="YYYY-MM-DD"
          :disabled-date="disabledDate" />
        <el-button type="primary" plain @click="load" :disabled="!customRange">查询</el-button>
        <span class="ss-tip">最长支持 90 天</span>
      </div>
      <div class="ss-tip-row">
        统计区间：<b>{{ data?.range?.from || '-' }}</b> ~ <b>{{ data?.range?.to || '-' }}</b>
        <el-tag v-if="data?.ip_region_enabled" type="success" effect="plain" size="small" style="margin-left:8px;">IP 归属已启用</el-tag>
        <el-tag v-else type="info" effect="plain" size="small" style="margin-left:8px;">IP 归属未启用</el-tag>
      </div>

      <!-- 汇总卡片 -->
      <div v-if="data" class="ss-cards">
        <div class="ss-card">
          <div class="ss-card-label">流量</div>
          <div class="ss-card-value">{{ fmtBytes(data.range.traffic) }}</div>
        </div>
        <div class="ss-card">
          <div class="ss-card-label">请求数</div>
          <div class="ss-card-value">{{ data.range.requests.toLocaleString() }}</div>
        </div>
        <div class="ss-card">
          <div class="ss-card-label">独立 IP</div>
          <div class="ss-card-value">{{ data.range.ips.toLocaleString() }}</div>
        </div>
        <div class="ss-card">
          <div class="ss-card-label">UV</div>
          <div class="ss-card-value">{{ data.range.uv.toLocaleString() }}</div>
        </div>
        <div class="ss-card">
          <div class="ss-card-label">PV</div>
          <div class="ss-card-value">{{ data.range.pv.toLocaleString() }}</div>
        </div>
      </div>

      <!-- 状态码细分 -->
      <div v-if="data" class="ss-status-row">
        <el-tag effect="plain" type="success">2xx {{ data.range.status_2xx }}</el-tag>
        <el-tag effect="plain" type="info">3xx {{ data.range.status_3xx }}</el-tag>
        <el-tag effect="plain" type="warning">4xx {{ data.range.status_4xx }}</el-tag>
        <el-tag effect="plain" type="danger">5xx {{ data.range.status_5xx }}</el-tag>
      </div>

      <!-- 曲线图 -->
      <div class="ss-chart-wrap" v-if="data && data.series && data.series.days && data.series.days.length">
        <div class="ss-chart-legend">
          <span class="ss-legend-item" :class="{ active: shown.traffic }" @click="shown.traffic = !shown.traffic">
            <i style="background:#409eff"></i>流量 (B)
          </span>
          <span class="ss-legend-item" :class="{ active: shown.requests }" @click="shown.requests = !shown.requests">
            <i style="background:#67c23a"></i>请求数
          </span>
          <span class="ss-legend-item" :class="{ active: shown.ips }" @click="shown.ips = !shown.ips">
            <i style="background:#e6a23c"></i>独立 IP
          </span>
        </div>
        <div ref="chartEl" class="ss-chart">
          <svg :width="chartSize.w" :height="chartSize.h" :viewBox="`0 0 ${chartSize.w} ${chartSize.h}`" preserveAspectRatio="none">
            <!-- 网格横线 -->
            <g class="ss-grid">
              <line v-for="(g, i) in 5" :key="'g' + i"
                :x1="pad.l" :x2="chartSize.w - pad.r"
                :y1="pad.t + (chartSize.h - pad.t - pad.b) * g / 5"
                :y2="pad.t + (chartSize.h - pad.t - pad.b) * g / 5"
                stroke="#ebeef5" stroke-width="1" />
            </g>
            <!-- Y 轴标签 -->
            <g class="ss-axis">
              <text v-for="(g, i) in 5" :key="'y' + i"
                :x="pad.l - 6" :y="pad.t + (chartSize.h - pad.t - pad.b) * g / 5 + 4"
                text-anchor="end" fill="#909399" font-size="11">{{ yLabels[i] }}</text>
            </g>
            <!-- X 轴标签 -->
            <g class="ss-axis">
              <text v-for="(d, i) in xLabels" :key="'x' + i"
                :x="d.x" :y="chartSize.h - 6"
                text-anchor="middle" fill="#909399" font-size="11">{{ d.text }}</text>
            </g>
            <!-- 数据折线 -->
            <polyline v-if="shown.traffic && trafficPointsArr.length"
              :points="trafficPoly" fill="none" stroke="#409eff" stroke-width="1.8" />
            <polyline v-if="shown.requests && requestsPointsArr.length"
              :points="requestsPoly" fill="none" stroke="#67c23a" stroke-width="1.8" />
            <polyline v-if="shown.ips && ipsPointsArr.length"
              :points="ipsPoly" fill="none" stroke="#e6a23c" stroke-width="1.8" />
            <!-- 数据点 (悬停用) -->
            <circle v-for="(p, idx) in trafficPointsArr" v-show="shown.traffic"
              :key="'c' + idx" :cx="p.x" :cy="p.y" r="3" fill="#409eff"
              :stroke="idx === hoverIdx ? '#000' : 'none'" stroke-width="1.5" />
            <circle v-for="(p, idx) in requestsPointsArr" v-show="shown.requests"
              :key="'r' + idx" :cx="p.x" :cy="p.y" r="3" fill="#67c23a"
              :stroke="idx === hoverIdx ? '#000' : 'none'" stroke-width="1.5" />
            <circle v-for="(p, idx) in ipsPointsArr" v-show="shown.ips"
              :key="'i' + idx" :cx="p.x" :cy="p.y" r="3" fill="#e6a23c"
              :stroke="idx === hoverIdx ? '#000' : 'none'" stroke-width="1.5" />
            <!-- 透明触发竖线 + tooltip -->
            <g>
              <rect v-for="(d, i) in data.series.days" :key="'t' + i"
                :x="xPos(i) - 12" :y="pad.t" width="24"
                :height="chartSize.h - pad.t - pad.b" fill="rgba(0,0,0,0)"
                @mouseenter="hoverIdx = i" @mouseleave="hoverIdx = -1" />
            </g>
            <foreignObject v-if="hoverIdx >= 0" :x="tooltipX" :y="pad.t" width="240" height="120">
              <div class="ss-tooltip">
                <div class="ss-tip-day">{{ data.series.days[hoverIdx] }}</div>
                <div v-if="shown.traffic"><i style="background:#409eff"></i>流量 {{ fmtBytes(data.series.traffic[hoverIdx]) }}</div>
                <div v-if="shown.requests"><i style="background:#67c23a"></i>请求数 {{ data.series.requests[hoverIdx].toLocaleString() }}</div>
                <div v-if="shown.ips"><i style="background:#e6a23c"></i>独立 IP {{ data.series.ips[hoverIdx].toLocaleString() }}</div>
              </div>
            </foreignObject>
          </svg>
        </div>
      </div>
      <div v-else-if="data" class="ss-empty">该时间段暂无访问数据</div>

      <el-tabs v-if="data" v-model="tab" class="ss-tabs">
        <!-- 地域分布 -->
        <el-tab-pane label="访客归属地" name="region">
          <el-table :data="data.regions || []" max-height="320" empty-text="未启用 IP 归属 / 无数据">
            <el-table-column prop="province" label="省份" width="100" />
            <el-table-column prop="city" label="城市" width="120" />
            <el-table-column prop="requests" label="请求数" width="120" />
            <el-table-column prop="ips" label="独立 IP" width="100" />
            <el-table-column prop="traffic" label="流量" width="120">
              <template #default="{ row }">{{ fmtBytes(row.traffic) }}</template>
            </el-table-column>
            <el-table-column prop="requests" label="占比">
              <template #default="{ row }">
                <el-progress :percentage="(row.requests / Math.max(1, data.range.requests) * 100)"
                  :stroke-width="8" :show-text="false" style="width: 60%;" />
                <span style="margin-left: 6px; color: #909399;">{{ ((row.requests / Math.max(1, data.range.requests)) * 100).toFixed(1) }}%</span>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>

        <!-- Top 路径 -->
        <el-tab-pane label="访问路径排行" name="path">
          <el-table :data="data.top_paths || []" max-height="320" empty-text="暂无数据">
            <el-table-column type="index" label="#" width="50" />
            <el-table-column prop="path" label="路径" />
            <el-table-column prop="requests" label="请求数" width="120" />
            <el-table-column prop="traffic" label="流量" width="120">
              <template #default="{ row }">{{ fmtBytes(row.traffic) }}</template>
            </el-table-column>
          </el-table>
        </el-tab-pane>

        <!-- Top IP -->
        <el-tab-pane label="来访 IP 排行" name="ip">
          <el-table :data="data.top_ips || []" max-height="320" empty-text="暂无数据">
            <el-table-column type="index" label="#" width="50" />
            <el-table-column prop="ip" label="IP" width="180" />
            <el-table-column prop="province" label="省份" width="100" />
            <el-table-column prop="city" label="城市" width="120" />
            <el-table-column prop="isp" label="ISP" />
            <el-table-column prop="requests" label="请求数" width="100" />
            <el-table-column prop="visited_at" label="最近访问" width="180">
              <template #default="{ row }">
                {{ row.visited_at ? new Date(row.visited_at).toLocaleString() : '-' }}
              </template>
            </el-table-column>
            <el-table-column label="操作" width="100" fixed="right">
              <template #default="{ row }">
                <el-button size="small" type="danger" link @click="openBlockIPDialog(row)">拉黑</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
      </el-tabs>
    </div>

    <!-- 拉黑 IP 弹窗 -->
    <el-dialog
      v-model="blockDialogVisible"
      title="拉黑 IP"
      width="440px"
      append-to-body
      destroy-on-close
    >
      <div v-if="blockTarget">
        <el-alert :closable="false" type="warning" show-icon style="margin-bottom: 14px">
          <div>目标 IP：<b style="font-size: 15px;">{{ blockTarget.ip }}</b></div>
          <div style="font-size: 12px; margin-top: 4px; color: #909399;">
            归属地 {{ blockTarget.province || '未知' }} {{ blockTarget.city || '' }} · 请求 {{ blockTarget.requests }} 次
          </div>
        </el-alert>
        <el-form label-width="80px">
          <el-form-item label="拉黑范围">
            <el-radio-group v-model="blockForm.scope">
              <el-radio value="site">仅本站点</el-radio>
              <el-radio value="global">全服务器</el-radio>
            </el-radio-group>
            <div class="ss-block-tip">
              <span v-if="blockForm.scope === 'site'">仅阻止此 IP 访问当前站点（{{ siteName || '' }}），不影响其他站点</span>
              <span v-else>阻止此 IP 访问服务器上所有站点（同步到 WAF 全局黑名单）</span>
            </div>
          </el-form-item>
          <el-form-item label="拉黑时长">
            <el-radio-group v-model="blockForm.duration">
              <el-radio value="3600">1 小时</el-radio>
              <el-radio value="86400">1 天</el-radio>
              <el-radio value="604800">7 天</el-radio>
              <el-radio value="0">永久</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="备注">
            <el-input v-model="blockForm.remark" placeholder="可选" maxlength="200" show-word-limit />
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <el-button @click="blockDialogVisible = false">取消</el-button>
        <el-button type="danger" :loading="blockSubmitting" @click="submitBlockIP">确认拉黑</el-button>
      </template>
    </el-dialog>
  </el-dialog>
</template>

<script setup>
import { ref, computed, watch, nextTick, onBeforeUnmount, onMounted } from 'vue'
import request from '@/utils/request'

const props = defineProps({
  modelValue: Boolean,
  siteId: { type: Number, default: 0 },
  siteName: { type: String, default: '' }
})
const emit = defineEmits(['update:modelValue'])

const visible = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v)
})

const loading = ref(false)
const rangeKind = ref('today')
const customRange = ref(null)
const data = ref(null)
const tab = ref('region')
const hoverIdx = ref(-1)

const chartEl = ref(null)
const chartSize = ref({ w: 880, h: 240 })
const pad = { l: 50, r: 16, t: 12, b: 28 }

const shown = ref({ traffic: true, requests: true, ips: true })

function onRangeChange() {
  if (rangeKind.value !== 'custom') load()
}

async function load() {
  if (!props.siteId) return
  loading.value = true
  data.value = null
  try {
    const params = { id: props.siteId, range: rangeKind.value }
    if (rangeKind.value === 'custom' && customRange.value && customRange.value.length === 2) {
      params.start = customRange.value[0]
      params.end = customRange.value[1]
    }
    const res = await request.get('/site/stat', { params })
    if (res && res.code === 0) {
      data.value = res.data
      await nextTick()
      measureChart()
    }
  } finally {
    loading.value = false
  }
}

// ---------- 拉黑 IP ----------
const blockDialogVisible = ref(false)
const blockSubmitting = ref(false)
const blockTarget = ref(null) // 整行 IP 信息
const blockForm = ref({
  scope: 'site',         // site / global
  duration: '86400',     // 秒数或 0=永久
  remark: ''
})

function openBlockIPDialog(row) {
  blockTarget.value = row
  blockForm.value = { scope: 'site', duration: '86400', remark: '' }
  blockDialogVisible.value = true
}

async function submitBlockIP() {
  if (!blockTarget.value || !props.siteId) return
  blockSubmitting.value = true
  try {
    const ip = blockTarget.value.ip
    const duration = Number(blockForm.value.duration) || 0
    if (blockForm.value.scope === 'site') {
      // 站点级：仅对该站点生效
      await request.post('/site/blockip/add', {
        site_id: props.siteId,
        ip: ip,
        expire_seconds: duration,
        remark: blockForm.value.remark || '访问统计拉黑'
      })
    } else {
      // 全服务器：复用 WAF 全局 IP 拉黑
      await request.post('/waf/iprule/add', {
        type: 'ip',
        action: 'block',
        content: ip,
        expire_seconds: duration,
        remark: blockForm.value.remark || `访问统计拉黑 [${props.siteName || ''}]`
      })
    }
    ElMessage.success('已加入拉黑名单')
    blockDialogVisible.value = false
  } catch (e) { /* interceptor handles */ } finally {
    blockSubmitting.value = false
  }
}

function disabledDate(date) {
  const today = new Date()
  today.setHours(23, 59, 59, 999)
  const min = new Date()
  min.setDate(min.getDate() - 89)
  return date > today || date < min
}

function measureChart() {
  if (!chartEl.value) return
  const w = chartEl.value.clientWidth || 880
  const h = chartEl.value.clientHeight || 240
  chartSize.value = { w: Math.max(560, w), h: Math.max(180, h) }
}

const xLabels = computed(() => {
  if (!data.value?.series?.days?.length) return []
  const days = data.value.series.days
  const step = Math.max(1, Math.floor(days.length / 8))
  return days.map((d, i) => ({
    text: i % step === 0 || i === days.length - 1 ? d.slice(5) : '',
    x: xPos(i)
  }))
})

// 三条曲线分别独立的 y 缩放（避免被大数字压扁）
const trafficScale = computed(() => scaleFor(data.value?.series?.traffic || []))
const requestsScale = computed(() => scaleFor(data.value?.series?.requests || []))
const ipsScale = computed(() => scaleFor(data.value?.series?.ips || []))

function scaleFor(arr) {
  const max = arr.reduce((a, b) => Math.max(a, b), 0)
  if (max <= 0) return { max: 1 }
  // 顶部留 10% 空间
  return { max: max * 1.1 }
}

function yPos(value, scale) {
  const innerH = chartSize.value.h - pad.t - pad.b
  if (!scale.max) return pad.t + innerH
  return pad.t + innerH - (value / scale.max) * innerH
}

function xPos(i) {
  const days = data.value?.series?.days || []
  const innerW = chartSize.value.w - pad.l - pad.r
  if (days.length <= 1) return pad.l + innerW / 2
  return pad.l + (i / (days.length - 1)) * innerW
}

const trafficPointsArr = computed(() => buildPoints(data.value?.series?.traffic || [], trafficScale.value))
const requestsPointsArr = computed(() => buildPoints(data.value?.series?.requests || [], requestsScale.value))
const ipsPointsArr = computed(() => buildPoints(data.value?.series?.ips || [], ipsScale.value))
const trafficPoly = computed(() => trafficPointsArr.value.map((p) => `${p.x},${p.y}`).join(' '))
const requestsPoly = computed(() => requestsPointsArr.value.map((p) => `${p.x},${p.y}`).join(' '))
const ipsPoly = computed(() => ipsPointsArr.value.map((p) => `${p.x},${p.y}`).join(' '))

function buildPoints(arr, scale) {
  if (!arr.length) return []
  return arr.map((v, i) => ({ x: xPos(i), y: yPos(v, scale) }))
}

const yLabels = computed(() => {
  // 显示流量对应的最大刻度（traffic 取值跨度最大，最能突出量级）
  const scale = trafficScale.value
  return Array.from({ length: 6 }).map((_, i) => {
    const v = (scale.max / 5) * (5 - i)
    return fmtBytesShort(v)
  })
})

const tooltipX = computed(() => {
  if (hoverIdx.value < 0) return 0
  const tx = xPos(hoverIdx.value) - 120
  return Math.max(8, Math.min(tx, chartSize.value.w - 248))
})

function fmtBytes(n) {
  if (n == null || isNaN(n)) return '-'
  if (n < 1024) return n + ' B'
  if (n < 1024 * 1024) return (n / 1024).toFixed(2) + ' KB'
  if (n < 1024 * 1024 * 1024) return (n / 1024 / 1024).toFixed(2) + ' MB'
  return (n / 1024 / 1024 / 1024).toFixed(2) + ' GB'
}
function fmtBytesShort(n) {
  if (n == null || isNaN(n)) return '-'
  if (n < 1024) return n + 'B'
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + 'K'
  if (n < 1024 * 1024 * 1024) return (n / 1024 / 1024).toFixed(1) + 'M'
  return (n / 1024 / 1024 / 1024).toFixed(1) + 'G'
}

watch(visible, (v) => {
  if (v) load()
})

let ro = null
onMounted(() => {
  if (window.ResizeObserver) {
    ro = new ResizeObserver(() => measureChart())
    if (chartEl.value) ro.observe(chartEl.value)
  } else {
    window.addEventListener('resize', measureChart)
  }
})
onBeforeUnmount(() => {
  if (ro && chartEl.value) ro.unobserve(chartEl.value)
  else window.removeEventListener('resize', measureChart)
})
</script>

<style scoped>
.ss-dialog { padding: 0 8px; }
.ss-custom-range { margin: 10px 0; display: flex; align-items: center; gap: 8px; }
.ss-tip-row { margin: 8px 0 16px; color: #606266; font-size: 13px; }
.ss-tip { margin-left: 8px; color: #909399; font-size: 12px; }
.ss-cards {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  gap: 12px;
  margin: 12px 0;
}
.ss-card {
  background: linear-gradient(135deg, #f7f9fc, #fff);
  border: 1px solid #ebeef5;
  border-radius: 8px;
  padding: 14px 16px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.ss-card-label { color: #909399; font-size: 12px; }
.ss-card-value { color: #303133; font-size: 22px; font-weight: 700; font-variant-numeric: tabular-nums; }
.ss-status-row {
  display: flex;
  gap: 8px;
  align-items: center;
  margin: 8px 0 12px;
}
.ss-chart-wrap {
  background: #fff;
  border: 1px solid #ebeef5;
  border-radius: 8px;
  padding: 8px 12px 4px;
  margin: 8px 0 16px;
}
.ss-chart-legend {
  display: flex;
  gap: 18px;
  font-size: 12px;
  color: #606266;
  padding: 4px 6px;
}
.ss-legend-item {
  cursor: pointer;
  user-select: none;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  opacity: 0.4;
}
.ss-legend-item.active { opacity: 1; }
.ss-legend-item i {
  display: inline-block;
  width: 10px;
  height: 10px;
  border-radius: 2px;
}
.ss-chart {
  width: 100%;
  height: 240px;
}
.ss-chart svg {
  width: 100%;
  height: 100%;
}
.ss-tooltip {
  background: rgba(20, 20, 24, 0.92);
  color: #fff;
  padding: 8px 10px;
  border-radius: 6px;
  font-size: 12px;
  pointer-events: none;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.ss-tip-day {
  font-weight: 600;
  margin-bottom: 4px;
  font-size: 12px;
}
.ss-tooltip i {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 2px;
  margin-right: 6px;
  vertical-align: middle;
}
.ss-empty {
  text-align: center;
  color: #909399;
  padding: 24px 0;
}
.ss-tabs { margin-top: 8px; }
.ss-tabs :deep(.el-tabs__content) { overflow: visible; }

/* 弹窗过高时整体可滚（防止归属地 50 行 + 路径/IP 排行把内容挤出屏幕） */
:deep(.el-dialog) {
  max-height: 92vh !important;
  margin-top: 4vh !important;
  display: flex !important;
  flex-direction: column !important;
}
:deep(.el-dialog__header) {
  flex-shrink: 0;
}
:deep(.el-dialog__body) {
  flex: 1 1 auto !important;
  overflow-y: auto !important;
  padding: 16px 20px !important;
  min-height: 0; /* 关键：让 flex 子项可缩小触发滚动 */
}
:deep(.el-dialog__body::-webkit-scrollbar) { width: 6px; }
:deep(.el-dialog__body::-webkit-scrollbar-thumb) { background: #c0c4cc; border-radius: 3px; }
:deep(.el-dialog__body::-webkit-scrollbar-thumb:hover) { background: #909399; }
</style>
