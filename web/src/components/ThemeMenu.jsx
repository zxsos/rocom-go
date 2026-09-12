import React, { useEffect, useLayoutEffect, useRef } from 'react'
import { useOutsideClick } from '../hooks/useDropdown'
import { IconCheck } from './svg'

// 主题选择菜单:两行 —— 「风格」与「明暗」,点哪项就是哪项(不是循环切换)。
//
// 两个入口共用它(顶栏的版本号与主题图标,见 App.jsx):开合状态**由调用方持有**,
// 于是两份入口天然同步 —— 从任一处打开的都是同一个面板,不存在两套状态要对齐。
//
// 为什么是两行而不是平铺一张「四套主题」的清单:
//   用户脑子里的模型是两件事 —— 「我要洛克那种感觉」+「我这会儿想要浅色」;
//   而「跟随系统」不是第五套主题,它是「明暗这一轴交给系统」,平铺清单里无处安放。
//   两行还让「经典的跟随系统」与「洛克的跟随系统」各归其位(见 useTheme 的注释)。
//
// 无障碍:选项都是真 <button> + aria-pressed,Tab/Enter 由浏览器负责,不自己写方向键导航;
// Esc 收起、点面板外收起(复用 useOutsideClick,与站内其它浮层同一套行为)。
const FAM = [
  { key: 'roco', label: '洛克', hint: '图鉴 · 羊皮纸与金' },
  { key: 'classic', label: '经典', hint: '白天 / 夜间 · 灰调' },
]
const MODE = [
  { key: 'auto', label: '跟随系统', hint: '明暗交给系统设置' },
  { key: 'light', label: '浅色', hint: '固定浅色' },
  { key: 'dark', label: '深色', hint: '固定深色' },
]

export default function ThemeMenu({ theme, onPick, onClose }) {
  const rootRef = useRef(null)
  useOutsideClick(rootRef, onClose, true)

  // 把面板夹回视口内。
  //
  // 面板贴着触发元素展开(左/右锚点由 CSS 按入口分设),但**触发元素未必在视口边缘**:
  // 版本号在顶栏左侧,主题图标在右侧 —— 而手机上「还没有账号」时图标会被挤到中间。
  // 纯 CSS 的 left/right 锚点在这几种排布下都会让面板探出视口(实测:1280px 下版本号
  // 那侧探出 99px、390px 下图标那侧探出 26px),而 CSS 里没有「夹在视口内」这种表达
  // (left: 0 是相对锚点容器,不是相对视口)。故渲染后量一次、推回来。
  //
  // 用 useLayoutEffect 而非 useEffect:必须在**首帧绘制前**定下偏移,否则会看到面板先
  // 探出去、再弹回来。只量一次(面板尺寸固定、打开期间不会变)。
  useLayoutEffect(() => {
    const el = rootRef.current
    if (!el) return
    const r = el.getBoundingClientRect()
    const pad = 8
    let dx = 0
    if (r.left < pad) dx = pad - r.left
    else if (r.right > window.innerWidth - pad) dx = window.innerWidth - pad - r.right
    if (dx) el.style.transform = `translateX(${dx}px)`
  }, [])

  useEffect(() => {
    const onKey = (e) => { if (e.key === 'Escape') onClose() }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  // 两行结构相同,只有数据与「当前值」不同 —— 抽成函数避免两处各写一遍(尤其别让
  // 其中一处忘了 aria-pressed:键盘与读屏用户靠它知道当前选的是哪个)。
  const row = (label, items, cur, build) => (
    <div className="tm-row">
      <span className="tm-label">{label}</span>
      <div className="tm-opts" role="group" aria-label={label}>
        {items.map((it) => (
          <button
            key={it.key}
            type="button"
            className={'tm-opt' + (cur === it.key ? ' on' : '')}
            aria-pressed={cur === it.key}
            title={it.hint}
            onClick={(e) => onPick(build(it.key), e)}
          >
            {/* 勾位**恒定占位**:不给空位留地方的话,选中项一变、同排按钮就会左右错开 */}
            <span className="tm-check" aria-hidden="true">{cur === it.key ? <IconCheck size={12} /> : null}</span>
            <span className="tm-text">{it.label}</span>
          </button>
        ))}
      </div>
    </div>
  )

  return (
    <div className="theme-menu" ref={rootRef} role="dialog" aria-label="选择主题">
      {row('风格', FAM, theme.fam, (fam) => ({ fam, mode: theme.mode }))}
      {row('明暗', MODE, theme.mode, (mode) => ({ fam: theme.fam, mode }))}
    </div>
  )
}
