<template>
  <el-dialog
    v-model="visible"
    :title="dialogTitle"
    width="min(900px, 96vw)"
    top="5vh"
    align-center
    :close-on-click-modal="false"
    class="mail-compose-dialog"
    @closed="onClosed"
  >
    <div class="mc-wrap">
      <!-- 发件人 -->
      <div class="mc-row">
        <span class="mc-label">发件人</span>
        <el-select v-model="fromId" size="default" style="width: 260px" placeholder="选择发件账号">
          <el-option v-for="a in accounts" :key="a.id" :label="a.address" :value="a.id" />
        </el-select>
      </div>
      <!-- 收件人 -->
      <div class="mc-row">
        <span class="mc-label">收件人</span>
        <div class="mc-recipients">
          <el-tag
            v-for="(t, i) in toList"
            :key="i"
            closable
            size="large"
            type="info"
            effect="light"
            class="mc-tag"
            @close="removeRecipient(i)"
          >{{ t }}</el-tag>
          <el-input
            v-model="toInput"
            class="mc-recipient-input"
            placeholder="输入邮箱后回车；可用 , ; ； ，空格 换行 分割多个"
            @keyup.enter="confirmRecipientInput"
            @keydown="onRecipientKeydown"
            @paste="onRecipientPaste"
          />
        </div>
      </div>
      <!-- 主题 -->
      <div class="mc-row">
        <span class="mc-label">主　题</span>
        <el-input v-model="subject" placeholder="邮件主题" />
      </div>

      <!-- 工具栏 -->
      <div class="mc-toolbar">
        <button type="button" class="mc-tb" title="撤销" @click="exec('undo')">↶</button>
        <button type="button" class="mc-tb" title="重做" @click="exec('redo')">↷</button>
        <span class="mc-sep" />
        <select class="mc-select" title="字体" @change="onFontFamily($event)">
          <option value="">字体</option>
          <option value="Microsoft YaHei">微软雅黑</option>
          <option value="SimSun">宋体</option>
          <option value="SimHei">黑体</option>
          <option value="KaiTi">楷体</option>
          <option value="Arial">Arial</option>
          <option value="Georgia">Georgia</option>
          <option value="Courier New">Courier New</option>
        </select>
        <select class="mc-select" title="字号" @change="onFontSize($event)">
          <option value="">字号</option>
          <option value="2">小</option>
          <option value="3">正常</option>
          <option value="4">中</option>
          <option value="5">大</option>
          <option value="6">特大</option>
        </select>
        <span class="mc-sep" />
        <button type="button" class="mc-tb" :class="{ on: active.bold }" title="加粗" @click="exec('bold')"><b>B</b></button>
        <button type="button" class="mc-tb" :class="{ on: active.italic }" title="斜体" @click="exec('italic')"><i>I</i></button>
        <button type="button" class="mc-tb" :class="{ on: active.underline }" title="下划线" @click="exec('underline')"><u>U</u></button>
        <button type="button" class="mc-tb" :class="{ on: active.strikeThrough }" title="删除线" @click="exec('strikeThrough')"><s>S</s></button>
        <span class="mc-sep" />
        <label class="mc-tb mc-color" title="字体颜色">
          <span class="mc-color-a">A</span>
          <span class="mc-color-bar" :style="{ background: foreColor }" />
          <input type="color" v-model="foreColor" @input="exec('foreColor', foreColor)" />
        </label>
        <label class="mc-tb mc-color" title="背景颜色">
          <span class="mc-color-a">▨</span>
          <span class="mc-color-bar" :style="{ background: backColor }" />
          <input type="color" v-model="backColor" @input="exec('hiliteColor', backColor)" />
        </label>
        <span class="mc-sep" />
        <button type="button" class="mc-tb" title="左对齐" @click="exec('justifyLeft')">左</button>
        <button type="button" class="mc-tb" title="居中" @click="exec('justifyCenter')">中</button>
        <button type="button" class="mc-tb" title="右对齐" @click="exec('justifyRight')">右</button>
        <span class="mc-sep" />
        <button type="button" class="mc-tb" title="无序列表" @click="exec('insertUnorderedList')">•≡</button>
        <button type="button" class="mc-tb" title="有序列表" @click="exec('insertOrderedList')">1≡</button>
        <button type="button" class="mc-tb" title="引用" @click="exec('formatBlock', 'blockquote')">❝</button>
        <span class="mc-sep" />
        <button type="button" class="mc-tb" title="插入链接" @click="insertLink">🔗</button>
        <button type="button" class="mc-tb" title="插入图片(≤5M)" @click="pickImage">🖼</button>
        <button type="button" class="mc-tb" title="清除格式" @click="exec('removeFormat')">Tx</button>
      </div>

      <!-- 正文 -->
      <div
        ref="editorRef"
        class="mc-editor"
        contenteditable="true"
        @input="onEditorInput"
        @keyup="updateActive"
        @mouseup="updateActive"
        @paste="onPaste"
      ></div>

      <!-- 附件列表 -->
      <div v-if="attachments.length" class="mc-attach-list">
        <div v-for="(f, i) in attachments" :key="i" class="mc-attach-item">
          <el-icon><Document /></el-icon>
          <span class="mc-attach-name">{{ f.filename }}</span>
          <span class="mc-attach-size">{{ fmtSize(f.size) }}</span>
          <el-button link type="danger" size="small" @click="attachments.splice(i, 1)">移除</el-button>
        </div>
      </div>
    </div>

    <template #footer>
      <div class="mc-footer">
        <div class="mc-footer-left">
          <el-button size="default" :icon="Paperclip" @click="pickAttach">附件</el-button>
          <span class="mc-tip">图片≤5M内嵌 · 附件总计≤25M</span>
        </div>
        <div class="mc-footer-right">
          <el-button @click="visible = false">取消</el-button>
          <el-button :loading="saving" @click="doSaveDraft">存草稿</el-button>
          <el-button type="primary" :loading="sending" @click="doSend">发送</el-button>
        </div>
      </div>
    </template>

    <input ref="fileInputRef" type="file" multiple style="display:none" @change="onPickAttach" />
    <input ref="imgInputRef" type="file" accept="image/*" style="display:none" @change="onPickImage" />
  </el-dialog>
</template>

<script setup>
import { ref, computed, nextTick, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Paperclip, Document } from '@element-plus/icons-vue'
import { sendMail, saveMailDraft } from '../../api/mail'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  accounts: { type: Array, default: () => [] },
  defaultFromId: { type: Number, default: 0 },
  prefill: {
    type: Object,
    default: null,
    // { to: 'a@x.com,b@x.com', subject, html, text }
  },
  mode: { type: String, default: '' }, // 'reply' | 'forward' | 'draft' | ''
  draftId: { type: Number, default: 0 }
})
const emit = defineEmits(['update:modelValue', 'sent', 'draft-saved'])

const visible = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v)
})

const dialogTitle = computed(() => {
  if (props.mode === 'reply') return '回复邮件'
  if (props.mode === 'forward') return '转发邮件'
  if (props.mode === 'draft') return '编辑草稿'
  return '写信'
})

const fromId = ref(0)
const toList = ref([])
const toInput = ref('')
const subject = ref('')
const sending = ref(false)
const saving = ref(false)
const attachments = ref([]) // {filename, type, size, data(base64), inline}
const editorRef = ref(null)
const fileInputRef = ref(null)
const imgInputRef = ref(null)
const foreColor = ref('#000000')
const backColor = ref('#ffff00')
const active = ref({ bold: false, italic: false, underline: false, strikeThrough: false })

// 打开时初始化
watch(() => props.modelValue, (v) => {
  if (!v) return
  fromId.value = props.defaultFromId || (props.accounts[0]?.id || 0)
  toList.value = []
  toInput.value = ''
  subject.value = ''
  attachments.value = []
  const p = props.prefill
  if (p) {
    if (p.to) {
      for (const a of String(p.to).split(/[,，;；\s\n\r]+/)) {
        const clean = extractEmail(a)
        if (clean) pushRecipient(clean)
      }
    }
    if (p.subject) subject.value = p.subject
    nextTick(() => {
      if (editorRef.value) editorRef.value.innerHTML = p.html || htmlToPlain(p.text || '')
    })
  } else {
    nextTick(() => { if (editorRef.value) editorRef.value.innerHTML = '' })
  }
})

// 从 "Name <a@b>" 或 "a@b" 中提取纯邮箱
function extractEmail(s) {
  const m = String(s || '').match(/<([^>]+)>/)
  if (m) return m[1].trim()
  return String(s || '').trim()
}

function onClosed() {
  if (editorRef.value) editorRef.value.innerHTML = ''
}

// 收件人
const emailRe = /^[^@\s]+@[^@\s]+\.[^@\s]+$/
const splitRe = /[,，;；\s\n\r]+/

function pushRecipient(addr) {
  const v = String(addr || '').trim()
  if (!v) return false
  if (!emailRe.test(v)) {
    ElMessage.warning('邮箱格式不正确：' + v)
    return false
  }
  if (!toList.value.includes(v)) toList.value.push(v)
  return true
}

// 回车确认：拆分当前输入并逐个加入
function confirmRecipientInput() {
  const raw = toInput.value
  if (!raw) return
  const parts = raw.split(splitRe).map((s) => s.trim()).filter(Boolean)
  if (parts.length === 0) return
  let added = 0
  for (const p of parts) { if (pushRecipient(p)) added++ }
  if (added > 0) toInput.value = ''
}

// 粘贴多个邮箱时按分隔符批量加入
function onRecipientPaste(e) {
  e.preventDefault()
  const text = (e.clipboardData || window.clipboardData)?.getData('text') || ''
  if (!text) return
  const parts = text.split(splitRe).map((s) => s.trim()).filter(Boolean)
  for (const p of parts) pushRecipient(p)
  const remainder = text.split(splitRe).pop() || ''
  toInput.value = ''
  if (remainder && !emailRe.test(remainder)) toInput.value = remainder
}

// 输入中出现分隔符即拆分为标签
function onRecipientKeydown() {
  if (!toInput.value || !splitRe.test(toInput.value)) return
  confirmRecipientInput()
}

function removeRecipient(i) {
  toList.value.splice(i, 1)
}

function exec(cmd, val) {
  editorRef.value?.focus()
  try {
    document.execCommand('styleWithCSS', false, true)
    document.execCommand(cmd, false, val)
  } catch (e) { /* ignore */ }
  updateActive()
}
function onFontFamily(e) {
  if (e.target.value) exec('fontName', e.target.value)
  e.target.value = ''
}
function onFontSize(e) {
  if (e.target.value) exec('fontSize', e.target.value)
  e.target.value = ''
}
function updateActive() {
  try {
    active.value = {
      bold: document.queryCommandState('bold'),
      italic: document.queryCommandState('italic'),
      underline: document.queryCommandState('underline'),
      strikeThrough: document.queryCommandState('strikeThrough')
    }
  } catch (e) { /* ignore */ }
}
function insertLink() {
  ElMessageBox.prompt('请输入链接地址', '插入链接', { inputValue: 'https://' }).then(({ value }) => {
    if (value) exec('createLink', value)
  }).catch(() => {})
}
function onEditorInput() { /* 可扩展字数统计 */ }

// 粘贴：只保留纯文本，避免带入外部复杂样式
function onPaste(e) {
  e.preventDefault()
  const text = (e.clipboardData || window.clipboardData).getData('text/plain')
  document.execCommand('insertText', false, text)
}

// 图片：编辑器内用 data URL 预览，发送时提取为内嵌附件并换成 cid
function pickImage() { imgInputRef.value?.click() }
function onPickImage(e) {
  const file = e.target.files?.[0]
  e.target.value = ''
  if (!file) return
  if (!file.type.startsWith('image/')) { ElMessage.warning('请选择图片文件'); return }
  if (file.size > 5 * 1024 * 1024) { ElMessage.warning('图片不能超过 5MB（更大图片暂不支持）'); return }
  const reader = new FileReader()
  reader.onload = () => {
    const dataUrl = String(reader.result)
    editorRef.value?.focus()
    document.execCommand('insertHTML', false, `<img src="${dataUrl}" data-mc-inline="1" style="max-width:100%" />`)
  }
  reader.readAsDataURL(file)
}

function pickAttach() { fileInputRef.value?.click() }
function onPickAttach(e) {
  const files = Array.from(e.target.files || [])
  e.target.value = ''
  for (const file of files) {
    const cur = attachments.value.filter((a) => !a.inline).reduce((s, a) => s + a.size, 0)
    if (cur + file.size > 25 * 1024 * 1024) {
      ElMessage.warning('附件总大小不能超过 25MB')
      break
    }
    const reader = new FileReader()
    reader.onload = () => {
      const base64 = String(reader.result).split(',')[1]
      attachments.value.push({ filename: file.name, type: file.type || 'application/octet-stream', size: file.size, data: base64, inline: false })
    }
    reader.readAsDataURL(file)
  }
}

function fmtSize(n) {
  if (n < 1024) return n + ' B'
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB'
  return (n / 1024 / 1024).toFixed(2) + ' MB'
}

// 收集正文与附件（正文里的 data URL 图片提取为内嵌附件并换成 cid 引用）
function collectContent() {
  let html = editorRef.value?.innerHTML || ''
  const text = (editorRef.value?.innerText || '').trim()
  const inlineAtts = []
  html = html.replace(/<img\b[^>]*\bsrc="(data:image\/[^"]+)"[^>]*>/gi, (m, dataUrl) => {
    const semi = dataUrl.indexOf(';')
    const comma = dataUrl.indexOf(',')
    if (semi < 0 || comma < 0) return m
    const mime = dataUrl.slice(5, semi)
    const b64 = dataUrl.slice(comma + 1)
    const cid = 'img_' + Date.now() + '_' + Math.random().toString(36).slice(2, 8)
    inlineAtts.push({ filename: cid + '.' + (mime.split('/')[1] || 'png'), type: mime, data: b64, inline: true, cid })
    return `<img src="cid:${cid}" style="max-width:100%" />`
  })
  const attach = [
    ...inlineAtts,
    ...attachments.value.map((a) => ({
      filename: a.filename, type: a.type, data: a.data, inline: !!a.inline, cid: a.cid || ''
    }))
  ]
  return { html, text, attach, hasInline: inlineAtts.length > 0 }
}

async function doSend() {
  confirmRecipientInput()
  if (!fromId.value) return ElMessage.warning('请选择发件账号')
  if (!toList.value.length) return ElMessage.warning('请填写收件人')
  const { html, text, attach } = collectContent()
  if (!text && !attachments.value.length) return ElMessage.warning('请输入正文或添加附件')
  sending.value = true
  try {
    const { data } = await sendMail({
      mailbox_id: fromId.value,
      to: toList.value,
      subject: subject.value,
      text,
      html,
      attachments: attach
    })
    const remote = data?.remote?.length || 0
    const local = data?.local?.length || 0
    const failed = data?.failed || []
    if (failed.length) {
      ElMessage.warning(`部分成功：本地${local} 外发${remote} 失败${failed.length}（${failed.join('；')}）`)
    } else {
      ElMessage.success(`已发送（本地${local} 外发${remote}）`)
    }
    visible.value = false
    emit('sent')
  } catch (e) {
    ElMessage.error(e?.response?.data?.msg || '发送失败')
  } finally {
    sending.value = false
  }
}

async function doSaveDraft() {
  confirmRecipientInput()
  if (!fromId.value) return ElMessage.warning('请选择发件账号')
  const { html, text, attach } = collectContent()
  if (!toList.value.length && !subject.value && !text && !attachments.value.length) {
    return ElMessage.warning('内容为空，无法保存草稿')
  }
  saving.value = true
  try {
    const { data } = await saveMailDraft({
      mailbox_id: fromId.value,
      draft_id: props.draftId || 0,
      to: toList.value,
      subject: subject.value,
      text,
      html,
      attachments: attach
    })
    ElMessage.success('草稿已保存')
    emit('draft-saved', data?.id || 0)
    visible.value = false
  } catch (e) {
    ElMessage.error(e?.response?.data?.msg || '保存失败')
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.mc-wrap { display: flex; flex-direction: column; gap: 8px; }
.mc-row { display: flex; align-items: center; gap: 10px; }
.mc-label { flex: 0 0 56px; color: #64748b; font-size: 14px; }
.mc-recipients { flex: 1; display: flex; flex-wrap: wrap; align-items: center; gap: 8px; border: 1px solid #dcdfe6; border-radius: 6px; padding: 6px 10px; min-height: 40px; }
.mc-recipients .mc-tag { margin: 0; }
.mc-recipient-input { width: 220px; flex: 1; min-width: 160px; }
.mc-recipient-input :deep(.el-input__wrapper),
.mc-recipient-input :deep(.el-input__wrapper.is-focus),
.mc-recipient-input :deep(.el-input__wrapper:hover),
.mc-recipient-input :deep(.el-input__wrapper:focus-within) {
  box-shadow: none !important; outline: none; }
.mc-recipient-input :deep(.el-input__inner) { outline: none; }
.mc-toolbar { display: flex; align-items: center; flex-wrap: wrap; gap: 2px; padding: 6px 8px; background: #f8fafc; border: 1px solid #e2e8f0; border-bottom: none; border-radius: 6px 6px 0 0; }
.mc-tb { display: inline-flex; align-items: center; justify-content: center; min-width: 30px; height: 28px; padding: 0 6px; border: none; background: transparent; border-radius: 4px; cursor: pointer; font-size: 14px; color: #334155; }
.mc-tb:hover { background: #e2e8f0; }
.mc-tb.on { background: #dbeafe; color: #1d4ed8; }
.mc-sep { width: 1px; height: 18px; background: #cbd5e1; margin: 0 4px; }
.mc-select { height: 28px; border: 1px solid #e2e8f0; border-radius: 4px; background: #fff; color: #334155; font-size: 13px; }
.mc-color { position: relative; flex-direction: column; }
.mc-color input[type=color] { position: absolute; inset: 0; opacity: 0; cursor: pointer; width: 100%; height: 100%; }
.mc-color-a { font-size: 13px; line-height: 1; }
.mc-color-bar { width: 18px; height: 3px; border-radius: 1px; }
.mc-editor { min-height: 300px; max-height: 46vh; overflow-y: auto; padding: 12px 14px; border: 1px solid #e2e8f0; border-radius: 0 0 6px 6px; font-size: 14px; line-height: 1.7; color: #1e293b; outline: none; }
.mc-editor :deep(img) { max-width: 100%; }
.mc-editor :deep(blockquote) { border-left: 3px solid #cbd5e1; margin: 6px 0; padding-left: 10px; color: #64748b; }
.mc-attach-list { display: flex; flex-direction: column; gap: 4px; margin-top: 4px; }
.mc-attach-item { display: flex; align-items: center; gap: 8px; font-size: 13px; color: #334155; background: #f8fafc; border-radius: 6px; padding: 4px 10px; }
.mc-attach-name { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.mc-attach-size { color: #94a3b8; }
.mc-footer { display: flex; align-items: center; justify-content: space-between; }
.mc-footer-left { display: flex; align-items: center; gap: 10px; }
.mc-tip { font-size: 12px; color: #94a3b8; }
.mc-footer-right { display: flex; gap: 10px; }

/* ============ 手机适配 ============ */
@media (max-width: 767px) {
  /* 发件人下拉自适应宽度 */
  .mc-row :deep(.el-select) { width: auto !important; flex: 1; min-width: 0; }
  .mc-row { flex-wrap: wrap; }
  .mc-label { flex: 0 0 56px; }
  .mc-recipients { flex: 1 1 100%; }
  .mc-recipient-input { width: 100%; min-width: 0; }
  .mc-toolbar { gap: 1px; padding: 5px 6px; }
  .mc-tb { min-width: 28px; padding: 0 4px; }
  .mc-sep { margin: 0 2px; }
  .mc-editor { min-height: 34vh; max-height: 46vh; }
  .mc-footer { flex-direction: column; align-items: stretch; gap: 10px; }
  .mc-footer-left { flex-wrap: wrap; }
  .mc-footer-right { justify-content: flex-end; flex-wrap: wrap; }
}
</style>
