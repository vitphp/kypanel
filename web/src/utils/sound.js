// Web Audio 音效工具（无第三方依赖）
// 「卖点提示」音效取自 beep-tester.html：E5(660) → A4(440) 下行两声
let _audioCtx = null
function ctx() {
  if (!_audioCtx) _audioCtx = new (window.AudioContext || window.webkitAudioContext)()
  if (_audioCtx.state === 'suspended') _audioCtx.resume()
  return _audioCtx
}

// 基本音符：正弦波 + 指数音量包络
function note(c, freq, start, dur, vol, type) {
  const o = c.createOscillator()
  const g = c.createGain()
  o.type = type || 'sine'
  o.frequency.value = freq
  g.gain.setValueAtTime(0.0001, c.currentTime + start)
  g.gain.exponentialRampToValueAtTime(vol, c.currentTime + start + 0.02)
  g.gain.exponentialRampToValueAtTime(0.0001, c.currentTime + start + dur)
  o.connect(g)
  g.connect(c.destination)
  o.start(c.currentTime + start)
  o.stop(c.currentTime + start + dur + 0.08)
}

// 播放「卖点提示」音效（软件安装/卸载失败时调用）
export function playFailSound() {
  try {
    const c = ctx()
    note(c, 660, 0, 0.2, 0.25)
    note(c, 440, 0.22, 0.3, 0.25)
  } catch (e) {
    /* 音效异常不影响功能 */
  }
}

// 播放「完成」音效（所有安装任务全部完成时调用，上行 C5→E5→G5 三和弦）
export function playSuccessSound() {
  try {
    const c = ctx()
    note(c, 523.25, 0.00, 0.18, 0.22, 'triangle')   // C5
    note(c, 659.25, 0.10, 0.18, 0.22, 'triangle')   // E5
    note(c, 783.99, 0.20, 0.36, 0.22, 'triangle')   // G5
  } catch (e) {
    /* 音效异常不影响功能 */
  }
}

// 通知音效（新任务开始时短促一声"叮"）
export function playStartSound() {
  try {
    const c = ctx()
    note(c, 880, 0, 0.12, 0.18, 'sine')             // A5
  } catch (e) {
    /* 音效异常不影响功能 */
  }
}
