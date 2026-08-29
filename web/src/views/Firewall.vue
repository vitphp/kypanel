<template>
  <div class="security-page">
    <el-tabs v-model="activeTab" class="sec-tabs">
      <!-- 端口规则 -->
      <el-tab-pane label="端口规则" name="ports">
        <div class="toolbar">
          <div class="toolbar-title">
            <el-icon><Connection /></el-icon>
            <span>端口规则</span>
            <span class="tip">支持多端口(80,443)、范围(8000-8100)、指定来源 IP</span>
          </div>
          <div class="toolbar-actions">
            <el-button type="primary" :icon="Plus" @click="openPortDialog">添加端口规则</el-button>
          </div>
        </div>
        <Skeleton v-if="loadingPorts" type="table" :rows="5" :columns="[{width:'140px'},{width:'100px'},{width:'160px'},{width:'90px'},{width:'90px'},{width:'90px'},{flex:1},{width:'90px'}]" />
        <el-table v-else :data="portRules" size="small">
          <el-table-column prop="port" label="端口" min-width="140">
            <template #default="{ row }">
              <span class="mono">{{ row.port }}</span>
            </template>
          </el-table-column>
          <el-table-column prop="proto" label="协议" width="100">
            <template #default="{ row }">
              <el-tag size="small" :type="row.proto === 'udp' ? 'warning' : 'primary'">{{ protoLabel(row.proto) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="来源" min-width="160">
            <template #default="{ row }">
              <span v-if="!row.source_ip" class="muted">所有 IP</span>
              <span v-else class="mono">{{ row.source_ip }}</span>
            </template>
          </el-table-column>
          <el-table-column label="策略" width="90">
            <template #default="{ row }">
              <el-tag size="small" :type="row.action === 'allow' ? 'success' : 'danger'">{{ row.action === 'allow' ? '放行' : '禁止' }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="方向" width="90">
            <template #default="{ row }">
              {{ directionLabel(row.direction) }}
            </template>
          </el-table-column>
          <el-table-column label="状态" width="90">
            <template #default>
              <el-tag size="small" type="success">生效中</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="备注" min-width="160">
            <template #default="{ row }">
              <div v-if="portRemarkEditingKey === portRowKey(row)" class="remark-edit">
                <el-input
                  v-model="portRemarkEditingValue"
                  size="small"
                  placeholder="备注"
                  maxlength="100"
                  @keyup.enter="savePortRemark(row)"
                  @keyup.esc="cancelPortRemarkEdit"
                  @blur="savePortRemark(row)"
                />
              </div>
              <div v-else class="remark-text" :class="{ empty: !row.remark }" @click="startPortRemarkEdit(row)">
                <span>{{ row.remark || '点击添加备注' }}</span>
                <el-icon class="edit-icon"><Edit /></el-icon>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="160" fixed="right">
            <template #default="{ row }">
              <el-button size="small" type="primary" text @click="openPortDialog(row)">编辑</el-button>
              <el-button size="small" type="danger" text @click="removeRule(row)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <!-- WAF 应用防护 -->
      <el-tab-pane label="WAF防护" name="waf">
        <WafPanel />
      </el-tab-pane>

      <!-- IP 规则（IP / 国家 / 运营商） -->
      <el-tab-pane label="IP规则" name="ips">
        <div class="toolbar">
          <div class="toolbar-title">
            <el-icon><Lock /></el-icon>
            <span>IP 规则</span>
            <span class="tip">可添加 IP/网段/范围、国家、运营商，一行一个</span>
          </div>
          <div class="toolbar-actions">
            <el-button type="primary" :icon="Plus" @click="openIpDialog">添加IP规则</el-button>
          </div>
        </div>
        <div class="toolbar sub">
          <div class="toolbar-title">
            <el-icon><LocationInformation /></el-icon>
            <span>IP 归属查询</span>
          </div>
          <div class="toolbar-actions">
            <el-input v-model="lookupIp" placeholder="输入 IP 查询归属地" style="width: 200px" clearable @keyup.enter="lookup" />
            <el-button :icon="Search" @click="lookup">查询</el-button>
            <span v-if="lookupResult" class="lookup-result">{{ lookupResult }}</span>
          </div>
        </div>
        <Skeleton v-if="loadingIps" type="table" :rows="4" :columns="[{width:'100px'},{flex:1},{width:'90px'},{width:'140px'},{width:'170px'},{width:'90px'}]" />
        <el-table v-else :data="ipRules" size="small">
          <el-table-column label="类型" width="100">
            <template #default="{ row }">
              <el-tag size="small" :type="typeTag(row.type)">{{ typeLabel(row.type) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="内容" min-width="260">
            <template #default="{ row }">
              <span v-if="!row.content || row.content.length === 0" class="muted">—</span>
              <el-tooltip v-else-if="row.content.length > 3" placement="top" :content="row.content.join('，')">
                <span class="mono">
                  <el-tag v-for="(c, i) in row.content.slice(0, 3)" :key="i" size="small" class="content-tag">{{ c }}</el-tag>
                  <span class="more">+{{ row.content.length - 3 }}</span>
                </span>
              </el-tooltip>
              <span v-else class="mono">
                <el-tag v-for="(c, i) in row.content" :key="i" size="small" class="content-tag">{{ c }}</el-tag>
              </span>
            </template>
          </el-table-column>
          <el-table-column label="策略" width="90">
            <template #default="{ row }">
              <el-tag size="small" :type="row.action === 'allow' ? 'success' : 'danger'">{{ row.action === 'allow' ? '放行' : '禁止' }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="方向" width="90">
            <template #default="{ row }">
              {{ directionLabel(row.direction) }}
            </template>
          </el-table-column>
          <el-table-column label="状态" width="90">
            <template #default>
              <el-tag size="small" type="success">生效中</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="备注" min-width="160">
            <template #default="{ row }">
              <div v-if="portRemarkEditingKey === portRowKey(row)" class="remark-edit">
                <el-input
                  v-model="portRemarkEditingValue"
                  size="small"
                  placeholder="备注"
                  maxlength="100"
                  @keyup.enter="savePortRemark(row)"
                  @keyup.esc="cancelPortRemarkEdit"
                  @blur="savePortRemark(row)"
                />
              </div>
              <div v-else class="remark-text" :class="{ empty: !row.remark }" @click="startPortRemarkEdit(row)">
                <span>{{ row.remark || '点击添加备注' }}</span>
                <el-icon class="edit-icon"><Edit /></el-icon>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="160" fixed="right">
            <template #default="{ row }">
              <el-button size="small" type="primary" text @click="openPortDialog(row)">编辑</el-button>
              <el-button size="small" type="danger" text @click="removeRule(row)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>
    </el-tabs>

    <!-- 添加/编辑端口规则 -->
    <el-dialog v-model="portDialog.visible" :title="portDialog.isEdit ? '编辑端口规则' : '添加端口规则'" width="480" :close-on-click-modal="false">
      <el-form label-width="70px">
        <el-form-item label="协议">
          <el-radio-group v-model="portForm.proto">
            <el-radio-button value="tcp">TCP</el-radio-button>
            <el-radio-button value="udp">UDP</el-radio-button>
            <el-radio-button value="tcpudp">TCP+UDP</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="端口">
          <el-input v-model="portForm.port" placeholder="如 80 或 80,443 或 8000-8100" clearable />
        </el-form-item>
        <el-form-item label="来源">
          <el-radio-group v-model="portForm.srcType">
            <el-radio value="all">所有 IP</el-radio>
            <el-radio value="spec">指定 IP</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="portForm.srcType === 'spec'" label="指定 IP">
          <el-input v-model="portForm.source_ip" placeholder="IP/网段/范围，如 1.2.3.4 或 1.2.3.0/24 或 1.2.3.1-1.2.3.100" clearable />
        </el-form-item>
        <el-form-item label="策略">
          <el-radio-group v-model="portForm.action">
            <el-radio value="allow">放行</el-radio>
            <el-radio value="block">禁止</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="方向">
          <el-radio-group v-model="portForm.direction">
            <el-radio value="in">入站</el-radio>
            <el-radio value="out">出站</el-radio>
            <el-radio value="both">双向</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="!portDialog.isEdit" label="备注">
          <el-input v-model="portForm.remark" placeholder="选填" clearable />
        </el-form-item>
        <el-form-item v-else label="备注">
          <span class="muted">{{ portForm.remark || '（无）' }}<span class="muted" style="margin-left: 6px">（编辑模式不可改）</span></span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="portDialog.visible = false">取消</el-button>
        <el-button type="primary" :loading="portDialog.submitting" @click="submitPortRule">确定</el-button>
      </template>
    </el-dialog>

    <!-- 添加 IP 规则 -->
    <el-dialog v-model="ipDialog.visible" title="添加IP规则" width="520" :close-on-click-modal="false">
      <el-form label-width="70px">
        <el-form-item label="类型">
          <el-radio-group v-model="ipForm.type">
            <el-radio-button value="ip">IP</el-radio-button>
            <el-radio-button value="country">国家</el-radio-button>
            <el-radio-button value="isp">运营商</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="内容">
          <el-input
            v-model="ipForm.content"
            type="textarea"
            :rows="6"
            :placeholder="ipForm.type === 'ip'
              ? '每行一个：IP、网段或范围\n1.2.3.4\n1.2.3.0/24\n1.2.3.10-1.2.3.50'
              : '每行一个：' + (ipForm.type === 'country' ? '国家名称\n美国\n日本' : '运营商名称\n电信\n联通')"
          />
          <div class="form-tip">一行一个，支持 IP、网段(CIDR)、范围(1.2.3.10-1.2.3.50)</div>
        </el-form-item>
        <el-form-item label="策略">
          <el-radio-group v-model="ipForm.action">
            <el-radio value="allow">放行（白名单）</el-radio>
            <el-radio value="block">禁止（黑名单）</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="ipForm.type === 'ip'" label="方向">
          <el-radio-group v-model="ipForm.direction">
            <el-radio value="in">入站</el-radio>
            <el-radio value="out">出站</el-radio>
            <el-radio value="both">双向</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="ipForm.remark" placeholder="选填" clearable />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="ipDialog.visible = false">取消</el-button>
        <el-button type="primary" :loading="ipDialog.submitting" @click="addIpRule">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Plus, Connection, LocationInformation, Search, Lock, Edit
} from '@element-plus/icons-vue'
import request from '../utils/request'
import WafPanel from '../components/WafPanel.vue'

const activeTab = ref('ports')
const status = reactive({ backend: '', default_drop: false, base_ports: [] })

const loadingPorts = ref(true)
const portRules = ref([])
const loadingIps = ref(true)
const ipRules = ref([])

const lookupIp = ref('')
const lookupResult = ref('')

const portDialog = reactive({ visible: false, submitting: false, isEdit: false, editingId: '' })
// 端口 / IP 添加弹窗表单
const portForm = reactive({ proto: 'tcp', port: '', srcType: 'all', source_ip: '', action: 'allow', direction: 'in', remark: '' })

// 端口规则备注行内编辑
const portRemarkEditingKey = ref('') // 用 port+proto+direction 组合作为编辑 key（id 共享会导致多行同时进入编辑态）
const portRemarkEditingValue = ref('')

function portRowKey(row) {
  return `${row.port}|${row.proto}|${row.direction || ''}`
}

function startPortRemarkEdit(row) {
  portRemarkEditingKey.value = portRowKey(row)
  portRemarkEditingValue.value = row.remark || ''
  // 用 nextTick + DOM 查找聚焦当前行内的 input（v-for 内 ref 行为不可靠）
  nextTick(() => {
    const inputs = document.querySelectorAll('.remark-edit input')
    for (const el of inputs) {
      if (el.closest('tr')?.querySelector('.remark-edit')) {
        el.focus()
        break
      }
    }
  })
}

function cancelPortRemarkEdit() {
  portRemarkEditingKey.value = ''
  portRemarkEditingValue.value = ''
}

async function savePortRemark(row) {
  const key = portRemarkEditingKey.value
  if (!key) return
  const newRemark = (portRemarkEditingValue.value || '').trim()
  const oldRemark = row.remark || ''
  // 立即关闭编辑态，避免连续操作闪烁
  portRemarkEditingKey.value = ''
  portRemarkEditingValue.value = ''
  if (newRemark === oldRemark) return
  try {
    await request.post('/security/rule/remark', { id: row.id, remark: newRemark })
    row.remark = newRemark
  } catch (e) {
    // 错误提示由 interceptor 统一处理
  }
}

const ipDialog = reactive({ visible: false, submitting: false })
const ipForm = reactive({ type: 'ip', content: '', action: 'block', direction: 'in', remark: '' })

function protoLabel(p) {
  return { tcp: 'TCP', udp: 'UDP', tcpudp: 'TCP+UDP' }[p] || p
}
function typeLabel(t) {
  return { ip: 'IP', country: '国家', isp: '运营商' }[t] || t
}
function typeTag(t) {
  return { ip: 'primary', country: 'success', isp: 'warning' }[t] || 'info'
}
function directionLabel(d) {
  return { in: '入站', out: '出站', both: '双向' }[d] || '-'
}

async function loadStatus() {
  const res = await request.get('/security/status')
  Object.assign(status, res.data)
}

async function loadPortRules() {
  loadingPorts.value = true
  try {
    const res = await request.get('/security/rules', { params: { type: 'port' } })
    portRules.value = res.data.rules || []
  } finally {
    loadingPorts.value = false
  }
}

async function loadIpRules() {
  loadingIps.value = true
  try {
    const [ipRes, geoRes] = await Promise.all([
      request.get('/security/rules', { params: { type: 'ip' } }),
      request.get('/security/rules', { params: { type: 'geo' } }),
    ])
    ipRules.value = [...(ipRes.data.rules || []), ...(geoRes.data.rules || [])]
  } finally {
    loadingIps.value = false
  }
}

// 打开端口规则弹窗：不传 row = 添加模式，传入 row = 编辑模式
function openPortDialog(row) {
  if (row) {
    // 编辑模式：把现有规则数据填进 portForm
    const proto = row.proto || 'tcp'
    Object.assign(portForm, {
      proto,
      port: row.port || '',
      srcType: row.source_ip ? 'spec' : 'all',
      source_ip: row.source_ip || '',
      action: row.action || 'allow',
      direction: row.direction || 'in',
      remark: row.remark || '',
    })
    portDialog.isEdit = true
    portDialog.editingId = row.id
  } else {
    // 添加模式：重置
    Object.assign(portForm, { proto: 'tcp', port: '', srcType: 'all', source_ip: '', action: 'allow', direction: 'in', remark: '' })
    portDialog.isEdit = false
    portDialog.editingId = ''
  }
  portDialog.visible = true
}

// 提交端口规则：按 isEdit 走 add 或 update
async function submitPortRule() {
  if (!portForm.port.trim()) {
    ElMessage.warning('请输入端口')
    return
  }
  portDialog.submitting = true
  try {
    const payload = {
      type: 'port',
      port: portForm.port,
      proto: portForm.proto,
      source_ip: portForm.srcType === 'spec' ? portForm.source_ip : '',
      action: portForm.action,
      direction: portForm.direction,
      remark: portForm.remark,
    }
    if (portDialog.isEdit) {
      // id 用 query 传，避免和 body 字段冲突
      await request.post(`/security/rule/update?id=${encodeURIComponent(portDialog.editingId)}`, payload)
      ElMessage.success('端口规则已更新')
    } else {
      await request.post('/security/rule/add', payload)
      ElMessage.success('端口规则已添加')
    }
    portDialog.visible = false
    loadPortRules()
  } catch (e) {
    ElMessage.error(e.message || '保存失败')
  } finally {
    portDialog.submitting = false
  }
}

function openIpDialog() {
  Object.assign(ipForm, { type: 'ip', content: '', action: 'block', direction: 'in', remark: '' })
  ipDialog.visible = true
}

async function addIpRule() {
  if (!ipForm.content.trim()) {
    ElMessage.warning('请输入内容')
    return
  }
  ipDialog.submitting = true
  try {
    await request.post('/security/rule/add', {
      type: ipForm.type,
      content: ipForm.content,
      action: ipForm.action,
      direction: ipForm.direction,
      remark: ipForm.remark,
    })
    ElMessage.success('IP规则已添加')
    ipDialog.visible = false
    loadIpRules()
  } catch (e) {
    ElMessage.error(e.message || '添加失败')
  } finally {
    ipDialog.submitting = false
  }
}

async function removeRule(row) {
  const label = row.type === 'port' ? `端口 ${row.port} 规则` : row.type === 'ip' ? `IP 规则` : row.type === 'country' ? '国家规则' : '运营商规则'
  await ElMessageBox.confirm(`确认删除该${label}？`, '提示', { type: 'warning' })
  await request.post('/security/rule/delete', { id: row.id })
  ElMessage.success('已删除')
  if (row.type === 'port') {
    loadPortRules()
  } else {
    loadIpRules()
  }
}

async function lookup() {
  const ip = lookupIp.value.trim()
  if (!ip) {
    ElMessage.warning('请输入 IP')
    return
  }
  const res = await request.get('/security/ip/lookup', { params: { ip } })
  if (res.data) {
    const d = res.data
    lookupResult.value = `${d.country || ''} ${d.province || ''} ${d.city || ''} ${d.isp || ''}`.trim() || '未找到归属信息'
  } else {
    lookupResult.value = '未找到归属信息'
  }
}

onMounted(() => {
  loadStatus()
  loadPortRules()
  loadIpRules()
})
</script>

<style scoped>
.sec-tabs {
  background: #fff;
  border-radius: 6px;
  padding: 4px 16px 16px;
  box-shadow: 0 1px 3px rgba(0, 21, 41, 0.08);
}
.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 10px;
  padding: 12px 0;
}
.toolbar.sub {
  padding-top: 0;
}
.toolbar-title {
  display: flex;
  align-items: center;
  gap: 6px;
  color: #303133;
  font-size: 14px;
  font-weight: 500;
}
.tip {
  color: #909399;
  font-size: 12px;
  font-weight: 400;
}
.toolbar-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.mono {
  font-family: Consolas, Menlo, monospace;
  font-size: 12.5px;
}
.muted {
  color: #909399;
  font-size: 13px;
}
/* 备注行内编辑：点击进入、blur/回车自动保存 */
.remark-text {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 4px 0;
  cursor: pointer;
  color: #303133;
  font-size: 13px;
  min-height: 24px;
  border-radius: 3px;
  transition: background 0.15s;
}
.remark-text:hover { background: #f5f7fa; }
.remark-text.empty { color: #c0c4cc; }
.remark-text .edit-icon {
  font-size: 12px;
  color: #c0c4cc;
  opacity: 0;
  transition: opacity 0.15s;
}
.remark-text:hover .edit-icon { opacity: 1; }
.remark-edit { display: flex; align-items: center; }
.remark-edit .el-input { width: 100%; }
.more {
  color: #909399;
  font-size: 12px;
  margin-left: 4px;
}
.content-tag {
  margin-right: 4px;
}
.lookup-result {
  color: #409eff;
  font-size: 13px;
}
.form-tip {
  color: #909399;
  font-size: 12px;
  line-height: 1.5;
  margin-top: 4px;
  white-space: pre-line;
}
</style>
