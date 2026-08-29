<template>
  <div class="logs-page">
    <el-tabs v-model="activeTab">
      <!-- 操作日志：前端会话（JWT）触发的操作 -->
      <el-tab-pane label="操作日志" name="oplog">
        <div class="toolbar">
          <el-button @click="loadOplog">刷新</el-button>
          <el-button type="primary" @click="exportOplog">导出 CSV</el-button>
          <el-button type="danger" :disabled="!oplogTotal" @click="() => clearOplog('login')">清空</el-button>
        </div>
        <Skeleton v-if="loadingO" type="table" :rows="8" :columns="[{width:'180px'},{width:'130px'},{flex:1},{width:'140px'},{width:'90px'}]" />
        <el-table v-else :data="oplogs" stripe>
          <el-table-column label="时间" width="180">
            <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
          </el-table-column>
          <el-table-column label="模块" width="130">
            <template #default="{ row }">
              <el-tag size="small">{{ moduleName(row.action.split('.')[0]) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="detail" label="操作" min-width="220" show-overflow-tooltip />
          <el-table-column prop="ip" label="IP" width="140" />
          <el-table-column label="结果" width="90" align="center">
            <template #default="{ row }">
              <el-tag :type="row.status === 'success' ? 'success' : 'danger'" size="small">
                {{ row.status === 'success' ? '成功' : '失败' }}
              </el-tag>
            </template>
          </el-table-column>
        </el-table>
        <div class="pager">
          <el-pagination
            layout="prev, pager, next, total"
            :total="oplogTotal"
            :page-size="pageSize"
            v-model:current-page="oplogPage"
            @current-change="loadOplog"
          />
        </div>
      </el-tab-pane>

      <!-- API 日志：通过 API token 触发的操作 -->
      <el-tab-pane label="API 日志" name="api">
        <div class="toolbar">
          <el-button @click="loadApiLog">刷新</el-button>
          <el-button type="danger" :disabled="!apiTotal" @click="() => clearOplog('api')">清空</el-button>
          <span class="hint">通过 API 令牌（开放 API）调用的全部操作</span>
        </div>
        <el-table :data="apiLogs" v-loading="loadingApi" stripe>
          <el-table-column label="时间" width="180">
            <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
          </el-table-column>
          <el-table-column label="模块" width="140">
            <template #default="{ row }">
              <el-tag size="small" type="primary">{{ moduleName(row.action.replace(/^api\./, '').split('.')[0]) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="detail" label="操作" min-width="220" show-overflow-tooltip />
          <el-table-column prop="ip" label="IP" width="140" />
          <el-table-column label="结果" width="90" align="center">
            <template #default="{ row }">
              <el-tag :type="row.status === 'success' ? 'success' : 'danger'" size="small">
                {{ row.status === 'success' ? '成功' : '失败' }}
              </el-tag>
            </template>
          </el-table-column>
        </el-table>
        <div class="pager">
          <el-pagination
            layout="prev, pager, next, total"
            :total="apiTotal"
            :page-size="pageSize"
            v-model:current-page="apiPage"
            @current-change="loadApiLog"
          />
        </div>
      </el-tab-pane>

      <!-- MCP 日志：通过 MCP token / AI 工具触发的操作 -->
      <el-tab-pane label="MCP 日志" name="mcp">
        <div class="toolbar">
          <el-button @click="loadMcpLog">刷新</el-button>
          <el-button type="danger" :disabled="!mcpTotal" @click="() => clearOplog('mcp')">清空</el-button>
          <span class="hint">通过 MCP 令牌（AI 工具调用面板）触发的全部操作</span>
        </div>
        <el-table :data="mcpLogs" v-loading="loadingMcp" stripe>
          <el-table-column label="时间" width="180">
            <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
          </el-table-column>
          <el-table-column label="工具/模块" width="160">
            <template #default="{ row }">
              <el-tag size="small" type="success">{{ moduleName(row.action.replace(/^mcp\./, '').split('.')[0]) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="detail" label="操作" min-width="240" show-overflow-tooltip />
          <el-table-column prop="ip" label="来源 IP" width="140" />
          <el-table-column label="结果" width="90" align="center">
            <template #default="{ row }">
              <el-tag :type="row.status === 'success' ? 'success' : 'danger'" size="small">
                {{ row.status === 'success' ? '成功' : '失败' }}
              </el-tag>
            </template>
          </el-table-column>
        </el-table>
        <div class="pager">
          <el-pagination
            layout="prev, pager, next, total"
            :total="mcpTotal"
            :page-size="pageSize"
            v-model:current-page="mcpPage"
            @current-change="loadMcpLog"
          />
        </div>
      </el-tab-pane>

      <!-- 面板运行日志 -->
      <el-tab-pane label="面板日志" name="system">
        <div class="toolbar">
          <el-button @click="loadSystemLog">刷新</el-button>
          <el-select v-model="systemLines" size="default" style="width: 120px" @change="loadSystemLog">
            <el-option label="最近 100 行" :value="100" />
            <el-option label="最近 200 行" :value="200" />
            <el-option label="最近 500 行" :value="500" />
            <el-option label="最近 1000 行" :value="1000" />
          </el-select>
          <el-input
            v-model="systemKeyword"
            placeholder="按消息 / IP / 级别过滤"
            clearable
            style="width: 260px"
            @input="filterSystemLog"
          >
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
          <el-button :disabled="!systemTotal" type="danger" @click="clearSystemLog">清空日志文件</el-button>
          <span class="hint">共 {{ systemTotal }} 条，{{ systemFiltered.length }} 条匹配</span>
        </div>
        <el-table :data="systemFiltered" v-loading="loadingSys" stripe max-height="560" empty-text="暂无日志">
          <el-table-column label="时间" width="170">
            <template #default="{ row }">{{ row.time || '-' }}</template>
          </el-table-column>
          <el-table-column label="级别" width="90" align="center">
            <template #default="{ row }">
              <el-tag :type="levelType(row.level)" size="small">{{ row.level || '-' }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="IP" width="140">
            <template #default="{ row }">
              <span v-if="row.ip" class="ip-cell">{{ row.ip }}</span>
              <span v-else class="muted">-</span>
            </template>
          </el-table-column>
          <el-table-column label="消息" min-width="320" show-overflow-tooltip>
            <template #default="{ row }">
              <span :class="['msg-cell', levelClass(row.level)]">{{ row.message }}</span>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <!-- 应用安装日志：按应用名查看安装/卸载历史日志 -->
      <el-tab-pane label="应用安装日志" name="app">
        <div class="toolbar">
          <el-button @click="loadAppLogList">刷新</el-button>
          <el-select v-model="appLogKey" placeholder="选择应用" style="width: 240px" @change="loadAppLog">
            <el-option v-for="k in appLogList" :key="k" :label="k" :value="k" />
          </el-select>
          <el-input v-model="appLogKeyword" placeholder="关键字过滤（按日志内容）" clearable
 style="width: 260px" @input="filterAppLog">
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
          <el-radio-group v-model="appLogLines" size="default" @change="loadAppLog">
            <el-radio-button :value="100">100</el-radio-button>
            <el-radio-button :value="200">200</el-radio-button>
            <el-radio-button :value="500">500</el-radio-button>
            <el-radio-button :value="1000">1000</el-radio-button>
          </el-radio-group>
          <span class="hint">共 {{ appLogTotal }} 行，{{ appLogFiltered.length }} 行匹配</span>
        </div>
        <pre v-if="appLogFiltered.length" class="log-box">{{ appLogFiltered.join('\n') }}</pre>
        <div v-else style="text-align: center; color: #c0c4cc; padding: 40px 0; font-size: 13px">
          {{ appLogKey ? '暂无匹配日志' : '请选择左侧应用' }}
        </div>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { ref, watch, onMounted, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import request from '../utils/request'
import { fmtTimeStamp } from '../utils/format'

// 日志页 Tab 持久化（localStorage）：下次进入日志页停留在上次选中的 Tab
const LOGS_TAB_KEY = 'lp_logs_active_tab'
function readLogsTab() {
  try {
    const v = localStorage.getItem(LOGS_TAB_KEY)
    return v || 'oplog'
  } catch (e) { return 'oplog' }
}
const activeTab = ref(readLogsTab())

const oplogs = ref([])
const oplogTotal = ref(0)
const oplogPage = ref(1)

const apiLogs = ref([])
const apiTotal = ref(0)
const apiPage = ref(1)

const mcpLogs = ref([])
const mcpTotal = ref(0)
const mcpPage = ref(1)

const pageSize = 20
const loadingO = ref(true)
const loadingApi = ref(true)
const loadingMcp = ref(true)
const loadingSys = ref(true)
const systemLines = ref(200)
const systemLogs = ref([])
const systemTotal = ref(0)
const systemKeyword = ref('')

// 应用安装日志
const appLogList = ref([])
const appLogKey = ref('')
const appLogLines = ref(200)
const appLogText = ref('')
const appLogKeyword = ref('')
const appLogTotal = ref(0)
const appLogFiltered = computed(() => {
  const kw = appLogKeyword.value.trim().toLowerCase()
  if (!kw) return appLogText.value
  return appLogText.value.filter((line) => line.toLowerCase().includes(kw))
})
async function loadAppLogList() {
  const res = await request.get('/logs/install-list')
  appLogList.value = res.data || []
  // 默认选第一个
  if (!appLogKey.value && appLogList.value.length) {
    appLogKey.value = appLogList.value[0]
    loadAppLog()
  }
}
async function loadAppLog() {
  if (!appLogKey.value) return
  const res = await request.get('/apps/log', { params: { key: appLogKey.value, lines: appLogLines.value } })
  const text = res.data?.log || ''
  appLogText.value = text.split('\n')
  appLogTotal.value = appLogText.value.length
}
function filterAppLog() { /* 触发 computed */ }

const moduleNames = {
  auth: '认证', file: '文件', app: '应用', apps: '应用', site: '网站', cron: '计划任务',
  database: '数据库', ftp: 'FTP', docker: '容器', settings: '设置',
  waf: 'WAF', firewall: '防火墙', monitor: '监控', alert: '告警', backup: '备份',
  security: '安全', system: '系统', api_token: 'API令牌', op: '操作'
}

function moduleName(m) { return moduleNames[m] || m }

function fmtTime(t) {
  return fmtTimeStamp(t)
}

// Tab 切换时加载对应数据
watch(activeTab, (v) => {
  if (v === 'oplog' && !oplogs.value.length) loadOplog()
  else if (v === 'api' && !apiLogs.value.length) loadApiLog()
  else if (v === 'mcp' && !mcpLogs.value.length) loadMcpLog()
  else if (v === 'system' && !systemLogs.value.length) loadSystemLog()
  else if (v === 'app' && !appLogList.value.length) loadAppLogList()
})

// 面板日志过滤（按关键字搜索消息 / IP / 级别）
const systemFiltered = computed(() => {
  const kw = systemKeyword.value.trim().toLowerCase()
  if (!kw) return systemLogs.value
  return systemLogs.value.filter((row) =>
    (row.message || '').toLowerCase().includes(kw) ||
    (row.ip || '').toLowerCase().includes(kw) ||
    (row.level || '').toLowerCase().includes(kw)
  )
})
function filterSystemLog() { /* 触发 computed */ }
function levelType(l) {
  return ({ ERROR: 'danger', WARN: 'warning', DEBUG: 'info' })[l] || 'success'
}
function levelClass(l) {
  return ({ ERROR: 'msg-error', WARN: 'msg-warn' })[l] || ''
}

async function loadOplog() {
  loadingO.value = true
  try {
    const res = await request.get('/oplog/list', { params: { page: oplogPage.value, page_size: pageSize, source: 'login' } })
    oplogs.value = res.data.list || []
    oplogTotal.value = res.data.total || 0
  } finally { loadingO.value = false }
}
async function loadApiLog() {
  loadingApi.value = true
  try {
    const res = await request.get('/oplog/list', { params: { page: apiPage.value, page_size: pageSize, source: 'api' } })
    apiLogs.value = res.data.list || []
    apiTotal.value = res.data.total || 0
  } finally { loadingApi.value = false }
}
async function loadMcpLog() {
  loadingMcp.value = true
  try {
    const res = await request.get('/oplog/list', { params: { page: mcpPage.value, page_size: pageSize, source: 'mcp' } })
    mcpLogs.value = res.data.list || []
    mcpTotal.value = res.data.total || 0
  } finally { loadingMcp.value = false }
}

async function loadSystemLog() {
  loadingSys.value = true
  try {
    const res = await request.get('/logs/system', { params: { lines: systemLines.value } })
    systemLogs.value = res.data.lines || []
    systemTotal.value = res.data.count || systemLogs.value.length
  } finally { loadingSys.value = false }
}

async function clearSystemLog() {
  try {
    await ElMessageBox.confirm('确定清空面板日志文件吗？此操作不可恢复。', '清空日志', { type: 'warning' })
  } catch { return }
  await request.post('/logs/system/clear')
  ElMessage.success('已清空')
  systemLogs.value = []
  systemTotal.value = 0
}

async function exportOplog() {
  // 申请一次性下载 token（不泄露面板 JWT）
  try {
    const res = await request.post('/oplog/export-token')
    const dtoken = res.data?.token
    if (!dtoken) return ElMessage.error('获取导出链接失败')
    window.open(`/api/oplog/export?token=${encodeURIComponent(dtoken)}`)
  } catch (e) {
    ElMessage.error('获取导出链接失败')
  }
}

async function clearOplog(source) {
  const labels = { login: '操作日志', api: 'API 日志', mcp: 'MCP 日志' }
  const label = labels[source] || '日志'
  try {
    await ElMessageBox.confirm(`确定清空所有【${label}】吗？此操作不可恢复。`, '清空日志', { type: 'warning' })
  } catch { return }
  await request.post(`/oplog/clear?source=${source}`)
  ElMessage.success('已清空')
  if (source === 'login') loadOplog()
  else if (source === 'api') loadApiLog()
  else if (source === 'mcp') loadMcpLog()
}

// Tab 切换时持久化
watch(activeTab, () => {
  try { localStorage.setItem(LOGS_TAB_KEY, activeTab.value) } catch (e) { /* 忽略 */ }
})

onMounted(() => { loadOplog() })
</script>

<style scoped>
.logs-page { padding: 0 8px; }
.toolbar { display: flex; align-items: center; gap: 8px; margin-bottom: 16px; flex-wrap: wrap; }
.hint { color: #909399; font-size: 12px; margin-left: 4px; }
.pager { display: flex; justify-content: flex-end; margin-top: 12px; }
.log-box {
  background: #1e1e1e; color: #d4d4d4; padding: 12px;
  border-radius: 6px; font-size: 12px; line-height: 1.7;
  max-height: calc(100vh - 300px); overflow: auto; white-space: pre-wrap;
  word-break: break-all; margin: 0;
}

/* 面板日志字段样式 */
.ip-cell { font-family: 'SF Mono', Consolas, monospace; font-size: 12px; color: #475569; }
.msg-cell { font-family: 'SF Mono', Consolas, monospace; font-size: 12px; color: #1f2937; }
.msg-error { color: #dc2626; font-weight: 500; }
.msg-warn { color: #d97706; }
</style>