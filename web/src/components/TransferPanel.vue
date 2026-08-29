<template>
  <Teleport to="body">
    <!-- 传输任务弹窗（始终居中显示） -->
    <el-dialog
      v-model="dialogVisible"
      title="传输任务"
      width="780px"
      align-center
      :close-on-click-modal="false"
      :close-on-press-escape="false"
      :show-close="true"
      :modal="true"
      class="transfer-dialog"
      :before-close="handleBeforeClose"
      :top="dialogTop"
    >
      <!-- 拖拽上传：拖文件到弹窗内任意位置 -->
      <div
        class="tp-dropzone"
        :class="{ 'is-dragover': dragOver }"
        @dragenter.prevent.stop="onDragEnter"
        @dragover.prevent.stop="onDragOver"
        @dragleave.prevent.stop="onDragLeave"
        @drop.prevent.stop="onDrop"
      >
        <div class="tp-body">
          <el-empty v-if="store.tasks.length === 0" :image-size="50" description="暂无传输任务（可拖入文件上传）" />
          <div v-else class="tp-list">
            <div v-for="t in store.tasks" :key="t.id" class="tp-item" :class="`tp-${t.status}`">
              <div class="tp-item-head">
                <el-icon class="tp-item-icon" :class="t.type">
                  <UploadFilled v-if="t.type === 'upload'" />
                  <Download v-else />
                </el-icon>
                <div class="tp-item-name">
                  <div class="tp-name" :title="t.name">{{ t.name }}</div>
                  <div class="tp-dir" :title="t.dir">
                    {{ t.type === 'upload' ? '上传到' : '下载到' }} {{ t.dir || '/' }}
                  </div>
                </div>
                <div class="tp-item-status">
                  <span class="tp-percent">{{ percent(t) }}%</span>
                  <span v-if="t.status === 'running' && t.speed" class="tp-speed">{{ fmtSpeed(t.speed) }}</span>
                </div>
              </div>

              <el-progress
                :percentage="percent(t)"
                :status="progressStatus(t)"
                :stroke-width="6"
                :show-text="false"
              />

              <div v-if="t.status === 'error'" class="tp-error-text">{{ t.error || '任务失败' }}</div>

              <div class="tp-item-actions">
                <template v-if="t.status === 'running'">
                  <el-button v-if="t.type === 'upload'" size="small" text @click="store.pauseTask(t.id)">
                    <el-icon><VideoPause /></el-icon> 暂停
                  </el-button>
                  <el-button size="small" text type="danger" @click="cancelTask(t)">
                    <el-icon><Close /></el-icon> 取消
                  </el-button>
                </template>

                <template v-else-if="t.status === 'pending'">
                  <el-button size="small" text type="danger" @click="cancelTask(t)">
                    <el-icon><Close /></el-icon> 取消
                  </el-button>
                </template>

                <template v-else-if="t.status === 'paused'">
                  <el-button size="small" text type="primary" @click="store.resumeTask(t.id)">
                    <el-icon><VideoPlay /></el-icon> 继续
                  </el-button>
                  <el-button size="small" text type="danger" @click="cancelTask(t)">
                    <el-icon><Close /></el-icon> 取消
                  </el-button>
                </template>

                <template v-else-if="t.status === 'error' || t.status === 'canceled'">
                  <el-button v-if="t.type === 'upload'" size="small" text type="primary" @click="store.retryTask(t.id)">
                    <el-icon><Refresh /></el-icon> 重试
                  </el-button>
                  <el-button size="small" text @click="store.removeTask(t.id)">
                    <el-icon><Delete /></el-icon> 移除
                  </el-button>
                </template>

                <template v-else>
                  <el-button size="small" text @click="store.removeTask(t.id)">
                    <el-icon><Delete /></el-icon> 移除
                  </el-button>
                </template>
              </div>
            </div>
          </div>
        </div>

        <!-- 拖拽上传的视觉提示（覆盖层） -->
        <div v-if="dragOver" class="tp-drop-hint">
          <el-icon :size="36"><UploadFilled /></el-icon>
          <div>松手即可加入传输队列</div>
        </div>
      </div>

      <template #footer>
        <div class="tp-footer">
          <span class="tp-footer-stats">
            <template v-if="store.runningCount">传输中 {{ store.runningCount }}</template>
            <template v-if="pendingCount"> · 排队 {{ pendingCount }}</template>
            <template v-if="store.totalSpeed"> · {{ fmtSpeed(store.totalSpeed) }}</template>
          </span>
          <div class="tp-footer-btns">
            <el-button size="small" text @click="store.clearFinished()" :disabled="!hasFinished">清空已结束</el-button>
          </div>
        </div>
      </template>
    </el-dialog>
  </Teleport>
</template>

<script setup>
import { computed, ref } from 'vue'
import { ElMessageBox } from 'element-plus'
import { useTransferStore } from '@/stores/transfer'
import {
  UploadFilled, Download, Close, VideoPause, VideoPlay, Refresh, Delete,
} from '@element-plus/icons-vue'

const store = useTransferStore()

// 弹窗默认关闭；上传/下载任务被加入时会请求打开。
const dialogVisible = ref(false)
// 让弹窗垂直居中：element-plus 的 top prop 只能给 margin-top，无法直接垂直居中。
// 这里给一个非常大的值让其贴底，再用 transform 反向上移自身一半高度（CSS 处理）。
const dialogTop = ref('50vh')

// 上传任务添加后会自动弹出弹窗（这里通过自定义事件监听实现，FileManager 触发）
// 简易方式：store 增加 hasTasks 时自动弹
import { watch } from 'vue'
watch(
  () => store.tasks.length,
  (n, old) => {
    if (n > 0 && n >= (old || 0)) dialogVisible.value = true
  }
)

const pendingCount = computed(() => store.tasks.filter((t) => t.status === 'pending').length)

function percent(t) {
  if (!t.total) return t.status === 'done' ? 100 : 0
  const p = Math.floor((t.loaded / t.total) * 100)
  return Math.max(0, Math.min(100, p))
}

function progressStatus(t) {
  if (t.status === 'done') return 'success'
  if (t.status === 'error' || t.status === 'canceled') return 'exception'
  return ''
}

function fmtSpeed(bps) {
  if (!bps || bps <= 0) return '0 B/s'
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s']
  let v = bps
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return (v >= 100 ? v.toFixed(0) : v.toFixed(1)) + ' ' + units[i]
}

function cancelTask(t) {
  if (t.status === 'done' || t.status === 'error' || t.status === 'canceled') {
    store.removeTask(t.id)
  } else {
    store.cancelTask(t.id)
  }
}

const hasFinished = computed(() =>
  store.tasks.some((t) => t.status === 'done' || t.status === 'error' || t.status === 'canceled')
)

// 关闭前确认：有上传任务在跑时，提示用户
async function handleBeforeClose(done) {
  if (!store.hasActiveUploads) {
    done()
    return
  }
  try {
    await ElMessageBox.confirm(
      '当前还有上传任务正在传输，确定要取消并关闭吗？',
      '关闭传输任务',
      {
        confirmButtonText: '确定取消并关闭',
        cancelButtonText: '继续传输',
        type: 'warning'
      }
    )
    // 用户确认取消：把所有 running/pending 上传任务取消
    store.tasks
      .filter((t) => t.type === 'upload' && (t.status === 'running' || t.status === 'pending'))
      .forEach((t) => store.cancelTask(t.id))
    done()
  } catch (e) {
    // 用户点了"继续传输"
  }
}

// ========== 拖拽上传支持 ==========
const dragOver = ref(false)
let dragDepth = 0

function onDragEnter(e) {
  if (!e.dataTransfer) return
  // 仅响应文件拖拽
  if (!Array.from(e.dataTransfer.types || []).includes('Files')) return
  dragDepth++
  dragOver.value = true
  e.dataTransfer.dropEffect = 'copy'
}
function onDragOver(e) {
  if (!e.dataTransfer) return
  if (!Array.from(e.dataTransfer.types || []).includes('Files')) return
  e.dataTransfer.dropEffect = 'copy'
}
function onDragLeave() {
  dragDepth--
  if (dragDepth <= 0) {
    dragDepth = 0
    dragOver.value = false
  }
}
function onDrop(e) {
  dragDepth = 0
  dragOver.value = false
  const files = Array.from(e.dataTransfer?.files || [])
  if (files.length === 0) return
  // 触发全局上传事件，由 FileManager 监听处理（拿到 currentPath 等上下文）
  window.dispatchEvent(new CustomEvent('lp-transfer-drop-files', { detail: { files } }))
}
</script>

<style>
/* ===== 弹窗：垂直方向完全居中 =====
   element-plus 的 .el-dialog 默认 `margin-top: 4vh !important`（见 DevTools），
   导致 align-center 仅水平居中、垂直方向贴顶。这里强制用 `top` prop + 覆盖
   margin-top，让弹窗从视口中央开始，再用 transform 上移自身一半高度。
   仅作用于 .transfer-dialog.el-dialog（transfer-dialog 是 Teleport 容器上的
   class），不会污染其他 dialog。
*/
.transfer-dialog.el-dialog {
  margin-top: 50vh !important;
  transform: translateY(-50%) !important;
}
</style>

<style scoped>
/* ===== 弹窗内部样式（仅作用于 TransferPanel 自身） ===== */
.transfer-dialog :deep(.el-dialog__body) {
  padding: 8px 16px 4px 16px;
}

.tp-dropzone {
  position: relative;
  min-height: 80px;
  border-radius: 6px;
  transition: background-color 0.15s;
}
.tp-dropzone.is-dragover {
  background: rgba(64, 158, 255, 0.06);
  outline: 2px dashed #409eff;
  outline-offset: -4px;
}
.tp-drop-hint {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  background: rgba(255, 255, 255, 0.85);
  color: #409eff;
  font-size: 13px;
  pointer-events: none;
  border-radius: 6px;
}

.tp-body { max-height: 60vh; overflow-y: auto; padding: 4px 0; }
.tp-list { display: flex; flex-direction: column; }
.tp-item {
  padding: 10px 4px;
  border-bottom: 1px solid #f2f4f7;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.tp-item:last-child { border-bottom: 0; }
.tp-item-head { display: flex; align-items: center; gap: 8px; }
.tp-item-icon { flex: 0 0 22px; width: 22px; height: 22px; display: flex; align-items: center; justify-content: center; }
.tp-item-icon.upload { color: #409eff; }
.tp-item-icon.download { color: #67c23a; }
.tp-item-name { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.tp-name { font-size: 13px; color: #303133; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.tp-dir { font-size: 12px; color: #909399; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.tp-item-status { flex: 0 0 auto; display: flex; align-items: center; gap: 6px; }
.tp-percent { font-size: 13px; color: #303133; font-variant-numeric: tabular-nums; }
.tp-speed { font-size: 12px; color: #909399; font-variant-numeric: tabular-nums; }
.tp-error-text { font-size: 12px; color: #f56c6c; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.tp-item-actions { display: flex; align-items: center; gap: 8px; }

.tp-footer { display: flex; align-items: center; justify-content: space-between; width: 100%; }
.tp-footer-stats { font-size: 12.5px; color: #909399; }
.tp-footer-btns { display: flex; align-items: center; gap: 4px; }
</style>