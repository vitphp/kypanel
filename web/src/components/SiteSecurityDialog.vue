<template>
  <el-dialog
    v-model="visible"
    width="920"
    top="5vh"
    :close-on-click-modal="false"
    :title="`站点安全防护 — ${siteName || ''}`"
    destroy-on-close
    append-to-body
  >
    <div v-loading="loading" class="ss-dialog">
      <!-- 顶部开关 -->
      <div class="ss-switch-row">
        <div class="ss-switch-left">
          <el-switch v-model="cfg.enabled" :disabled="saving" />
          <span class="ss-switch-label">启用安全防护</span>
          <el-tag v-if="cfg.enabled" :type="cfg.mode === 'block' ? 'danger' : 'warning'" size="small">
            {{ cfg.mode === 'block' ? '拦截模式' : '观察模式' }}
          </el-tag>
        </div>
        <div class="ss-switch-right">
          <el-button size="small" type="primary" :loading="saving" @click="saveConfig">保存配置</el-button>
        </div>
      </div>

      <el-tabs v-model="activeTab">
        <!-- ===== Tab 1: 访问控制 ===== -->
        <el-tab-pane label="访问控制" name="access">
          <div class="ss-form">
            <el-form label-width="120px" size="small">
              <el-form-item label="防护模式">
                <el-radio-group v-model="cfg.mode">
                  <el-radio-button label="block">拦截</el-radio-button>
                  <el-radio-button label="observe">观察</el-radio-button>
                </el-radio-group>
              </el-form-item>
              <el-form-item label="沿用全局规则">
                <el-switch v-model="cfg.use_global_rules" />
                <span class="ss-hint">关闭后本规则独立于全局 WAF（不同站不同安全级别）</span>
              </el-form-item>
              <el-form-item label="IP 白名单模式">
                <el-switch v-model="cfg.ip_whitelist_enabled" />
                <span class="ss-hint">开启后仅放行下方白名单 IP，其余全部拒绝</span>
              </el-form-item>
              <el-form-item label="UA 白名单模式">
                <el-switch v-model="cfg.ua_whitelist_enabled" />
                <span class="ss-hint">开启后仅放行下方 UA 白名单，其余 UA 拒绝</span>
              </el-form-item>
              <el-form-item label="Referer 校验">
                <el-select v-model="cfg.referer_check" style="width: 160px">
                  <el-option label="关闭" value="off" />
                  <el-option label="黑名单" value="blacklist" />
                  <el-option label="白名单" value="whitelist" />
                </el-select>
                <span class="ss-hint">白名单模式常用于防盗链</span>
              </el-form-item>
            </el-form>
          </div>

          <!-- IP 规则 -->
          <div class="ss-sub-card">
            <div class="ss-sub-head">
              <span>IP 规则</span>
              <div class="ss-sub-actions">
                <el-select v-model="ipForm.match_type" size="small" style="width: 100px">
                  <el-option label="单 IP" value="ip" />
                  <el-option label="CIDR" value="cidr" />
                  <el-option label="IP 段" value="range" />
                  <el-option label="国家" value="country" />
                  <el-option label="ISP" value="isp" />
                </el-select>
                <el-select v-model="ipForm.action" size="small" style="width: 90px">
                  <el-option label="拉黑" value="block" />
                  <el-option label="放行" value="allow" />
                </el-select>
                <el-input v-model="ipForm.content" size="small" placeholder="IP / CIDR / 国家 / ISP" style="width: 180px" />
                <el-select v-model="ipForm.expire_seconds" size="small" style="width: 110px">
                  <el-option label="永久" :value="0" />
                  <el-option label="1 小时" :value="3600" />
                  <el-option label="24 小时" :value="86400" />
                  <el-option label="7 天" :value="604800" />
                  <el-option label="30 天" :value="2592000" />
                </el-select>
                <el-button size="small" type="primary" @click="addIpRule">添加</el-button>
              </div>
            </div>
            <el-table :data="ipRules" size="small">
              <el-table-column prop="content" label="内容" min-width="160" />
              <el-table-column prop="match_type" label="类型" width="80" />
              <el-table-column label="动作" width="80">
                <template #default="{ row }">
                  <el-tag :type="row.action === 'block' ? 'danger' : 'success'" size="small">{{ row.action === 'block' ? '拉黑' : '放行' }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="到期" width="140">
                <template #default="{ row }">{{ row.expire_at ? fmtTime(row.expire_at) : '永久' }}</template>
              </el-table-column>
              <el-table-column prop="remark" label="备注" min-width="100" />
              <el-table-column label="操作" width="70">
                <template #default="{ row }">
                  <el-button size="small" type="danger" link @click="delIpRule(row)">删除</el-button>
                </template>
              </el-table-column>
            </el-table>
          </div>

          <!-- UA 规则 -->
          <div class="ss-sub-card">
            <div class="ss-sub-head">
              <span>UA 规则</span>
              <div class="ss-sub-actions">
                <el-select v-model="uaForm.action" size="small" style="width: 90px">
                  <el-option label="拉黑" value="block" />
                  <el-option label="放行" value="allow" />
                </el-select>
                <el-input v-model="uaForm.content" size="small" placeholder="UA 关键字，如 sqlmap" style="width: 220px" />
                <el-button size="small" type="primary" @click="addUaRule">添加</el-button>
              </div>
            </div>
            <el-table :data="uaRules" size="small">
              <el-table-column prop="content" label="内容" min-width="220" />
              <el-table-column label="动作" width="80">
                <template #default="{ row }">
                  <el-tag :type="row.action === 'block' ? 'danger' : 'success'" size="small">{{ row.action === 'block' ? '拉黑' : '放行' }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="remark" label="备注" min-width="100" />
              <el-table-column label="操作" width="70">
                <template #default="{ row }">
                  <el-button size="small" type="danger" link @click="delUaRule(row)">删除</el-button>
                </template>
              </el-table-column>
            </el-table>
          </div>

          <!-- Referer 规则 -->
          <div class="ss-sub-card">
            <div class="ss-sub-head">
              <span>Referer 规则</span>
              <div class="ss-sub-actions">
                <el-select v-model="refForm.action" size="small" style="width: 90px">
                  <el-option label="拉黑" value="block" />
                  <el-option label="放行" value="allow" />
                </el-select>
                <el-input v-model="refForm.content" size="small" placeholder="域名，如 evil.com" style="width: 220px" />
                <el-button size="small" type="primary" @click="addRefRule">添加</el-button>
              </div>
            </div>
            <el-table :data="refRules" size="small">
              <el-table-column prop="content" label="内容" min-width="220" />
              <el-table-column label="动作" width="80">
                <template #default="{ row }">
                  <el-tag :type="row.action === 'block' ? 'danger' : 'success'" size="small">{{ row.action === 'block' ? '拉黑' : '放行' }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="remark" label="备注" min-width="100" />
              <el-table-column label="操作" width="70">
                <template #default="{ row }">
                  <el-button size="small" type="danger" link @click="delRefRule(row)">删除</el-button>
                </template>
              </el-table-column>
            </el-table>
          </div>
        </el-tab-pane>

        <!-- ===== Tab 2: CC 防护 ===== -->
        <el-tab-pane label="CC 防护" name="cc">
          <el-form label-width="160px" size="small" class="ss-form">
            <el-form-item label="启用 CC 防护">
              <el-switch v-model="cfg.cc_enabled" />
            </el-form-item>
            <el-form-item label="窗口内最大请求数">
              <el-input-number v-model="cfg.cc_max_requests" :min="1" :max="100000" />
              <span class="ss-hint">超出即触发限速</span>
            </el-form-item>
            <el-form-item label="统计窗口（秒）">
              <el-input-number v-model="cfg.cc_window_sec" :min="1" :max="3600" />
            </el-form-item>
            <el-form-item label="封禁时长（秒）">
              <el-input-number v-model="cfg.cc_block_sec" :min="0" :max="864000" />
              <span class="ss-hint">触发后自动封禁时长</span>
            </el-form-item>
            <el-form-item label="单 IP 最大并发">
              <el-input-number v-model="cfg.cc_max_conn_per_ip" :min="0" :max="10000" />
              <span class="ss-hint">0 表示不限制</span>
            </el-form-item>
          </el-form>
        </el-tab-pane>

        <!-- ===== Tab 3: 拦截规则 ===== -->
        <el-tab-pane label="拦截规则" name="rules">
          <div class="ss-sub-card">
            <div class="ss-sub-head">
              <span>自定义规则</span>
              <div class="ss-sub-actions">
                <el-input v-model="ruleForm.name" size="small" placeholder="规则名" style="width: 120px" />
                <el-select v-model="ruleForm.match_field" size="small" style="width: 100px">
                  <el-option label="URI" value="uri" />
                  <el-option label="参数" value="args" />
                  <el-option label="UA" value="ua" />
                  <el-option label="Referer" value="referer" />
                  <el-option label="Header" value="header" />
                  <el-option label="Body" value="body" />
                </el-select>
                <el-input v-model="ruleForm.pattern" size="small" placeholder="正则表达式" style="width: 200px" />
                <el-button size="small" type="primary" @click="addRule">添加</el-button>
              </div>
            </div>
            <el-table :data="customRules" size="small">
              <el-table-column prop="name" label="名称" min-width="120" />
              <el-table-column prop="pattern" label="规则" min-width="200" show-overflow-tooltip />
              <el-table-column prop="match_field" label="匹配" width="80" />
              <el-table-column label="状态" width="80">
                <template #default="{ row }">
                  <el-switch v-model="row.enabled" size="small" @change="toggleRule(row)" />
                </template>
              </el-table-column>
              <el-table-column label="操作" width="70">
                <template #default="{ row }">
                  <el-button size="small" type="danger" link @click="delRule(row)">删除</el-button>
                </template>
              </el-table-column>
            </el-table>
          </div>
        </el-tab-pane>

        <!-- ===== Tab 4: HTTP 头 ===== -->
        <el-tab-pane label="HTTP 安全头" name="headers">
          <el-form label-width="180px" size="small" class="ss-form">
            <el-form-item label="HSTS">
              <el-switch v-model="cfg.hsts" />
              <span class="ss-hint">强制浏览器走 HTTPS，防 SSL 降级</span>
            </el-form-item>
            <el-form-item label="X-Frame-Options">
              <el-select v-model="cfg.x_frame_options" style="width: 160px">
                <el-option label="DENY" value="DENY" />
                <el-option label="SAMEORIGIN" value="SAMEORIGIN" />
                <el-option label="关闭" value="off" />
              </el-select>
              <span class="ss-hint">防点击劫持</span>
            </el-form-item>
            <el-form-item label="禁止 MIME 嗅探">
              <el-switch v-model="cfg.no_sniff" />
              <span class="ss-hint">X-Content-Type-Options: nosniff</span>
            </el-form-item>
            <el-form-item label="禁止目录浏览">
              <el-switch v-model="cfg.no_dir_list" />
              <span class="ss-hint">autoindex off</span>
            </el-form-item>
          </el-form>
        </el-tab-pane>

        <!-- ===== Tab 5: 攻击日志 ===== -->
        <el-tab-pane label="攻击日志" name="logs">
          <div class="ss-log-stats" v-if="stats">
            <div class="ss-log-stat">
              <div class="ss-log-stat-label">总拦截</div>
              <div class="ss-log-stat-value">{{ stats.total_block.toLocaleString() }}</div>
            </div>
            <div class="ss-log-stat">
              <div class="ss-log-stat-label">今日拦截</div>
              <div class="ss-log-stat-value">{{ stats.today_block.toLocaleString() }}</div>
            </div>
            <div class="ss-log-stat">
              <div class="ss-log-stat-label">总日志</div>
              <div class="ss-log-stat-value">{{ stats.total_log.toLocaleString() }}</div>
            </div>
          </div>
          <div class="ss-log-toolbar">
            <el-input v-model="logKeyword" size="small" placeholder="搜索 IP / URI / 规则" style="width: 220px" clearable @clear="loadLogs" @keyup.enter="loadLogs" />
            <el-button size="small" @click="loadLogs">查询</el-button>
            <el-button size="small" type="danger" plain @click="clearLogs">清空日志</el-button>
          </div>
          <el-table :data="logs" size="small" max-height="320">
            <el-table-column label="时间" width="150">
              <template #default="{ row }">{{ fmtTime(row.time) }}</template>
            </el-table-column>
            <el-table-column prop="ip" label="IP" width="130" />
            <el-table-column prop="rule_name" label="规则" min-width="120" />
            <el-table-column prop="method" label="方法" width="70" />
            <el-table-column prop="uri" label="URI" min-width="180" show-overflow-tooltip />
            <el-table-column label="动作" width="70">
              <template #default="{ row }">
                <el-tag :type="row.action === 'block' ? 'danger' : 'warning'" size="small">{{ row.action === 'block' ? '拦截' : '记录' }}</el-tag>
              </template>
            </el-table-column>
          </el-table>
          <div class="ss-log-pagination">
            <el-pagination
              v-model:current-page="logPage"
              :page-size="logPageSize"
              :total="logTotal"
              layout="total, prev, pager, next"
              @current-change="loadLogs"
            />
          </div>
        </el-tab-pane>
      </el-tabs>
    </div>
  </el-dialog>
</template>

<script setup>
import { ref, reactive, computed, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import request from '../utils/request'

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
const saving = ref(false)
const activeTab = ref('access')

const cfg = ref({
  enabled: false,
  use_global_rules: true,
  mode: 'block',
  ip_whitelist_enabled: false,
  ua_whitelist_enabled: false,
  referer_check: 'off',
  cc_enabled: false,
  cc_max_requests: 100,
  cc_window_sec: 10,
  cc_block_sec: 3600,
  cc_max_conn_per_ip: 0,
  hsts: false,
  x_frame_options: 'DENY',
  no_sniff: true,
  no_dir_list: true
})

// 规则列表
const ipRules = ref([])
const uaRules = ref([])
const refRules = ref([])
const customRules = ref([])

// 表单
const ipForm = reactive({ action: 'block', match_type: 'ip', content: '', expire_seconds: 0 })
const uaForm = reactive({ action: 'block', content: '' })
const refForm = reactive({ action: 'block', content: '' })
const ruleForm = reactive({ name: '', match_field: 'uri', pattern: '' })

// 日志
const logs = ref([])
const logTotal = ref(0)
const logPage = ref(1)
const logPageSize = 20
const logKeyword = ref('')
const stats = ref(null)

watch(() => props.modelValue, (v) => {
  if (v && props.siteId) {
    loadAll()
  }
})

async function loadAll() {
  loading.value = true
  try {
    await Promise.all([loadConfig(), loadIpRules(), loadUaRules(), loadRefRules(), loadRules(), loadStats(), loadLogs()])
  } finally {
    loading.value = false
  }
}

async function loadConfig() {
  const res = await request.get('/site/security/config', { params: { site_id: props.siteId } })
  if (res.data) {
    cfg.value = { ...cfg.value, ...res.data }
  }
}

async function loadIpRules() {
  const res = await request.get('/site/security/ip', { params: { site_id: props.siteId } })
  ipRules.value = res.data || []
}

async function loadUaRules() {
  const res = await request.get('/site/security/ua', { params: { site_id: props.siteId } })
  uaRules.value = res.data || []
}

async function loadRefRules() {
  const res = await request.get('/site/security/referer', { params: { site_id: props.siteId } })
  refRules.value = res.data || []
}

async function loadRules() {
  const res = await request.get('/site/security/rule', { params: { site_id: props.siteId } })
  customRules.value = res.data || []
}

async function loadStats() {
  const res = await request.get('/site/security/stats', { params: { site_id: props.siteId } })
  stats.value = res.data
}

async function loadLogs() {
  const res = await request.get('/site/security/logs', {
    params: { site_id: props.siteId, page: logPage.value, page_size: logPageSize, keyword: logKeyword.value }
  })
  logs.value = res.data?.logs || []
  logTotal.value = res.data?.total || 0
}

// 统一把 site_id 放到 query 参数（后端 parseSiteID 从 query 读取）
function secParams() {
  return { site_id: props.siteId }
}

async function saveConfig() {
  saving.value = true
  try {
    await request.post('/site/security/config', { ...cfg.value }, { params: secParams() })
    ElMessage.success('配置已保存')
  } catch (e) { /* interceptor handles */ } finally {
    saving.value = false
  }
}

async function addIpRule() {
  if (!ipForm.content) { ElMessage.warning('请输入内容'); return }
  try {
    await request.post('/site/security/ip/add', {
      action: ipForm.action,
      match_type: ipForm.match_type,
      content: ipForm.content,
      expire_seconds: ipForm.expire_seconds
    }, { params: secParams() })
    ipForm.content = ''
    ElMessage.success('已添加')
    loadIpRules()
  } catch (e) { /* interceptor handles */ }
}

async function delIpRule(row) {
  await request.post('/site/security/ip/delete', { id: row.id }, { params: secParams() })
  ElMessage.success('已删除')
  loadIpRules()
}

async function addUaRule() {
  if (!uaForm.content) { ElMessage.warning('请输入内容'); return }
  await request.post('/site/security/ua/add', { action: uaForm.action, content: uaForm.content }, { params: secParams() })
  uaForm.content = ''
  ElMessage.success('已添加')
  loadUaRules()
}

async function delUaRule(row) {
  await request.post('/site/security/ua/delete', { id: row.id }, { params: secParams() })
  ElMessage.success('已删除')
  loadUaRules()
}

async function addRefRule() {
  if (!refForm.content) { ElMessage.warning('请输入内容'); return }
  await request.post('/site/security/referer/add', { action: refForm.action, content: refForm.content }, { params: secParams() })
  refForm.content = ''
  ElMessage.success('已添加')
  loadRefRules()
}

async function delRefRule(row) {
  await request.post('/site/security/referer/delete', { id: row.id }, { params: secParams() })
  ElMessage.success('已删除')
  loadRefRules()
}

async function addRule() {
  if (!ruleForm.pattern) { ElMessage.warning('请输入规则正则'); return }
  await request.post('/site/security/rule/add', {
    name: ruleForm.name || '自定义规则',
    match_field: ruleForm.match_field,
    pattern: ruleForm.pattern
  }, { params: secParams() })
  ruleForm.pattern = ''
  ruleForm.name = ''
  ElMessage.success('已添加')
  loadRules()
}

async function toggleRule(row) {
  await request.post('/site/security/rule/update', { id: row.id, enabled: row.enabled }, { params: secParams() })
  ElMessage.success('已更新')
}

async function delRule(row) {
  await request.post('/site/security/rule/delete', { id: row.id }, { params: secParams() })
  ElMessage.success('已删除')
  loadRules()
}

async function clearLogs() {
  try { await ElMessageBox.confirm('确定清空该站点的攻击日志吗？', '提示', { type: 'warning' }) } catch { return }
  await request.post('/site/security/logs/clear', {}, { params: secParams() })
  ElMessage.success('已清空')
  loadLogs()
  loadStats()
}

function fmtTime(t) {
  if (!t) return '-'
  const d = new Date(t)
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}
</script>

<style scoped>
.ss-dialog { min-height: 480px; }
.ss-switch-row { display: flex; align-items: center; justify-content: space-between; padding: 12px 16px; background: #f5f7fa; border-radius: 8px; margin-bottom: 16px; }
.ss-switch-left { display: flex; align-items: center; gap: 10px; }
.ss-switch-label { font-weight: 600; }
.ss-switch-right { display: flex; gap: 8px; }
.ss-form { margin-bottom: 8px; }
.ss-hint { margin-left: 10px; color: #94a3b8; font-size: 12px; }
.ss-sub-card { border: 1px solid #ebeef5; border-radius: 8px; padding: 12px; margin-bottom: 16px; }
.ss-sub-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 10px; }
.ss-sub-head > span { font-weight: 600; }
.ss-sub-actions { display: flex; gap: 6px; align-items: center; }
.ss-log-stats { display: flex; gap: 16px; margin-bottom: 16px; }
.ss-log-stat { flex: 1; background: #f5f7fa; border-radius: 8px; padding: 16px; text-align: center; }
.ss-log-stat-label { color: #94a3b8; font-size: 13px; }
.ss-log-stat-value { font-size: 24px; font-weight: 700; margin-top: 4px; }
.ss-log-toolbar { display: flex; gap: 8px; margin-bottom: 12px; }
.ss-log-pagination { display: flex; justify-content: flex-end; margin-top: 12px; }
</style>
