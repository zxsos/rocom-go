import React, { useContext, useMemo, useState } from 'react'
import { ALL_TYPES } from '../../constants'
import { IconsContext } from '../../context'
import { imgURL } from '../../components/icons'

// 徽章星盘:18 个属性系分布在同心圆环上,每通关系点亮一盏元素灯火。
//
// 为什么做成轮盘而不是格子:18 个系是**一圈**而非一列 —— 玩家在游戏里也是
// 按元素轮转着打,圆形能一眼看出「还差哪一片扇区」,而格子只能逐个数。
// 全部点亮时相邻节点之间的「封印链」闭合成完整的环,那一刻才叫通关。
//
// 数据来源 history.slots(GET /api/trial 的整份快照,SSE 覆盖更新)。
// 只有 cleared 计数,没有每个难度的名字 —— 3 个难度用节点内侧的 3 枚刻度表示。

// 18 系配色 —— 与 web/src/styles/base.css 的 .type[data-t="…"] **一一对应**。
// ⚠️ 两处必须同步改:那里管系别胶囊的底色,这里管星盘的光色。
//    色值取自游戏内配色,玩家靠颜色辨认系别 —— 改色等于改含义,别动。
const TYPE_COLORS = {
  普: '#6b7280', 草: '#46b35a', 火: '#e2563b', 水: '#3b8fe2',
  光: '#e0c33a', 地: '#a9763e', 冰: '#54bcd6', 龙: '#6b58d6',
  电: '#d6b53a', 毒: '#9b4fc4', 虫: '#86a832', 武: '#c2452f',
  翼: '#5bc0c9', 萌: '#e58fb8', 幽: '#5a4a7a', 恶: '#3a3f4a',
  机械: '#7a8794', 幻: '#b46fd6',
}

// —— 几何 ——
// viewBox 440×440,圆心 (220,220)。半径自外向内递减,每一层承担一个语义。
const CX = 220, CY = 220
const R_RIM = 206 // 最外装饰环(星盘的"盘缘")
// 189 而非更靠内:节点光晕在 hover 放大 1.18 倍后外缘到 179.9,
// 系名文字内缘在 R_LABEL - 7(半高),再小就会压到光晕上。
const R_LABEL = 189 // 系名
const R_NODE = 148 // 节点圆心
const R_TICK = [99, 110, 121] // 3 个难度刻度,由内到外
const R_ARC = 74 // 进度弧中线
const W_ARC = 19 // 弧带宽度
const R_CORE = 44 // 中心印记

const TAU = Math.PI * 2
const SEG = TAU / 18 // 每个系占 20°
const GAP = 0.013 // 扇区之间的留白(弧度),免得相邻弧粘成一片

// angleOf:第 i 个系的方向,**主题系固定在正上方(-90°)**,其余依次顺时针。
//
// 为什么整个盘要跟着主题转:这是「草系徽章试炼」页 —— 草是这个盘的主角,
// 让它坐正上方的王座位,一眼就知道这张图说的是哪个徽章。
// 相对顺序不变(草→火→水→…→普),故老玩家记住的方位不会乱。
//
// 顺带也让它能直接复用到将来的火系/水系徽章页:换 theme 即可,不用改几何。
const angleOf = (i, themeIdx) => (i - themeIdx) * SEG - Math.PI / 2
const pt = (r, a) => [CX + r * Math.cos(a), CY + r * Math.sin(a)]
const f = (n) => n.toFixed(2)

// 18 边形路径(顶点即节点圆心所在半径)
const polyPath = (r, themeIdx) => ALL_TYPES
  .map((_, i) => {
    const [x, y] = pt(r, angleOf(i, themeIdx))
    return `${i ? 'L' : 'M'}${f(x)},${f(y)}`
  })
  .join('') + 'Z'

// 弧:给定半径与起止角。sweep 恒为 1(顺时针),因为每个系的弧都不到半圈。
const arcPath = (r, a0, a1) => {
  const [x0, y0] = pt(r, a0)
  const [x1, y1] = pt(r, a1)
  return `M${f(x0)},${f(y0)}A${r},${r} 0 0 1 ${f(x1)},${f(y1)}`
}

export default function ElementWheel({ slots, theme = '草' }) {
  const icons = useContext(IconsContext) || {}
  const typeIcon = icons.type || {}
  // hover 哪个系(-1 = 没有,中央显示主题系)
  const [hover, setHover] = useState(-1)

  // 主题系在 ALL_TYPES 里的下标,决定整个盘的旋转。
  // 取不到(传了列表外的名字)时退回 0 —— 宁可盘转错一点,
  // 也不能让 indexOf 返回 -1 把角度算成 NaN、整张盘消失。
  const themeIdx = useMemo(() => Math.max(0, ALL_TYPES.indexOf(theme)), [theme])

  // 几何**由 ALL_TYPES 固定**,后端数据只负责填数字。
  //
  // 刻意不按后端下发顺序排:服务器给 slots 的顺序是协议里的 slot_id 序,
  // 换版本可能变 —— 若照它排,同一个系的位置会跳,玩家记不住「草在哪个方位」,
  // 而位置记忆正是这个图存在的意义。缺失的系按 0/3 处理(首次进游戏时
  // 后端往往只下发已解锁的那几个),18 个顶点始终齐全,盘面不会缺角。
  const data = useMemo(() => {
    const byName = {}
    for (const s of slots || []) {
      if (s && s.damName) byName[s.damName] = s.cleared || 0
    }
    return ALL_TYPES.map((name, i) => {
      // 上限夹到 3:协议里 cleared 是已通关 id 的个数,理论上不该超过 3,
      // 但夹一下更省心 —— 超过会让进度弧画出扇区、盖到隔壁系头上。
      const cleared = Math.min(3, Math.max(0, byName[name] || 0))
      return {
        name, cleared, color: TYPE_COLORS[name] || '#8b95a5', full: cleared >= 3,
        // 主题系(当前徽章所属的那个系):加冕显示,见下方节点与中心印记。
        theme: i === ALL_TYPES.indexOf(theme),
      }
    })
  }, [slots, theme])

  const clearedTotal = data.reduce((a, d) => a + d.cleared, 0)
  const fullCount = data.filter((d) => d.full).length
  const allDone = fullCount === data.length && data.length > 0
  // 中央显示:hover 时是那一系,否则是**主题系**(这张图的主角)。
  //
  // 不显示总数是有意的:这个组件挂在「草系徽章试炼」页里,玩家点进来想看的是
  // 草系打得怎么样。18 系的总数是次要信息,缩到副标题里就行。
  const focus = (hover >= 0 ? data[hover] : null) || data[themeIdx]

  return (
    <div className={'sigil-wrap' + (allDone ? ' sigil-done' : '')}>
      <svg
        className="sigil"
        viewBox="0 0 440 440"
        role="img"
        aria-label={`${theme}系徽章试炼 — 各系通关进度:${theme}系 ${data[themeIdx].cleared}/3,`
          + `全服共 ${fullCount}/${data.length} 系全通(${clearedTotal}/${data.length * 3} 个难度)`}
      >
        <defs>
          {/* 盘面:中心稍亮提供纵深,边缘压暗把光都收到中间 */}
          <radialGradient id="sigil-plate" cx="50%" cy="50%" r="50%">
            <stop offset="0%" stopColor="#1b2231" />
            <stop offset="55%" stopColor="#141924" />
            <stop offset="100%" stopColor="#0e1218" />
          </radialGradient>
          {/* 中心印记:全通时从暗金翻成亮金(两者用同一 gradient,CSS 只切透明度) */}
          <radialGradient id="sigil-core" cx="50%" cy="38%" r="62%">
            <stop offset="0%" stopColor="#f7d488" />
            <stop offset="52%" stopColor="#d9a13c" />
            <stop offset="100%" stopColor="#8a6316" />
          </radialGradient>
        </defs>

        {/* —— 盘面 —— */}
        <circle cx={CX} cy={CY} r={R_RIM} fill="url(#sigil-plate)" />
        <circle cx={CX} cy={CY} r={R_RIM} className="sigil-rim" />
        <circle cx={CX} cy={CY} r={R_RIM - 7} className="sigil-rim sigil-rim-soft" />

        {/* —— 全通时的旋转光芒(盘缘内侧)—— —— */}
        {allDone && (
          <g className="sigil-rays">
            {Array.from({ length: 36 }, (_, k) => {
              const a = (k / 36) * TAU
              const [x0, y0] = pt(R_CORE + 10, a)
              const [x1, y1] = pt(R_ARC - W_ARC / 2 - 2, a)
              return <line key={k} x1={f(x0)} y1={f(y0)} x2={f(x1)} y2={f(y1)} />
            })}
          </g>
        )}

        {/* —— 18 边形"封印链":底链 + 相邻两系都满时闭合的亮段 ——
            亮段的规则是**相邻两个都满**,单数个满系不会连成链 ——
            这让"差一个系"在视觉上特别刺眼(环上有个缺口),正是想要的效果。 */}
        <path d={polyPath(R_NODE, themeIdx)} className="sigil-poly" />
        {data.map((d, i) => {
          const j = (i + 1) % data.length
          if (!d.full || !data[j].full) return null
          const [x0, y0] = pt(R_NODE, angleOf(i, themeIdx))
          const [x1, y1] = pt(R_NODE, angleOf(j, themeIdx))
          return (
            <line key={i} className="sigil-link" x1={f(x0)} y1={f(y0)} x2={f(x1)} y2={f(y1)}
              style={{ '--d': `${i * 60}ms` }} />
          )
        })}

        {/* —— 进度弧带:内圈那一环,每系一段,从扇区中心向两侧展开 ——
            对称展开(而非从一端填到另一端)是因为它对应的是"这个系打了几个难度",
            扇形居中才看得出每段属于哪个方向。 */}
        {data.map((d, i) => {
          const a = angleOf(i, themeIdx)
          const half = (SEG / 2) * (d.cleared / 3)
          if (d.cleared <= 0) {
            return null // 0/3 不画,留出暗底槽即可
          }
          return (
            <path key={d.name} d={arcPath(R_ARC, a - half + GAP, a + half - GAP)}
              className={'sigil-arc' + (hover === i ? ' hot' : '')}
              stroke={d.color} strokeWidth={W_ARC}
              style={{ '--c': d.color, '--d': `${300 + i * 40}ms` }} />
          )
        })}
        {/* 弧带的暗底槽:让 0/3 的位置也能看出"这里有一格" */}
        {data.map((d, i) => {
          const a = angleOf(i, themeIdx)
          return (
            <path key={d.name} d={arcPath(R_ARC, a - SEG / 2 + GAP, a + SEG / 2 - GAP)}
              className="sigil-arc-slot" strokeWidth={W_ARC} />
          )
        })}

        {/* —— 3 个难度刻度:每系方向上的 3 枚菱形,由内到外 ——
            放在节点内侧是刻意的:它们描述的是"这个系"的进度,
            贴着该系的方向才不会看成别的系的。 */}
        {data.map((d, i) => {
          const a = angleOf(i, themeIdx)
          return R_TICK.map((r, k) => {
            const [x, y] = pt(r, a)
            const on = d.cleared > k
            return (
              <g key={d.name + k} transform={`translate(${f(x)},${f(y)}) rotate(45)`}
                style={{ '--c': d.color, '--d': `${420 + i * 40 + k * 12}ms` }}>
                <rect x="-3.4" y="-3.4" width="6.8" height="6.8"
                  className={'sigil-tick' + (on ? ' on' : '') + (hover === i && on ? ' hot' : '')}
                  fill={on ? d.color : 'none'} />
              </g>
            )
          })
        })}

        {/* —— 系名 ——
            full = 该系全通(元素色加粗);hot = 正悬停;theme = 当前徽章的主题系
            (常驻金色描边)。三者可叠加,让"我现在看的是哪个系"在任何完成度下都分得清。 */}
        {data.map((d, i) => {
          const [x, y] = pt(R_LABEL, angleOf(i, themeIdx))
          return (
            <text key={d.name} x={f(x)} y={f(y)}
              textAnchor="middle" dominantBaseline="central"
              className={'sigil-label' + (d.full ? ' full' : '') + (hover === i ? ' hot' : '') +
                (d.theme ? ' theme' : '')}
              style={{ '--c': d.color, '--d': `${520 + i * 40}ms` }}>
              {d.name}
            </text>
          )
        })}

        {/* —— 18 个节点:星盘上的"灯" ——
            外层 g 负责交互(鼠标/键盘),内层各图形负责视觉。
            热区(透明圆 r=26)比节点本身大,触屏上也点得中。 */}
        {data.map((d, i) => {
          const [x, y] = pt(R_NODE, angleOf(i, themeIdx))
          const src = typeIcon[d.name]
          const on = hover === i
          return (
            <g key={d.name}
              className={'sigil-node' + (d.full ? ' full' : '') + (on ? ' on' : '') +
                (d.theme ? ' theme' : '')}
              transform={`translate(${f(x)},${f(y)})`}
              style={{ '--c': d.color, '--d': `${140 + i * 45}ms` }}
              onMouseEnter={() => setHover(i)}
              onMouseLeave={() => setHover((h) => (h === i ? -1 : h))}
              onFocus={() => setHover(i)}
              onBlur={() => setHover((h) => (h === i ? -1 : h))}
              tabIndex={0} role="button"
              aria-label={`${d.name} ${d.cleared}/3`}
            >
              <title>{`${d.name} ${d.cleared}/3`}</title>
              {/* 这层承载 CSS 的缩放与入场动画。
                  ⚠️ 不能省:外层 g 的定位靠 SVG 的 transform **属性**,而 CSS 的
                  transform 会整个覆盖它 —— 若把 scale 直接加在外层,节点会全部
                  飞到左上角。故外层只定位、内层只变换。
                  热区留在外层,不跟着缩放 —— 鼠标一悬停目标就放大跑掉很难点。 */}
              <g className="sigil-node-in">
                {/* 加冕:主题系无论通没通都戴一圈金环 ——
                    「这是当前徽章」是**页面的主题**,不是通关状态,
                    不该跟着进度变。金环画在最底层,不遮图标。 */}
                {d.theme && <circle className="sigil-crown" r="21.5" />}
                {/* 光晕:两层递减透明度的实心圆手工模拟,不用 SVG filter ——
                    18 个 filter 实例在移动端会掉帧,而两个圆几乎零成本。 */}
                {d.full && (
                  <>
                    <circle className="sigil-halo" r="27" fill={d.color} />
                    <circle className="sigil-halo sigil-halo-2" r="21.5" fill={d.color} />
                  </>
                )}
                <circle className="sigil-node-bg" r="17" />
                <circle className="sigil-node-ring" r="17" stroke={d.full ? d.color : 'none'} />
                {/* 图标:webp,用 SVG <image> 而非 <img>(后者不能进 SVG 文档) */}
                {src && (
                  <image href={imgURL(src)} x="-11" y="-11" width="22" height="22"
                    preserveAspectRatio="xMidYMid meet" className="sigil-node-ic" />
                )}
                {/* 未通关时盖一层暗纱:图标还在(看得出是哪个系),但明显"没亮" */}
                {!d.full && <circle className="sigil-node-dim" r="17" />}
              </g>
              <circle className="sigil-hit" r="26" />
            </g>
          )
        })}

        {/* —— 中心印记:当前徽章 ——
            常驻显示**主题系**(草系徽章 → 草),hover 别的系时临时换成那一系,
            同一个位置换内容,视线不用跳动。

            三种内容共用一个布局:系图标 → 进度 → 名字。全通时底色翻成金 +
            外圈光环。 */}
        <g className="sigil-core">
          {allDone && <circle cx={CX} cy={CY} r={R_CORE + 9} className="sigil-core-glow" />}
          <circle cx={CX} cy={CY} r={R_CORE} className="sigil-core-bg" />
          {allDone && <circle cx={CX} cy={CY} r={R_CORE} fill="url(#sigil-core)" className="sigil-core-fill" />}
          {/* 系图标:让中心一眼就是"草系",而不只是一行字。
              缺图标时留空 —— 下面那行字已经写明了系名,不至于认不出。 */}
          {typeIcon[focus.name] && (
            <image href={imgURL(typeIcon[focus.name])} x={CX - 13} y={CY - 32} width="26" height="26"
              preserveAspectRatio="xMidYMid meet" className="sigil-core-ic" />
          )}
          <text x={CX} y={CY + 6} className="sigil-core-num" fill={focus.color}
            textAnchor="middle" dominantBaseline="central">
            {focus.cleared}
            <tspan className="sigil-core-den">/3</tspan>
          </text>
          {/* 主题系写「草系徽章」点明这是当前徽章;hover 别的系时只写系名 ——
              那些系不是本页主题,写"徽章"会把主题说错。 */}
          <text x={CX} y={CY + 27} className="sigil-core-sub" fill={focus.color}
            textAnchor="middle" dominantBaseline="central">
            {focus.theme || hover < 0 ? `${focus.name}系徽章` : `${focus.name}系`}
          </text>
        </g>
      </svg>

      {/* 图例:说明 3 枚刻度与两种光环的含义。写在图外而非图内 ——
          图内 18 个方位都已占满,塞说明会破坏对称。 */}
      <div className="sigil-legend">
        <span><i className="sigil-lg-tick on" />已通难度</span>
        <span><i className="sigil-lg-tick" />未通</span>
        <span><i className="sigil-lg-node" />该系全通 3/3</span>
        <span><i className="sigil-lg-crown" />{theme}系徽章(本页)</span>
      </div>
    </div>
  )
}
