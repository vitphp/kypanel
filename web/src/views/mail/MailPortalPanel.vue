<template>
  <div v-loading="loading" class="mp-body">
    <!-- 开关区：开启网站 / 开放注册（同一行） -->
    <div class="mp-switches">
      <div class="mp-switch">
        <div class="mp-switch-txt">
          <div class="mp-switch-title">开启网站</div>
          <div class="mp-switch-sub">关闭后停止对外访问，配置保留</div>
        </div>
        <el-switch v-model="form.enabled" />
      </div>
      <div class="mp-switch">
        <div class="mp-switch-txt">
          <div class="mp-switch-title">开放注册</div>
          <div class="mp-switch-sub">允许访客自助注册邮箱账号</div>
        </div>
        <el-switch v-model="form.register" :disabled="!form.enabled" />
      </div>
    </div>

    <div class="mp-grid">
      <div class="mp-field">
        <label>门户域名</label>
        <el-input v-model="form.portal_domain" placeholder="默认 mail.域名" :disabled="!form.enabled" />
      </div>
      <div class="mp-field">
        <label>附加绑定域名</label>
        <el-input v-model="form.extra_domains" placeholder="多个用逗号分隔，可留空" :disabled="!form.enabled" />
      </div>
    </div>

    <!-- HTTPS -->
    <div class="mp-switch">
      <div class="mp-switch-txt">
        <div class="mp-switch-title">开启 HTTPS</div>
        <div class="mp-switch-sub">保存时自动为下方勾选的域名申请免费证书</div>
      </div>
      <div class="mp-switch-ops">
        <el-button
          v-if="form.enabled && form.ssl"
          link
          type="primary"
          class="mp-cert-btn"
          @click="certOpen = !certOpen"
        >
          配置<el-icon v-if="!certOpen" class="mp-cert-btn-ic"><ArrowRight /></el-icon>
        </el-button>
        <el-switch v-model="form.ssl" :disabled="!form.enabled" />
      </div>
    </div>

    <!-- 证书设置（默认收起，点「配置」展开） -->
    <div v-if="form.enabled && form.ssl && certOpen" class="mp-cert">
      <div class="mp-cert-head">
        <span class="mp-cert-title">
          证书设置
          <el-tag v-if="cert.exists" :type="certTagType" size="small">已签发 · 剩余 {{ cert.days }} 天</el-tag>
          <el-tag v-else type="info" size="small">尚未申请证书</el-tag>
        </span>
        <el-button link type="primary" class="mp-cert-adv" @click="certAdvanced = !certAdvanced">
          {{ certAdvanced ? '收起' : '高级设置' }}
        </el-button>
      </div>

      <div v-if="certAdvanced" class="mp-cert-advbox">
        <div class="mp-grid">
          <div class="mp-field">
            <label>证书品牌</label>
            <el-select v-model="form.cert_brand" style="width: 100%">
              <el-option label="Let's Encrypt（推荐）" value="letsencrypt" />
              <el-option label="LiteSSL" value="litessl" />
            </el-select>
          </div>
          <div class="mp-field">
            <label>证书算法</label>
            <el-select v-model="form.cert_algo" style="width: 100%">
              <el-option label="RSA 2048（推荐）" value="rsa2048" />
              <el-option label="ECC 256" value="ecc256" />
            </el-select>
          </div>
        </div>
        <div class="mp-field">
          <label>通知邮箱</label>
          <el-input v-model="form.cert_email" placeholder="接收证书到期通知的邮箱（可选）" />
        </div>
        <el-alert
          v-if="form.cert_brand === 'litessl'"
          type="info"
          :closable="false"
          show-icon
          title="使用 LiteSSL 前请先在「面板设置 → 证书」中配置 EAB KID 与 EAB HMAC Key。"
        />
      </div>

      <div class="mp-field">
        <label>证书域名（可多选，一次签发同一张证书）</label>
        <div class="mp-cert-domains">
          <el-checkbox v-model="certAll" :indeterminate="certIndeterminate">全选</el-checkbox>
          <el-checkbox-group v-model="form.cert_domains" class="mp-cert-list">
            <el-checkbox v-for="d in certOptions" :key="d" :value="d">{{ d }}</el-checkbox>
          </el-checkbox-group>
          <span v-if="!certOptions.length" class="mp-hint">请先填写门户域名或附加绑定域名</span>
        </div>
      </div>

      <div class="mp-cert-actions">
        <el-button type="success" plain :loading="applying" :disabled="!form.cert_domains.length" @click="applyCert">
          立即申请 / 重新申请证书
        </el-button>
        <span class="mp-hint">申请前请确认域名已解析到本服务器，且 80 端口可访问</span>
      </div>
    </div>

    <div class="mp-grid">
      <div class="mp-field">
        <label>官网标题</label>
        <el-input v-model="form.title" placeholder="浏览器标签标题，默认取网站名称" :disabled="!form.enabled" />
      </div>
      <div class="mp-field">
        <label>网站名称</label>
        <el-input v-model="form.name" placeholder="显示在页面顶部，默认取邮箱域名" :disabled="!form.enabled" />
      </div>
    </div>

    <!-- Logo -->
    <div class="mp-field">
      <label>网站 Logo</label>
      <div class="mp-logo-row">
        <div class="mp-logo-preview">
          <img v-if="form.logo && !logoBroken" :src="form.logo" alt="logo" @error="logoBroken = true" />
          <span v-else class="mp-logo-ph">{{ logoBroken ? '图片失效' : '未上传' }}</span>
        </div>
        <div class="mp-logo-ops">
          <el-upload
            action="#"
            :show-file-list="false"
            :auto-upload="false"
            accept="image/png,image/jpeg,image/gif,image/webp,image/svg+xml,image/x-icon,image/bmp"
            :on-change="onLogoPick"
          >
            <el-button :disabled="!form.enabled" :loading="logoUploading">
              {{ form.logo ? '重新上传' : '上传 Logo' }}
            </el-button>
          </el-upload>
          <el-button v-if="form.logo" link type="danger" :disabled="!form.enabled" @click="clearLogo">移除</el-button>
        </div>
      </div>
      <span v-if="logoBroken" class="mp-hint mp-warn">已保存的 Logo 无法加载（地址已失效），请重新上传或点「移除」</span>
      <span v-else class="mp-hint">仅支持上传图片（png / jpg / gif / webp / svg / ico / bmp，≤2MB）；不上传则显示站点名首字母徽标</span>
    </div>

    <div class="mp-field">
      <label>底部版权</label>
      <el-input v-model="form.footer" placeholder="如 © 2026 公司名称，留空自动生成" :disabled="!form.enabled" />
    </div>

    <div v-if="form.enabled && siteRoot" class="mp-dir">
      附件存放目录：<code>{{ siteRoot }}/files/&lt;邮箱账号&gt;</code>
      <span class="mp-hint">（webmail 中「我的文件」上传的附件保存在此目录，该目录已禁止直接访问，仅账号登录后可读写）</span>
    </div>

    <div class="mp-actions">
      <el-button type="primary" :loading="saving" @click="save">
        {{ saving && form.enabled && form.ssl && !cert.exists ? '保存并申请证书中…' : '保存门户设置' }}
      </el-button>
      <span v-if="form.enabled && previewUrl" class="mp-url">
        访问地址：<a :href="previewUrl" target="_blank" rel="noopener">{{ previewUrl }}</a>
      </span>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { getMailPortal, saveMailPortal, uploadMailPortalLogo, applyMailPortalCert } from '../../api/mail'

const props = defineProps({
  domain: { type: Object, default: null }
})
const emit = defineEmits(['saved'])

const loading = ref(false)
const saving = ref(false)
const applying = ref(false)
const certAdvanced = ref(false)
const certOpen = ref(false)
const logoUploading = ref(false)
const cert = ref({ exists: false, days: 0 })
const siteRoot = ref('')
const logoBroken = ref(false)
const form = ref({
  enabled: false,
  portal_domain: '',
  extra_domains: '',
  ssl: false,
  title: '',
  name: '',
  logo: '',
  footer: '',
  register: false,
  cert_brand: 'letsencrypt',
  cert_algo: 'rsa2048',
  cert_email: '',
  cert_domains: []
})

const previewUrl = computed(() => {
  if (!form.value.portal_domain) return ''
  return (form.value.ssl ? 'https://' : 'http://') + form.value.portal_domain
})

// 证书可选域名：门户域名 + 附加绑定域名（随输入实时更新）
const certOptions = computed(() => {
  const out = []
  const seen = new Set()
  const add = (s) => {
    const d = String(s || '').trim().toLowerCase().replace(/^\.+|\.+$/g, '')
    if (d && !seen.has(d)) { seen.add(d); out.push(d) }
  }
  add(form.value.portal_domain)
  ;(form.value.extra_domains || '').split(/[,，;；\s\n\r]+/).forEach(add)
  return out
})

const certIndeterminate = computed(() =>
  form.value.cert_domains.length > 0 && form.value.cert_domains.length < certOptions.value.length
)
const certAll = computed({
  get: () => certOptions.value.length > 0 && form.value.cert_domains.length === certOptions.value.length,
  set: (v) => { form.value.cert_domains = v ? [...certOptions.value] : [] }
})

const certTagType = computed(() => {
  const d = cert.value.days
  if (d <= 7) return 'danger'
  if (d <= 30) return 'warning'
  return 'success'
})

// Logo 地址变化后重新判定图片是否可加载
watch(() => form.value.logo, () => { logoBroken.value = false })

// 关闭 HTTPS 时收起证书配置
watch(() => form.value.ssl, (v) => { if (!v) certOpen.value = false })

// 门户/附加域名变化时，剔除已失效的勾选；若全部失效则回退为全选
watch(certOptions, (opts) => {
  const keep = form.value.cert_domains.filter((d) => opts.includes(d))
  form.value.cert_domains = keep.length ? keep : [...opts]
})

// 把后端返回的门户视图套用到表单（保存/申请证书后刷新用）
function applyView(d) {
  const v = d || {}
  cert.value = { exists: !!v.cert_exists, days: v.cert_days || 0 }
  siteRoot.value = v.site_root || ''
  const pd = v.portal_domain || ('mail.' + (props.domain?.domain || ''))
  const extras = v.extra_domains || ''
  // 依据「即将写入表单」的域名算出可选域名（不能依赖 certOptions，它读的是旧表单值）
  const opts = []
  const seen = new Set()
  const add = (s) => {
    const x = String(s || '').trim().toLowerCase().replace(/^\.+|\.+$/g, '')
    if (x && !seen.has(x)) { seen.add(x); opts.push(x) }
  }
  add(pd)
  extras.split(/[,，;；\s\n\r]+/).forEach(add)
  const picked = (Array.isArray(v.cert_domains) ? v.cert_domains : []).filter((x) => opts.includes(x))
  form.value = {
    enabled: !!v.enabled,
    portal_domain: pd,
    extra_domains: extras,
    ssl: !!v.ssl,
    title: v.title || '',
    name: v.name || '',
    logo: v.logo || '',
    footer: v.footer || '',
    register: !!v.register,
    cert_brand: v.cert_brand || 'letsencrypt',
    cert_algo: v.cert_algo || 'rsa2048',
    cert_email: v.cert_email || '',
    cert_domains: picked.length ? picked : opts
  }
}

// 读取门户配置。注意：request 拦截器返回的是 { code, msg, data } 整体，
// 业务数据在 data 字段里，必须解构出来，否则 enabled 恒为 undefined（表现为“保存后再次打开仍是关闭”）。
async function load() {
  if (!props.domain?.id) return
  loading.value = true
  try {
    const { data } = await getMailPortal(props.domain.id)
    applyView(data || {})
  } catch (e) {
    ElMessage.error(e?.message || '读取门户配置失败')
  } finally {
    loading.value = false
  }
}

// 选择 Logo 图片后立即上传，返回可引用的地址并回填
async function onLogoPick(uploadFile) {
  const raw = uploadFile?.raw
  if (!raw || !props.domain?.id) return
  if (raw.size > 2 * 1024 * 1024) {
    ElMessage.warning('Logo 文件不能超过 2MB')
    return
  }
  const fd = new FormData()
  fd.append('file', raw)
  logoUploading.value = true
  try {
    const { data } = await uploadMailPortalLogo(props.domain.id, fd)
    form.value.logo = data?.url || ''
    ElMessage.success('Logo 上传成功，保存后生效')
  } catch (e) {
    ElMessage.error(e?.message || 'Logo 上传失败')
  } finally {
    logoUploading.value = false
  }
}

function clearLogo() {
  form.value.logo = ''
}

// 单独申请 / 重新申请证书（用于已有证书但想新增域名或更换品牌时）
async function applyCert() {
  if (!props.domain?.id) return
  if (!form.value.cert_domains.length) {
    return ElMessage.warning('请至少选择一个证书域名')
  }
  applying.value = true
  try {
    const { data } = await applyMailPortalCert(props.domain.id, {
      brand: form.value.cert_brand,
      algorithm: form.value.cert_algo,
      email: form.value.cert_email,
      domains: form.value.cert_domains
    })
    applyView(data || {})
    ElMessage.success('证书已签发，HTTPS 已启用')
    emit('saved')
  } catch (e) {
    ElMessage.error(e?.message || '证书申请失败')
  } finally {
    applying.value = false
  }
}

async function save() {
  if (!props.domain?.id) return
  if (form.value.enabled && !form.value.portal_domain.trim()) {
    return ElMessage.warning('请填写门户域名')
  }
  saving.value = true
  try {
    const { data } = await saveMailPortal(props.domain.id, {
      enabled: form.value.enabled,
      portal_domain: form.value.portal_domain,
      extra_domains: form.value.extra_domains,
      ssl: form.value.ssl,
      title: form.value.title,
      name: form.value.name,
      logo: form.value.logo,
      footer: form.value.footer,
      register: form.value.register,
      cert_brand: form.value.cert_brand,
      cert_algo: form.value.cert_algo,
      cert_email: form.value.cert_email,
      cert_domains: form.value.cert_domains
    })
    applyView(data || {})
    ElMessage.success(form.value.enabled ? '门户网站已保存并生效' : '门户网站已关闭')
    emit('saved')
  } catch (e) {
    ElMessage.error(e?.message || '保存失败')
  } finally {
    saving.value = false
  }
}

defineExpose({ load })
</script>

<style scoped>
.mp-body { display: flex; flex-direction: column; gap: 12px; }
.mp-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.mp-field { display: flex; flex-direction: column; gap: 6px; }
.mp-field label { font-size: 13px; font-weight: 600; color: #334155; }
.mp-hint { font-size: 12px; color: #94a3b8; }
.mp-warn { color: #d97706; }
.mp-switches { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.mp-switch { display: flex; align-items: center; justify-content: space-between; gap: 12px; background: #f8fafc; border: 1px solid #eef2f7; border-radius: 10px; padding: 10px 14px; }
.mp-switch-txt { min-width: 0; }
.mp-switch-ops { display: flex; align-items: center; gap: 10px; flex: 0 0 auto; }
.mp-cert-btn { padding: 0; height: auto; font-size: 13px; font-weight: 600; }
.mp-cert-btn-ic { margin-left: 2px; font-size: 12px; }
.mp-switch-title { font-size: 14px; font-weight: 600; color: #1f2937; }
.mp-switch-sub { font-size: 12px; color: #94a3b8; margin-top: 2px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.mp-actions { display: flex; align-items: center; gap: 14px; flex-wrap: wrap; padding-top: 4px; }
@media (max-width: 560px) {
  .mp-grid, .mp-switches { grid-template-columns: 1fr; }
  .mp-switch-sub { white-space: normal; }
}
.mp-dir { font-size: 12.5px; color: #64748b; background: #f5f7fc; border: 1px solid #eef0f5; border-radius: 10px; padding: 10px 12px; line-height: 1.7; }
.mp-dir code { font-family: ui-monospace, Menlo, Consolas, monospace; color: #4f46e5; word-break: break-all; }
.mp-url { font-size: 13px; color: #475569; background: #f1f5ff; border-radius: 8px; padding: 10px 12px; }
.mp-url a { color: #4f46e5; font-weight: 600; }

/* 证书设置 */
.mp-cert { display: flex; flex-direction: column; gap: 12px; border: 1px solid #e6ecf5; background: #fbfcff; border-radius: 12px; padding: 14px; }
.mp-cert-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.mp-cert-title { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; font-size: 14px; font-weight: 700; color: #1f2937; }
.mp-cert-adv { padding: 0; height: auto; font-size: 12.5px; }
.mp-cert-advbox { display: flex; flex-direction: column; gap: 12px; padding: 12px; background: #fff; border: 1px solid #e6ecf5; border-radius: 10px; }
.mp-cert-domains { display: flex; flex-direction: column; gap: 8px; border: 1px solid #e2e5ec; border-radius: 10px; padding: 10px 12px; background: #fff; }
.mp-cert-list { display: flex; flex-direction: column; gap: 4px; }
.mp-cert-actions { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }

/* Logo */
.mp-logo-row { display: flex; align-items: center; gap: 14px; }
.mp-logo-preview { width: 72px; height: 72px; flex: 0 0 auto; border: 1px dashed #d5dbe6; border-radius: 10px; background: #fafbfe; display: flex; align-items: center; justify-content: center; overflow: hidden; }
.mp-logo-preview img { max-width: 100%; max-height: 100%; object-fit: contain; }
.mp-logo-ph { font-size: 12px; color: #b6bfcf; }
.mp-logo-ops { display: flex; align-items: center; gap: 8px; }
</style>
