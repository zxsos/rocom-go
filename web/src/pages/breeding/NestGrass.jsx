import React from 'react'

// 学院小窝的绿草图形:小窝是**用草编的**,故选中态不从「打勾」入手,而是给头像套一圈**编出来的草环**
// (见 docs/data.md 的学院小窝一节)。
//
// 两件东西都在这里:
//   NestWreath —— 头像外那圈编草环;
//   NestSprig  —— 按钮上那株小草芽图标。
//
// 环是怎么构成的(这一版的形状信息全在**沿圆周**上,径向只负责纹理):
//   · 环带   —— 一圈粗描边,给环一个「料」的厚度;
//   · 三股草绳 —— 半径按 sin 在内外缘之间来回,三股各错开 120°,交叠处自然是辫子纹;
//   · 纤维   —— 带宽内一圈细密短划(深浅交错),这是「编」的**纹理**所在,没有它环就是一条塑料带;
//   · 内外缘 —— 两道细线收边;
//   · 三处小叶结 —— 贴着环走的两片小叶,给一点手工感。
// 之前那版是「一圈朝外辐射的长草」,看着像蒲公英/火花 —— 编绳的形状信息在切向,不在这里。
//
// 路径全部**确定性**生成(只用下标算角度与长短,不用随机数):同一组件渲染两次必须一模一样,
// 否则每次重渲染草纹都在变。

// 圆心与环带半径:与 46×46 的 viewBox 同一坐标系。头像本身 22px(见 .dropdown-av),半径 11,
// 故内缘从 11.4 起 —— 贴着头发外沿,不压住头像。
const C = 23
const R_IN = 11.4
const R_OUT = 16.2
const R_MID = (R_IN + R_OUT) / 2
const TURNS = 9 // 草绳绕一圈起伏几次(越多越密)
const AMP = 1.55 // 起伏幅度:刚好贴住内外缘
const SAMPLES = 160

const polar = (r, a) => [C + Math.cos(a) * r, C + Math.sin(a) * r]

// strand 一股草绳:半径 r(θ) = 中径 + 幅度·sin(圈数·θ + 相位) + 一点细抖动。
// 三股只差相位 —— 它们彼此穿插的地方就是辫子。
// 幅度按股、按角度再抖一点(仍是确定性的):等幅等距的辫子像机器织的带子,草编的不齐才像草。
const strand = (phase, k) => {
  const amp = AMP * (0.72 + 0.28 * Math.abs(Math.sin(k * 2.3 + phase)))
  const pts = []
  for (let i = 0; i <= SAMPLES; i++) {
    const t = (i / SAMPLES) * Math.PI * 2
    const r = R_MID + amp * Math.sin(TURNS * t + phase) + 0.22 * Math.sin(t * 23 + k * 1.7)
    const [x, y] = polar(r, t - Math.PI / 2)
    pts.push(`${x.toFixed(2)},${y.toFixed(2)}`)
  }
  return 'M' + pts.join('L')
}
const strands = [0, (2 * Math.PI) / 3, (4 * Math.PI) / 3].map((phase, i) => ({
  d: strand(phase, i),
  delay: i * 140, // 一股比一股晚:合起来是「沿圆周编过去」
}))

// 纹理:带宽内一圈细密短纤维。长短、深浅、出现时机都按下标错开 —— 等长等深的会像刻度盘。
const FIBERS = 108
const fibers = Array.from({ length: FIBERS }, (_, i) => {
  const a = (i / FIBERS) * Math.PI * 2 - Math.PI / 2
  const [x1, y1] = polar(R_IN + 0.5 + Math.sin(i * 2.7) * 0.5, a)
  const [x2, y2] = polar(R_OUT - 0.6 + Math.cos(i * 1.9) * 0.6, a)
  return {
    d: `M${x1.toFixed(2)},${y1.toFixed(2)}L${x2.toFixed(2)},${y2.toFixed(2)}`,
    dark: i % 3 === 0,
    dim: i % 5 === 0,
    delay: 240 + (i % 9) * 22,
  }
})

// 三处小叶结:两片小叶贴着环走(切向偏移、只比带宽外一点点),不往外支 —— 一往外支就又变成辐射了。
const knots = [28, 152, 262].map((deg, k) => {
  const a = ((deg - 90) * Math.PI) / 180
  const [x0, y0] = polar(R_MID, a)
  return [1, -1].map((dir) => {
    const [tx, ty] = polar(R_MID + 1.5, a + dir * 0.34)
    const [cx, cy] = polar(R_MID + 2.3, a + dir * 0.1)
    return {
      d: `M${x0.toFixed(2)},${y0.toFixed(2)}Q${cx.toFixed(2)},${cy.toFixed(2)} ${tx.toFixed(2)},${ty.toFixed(2)}`,
      delay: 560 + k * 70,
    }
  })
}).flat()

// 飘起的花粉:只留三粒(位置写死,动画只做「向上飘散」),别抢环的戏。
const SPECKS = [
  { left: '20%', top: '74%', delay: 320 },
  { left: '70%', top: '26%', delay: 460 },
  { left: '44%', top: '10%', delay: 600 },
]

// NestWreath 头像外那圈编草环。挂在头像容器里(position: relative 的那个),自身绝对定位铺满。
//
// 生长动画:草绳用 pathLength=100 把长度归一,于是 dasharray/dashoffset 都能写死 —— 一股接一股
// 从 0 画到一整圈,看上去就是「沿着头像绕一圈编进去」(而不是往外长)。整段动画只在**挂载时**跑
// 一次:组件只在「选中」时渲染,故「点上去 → 草编起来」天然只发生在状态翻转那一次。
export function NestWreath() {
  return (
    <span className="ng-wreath" aria-hidden="true">
      <span className="ng-sweep" />
      <svg className="ng-wreath-svg" viewBox="0 0 46 46">
        <circle className="ng-band" cx={C} cy={C} r={R_MID} />
        <circle className="ng-rim" cx={C} cy={C} r={R_IN} />
        <circle className="ng-rim ng-rim-out" cx={C} cy={C} r={R_OUT} />
        {fibers.map((f, i) => (
          <path
            key={'f' + i}
            className={'ng-fiber' + (f.dark ? ' dark' : '') + (f.dim ? ' dim' : '')}
            d={f.d}
            style={{ animationDelay: `${f.delay}ms` }}
          />
        ))}
        {strands.map((s, i) => (
          <path
            key={'s' + i}
            className={'ng-strand ng-strand-' + i}
            d={s.d}
            pathLength="100"
            style={{ animationDelay: `${s.delay}ms` }}
          />
        ))}
        {knots.map((k, i) => (
          <path key={'k' + i} className="ng-knot" d={k.d} style={{ animationDelay: `${k.delay}ms` }} />
        ))}
      </svg>
      {SPECKS.map((s, i) => (
        <i key={i} className="ng-speck" style={{ left: s.left, top: s.top, animationDelay: `${s.delay}ms` }} />
      ))}
    </span>
  )
}

// NestSprig 按钮上那株草芽。用 currentColor 描边:按钮的三种状态(未选/计划/实况)各给一个颜色,
// 图标自己不需要知道是哪种。
export function NestSprig({ size = 13 }) {
  return (
    <svg className="ng-sprig" width={size} height={size} viewBox="0 0 16 16" aria-hidden="true">
      <path d="M8 15V7.4" />
      <path d="M8 9.6C8 9.6 5.3 9.2 4.1 7.1 3.3 5.6 4.6 3.8 4.6 3.8c1.7.6 2.9 2.5 3.4 4.3" />
      <path d="M8 10.6c0 0 2.7-.4 3.9-2.5.8-1.5-.5-3.3-.5-3.3-1.7.6-2.9 2.5-3.4 4.3" />
      <path d="M8 7.8S6.7 5.2 6.9 3.5C7.1 2.1 8 1.3 8 1.3s.9.8 1.1 2.2c.2 1.7-1.1 4.3-1.1 4.3Z" />
    </svg>
  )
}
