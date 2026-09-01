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
        <el-steps :active="expStep === 4 ? 4 : expStep - 1" finish-status="success" align-center class="mig-steps">
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
          <div v-if="hasLocalList" class="imp-three-col">
            <div class="imp-col">
              <div class="imp-col-header">
                <el-checkbox v-model="expSiteAllChecked" :indeterminate="expSiteIndeterminate" @change="onExpSiteAllToggle">
                  网站 ({{ localSites.length }})
                </el-checkbox>
              </div>
              <div class="imp-col-body">
                <el-checkbox-group v-model="expSites">
                  <el-checkbox
                    v-for="s in localSites"
                    :key="s.id || s.name"
                    :value="s.name"
                    class="imp-item"
                  >
                    <span class="imp-item-name">{{ s.name }}</span>
                    <span class="imp-item-meta">{{ s.type || '-' }}<span v-if="s.php_version"> · PHP {{ s.php_version }}</span></span>
                  </el-checkbox>
                </el-checkbox-group>
                <div v-if="localSites.length === 0" class="imp-empty">本机暂无网站</div>
              </div>
            </div>

            <div class="imp-col">
              <div class="imp-col-header">
                <el-checkbox v-model="expDbAllChecked" :indeterminate="expDbIndeterminate" @change="onExpDbAllToggle">
                  数据库 ({{ localDbs.length }})
                </el-checkbox>
              </div>
              <div class="imp-col-body">
                <el-checkbox-group v-model="expDbs">
                  <el-checkbox
                    v-for="d in localDbs"
                    :key="d.name"
                    :value="d.name"
                    class="imp-item"
                  >
                    <span class="imp-item-name">{{ d.name }}</span>
                    <span class="imp-item-meta">{{ d.type || 'mysql' }}</span>
                  </el-checkbox>
                </el-checkbox-group>
                <div v-if="localDbs.length === 0" class="imp-empty">本机暂无数据库</div>
              </div>
            </div>

            <div class="imp-col">
              <div class="imp-col-header">
                <el-checkbox v-model="expFtpAllChecked" :indeterminate="expFtpIndeterminate" @change="onExpFtpAllToggle">
                  FTP ({{ localFtps.length }})
                </el-checkbox>
              </div>
              <div class="imp-col-body">
                <el-checkbox-group v-model="expFtps">
                  <el-checkbox
                    v-for="f in localFtps"
                    :key="f.username"
                    :value="f.username"
                    class="imp-item"
                  >
                    <span class="imp-item-name">{{ f.username }}</span>
                    <span class="imp-item-meta">{{ f.home_dir || f.path || '' }}</span>
                  </el-checkbox>
                </el-checkbox-group>
                <div v-if="localFtps.length === 0" class="imp-empty">本机暂无 FTP</div>
              </div>
            </div>
          </div>

          <div v-if="hasLocalList" class="sel-summary">
            已选网站 {{ expSites.length }} 个、数据库 {{ expDbs.length }} 个、FTP {{ expFtps.length }} 个
          </div>

          <el-form-item class="mig-plan-btn">
            <el-button @click="expStep = 1">上一步</el-button>
            <el-button :loading="precheckLoading" :disabled="!hasExportSelection" @click="doPrecheck">环境预检</el-button>
            <el-button
              v-if="precheckResult"
              type="primary"
              :loading="exporting"
              :disabled="expEnvBlocked"
              @click="startExport"
            >
              开始迁移
            </el-button>
            <span v-if="precheckResult && expEnvBlocked" class="field-note">预检未通过，请先修复环境问题</span>
          </el-form-item>

          <el-alert
            v-if="precheckResult"
            class="mig-alert"
            :type="expAlertType"
            :closable="false"
            show-icon
          >
            <template #title>
              {{ precheckResult.target === 'bt' ? '对端面板环境对比结果' : `目标面板（v${precheckResult.version}）环境对比结果` }}
            </template>
            <template #default>
              <template v-if="precheckResult.target === 'bt'">
                <div class="env-compare">
                  <div class="env-row" :class="{ bad: expPhpMissing.length > 0 }">
                    <div class="env-name">PHP 版本</div>
                    <div class="env-val">
                      本机需要：<b>{{ fmtBtRequired(expBtResult) }}</b>
                      <span class="env-sub">对端已装：{{ fmtBtPhp(expBtResult) }}</span>
                    </div>
                    <div class="env-status" :class="expPhpMissing.length > 0 ? 'bad' : 'ok'">
                      {{ expPhpMissing.length > 0 ? `缺少 ${fmtBtMissing(expBtResult)}` : '已满足' }}
                    </div>
                  </div>
                  <div v-if="expBtResult && expBtResult.mysql_required" class="env-row" :class="{ bad: expMysqlDiff === 'missing', warn: expMysqlNeedAck }">
                    <div class="env-name">MySQL</div>
                    <div class="env-val">
                      本机版本：<b>{{ expBtResult.mysql_local_version || '未知' }}</b>
                      <span class="env-sub">对端版本：{{ expBtResult.mysql_remote_version || (expBtResult.mysql_installed ? '已安装（版本未知）' : '未安装') }}</span>
                    </div>
                    <div class="env-status" :class="expMysqlStatusCls">{{ expMysqlStatusText }}</div>
                  </div>
                </div>
                <div v-if="expPhpMissing.length > 0" class="env-block-tip">
                  对端面板缺少以上 PHP 版本，请先在对端面板「软件商店」安装后再迁移
                </div>
                <div v-if="expMysqlDiff === 'missing'" class="env-block-tip">
                  对端面板未安装 MySQL，请先在对端面板安装 MySQL 后再迁移
                </div>
                <div v-if="expMysqlNeedAck" class="env-block-tip">
                  本机与对端 MySQL 版本不同，数据迁移后字符集/排序规则可能发生变化；本机为高版本而对端为低版本时，建表语句可能不被对端识别（面板已自动做 collation 降级处理）。
                </div>
                <el-checkbox v-if="expMysqlNeedAck" v-model="mysqlAck" class="mysql-ack">
                  我已知晓 MySQL 版本差异风险，仍要迁移
                </el-checkbox>
              </template>
              <template v-else>
                <span v-if="(precheckResult.missing || []).length === 0">目标面板环境完全满足，可以开始迁移。</span>
                <span v-else>目标面板缺少以下环境，迁移时请先在目标面板安装：{{ (precheckResult.missing || []).map(m => m.name).join('、') }}</span>
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
            <!-- 全部子任务成功：显示绿色"完成"页 -->
            <template v-if="task && task.status === 'success'">
              <el-result icon="success" title="迁出完成" sub-title="对端面板已恢复网站/数据库/FTP">
                <template #extra>
                  <el-button type="primary" @click="resetExport">完成</el-button>
                </template>
              </el-result>
            </template>
            <!-- 有子任务失败：显示失败列表，强制用户处理后再重试 -->
            <template v-else-if="task && task.status === 'failed'">
              <el-result icon="error" title="迁出未完成" sub-title="存在失败的子任务，必须全部处理后才能完成迁出">
                <template #extra>
                  <el-button @click="resetExport">取消</el-button>
                  <el-button type="primary" @click="expStep = 2">返回重新迁移</el-button>
                </template>
              </el-result>
              <div v-if="failedItems.length > 0" class="mig-fail-list">
                <div class="mig-fail-title">失败任务（{{ failedItems.length }} 项）：</div>
                <el-alert
                  v-for="(it, idx) in failedItems"
                  :key="idx"
                  :title="`${labelOf(it.type)}：${it.name}`"
                  :description="it.message"
                  type="error"
                  :closable="false"
                  show-icon
                  class="mig-fail-item"
                />
              </div>
              <div v-else class="mig-fail-list">
                <div class="mig-fail-title">整体迁出失败，请查看第 3 步的进度日志获取详细错误。</div>
              </div>
            </template>
            <!-- 任务尚未结束（运行中或刚被取消） -->
            <template v-else>
              <el-result icon="info" title="等待任务结果" sub-title="任务尚未完成" />
            </template>
          </template>
          <template v-else>
            <el-result icon="success" title="完成" />
          </template>
        </div>
      </el-tab-pane>

      <!-- ==================== 网站迁入 ==================== -->
      <el-tab-pane label="网站迁入" name="import">
        <el-steps :active="impStep === 4 ? 4 : impStep - 1" finish-status="success" align-center class="mig-steps">
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
          <div v-if="hasRemoteList" class="imp-three-col">
            <div class="imp-col">
              <div class="imp-col-header">
                <el-checkbox v-model="siteAllChecked" :indeterminate="siteIndeterminate" @change="onSiteAllToggle">
                  网站 ({{ remoteSites.length }})
                </el-checkbox>
              </div>
              <div class="imp-col-body">
                <el-checkbox-group v-model="impSelected">
                  <el-checkbox
                    v-for="s in remoteSites"
                    :key="s.name"
                    :value="s.name"
                    class="imp-item"
                  >
                    <span class="imp-item-name">{{ s.name }}</span>
                    <span class="imp-item-meta">{{ s.type || '-' }}<span v-if="s.php_version"> · PHP {{ s.php_version }}</span></span>
                  </el-checkbox>
                </el-checkbox-group>
                <div v-if="remoteSites.length === 0" class="imp-empty">源面板暂无网站</div>
              </div>
            </div>

            <div class="imp-col">
              <div class="imp-col-header">
                <el-checkbox v-model="dbAllChecked" :indeterminate="dbIndeterminate" @change="onDbAllToggle">
                  数据库 ({{ remoteDatabases.length }})
                </el-checkbox>
              </div>
              <div class="imp-col-body">
                <el-checkbox-group v-model="impDbs">
                  <el-checkbox
                    v-for="d in remoteDatabases"
                    :key="d.name"
                    :value="d.name"
                    class="imp-item"
                  >
                    <span class="imp-item-name">{{ d.name }}</span>
                    <span class="imp-item-meta">{{ d.type || 'mysql' }}</span>
                  </el-checkbox>
                </el-checkbox-group>
                <div v-if="remoteDatabases.length === 0" class="imp-empty">源面板暂无数据库</div>
              </div>
            </div>

            <div class="imp-col">
              <div class="imp-col-header">
                <el-checkbox v-model="ftpAllChecked" :indeterminate="ftpIndeterminate" @change="onFtpAllToggle">
                  FTP ({{ remoteFtps.length }})
                </el-checkbox>
              </div>
              <div class="imp-col-body">
                <el-checkbox-group v-model="impFtps">
                  <el-checkbox
                    v-for="f in remoteFtps"
                    :key="f.username"
                    :value="f.username"
                    class="imp-item"
                  >
                    <span class="imp-item-name">{{ f.username }}</span>
                    <span class="imp-item-meta">{{ f.path || '' }}</span>
                  </el-checkbox>
                </el-checkbox-group>
                <div v-if="remoteFtps.length === 0" class="imp-empty">源面板暂无 FTP</div>
              </div>
            </div>
          </div>

          <div v-if="hasRemoteList" class="sel-summary">
            已选网站 {{ impSelected.length }} 个、数据库 {{ impDbs.length }} 个、FTP {{ impFtps.length }} 个
          </div>

          <el-form-item class="mig-plan-btn">
            <el-button @click="impStep = 1">上一步</el-button>
            <el-button :loading="planning" :disabled="impSelected.length + impDbs.length + impFtps.length === 0" @click="doPlan">
              环境预检
            </el-button>
          </el-form-item>

          <template v-if="plan">
            <!-- 同名冲突提示：按需出现，无冲突不显示 -->
            <el-alert
              v-if="hasConflict"
              type="warning"
              :closable="false"
              show-icon
              class="mig-conflict-alert"
            >
              <template #title>目标机已存在同名项目，请确认如何处理</template>
              <template #default>
                <div v-if="conflictSites.length" class="conflict-line">
                  <span class="conflict-kind">网站：</span>{{ conflictSites.join('、') }}
                </div>
                <div v-if="conflictDbs.length" class="conflict-line">
                  <span class="conflict-kind">数据库：</span>{{ conflictDbs.join('、') }}
                </div>
                <div v-if="conflictFtps.length" class="conflict-line">
                  <span class="conflict-kind">FTP：</span>{{ conflictFtps.join('、') }}
                </div>
                <el-checkbox v-model="overwrite" class="conflict-overwrite">
                  覆盖目标机上的同名项目（未勾选则跳过同名项目，仅迁移新增项）
                </el-checkbox>
              </template>
            </el-alert>

            <el-alert
              :type="(plan.missing || []).length === 0 ? 'success' : 'warning'"
              :closable="false"
              show-icon
              class="mig-alert"
            >
              <template #title>
                {{ (plan.missing || []).length === 0 ? '本机环境完全满足，可直接迁移' : `本机缺少 ${(plan.missing || []).length} 项环境，请先安装环境后再迁移` }}
              </template>
              <template #default v-if="(plan.missing || []).length > 0">
                <div>勾选需要自动安装的环境（安装完成后自动继续迁移），未勾选前无法开始迁移：</div>
                <el-checkbox-group v-model="autoInstall">
                  <el-checkbox
                    v-for="m in plan.missing || []"
                    :key="m.key"
                    :value="m.key"
                    :disabled="!m.installable"
                  >
                    {{ m.name }}
                    <span class="field-note">{{ m.installable ? '（应用商店可安装）' : '（商店无此应用，需手动安装）' }}</span>
                  </el-checkbox>
                </el-checkbox-group>
                <div v-if="impBlockHint" class="field-note" style="color: #e6a23c;">{{ impBlockHint }}</div>
              </template>
            </el-alert>

            <el-form label-width="100px" label-position="left" class="mig-opt-form">
              <el-form-item>
                <el-button
                  type="primary"
                  :loading="importing"
                  :disabled="impSelected.length + impDbs.length + impFtps.length === 0 || impEnvBlocked"
                  @click="startImport"
                >
                  开始迁移
                </el-button>
                <span v-if="impEnvBlocked" class="field-note">请先安装缺失环境后再迁移</span>
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
const mysqlAck = ref(false) // 用户已确认 MySQL 版本差异风险
const exporting = ref(false)
const exportResult = ref(null)

// ---------- 迁入 ----------
const impStep = ref(1)
const impForm = reactive({ url: '', token: '', panelType: '', version: '', detected: false })
const impDetecting = ref(false)
const fetching = ref(false)
const remoteSites = ref([])
const remoteDatabases = ref([])
const remoteFtps = ref([])
const lastFetchedAt = ref('')
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

// 进入迁入第 2 步时，若尚未拉取源面板网站列表则自动加载
watch(impStep, (v) => {
  if (v === 2 && impForm.detected && remoteSites.value.length === 0 && !fetching.value) {
    fetchSites()
  }
})

// ---------- 迁出：对端面板（bt）环境对比 ----------
const expBtResult = computed(() => (precheckResult.value && precheckResult.value.target === 'bt' ? precheckResult.value.result : null))
const expPhpMissing = computed(() => (expBtResult.value && expBtResult.value.php_missing) || [])
const expMysqlDiff = computed(() => (expBtResult.value && expBtResult.value.mysql_diff) || 'none')
const expMysqlNeedAck = computed(() => ['diff', 'downgrade', 'unknown'].includes(expMysqlDiff.value))
const expMysqlStatusCls = computed(() => {
  if (expMysqlDiff.value === 'same') return 'ok'
  if (expMysqlDiff.value === 'missing') return 'bad'
  return 'warn'
})
const expMysqlStatusText = computed(() => {
  const r = expBtResult.value || {}
  const lv = r.mysql_local_version || '未知'
  const rv = r.mysql_remote_version || '未知'
  switch (expMysqlDiff.value) {
    case 'none': return '未选择数据库'
    case 'missing': return '对端未安装 MySQL，禁止迁移'
    case 'same': return '版本一致'
    case 'downgrade': return `版本不同（${lv} → ${rv}），高版本迁低版本存在兼容风险，需确认`
    case 'diff': return `版本不同（${lv} → ${rv}），需确认`
    case 'unknown': return '版本信息未知，需确认'
    default: return ''
  }
})
// 环境不满足时禁止「开始迁移」：bt 面板场景强制预检通过；kypanel 打包模式仅提示不阻断
const expEnvBlocked = computed(() => {
  if (precheckLoading.value) return expForm.panelType === 'bt'
  const r = precheckResult.value
  if (!r) return expForm.panelType === 'bt' // 未预检：bt 必须先预检
  if (r.target === 'bt') {
    const rr = r.result || {}
    if ((rr.php_missing || []).length > 0) return true // 对端缺 PHP 版本：阻断
    if (rr.mysql_required && !rr.mysql_installed) return true // 对端无 MySQL：阻断
    if (expMysqlNeedAck.value && !mysqlAck.value) return true // 版本差异未确认：阻断
    return false
  }
  return false // kypanel 打包不阻断
})
// 必须环境预检通过后才允许开始迁移
const expCanExport = computed(() => hasExportSelection.value && !!precheckResult.value && !expEnvBlocked.value)
// 迁入：本机缺失环境时禁止「开始迁移」，需先安装（勾选的可安装项将在迁移时自动安装）。
// 存在「商店无可安装」项，或「可安装但未勾选」项时均阻塞。
const impEnvBlocked = computed(() => {
  if (planning.value) return true
  const m = (plan.value && plan.value.missing) || []
  if (m.length === 0) return false
  const uninstallable = m.filter((x) => !x.installable)
  const unpicked = m.filter((x) => x.installable && !autoInstall.value.includes(x.key))
  return uninstallable.length > 0 || unpicked.length > 0
})
// 迁入阻塞原因提示文案
const impBlockHint = computed(() => {
  const m = (plan.value && plan.value.missing) || []
  const uninstallable = m.filter((x) => !x.installable)
  const unpicked = m.filter((x) => x.installable && !autoInstall.value.includes(x.key))
  if (uninstallable.length > 0) {
    return `商店无以下应用，请先手动安装后再迁移：${uninstallable.map((x) => x.name).join('、')}`
  }
  if (unpicked.length > 0) {
    return '请先勾选需要自动安装的环境后再迁移'
  }
  return ''
})
const expAlertType = computed(() => {
  if (precheckResult.value && precheckResult.value.target === 'bt') {
    if (expPhpMissing.value.length > 0 || expMysqlDiff.value === 'missing') return 'error'
    if (expMysqlNeedAck.value) return 'warning'
    return 'success'
  }
  if (precheckResult.value && precheckResult.value.missing && precheckResult.value.missing.length > 0) return 'warning'
  return 'success'
})
const downloadUrl = computed(() => {
  if (!exportResult.value) return ''
  const auth = useAuthStore()
  return `${location.origin}/api/migrate/download/${exportResult.value.id}?token=${auth.token || ''}`
})

// 子任务类型 → 中文标签（仅展示用，类型本身是后端契约不要改）
const ITEM_LABELS = {
  site: '网站',
  'site-file': '网站文件',
  'site-config': '网站配置（伪静态/SSL/运行目录）',
  database: '数据库',
  'database-data': '数据库数据',
  ftp: 'FTP 账号'
}
function labelOf(t) { return ITEM_LABELS[t] || t }

// 当前任务中所有失败项，供第 4 步失败页面渲染
const failedItems = computed(() => {
  if (!task.value || !Array.isArray(task.value.items)) return []
  return task.value.items.filter((it) => it.status === 'failed')
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
  // 三个接口独立容错：某项环境未安装（如 MySQL）导致接口失败时，
  // 对应列表置空即可，不影响其它列的加载，也不弹出报错。
  try {
    const r = await request.get('/site/list')
    localSites.value = r.data || []
  } catch (e) {
    localSites.value = []
  }
  try {
    const r = await request.get('/database/list')
    localDbs.value = r.data || []
  } catch (e) {
    localDbs.value = []
  }
  try {
    const r = await request.get('/ftp/list')
    localFtps.value = r.data || []
  } catch (e) {
    localFtps.value = []
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
// 记录预检发起时的迁移对象快照，返回后若对象已变化则作废结果
let precheckSnapshot = ''
async function doPrecheck() {
  if (!hasExportSelection.value) {
    ElMessage.warning('请先选择要迁出的内容')
    return
  }
  precheckLoading.value = true
  precheckResult.value = null
  mysqlAck.value = false
  precheckSnapshot = JSON.stringify([expSites.value, expDbs.value, expFtps.value])
  try {
    const res = await request.post('/migrate/export/precheck', {
      panel_type: expForm.panelType,
      panel_url: expForm.url,
      panel_token: expForm.token,
      sites: expSites.value,
      databases: expDbs.value,
      ftps: expFtps.value
    })
    // 预检期间迁移对象发生变化 → 本次结果作废，需重新预检
    if (precheckSnapshot !== JSON.stringify([expSites.value, expDbs.value, expFtps.value])) {
      return
    }
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
  try {
    if (expForm.panelType === 'kypanel') {
      expStep.value = 3
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
    expStep.value = 3
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
  // 后端按「项目名 → 是否覆盖」的 map 逐项决策，需把弹窗的全局选择展开到每个同名项目
  const siteOverwrite = {}
  conflict.value.sites.forEach((n) => { siteOverwrite[n] = conflictChoice.siteOverwrite })
  const databaseOverwrite = {}
  conflict.value.databases.forEach((n) => { databaseOverwrite[n] = conflictChoice.dbOverwrite })
  const ftpOverwrite = {}
  conflict.value.ftps.forEach((n) => { ftpOverwrite[n] = conflictChoice.ftpOverwrite })
  doBTExport({
    site_overwrite: siteOverwrite,
    database_overwrite: databaseOverwrite,
    ftp_overwrite: ftpOverwrite
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

// ---------- 迁入：获取源面板网站/数据库/FTP ----------
async function fetchSites() {
  if (!impForm.detected) {
    ElMessage.warning('请先连接并识别源面板')
    return
  }
  fetching.value = true
  try {
    const res = await request.post('/migrate/import/fetch-sites', {
      panel_type: impForm.panelType,
      panel_url: impForm.url,
      panel_token: impForm.token
    })
    const payload = res.data || {}
    remoteSites.value = payload.sites || []
    remoteDatabases.value = payload.databases || []
    remoteFtps.value = payload.ftps || []
    lastFetchedAt.value = new Date().toLocaleTimeString()
    // 拉取新列表后清空旧选择 + 旧的预检结果
    impSelected.value = []
    impDbs.value = []
    impFtps.value = []
    plan.value = null
    overwrite.value = false
    const total = remoteSites.value.length + remoteDatabases.value.length + remoteFtps.value.length
    if (total === 0) ElMessage.info('源面板暂无可迁移的项目')
    else ElMessage.success(`已获取网站 ${remoteSites.value.length} 个、数据库 ${remoteDatabases.value.length} 个、FTP ${remoteFtps.value.length} 个`)
  } finally {
    fetching.value = false
  }
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
      sites: impSelected.value,
      databases: impDbs.value,
      ftps: impFtps.value
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

function fmtBtRequired(result) {
  const list = result && result.php_required ? Object.keys(result.php_required) : []
  return list.length ? list.map((v) => `PHP ${v}`).join('、') : '（静态/纯前端网站）'
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
  // 后台仍有任务在跑（或停留在失败结果页）时不清空状态，
  // 下次打开可以直接回到进度页继续查看，而不是从第 1 步重来
  if (task.value && task.value.status !== 'success') {
    return
  }
  clearInterval(taskTimer)
  activeTab.value = 'export'
  resetExport()
  resetImport()
}

// 重新打开工具时，若后台还有未完成（进行中/失败）的迁移任务，
// 直接切到对应 Tab 并跳到进度步骤继续查看，避免每次都从第 1 步开始。
async function resumePendingTask() {
  try {
    const res = await request.get('/migrate/tasks')
    const list = res.data || []
    const pending = list.find((t) => t.status === 'running') || list.find((t) => t.status === 'failed')
    if (!pending) return

    const isImport = pending.kind === 'import'
    activeTab.value = isImport ? 'import' : 'export'
    task.value = pending
    // 暂停在第 3 步（进度页），成功后自动跳第 4 步
    if (isImport) impStep.value = 3
    else expStep.value = 3

    if (pending.status === 'running') {
      pollTask(pending.id, () => {
        if (isImport) impStep.value = 4
        else expStep.value = 4
      })
    }
  } catch (e) {
    /* 忽略：恢复失败不影响正常使用 */
  }
}

watch(visible, (v) => {
  if (v) {
    loadLocalData()
    loadStoredAPI(expForm, EXP_STORE_KEY)
    loadStoredAPI(impForm, IMP_STORE_KEY)
    resumePendingTask()
  }
})

// 迁移对象变化 → 作废已通过的预检结果，需重新手动预检
watch([expSites, expDbs, expFtps], () => {
  if (precheckResult.value || precheckLoading.value) {
    precheckResult.value = null
    mysqlAck.value = false
    ElMessage.info('迁移对象已变化，请重新进行环境预检')
  }
})

// 迁移对象变化 → 作废已出的环境预检结果，需重新手动预检
watch([impSelected, impDbs, impFtps], () => {
  if (plan.value || planning.value) {
    plan.value = null
    autoInstall.value = []
    ElMessage.info('迁移对象已变化，请重新进行环境预检')
  }
})

// 三列布局：是否已加载到源面板数据（用于控制「对比环境」按钮可用性之外的空态渲染）
const hasRemoteList = computed(() => remoteSites.value.length + remoteDatabases.value.length + remoteFtps.value.length > 0)
// 三列布局：迁出时是否已有本机数据
const hasLocalList = computed(() => localSites.value.length + localDbs.value.length + localFtps.value.length > 0)

// 同名冲突：来自 plan 接口的 existing_* 字段。空集合对应不显示告警。
const conflictSites = computed(() => (plan.value && plan.value.existing_sites) || [])
const conflictDbs = computed(() => (plan.value && plan.value.existing_dbs) || [])
const conflictFtps = computed(() => (plan.value && plan.value.existing_ftps) || [])
const hasConflict = computed(() => conflictSites.value.length + conflictDbs.value.length + conflictFtps.value.length > 0)

// 各列「全选/半选」状态
function useAllChecked(source, selected) {
  const all = computed(() => source.value.length > 0 && selected.value.length === source.value.length)
  const indeterminate = computed(() => selected.value.length > 0 && selected.value.length < source.value.length)
  function toggle(val) {
    selected.value = val ? source.value.map((x) => x.name || x.username) : []
  }
  return { all, indeterminate, toggle }
}
const { all: siteAllChecked, indeterminate: siteIndeterminate, toggle: onSiteAllToggle } = useAllChecked(remoteSites, impSelected)
const { all: dbAllChecked, indeterminate: dbIndeterminate, toggle: onDbAllToggle } = useAllChecked(remoteDatabases, impDbs)
const { all: ftpAllChecked, indeterminate: ftpIndeterminate, toggle: onFtpAllToggle } = useAllChecked(remoteFtps, impFtps)
const { all: expSiteAllChecked, indeterminate: expSiteIndeterminate, toggle: onExpSiteAllToggle } = useAllChecked(localSites, expSites)
const { all: expDbAllChecked, indeterminate: expDbIndeterminate, toggle: onExpDbAllToggle } = useAllChecked(localDbs, expDbs)
const { all: expFtpAllChecked, indeterminate: expFtpIndeterminate, toggle: onExpFtpAllToggle } = useAllChecked(localFtps, expFtps)

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
/* 多选标签全部展示：禁止折叠为 +N，按钮紧贴文字以区分类型 */
.mig-multiselect :deep(.el-select__wrapper) {
  min-height: auto;
  height: auto;
}
.mig-multiselect :deep(.el-select__tags) {
  flex-wrap: wrap;
  max-height: none;
  gap: 4px 6px;
}
.mig-multiselect :deep(.el-select__tags-text) {
  display: inline-flex;
  align-items: center;
}
.mig-multiselect :deep(.el-tag) {
  margin: 2px 4px 2px 0;
}
.env-compare {
  margin-top: 4px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 6px;
  overflow: hidden;
}
.env-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 12px;
  font-size: 13px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.env-row:last-child {
  border-bottom: none;
}
.env-row.bad {
  background: rgba(245, 108, 108, 0.06);
}
.env-row.warn {
  background: rgba(230, 162, 60, 0.08);
}
.env-name {
  flex: 0 0 64px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}
.env-val {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.env-sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.env-status {
  flex: 0 0 auto;
  font-size: 12px;
  font-weight: 600;
}
.env-status.ok {
  color: var(--el-color-success);
}
.env-status.bad {
  color: var(--el-color-danger);
}
.env-status.warn {
  color: var(--el-color-warning);
}
.env-block-tip {
  margin-top: 8px;
  font-size: 12px;
  line-height: 1.6;
  color: var(--el-color-warning);
}
.mysql-ack {
  margin-top: 10px;
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
.mig-fail-list {
  margin-top: 16px;
  text-align: left;
}
.mig-fail-title {
  font-weight: 600;
  margin-bottom: 8px;
  color: var(--el-color-danger);
}
.mig-fail-item {
  margin-bottom: 8px;
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
/* ===== 迁入：网站/数据库/FTP 三列多选 ===== */
.imp-three-col {
  display: grid;
  grid-template-columns: 1fr 1fr 1fr;
  gap: 10px;
  margin-top: 6px;
}
.imp-col {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 6px;
  display: flex;
  flex-direction: column;
  min-height: 220px;
  max-height: 320px;
}
.imp-col-header {
  padding: 8px 12px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  background: var(--el-fill-color-light);
  font-weight: 600;
  font-size: 13px;
}
.imp-col-body {
  flex: 1;
  overflow-y: auto;
  padding: 6px 12px 8px;
}
.imp-item {
  display: flex !important;
  align-items: center;
  width: 100%;
  margin-right: 0 !important;
  margin-bottom: 4px;
  white-space: normal;
  height: auto;
  line-height: 1.4;
}
.imp-item :deep(.el-checkbox__label) {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  white-space: normal;
  width: 100%;
}
.imp-item-name {
  font-weight: 500;
  color: var(--el-text-color-primary);
  word-break: break-all;
}
.imp-item-meta {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  margin-top: 1px;
}
.imp-empty {
  padding: 12px 0;
  text-align: center;
  font-size: 12px;
  color: var(--el-text-color-placeholder);
}
.mig-conflict-alert {
  margin-top: 12px;
}
.conflict-line {
  font-size: 13px;
  line-height: 1.7;
  word-break: break-all;
}
.conflict-kind {
  font-weight: 600;
  margin-right: 4px;
}
.conflict-overwrite {
  margin-top: 6px;
}
</style>
