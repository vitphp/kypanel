<template>
  <div class="trash-page">
    <el-card shadow="never">
      <!-- 工具栏 -->
      <div class="trash-toolbar">
        <div class="trash-tip">
          <el-icon><Delete /></el-icon>&nbsp;文件删除后进入回收站，可在此还原或彻底删除
        </div>
        <div class="spacer" />
        <el-button type="primary" plain @click="goFiles">
          <el-icon><Back /></el-icon>&nbsp;返回文件
        </el-button>
        <el-button v-if="items.length > 0" type="success" plain :disabled="selectedIds.length === 0" @click="restoreSelected">
          <el-icon><RefreshLeft /></el-icon>&nbsp;还原所选
        </el-button>
        <el-button v-if="items.length > 0" type="danger" plain :disabled="selectedIds.length === 0" @click="purgeSelected">
          <el-icon><DeleteFilled /></el-icon>&nbsp;彻底删除所选
        </el-button>
        <el-button v-if="items.length > 0" type="danger" @click="emptyTrash">
          <el-icon><DeleteFilled /></el-icon>&nbsp;清空回收站
        </el-button>
      </div>

      <!-- 列表 -->
      <el-table :data="items" v-loading="loading" stripe @selection-change="onSelectionChange">
        <el-table-column type="selection" width="42" />
        <el-table-column label="名称" min-width="220">
          <template #default="{ row }">
            <span class="trash-name">
              <el-icon :color="row.type === 'dir' ? '#e6a23c' : '#909399'">
                <Folder v-if="row.type === 'dir'" />
                <Document v-else />
              </el-icon>
              {{ row.name }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="类型" width="80" align="center">
          <template #default="{ row }">
            <el-tag size="small" :type="row.type === 'dir' ? 'warning' : 'info'">
              {{ row.type === 'dir' ? '目录' : '文件' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="原始位置" min-width="240" show-overflow-tooltip>
          <template #default="{ row }">
            <span class="origin-path">{{ row.origin_path }}</span>
          </template>
        </el-table-column>
        <el-table-column label="大小" width="100">
          <template #default="{ row }">{{ formatSize(row.size) }}</template>
        </el-table-column>
        <el-table-column label="删除时间" width="170">
          <template #default="{ row }">{{ fmtTimeISO(row.deleted_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" min-width="120" align="center">
          <template #default="{ row }">
            <div class="ops-cell">
              <el-button link type="success" @click="restoreOne(row)">还原</el-button>
              <el-button link type="danger" @click="purgeOne(row)">彻底删除</el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && items.length === 0" description="回收站是空的" />
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import request from '../utils/request'
import { formatBytes } from '../utils/format'
import { fmtTimeISO } from '../utils/format'

const router = useRouter()
const items = ref([])
const loading = ref(false)
const selectedRows = ref([])

const selectedIds = ref([])

function formatSize(n) {
  return formatBytes(n)
}

async function load() {
  loading.value = true
  try {
    const res = await request.get('/file/trash/list')
    items.value = res.data || []
  } catch (e) {
    // request 层已提示
  } finally {
    loading.value = false
  }
}

function onSelectionChange(rows) {
  selectedRows.value = rows
  selectedIds.value = rows.map(r => r.id)
}

function goFiles() {
  router.push('/files')
}

async function restoreOne(row) {
  try {
    await request.post('/file/trash/restore', { id: row.id })
    ElMessage.success('已还原')
    load()
  } catch (e) { /* 已提示 */ }
}

async function purgeOne(row) {
  try {
    await ElMessageBox.confirm(`彻底删除「${row.name}」后将无法恢复，确定吗？`, '彻底删除', {
      type: 'warning', confirmButtonText: '彻底删除', cancelButtonText: '取消'
    })
    await request.post('/file/trash/purge', { id: row.id })
    ElMessage.success('已彻底删除')
    load()
  } catch (e) {
    if (e !== 'cancel') { /* 已提示 */ }
  }
}

async function restoreSelected() {
  try {
    await ElMessageBox.confirm(`确定还原选中的 ${selectedIds.value.length} 项吗？`, '批量还原', {
      type: 'info', confirmButtonText: '还原', cancelButtonText: '取消'
    })
    for (const id of selectedIds.value) {
      try { await request.post('/file/trash/restore', { id }) } catch (e) { /* 单条失败继续 */ }
    }
    ElMessage.success('还原完成')
    load()
  } catch (e) { /* 取消 */ }
}

async function purgeSelected() {
  try {
    await ElMessageBox.confirm(`彻底删除选中的 ${selectedIds.value.length} 项后将无法恢复，确定吗？`, '批量彻底删除', {
      type: 'warning', confirmButtonText: '彻底删除', cancelButtonText: '取消'
    })
    for (const id of selectedIds.value) {
      try { await request.post('/file/trash/purge', { id }) } catch (e) { /* 单条失败继续 */ }
    }
    ElMessage.success('已彻底删除')
    load()
  } catch (e) { /* 取消 */ }
}

async function emptyTrash() {
  try {
    await ElMessageBox.confirm(`回收站中共 ${items.value.length} 项，清空后将全部无法恢复，确定吗？`, '清空回收站', {
      type: 'warning', confirmButtonText: '清空', cancelButtonText: '取消'
    })
    await request.post('/file/trash/empty')
    ElMessage.success('回收站已清空')
    load()
  } catch (e) {
    if (e !== 'cancel') { /* 已提示 */ }
  }
}

onMounted(load)
</script>

<style scoped>
.trash-toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 16px;
  flex-wrap: wrap;
}
.trash-tip {
  display: flex;
  align-items: center;
  font-size: 13px;
  color: #909399;
}
.spacer { flex: 1; }
.trash-name {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: #303133;
}
.origin-path {
  font-size: 12px;
  color: #909399;
  font-family: 'Consolas', monospace;
}
</style>
