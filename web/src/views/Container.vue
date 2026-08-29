<template>
  <div class="container-page">
    <el-card shadow="never">
      <!-- 首次加载骨架屏 -->
      <Skeleton v-if="loading" type="table" :rows="6" :columns="[{flex:1},{flex:1},{width:'150px'},{flex:1},{width:'120px'},{width:'160px'}]" />

      <!-- 安装/卸载中遮罩 -->
      <div v-else-if="installingMap.docker" class="env-missing">
        <div class="env-missing-inner">
          <el-icon :size="48" color="#409eff"><Warning /></el-icon>
          <h3 v-if="pendingActionMap.docker === 'uninstall'">正在卸载 Docker...</h3>
          <h3 v-else>正在安装 Docker...</h3>
          <p>{{ pendingActionMap.docker === 'uninstall' ? '卸载进行中，请稍候...' : '安装进行中，请稍候...' }}</p>
        </div>
      </div>

      <!-- Docker 未安装遮罩提示 -->
      <div v-else-if="!dockerStatus?.installed" class="env-missing">
        <div class="env-missing-inner">
          <el-icon :size="48" color="#e6a23c"><Warning /></el-icon>
          <h3>未检测到 Docker 环境</h3>
          <p>{{ envStatusMap.docker?.remarks || dockerStatus?.error || '请先安装 Docker 后使用容器功能' }}</p>
          <el-button type="primary" size="large" :loading="installingMap.docker" @click="installDocker">
            <el-icon><Download /></el-icon> 一键安装 Docker
          </el-button>
          <p v-if="installingMap.docker" class="installing-hint">安装进行中，请稍候...</p>
        </div>
      </div>

      <el-alert
        v-else-if="dockerStatus && !dockerStatus.running"
        :title="dockerStatus.error || 'Docker 守护进程未运行'"
        type="error"
        :closable="false"
        show-icon
        class="tip"
      />

      <el-tabs v-else v-model="activeTab">
        <!-- 容器列表 -->
        <el-tab-pane label="容器列表" name="containers">
          <div class="toolbar">
            <el-button type="primary" :disabled="!dockerOk" @click="openCreate">
              <el-icon><Plus /></el-icon>&nbsp;创建容器
            </el-button>
            <el-button :disabled="!dockerOk" @click="loadContainers">刷新</el-button>
            <span v-if="dockerStatus && dockerStatus.version" class="ver">Docker {{ dockerStatus.version }}</span>
          </div>
          <Skeleton v-if="loadingC" type="table" :rows="6" :columns="[{flex:1},{flex:1},{width:'150px'},{flex:1},{width:'120px'},{width:'160px'}]" />
          <el-table v-else :data="containers" stripe>
            <el-table-column prop="name" label="名称" min-width="160" />
            <el-table-column prop="image" label="镜像" min-width="180" show-overflow-tooltip />
            <el-table-column label="状态" width="150">
              <template #default="{ row }">
                <el-tag :type="row.running ? 'success' : 'info'">{{ row.status }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="ports" label="端口" min-width="180" show-overflow-tooltip />
            <el-table-column prop="id" label="容器 ID" min-width="120" show-overflow-tooltip />
            <el-table-column label="操作" min-width="160" align="center">
              <template #default="{ row }">
                <div class="ops-cell">
                  <el-button v-if="row.running" link type="warning" @click="action(row, 'stop')">停止</el-button>
                  <el-button v-else link type="success" @click="action(row, 'start')">启动</el-button>
                  <el-button link @click="action(row, 'restart')">重启</el-button>
                  <el-button link @click="showLogs(row)">日志</el-button>
                  <el-button link type="danger" @click="remove(row)">删除</el-button>
                </div>
              </template>
            </el-table-column>
          </el-table>
          <el-empty v-if="!loadingC && containers.length === 0" description="暂无容器" />
        </el-tab-pane>

        <!-- 镜像列表 -->
        <el-tab-pane label="镜像" name="images">
          <div class="toolbar">
            <el-button :disabled="!dockerOk" @click="loadImages">刷新</el-button>
          </div>
          <Skeleton v-if="loadingI" type="table" :rows="5" :columns="[{flex:2},{flex:1},{width:'110px'},{width:'140px'}]" />
          <el-table v-else :data="images" stripe>
            <el-table-column prop="tag" label="镜像" min-width="240" />
            <el-table-column prop="id" label="ID" min-width="120" show-overflow-tooltip />
            <el-table-column prop="size" label="大小" width="110" />
            <el-table-column prop="created" label="创建时间" width="140" />
          </el-table>
          <el-empty v-if="!loadingI && images.length === 0" description="暂无镜像" />
        </el-tab-pane>

        <!-- 网络列表 -->
        <el-tab-pane label="网络" name="networks">
          <div class="toolbar">
            <el-button type="primary" :disabled="!dockerOk" @click="openCreateNetwork">
              <el-icon><Plus /></el-icon>&nbsp;创建网络
            </el-button>
            <el-button :disabled="!dockerOk" @click="loadNetworks">刷新</el-button>
          </div>
          <Skeleton v-if="loadingN" type="table" :rows="4" :columns="[{flex:1},{width:'90px'},{width:'90px'},{flex:1},{flex:1},{width:'85px'},{width:'110px'},{width:'80px'}]" />
          <el-table v-else :data="networks" stripe>
            <el-table-column prop="name" label="名称" min-width="150" />
            <el-table-column prop="driver" label="驱动" width="90" />
            <el-table-column prop="scope" label="作用域" width="90" />
            <el-table-column prop="subnet" label="子网" min-width="140" show-overflow-tooltip />
            <el-table-column prop="gateway" label="网关" min-width="130" show-overflow-tooltip />
            <el-table-column prop="containers" label="容器数" width="85" align="center" />
            <el-table-column prop="id" label="网络 ID" min-width="110" show-overflow-tooltip />
            <el-table-column label="操作" min-width="80" align="center">
              <template #default="{ row }">
                <el-button
                  link
                  type="danger"
                  :disabled="['bridge', 'host', 'none'].includes(row.name)"
                  @click="removeNetwork(row)"
                >删除</el-button>
              </template>
            </el-table-column>
          </el-table>
          <el-empty v-if="!loadingN && networks.length === 0" description="暂无网络" />
        </el-tab-pane>

        <!-- 容器应用商店 -->
        <el-tab-pane label="应用商店" name="apps">
          <Skeleton v-if="loadingApps" type="card" :rows="8" />
          <el-row v-else :gutter="16">
            <el-col :span="6" v-for="app in dockerApps" :key="app.key">
              <el-card shadow="hover" class="app-card">
                <div class="app-head">
                  <div class="app-icon" :style="{ background: iconColor(app.icon) }">
                    <el-icon :size="24" color="#fff"><component :is="app.icon" /></el-icon>
                  </div>
                  <div class="app-title">
                    <div class="app-name">{{ app.name }}</div>
                    <el-tag size="small" :type="appTag(app)">{{ appText(app) }}</el-tag>
                  </div>
                </div>
                <div class="app-desc">{{ app.description }}</div>
                <div class="app-info">
                  <span>默认端口 <b>{{ app.default_port }}</b></span>
                  <span v-if="app.installed_at">安装于 {{ fmtTime(app.installed_at) }}</span>
                </div>
                <el-alert v-if="app.error" :title="app.error" type="error" :closable="false" class="app-error" />
                <div class="app-actions">
                  <template v-if="app.status === 'not_installed' || app.status === 'failed'">
                    <el-button type="primary" size="small" :disabled="!dockerOk" @click="openInstall(app)">安装</el-button>
                  </template>
                  <template v-else-if="app.status === 'installing'">
                    <el-button type="warning" size="small" disabled>安装中...</el-button>
                  </template>
                  <template v-else>
                    <el-button size="small" :type="app.running ? 'success' : 'info'" disabled>
                      {{ app.running ? '运行中' : '已停止' }}
                    </el-button>
                    <el-button size="small" type="danger" @click="uninstall(app)">卸载</el-button>
                  </template>
                </div>
                <div v-if="app.env_tips" class="app-remarks">
                  <el-icon><InfoFilled /></el-icon>
                  <span>{{ app.env_tips }}</span>
                </div>
              </el-card>
            </el-col>
          </el-row>
          <el-empty v-if="!loadingApps && dockerApps.length === 0" description="暂无可用应用" />
        </el-tab-pane>
      </el-tabs>
    </el-card>

    <!-- 创建容器弹窗 -->
    <el-dialog v-model="createVisible" title="创建容器" width="560px">
      <el-form :model="cf" label-width="90px">
        <el-form-item label="容器名称" required>
          <el-input v-model="cf.name" placeholder="如 myapp" />
        </el-form-item>
        <el-form-item label="镜像" required>
          <el-input v-model="cf.image" placeholder="如 nginx:alpine" />
        </el-form-item>
        <el-form-item label="端口映射">
          <el-select v-model="portInputs" multiple filterable allow-create default-first-option placeholder="如 8080:80，回车添加" style="width: 100%">
            <el-option v-for="p in portInputs" :key="p" :value="p" :label="p" />
          </el-select>
        </el-form-item>
        <el-form-item label="环境变量">
          <el-select v-model="envInputs" multiple filterable allow-create default-first-option placeholder="如 MYSQL_ROOT_PASSWORD=123456，回车添加" style="width: 100%">
            <el-option v-for="e in envInputs" :key="e" :value="e" :label="e" />
          </el-select>
        </el-form-item>
        <el-form-item label="卷挂载">
          <el-select v-model="volInputs" multiple filterable allow-create default-first-option placeholder="如 /data:/data，回车添加" style="width: 100%">
            <el-option v-for="v in volInputs" :key="v" :value="v" :label="v" />
          </el-select>
        </el-form-item>
        <el-form-item label="重启策略">
          <el-select v-model="cf.restart" style="width: 100%">
            <el-option label="always（总是）" value="always" />
            <el-option label="unless-stopped（除手动停止外）" value="unless-stopped" />
            <el-option label="no（不自动重启）" value="no" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="create">创建并启动</el-button>
      </template>
    </el-dialog>

    <!-- 创建网络弹窗 -->
    <el-dialog v-model="netVisible" title="创建 Docker 网络" width="500px">
      <el-form :model="nf" label-width="90px">
        <el-form-item label="网络名称" required>
          <el-input v-model="nf.name" placeholder="如 mynet" />
        </el-form-item>
        <el-form-item label="驱动">
          <el-select v-model="nf.driver" style="width: 100%">
            <el-option label="bridge（桥接，默认）" value="bridge" />
            <el-option label="overlay（跨主机）" value="overlay" />
            <el-option label="macvlan（分配 MAC）" value="macvlan" />
            <el-option label="ipvlan" value="ipvlan" />
          </el-select>
        </el-form-item>
        <el-form-item label="子网">
          <el-input v-model="nf.subnet" placeholder="如 172.20.0.0/16（可选）" />
        </el-form-item>
        <el-form-item label="网关">
          <el-input v-model="nf.gateway" placeholder="如 172.20.0.1（可选）" />
        </el-form-item>
        <el-form-item label="IP 范围">
          <el-input v-model="nf.ip_range" placeholder="如 172.20.0.2/24（可选）" />
        </el-form-item>
        <el-form-item label="内部网络">
          <el-switch v-model="nf.internal" />
          <span class="form-hint">启用后仅允许容器间通信，不提供外部访问</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="netVisible = false">取消</el-button>
        <el-button type="primary" :loading="creatingNet" @click="createNetwork">创建</el-button>
      </template>
    </el-dialog>

    <!-- 安装容器应用弹窗 -->
    <el-dialog v-model="installVisible" title="安装容器应用" width="440px">
      <p class="install-name">正在安装：<b>{{ installApp && installApp.name }}</b></p>
      <el-form label-width="90px">
        <el-form-item label="对外端口">
          <el-input-number v-model="installPort" :min="1" :max="65535" />
          <span class="form-hint">默认 {{ installApp && installApp.default_port }}</span>
        </el-form-item>
        <el-form-item v-if="installApp && installApp.need_pwd" label="密码" required>
          <el-input v-model="installPwd" type="password" show-password placeholder="设置访问/连接密码" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="installVisible = false">取消</el-button>
        <el-button type="primary" :loading="installing" @click="doInstall">安装</el-button>
      </template>
    </el-dialog>

    <!-- 容器日志抽屉 -->
    <el-drawer v-model="logVisible" :title="`容器日志 - ${logName}`" size="60%">
      <pre class="log-box">{{ logText }}</pre>
    </el-drawer>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Warning, Download } from '@element-plus/icons-vue'
import request from '../utils/request'

const activeTab = ref('containers')
const dockerStatus = ref(null)
const loading = ref(true)
const dockerOk = computed(() => dockerStatus.value && dockerStatus.value.installed && dockerStatus.value.running)

const containers = ref([])
const loadingC = ref(true)
const images = ref([])
const loadingI = ref(true)
const networks = ref([])
const loadingN = ref(true)
const dockerApps = ref([])
const loadingApps = ref(true)

const createVisible = ref(false)
const creating = ref(false)
const cf = ref({ name: '', image: '', restart: 'unless-stopped' })
const portInputs = ref([])
const envInputs = ref([])
const volInputs = ref([])

const installVisible = ref(false)
const installing = ref(false)
const installApp = ref(null)
const installPort = ref(0)
const installPwd = ref('')

const netVisible = ref(false)
const creatingNet = ref(false)
const nf = ref({ name: '', driver: 'bridge', subnet: '', gateway: '', ip_range: '', internal: false })

const logVisible = ref(false)
const logName = ref('')
const logText = ref('')

let appTimer = null

// 环境安装状态
const envStatusMap = ref({})
const installingMap = ref({})
const pendingActionMap = ref({}) // {docker:'install'|'uninstall'|''}
const installTimer = ref(null)

async function loadEnvStatus() {
  try {
    const res = await request.get('/apps/env-status')
    const data = res.data || {}
    envStatusMap.value = data
  } catch (e) {}
}

async function installDocker() {
  const meta = envStatusMap.value.docker
  if (!meta || !meta.key) {
    ElMessage.warning('暂不支持一键安装 Docker')
    return
  }
  installingMap.value.docker = true
  pendingActionMap.value.docker = 'install'
  try {
    await request.post('/apps/install', { key: meta.key })
    ElMessage.success('Docker 安装已开始，请稍候...')
    pollInstallStatus(meta.key, 'docker')
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '安装请求失败')
    installingMap.value.docker = false
    pendingActionMap.value.docker = ''
  }
}

function pollInstallStatus(key, type) {
  if (installTimer.value) clearInterval(installTimer.value)
  installTimer.value = setInterval(async () => {
    try {
      const res = await request.get('/apps/list')
      const apps = res.data || []
      const app = apps.find((a) => a.key === key)
      if (!app) return
      const action = pendingActionMap.value[type]
      if (action === 'install' && app.status === 'installed') {
        clearInterval(installTimer.value)
        installTimer.value = null
        installingMap.value[type] = false
        pendingActionMap.value[type] = ''
        ElMessage.success('Docker 安装完成')
        await loadEnvStatus()
        await load()
      } else if (action === 'uninstall' && app.status === 'not_installed') {
        clearInterval(installTimer.value)
        installTimer.value = null
        installingMap.value[type] = false
        pendingActionMap.value[type] = ''
        ElMessage.success('Docker 卸载完成')
        await loadEnvStatus()
        await load()
      } else if (app.status === 'failed') {
        clearInterval(installTimer.value)
        installTimer.value = null
        installingMap.value[type] = false
        pendingActionMap.value[type] = ''
        ElMessage.error('操作失败，请查看日志')
      }
    } catch (e) {}
  }, 3000)
}

// 进入页面/刷新时探测后端是否已有进行中的安装/卸载，避免刷新后丢失内存状态误显示"未检测到"
async function checkPendingAction(type) {
  const key = envStatusMap.value[type]?.key
  if (!key) return false
  try {
    const res = await request.get('/apps/list')
    const app = (res.data || []).find((a) => a.key === key)
    if (!app) return false
    if (app.status === 'installing') {
      installingMap.value[type] = true
      pendingActionMap.value[type] = 'install'
      pollInstallStatus(key, type)
      return true
    } else if (app.status === 'uninstalling') {
      installingMap.value[type] = true
      pendingActionMap.value[type] = 'uninstall'
      pollInstallStatus(key, type)
      return true
    }
  } catch (e) {}
  return false
}

const iconColors = ['#409eff', '#67c23a', '#e6a23c', '#f56c6c', '#909399', '#9c27b0']

function iconColor(icon) {
  let h = 0
  for (const ch of (icon || '')) h = (h * 31 + ch.charCodeAt(0)) % 997
  return iconColors[h % iconColors.length]
}
function appText(app) {
  const map = { not_installed: '未安装', installing: '安装中', installed: '已安装', failed: '失败', removed: '已移除' }
  return map[app.status] || app.status
}
function appTag(app) {
  const map = { not_installed: 'info', installing: 'warning', installed: 'success', failed: 'danger', removed: 'info' }
  return map[app.status] || 'info'
}
function fmtTime(t) {
  if (!t) return ''
  const d = new Date(t)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

async function loadStatus() {
  dockerStatus.value = (await request.get('/docker/status')).data
}

async function loadContainers() {
  if (!dockerOk.value) return
  loadingC.value = true
  try {
    containers.value = (await request.get('/docker/containers', { params: { all: 1 } })).data || []
  } finally {
    loadingC.value = false
  }
}

async function loadImages() {
  if (!dockerOk.value) return
  loadingI.value = true
  try {
    images.value = (await request.get('/docker/images')).data || []
  } finally {
    loadingI.value = false
  }
}

async function loadApps() {
  try {
    dockerApps.value = (await request.get('/docker/apps')).data || []
  } finally {
    loadingApps.value = false
  }
}

function openCreate() {
  cf.value = { name: '', image: '', restart: 'unless-stopped' }
  portInputs.value = []
  envInputs.value = []
  volInputs.value = []
  createVisible.value = true
}

async function create() {
  if (!cf.value.name || !cf.value.image) return ElMessage.warning('请填写容器名称和镜像')
  creating.value = true
  try {
    await request.post('/docker/containers/create', {
      ...cf.value,
      ports: portInputs.value,
      env: envInputs.value,
      volumes: volInputs.value
    })
    ElMessage.success('容器创建成功')
    createVisible.value = false
    loadContainers()
  } finally {
    creating.value = false
  }
}

function action(row, act) {
  request.post(`/docker/containers/${row.id}/action`, { action: act }).then(() => {
    ElMessage.success(`${act} 成功`)
    loadContainers()
  }).catch(() => {})
}

function remove(row) {
  ElMessageBox.confirm(`确定删除容器 ${row.name} 吗？`, '删除容器', {
    confirmButtonText: '删除',
    cancelButtonText: '取消',
    type: 'warning'
  }).then(async () => {
    await request.delete(`/docker/containers/${row.id}`, { params: { force: 1 } })
    ElMessage.success('已删除')
    loadContainers()
  }).catch(() => {})
}

async function showLogs(row) {
  logName.value = row.name
  logText.value = '正在加载...'
  logVisible.value = true
  const res = await request.get(`/docker/containers/${row.id}/logs`, { params: { lines: 300 } })
  logText.value = res.data.log || '（暂无日志）'
}

// ====== 网络管理 ======
async function loadNetworks() {
  if (!dockerOk.value) return
  loadingN.value = true
  try {
    networks.value = (await request.get('/docker/networks')).data || []
  } finally {
    loadingN.value = false
  }
}

function openCreateNetwork() {
  nf.value = { name: '', driver: 'bridge', subnet: '', gateway: '', ip_range: '', internal: false }
  netVisible.value = true
}

async function createNetwork() {
  if (!nf.value.name) return ElMessage.warning('请填写网络名称')
  creatingNet.value = true
  try {
    await request.post('/docker/networks/create', nf.value)
    ElMessage.success('网络创建成功')
    netVisible.value = false
    loadNetworks()
  } finally {
    creatingNet.value = false
  }
}

function removeNetwork(row) {
  ElMessageBox.confirm(`确定删除网络 ${row.name} 吗？容器接入该网络后将断开。`, '删除网络', {
    confirmButtonText: '删除',
    cancelButtonText: '取消',
    type: 'warning'
  }).then(async () => {
    await request.delete(`/docker/networks/${row.id}`)
    ElMessage.success('已删除')
    loadNetworks()
  }).catch(() => {})
}

function openInstall(app) {
  installApp.value = app
  installPort.value = app.default_port
  installPwd.value = ''
  installVisible.value = true
}

async function doInstall() {
  if (installApp.value.need_pwd && !installPwd.value) return ElMessage.warning('请设置密码')
  installing.value = true
  try {
    await request.post('/docker/apps/install', {
      key: installApp.value.key,
      port: installPort.value,
      password: installPwd.value
    })
    ElMessage.success('开始安装，请稍候...')
    installVisible.value = false
    startAppPolling()
  } finally {
    installing.value = false
  }
}

function uninstall(app) {
  ElMessageBox.confirm(`确定卸载容器应用 ${app.name} 吗？其数据卷也会一并删除！`, '卸载应用', {
    confirmButtonText: '卸载',
    cancelButtonText: '取消',
    type: 'warning'
  }).then(async () => {
    await request.post('/docker/apps/uninstall', { key: app.key })
    ElMessage.success('已卸载')
    loadApps()
  }).catch(() => {})
}

function startAppPolling() {
  stopAppPolling()
  appTimer = setInterval(loadApps, 3000)
  setTimeout(stopAppPolling, 10 * 60 * 1000)
}
function stopAppPolling() {
  if (appTimer) { clearInterval(appTimer); appTimer = null }
}

async function load() {
  // 任何情况下先显示骨架屏，查询出状态后再根据状态切换
  loading.value = true
  try {
    await loadEnvStatus()
    // 先探测后端是否有进行中的安装/卸载，避免误显示"未检测到"
    if (await checkPendingAction('docker')) {
      return
    }
    await loadStatus()
    if (dockerOk.value) {
      await Promise.all([loadContainers(), loadImages(), loadNetworks()])
    }
  } catch (e) {
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  load()
  loadApps()
})

onUnmounted(() => {
  stopAppPolling()
  if (installTimer.value) {
    clearInterval(installTimer.value)
    installTimer.value = null
  }
})
</script>

<style scoped>
.tip { margin-bottom: 16px; }
.toolbar { display: flex; align-items: center; gap: 8px; margin-bottom: 16px; }
.ver { color: #909399; font-size: 12px; }
.env-missing {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 400px;
  background: #fafafa;
  border: 1px dashed #dcdfe6;
  border-radius: 8px;
}
.env-missing-inner {
  text-align: center;
  padding: 32px;
}
.env-missing-inner h3 {
  margin: 12px 0 8px;
  font-size: 18px;
  color: #303133;
}
.env-missing-inner p {
  color: #606266;
  font-size: 14px;
  margin: 0 0 16px;
  max-width: 420px;
}
.installing-hint {
  margin-top: 12px;
  color: #409eff;
  font-size: 13px;
}
.app-card { margin-bottom: 16px; }
.app-head { display: flex; align-items: center; gap: 12px; margin-bottom: 12px; }
.app-icon {
  width: 44px; height: 44px; border-radius: 10px;
  display: flex; align-items: center; justify-content: center; flex-shrink: 0;
}
.app-title { display: flex; flex-direction: column; gap: 6px; }
.app-name { font-size: 15px; font-weight: 600; color: #303133; }
.app-desc { color: #606266; font-size: 13px; line-height: 1.6; min-height: 40px; margin-bottom: 10px; }
.app-info { display: flex; flex-direction: column; gap: 4px; color: #909399; font-size: 12px; margin-bottom: 10px; }
.app-error { margin-bottom: 10px; }
.app-actions { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 10px; }
.app-remarks {
  display: flex; align-items: flex-start; gap: 4px;
  color: #909399; font-size: 12px; line-height: 1.5;
}
.install-name { margin: 0 0 12px; color: #606266; }
.form-hint { margin-left: 8px; color: #909399; font-size: 12px; }
.log-box {
  background: #1e1e1e; color: #d4d4d4; padding: 12px;
  border-radius: 6px; font-size: 12px; line-height: 1.7;
  height: calc(100vh - 120px); overflow: auto; white-space: pre-wrap;
  word-break: break-all; margin: 0;
}
</style>
