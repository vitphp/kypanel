<template>
  <el-dialog
    :model-value="modelValue"
    :title="`SSL 证书 - ${site?.name || ''}`"
    width="680px"
    @update:model-value="$emit('update:modelValue', $event)"
    @opened="onOpen"
  >
    <el-radio-group v-model="mode" style="margin-bottom: 16px">
      <el-radio-button value="custom">配置 SSL 证书</el-radio-button>
      <el-radio-button value="apply">一键申请</el-radio-button>
    </el-radio-group>

    <!-- 自定义证书 -->
    <template v-if="mode === 'custom'">
      <el-form label-width="110px">
        <el-form-item label="当前状态">
          <el-tag v-if="site?.ssl_enabled" type="success">HTTPS 已开启</el-tag>
          <el-tag v-else type="info">HTTPS 未开启</el-tag>
        </el-form-item>
        <el-form-item label="PEM 证书">
          <div class="cert-field">
            <el-input v-model="customForm.cert" type="textarea" :rows="8" placeholder="-----BEGIN CERTIFICATE----- ..." />
            <div class="cert-copy">
              <el-button size="small" link type="primary" :disabled="!customForm.cert" @click="copyText(customForm.cert)">复制证书</el-button>
            </div>
          </div>
        </el-form-item>
        <el-form-item label="KEY 私钥">
          <div class="cert-field">
            <el-input v-model="customForm.key" type="textarea" :rows="8" placeholder="-----BEGIN PRIVATE KEY----- ..." />
            <div class="cert-copy">
              <el-button size="small" link type="primary" :disabled="!customForm.key" @click="copyText(customForm.key)">复制私钥</el-button>
            </div>
          </div>
        </el-form-item>
        <el-form-item label="启用 HTTPS">
          <el-switch v-model="customForm.enabled" />
        </el-form-item>
        <el-form-item v-if="customForm.enabled" label="强制 HTTPS">
          <el-switch v-model="customForm.force" />
        </el-form-item>
      </el-form>
      <div class="ssl-tip">如不填写证书/私钥，将保持现有证书不变；仅关闭 HTTPS 时填不填均可。</div>
    </template>

    <!-- 一键申请 -->
    <template v-else>
      <div class="ssl-apply-new">
        <span class="ssl-tip">为新域名申请 Let's Encrypt / LiteSSL 免费证书</span>
        <el-button type="success" :disabled="!certbotOk" @click="openApplyNew">申请新证书</el-button>
      </div>
      <el-table :data="certList" size="small" stripe style="margin-top: 16px" v-loading="certListLoading">
        <el-table-column prop="name" label="证书名" min-width="140" show-overflow-tooltip />
        <el-table-column label="域名" min-width="180">
          <template #default="{ row }">
            <el-tag v-for="d in row.domains.slice(0, 2)" :key="d" size="small" style="margin-right: 4px">{{ d }}</el-tag>
            <span v-if="row.domains.length > 2" style="color: #909399; font-size: 12px">+{{ row.domains.length - 2 }}</span>
          </template>
        </el-table-column>
        <el-table-column label="品牌" width="110">
          <template #default="{ row }">
            <el-tag v-if="row.brand === 'letsencrypt'" type="success" size="small">Let's Encrypt</el-tag>
            <el-tag v-else-if="row.brand === 'litessl'" type="primary" size="small">LiteSSL</el-tag>
            <el-tag v-else type="info" size="small">{{ row.brand || '自定义' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="剩余天数" width="90" align="center">
          <template #default="{ row }">
            <el-tag :type="row.days <= 7 ? 'danger' : row.days <= 30 ? 'warning' : 'success'" size="small">{{ row.days }} 天</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="200" align="center" fixed="right">
          <template #default="{ row }">
            <el-button size="small" link type="primary" @click="deployCert(row)">部署</el-button>
            <el-button size="small" link type="primary" @click="downloadCert(row)">下载</el-button>
            <el-button size="small" link type="danger" @click="deleteCert(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </template>

    <template #footer>
      <el-button @click="$emit('update:modelValue', false)">关闭</el-button>
      <el-button v-if="mode === 'custom'" type="primary" :loading="saving" @click="saveCustom">保存</el-button>
    </template>
  </el-dialog>

  <!-- 申请新证书弹窗 -->
  <el-dialog v-model="applyNewVisible" title="申请免费证书" width="560px" append-to-body>
    <el-form label-width="100px" class="ssl-apply-form">
      <el-form-item label="品牌">
        <el-radio-group v-model="applyForm.brand">
          <el-radio-button value="letsencrypt">Let's Encrypt</el-radio-button>
          <el-radio-button value="litessl">LiteSSL</el-radio-button>
        </el-radio-group>
      </el-form-item>
      <el-form-item label="证书算法">
        <el-select v-model="applyForm.algorithm" style="width: 100%">
          <el-option label="RSA 2048" value="rsa2048" />
          <el-option label="ECC 256" value="ecc256" />
        </el-select>
      </el-form-item>
      <el-form-item label="验证方法">
        <el-radio-group v-model="applyForm.method">
          <el-radio-button value="webroot">文件验证</el-radio-button>
          <el-radio-button value="dns">DNS验证(支持通配符)</el-radio-button>
        </el-radio-group>
      </el-form-item>
      <el-form-item label="通知邮箱">
        <el-input v-model="applyForm.email" placeholder="接收证书过期通知的邮箱（可选）" />
      </el-form-item>
      <el-form-item label="域名" style="align-items: flex-start">
        <div class="ssl-domain-select">
          <el-checkbox v-model="applyAllDomains" @change="toggleSelectAll">全选</el-checkbox>
          <el-checkbox-group v-model="applyForm.domains" class="ssl-domain-list">
            <el-checkbox
              v-for="d in domainOptions"
              :key="d"
              :value="d"
              :disabled="d.startsWith('*.') && applyForm.method !== 'dns'"
            >
              {{ d }}
            </el-checkbox>
          </el-checkbox-group>
        </div>
      </el-form-item>
      <el-alert
        v-if="applyForm.brand === 'litessl'"
        type="info"
        :closable="false"
        show-icon
        title="使用 LiteSSL 前请先在「面板设置 → 证书」中配置 EAB KID 和 EAB HMAC Key。"
        style="margin-bottom: 12px"
      />
    </el-form>
    <template #footer>
      <el-button @click="applyNewVisible = false">取消</el-button>
      <el-button type="primary" :loading="applying" :disabled="applyForm.domains.length === 0" @click="submitApply">申请证书</el-button>
    </template>
  </el-dialog>

  <!-- DNS 验证引导弹窗 -->
  <el-dialog
    v-model="dnsVisible"
    title="DNS 验证 - 添加解析记录"
    width="680px"
    append-to-body
    :close-on-click-modal="false"
  >
    <el-steps :active="dnsStep" finish-status="success" align-center style="margin-bottom: 16px">
      <el-step title="添加 TXT 记录" />
      <el-step title="等待解析生效" />
      <el-step title="完成签发" />
    </el-steps>

    <el-alert
      type="warning"
      :closable="false"
      show-icon
      title="请到你的域名解析服务商（阿里云 / 腾讯云 / DNSPod / Cloudflare 等）为以下域名添加 TXT 记录，主机记录填左侧值，记录类型选 TXT。"
      style="margin-bottom: 12px"
    />

    <el-table :data="dnsRecords" size="small" stripe>
      <el-table-column label="主机记录 / 记录类型" min-width="230">
        <template #default="{ row }">
          <div class="dns-host">{{ row.host }}</div>
          <div class="dns-zone">TXT · 属于 {{ row.zone }}</div>
        </template>
      </el-table-column>
      <el-table-column prop="value" label="记录值" min-width="230" show-overflow-tooltip />
      <el-table-column label="状态" width="80" align="center">
        <template #default="{ row }">
          <el-tag v-if="row.ok" type="success" size="small">已生效</el-tag>
          <el-tag v-else type="info" size="small">待生效</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="复制" width="70" align="center">
        <template #default="{ row }">
          <el-button size="small" link type="primary" @click="copyText(row.value)">复制</el-button>
        </template>
      </el-table-column>
    </el-table>

    <div class="ssl-tip" style="margin-top: 10px">添加完成后一般 1~5 分钟生效。点击「检查解析状态」可确认记录是否已生效，确认无误后再点击「继续申请」完成签发。</div>

    <template #footer>
      <el-button @click="dnsVisible = false">取消</el-button>
      <el-button :loading="checking" @click="checkDNS">检查解析状态</el-button>
      <el-button type="primary" :loading="completing" :disabled="!dnsAllOk" @click="completeDNS">我已解析完成，继续申请</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import request from '../utils/request'

const props = defineProps({
  modelValue: Boolean,
  site: { type: Object, default: () => ({}) }
})
const emit = defineEmits(['update:modelValue', 'success'])

const mode = ref('custom')
const saving = ref(false)
const applying = ref(false)
const certbotOk = ref(false)
const certList = ref([])
const certListLoading = ref(false)

const customForm = ref({
  enabled: false,
  force: false,
  cert: '',
  key: ''
})

const applyNewVisible = ref(false)
const applyForm = ref({
  brand: 'letsencrypt',
  method: 'webroot',
  algorithm: 'rsa2048',
  domains: [],
  email: ''
})
const applyAllDomains = ref(false)

const dnsVisible = ref(false)
const dnsStep = ref(1)
const dnsTaskId = ref('')
const dnsRecords = ref([])
const checking = ref(false)
const completing = ref(false)
const dnsAllOk = computed(() => dnsRecords.value.length > 0 && dnsRecords.value.every(r => r.ok))

const domainOptions = computed(() => {
  const list = []
  const add = d => {
    d = (d || '').trim()
    if (d && !list.includes(d)) list.push(d)
  }
  add(props.site?.domain)
  const extra = props.site?.domains
  if (typeof extra === 'string' && extra) {
    extra.split(',').forEach(add)
  } else if (Array.isArray(extra)) {
    extra.forEach(add)
  }
  return list
})

watch(() => props.modelValue, (v) => {
  if (v) {
    mode.value = 'custom'
    customForm.value.enabled = !!props.site?.ssl_enabled
    customForm.value.force = !!props.site?.ssl_force
    customForm.value.cert = ''
    customForm.value.key = ''
    loadCurrentSSL()
  }
})

async function loadCurrentSSL() {
  if (!props.site?.ssl_enabled) return
  try {
    const res = await request.get('/site/ssl/cert/download', { params: { site_id: props.site.id } })
    customForm.value.cert = res.data?.cert || ''
    customForm.value.key = res.data?.key || ''
  } catch (e) {
    // 证书文件不存在/未部署时保持空值，让用户手动填写
  }
}

function onOpen() {
  loadCertbot()
  loadCertList()
}

async function loadCertbot() {
  try {
    const res = await request.get('/site/ssl/certbot')
    certbotOk.value = res.data?.installed || false
  } catch (e) { certbotOk.value = false }
}

async function loadCertList() {
  certListLoading.value = true
  try {
    const res = await request.get('/site/ssl/certs')
    certList.value = res.data || []
  } catch (e) {
    certList.value = []
  } finally {
    certListLoading.value = false
  }
}

async function deployCert(row) {
  try {
    const res = await request.post('/site/ssl/apply-cert', {
      id: props.site.id,
      cert_name: row.name
    })
    // 部署成功后切到"配置 SSL 证书" tab 并回填证书/私钥内容，让用户直观看到当前部署的证书
    customForm.value.cert = res.data?.cert || ''
    customForm.value.key = res.data?.key || ''
    customForm.value.enabled = true
    customForm.value.force = false
    mode.value = 'custom'
    ElMessage.success('证书已部署到当前站点')
    emit('success')
  } catch (e) {
    ElMessage.error(e.response?.data?.message || '部署失败')
  }
}

async function downloadCert(row) {
  try {
    const res = await request.get('/site/ssl/cert/download', { params: { id: row.id } })
    const cert = res.data?.cert || ''
    const key = res.data?.key || ''
    const blob = new Blob([cert + '\n' + key], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${row.name}_ssl.pem`
    a.click()
    URL.revokeObjectURL(url)
    ElMessage.success('下载成功')
  } catch (e) {
    ElMessage.error(e.response?.data?.message || '下载失败')
  }
}

async function deleteCert(row) {
  try {
    await ElMessageBox.confirm(`确定删除证书「${row.name}」吗？`, '提示', { type: 'warning' })
    await request.post('/site/ssl/cert/delete', { id: row.id })
    ElMessage.success('删除成功')
    await loadCertList()
  } catch (e) {
    if (e !== 'cancel') {
      ElMessage.error(e.response?.data?.message || '删除失败')
    }
  }
}

async function saveCustom() {
  saving.value = true
  try {
    await request.post('/site/settings/ssl', {
      id: props.site.id,
      enabled: customForm.value.enabled,
      force: customForm.value.force,
      cert: customForm.value.cert,
      key: customForm.value.key
    })
    ElMessage.success('保存成功')
    emit('success')
    emit('update:modelValue', false)
  } finally {
    saving.value = false
  }
}

function openApplyNew() {
  applyForm.value = {
    brand: 'letsencrypt',
    method: 'webroot',
    algorithm: 'rsa2048',
    domains: domainOptions.value.filter(d => !d.startsWith('*.')),
    email: ''
  }
  applyAllDomains.value = false
  applyNewVisible.value = true
}

function toggleSelectAll(val) {
  applyForm.value.domains = val
    ? domainOptions.value.filter(d => !(d.startsWith('*.') && applyForm.value.method !== 'dns'))
    : []
}

watch(() => applyForm.value.method, (m) => {
  if (m !== 'dns') {
    applyForm.value.domains = applyForm.value.domains.filter(d => !d.startsWith('*.'))
  }
})

watch(() => applyForm.value.domains, (list) => {
  if (list.some(d => d.startsWith('*.'))) {
    applyForm.value.method = 'dns'
  }
})

async function submitApply() {
  if (applyForm.value.method === 'dns') {
    applying.value = true
    try {
      const res = await request.post('/site/ssl/dns-start', {
        id: props.site.id,
        ...applyForm.value
      })
      dnsTaskId.value = res.data?.task_id || ''
      dnsRecords.value = (res.data?.records || []).map(r => ({ ...r, ok: false }))
      dnsStep.value = 1
      applyNewVisible.value = false
      dnsVisible.value = true
    } catch (e) {
    } finally {
      applying.value = false
    }
    return
  }
  applying.value = true
  try {
    const res = await request.post('/site/ssl/apply', {
      id: props.site.id,
      ...applyForm.value
    })
    // 申请+部署成功后切到"配置 SSL 证书" tab 并回填证书/私钥内容
    customForm.value.cert = res.data?.cert || ''
    customForm.value.key = res.data?.key || ''
    customForm.value.enabled = true
    customForm.value.force = false
    mode.value = 'custom'
    applyNewVisible.value = false
    ElMessage.success('证书已部署，HTTPS 已启用')
    emit('success')
  } finally {
    applying.value = false
  }
}

async function checkDNS() {
  checking.value = true
  try {
    const res = await request.post('/site/ssl/dns-check', { task_id: dnsTaskId.value })
    const records = res.data?.records || []
    dnsRecords.value = records
    if (res.data?.ready) {
      dnsStep.value = 2
      ElMessage.success('所有 TXT 记录均已生效，可以申请了')
    } else {
      const pending = records.filter(r => !r.ok).length
      ElMessage.warning(`还有 ${pending} 条记录未生效，请确认已添加且 DNS 传播完成后再试`)
    }
  } catch (e) {
  } finally {
    checking.value = false
  }
}

async function completeDNS() {
  completing.value = true
  try {
    const res = await request.post('/site/ssl/dns-complete', { task_id: dnsTaskId.value })
    dnsStep.value = 3
    ElMessage.success(res.data?.message || '申请成功并已启用 HTTPS')
    setTimeout(() => {
      dnsVisible.value = false
      emit('success')
      emit('update:modelValue', false)
    }, 800)
  } catch (e) {
  } finally {
    completing.value = false
  }
}

function copyText(text) {
  if (navigator.clipboard?.writeText) {
    navigator.clipboard.writeText(text)
      .then(() => ElMessage.success('已复制'))
      .catch(() => ElMessage.warning('复制失败，请手动选择复制'))
  } else {
    ElMessage.warning('当前浏览器不支持一键复制，请手动选择复制')
  }
}
</script>

<style scoped>
.cert-field {
  width: 100%;
}
.cert-copy {
  text-align: right;
  margin-top: 4px;
}
.ssl-tip {
  color: #606266;
  font-size: 13px;
  margin-bottom: 12px;
}
.ssl-tip.warn {
  color: #e6a23c;
}
.ssl-tip code {
  background: #f4f4f5;
  padding: 2px 6px;
  border-radius: 4px;
}
.ssl-apply-new {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.ssl-domain-select {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.ssl-apply-form .ssl-domain-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.dns-host {
  font-family: monospace;
  color: #303133;
  word-break: break-all;
}
.dns-zone {
  font-size: 12px;
  color: #909399;
  margin-top: 2px;
}
</style>
