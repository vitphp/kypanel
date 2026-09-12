<template>
  <div class="ops-wrap">
    <el-tabs v-model="tab" class="ops-tabs">
      <!-- ===== 账号策略：转发 / 自动回复 / 容量 ===== -->
      <el-tab-pane label="账号策略" name="policy">
        <div class="ops-toolbar">
          <el-select v-model="policyAccountId" placeholder="选择邮箱账号" size="small" style="width: 260px" @change="onPolicyAccountChange">
            <el-option v-for="a in accounts" :key="a.id" :label="a.address" :value="a.id" />
          </el-select>
          <el-button size="small" :icon="Refresh" :loading="loading" @click="loadAccounts">刷新</el-button>
        </div>

        <div v-if="policyAccountId" class="ops-form">
          <div class="ops-section-title">容量用量</div>
          <div class="ops-usage">
            <el-progress
              :percentage="Math.min(100, Math.round(usage.percent || 0))"
              :status="usage.percent >= 95 ? 'exception' : usage.percent >= 80 ? 'warning' : ''"
              :stroke-width="14"
            />
            <span class="ops-usage-text">
              已用 {{ fmtSize(usage.used) }} / {{ fmtSize(usage.quota) }}（{{ Math.round(usage.percent || 0) }}%，共 {{ usage.messages }} 封）
            </span>
          </div>

          <div class="ops-section-title">转发设置</div>
          <div class="ops-row">
            <span class="ops-label">转发到</span>
            <el-input v-model="policy.forward_to" placeholder="多个地址用逗号分隔，留空不转发" style="max-width: 420px" />
          </div>
          <div class="ops-row">
            <span class="ops-label">保留副本</span>
            <el-switch v-model="policy.keep_copy" />
            <span class="ops-hint">关闭后转发成功即删除本地副本（仅转发）</span>
          </div>

          <div class="ops-section-title">自动回复</div>
          <div class="ops-row">
            <span class="ops-label">启用</span>
            <el-switch v-model="policy.auto_reply_on" />
            <span class="ops-hint">同一发件人 1 小时内只回复一次；本站地址与退信地址不回复</span>
          </div>
          <div class="ops-row ops-row-top">
            <span class="ops-label">回复内容</span>
            <el-input v-model="policy.auto_reply_text" type="textarea" :rows="4" placeholder="例如：您好，我暂时不在，稍后回复。" style="max-width: 480px" />
          </div>

          <div class="ops-section-title">容量上限</div>
          <div class="ops-row">
            <span class="ops-label">容量(MB)</span>
            <el-input-number v-model="policy.quota_mb" :min="1" :max="102400" />
            <el-button size="small" @click="recalcUsage">按索引重算用量</el-button>
          </div>

          <div class="ops-actions">
            <el-button type="primary" :loading="saving" @click="savePolicy">保存设置</el-button>
          </div>
        </div>
        <div v-else class="ops-empty">请先选择一个邮箱账号</div>
      </el-tab-pane>

      <!-- ===== 别名 ===== -->
      <el-tab-pane label="别名" name="alias">
        <div class="ops-toolbar">
          <el-input v-model="aliasForm.source" placeholder="别名（@ 前，如 info）" size="small" style="width: 180px" />
          <span class="ops-at">@{{ domain?.domain }}</span>
          <el-input v-model="aliasForm.target" placeholder="转发到（可多个，逗号分隔）" size="small" style="width: 300px" />
          <el-button type="primary" size="small" :loading="saving" @click="addAlias">添加别名</el-button>
          <el-button size="small" :icon="Refresh" :loading="loading" @click="loadAliases">刷新</el-button>
        </div>
        <el-table v-loading="loading" :data="aliases" size="small" max-height="380" empty-text="还没有别名">
          <el-table-column label="别名" min-width="190">
            <template #default="{ row }">{{ row.source }}@{{ row.domain }}</template>
          </el-table-column>
          <el-table-column prop="target" label="转发到" min-width="220" show-overflow-tooltip />
          <el-table-column label="状态" width="80">
            <template #default="{ row }">
              <el-tag :type="row.enabled ? 'success' : 'info'" size="small">{{ row.enabled ? '启用' : '停用' }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="130" align="right">
            <template #default="{ row }">
              <el-button link type="warning" size="small" @click="toggleAlias(row)">{{ row.enabled ? '停用' : '启用' }}</el-button>
              <el-button link type="danger" size="small" @click="removeAlias(row)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <!-- ===== 外发队列 ===== -->
      <el-tab-pane label="外发队列" name="outbox">
        <div class="ops-toolbar">
          <el-select v-model="outboxStatus" size="small" style="width: 140px" @change="loadOutbox">
            <el-option label="全部" value="" />
            <el-option label="待发送" value="pending" />
            <el-option label="已发送" value="sent" />
            <el-option label="失败" value="failed" />
          </el-select>
          <span class="ops-hint">待发 {{ outboxPending }} 封（失败会自动重试并最终退信）</span>
          <el-button size="small" :icon="Refresh" :loading="loading" @click="loadOutbox">刷新</el-button>
        </div>
        <el-table v-loading="loading" :data="outbox" size="small" max-height="380" empty-text="队列为空">
          <el-table-column prop="to_addrs" label="收件人" min-width="200" show-overflow-tooltip />
          <el-table-column prop="subject" label="主题" min-width="160" show-overflow-tooltip />
          <el-table-column label="状态" width="90">
            <template #default="{ row }">
              <el-tag :type="row.status === 'sent' ? 'success' : row.status === 'failed' ? 'danger' : 'warning'" size="small">
                {{ row.status === 'sent' ? '已发送' : row.status === 'failed' ? '失败' : '待发送' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="重试" width="70"><template #default="{ row }">{{ row.retry }}/{{ row.max_retry }}</template></el-table-column>
          <el-table-column prop="last_error" label="最近错误" min-width="180" show-overflow-tooltip />
          <el-table-column label="操作" width="130" align="right">
            <template #default="{ row }">
              <el-button v-if="row.status === 'pending'" link type="primary" size="small" @click="retryOutbox(row)">重试</el-button>
              <el-button link type="danger" size="small" @click="removeOutbox(row)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <!-- ===== 收发信日志 ===== -->
      <el-tab-pane label="收发信日志" name="logs">
        <div class="ops-toolbar">
          <el-select v-model="logFilter.direction" size="small" style="width: 120px" @change="loadLogs">
            <el-option label="全部" value="" />
            <el-option label="收信" value="in" />
            <el-option label="发信" value="out" />
            <el-option label="退信" value="bounce" />
          </el-select>
          <el-input v-model="logFilter.keyword" placeholder="按地址/主题搜索" size="small" style="width: 220px" clearable @keyup.enter="loadLogs" />
          <el-button size="small" @click="loadLogs">查询</el-button>
          <el-button size="small" :icon="Refresh" :loading="loading" @click="loadLogs">刷新</el-button>
        </div>
        <div class="ops-stats">
          <span>总计 {{ stats.total }}</span>
          <span>收信成功 {{ stats.in_ok }}</span>
          <span>发信成功 {{ stats.out_ok }}</span>
          <span class="ops-stat-bad">失败 {{ stats.failed }}</span>
          <span>今日收 {{ stats.today_in }}</span>
          <span>今日发 {{ stats.today_out }}</span>
        </div>
        <el-table v-loading="loading" :data="logs" size="small" max-height="360" empty-text="暂无日志">
          <el-table-column label="方向" width="70">
            <template #default="{ row }">
              <el-tag size="small" :type="row.direction === 'in' ? 'primary' : row.direction === 'bounce' ? 'danger' : 'success'">
                {{ row.direction === 'in' ? '收信' : row.direction === 'bounce' ? '退信' : '发信' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="from_addr" label="发件人" min-width="170" show-overflow-tooltip />
          <el-table-column prop="to_addrs" label="收件人" min-width="170" show-overflow-tooltip />
          <el-table-column prop="subject" label="主题" min-width="140" show-overflow-tooltip />
          <el-table-column label="结果" width="80">
            <template #default="{ row }">
              <el-tag size="small" :type="row.status === 'ok' || row.status === 'sent' ? 'success' : row.status === 'queued' ? 'warning' : 'danger'">
                {{ statusText(row.status) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="时间" width="150"><template #default="{ row }">{{ fmtTime(row.created_at) }}</template></el-table-column>
        </el-table>
      </el-tab-pane>

      <!-- ===== API 令牌 ===== -->
      <el-tab-pane label="API 令牌" name="token">
        <div class="ops-toolbar">
          <el-input v-model="tokenName" placeholder="令牌用途备注" size="small" style="width: 220px" />
          <el-button type="primary" size="small" :loading="saving" @click="createToken">生成令牌</el-button>
          <el-button size="small" :icon="Refresh" :loading="loading" @click="loadTokens">刷新</el-button>
        </div>
        <div class="ops-hint ops-hint-block">
          第三方系统可用此令牌调用对外邮件接口（无需面板账号）：<br>
          发信 <code>POST /api/mail-api/v1/send</code>　收信 <code>GET /api/mail-api/v1/messages?address=xx@{{ domain?.domain }}</code>　鉴权头 <code>X-API-Key: &lt;令牌&gt;</code>
        </div>
        <el-table v-loading="loading" :data="tokens" size="small" max-height="360" empty-text="还没有令牌">
          <el-table-column prop="name" label="备注" min-width="140" />
          <el-table-column prop="key_prefix" label="令牌前缀" width="140" />
          <el-table-column label="调用次数" width="100"><template #default="{ row }">{{ row.call_count }}</template></el-table-column>
          <el-table-column label="状态" width="80">
            <template #default="{ row }">
              <el-tag :type="row.enabled ? 'success' : 'info'" size="small">{{ row.enabled ? '启用' : '停用' }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="130" align="right">
            <template #default="{ row }">
              <el-button link type="warning" size="small" @click="toggleToken(row)">{{ row.enabled ? '停用' : '启用' }}</el-button>
              <el-button link type="danger" size="small" @click="removeToken(row)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <!-- ===== 服务状态 ===== -->
      <el-tab-pane label="服务状态" name="status">
        <div class="ops-status-list">
          <div class="ops-status-item">
            <span class="ops-status-label">SMTP 收信端口（明文 / STARTTLS）</span>
            <span class="ops-status-value">{{ smtpStatus.plain_ports?.join('、') || '-' }}</span>
          </div>
          <div class="ops-status-item">
            <span class="ops-status-label">SMTPS 端口（隐式 TLS）</span>
            <span class="ops-status-value">{{ smtpStatus.smtps_ports?.join('、') || '-' }}</span>
          </div>
          <div class="ops-status-item">
            <span class="ops-status-label">提交端口（需认证）</span>
            <span class="ops-status-value">{{ smtpStatus.submission_ports?.join('、') || '-' }}</span>
          </div>
          <div class="ops-status-item">
            <span class="ops-status-label">TLS 证书</span>
            <span class="ops-status-value">
              <el-tag :type="smtpStatus.tls_ready ? 'success' : 'danger'" size="small">
                {{ smtpStatus.tls_ready ? '已就绪（' + (smtpStatus.tls_source === 'panel' ? '面板证书' : '自签证书') + '）' : '不可用' }}
              </el-tag>
            </span>
          </div>
          <div class="ops-status-item">
            <span class="ops-status-label">DKIM 签名</span>
            <span class="ops-status-value">
              <el-tag :type="dkim.generated ? 'success' : 'warning'" size="small">{{ dkim.generated ? '已配置' : '未生成' }}</el-tag>
              <span v-if="dkim.host" class="ops-hint">{{ dkim.host }}</span>
            </span>
          </div>
          <div class="ops-status-item">
            <span class="ops-status-label">反垃圾（入站）</span>
            <span class="ops-status-value ops-hint">SPF 校验（-all 拒收）+ DKIM 校验 + 每 IP 限速</span>
          </div>
        </div>
        <div class="ops-hint ops-hint-block">
          终端用户/邮件客户端连接参数：收信 IMAP 暂未提供（可用门户网页收发）；发信 SMTP 服务器填本机域名，端口 587（STARTTLS）或 465（SSL），用户名填完整邮箱地址。
        </div>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { ref, watch, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import {
  listMailAccounts, updateMailAccountSettings, getMailAccountUsage, recalcMailAccountUsage,
  listMailAliases, addMailAlias, updateMailAlias, deleteMailAlias,
  listMailOutbox, retryMailOutbox, deleteMailOutbox,
  listMailLogs, getMailLogStats,
  listMailApiTokens, createMailApiToken, setMailApiTokenEnabled, deleteMailApiToken,
  getMailSmtpStatus, getMailDomainDkim
} from '../../api/mail'

const props = defineProps({
  domain: { type: Object, default: null },
  domainId: { type: Number, default: 0 }
})

const tab = ref('policy')
const loading = ref(false)
const saving = ref(false)

// 账号策略
const accounts = ref([])
const policyAccountId = ref(0)
const policy = ref({ forward_to: '', keep_copy: true, auto_reply_on: false, auto_reply_text: '', quota_mb: 1024 })
const usage = ref({ used: 0, quota: 0, percent: 0, messages: 0 })

// 别名
const aliases = ref([])
const aliasForm = ref({ source: '', target: '' })

// 外发队列
const outbox = ref([])
const outboxPending = ref(0)
const outboxStatus = ref('')

// 日志
const logs = ref([])
const stats = ref({ total: 0, in_ok: 0, out_ok: 0, failed: 0, today_in: 0, today_out: 0 })
const logFilter = ref({ direction: '', keyword: '' })

// 令牌
const tokens = ref([])
const tokenName = ref('')

// 服务状态
const smtpStatus = ref({})
const dkim = ref({ generated: false, host: '' })

function fmtSize(bytes) {
  const n = Number(bytes || 0)
  if (n < 1024) return n + ' B'
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB'
  if (n < 1024 * 1024 * 1024) return (n / 1024 / 1024).toFixed(1) + ' MB'
  return (n / 1024 / 1024 / 1024).toFixed(2) + ' GB'
}
function fmtTime(t) {
  if (!t) return '-'
  const d = new Date(typeof t === 'number' ? t * 1000 : t)
  if (isNaN(d.getTime())) return '-'
  const p = (x) => String(x).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}
function statusText(s) {
  return { ok: '成功', queued: '已入队', sent: '成功', failed: '失败', reject: '拒收' }[s] || s || '-'
}
function errMsg(e) {
  return e?.response?.data?.message || e?.message || '操作失败'
}

async function loadAccounts() {
  if (!props.domainId) return
  loading.value = true
  try {
    const { data } = await listMailAccounts(props.domainId)
    accounts.value = data || []
    if (!policyAccountId.value && accounts.value.length) {
      policyAccountId.value = accounts.value[0].id
      onPolicyAccountChange(policyAccountId.value)
    }
  } catch { /* ignore */ } finally {
    loading.value = false
  }
}

function onPolicyAccountChange(id) {
  const a = accounts.value.find((x) => x.id === id)
  if (!a) return
  policy.value = {
    forward_to: a.forward_to || '',
    keep_copy: a.keep_copy !== false,
    auto_reply_on: !!a.auto_reply_on,
    auto_reply_text: a.auto_reply_text || '',
    quota_mb: a.quota_mb || 1024
  }
  loadUsage(id)
}

async function loadUsage(id) {
  if (!id) return
  try {
    const { data } = await getMailAccountUsage(id)
    usage.value = data || { used: 0, quota: 0, percent: 0, messages: 0 }
  } catch { /* ignore */ }
}

async function recalcUsage() {
  if (!policyAccountId.value) return
  try {
    await recalcMailAccountUsage(policyAccountId.value)
    await loadUsage(policyAccountId.value)
    ElMessage.success('已按邮件索引重算用量')
  } catch (e) {
    ElMessage.error(errMsg(e))
  }
}

async function savePolicy() {
  if (!policyAccountId.value) return
  saving.value = true
  try {
    await updateMailAccountSettings(policyAccountId.value, policy.value)
    ElMessage.success('已保存')
    loadAccounts()
  } catch (e) {
    ElMessage.error(errMsg(e))
  } finally {
    saving.value = false
  }
}

async function loadAliases() {
  if (!props.domainId) return
  loading.value = true
  try {
    const { data } = await listMailAliases(props.domainId)
    aliases.value = data || []
  } catch { /* ignore */ } finally {
    loading.value = false
  }
}

async function addAlias() {
  if (!aliasForm.value.source || !aliasForm.value.target) {
    ElMessage.warning('请填写别名与转发目标')
    return
  }
  saving.value = true
  try {
    await addMailAlias({ domain_id: props.domainId, source: aliasForm.value.source, target: aliasForm.value.target })
    ElMessage.success('别名已添加')
    aliasForm.value = { source: '', target: '' }
    loadAliases()
  } catch (e) {
    ElMessage.error(errMsg(e))
  } finally {
    saving.value = false
  }
}

async function toggleAlias(row) {
  try {
    await updateMailAlias(row.id, { enabled: !row.enabled })
    loadAliases()
  } catch (e) {
    ElMessage.error(errMsg(e))
  }
}

async function removeAlias(row) {
  try {
    await ElMessageBox.confirm(`确定删除别名 ${row.source}@${row.domain}？`, '提示', { type: 'warning' })
  } catch { return }
  try {
    await deleteMailAlias(row.id)
    ElMessage.success('已删除')
    loadAliases()
  } catch (e) {
    ElMessage.error(errMsg(e))
  }
}

async function loadOutbox() {
  loading.value = true
  try {
    const { data } = await listMailOutbox({ status: outboxStatus.value, limit: 200 })
    outbox.value = data?.list || []
    outboxPending.value = data?.pending || 0
  } catch { /* ignore */ } finally {
    loading.value = false
  }
}

async function retryOutbox(row) {
  try {
    await retryMailOutbox(row.id)
    ElMessage.success('已触发重试')
    setTimeout(loadOutbox, 1500)
  } catch (e) {
    ElMessage.error(errMsg(e))
  }
}

async function removeOutbox(row) {
  try {
    await deleteMailOutbox(row.id)
    loadOutbox()
  } catch (e) {
    ElMessage.error(errMsg(e))
  }
}

async function loadLogs() {
  loading.value = true
  try {
    const [logsRes, statsRes] = await Promise.all([
      listMailLogs({ direction: logFilter.value.direction, keyword: logFilter.value.keyword, domain: props.domain?.domain, limit: 200 }),
      getMailLogStats(props.domain?.domain)
    ])
    logs.value = logsRes.data || []
    stats.value = statsRes.data || stats.value
  } catch { /* ignore */ } finally {
    loading.value = false
  }
}

async function loadTokens() {
  if (!props.domainId) return
  loading.value = true
  try {
    const { data } = await listMailApiTokens(props.domainId)
    tokens.value = data || []
  } catch { /* ignore */ } finally {
    loading.value = false
  }
}

async function createToken() {
  saving.value = true
  try {
    const { data } = await createMailApiToken({ domain_id: props.domainId, name: tokenName.value })
    tokens.value.unshift(data.key)
    tokenName.value = ''
    await ElMessageBox.alert(
      `令牌（仅显示一次，请立即复制保存）：\n\n${data.token}`,
      '创建成功',
      { confirmButtonText: '我已保存' }
    )
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(errMsg(e))
  } finally {
    saving.value = false
  }
}

async function toggleToken(row) {
  try {
    await setMailApiTokenEnabled(row.id, !row.enabled)
    loadTokens()
  } catch (e) {
    ElMessage.error(errMsg(e))
  }
}

async function removeToken(row) {
  try {
    await ElMessageBox.confirm('删除后使用该令牌的第三方系统将无法调用，确定？', '提示', { type: 'warning' })
  } catch { return }
  try {
    await deleteMailApiToken(row.id)
    loadTokens()
  } catch (e) {
    ElMessage.error(errMsg(e))
  }
}

async function loadStatus() {
  try {
    const { data } = await getMailSmtpStatus()
    smtpStatus.value = data || {}
  } catch { /* ignore */ }
  if (props.domainId) {
    try {
      const { data } = await getMailDomainDkim(props.domainId)
      dkim.value = { generated: !!data?.generated, host: data?.host || '' }
    } catch { /* ignore */ }
  }
}

function loadAll() {
  loadAccounts()
  loadAliases()
  loadOutbox()
  loadLogs()
  loadTokens()
  loadStatus()
}

watch(tab, (v) => {
  if (v === 'token' || v === 'status') loadTokens()
  if (v === 'status') loadStatus()
  if (v === 'logs') loadLogs()
  if (v === 'outbox') loadOutbox()
})
watch(() => props.domainId, () => {
  policyAccountId.value = 0
  loadAll()
})

onMounted(loadAll)
</script>

<style scoped>
.ops-wrap { padding: 4px 2px 12px; }
.ops-toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-bottom: 10px; }
.ops-at { color: #6b7280; font-size: 13px; }
.ops-form { max-width: 720px; }
.ops-section-title { font-size: 13px; font-weight: 600; color: #303133; margin: 16px 0 8px; }
.ops-section-title:first-child { margin-top: 4px; }
.ops-row { display: flex; align-items: center; gap: 10px; margin-bottom: 10px; }
.ops-row-top { align-items: flex-start; }
.ops-label { width: 76px; flex: 0 0 auto; color: #606266; font-size: 13px; }
.ops-hint { color: #909399; font-size: 12px; line-height: 1.6; }
.ops-hint-block { display: block; margin: 8px 0; background: #f8fafc; border: 1px solid #eef2f7; border-radius: 6px; padding: 8px 10px; }
.ops-hint code { background: #eef2f7; padding: 0 4px; border-radius: 3px; }
.ops-usage { margin-bottom: 6px; }
.ops-usage-text { display: inline-block; margin-top: 4px; font-size: 12px; color: #909399; }
.ops-actions { margin-top: 18px; }
.ops-empty { color: #909399; font-size: 13px; padding: 24px 0; text-align: center; }
.ops-stats { display: flex; flex-wrap: wrap; gap: 14px; font-size: 12px; color: #606266; margin-bottom: 8px; }
.ops-stat-bad { color: #f56c6c; }
.ops-status-list { max-width: 720px; }
.ops-status-item { display: flex; align-items: center; gap: 12px; padding: 10px 0; border-bottom: 1px dashed #eef2f7; }
.ops-status-label { width: 240px; flex: 0 0 auto; color: #606266; font-size: 13px; }
.ops-status-value { display: flex; align-items: center; gap: 8px; font-size: 13px; color: #303133; word-break: break-all; }
@media (max-width: 768px) {
  .ops-label { width: 60px; }
  .ops-status-label { width: 130px; }
  .ops-row { flex-wrap: wrap; }
}
</style>
