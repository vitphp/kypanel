<template>
  <el-dialog
    v-model="visible"
    title="网站搬家"
    width="900px"
    top="4vh"
    :close-on-click-modal="false"
    destroy-on-close
    @closed="onClosed"
  >
    <el-tabs v-model="activeTab" type="border-card">
      <!-- ==================== 网站迁出 ==================== -->
      <el-tab-pane label="网站迁出" name="export">
        <el-steps :active="expStep - 1" finish-status="success" align-center class="mig-steps">
          <el-step title="API 信息" />
          <el-step title="选择内容" />
          <el-step title="开始迁移" />
          <el-step title="完成" />
        </el-steps>

        <!-- Step 1: API 信息 -->
        <div v-if="expStep === 1" class="step-body">
          <el-form label-width="100px" label-position="left">
            <el-form-item label="目标面板地址">
              <el-input v-model="expForm.url" placeholder="如 https://1.2.3.4:9999 或 http://1.2.3.4:8888" />
            </el-form-item>
            <el-form-item label="API 密钥">
              <el-input
                v-model="expForm.token"
                placeholder="目标面板「系统设置-API令牌」或对端面板「API 接口」中的密钥"
                show-password
              />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" :loading="expDetecting" @click="detectExpPanel">连接并识别</el-button>
            </el-form-item>
          </el-form>
        </div>

        <!-- Step 2: 选择内容 -->
        <div v-if="expStep === 2" class="step-body">
          <el-form label-width="100px" label-position="left">
            <el-form-item label="网站">
              <el-select v-model="expSites" multiple filterable collapse-tags placeholder="选择网站" style="width: 100%">
                <el-option v-for="s in localSites" :key="s.id" :label="`${s.name}（${s.domain}）`" :value="s.name" />
              </el-select>
            </el-form-item>
            <el-form-item label="数据库">
              <el-select v-model="expDbs" multiple filterable collapse-tags placeholder="选择数据库（不选则不迁数据库）" style="width: 100%">
                <el-option v-for="d in localDbs" :key="d.name" :label="d.name" :value="d.name" />
              </el-select>
            </el-form-item>
            <el-form-item label="FTP 账号">
              <el-select v-model="expFtps" multiple filterable collapse-tags placeholder="选择 FTP 账号（不选则不迁 FTP）" style="width: 100%">
                <el-option v-for="f in localFtps" :key="f.username" :label="f.username" :value="f.username" />
              </el-select>
            </el-form-item>

            <el-form-item>
              <el-button @click="expStep = 1">上一步</el-button>
              <el-button :loading="precheckLoading" :disabled="!hasExportSelection" @click="doPrecheck">环境预检</el-button>
              <el-button type="primary" :loading="exporting" :disabled="!hasExportSelection" @click="startExport">
                开始迁移
              </el-button>
            </el-form-item>
          </el-form>

          <el-alert
            v-if="precheckResult"
            class="mig-alert"
            :type="precheckResult.all_ready || (precheckResult.result && precheckResult.result.all_ready) ? 'success' : 'warning'"
            :closable="false"
            show-icon
          >
            <template #title>
              {{ precheckResult.target === 'bt' ? '对端面板环境对比结果' : `目标面板（v${precheckResult.version}）环境对比结果` }}
            </template>
            <template #default>
              <template v-if="precheckResult.target === 'bt'">
                已安装 PHP：{{ fmtBtPhp(precheckResult.result) }}；缺失：{{ fmtBtMissing(precheckResult.result) }}
              </template>
              <template v-else>
                <span v-if="precheckResult.missing.length === 0">目标面板环境完全满足，可以开始迁移。</span>
                <span v-else>目标面板缺少以下环境，迁移时请先在目标面板安装：{{ precheckResult.missing.map(m => m.name).join('、') }}</span>
              </template>
            </template>
          </el-alert>
        </div>

        <!-- Step 3: 开始迁移 -->
        <div v-if="expStep === 3" class="step-body">
          <div class="task-title">
            迁移进度
            <el-tag v-if="task && task.status === 'running'" type="warning" size="small">进行中</el-tag>
            <el-tag v-else-if="task && task.status === 'success'" type="success" size="small">完成</el-tag>
            <el-tag v-else-if="task && task.status === 'failed'" type="danger" size="small">失败</el-tag>
          </div>
          <pre ref="expTaskLog" class="task-log">{{ task && task.logs ? task.logs.join('\n') : '等待开始...' }}</pre>
          <div v-if="task && task.status === 'failed'" class="mig-back-btn">
            <el-button type="primary" @click="expStep = 2">返回上一步</el-button>
          </div>
        </div>

        <!-- Step 4: 完成 -->
        <div v-if="expStep === 4" class="step-body">
          <template v-if="expForm.panelType === 'kypanel' && exportResult">
            <el-result icon="success" title="打包完成" :sub-title="`迁移包 ${exportResult.file}（${fmtSize(exportResult.size)}），包含网站 ${exportResult.manifest.sites.length} 个、数据库 ${exportResult.manifest.databases.length} 个、FTP ${exportResult.manifest.ftps.length} 个`">
              <template #extra>
                <div class="dl-row">
                  <el-input :model-value="downloadUrl" readonly>
                    <template #append>
                      <el-button @click="copyDownloadUrl">复制链接</el-button>
                    </template>
                  </el-input>
                </div>
                <div class="dl-tip">
                  到目标面板打开「网站搬家 → 网站迁入」，填入本面板地址与 API 令牌，即可拉取迁移。
                </div>
              </template>
            </el-result>
          </template>
          <template v-else-if="expForm.panelType === 'bt'">
            <el-result icon="success" title="迁出完成" :sub-title="task && task.status === 'success' ? '对端面板已恢复网站/数据库/FTP' : ''">
              <template #extra>
                <el-button @click="resetExport">再迁一次</el-button>
              </template>
            </el-result>
          </template>
          <template v-else>
            <el-result icon="success" title="完成" />
          </template>
        </div>
      </el-tab-pane>

      <!-- ==================== 网站迁入 ==================== -->
      <el-tab-pane label="网站迁入" name="import">
        <el-steps :active="impStep - 1" finish-status="success" align-center class="mig-steps">
          <el-step title="API 信息" />
          <el-step title="选择内容" />
          <el-step title="开始迁移" />
          <el-step title="完成" />
        </el-steps>

        <!-- Step 1: API 信息 -->
        <div v-if="impStep === 1" class="step-body">
          <el-form label-width="100px" label-position="left">
            <el-form-item label="源面板地址">
              <el-input v-model="impForm.url" placeholder="源面板地址，如 https://1.2.3.4:9999 或 http://1.2.3.4:8888" />
            </el-form-item>
            <el-form-item label="API 密钥">
              <el-input
                v-model="impForm.token"
                placeholder="源面板「系统设置-API令牌」或对端面板「API 接口」中的密钥"
                show-password
              />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" :loading="impDetecting" @click="detectImpPanel">连接并识别</el-button>
            </el-form-item>
          </el-form>
        </div>

        <!-- Step 2: 选择内容 -->
        <div v-if="impStep === 2" class="step-body">
          <el-form-item>
            <el-button :loading="fetching" @click="fetchSites">获取源面板网站列表</el-button>
          </el-form-item>

          <el-table
            v-if="remoteSites.length > 0"
            :data="remoteSites"
            height="200"
            size="small"
            border
            @selection-change="onRemoteSelect"
          >
            <el-table-column type="selection" width="40" />
            <el-table-column prop="name" label="网站" min-width="140" />
            <el-table-column prop="domain" label="域名" min-width="160" show-overflow-tooltip />
            <el-table-column prop="type" label="类型" width="80" />
            <el-table-column label="PHP" width="70">
              <template #default="{ row }">{{ row.php_version || '-' }}</template>
            </el-table-column>
            <el-table-column label="关联" width="140">
              <template #default="{ row }">
                <span v-if="(row.dbs || []).length">库×{{ row.dbs.length }}</span>
                <span v-if="(row.ftps || []).length"> FTP×{{ row.ftps.length }}</span>
              </template>
            </el-table-column>
          </el-table>

          <div v-if="remoteSites.length > 0" class="sel-summary">
            已选网站 {{ impSelected.length }} 个，自动带出数据库 {{ impDbs.length }} 个、FTP {{ impFtps.length }} 个
          </div>

          <el-form-item class="mig-plan-btn">
            <el-button @click="impStep = 1">上一步</el-button>
            <el-button :loading="planning" :disabled="impSelected.length === 0" @click="doPlan">
              对比环境（检查缺失项）
            </el-button>
          </el-form-item>

          <template v-if="plan">
            <el-alert
              :type="plan.missing.length === 0 ? 'success' : 'warning'"
              :closable="false"
              show-icon
            >
              <template #title>
                {{ plan.missing.length === 0 ? '本机环境完全满足，可直接迁移' : `本机缺少 ${plan.missing.length} 项环境` }}
              </template>
              <template #default v-if="plan.missing.length > 0">
                <div>勾选需要自动安装的环境（安装完成后自动继续迁移）：</div>
                <el-checkbox-group v-model="autoInstall">
                  <el-checkbox
                    v-for="m in plan.missing"
                    :key="m.key"
                    :value="m.key"
                    :disabled="!m.installable"
                  >
                    {{ m.name }}
                    <span class="field-note">{{ m.installable ? '（应用商店可安装）' : '（商店无此应用，需手动安装）' }}</span>
                  </el-checkbox>
                </el-checkbox-group>
              </template>
            </el-alert>

            <el-form label-width="100px" label-position="left" class="mig-opt-form">
              <el-form-item>
                <el-checkbox v-model="overwrite">目标机已存在同名网站时跳过（不覆盖）</el-checkbox>
              </el-form-item>
              <el-form-item>
                <el-button type="primary" :loading="importing" :disabled="impSelected.length === 0" @click="startImport">
                  开始迁移
                </el-button>
              </el-form-item>
            </el-form>
          </template>
        </div>

        <!-- Step 3: 开始迁移 -->
        <div v-if="impStep === 3" class="step-body">
          <div class="task-title">
            迁移进度
            <el-tag v-if="task && task.status === 'running'" type="warning" size="small">进行中</el-tag>
            <el-tag v-else-if="task && task.status === 'success'" type="success" size="small">完成</el-tag>
            <el-tag v-else-if="task && task.status === 'failed'" type="danger" size="small">失败</el-tag>
          </div>
          <pre ref="impTaskLog" class="task-log">{{ task && task.logs ? task.logs.join('\n') : '等待开始...' }}</pre>
          <div v-if="task && task.status === 'failed'" class="mig-back-btn">
            <el-button type="primary" @click="impStep = 2">返回上一步</el-button>
          </div>
        </div>

        <!-- Step 4: 完成 -->
        <div v-if="impStep === 4" class="step-body">
          <el-result icon="success" title="迁入完成">
            <template #extra>
              <el-button @click="resetImport">再迁一次</el-button>
            </template>
          </el-result>
        </div>
      </el-tab-pane>
    </el-tabs>

    <!-- 迁出到对端面板：冲突确认弹窗 -->
    <el-dialog v-model="conflictVisible" title="检测到目标对端面板已有同名项目" width="600px" :close-on-click-modal="false">
      <div v-if="conflict.sites.length" class="conflict-group">
        <div class="conflict-group-title">网站（{{ conflict.sites.length }} 个）</div>
        <div class="conflict-names">{{ conflict.sites.join('、') }}</div>
        <el-radio-group v-model="conflictChoice.siteOverwrite">
          <el-radio :value="true">覆盖（删除对端面板旧网站后重建）</el-radio>
          <el-radio :value="false">跳过（不动对端面板已有网站）</el-radio>
        </el-radio-group>
      </div>
      <div v-if="conflict.databases.length" class="conflict-group">
        <div class="conflict-group-title">数据库（{{ conflict.databases.length }} 个）</div>
        <div class="conflict-names">{{ conflict.databases.join('、') }}</div>
        <el-radio-group v-model="conflictChoice.dbOverwrite">
          <el-radio :value="true">覆盖（删除对端面板旧数据库后重建并导入数据）</el-radio>
          <el-radio :value="false">跳过（不动对端面板已有数据库）</el-radio>
        </el-radio-group>
      </div>
      <div v-if="conflict.ftps.length" class="conflict-group">
        <div class="conflict-group-title">FTP 账号（{{ conflict.ftps.length }} 个）</div>
        <div class="conflict-names">{{ conflict.ftps.join('、') }}</div>
        <el-radio-group v-model="conflictChoice.ftpOverwrite">
          <el-radio :value="true">覆盖（删除对端面板旧账号后重建）</el-radio>
          <el-radio :value="false">跳过（不动对端面板已有账号）</el-radio>
        </el-radio-group>
      </div>
      <div class="conflict-tip">覆盖会先删除目标对端面板上同名的项目再重新创建；跳过则不动目标对端面板上已有的同名项目。</div>
      <template #footer>
        <el-button @click="cancelConflict">取消</el-button>
        <el-button type="primary" @click="confirmConflict">确认并开始迁移</el-button>
      </template>
    </el-dialog>
  </el-dialog>
</template>

<script setup>
import { ref, reactive, computed, watch, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import request from '../utils/request'
import { useAuthStore } from '../stores/auth'

const props = defineProps({ modelValue: Boolean })
const emit = defineEmits(['update:modelValue'])
const visible = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v)
})

const activeTab = ref('export')

// ---------- 本机数据 ----------
const localSites = ref([])
const localDbs = ref([])
const localFtps = ref([])

// ---------- 迁出 ----------
const expStep = ref(1)
const expForm = reactive({ url: '', token: '', panelType: '', version: '', detected: false })
const expDetecting = ref(false)
const expSites = ref([])
const expDbs = ref([])
const expFtps = ref([])
const precheckResult = ref(null)
const precheckLoading = ref(false)
const exporting = ref(false)
const exportResult = ref(null)

// ---------- 迁入 ----------
const impStep = ref(1)
const impForm = reactive({ url: '', token: '', panelType: '', version: '', detected: false })
const impDetecting = ref(false)
const fetching = ref(false)
const remoteSites = ref([])
const impSelected = ref([])
const impDbs = ref([])
const impFtps = ref([])
const planning = ref(false)
const plan = ref(null)
const autoInstall = ref([])
const overwrite = ref(false)
const importing = ref(false)

const task = ref(null)
let taskTimer = null

// 迁出到对端面板：冲突检测弹窗
const conflictVisible = ref(false)
const conflict = ref({ sites: [], databases: [], ftps: [] })
const conflictChoice = reactive({ siteOverwrite: true, dbOverwrite: true, ftpOverwrite: true })

// 迁移日志自动滚动到底部
const expTaskLog = ref(null)
const impTaskLog = ref(null)
watch(() => task.value?.logs?.length, async () => {
  await nextTick()
  const el = expStep.value === 3 ? expTaskLog.value : (impStep.value === 3 ? impTaskLog.value : null)
  if (el) el.scrollTop = el.scrollHeight
})

const hasExportSelection = computed(() => expSites.value.length + expDbs.value.length + expFtps.value.length > 0)
const downloadUrl = computed(() => {
  if (!exportResult.value) return ''
  const auth = useAuthStore()
  return `${location.origin}/api/migrate/download/${exportResult.value.id}?token=${auth.token || ''}`
})

const EXP_STORE_KEY = 'kypanel_migrate_exp_api'
const IMP_STORE_KEY = 'kypanel_migrate_imp_api'

function loadStoredAPI(form, key) {
  try {
    const raw = localStorage.getItem(key)
    if (raw) {
      const saved = JSON.parse(raw)
      form.url = saved.url || ''
      form.token = saved.token || ''
      form.panelType = saved.panelType || ''
      form.version = saved.version || ''
      form.detected = !!saved.detected
    }
  } catch (e) { /* ignore */ }
}

function saveAPI(form, key) {
  try {
    localStorage.setItem(key, JSON.stringify({
      url: form.url,
      token: form.token,
      panelType: form.panelType,
      version: form.version,
      detected: form.detected
    }))
  } catch (e) { /* ignore */ }
}

async function loadLocalData() {
  try {
    const [sites, dbs, ftps] = await Promise.all([
      request.get('/site/list'),
      request.get('/database/list'),
      request.get('/ftp/list')
    ])
    localSites.value = sites.data || []
    localDbs.value = dbs.data || []
    localFtps.value = ftps.data || []
  } catch (e) {
    /* ignore */
  }
}

// ---------- 面板探测 ----------
async function detectPanel(form, url, token) {
  if (!url || !token) {
    ElMessage.warning('请先填写面板地址和密钥')
    return null
  }
  const res = await request.post('/migrate/detect-panel', { url, token })
  return res.data
}

async function detectExpPanel() {
  expDetecting.value = true
  try {
    const data = await detectPanel(expForm, expForm.url, expForm.token)
    if (!data) return
    expForm.panelType = data.panel_type || ''
    expForm.version = data.version || ''
    expForm.detected = true
    saveAPI(expForm, EXP_STORE_KEY)
    ElMessage.success('面板连接成功并已识别类型')
    expStep.value = 2
  } catch (e) {
    expForm.detected = false
  } finally {
    expDetecting.value = false
  }
}

async function detectImpPanel() {
  impDetecting.value = true
  try {
    const data = await detectPanel(impForm, impForm.url, impForm.token)
    if (!data) return
    impForm.panelType = data.panel_type || ''
    impForm.version = data.version || ''
    impForm.detected = true
    saveAPI(impForm, IMP_STORE_KEY)
    ElMessage.success('面板连接成功并已识别类型')
    impStep.value = 2
  } catch (e) {
    impForm.detected = false
  } finally {
    impDetecting.value = false
  }
}

// ---------- 迁出：环境预检 ----------
async function doPrecheck() {
  if (!hasExportSelection.value) {
    ElMessage.warning('请先选择要迁出的内容')
    return
  }
  precheckLoading.value = true
  precheckResult.value = null
  try {
    const res = await request.post('/migrate/export/precheck', {
      panel_type: expForm.panelType,
      panel_url: expForm.url,
      panel_token: expForm.token,
      sites: expSites.value,
      databases: expDbs.value,
      ftps: expFtps.value
    })
    precheckResult.value = res.data
  } finally {
    precheckLoading.value = false
  }
}

// ---------- 迁出：打包 ----------
async function startExport() {
  exporting.value = true
  exportResult.value = null
  task.value = null
  expStep.value = 3
  try {
    if (expForm.panelType === 'kypanel') {
      const res = await request.post('/migrate/export', {
        sites: expSites.value,
        databases: expDbs.value,
        ftps: expFtps.value
      })
      exportResult.value = res.data
      task.value = { status: 'success', logs: ['打包完成'] }
      ElMessage.success('打包完成')
      expStep.value = 4
    } else if (expForm.panelType === 'bt') {
      // 迁移前冲突检测：目标对端面板是否已有同名网站/数据库/FTP
      let pre = null
      try {
        const res = await request.post('/migrate/export/bt/precheck', {
          bt_url: expForm.url,
          bt_sk: expForm.token,
          sites: expSites.value,
          databases: expDbs.value,
          ftps: expFtps.value
        })
        pre = res.data || {}
      } catch (e) {
        ElMessage.error('迁移前检测失败：' + (e.response?.data?.msg || e.message))
        expStep.value = 2
        return
      }
      conflict.value = {
        sites: pre.exists_sites || [],
        databases: pre.exists_databases || [],
        ftps: pre.exists_ftps || []
      }
      if (conflict.value.sites.length + conflict.value.databases.length + conflict.value.ftps.length > 0) {
        conflictChoice.siteOverwrite = true
        conflictChoice.dbOverwrite = true
        conflictChoice.ftpOverwrite = true
        conflictVisible.value = true
        return
      }
      await doBTExport({})
    } else {
      ElMessage.warning('请先识别目标面板类型')
      expStep.value = 2
    }
  } catch (e) {
    expStep.value = 2
  } finally {
    exporting.value = false
  }
}

// 开始迁出到对端面板（带冲突决策）
async function doBTExport(decision) {
  try {
    const res = await request.post('/migrate/export/bt', {
      bt_url: expForm.url,
      bt_sk: expForm.token,
      sites: expSites.value,
      databases: expDbs.value,
      ftps: expFtps.value,
      ...decision
    })
    pollTask(res.data.id, () => { expStep.value = 4 })
  } catch (e) {
    ElMessage.error('开始迁移失败：' + (e.response?.data?.msg || e.message))
    expStep.value = 2
  }
}

function confirmConflict() {
  conflictVisible.value = false
  doBTExport({
    site_overwrite: conflictChoice.siteOverwrite,
    database_overwrite: conflictChoice.dbOverwrite,
    ftp_overwrite: conflictChoice.ftpOverwrite
  })
}

function cancelConflict() {
  conflictVisible.value = false
  conflict.value = { sites: [], databases: [], ftps: [] }
  expStep.value = 2
}

async function copyDownloadUrl() {
  try {
    await navigator.clipboard.writeText(downloadUrl.value)
    ElMessage.success('下载链接已复制')
  } catch (e) {
    ElMessage.warning('复制失败，请手动复制')
  }
}

// ---------- 迁入：获取源面板网站 ----------
async function fetchSites() {
  if (!impForm.detected) {
    ElMessage.warning('请先连接并识别源面板')
    return
  }
  fetching.value = true
  remoteSites.value = []
  plan.value = null
  task.value = null
  try {
    const res = await request.post('/migrate/import/fetch-sites', {
      panel_type: impForm.panelType,
      panel_url: impForm.url,
      panel_token: impForm.token
    })
    const list = res.data || []
    remoteSites.value = list
    if (list.length === 0) ElMessage.info('源面板暂无网站')
    else ElMessage.success(`已获取源面板 ${list.length} 个网站`)
  } finally {
    fetching.value = false
  }
}

function onRemoteSelect(rows) {
  impSelected.value = rows.map((r) => r.name)
  impDbs.value = []
  impFtps.value = []
  rows.forEach((r) => {
    ;(r.dbs || []).forEach((d) => {
      if (!impDbs.value.includes(d)) impDbs.value.push(d)
    })
    ;(r.ftps || []).forEach((f) => {
      if (!impFtps.value.includes(f)) impFtps.value.push(f)
    })
  })
  plan.value = null
}

// ---------- 迁入：环境对比 ----------
async function doPlan() {
  planning.value = true
  plan.value = null
  try {
    const res = await request.post('/migrate/import/plan', {
      panel_type: impForm.panelType,
      panel_url: impForm.url,
      panel_token: impForm.token,
      sites: impSelected.value
    })
    plan.value = res.data
    autoInstall.value = (res.data.missing || [])
      .filter((m) => m.installable)
      .map((m) => m.key)
  } finally {
    planning.value = false
  }
}

// ---------- 迁入：开始迁移 ----------
async function startImport() {
  importing.value = true
  task.value = null
  impStep.value = 3
  try {
    const res = await request.post('/migrate/import/run', {
      panel_type: impForm.panelType,
      panel_url: impForm.url,
      panel_token: impForm.token,
      sites: impSelected.value,
      databases: impDbs.value,
      ftps: impFtps.value,
      auto_install: autoInstall.value,
      overwrite: overwrite.value
    })
    pollTask(res.data.id, () => { impStep.value = 4 })
    ElMessage.success('迁移已开始')
  } catch (e) {
    impStep.value = 2
  } finally {
    importing.value = false
  }
}

function pollTask(id, onDone) {
  clearInterval(taskTimer)
  taskTimer = setInterval(async () => {
    try {
      const res = await request.get(`/migrate/tasks/${id}`)
      task.value = res.data
      if (res.data.status === 'success' || res.data.status === 'failed') {
        clearInterval(taskTimer)
        if (res.data.status === 'success') {
          ElMessage.success('操作完成')
          if (onDone) onDone()
        } else {
          ElMessage.error(res.data.error || '操作失败')
        }
      }
    } catch (e) {
      clearInterval(taskTimer)
    }
  }, 2000)
}

// ---------- 工具 ----------
function fmtSize(bytes) {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let n = bytes
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i++
  }
  return `${n.toFixed(n >= 100 || i === 0 ? 0 : 1)} ${units[i]}`
}

function fmtBtPhp(result) {
  const list = result && result.php_installed ? Object.keys(result.php_installed) : []
  return list.length ? list.map((v) => `PHP ${v}`).join('、') : '未检测到'
}

function fmtBtMissing(result) {
  const list = (result && result.php_missing) || []
  return list.length ? list.map((v) => `PHP ${v}`).join('、') : '无'
}

function resetExport() {
  expStep.value = 1
  exportResult.value = null
  precheckResult.value = null
  task.value = null
  expSites.value = []
  expDbs.value = []
  expFtps.value = []
}

function resetImport() {
  impStep.value = 1
  remoteSites.value = []
  impSelected.value = []
  impDbs.value = []
  impFtps.value = []
  plan.value = null
  task.value = null
}

function onClosed() {
  clearInterval(taskTimer)
  activeTab.value = 'export'
  resetExport()
  resetImport()
}

watch(visible, (v) => {
  if (v) {
    loadLocalData()
    loadStoredAPI(expForm, EXP_STORE_KEY)
    loadStoredAPI(impForm, IMP_STORE_KEY)
  }
})

// 选择迁出内容后自动进行环境对比
watch([expSites, expDbs, expFtps], () => {
  if (hasExportSelection.value && expForm.detected && expForm.panelType) {
    doPrecheck()
  }
})

// 选择迁入网站后自动进行环境对比
watch(impSelected, () => {
  if (impSelected.value.length > 0 && impForm.detected && impForm.panelType) {
    doPlan()
  }
})

defineExpose({})
</script>

<style scoped>
.mig-steps {
  margin-bottom: 16px;
}
.step-body {
  min-height: 260px;
}
.detect-result {
  margin-bottom: 0;
}
.detect-result :deep(.el-form-item__content) {
  line-height: 1.4;
}
.field-note {
  margin-left: 10px;
  font-size: 12px;
  color: #999;
}
.mig-alert {
  margin: 10px 0;
}
.export-result {
  margin-top: 6px;
}
.dl-row {
  display: flex;
  gap: 8px;
}
.dl-tip {
  margin-top: 12px;
  font-size: 12px;
  color: #909399;
  line-height: 1.7;
  max-width: 560px;
}
.sel-summary {
  margin: 10px 0 4px;
  font-size: 13px;
  color: #606266;
}
.mig-plan-btn {
  margin-top: 12px;
}
.mig-opt-form {
  margin-top: 14px;
}
.task-title {
  padding: 8px 12px;
  font-size: 13px;
  color: #c9d1d9;
  border-bottom: 1px solid #30363d;
  display: flex;
  align-items: center;
  gap: 8px;
  background: #0d1117;
  border-radius: 6px 6px 0 0;
}
.task-log {
  padding: 10px 12px;
  font-size: 12px;
  line-height: 1.7;
  color: #7ee787;
  max-height: 240px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
  margin: 0;
  background: #0d1117;
  border-radius: 0 0 6px 6px;
}
.mig-back-btn {
  margin-top: 12px;
  text-align: center;
}
.conflict-group {
  margin-bottom: 16px;
  padding: 12px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 6px;
}
.conflict-group-title {
  font-weight: 600;
  margin-bottom: 6px;
}
.conflict-names {
  font-size: 12px;
  color: #e6a23c;
  margin-bottom: 8px;
  word-break: break-all;
}
.conflict-tip {
  margin-top: 4px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
</style>
