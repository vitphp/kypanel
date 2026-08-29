<template>
  <div class="backup-page">
    <el-tabs v-model="activeTab">
      <!-- 备份记录 -->
      <el-tab-pane label="备份记录" name="list">
        <el-card shadow="never">
          <template #header>
            <div class="card-head">
              <span>备份记录</span>
              <div class="head-actions">
                <el-button type="primary" size="small" @click="createBackup">立即备份</el-button>
                <el-button size="small" @click="loadTasks">刷新</el-button>
              </div>
            </div>
          </template>
          <Skeleton v-if="tasksLoading" type="table" :rows="6" :columns="[{width:'60px'},{width:'90px'},{width:'160px'},{flex:1},{width:'100px'},{width:'80px'},{width:'80px'},{width:'160px'},{flex:1}]" />
          <el-table v-else :data="tasks" size="small">
            <el-table-column prop="id" label="ID" width="60" />
            <el-table-column prop="type" label="类型" width="90">
              <template #default="{ row }">
                <el-tag :type="typeTag(row.type)" size="small">{{ typeText(row.type) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="target" label="目标" width="160" />
            <el-table-column prop="file_name" label="文件名" show-overflow-tooltip />
            <el-table-column label="大小" width="100">
              <template #default="{ row }">{{ formatSize(row.size) }}</template>
            </el-table-column>
            <el-table-column prop="storage" label="存储" width="80">
              <template #default="{ row }">
                <el-tag :type="row.storage === 'local' ? 'info' : 'success'" size="small">{{ row.storage }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="状态" width="80">
              <template #default="{ row }">
                <el-tag :type="statusTag(row.status)" size="small">{{ statusText(row.status) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="创建时间" width="160">
              <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
            </el-table-column>
            <el-table-column label="操作" min-width="200">
              <template #default="{ row }">
                <div class="ops-cell">
                  <el-button link type="primary" size="small" @click="download(row)">下载</el-button>
                  <el-button link type="danger" size="small" @click="remove(row)">删除</el-button>
                </div>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-tab-pane>

      <!-- 远程存储 -->
      <el-tab-pane label="远程存储" name="storage">
        <el-card shadow="never">
          <template #header>
            <div class="card-head">
              <span>远程存储配置</span>
              <el-button type="primary" size="small" @click="addStorage">添加存储</el-button>
            </div>
          </template>
          <el-alert type="info" :closable="false" style="margin-bottom: 12px"
            title="支持云存储：腾讯云 COS、阿里云 OSS、七牛云 Kodo、Amazon S3、通用 S3 兼容、FTP/SFTP。"
            description="选择厂商与地域后，请自行填写域名（Endpoint）；可填内网域名或 S3 兼容端点。" />
          <Skeleton v-if="storagesLoading" type="list" :rows="2" />
          <el-empty v-else-if="!storages.length" description="尚未配置远程存储" :image-size="60" />
          <el-table v-else :data="storages" stripe>
            <el-table-column prop="name" label="名称" min-width="140" />
            <el-table-column label="类型" width="120">
              <template #default="{ row }">{{ providerLabel(row.type) }}</template>
            </el-table-column>
            <el-table-column label="域名 / 主机" min-width="200" show-overflow-tooltip>
              <template #default="{ row }">
                {{ row.type === 'ftp' ? row.host : row.endpoint }}
              </template>
            </el-table-column>
            <el-table-column label="Bucket" min-width="140" show-overflow-tooltip>
              <template #default="{ row }">{{ row.bucket || '—' }}</template>
            </el-table-column>
            <el-table-column label="状态" width="80" align="center">
              <template #default="{ row }">
                <el-tag :type="row.enabled ? 'success' : 'info'" size="small">{{ row.enabled ? '启用' : '停用' }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="140" align="center">
              <template #default="{ row }">
                <el-button link type="primary" size="small" @click="editStorage(row)">编辑</el-button>
                <el-button link type="danger" size="small" @click="removeStorage(row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-tab-pane>
    </el-tabs>

    <!-- 创建备份对话框 -->
    <el-dialog v-model="createVisible" title="创建备份" width="480px">
      <el-form label-width="90px">
        <el-form-item label="备份类型">
          <el-radio-group v-model="createForm.type">
            <el-radio label="panel">面板数据</el-radio>
            <el-radio label="site">网站</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="createForm.type === 'site'" label="网站">
          <el-select v-model="createForm.target" placeholder="选择网站" style="width: 100%">
            <el-option v-for="s in sites" :key="s.name" :label="s.name" :value="s.name" />
          </el-select>
        </el-form-item>
        <el-form-item label="储存位置">
          <el-radio-group v-model="createForm.target_type">
            <el-radio label="local">本地</el-radio>
            <el-radio label="remote">远程存储</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="createForm.target_type === 'remote'" label="远程存储" required>
          <el-select v-model="createForm.target_name" placeholder="请选择远程存储" style="width: 100%">
            <el-option v-for="s in storages" :key="s.name" :value="s.name" :label="`${s.name} (${s.type})`">
              <div class="opt-row">
                <span class="opt-label">{{ s.name }}</span>
                <span class="opt-desc">{{ s.type }}</span>
              </div>
            </el-option>
          </el-select>
          <div v-if="storages.length === 0" class="form-hint">暂无远程存储，请先到「远程存储」添加。</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="confirmCreate">开始备份</el-button>
      </template>
    </el-dialog>

    <!-- 上传远程对话框（已移除：上传功能迁移到创建备份时直接选择储存位置） -->

    <!-- 添加/编辑远程存储对话框 -->
    <el-dialog v-model="addStorageVisible" :title="editingStorageName ? '编辑远程存储' : '添加远程存储'" width="640px" :close-on-click-modal="false">
      <el-form label-width="110px">
        <el-form-item label="存储名称">
          <el-input v-model="addStorageForm.name" placeholder="如「公司备份」" />
        </el-form-item>
        <el-form-item label="存储类型">
          <el-radio-group v-model="addStorageForm.type" @change="onStorageTypeChange(addStorageForm)">
            <el-radio label="s3">通用 S3</el-radio>
            <el-radio label="cos">腾讯云 COS</el-radio>
            <el-radio label="oss">阿里云 OSS</el-radio>
            <el-radio label="kodo">七牛云 Kodo</el-radio>
            <el-radio label="s3_aws">Amazon S3</el-radio>
            <el-radio label="ftp">FTP / SFTP</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="addStorageForm.enabled" />
        </el-form-item>

        <!-- FTP 字段 -->
        <template v-if="addStorageForm.type === 'ftp'">
          <el-form-item label="FTP 主机">
            <el-input v-model="addStorageForm.host" placeholder="主机地址" style="width: 360px" />
          </el-form-item>
          <el-form-item label="端口">
            <el-input-number v-model="addStorageForm.port" :min="1" :max="65535" />
          </el-form-item>
          <el-form-item label="用户名">
            <el-input v-model="addStorageForm.user" style="width: 360px" />
          </el-form-item>
          <el-form-item label="密码">
            <el-input v-model="addStorageForm.pass" type="password" style="width: 360px" show-password />
          </el-form-item>
          <el-form-item label="远程目录">
            <el-input v-model="addStorageForm.path" placeholder="可选，如 /backup" style="width: 360px" />
          </el-form-item>
        </template>

        <!-- S3 类字段 -->
        <template v-else>
          <el-form-item label="地域">
            <el-select v-model="addStorageForm.region" placeholder="选择地域（可手动输入）" filterable allow-create style="width: 360px">
              <el-option v-for="r in regionOptions(addStorageForm.type)" :key="r.value" :label="r.label" :value="r.value" />
            </el-select>
          </el-form-item>
          <el-form-item label="域名">
            <el-input v-model="addStorageForm.endpoint" placeholder="如 https://oss-cn-hangzhou.aliyuncs.com" clearable style="width: 360px" />
          </el-form-item>
          <el-form-item label="Bucket 名称">
            <el-input v-model="addStorageForm.bucket" style="width: 360px" />
          </el-form-item>
          <el-form-item :label="providerKeyLabel(addStorageForm.type)">
            <el-input v-model="addStorageForm.access_key" style="width: 360px" />
          </el-form-item>
          <el-form-item :label="providerSecretLabel(addStorageForm.type)">
            <el-input v-model="addStorageForm.secret_key" type="password" show-password style="width: 360px" />
          </el-form-item>
          <el-form-item label="路径前缀">
            <el-input v-model="addStorageForm.path" placeholder="可选，如 kypanel-backups" style="width: 360px" />
          </el-form-item>
          <el-form-item label="参考文档" v-if="providerDocUrl(addStorageForm.type)">
            <a :href="providerDocUrl(addStorageForm.type)" target="_blank" class="doc-link">
              <el-icon><Link /></el-icon> {{ providerLabel(addStorageForm.type) }} 密钥获取说明 ↗
            </a>
          </el-form-item>
        </template>
      </el-form>
      <template #footer>
        <el-button @click="addStorageVisible = false">取消</el-button>
        <el-button type="primary" @click="confirmAddStorage">添加</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import request from '../utils/request'
import { formatBytes } from '../utils/format'
import { fmtTimeISO } from '../utils/format'

// 当前 tab：从 localStorage 恢复上次选择的 tab（按页面 key 隔离，避免影响其他页面）
const TAB_KEY = 'backup.tab'
const activeTab = ref(localStorage.getItem(TAB_KEY) || 'list')
watch(activeTab, (v) => localStorage.setItem(TAB_KEY, v))
const tasks = ref([])
const tasksLoading = ref(true)
const storages = ref([])
const storagesLoading = ref(true)
const sites = ref([])
const createVisible = ref(false)
const creating = ref(false)
const createForm = ref({ type: 'panel', target: '', target_type: 'local', target_name: '' })
const addStorageVisible = ref(false)
const addStorageForm = ref({
  type: 's3_aws', name: '', enabled: true, endpoint: '', bucket: '', region: '',
  access_key: '', secret_key: '', host: '', port: 21, user: '', pass: '', path: ''
})

function typeText(t) { return { site: '网站', panel: '面板', database: '数据库' }[t] || t }
function typeTag(t) { return { site: 'primary', panel: 'warning', database: 'success' }[t] || 'info' }
function statusText(s) { return { success: '成功', failed: '失败', running: '进行中' }[s] || s }
function statusTag(s) { return { success: 'success', failed: 'danger', running: 'warning' }[s] || 'info' }

async function loadTasks() {
  try {
    const res = await request.get('/backup/list')
    tasks.value = res.data || []
  } finally {
    tasksLoading.value = false
  }
}

async function loadStorages() {
  try {
    const res = await request.get('/backup/storages')
    storages.value = res.data || []
  } finally {
    storagesLoading.value = false
  }
}

async function loadSites() {
  try {
    const res = await request.get('/site/list')
    sites.value = res.data || []
  } catch (e) { sites.value = [] }
}

async function createBackup() {
  await loadSites()
  await loadStorages()
  createForm.value = { type: 'panel', target: '', target_type: 'local', target_name: '' }
  createVisible.value = true
}

async function confirmCreate() {
  if (createForm.value.type === 'site' && !createForm.value.target) {
    return ElMessage.warning('请选择网站')
  }
  if (createForm.value.target_type === 'remote' && !createForm.value.target_name) {
    return ElMessage.warning('请选择远程存储')
  }
  creating.value = true
  try {
    await request.post('/backup/create', createForm.value)
    ElMessage.success(createForm.value.target_type === 'remote' ? '备份完成，已上传到远程' : '备份完成')
    createVisible.value = false
    loadTasks()
  } catch (e) { /* interceptor */ } finally {
    creating.value = false
  }
}

async function download(row) {
  // 申请一次性下载 token（不泄露面板 JWT）
  try {
    const res = await request.post('/backup/download-token', { id: row.id })
    const dtoken = res.data?.token
    if (!dtoken) return ElMessage.error('获取下载链接失败')
    window.open(`/api/backup/download?id=${row.id}&token=${encodeURIComponent(dtoken)}`)
  } catch (e) {
    ElMessage.error('获取下载链接失败')
  }
}

async function remove(row) {
  try {
    await ElMessageBox.confirm(`确定删除备份「${row.file_name}」吗？`, '删除备份', { type: 'warning' })
  } catch { return }
  await request.post('/backup/delete', { id: row.id })
  ElMessage.success('已删除')
  loadTasks()
}

// 弹窗是"新增"还是"编辑"模式（编辑时保存原 name 用于后端定位）
const editingStorageName = ref('')

function addStorage() {
  editingStorageName.value = ''
  addStorageForm.value = {
    type: 's3_aws',
    name: '',
    enabled: true,
    endpoint: '',
    bucket: '',
    region: '',
    access_key: '',
    secret_key: '',
    host: '',
    port: 21,
    user: '',
    pass: '',
    path: ''
  }
  addStorageVisible.value = true
}

function editStorage(row) {
  editingStorageName.value = row.name
  addStorageForm.value = { ...row }
  addStorageVisible.value = true
}

async function confirmAddStorage() {
  // 基础校验
  if (!addStorageForm.value.name || !addStorageForm.value.name.trim()) {
    return ElMessage.warning('请填写存储名称')
  }
  if (addStorageForm.value.type === 'ftp') {
    if (!addStorageForm.value.host) return ElMessage.warning('请填写 FTP 主机')
  } else {
    if (!addStorageForm.value.bucket) return ElMessage.warning('请填写 Bucket 名称')
    if (!addStorageForm.value.access_key) return ElMessage.warning('请填写 AccessKey')
    if (!addStorageForm.value.secret_key) return ElMessage.warning('请填写 SecretKey')
  }
  try {
    if (editingStorageName.value) {
      // 编辑：按原 name 替换
      await request.post('/backup/storages', {
        storage: { ...addStorageForm.value },
        old_name: editingStorageName.value
      })
      ElMessage.success('已更新')
    } else {
      await request.post('/backup/storages', { storage: { ...addStorageForm.value } })
      ElMessage.success('已添加')
    }
    addStorageVisible.value = false
    await loadStorages()
  } catch (e) {
    ElMessage.error(e?.response?.data?.msg || e?.message || '保存失败')
  }
}

async function removeStorage(row) {
  ElMessageBox.confirm(`确定删除存储「${row.name}」吗？`, '删除存储', {
    confirmButtonText: '删除', cancelButtonText: '取消', type: 'warning'
  }).then(async () => {
    try {
      await request.post('/backup/storage-delete', { name: row.name })
      ElMessage.success('已删除')
      await loadStorages()
    } catch (e) {
      ElMessage.error(e?.response?.data?.msg || e?.message || '删除失败')
    }
  }).catch(() => {})
}

// ===== 厂商预设（Endpoint 模板 + 常用地域） =====
// 弹窗中「域名」字段默认留空，用户确认添加时若仍为空则按厂商模板 + 地域自动拼接。

const PROVIDER_META = {
  s3:      { label: '通用 S3', endpoint: '',     key: 'AccessKey', secret: 'SecretKey', doc: 'https://docs.min.io/cn/' },
  cos:     { label: '腾讯云 COS', endpoint: 'https://cos.{region}.myqcloud.com', key: 'SecretId', secret: 'SecretKey', doc: 'https://cloud.tencent.com/document/product/5/43618' },
  oss:     { label: '阿里云 OSS', endpoint: 'https://oss-cn-hangzhou.aliyuncs.com', key: 'AccessKey ID', secret: 'AccessKey Secret', doc: 'https://help.aliyun.com/zh/oss/' },
  kodo:    { label: '七牛云 Kodo', endpoint: 'https://s3-{region}.qiniucs.com', key: 'AccessKey', secret: 'SecretKey', doc: 'https://developer.qiniu.com/kodo' },
  s3_aws:  { label: 'Amazon S3', endpoint: 'https://s3.us-east-1.amazonaws.com', key: 'Access Key ID', secret: 'Secret Access Key', doc: 'https://docs.aws.amazon.com/s3/' },
  ftp:     { label: 'FTP / SFTP', endpoint: '', key: '', secret: '', doc: '' }
}

const REGION_LISTS = {
  cos: [
    { label: '中国大陆 - 北京', value: 'ap-beijing' },
    { label: '中国大陆 - 南京', value: 'ap-nanjing' },
    { label: '中国大陆 - 上海', value: 'ap-shanghai' },
    { label: '中国大陆 - 广州', value: 'ap-guangzhou' },
    { label: '中国大陆 - 成都', value: 'ap-chengdu' },
    { label: '中国大陆 - 重庆', value: 'ap-chongqing' },
    { label: '中国大陆 - 深圳金融', value: 'ap-shenzhen-fsi' },
    { label: '中国大陆 - 上海金融', value: 'ap-shanghai-fsi' },
    { label: '中国大陆 - 北京金融', value: 'ap-beijing-fsi' },
    { label: '亚太 - 中国香港', value: 'ap-hongkong' },
    { label: '亚太 - 新加坡', value: 'ap-singapore' },
    { label: '亚太 - 雅加达', value: 'ap-jakarta' },
    { label: '亚太 - 首尔', value: 'ap-seoul' },
    { label: '亚太 - 曼谷', value: 'ap-bangkok' },
    { label: '亚太 - 东京', value: 'ap-tokyo' },
    { label: '中东 - 利雅得', value: 'me-saudi-arabia' },
    { label: '北美 - 硅谷（美西）', value: 'na-siliconvalley' },
    { label: '北美 - 弗吉尼亚（美东）', value: 'na-ashburn' },
    { label: '南美 - 圣保罗', value: 'sa-saopaulo' },
    { label: '欧洲 - 法兰克福', value: 'eu-frankfurt' }
  ],
  oss: [
    { label: '华东1（杭州）', value: 'cn-hangzhou' },
    { label: '华东2（上海）', value: 'cn-shanghai' },
    { label: '华北1（青岛）', value: 'cn-qingdao' },
    { label: '华北2（北京）', value: 'cn-beijing' },
    { label: '华北3（张家口）', value: 'cn-zhangjiakou' },
    { label: '华北5（呼和浩特）', value: 'cn-huhehaote' },
    { label: '华北6（乌兰察布）', value: 'cn-wulanchabu' },
    { label: '华南1（深圳）', value: 'cn-shenzhen' },
    { label: '华南2（河源）', value: 'cn-heyuan' },
    { label: '华南3（广州）', value: 'cn-guangzhou' },
    { label: '西南1（成都）', value: 'cn-chengdu' },
    { label: '西北2（中卫）', value: 'cn-zhongwei' },
    { label: '中国香港', value: 'cn-hongkong' },
    { label: '亚太 - 日本（东京）', value: 'ap-northeast-1' },
    { label: '亚太 - 韩国（首尔）', value: 'ap-northeast-2' },
    { label: '亚太 - 新加坡', value: 'ap-southeast-1' },
    { label: '亚太 - 马来西亚（吉隆坡）', value: 'ap-southeast-3' },
    { label: '亚太 - 印度尼西亚（雅加达）', value: 'ap-southeast-5' },
    { label: '亚太 - 菲律宾（马尼拉）', value: 'ap-southeast-6' },
    { label: '亚太 - 泰国（曼谷）', value: 'ap-southeast-7' },
    { label: '亚太 - 马来西亚（柔佛州）', value: 'ap-southeast-8' },
    { label: '美国 - 硅谷', value: 'us-west-1' },
    { label: '美国 - 弗吉尼亚', value: 'us-east-1' },
    { label: '南美 - 巴西（圣保罗）', value: 'sa-east-1' },
    { label: '欧洲 - 德国（法兰克福）', value: 'eu-central-1' },
    { label: '欧洲 - 英国（伦敦）', value: 'eu-west-1' },
    { label: '欧洲 - 法国（巴黎）', value: 'eu-west-2' },
    { label: '北美 - 墨西哥', value: 'na-south-1' },
    { label: '中东 - 阿联酋（迪拜）', value: 'me-east-1' }
  ],
  kodo: [
    { label: '华东-浙江', value: 'z0' },
    { label: '华北-河北', value: 'z1' },
    { label: '华南-广东', value: 'z2' },
    { label: '华东-浙江2', value: 'cn-east-2' },
    { label: '北美-洛杉矶', value: 'na0' },
    { label: '亚太-新加坡', value: 'as0' },
    { label: '亚太-新加坡2', value: 'ap-southeast-2' }
  ],
  s3_aws: [
    { label: '美东（俄亥俄）', value: 'us-east-2' },
    { label: '美东（弗吉尼亚）', value: 'us-east-1' },
    { label: '美西（俄勒冈）', value: 'us-west-2' },
    { label: '美西（加利福尼亚）', value: 'us-west-1' },
    { label: '亚太（东京）', value: 'ap-northeast-1' },
    { label: '亚太（首尔）', value: 'ap-northeast-2' },
    { label: '亚太（新加坡）', value: 'ap-southeast-1' },
    { label: '亚太（悉尼）', value: 'ap-southeast-2' },
    { label: '亚太（雅加达）', value: 'ap-southeast-3' },
    { label: '亚太（孟买）', value: 'ap-south-1' },
    { label: '欧洲（法兰克福）', value: 'eu-central-1' },
    { label: '欧洲（爱尔兰）', value: 'eu-west-1' },
    { label: '欧洲（伦敦）', value: 'eu-west-2' },
    { label: '欧洲（巴黎）', value: 'eu-west-3' },
    { label: '欧洲（米兰）', value: 'eu-south-1' },
    { label: '欧洲（斯德哥尔摩）', value: 'eu-north-1' },
    { label: '加拿大（中部）', value: 'ca-central-1' },
    { label: '南美（圣保罗）', value: 'sa-east-1' },
    { label: '中东（巴林）', value: 'me-south-1' },
    { label: '中东（阿联酋）', value: 'me-central-1' },
    { label: '非洲（开普敦）', value: 'af-south-1' }
  ]
}

function providerLabel(t) { return (PROVIDER_META[t] && PROVIDER_META[t].label) || t }
function providerKeyLabel(t) { return (PROVIDER_META[t] && PROVIDER_META[t].key) || 'AccessKey' }
function providerSecretLabel(t) { return (PROVIDER_META[t] && PROVIDER_META[t].secret) || 'SecretKey' }
function providerDocUrl(t) { return (PROVIDER_META[t] && PROVIDER_META[t].doc) || '' }
function regionOptions(t) { return REGION_LISTS[t] || [] }

// 切换到 FTP 时清空 endpoint（FTP 不用 endpoint）
function onStorageTypeChange(st) {
  if (st && st.type === 'ftp') {
    st.endpoint = ''
  }
}

function formatSize(n) {
  return formatBytes(n)
}

function fmtTime(t) {
  return fmtTimeISO(t)
}

onMounted(() => {
  loadTasks()
  loadStorages()
  loadSites()
})
</script>

<style scoped>
.card-head { display: flex; justify-content: space-between; align-items: center; }
.head-actions { display: flex; gap: 8px; }
.storage-item { padding: 12px 0; border-bottom: 1px solid #f0f0f0; }
.storage-item:last-child { border-bottom: none; }
.storage-row { display: flex; align-items: center; gap: 10px; margin-bottom: 8px; }
.storage-fields { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.ops-cell { display: inline-flex; flex-wrap: wrap; gap: 0; }
.doc-link {
  font-size: 12px; color: #6366f1;
  display: inline-flex; align-items: center; gap: 4px;
  text-decoration: none;
}
.doc-link:hover { text-decoration: underline; }
</style>
