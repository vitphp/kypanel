// 轻量顶部加载进度条（绿色），无第三方依赖。
// 用法：router.beforeEach 里 progress.start()，afterEach/onError 里 progress.done()。
// 纯 DOM + CSS 实现，挂在 body 顶部，样式见内联。

let el = null
let timer = null
let width = 0
let visible = false

function ensureEl() {
  if (el && document.body.contains(el)) return el
  el = document.createElement('div')
  el.id = 'lp-top-progress'
  el.style.cssText = [
    'position: fixed',
    'top: 0',
    'left: 0',
    'height: 3px',
    'width: 0',
    'background: linear-gradient(90deg, #22c55e, #16a34a)',
    'box-shadow: 0 0 8px rgba(34, 197, 94, 0.6)',
    'z-index: 99999',
    'border-radius: 0 2px 2px 0',
    'transition: width 0.2s ease, opacity 0.25s ease',
    'opacity: 0',
    'pointer-events: none'
  ].join(';')
  document.body.appendChild(el)
  return el
}

function start() {
  const node = ensureEl()
  clearTimeout(timer)
  visible = true
  width = 8
  node.style.opacity = '1'
  node.style.width = width + '%'
  // 模拟渐进加载：不真的跟踪进度，用缓动逼近 90%
  timer = setInterval(() => {
    if (!visible) return
    width += (90 - width) * 0.1
    if (width > 90) width = 90
    node.style.width = width + '%'
  }, 200)
}

function done() {
  visible = false
  clearTimeout(timer)
  const node = ensureEl()
  width = 100
  node.style.width = '100%'
  // 满格后短暂停留再淡出
  setTimeout(() => {
    node.style.opacity = '0'
    setTimeout(() => { node.style.width = '0' }, 250)
  }, 150)
}

export default { start, done }
