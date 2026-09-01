<template>
  <el-dialog
    :model-value="modelValue"
    :title="`${envName || '运行'}环境管理`"
    width="1020px"
    top="4vh"
    :close-on-click-modal="false"
    class="env-manager-dialog"
    @update:model-value="onVisibleChange"
    @open="onOpen"
    @closed="onClosed"
  >
    <div class="env-manager-body">
      <!-- 左侧菜单 -->
      <div class="env-sidebar">
        <div v-if="props.versions.length > 1" class="env-version-switch">
          <div class="env-switch-row">
            <el-select v-model="activeVersion" size="small" class="env-switch-select" :popper-options="{ placement: 'bottom-start' }">
              <el-option v-for="v in props.versions" :key="v.key" :label="v.name" :value="v.name">
                <div class="opt-row">
                  <span class="opt-name">{{ v.name }}</span>
                  <!-- 状态标签：安装中 / 队列中（运行时状态来自 /apps/list） -->
                  <el-tag v-if="v.status === 'installing'" type="warning" size="small">安装中</el-tag>
                  <el-tag v-else-if="v.status === 'queued'" type="info" size="small">队列中</el-tag>
                  <el-tag v-else-if="v.status === 'uninstalling'" type="warning" size="small">卸载中</el-tag>
                  <el-tag v-else-if="v.status === 'failed'" type="danger" size="small">失败</el-tag>
                  <el-button v-if="v.installed && v.uninstallable !== false"
                             type="danger" link size="small"
                             :loading="busyKey === v.key"
                             @click.stop="confirmUninstall(v.key, v.name)">
                    卸载
                  </el-button>
                  <el-button v-else-if="v.status === 'installing' || v.status === 'queued' || v.status === 'uninstalling'"
                             type="info" link size="small" disabled>进行中</el-button>
                  <el-button v-else
                             type="primary" link size="small"
                             :loading="busyKey === v.key"
                             @click.stop="confirmInstall(v.key, v.name)">
                    安装
                  </el-button>
                </div>
              </el-option>
            </el-select>
          </div>
        </div>
        <div
          v-for="m in menuItems"
          :key="m.key"
          class="env-menu-item"
          :class="{ active: subMenu === m.key }"
          @click="switchMenu(m.key)"
        >
          <el-icon><component :is="m.icon" /></el-icon>
          <span>{{ m.label }}</span>
        </div>
      </div>
      <!-- 右侧内容 -->
      <div class="env-content" v-loading="loading">
        <!-- ============ 服务 ============ -->
        <template v-if="subMenu === 'service'">
          <!-- PHP：有独立 systemd 服务，提供启停 -->
          <template v-if="lang === 'php'">
            <div class="sub-head">
              <h3>服务</h3>
              <div>
                <el-tag :type="envInfo.status === 'running' ? 'success' : 'danger'">
                  {{ envInfo.status === 'running' ? '运行中' : '已停止' }}
                </el-tag>
              </div>
            </div>
            <div class="info-grid">
              <div class="info-item"><label>PHP 版本</label><span>{{ envInfo.full_version || envInfo.version || '-' }}</span></div>
              <div class="info-item"><label>服务名称</label><span>{{ envInfo.service || '-' }}</span></div>
              <div class="info-item"><label>状态</label><span>{{ envInfo.detail || '-' }}</span></div>
            </div>
            <div class="sub-actions">
              <el-button v-if="envInfo.status !== 'running'"
                         type="success" :loading="acting" @click="envService('start')">
                <el-icon><VideoPlay /></el-icon> 启动
              </el-button>
              <el-button v-if="envInfo.status === 'running'"
                         type="danger" :loading="acting" @click="envService('stop')">
                <el-icon><VideoPause /></el-icon> 停止
              </el-button>
              <el-button v-if="envInfo.status === 'running'"
                         type="primary" :loading="acting" @click="envService('restart')">
                <el-icon><RefreshRight /></el-icon> 重启
              </el-button>
            </div>
          </template>
          <!-- Python / Node / Go：无独立 systemd 服务，展示真实运行状态 -->
          <template v-else>
            <div class="sub-head">
              <h3>服务</h3>
              <div>
                <el-tag v-if="!envInfo.ready" type="info">未就绪</el-tag>
                <el-tag v-else-if="envInfo.status === 'running'" type="success">运行中</el-tag>
                <el-tag v-else type="info">无运行进程</el-tag>
              </div>
            </div>
            <div class="info-grid">
              <div class="info-item"><label>{{ lang === 'python' ? 'Python 版本' : lang === 'node' ? 'Node 版本' : 'Go 版本' }}</label><span>{{ envInfo.full_version || envInfo.version || '-' }}</span></div>
              <div class="info-item"><label>可执行文件</label><span>{{ envInfo.bin || '-' }}</span></div>
              <div class="info-item"><label>运行状态</label><span>{{ envInfo.status === 'running' ? '运行中' : envInfo.ready ? '就绪（无运行进程）' : '未就绪' }}</span></div>
            </div>
            <!-- 运行进程列表 -->
            <div v-if="envInfo.processes && envInfo.processes.length" class="proc-list">
              <div class="proc-list-title">相关运行进程（{{ envInfo.processes.length }}）</div>
              <el-table :data="envInfo.processes" size="small" max-height="260">
                <el-table-column prop="pid" label="PID" width="80" />
                <el-table-column prop="etime" label="运行时长" width="110" />
                <el-table-column prop="cmd" label="命令" show-overflow-tooltip />
              </el-table>
            </div>
            <el-empty v-else :description="envInfo.ready ? '当前无该语言的运行进程' : '该环境未就绪，请先安装'" :image-size="60" />
            <div class="sub-actions">
              <!-- 真实可用操作：重新检测状态 / 终止该语言所有运行进程（pkill） -->
              <el-button type="primary" :loading="acting" @click="envLangRefresh">
                <el-icon><RefreshRight /></el-icon> 重新检测
              </el-button>
              <el-button v-if="envInfo.processes && envInfo.processes.length"
                         type="danger" :loading="acting" @click="envLangKillAll">
                <el-icon><VideoPause /></el-icon> 终止所有进程（{{ envInfo.processes.length }}）
              </el-button>
            </div>
          </template>
        </template>

        <!-- ============ PHP 安装扩展 ============ -->
        <template v-else-if="subMenu === 'extensions'">
          <div class="sub-head">
            <h3>安装扩展</h3>
            <el-button size="small" @click="loadPhpExts">
              <el-icon><Refresh /></el-icon> 刷新
            </el-button>
          </div>
          <el-tabs v-model="extTab">
            <el-tab-pane label="已安装扩展" name="installed">
              <el-empty v-if="!extInstalled.length" description="暂无已安装扩展" :image-size="80" />
              <div v-else class="ext-grid">
                <div v-for="ext in extInstalled" :key="ext" class="ext-card installed">
                  <span class="ext-name">{{ ext }}</span>
                  <el-tag size="small" type="success">已安装</el-tag>
                  <el-button size="small" type="danger" link @click="extUninstall(ext)">
                    <el-icon><Delete /></el-icon> 卸载
                  </el-button>
                </div>
              </div>
            </el-tab-pane>
            <el-tab-pane label="可安装扩展" name="available">
              <el-empty v-if="!extAvailable.length" description="所有常用扩展均已安装" :image-size="80" />
              <div v-else class="ext-grid">
                <div v-for="ext in extAvailable" :key="ext" class="ext-card">
                  <span class="ext-name">{{ ext }}</span>
                  <el-tag size="small" type="info">未安装</el-tag>
                  <el-button size="small" type="primary" link :loading="extInstalling === ext" @click="extInstall(ext)">
                    <el-icon><Download /></el-icon> 安装
                  </el-button>
                </div>
              </div>
            </el-tab-pane>
          </el-tabs>
        </template>

        <!-- ============ PHP 配置修改 / 上传限制 / 超时限制 / 禁用函数 / Session ============ -->
        <template v-else-if="['config','upload','timeout','disable','session'].includes(subMenu)">
          <div class="sub-head">
            <h3>{{ subMeta.title }}</h3>
            <div>
              <el-button size="small" @click="loadPhpIni">
                <el-icon><Refresh /></el-icon> 刷新
              </el-button>
              <el-button size="small" type="primary" :loading="saving" @click="savePhpIni">
                <el-icon><Check /></el-icon> 保存
              </el-button>
            </div>
          </div>
          <p v-if="iniFile" class="file-path">配置文件：{{ iniFile }}</p>
          <el-form label-width="220px" label-position="left" class="ini-form">
            <el-form-item v-for="k in subMeta.keys" :key="k" :label="k">
              <el-input v-model="iniForm[k]" clearable />
            </el-form-item>

            <!-- 禁用函数 特殊渲染：输入框 + 添加按钮 + 标签列表 -->
            <template v-if="subMenu === 'disable'">
              <el-form-item label="disable_functions">
                <div class="disable-func-block">
                  <div class="add-row">
                    <el-input
                      v-model="newDisableFunc"
                      placeholder="输入要禁用的函数名，例如 pcntl_fork"
                      clearable
                      @keyup.enter="addDisableFunc"
                    />
                    <el-button type="primary" @click="addDisableFunc">
                      <el-icon><Plus /></el-icon> 添加
                    </el-button>
                  </div>
                  <div v-if="disableFuncList.length" class="tag-list">
                    <el-tag
                      v-for="fn in disableFuncList"
                      :key="fn"
                      closable
                      type="info"
                      effect="plain"
                      class="func-tag"
                      @close="removeDisableFunc(fn)"
                    >{{ fn }}</el-tag>
                  </div>
                  <div v-else class="empty-hint">暂无禁用函数</div>
                </div>
              </el-form-item>
            </template>
          </el-form>
        </template>

        <!-- ============ PHP FPM 配置 ============ -->
        <template v-else-if="subMenu === 'fpm'">
          <div class="sub-head">
            <h3>FPM 配置文件</h3>
            <div>
              <el-button size="small" @click="loadPhpFpm">
                <el-icon><Refresh /></el-icon> 刷新
              </el-button>
              <el-button size="small" type="primary" :loading="saving" @click="savePhpFpm">
                <el-icon><Check /></el-icon> 保存
              </el-button>
            </div>
          </div>
          <p v-if="fpmPath" class="file-path">配置文件：{{ fpmPath }}</p>
          <el-alert type="info" :closable="false" show-icon class="tip">
            <template #title>修改后自动重启 PHP-FPM 生效；pm 相关参数仅在 pm=dynamic 时生效</template>
          </el-alert>
          <el-form label-width="220px" label-position="left" class="ini-form">
            <el-form-item v-for="k in phpFpmKeys" :key="k" :label="k">
              <el-input v-model="fpmForm[k]" clearable />
            </el-form-item>
          </el-form>
        </template>

        <!-- ============ PHP 性能调整 ============ -->
        <template v-else-if="subMenu === 'performance'">
          <div class="sub-head">
            <h3>性能调整</h3>
            <div>
              <el-button size="small" type="primary" :loading="saving" @click="savePhpFpm">
                <el-icon><Check /></el-icon> 保存
              </el-button>
            </div>
          </div>
          <el-alert type="warning" :closable="false" show-icon class="tip">
            <template #title>根据服务器内存自动推荐：可用内存 {{ memoryTotal }}MB，推荐 max_children ≈ {{ recommendedChildren }}（每个 worker 约 50MB）</template>
          </el-alert>
          <el-form label-width="220px" label-position="left" class="ini-form">
            <el-form-item label="pm.max_children">
              <el-input-number v-model.number="perfForm.max_children" :min="1" :max="512" />
              <span class="hint-text">最大子进程数（推荐 {{ recommendedChildren }}）</span>
            </el-form-item>
            <el-form-item label="pm.start_servers">
              <el-input-number v-model.number="perfForm.start_servers" :min="1" :max="128" />
              <span class="hint-text">启动时进程数</span>
            </el-form-item>
            <el-form-item label="pm.min_spare_servers">
              <el-input-number v-model.number="perfForm.min_spare" :min="1" :max="128" />
              <span class="hint-text">最小空闲进程数</span>
            </el-form-item>
            <el-form-item label="pm.max_spare_servers">
              <el-input-number v-model.number="perfForm.max_spare" :min="1" :max="128" />
              <span class="hint-text">最大空闲进程数</span>
            </el-form-item>
          </el-form>
          <div class="sub-actions">
            <el-button type="primary" :loading="saving" @click="applyPerformance">
              <el-icon><MagicStick /></el-icon> 应用推荐配置
            </el-button>
          </div>
        </template>

        <!-- ============ PHP 负载状态 ============ -->
        <template v-else-if="subMenu === 'status'">
          <div class="sub-head">
            <h3>负载状态</h3>
            <el-button size="small" @click="loadPhpStatus">
              <el-icon><Refresh /></el-icon> 刷新
            </el-button>
          </div>
          <el-alert v-if="phpStatus.status_page === false" type="warning" :closable="false" show-icon class="tip">
            <template #title>{{ phpStatus.hint }}</template>
          </el-alert>
          <el-descriptions v-if="phpStatus.status_page" :column="2" border class="status-desc">
            <el-descriptions-item v-for="(v, k) in phpStatus" :key="k" :label="fmtKey(k)">{{ v }}</el-descriptions-item>
          </el-descriptions>
          <div v-else class="info-grid">
            <div class="info-item"><label>服务状态</label><span>{{ phpStatus.service_detail || '-' }}</span></div>
            <div class="info-item"><label>Master PID</label><span>{{ phpStatus.master_pid || '-' }}</span></div>
            <div class="info-item"><label>Worker 进程数</label><span>{{ phpStatus.worker_count || '-' }}</span></div>
            <div class="info-item"><label>Worker 内存 (MB)</label><span>{{ phpStatus.memory_mb || '-' }}</span></div>
            <div class="info-item"><label>max_children</label><span>{{ phpStatus.max_children || '-' }}</span></div>
          </div>
        </template>

        <!-- ============ PHP 日志 / 慢日志 ============ -->
        <template v-else-if="subMenu === 'logs' || subMenu === 'slow'">
          <div class="sub-head">
            <h3>{{ subMenu === 'logs' ? '日志' : '慢日志' }}</h3>
            <div>
              <el-button size="small" @click="loadPhpLog">
                <el-icon><Refresh /></el-icon> 刷新
              </el-button>
            </div>
          </div>
          <p class="file-path">文件：{{ phpLog.path || '-' }}</p>
          <el-alert v-if="phpLog.hint" type="info" :closable="false" show-icon class="tip">
            <template #title>{{ phpLog.hint }}</template>
          </el-alert>
          <pre class="log-view">{{ phpLog.log || '（空）' }}</pre>
        </template>

        <!-- ============ PHP phpinfo ============ -->
        <template v-else-if="subMenu === 'phpinfo'">
          <div class="sub-head">
            <h3>phpinfo</h3>
            <el-button size="small" @click="loadPhpInfoPage">
              <el-icon><Refresh /></el-icon> 刷新
            </el-button>
          </div>
          <div class="info-grid" v-if="phpInfo.info">
            <div class="info-item" v-for="(v, k) in phpInfo.info" :key="k"><label>{{ k }}</label><span>{{ v }}</span></div>
          </div>
          <el-descriptions v-if="phpInfo.ini" :column="3" border class="status-desc">
            <el-descriptions-item v-for="(v, k) in phpInfo.ini" :key="k" :label="k">{{ v }}</el-descriptions-item>
          </el-descriptions>
          <div v-if="phpInfo.modules && phpInfo.modules.length" class="mods-block">
            <h4>已加载模块（{{ phpInfo.modules.length }}）</h4>
            <div class="ext-grid">
              <span v-for="m in phpInfo.modules" :key="m" class="mod-tag">{{ m }}</span>
            </div>
          </div>
        </template>

        <!-- ============ Python / Node / Go 版本信息 ============ -->
        <template v-else-if="subMenu === 'info'">
          <div class="sub-head">
            <h3>版本信息</h3>
            <el-button size="small" @click="loadEnvInfo">
              <el-icon><Refresh /></el-icon> 刷新
            </el-button>
          </div>
          <div class="info-grid">
            <div class="info-item" v-for="(v, k) in envInfo" :key="k" v-show="k !== 'status' && k !== 'detail' && v">
              <label>{{ fmtKey(k) }}</label><span>{{ v }}</span>
            </div>
          </div>
        </template>

        <!-- ============ Python / Node 已装包 ============ -->
        <template v-else-if="subMenu === 'packages'">
          <div class="sub-head">
            <h3>已安装包 / 模块</h3>
            <div>
              <el-button size="small" @click="loadPackages">
                <el-icon><Refresh /></el-icon> 刷新
              </el-button>
              <el-input v-model="pkgFilter" size="small" placeholder="搜索包名" clearable style="width: 180px; margin-left: 8px;">
                <template #prefix><el-icon><Search /></el-icon></template>
              </el-input>
            </div>
          </div>
          <el-alert v-if="packages.note" type="info" :closable="false" show-icon class="tip">
            <template #title>{{ packages.note }}</template>
          </el-alert>
          <el-table :data="filteredPkgs" size="small" max-height="460" stripe>
            <el-table-column prop="name" label="名称" min-width="220" />
            <el-table-column prop="version" label="版本" min-width="120" />
          </el-table>
          <div class="pkg-count">共 {{ filteredPkgs.length }} 个包</div>
        </template>

        <!-- ============ Python / Node / Go 全局配置 ============ -->
        <template v-else-if="subMenu === 'global'">
          <div class="sub-head">
            <h3>全局配置</h3>
            <div>
              <el-button size="small" @click="loadGlobalConfig">
                <el-icon><Refresh /></el-icon> 刷新
              </el-button>
              <el-button size="small" type="primary" :loading="saving" @click="saveGlobalConfig">
                <el-icon><Check /></el-icon> 保存
              </el-button>
            </div>
          </div>
          <el-form label-width="200px" label-position="left" class="ini-form">
            <el-form-item v-for="(v, k) in globalConfig" :key="k" :label="fmtKey(k)">
              <el-input v-model="globalConfig[k]" clearable />
            </el-form-item>
          </el-form>
        </template>
      </div>
    </div>
    <!-- 底部按钮已删除：右上角 ×、点遮罩、按 Esc 都能关闭；原"关闭"和"完成"两个按钮功能完全一样（都是 onVisibleChange(false)） -->
  </el-dialog>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Tools, Setting, Download, Refresh, RefreshRight, Check, Delete,
  VideoPlay, VideoPause, MagicStick, Search, QuestionFilled, Cpu, Monitor, Document, InfoFilled, Box, Connection,
  Upload, Clock, NoSmoking, Plus
} from '@element-plus/icons-vue'
import request from '../utils/request'
import { useInstallTrackerStore } from '../stores/installTracker'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  envName: { type: String, default: '' },
  versions: { type: Array, default: () => [] }
})
const emit = defineEmits(['update:modelValue', 'refresh'])

// ==================== 基础 ====================
// 当前管理的版本（默认选中第一个已安装版本）
const activeVersion = ref('')
const busyKey = ref('')
// 当前选中的版本对象（用于在 el-select 旁显示一键安装/卸载按钮）
const currentVer = computed(() => props.versions.find((v) => v.name === activeVersion.value))

// 一键安装：弹确认框 → 调 /apps/install { key }
async function confirmInstall(key, name) {
  // Python 多版本走源码编译（pyenv），耗时显著长于其他运行时，明确告知预期时长
  const pythonTip = key.startsWith('python')
    ? `确定要安装 ${name} 吗？\nPython 为源码编译安装，预计需要 10~20 分钟，期间请勿关闭或刷新页面。`
    : `确定要安装 ${name} 吗？安装过程可能持续数分钟，期间请勿关闭或刷新页面。`
  try {
    await ElMessageBox.confirm(pythonTip, '安装环境', {
      type: 'warning', confirmButtonText: '一键安装', cancelButtonText: '取消'
    })
  } catch {
    return
  }
  busyKey.value = key
  try {
    await request.post('/apps/install', { key })
    // 写入全局任务队列，右下角浮窗立即显示（与 AppStore 一致）
    const tracker = useInstallTrackerStore()
    tracker.unmarkRemoved(key) // 用户主动触发：先清除"被移除"标记
    tracker.upsert({ key, name, action: 'install', status: 'queued', message: '排队等待中...' })
    ElMessage.success(`${name} 安装任务已启动，请稍候查看「应用商店」日志`)
    emit('refresh')
  } catch (e) {
    ElMessage.error('安装失败：' + (e?.response?.data?.message || e?.message || '未知错误'))
  } finally {
    busyKey.value = ''
  }
}

// 卸载：弹确认框 → 调 /apps/uninstall { key }
async function confirmUninstall(key, name) {
  try {
    await ElMessageBox.confirm(
      `确定要卸载 ${name} 吗？\n该操作将删除该版本及其运行文件，依赖该版本的网站将无法运行！`,
      '卸载环境',
      {
        type: 'warning',
        confirmButtonText: '确定卸载',
        cancelButtonText: '取消',
        confirmButtonClass: 'el-button--danger'
      }
    )
  } catch {
    return
  }
  busyKey.value = key
  try {
    await request.post('/apps/uninstall', { key })
    // 写入全局任务队列，右下角浮窗立即显示（与 AppStore 一致）
    const tracker = useInstallTrackerStore()
    tracker.unmarkRemoved(key) // 用户主动触发：先清除"被移除"标记
    tracker.upsert({ key, name, action: 'uninstall', status: 'uninstalling', message: '正在卸载...' })
    ElMessage.success(`${name} 卸载任务已启动，请稍候查看「应用商店」日志`)
    emit('refresh')
  } catch (e) {
    ElMessage.error('卸载失败：' + (e?.response?.data?.message || e?.message || '未知错误'))
  } finally {
    busyKey.value = ''
  }
}

function langOf(name) {
  const n = (name || '').toLowerCase()
  if (n.startsWith('php')) return 'php'
  if (n.startsWith('python')) return 'python'
  if (n.startsWith('node')) return 'node'
  if (n.startsWith('go')) return 'go'
  return 'php'
}
const lang = computed(() => langOf(activeVersion.value || props.envName))

function parseVer(name) {
  const m = (name || '').match(/(\d+\.?\d*\.?\d*)/)
  return m ? m[1] : ''
}

const version = computed(() => parseVer(activeVersion.value || props.envName))

// 切换环境管理（envName / versions 变化）时：
//   - envName 变化 → 父组件切换了管理对象，强制把 activeVersion 同步到 envName
//     （修复：之前仅判断 activeVersion 是否在 versions 里，若 versions 包含多种语言
//      的所有版本，PHP→Python 切换时 activeVersion 仍在列表里，导致下拉框和按钮错乱）
//   - versions 变化 → 仅当当前选中不在新列表中时重置（安装/卸载后回填）
watch(() => props.envName, (newName) => {
  if (newName && newName !== activeVersion.value) {
    activeVersion.value = newName
  }
})
watch(() => props.versions, (list) => {
  const arr = list || []
  if (!arr.some((v) => v.name === activeVersion.value)) {
    const installed = arr.find((v) => v.installed)
    activeVersion.value = (installed || arr[0] || {}).name || props.envName || ''
  }
}, { immediate: true })
watch(activeVersion, () => {
  subMenu.value = 'service'
  switchMenu(subMenu.value)
})

const menuItems = computed(() => {
  const php = [
    { key: 'service', label: '服务', icon: VideoPlay },
    { key: 'extensions', label: '安装扩展', icon: Box },
    { key: 'config', label: '配置修改', icon: Tools },
    { key: 'upload', label: '上传限制', icon: Upload },
    { key: 'timeout', label: '超时限制', icon: Clock },
    { key: 'disable', label: '禁用函数', icon: NoSmoking },
    { key: 'fpm', label: 'FPM 配置', icon: Setting },
    { key: 'performance', label: '性能调整', icon: MagicStick },
    { key: 'status', label: '负载状态', icon: Monitor },
    { key: 'session', label: 'Session 配置', icon: Connection },
    { key: 'logs', label: '日志', icon: Document },
    { key: 'slow', label: '慢日志', icon: Document },
    { key: 'phpinfo', label: 'phpinfo', icon: InfoFilled }
  ]
  const common = [
    { key: 'info', label: '版本信息', icon: Cpu },
    // 用 'global' 区分 PHP 的 'config'（PHP 的 '配置修改' subMenu 走的 loadPhpIni），
    // 避免 Python/Go/Node 点「全局配置」时误调 PHP ini 接口
    { key: 'global', label: '全局配置', icon: Tools }
  ]
  if (lang.value === 'php') return php
  // 非 PHP 环境也有「服务」页（展示真实运行状态），放在第一位与 PHP 对齐
  const svc = { key: 'service', label: '服务', icon: VideoPlay }
  const pkgs = { key: 'packages', label: '已装包 / 模块', icon: Box }
  if (lang.value === 'go') return [svc, ...common]
  return [svc, ...common.slice(0, 1), pkgs, ...common.slice(1)]
})

const subMenu = ref('service')
const loading = ref(false)
const saving = ref(false)
const acting = ref(false)

function switchMenu(key) {
  subMenu.value = key
  const handlers = {
    service: loadEnvInfo,
    extensions: loadPhpExts,
    config: loadPhpIni,
    upload: loadPhpIni,
    timeout: loadPhpIni,
    disable: loadPhpIni,
    session: loadPhpIni,
    fpm: loadPhpFpm,
    performance: loadPhpFpm,
    status: loadPhpStatus,
    logs: loadPhpLog,
    slow: loadPhpLog,
    phpinfo: loadPhpInfoPage,
    info: loadEnvInfo,
    packages: loadPackages,
    // Python/Node/Go 的「全局配置」（与 PHP 的 'config' 区分，PHP 走 loadPhpIni）
    global: loadGlobalConfig
  }
  const fn = handlers[key] || loadGlobalConfig
  fn()
}

function onVisibleChange(v) {
  emit('update:modelValue', v)
}
function onOpen() {
  switchMenu(subMenu.value)
}
function onClosed() {
  emit('refresh')
}

// ==================== 菜单 meta ====================
const subMeta = computed(() => {
  const base = {
    config: { title: '配置修改', keys: ['short_open_tag', 'memory_limit', 'error_reporting', 'display_errors', 'expose_php', 'date.timezone', 'realpath_cache_size'] },
    upload: { title: '上传限制', keys: ['upload_max_filesize', 'post_max_size', 'max_file_uploads'] },
    timeout: { title: '超时限制', keys: ['max_execution_time', 'max_input_time', 'default_socket_timeout'] },
    disable: { title: '禁用函数', keys: [] },
    session: { title: 'Session 配置', keys: ['session.save_handler', 'session.save_path', 'session.cookie_lifetime', 'session.gc_maxlifetime', 'session.use_cookies'] }
  }
  return base[subMenu.value] || base.config
})

// ==================== 服务 ====================
const envInfo = ref({})
async function loadEnvInfo() {
  if (!props.envName) return
  loading.value = true
  try {
    const res = await request.get('/env/info', { params: { name: props.envName } })
    envInfo.value = res.data || {}
  } catch (e) { /* interceptor */ } finally { loading.value = false }
}

async function envService(action) {
  acting.value = true
  try {
    await request.post('/env/service', { name: props.envName, action })
    ElMessage.success(action === 'start' ? '已启动' : action === 'stop' ? '已停止' : '已重启')
    await loadEnvInfo()
  } catch (e) { /* interceptor */ } finally { acting.value = false }
}

// Python/Node/Go 服务页：重新检测（重新拉 envInfo 显示最新进程）
async function envLangRefresh() {
  acting.value = true
  try { await loadEnvInfo() }
  finally { acting.value = false }
}

// Python/Node/Go 服务页：终止所有该语言运行进程（pkill）
async function envLangKillAll() {
  const n = envInfo.value.processes ? envInfo.value.processes.length : 0
  try {
    await ElMessageBox.confirm(
      `确定要终止所有 ${lang.value} 运行进程吗？共 ${n} 个进程。这不会影响其他语言。`,
      '终止进程', { type: 'warning', confirmButtonText: '确定终止', cancelButtonText: '取消', confirmButtonClass: 'el-button--danger' }
    )
  } catch { return }
  acting.value = true
  try {
    await request.post('/env/service', { name: props.envName, action: 'stop' })
    ElMessage.success('已发送终止信号')
    await loadEnvInfo()
  } catch (e) { /* interceptor */ } finally { acting.value = false }
}

// ==================== PHP 扩展 ====================
const extTab = ref('installed')
const extInstalled = ref([])
const extAvailable = ref([])
const extInstalling = ref('')
async function loadPhpExts() {
  loading.value = true
  try {
    const res = await request.get('/env/php/extensions', { params: { version: version.value } })
    extInstalled.value = (res.data && res.data.installed) || []
    extAvailable.value = (res.data && res.data.available) || []
  } catch (e) { /* interceptor */ } finally { loading.value = false }
}

async function extInstall(ext) {
  extInstalling.value = ext
  try {
    await request.post('/env/php/extensions/install', { version: version.value, ext })
    ElMessage.success(`扩展 ${ext} 安装成功，PHP-FPM 已重启`)
    await loadPhpExts()
  } catch (e) { /* interceptor */ } finally { extInstalling.value = '' }
}

async function extUninstall(ext) {
  try {
    await ElMessageBox.confirm(`确定卸载扩展 ${ext} 吗？卸载后依赖该扩展的站点功能将不可用。`, '确认卸载', { type: 'warning' })
  } catch { return }
  try {
    await request.post('/env/php/extensions/uninstall', { version: version.value, ext })
    ElMessage.success(`扩展 ${ext} 已卸载`)
    await loadPhpExts()
  } catch (e) { /* interceptor */ }
}

// ==================== PHP ini ====================
const iniFile = ref('')
const iniForm = ref({})
// 禁用函数专用：标签列表 + 输入框
const disableFuncList = ref([])
const newDisableFunc = ref('')
async function loadPhpIni() {
  loading.value = true
  try {
    const res = await request.get('/env/php/ini', { params: { version: version.value } })
    const cfg = (res.data && res.data.config) || {}
    iniFile.value = cfg._file || ''
    const form = {}
    for (const k of subMeta.value.keys) {
      if (cfg[k] !== undefined) form[k] = cfg[k]
      else form[k] = ''
    }
    iniForm.value = form
    // 禁用函数：把逗号分隔字符串拆为数组
    const v = cfg.disable_functions || ''
    disableFuncList.value = v
      ? v.split(',').map(s => s.trim()).filter(Boolean)
      : []
    newDisableFunc.value = ''
  } catch (e) { /* interceptor */ } finally { loading.value = false }
}

async function savePhpIni() {
  saving.value = true
  try {
    const updates = {}
    for (const k of subMeta.value.keys) {
      if (iniForm.value[k] !== '') updates[k] = iniForm.value[k]
    }
    // 禁用函数：把数组拼成逗号分隔字符串
    if (subMenu.value === 'disable') {
      updates.disable_functions = disableFuncList.value.join(',')
    }
    await request.post('/env/php/ini', { version: version.value, updates })
    ElMessage.success('配置已保存，PHP-FPM 已重启')
  } catch (e) { /* interceptor */ } finally { saving.value = false }
}

function addDisableFunc() {
  const v = newDisableFunc.value.trim()
  if (!v) { ElMessage.warning('请输入函数名'); return }
  if (disableFuncList.value.includes(v)) { ElMessage.warning('该函数已在禁用列表中'); return }
  disableFuncList.value.push(v)
  newDisableFunc.value = ''
}

function removeDisableFunc(fn) {
  disableFuncList.value = disableFuncList.value.filter(x => x !== fn)
}

// ==================== PHP FPM 配置 ====================
const phpFpmKeys = ['listen', 'listen.allowed_clients', 'user', 'group', 'pm', 'pm.max_children', 'pm.start_servers', 'pm.min_spare_servers', 'pm.max_spare_servers', 'pm.max_requests', 'pm.status_path', 'request_terminate_timeout', 'slowlog', 'catch_workers_output']
const fpmPath = ref('')
const fpmForm = ref({})
const perfForm = ref({ max_children: 10, start_servers: 2, min_spare: 2, max_spare: 5 })
const memoryTotal = ref(0)

async function loadPhpFpm() {
  loading.value = true
  try {
    const res = await request.get('/env/php/fpm-config', { params: { version: version.value } })
    const cfg = (res.data && res.data.config) || {}
    fpmPath.value = (res.data && res.data.path) || ''
    const form = {}
    for (const k of phpFpmKeys) {
      if (cfg[k] !== undefined) form[k] = cfg[k]
      else form[k] = ''
    }
    fpmForm.value = form
    // 性能表单
    perfForm.value = {
      max_children: parseInt(cfg['pm.max_children']) || 10,
      start_servers: parseInt(cfg['pm.start_servers']) || 2,
      min_spare: parseInt(cfg['pm.min_spare_servers']) || 2,
      max_spare: parseInt(cfg['pm.max_spare_servers']) || 5
    }
    // 读取系统内存（本地估算）
    try {
      const sys = await request.get('/system/info')
      if (sys.data && sys.data.memory_total) memoryTotal.value = sys.data.memory_total
    } catch (e) { /* ignore */ }
  } catch (e) { /* interceptor */ } finally { loading.value = false }
}

const recommendedChildren = computed(() => {
  if (!memoryTotal.value) return 20
  return Math.max(2, Math.floor(memoryTotal.value / 50))
})

async function savePhpFpm() {
  saving.value = true
  try {
    const updates = {}
    for (const k of phpFpmKeys) {
      if (fpmForm.value[k] !== '') updates[k] = fpmForm.value[k]
    }
    await request.post('/env/php/fpm-config', { version: version.value, updates })
    ElMessage.success('FPM 配置已保存，PHP-FPM 已重启')
  } catch (e) { /* interceptor */ } finally { saving.value = false }
}

async function applyPerformance() {
  saving.value = true
  try {
    const updates = {
      'pm.max_children': String(perfForm.value.max_children),
      'pm.start_servers': String(perfForm.value.start_servers),
      'pm.min_spare_servers': String(perfForm.value.min_spare),
      'pm.max_spare_servers': String(perfForm.value.max_spare)
    }
    await request.post('/env/php/fpm-config', { version: version.value, updates })
    ElMessage.success('性能配置已应用，PHP-FPM 已重启')
  } catch (e) { /* interceptor */ } finally { saving.value = false }
}

// ==================== PHP 负载状态 ====================
const phpStatus = ref({})
async function loadPhpStatus() {
  loading.value = true
  try {
    const res = await request.get('/env/php/status', { params: { version: version.value } })
    phpStatus.value = res.data || {}
  } catch (e) { /* interceptor */ } finally { loading.value = false }
}

// ==================== PHP 日志 ====================
const phpLog = ref({})
async function loadPhpLog() {
  loading.value = true
  try {
    const type = subMenu.value === 'slow' ? 'slow' : 'fpm'
    const res = await request.get('/env/php/log', { params: { version: version.value, type, lines: 300 } })
    phpLog.value = res.data || {}
  } catch (e) { /* interceptor */ } finally { loading.value = false }
}

// ==================== PHP phpinfo ====================
const phpInfo = ref({})
async function loadPhpInfoPage() {
  loading.value = true
  try {
    const res = await request.get('/env/php/phpinfo', { params: { version: version.value } })
    phpInfo.value = res.data || {}
  } catch (e) { /* interceptor */ } finally { loading.value = false }
}

// ==================== Python / Node 包列表 ====================
const packages = ref({ packages: [] })
const pkgFilter = ref('')
const filteredPkgs = computed(() => {
  const list = packages.value.packages || []
  if (!pkgFilter.value) return list
  return list.filter(p => (p.name || '').toLowerCase().includes(pkgFilter.value.toLowerCase()))
})
async function loadPackages() {
  loading.value = true
  try {
    const res = await request.get('/env/packages', { params: { name: props.envName } })
    packages.value = res.data || { packages: [] }
  } catch (e) { /* interceptor */ } finally { loading.value = false }
}

// ==================== 全局配置 ====================
const globalConfig = ref({})
async function loadGlobalConfig() {
  loading.value = true
  // 先清空再加载，避免切换 envName 时短暂显示上一个 runtime 的 keys（key 名称可能错位）
  globalConfig.value = {}
  try {
    const res = await request.get('/env/global-config', { params: { name: props.envName } })
    globalConfig.value = (res.data && res.data.config) || {}
  } catch (e) { /* interceptor */ } finally { loading.value = false }
}

async function saveGlobalConfig() {
  saving.value = true
  try {
    await request.post('/env/global-config', { name: props.envName, updates: globalConfig.value })
    ElMessage.success('全局配置已保存')
  } catch (e) { /* interceptor */ } finally { saving.value = false }
}

// ==================== 工具 ====================
function fmtKey(k) {
  return String(k).replace(/_/g, ' ')
}
</script>

<style scoped>
.env-manager-body { display: flex; gap: 16px; min-height: 460px; max-height: calc(100vh - 200px); }
.env-sidebar { width: 180px; flex-shrink: 0; background: #f7f8fa; border-radius: 8px; padding: 8px 0; max-height: 100%; overflow-y: auto; }
.env-version-switch { padding: 4px 12px 10px; border-bottom: 1px solid #e4e7ed; margin-bottom: 6px; }
.ver-tag { margin-left: 6px; transform: scale(.85); }
.env-switch-row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
/* 版本下拉：宽度受左侧菜单框约束（max-width: 100%），下拉浮层宽度跟随 trigger，
   避免 option 里的「卸载/安装」按钮把下拉撑出 sidebar */
.env-switch-select { min-width: 0; flex: 1 1 120px; max-width: 100%; }
.env-switch-actions { display: flex; gap: 8px; }
.opt-row { display: flex; align-items: center; justify-content: space-between; gap: 6px; width: 100%; }
.opt-name { font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
/* 限制下拉浮层最大宽度 = sidebar 宽度，避免 option 的按钮把弹层撑出左侧 */
::deep(.env-manager-dialog .env-version-switch .el-select__popper) { max-width: 180px; }
.env-menu-item { display: flex; align-items: center; gap: 8px; padding: 10px 16px; cursor: pointer; color: #606266; font-size: 13px; border-left: 3px solid transparent; transition: all .15s; }
.env-menu-item:hover { background: #eef1f6; color: #409eff; }
.env-menu-item.active { background: #ecf5ff; color: #409eff; border-left-color: #409eff; font-weight: 600; }
.env-content { flex: 1; min-width: 0; min-height: 0; padding: 4px 8px; max-height: 100%; overflow-y: auto; }
.sub-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 14px; }
.sub-head h3 { margin: 0; font-size: 16px; color: #303133; }
.sub-actions { margin-top: 20px; display: flex; gap: 10px; }
.proc-list { margin-top: 16px; }
.proc-list-title { font-size: 13px; font-weight: 600; color: #303133; margin-bottom: 8px; }
.info-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 12px; }
.info-item { background: #f7f8fa; border-radius: 8px; padding: 12px 16px; display: flex; flex-direction: column; gap: 6px; }
.info-item label { font-size: 12px; color: #909399; }
.info-item span { font-size: 13px; color: #303133; word-break: break-all; }
.file-path { font-size: 12px; color: #909399; margin: 0 0 10px; word-break: break-all; }
.ini-form { max-height: 460px; overflow-y: auto; padding-right: 4px; }
.env-manager-dialog :deep(.el-dialog__body) { padding: 16px 20px; }
.tip { margin-bottom: 14px; }
.hint-text { margin-left: 12px; font-size: 12px; color: #909399; }

/* 禁用函数标签输入区 */
.disable-func-block { width: 100%; }
.disable-func-block .add-row { display: flex; gap: 8px; margin-bottom: 10px; }
.disable-func-block .add-row .el-input { flex: 1 1 auto; min-width: 0; }
.disable-func-block .tag-list { display: flex; flex-wrap: wrap; gap: 6px; padding: 8px; background: #f5f7fa; border-radius: 4px; min-height: 36px; }
.disable-func-block .func-tag { margin: 0; }
.disable-func-block .empty-hint { padding: 14px 8px; color: #c0c4cc; font-size: 12px; text-align: center; background: #f5f7fa; border-radius: 4px; }
.ext-grid { display: flex; flex-wrap: wrap; gap: 10px; max-height: 460px; overflow-y: auto; padding: 4px; }
.ext-card { display: flex; align-items: center; gap: 8px; padding: 8px 14px; background: #f7f8fa; border: 1px solid #e4e7ed; border-radius: 8px; font-size: 13px; }
.ext-card.installed { background: #f0f9eb; border-color: #e1f3d8; }
.ext-name { font-weight: 600; color: #303133; min-width: 70px; }
.mod-tag { padding: 4px 12px; background: #ecf5ff; color: #409eff; border-radius: 6px; font-size: 12px; border: 1px solid #d9ecff; }
.mods-block h4 { margin: 16px 0 10px; font-size: 14px; color: #303133; }
.status-desc { margin-top: 10px; }
.log-view { background: #0f1419; color: #c9d1d9; padding: 14px; border-radius: 8px; font-family: 'JetBrains Mono', Consolas, monospace; font-size: 12px; line-height: 1.6; max-height: 440px; overflow: auto; white-space: pre-wrap; word-break: break-all; }
.pkg-count { margin-top: 10px; font-size: 12px; color: #909399; }
</style>
