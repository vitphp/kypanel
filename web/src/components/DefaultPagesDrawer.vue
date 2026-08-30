<template>
  <el-drawer v-model="visible" title="默认页面" :size="isMobile ? '100%' : '720px'" @opened="load">
    <!-- 默认站点 -->
    <div class="dp-section">
      <div class="dp-field">
        <div class="dp-field-label">默认站点</div>
        <el-select
          v-model="defaultSiteId"
          placeholder="未设置：访问未绑定域名时显示「不存在页」"
          clearable
          style="width: 100%"
          :disabled="submitting"
          @change="onDefaultSiteChange"
        >
          <el-option key="__unset" :value="null" label="不设置（显示「不存在页面」）" />
          <el-option v-for="s in siteOptions" :key="s.id" :label="`${s.name}（${s.domain}）`" :value="s.id" />
        </el-select>
        <div class="dp-field-tip">
          设为默认站点后，访问未绑定到服务器的域名将直接打开该站点；未设置时显示「不存在页面」。
        </div>
      </div>
    </div>

    <el-divider />

    <!-- 4 个默认页面 -->
    <el-tabs v-model="tab" class="dp-tabs">
      <el-tab-pane v-for="p in pageList" :key="p.key" :name="p.key">
        <template #label>
          {{ p.title }}
        </template>
        <div class="dp-desc">{{ p.desc }}</div>
        <div class="dp-editor-wrap">
          <CodeEditor
            v-model="pages[p.key].content"
            lang="html"
            fill
            :enable-completion="false"
            placeholder="在此粘贴 HTML 内容..."
          />
        </div>
        <div class="dp-toolbar">
          <el-button size="small" @click="togglePreview(p.key)">
            <el-icon><View /></el-icon>&nbsp;{{ previewKey === p.key ? '收起预览' : '预览' }}
          </el-button>
          <span class="dp-tip">
            {{ p.key === 'index' || p.key === 'page404' ? '修改仅影响之后新建的网站' : '保存后立即生效' }}
          </span>
        </div>
        <iframe v-if="previewKey === p.key" :srcdoc="pages[p.key].content" class="dp-preview" />
      </el-tab-pane>
    </el-tabs>

    <template #footer>
      <div class="dp-footer">
        <el-button @click="visible = false">关闭</el-button>
        <el-button type="primary" :loading="submitting" @click="saveAll">
          <el-icon><Check /></el-icon>&nbsp;保存全部
        </el-button>
      </div>
    </template>
  </el-drawer>
</template>

<script setup>
import { ref, reactive, onMounted, onBeforeUnmount } from 'vue'
import { ElMessage } from 'element-plus'
import { View, Check } from '@element-plus/icons-vue'
import request from '../utils/request'
import CodeEditor from './CodeEditor.vue'

const visible = ref(false)
const submitting = ref(false)
const tab = ref('index')
const previewKey = ref('')
const defaultSiteId = ref(null)
const siteOptions = ref([])

// kind -> 后端页面文件名的映射
const kindMap = {
  index: 'index.html',
  page404: '404.html',
  stop: 'stop.html',
  nosite: 'nosite.html',
}

const pageList = [
  { key: 'index', title: '默认首页', desc: '未绑定域名（且未设置默认站点）访问时的兜底首页；新建网站时会自动复制到站点根目录作为初始首页。' },
  { key: 'page404', title: '404 页面', desc: '站内路径不存在时显示的页面；新建网站时会自动复制到站点根目录，站长可单独修改自己站点的 404 页。' },
  { key: 'stop', title: '停用页面', desc: '站点被「停止」后，访问该站点域名时显示的「网站已暂停服务」页面。' },
  { key: 'nosite', title: '不存在页面', desc: '访问未绑定到服务器的域名、且未设置默认站点时，显示的「网站不存在」页面。' },
]

const pages = reactive({
  index: { content: '' },
  page404: { content: '' },
  stop: { content: '' },
  nosite: { content: '' },
})

const isMobile = ref(false)
function updateResponsive() {
  isMobile.value = window.innerWidth < 768
}
onMounted(() => {
  updateResponsive()
  window.addEventListener('resize', updateResponsive)
})
onBeforeUnmount(() => window.removeEventListener('resize', updateResponsive))

function open() {
  visible.value = true
}

async function load() {
  try {
    const res = await request.get('/site/default-pages')
    pages.index.content = res.data.index || ''
    pages.page404.content = res.data.page404 || ''
    pages.stop.content = res.data.stop || ''
    pages.nosite.content = res.data.nosite || ''
    defaultSiteId.value = res.data.default_site_id || null
    // 站点列表用于默认站点下拉
    const listRes = await request.get('/site/list')
    siteOptions.value = (listRes.data || []).filter(s => s.id)
  } catch (e) { /* interceptor handles */ }
}

function togglePreview(key) {
  previewKey.value = previewKey.value === key ? '' : key
}

async function onDefaultSiteChange(id) {
  if (submitting.value) return
  submitting.value = true
  try {
    await request.post('/site/default-site', { id: id || 0 })
    ElMessage.success(id ? '已设为默认站点' : '已取消默认站点')
    emit('refresh')
  } catch (e) { /* interceptor handles */ } finally {
    submitting.value = false
  }
}

async function saveAll() {
  if (submitting.value) return
  submitting.value = true
  try {
    for (const [k, v] of Object.entries(pages)) {
      await request.post('/site/default-pages/save', { kind: kindMap[k], content: v.content })
    }
    ElMessage.success('默认页面已保存')
  } catch (e) { /* interceptor handles */ } finally {
    submitting.value = false
  }
}

const emit = defineEmits(['refresh'])
defineExpose({ open })
</script>

<style scoped>
.dp-section {
  padding: 0 2px;
}
.dp-field {
  margin-bottom: 4px;
}
.dp-field-label {
  font-size: 14px;
  font-weight: 600;
  margin-bottom: 8px;
}
.dp-field-tip {
  font-size: 12px;
  color: #909399;
  margin-top: 6px;
  line-height: 1.6;
}
.dp-desc {
  font-size: 13px;
  color: #909399;
  margin-bottom: 10px;
  line-height: 1.7;
}
.dp-editor-wrap {
  height: 460px;
  border-radius: 6px;
  overflow: hidden;
  border: 1px solid #dcdfe6;
}
@media (max-width: 767px) {
  .dp-editor-wrap { height: 340px; }
}
.dp-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 10px;
}
.dp-tip {
  font-size: 12px;
  color: #e6a23c;
}
.dp-preview {
  width: 100%;
  height: 300px;
  margin-top: 10px;
  border: 1px solid #dcdfe6;
  border-radius: 6px;
  background: #fff;
}
.dp-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
.dp-tabs :deep(.el-tabs__item) {
  font-size: 14px;
}
</style>
