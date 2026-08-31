import React, { useEffect, useRef, useState } from 'react'

// TweenNumber 数字滚动:值变化时用 rAF 在 400ms 内补间,而不是硬切。
//
// 为什么值得做:本工具满屏是数字,但它们的变化往往是**无声**的 ——
// 新抓到一只宠,「累计获得」从 1234 跳到 1235,那个 +1 承载的正是用户等待的东西;
// 硬切的话眼睛扫过去只会看到一个静态数字,不知道它刚变过。滚动让它**被看见**。
//
// 三条刻意的克制:
//   1. 首次挂载**不播** —— 从 0 滚到 1234 只会让人以为加载慢;
//   2. 值没变就不播(数据定时刷新时,绝大多数刷新是空转);
//   3. prefers-reduced-motion 直接落位。
//
// format 走 ref 而非 effect 依赖:调用方常写内联箭头函数(如 v => (v>0?'+':'')+fmt(v)),
// 每次渲染都是新引用,进依赖数组会让 effect 每渲染都重跑(虽然不会死循环,但白白多跑)。
const prefersReduced = () =>
  typeof window !== 'undefined' && !!window.matchMedia?.('(prefers-reduced-motion: reduce)').matches

const defaultFormat = (v) => v.toLocaleString('zh-CN')

export function TweenNumber({ value, format = defaultFormat, duration = 400 }) {
  const fmtRef = useRef(format)
  fmtRef.current = format // 每次渲染同步,effect 内只读 ref

  const [shown, setShown] = useState(value)
  // 上一次的目标值:补间的起点。初始 = 首次的 value,故挂载时 from === to → 不播。
  const fromRef = useRef(value)
  const rafRef = useRef(0)

  useEffect(() => {
    // 非数字(null/undefined/NaN)直接渲染:这些是「待同步」类占位,
    // 它没有什么「从旧值变到新值」可言,补间只会让人以为在加载。
    if (typeof value !== 'number' || !Number.isFinite(value)) {
      fromRef.current = value
      setShown(value)
      return undefined
    }
    const from = fromRef.current
    const to = value
    fromRef.current = to

    const sameOrNoBase = typeof from !== 'number' || !Number.isFinite(from) || from === to
    if (sameOrNoBase || prefersReduced()) {
      setShown(to)
      return undefined
    }

    // 两端都是整数时,中间值也取整 —— 否则「1234 → 1235」会闪过 1234.37,
    // 位数一跳一跳的,比不滚动还乱。
    const asInt = Number.isInteger(from) && Number.isInteger(to)
    const t0 = performance.now()
    const tick = (now) => {
      const p = Math.min(1, (now - t0) / duration)
      const eased = 1 - (1 - p) ** 3 // easeOutCubic:起手快、末尾缓,收得住
      const v = from + (to - from) * eased
      setShown(asInt ? Math.round(v) : v)
      if (p < 1) rafRef.current = requestAnimationFrame(tick)
    }
    rafRef.current = requestAnimationFrame(tick)
    // 卸载或值再次变化时取消上一轮,否则两个 rAF 会互相打架(数字来回跳)
    return () => cancelAnimationFrame(rafRef.current)
  }, [value, duration])

  return <>{fmtRef.current(shown)}</>
}
