import React, { useCallback, useState, useContext, useMemo } from 'react'
import { getHandbookGlasses } from '../../api'
import { AccountContext, AccountNameContext, IconsContext } from '../../context'
import { imgURL } from '../../components/icons'
import { GlassChip } from '../../components/badges'
import { Skeleton } from '../../components/Skeleton'
import { toast } from '../../components/toast'
import { useAsyncData } from '../../hooks/useAsyncData'
import { GLASS_BG, GLASS_BG2, GLASS_PARTICLES, GLASS_COLORS, GLASS_HIDDEN } from '../../data/glassConf'
import { SHARE_COLS, MAJOR_TO_SHARE, loadImg, drawShareCanvas } from './share'

// 主色(ui_color_1)→ 显示名。11 种主色按 GLASS_COLORS 键序(见 web/src/data/glassConf.js)。
const MAJOR_NAMES = {
  '#3c32cf': '深蓝紫', '#83d256': '草绿', '#3ed7d7': '青',
  '#b869ed': '紫', '#e5576f': '绯红', '#bac8fb': '淡蓝',
  '#aedfae': '淡绿', '#a8e5e5': '淡青', '#dbbceb': '淡紫',
  '#fdcdd3': '淡粉', '#f1d397': '淡黄',
}

export default function HandbookGlasses() {
  const account = useContext(AccountContext) // 切账号需重取(图鉴快照按账号隔离)
  const accountName = useContext(AccountNameContext) // 当前账号昵称,分享图标题「xx的炫彩色卡统计」用
  const icons = useContext(IconsContext)
  // data:null=加载中,其余为 glasses 数组
  const { data, error } = useAsyncData(
    useCallback(async () => (await getHandbookGlasses()).glasses || [], []),
    { reloadKey: account },
  )
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
      const total = shareCols.reduce((s, c) => s + c.cards.length, 0)
      const url = drawShareCanvas(shareCols, imgs, accountName || '我的', total)
      const a = document.createElement('a')
      a.href = url
      const d = new Date()
      const ymd = `${d.getFullYear()}${String(d.getMonth() + 1).padStart(2, '0')}${String(d.getDate()).padStart(2, '0')}`
      a.download = `炫彩图鉴_${ymd}.png`
      a.click()
    } catch {
      // toast 而非 alert:不阻塞页面、样式跟主题。错误信息停留久一点(4s)便于阅读。
      toast('导出失败,请重试', 4000)
    } finally {
      setExporting(false)
    }
  }

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
        cards: [...g.cards.values()].sort((a, b) => (a & 0xFFFFF) - (b & 0xFFFFF) || a - b),
        pets: [...g.pets.values()]
          .map((p) => ({ ...p, values: [...p.values].sort((a, b) => a - b) }))
          .sort((a, b) => a.base - b.base),
      }))
      // 组序按组内最小配色 id(与 GLASS_COLORS 键序一致)
      .sort((a, b) => (a.cards[0] & 0xFFFFF) - (b.cards[0] & 0xFFFFF))
  }, [data])

  // 分享图 8 列数据:每列一个颜色分类,列内垂直堆叠该分类下的炫彩色卡卡片。
  // ① 只取最高形态:同一进化链(evo 分组)只保留进化阶段(stage)最高的那条形态作为
  //    展示条目(头像/名字),同阶段分支进化取 petbase 小者;无进化链(evo==0)各自成组。
  //    但该链各形态收集到的炫彩(value)全部合并,不丢低形态的色卡。
  // ② 卡片 = 一张炫彩变体:同一宠物有多个同分类炫彩时每张都展示(不再按 base 去重),
  //    跨分类(不同色系的炫彩)出现在不同列;普通炫彩按主色归入 6 个彩色系,隐藏炫彩
  //    1000 归黑白、1/2/3 赛季归赛季彩色。
  // ③ 每张卡带 type/value:分享图用真实炫彩色卡(GlassChip 三层合成/隐藏整图)渲染,
  //    头像透明背景直接叠在色卡左上角。
  const shareCols = useMemo(() => {
    if (!data) return null
    const chains = new Map() // key(evo||base) -> { it: 最高形态条目, common:Set, hidden:Set }
    for (const it of data) {
      const k = it.evo || it.base
      let c = chains.get(k)
      if (!c) {
        c = { it, common: new Set(), hidden: new Set() }
        chains.set(k, c)
      } else if (it.stage > c.it.stage || (it.stage === c.it.stage && it.base < c.it.base)) {
        c.it = it // 更高形态(同阶段取更小 base)作为展示条目
      }
      for (const v of it.common || []) c.common.add(v)
      for (const v of it.hidden || []) c.hidden.add(v)
    }
    const items = [...chains.values()]
      .map((c) => ({ base: c.it.base, book: c.it.book, name: c.it.name, head: c.it.head, common: [...c.common], hidden: [...c.hidden] }))
      .sort((a, b) => a.book - b.book || a.base - b.base)
    const cols = SHARE_COLS.map((c) => ({ key: c.key, name: c.name, color: c.color, cards: [] }))
    for (const it of items) {
      for (const v of it.common) {
        const colors = GLASS_COLORS[v & 0xFFFFF]
        if (!colors) continue
        const key = MAJOR_TO_SHARE[colors[0]]
        if (!key) continue
        const ci = SHARE_COLS.findIndex((c) => c.key === key)
        cols[ci].cards.push({ base: it.base, book: it.book, name: it.name, head: it.head, type: 1, value: v })
      }
      for (const v of it.hidden) {
        const key = v === 1000 ? 'mono' : 'season'
        const ci = SHARE_COLS.findIndex((c) => c.key === key)
        cols[ci].cards.push({ base: it.base, book: it.book, name: it.name, head: it.head, type: 2, value: v })
      }
    }
    // 列内排序:普通炫彩按配色 id(先按主色 ui_color_1 分组、组内副色 ui_color_2 排完
    // 再排下一主色,同配色再按宠物);隐藏炫彩按 value 升序(赛季 1→2→3,黑白 1000)。
    for (const c of cols) {
      c.cards.sort((a, b) => {
        const ka = a.type === 1 ? a.value & 0xFFFFF : a.value
        const kb = b.type === 1 ? b.value & 0xFFFFF : b.value
        return ka - kb || a.base - b.base
      })
    }
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
        // 骨架屏(P5.4.2):直接复用 .hb-major-group / .hb-major-cards 等真实布局类,
        // 由它们提供确切的盒模型(内边距、换行、42×23 的色卡格),骨架只负责填色 ——
        // 这样骨架与内容是**结构上同构**的,数据到位时不产生布局位移。
        <div role="status" aria-label="加载中">
          <Skeleton h={20} w={280} style={{ marginBottom: 12 }} />
          {[0, 1, 2].map((g) => (
            <div className="hb-major-group" key={g} aria-hidden="true">
              <div className="hb-major-head"><Skeleton h={16} w={150} /></div>
              <div className="hb-major-cards">
                {Array.from({ length: 18 }, (_, i) => <Skeleton key={i} w={42} h={23} />)}
              </div>
            </div>
          ))}
        </div>
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

          {stats.species === 0 && !error ? (
            <div className="hb-empty">
              <span className="hb-empty-ic">{icons.colorful ? <img src={imgURL(icons.colorful)} alt="" /> : '彩'}</span>
              <p>还没有收集到炫彩。</p>
              <p className="hb-empty-sub">登录一次游戏后自动同步图鉴记录;同一品种的多个炫彩变体都会按图鉴号汇总在这里。</p>
            </div>
          ) : error ? (
            <div className="hb-empty">
              <p className="hb-empty-sub">加载失败:{error.message}</p>
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
