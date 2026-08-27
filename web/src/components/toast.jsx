// Toast 轻提示:替代 window.alert 的自制弹层——不阻塞页面、样式跟随主题、可全局调用。
// 用法:import { toast } from '../../components/toast'; toast('文案', 3000)
// 同一时刻只留一条,新提示顶掉旧的;时长默认 2.6s,到时自动淡出移除。
let host = null
let timer = null

export function toast(msg, ms = 2600) {
  if (typeof document === 'undefined') return
  if (!host) {
    host = document.createElement('div')
    host.className = 'toast-host'
    document.body.appendChild(host)
  }
  // 新提示顶掉旧提示,避免堆叠
  host.innerHTML = ''
  clearTimeout(timer)
  const el = document.createElement('div')
  el.className = 'toast'
  el.textContent = msg
  host.appendChild(el)
  requestAnimationFrame(() => el.classList.add('show'))
  timer = setTimeout(() => {
    el.classList.remove('show')
    setTimeout(() => el.remove(), 250)
  }, ms)
}
