<template>
  <div class="mail-domains">
    <!-- 顶部操作条 -->
    <div class="md-toolbar">
      <div class="md-toolbar-left">
        <span class="md-title">域名邮箱</span>
        <span class="md-subtitle">添加一个域名 → 分步配好解析（面板会自动检测）→ 配好的才能添加用户</span>
      </div>
      <el-button type="primary" :icon="Plus" @click="openAdd">添加域名</el-button>
    </div>

    <!-- 域名列表 -->
    <el-card shadow="never" class="md-card">
      <el-table v-loading="loading" :data="list" style="width: 100%">
        <el-table-column prop="domain" label="域名" min-width="200">
          <template #default="{ row }">
            <div class="md-domain-cell">
              <span class="md-domain-name">{{ row.domain }}</span>
              <el-tag v-if="statusMap[row.id]?.ready" type="success" size="small" effect="light">已对接</el-tag>
              <el-tag v-else type="warning" size="small" effect="light" @click="checkOne(row)">未对接</el-tag>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="对接状态" min-width="220">
          <template #default="{ row }">
            <template v-if="statusMap[row.id]?.ready">
              <div class="md-status-ok">✓ 已正确指向本机，能收信，可添加用户</div>
            </template>
            <template v-else>
              <div class="md-status-bad" :title="statusMap[row.id]?.not_ready_msg || statusMap[row.id]?.detail || '还未检测'">
                <span>{{ statusMap[row.id]?.detail || statusMap[row.id]?.not_ready_msg || '检测中…' }}</span>
              </div>
              <el-button link type="primary" size="small" @click="openDnsGuide(row)">去配置解析</el-button>
            </template>
          </template>
        </el-table-column>
        <el-table-column prop="remark" label="备注" min-width="120" show-overflow-tooltip />
        <el-table-column label="操作" width="260" align="right">
          <template #default="{ row }">
            <el-button
              link
              type="primary"
              size="small"
              :disabled="!statusMap[row.id]?.ready"
              @click="openAccounts(row)"
            >
              添加用户
            </el-button>
            <el-button link type="warning" size="small" @click="toggleEnabled(row)">{{ row.enabled ? '停用' : '启用' }}</el-button>
            <el-button link type="danger" size="small" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- ===== 添加域名：3 步向导 ===== -->
    <el-dialog v-model="addVisible" :title="addStep === 1 ? '第 1 步 · 填写你的域名' : addStep === 2 ? '第 2 步 · 去域名服务商添加解析' : '第 3 步 · 检测对接结果'" width="min(640px, 94vw)" align-center :close-on-click-modal="false">
      <!-- 步骤指示器 -->
      <el-steps :active="addStep - 1" align-center finish-status="success" class="md-steps">
        <el-step title="填域名" />
        <el-step title="配解析" />
        <el-step title="自动检测" />
      </el-steps>

      <!-- STEP 1：填域名 -->
      <div v-if="addStep === 1" class="md-step-body">
        <div class="md-field-hint">填一个你想开通邮箱的域名（例如 yoursite.com）。</div>
        <el-input v-model="addForm.domain" placeholder="example.com" size="large" @keyup.enter="goAddStep2" />
        <div v-if="addErr" class="md-err">{{ addErr }}</div>
      </div>

      <!-- STEP 2：配置 -->
      <div v-else-if="addStep === 2" class="md-step-body">
        <div class="md-step2-tip">请到你的<span class="md-link">域名服务商</span>（阿里云/腾讯云/Cloudflare）后台「DNS 解析」里，添加下面 {{ guide.needTxt ? 3 : 2 }} 条记录，复制「值」粘进服务商对应框即可。</div>
        <div class="md-guide-rec">
          <div class="md-rec-head"><b>记录 1</b><span class="md-rec-why">让 mail.域名 能找到这台服务器（收信用）</span></div>
          <div v-for="(f, j) in recFields1" :key="j" class="md-rec-field"><span class="md-rec-label">{{ f.label }}</span><span class="md-rec-val">{{ f.value }}</span><el-button v-if="f.copiable" link type="primary" size="small" @click="copy(f.value)">复制</el-button></div>
        </div>
        <div class="md-guide-rec">
          <div class="md-rec-head"><b>记录 2</b><span class="md-rec-why">让全网把信投到这台服务器（收信用）</span></div>
          <div v-for="(f, j) in recFields2" :key="j" class="md-rec-field"><span class="md-rec-label">{{ f.label }}</span><span class="md-rec-val">{{ f.value }}</span><el-button v-if="f.copiable" link type="primary" size="small" @click="copy(f.value)">复制</el-button></div>
        </div>
        <div class="md-guide-rec">
          <div class="md-rec-head"><b>记录 3</b><span class="md-rec-why">让这台服务器能发信、不被当垃圾（发信用）</span></div>
          <div v-for="(f, j) in recFields3" :key="j" class="md-rec-field"><span class="md-rec-label">{{ f.label }}</span><span class="md-rec-val">{{ f.value }}</span><el-button v-if="f.copiable" link type="primary" size="small" @click="copy(f.value)">复制</el-button></div>
        </div>
        <div class="md-step2-foot">这 3 条都加到服务商后，点「我已添加完，下一步」。</div>
      </div>

      <!-- STEP 3：自动检测 -->
      <div v-else class="md-step-body">
        <div class="md-checking" :class="{ ok: addReady, checking: addChecking }">
          <template v-if="addChecking"><el-icon class="is-loading"><Loading /></el-icon> 正在检测你的解析是否生效…</template>
          <template v-else-if="addReady"><el-icon><CircleCheckFilled /></el-icon> 检测通过！</template>
          <template v-else><el-icon><WarningFilled /></el-icon> 还没检测通过</template>
        </div>
        <div class="md-check-detail">{{ addCheckDetail }}</div>
        <div v-if="addChecking" class="md-check-sub">每几秒自动重测。DNS 全球生效通常几分钟，若一直不通过请确认你确实在服务商后台添加了上面的记录。</div>
        <div v-if="!addReady && !addChecking" class="md-check-sub">可再等一会儿，DNS 仍在传播；面板会继续自动检测。</div>
      </div>

      <template #footer>
        <template v-if="addStep === 1">
          <el-button @click="addVisible = false">取消</el-button>
          <el-button type="primary" :loading="genGuideLoading" @click="goAddStep2">下一步</el-button>
        </template>
        <template v-else-if="addStep === 2">
          <el-button @click="addStep = 1">上一步</el-button>
          <el-button type="primary" @click="goAddStep3">我已添加完，下一步</el-button>
        </template>
        <template v-else>
          <el-button @click="addStep = 2">上一步（回去改配置）</el-button>
          <el-button type="primary" :disabled="!addReady" :loading="saving" @click="confirmAdd">检测通过，确认添加</el-button>
        </template>
      </template>
    </el-dialog>

    <!-- DNS 引导（用于列表里"未对接"时点开看配置，复用配置展示） -->
    <el-dialog v-model="dnsGuideVisible" title="去配置解析" width="min(620px, 92vw)" align-center>
      <div v-if="dnsGuideData" class="md-dnsguide-body">
        <div v-for="(f, j) in dnsGuideData" :key="j" class="md-rec-field"><span class="md-rec-label">{{ f.label }}</span><span class="md-rec-val">{{ f.value }}</span><el-button v-if="f.copiable" link type="primary" size="small" @click="copy(f.value)">复制</el-button></div>
      </div>
      <div class="md-dnsguide-foot">
        <el-button :loading="checking" @click="checkOne(currentRow)">再检测一次</el-button>
        <span v-if="currentRow && statusMap[currentRow.id]?.ready" class="md-status-ok" style="font-size:13px">✓ 已对接</span>
      </div>
    </el-dialog>

    <!-- ===== 添加用户 / 管理账号 ===== -->
    <el-dialog v-model="accountsVisible" :title="`账号管理 · ${currentDomainName}`" width="min(720px, 96vw)" align-center>
      <el-tabs v-model="accountTab">
        <!-- 单个添加 -->
        <el-tab-pane label="单个添加" name="single">
          <div class="acc-form">
            <div class="acc-row"><span class="acc-label">邮箱名</span><el-input v-model="accSingle.name" placeholder="如 admin（将创建 admin@域名）" style="max-width:360px" /></div>
            <div class="acc-row"><span class="acc-label">密码</span><el-input v-model="accSingle.password" type="password" show-password placeholder="登录密码" style="max-width:360px" /></div>
            <div class="acc-row"><span class="acc-label">容量(MB)</span><el-input-number v-model="accSingle.quota" :min="1" :max="102400" style="max-width:200px" /></div>
            <div class="acc-row acc-submit"><el-button type="primary" :loading="accBusy" @click="doAddOne">添加这个账号</el-button></div>
          </div>
        </el-tab-pane>
        <!-- 批量 -->
        <el-tab-pane label="批量添加" name="batch">
          <div class="acc-batch-tip">每行一个，格式：<code>邮箱名 密码</code>（或 <code>邮箱名:密码</code>）</div>
          <el-input v-model="accBatch.lines" type="textarea" :rows="6" placeholder="admin1 密码123&#10;admin2 密码456&#10;sales1:pass888" />
          <div class="acc-row acc-submit"><el-button type="primary" :loading="accBusy" @click="doAddBatch">批量添加</el-button></div>
        </el-tab-pane>
        <!-- 随机 -->
        <el-tab-pane label="随机生成" name="random">
          <div class="acc-rand-grid">
            <div class="acc-row"><span class="acc-label">前缀</span><el-input v-model="accRandom.prefix" placeholder="可选，如 vip" style="max-width:160px" /></div>
            <div class="acc-row"><span class="acc-label">随机部分位数</span><el-input-number v-model="accRandom.length" :min="1" :max="16" /></div>
            <div class="acc-row"><span class="acc-label">字符</span>
              <el-select v-model="accRandom.digit" style="width:140px">
                <el-option label="字母+数字" :value="0" />
                <el-option label="纯数字" :value="1" />
                <el-option label="纯字母" :value="2" />
              </el-select>
            </div>
            <div class="acc-row"><span class="acc-label">生成数量</span><el-input-number v-model="accRandom.count" :min="1" :max="200" /></div>
            <div class="acc-row"><span class="acc-label">统一密码</span><el-input v-model="accRandom.password" placeholder="留空则自动生成" style="max-width:200px" /></div>
            <div class="acc-row acc-submit"><el-button type="primary" :loading="accBusy" @click="doAddRandom">生成账号</el-button></div>
          </div>
        </el-tab-pane>
      </el-tabs>

      <!-- 生成结果 / 账号列表 -->
      <template v-if="accResult.length">
        <div class="acc-result-title">本次已生成（请复制保存）：</div>
        <div class="acc-result-box">
          <div v-for="(a, i) in accResult" :key="i" class="acc-result-line">{{ a.address }}<span v-if="a.password">　密码：{{ a.password }}</span></div>
        </div>
      </template>
      <div class="acc-result-title" style="margin-top:14px">该域名下已有账号（{{ accList.length }}）：</div>
      <el-table v-loading="accLoading" :data="accList" size="small" max-height="300">
        <el-table-column prop="address" label="邮箱地址" min-width="180" />
        <el-table-column label="状态" width="80"><template #default="{ row }"><el-tag :type="row.enabled ? 'success' : 'info'" size="small">{{ row.enabled ? '启用' : '停用' }}</el-tag></template></el-table-column>
        <el-table-column prop="quota_mb" label="容量MB" width="90" />
        <el-table-column label="操作" width="140" align="right">
          <template #default="{ row }">
            <el-button link type="warning" size="small" @click="toggleAcc(row)">{{ row.enabled ? '停用' : '启用' }}</el-button>
            <el-button link type="danger" size="small" @click="delAcc(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, onBeforeUnmount, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Loading, CircleCheckFilled, WarningFilled } from '@element-plus/icons-vue'
import {
  listMailDomains, addMailDomain, updateMailDomain, deleteMailDomain,
  getDomainGuide, checkDomainReady, checkMailDomainsReady,
  listMailAccounts, addMailAccount, addMailAccountsBatch, randomMailAccounts,
  deleteMailAccount, setMailAccountEnabled
} from '../../api/mail'

const list = ref([])
const loading = ref(false)
const statusMap = ref({}) // domainId -> {ready, detail}
const checking = ref(false)

// 添加向导
const addVisible = ref(false)
const addStep = ref(1)
const addErr = ref('')
const addForm = ref({ domain: '' })
const genGuideLoading = ref(false)
const guide = ref({ domain: '', mail_server: '', spf_value: '', mx_host_name: '' })
const addReady = ref(false)
const addChecking = ref(false)
const addCheckDetail = ref('')
const saving = ref(false)
let addCheckTimer = null

const recFields1 = computed(() => [
  { label: '记录类型', value: 'A' },
  { label: '主机记录', value: 'mail' },
  { label: '记录值', value: guide.value.mail_server, copiable: true }
])
const recFields2 = computed(() => [
  { label: '记录类型', value: 'MX' },
  { label: '主机记录', value: '@（留空）' },
  { label: '记录值', value: guide.value.mx_host_name, copiable: true },
  { label: '优先级', value: '10' }
])
const recFields3 = computed(() => [
  { label: '记录类型', value: 'TXT' },
  { label: '主机记录', value: '@（留空）' },
  { label: '记录值', value: guide.value.spf_value, copiable: true }
])

// DNS 引导（列表"未对接"查看）
const dnsGuideVisible = ref(false)
const dnsGuideData = ref([])
const currentRow = ref(null)

// 账号
const accountsVisible = ref(false)
const currentDomainName = ref('')
const accountTab = ref('single')
const accList = ref([])
const accLoading = ref(false)
const accBusy = ref(false)
const accResult = ref([])
const accSingle = ref({ name: '', password: '', quota: 1024 })
const accBatch = ref({ lines: '' })
const accRandom = ref({ prefix: '', length: 6, digit: 0, count: 10, password: '' })

async function load() {
  loading.value = true
  try {
    const { data } = await listMailDomains()
    list.value = data || []
    // 加载后自动检测所有域名对接状态
    await checkAll()
  } finally {
    loading.value = false
  }
}

async function checkAll() {
  const ids = list.value.map((x) => x.id)
  if (!ids.length) return
  try {
    const { data } = await checkMailDomainsReady(ids)
    if (data) {
      statusMap.value = {}
      for (const id of ids) {
        const s = data[id]
        if (s) statusMap.value[id] = { ready: !!s.ready, detail: s.not_ready_msg || '' }
      }
    }
  } catch { /* 忽略网络失败，保持原状 */ }
}

async function checkOneById(id) {
  try {
    const { data } = await checkMailDomainsReady([id])
    const s = data?.[id]
    if (s) statusMap.value[id] = { ready: !!s.ready, detail: s.not_ready_msg || '' }
  } catch { /* ignore */ }
}

async function checkOne(row) {
  checking.value = true
  try {
    await checkOneById(row.id)
  } finally {
    checking.value = false
  }
}

// 打开 DNS 引导（给"未对接"域名展示记录值，供去服务商配置）
async function openDnsGuide(row) {
  currentRow.value = row
  dnsGuideVisible.value = true
  await checkOne(row)
  let srv = ''
  try {
    const { data } = await getDomainGuide(row.domain)
    srv = data?.mail_server || ''
  } catch { /* ignore */ }
  if (!srv) srv = '（自动检测到的本机IP）'
  dnsGuideData.value = [
    { label: 'A 记录 · 记录值', value: srv, copiable: true },
    { label: 'MX 记录 · 记录值', value: 'mail.' + row.domain, copiable: true },
    { label: 'TXT(SPF) · 记录值', value: 'v=spf1 ip4:' + srv + ' ~all', copiable: true }
  ]
}

// ===== 添加向导 =====
function openAdd() {
  addForm.value = { domain: '' }
  addErr.value = ''
  addStep.value = 1
  addReady.value = false
  addVisible.value = true
}

async function goAddStep2() {
  const d = addForm.value.domain.trim().toLowerCase()
  if (!d) return (addErr.value = '请输入域名')
  if (!/^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$/.test(d)) {
    return (addErr.value = '域名格式不对，示例：example.com')
  }
  // 查重
  if (list.value.some((x) => x.domain === d)) {
    return (addErr.value = '这个域名已经在列表里了')
  }
  addErr.value = ''
  genGuideLoading.value = true
  try {
    const { data } = await getDomainGuide(d)
    guide.value = data || {}
    guide.value.domain = d
    addStep.value = 2
  } catch (e) {
    ElMessage.error(e?.response?.data?.msg || '获取配置失败')
  } finally {
    genGuideLoading.value = false
  }
}

function goAddStep3() {
  addStep.value = 3
  addReady.value = false
  addChecking.value = true
  addCheckDetail.value = ''
  runAutoCheck()
}

async function runAutoCheck() {
  const d = guide.value.domain
  if (!d) return
  try {
    const { data } = await checkDomainReady(d)
    if (data?.ready) {
      addReady.value = true
      addChecking.value = false
      addCheckDetail.value = data.detail || '检测通过'
      return
    }
    addCheckDetail.value = data?.detail || '还没检测通过'
  } catch (e) {
    addCheckDetail.value = ''
  }
  // 未通过：继续轮询
  if (addVisible.value && addStep.value === 3 && !addReady.value) {
    addCheckTimer = setTimeout(runAutoCheck, 4000)
  } else {
    addChecking.value = false
  }
}

async function confirmAdd() {
  if (!addReady.value) return
  saving.value = true
  try {
    const { data } = await addMailDomain({ domain: guide.value.domain })
    ElMessage.success('域名已添加并检测通过')
    addVisible.value = false
    if (addCheckTimer) clearTimeout(addCheckTimer)
    await load()
  } catch (e) {
    ElMessage.error(e?.response?.data?.msg || '添加失败')
  } finally {
    saving.value = false
  }
}

// ===== 域名其他操作 =====
async function toggleEnabled(row) {
  try {
    await updateMailDomain(row.id, { enabled: !row.enabled })
    load()
  } catch (e) { ElMessage.error(e?.response?.data?.msg || '操作失败') }
}
async function remove(row) {
  try { await ElMessageBox.confirm(`确定删除域名「${row.domain}」吗？`, '删除', { type: 'warning' }) } catch { return }
  try { await deleteMailDomain(row.id); ElMessage.success('已删除'); load() } catch (e) { ElMessage.error(e?.response?.data?.msg || '删除失败') }
}

// ===== 账号 =====
async function openAccounts(row) {
  currentDomainName.value = row.domain
  accountsVisible.value = true
  accountTab.value = 'single'
  accSingle.value = { name: '', password: '', quota: 1024 }
  accBatch.value = { lines: '' }
  accRandom.value = { prefix: '', length: 6, digit: 0, count: 10, password: '' }
  accResult.value = []
  currentDomainId = row.id
  await loadAccounts()
}
let currentDomainId = 0

async function loadAccounts() {
  accLoading.value = true
  try {
    const { data } = await listMailAccounts(currentDomainId)
    accList.value = data || []
  } finally { accLoading.value = false }
}

async function doAddOne() {
  const name = accSingle.value.name.trim()
  if (!name) return ElMessage.warning('请输入邮箱名')
  if (!accSingle.value.password) return ElMessage.warning('请输入密码')
  accBusy.value = true
  try {
    const { data } = await addMailAccount({ domain_id: currentDomainId, name, password: accSingle.value.password, quota: accSingle.value.quota })
    accResult.value = [{ address: data.address, password: accSingle.value.password }]
    accSingle.value.name = ''
    await loadAccounts()
  } catch (e) { ElMessage.error(e?.response?.data?.msg || '添加失败') } finally { accBusy.value = false }
}

async function doAddBatch() {
  accBusy.value = true
  try {
    const { data } = await addMailAccountsBatch({ domain_id: currentDomainId, lines: accBatch.value.lines })
    ElMessage.success(`成功 ${data.created} 个${data.failed?.length ? '，失败 ' + data.failed.length + ' 个' : ''}`)
    await loadAccounts()
  } catch (e) { ElMessage.error(e?.response?.data?.msg || '批量失败') } finally { accBusy.value = false }
}

async function doAddRandom() {
  accBusy.value = true
  try {
    const { data } = await randomMailAccounts({ domain_id: currentDomainId, ...accRandom.value })
    accResult.value = (data.accounts || []).map((a) => ({ address: a.name + '@' + currentDomainName.value, password: a.password }))
    if (data.failed?.length) ElMessage.warning(`失败 ${data.failed.length} 个`)
    await loadAccounts()
  } catch (e) { ElMessage.error(e?.response?.data?.msg || '生成失败') } finally { accBusy.value = false }
}

async function toggleAcc(row) {
  try { await setMailAccountEnabled(row.id, !row.enabled); await loadAccounts() } catch (e) { ElMessage.error('操作失败') }
}
async function delAcc(row) {
  try { await ElMessageBox.confirm(`确定删除账号 ${row.address} 吗？`, '删除', { type: 'warning' }) } catch { return }
  try { await deleteMailAccount(row.id); await loadAccounts() } catch (e) { ElMessage.error('删除失败') }
}

async function copy(text) {
  try { await navigator.clipboard.writeText(String(text || '')); ElMessage.success('已复制') } catch { ElMessage.error('复制失败') }
}

onMounted(load)
onBeforeUnmount(() => { if (addCheckTimer) clearTimeout(addCheckTimer) })
</script>

<style scoped>
.mail-domains { display: flex; flex-direction: column; gap: 14px; }
.md-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
.md-toolbar-left { min-width: 0; }
.md-title { font-size: 18px; font-weight: 700; color: #0f172a; }
.md-subtitle { font-size: 12.5px; color: #94a3b8; margin-left: 8px; }
.md-card { border-radius: 12px; }
.md-domain-cell { display: flex; align-items: center; gap: 8px; }
.md-domain-name { font-weight: 600; color: #0f172a; }
.md-status-ok { font-size: 12.5px; color: #059669; font-weight: 500; }
.md-status-bad { font-size: 12.5px; color: #d97706; line-height: 1.5; max-width: 100%; }
.md-steps { margin: 6px 0 20px; }
.md-step-body { min-height: 180px; }
.md-field-hint { font-size: 13px; color: #64748b; margin-bottom: 10px; }
.md-err { color: #dc2626; font-size: 13px; margin-top: 8px; }
.md-step2-tip { font-size: 13px; color: #334155; background: #eff6ff; border: 1px solid #bfdbfe; padding: 10px 12px; border-radius: 8px; margin-bottom: 14px; line-height: 1.6; }
.md-link { color: #2563eb; font-weight: 600; }
.md-guide-rec { border: 1px solid #e2e8f0; border-radius: 8px; padding: 10px 12px; margin-bottom: 10px; }
.md-rec-head { display: flex; align-items: center; gap: 8px; margin-bottom: 6px; }
.md-rec-why { font-size: 12.5px; color: #64748b; }
.md-rec-field { display: flex; align-items: center; gap: 8px; padding: 3px 0; font-size: 13px; }
.md-rec-label { flex: 0 0 84px; color: #64748b; }
.md-rec-val { flex: 1; color: #0f172a; font-family: ui-monospace, monospace; word-break: break-all; }
.md-step2-foot { font-size: 13px; color: #475569; margin-top: 6px; }
.md-checking { display: flex; align-items: center; gap: 8px; font-size: 16px; font-weight: 700; margin-bottom: 10px; }
.md-checking .el-icon { font-size: 20px; }
.md-checking.ok { color: #059669; }
.md-checking.checking { color: #2563eb; }
.md-checking:not(.ok):not(.checking) { color: #d97706; }
.md-check-detail { font-size: 13.5px; color: #475569; background: #f8fafc; border-radius: 8px; padding: 10px 12px; line-height: 1.7; }
.md-check-sub { font-size: 12.5px; color: #94a3b8; margin-top: 10px; }
.md-dnsguide-body .md-rec-field { border-bottom: 1px dashed #e2e8f0; padding: 8px 0; }
.md-dnsguide-foot { margin-top: 14px; display: flex; align-items: center; gap: 12px; }
.acc-form { display: flex; flex-direction: column; gap: 12px; max-width: 520px; }
.acc-row { display: flex; align-items: center; gap: 10px; }
.acc-label { flex: 0 0 90px; color: #475569; font-size: 13.5px; }
.acc-submit { margin-top: 6px; }
.acc-batch-tip { font-size: 13px; color: #64748b; margin-bottom: 8px; }
.acc-batch-tip code { background: #f1f5f9; padding: 1px 6px; border-radius: 4px; }
.acc-rand-grid { display: flex; flex-direction: column; gap: 12px; max-width: 520px; }
.acc-result-title { font-size: 13px; font-weight: 600; color: #0f172a; margin-top: 10px; }
.acc-result-box { background: #f0fdf4; border: 1px solid #bbf7d0; border-radius: 8px; padding: 10px 12px; max-height: 220px; overflow: auto; margin-top: 8px; }
.acc-result-line { font-size: 13px; color: #166534; padding: 3px 0; font-family: ui-monospace, monospace; }
@media (max-width: 599px) { .md-toolbar { flex-direction: column; align-items: stretch; } }
</style>
