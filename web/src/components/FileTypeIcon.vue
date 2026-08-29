<!--
  文件类型图标（自绘彩色 SVG，无第三方依赖）
  --------------------------------------------------
  用法：
    <FileTypeIcon :name="row.name" :is-dir="row.is_dir" />
    <FileTypeIcon :name="row.name" :is-dir="row.is_dir" :size="24" />
  --------------------------------------------------
  - 文件夹：金色文件夹 SVG（自带形状，与文件区分）
  - 文件  ：按扩展名查 FILE_PALETTE，没有匹配则通用灰文件图标
-->
<template>
  <span class="fti" :style="boxStyle" :title="title">
    <!-- 文件夹 -->
    <svg
      v-if="isDir"
      :width="size"
      :height="size * 0.92"
      viewBox="0 0 64 59"
      xmlns="http://www.w3.org/2000/svg"
      aria-hidden="true"
    >
      <path
        :fill="pal.fold"
        d="M2 14.5C2 11.5 4.5 9 7.5 9 H22 L26 5 H56.5C59.5 5 62 7.5 62 10.5V14H2 V14.5Z"
      />
      <path
        :fill="pal.body"
        d="M2 14 H62 V50.5C62 53.5 59.5 56 56.5 56 H7.5C4.5 56 2 53.5 2 50.5V14 Z"
      />
      <!-- 顶部微反光 -->
      <path
        :fill="pal.fold"
        d="M2 14 H62 V18 H2 Z"
        opacity="0.55"
      />
    </svg>

    <!-- 普通文件 -->
    <svg
      v-else
      :width="size"
      :height="size * 1.08"
      viewBox="0 0 24 26"
      xmlns="http://www.w3.org/2000/svg"
      aria-hidden="true"
    >
      <!-- 主体 -->
      <path
        :fill="pal.body"
        d="M3 1.5 H16 L22.5 8 V22.5 C22.5 23.88 21.38 25 20 25 H4 C2.62 25 1.5 23.88 1.5 22.5 V4 C1.5 2.62 2.62 1.5 4 1.5 Z"
      />
      <!-- 折角 -->
      <path
        :fill="pal.fold"
        d="M16 1.5 V7 C16 7.55 16.45 8 17 8 H22.5 L16 1.5 Z"
      />
      <!-- 底部标签条（让上半/下半略作色差） -->
      <path
        :fill="pal.fold"
        d="M1.5 18 H22.5 V22.5 C22.5 23.88 21.38 25 20 25 H4 C2.62 25 1.5 23.88 1.5 22.5 V18 Z"
        opacity="0.32"
      />
      <!-- 中央文字标签 -->
      <text
        :fill="pal.text"
        x="12"
        y="14.5"
        text-anchor="middle"
        dominant-baseline="middle"
        font-size="6.6"
        font-weight="800"
        font-family="ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, 'Roboto Mono', monospace"
        style="letter-spacing: 0.2px;"
      >{{ pal.label }}</text>
    </svg>
  </span>
</template>

<script setup>
import { computed } from 'vue'
import { FILE_PALETTE, FALLBACK_PALETTE, FOLDER_PALETTE, fileExt } from '@/utils/fileIcon'

const props = defineProps({
  name:    { type: String, default: '' },
  isDir:   { type: Boolean, default: false },
  size:    { type: Number, default: 22 }
})

const pal = computed(() => {
  if (props.isDir) return FOLDER_PALETTE
  const ext = fileExt(props.name)
  return FILE_PALETTE[ext] || FALLBACK_PALETTE
})

const boxStyle = computed(() => ({
  display: 'inline-flex',
  alignItems: 'center',
  justifyContent: 'center',
  width: props.size + 'px',
  height: (props.size * 1.12) + 'px',
  lineHeight: 0
}))

const title = computed(() => {
  if (props.isDir) return props.name + ' (目录)'
  const ext = fileExt(props.name)
  return props.name + (ext ? ' · ' + ext.toUpperCase() + ' 文件' : ' · 文件')
})
</script>

<style scoped>
.fti {
  vertical-align: middle;
  flex-shrink: 0;
  user-select: none;
}
.fti svg {
  display: block;
}
</style>
