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
        <div class="md-why">
          <strong>为什么需要这一步：</strong>
          要让全世界能把发给你的邮件送到这台服务器、需要你告诉全网"我的邮件服务器是这个 IP"。
          操作方式：到你的<span class="md-link">域名服务商</span>（阿里云 / 腾讯云 / Cloudflare 等）后台，添加下面 3 条记录即可。其它记录是"以后再说"。
        </div>

        <!-- 自动检测到的邮件服务器值（用户不用手填） -->
        <div class="md-detected">
          <el-icon :size="16" class="md-detected-icon"><Connection /></el-icon>
          <span class="md-detected-text">本面板已检测到你的服务器地址：</span>
          <el-tag class="md-detected-tag" type="success" effect="light">{{ dnsGuide.mail_server }}</el-tag>
        </div>

        <!-- 引导列表 -->
        <div class="md-dns-list">
          <div v-for="(r, i) in dnsRows" :key="i" class="md-dns-row" :class="{ 'md-dns-row-opt': r.kind !== '必做' }">
            <div class="md-dns-row-head">
              <span class="md-dns-step-tag" :class="r.kind === '必做' ? 'must' : 'opt'">{{ r.step }}</span>
              <span class="md-dns-kind" :class="r.kind === '必做' ? 'must' : 'opt'">{{ r.kind }}</span>
            </div>
            <div class="md-dns-title">{{ r.title }}</div>
            <div class="md-dns-why">为什么：{{ r.why }}</div>

            <template v-if="r.steps.length">
              <div class="md-dns-fields">
                <div class="md-dns-fields-title">打开服务商后台 → 添加解析 → 按下面填：</div>
                <div v-for="(s, j) in r.steps" :key="j" class="md-dns-field-row">
                  <span class="md-dns-field-label">{{ s.label }}</span>
                  <span class="md-dns-field-value">{{ s.value }}</span>
                </div>
              </div>
              <div class="md-dns-copybar">
                <el-button size="small" type="primary" plain @click="copy(r.steps[r.steps.length - 1].value)">
                  复制「记录值」
                </el-button>
                <span class="md-dns-copy-tip">点击后到服务商后台"记录值"框粘进去就行</span>
              </div>
            </template>
            <template v-else>
              <div class="md-dns-placeholder">
                <el-icon><Clock /></el-icon>
                <span>这一项当前不用管，面板功能上线后会引导你补配</span>
              </div>
            </template>
          </div>
        </div>

        <!-- 操作引导 -->
        <div class="md-how">
          <div class="md-how-title">怎么去服务商后台？</div>
          <ol class="md-how-list">
            <li>登录你买域名时的<span class="md-link">服务商</span>（阿里云 / 腾讯云 / Cloudflare 等）</li>
            <li>找到「<span class="md-link">DNS 解析</span> / <span class="md-link">域名解析</span>」菜单</li>
            <li>点「<span class="md-link">添加记录</span>」</li>
            <li>按上面"打开服务商后台 → 添加解析 → 按下面填"里的每一项，一项一项填进去</li>
            <li>三条必做都加好后，下面的检测会自动判断是否生效（DNS 全球传播通常几分钟）</li>
          </ol>
        </div>

        <!-- 自检 + 大白话勾选 -->
        <div class="md-dns-checkbar">
          <el-button :loading="checking" @click="runDnsCheck">我配完了，帮我查一下是否生效</el-button>
          <span v-if="checkResult !== null" class="md-check-result" :class="checkResult ? 'ok' : 'bad'">
            {{ checkResult ? '✓ 检测到了！邮件已经能投递到本服务器' : '⏳ 还没检测到（DNS 还在传播，再等几分钟重试）' }}
          </span>
        </div>
        <div class="md-dns-markbar">
          <div class="md-dns-markbar-title">我已经去服务商后台添加了：</div>
          <div class="md-dns-markbar-list">
            <el-checkbox v-model="dnsMark.mx" @change="saveDnsMark">第 1 步（A 记录：mail.我的域名 → 我的服务器）</el-checkbox>
            <el-checkbox v-model="dnsMark.spf" @change="saveDnsMark">第 2 步（MX：@ → mail.我的域名）</el-checkbox>
            <el-checkbox v-model="dnsMark.dkim" @change="saveDnsMark">第 3 步（SPF：@ → 一段以 v=spf1 开头的配置）</el-checkbox>
            <el-checkbox v-model="dnsMark.dmarc" @change="saveDnsMark">第 4、5 步（高级项，暂时可不勾）</el-checkbox>
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
import { Plus, Connection, Clock } from '@element-plus/icons-vue'
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

// 把后端引导折叠成"小白视角"的条目：每条带大白话标题、干什么用、
// 在服务商后台要填哪些字段（steps）、以及要粘的内容（value）。
// 渲染时按 steps 拆开列展示 → 让用户清楚"我下一步该点哪里、填什么"。
const dnsRows = computed(() => {
  const g = dnsGuide.value
  if (!g) return []
  const hostName = `mail.${g.domain}`
  return [
    {
      step: '步骤 1',
      title: '先把"邮件服务器"这个地址告诉全网',
      why: '让别人在浏览器 / 邮件客户端里访问 mail.你的域名 时能找到你的服务器 IP',
      kind: '必做',
      steps: [
        { label: '记录类型', value: 'A' },
        { label: '主机记录', value: 'mail' },
        { label: '记录值', value: g.mail_server }
      ],
      raw: `A mail → ${g.mail_server}`
    },
    {
      step: '步骤 2',
      title: '告诉全网：发给我的邮件请送到我的服务器',
      why: '没有这步，别人发的邮件根本到不了你的邮箱（会直接退回）',
      kind: '必做',
      steps: [
        { label: '记录类型', value: 'MX' },
        { label: '主机记录', value: '@（保持空）' },
        { label: '记录值', value: hostName },
        { label: '优先级', value: '10' }
      ],
      raw: `MX @ → ${hostName}（优先级 10）`
    },
    {
      step: '步骤 3',
      title: '告诉全网：只有我的服务器能用我的域名发信',
      why: '不配这步，从你服务器发出去的信很容易被对方判为"伪造邮件"进垃圾箱',
      kind: '必做',
      steps: [
        { label: '记录类型', value: 'TXT' },
        { label: '主机记录', value: '@（保持空）' },
        { label: '记录值', value: g.spf_value }
      ],
      raw: `TXT @ → ${g.spf_value}`
    },
    {
      step: '步骤 4（可选）',
      title: '给每封发出去的信加数字签名（更可信）',
      why: '目前面板还没做签名功能，等上线后这里会自动出现真实密钥让你复制。届时强烈建议配。',
      kind: '以后再说',
      steps: [],
      raw: ''
    },
    {
      step: '步骤 5（可选）',
      title: '告诉收信方：遇到冒充我的信该怎么处理',
      why: '属于更高级的策略；先把第 1~3 步做完即可，后续面板会引导你补这步',
      kind: '以后再说',
      steps: [],
      raw: ''
    }
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
</style>
