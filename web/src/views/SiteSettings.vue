<template>
  <el-drawer v-model="visible" :title="`网站设置 - ${site.name || ''}`" size="780px" destroy-on-close>
    <el-tabs v-model="activeTab">
      <!-- 域名（拖拽排序、批量添加） -->
      <el-tab-pane label="域名" name="domains">
        <div class="domain-tab">
          <div class="domain-list-wrap">
            <div class="domain-list-header">
              <span class="drag-handle-placeholder"></span>
              <span class="domain-name-cell header">域名</span>
              <span class="domain-port header">端口</span>
              <span class="domain-actions-header">操作</span>
            </div>
            <draggable v-model="form.domains" item-key="(d) => d" animation="150" ghost-class="domain-ghost" @update="onDomainListChange">
              <template #item="{ element: d, index }">
                <div class="domain-row">
                  <div class="domain-col-handle">
                    <el-icon class="drag-handle" :size="14"><Rank /></el-icon>
                  </div>
                  <div class="domain-col-name">
                    <el-input
                      v-if="editingDomainIdx === index"
                      ref="domainInputRefs"
                      v-model="form.domains[index]"
                      size="small"
                      @blur="saveDomainRowEdit(index)"
                      @keyup.enter="saveDomainRowEdit(index)"
                      @input="form.domains[index] = normalizeColon(form.domains[index])"
                      @mousedown.stop
                    />
                    <a v-else :href="domainVisitUrl(d)" target="_blank" class="domain-text" @click.stop>{{ domainHostLabel(d) }}</a>
                  </div>
                  <div class="domain-col-port">
                    <span class="domain-port">{{ domainPortLabel(d) }}</span>
                  </div>
                  <div class="domain-col-actions">
                    <el-button link type="primary" size="small" @click="startEditDomain(index)" @mousedown.stop>编辑</el-button>
                    <el-button link type="danger" size="small" @click="deleteDomainRow(index)" @mousedown.stop>删除</el-button>
                  </div>
                </div>
              </template>
            </draggable>
            <div v-if="!form.domains.length" class="domain-empty">暂无绑定域名，请下方添加</div>
          </div>

          <el-divider style="margin: 16px 0 12px">批量添加</el-divider>
          <div class="batch-add">
            <el-input
              v-model="batchDomainInput"
              type="textarea"
              :rows="4"
              placeholder="每行一个，例：&#10;www.example.com&#10;api.example.com&#10;127.0.0.1:8899&#10;*.example.com"
              @input="batchDomainInput = normalizeColon(batchDomainInput)"
            />
            <div style="margin-top: 12px;">
              <el-button type="primary" size="default" :disabled="!batchPreviewValidCount" @click="addBatchDomains">全部加入（{{ batchPreviewValidCount }} 条）</el-button>
            </div>
          </div>
        </div>
      </el-tab-pane>

      <!-- 基础设置 -->
      <el-tab-pane label="基础设置" name="base">
        <el-form label-width="110px" class="setting-form">
          <el-form-item label="网站名称">
            <el-input v-model="form.name" maxlength="255" show-word-limit placeholder="网站名称（可随意填写，支持汉字等任意字符）" />
          </el-form-item>
          <el-form-item :label="site.type === 'static' || site.type === 'php' ? '网站目录' : '项目路径'">
            <div style="display: flex; gap: 8px;">
              <el-input v-model="form.root" :disabled="site.type === 'proxy'" />
              <el-button :disabled="site.type === 'proxy'" @click="openDirPicker">
                <el-icon><FolderOpened /></el-icon>
              </el-button>
            </div>
            <span class="tip">留空使用 /www/wwwroot/站点名</span>
          </el-form-item>

          <template v-if="site.type === 'static' || site.type === 'php'">
            <el-form-item label="运行目录">
              <el-select
                v-model="form.runtime_dir"
                placeholder="选择或输入运行目录"
                filterable
                allow-create
                default-first-option
                clearable
                style="width: 100%"
              >
                <el-option
                  v-for="item in runtimeDirOptions"
                  :key="item.value"
                  :label="item.label"
                  :value="item.value"
                />
              </el-select>
              <span class="tip">部分程序需要指定二级目录作为运行目录，如 ThinkPHP、Laravel</span>
            </el-form-item>

            <el-form-item label="默认文档">
              <div class="tag-editor" ref="indexTagEditor">
                <el-tag
                  v-for="(item, idx) in defaultIndexes"
                  :key="item"
                  closable
                  draggable="true"
                  @close="removeIndexItem(item)"
                  @dragstart="handleDragStart(idx)"
                  @dragover.prevent
                  @drop="handleDrop(idx)"
                  class="drag-tag"
                >{{ item }}</el-tag>
                <el-input
                  v-if="addingIndex"
                  ref="indexInputRef"
                  v-model="indexInput"
                  size="small"
                  style="width: 160px"
                  placeholder="如 index.php"
                  @keyup.enter="addIndexItem"
                  @blur="addIndexItem"
                />
                <el-button v-else size="small" @click="showIndexInput">+ 添加默认文档</el-button>
              </div>
              <span class="tip">按回车添加文档，拖拽标签可调整优先级</span>
            </el-form-item>
          </template>

          <template v-if="site.type === 'php'">
            <el-form-item label="PHP 版本">
              <el-select v-model="form.php_fpm" style="width: 100%">
                <el-option v-for="o in phpFpms" :key="o.value" :label="o.label" :value="o.value" />
              </el-select>
              <span class="tip">切换后自动更新 PHP-FPM 连接目标</span>
            </el-form-item>
          </template>

          <template v-if="site.type === 'node' || site.type === 'python' || site.type === 'go'">
            <el-form-item label="运行版本">
              <el-select v-model="form.runtime_version" style="width: 100%" placeholder="选择运行版本">
                <el-option v-for="o in runtimeEnvVersions[site.type] || []" :key="o.value" :label="o.label" :value="o.value" />
                <template v-if="!(runtimeEnvVersions[site.type] || []).length">
                  <el-option label="未检测到已安装的版本，请先在应用商店安装" value="" disabled />
                </template>
              </el-select>
              <!-- 存量站点迁移：站点当前运行版本未安装 → 警告并引导重新选择 -->
              <span v-if="runtimeVersionMissing" class="tip warn" style="display: block; margin-top: 4px;">
                当前运行版本「{{ form.runtime_version }}」未检测到已安装，网站可能无法启动。
                请在应用商店安装该版本，或在此重新选择已安装的版本后保存。
              </span>
            </el-form-item>
            <el-form-item label="项目端口">
              <el-input-number v-model="form.proxy_port" :min="1" :max="65535" />
              <span class="tip">反代到 127.0.0.1:{{ form.proxy_port }}</span>
            </el-form-item>
            <el-form-item label="启动命令">
              <el-input v-model="form.start_command" placeholder="如 python app.py / npm run start" />
            </el-form-item>
            <el-form-item label="环境变量">
              <el-input v-model="form.env_vars" type="textarea" :rows="2" placeholder="KEY=VALUE，每行一个" />
            </el-form-item>
          </template>

          <template v-if="site.type === 'proxy'">
            <el-form-item label="代理目标">
              <el-input v-model="form.proxy_pass" placeholder="如 http://127.0.0.1:3000" />
            </el-form-item>
          </template>

          <el-form-item label="流量限制">
            <el-input-number v-model="form.rate_limit_kbs" :min="0" :max="1000000" />
            <span class="tip">KB/s，0 表示不限速</span>
          </el-form-item>

          <el-form-item>
            <el-button type="primary" :loading="saving" @click="saveSettings">保存设置</el-button>
          </el-form-item>
        </el-form>
      </el-tab-pane>

      <!-- 配置文件 -->
      <el-tab-pane label="配置文件" name="config" lazy>
        <el-alert
          type="warning"
          :closable="false"
          show-icon
          :title="`配置文件为完整 ${webServerLabel} 配置，保存后将进入自定义模式；此后修改其他设置会重置为自动生成配置。`"
          style="margin-bottom: 12px"
        />
        <CodeEditor
          v-model="configContent"
          :placeholder="loadedConfig ? '' : '加载中...'"
        />
        <div class="action-bar">
          <span class="config-hint">{{ webServerLabel }} 语法高亮 · 当前 {{ configContent.split('\n').length }} 行</span>
          <el-button @click="loadConfig">刷新</el-button>
          <el-button type="primary" :loading="savingConfig" @click="saveConfig">保存配置</el-button>
        </div>
      </el-tab-pane>

      <!-- 反向代理（proxy 类型） -->
      <el-tab-pane v-if="site.type === 'proxy'" label="反向代理" name="proxy">
        <el-form label-width="110px" class="setting-form">
          <el-form-item label="代理目标">
            <el-input v-model="form.proxy_pass" placeholder="如 http://127.0.0.1:3000 或 https://api.example.com" />
          </el-form-item>
          <el-form-item>
            <el-button type="primary" :loading="saving" @click="saveSettings">保存</el-button>
          </el-form-item>
        </el-form>
      </el-tab-pane>

      <!-- 重定向 -->
      <el-tab-pane label="重定向" name="redirect">
        <el-alert
          type="info"
          :closable="false"
          show-icon
          title="一个站点可配置多条重定向规则；按域名匹配整站域名，按目录匹配指定路径。"
          style="margin-bottom: 12px"
        />
        <div class="redirect-rules">
          <div v-if="redirectRules.length === 0" class="redirect-empty">
            暂无重定向规则，点击下方「添加规则」按钮创建。
          </div>
          <div v-for="(rule, idx) in redirectRules" :key="idx" class="redirect-rule">
            <div class="redirect-rule-head">
              <span class="redirect-rule-title">规则 {{ idx + 1 }}</span>
              <el-button size="small" type="danger" link @click="removeRedirectRule(idx)">删除</el-button>
            </div>

            <el-form label-width="90px" class="setting-form">
              <el-form-item label="匹配方式">
                <el-radio-group v-model="rule.match_type">
                  <el-radio-button value="domain">按域名</el-radio-button>
                  <el-radio-button value="dir">按目录</el-radio-button>
                </el-radio-group>
              </el-form-item>

              <el-form-item v-if="rule.match_type === 'domain'" label="匹配域名">
                <el-select v-model="rule.domains" multiple placeholder="选择域名（可多选）" style="width: 100%">
                  <el-option v-for="d in siteDomains" :key="d" :label="d" :value="d" />
                </el-select>
                <span class="tip">可选域名来自本站绑定的主域名与附加域名</span>
              </el-form-item>

              <el-form-item v-else label="匹配目录">
                <el-select v-model="rule.paths" multiple placeholder="选择目录（可多选）" style="width: 100%" :loading="loadingDirs">
                  <el-option v-for="d in siteDirs" :key="d.value" :label="d.value" :value="d.value" />
                </el-select>
                <span class="tip">可多选网站根目录下的文件夹</span>
              </el-form-item>

              <el-form-item label="目标 URL">
                <el-input v-model="rule.target_url" placeholder="如 https://new.example.com" clearable />
                <span class="tip">必须以 http:// 或 https:// 开头</span>
              </el-form-item>

              <el-form-item label="状态码">
                <el-radio-group v-model="rule.code">
                  <el-radio-button :value="301">301 永久</el-radio-button>
                  <el-radio-button :value="302">302 临时</el-radio-button>
                  <el-radio-button :value="307">307 临时保持方法</el-radio-button>
                  <el-radio-button :value="308">308 永久保持方法</el-radio-button>
                </el-radio-group>
              </el-form-item>

              <el-form-item label="保留路径">
                <el-switch v-model="rule.keep_path" active-text="开启" inactive-text="关闭" />
                <span class="tip">开启后访问 /a/b 跳到 目标URL/a/b；关闭则统一跳到目标 URL 根路径</span>
              </el-form-item>
            </el-form>
          </div>

          <el-button style="width: 100%" @click="addRedirectRule">+ 添加规则</el-button>
        </div>

        <div class="action-bar">
          <el-button type="primary" :loading="saving" @click="saveSettings">保存重定向</el-button>
        </div>
      </el-tab-pane>

      <el-tab-pane v-if="site.type === 'php' || site.type === 'static'" label="伪静态" name="rewrite">
        <el-alert
          type="info"
          :closable="false"
          show-icon
          title="伪静态规则将插入 location / 内，示例：if (!-e $request_filename) { rewrite ^(.*)$ /index.php?s=$1 last; }"
          style="margin-bottom: 12px"
        />
        <el-form-item label="常用规则" class="rewrite-preset">
          <el-select v-model="selectedPreset" clearable placeholder="选择常用 PHP 框架规则" style="width: 100%">
            <el-option
              v-for="p in rewritePresets"
              :key="p.key"
              :label="p.label"
              :value="p.key"
            />
          </el-select>
        </el-form-item>
        <CodeEditor v-model="form.rewrite" placeholder="# 填写 rewrite 规则，或从上方选择常用框架规则" />
        <div class="action-bar">
          <el-button type="primary" :loading="saving" @click="saveSettings">保存伪静态</el-button>
        </div>
      </el-tab-pane>

      <!-- 防盗链 -->
      <el-tab-pane v-if="site.type === 'static' || site.type === 'php' || site.type === 'proxy'" label="防盗链" name="hotlink">
        <el-form label-width="110px" class="setting-form">
          <el-form-item label="防盗链">
            <el-switch v-model="form.hotlink_enabled" active-text="开启" inactive-text="关闭" />
          </el-form-item>
          <template v-if="form.hotlink_enabled">
            <el-form-item label="允许域名">
              <div class="tag-editor">
                <el-tag v-for="d in form.hotlink_referers" :key="d" closable @close="removeTag(form.hotlink_referers, d)">{{ d }}</el-tag>
                <el-input
                  v-if="addingRef"
                  v-model="refInput"
                  size="small"
                  style="width: 180px"
                  @keyup.enter="addTag(form.hotlink_referers, refInput, () => refInput = '')"
                  @blur="addTag(form.hotlink_referers, refInput, () => refInput = '')"
                />
                <el-button v-else size="small" @click="addingRef = true">+ 添加域名</el-button>
              </div>
              <span class="tip">白名单域名（不含协议），如 example.com，支持 *.example.com</span>
            </el-form-item>
          </template>
          <template v-if="site.type === 'static' || site.type === 'php'">
            <el-form-item label="静态缓存">
              <el-switch v-model="form.cache_enabled" active-text="开启" inactive-text="关闭" />
              <span class="tip">图片/字体缓存 30 天，JS/CSS 缓存 12 小时</span>
            </el-form-item>
          </template>
          <el-form-item label="安全响应头">
            <el-switch v-model="form.security_headers" active-text="开启" inactive-text="关闭" />
            <span class="tip">X-Frame-Options 防点击劫持 / nosniff 防 MIME 嗅探 / XSS 防护 / Referrer-Policy</span>
          </el-form-item>
          <el-form-item>
            <el-button type="primary" :loading="saving" @click="saveSettings">保存防盗链</el-button>
          </el-form-item>
        </el-form>
      </el-tab-pane>
    </el-tabs>
  </el-drawer>

  <!-- 目录选择器 -->
  <el-dialog
    v-model="dirPickerVisible"
    title="选择目录"
    width="560px"
    :close-on-click-modal="false"
  >
    <div v-loading="dirPickerLoading" class="dir-picker">
      <div class="dir-picker-header">
        <el-button size="small" :disabled="dirPickerPath === '/'" @click="parentDirPicker">
          <el-icon><ArrowUp /></el-icon> 上级目录
        </el-button>
        <el-input v-model="dirPickerPath" size="small" readonly />
      </div>
      <div class="dir-picker-list">
        <div
          v-for="d in dirPickerDirs"
          :key="d.path"
          :class="['dir-picker-item', { 'is-active': dirPickerPath === d.path }]"
          @dblclick="enterDirPicker(d.path)"
          @click="dirPickerPath = d.path"
        >
          <el-icon><Folder /></el-icon>
          <span class="dir-picker-name">{{ d.name }}</span>
        </div>
        <div v-if="!dirPickerDirs.length" class="dir-picker-empty">暂无子目录</div>
      </div>
    </div>
    <template #footer>
      <el-button @click="dirPickerVisible = false">取消</el-button>
      <el-button type="primary" @click="confirmDirPicker">选择当前目录</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, reactive, computed, watch, nextTick } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import request from '../utils/request'
import { FolderOpened, ArrowUp, Folder, Rank } from '@element-plus/icons-vue'
import draggable from 'vuedraggable'
import CodeEditor from '../components/CodeEditor.vue'

const visible = ref(false)
const site = ref({})
const emit = defineEmits(['success'])
const activeTab = ref('domains')
const saving = ref(false)
const savingConfig = ref(false)
const phpFpms = ref([])
const runtimeDirOptions = ref([{ label: '使用网站目录', value: '' }])
// 运行时多版本（node / python / go）已安装版本列表，供「运行版本」下拉选择。
// 用于存量站点迁移：升级后站点 runtime_version 可能对应当前未安装的版本，
// 此时展示警告并允许重新选择已安装版本。
const runtimeEnvVersions = ref({}) // { python: [{value:'Python 3.13', label:'Python 3.13'}], node: [...], go: [...] }
const runtimeVersionMissing = computed(() => {
  const t = site.value.type
  if (!['python', 'node', 'go'].includes(t)) return false
  if (!form.runtime_version) return false
  return !(runtimeEnvVersions.value[t] || []).some(o => o.value === form.runtime_version)
})

// 目录选择器
const dirPickerVisible = ref(false)
const dirPickerLoading = ref(false)
const dirPickerPath = ref('/')
const dirPickerDirs = ref([])

const form = reactive({
  name: '',
  remark: '',
  domains: [], // 合并的域名列表（第一个为主域名），支持域名/IP/IP:端口/域名:端口
  root: '',
  runtime_dir: '',
  default_index: '',
  rate_limit_kbs: 0,
  rewrite: '',
  hotlink_enabled: false,
  security_headers: true,
  hotlink_referers: [],
  cache_enabled: false,
  redirect_url: '',
  redirect_code: 301,
  redirect_keep_path: true,
  php_fpm: '',
  proxy_pass: '',
  start_command: '',
  env_vars: '',
  proxy_port: 3000,
  runtime_version: ''
})

// 重定向规则列表（多条）
const redirectRules = ref([])
const siteDirs = ref([])
const loadingDirs = ref(false)

// 站点绑定域名（主域名 + 附加域名），用于「按域名」多选（重定向/防盗链）
const siteDomains = computed(() => mergeDomainBindings(site.value.domain, site.value.domains))

const configContent = ref('')
const loadedConfig = ref(false)
const batchDomainInput = ref('')
const editingDomainIdx = ref(-1) // -1 表示无编辑；>=0 表示正在编辑该行
const domainInputRefs = ref([])
const addingRef = ref(false)
const refInput = ref('')
const addingIndex = ref(false)
const indexInput = ref('')
const indexInputRef = ref()
const defaultIndexes = ref([])
const dragIndex = ref(-1)
const selectedPreset = ref('')

const rewritePresets = [
  {
    key: 'thinkphp',
    label: 'ThinkPHP',
    nginx: `location ~* (runtime|application)/{
	return 403;
}
location / {
	if (!-e $request_filename){
		rewrite  ^(.*)$  /index.php?s=$1  last;   break;
	}
}`,
    apache: `<IfModule mod_rewrite.c>
  RewriteEngine On
  RewriteCond %{REQUEST_FILENAME} !-d
  RewriteCond %{REQUEST_FILENAME} !-f
  RewriteRule ^(.*)$ index.php/$1 [QSA,PT,L]
</IfModule>`
  },
  {
    key: 'laravel',
    label: 'Laravel',
    nginx: `try_files $uri $uri/ /index.php?$query_string;`,
    apache: `<IfModule mod_rewrite.c>
  RewriteEngine On
  RewriteCond %{REQUEST_FILENAME} !-d
  RewriteCond %{REQUEST_FILENAME} !-f
  RewriteRule ^(.*)$ index.php [QSA,L]
</IfModule>`
  },
  {
    key: 'wordpress',
    label: 'WordPress',
    nginx: `try_files $uri $uri/ /index.php?$query_string;`,
    apache: `<IfModule mod_rewrite.c>
  RewriteEngine On
  RewriteCond %{REQUEST_FILENAME} !-f
  RewriteCond %{REQUEST_FILENAME} !-d
  RewriteRule . /index.php [L]
</IfModule>`
  },
  {
    key: 'yii2',
    label: 'Yii2',
    nginx: `try_files $uri $uri/ /index.php?$query_string;`,
    apache: `<IfModule mod_rewrite.c>
  RewriteEngine On
  RewriteCond %{REQUEST_FILENAME} !-d
  RewriteCond %{REQUEST_FILENAME} !-f
  RewriteRule ^(.*)$ index.php/$1 [QSA,PT,L]
</IfModule>`
  },
  {
    key: 'codeigniter',
    label: 'CodeIgniter',
    nginx: `try_files $uri $uri/ /index.php?$uri&$query_string;`,
    apache: `<IfModule mod_rewrite.c>
  RewriteEngine On
  RewriteCond %{REQUEST_FILENAME} !-d
  RewriteCond %{REQUEST_FILENAME} !-f
  RewriteRule ^(.*)$ index.php/$1 [QSA,PT,L]
</IfModule>`
  },
  {
    key: 'symfony',
    label: 'Symfony',
    nginx: `try_files $uri $uri/ /index.php?$query_string;`,
    apache: `<IfModule mod_rewrite.c>
  RewriteEngine On
  RewriteCond %{REQUEST_FILENAME} !-f
  RewriteCond %{REQUEST_FILENAME} !-d
  RewriteRule ^(.*)$ index.php [QSA,L]
</IfModule>`
  }
]

const defaultIndexText = computed({
  get: () => defaultIndexes.value.join(' '),
  set: (v) => {
    defaultIndexes.value = (v || '').split(/\s+/).map(x => x.trim()).filter(Boolean)
  }
})

function open(id, tab = null) {
  site.value = { id }
  visible.value = true
  if (tab) activeTab.value = tab
  loadDetail(id).then(() => {
    loadRuntimeDirOptions()
    loadSiteDirs()
  })
  loadPhpFpms()
  loadRuntimeVersions()
}

function loadDetail(id) {
  return request.get('/site/detail', { params: { id } }).then((res) => {
    const d = res.data
    site.value = d
    Object.assign(form, {
      name: d.name || '',
      remark: d.remark || '',
      // 合并主域名 + 附加域名到一个数组（第一个为主域名）
      domains: mergeDomainBindings(d.domain, d.domains),
      root: d.root || '',
      runtime_dir: d.runtime_dir || '',
      default_index: d.default_index || '',
      rate_limit_kbs: d.rate_limit_kbs || 0,
      rewrite: d.rewrite || '',
      hotlink_enabled: !!d.hotlink_enabled,
      hotlink_referers: splitCSV(d.hotlink_referers),
      cache_enabled: !!d.cache_enabled,
      security_headers: d.security_headers !== false,
      redirect_url: d.redirect_url || '',
      redirect_code: d.redirect_code || 301,
      redirect_keep_path: d.redirect_keep_path !== false,
      php_fpm: d.php_fpm || '',
      proxy_pass: d.proxy_pass || '',
      start_command: d.start_command || '',
      env_vars: d.env_vars || '',
      proxy_port: d.proxy_port || 3000,
      runtime_version: d.runtime_version || ''
    })
    let indexes = (d.default_index || '').split(/\s+/).map(x => x.trim()).filter(Boolean)
    // PHP / 静态站点若服务端未返回默认文档，填充系统默认值
    if (indexes.length === 0 && (d.type === 'php' || d.type === 'static')) {
      indexes = d.type === 'php'
        ? ['index.php', 'index.html', 'index.htm', 'default.php', 'default.htm', 'default.html']
        : ['index.html', 'index.htm', 'default.html', 'default.htm']
    }
    defaultIndexes.value = indexes

    // 重定向规则列表
    const rules = (d.redirects || []).map((r) => ({
      id: r.id,
      match_type: r.match_type || 'domain',
      domains: splitCSV(r.domains),
      paths: splitCSV(r.paths),
      target_url: r.target_url || '',
      code: r.code || 301,
      keep_path: r.keep_path !== false
    }))
    redirectRules.value = rules
  })
}

function splitCSV(s) {
  if (!s) return []
  return s.split(',').map(x => x.trim()).filter(Boolean)
}

// ---------- 重定向规则 ----------
function emptyRedirectRule() {
  return {
    match_type: 'domain',
    domains: [],
    paths: [],
    target_url: '',
    code: 301,
    keep_path: true
  }
}

function addRedirectRule() {
  redirectRules.value.push(emptyRedirectRule())
}

function removeRedirectRule(idx) {
  redirectRules.value.splice(idx, 1)
}

function loadSiteDirs() {
  if (!site.value.id) return
  loadingDirs.value = true
  request.get('/site/redirect/dirs', { params: { id: site.value.id } }).then((res) => {
    siteDirs.value = res.data || []
  }).catch(() => {
    siteDirs.value = []
  }).finally(() => {
    loadingDirs.value = false
  })
}

function loadPhpFpms() {
  request.get('/site/php-fpms').then((res) => {
    phpFpms.value = res.data || []
    deriveRuntimeVersion()
  }).catch(() => {})
}

// 加载已安装的运行时多版本（node / python / go），用于「运行版本」下拉
function loadRuntimeVersions() {
  request.get('/apps/env-status').then((res) => {
    const data = res.data || {}
    const map = { python: [], node: [], go: [] }
    for (const [key, meta] of Object.entries(data)) {
      if (!meta || !meta.installed) continue
      let t = ''
      if (key.startsWith('python')) t = 'python'
      else if (key.startsWith('node')) t = 'node'
      else if (key.startsWith('go')) t = 'go'
      else continue
      const label = meta.name || key
      map[t].push({ value: label, label })
    }
    runtimeEnvVersions.value = map
  }).catch(() => { /* 静默：版本下拉为空时仅影响编辑时的可选性 */ })
}

function loadRuntimeDirOptions() {
  if (!site.value.id || (site.value.type !== 'static' && site.value.type !== 'php')) {
    runtimeDirOptions.value = [{ label: '使用网站目录', value: '' }]
    return
  }
  request.get('/site/subdirs', { params: { id: site.value.id } }).then((res) => {
    runtimeDirOptions.value = res.data || [{ label: '使用网站目录', value: '' }]
  }).catch(() => {
    runtimeDirOptions.value = [{ label: '使用网站目录', value: '' }]
  })
}

function openDirPicker() {
  dirPickerPath.value = form.root || '/www/wwwroot'
  dirPickerVisible.value = true
  loadDirTree(dirPickerPath.value)
}

function loadDirTree(path) {
  dirPickerLoading.value = true
  request.get('/site/dir-tree', { params: { path } }).then((res) => {
    const d = res.data || {}
    dirPickerPath.value = d.current || path
    dirPickerDirs.value = d.dirs || []
  }).catch((e) => {
    ElMessage.error(e?.response?.data?.msg || '读取目录失败')
  }).finally(() => {
    dirPickerLoading.value = false
  })
}

function enterDirPicker(path) {
  loadDirTree(path)
}

function parentDirPicker() {
  request.get('/site/dir-tree', { params: { path: dirPickerPath.value } }).then((res) => {
    const parent = res.data?.parent
    if (parent) loadDirTree(parent)
  }).catch(() => {})
}

function confirmDirPicker() {
  form.root = dirPickerPath.value
  dirPickerVisible.value = false
}

function deriveRuntimeVersion() {
  const val = form.php_fpm || ''
  if (!val) return
  const opt = phpFpms.value.find(o => o.value === val)
  if (opt && opt.label) {
    form.runtime_version = opt.label
    return
  }
  // 兜底：从 socket 路径解析
  const m = val.match(/php([\d.]+)-fpm\.sock$/)
  if (m) {
    form.runtime_version = 'PHP ' + m[1]
  }
}

watch(() => form.php_fpm, deriveRuntimeVersion)

// 防盗链开关由关闭切到开启时，若允许域名列表为空则默认填入站点绑定的域名
// 仅在 referers 为空时自动填充，避免覆盖用户已配置的域名
watch(() => form.hotlink_enabled, (enabled) => {
  if (enabled && (!form.hotlink_referers || form.hotlink_referers.length === 0)) {
    form.hotlink_referers = [...siteDomains.value]
  }
})

// 把中文冒号 ： 自动转成英文冒号 :（域名/IP:端口 输入兼容）
function normalizeColon(s) {
  return (s || '').replace(/：/g, ':')
}

// 合并主域名 + 附加域名到一个数组，去重，保留主域名在前
function mergeDomainBindings(domain, domainsStr) {
  const list = []
  const seen = new Set()
  const main = (domain || '').trim()
  if (main) { list.push(main); seen.add(main) }
  for (const d of splitCSV(domainsStr || '')) {
    if (d && !seen.has(d)) { list.push(d); seen.add(d) }
  }
  return list
}

function addTag(list, input, clearFn) {
  const v = normalizeColon(input || '').trim()
  if (v && !list.includes(v)) list.push(v)
  if (clearFn) clearFn()
}

// 校验单个绑定（与后端 isValidBinding 对齐，域名/IP/IP:端口/域名:端口）
function validateBindingLocal(s) {
  s = normalizeColon(s || '').trim()
  if (!s) return false
  // 泛域名支持
  if (/^\*\.[\w.\-]+$/.test(s)) return true
  // 域名:端口
  const m = /^(.+):(\d{1,5})$/.exec(s)
  if (m) {
    const port = parseInt(m[2], 10)
    if (port < 1 || port > 65535) return false
    return isDomainOrIP(m[1])
  }
  return isDomainOrIP(s)
}
function isDomainOrIP(s) {
  if (!s) return false
  // IPv4
  if (/^(\d{1,3}\.){3}\d{1,3}$/.test(s)) {
    return s.split('.').every(p => { const n = +p; return n >= 0 && n <= 255; })
  }
  // 域名
  return /^(\*\.)?[a-zA-Z0-9]([a-zA-Z0-9\-\.]*[a-zA-Z0-9])?$/.test(s)
}

// 实时解析批量输入：每行一个，返回 { value, port, status } 列表
// status: 'valid' | 'duplicate' | 'invalid' | 'empty'
const batchPreviewLines = computed(() => {
  const text = normalizeColon(batchDomainInput.value || '')
  const lines = text.split('\n')
  const existed = new Set(form.domains)
  return lines.map((raw) => {
    const s = raw.trim()
    if (!s) return { value: s, port: '', status: 'empty' }
    const [, port] = /:(?:\d{1,5})$/.test(s) ? s.match(/^(.+):(\d{1,5})$/) : ['', '']
    if (existed.has(s)) return { value: s, port, status: 'duplicate' }
    if (!validateBindingLocal(s)) return { value: s, port, status: 'invalid' }
    return { value: s, port, status: 'valid' }
  })
})
const batchPreviewValidCount = computed(() => batchPreviewLines.value.filter(x => x.status === 'valid').length)
const batchPreviewDuplicates = computed(() => batchPreviewLines.value.filter(x => x.status === 'duplicate').length)
const batchPreviewInvalid = computed(() => batchPreviewLines.value.filter(x => x.status === 'invalid').length)

// 批量添加（每行一个）：自动校验、去重、跳过空行/重复项，成功后自动保存
function addBatchDomains() {
  const valid = batchPreviewLines.value.filter(x => x.status === 'valid').map(x => x.value)
  if (!valid.length) {
    ElMessage.warning('没有可添加的有效域名')
    return
  }
  const existed = new Set(form.domains)
  const added = []
  for (const ln of valid) {
    if (existed.has(ln)) continue
    added.push(ln)
    existed.add(ln)
  }
  if (added.length) {
    form.domains.push(...added)
    batchDomainInput.value = ''
    // 静默保存：批量添加域名也属于"域名变更"类，不弹"设置已保存并生效"提示
    saveSettings({ silent: true })
  }
}

// 拖拽结束自动保存
function onDomainListChange() {
  // 拖拽排序：静默保存，不弹"设置已保存并生效"提示
  saveSettings({ silent: true })
}

// 开始编辑某行域名（默认显示为文本，点编辑后切输入框）
function startEditDomain(index) {
  editingDomainIdx.value = index
  nextTick(() => {
    const inputs = domainInputRefs.value
    const ref = Array.isArray(inputs) ? inputs[index] : inputs
    if (ref && ref.focus) ref.focus()
  })
}

// 保存编辑并退出输入模式
function saveDomainRowEdit(index) {
  if (editingDomainIdx.value === index) {
    editingDomainIdx.value = -1
    // 行内编辑域名也走静默保存
    saveSettings({ silent: true })
  }
}

// 域名显示为「host（不含端口）」，端口单独显示在端口列
function domainHostLabel(d) {
  return d.replace(/:(\d{1,5})$/, '')
}

// 域名后带端口时显示端口号，否则显示站点默认端口
function domainPortLabel(d) {
  const m = /:(\d{1,5})$/.exec(d)
  if (m) return m[1]
  return String(site.value.port || 80)
}

// 域名点击打开链接
// 规则：含显式端口时 → 保留端口（拼接 host:port）；443 → https；80 → http
// 无显式端口时按站点默认（site.ssl_enabled 决定协议）
function domainVisitUrl(d) {
  let host = d
  if (host.startsWith('*.')) host = 'fan.' + host.slice(2)
  // 先把端口从 d 里切出来（显示用）
  const m = /:(\d{1,5})$/.exec(host)
  const explicitPort = m ? m[1] : ''
  if (explicitPort) host = host.slice(0, -m[0].length)
  // 协议 + 端口
  let proto, port
  if (explicitPort) {
    port = parseInt(explicitPort, 10)
    proto = port === 443 ? 'https:' : (port === 80 ? 'http:' : (site.value.ssl_enabled ? 'https:' : 'http:'))
  } else {
    port = site.value.port || 80
    proto = site.value.ssl_enabled ? 'https:' : 'http:'
  }
  // 默认 80 / 443 不拼端口
  const p = (port === 80 || port === 443) ? '' : `:${port}`
  return `${proto}//${host}${p}`
}

// 单行删除（带二次确认）
async function deleteDomainRow(index) {
  const d = form.domains[index]
  try {
    await ElMessageBox.confirm(
      `确定要删除域名 "${d}" 吗？删除后将无法访问。`,
      '删除域名',
      {
        type: 'warning',
        confirmButtonText: '删除',
        cancelButtonText: '取消',
      }
    )
  } catch {
    return  // 用户点取消
  }
  form.domains.splice(index, 1)
  // 静默保存：不弹"设置已保存并生效"提示
  saveSettings({ silent: true })
}

function removeTag(list, item) {
  const i = list.indexOf(item)
  if (i > -1) list.splice(i, 1)
}

// ---------- 默认文档标签编辑 ----------
function showIndexInput() {
  addingIndex.value = true
  nextTick(() => indexInputRef.value?.focus())
}

function addIndexItem() {
  const v = (indexInput.value || '').trim()
  if (v && !defaultIndexes.value.includes(v)) {
    defaultIndexes.value.push(v)
  }
  indexInput.value = ''
  addingIndex.value = false
}

function removeIndexItem(item) {
  const i = defaultIndexes.value.indexOf(item)
  if (i > -1) defaultIndexes.value.splice(i, 1)
}

function handleDragStart(idx) {
  dragIndex.value = idx
}

function handleDrop(idx) {
  const from = dragIndex.value
  if (from === idx || from < 0) return
  const item = defaultIndexes.value[from]
  defaultIndexes.value.splice(from, 1)
  defaultIndexes.value.splice(idx, 0, item)
  dragIndex.value = -1
}

// 按当前 Tab 构建独立保存 payload：仅提交该 Tab 涉及的字段，后端只更新对应字段组
function buildSettingsPayload() {
  const base = { id: site.value.id, tab: activeTab.value }
  switch (activeTab.value) {
    case 'domains':
      return {
        ...base,
        domain: (form.domains && form.domains[0]) || '',
        domains: (form.domains || []).slice(1).join(',')
      }
    case 'redirect':
      return {
        ...base,
        redirect_url: form.redirect_url,
        redirect_code: form.redirect_code,
        redirect_keep_path: form.redirect_keep_path,
        redirects: redirectRules.value.map((r) => ({
          id: r.id,
          match_type: r.match_type,
          domains: r.domains || [],
          paths: r.paths || [],
          target_url: r.target_url,
          code: r.code,
          keep_path: r.keep_path
        }))
      }
    case 'rewrite':
      return { ...base, rewrite: form.rewrite }
    case 'hotlink':
      return {
        ...base,
        hotlink_enabled: form.hotlink_enabled,
        hotlink_referers: (form.hotlink_referers || []).join(','),
        cache_enabled: form.cache_enabled,
        security_headers: form.security_headers
      }
    default: // base
      return {
        ...base,
        name: form.name,
        remark: form.remark,
        // 第一个元素为主域名，其余为附加域名
        domain: (form.domains && form.domains[0]) || '',
        domains: (form.domains || []).slice(1).join(','),
        root: form.root,
        runtime_dir: form.runtime_dir,
        default_index: defaultIndexes.value.join(' '),
        rate_limit_kbs: form.rate_limit_kbs,
        php_fpm: form.php_fpm,
        proxy_pass: form.proxy_pass,
        start_command: form.start_command,
        env_vars: form.env_vars,
        proxy_port: form.proxy_port,
        runtime_version: form.runtime_version
      }
  }
}

const webServerType = computed(() => site.value.web_server || 'nginx')
const webServerLabel = computed(() => webServerType.value === 'apache' ? 'Apache' : 'Nginx')

watch(selectedPreset, (key) => {
  if (!key) return
  const p = rewritePresets.find(item => item.key === key)
  if (!p) return
  const server = webServerType.value
  form.rewrite = p[server] || p.nginx || ''
})

async function saveSettings(opts = {}) {
  const { silent = false } = opts
  saving.value = true
  try {
    await request.post('/site/settings', buildSettingsPayload())
    if (!silent) {
      ElMessage.success('设置已保存并生效')
    }
    // 把最新主域名 + 附加域名传给父组件，父组件原地更新站点行（避免全量刷新）
    // 即使 silent（删除/拖拽域名），父组件仍需知道新数据来原地更新行
    emit('success', {
      id: site.value.id,
      domain: (form.domains && form.domains[0]) || '',
      domains: (form.domains || []).slice(1).join(','),
      name: form.name,
      root: form.root,
    })
    // 独立保存：不再重新 loadDetail 全量刷新，避免覆盖其他 Tab 的未保存改动
    // 若改了站点名称，同步更新标题
    if (activeTab.value === 'base' && form.name) {
      site.value.name = form.name
    }
  } catch (e) { /* interceptor handles */ } finally {
    saving.value = false
  }
}

function loadConfig() {
  return request.get('/site/config', { params: { id: site.value.id } }).then((res) => {
    configContent.value = res.data.content || ''
    loadedConfig.value = true
  })
}

async function saveConfig() {
  savingConfig.value = true
  try {
    await request.post('/site/config', { id: site.value.id, content: configContent.value })
    ElMessage.success(`配置已保存并重载 ${webServerLabel.value}`)
  } catch (e) { /* interceptor handles */ } finally {
    savingConfig.value = false
  }
}

watch(activeTab, (t) => {
  if (t === 'config') loadConfig()
})

watch(() => form.root, () => {
  loadRuntimeDirOptions()
})

defineExpose({ open })
</script>

<style scoped>
.setting-form { margin-top: 8px; }
.tip { margin-left: 8px; font-size: 12px; color: #909399; line-height: 1.4; }
.tip.warn { color: #e6a23c; margin-left: 0; }
/* Nginx 代码编辑器样式已抽到 components/CodeEditor.vue */
.config-hint { color: #909399; font-size: 12px; margin-right: auto; }
.action-bar { margin-top: 12px; display: flex; justify-content: flex-end; gap: 8px; }
.tag-editor { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
.drag-tag { cursor: move; }
.rewrite-preset { margin-bottom: 12px; }
.domain-tab { padding: 0 4px; }
/* 域名列表（vuedraggable + 固定列宽） */
.domain-list-wrap { border: 1px solid #ebeef5; border-radius: 6px; padding: 6px; min-height: 80px; }
/* 标题行 + 内容行都用同一套 grid 列宽（handle / name / port / actions） */
.domain-list-header,
.domain-row {
  display: grid;
  grid-template-columns: 22px 1fr 64px 130px;
  align-items: center;
  gap: 8px;
}
.domain-list-header { padding: 6px 8px; font-size: 13px; color: #909399; font-weight: 600; border-bottom: 1px solid #ebeef5; margin-bottom: 4px; background: #fafbfc; border-radius: 4px; }
.domain-list-header .drag-handle-placeholder::before { content: ''; display: inline-block; width: 14px; height: 14px; }
.domain-list-header .domain-name-cell.header { padding-left: 0; }
.domain-list-header .domain-port.header { text-align: center; }
.domain-list-header .domain-actions-header { text-align: right; padding-right: 8px; }
.domain-row { padding: 6px 8px; background: #fff; border-radius: 4px; margin-bottom: 4px; }
.domain-row.is-primary { background: #ecf5ff; }
.domain-row .domain-col-handle { display: flex; align-items: center; justify-content: center; cursor: grab; color: #909399; }
.domain-row .domain-col-handle:active { cursor: grabbing; }
.domain-row .domain-col-name { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.domain-row .domain-col-port { text-align: center; font-size: 13px; color: #909399; }
.domain-row .domain-col-actions { text-align: right; padding-right: 0; }
.domain-row .domain-col-actions .el-button { padding: 2px 4px; }
.domain-row .el-input { width: 100%; }
.domain-text { color: #409eff; text-decoration: none; }
.domain-text:hover { text-decoration: underline; }
.domain-ghost { opacity: 0.4; background: #f0f9ff; }
.domain-empty { grid-column: 1 / -1; text-align: center; color: #909399; padding: 24px 0; font-size: 13px; }
.rewrite-preset :deep(.el-form-item__content) { display: block; }
.preset-row { display: flex; gap: 8px; align-items: center; }

.dir-picker-header { display: flex; gap: 8px; align-items: center; margin-bottom: 12px; }
.dir-picker-header .el-input { flex: 1; }
.dir-picker-list { border: 1px solid #e4e7ed; border-radius: 6px; max-height: 320px; overflow-y: auto; }
.dir-picker-item { display: flex; align-items: center; gap: 8px; padding: 10px 12px; cursor: pointer; border-bottom: 1px solid #f0f2f5; }
.dir-picker-item:last-child { border-bottom: none; }
.dir-picker-item:hover { background: #f5f7fa; }
.dir-picker-item.is-active { background: #ecf5ff; }
.dir-picker-name { font-size: 14px; }
.dir-picker-empty { padding: 24px; text-align: center; color: #909399; font-size: 13px; }

.redirect-rules { display: flex; flex-direction: column; gap: 12px; }
.redirect-empty {
  padding: 24px;
  text-align: center;
  color: #909399;
  font-size: 13px;
  border: 1px dashed #dcdfe6;
  border-radius: 8px;
}
.redirect-rule {
  border: 1px solid #e4e7ed;
  border-radius: 8px;
  padding: 12px;
  background: #fafafa;
}
.redirect-rule-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
}
.redirect-rule-title {
  font-weight: 600;
  font-size: 14px;
  color: #303133;
}
</style>

