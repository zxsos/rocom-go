import React, { useEffect, useState, useContext, useMemo } from 'react'
import { getHandbookGlasses } from '../api'
import { IconsContext } from '../context'
import { imgURL } from '../components/icons'
import { GlassChip } from '../components/badges'
import { GLASS_COLORS } from '../data/glassConf'

// 主色(ui_color_1)→ 显示名。11 种主色按 GLASS_COLORS 键序(见 web/src/data/glassConf.js)。
const MAJOR_NAMES = {
  '#3c32cf': '深蓝紫', '#83d256': '草绿', '#3ed7d7': '青',
  '#b869ed': '紫', '#e5576f': '绯红', '#bac8fb': '淡蓝',
  '#aedfae': '淡绿', '#a8e5e5': '淡青', '#dbbceb': '淡紫',
  '#fdcdd3': '淡粉', '#f1d397': '淡黄',
}

// 炫彩图鉴:按品种聚合展示本账号收集到的普通/隐藏炫彩色卡。
// 数据来自登录包 pet_handbook(每次登录时快照更新),点击色卡可放大预览。
export default function HandbookGlasses() {
  const icons = useContext(IconsContext)
  const [data, setData] = useState(null) // null=加载中,其余为 glasses 数组
  const [err, setErr] = useState('')

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

  return (
    <div className="hb-page">
      <div className="hb-head">
        <h2 className="hb-title">✨ 炫彩图鉴</h2>
        <span className="hb-note">数据来自登录包快照(每次登录更新),非实时</span>
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
