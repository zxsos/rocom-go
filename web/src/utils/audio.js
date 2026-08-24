// 提示音工具:Web Audio 合成,不依赖音频资源文件。AudioContext 需在用户手势后创建,
// 首个开关点击即手势;失败(无设备/被拦截)时静默跳过,不影响页面其它功能。

let ctx = null
function ac() {
  if (!ctx) {
    const AC = window.AudioContext || window.webkitAudioContext
    if (!AC) return null
    ctx = new AC()
  }
  if (ctx.state === 'suspended') ctx.resume()
  return ctx
}

// beep 按音符序列播一段提示音:notes = [[频率, 起始秒], ...],每音包络固定 0.3s。
function beep(notes) {
  try {
    const c = ac()
    if (!c) return
    const now = c.currentTime
    for (const [f, t] of notes) {
      const osc = c.createOscillator()
      const g = c.createGain()
      osc.type = 'sine'
      osc.frequency.value = f
      g.gain.setValueAtTime(0.001, now + t)
      g.gain.exponentialRampToValueAtTime(0.28, now + t + 0.02)
      g.gain.exponentialRampToValueAtTime(0.001, now + t + 0.3)
      osc.connect(g).connect(c.destination)
      osc.start(now + t)
      osc.stop(now + t + 0.35)
    }
  } catch { /* 音频不可用:静默跳过 */ }
}

// chime 三连上行「叮咚」(880 → 1174.66 → 1567.98 Hz),普通高亮命中。
export function chime() { beep([[880, 0], [1174.66, 0.12], [1567.98, 0.24]]) }

// rareChime 高八度「叮咚」(1567.98 → 2093 → 2637.02 Hz),异色/炫彩等超稀有命中,更尖更醒目。
export function rareChime() { beep([[1567.98, 0], [2093, 0.12], [2637.02, 0.24]]) }
