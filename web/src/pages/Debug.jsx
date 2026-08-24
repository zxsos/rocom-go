import React, { useState, useEffect, useRef, useContext } from 'react'
import { subscribe } from '../api'
import { AccountContext } from '../context'
import { useStoredJSON } from '../hooks/useStoredState'
import { fmtClock } from '../utils/format'

// 默认忽略高频且无分析价值的场景 NPC 位置同步(每秒多条,会淹没事件流)。
// localStorage 无该键时用默认值;用户清空后存 [] 且不再回落默认。
const DEFAULT_IGNORED = ['ZONE_SCENE_SET_NPC_POS_REQ', 'ZONE_SCENE_SET_NPC_POS_RSP', 'ZONE_SCENE_PLAY_ACTS_NOTIFY']

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

  const rowKey = (r, i) => i + ':' + r.time + ':' + r.opcode + ':' + r.dir
  const toggleRow = async (r, i) => {
    const key = rowKey(r, i)
    if (open && open.key === key) { setOpen(null); return }
    if (!r.hex) return
    setOpen({ key, name: r.name, hex: r.hex, loading: true, text: '' })
    try {
      const res = await fetch('/api/debug/parse', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ opcode: parseInt(r.opcode, 16), dir: r.dir, hex: r.hex }),
      })
      const j = await res.json()
      setOpen((o) => (o && o.key === key ? { ...o, loading: false, text: j.text || j.error || '无内容' } : o))
    } catch (err) {
      setOpen((o) => (o && o.key === key ? { ...o, loading: false, text: '解析请求失败: ' + err.message } : o))
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

  const addIgnore = (name) => { if (name) setIgnored((s) => (s.includes(name) ? s : [...s, name])) }
  const removeIgnore = (name) => setIgnored((s) => s.filter((n) => n !== name))

  const shown = filter
    ? rows.filter((r) => (r.name || '').toLowerCase().includes(filter.toLowerCase()) || (r.opcode || '').includes(filter))
    : rows

  return (
    <div>
      <div className="toolbar">
        <h3 style={{ margin: 0 }}>游戏事件流</h3>
        <span className="muted toolbar-hint">实时展示当前账号的应用层消息(opcode);离开本页或暂停即停止拉取</span>
        <div className="spacer" />
        <input className="input" style={{ maxWidth: 220 }} placeholder="过滤名称 / opcode" value={filter} onChange={(e) => setFilter(e.target.value)} />
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
                    <td className="dbg-op">{r.opcode}</td>
                    <td className="dbg-name">{r.name}</td>
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
                          {open.hex.length / 2}B
                        </div>
                        {open.loading ? <div className="muted">解析中…</div>
                          : <pre className="dbg-tree">{open.text}</pre>}
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
        {shown.length === 0 && <div className="empty">等待事件…(需要后端正在抓包/回放)</div>}
      </div>
    </div>
  )
}
