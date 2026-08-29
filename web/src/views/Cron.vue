<template>
  <div class="cron-page">
    <el-card shadow="never">
      <div class="toolbar">
        <el-button type="primary" @click="openCreate">
          <el-icon><Plus /></el-icon>&nbsp;添加计划任务
        </el-button>
        <el-button @click="load">刷新</el-button>
        <div class="spacer" />
      </div>

      <Skeleton v-if="loading" type="table" :rows="6" :columns="[{flex:1},{width:'170px'},{flex:2},{width:'90px'},{width:'170px'},{width:'150px'},{width:'160px'}]" />
      <el-table v-else :data="list" stripe>
        <el-table-column prop="name" label="名称" min-width="160" />
        <el-table-column label="执行周期" width="150">
          <template #default="{ row }">
            <span class="spec-text">{{ specToText(row.spec) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="执行次数" width="100" align="center">
          <template #default="{ row }">
            <el-button v-if="isBackupTask(row)" type="primary" link size="small" @click="openBackupFiles(row)">
              {{ row.run_count || 0 }}
            </el-button>
            <span v-else class="muted">{{ row.run_count || 0 }}</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="90" align="center">
          <template #default="{ row }">
            <el-tag :type="row.status === 'enabled' ? 'success' : 'info'">
              {{ row.status === 'enabled' ? '启用' : '停用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="上次执行" width="170">
          <template #default="{ row }">
            <span v-if="row.last_run">{{ fmtTime(row.last_run) }}</span>
            <span v-else class="muted">从未执行</span>
          </template>
        </el-table-column>
        <el-table-column label="结果" min-width="150" show-overflow-tooltip>
          <template #default="{ row }">
            <span :class="{ 'ok': row.last_result === '执行成功', 'bad': row.last_result && row.last_result !== '执行成功' }">
              {{ row.last_result || '-' }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="操作" min-width="160" align="center">
          <template #default="{ row }">
            <div class="ops-cell">
              <el-button link type="success" @click="run(row)">执行</el-button>
              <el-button link type="info" @click="showLog(row)">日志</el-button>
              <el-button link type="primary" @click="edit(row)">编辑</el-button>
              <el-button link :type="row.status === 'enabled' ? 'warning' : 'success'" @click="toggle(row)">
                {{ row.status === 'enabled' ? '停用' : '启用' }}
              </el-button>
              <el-button link type="danger" @click="remove(row)">删除</el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && list.length === 0" description="暂无计划任务" />
    </el-card>

    <!-- 创建/编辑弹窗：两步式 -->
    <el-dialog v-model="dialogVisible" :title="form.id ? '编辑计划任务' : '添加计划任务'" width="640px" top="5vh">
      <el-form :model="form" label-width="100px" v-loading="loadingData">
        <!-- 任务类型（仅新建时选择，编辑时锁定） -->
        <el-form-item label="任务类型" required>
          <el-select v-if="!form.id" v-model="form.template" placeholder="请选择任务类型" style="width: 100%" @change="onTemplateChange" filterable>
            <el-option-group v-for="grp in groupedTemplates" :key="grp.group" :label="groupLabel(grp.group)">
              <el-option v-for="t in grp.items" :key="t.key" :value="t.key" :label="t.label">
                <div class="opt-row">
                  <el-icon class="opt-icon"><component :is="t.icon" /></el-icon>
                  <span class="opt-label">{{ t.label }}</span>
                  <span class="opt-desc">{{ t.description }}</span>
                </div>
              </el-option>
            </el-option-group>
          </el-select>
          <el-tag v-else type="info" size="large">
            <el-icon style="margin-right: 4px"><component :is="currentTemplate?.icon" /></el-icon>
            {{ currentTemplate?.label || form.template }}
          </el-tag>
        </el-form-item>

        <!-- 任务名称 -->
        <el-form-item label="任务名称" required>
          <el-input v-model="form.name" placeholder="给任务起个名字" />
        </el-form-item>

        <!-- 动态字段 -->
        <template v-if="currentTemplate">
          <el-form-item v-if="hasNeed('site')" label="选择站点" required>
            <el-select v-model="form.site_name" multiple collapse-tags collapse-tags-tooltip placeholder="可多选；选「全部」备份所有站点" style="width: 100%" @change="onSiteChange">
              <el-option value="all" label="全部站点">
                <div class="opt-row"><span class="opt-label">全部站点</span><span class="opt-desc">备份所有站点（推荐）</span></div>
              </el-option>
              <el-option v-for="s in sites" :key="s.name" :value="s.name" :label="s.name">
                <div class="opt-row">
                  <span class="opt-label">{{ s.name }}</span>
                  <span class="opt-desc">{{ s.root }}</span>
                </div>
              </el-option>
            </el-select>
          </el-form-item>

          <el-form-item v-if="hasNeed('database')" label="选择数据库" required>
            <el-select v-model="form.database" multiple collapse-tags collapse-tags-tooltip placeholder="可多选；选「全部」备份所有数据库" style="width: 100%" filterable>
              <el-option v-if="!mysqlAvailable" label="未安装 MySQL/MariaDB，请先在应用商店安装" value="" disabled />
              <el-option value="all" label="全部数据库">
                <div class="opt-row"><span class="opt-label">全部数据库</span><span class="opt-desc">备份所有 MySQL 数据库</span></div>
              </el-option>
              <el-option v-for="d in databases" :key="d.name" :value="d.name" :label="d.name" />
            </el-select>
            <div v-if="!mysqlAvailable" class="form-hint">未检测到 MySQL/MariaDB，无法备份数据库。请先在「应用商店」安装 MySQL 或 MariaDB。</div>
          </el-form-item>

          <!-- 备份目标：本地 或 远程存储（仅备份网站/数据库模板显示） -->
          <el-form-item v-if="currentTemplate && (currentTemplate.key === 'backup_site' || currentTemplate.key === 'backup_db' || currentTemplate.key === 'backup_db_incremental')" label="储存位置">
            <el-radio-group v-model="form.target_type" @change="onTargetTypeChange">
              <el-radio label="local">本地</el-radio>
              <el-radio label="remote">远程存储</el-radio>
            </el-radio-group>
          </el-form-item>
          <template v-if="currentTemplate && (currentTemplate.key === 'backup_site' || currentTemplate.key === 'backup_db' || currentTemplate.key === 'backup_db_incremental') && form.target_type === 'remote'">
            <el-form-item label="远程存储" required>
              <el-select v-model="form.target_name" placeholder="请选择远程存储" style="width: 100%">
                <el-option v-for="s in storages" :key="s.name" :value="s.name" :label="`${s.name} (${s.type})`">
                  <div class="opt-row">
                    <span class="opt-label">{{ s.name }}</span>
                    <span class="opt-desc">{{ s.type }}{{ s.enabled ? '' : '（已停用）' }}</span>
                  </div>
                </el-option>
              </el-select>
              <div v-if="storages.length === 0" class="form-hint">暂无远程存储，请先到「备份中心 → 远程存储」添加。</div>
            </el-form-item>
            <el-form-item label="保留份数">
              <el-input-number v-model="form.remote_keep" :min="1" :max="999" />
              <span class="form-hint">份（超过自动清理最旧的；选远程时不再保存到本地）</span>
            </el-form-item>
          </template>

          <el-form-item v-if="hasNeed('dir')" label="目录路径" required>
            <el-input v-model="form.dir" placeholder="如 /www/wwwroot 或 /var/log">
              <template #append>
                <el-dropdown trigger="click" @command="(c) => form.dir = c">
                  <el-button><el-icon><ArrowDown /></el-icon>常用</el-button>
                  <template #dropdown>
                    <el-dropdown-menu>
                      <el-dropdown-item v-for="d in commonDirs" :key="d" :command="d">{{ d }}</el-dropdown-item>
                    </el-dropdown-menu>
                  </template>
                </el-dropdown>
              </template>
            </el-input>
          </el-form-item>

          <el-form-item v-if="hasNeed('url')" label="URL 地址" required>
            <el-input v-model="form.url" placeholder="https://example.com/endpoint" />
          </el-form-item>

          <el-form-item v-if="hasNeed('days')" label="清理多少天前的">
            <el-input-number v-model="form.days" :min="1" :max="365" />
            <span class="form-hint">天的日志文件会被删除</span>
          </el-form-item>

          <el-form-item v-if="hasNeed('keep') && form.target_type !== 'remote'" label="保留份数">
            <el-input-number v-model="form.keep" :min="1" :max="90" />
            <span class="form-hint">份</span>
          </el-form-item>

          <el-form-item v-if="hasNeed('format')" label="压缩格式">
            <el-radio-group v-model="form.format">
              <el-radio label="tar.gz">tar.gz（推荐）</el-radio>
              <el-radio label="zip">zip</el-radio>
            </el-radio-group>
          </el-form-item>

          <el-form-item v-if="hasNeed('script')" label="脚本内容" required>
            <el-input v-model="form.script" type="textarea" :rows="6" placeholder="逐行输入 shell 命令" spellcheck="false" />
          </el-form-item>
        </template>

        <!-- 执行周期：可视化选择器 + cron 预览 -->
        <el-form-item label="执行周期" required>
          <div class="schedule">
            <el-select v-model="schedule.kind" style="width: 130px" @change="rebuildSpec">
              <el-option label="每分钟" value="minute" />
              <el-option label="每小时" value="hour" />
              <el-option label="每天" value="day" />
              <el-option label="每周" value="week" />
              <el-option label="每月" value="month" />
            </el-select>
            <template v-if="schedule.kind === 'minute'">
              <span class="schedule-sep">每</span>
              <el-input-number v-model="schedule.interval" :min="1" :max="59" @change="rebuildSpec" />
              <span class="schedule-sep">分钟</span>
            </template>
            <template v-else-if="schedule.kind === 'hour'">
              <span class="schedule-sep">每小时的</span>
              <el-input-number v-model="schedule.minute" :min="0" :max="59" @change="rebuildSpec" />
              <span class="schedule-sep">分</span>
            </template>
            <template v-else-if="schedule.kind === 'day'">
              <el-input-number v-model="schedule.hour" :min="0" :max="23" @change="rebuildSpec" />
              <span class="schedule-sep">时</span>
              <el-input-number v-model="schedule.minute" :min="0" :max="59" @change="rebuildSpec" />
              <span class="schedule-sep">分</span>
            </template>
            <template v-else-if="schedule.kind === 'week'">
              <el-select v-model="schedule.weekday" style="width: 100px" @change="rebuildSpec">
                <el-option v-for="(w, i) in weekOptions" :key="i" :value="i" :label="w" />
              </el-select>
              <el-input-number v-model="schedule.hour" :min="0" :max="23" @change="rebuildSpec" />
              <span class="schedule-sep">时</span>
              <el-input-number v-model="schedule.minute" :min="0" :max="59" @change="rebuildSpec" />
              <span class="schedule-sep">分</span>
            </template>
            <template v-else-if="schedule.kind === 'month'">
              <span class="schedule-sep">每月</span>
              <el-input-number v-model="schedule.day" :min="1" :max="28" @change="rebuildSpec" />
              <span class="schedule-sep">号</span>
              <el-input-number v-model="schedule.hour" :min="0" :max="23" @change="rebuildSpec" />
              <span class="schedule-sep">时</span>
              <el-input-number v-model="schedule.minute" :min="0" :max="59" @change="rebuildSpec" />
              <span class="schedule-sep">分</span>
            </template>
          </div>
        </el-form-item>

        <!-- 命令预览（只读） -->
        <!-- （隐藏命令预览，由后端透明生成；用户无需关心） -->

        <el-form-item label="备注">
          <el-input v-model="form.remark" placeholder="选填" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </template>
    </el-dialog>

    <!-- 执行日志弹窗 -->
    <el-dialog v-model="logVisible" :title="`执行日志 - ${logTaskName}`" width="720px" top="8vh">
      <div class="log-toolbar">
        <el-button size="small" @click="loadLog">刷新</el-button>
        <span class="hint">最近 200 行输出</span>
      </div>
      <pre class="cron-log-box">{{ cronLog || '（加载中...）' }}</pre>
    </el-dialog>

    <!-- 备份文件列表弹窗 -->
    <el-dialog v-model="backupVisible" :title="`备份文件 - ${backupTaskName}`" width="780px" top="8vh">
      <div class="log-toolbar">
        <el-button size="small" @click="loadBackupFiles">刷新</el-button>
        <span class="hint">目录：{{ backupDir }}（最多保留份数由任务设置自动清理）</span>
      </div>
      <el-table :data="backupFiles" stripe v-loading="backupLoading" empty-text="暂无备份文件">
        <el-table-column prop="name" label="文件名" min-width="280" />
        <el-table-column prop="size" label="大小" width="110">
          <template #default="{ row }">{{ fmtBytes(row.size) }}</template>
        </el-table-column>
        <el-table-column prop="mtime" label="备份时间" width="180">
          <template #default="{ row }">{{ fmtTime(row.mtime * 1000) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="160" align="center">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="downloadBackup(row)">下载</el-button>
            <el-button link type="danger" size="small" @click="deleteBackup(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Plus, ArrowDown, Document, MagicStick, FolderOpened, Coin, Files, Box,
  Delete, Cpu, Clock, WarningFilled, Link, Position, Refresh
} from '@element-plus/icons-vue'
import request from '../utils/request'
import { fmtTimeStamp } from '../utils/format'

const list = ref([])
const loading = ref(true)
const loadingData = ref(false)
const dialogVisible = ref(false)
const saving = ref(false)

const templates = ref([])
const sites = ref([])
const databases = ref([])
const storages = ref([]) // 远程存储列表（备份任务可选目标）
// MySQL/MariaDB 是否可用（不可用时备份数据库下拉框显示禁用提示）
const mysqlAvailable = ref(true)
// 是否已尝试加载过数据库列表（避免反复请求）
const dbLoaded = ref(false)

const weekOptions = ['周一', '周二', '周三', '周四', '周五', '周六', '周日']
const commonDirs = ['/www/wwwroot', '/www/wwwlogs', '/backup', '/root', '/var/log', '/tmp']

const form = ref(emptyForm())
const schedule = ref(emptySchedule())
const generatedCommand = ref('')

function emptyForm() {
  return {
    id: 0,
    template: '',
    name: '',
    site_name: '',     // 数组（多选）；"all" 代表全部
    site_root: '',     // 数组（多选），与 site_name 一一对应
    database: '',      // 数组（多选）；"all" 代表全部
    dir: '',
    url: '',
    method: 'GET',
    script: '',
    days: 30,
    keep: 7,
    format: 'tar.gz',
    spec: '0 3 * * *',
    remark: '',
    target_type: 'local',
    target_name: '',
    remote_keep: 7
  }
}
function emptySchedule() {
  return { kind: 'day', interval: 5, minute: 0, hour: 3, weekday: 1, day: 1 }
}

const groupedTemplates = computed(() => {
  const map = {}
  for (const t of templates.value) {
    if (!map[t.group]) map[t.group] = []
    map[t.group].push(t)
  }
  return [
    { group: 'common', items: map.common || [] },
    { group: 'backup', items: map.backup || [] },
    { group: 'maintenance', items: map.maintenance || [] },
    { group: 'network', items: map.network || [] }
  ].filter((g) => g.items.length > 0)
})
function groupLabel(g) {
  return { common: '常用', backup: '备份', maintenance: '维护', network: '网络' }[g] || g
}
const currentTemplate = computed(() => {
  return templates.value.find((t) => t.key === form.value.template) || null
})

function hasNeed(key) {
  return currentTemplate.value && (currentTemplate.value.needs || []).includes(key)
}

async function load() {
  loading.value = true
  try {
    list.value = (await request.get('/cron/list')).data || []
  } catch (e) {
    list.value = []
  } finally {
    loading.value = false
  }
  // 异步加载各备份任务的备份数（不阻塞列表渲染）
  loadAllBackupCounts().catch(() => {})
}
async function loadTemplates() {
  templates.value = (await request.get('/cron/templates')).data || []
}
async function loadSites() {
  try {
    sites.value = (await request.get('/cron/sites')).data || []
  } catch (e) {
    sites.value = []
  }
}
async function loadDatabases() {
  try {
    const res = await request.get('/cron/databases')
    const data = res.data || {}
    mysqlAvailable.value = data.available !== false
    databases.value = data.databases || []
  } catch (e) {
    mysqlAvailable.value = false
    databases.value = []
  }
  dbLoaded.value = true
}
async function loadStorages() {
  try {
    const res = await request.get('/backup/storages')
    storages.value = (res.data || []).filter((s) => s.enabled)
  } catch (e) {
    storages.value = []
  }
}

async function openCreate() {
  form.value = emptyForm()
  schedule.value = emptySchedule()
  generatedCommand.value = ''
  mysqlAvailable.value = true
  dbLoaded.value = false
  databases.value = []
  dialogVisible.value = true
  loadingData.value = true
  try {
    await Promise.all([loadTemplates(), loadSites(), loadStorages()])
  } finally {
    loadingData.value = false
  }
}

function edit(row) {
  // 把后端的逗号分隔字符串解析回数组（多选回显）
  const parseList = (v) => {
    if (!v) return []
    return v.split(',').map((s) => s.trim()).filter(Boolean)
  }
  form.value = {
    id: row.id,
    template: row.template || 'shell',
    name: row.name,
    site_name: parseList(row.site_name),
    site_root: parseList(row.site_root),
    database: parseList(row.database),
    dir: row.dir || '',
    url: row.url || '',
    method: row.method || 'GET',
    script: row.script || row.command || '',
    days: row.days || 30,
    keep: row.keep || 7,
    format: row.format || 'tar.gz',
    spec: row.spec,
    remark: row.remark || '',
    target_type: row.target_type || 'local',
    target_name: row.target_name || '',
    remote_keep: row.remote_keep || row.keep || 7
  }
  schedule.value = parseSpecToSchedule(row.spec)
  mysqlAvailable.value = true
  dbLoaded.value = false
  databases.value = []
  dialogVisible.value = true
  loadingData.value = true
  const tasks = [loadTemplates(), loadSites()]
  // 编辑的是备份数据库类任务时才加载数据库列表
  if (row.template === 'backup_db' || row.template === 'backup_db_incremental') {
    tasks.push(loadDatabases())
  }
  // 加载远程存储列表（备份类型任务可能需要选目标存储）
  tasks.push(loadStorages())
  Promise.all(tasks).then(() => {
    loadingData.value = false
    rebuildGeneratedCommand()
  })
}

function onTemplateChange(key) {
  const t = templates.value.find((x) => x.key === key)
  if (t && t.suggest_spec) {
    form.value.spec = t.suggest_spec
    schedule.value = parseSpecToSchedule(t.suggest_spec)
  }
  // 自动生成任务名
  const autoLabels = {
    backup_site: '备份网站', backup_db: '备份数据库',
    backup_db_incremental: '数据库增量备份', backup_dir: '备份目录',
    cut_log: '日志切割', clear_log: '清理日志', free_mem: '释放内存',
    sync_time: '同步时间', scan_trojan: '木马查杀',
    http_get: 'GET 请求', http_post: 'POST 请求',
    nginx_reload: 'Nginx 重新载入', nginx_restart: 'Nginx 重启',
    apache_reload: 'Apache 重新载入', apache_restart: 'Apache 重启',
    shell: 'Shell 任务', python: 'Python 任务'
  }
  if (!form.value.name && autoLabels[key]) {
    form.value.name = autoLabels[key]
  }
  // 备份数据库类模板：按需加载数据库列表
  if ((key === 'backup_db' || key === 'backup_db_incremental') && !dbLoaded.value) {
    loadDatabases()
  }
  // 命令预览由 watch 统一触发，此处不再 nextTick 重复调用（避免和 watch 叠加产生重复 toast）
}
// 多选站点：name 是数组（含 "all" 或具体站点名），同步更新 site_root（一一对应）。
// 选择 "all" 时清空 site_root（让后端按"全部"逻辑处理）。
function onSiteChange(names) {
  if (!Array.isArray(names)) {
    // 兼容旧单选逻辑
    const s = sites.value.find((x) => x.name === names)
    form.value.site_root = s ? s.root : ''
    rebuildGeneratedCommand()
    return
  }
  if (names.includes('all')) {
    form.value.site_root = ''
  } else {
    form.value.site_root = names
      .map((n) => sites.value.find((x) => x.name === n)?.root || '')
      .filter(Boolean)
  }
  rebuildGeneratedCommand()
}

function onTargetTypeChange() {
  // 切换储存位置时不需要特殊处理；远程存储名清空让用户重新选
  if (form.value.target_type !== 'remote') {
    form.value.target_name = ''
  }
  rebuildGeneratedCommand()
}

function rebuildSpec() {
  const s = schedule.value
  let spec = ''
  switch (s.kind) {
    case 'minute':
      spec = `*/${s.interval} * * * *`
      break
    case 'hour':
      spec = `${s.minute} * * * *`
      break
    case 'day':
      spec = `${s.minute} ${s.hour} * * *`
      break
    case 'week':
      spec = `${s.minute} ${s.hour} * * ${s.weekday}`
      break
    case 'month':
      spec = `${s.minute} ${s.hour} ${s.day} * *`
      break
  }
  form.value.spec = spec
}
function parseSpecToSchedule(spec) {
  const f = (spec || '').trim().split(/\s+/)
  if (f.length !== 5) return emptySchedule()
  // 分
  if (/^\*\/\d+$/.test(f[0]) && f[1] === '*' && f[2] === '*' && f[3] === '*' && f[4] === '*') {
    return { ...emptySchedule(), kind: 'minute', interval: parseInt(f[0].slice(2), 10) || 1 }
  }
  // 时
  if (/^\d+$/.test(f[0]) && f[1] === '*' && f[2] === '*' && f[3] === '*' && f[4] === '*') {
    return { ...emptySchedule(), kind: 'hour', minute: parseInt(f[0], 10) }
  }
  // 周
  if (/^\d+$/.test(f[0]) && /^\d+$/.test(f[1]) && f[2] === '*' && f[3] === '*' && /^\d+$/.test(f[4])) {
    return { ...emptySchedule(), kind: 'week', minute: parseInt(f[0], 10), hour: parseInt(f[1], 10), weekday: parseInt(f[4], 10) }
  }
  // 月
  if (/^\d+$/.test(f[0]) && /^\d+$/.test(f[1]) && /^\d+$/.test(f[2]) && f[3] === '*' && f[4] === '*') {
    return { ...emptySchedule(), kind: 'month', minute: parseInt(f[0], 10), hour: parseInt(f[1], 10), day: parseInt(f[2], 10) }
  }
  // 日
  if (/^\d+$/.test(f[0]) && /^\d+$/.test(f[1]) && f[2] === '*' && f[3] === '*' && f[4] === '*') {
    return { ...emptySchedule(), kind: 'day', minute: parseInt(f[0], 10), hour: parseInt(f[1], 10) }
  }
  return emptySchedule()
}
function pad(n) { return String(n).padStart(2, '0') }

function specToText(spec) {
  if (!spec) return ''
  const s = parseSpecToSchedule(spec)
  switch (s.kind) {
    case 'minute': return `每 ${s.interval} 分钟`
    case 'hour': return `每小时 ${s.minute} 分`
    case 'day': return `每天 ${pad(s.hour)}:${pad(s.minute)}`
    case 'week': return `${weekOptions[s.weekday]} ${pad(s.hour)}:${pad(s.minute)}`
    case 'month': return `每月 ${s.day} 号 ${pad(s.hour)}:${pad(s.minute)}`
  }
  return spec
}

async function rebuildGeneratedCommand() {
  if (!form.value.template) {
    generatedCommand.value = ''
    return
  }
  // shell / python 直接用 form.script
  if (form.value.template === 'shell' || form.value.template === 'python') {
    generatedCommand.value = form.value.script || ''
    return
  }
  // 必填字段未填齐时不要请求后端预览——避免一进入就弹「请选择有效站点/数据库名无效」等红框
  // 也避免和 watch + nextTick 的双重触发产生重复 toast
  const t = currentTemplate.value
  if (t) {
    const need = t.needs || []
    const missing =
      (need.includes('site') && !form.value.site_name) ||
      (need.includes('database') && !form.value.database) ||
      (need.includes('dir') && !form.value.dir) ||
      (need.includes('url') && !form.value.url) ||
      (need.includes('script') && !form.value.script)
    if (missing) {
      generatedCommand.value = '// 请先填写上方必填项'
      return
    }
  }
  // 其他模板调用预览接口
  try {
    const res = await request.post('/cron/preview', {
      template: form.value.template,
      site_name: arrToCsv(form.value.site_name),
      site_root: arrToCsv(form.value.site_root),
      database: arrToCsv(form.value.database),
      dir: form.value.dir,
      url: form.value.url,
      method: form.value.method,
      days: form.value.days,
      keep: form.value.keep,
      format: form.value.format,
      script: form.value.script
    })
    generatedCommand.value = (res.data && res.data.command) || ''
  } catch (e) {
    generatedCommand.value = '// ' + (e?.response?.data?.msg || e?.message || '生成失败，请检查配置')
  }
}

// 表单字段变化时联动更新命令预览
watch(
  () => [form.value.template, form.value.site_name, form.value.site_root,
         form.value.database, form.value.dir, form.value.url,
         form.value.days, form.value.keep, form.value.format, form.value.script,
         form.value.target_type, form.value.target_name, form.value.remote_keep],
  () => {
    if (dialogVisible.value) rebuildGeneratedCommand()
  }
)

// 把多选数组转换为逗号分隔的字符串（用于后端存储）；数组里的 "all" 表示备份全部。
function arrToCsv(arr) {
  if (!Array.isArray(arr)) return ''
  return arr.filter(Boolean).join(',')
}

async function save() {
  if (!form.value.template) return ElMessage.warning('请选择任务类型')
  if (!form.value.name) return ElMessage.warning('请填写任务名称')
  if (!form.value.spec) return ElMessage.warning('请设置执行周期')

  // 必需字段校验（数组为空数组也视为空）
  if (hasNeed('site') && (!Array.isArray(form.value.site_name) || form.value.site_name.length === 0)) {
    return ElMessage.warning('请选择站点')
  }
  if (hasNeed('database') && (!Array.isArray(form.value.database) || form.value.database.length === 0)) {
    return ElMessage.warning('请选择数据库')
  }
  if (hasNeed('dir') && !form.value.dir) return ElMessage.warning('请填写目录')
  if (hasNeed('url') && !form.value.url) return ElMessage.warning('请填写 URL')
  if (hasNeed('script') && !form.value.script) return ElMessage.warning('请填写脚本内容')

  // 备份类型任务：选了远程存储就必须填存储名
  const isBackup = ['backup_site', 'backup_db', 'backup_db_incremental'].includes(form.value.template)
  if (isBackup && form.value.target_type === 'remote' && !form.value.target_name) {
    return ElMessage.warning('请选择远程存储')
  }

  // 确定最终命令
  let finalCommand = ''
  if (form.value.template === 'shell') {
    finalCommand = form.value.script
  } else if (form.value.template === 'python') {
    finalCommand = `python3 -c '${(form.value.script || '').replace(/'/g, "'\\''")}'`
  } else {
    finalCommand = generatedCommand.value
    if (!finalCommand || finalCommand.startsWith('//')) {
      return ElMessage.error('命令生成失败，请检查配置是否完整')
    }
  }

  saving.value = true
  try {
    await request.post('/cron/save', {
      id: form.value.id,
      name: form.value.name,
      spec: form.value.spec,
      command: finalCommand,
      remark: form.value.remark,
      template: form.value.template,
      site_name: arrToCsv(form.value.site_name),
      site_root: arrToCsv(form.value.site_root),
      database: arrToCsv(form.value.database),
      dir: form.value.dir,
      url: form.value.url,
      days: form.value.days,
      keep: form.value.keep,
      format: form.value.format,
      target_type: isBackup ? (form.value.target_type || 'local') : 'local',
      target_name: isBackup && form.value.target_type === 'remote' ? form.value.target_name : '',
      remote_keep: isBackup && form.value.target_type === 'remote' ? (form.value.remote_keep || form.value.keep || 7) : 0
    })
    ElMessage.success('保存成功')
    dialogVisible.value = false
    load()
  } catch (e) {
    ElMessage.error(e?.response?.data?.msg || e?.message || '保存失败')
  } finally {
    saving.value = false
  }
}

function toggle(row) {
  request.post('/cron/toggle', { id: row.id, enable: row.status !== 'enabled' }).then(() => {
    ElMessage.success('操作成功')
    load()
  }).catch(() => {})
}
function run(row) {
  request.post('/cron/run', { id: row.id }).then(() => {
    ElMessage.success('已提交执行')
  }).catch(() => {})
}

// ---- 执行日志 ----
const logVisible = ref(false)
const logTaskId = ref(0)
const logTaskName = ref('')
const cronLog = ref('')

function showLog(row) {
  logTaskId.value = row.id
  logTaskName.value = row.name
  logVisible.value = true
  cronLog.value = ''
  loadLog()
}

async function loadLog() {
  const res = await request.get('/cron/log', { params: { id: logTaskId.value } })
  cronLog.value = res.data.log || '（暂无日志）'
}
function remove(row) {
  ElMessageBox.confirm(`确定删除计划任务「${row.name}」吗？`, '删除任务', {
    confirmButtonText: '删除', cancelButtonText: '取消', type: 'warning'
  }).then(async () => {
    await request.post('/cron/delete', { id: row.id })
    ElMessage.success('已删除')
    load()
  }).catch(() => {})
}

function fmtTime(t) {
  return fmtTimeStamp(t)
}

function fmtBytes(n) {
  if (!n || n < 0) return '0 B'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n, i = 0
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++ }
  return (v >= 100 ? v.toFixed(0) : v.toFixed(1)) + ' ' + u[i]
}

// 备份类型任务判断
const BACKUP_TEMPLATES = ['backup_site', 'backup_db', 'backup_db_incremental', 'backup_dir']
function isBackupTask(row) {
  return BACKUP_TEMPLATES.includes(row.template)
}
// 每行任务的备份份数（懒加载：列表渲染后异步拉取）
const backupCount = ref({})

// 备份文件列表弹窗状态
const backupVisible = ref(false)
const backupTaskId = ref(0)
const backupTaskName = ref('')
const backupFiles = ref([])
const backupDir = ref('')
const backupLoading = ref(false)

async function loadAllBackupCounts() {
  const map = {}
  await Promise.all(list.value.filter(isBackupTask).map(async (row) => {
    try {
      const res = await request.get('/cron/backup-files', { params: { id: row.id } })
      map[row.id] = (res.data?.files || []).length
    } catch (e) {
      map[row.id] = 0
    }
  }))
  backupCount.value = map
}

async function openBackupFiles(row) {
  backupTaskId.value = row.id
  backupTaskName.value = row.name
  backupVisible.value = true
  await loadBackupFiles()
}

async function loadBackupFiles() {
  if (!backupTaskId.value) return
  backupLoading.value = true
  try {
    const res = await request.get('/cron/backup-files', { params: { id: backupTaskId.value } })
    backupFiles.value = res.data?.files || []
    backupDir.value = res.data?.dir || ''
    backupCount.value[backupTaskId.value] = backupFiles.value.length
  } catch (e) {
    backupFiles.value = []
  } finally {
    backupLoading.value = false
  }
}

async function downloadBackup(row) {
  // 先申请一次性下载 token（不泄露面板 JWT），再跳转下载。
  try {
    const res = await request.post('/cron/backup-download-token', { id: backupTaskId.value, name: row.name })
    const dtoken = res.data?.token
    if (!dtoken) return ElMessage.error('获取下载链接失败')
    const url = `/api/cron/backup-download?id=${backupTaskId.value}&name=${encodeURIComponent(row.name)}&token=${encodeURIComponent(dtoken)}`
    window.open(url, '_blank')
  } catch (e) {
    ElMessage.error('获取下载链接失败')
  }
}

function deleteBackup(row) {
  ElMessageBox.confirm(`确定删除备份「${row.name}」吗？`, '删除备份', {
    confirmButtonText: '删除', cancelButtonText: '取消', type: 'warning'
  }).then(async () => {
    await request.post('/cron/backup-delete', { id: backupTaskId.value, name: row.name })
    ElMessage.success('已删除')
    loadBackupFiles()
  }).catch(() => {})
}

onMounted(load)
</script>

<style scoped>
.cron-page { /* 间距由 AdminLayout.lp-main 统一控制（16px），<600px 时为 0 */ }
.toolbar { display: flex; align-items: center; gap: 8px; margin-bottom: 16px; }
.spacer { flex: 1; }
.hint { color: #909399; font-size: 12px; }
.spec-text { margin-left: 6px; color: #909399; font-size: 12px; }
.log-toolbar { display: flex; align-items: center; gap: 8px; margin-bottom: 12px; }
.cron-log-box {
  background: #1e1e1e; color: #d4d4d4; padding: 12px;
  border-radius: 6px; font-size: 12px; line-height: 1.7;
  max-height: 60vh; overflow: auto; white-space: pre-wrap;
  word-break: break-all; margin: 0;
}
.muted { color: #c0c4cc; }
.ok { color: #67c23a; }
.bad { color: #f56c6c; }

.opt-row { display: flex; align-items: center; gap: 8px; }
.opt-icon { color: #409eff; font-size: 16px; }
.opt-label { font-weight: 500; }
.opt-desc { color: #909399; font-size: 12px; margin-left: auto; }

.schedule {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
}
.schedule-sep { color: #606266; }
.command-preview :deep(textarea) {
  background: #fafafa;
  font-family: 'Consolas', 'Monaco', monospace;
  font-size: 12px;
  color: #303133;
}
.form-hint { color: #909399; font-size: 12px; margin-top: 4px; }
</style>