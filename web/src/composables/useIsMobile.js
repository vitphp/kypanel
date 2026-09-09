import { ref } from 'vue'

const isMobile = ref(typeof window !== 'undefined' && window.innerWidth < 768)

if (typeof window !== 'undefined') {
  const update = () => {
    isMobile.value = window.innerWidth < 768
  }
  window.addEventListener('resize', update)
  update()
}

export function useIsMobile() {
  return { isMobile }
}

// 手机端操作列宽度：按按钮数量计算刚好容纳 2 行（4+ 时换行）的宽度。
// 每个按钮约 34px，间隙 2px，再留 8px 余量，避免文字被截断。
export function opMobileWidth(count) {
  if (count <= 0) return 48
  const maxPerRow = count <= 3 ? count : Math.ceil(count / 2)
  return maxPerRow * 34 + (maxPerRow - 1) * 2 + 8
}
