<template>
  <div class="ftn-wrap">
    <div
      class="ftn"
      :class="{
        active: isActive,
        dir: isDir,
        'is-open': isDir && isOpen,
        dirty: isDirty
      }"
      :style="{ paddingLeft: depth * 14 + 6 + 'px' }"
      @click="onClick"
      @contextmenu="onContextMenu"
    >
      <span class="ftn-caret" :class="{ open: isOpen, leaf: !isDir }" @click.stop="toggle">
        <el-icon v-if="isDir && loading" class="ftn-spin"><Loading /></el-icon>
        <span v-else-if="isDir">{{ isOpen ? '▾' : '▸' }}</span>
      </span>
      <el-icon class="ftn-ico" :class="{ 'is-dir': isDir }">
        <FolderOpened v-if="isDir && isOpen" />
        <Folder v-else-if="isDir" />
        <Document v-else />
      </el-icon>
      <span class="ftn-name" :title="item.path">{{ item.name }}</span>
      <span v-if="isDirty" class="ftn-dot" title="有未保存的修改"></span>
    </div>
    <div v-if="isDir && isOpen" class="ftn-children">
      <template v-if="loading && !children">
        <div v-for="i in 3" :key="'s' + i" class="ftn-skel" :style="{ paddingLeft: (depth + 1) * 14 + 18 + 'px' }"></div>
      </template>
      <FTNode
        v-for="it in children || []"
        :key="it.path"
        :item="it"
        :depth="depth + 1"
        :active-path="activePath"
        :dirty-tabs="dirtyTabs"
        :bump-version="bumpVersion"
        @open="(p) => $emit('open', p)"
        @menu="(m) => $emit('menu', m)"
        @refresh-path="(p) => $emit('refresh-path', p)"
      />
      <div v-if="loaded && children && children.length === 0 && !loading" class="ftn-empty" :style="{ paddingLeft: (depth + 1) * 14 + 24 + 'px' }">空</div>
    </div>
  </div>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { Document, Folder, FolderOpened, Loading } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import request from '@/utils/request'
import { useFileEditorStore } from '@/stores/fileEditor'

// 自递归组件：让模板里 <FTNode> 能解析到自身
defineOptions({ name: 'FTNode' })

const props = defineProps({
  item: { type: Object, required: true },
  depth: { type: Number, default: 0 },
  activePath: { type: String, default: '' },
  dirtyTabs: { type: Object, default: () => ({}) },
  bumpVersion: { type: Number, default: 0 }
})
const emit = defineEmits(['open', 'menu', 'refresh-path'])

const store = useFileEditorStore()

const isDir = computed(() => !!props.item.is_dir)
const isOpen = computed(() => !!store.expanded[props.item.path])
const isActive = computed(() => props.activePath === props.item.path)
const isDirty = computed(() => !!props.dirtyTabs[props.item.path])

const loading = ref(false)
const children = ref(null) // null = 未加载
const loaded = ref(false)

async function load(force = false) {
  if (!isDir.value || loading.value) return
  if (loaded.value && !force) return
  loading.value = true
  try {
    const res = await request.get('/file/list', { params: { path: props.item.path } })
    const items = (res.data || []).slice()
    items.sort((a, b) => (b.is_dir ? 1 : 0) - (a.is_dir ? 1 : 0) || a.name.localeCompare(b.name))
    children.value = items
    loaded.value = true
  } catch (e) {
    children.value = []
  } finally {
    loading.value = false
  }
}

// 全局版本号变化 → 强制重载
watch(() => props.bumpVersion, (v) => {
  if (!v) return
  loaded.value = false
  children.value = null
  if (isOpen.value) load(true)
})

// item.path 变化 → 重置
watch(() => props.item.path, () => { loaded.value = false; children.value = null })

function toggle() {
  if (!isDir.value) return
  const willOpen = !isOpen.value
  store.toggleDir(props.item.path)
  if (willOpen) load()
}

function onClick() {
  if (isDir.value) {
    toggle()
  } else {
    emit('open', props.item.path)
  }
}

function onContextMenu(e) {
  e.preventDefault()
  e.stopPropagation()
  emit('menu', { event: e, node: props.item })
}
</script>

<style scoped>
.ftn-wrap { display: block; }
.ftn {
  display: flex; align-items: center;
  height: 24px;
  padding-right: 8px;
  white-space: nowrap;
  cursor: pointer;
  gap: 2px;
  color: #cccccc;
  position: relative;
}
.ftn:hover { background: #2a2d2e; }
.ftn.active { background: #094771; color: #ffffff; }
.ftn-caret {
  width: 14px; text-align: center; color: #b5b5b5; font-size: 10px;
  flex: none; line-height: 24px;
  transition: transform 0.15s;
}
.ftn-caret.open { transform: rotate(0deg); }
.ftn-caret.leaf { visibility: hidden; }
.ftn-ico { font-size: 14px; color: #8a8a8a; flex: none; }
.ftn-ico.is-dir { color: #dcb67a; }
.ftn-name {
  overflow: hidden; text-overflow: ellipsis; line-height: 24px; flex: 1;
}
.ftn-dot {
  width: 6px; height: 6px; border-radius: 50%;
  background: #d19a66; flex: none; margin-left: 4px;
}
.ftn-children { }
.ftn-empty { color: #6b6b6b; font-size: 12px; line-height: 24px; }
.ftn-skel {
  height: 20px; margin: 2px 8px; border-radius: 3px;
  background: linear-gradient(90deg, #2a2d2e 25%, #323536 50%, #2a2d2e 75%);
  background-size: 200% 100%;
  animation: ft-shimmer 1.2s infinite;
}
.ftn-spin { animation: ft-rot 0.8s linear infinite; font-size: 13px; color: #7d8590; }

@keyframes ft-shimmer {
  from { background-position: 200% 0; }
  to { background-position: -200% 0; }
}
@keyframes ft-rot { to { transform: rotate(360deg); } }
</style>
