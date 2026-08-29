import { imgURL } from '../../components/icons'
import { GLASS_BG, GLASS_BG2, GLASS_PARTICLES, GLASS_COLORS, GLASS_HIDDEN } from '../../data/glassConf'

// 炫彩图鉴分享图:纯 canvas 直接绘制(不再用 html-to-image 克隆 DOM,避免导出时主线程卡顿;
// 图片异步预加载不阻塞 UI,绘制阶段仅 drawImage/蒙版合成,毫秒级)。
// 本文件不依赖 React,可单独测试。

export const SHARE_COLS = [
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
// 组件侧(分组展示)也要用它把主色映射到分享图列,故导出。
export const MAJOR_TO_SHARE = {
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
export const loadImg = (src) => new Promise((resolve, reject) => {
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

// 单张卡片:真实炫彩色卡(普通三层合成 / 隐藏整图)+ 左上角透明背景头像 + 白字名字
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
  // 头像:直接画在左上角(保留图片透明背景,不加白底圆),contain 等比不裁切
  const head = imgs.get(imgURL(card.head))
  if (head) {
    const s = Math.min(34 / head.naturalWidth, 34 / head.naturalHeight)
    const dw = head.naturalWidth * s
    const dh = head.naturalHeight * s
    ctx.drawImage(head, x + 5, y + 5, dw, dh)
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

// 白色背景 900px 分享图:标题「xx的炫彩色卡统计」+ 总数 + 8 色图例 +
// 8 列瀑布(列顶齐列底参差)+ 底部灰色水印
export const drawShareCanvas = (cols, imgs, owner, total) => {
  const W = SHARE_W
  const colW = (W - SHARE_PAD * 2 - SHARE_COLS_GAP * (cols.length - 1)) / cols.length
  const cardH = colW * 154 / 280
  const titleY = 26 + 19
  const statY = titleY + 12 // 「共收集 N 张炫彩色卡」
  const legendY = statY + 9 + 9
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
  // 标题:xx的炫彩色卡统计(xx=当前账号,未登录时显示「我的」)
  ctx.font = `800 24px ${SHARE_FONT}`
  ctx.textAlign = 'center'
  ctx.textBaseline = 'alphabetic'
  ctx.fillStyle = '#222'
  ctx.fillText(`${owner}的炫彩色卡统计`, W / 2, titleY)
  // 总数:共收集 N 张炫彩色卡
  ctx.font = `400 13px ${SHARE_FONT}`
  ctx.fillStyle = '#888'
  ctx.fillText(`共收集 ${total} 张炫彩色卡`, W / 2, statY)
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
