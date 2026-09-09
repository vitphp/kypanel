<template>
  <div class="mail-domains">
    <!-- 顶部操作条 -->
    <div class="md-toolbar">
      <div class="md-toolbar-left">
        <span class="md-title">域名邮箱</span>
        <span class="md-subtitle">添加你要开通邮箱的域名，再按其 DNS 解析引导完成绑定</span>
      </div>
      <el-button type="primary" :icon="Plus" @click="openAdd">添加域名</el-button>
    </div>

    <!-- 域名列表 -->
    <el-card shadow="never" class="md-card">
      <el-table v-loading="loading" :data="list" style="width: 100%">
        <el-table-column prop="domain" label="域名" min-width="180">
          <template #default="{ row }">
            <div class="md-domain-cell">
              <span class="md-domain-name">{{ row.domain }}</span>
              <el-tag v-if="row.enabled" type="success" size="small" effect="light">启用</el-tag>
              <el-tag v-else type="info" size="small" effect="light">停用</el-tag>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="remark" label="备注" min-width="120" show-overflow-tooltip />
        <el-table-column label="DNS 绑定" min-width="160">
          <template #default="{ row }">
            <div class="md-dns-status">
              <span class="md-dns-chip" :class="{ ok: row.mx_configured }">MX{{ row.mx_configured ? '✓' : '' }}</span>
              <span class="md-dns-chip" :class="{ ok: row.spf_configured }">SPF{{ row.spf_configured ? '✓' : '' }}</span>
              <span class="md-dns-chip" :class="{ ok: row.dkim_configured }">DKIM{{ row.dkim_configured ? '✓' : '' }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="200" align="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openDns(row)">绑定/解析</el-button>
            <el-button link type="warning" size="small" @click="toggleEnabled(row)">
              {{ row.enabled ? '停用' : '启用' }}
            </el-button>
            <el-button link type="danger" size="small" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 添加域名 -->
    <el-dialog v-model="addVisible" title="添加邮箱域名" width="min(520px, 92vw)" align-center>
      <el-form label-width="90px">
        <el-form-item label="域名" required>
          <el-input v-model="addForm.domain" placeholder="例：example.com" @keyup.enter="submitAdd" />
          <div class="md-hint">填写你想开通邮箱的根域名（不带 www / @ 前缀）。</div>
        </el-form-item>
        <el-form-item label="单账号容量">
          <el-input-number v-model="addForm.quota" :min="1" :max="102400" style="width: 180px" />
          <span class="md-hint" style="margin-left: 8px">MB</span>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="addForm.remark" placeholder="可选" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="addVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="submitAdd">确认添加</el-button>
      </template>
    </el-dialog>

    <!-- DNS 绑定引导 -->
    <el-dialog v-model="dnsVisible" title="域名解析引导" width="min(680px, 94vw)" align-center>
      <template v-if="dnsGuide">
        <el-alert type="info" :closable="false" show-icon class="md-dns-alert"
          title="请到你的域名服务商（阿里云/腾讯云/Cloudflare 等）的『DNS 解析』里添加以下记录，把邮件收发指向这台服务器。"
          description="生效时间通常几分钟到数小时。MX 未生效时对方无法把信投到你的邮箱。" />
        <!-- 自动检测到的邮件服务器值 -->
        <div class="md-detected">
          <el-icon :size="16" class="md-detected-icon"><Connection /></el-icon>
          <span class="md-detected-text">面板已自动检测到你的邮件服务器：</span>
          <el-tag class="md-detected-tag" type="success" effect="light">{{ dnsGuide.mail_server }}</el-tag>
        </div>

        <div class="md-dns-list">
          <div v-for="(r, i) in dnsRows" :key="i" class="md-dns-row" :class="{ 'md-dns-row-opt': !r.required }">
            <div class="md-dns-row-head">
              <span class="md-dns-row-type" :class="r.type.toLowerCase()">{{ r.type }}</span>
              <div class="md-dns-row-copy">
                <div class="md-dns-row-line">
                  <span class="md-dns-row-label">主机记录 {{ r.host }}</span>
                  <el-tag v-if="r.required" size="small" type="danger" effect="plain">必须</el-tag>
                  <el-tag v-else size="small" type="info" effect="plain">后续可补</el-tag>
                </div>
                <div class="md-dns-row-line">
                  <span class="md-dns-row-value">{{ r.value }}</span>
                  <el-button link type="primary" size="small" :disabled="!r.required" @click="copy(r.value)">{{ r.required ? '复制值' : '暂不可用' }}</el-button>
                </div>
                <div class="md-dns-row-note">{{ r.note }}</div>
              </div>
            </div>
          </div>
        </div>

        <!-- 手动说明 -->
        <div class="md-dns-steps">
          <div class="md-steps-title">详细步骤</div>
          <div v-for="(n, i) in dnsGuide.notes" :key="i" class="md-step-line">{{ n }}</div>
        </div>

        <!-- 自检 + 勾选已完成 -->
        <div class="md-dns-checkbar">
          <el-button size="small" :loading="checking" @click="runDnsCheck">检测解析是否生效</el-button>
          <span v-if="checkResult !== null" class="md-check-result" :class="checkResult ? 'ok' : 'bad'">
            {{ checkResult ? '✓ 检测到 MX 记录，已生效' : '✗ 暂未检测到 MX（可能还在传播，请稍后重试）' }}
          </span>
        </div>
        <div class="md-dns-markbar">
          <el-checkbox v-model="dnsMark.mx" @change="saveDnsMark">我已配置 MX</el-checkbox>
          <el-checkbox v-model="dnsMark.spf" @change="saveDnsMark">我已配置 SPF</el-checkbox>
          <el-checkbox v-model="dnsMark.dkim" @change="saveDnsMark">我已配置 DKIM</el-checkbox>
          <el-checkbox v-model="dnsMark.dmarc" @change="saveDnsMark">我已配置 DMARC</el-checkbox>
        </div>
      </template>
      <template #footer>
        <el-button @click="dnsVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Connection } from '@element-plus/icons-vue'
import { listMailDomains, addMailDomain, updateMailDomain, deleteMailDomain, getMailDnsGuide, checkMailDns } from '../../api/mail'

const list = ref([])
const loading = ref(false)

// 添加
const addVisible = ref(false)
const saving = ref(false)
const addForm = ref({ domain: '', quota: 1024, remark: '' })

// DNS
const dnsVisible = ref(false)
const dnsGuide = ref(null)
const checking = ref(false)
const checkResult = ref(null)
const dnsMark = ref({ mx: false, spf: false, dkim: false, dmarc: false })
const currentDomain = ref(null)

// 把 guide 折叠成可复制的行，required=true 表示现在就必须配；false 表示后续可补
const dnsRows = computed(() => {
  const g = dnsGuide.value
  if (!g) return []
  return [
    { type: 'A', host: `mail.${g.domain}`, value: g.mail_server, required: true, note: '把 mail.你的域名 解析到本服务器' },
    { type: 'MX', host: '@', value: `${g.mx_value} (优先级 10)`, required: true, note: '把邮件路由到 mail.你的域名' },
    { type: 'TXT', host: '@', value: g.spf_value, required: true, note: '声明本服务器是唯一代发方（避免发出去进垃圾箱）' },
    { type: 'TXT', host: g.dkim_host, value: g.dkim_value, required: false, note: '签名功能上线后面板会自动给真实公钥' },
    { type: 'TXT', host: g.dmarc_host, value: g.dmarc_value, required: false, note: '建议配置；先按 SPF/DKIM 是否配齐来定' }
  ]
})

async function load() {
  loading.value = true
  try {
    const { data } = await listMailDomains()
    list.value = data || []
  } finally {
    loading.value = false
  }
}

function openAdd() {
  addForm.value = { domain: '', quota: 1024, remark: '' }
  addVisible.value = true
}

async function submitAdd() {
  const d = addForm.value.domain.trim()
  if (!d) return ElMessage.warning('请输入域名')
  saving.value = true
  try {
    await addMailDomain({ domain: d, quota: addForm.value.quota, remark: addForm.value.remark })
    ElMessage.success('域名已添加')
    addVisible.value = false
    load()
  } finally {
    saving.value = false
  }
}

async function toggleEnabled(row) {
  try {
    await updateMailDomain(row.id, { enabled: !row.enabled })
    ElMessage.success(row.enabled ? '已停用' : '已启用')
    load()
  } catch (e) {
    ElMessage.error(e?.response?.data?.msg || '操作失败')
  }
}

async function remove(row) {
  try {
    await ElMessageBox.confirm(`确定删除域名「${row.domain}」吗？`, '删除', { type: 'warning' })
  } catch { return }
  try {
    await deleteMailDomain(row.id)
    ElMessage.success('已删除')
    load()
  } catch (e) {
    ElMessage.error(e?.response?.data?.msg || '删除失败')
  }
}

async function openDns(row) {
  currentDomain.value = row
  dnsMark.value = { mx: row.mx_configured, spf: row.spf_configured, dkim: row.dkim_configured, dmarc: row.dmarc_configured }
  checkResult.value = null
  dnsVisible.value = true
  try {
    const { data } = await getMailDnsGuide(row.id)
    dnsGuide.value = data
  } catch (e) {
    ElMessage.error(e?.response?.data?.msg || '加载引导失败')
    dnsVisible.value = false
  }
}

async function runDnsCheck() {
  checking.value = true
  try {
    const { data } = await checkMailDns(currentDomain.value.id)
    checkResult.value = !!data?.ready
  } catch (e) {
    ElMessage.error(e?.response?.data?.msg || '检测失败')
  } finally {
    checking.value = false
  }
}

async function saveDnsMark() {
  if (!currentDomain.value) return
  try {
    await updateMailDomain(currentDomain.value.id, {
      mx_configured: dnsMark.value.mx,
      spf_configured: dnsMark.value.spf,
      dkim_configured: dnsMark.value.dkim,
      dmarc_configured: dnsMark.value.dmarc
    })
    const row = list.value.find((x) => x.id === currentDomain.value.id)
    if (row) {
      row.mx_configured = dnsMark.value.mx
      row.spf_configured = dnsMark.value.spf
      row.dkim_configured = dnsMark.value.dkim
      row.dmarc_configured = dnsMark.value.dmarc
    }
  } catch (e) {
    ElMessage.error(e?.response?.data?.msg || '保存失败')
  }
}

async function copy(text) {
  try {
    await navigator.clipboard.writeText(text.replace(' (优先级 10)', ''))
    ElMessage.success('已复制')
  } catch {
    ElMessage.error('复制失败')
  }
}

onMounted(load)
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
.md-dns-status { display: flex; gap: 6px; flex-wrap: wrap; }
.md-dns-chip { font-size: 11px; padding: 2px 6px; border-radius: 4px; background: #f1f5f9; color: #94a3b8; }
.md-dns-chip.ok { background: #ecfdf5; color: #059669; }
.md-hint { font-size: 12px; color: #94a3b8; }
.md-dns-alert { margin-bottom: 12px; }
.md-dns-list { display: flex; flex-direction: column; gap: 8px; }
.md-dns-row { border: 1px solid #e2e8f0; border-radius: 8px; padding: 8px 12px; }
.md-dns-row-head { display: flex; align-items: center; gap: 10px; }
.md-dns-row-type { flex: 0 0 auto; font-size: 11px; font-weight: 700; color: #059669; background: #ecfdf5; border: 1px solid #a7f3d0; border-radius: 4px; padding: 1px 6px; }
.md-dns-row-copy { flex: 1; min-width: 0; display: flex; flex-direction: column; }
.md-dns-row-label { font-size: 12.5px; color: #475569; word-break: break-all; }
.md-dns-steps { margin-top: 14px; }
.md-steps-title { font-size: 13px; font-weight: 600; color: #0f172a; margin-bottom: 6px; }
.md-step-line { font-size: 12.5px; color: #64748b; line-height: 1.9; padding-left: 4px; }
.md-dns-checkbar { margin-top: 14px; display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.md-check-result { font-size: 13px; font-weight: 500; }
.md-check-result.ok { color: #059669; }
.md-check-result.bad { color: #f59e0b; }
.md-dns-markbar { margin-top: 12px; display: flex; gap: 16px; flex-wrap: wrap; }
/* 自动检测结果提示 */
.md-detected { display: flex; align-items: center; gap: 8px; padding: 10px 14px; margin: 8px 0 14px; background: #f0f9ff; border: 1px solid #bae6fd; border-radius: 8px; }
.md-detected-icon { color: #0284c7; }
.md-detected-text { font-size: 13px; color: #0f172a; }
.md-detected-tag { font-family: ui-monospace, SFMono-Regular, monospace; }
/* 行内多行布局 + 必填/可选样式 */
.md-dns-row-line { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.md-dns-row-value { font-family: ui-monospace, SFMono-Regular, monospace; font-size: 12.5px; color: #0f172a; word-break: break-all; }
.md-dns-row-note { font-size: 12px; color: #94a3b8; margin-top: 2px; }
.md-dns-row-opt { background: #f8fafc; border-style: dashed; }
.md-dns-row-opt .md-dns-row-value { color: #94a3b8; }
@media (max-width: 599px) {
  .md-toolbar { flex-direction: column; align-items: stretch; }
}
</style>
