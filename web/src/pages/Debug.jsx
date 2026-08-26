import React, { useState, useEffect, useRef, useContext, useMemo } from 'react'
import { subscribe } from '../api'
import { AccountContext } from '../context'
import { useStoredJSON } from '../hooks/useStoredState'
import { fmtClock } from '../utils/format'
import { copyText } from '../utils/clipboard'
import { Highlight } from '../components/Highlight'

// 默认忽略高频且无分析价值的场景 NPC 位置同步(每秒多条,会淹没事件流)。
// localStorage 无该键时用默认值;用户清空后存 [] 且不再回落默认。
const DEFAULT_IGNORED = ['ZONE_SCENE_SET_NPC_POS_REQ', 'ZONE_SCENE_SET_NPC_POS_RSP', 'ZONE_SCENE_PLAY_ACTS_NOTIFY']

// 内容搜索时最多扫描多少条「名称/opcode 未命中」的最近消息(逐条走后端解析,量级由本页可容忍的延迟决定)。
const CONTENT_SCAN_MAX = 100

export default function Debug() {
  const account = useContext(AccountContext)
  const [rows, setRows] = useState([])
  const [paused, setPaused] = useState(false)
  const [filter, setFilter] = useState('')
  const [ignored, setIgnored] = useStoredJSON(localStorage, 'debugIgnore', DEFAULT_IGNORED)
  // 忽略集合放进 ref,避免每次增删都重新订阅;订阅回调里以名称精确匹配丢弃。
  const ignoredRef = useRef(ignored)
  ignoredRef.current = ignored
  // 展开的数据详情:key 标识行;点击 ▶ 时向后端请求按 opcode 解析(精确解码,失败退 wire 级)。
  const [open, setOpen] = useState(null) // {key, name, hex, loading, text}

  // 解析结果缓存:稳定 key(time:opcode:dir:hex,不随列表滚动变化)→ {loading, text}。
  // 展开与内容搜索共用同一份,避免同一条消息重复请求后端。
  const cacheRef = useRef(new Map())
  const [tick, setTick] = useState(0) // 缓存写入后 +1,触发 shown 重算(它读 cacheRef)
  const [searching, setSearching] = useState(false) // 内容搜索(逐条解析)进行中
  const [copied, setCopied] = useState('') // 复制反馈:'' | 'text' | 'hex'

  const rowKey = (r, i) => i + ':' + r.time + ':' + r.opcode + ':' + r.dir
  const stableKey = (r) => r.time + ':' + r.opcode + ':' + r.dir + ':' + r.hex
  const parseBody = (r) => JSON.stringify({ opcode: parseInt(r.opcode, 16), dir: r.dir, hex: r.hex })

  const toggleRow = async (r, i) => {
    const key = rowKey(r, i)
    if (open && open.key === key) { setOpen(null); return }
    if (!r.hex) return
    const skey = stableKey(r)
    const cached = cacheRef.current.get(skey)
    if (cached && !cached.loading) { // 内容搜索已解析过,直接复用
      setOpen({ key, name: r.name, hex: r.hex, loading: false, text: cached.text })
      return
    }
    setOpen({ key, name: r.name, hex: r.hex, loading: true, text: '' })
    try {
      const res = await fetch('/api/debug/parse', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: parseBody(r),
      })
      const j = await res.json()
      const text = j.text || j.error || '无内容'
      cacheRef.current.set(skey, { loading: false, text })
      setOpen((o) => (o && o.key === key ? { ...o, loading: false, text } : o))
    } catch (err) {
      const text = '解析请求失败: ' + err.message
      cacheRef.current.set(skey, { loading: false, text })
      setOpen((o) => (o && o.key === key ? { ...o, loading: false, text } : o))
    }
  }

  const doCopy = async (which, text) => {
    if (await copyText(text)) {
      setCopied(which)
      setTimeout(() => setCopied((c) => (c === which ? '' : c)), 1200)
    }
  }

  // 仅本页(且未暂停)才订阅高频 debug 流;暂停 = 关闭连接,服务端随之停止推送,而非前端丢弃。
  // account 变化时重订阅(切换账号后只看新账号的流量)。
  useEffect(() => {
    if (paused) return
    return subscribe((m) => {
      if (m.type !== 'debug') return
      if (ignoredRef.current.includes(m.data.name)) return // 忽略的 opcode 直接不入缓冲,避免挤掉有用事件
      setRows((r) => [m.data, ...r].slice(0, 800))
    }, { debug: true })
  }, [paused, account])

  // 内容搜索:输入关键词后,对名称/opcode 未命中且未缓存的最近消息逐条解析,命中即入列。
  // 用代数(gen)作取消令牌:filter/rows 一变,旧搜索立即作废,新搜索从最新消息重新扫描。
  const genRef = useRef(0)
  useEffect(() => {
    const q = filter.trim().toLowerCase()
    if (!q) { genRef.current++; setSearching(false); return }
    const gen = ++genRef.current
    setSearching(true)
    const id = setTimeout(() => {
      const need = []
      for (let i = rows.length - 1; i >= 0 && need.length < CONTENT_SCAN_MAX; i--) {
        const r = rows[i]
        if ((r.name || '').toLowerCase().includes(q) || (r.opcode || '').includes(q)) continue
        const key = stableKey(r)
        if (!cacheRef.current.has(key)) need.push({ r, key })
      }
      ;(async () => {
        for (const { r, key } of need) {
          if (gen !== genRef.current) return
          cacheRef.current.set(key, { loading: true, text: '' })
          try {
            const res = await fetch('/api/debug/parse', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: parseBody(r),
            })
            const j = await res.json()
            if (gen !== genRef.current) return
            cacheRef.current.set(key, { loading: false, text: j.text || j.error || '' })
            setTick((t) => t + 1)
          } catch {
            if (gen !== genRef.current) return
            cacheRef.current.set(key, { loading: false, text: '' })
          }
        }
        if (gen === genRef.current) setSearching(false)
      })()
    }, 250)
    return () => clearTimeout(id)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filter, rows])

  const addIgnore = (name) => { if (name) setIgnored((s) => (s.includes(name) ? s : [...s, name])) }
  const removeIgnore = (name) => setIgnored((s) => s.filter((n) => n !== name))

  // 过滤:名称 / opcode 即时命中,或展开/内容搜索已解析出的文本中包含关键词。
  const shown = useMemo(() => {
    const q = filter.trim().toLowerCase()
    if (!q) return rows
    return rows.filter((r) => {
      if ((r.name || '').toLowerCase().includes(q) || (r.opcode || '').includes(q)) return true
      const t = cacheRef.current.get(stableKey(r))?.text
      return !!(t && t.toLowerCase().includes(q))
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows, filter, tick])

  return (
    <div>
      <div className="toolbar">
        <h3 style={{ margin: 0 }}>游戏事件流</h3>
        <span className="muted toolbar-hint">实时展示当前账号的应用层消息(opcode);离开本页或暂停即停止拉取。内容搜索自动扫描最近 {CONTENT_SCAN_MAX} 条未匹配消息</span>
        <div className="spacer" />
        <input className="input" style={{ maxWidth: 260 }} placeholder="过滤名称 / opcode / 内容" value={filter} onChange={(e) => setFilter(e.target.value)} />
        <button className="btn" onClick={() => setPaused((p) => !p)}>{paused ? '继续' : '暂停'}</button>
        <button className="btn" onClick={() => setRows([])}>清空</button>
      </div>

      {ignored.length > 0 && (
        <div className="ignore-bar">
          <span className="muted">已忽略:</span>
          {ignored.map((n) => (
            <span key={n} className="chip on" title="点击取消忽略" onClick={() => removeIgnore(n)}>{n} ✕</span>
          ))}
        </div>
      )}

      <div className="table-wrap" style={{ display: 'block' }}>
        <table className="debug-table">
          <tbody>
            {shown.map((r, i) => {
              const key = rowKey(r, i)
              return (
                <React.Fragment key={i}>
                  <tr>
                    <td className="muted dbg-time">{fmtClock(r.time)}</td>
                    <td className={r.dir === 'c2s' ? 'dir-c2s' : 'dir-s2c'}>{r.dir}</td>
                    <td className="muted dbg-acct">{(r.account || '').replace(/^ip:/, '')}</td>
                    <td className="dbg-op"><Highlight text={r.opcode} query={filter} /></td>
                    <td className="dbg-name"><Highlight text={r.name} query={filter} /></td>
                    <td style={{ whiteSpace: 'nowrap' }}>
                      <button className="btn-ignore" disabled={!r.hex} title={r.hex ? '查看服务器下发数据' : '该消息无数据'}
                        onClick={() => toggleRow(r, i)}>{open && open.key === key ? '▼' : '▶'}</button>
                      <button className="btn-ignore" title="忽略该事件" onClick={() => addIgnore(r.name)}>🚫</button>
                    </td>
                  </tr>
                  {open && open.key === key && (
                    <tr className="dbg-detail">
                      <td colSpan={6}>
                        <div className="dbg-detail-head">
                          <span className="muted">{open.name} 数据:</span>
                          <span className="muted">{open.hex.length / 2}B</span>
                          <span style={{ flex: 1 }} />
                          <button className="btn-dbg-copy" disabled={open.loading} title="复制解析结果"
                            onClick={() => doCopy('text', open.text)}>{copied === 'text' ? '已复制 ✓' : '📋 复制解析'}</button>
                          <button className="btn-dbg-copy" disabled={open.loading} title="复制原始 hex"
                            onClick={() => doCopy('hex', open.hex)}>{copied === 'hex' ? '已复制 ✓' : '📋 复制 hex'}</button>
                        </div>
                        {open.loading ? <div className="muted">解析中…</div>
                          : <pre className="dbg-tree"><Highlight text={open.text} query={filter} /></pre>}
                        <details className="dbg-hex">
                          <summary className="muted">原始数据 hex({open.hex.length / 2}B)</summary>
                          <code>{open.hex}</code>
                        </details>
                      </td>
                    </tr>
                  )}
                </React.Fragment>
              )
            })}
          </tbody>
        </table>
        {shown.length === 0 && (
          <div className="empty">{filter ? (searching ? '内容搜索中…' : '无匹配(已扫描最近消息)') : '等待事件…(需要后端正在抓包/回放)'}</div>
        )}
      </div>
    </div>
  )
}
