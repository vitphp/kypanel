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

    <!-- DNS 绑定引导（小白版：不出现任何 DNS 黑话，每条都拆成服务商后台要填的字段） -->
    <el-dialog v-model="dnsVisible" title="把你的域名接到这台服务器" width="min(720px, 96vw)" align-center>
      <template v-if="dnsGuide">
        <!-- 用途选择：只收 / 只发 / 都要（只问一次用途，引导对应项） -->
        <el-radio-group v-model="dnsPurpose" class="md-purpose">
          <el-radio-button value="receive">只要收信</el-radio-button>
          <el-radio-button value="send">只要发信</el-radio-button>
          <el-radio-button value="both">收和发都要</el-radio-button>
        </el-radio-group>

        <!-- 按用途分组引导：每条都拆成服务商后台的字段 + 一键复制 -->
        <div v-for="(grp, gi) in dnsGroups" :key="gi" class="md-dns-group" :class="'md-group-' + grp.key">
          <div class="md-group-head">
            <span class="md-group-badge" :class="grp.key">{{ grp.badge }}</span>
            <span class="md-group-title">{{ grp.title }}</span>
          </div>

          <div class="md-dns-list">
            <div v-for="(r, i) in grp.rows" :key="i" class="md-dns-row">
              <div class="md-dns-row-head">
                <span class="md-dns-step-tag must">{{ r.num }}</span>
              </div>
              <div class="md-dns-title">{{ r.title }}</div>
              <div class="md-dns-fields-title">打开服务商后台 → 添加解析 → 按下面填：</div>
              <div v-for="(s, j) in r.steps" :key="j" class="md-dns-field-row">
                <span class="md-dns-field-label">{{ s.label }}</span>
                <span class="md-dns-field-value">{{ s.value }}</span>
              </div>
              <div class="md-dns-copybar">
                <el-button size="small" type="primary" plain @click="copy(r.steps[r.steps.length - 1].value)">
                  复制「记录值」
                </el-button>
                <span class="md-dns-copy-tip">点击后到服务商后台"记录值"框粘进去</span>
              </div>
            </div>
          </div>
        </div>

        <!-- 操作指引 + 自动检测状态（打开即检，不用按钮） -->
        <div class="md-how">
          <div class="md-how-title">怎么去服务商后台？</div>
          <ol class="md-how-list">
            <li>登录你买域名时的<span class="md-link">服务商</span>（阿里云 / 腾讯云 / Cloudflare 等）</li>
            <li>找到「<span class="md-link">DNS 解析</span> / <span class="md-link">域名解析</span>」菜单</li>
            <li>点「<span class="md-link">添加记录</span>」</li>
            <li>照着上面"打开服务商后台 → 添加解析 → 按下面填"里每一项的字段，一项一项填进去</li>
          </ol>
          <div class="md-checkbar-inline">
            <span v-if="checking" class="md-check-result">⏳ 正在检测你的解析是否生效…</span>
            <span v-else-if="checkResult === true" class="md-check-result ok">✓ 检测到了！你的域名已经能往本服务器收信（DNS 全球传播通常几分钟）</span>
            <span v-else-if="checkResult === false" class="md-check-result bad">⏳ 暂时还没检测到（可能你刚配完 DNS 还在传播，关闭弹窗稍等再打开会自动重查）</span>
            <span v-else class="md-check-result">检测中…</span>
          </div>
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
import { Plus } from '@element-plus/icons-vue'
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

// 用途：both(收+发) / receive(只收) / send(只发)。决定展示哪些 DNS 分组。
const dnsPurpose = ref('both')

// 按用途与后端引导分组成"收信组 / 发信组"两条，各自管一件"业务目标"。
// 每组里是小白视角的 DNS 项（steps 直接对应服务商后台的字段）。
const dnsGroups = computed(() => {
  const g = dnsGuide.value
  if (!g) return []
  const hostName = `mail.${g.domain}`
  const groups = []
  // 收信组：让人能往你的域名发信、你能收得到（A + MX）
  if (dnsPurpose.value !== 'send') {
    groups.push({
      key: 'receive',
      badge: '收信',
      title: '让全世界能把信投到你的邮箱（要"能收到信"才需要配）',
      rows: [
        {
          num: '收信 1/2',
          title: '先让 "mail.你的域名" 这个地址找到你这台服务器',
          why: '这是服务器的主机名地址，别人要凭它找到你的服务器',
          steps: [
            { label: '记录类型', value: 'A' },
            { label: '主机记录', value: 'mail' },
            { label: '记录值', value: g.mail_server }
          ]
        },
        {
          num: '收信 2/2',
          title: '告诉全网：发给 @你的域名 的信，请投到上面的地址',
          why: '没这条，别人发来的信根本到不了你这儿（会直接退回）',
          steps: [
            { label: '记录类型', value: 'MX' },
            { label: '主机记录', value: '@（留空）' },
            { label: '记录值', value: hostName },
            { label: '优先级', value: '10' }
          ]
        }
      ]
    })
  }
  // 发信组：让本域名能往外发信、不被对方当垃圾/伪造（当前仅 SPF 可配）
  if (dnsPurpose.value !== 'receive') {
    groups.push({
      key: 'send',
      badge: '发信',
      title: '让这台服务器能用你的域名发信（要"能往外发信"才需要配）',
      rows: [
        {
          num: '发信 1/1',
          title: '告诉全网：只有这台服务器可以用你的域名发信',
          why: '不配这条，从你服务器发出去的信很容易被收信方当"伪造邮件"丢进垃圾箱',
          steps: [
            { label: '记录类型', value: 'TXT' },
            { label: '主机记录', value: '@（留空）' },
            { label: '记录值', value: g.spf_value }
          ]
        }
      ]
    })
  }
  return groups
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
    const res = await addMailDomain({ domain: d, quota: addForm.value.quota, remark: addForm.value.remark })
    const rec = res.data || {}
    ElMessage.success('域名已添加')
    addVisible.value = false
    await load()
    // 添加后立刻引导用户去解析（用后端返回的记录做向导）
    openDns({
      id: rec.id,
      domain: d,
      mx_configured: false,
      spf_configured: false,
      dkim_configured: false,
      dmarc_configured: false
    })
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
  checking.value = false
  dnsVisible.value = true
  try {
    const { data } = await getMailDnsGuide(row.id)
    dnsGuide.value = data
  } catch (e) {
    ElMessage.error(e?.response?.data?.msg || '加载引导失败')
    dnsVisible.value = false
    return
  }
  // 弹窗打开后立即自动检测解析是否生效（用户不用点按钮）
  await runDnsCheck()
}

async function runDnsCheck() {
  checking.value = true
  try {
    const { data } = await checkMailDns(currentDomain.value.id)
    checkResult.value = !!data?.ready
  } catch (e) {
    // 后台自动检测：失败不打扰用户，仅标记"未生效"
    checkResult.value = false
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
    await navigator.clipboard.writeText(String(text || ''))
    ElMessage.success('已复制，去服务商后台粘贴即可')
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
/* ====== 小白版 DNS 引导样式 ====== */
/* 顶栏：为什么需要这一步 */
.md-why { font-size: 13.5px; color: #334155; background: #fffbeb; border: 1px solid #fde68a; border-radius: 10px; padding: 12px 14px; line-height: 1.7; margin-bottom: 12px; }
.md-why strong { color: #92400e; }
/* "服务商"等关键字眼 */
.md-link { color: #2563eb; font-weight: 600; background: #eff6ff; padding: 1px 6px; border-radius: 4px; }
/* 引导卡片 */
.md-dns-row { border: 1px solid #e2e8f0; border-radius: 10px; padding: 14px; background: #fff; }
.md-dns-row-opt { background: #fafafa; border-style: dashed; }
.md-dns-row-head { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.md-dns-step-tag { font-size: 12px; font-weight: 700; padding: 3px 10px; border-radius: 999px; }
.md-dns-step-tag.must { background: #fef2f2; color: #b91c1c; border: 1px solid #fecaca; }
.md-dns-step-tag.opt { background: #f1f5f9; color: #64748b; }
.md-dns-kind { font-size: 11px; padding: 1px 6px; border-radius: 4px; }
.md-dns-kind.must { background: #fee2e2; color: #dc2626; }
.md-dns-kind.opt { background: #e2e8f0; color: #64748b; }
.md-dns-title { font-size: 15px; font-weight: 700; color: #0f172a; margin-bottom: 4px; }
.md-dns-why { font-size: 12.5px; color: #64748b; margin-bottom: 10px; line-height: 1.6; }
/* 服务商后台的"记录类型 / 主机记录 / 记录值"等字段表格化 */
.md-dns-fields { background: #f8fafc; border: 1px solid #e2e8f0; border-radius: 8px; padding: 10px 12px; margin-bottom: 10px; }
.md-dns-fields-title { font-size: 12.5px; color: #475569; margin-bottom: 8px; font-weight: 600; }
.md-dns-field-row { display: flex; gap: 10px; padding: 5px 0; border-bottom: 1px dashed #e2e8f0; font-size: 13px; align-items: baseline; }
.md-dns-field-row:last-child { border-bottom: none; }
.md-dns-field-label { flex: 0 0 90px; color: #64748b; }
.md-dns-field-value { flex: 1; color: #0f172a; font-family: ui-monospace, SFMono-Regular, monospace; word-break: break-all; }
.md-dns-copybar { display: flex; align-items: center; gap: 10px; margin-top: 4px; }
.md-dns-copy-tip { font-size: 12px; color: #94a3b8; }
/* DKIM/DMARC 暂不可用项 */
.md-dns-placeholder { display: flex; align-items: center; gap: 6px; padding: 12px; background: #fff; border: 1px dashed #cbd5e1; border-radius: 8px; color: #94a3b8; font-size: 13px; }
.md-dns-placeholder .el-icon { font-size: 16px; }
/* 操作指引 */
.md-how { background: #f1f5f9; border-radius: 10px; padding: 14px 16px; margin-top: 16px; }
.md-how-title { font-size: 13px; font-weight: 700; color: #0f172a; margin-bottom: 8px; }
.md-how-list { font-size: 13px; color: #334155; line-height: 1.9; padding-left: 22px; margin: 0; }
.md-how-list li { margin-bottom: 2px; }
/* 勾选区（小白化） */
.md-dns-markbar { margin-top: 14px; padding: 10px 14px; background: #f0f9ff; border-radius: 10px; }
.md-dns-markbar-title { font-size: 13px; font-weight: 600; color: #0f172a; margin-bottom: 8px; }
.md-dns-markbar-list { display: flex; flex-direction: column; gap: 6px; }
/* 保留旧结构 class 兼容，以防 dnsCheck 自检区中用到 */
.md-dns-alert { margin-bottom: 12px; }
@media (max-width: 599px) {
  .md-toolbar { flex-direction: column; align-items: stretch; }
  .md-dns-field-row { flex-direction: column; gap: 2px; }
  .md-dns-field-label { flex: 1 0 auto; }
}
/* ===== 用途选择 ===== */
.md-purpose { margin: 6px 0 6px; }
.md-purpose-hint { font-size: 13px; color: #0369a1; background: #f0f9ff; border: 1px dashed #7dd3fc; padding: 8px 12px; border-radius: 8px; margin-bottom: 12px; line-height: 1.6; }
/* ===== 收信/发信分组 ===== */
.md-dns-group { border: 1px solid #e2e8f0; border-radius: 12px; padding: 14px; margin-bottom: 14px; }
.md-group-receive { border-color: #bae6fd; background: #f8fbff; }
.md-group-send { border-color: #bbf7d0; background: #f9fdf8; }
.md-group-head { display: flex; align-items: center; gap: 10px; margin-bottom: 12px; }
.md-group-badge { flex: 0 0 auto; font-size: 13px; font-weight: 700; padding: 4px 12px; border-radius: 6px; color: #fff; }
.md-group-badge.receive { background: #0284c7; }
.md-group-badge.send { background: #16a34a; }
.md-group-title { font-size: 14px; color: #334155; line-height: 1.5; }
/* 组内小项卡片间距 */
.md-dns-group .md-dns-row { background: #fff; }
.md-dns-group .md-dns-row + .md-dns-row { margin-top: 10px; }
</style>
