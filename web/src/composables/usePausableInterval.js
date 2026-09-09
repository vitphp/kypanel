import { onMounted, onBeforeUnmount } from 'vue'

// 可暂停的轮询定时器：标签页切到后台（document.hidden）时暂停，切回前台立即恢复并补刷一次。
// 目的：避免面板停在某个常驻轮询页面、又切到其它标签页时，仍按原间隔持续打后端
// （浏览器虽会节流后台 setInterval，但仍会打到接口，白耗服务器资源与请求）。
//
// 用法：
//   const stop = usePausableInterval(loadFn, 5000)
//   // stop() 可选地手动终止（组件卸载会自动清理）
export function usePausableInterval(fn, intervalMs) {
  let timer = null
  const start = () => {
    if (timer) return
    timer = setInterval(fn, intervalMs)
  }
  const stop = () => {
    if (timer) {
      clearInterval(timer)
      timer = null
    }
  }
  const onVisibility = () => {
    if (document.hidden) {
      stop()
    } else {
      // 回到前台：立即补刷一次，避免图表/数值断层
      fn()
      start()
    }
  }

  onMounted(() => {
    start()
    document.addEventListener('visibilitychange', onVisibility)
  })
  onBeforeUnmount(() => {
    stop()
    document.removeEventListener('visibilitychange', onVisibility)
  })

  return stop
}
