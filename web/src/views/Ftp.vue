<template>
  <div class="ftp-page">
    <el-card shadow="never">
      <div class="toolbar">
        <el-button type="primary" :disabled="!ftpAvailable" @click="openCreate">
          <el-icon><Plus /></el-icon>&nbsp;创建 FTP 用户
        </el-button>
        <el-button @click="load">刷新</el-button>
        <div class="spacer" />
        <span class="hint">FTP 用户基于系统用户，仅限 FTP 登录（nologin），家目录与网站根目录对应</span>
      </div>

      <!-- 首次加载骨架屏 -->
      <Skeleton v-if="loading" type="table" :rows="6" :columns="[{flex:1},{flex:2},{flex:1},{width:'110px'},{width:'120px'}]" />

      <!-- 安装/卸载中遮罩 -->
      <div v-else-if="installingMap.ftp" class="env-missing">
        <div class="env-missing-inner">
          <el-icon :size="48" color="#409eff"><Warning /></el-icon>
          <h3 v-if="pendingActionMap.ftp === 'uninstall'">正在卸载 FTP 服务...</h3>
          <h3 v-else>正在安装 FTP 服务...</h3>
          <p>{{ pendingActionMap.ftp === 'uninstall' ? '卸载进行中，请稍候...' : '安装进行中，请稍候...' }}</p>
        </div>
      </div>

      <!-- 环境未安装提示 -->
      <div v-else-if="!ftpAvailable" class="env-missing">
        <div class="env-missing-inner">
          <el-icon :size="48" color="#e6a23c"><Warning /></el-icon>
          <h3>未检测到 FTP 服务环境</h3>
          <p>{{ envStatusMap.ftp?.remarks || ftpMsg || '请先安装 vsftpd 服务' }}</p>
          <div v-if="envStatusMap.ftp?.select_version" class="version-row">
            <span>选择版本：</span>
            <el-select v-model="selectedVersionMap.ftp" size="default" style="width: 160px;">
              <el-option v-for="v in envStatusMap.ftp?.versions" :key="v" :label="v" :value="v" />
            </el-select>
          </div>
          <el-button type="primary" size="large" :loading="installingMap.ftp" @click="installEnv">
            <el-icon><Download /></el-icon> 一键安装
          </el-button>
          <p v-if="installingMap.ftp" class="installing-hint">安装进行中，请稍候...</p>
        </div>
      </div>

      <template v-else>
        <el-table :data="list" stripe>
          <el-table-column prop="username" label="用户名" min-width="140" />
          <el-table-column prop="home_dir" label="家目录" min-width="200" show-overflow-tooltip />
          <el-table-column prop="remark" label="备注" min-width="120" />
          <el-table-column label="状态" width="110" align="center">
            <template #default="{ row }">
              <el-tag :type="row.status === 'enabled' ? 'success' : 'info'">
                {{ row.status === 'enabled' ? '正常' : '已禁用' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="操作" min-width="120" align="center">
            <template #default="{ row }">
              <div class="ops-cell">
                <el-button link :type="row.status === 'enabled' ? 'warning' : 'success'" @click="toggle(row)">
                  {{ row.status === 'enabled' ? '禁用' : '启用' }}
                </el-button>
                <el-button type="danger" link @click="remove(row)">删除</el-button>
              </div>
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-if="!loading && list.length === 0" description="暂无 FTP 用户" />
      </template>
    </el-card>

    <!-- 创建弹窗 -->
    <el-dialog v-model="createVisible" title="创建 FTP 用户" width="480px">
      <el-form :model="form" label-width="90px">
        <el-form-item label="用户名" required>
          <el-input v-model="form.username" placeholder="如 myftp（字母/数字/下划线）" />
        </el-form-item>
        <el-form-item label="密码" required>
          <el-input v-model="form.password" type="password" show-password placeholder="至少 6 位" />
        </el-form-item>
        <el-form-item label="家目录" required>
          <el-input v-model="form.home_dir" placeholder="如 /www/wwwroot/myftp" />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.remark" placeholder="选填" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="create">创建</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Warning, Download } from '@element-plus/icons-vue'
import request from '../utils/request'

const ftpAvailable = ref(true)
const ftpMsg = ref('')
const loading = ref(true)
const list = ref([])
const createVisible = ref(false)
const creating = ref(false)
const form = ref({ username: '', password: '', home_dir: '', remark: '' })

// 环境安装状态
const envStatusMap = ref({})
const installingMap = ref({})
const pendingActionMap = ref({}) // {ftp:'install'|'uninstall'|''}
const selectedVersionMap = ref({})
const installTimer = ref(null)

async function loadEnvStatus() {
  try {
    const res = await request.get('/apps/env-status')
    const data = res.data || {}
    envStatusMap.value = data
    if (data.ftp) {
      // 只有「二进制存在 + 版本探测成功」才算真正可用，
      // 否则展示未安装遮罩（避免装了但起不来还能进入页面误导用户）
      // 只要 vsftpd 二进制存在即视为可用；版本仅用于展示，不作为可用性门槛
      ftpAvailable.value = !!data.ftp.installed
      ftpMsg.value = data.ftp.remarks || ''
      if (data.ftp.select_version && !selectedVersionMap.value.ftp) {
        selectedVersionMap.value.ftp = data.ftp.version_default || data.ftp.versions?.[0] || ''
      }
    }
  } catch (e) {
    // 静默失败，使用原接口兜底
    const res = await request.get('/ftp/status')
    ftpAvailable.value = !!res.data.available
    ftpMsg.value = res.data.message || ''
  }
}

async function installEnv() {
  const meta = envStatusMap.value.ftp
  if (!meta || !meta.key) {
    ElMessage.warning('暂不支持一键安装 FTP 服务')
    return
  }
  installingMap.value.ftp = true
  pendingActionMap.value.ftp = 'install'
  try {
    const payload = { key: meta.key }
    if (meta.select_version && selectedVersionMap.value.ftp) {
      payload.version = selectedVersionMap.value.ftp
    }
    await request.post('/apps/install', payload)
    ElMessage.success('安装已开始，请稍候...')
    pollInstallStatus(meta.key, 'ftp')
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '安装请求失败')
    installingMap.value.ftp = false
    pendingActionMap.value.ftp = ''
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
        ElMessage.success('安装完成')
        await loadEnvStatus()
        await load()
      } else if (action === 'uninstall' && app.status === 'not_installed') {
        clearInterval(installTimer.value)
        installTimer.value = null
        installingMap.value[type] = false
        pendingActionMap.value[type] = ''
        ElMessage.success('卸载完成')
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

async function loadStatus() {
  const res = await request.get('/ftp/status')
  ftpAvailable.value = !!res.data.available
  ftpMsg.value = res.data.message || ''
}

async function load() {
  // 任何情况下先显示骨架屏，查询出状态后再根据状态切换
  loading.value = true
  try {
    await loadEnvStatus()
    // 先探测后端是否有进行中的安装/卸载，避免误显示"未检测到"
    if (await checkPendingAction('ftp')) {
      return
    }
    await loadStatus()
    const res = await request.get('/ftp/list')
    list.value = res.data
  } catch (e) {
  } finally {
    loading.value = false
  }
}

function openCreate() {
  form.value = { username: '', password: '', home_dir: '', remark: '' }
  createVisible.value = true
}

async function create() {
  if (!form.value.username) return ElMessage.warning('请填写用户名')
  if (!form.value.password || form.value.password.length < 6) return ElMessage.warning('密码至少 6 位')
  if (!form.value.home_dir) return ElMessage.warning('请填写家目录')
  creating.value = true
  try {
    await request.post('/ftp/create', form.value)
    ElMessage.success('FTP 用户创建成功')
    createVisible.value = false
    load()
  } finally {
    creating.value = false
  }
}

function toggle(row) {
  const enable = row.status !== 'enabled'
  request.post('/ftp/toggle', { id: row.id, enable }).then(() => {
    ElMessage.success(enable ? '已启用' : '已禁用')
    load()
  }).catch(() => {})
}

function remove(row) {
  ElMessageBox.confirm(`确定删除 FTP 用户 ${row.username} 吗？`, '删除用户', {
    confirmButtonText: '删除',
    cancelButtonText: '取消',
    type: 'warning'
  }).then(async () => {
    await request.post('/ftp/delete', { id: row.id })
    ElMessage.success('删除成功')
    load()
  }).catch(() => {})
}

onMounted(() => {
  load()
})

onUnmounted(() => {
  if (installTimer.value) {
    clearInterval(installTimer.value)
    installTimer.value = null
  }
})
</script>

<style scoped>
.tip { margin-bottom: 16px; }
.toolbar { display: flex; align-items: center; gap: 8px; margin-bottom: 16px; }
.spacer { flex: 1; }
.hint { color: #909399; font-size: 12px; }
.env-missing {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 280px;
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
.version-row {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  margin-bottom: 16px;
  font-size: 14px;
  color: #606266;
}
.installing-hint {
  margin-top: 12px;
  color: #409eff;
  font-size: 13px;
}
</style>
