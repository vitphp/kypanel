<template>
  <div class="database-page">
    <el-card shadow="never" class="tabs-card">
      <el-tabs v-model="activeType" type="border-card" @tab-change="onTabChange">
        <el-tab-pane
          v-for="t in dbTypes"
          :key="t.type"
          :label="dbLabel(t)"
          :name="t.type"
        >
          <div class="toolbar">
            <div class="toolbar-left">
              <el-button
                type="primary"
                :disabled="!statusMap[t.type]?.available"
                @click="openCreate(t.type)"
              >
                <el-icon><Plus /></el-icon>&nbsp;创建数据库
              </el-button>

              <!-- 服务状态与启停（仅在环境已装且非 SQLite 时显示） -->
              <template v-if="statusMap[t.type]?.available && t.type !== 'sqlite'">
                <el-dropdown
                  trigger="hover"
                  :hide-on-click="false"
                  @command="(cmd) => handleServiceAction(t.type, cmd)"
                >
                  <span
                    class="service-status"
                    :class="{ 'is-loading': serviceLoadingMap[t.type], 'is-unknown': !serviceStatusMap[t.type] || serviceStatusMap[t.type] === 'unknown' }"
                  >
                    <span class="status-dot" :class="serviceStatusMap[t.type] || 'unknown'"></span>
                    <span class="status-text">{{ serviceStatusText(t.type) }}</span>
                    <el-icon class="caret" :size="12"><ArrowDown /></el-icon>
                  </span>
                  <template #dropdown>
                    <el-dropdown-menu>
                      <el-dropdown-item command="start" :disabled="serviceStatusMap[t.type] === 'running' || serviceStatusMap[t.type] === 'unknown'">
                        <el-icon><VideoPlay /></el-icon>启动
                      </el-dropdown-item>
                      <el-dropdown-item command="stop" :disabled="serviceStatusMap[t.type] !== 'running'">
                        <el-icon><VideoPause /></el-icon>停止
                      </el-dropdown-item>
                      <el-dropdown-item command="restart" :disabled="serviceStatusMap[t.type] === 'unknown'">
                        <el-icon><Refresh /></el-icon>重启
                      </el-dropdown-item>
                    </el-dropdown-menu>
                  </template>
                </el-dropdown>
                <el-button @click="openRootPassword(t.type)">root 密码</el-button>
              </template>

              <el-button :disabled="!statusMap[t.type]?.available" @click="load()">
                刷新
              </el-button>
            </div>
            <div class="toolbar-right">
              <el-input
                v-model="keywordMap[t.type]"
                placeholder="搜索数据库名"
                clearable
                style="width: 220px"
              />
            </div>
          </div>

          <!-- 首次加载骨架屏 -->
          <Skeleton v-if="loading" type="table" :rows="6" :columns="[{flex:2},{flex:1},{flex:1},{flex:1},{flex:1},{width:'160px'}]" />

          <!-- 安装/卸载中：遮罩提示（覆盖 available 状态，避免 service 不可用时调接口报错） -->
          <div v-else-if="installingMap[t.type]" class="env-missing">
            <div class="env-missing-inner">
              <el-icon :size="48" color="#409eff"><Warning /></el-icon>
              <h3 v-if="pendingActionMap[t.type] === 'uninstall'">正在卸载 {{ dbLabel(t) }}...</h3>
              <h3 v-else>正在安装 {{ dbLabel(t) }}...</h3>
              <p>{{ pendingActionMap[t.type] === 'uninstall' ? '卸载进行中，请稍候...' : '安装进行中，请稍候...' }}</p>
            </div>
          </div>

          <!-- 环境未安装：遮罩提示 -->
          <div v-else-if="!statusMap[t.type]?.available" class="env-missing">
            <div class="env-missing-inner">
              <el-icon :size="48" color="#e6a23c"><Warning /></el-icon>
              <h3>未检测到 {{ dbLabel(t) }} 环境</h3>
              <p>{{ envStatusMap[t.type]?.remarks || statusMap[t.type]?.message || '请先安装对应的数据库环境' }}</p>
              <div v-if="envStatusMap[t.type]?.select_version" class="version-row">
                <span>选择版本：</span>
                <el-select v-model="selectedVersionMap[t.type]" size="default" style="width: 160px;">
                  <el-option v-for="v in envStatusMap[t.type]?.versions" :key="v" :label="v" :value="v" />
                </el-select>
              </div>
              <el-button type="primary" size="large" :loading="installingMap[t.type]" @click="installEnv(t.type)">
                <el-icon><Download /></el-icon> 一键安装
              </el-button>
            </div>
          </div>

          <el-table v-else :data="filtered(t.type)" v-loading="loading" stripe>
            <!-- 通用列：数据库名 -->
            <el-table-column label="数据库名" min-width="180" show-overflow-tooltip>
              <template #default="{ row }">
                <span>{{ row.name }}</span>
                <el-button class="copy-btn" size="small" text title="复制数据库名" @click="copyPwd(row.name, '数据库名')">
                  <el-icon><CopyDocument /></el-icon>
                </el-button>
              </template>
            </el-table-column>

            <!-- MySQL / PgSQL 专属列 -->
            <template v-if="['mysql','pgsql'].includes(t.type)">
              <el-table-column label="用户名" min-width="150">
                <template #default="{ row }">
                  <span>{{ row.user }}</span>
                  <el-button class="copy-btn" size="small" text title="复制用户名" @click="copyPwd(row.user, '用户名')">
                    <el-icon><CopyDocument /></el-icon>
                  </el-button>
                </template>
              </el-table-column>
              <el-table-column label="密码" min-width="170">
                <template #default="{ row }">
                  <div v-if="row.password" class="password-cell">
                    <span v-if="isMaskedPwd(row.password)" class="pwd-mask">仅超管可见</span>
                    <template v-else>
                      <span class="pwd-mask">{{ visiblePwd[row.name] ? row.password : '********' }}</span>
                      <el-button size="small" text :title="visiblePwd[row.name] ? '隐藏密码' : '显示密码'" @click="togglePwd(row.name)">
                        <el-icon><View v-if="visiblePwd[row.name]" /><Hide v-else /></el-icon>
                      </el-button>
                      <el-button size="small" text title="复制密码" @click="copyPwd(row.password)">
                        <el-icon><CopyDocument /></el-icon>
                      </el-button>
                    </template>
                  </div>
                  <div v-else class="password-cell">
                    <span class="pwd-mask">未记录</span>
                    <el-button size="small" type="primary" link @click="openPwd(row)">设置密码</el-button>
                  </div>
                </template>
              </el-table-column>
              <el-table-column prop="charset" label="编码" min-width="100" />
              <el-table-column v-if="t.type === 'mysql'" label="备份" min-width="110">
                <template #default="{ row }">
                  <div class="backup-cell">
                    <el-button size="small" type="primary" link @click="openBackup(row)">{{ row.backup || '0个' }}</el-button>
                    <el-button size="small" type="primary" link @click="createBackupFromRow(row)">备份</el-button>
                  </div>
                </template>
              </el-table-column>
              <el-table-column v-else-if="t.type === 'pgsql'" label="备份" min-width="100">
                <template #default="{ row }">
                  <span>{{ row.backup || '0个' }}</span>
                </template>
              </el-table-column>
              <el-table-column prop="location" label="数据库位置" min-width="110" />
              <el-table-column label="备注" min-width="160">
                <template #default="{ row }">
                  <div v-if="editingCommentId === row.name" class="comment-edit">
                    <el-input
                      ref="commentInputRef"
                      v-model="editingCommentText"
                      type="textarea"
                      :rows="2"
                      placeholder="请输入备注"
                      @blur="onCommentBlur(row)"
                      @keydown.enter.prevent="onCommentBlur(row)"
                      @keydown.esc.prevent="cancelCommentEdit(row)"
                    />
                  </div>
                  <div v-else class="comment-cell" @click="startEdit(row)">
                    <span :class="['comment-text', row.comment ? 'has-comment' : 'no-comment']">{{ row.comment || '—' }}</span>
                  </div>
                </template>
              </el-table-column>
            </template>

            <!-- SQLServer -->
            <el-table-column
              v-if="t.type === 'sqlserver'"
              prop="name"
              label="数据库名"
              min-width="240"
            />

            <!-- MongoDB -->
            <el-table-column
              v-if="t.type === 'mongodb'"
              prop="name"
              label="数据库名"
              min-width="200"
            />
            <el-table-column
              v-if="t.type === 'mongodb'"
              label="大小"
              min-width="120"
            >
              <template #default="{ row }">
                {{ formatBytes(row.size) }}
              </template>
            </el-table-column>

            <!-- Redis -->
            <el-table-column
              v-if="t.type === 'redis'"
              prop="name"
              label="逻辑库"
              min-width="120"
            />
            <el-table-column
              v-if="t.type === 'redis'"
              prop="keys"
              label="Key 数"
              min-width="120"
            />
            <el-table-column
              v-if="t.type === 'redis'"
              prop="comment"
              label="说明"
              min-width="180"
            />

            <!-- SQLite -->
            <el-table-column
              v-if="t.type === 'sqlite'"
              prop="name"
              label="文件名"
              min-width="200"
            />
            <el-table-column
              v-if="t.type === 'sqlite'"
              label="大小"
              min-width="120"
            >
              <template #default="{ row }">
                {{ formatBytes(row.size) }}
              </template>
            </el-table-column>
            <el-table-column
              v-if="t.type === 'sqlite'"
              prop="path"
              label="路径"
              min-width="240"
              show-overflow-tooltip
            />

            <el-table-column label="操作" min-width="160" align="center" fixed="right">
              <template #default="{ row }">
                <div class="ops-cell">
                  <el-button size="small" type="primary" link @click="manageDb(row)">管理</el-button>
                  <el-button v-if="t.type === 'mysql' || t.type === 'sqlite'" size="small" type="primary" link @click="openImport(row)">导入</el-button>
                  <el-button v-if="t.type === 'mysql'" size="small" type="primary" link @click="openPerms(row)">权限</el-button>
                  <el-button v-if="t.type === 'mysql'" size="small" type="primary" link @click="openPwd(row)">改密</el-button>
                  <el-button size="small" type="danger" link @click="remove(t.type, row)">删除</el-button>
                </div>
              </template>
            </el-table-column>
          </el-table>

          <el-empty v-if="!loading && statusMap[t.type]?.available && filtered(t.type).length === 0" description="暂无数据" />
        </el-tab-pane>
      </el-tabs>
    </el-card>

    <!-- 创建弹窗 -->
    <el-dialog v-model="createVisible" :title="dialogTitle" width="460px">
      <el-form :model="form" label-width="90px">
        <el-form-item label="数据库名" required>
          <el-input v-model="form.name" :placeholder="namePlaceholder" />
        </el-form-item>

        <!-- 关系型数据库用户名/密码（SQLite/SQLServer/MongoDB/Redis 不需要） -->
        <template v-if="dialogNeedAuth">
          <el-form-item label="用户名">
            <el-input v-model="form.user" placeholder="留空则与数据库名相同" />
          </el-form-item>
          <el-form-item label="密码" required>
            <el-input v-model="form.password" :type="createPwdVisible ? 'text' : 'password'" placeholder="设置访问密码">
              <template #suffix>
                <el-icon class="pwd-eye-icon" :size="16" @click="createPwdVisible = !createPwdVisible">
                  <Hide v-if="createPwdVisible" />
                  <View v-else />
                </el-icon>
                <el-tooltip content="随机生成新密码" placement="top">
                  <el-icon class="pwd-refresh-icon" :size="16" @click="randomPasswordNow"><Refresh /></el-icon>
                </el-tooltip>
              </template>
            </el-input>
          </el-form-item>
          <el-form-item v-if="activeType === 'mysql'" label="编码">
            <el-select v-model="form.charset" filterable placeholder="默认 utf8mb4" style="width: 100%">
              <el-option v-for="cs in mysqlCharsets" :key="cs" :label="cs" :value="cs" />
            </el-select>
          </el-form-item>
        </template>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="create">
          创建
        </el-button>
      </template>
    </el-dialog>

    <!-- 管理：连接信息 -->
    <el-dialog v-model="connVisible" title="数据库连接信息" width="420px">
      <el-descriptions :column="1" border>
        <el-descriptions-item label="数据库名">{{ currentRow?.name }}</el-descriptions-item>
        <el-descriptions-item label="主机">127.0.0.1</el-descriptions-item>
        <el-descriptions-item label="端口">{{ dbPortMap[activeType] || '-' }}</el-descriptions-item>
        <el-descriptions-item label="用户名">{{ currentRow?.user || '-' }}</el-descriptions-item>
        <el-descriptions-item label="密码">
          <span v-if="currentRow?.password">{{ currentRow.password }}</span>
          <span v-else>-</span>
        </el-descriptions-item>
      </el-descriptions>
      <template #footer>
        <el-button type="primary" @click="connVisible = false">确定</el-button>
      </template>
    </el-dialog>

    <!-- 设置权限 -->
    <el-dialog v-model="permVisible" title="设置数据库权限" width="460px">
      <el-form label-width="90px">
        <el-form-item label="数据库">{{ currentRow?.name }}</el-form-item>
        <el-form-item label="访问权限">
          <el-select v-model="permMode" style="width: 100%;">
            <el-option label="本地服务器 (localhost)" value="local" />
            <el-option label="所有人 (%)" value="all" />
            <el-option label="指定 IP" value="ip" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="permMode === 'ip'" label="允许 IP">
          <el-input
            v-model="permIps"
            type="textarea"
            :rows="5"
            placeholder="一行一个 IP 地址，例如：&#10;192.168.1.100&#10;10.0.0.5"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="permVisible = false">取消</el-button>
        <el-button type="primary" :loading="permSaving" @click="savePerms">确定</el-button>
      </template>
    </el-dialog>

    <!-- 修改密码 -->
    <el-dialog v-model="pwdVisible" :title="currentRow?.password ? '修改数据库密码' : '设置数据库密码'" width="420px">
      <el-form label-width="90px">
        <div class="pwd-info-row">
          <span class="pwd-info-item"><label>数据库</label>{{ currentRow?.name }}</span>
          <span class="pwd-info-item"><label>用户名</label>{{ currentRow?.user || '-' }}</span>
        </div>
        <el-form-item label="新密码" required>
          <el-input v-model="newPassword" type="text" placeholder="请输入新密码">
            <template #append>
              <el-button text title="随机生成密码" @click="newPassword = generateRandomPwd()">
                <el-icon><Refresh /></el-icon>
              </el-button>
            </template>
          </el-input>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="pwdVisible = false">取消</el-button>
        <el-button type="primary" :loading="pwdSaving" @click="savePwd">确定</el-button>
      </template>
    </el-dialog>

    <!-- 数据库备份 -->
    <el-dialog v-model="backupVisible" title="数据库备份" width="800px">
      <div class="backup-toolbar">
        <el-button type="primary" :loading="backupLoading" @click="onCreateBackup">立即备份</el-button>
        <span class="backup-db-name">{{ backupRow?.name }}</span>
      </div>
      <el-table :data="backupList" v-loading="backupLoading" stripe>
        <el-table-column prop="name" label="文件名称" min-width="260" show-overflow-tooltip />
        <el-table-column prop="storage" label="存储对象" min-width="100" />
        <el-table-column prop="sizeText" label="大小" min-width="100" />
        <el-table-column prop="time" label="备份时间" min-width="170" />
        <el-table-column prop="remark" label="备注" min-width="120" />
        <el-table-column label="操作" min-width="140" align="center" fixed="right">
          <template #default="{ row }">
            <div class="ops-cell">
              <el-button size="small" type="primary" link @click="restoreBackup(row)">恢复</el-button>
              <el-button size="small" type="primary" link @click="downloadBackup(row)">下载</el-button>
              <el-button size="small" type="danger" link @click="deleteBackup(row)">删除</el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!backupLoading && backupList.length === 0" description="暂无备份" />
    </el-dialog>

    <!-- 数据库导入 -->
    <el-dialog v-model="importVisible" title="导入数据库" width="560px" :close-on-click-modal="false" @closed="onImportClosed">
      <div class="import-db-name">数据库：<strong>{{ importRow?.name }}</strong></div>
      <el-tabs v-model="importMode" type="border-card">
        <el-tab-pane label="从备份导入" name="backup">
          <div v-if="importBackupLoading" class="import-loading">加载备份列表...</div>
          <el-select
            v-else
            v-model="selectedBackupFile"
            placeholder="请选择备份文件"
            style="width: 100%"
            clearable
          >
            <el-option
              v-for="item in importBackupList"
              :key="item.name"
              :label="`${item.name} (${item.sizeText || item.size || ''})`"
              :value="item.name"
            />
          </el-select>
          <el-empty v-if="!importBackupLoading && importBackupList.length === 0" description="暂无可用备份" :image-size="80" />
        </el-tab-pane>
        <el-tab-pane label="上传文件导入" name="file">
          <el-upload
            ref="uploadImportRef"
            drag
            action="/api/database/import"
            :data="{ type: activeType, name: importRow?.name }"
            :headers="{ Authorization: `Bearer ${authStore.token}` }"
            :show-file-list="true"
            :limit="1"
            :auto-upload="false"
            :file-list="importUploadFileList"
            accept=".sql,.sql.gz,.db,.sqlite"
            :before-upload="beforeImportUpload"
            :on-success="onImportUploadSuccess"
            :on-error="onImportUploadError"
            :on-change="onImportFileChange"
          >
            <el-icon class="el-icon--upload"><upload-filled /></el-icon>
            <div class="el-upload__text">
              拖拽文件到此处或 <em>点击上传</em>
            </div>
            <template #tip>
              <div class="el-upload__tip">
                支持 .sql、.sql.gz（MySQL）、.db / .sqlite（SQLite）
              </div>
            </template>
          </el-upload>
        </el-tab-pane>
      </el-tabs>
      <template #footer>
        <el-button @click="importVisible = false">取消</el-button>
        <el-button type="primary" :loading="importLoading" @click="confirmImport">开始导入</el-button>
      </template>
    </el-dialog>

    <!-- root 密码管理弹窗 -->
    <el-dialog
      v-model="rootPwdDialogVisible"
      :title="`${dbLabel(dbTypes.find(t => t.type === rootPwdType))} root 密码`"
      width="540px"
      :close-on-click-modal="false"
    >
      <div v-if="rootInfoMap[rootPwdType]" class="root-pwd-content">
        <el-form label-width="90px">
          <el-form-item label="账户">
            <b>{{ rootInfoMap[rootPwdType].user }}</b>
            <span class="muted">&nbsp;@&nbsp;{{ rootInfoMap[rootPwdType].host }}</span>
          </el-form-item>
          <el-form-item label="当前密码">
            <div class="pwd-row">
              <span v-if="rootInfoMap[rootPwdType].masked" class="pwd-text muted">仅超管可见</span>
              <template v-else>
                <span class="pwd-text">{{ rootInfoMap[rootPwdType].password || '（未设置）' }}</span>
                <el-button
                  v-if="rootInfoMap[rootPwdType].password"
                  size="small"
                  text
                  @click="copyText(rootInfoMap[rootPwdType].password)"
                >复制</el-button>
              </template>
            </div>
          </el-form-item>
          <el-alert
            v-if="rootInfoMap[rootPwdType].hint"
            :title="rootInfoMap[rootPwdType].hint"
            :type="rootInfoMap[rootPwdType].support_change ? 'info' : 'warning'"
            :closable="false"
            show-icon
          />
        </el-form>

        <template v-if="rootInfoMap[rootPwdType].support_change">
          <el-divider>修改密码</el-divider>
          <el-form label-width="90px">
            <el-form-item label="新密码">
              <el-input v-model="newRootPwd" placeholder="留空则不修改，点右侧可随机生成">
                <template #append>
                  <el-button text title="随机生成密码" @click="newRootPwd = generateRandomPwd()">
                    <el-icon><Refresh /></el-icon>
                  </el-button>
                </template>
              </el-input>
            </el-form-item>
          </el-form>
        </template>
      </div>
      <template #footer>
        <el-button @click="rootPwdDialogVisible = false">关闭</el-button>
        <el-button
          v-if="rootInfoMap[rootPwdType]?.support_change"
          type="primary"
          :loading="savingRootPwd"
          @click="changeRootPassword"
        >保存新密码</el-button>
      </template>
    </el-dialog>

  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, View, Hide, CopyDocument, Refresh, UploadFilled, Download, Warning, ArrowDown, VideoPlay, VideoPause } from '@element-plus/icons-vue'
import request from '../utils/request'
import { formatBytes as _fmtBytes } from '../utils/format'
import { useAuthStore } from '../stores/auth'
import { useInstallTrackerStore } from '../stores/installTracker'
import Skeleton from '../components/Skeleton.vue'

const authStore = useAuthStore()
const activeType = ref('mysql')
const dbTypes = ref([])
const statusMap = ref({})
const envStatusMap = ref({})
const loading = ref(true)
const listMap = ref({})
const keywordMap = ref({})
const createVisible = ref(false)
const creating = ref(false)
const createPwdVisible = ref(true) // 创建弹窗密码默认明文显示
const form = ref({ name: '', user: '', password: '', charset: '' })

// 一键安装状态
const installingMap = ref({})
const pendingActionMap = ref({}) // {mysql:'install'|'uninstall'|''}  区分安装/卸载中状态
const selectedVersionMap = ref({})
const installTimer = ref(null)

// 弹窗状态
const connVisible = ref(false)
const permVisible = ref(false)
const pwdVisible = ref(false)
const commentVisible = ref(false)
const currentRow = ref(null)
const visiblePwd = ref({})

const permMode = ref('local')
const permIps = ref('')
const permSaving = ref(false)

const newPassword = ref('')
const pwdSaving = ref(false)

const backupVisible = ref(false)
const backupLoading = ref(false)
const backupList = ref([])
const backupRow = ref(null)

// 导入弹窗
const importVisible = ref(false)
const importLoading = ref(false)
const importRow = ref(null)
const importMode = ref('backup')
const importBackupList = ref([])
const importBackupLoading = ref(false)
const selectedBackupFile = ref('')
const uploadImportRef = ref(null)
const importUploadFileList = ref([])

// 服务状态与启停（仅对有 systemd 服务的数据库类型）
const serviceStatusMap = ref({}) // {mysql:'running', redis:'stopped', ...}
const serviceLoadingMap = ref({}) // {mysql:'start'|'stop'|'restart'|''}
let serviceStatusTimer = null

// root 密码管理弹窗
const rootPwdDialogVisible = ref(false)
const rootPwdType = ref('')
const rootInfoMap = ref({})
const newRootPwd = ref('')
const confirmRootPwd = ref('')
const showRootPwd = ref(false)
const savingRootPwd = ref(false)

const editingCommentId = ref('')
const editingCommentText = ref('')
const commentInputRef = ref(null)
const commentBlurLock = ref(false)

const dbPortMap = {
  mysql: 3306,
  pgsql: 5432,
  sqlserver: 1433,
  mongodb: 27017,
  redis: 6379
}

const dialogTitle = computed(() => {
  const t = dbTypes.value.find((t) => t.type === activeType.value)
  return `创建 ${t ? dbLabel(t) : ''} 数据库`
})

// 数据库类型显示名：优先用 env-status 的真实环境名（后端对 MySQL 实装 MariaDB 时
// 返回 "MySQL (MariaDB)" 标注真实引擎），未探测到时回退静态 label。
function dbLabel(t) {
  if (!t) return ''
  const real = envStatusMap.value[t.type]?.name
  return real || t.label || t.type
}

const dialogNeedAuth = computed(() => {
  return ['mysql', 'pgsql'].includes(activeType.value)
})

const namePlaceholder = computed(() => {
  if (activeType.value === 'sqlite') return '如 app.db'
  if (activeType.value === 'redis') return '由系统自动分配 db 编号'
  return '如 myapp（字母/数字/下划线）'
})

function filtered(type) {
  const arr = Array.isArray(listMap.value[type]) ? listMap.value[type] : []
  const kw = keywordMap.value[type] || ''
  if (!kw) return arr
  return arr.filter((d) => d.name && d.name.includes(kw))
}

function formatBytes(v) {
  return _fmtBytes(v, { acceptString: true })
}

async function loadTypes() {
  try {
    const res = await request.get('/database/types')
    dbTypes.value = Array.isArray(res.data) ? res.data : []
  } catch (e) {
    dbTypes.value = [
      { type: 'mysql', label: 'MySQL' },
      { type: 'sqlserver', label: 'SQLServer' },
      { type: 'mongodb', label: 'MongoDB' },
      { type: 'redis', label: 'Redis' },
      { type: 'pgsql', label: 'PgSQL' },
      { type: 'sqlite', label: 'SQLite' }
    ]
  }
}

async function loadStatus(type) {
  try {
    const res = await request.get('/database/status', { params: { type } })
    const d = res.data || {}
    statusMap.value[type] = { available: !!d.available, message: d.message || '' }
  } catch (e) {
    statusMap.value[type] = { available: false, message: '检测失败' }
  }
}

async function loadList(type) {
  if (!statusMap.value[type]?.available) {
    listMap.value[type] = []
    return
  }
  try {
    const res = await request.get('/database/list', { params: { type } })
    listMap.value[type] = Array.isArray(res.data) ? res.data : []
  } catch (e) {
    listMap.value[type] = []
    ElMessage.error(e?.response?.data?.message || e.message || '加载失败')
  }
}

async function loadEnvStatus() {
  try {
    const res = await request.get('/apps/env-status')
    const data = res.data || {}
    envStatusMap.value = data
    // 初始化默认版本选择
    for (const key of Object.keys(data)) {
      if (data[key]?.select_version && !selectedVersionMap.value[key]) {
        selectedVersionMap.value[key] = data[key].version_default || data[key].versions?.[0] || ''
      }
    }
  } catch (e) {
    // 静默失败，使用单条状态接口兜底
  }
}

async function installEnv(type) {
  const meta = envStatusMap.value[type]
  if (!meta) return
  const key = meta.key
  if (!key) {
    ElMessage.warning('暂不支持一键安装该环境')
    return
  }
  installingMap.value[type] = true
  pendingActionMap.value[type] = 'install'
  try {
    const payload = { key }
    if (meta.select_version && selectedVersionMap.value[type]) {
      payload.version = selectedVersionMap.value[type]
    }
    await request.post('/apps/install', payload)
    // 写入全局安装队列，右下角浮窗立即显示（不再自动弹大弹窗，避免遮挡）
    const tracker = useInstallTrackerStore()
    tracker.upsert({ key, name: meta.name || type, action: 'install', status: 'queued', message: '排队等待中...' })
    ElMessage.success('安装已开始，请稍候...')
    pollInstallStatus(key, type)
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '安装请求失败')
    installingMap.value[type] = false
    pendingActionMap.value[type] = ''
  }
}

function pollInstallStatus(key, type) {
  if (installTimer.value) {
    clearInterval(installTimer.value)
  }
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
        await loadStatus(type)
        // 立即刷新服务状态，避免安装完成后停留在"状态未知"（需手动刷新才显示"运行中"）
        await loadServiceStatus(type)
        await loadList(type)
      } else if (action === 'uninstall' && app.status === 'not_installed') {
        clearInterval(installTimer.value)
        installTimer.value = null
        installingMap.value[type] = false
        pendingActionMap.value[type] = ''
        ElMessage.success('卸载完成')
        await loadEnvStatus()
        await loadStatus(type)
        await loadList(type)
      } else if (app.status === 'failed') {
        clearInterval(installTimer.value)
        installTimer.value = null
        installingMap.value[type] = false
        pendingActionMap.value[type] = ''
        ElMessage.error('操作失败，请查看日志')
      }
    } catch (e) {
      // 轮询失败继续
    }
  }, 3000)
}

// 进入页面时探测后端是否已存在进行中的安装/卸载，
// 避免刷新页面后丢失内存中的 installTracker 导致 service 不可用时直接调接口报错。
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

async function onTabChange(type) {
  activeType.value = type
  // 记忆当前 Tab，下次进入时默认定位
  try { localStorage.setItem('kypanel.database.lastTab', type) } catch (e) {}
  // 任何情况下先显示骨架屏，查询出状态后再根据状态切换
  loading.value = true
  try {
    await loadEnvStatus()
    // 先检查后端是否有进行中的安装/卸载，避免 service 不可用时调接口报错
    if (await checkPendingAction(type)) {
      return
    }
    await loadStatus(type)
    await loadServiceStatus(type)
    await loadList(type)
  } finally {
    loading.value = false
  }
}

async function load() {
  // 任何情况下先显示骨架屏，查询出状态后再根据状态切换
  loading.value = true
  try {
    await loadEnvStatus()
    // 先检查后端是否有进行中的安装/卸载
    if (await checkPendingAction(activeType.value)) {
      return
    }
    await loadStatus(activeType.value)
    await loadServiceStatus(activeType.value)
    await loadList(activeType.value)
  } catch (e) {
  } finally {
    loading.value = false
  }
}

function openCreate(type) {
  activeType.value = type
  form.value = { name: '', user: '', password: randomPassword(16), charset: 'utf8mb4' }
  createVisible.value = true
}

// MySQL 常用字符集
const mysqlCharsets = [
  'utf8mb4', 'utf8', 'gbk', 'gb2312', 'big5', 'latin1', 'ascii',
]

// n 位 [A-Za-z0-9] 随机密码（用于数据库访问密码）
function randomPassword(n = 16) {
  const cs = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789'
  let s = ''
  for (let i = 0; i < n; i++) s += cs[Math.floor(Math.random() * cs.length)]
  return s
}

function randomPasswordNow() {
  form.value.password = randomPassword(16)
}

async function create() {
  if (!form.value.name) {
    return ElMessage.warning('请填写数据库名')
  }
  if (dialogNeedAuth.value && !form.value.password) {
    return ElMessage.warning('请设置密码')
  }
  creating.value = true
  try {
    await request.post('/database/create?type=' + activeType.value, form.value)
    ElMessage.success('创建成功')
    createVisible.value = false
    await loadList(activeType.value)
  } finally {
    creating.value = false
  }
}

function remove(type, row) {
  const name = row.name || ''
  // 转义正则特殊字符，保证输入校验精确匹配库名
  const escaped = name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  // 危险操作：必须输入完整数据库名才能删除，防止误操作
  ElMessageBox.prompt(
    `删除数据库「${name}」后无法恢复，其中的数据将被永久清除！\n请输入数据库名「${name}」以确认删除：`,
    '删除数据库（危险操作）',
    {
      confirmButtonText: '确认删除',
      cancelButtonText: '取消',
      type: 'error',
      inputPlaceholder: `请输入 ${name}`,
      inputPattern: new RegExp('^' + escaped + '$'),
      inputErrorMessage: `输入内容与数据库名「${name}」不一致，无法删除`
    }
  ).then(async () => {
    await request.post('/database/delete?type=' + type, { name })
    ElMessage.success('删除成功')
    await loadList(type)
  }).catch(() => {})
}

// 管理：MySQL 跳转 phpMyAdmin 免密登录；其他类型显示连接信息
async function manageDb(row) {
  if (activeType.value !== 'mysql') {
    return openConn(row)
  }
  let status
  try {
    status = await request.get('/database/pma/status')
  } catch (e) {
    return ElMessage.error('获取 phpMyAdmin 状态失败')
  }
  if (status.data?.installed) {
    // 每次点击都签发最新的专用 PMA token（不泄露面板 JWT）
    let pmaToken = ''
    try {
      const r = await request.post('/database/pma/token')
      pmaToken = r.data?.token || ''
    } catch (e) {
      return ElMessage.error('获取 phpMyAdmin 访问令牌失败')
    }
    if (!pmaToken) return ElMessage.error('获取 phpMyAdmin 访问令牌失败')
    // 带上数据库名直接进入对应库，页面标题显示「数据库名 - phpMyAdmin」
    window.open('/phpmyadmin/index.php?route=/database/structure&token=' + encodeURIComponent(pmaToken) + '&db=' + encodeURIComponent(row.name), '_blank')
    return
  }
  ElMessageBox.confirm(
    'phpMyAdmin 未安装。安装后可通过「管理」直接免密登录数据库，是否立即安装？',
    '需要安装 phpMyAdmin',
    { confirmButtonText: '一键安装', cancelButtonText: '取消', type: 'warning' }
  ).then(async () => {
    await request.post('/apps/install', { key: 'phpmyadmin' })
    const tracker = useInstallTrackerStore()
    tracker.upsert({ key: 'phpmyadmin', name: 'phpMyAdmin', action: 'install', status: 'queued', message: '排队等待中...' })
    ElMessage.success('安装已开始，完成后刷新页面即可使用「管理」进入')
  }).catch(() => {})
}

// 管理：连接信息
function openConn(row) {
  currentRow.value = row
  connVisible.value = true
}

// 密码显隐/复制
function togglePwd(name) {
  visiblePwd.value[name] = !visiblePwd.value[name]
}
// 后端脱敏后的密码以 **** 开头，子账号不可见明文
function isMaskedPwd(p) {
  return typeof p === 'string' && p.startsWith('****')
}
async function copyPwd(pwd, label = '密码') {
  if (!pwd) return
  try {
    await navigator.clipboard.writeText(pwd)
    ElMessage.success(label + '已复制')
    return
  } catch (e) {}
  // 降级方案：通过临时 textarea 复制
  const textarea = document.createElement('textarea')
  textarea.value = pwd
  textarea.style.position = 'fixed'
  textarea.style.left = '-9999px'
  textarea.style.top = '-9999px'
  document.body.appendChild(textarea)
  textarea.focus()
  textarea.select()
  try {
    const ok = document.execCommand('copy')
    if (ok) {
      ElMessage.success(label + '已复制')
    } else {
      ElMessage.warning('复制失败，请手动复制')
    }
  } catch (err) {
    ElMessage.warning('复制失败，请手动复制')
  }
  document.body.removeChild(textarea)
}

// 设置权限
function openPerms(row) {
  currentRow.value = row
  const hosts = (row.hosts || 'localhost').split(',').map(x => x.trim()).filter(Boolean)
  if (hosts.includes('%')) {
    permMode.value = 'all'
  } else if (hosts.length === 1 && hosts[0] === 'localhost') {
    permMode.value = 'local'
  } else {
    permMode.value = 'ip'
  }
  permIps.value = hosts.filter(h => h !== '%' && h !== 'localhost').join('\n')
  permVisible.value = true
}

async function savePerms() {
  if (!currentRow.value) return
  let hosts = []
  if (permMode.value === 'all') {
    hosts = ['%']
  } else if (permMode.value === 'local') {
    hosts = ['localhost']
  } else {
    hosts = permIps.value.split('\n').map(x => x.trim()).filter(Boolean)
    if (hosts.length === 0) {
      return ElMessage.warning('请至少填写一个 IP')
    }
  }
  permSaving.value = true
  try {
    await request.post('/database/perms?type=' + activeType.value, {
      name: currentRow.value.name,
      hosts
    })
    ElMessage.success('权限设置成功')
    permVisible.value = false
    await loadList(activeType.value)
  } finally {
    permSaving.value = false
  }
}

// 修改密码
function openPwd(row) {
  currentRow.value = row
  newPassword.value = row.password || generateRandomPwd()
  pwdVisible.value = true
}

// 随机生成密码（大写+小写+数字，默认 16 位）
function generateRandomPwd(len = 16) {
  const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789'
  let pwd = ''
  for (let i = 0; i < len; i++) {
    pwd += chars.charAt(Math.floor(Math.random() * chars.length))
  }
  // 确保至少包含 1 位数字
  if (!/\d/.test(pwd)) {
    pwd = pwd.slice(0, -1) + '9'
  }
  return pwd
}

async function savePwd() {
  if (!currentRow.value || !newPassword.value) {
    return ElMessage.warning('请输入新密码')
  }
  pwdSaving.value = true
  try {
    await request.post('/database/password?type=' + activeType.value, {
      name: currentRow.value.name,
      password: newPassword.value
    })
    ElMessage.success('密码修改成功')
    pwdVisible.value = false
    await loadList(activeType.value)
  } finally {
    pwdSaving.value = false
  }
}

// 数据库备份
function openBackup(row) {
  backupRow.value = row
  backupVisible.value = true
  loadBackupList()
}
async function loadBackupList() {
  if (!backupRow.value) return
  backupLoading.value = true
  try {
    const res = await request.get('/database/backup/list', {
      params: { type: activeType.value, name: backupRow.value.name }
    })
    backupList.value = Array.isArray(res.data) ? res.data : []
  } catch (e) {
    backupList.value = []
  }
  backupLoading.value = false
}
function createBackupFromRow(row) {
  backupRow.value = row
  onCreateBackup()
}
async function onCreateBackup() {
  if (!backupRow.value) return
  backupLoading.value = true
  try {
    await request.post('/database/backup/create?type=' + activeType.value + '&name=' + encodeURIComponent(backupRow.value.name))
    ElMessage.success('备份成功')
    await loadBackupList()
    await loadList(activeType.value)
  } catch (e) {}
  backupLoading.value = false
}
async function restoreBackup(row) {
  if (!backupRow.value) return
  try {
    await ElMessageBox.confirm('确定要恢复该备份吗？当前数据库数据将被覆盖。', '恢复备份', { type: 'warning' })
  } catch (e) { return }
  backupLoading.value = true
  try {
    await request.post('/database/backup/restore?type=' + activeType.value + '&name=' + encodeURIComponent(backupRow.value.name) + '&file=' + encodeURIComponent(row.name))
    ElMessage.success('恢复成功')
  } catch (e) {}
  backupLoading.value = false
}
async function deleteBackup(row) {
  if (!backupRow.value) return
  try {
    await ElMessageBox.confirm('确定要删除该备份吗？删除后不可恢复。', '删除备份', { type: 'warning' })
  } catch (e) { return }
  backupLoading.value = true
  try {
    await request.post('/database/backup/delete?type=' + activeType.value + '&name=' + encodeURIComponent(backupRow.value.name) + '&file=' + encodeURIComponent(row.name))
    ElMessage.success('删除成功')
    await loadBackupList()
    await loadList(activeType.value)
  } catch (e) {}
  backupLoading.value = false
}
async function downloadBackup(row) {
  if (!backupRow.value) return
  try {
    const res = await request.get('/database/backup/download?type=' + activeType.value + '&name=' + encodeURIComponent(backupRow.value.name) + '&file=' + encodeURIComponent(row.name), {
      responseType: 'blob'
    })
    const blob = new Blob([res.data])
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = row.name
    document.body.appendChild(a)
    a.click()
    URL.revokeObjectURL(url)
    document.body.removeChild(a)
  } catch (e) {
    ElMessage.error('下载失败')
  }
}

// 导入数据库
function openImport(row) {
  importRow.value = row
  importMode.value = 'backup'
  selectedBackupFile.value = ''
  importUploadFileList.value = []
  uploadImportRef.value?.clearFiles?.()
  importVisible.value = true
  loadImportBackupList()
}
async function loadImportBackupList() {
  if (!importRow.value) return
  importBackupLoading.value = true
  try {
    const res = await request.get('/database/backup/list', {
      params: { type: activeType.value, name: importRow.value.name }
    })
    importBackupList.value = Array.isArray(res.data) ? res.data : []
  } catch (e) {
    importBackupList.value = []
  }
  importBackupLoading.value = false
}
async function confirmImport() {
  if (!importRow.value) return
  if (importMode.value === 'backup') {
    if (!selectedBackupFile.value) {
      ElMessage.warning('请选择要恢复的备份文件')
      return
    }
    importLoading.value = true
    try {
      await ElMessageBox.confirm('确定要恢复该备份吗？当前数据库数据将被覆盖。', '恢复备份', { type: 'warning' })
    } catch (e) {
      importLoading.value = false
      return
    }
    try {
      await request.post('/database/backup/restore?type=' + activeType.value + '&name=' + encodeURIComponent(importRow.value.name) + '&file=' + encodeURIComponent(selectedBackupFile.value))
      ElMessage.success('导入成功')
      importVisible.value = false
    } catch (e) {}
    importLoading.value = false
  } else {
    if (!uploadImportRef.value) {
      ElMessage.warning('请选择导入文件')
      return
    }
    importLoading.value = true
    uploadImportRef.value.submit()
  }
}
function beforeImportUpload(file) {
  const name = file.name.toLowerCase()
  const isAllowed = name.endsWith('.sql') || name.endsWith('.sql.gz') || name.endsWith('.db') || name.endsWith('.sqlite')
  if (!isAllowed) {
    ElMessage.error('仅支持 .sql、.sql.gz、.db、.sqlite 文件')
    return false
  }
  return true
}
function onImportFileChange(file, fileList) {
  importUploadFileList.value = fileList.slice(-1)
}
function onImportUploadSuccess() {
  importLoading.value = false
  importVisible.value = false
  ElMessage.success('导入成功')
  loadList(activeType.value)
}
function onImportUploadError(err) {
  importLoading.value = false
  let msg = '导入失败'
  try {
    const data = JSON.parse(err.message)
    msg = data.msg || data.message || msg
  } catch (e) {}
  ElMessage.error(msg)
}
function onImportClosed() {
  importRow.value = null
  selectedBackupFile.value = ''
  importUploadFileList.value = []
}

let outsideCommentHandler = null

function removeOutsideCommentListener() {
  if (outsideCommentHandler) {
    document.removeEventListener('mousedown', outsideCommentHandler, true)
    outsideCommentHandler = null
  }
}

// 行内修改备注
function startEdit(row) {
  if (commentBlurLock.value) return
  editingCommentId.value = row.name
  editingCommentText.value = row.comment || ''
  nextTick(() => {
    commentInputRef.value?.focus()
  })
  removeOutsideCommentListener()
  outsideCommentHandler = (e) => {
    const editEl = commentInputRef.value?.$el || commentInputRef.value
    if (editEl && editEl.contains(e.target)) return
    onCommentBlur(row)
  }
  document.addEventListener('mousedown', outsideCommentHandler, true)
}

function cancelCommentEdit(row) {
  if (editingCommentId.value !== row.name) return
  editingCommentId.value = ''
  removeOutsideCommentListener()
  commentBlurLock.value = true
  setTimeout(() => {
    commentBlurLock.value = false
  }, 150)
}

async function onCommentBlur(row) {
  if (editingCommentId.value !== row.name) return
  const newVal = editingCommentText.value.trim()
  const oldVal = (row.comment || '').trim()
  editingCommentId.value = ''
  removeOutsideCommentListener()
  // 短暂锁定，防止 blur 后同一 click 事件再次触发 startEdit
  commentBlurLock.value = true
  setTimeout(() => {
    commentBlurLock.value = false
  }, 150)
  if (newVal === oldVal) return
  try {
    await request.post('/database/comment?type=' + activeType.value, {
      name: row.name,
      comment: newVal
    })
    row.comment = newVal
    ElMessage.success('备注修改成功')
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '保存失败')
  }
}

// ============== 服务状态与启停控制 ==============
function serviceStatusText(type) {
  const s = serviceStatusMap.value[type]
  if (s === 'running') return '运行中'
  if (s === 'stopped') return '已停止'
  if (s === 'n/a') return ''
  return '状态未知'
}

async function loadServiceStatus(type) {
  if (!type || type === 'sqlite') return
  // 环境未安装时跳过服务状态查询（避免后端报"未找到 XX 服务单元"）
  if (!statusMap.value[type]?.available) {
    serviceStatusMap.value[type] = 'unknown'
    return
  }
  try {
    const res = await request.get('/database/service-status', { params: { type } })
    serviceStatusMap.value[type] = res.data?.status || 'unknown'
  } catch (e) {
    serviceStatusMap.value[type] = 'unknown'
  }
}

async function handleServiceAction(type, action) {
  serviceLoadingMap.value[type] = action
  try {
    await request.post('/database/service-action?type=' + type, { action })
    ElMessage.success('服务' + ({ start: '已启动', stop: '已停止', restart: '已重启' }[action] || '已操作'))
    setTimeout(() => loadServiceStatus(type), 1200)
    setTimeout(() => loadServiceStatus(type), 3000)
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '操作失败')
  } finally {
    serviceLoadingMap.value[type] = ''
  }
}

function startServiceStatusPolling() {
  stopServiceStatusPolling()
  serviceStatusTimer = setInterval(() => {
    const t = activeType.value
    if (t && t !== 'sqlite' && statusMap.value[t]?.available) {
      loadServiceStatus(t)
    }
  }, 8000)
}

function stopServiceStatusPolling() {
  if (serviceStatusTimer) {
    clearInterval(serviceStatusTimer)
    serviceStatusTimer = null
  }
}

// ============== root 密码管理 ==============
async function openRootPassword(type) {
  rootPwdType.value = type
  newRootPwd.value = ''
  showRootPwd.value = true
  rootPwdDialogVisible.value = true
  try {
    const res = await request.get('/database/root-info', { params: { type } })
    rootInfoMap.value[type] = res.data || {}
  } catch (e) {
    rootInfoMap.value[type] = {
      user: '-',
      host: '-',
      password: '',
      support_change: false,
      hint: e?.response?.data?.message || '获取 root 账户信息失败',
    }
  }
}

async function changeRootPassword() {
  if (!newRootPwd.value) return ElMessage.warning('请输入新密码或点击随机生成')
  if (newRootPwd.value.length < 6) return ElMessage.warning('新密码至少 6 位')
  savingRootPwd.value = true
  try {
    await request.post('/database/root-password?type=' + rootPwdType.value, { password: newRootPwd.value })
    ElMessage.success('密码修改成功')
    if (rootInfoMap.value[rootPwdType.value]) {
      rootInfoMap.value[rootPwdType.value].password = newRootPwd.value
    }
    newRootPwd.value = ''
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '修改失败')
  } finally {
    savingRootPwd.value = false
  }
}

function copyText(text) {
  if (!text) return
  if (navigator.clipboard?.writeText) {
    navigator.clipboard.writeText(text)
      .then(() => ElMessage.success('已复制'))
      .catch(() => ElMessage.error('复制失败'))
  } else {
    const ta = document.createElement('textarea')
    ta.value = text
    document.body.appendChild(ta)
    ta.select()
    try { document.execCommand('copy'); ElMessage.success('已复制') } catch (e) { ElMessage.error('复制失败') }
    document.body.removeChild(ta)
  }
}

onMounted(async () => {
  await loadTypes()
  if (dbTypes.value.length) {
    // 优先恢复上次停留的 Tab；若已不存在则回退到第一个
    let restore = ''
    try { restore = localStorage.getItem('kypanel.database.lastTab') || '' } catch (e) {}
    const hit = dbTypes.value.find((d) => d.type === restore)
    activeType.value = hit ? hit.type : dbTypes.value[0].type
  }
  // 预加载当前 tab
  await onTabChange(activeType.value)
  // 启动服务状态轮询
  startServiceStatusPolling()
})

onUnmounted(() => {
  removeOutsideCommentListener()
  if (installTimer.value) {
    clearInterval(installTimer.value)
    installTimer.value = null
  }
  stopServiceStatusPolling()
})
</script>

<style scoped>
.database-page { /* 间距由 AdminLayout.lp-main 统一控制（16px），<600px 时为 0 */ }
.tabs-card :deep(.el-card__body) { padding: 0; }

/* 数据库服务状态指示（圆点 + 文字） */
.service-status {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 0 8px;
  height: 28px;
  border-radius: 4px;
  cursor: pointer;
  user-select: none;
  transition: background 0.15s;
  outline: none;
  margin-left: 4px;
  border-left: 1px solid #ebeef5;
  height: 32px;
}
.status-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-right: 6px;
  vertical-align: middle;
}
.status-dot.running { background: #67c23a; box-shadow: 0 0 6px rgba(103, 194, 58, 0.6); }
.status-dot.stopped { background: #f56c6c; }
.status-dot.unknown { background: #c0c4cc; }
.status-text { color: #606266; font-size: 13px; }
.service-status:hover { background: #f5f7fa; }
.service-status .caret { color: #909399; }
.service-status.is-loading { cursor: wait; opacity: 0.6; }
.service-status.is-unknown { cursor: not-allowed; }
.pwd-eye-icon,
.pwd-refresh-icon { cursor: pointer; color: #909399; transition: color 0.15s; }
.pwd-eye-icon:hover,
.pwd-refresh-icon:hover { color: #409eff; }
.pwd-eye-icon { margin-right: 6px; }
.service-status.is-unknown .caret { display: none; }

/* root 密码弹窗 */
.root-pwd-content .muted { color: #909399; }
.root-pwd-content .pwd-row {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.root-pwd-content .pwd-text {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 13px;
  color: #303133;
  background: #f5f7fa;
  padding: 4px 10px;
  border-radius: 4px;
  margin-right: 6px;
  user-select: all;
  min-width: 120px;
  display: inline-block;
}
.env-missing {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 320px;
  background: #fafafa;
  border: 1px dashed #dcdfe6;
  border-radius: 8px;
  margin-top: 12px;
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
.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
  flex-wrap: wrap;
  gap: 12px;
}
.toolbar-left, .toolbar-right { display: flex; align-items: center; gap: 8px; }
.password-cell {
  display: flex;
  align-items: center;
  gap: 0;
}
.password-cell :deep(.el-button) {
  padding: 0 2px;
  margin: 0;
}
.password-cell :deep(.el-button + .el-button) {
  margin-left: 0;
}
.pwd-mask {
  font-family: monospace;
  min-width: 70px;
  display: inline-block;
}
.comment-cell {
  display: flex;
  align-items: center;
  min-height: 28px;
  cursor: text;
}
.comment-text {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  text-overflow: ellipsis;
  line-height: 1.4;
  white-space: pre-wrap;
  word-break: break-all;
}
.comment-text.no-comment {
  color: #999;
}
.comment-edit :deep(.el-textarea__inner) {
  padding: 2px 6px;
  resize: none;
}
/* 操作列按钮已统一走全局 .ops-cell（styles/index.css），此处旧样式已移除 */
.pwd-info-row {
  display: flex;
  align-items: center;
  gap: 24px;
  margin-bottom: 18px;
  margin-left: 90px; /* 与 el-form label-width 对齐 */
  line-height: 32px;
  font-size: 14px;
}
.pwd-info-row label {
  color: #606266;
  margin-right: 8px;
}
.backup-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}
.backup-db-name {
  color: #606266;
  font-size: 14px;
}
.copy-btn {
  padding: 0 4px;
  margin-left: 2px;
}
.backup-cell {
  display: inline-flex;
  align-items: center;
  gap: 2px;
}
.backup-cell :deep(.el-button) {
  padding: 0 4px;
  margin: 0;
}
.backup-cell :deep(.el-button + .el-button) {
  margin-left: 0;
}
.import-db-name {
  color: #606266;
  font-size: 14px;
  margin-bottom: 16px;
}
.import-loading {
  color: #909399;
  font-size: 14px;
  text-align: center;
  padding: 20px 0;
}
</style>
