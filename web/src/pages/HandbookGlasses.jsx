import React, { useEffect, useState, useContext, useMemo } from 'react'
import { getHandbookGlasses } from '../api'
import { IconsContext } from '../context'
import { imgURL } from '../components/icons'
import { GlassChip } from '../components/badges'
import { GLASS_BG, GLASS_BG2, GLASS_PARTICLES, GLASS_COLORS, GLASS_HIDDEN } from '../data/glassConf'

// 主色(ui_color_1)→ 显示名。11 种主色按 GLASS_COLORS 键序(见 web/src/data/glassConf.js)。
const MAJOR_NAMES = {
  '#3c32cf': '深蓝紫', '#83d256': '草绿', '#3ed7d7': '青',
  '#b869ed': '紫', '#e5576f': '绯红', '#bac8fb': '淡蓝',
  '#aedfae': '淡绿', '#a8e5e5': '淡青', '#dbbceb': '淡紫',
  '#fdcdd3': '淡粉', '#f1d397': '淡黄',
}

// 分享图 8 个颜色分类(图例/列):6 个彩色系 + 黑白 + 赛季彩色。
// 彩色系的代表色即列底色与图例圆点色;赛季彩色用多彩渐变。
const SHARE_COLS = [
  { key: 'blue', name: '蓝', color: '#4f6ff0' },
  { key: 'cyan', name: '青', color: '#35c4dd' },
  { key: 'green', name: '绿', color: '#67c94f' },
  { key: 'yellow', name: '黄', color: '#f2c531' },
  { key: 'pink', name: '粉', color: '#f2799e' },
  { key: 'purple', name: '紫', color: '#a86df2' },
  { key: 'mono', name: '黑白', color: '#7d8694' },
  { key: 'season', name: '赛季', color: 'linear-gradient(135deg,#f2c531,#f2799e,#a86df2,#35c4dd)' },
]
// 11 种主色(ui_color_1)→ 分享图分类:按色相归入蓝/青/绿/黄/粉/紫。
const MAJOR_TO_SHARE = {
  '#3c32cf': 'blue', '#bac8fb': 'blue',
  '#3ed7d7': 'cyan', '#a8e5e5': 'cyan',
  '#83d256': 'green', '#aedfae': 'green',
  '#f1d397': 'yellow',
  '#e5576f': 'pink', '#fdcdd3': 'pink',
  '#b869ed': 'purple', '#dbbceb': 'purple',
}
// 每列底部留白,让 8 列底部高度参差不齐(数量本就不同 + 留白差异,错落不齐)。
const SHARE_COL_GAP = [8, 24, 14, 30, 10, 26, 16, 32]

// 炫彩图鉴:按品种聚合展示本账号收集到的普通/隐藏炫彩色卡。
// 数据来自登录包 pet_handbook(每次登录时快照更新),点击色卡可放大预览。
// ---- 分享图导出:纯 canvas 直接绘制(不再用 html-to-image 克隆 DOM,避免导出时主线程卡顿;
//     图片异步预加载不阻塞 UI,绘制阶段仅 drawImage/蒙版合成,毫秒级) ----
const SHARE_W = 900
const SHARE_PAD = 30
const SHARE_COLS_GAP = 14
const SHARE_CARD_GAP = 9
const SHARE_SCALE = 2
const SHARE_FONT = "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Microsoft YaHei', sans-serif"

// 预加载图片(同源 /img/,canvas 不受 CORS 污染)
const loadImg = (src) => new Promise((resolve, reject) => {
  const img = new Image()
  img.onload = () => resolve(img)
  img.onerror = () => reject(new Error('图片加载失败: ' + src))
  img.src = src
})

const roundRectPath = (ctx, x, y, w, h, r) => {
  ctx.beginPath()
  ctx.moveTo(x + r, y)
  ctx.arcTo(x + w, y, x + w, y + h, r)
  ctx.arcTo(x + w, y + h, x, y + h, r)
  ctx.arcTo(x, y + h, x, y, r)
  ctx.arcTo(x, y, x + w, y, r)
  ctx.closePath()
}

// 蒙版填色层:临时 canvas「填色 → destination-in 蒙版裁切」后画到目标,等价 CSS mask
// (按素材 alpha 蒙版填 color;topAlign=true 时蒙版高度按 108/154 等比且顶部对齐,同 GlassChip)
const drawMaskLayer = (ctx, x, y, w, h, mask, color, topAlign) => {
  const mh = topAlign ? h * 108 / 154 : h
  const t = document.createElement('canvas')
  t.width = Math.max(1, Math.round(w))
  t.height = Math.max(1, Math.round(mh))
  const tc = t.getContext('2d')
  tc.fillStyle = color
  tc.fillRect(0, 0, t.width, t.height)
  tc.globalCompositeOperation = 'destination-in'
  tc.drawImage(mask, 0, 0, t.width, t.height)
  ctx.drawImage(t, x, y)
}

const ellipsisText = (ctx, text, maxW) => {
  if (ctx.measureText(text).width <= maxW) return text
  let s = text
  while (s.length > 1 && ctx.measureText(s + '…').width > maxW) s = s.slice(0, -1)
  return s + '…'
}

// 单张卡片:真实炫彩色卡(普通三层合成 / 隐藏整图)+ 圆形白底头像镶嵌 + 白字名字
const drawGlassCard = (ctx, x, y, w, h, card, imgs) => {
  ctx.save()
  roundRectPath(ctx, x, y, w, h, 13)
  ctx.clip()
  if (card.type === 2) {
    const src = GLASS_HIDDEN[card.value]
    const img = src && imgs.get(imgURL('dazzling/' + src))
    if (img) ctx.drawImage(img, x, y, w, h)
  } else {
    const colors = GLASS_COLORS[card.value & 0xFFFFF]
    const particle = GLASS_PARTICLES[card.value >> 20]
    const bg = imgs.get(imgURL('dazzling/' + GLASS_BG))
    const bg2 = imgs.get(imgURL('dazzling/' + GLASS_BG2))
    const pimg = particle && imgs.get(imgURL('dazzling/' + particle))
    if (colors && bg) drawMaskLayer(ctx, x, y, w, h, bg, colors[1], false)
    if (colors && bg2) drawMaskLayer(ctx, x, y, w, h, bg2, colors[0], true)
    if (pimg) drawMaskLayer(ctx, x, y, w, h, pimg, '#ffffff', false)
  }
  ctx.restore()
  const head = imgs.get(imgURL(card.head))
  if (head) {
    const cx = x + w / 2
    const cy = y + h * 0.44
    const r = 21
    ctx.save()
    ctx.beginPath()
    ctx.arc(cx, cy, r, 0, Math.PI * 2)
    ctx.fillStyle = '#fff'
    ctx.fill()
    ctx.clip()
    const s = Math.min(36 / head.naturalWidth, 36 / head.naturalHeight)
    const dw = head.naturalWidth * s
    const dh = head.naturalHeight * s
    ctx.drawImage(head, cx - dw / 2, cy - dh / 2, dw, dh)
    ctx.restore()
  }
  if (card.name) {
    ctx.save()
    ctx.font = `700 11px ${SHARE_FONT}`
    ctx.textAlign = 'center'
    ctx.textBaseline = 'bottom'
    ctx.shadowColor = 'rgba(0,0,0,.55)'
    ctx.shadowBlur = 2
    ctx.fillStyle = '#fff'
    ctx.fillText(ellipsisText(ctx, card.name, w - 8), x + w / 2, y + h - 3)
    ctx.restore()
  }
}

const drawLegendDot = (ctx, cx, cy, r, col) => {
  ctx.save()
  if (col.key === 'season') {
    const g = ctx.createLinearGradient(cx - r, cy - r, cx + r, cy + r)
    g.addColorStop(0, '#f2c531')
    g.addColorStop(0.33, '#f2799e')
    g.addColorStop(0.66, '#a86df2')
    g.addColorStop(1, '#35c4dd')
    ctx.fillStyle = g
  } else {
    ctx.fillStyle = col.color
  }
  ctx.beginPath()
  ctx.arc(cx, cy, r, 0, Math.PI * 2)
  ctx.fill()
  ctx.restore()
}

// 白色背景 900px 分享图:标题 + 8 色图例 + 8 列瀑布(列顶齐列底参差)+ 底部灰色水印
const drawShareCanvas = (cols, imgs) => {
  const W = SHARE_W
  const colW = (W - SHARE_PAD * 2 - SHARE_COLS_GAP * (cols.length - 1)) / cols.length
  const cardH = colW * 154 / 280
  const titleY = 26 + 19
  const legendY = titleY + 10 + 9
  const colTop = legendY + 9 + 16
  const colBottom = cols.map((c, i) => {
    const n = c.cards.length
    const cardsH = n > 0 ? n * cardH + (n - 1) * SHARE_CARD_GAP : 0
    return colTop + cardsH + SHARE_COL_GAP[i]
  })
  const contentBottom = Math.max(...colBottom, colTop)
  const H = Math.ceil(contentBottom + 16 + 11 + 12)
  const canvas = document.createElement('canvas')
  canvas.width = W * SHARE_SCALE
  canvas.height = H * SHARE_SCALE
  const ctx = canvas.getContext('2d')
  ctx.scale(SHARE_SCALE, SHARE_SCALE)
  ctx.fillStyle = '#fff'
  ctx.fillRect(0, 0, W, H)
  // 标题
  ctx.font = `800 24px ${SHARE_FONT}`
  ctx.textAlign = 'center'
  ctx.textBaseline = 'alphabetic'
  ctx.fillStyle = '#222'
  ctx.fillText('✨ 炫彩图鉴', W / 2, titleY)
  // 图例(整体居中):8 个颜色分类圆点 + 名字
  ctx.font = `400 13px ${SHARE_FONT}`
  const items = cols.map((c) => ({ c, w: 13 + 5 + ctx.measureText(c.name).width + 16 }))
  let lx = (W - (items.reduce((s, it) => s + it.w, 0) - 16)) / 2
  ctx.textBaseline = 'middle'
  for (const it of items) {
    drawLegendDot(ctx, lx + 6.5, legendY, 6.5, it.c)
    ctx.textAlign = 'left'
    ctx.fillStyle = '#555'
    ctx.fillText(it.c.name, lx + 13 + 5, legendY)
    lx += it.w
  }
  // 8 列瀑布
  for (let i = 0; i < cols.length; i++) {
    const x = SHARE_PAD + i * (colW + SHARE_COLS_GAP)
    let y = colTop
    for (const card of cols[i].cards) {
      drawGlassCard(ctx, x, y, colW, cardH, card, imgs)
      y += cardH + SHARE_CARD_GAP
    }
  }
  // 底部灰色小字水印
  ctx.font = `400 11px ${SHARE_FONT}`
  ctx.textAlign = 'center'
  ctx.textBaseline = 'alphabetic'
  ctx.fillStyle = '#b0b0b0'
  ctx.fillText('洛克王国·世界 · 炫彩图鉴 · 数据来自登录包快照', W / 2, contentBottom + 16 + 11)
  return canvas.toDataURL('image/png')
}

export default function HandbookGlasses() {
  const icons = useContext(IconsContext)
  const [data, setData] = useState(null) // null=加载中,其余为 glasses 数组
  const [err, setErr] = useState('')
  const [exporting, setExporting] = useState(false)

  // 分享图片:先把需要的图片(头像/色卡素材)并行预加载(异步不卡 UI,素材大多已在
  // 页面缓存),再用 canvas 直接绘制成 PNG,全程不克隆 DOM、不逐节点算样式。
  const exportShare = async () => {
    if (exporting || !shareCols) return
    setExporting(true)
    try {
      const srcs = new Set()
      srcs.add(imgURL('dazzling/' + GLASS_BG))
      srcs.add(imgURL('dazzling/' + GLASS_BG2))
      for (const col of shareCols) {
        for (const card of col.cards) {
          srcs.add(imgURL(card.head))
          if (card.type === 2) {
            const src = GLASS_HIDDEN[card.value]
            if (src) srcs.add(imgURL('dazzling/' + src))
          } else {
            const p = GLASS_PARTICLES[card.value >> 20]
            if (p) srcs.add(imgURL('dazzling/' + p))
          }
        }
      }
      const imgs = new Map()
      await Promise.all([...srcs].map(async (src) => { imgs.set(src, await loadImg(src)) }))
      const url = drawShareCanvas(shareCols, imgs)
      const a = document.createElement('a')
      a.href = url
      const d = new Date()
      const ymd = `${d.getFullYear()}${String(d.getMonth() + 1).padStart(2, '0')}${String(d.getDate()).padStart(2, '0')}`
      a.download = `炫彩图鉴_${ymd}.png`
      a.click()
    } catch (e) {
      alert('导出失败,请重试')
    } finally {
      setExporting(false)
    }
  }

  useEffect(() => {
    let dead = false
    setErr('')
    getHandbookGlasses().then((d) => {
      if (!dead) setData((d && d.glasses) || [])
    }).catch((e) => {
      if (!dead) { setData([]); setErr(e.message) }
    })
    return () => { dead = true }
  }, [])

  const stats = useMemo(() => {
    if (!data) return null
    let common = 0
    let hidden = 0
    for (const it of data) {
      common += (it.common || []).length
      hidden += (it.hidden || []).length
    }
    return { species: data.length, common, hidden }
  }, [data])

  // 按主色(ui_color_1)分组:普通炫彩 value = (粒子id<<20)|配色id,主色 = GLASS_COLORS[配色id][0]。
  // 每组:cards = 该主色下已收集的全部色卡(value 粒度,横排);pets = 拥有该主色色卡的精灵(头像+其色卡)。
  const majorGroups = useMemo(() => {
    if (!data) return null
    const groups = new Map() // 主色 hex -> { cards: Map<value,value>, pets: Map<base, {base,head,name,book,values:Set> } }
    for (const it of data) {
      for (const v of it.common || []) {
        const colors = GLASS_COLORS[v & 0xFFFFF]
        if (!colors) continue
        const major = colors[0]
        let g = groups.get(major)
        if (!g) {
          g = { major, cards: new Map(), pets: new Map() }
          groups.set(major, g)
        }
        g.cards.set(v, v)
        let pet = g.pets.get(it.base)
        if (!pet) {
          pet = { base: it.base, head: it.head, name: it.name, book: it.book, values: new Set() }
          g.pets.set(it.base, pet)
        }
        pet.values.add(v)
      }
    }
    return [...groups.entries()]
      .map(([major, g]) => ({
        major,
        name: MAJOR_NAMES[major] || major,
        cards: [...g.cards.values()].sort((a, b) => a - b),
        pets: [...g.pets.values()]
          .map((p) => ({ ...p, values: [...p.values].sort((a, b) => a - b) }))
          .sort((a, b) => a.base - b.base),
      }))
      // 组序按组内最小配色 id(与 GLASS_COLORS 键序一致)
      .sort((a, b) => (a.cards[0] & 0xFFFFF) - (b.cards[0] & 0xFFFFF))
  }, [data])

  // 分享图 8 列数据:每列一个颜色分类,列内垂直堆叠该分类下的宠物卡片。
  // ① 只取最高形态:同一进化链(evo 分组)只保留进化阶段(stage)最高的那条形态,
  //    同阶段分支进化取 petbase 小者;无进化链(evo==0)各自成组,不受影响。
  // ② 卡片 = (宠物 × 分类):同一宠物有多个同分类炫彩只取第一张,跨分类
  //    (不同色系的炫彩)可出现在多列;普通炫彩按主色归入 6 个彩色系,隐藏炫彩
  //    1000 归黑白、1/2/3 赛季归赛季彩色。
  // ③ 每张卡带 type/value:分享图用真实炫彩色卡(GlassChip 三层合成/隐藏整图)渲染,
  //    头像镶嵌在色卡上面。
  const shareCols = useMemo(() => {
    if (!data) return null
    const top = new Map() // 最高形态归并:key = evo 分组
    for (const it of data) {
      const k = it.evo || it.base
      const cur = top.get(k)
      if (!cur || it.stage > cur.stage || (it.stage === cur.stage && it.base < cur.base)) {
        top.set(k, it)
      }
    }
    const items = [...top.values()].sort((a, b) => a.book - b.book || a.base - b.base)
    const cols = SHARE_COLS.map((c) => ({ key: c.key, name: c.name, color: c.color, cards: [] }))
    const seen = cols.map(() => new Set()) // 每列按 base 去重
    for (const it of items) {
      for (const v of it.common || []) {
        const colors = GLASS_COLORS[v & 0xFFFFF]
        if (!colors) continue
        const key = MAJOR_TO_SHARE[colors[0]]
        if (!key) continue
        const ci = SHARE_COLS.findIndex((c) => c.key === key)
        const s = seen[ci]
        if (s.has(it.base)) continue
        s.add(it.base)
        cols[ci].cards.push({ base: it.base, book: it.book, name: it.name, head: it.head, type: 1, value: v })
      }
      for (const v of it.hidden || []) {
        const key = v === 1000 ? 'mono' : 'season'
        const ci = SHARE_COLS.findIndex((c) => c.key === key)
        const s = seen[ci]
        if (s.has(it.base)) continue
        s.add(it.base)
        cols[ci].cards.push({ base: it.base, book: it.book, name: it.name, head: it.head, type: 2, value: v })
      }
    }
    for (const c of cols) c.cards.sort((a, b) => a.base - b.base)
    return cols
  }, [data])

  return (
    <div className="hb-page">
      <div className="hb-head">
        <h2 className="hb-title">✨ 炫彩图鉴</h2>
        <span className="hb-note">数据来自登录包快照(每次登录更新),非实时</span>
        {stats && stats.species > 0 && (
          <button className="btn hb-share-btn" onClick={exportShare} disabled={exporting}>
            {exporting ? '生成中…' : '分享图片'}
          </button>
        )}
      </div>

      {data === null ? (
        <div className="muted">加载中…</div>
      ) : (
        <>
          {stats.species > 0 && (
            <div className="hb-stats">
              <span className="hb-stat">收集品种 <b>{stats.species}</b></span>
              <span className="hb-stat">普通炫彩 <b>{stats.common}</b></span>
              <span className="hb-stat">隐藏炫彩 <b>{stats.hidden}</b></span>
            </div>
          )}

          {majorGroups && majorGroups.length > 0 && (
            <div className="hb-major">
              <div className="hb-major-title">按主色分组</div>
              {majorGroups.map((g) => (
                <div className="hb-major-group" key={g.major}>
                  <div className="hb-major-head">
                    <span className="hb-major-dot" style={{ background: g.major }} />
                    <span className="hb-major-name">{g.name}<em>{g.major}</em></span>
                    <span className="hb-major-count">{g.cards.length} 色卡 · {g.pets.length} 品种</span>
                  </div>
                  <div className="hb-major-cards">
                    {g.cards.map((v) => <GlassChip key={v} p={{ glassType: 1, glassValue: v }} />)}
                  </div>
                  <div className="hb-major-bar">
                    {g.pets.map((p) => (
                      <span className="hb-major-pet" key={p.base} title={`${p.name} · 图鉴 #${p.book}`}>
                        <img className="hb-major-head-ic" src={imgURL(p.head)} alt={p.name} loading="lazy" />
                        <span className="hb-major-pet-cards">
                          {p.values.map((v) => <GlassChip key={v} p={{ glassType: 1, glassValue: v }} />)}
                        </span>
                        <span className="hb-major-pet-name">{p.name}</span>
                      </span>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          )}

          {stats.species === 0 && !err ? (
            <div className="hb-empty">
              <span className="hb-empty-ic">{icons.colorful ? <img src={imgURL(icons.colorful)} alt="" /> : '彩'}</span>
              <p>还没有收集到炫彩。</p>
              <p className="hb-empty-sub">登录一次游戏后自动同步图鉴记录;同一品种的多个炫彩变体都会按图鉴号汇总在这里。</p>
            </div>
          ) : err ? (
            <div className="hb-empty">
              <p className="hb-empty-sub">加载失败:{err}</p>
            </div>
          ) : (
            <div className="hb-list">
              {data.map((it) => (
                <div className="hb-card" key={it.base}>
                  <img
                    className="hb-avatar"
                    src={imgURL(it.head)}
                    alt={it.name || `图鉴 #${it.book}`}
                    loading="lazy"
                  />
                  <div className="hb-meta">
                    <div className="hb-name">
                      {it.name || '未知品种'}
                      <span className="hb-book">图鉴 #{it.book}</span>
                    </div>
                    <div className="hb-cards">
                      {(it.common || []).length > 0 && (
                        <span className="hb-group">
                          <span className="hb-group-label">普通</span>
                          {it.common.map((v) => (
                            <GlassChip key={v} p={{ glassType: 1, glassValue: v }} />
                          ))}
                        </span>
                      )}
                      {(it.hidden || []).length > 0 && (
                        <span className="hb-group">
                          <span className="hb-group-label">隐藏</span>
                          {it.hidden.map((v) => (
                            <GlassChip key={v} p={{ glassType: 2, glassValue: v }} />
                          ))}
                        </span>
                      )}
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </>
      )}
    </div>
  )
}
