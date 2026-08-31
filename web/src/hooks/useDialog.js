import { useEffect, useRef } from 'react'

// 可聚焦元素的选择器(顺序与浏览器 Tab 顺序一致)。
const FOCUSABLE = [
  'a[href]', 'button:not([disabled])', 'input:not([disabled])',
  'select:not([disabled])', 'textarea:not([disabled])', '[tabindex]:not([tabindex="-1"])',
].join(',')

// 可见性用 getClientRects() 判,不用 offsetParent !== null:
// 后者对 position: fixed 的元素恒为 null,而弹窗容器**本身**就是 fixed,
// 会把容器里的固定定位元素(如工具栏)误判成不可聚焦。
const focusables = (root) =>
  [...root.querySelectorAll(FOCUSABLE)].filter((el) => el.getClientRects().length > 0)

// useDialog 弹窗焦点管理(P5.4.3),与 useScrollLock 同一套路(开→生效,关→还原):
//   1. 打开时把焦点移进弹窗(组件已自行聚焦某个输入时不抢);
//   2. Tab / Shift+Tab 在弹窗内循环,不跑到背后的页面上;
//   3. 关闭后把焦点还给打开它的那个元素。
//
// 为什么必须自己管:不管的话,键盘用户 Tab 会走进弹窗**背后**的页面 —— 视觉上
// 弹窗盖在最上层,焦点却在看不见的地方,按 Enter 触发的是背后的按钮(删数据、
// 切账号都可能)。这是「焦点迷失」,鼠标用户完全感知不到、键盘用户寸步难行。
//
// 用法:ref 与 dialogProps 都挂到弹窗的**最外层容器**(遮罩)上。
// Esc / 点击遮罩关闭由各弹窗自己处理(本 hook 不接管,避免重复绑定)。
export function useDialog(label) {
  const ref = useRef(null)

  // 焦点进弹窗 / 关闭后归还。
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const restore = document.activeElement
    // 组件自己已聚焦了某个元素(如 PinDialog 的 PIN 输入框)就别抢,保持它的选择。
    if (!el.contains(document.activeElement)) {
      const first = focusables(el)[0]
      ;(first || el).focus() // 没有可聚焦元素时聚焦容器自身(靠 tabIndex=-1)
    }
    return () => {
      // 触发弹窗的元素可能已随数据一起消失(如删掉的账号行),那时 focus() 是空操作、
      // 焦点落到 body —— 也比「停在一个已移除的节点上」好。
      if (restore && typeof restore.focus === 'function' && document.contains(restore)) restore.focus()
    }
  }, [])

  // Tab 圈定在弹窗内。监听挂在 document 上而非容器:焦点一旦意外跑出容器
  // (浏览器地址栏绕回、外部脚本聚焦),挂在容器上就再也拦不住了。
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const onKey = (e) => {
      if (e.key !== 'Tab') return
      const nodes = focusables(el)
      if (nodes.length === 0) { e.preventDefault(); return }
      const first = nodes[0]
      const last = nodes[nodes.length - 1]
      const active = document.activeElement
      // 焦点不在弹窗内时,一律拉回弹窗(而非放任它继续在背后页面走)。
      if (!el.contains(active)) { e.preventDefault(); (e.shiftKey ? last : first).focus() }
      else if (e.shiftKey && active === first) { e.preventDefault(); last.focus() }
      else if (!e.shiftKey && active === last) { e.preventDefault(); first.focus() }
    }
    document.addEventListener('keydown', onKey, true)
    return () => document.removeEventListener('keydown', onKey, true)
  }, [])

  // aria-modal 声明「这是模态的」,读屏据此忽略背后的内容;
  // tabIndex=-1 让容器**可程序聚焦**(上面没有可聚焦元素时的兜底),但不进 Tab 序列。
  return { ref, dialogProps: { role: 'dialog', 'aria-modal': 'true', tabIndex: -1, 'aria-label': label } }
}
