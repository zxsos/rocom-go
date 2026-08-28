// Confirm 确认框:替代 window.confirm 的自制弹层——样式跟随主题、可全局调用。
// 用法:import { confirmDialog } from '../../components/confirm'
//      if (!(await confirmDialog({ message: '文案', okText: '确定', danger: true }))) return
// 返回 Promise<boolean>(点确定 true,取消/点背景/Esc false)。
// 同一时刻只留一个,新确认顶掉旧的(旧的一律视为取消)。
let backdrop = null

export function confirmDialog({ message = '', okText = '确定', cancelText = '取消', danger = false } = {}) {
  if (typeof document === 'undefined') return Promise.resolve(false)
  if (backdrop) close(false) // 已有打开中的确认框:旧的按取消处理
  return new Promise((resolve) => {
    const bd = document.createElement('div')
    bd.className = 'confirm-backdrop'
    const dlg = document.createElement('div')
    dlg.className = 'confirm-dialog'
    const msg = document.createElement('div')
    msg.className = 'confirm-message'
    msg.textContent = message
    const actions = document.createElement('div')
    actions.className = 'confirm-actions'
    const cancel = document.createElement('button')
    cancel.className = 'btn'
    cancel.textContent = cancelText
    const ok = document.createElement('button')
    ok.className = 'btn primary' + (danger ? ' confirm-danger' : '')
    ok.textContent = okText
    actions.append(cancel, ok)
    dlg.append(msg, actions)
    bd.appendChild(dlg)
    document.body.appendChild(bd)
    backdrop = bd

    const onKey = (e) => { if (e.key === 'Escape') { e.stopPropagation(); close(false) } }
    document.addEventListener('keydown', onKey)
    bd.addEventListener('click', (e) => { if (e.target === bd) close(false) })
    cancel.addEventListener('click', () => close(false))
    ok.addEventListener('click', () => close(true))

    function close(val) {
      document.removeEventListener('keydown', onKey)
      bd.classList.remove('show')
      setTimeout(() => {
        if (backdrop === bd) backdrop = null
        bd.remove()
        resolve(val)
      }, 160)
    }

    requestAnimationFrame(() => bd.classList.add('show'))
    ok.focus() // 回车即确定,贴近系统确认框习惯
  })
}
