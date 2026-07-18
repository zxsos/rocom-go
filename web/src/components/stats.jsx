import React from 'react'
import { createPortal } from 'react-dom'
import { IconsContext } from '../context'
import { InlineIcon } from './icons'

// 六维/身高体重等数值展示组件。

const SIX = [
  ['生命', 'hp'],
  ['物攻', 'attack'],
  ['物防', 'defense'],
  ['速度', 'speed'],
  ['魔攻', 'spAttack'],
  ['魔防', 'spDefense'],
]

// Six 渲染六维(纯文字:标签 + 面板值 + 性格 ±10% 升降箭头 + 天分 +N)。列表用。
export function Six({ p }) {
  return (
    <div className="six">
      {SIX.map(([label, key]) => {
        const s = p[key] || {}
        return (
          <div key={key}>
            {label} <b>{s.value ?? 0}</b>
            {s.nature === 1 && <span className="up" title="性格 +10%"> ↑</span>}
            {s.nature === -1 && <span className="down" title="性格 -10%"> ↓</span>}
            {s.talentLv > 0 && <span className="talent" title="天分">+{s.talentLv}</span>}
          </div>
        )
      })}
    </div>
  )
}

// 雷达图六轴顺序(顺时针自顶点):生命→魔攻→魔防→速度→物防→物攻,与游戏内六维雷达一致。
const RADAR_AXES = ['hp', 'spAttack', 'spDefense', 'speed', 'defense', 'attack']
// 绝对刻度:外环对应的面板值上限(与游戏一致,按截图比例定标 219≈44%);超出者夹到外环。
const RADAR_MAX = 500

// NatBadge 右上角性格增减角标:SVG 实心粗箭头(箭头 + 矩形柄,同游戏内,无圆底),
// 绿=增益向上、红=减益向下。
function NatBadge({ dir }) {
  const up = dir === 1
  return (
    <span className={'radar-nat ' + (up ? 'up' : 'down')} title={up ? '性格 +10%' : '性格 -10%'}>
      <svg viewBox="0 0 12 14" aria-hidden="true">
        <path d={up ? 'M6 0L12 6H8.5V14H3.5V6H0Z' : 'M6 14L12 8H8.5V0H3.5V8H0Z'} />
      </svg>
    </span>
  )
}

// StatRadar 以六边形雷达图展示六维(仅图标,不显示中文标签):各顶点=属性图标 + 面板值
// (含性格 ±10% 箭头 / 天分 +N);橙色多边形按绝对刻度(RADAR_MAX)定标,越强多边形越大。详情页用。
export function StatRadar({ p }) {
  const icons = React.useContext(IconsContext)
  const stats = RADAR_AXES.map((k) => p[k] || {})
  const vals = stats.map((s) => s.value ?? 0)
  const size = 280, c = size / 2, R = 84, labelR = 108
  const pt = (i, r) => {
    const a = (-90 + i * 60) * Math.PI / 180
    return [c + r * Math.cos(a), c + r * Math.sin(a)]
  }
  const poly = (r) => RADAR_AXES.map((_, i) => pt(i, r).join(',')).join(' ')
  const dataPoly = vals.map((v, i) => pt(i, R * Math.min(1, v / RADAR_MAX)).join(',')).join(' ')
  return (
    <div className="radar">
      <svg className="radar-svg" viewBox={`0 0 ${size} ${size}`}>
        {[0.25, 0.5, 0.75, 1].map((rr, i) => <polygon key={i} className="radar-ring" points={poly(R * rr)} />)}
        {RADAR_AXES.map((_, i) => { const [x, y] = pt(i, R); return <line key={i} className="radar-spoke" x1={c} y1={c} x2={x} y2={y} /> })}
        <polygon className="radar-area" points={dataPoly} />
      </svg>
      {RADAR_AXES.map((key, i) => {
        const s = stats[i]
        const [x, y] = pt(i, labelR)
        const talented = s.talentLv > 0
        return (
          <div key={key} className="radar-label" style={{ left: (x / size * 100) + '%', top: (y / size * 100) + '%' }}>
            <InlineIcon src={icons.stat && icons.stat[key]} className="radar-ic" alt="" />
            <b className={talented ? 'has-talent' : undefined} title={talented ? `天分 +${s.talentLv}` : undefined}>{s.value ?? 0}</b>
            {s.nature === 1 && <NatBadge dir={1} />}
            {s.nature === -1 && <NatBadge dir={-1} />}
          </div>
        )
      })}
    </div>
  )
}

// Tooltip 把内容 portal 到 body 并 fixed 定位:挂载后按自身尺寸相对锚点居中,
// 默认放上方,空间不足翻下方,水平方向夹在视口内(留 4px 边距)。定位算完前隐藏避免闪跳。
function Tooltip({ content, anchor }) {
  const ref = React.useRef(null)
  const [pos, setPos] = React.useState(null)
  React.useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const gap = 6
    const w = el.offsetWidth, h = el.offsetHeight
    let left = anchor.left + anchor.width / 2 - w / 2
    left = Math.max(4, Math.min(left, window.innerWidth - w - 4))
    let top = anchor.top - gap - h            // 默认上方
    if (top < 4) top = anchor.bottom + gap    // 上方放不下 → 翻到下方
    setPos({ left, top })
  }, [content, anchor])
  return createPortal(
    <div ref={ref} className="tip-pop" style={pos ? { left: pos.left, top: pos.top } : { left: 0, top: 0, visibility: 'hidden' }}>
      {content}
    </div>,
    document.body,
  )
}

// StatRange 渲染身高/体重值;悬停时 tooltip 以 `99.67% (下限-上限)` 显示当前值百分位与该形态取值范围。
// 范围/百分位来自后端 FillSizePercentile(按当前形态注入);未知形态无范围时退化为纯文本(无 tooltip)。
// tooltip 经 portal 渲染到 body、fixed 定位:不受列表 .table-wrap 的 overflow 裁剪。
export function StatRange({ value, min, max, pct, unit }) {
  const text = `${value}${unit}`
  const ref = React.useRef(null)
  const [anchor, setAnchor] = React.useState(null) // 悬停时锚点元素的视口矩形
  if (!(max > min)) return <>{text}</>
  const pctText = pct != null ? `${pct.toFixed(2)}%` : null
  const content = pctText ? `${pctText} (${min}-${max})` : `${min}-${max}`
  const show = () => { if (ref.current) setAnchor(ref.current.getBoundingClientRect()) }
  const hide = () => setAnchor(null)
  return (
    <span ref={ref} onMouseEnter={show} onMouseLeave={hide}>
      {text}
      {anchor && <Tooltip content={content} anchor={anchor} />}
    </span>
  )
}
