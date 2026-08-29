<template>
  <el-dialog
    :model-value="modelValue"
    :title="title || '选择目录'"
    width="540px"
    :close-on-click-modal="false"
    @update:model-value="$emit('update:modelValue', $event)"
    @open="init"
  >
    <div class="picker">
      <div class="picker-bar">
        <el-button :disabled="path === '/'" @click="goUp">
          <el-icon><Back /></el-icon>
        </el-button>
        <el-button @click="load">
          <el-icon><Refresh /></el-icon>
        </el-button>
        <el-input
          v-model="pathInput"
          size="small"
          placeholder="目录路径"
          @keyup.enter="goPath"
        >
          <template #append>
            <el-button @click="goPath">前往</el-button>
          </template>
        </el-input>
      </div>
      <div class="picker-list" v-loading="loading">
        <div
          v-for="d in dirs"
          :key="d.path"
          class="picker-item"
          :class="{ active: d.path === path }"
          @click="select(d.path)"
          @dblclick="enter(d.path)"
        >
          <el-icon class="dir-icon"><Folder /></el-icon>
          <span class="dir-name">{{ d.name }}</span>
          <el-button link type="primary" size="small" class="enter-btn" @click.stop="enter(d.path)">
            进入
          </el-button>
        </div>
        <el-empty v-if="!loading && dirs.length === 0" description="该目录下没有子目录" :image-size="56" />
      </div>
      <div class="picker-current">
        <span>当前选择：<b>{{ path }}</b></span>
        <span v-if="invalidHint" class="invalid-hint">目录不存在</span>
      </div>
    </div>
    <template #footer>
      <el-button @click="$emit('update:modelValue', false)">取消</el-button>
      <el-button type="primary" :disabled="!path" @click="confirm">确定</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref } from 'vue'
import request from '../utils/request'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  title: { type: String, default: '' },
  // 打开时初始目录
  startPath: { type: String, default: '/' }
})
const emit = defineEmits(['update:modelValue', 'confirm'])

const path = ref('/')
const pathInput = ref('/')
const dirs = ref([])
const loading = ref(false)
const invalidHint = ref(false)

function init() {
  path.value = props.startPath || '/'
  pathInput.value = path.value
  load()
}

async function load() {
  loading.value = true
  invalidHint.value = false
  try {
    const res = await request.get('/file/list', { params: { path: path.value } })
    dirs.value = (res.data || []).filter(it => it.is_dir)
    pathInput.value = path.value
  } catch (e) {
    invalidHint.value = true
    dirs.value = []
  } finally {
    loading.value = false
  }
}

function select(p) {
  path.value = p
  pathInput.value = p
}

function enter(p) {
  path.value = p
  load()
}

function goUp() {
  if (path.value === '/') return
  const idx = path.value.lastIndexOf('/')
  path.value = idx <= 0 ? '/' : path.value.slice(0, idx)
  load()
}

function goPath() {
  const p = (pathInput.value || '').trim()
  if (!p) return
  path.value = p
  load()
}

function confirm() {
  emit('confirm', path.value)
  emit('update:modelValue', false)
}
</script>

<style scoped>
.picker-bar {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 10px;
}
.picker-bar .el-input { flex: 1; }
.picker-list {
  min-height: 200px;
  max-height: 320px;
  overflow: auto;
  border: 1px solid #ebeef5;
  border-radius: 6px;
  padding: 6px;
  background: #fafafa;
}
.picker-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 10px;
  border-radius: 4px;
  cursor: pointer;
  transition: background .15s;
}
.picker-item:hover { background: #f0f7ff; }
.picker-item.active { background: #ecf5ff; }
.dir-icon { color: #e6a23c; font-size: 15px; }
.dir-name { flex: 1; font-size: 13px; color: #303133; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.enter-btn { flex-shrink: 0; }
.picker-current {
  display: flex;
  justify-content: space-between;
  margin-top: 10px;
  font-size: 13px;
  color: #606266;
  word-break: break-all;
}
.invalid-hint { color: #f56c6c; }
</style>
