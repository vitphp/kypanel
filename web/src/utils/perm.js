// 文件权限工具：rwxrwxrwx ↔ 八进制数字
// 9 个位的顺序（与 PERM_BIT_LABELS 一致）：属主(r,w,x) 属组(r,w,x) 其他(r,w,x)

export const PERM_BIT_LABELS = [
  '属主读', '属主写', '属主执行',
  '属组读', '属组写', '属组执行',
  '其他读', '其他写', '其他执行'
]

const BIT_VALUES = [4, 2, 1]

/**
 * 把 9 个布尔位按 (属主,属组,其他) 分组
 * -> Array<[r,w,x], [r,w,x], [r,w,x]>
 */
export function splitPermBits(bits) {
  if (!Array.isArray(bits) || bits.length !== 9) {
    return [[true, true, true], [true, false, true], [true, false, true]]
  }
  return [bits.slice(0, 3), bits.slice(3, 6), bits.slice(6, 9)]
}

/**
 * 把 9 个布尔位转回八进制数字字符串（无前导 0）
 */
export function buildPermFromBits(bits) {
  if (!Array.isArray(bits) || bits.length !== 9) return '755'
  const o = [0, 0, 0]
  for (let i = 0; i < 9; i++) {
    if (bits[i]) o[Math.floor(i / 3)] += BIT_VALUES[i % 3]
  }
  return '' + o[0] + o[1] + o[2]
}

/**
 * 把八进制数字权限字符串（如 "755"、"0755"、"4755"）拆成 9 个布尔位。
 * 仅使用低 3 位（属主/属组/其他）。顺序：属主(r,w,x) 属组(r,w,x) 其他(r,w,x)。
 */
export function parsePermToBits(mode) {
  const raw = (mode || '').toString().trim()
  // 仅取 3 位低权限位（去掉首位特殊位如 SetUID/SetGID/Sticky）
  const m = raw.replace(/^0+(?=[0-7])/, '')
  const padded = (m.length < 3 ? m.padStart(3, '0') : m.slice(-3))
  if (!/^[0-7]{3}$/.test(padded)) {
    return [true, true, true, true, false, true, true, false, true] // 默认 755
  }
  const digits = padded.split('').map((d) => parseInt(d, 10))
  const bits = []
  for (const d of digits) {
    bits.push((d & 4) !== 0)
    bits.push((d & 2) !== 0)
    bits.push((d & 1) !== 0)
  }
  return bits
}

/**
 * 把 9 个布尔位渲染为 rwxr-xr-x 字符串
 */
export function bitsToStr(bits) {
  if (!Array.isArray(bits) || bits.length !== 9) return '----------'
  const chars = ['r', 'w', 'x']
  let out = ''
  for (let i = 0; i < 9; i++) {
    out += bits[i] ? chars[i % 3] : '-'
  }
  return out
}

/**
 * rwxr-xr-x 字符串 ↔ 八进制数字
 */
const RWX_MAP = { r: 4, w: 2, x: 1, '-': 0 }
export function rwxToOctal(s) {
  if (typeof s !== 'string') return ''
  const t = s.toLowerCase()
  if (!/^[rwx-]{9}$/.test(t)) return ''
  let v = ''
  for (let i = 0; i < 3; i++) {
    const seg = t.slice(i * 3, i * 3 + 3)
    v += (RWX_MAP[seg[0]] + RWX_MAP[seg[1]] + RWX_MAP[seg[2]])
  }
  return v
}
