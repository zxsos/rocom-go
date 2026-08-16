import { useState, useEffect, useRef, useCallback } from 'react'
import { getPaint, resetPaint, subscribe } from '../../api'

// —— 涂地图层(「这片扫过了」的覆盖图)——
// 涂的依据是**实际下发过的野生宠**:每收到一只,就把「玩家 ↔ 这只宠」之间的带子涂上
// (见 docs/data.md 3.8)。有宠的方向涂到宠那儿,一只没刷的方向不涂,故涂出来的形状贴着
// 真实下发情况而不是某个固定半径。剩下没涂的就是还没扫过、值得去的地方。
// 后端按格子记(见 internal/server/paint.go),这里只负责:拉整张位图铺底、收 SSE 增量补格子、
// 把它画成两层——**canvas 铺淡填充 + SVG 描边界细线**(为什么分两层见下)。
//
// 为什么填充不用 DOM/SVG:大陆一张图 510*510 = 26 万格,涂开之后是几万个色块,一格一个元素
// 必然卡死;canvas 一次 putImageData 就铺完。两层都只在**有新格子**时重画一次(并到下一帧,
// 见 schedule),平移与缩放全靠 CSS/transform,不触发任何重绘。
const LS_KEY = 'map.paint'

// 配色:**内部一层淡色 + 边界描一条细线**。底图是一张彩虹似的彩绘地图(沙、草、水、红顶、
// 紫蘑菇林、金黄枫林),任何单一色的半透明填充都必然在某个地区糊掉——实机截图逐个试过:
// 绿在草地上看不见,紫在蘑菇林里看不见,压暗在深色山地上看不见。而**轮廓**与底色无关:
// 沿边界描一条细亮线,涂过的范围在哪儿都一眼看得出边界,内部只需淡淡一层示意。
const FILL_RGBA = [168, 85, 247, 82] // #a855f7,约 32% —— 内部淡淡一层(描边色见 map.css)

// **描边走矢量(SVG),填充走位图(canvas)**,各取所长:
//   - 描边是这层唯一要看清的东西,而位图描边一放大就糊(画布按地图开,放大到三四倍时
//     那条 1 像素的线被拉成一条几像素宽的毛边)。SVG 的路径由浏览器在**当前缩放下**重新
//     栅格化,配合 vector-effect:non-scaling-stroke,线宽恒为屏幕上的 1.2px——放到最大
//     依旧是一条干净的细线,且平移缩放全由 CSS/transform 负责,一次都不用重画。
//   - 填充是一层淡色,糊不糊看不出来,继续用格子位图最省(26 万格一次 putImageData)。
const bitAt = (bits, i) => (bits[i >> 3] & (1 << (i & 7))) !== 0

// edgePath 把位图的「已涂 / 未涂」交界拼成一条 SVG 路径(坐标即格子号,由 viewBox 缩放)。
// 连续的一段边合成一条 H/V 直线,路径长度因此比「每格四条边」小一个量级。
function edgePath(bits, w, h) {
  const on = (x, y) => x >= 0 && y >= 0 && x < w && y < h && bitAt(bits, y * w + x)
  const out = []
  for (let y = 0; y < h; y++) { // 上边(y)与下边(y+1):按行扫,横向合并
    for (const [dy, edgeY] of [[-1, y], [1, y + 1]]) {
      let x = 0
      while (x < w) {
        if (!on(x, y) || on(x, y + dy)) { x++; continue }
        let x2 = x + 1
        while (x2 < w && on(x2, y) && !on(x2, y + dy)) x2++
        out.push(`M${x} ${edgeY}H${x2}`)
        x = x2 + 1
      }
    }
  }
  for (let x = 0; x < w; x++) { // 左边(x)与右边(x+1):按列扫,纵向合并
    for (const [dx, edgeX] of [[-1, x], [1, x + 1]]) {
      let y = 0
      while (y < h) {
        if (!on(x, y) || on(x + dx, y)) { y++; continue }
        let y2 = y + 1
        while (y2 < h && on(x, y2) && !on(x + dx, y2)) y2++
        out.push(`M${edgeX} ${y}V${y2}`)
        y = y2 + 1
      }
    }
  }
  return out.join('')
}

// decodeCells 把后端的 base64 位图解成 Uint8Array(每字节 8 格、低位在前)。
function decodeCells(b64, bytes) {
  const out = new Uint8Array(bytes)
  if (!b64) return out
  try {
    const bin = atob(b64)
    for (let i = 0; i < out.length && i < bin.length; i++) out[i] = bin.charCodeAt(i)
  } catch { /* 坏数据当没涂过 */ }
  return out
}

// usePaint 管理涂地图层:开关、整张位图、SSE 增量、重置。res/layer 变(换场景、进出洞穴)即重取。
// paintable 由位置推送给出(该场景有没有大地图底图),与图层开关无关——否则关着图层时无从得知
// 「这儿能不能涂」,开关就会被自己禁用掉、再也打不开。
export function usePaint(account, res, layer, paintable) {
  const [on, setOn] = useState(() => localStorage.getItem(LS_KEY) === '1')
  const [dims, setDims] = useState({ w: 0, h: 0 }) // canvas 尺寸 = 格子数;w=0 表示该场景不涂(无底图/家园)
  const bitsRef = useRef(null)   // 位图本体(整张的权威副本,canvas 掉了能重画)
  const canvasRef = useRef(null) // 画布 DOM(只在图层开着且有底图时存在)
  const [ver, setVer] = useState(0) // 整张换了(重取/重置)时递增,触发全量重画

  const gridRef = useRef({ w: 0, h: 0 }) // 格子数(= canvas 像素尺寸 = SVG viewBox)
  const rafRef = useRef(0)
  const [edge, setEdge] = useState('') // 边界的 SVG 路径(见 edgePath)

  // draw 按位图整张重画:canvas 铺淡填充,顺带把边界拼成 SVG 路径交给 React 渲染。
  // 不做「只画新来那几格」的增量——新格子会改变周围的边界走向,局部补画要连带擦掉旧描边,
  // 得不偿失;整张重画本身是几趟按格扫描,几毫秒,而且只在真有新格子时跑一次(见 schedule)。
  const draw = useCallback(() => {
    const cv = canvasRef.current
    const bits = bitsRef.current
    const { w, h } = gridRef.current
    if (!bits || !w) return
    if (cv) {
      const ctx = cv.getContext('2d')
      if (ctx) {
        // 淡填充:几万次 fillRect 太慢,直接按像素写一张 w*h 的 ImageData 铺上去。
        const img = ctx.createImageData(w, h)
        const d = img.data
        for (let b = 0, n = bits.length; b < n; b++) {
          if (bits[b] === 0) continue // 整字节没涂(绝大多数):8 格一起跳过
          for (let k = 0; k < 8; k++) {
            if ((bits[b] & (1 << k)) === 0) continue
            const o = ((b << 3) | k) * 4
            d[o] = FILL_RGBA[0]; d[o + 1] = FILL_RGBA[1]; d[o + 2] = FILL_RGBA[2]; d[o + 3] = FILL_RGBA[3]
          }
        }
        ctx.putImageData(img, 0, 0) // putImageData 直接覆盖,不必先 clear
      }
    }
    setEdge(edgePath(bits, w, h))
  }, [])

  // schedule 把重画并到下一帧:一批增量(几十格)只重画一次。
  const schedule = useCallback(() => {
    if (rafRef.current) return
    rafRef.current = requestAnimationFrame(() => { rafRef.current = 0; draw() })
  }, [draw])

  // 画布挂载(开图层/换场景/换层)时重画一次:canvas 的 width/height 一变内容就被清空。
  const attach = useCallback((node) => {
    canvasRef.current = node
    if (node) draw()
  }, [draw])

  // 换账号/场景/分层、或刚打开图层:重取整张位图。
  // **图层关着就不取**——整张 43KB,而传送/换场景是常事,为一个没开的图层每次都拉一遍不值;
  // 后端始终在记(与开关无关),故打开的那一刻拉到的就是这一路走过的全部痕迹,不会从此刻重涂。
  useEffect(() => {
    if (!res || !on || !paintable) { setDims({ w: 0, h: 0 }); bitsRef.current = null; return }
    let alive = true
    getPaint(res, layer || 0).then((d) => {
      if (!alive) return
      const w = d.w | 0, h = d.h | 0
      bitsRef.current = w > 0 ? decodeCells(d.cells, Math.ceil((w * h) / 8)) : null
      gridRef.current = { w, h }
      setDims({ w, h })
      setVer((v) => v + 1)
    }).catch(() => {})
    return () => { alive = false }
  }, [account, res, layer, on, paintable])

  // SSE 增量:新涂的格子(常态几个到几十个)直接补进位图并画上;reset 则清空重画。
  useEffect(() => subscribe((m) => {
    if (m.type !== 'paint') return
    const d = m.data || {}
    if (d.res !== res || (d.layer || 0) !== (layer || 0)) return // 别的场景/层,不关我的事
    const bits = bitsRef.current
    if (d.reset) {
      if (bits) bits.fill(0)
      setVer((v) => v + 1)
      return
    }
    if (!bits || !d.cells) return
    for (const idx of d.cells) bits[idx >> 3] |= 1 << (idx & 7)
    schedule()
  }), [account, res, layer, schedule])

  useEffect(() => { draw() }, [ver, on, draw]) // 整张换了/图层刚打开:重画
  useEffect(() => () => cancelAnimationFrame(rafRef.current), []) // 卸载时别留下待执行的重画

  const toggle = () => setOn((v) => {
    localStorage.setItem(LS_KEY, v ? '0' : '1')
    return !v
  })

  // 重置只清当前场景当前层:别处扫过的照旧保留(重来一遍代价太大)。
  const reset = () => {
    if (!res) return
    resetPaint(res, layer || 0).catch(() => {})
    if (bitsRef.current) bitsRef.current.fill(0)
    setVer((v) => v + 1)
  }

  // w/h 既是 canvas 的像素尺寸也是 SVG 的 viewBox(都按格子数);edge 是边界路径;
  // ready:位图已到手、可以画了;available:这个场景能不能涂(管开关是否可点)。
  return {
    on, toggle, reset, attach, edge,
    w: dims.w, h: dims.h,
    ready: dims.w > 0, available: !!paintable,
  }
}
