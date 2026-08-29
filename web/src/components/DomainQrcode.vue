<template>
  <el-popover
    :width="220"
    placement="bottom"
    :show-arrow="false"
    trigger="hover"
    :hide-after="0"
    popper-class="qr-popover"
    @show="onShow"
  >
    <template #reference>
      <span class="qr-trigger" :title="`扫码访问 ${domain}`" @contextmenu.prevent>
        <!-- 二维码图标（4 个角 + 中央点，纯 SVG 模拟） -->
        <svg width="14" height="14" viewBox="0 0 14 14" fill="currentColor" xmlns="http://www.w3.org/2000/svg">
          <path d="M1 1h5v5H1V1zm1 1v3h3V2H2z" />
          <path d="M8 1h5v5H8V1zm1 1v3h3V2H9z" />
          <path d="M1 8h5v5H1V8zm1 1v3h3V9H2z" />
          <rect x="8" y="8" width="2" height="2" />
          <rect x="11" y="8" width="2" height="2" />
          <rect x="8" y="11" width="2" height="2" />
          <rect x="11" y="11" width="2" height="2" />
        </svg>
      </span>
    </template>
    <div class="qr-card">
      <div class="qr-title">{{ visitHost }}</div>
      <canvas ref="canvasRef" width="200" height="200" class="qr-canvas"></canvas>
      <div class="qr-tip">右键图片可复制 / 保存</div>
    </div>
  </el-popover>
</template>

<script setup>
import { computed, ref } from 'vue'
import QRCode from 'qrcode'

const props = defineProps({
  domain: { type: String, required: true },
  port: { type: Number, default: 80 },
  isHttps: { type: Boolean, default: false }
})

const canvasRef = ref(null)

// 浏览器不能访问通配符域名（*.example.com），跳转到 fan.example.com 作为示例
const visitHost = computed(() => {
  let d = props.domain
  if (d.startsWith('*.')) d = 'fan.' + d.slice(2)
  d = d.split(':')[0]
  return d
})

const visitUrl = computed(() => {
  const proto = props.isHttps ? 'https:' : 'http:'
  const p = props.port === 80 || props.port === 443 ? '' : `:${props.port}`
  return `${proto}//${visitHost.value}${p}`
})

async function onShow() {
  if (!canvasRef.value) return
  try {
    // 按父容器宽度渲染（默认 200），QRCode 库会用 canvas attribute 设实际像素
    const w = canvasRef.value.parentElement?.clientWidth || 200
    const size = Math.max(120, Math.min(w - 8, 220)) // 留 padding，限制最大 220
    await QRCode.toCanvas(canvasRef.value, visitUrl.value, {
      width: size,
      margin: 1,
      color: { dark: '#303133', light: '#ffffff' }
    })
    // CSS 100% 让 canvas 撑满父容器（200px 实际像素自适应显示）
    canvasRef.value.style.width = '100%'
    canvasRef.value.style.height = 'auto'
  } catch (e) {
    console.error('QRCode render failed:', e)
  }
}
</script>

<style scoped>
.qr-trigger {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  vertical-align: middle;
  line-height: 1;
}
.qr-card {
  padding: 4px;
  text-align: center;
  display: flex;
  flex-direction: column;
  align-items: center;
  width: 100%;
  box-sizing: border-box;
}
.qr-title {
  font-size: 12px;
  color: #303133;
  margin-bottom: 2px;
  word-break: break-all;
  text-align: center;
  width: 100%;
  line-height: 16px;
}
.qr-canvas {
  display: block;
  margin: 0 auto;
  align-self: center;
  width: 100%;
}
.qr-tip {
  font-size: 10px;
  color: #909399;
  margin-top: 2px;
  text-align: center;
  width: 100%;
  line-height: 14px;
}
</style>