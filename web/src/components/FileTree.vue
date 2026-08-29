<template>
  <div class="ft" @click="closeMenu">
    <!-- 顶部工具栏 -->
    <div class="ft-toolbar">
      <span class="ft-title">资源管理器</span>
      <div class="ft-tools">
        <span class="ft-tool" title="新建文件" @click="openCreateFile(root)"><el-icon><DocumentAdd /></el-icon></span>
        <span class="ft-tool" title="新建文件夹" @click="openCreateDir(root)"><el-icon><FolderAdd /></el-icon></span>
        <span class="ft-tool" title="刷新" @click="refreshAll"><el-icon><Refresh /></el-icon></span>
      </div>
    </div>

    <!-- 路径面包屑 + 返回上一层 -->
    <div class="ft-crumb">
      <span class="ft-crumb-up" :class="{ disabled: atRoot }" title="返回上一层目录" @click="goUp">
        <el-icon><ArrowUp /></el-icon>
      </span>
      <span class="ft-crumb-path" :title="root">
        <template v-if="!root || root === '/'">
          <span class="ft-crumb-seg ft-crumb-seg-root">/</span>
        </template>
        <template v-else>
          <span class="ft-crumb-sep">/</span>
          <template v-for="(seg, i) in crumbSegs" :key="i">
            <a class="ft-crumb-seg" @click="onCrumbClick(i)">{{ seg }}</a>
            <span v-if="i < crumbSegs.length - 1" class="ft-crumb-sep">/</span>
          </template>
        </template>
      </span>
    </div>

    <!-- 主体：树 -->
    <div class="ft-body" v-loading="rootLoading">
      <FTNode
        v-for="n in rootItems"
        :key="n.path"
        :item="n"
        :depth="0"
        :active-path="activePath"
        :dirty-tabs="dirtyTabMap"
        :bump-version="bumpVersion"
        @open="(p) => $emit('open', p)"
        @menu="onNodeMenu"
        @refresh-path="onNodeRefresh"
      />
      <div v-if="!rootLoading && !rootItems.length" class="ft-empty">空目录</div>
    </div>

    <!-- 右键菜单 -->
    <ul
      v-if="ctxMenu.visible"
      class="ft-menu"
      :style="{ left: ctxMenu.x + 'px', top: ctxMenu.y + 'px' }"
      @click.stop
    >
      <li
        v-for="(a, i) in ctxMenu.items"
        :key="i"
        :class="{ sep: a.sep, danger: a.danger, disabled: a.disabled }"
        @click="!a.disabled && onMenuAction(a)"
      >
        <el-icon v-if="a.icon"><component :is="a.icon" /></el-icon>
        <span class="ft-menu-label">{{ a.label }}</span>
        <span v-if="a.shortcut" class="ft-menu-key">{{ a.shortcut }}</span>
      </li>
    </ul>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  ArrowUp, CopyDocument, Delete, DocumentAdd, Edit,
  FolderAdd, HomeFilled, Monitor, Refresh, View
} from '@element-plus/icons-vue'
import request from '@/utils/request'
import { useFileEditorStore } from '@/stores/fileEditor'
import { useFloatingTerminalStore } from '@/stores/floatingTerminal'
import FTNode from './FileTreeNode.vue'

const props = defineProps({
  root: { type: String, default: '/' },
  activePath: { type: String, default: '' },
  collapsed: { type: Boolean, default: false }
})
const emit = defineEmits(['open', 'toggle-collapsed'])

const store = useFileEditorStore()
const floatTerm = useFloatingTerminalStore()

const rootLoading = ref(false)
const rootItems = ref([])

// 目录请求竞态保护：root 快速变化时只采用最后一次请求的结果
let rootReqId = 0

// bumpVersion 变化触发全树重新加载（创建/重命名/删除后调用）
const _bump = ref(0)
const bumpVersion = computed(() => _bump.value)
function bump() { _bump.value++ }

// 脏文件路径映射（用于节点圆点显示）
const dirtyTabMap = computed(() => {
  const m = {}
  for (const t of store.tabs) if (t.dirty) m[t.path] = true
  return m
})

// 面包屑
const crumbSegs = computed(() => {
  const r = props.root
  if (!r || r === '/') return []
  return r.split('/').filter(Boolean)
})
const atRoot = computed(() => !props.root || props.root === '/' || crumbSegs.value.length <= 1)

async function loadRoot() {
  if (!props.root) return
  const reqId = ++rootReqId
  rootLoading.value = true
  try {
    const res = await request.get('/file/list', { params: { path: props.root } })
    if (reqId !== rootReqId) return
    const items = (res.data || []).slice()
    items.sort((a, b) => (b.is_dir ? 1 : 0) - (a.is_dir ? 1 : 0) || a.name.localeCompare(b.name))
    rootItems.value = items
  } catch (e) {
    if (reqId !== rootReqId) return
    rootItems.value = []
    ElMessage.error('加载目录失败: ' + (e.message || ''))
  } finally {
    if (reqId === rootReqId) rootLoading.value = false
  }
}

watch(() => props.root, () => loadRoot(), { immediate: true })

function refreshAll() {
  loadRoot()
  bump()
}

function onCrumbClick(segIdx) {
  if (segIdx < 0) { store.setRoot('/'); return }
  store.setRoot('/' + crumbSegs.value.slice(0, segIdx + 1).join('/'))
}

function goUp() {
  if (atRoot.value) return
  const r = props.root
  if (!r || r === '/') return
  const idx = r.lastIndexOf('/')
  store.setRoot(idx <= 0 ? '/' : r.substring(0, idx))
}

// =============================================================
// 右键菜单
// =============================================================
const ctxMenu = ref({ visible: false, x: 0, y: 0, target: null, items: [] })

function closeMenu() {
  if (ctxMenu.value.visible) ctxMenu.value.visible = false
}

function onNodeMenu({ event, node }) {
  const items = []
  if (node.is_dir) {
    items.push({ label: '新建文件', icon: DocumentAdd, action: () => openCreateFile(node.path) })
    items.push({ label: '新建文件夹', icon: FolderAdd, action: () => openCreateDir(node.path) })
    items.push({ label: '在终端中打开', icon: Monitor, action: () => openTerminal(node.path) })
    items.push({ label: '设为项目根目录', icon: HomeFilled, action: () => store.setRoot(node.path) })
    items.push({ sep: true })
  }
  if (!node.is_dir) {
    items.push({ label: '打开', icon: View, action: () => emit('open', node.path) })
    items.push({ sep: true })
  }
  items.push({ label: '重命名', icon: Edit, action: () => openRename(node) })
  items.push({ label: '复制路径', icon: CopyDocument, action: () => copyPath(node.path) })
  items.push({ sep: true })
  items.push({ label: '删除', icon: Delete, danger: true, action: () => openDelete(node) })

  // 防止菜单溢出视口
  const x = Math.min(event.clientX, window.innerWidth - 220)
  const y = Math.min(event.clientY, window.innerHeight - 30)
  ctxMenu.value = { visible: true, x, y, target: node, items }
}

function onMenuAction(a) {
  closeMenu()
  a.action && a.action()
}

function onNodeRefresh() {
  bump()
  loadRoot()
}

// =============================================================
// 操作
// =============================================================
async function openCreateFile(dir) {
  closeMenu()
  try {
    const { value } = await ElMessageBox.prompt('输入新文件名（不含路径）', '新建文件', {
      confirmButtonText: '创建', cancelButtonText: '取消',
      inputValidator: v => !!(v && v.trim() && !v.includes('/') && !v.includes('\\'))
    })
    const newPath = (dir || '/').replace(/\/+$/, '') + '/' + value.trim()
    await request.post('/file/create', { path: newPath })
    ElMessage.success('已创建')
    if ((dir || '/') === props.root) loadRoot()
    else bump()
    store.expanded[dir] = true
  } catch (e) {
    if (e !== 'cancel' && e !== 'close') ElMessage.error(e?.message || '创建失败')
  }
}

async function openCreateDir(dir) {
  closeMenu()
  try {
    const { value } = await ElMessageBox.prompt('输入新文件夹名', '新建文件夹', {
      confirmButtonText: '创建', cancelButtonText: '取消',
      inputValidator: v => !!(v && v.trim() && !v.includes('/') && !v.includes('\\'))
    })
    const newPath = (dir || '/').replace(/\/+$/, '') + '/' + value.trim()
    await request.post('/file/mkdir', { path: newPath })
    ElMessage.success('已创建')
    if ((dir || '/') === props.root) loadRoot()
    else bump()
    store.expanded[dir] = true
  } catch (e) {
    if (e !== 'cancel' && e !== 'close') ElMessage.error(e?.message || '创建失败')
  }
}

async function openRename(node) {
  try {
    const { value } = await ElMessageBox.prompt('输入新名称', '重命名', {
      confirmButtonText: '重命名', cancelButtonText: '取消',
      inputValue: node.name,
      inputValidator: v => !!(v && v.trim() && !v.includes('/') && !v.includes('\\') && v.trim() !== node.name)
    })
    const parent = node.path.substring(0, node.path.lastIndexOf('/')) || '/'
    const newPath = (parent === '/' ? '' : parent) + '/' + value.trim()
    await request.post('/file/rename', { old_path: node.path, new_path: newPath })
    ElMessage.success('已重命名')
    store.renameTabPath(node.path, newPath)
    if (parent === props.root) loadRoot()
    else bump()
  } catch (e) {
    if (e !== 'cancel' && e !== 'close') ElMessage.error(e?.message || '重命名失败')
  }
}

async function openDelete(node) {
  try {
    await ElMessageBox.confirm(
      `确认删除「${node.name}」？该操作会进入回收站。`,
      '删除', { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' }
    )
    await request.post('/file/delete', { path: node.path })
    ElMessage.success('已移入回收站')
    store.closeTabsByPrefix(node.path)
    const parent = node.path.substring(0, node.path.lastIndexOf('/')) || '/'
    if (parent === props.root) loadRoot()
    else bump()
  } catch (e) {
    if (e !== 'cancel' && e !== 'close') ElMessage.error(e?.message || '删除失败')
  }
}

async function copyPath(p) {
  try {
    await navigator.clipboard.writeText(p)
    ElMessage.success('路径已复制')
  } catch {
    const ta = document.createElement('textarea')
    ta.value = p
    document.body.appendChild(ta)
    ta.select()
    try { document.execCommand('copy'); ElMessage.success('路径已复制') }
    catch { ElMessage.error('复制失败') }
    finally { document.body.removeChild(ta) }
  }
}

function openTerminal(dir) {
  const path = (dir || '/').replace(/\/+$/, '') || '/'
  floatTerm.open({ cwd: path })
}

function onDocClick() {
  if (ctxMenu.value.visible) ctxMenu.value.visible = false
}
onMounted(() => document.addEventListener('click', onDocClick))
onBeforeUnmount(() => document.removeEventListener('click', onDocClick))
</script>

<style scoped>
.ft {
  display: flex;
  flex-direction: column;
  height: 100%;
  background: #252526;
  color: #cccccc;
  font-size: 13px;
  user-select: none;
  position: relative;
}

/* ===== 顶部工具栏 ===== */
.ft-toolbar {
  display: flex; align-items: center;
  padding: 0 8px; height: 32px;
  border-bottom: 1px solid #1a1a1a; background: #252526; flex: none;
}
.ft-title {
  flex: 1; font-size: 11px; font-weight: 700; color: #bbbbbb;
  letter-spacing: 0.6px; text-transform: uppercase;
}
.ft-tools { display: flex; gap: 1px; }
.ft-tool {
  width: 22px; height: 22px;
  display: inline-flex; align-items: center; justify-content: center;
  border-radius: 3px; cursor: pointer; color: #aaaaaa;
}
.ft-tool:hover { background: #2a2d2e; color: #fff; }
.ft-tool .el-icon { font-size: 14px; }

/* ===== 面包屑 ===== */
.ft-crumb {
  display: flex; align-items: center; gap: 4px;
  padding: 4px 6px;
  border-bottom: 1px solid #1a1a1a;
  min-height: 28px; flex: none;
}
.ft-crumb-up {
  width: 22px; height: 22px;
  display: inline-flex; align-items: center; justify-content: center;
  border-radius: 3px; cursor: pointer; color: #cccccc; flex: none;
}
.ft-crumb-up:hover { background: #2a2d2e; color: #fff; }
.ft-crumb-up.disabled { opacity: 0.35; cursor: not-allowed; }
.ft-crumb-up.disabled:hover { background: transparent; color: #cccccc; }
.ft-crumb-up .el-icon { font-size: 13px; }
.ft-crumb-path {
  flex: 1; display: flex; align-items: center; gap: 1px;
  overflow: hidden; white-space: nowrap; font-size: 12px; min-width: 0;
}
.ft-crumb-seg {
  cursor: pointer; padding: 1px 4px; border-radius: 2px; color: #d4d4d4;
  max-width: 140px; overflow: hidden; text-overflow: ellipsis;
}
.ft-crumb-seg:hover { background: #2a2d2e; color: #fff; }
.ft-crumb-seg-root { color: #d4d4d4; }
.ft-crumb-sep { color: #666; font-size: 11px; flex: none; }

/* ===== 主体 ===== */
.ft-body { flex: 1; overflow: auto; padding: 2px 0; min-height: 0; }
.ft-empty { padding: 16px 12px; color: #6b6b6b; font-size: 12px; text-align: center; }

/* ===== 右键菜单 ===== */
.ft-menu {
  position: fixed; z-index: 9999; min-width: 200px;
  background: #252526; border: 1px solid #454545; border-radius: 4px;
  padding: 4px 0; list-style: none; margin: 0;
  box-shadow: 0 6px 20px rgba(0,0,0,0.55);
  font-size: 12px;
}
.ft-menu li {
  display: flex; align-items: center; gap: 8px;
  padding: 5px 14px; cursor: pointer; color: #cccccc;
}
.ft-menu li:hover:not(.disabled):not(.sep) { background: #094771; color: #fff; }
.ft-menu li.disabled { opacity: 0.4; cursor: not-allowed; }
.ft-menu li.danger { color: #f48771; }
.ft-menu li.danger:hover { background: #5a1d1d; color: #fff; }
.ft-menu li.sep { height: 1px; background: #454545; padding: 0; margin: 4px 0; cursor: default; }
.ft-menu li .el-icon { font-size: 14px; flex: none; }
.ft-menu-label { flex: 1; }
.ft-menu-key { color: #888; font-size: 11px; margin-left: 12px; }
</style>
