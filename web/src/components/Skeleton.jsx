// Skeleton 骨架屏(P5.4.2):数据未到时占住真实布局的坑位,替代「加载中…」文字。
//
// 为什么不给 spinner/一行文字:这个站每页都是「一屏数据」(列表 / 网格 / 卡片),
// 等待期只给一行字的话,数据到位时整屏内容从中间撑开,是一次大的布局位移 ——
// 而骨架按真实布局的形状与尺寸先铺一遍,数据到了只是「就地填色」,页面不跳。
//
// 无障碍:骨架是纯装饰,每块都 aria-hidden;外层容器自己补 role="status",
// 读屏听到的是「加载中」而不是一堆空节点。
// 扫光是循环动画,prefers-reduced-motion 下由 base.css 关掉(静态底色仍在,
// 「有东西在加载」这件事不丢)。

// Skeleton 单个色块。w/h/r 直接进 style,方便按各处真实尺寸给值。
export function Skeleton({ w, h = '1em', r, className = '', style }) {
  return (
    <span
      className={'skeleton ' + className}
      style={{ width: w, height: h, borderRadius: r, ...style }}
      aria-hidden="true"
    />
  )
}

// SkeletonRows 纵向若干行(排行榜 / 表格类骨架)。每行默认一整条。
export function SkeletonRows({ rows = 5, h = 44, gap = 8, className = '' }) {
  return (
    <div className={'skeleton-rows ' + className} style={{ gap }} aria-hidden="true">
      {Array.from({ length: rows }, (_, i) => (
        <Skeleton key={i} h={h} />
      ))}
    </div>
  )
}
// 注:没有 SkeletonGrid —— 网格类页面(如炫彩图鉴)直接复用真实布局类
// (.hb-major-group / .hb-major-cards)铺骨架,比另写一个「差不多」的网格更同构,
// 故不预留用不上的通用组件。需要时再加。
