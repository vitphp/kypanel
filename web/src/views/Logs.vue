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
          <el-table-column label="操作" min-width="260">
            <template #default="{ row }">
              <div class="op-line">
                <el-tag v-if="opVerb(row)" size="small" type="info" class="op-verb">{{ opVerb(row) }}</el-tag>
                <span v-if="row.detail && stripSourcePrefix(row.action) !== 'system.exec'" class="op-detail">{{ row.detail }}</span>
              </div>
            </template>
          </el-table-column>
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
          <el-table-column label="操作" min-width="260">
            <template #default="{ row }">
              <div class="op-line">
                <el-tag v-if="opVerb(row)" size="small" type="info" class="op-verb">{{ opVerb(row) }}</el-tag>
                <span v-if="row.detail && stripSourcePrefix(row.action) !== 'system.exec'" class="op-detail">{{ row.detail }}</span>
              </div>
            </template>
          </el-table-column>
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
          <el-table-column label="操作" min-width="260">
            <template #default="{ row }">
              <div class="op-line">
                <el-tag v-if="opVerb(row)" size="small" type="info" class="op-verb">{{ opVerb(row) }}</el-tag>
                <span v-if="row.detail && stripSourcePrefix(row.action) !== 'system.exec'" class="op-detail">{{ row.detail }}</span>
              </div>
            </template>
          </el-table-column>
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

// 操作动作中文映射（去掉 api./mcp./temp. 前缀后的完整 action 作为 key），用于把"操作"列显示成明文
const opVerbMap = {
  'system.exec': '执行命令', 'system.restart_panel': '重启面板', 'system.restart_server': '重启服务器',
  'system.clear_log': '清空系统日志',
  'file.write': '写入文件', 'file.mkdir': '创建目录', 'file.create': '创建文件', 'file.rename': '重命名',
  'file.delete': '删除', 'file.copy': '复制', 'file.mv': '移动', 'file.remote_download': '远程下载',
  'file.upload': '上传文件', 'file.chmod': '修改权限', 'file.zip': '压缩', 'file.unzip': '解压',
  'file.trash.empty': '清空回收站', 'file.trash.restore': '还原回收站项目', 'file.trash.purge': '彻底删除回收站项目',
  'oplog.clear': '清空日志', 'oplog.export': '导出日志',
  'waf.setting': '修改WAF设置', 'waf.rule.update': '修改WAF规则', 'waf.rule.add': '新增WAF规则',
  'waf.rule.delete': '删除WAF规则', 'waf.rules.reset': '重置WAF规则', 'waf.iprule.add': '新增WAF黑白名单',
  'waf.iprule.delete': '删除WAF黑白名单', 'waf.cc.save': '保存CC防护', 'waf.logs.clear': '清空攻击日志',
  'user.role_save': '保存角色', 'user.create': '创建子账号',
  'site.default_page': '保存默认页', 'site.default_site': '设置默认站点', 'site.create': '创建网站',
  'site.action': '启停网站', 'site.delete': '删除网站', 'site.settings': '修改网站设置',
  'site.ssl': '修改HTTPS设置', 'site.config': '修改站点配置', 'site.remark': '修改站点备注',
  'site.ssl.cert_delete': '删除证书记录', 'site.ssl.apply_cert': '部署证书',
  'site.ssl.letsencrypt': '申请证书', 'site.ssl.apply': '申请证书', 'site.ssl.apply_dns': '申请证书',
  'site.security.config': '更新站点安全', 'site.security.ip_add': '新增站点IP规则',
  'settings.username': '修改管理员账号', 'settings.port': '修改面板端口', 'settings.domain': '修改绑定域名',
  'settings.entrance': '修改安全入口', 'settings.panel_name': '修改面板名称', 'settings.report': '修改体验计划',
  'settings.mysql_pwd': '更新MySQL密码', 'settings.litessl': '更新LiteSSL', 'settings.login_allowlist': '更新登录白名单',
  'settings.session_kick': '踢下线会话', 'settings.session_kick_all': '踢下线全部会话',
  'security.rule.add': '新增防火墙规则', 'security.rule.update': '修改防火墙规则', 'security.rule.delete': '删除防火墙规则',
  'security.rule.remark': '修改规则备注',
  'process.kill': '结束进程', 'ftp.create': '创建FTP用户', 'ftp.delete': '删除FTP用户',
  'docker.create': '创建容器', 'docker.action': '启停容器', 'docker.remove': '删除容器',
  'docker.network.create': '创建网络', 'docker.network.rm': '删除网络',
  'docker.app.install': '安装容器应用', 'docker.app.uninstall': '卸载容器应用',
  'cron.save': '保存计划任务', 'cron.delete': '删除计划任务', 'cron.run': '执行计划任务', 'cron.backup_delete': '删除计划任务备份',
  'backup.create': '创建备份', 'backup.restore': '恢复备份', 'backup.delete': '删除备份', 'backup.upload': '上传备份',
  'auth.totp_enable': '启用双因素认证', 'auth.totp_disable': '关闭双因素认证',
  'api_token.create': '创建API令牌', 'api_token.delete': '删除API令牌',
  'app.install': '安装应用', 'app.uninstall': '卸载应用', 'app.service': '服务启停', 'app.cancel': '停止任务',
  'temp.access.create': '创建临时访问', 'temp.access.revoke': '吊销临时访问',
}
function stripSourcePrefix(a) { return (a || '').replace(/^(api|mcp|temp)\./, '') }
function opVerb(row) { return opVerbMap[stripSourcePrefix(row.action)] || '' }
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

onMounted(() => {
  if (activeTab.value === 'api') loadApiLog()
  else if (activeTab.value === 'mcp') loadMcpLog()
  else if (activeTab.value === 'system') loadSystemLog()
  else if (activeTab.value === 'app') loadAppLogList()
  else loadOplog()
})
</script>

<style scoped>
.logs-page { padding: 0 8px; }
/* 操作列：只显示中文动作名（无映射时兜底显示 detail） */
.op-line { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.op-verb { flex: none; }
.op-detail {
  color: #303133; word-break: break-all;
  display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;
}
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