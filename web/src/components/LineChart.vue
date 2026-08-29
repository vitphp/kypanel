<template>
  <div class="line-chart">
    <svg viewBox="0 0 600 180" style="width: 100%; height: auto; display: block">
      <!-- 网格 -->
      <line v-for="t in yTicks" :key="'y' + t.y" :x1="PAD" :y1="t.y" :x2="W - PAD" :y2="t.y" stroke="#ebeef5" stroke-width="1" />
      <text v-for="t in yTicks" :key="'l' + t.y" :x="PAD + 2" :y="t.y - 3" fill="#c0c4cc" font-size="10">{{ t.label }}</text>

      <!-- 面积 -->
      <path v-if="area" :d="area" :fill="color" opacity="0.08" />
      <!-- 第三条线 -->
      <polyline v-if="path3" :points="path3" fill="none" :stroke="thirdColor" stroke-width="2" stroke-dasharray="2 2" />
      <!-- 第二条线 -->
      <polyline v-if="path2" :points="path2" fill="none" :stroke="secondColor" stroke-width="2" stroke-dasharray="4 3" />
      <!-- 主线 -->
      <polyline v-if="mainPath" :points="mainPath" fill="none" :stroke="color" stroke-width="2" stroke-linejoin="round" />

      <!-- X 轴时间 -->
      <text v-for="t in xTicks" :key="'x' + t.x" :x="t.x" y="178" fill="#c0c4cc" font-size="10" text-anchor="middle">{{ t.label }}</text>
    </svg>
  </div>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  data: { type: Array, default: () => [] },
  max: { type: Number, default: 100 },
  color: { type: String, default: '#409eff' },
  labels: { type: Array, default: () => [] },
  second: { type: Array, default: null },
  secondColor: { type: String, default: '#909399' },
  third: { type: Array, default: null },
  thirdColor: { type: String, default: '#e6a23c' }
})

const W = 600
const H = 180
const PAD = 8

function buildPath(data) {
  if (!data || data.length < 2) return ''
  const innerW = W - PAD * 2
  const innerH = H - PAD * 2
  const safeMax = props.max <= 0 ? 1 : props.max
  return data.map((v, i) => {
    const x = PAD + (i / (data.length - 1)) * innerW
    const y = PAD + innerH - (Math.min(v, safeMax) / safeMax) * innerH
    return `${i === 0 ? '' : ' '}${x.toFixed(1)},${y.toFixed(1)}`
  }).join('')
}

const mainPath = computed(() => buildPath(props.data))
const path2 = computed(() => (props.second ? buildPath(props.second) : ''))
const path3 = computed(() => (props.third ? buildPath(props.third) : ''))
const area = computed(() => {
  const p = mainPath.value
  if (!p) return ''
  // SVG <path> 的 d 必须以 moveto(M) 开头，否则浏览器报
  // "Expected moveto path command" 且面积层不渲染
  return `M ${p} L ${W - PAD} ${H - PAD} L ${PAD} ${H - PAD} Z`
})

const yTicks = computed(() => {
  return [0, 25, 50, 75, 100].map((v) => ({
    y: PAD + (1 - v / 100) * (H - PAD * 2),
    label: (props.max * v / 100).toFixed(0)
  }))
})

const xTicks = computed(() => {
  const n = props.labels.length
  if (n < 2) return []
  const ticks = []
  const step = Math.max(1, Math.floor(n / 5))
  for (let i = 0; i < n; i += step) {
    ticks.push({ x: PAD + (i / (n - 1)) * (W - PAD * 2), label: props.labels[i] })
  }
  ticks.push({ x: W - PAD, label: props.labels[n - 1] })
  return ticks
})
</script>

<style scoped>
.line-chart { margin-top: 4px; }
</style>
