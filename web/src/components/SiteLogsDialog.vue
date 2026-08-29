<template>
  <el-dialog
    v-model="visible"
    :title="`访问日志 - ${site.name || ''}`"
    width="90%"
    top="5vh"
    destroy-on-close
    class="site-logs-dialog"
    :style="isMobile ? 'width: 96vw; max-width: 100%' : ''"
  >
    <template #header>
      <div class="logs-dialog-header">
        <span class="logs-dialog-title">访问日志 - {{ site.name || '' }}</span>
        <el-button size="small" :icon="Refresh" @click="loadAll" :loading="loading">刷新</el-button>
      </div>
    </template>
    <div class="logs-layout" v-loading="loading">
      <!-- 左侧菜单（桌面）/ 顶部 Tab（移动） -->
      <div class="logs-menu" :class="{ 'logs-menu-mobile': isMobile }">
        <div
          v-for="m in menus"
          :key="m.key"
          :class="['logs-menu-item', { active: activeMenu === m.key }]"
          @click="switchMenu(m.key)"
        >
          <el-icon class="logs-menu-icon"><component :is="m.icon" /></el-icon>
          <div class="logs-menu-content">
            <div class="logs-menu-title">{{ m.title }}</div>
            <div class="logs-menu-desc">{{ m.desc }}</div>
          </div>
        </div>
        <div class="logs-menu-footer" v-if="!isMobile"></div>
      </div>

      <!-- 右侧内容 -->
      <div class="logs-content">
        <!-- 1. 原始日志（结构化表格） -->
        <div v-show="activeMenu === 'raw'" class="logs-panel">
          <div class="logs-toolbar">
            <span class="logs-stat">
              显示 {{ filteredEntries.length }} / {{ result.total_lines || 0 }} 行
              <el-tag v-if="result.truncated" size="small" type="warning" effect="plain">已截断</el-tag>
              <el-tag v-else-if="!result.has_file" size="small" type="info" effect="plain">暂无日志</el-tag>
            </span>
            <el-input
              v-model="rawFilter"
              size="small"
              placeholder="过滤 IP / URL / 状态码"
              :style="isMobile ? 'flex: 1; min-width: 0' : 'width: 260px'"
              clearable
            />
            <el-button v-if="isMobile" size="small" :icon="Refresh" @click="loadAll" :loading="loading" style="margin-left: 8px">刷新</el-button>
          </div>
          <el-table
            :data="filteredEntries"
            stripe
            size="small"
            :max-height="isMobile ? 'calc(100vh - 230px)' : 'calc(75vh - 40px)'"
            :empty-text="result.has_file === false ? '暂无访问日志' : '无匹配结果'"
            style="width: 100%"
            @sort-change="(s) => (rawSort = s)"
          >
            <el-table-column label="时间" :width="isMobile ? 150 : 170" prop="time" sortable="custom">
              <template #default="{ row }">{{ formatNginxTime(row.time) }}</template>
            </el-table-column>
            <el-table-column label="IP" :width="isMobile ? 150 : 180">
              <template #default="{ row }">
                <div class="ip-cell">
                  <span class="cell-ip">{{ row.ip || '-' }}</span>
                  <span v-if="row.region" class="cell-region" :title="row.region">{{ row.region }}</span>
                </div>
              </template>
            </el-table-column>
            <el-table-column label="方法" :width="isMobile ? 64 : 80">
              <template #default="{ row }">
                <span :class="['method-tag', `method-${(row.method || '').toLowerCase()}`]">{{ row.method }}</span>
              </template>
            </el-table-column>
            <el-table-column label="状态码" :width="isMobile ? 64 : 80">
              <template #default="{ row }">
                <span :class="['status-tag', `status-${Math.floor(row.status / 100)}xx`]">{{ row.status }}</span>
              </template>
            </el-table-column>
            <el-table-column label="路径" min-width="200">
              <template #default="{ row }">
                <span class="cell-path" :title="row.path">{{ row.path }}</span>
              </template>
            </el-table-column>
            <el-table-column v-if="!isMobile" label="大小" width="90">
              <template #default="{ row }">{{ formatBytes(row.bytes) }}</template>
            </el-table-column>
            <el-table-column v-if="!isMobile" label="UA" min-width="200" show-overflow-tooltip>
              <template #default="{ row }">{{ row.user_agent }}</template>
            </el-table-column>
          </el-table>
        </div>

        <!-- 2. 访问统计 -->
        <div v-show="activeMenu === 'stats'" class="logs-panel stats-panel">
          <div v-if="analyze?.stats" class="stats-grid">
            <div class="stat-card stat-primary">
              <div class="stat-label">总请求数</div>
              <div class="stat-value">{{ analyze.stats.total }}</div>
            </div>
            <div class="stat-card stat-info">
              <div class="stat-label">独立 IP</div>
              <div class="stat-value">{{ analyze.stats.unique_ips }}</div>
            </div>
            <div class="stat-card" :class="statusCardClass(analyze.stats.status_dist['5xx'])">
              <div class="stat-label">5xx 错误</div>
              <div class="stat-value">{{ analyze.stats.status_dist['5xx'] || 0 }}</div>
            </div>
            <div class="stat-card" :class="statusCardClass(analyze.stats.status_dist['4xx'], 'warning')">
              <div class="stat-label">4xx 错误</div>
              <div class="stat-value">{{ analyze.stats.status_dist['4xx'] || 0 }}</div>
            </div>

            <div class="stat-block">
              <div class="stat-block-title">状态码分布</div>
              <div class="bar-list">
                <div v-for="(v, k) in analyze.stats.status_dist" :key="k" class="bar-item">
                  <span class="bar-label" :class="`status-${k}`">{{ k }}</span>
                  <div class="bar-track">
                    <div :class="['bar-fill', `status-fill-${k}`]" :style="{ width: barWidth(v, analyze.stats.total) }"></div>
                  </div>
                  <span class="bar-value">{{ v }}</span>
                </div>
              </div>
            </div>

            <div class="stat-block">
              <div class="stat-block-title">请求方法</div>
              <div class="chip-list">
                <el-tag v-for="(v, k) in analyze.stats.method_dist" :key="k" effect="plain" :type="methodTagType(k)">
                  {{ k }} · {{ v }}
                </el-tag>
              </div>
            </div>

            <div class="stat-block">
              <div class="stat-block-title">24 小时分布</div>
              <div class="hourly-chart">
                <div v-for="h in 24" :key="h - 1" class="hourly-col" :title="`${h-1}时: ${analyze.stats.hourly_dist[h-1] || 0} 次`">
                  <div class="hourly-bar" :style="{ height: hourlyHeight(h-1) }"></div>
                  <div class="hourly-label">{{ (h-1) % 6 === 0 ? (h-1) : '' }}</div>
                </div>
              </div>
            </div>

            <div class="stat-block">
              <div class="stat-block-title">Top 10 路径</div>
              <el-table :data="analyze.stats.top_paths" size="small" :show-header="true" max-height="220">
                <el-table-column type="index" label="#" width="50" />
                <el-table-column prop="path" label="路径" min-width="200" show-overflow-tooltip />
                <el-table-column prop="count" label="次数" width="80" align="right" />
              </el-table>
            </div>

            <div class="stat-block">
              <div class="stat-block-title">Top 10 IP（含归属地）</div>
              <el-table :data="analyze.stats.top_ips" size="small" max-height="220">
                <el-table-column type="index" label="#" width="50" />
                <el-table-column prop="ip" label="IP" width="150" />
                <el-table-column prop="region" label="归属地" min-width="160" />
                <el-table-column prop="count" label="次数" width="80" align="right" />
                <el-table-column label="操作" width="160" align="center">
                  <template #default="{ row }">
                    <el-button size="small" type="primary" link @click="openBlockIpDialog(row)">拉黑</el-button>
                    <el-button size="small" type="success" link @click="allowIp(row)">放行</el-button>
                  </template>
                </el-table-column>
              </el-table>
            </div>
          </div>
          <el-empty v-else description="暂无分析数据" />
        </div>

        <!-- 3. 风险分析 -->
        <div v-show="activeMenu === 'risk'" class="logs-panel">
          <div v-if="analyze?.risks" class="risk-panel">
            <div class="logs-toolbar">
              <el-radio-group v-model="riskFilter" size="small">
                <el-radio-button value="all">全部 ({{ analyze.risks.length }})</el-radio-button>
                <el-radio-button value="high">高危 ({{ countRisk('high') }})</el-radio-button>
                <el-radio-button value="medium">中危 ({{ countRisk('medium') }})</el-radio-button>
                <el-radio-button value="low">低危 ({{ countRisk('low') }})</el-radio-button>
              </el-radio-group>
            </div>
            <el-table
              :data="filteredRisks"
              stripe
              size="small"
              :max-height="isMobile ? 'calc(100vh - 230px)' : 'calc(75vh - 40px)'"
              :empty-text="analyze.risks.length === 0 ? '未发现风险' : '无匹配结果'"
            >
              <el-table-column label="严重程度" width="100">
                <template #default="{ row }">
                  <el-tag :type="severityTagType(row.severity)" size="small" effect="dark">{{ severityLabel(row.severity) }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="时间" width="170">
                <template #default="{ row }">{{ formatNginxTime(row.time) }}</template>
              </el-table-column>
              <el-table-column prop="ip" label="IP" width="180">
                <template #default="{ row }">
                  <div class="ip-cell">
                    <span class="cell-ip">{{ row.ip || '-' }}</span>
                    <span v-if="row.region" class="cell-region" :title="row.region">{{ row.region }}</span>
                  </div>
                </template>
              </el-table-column>
              <el-table-column prop="type" label="风险类型" width="130">
                <template #default="{ row }">{{ riskTypeLabel(row.type) }}</template>
              </el-table-column>
              <el-table-column prop="desc" label="描述" min-width="200" />
              <el-table-column label="请求" min-width="280">
                <template #default="{ row }">
                  <span class="cell-method">{{ row.method }}</span>
                  <span class="cell-path" :title="row.path">{{ row.path }}</span>
                  <el-tag v-if="row.status" :class="['status-tag', `status-${Math.floor(row.status/100)}xx`]" size="small" effect="plain">{{ row.status }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="UA" min-width="180" show-overflow-tooltip>
                <template #default="{ row }">{{ row.user_agent }}</template>
              </el-table-column>
            </el-table>
          </div>
          <el-empty v-else description="暂无风险数据" />
        </div>
      </div>
    </div>

    <!-- 拉黑 IP 弹窗 -->
    <el-dialog
      v-model="blockDialog.visible"
      title="拉黑 IP"
      width="440px"
      append-to-body
      :close-on-click-modal="false"
    >
      <div class="block-ip-target">
        <div class="block-ip-title">目标 IP：<b>{{ blockDialog.ip }}</b></div>
        <div class="block-ip-meta" v-if="blockDialog.region || blockDialog.count">
          归属地: {{ blockDialog.region || '-' }}，请求 {{ blockDialog.count }} 次
        </div>
      </div>

      <el-form label-width="84px" size="default">
        <el-form-item label="拉黑范围">
          <el-radio-group v-model="blockDialog.scope">
            <el-radio value="site">仅本站点</el-radio>
            <el-radio value="global">全服务器</el-radio>
          </el-radio-group>
          <div class="block-ip-tip" v-if="blockDialog.scope === 'site'">
            仅阻止此 IP 访问当前站点（{{ site.name || '' }}），不影响其他站点
          </div>
        </el-form-item>
        <el-form-item label="拉黑时长">
          <el-radio-group v-model="blockDialog.duration">
            <el-radio value="1h">1 小时</el-radio>
            <el-radio value="1d">1 天</el-radio>
            <el-radio value="7d">7 天</el-radio>
            <el-radio value="perm">永久</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="备注">
          <el-input
            v-model="blockDialog.remark"
            type="textarea"
            :rows="2"
            maxlength="200"
            show-word-limit
            placeholder="可选"
          />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="blockDialog.visible = false">取消</el-button>
        <el-button type="danger" :loading="blockDialog.submitting" @click="submitBlockIp">确认拉黑</el-button>
      </template>
    </el-dialog>
  </el-dialog>
</template>

<script setup>
import { ref, computed, defineExpose } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh, Document, DataAnalysis, Warning } from '@element-plus/icons-vue'
import request from '../utils/request'
import { formatBytes as _fmtBytes } from '../utils/format'

const visible = ref(false)
const loading = ref(false)
const site = ref({ name: '' })
const activeMenu = ref('raw')

// 拉黑 IP 弹窗
const blockDialog = ref({
  visible: false,
  submitting: false,
  ip: '',
  region: '',
  count: 0,
  scope: 'site', // site: 仅本站点； global: 全服务器
  duration: '1d', // 1h / 1d / 7d / perm
  remark: '',
})

// 原始日志
const result = ref({ entries: [], total_lines: 0, truncated: false, has_file: false })
const rawFilter = ref('')
const rawSort = ref({ prop: '', order: '' }) // 表头排序状态：未排序时按时间倒序展示

// 分析
const analyze = ref(null)
const riskFilter = ref('all')

const menus = [
  { key: 'raw', title: '原始日志', desc: '结构化表格查看', icon: Document },
  { key: 'stats', title: '访问统计', desc: '请求/IP/状态码/时段', icon: DataAnalysis },
  { key: 'risk', title: '风险分析', desc: '扫描/攻击/异常识别', icon: Warning }
]

const filteredEntries = computed(() => {
  const entries = (result.value.entries || []).slice()
  // 排序：默认按时间倒序；点表头切换 asc/desc；再次点同一列回到倒序
  const prop = rawSort.value.prop
  const order = rawSort.value.order
  if (prop === 'time' && order === 'ascending') {
    entries.sort((a, b) => (a.time_unix || 0) - (b.time_unix || 0))
  } else {
    // 默认 / 降序：最新在最上面
    entries.sort((a, b) => (b.time_unix || 0) - (a.time_unix || 0))
  }
  const q = (rawFilter.value || '').trim().toLowerCase()
  if (!q) return entries
  return entries.filter((e) =>
    (e.ip && e.ip.includes(q)) ||
    (e.path && e.path.toLowerCase().includes(q)) ||
    (String(e.status) === q) ||
    (e.method && e.method.toLowerCase() === q)
  )
})

const filteredRisks = computed(() => {
  if (!analyze.value?.risks) return []
  if (riskFilter.value === 'all') return analyze.value.risks
  return analyze.value.risks.filter((r) => r.severity === riskFilter.value)
})

const rowId = ref(0)

// 移动端检测：< 768px 切换为顶部 Tab + 全宽布局
const isMobile = ref(typeof window !== 'undefined' && window.innerWidth < 768)
function updateIsMobile() {
  isMobile.value = window.innerWidth < 768
}
if (typeof window !== 'undefined') {
  window.addEventListener('resize', updateIsMobile)
}

function open(row) {
  rowId.value = row.id
  site.value = { name: row.name }
  visible.value = true
  activeMenu.value = 'raw'
  rawFilter.value = ''
  riskFilter.value = 'all'
  result.value = { entries: [], total_lines: 0, truncated: false, has_file: false }
  analyze.value = null
  // 秒开：打开弹窗只加载原始日志（倒读末尾 1000 行）；分析数据等切到对应面板再懒加载
  loadLogs()
}

function switchMenu(key) {
  activeMenu.value = key
  if (key === 'stats' || key === 'risk') {
    if (!analyze.value) loadAnalyze()
  }
}

// 刷新当前面板
async function loadAll() {
  if (activeMenu.value === 'raw') {
    await loadLogs()
  } else {
    await loadAnalyze()
  }
}

async function loadLogs() {
  loading.value = true
  try {
    const res = await request.get('/site/logs', { params: { id: rowId.value, lines: 1000 } })
    result.value = res.data || { entries: [], total_lines: 0, truncated: false, has_file: false }
  } catch (e) {
    /* interceptor handles */
  } finally {
    loading.value = false
  }
}

async function loadAnalyze() {
  loading.value = true
  try {
    const res = await request.get('/site/logs/analyze', { params: { id: rowId.value, lines: 2000 } })
    analyze.value = res.data
  } catch (e) {
    /* interceptor handles */
  } finally {
    loading.value = false
  }
}

// 工具函数
function formatBytes(n) {
  return _fmtBytes(n)
}

// 打开拉黑 IP 弹窗（与 WAF 拉黑 IP 弹窗一致）
function openBlockIpDialog(row) {
  blockDialog.value = {
    visible: true,
    submitting: false,
    ip: row.ip,
    region: row.region || '',
    count: row.count || 0,
    scope: 'site',
    duration: '1d',
    remark: '',
  }
}

const DURATION_MAP = { '1h': 3600, '1d': 86400, '7d': 7 * 86400, perm: 0 }

async function submitBlockIp() {
  const d = blockDialog.value
  if (!d.ip) { ElMessage.warning('IP 为空'); return }
  d.submitting = true
  try {
    if (d.scope === 'site') {
      // 站点级拉黑：POST /site/blockip/add
      const expire = DURATION_MAP[d.duration] || 0
      await request.post('/site/blockip/add', {
        site_id: rowId.value,
        ip: d.ip,
        expire_seconds: expire,
        remark: d.remark,
      })
    } else {
      // 全服务器拉黑：WAF 全局 IP 规则
      await request.post('/security/rule/add', {
        type: 'ip',
        action: 'block',
        content: d.ip,
        direction: 'in',
        remark: d.remark || `从访问日志拉黑：${d.region || ''} 请求 ${d.count} 次`,
      })
    }
    ElMessage.success('IP 拉黑成功')
    d.visible = false
  } catch (e) {
    ElMessage.error(e.message || '拉黑失败')
  } finally {
    d.submitting = false
  }
}

// 放行：删除该 IP 的拉黑记录（站点级 + 全局）
async function allowIp(row) {
  try {
    await ElMessageBox.confirm(
      `确认将 IP ${row.ip}（${row.region || '-'}）从黑名单中移除？`,
      '放行 IP',
      { confirmButtonText: '确认放行', cancelButtonText: '取消', type: 'warning' }
    )
  } catch { return }
  try {
    // 1) 站点级黑名单：先查再删
    const list = await request.get('/site/blockip', { params: { id: rowId.value } })
    const ips = (list.data && list.data.ips) || []
    const rec = ips.find((x) => x.ip === row.ip)
    if (rec) {
      await request.post('/site/blockip/delete', { site_id: rowId.value, id: rec.id })
    }
    // 2) 全局黑名单：按 IP 内容删
    const rules = await request.get('/security/rules', { params: { type: 'ip' } })
    const rs = (rules.data && rules.data.rules) || []
    const globals = rs.filter((r) => r.action === 'block' && (r.content || '').split('\n').includes(row.ip))
    for (const r of globals) {
      await request.post('/security/rule/delete', { id: r.id })
    }
    ElMessage.success('已放行')
  } catch (e) {
    ElMessage.error(e.message || '放行失败')
  }
}

// 把访问日志原始时间格式 "25/Aug/2026:01:46:45 +0800" 转成可读格式 "2026-08-25 01:46:45"
// （nginx 与 apache 的 combined 日志时间格式一致，通用解析）
function formatNginxTime(s) {
  if (!s) return '-'
  // s 形如 "25/Aug/2026:01:46:45 +0800" 或 "25/Aug/2026:01:46:45"
  const m = String(s).match(/^(\d{2})\/(\w{3})\/(\d{4}):(\d{2}:\d{2}:\d{2})/)
  if (!m) return s
  const months = { Jan: '01', Feb: '02', Mar: '03', Apr: '04', May: '05', Jun: '06', Jul: '07', Aug: '08', Sep: '09', Oct: '10', Nov: '11', Dec: '12' }
  return `${m[3]}-${months[m[2]] || m[2]}-${m[1]} ${m[4]}`
}

function barWidth(v, total) {
  if (!total) return '0%'
  return Math.max(2, (v / total) * 100) + '%'
}

function statusCardClass(v, base) {
  if (base === 'warning') return v > 0 ? 'stat-warning' : 'stat-muted'
  return v > 0 ? 'stat-danger' : 'stat-muted'
}

function methodTagType(m) {
  if (m === 'GET') return ''
  if (m === 'POST') return 'success'
  if (m === 'PUT' || m === 'PATCH') return 'warning'
  if (m === 'DELETE') return 'danger'
  return 'info'
}

function hourlyHeight(h) {
  const v = analyze.value?.stats?.hourly_dist?.[h] || 0
  const max = Math.max(1, ...Object.values(analyze.value?.stats?.hourly_dist || { 0: 1 }))
  return (v / max) * 100 + '%'
}

function severityTagType(s) {
  if (s === 'high') return 'danger'
  if (s === 'medium') return 'warning'
  return 'info'
}
function severityLabel(s) {
  return s === 'high' ? '高危' : s === 'medium' ? '中危' : '低危'
}
function riskTypeLabel(t) {
  const map = {
    http_error: 'HTTP 错误',
    scanner: '扫描器',
    info_leak: '信息泄露',
    webshell: 'WebShell',
    sqli: 'SQL 注入',
    xss: 'XSS',
    traversal: '路径遍历',
    lfi: '本地文件包含',
    bot: '脚本请求',
    scraper: '爬虫'
  }
  return map[t] || t
}
function countRisk(s) {
  return analyze.value?.risks?.filter((r) => r.severity === s).length || 0
}

defineExpose({ open })
</script>

<style scoped>
.site-logs-dialog :deep(.el-dialog__body) { padding: 12px 16px; }
.site-logs-dialog :deep(.el-dialog) { max-width: 1200px; }
/* 自定义 header：标题在左，"刷新"按钮紧贴关闭按钮左边 */
.logs-dialog-header {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  padding-right: 8px;
}
.logs-dialog-title {
  flex: 1;
  font-size: 16px;
  font-weight: 600;
  color: #303133;
}
.logs-layout {
  display: flex;
  gap: 12px;
  min-height: 75vh;
}
.logs-menu {
  width: 200px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
  border-right: 1px solid #ebeef5;
  padding-right: 12px;
  align-self: flex-start;       /* 不拉满父容器高度，避免底部留大空白 */
}
.logs-menu-item {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 10px 12px;
  border-radius: 8px;
  cursor: pointer;
  transition: background 0.15s;
  border: 1px solid transparent;
}
.logs-menu-item:hover { background: #f5f7fa; }
.logs-menu-item.active {
  background: #ecf5ff;
  border-color: #409eff;
}
.logs-menu-icon { font-size: 18px; color: #409eff; margin-top: 2px; }
.logs-menu-content { flex: 1; }
.logs-menu-title { font-size: 14px; font-weight: 600; color: #303133; }
.logs-menu-desc { font-size: 12px; color: #909399; margin-top: 2px; }
.logs-menu-footer { margin-top: auto; padding-top: 8px; border-top: 1px dashed #ebeef5; }
.logs-content {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  /* 固定可视高度，与左侧菜单等高，三个 panel 切换时视觉一致 */
  height: calc(75vh - 40px);
  overflow: hidden;
}
.logs-panel {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
/* 访问统计 panel 长内容在内部滚动 */
.logs-panel.stats-panel { overflow-y: auto; padding-right: 4px; }
.logs-panel.stats-panel::-webkit-scrollbar { width: 6px; }
.logs-panel.stats-panel::-webkit-scrollbar-thumb { background: #c0c4cc; border-radius: 3px; }
.logs-panel.stats-panel::-webkit-scrollbar-thumb:hover { background: #909399; }
.logs-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
}
.logs-stat { color: #606266; font-size: 13px; display: flex; align-items: center; gap: 8px; }

/* 表格内单元格 */
.cell-ip { font-family: Consolas, Menlo, monospace; font-size: 12px; }
.ip-cell { display: flex; flex-direction: column; line-height: 1.3; gap: 1px; }
.cell-region { font-size: 11px; color: #909399; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.cell-path {
  font-family: Consolas, Menlo, monospace;
  font-size: 12px;
  word-break: break-all;
}
.cell-method {
  font-family: Consolas, Menlo, monospace;
  font-size: 11px;
  padding: 1px 5px;
  border-radius: 3px;
  background: #f0f2f5;
  margin-right: 4px;
}

/* 状态码/方法标签 */
.method-tag {
  display: inline-block;
  padding: 1px 6px;
  border-radius: 3px;
  font-size: 11px;
  font-weight: 600;
  font-family: Consolas, Menlo, monospace;
}
.method-get { background: #e1f3d8; color: #67c23a; }
.method-post { background: #d9ecff; color: #409eff; }
.method-put, .method-patch, .method-delete { background: #faecd8; color: #e6a23c; }

/* 状态码 tag：去除 el-tag plain 的描边（用 box-shadow 模拟，强制覆盖为透明） */
.status-tag {
  display: inline-block;
  padding: 1px 6px;
  border-radius: 3px;
  border: 0 !important;
  box-shadow: none !important; /* 覆盖 el-tag 的 inset 1px box-shadow 描边 */
  font-size: 11px;
  font-weight: 600;
  font-family: Consolas, Menlo, monospace;
}
.status-2xx { background: #e1f3d8; color: #67c23a; }
.status-3xx { background: #d9ecff; color: #409eff; }
.status-4xx { background: #faecd8; color: #e6a23c; }
.status-5xx { background: #fde2e2; color: #f56c6c; }

/* 统计卡片 */
.stats-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 10px;
}
.stat-card {
  background: #fff;
  border-radius: 8px;
  padding: 14px;
  border: 1px solid #ebeef5;
}
.stat-primary { background: #ecf5ff; border-color: #b3d8ff; }
.stat-info { background: #f0f9eb; border-color: #c2e7b0; }
.stat-warning { background: #fdf6ec; border-color: #f5dab1; }
.stat-danger { background: #fef0f0; border-color: #fbc4c4; }
.stat-muted { background: #f5f7fa; }
.stat-label { font-size: 12px; color: #909399; }
.stat-value { font-size: 24px; font-weight: 700; color: #303133; margin-top: 4px; }

.stat-block {
  grid-column: span 2;
  background: #fff;
  border-radius: 8px;
  padding: 12px 14px;
  border: 1px solid #ebeef5;
}
.stat-block-title { font-size: 14px; font-weight: 600; margin-bottom: 10px; color: #303133; }

/* 状态码条形图 */
.bar-list { display: flex; flex-direction: column; gap: 6px; }
.bar-item { display: flex; align-items: center; gap: 8px; font-size: 12px; }
.bar-label {
  width: 40px;
  text-align: center;
  padding: 2px 0;
  border-radius: 3px;
  font-weight: 600;
  font-family: Consolas, monospace;
}
.bar-label.status-2xx { background: #e1f3d8; color: #67c23a; }
.bar-label.status-3xx { background: #d9ecff; color: #409eff; }
.bar-label.status-4xx { background: #faecd8; color: #e6a23c; }
.bar-label.status-5xx { background: #fde2e2; color: #f56c6c; }
.bar-track { flex: 1; height: 14px; background: #f5f7fa; border-radius: 3px; overflow: hidden; }
.bar-fill { height: 100%; border-radius: 3px; transition: width 0.3s; }
.bar-fill.status-fill-2xx { background: #67c23a; }
.bar-fill.status-fill-3xx { background: #409eff; }
.bar-fill.status-fill-4xx { background: #e6a23c; }
.bar-fill.status-fill-5xx { background: #f56c6c; }
.bar-value { width: 50px; text-align: right; font-family: Consolas, monospace; color: #606266; }

.chip-list { display: flex; flex-wrap: wrap; gap: 6px; }

/* 24 小时柱状图 */
.hourly-chart {
  display: flex;
  align-items: flex-end;
  gap: 2px;
  height: 100px;
  padding: 4px 0;
}
.hourly-col { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: flex-end; height: 100%; position: relative; }
.hourly-bar {
  width: 100%;
  background: linear-gradient(180deg, #409eff, #66b1ff);
  border-radius: 2px 2px 0 0;
  min-height: 1px;
  transition: height 0.3s;
}
.hourly-label { font-size: 10px; color: #909399; margin-top: 2px; font-family: Consolas, monospace; }
.hourly-col:hover .hourly-bar { background: linear-gradient(180deg, #337ecc, #409eff); }

/* 风险表格 */
.risk-panel { display: flex; flex-direction: column; flex: 1; min-height: 0; }

/* 拉黑 IP 弹窗 */
.block-ip-target {
  background: #fdf6ec;
  border: 1px solid #faecd8;
  border-radius: 4px;
  padding: 10px 12px;
  margin-bottom: 14px;
  font-size: 13px;
  color: #606266;
}
.block-ip-title b { color: #f56c6c; font-family: Consolas, Menlo, monospace; }
.block-ip-meta { margin-top: 4px; color: #909399; font-size: 12px; }
.block-ip-tip { color: #909399; font-size: 12px; margin-top: 6px; line-height: 1.5; }

/* ---------- 移动端适配（< 768px）---------- */
@media (max-width: 767px) {
  .site-logs-dialog :deep(.el-dialog) {
    width: 96vw !important;
    max-width: 100% !important;
    margin: 0 !important;
  }
  .site-logs-dialog :deep(.el-dialog__body) {
    padding: 8px 8px !important;
  }
  .site-logs-dialog :deep(.el-dialog__header) {
    padding: 12px 12px !important;
  }
  .logs-layout {
    flex-direction: column;
    min-height: 80vh;
  }
  .logs-menu {
    width: 100% !important;
    flex-direction: row !important;
    border-right: none !important;
    border-bottom: 1px solid #ebeef5;
    padding: 0 0 8px 0;
    gap: 4px;
    overflow-x: auto;
  }
  .logs-menu-mobile {
    flex-direction: row !important;
  }
  .logs-menu-item {
    flex: 1;
    min-width: 0;
    flex-direction: column;
    align-items: center;
    padding: 8px 4px;
    border: 1px solid #ebeef5;
    background: #fff;
  }
  .logs-menu-item.active {
    background: #ecf5ff;
    border-color: #409eff;
  }
  .logs-menu-content { text-align: center; }
  .logs-menu-title { font-size: 12px; }
  .logs-menu-desc { display: none; }
  .logs-menu-icon { font-size: 16px; }
  .logs-content { width: 100%; }
  .logs-toolbar {
    flex-wrap: wrap;
    gap: 6px;
  }
  .logs-stat { font-size: 12px; width: 100%; }

  /* 统计面板：单列 */
  .stats-grid {
    grid-template-columns: 1fr !important;
  }
  .stat-block {
    grid-column: span 1 !important;
  }
}
</style>
