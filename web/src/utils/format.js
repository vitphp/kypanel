// 通用格式化工具（抽出完全一致、可安全复用的格式化函数）
// 注意：各组件里历史遗留的 formatBytes/formatTime 实现细节有差异，
// 为避免改变显示行为，这里只沉淀「语义完全一致」的版本，差异版本保持原地不动。

// fmtTimeISO 将后端返回的 ISO 时间串（如 "2024-01-01T12:00:00+08:00"）
// 格式化为 "YYYY-MM-DD HH:mm:ss"。空值返回 '-'。
export function fmtTimeISO(t) {
  if (!t) return '-'
  return String(t).replace('T', ' ').slice(0, 19)
}

// fmtTimeStamp 将时间戳（毫秒）格式化为 "YYYY-MM-DD HH:mm:ss"。
// 空值返回 '-'。
export function fmtTimeStamp(t) {
  if (!t) return '-'
  const d = new Date(t)
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

// formatBytes 统一的字节数格式化函数（1 位小数，B 无小数）。
// 用参数覆盖历史各组件里的差异点，替换时传对应参数保持空值/单位语义：
//   - empty: 空值/0 值时的返回值（'-' 或 '0 B'）
//   - withTB: 是否支持 TB 单位（默认 false，最大到 GB）
//   - acceptString: 是否接受字符串数字输入（默认 false）
//   - zeroIsEmpty: 0 是否按"空"处理（默认 false，即 0 显示 "0 B"）
export function formatBytes(n, opts = {}) {
  const { empty = '-', withTB = false, acceptString = false, zeroIsEmpty = false } = opts

  if (acceptString) {
    if (n === undefined || n === null) return empty
    const num = Number(n)
    if (Number.isNaN(num)) return n
    n = num
  }

  if (n == null || (zeroIsEmpty && n === 0)) return empty
  if (n === 0) return '0 B'

  const units = withTB ? ['B', 'KB', 'MB', 'GB', 'TB'] : ['B', 'KB', 'MB', 'GB']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(1)} ${units[i]}`
}

