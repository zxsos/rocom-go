import React, { useState } from 'react'
import { adminInjectWild, adminInjectFlower, adminRevokeInject } from '../../api'
import Dropdown from '../../components/Dropdown'
import { uidOf } from '../../data/nav'
import GlassPicker, { glassOf } from './GlassPicker'

// 两个「投放」功能卡片:向下拉选定的成员注入一只稀有野生精灵或假炫彩花种。
// 都只影响前端显示、不修改真实流量,故放同一文件(共享形态下拉的渲染逻辑)。
// 账号/形态列表由 Admin 统一持有(多个卡片共用),投放成功后回调 onInjectsChanged 刷新注入列表。

// accountOptions 目标玩家下拉项:在线标绿、离线标红(仅投放精灵要求在线)。
const accountOptions = (accounts) => [
  { value: '', label: '选择目标玩家' },
  ...accounts.map((a) => ({ value: a.account, label: `${a.online ? '🟢 ' : '🟥 '}${a.name} (UID:${uidOf(a.account)})` })),
]

// InjectWildCard 投放稀有野生精灵:位置取成员最近一次缓存位置,按当前场景投影到附近 N 米处。
// 成员需在「有底图的场景」中且有缓存位置才能投放成功;异色只列有异色形态的精灵。
export function InjectWildCard({ accounts, wildOptions, injects, onInjectsChanged }) {
  const [injAccount, setInjAccount] = useState('')
  const [injBase, setInjBase] = useState('')
  const [injKind, setInjKind] = useState('shiny')
  const [injOffset, setInjOffset] = useState(30)
  const [injLevel, setInjLevel] = useState(0) // 0=随机 30-60;指定 1-100 固定等级
  const [injErr, setInjErr] = useState('')
  const [injMsg, setInjMsg] = useState('')
  const [injGlass, setInjGlass] = useState({ type: 'random', particle: 1, color: 1, hidden: 1 })

  const injectWild = async (e) => {
    e.preventDefault()
    const account = injAccount.trim()
    if (!account || !injBase) return
    setInjErr(''); setInjMsg('')
    try {
      const { glassType, glassValue } = glassOf(injGlass)
      const res = await adminInjectWild(
        account, Number(injBase), injKind, Number(injOffset) || 30, Number(injLevel) || 0, glassType, glassValue,
      )
      setInjMsg('已投放:' + (res.id || '') + '(u=' + (res.u != null ? res.u.toFixed(3) : '') + ')')
      onInjectsChanged()
    } catch (err) {
      setInjErr(err.message || '投放失败')
    }
  }

  const revokeInject = async (account, id) => {
    setInjErr('')
    try {
      await adminRevokeInject(account, id)
      setInjMsg('已撤销注入:' + id)
      onInjectsChanged()
    } catch (err) {
      setInjErr(err.message || '撤销失败')
    }
  }

  return (
    <div className="admin-card admin-rules">
      <h3>投放稀有野生精灵</h3>
      <p className="admin-hint">
        向下拉选定的成员实时地图注入一只稀有野生精灵(异色 / 炫彩)。
        位置取该成员最近一次缓存位置,按当前场景投影到附近 30 米处;仅影响前端地图显示,
        不修改真实流量。成员需在「有底图的场景」中且有缓存位置才能投放成功;异色只列有异色形态的精灵。
      </p>
      <form onSubmit={injectWild} className="admin-inject-form">
        <Dropdown
          value={injAccount}
          options={accountOptions(accounts)}
          onChange={(v) => setInjAccount(v)}
          title="选择投放目标玩家(需在线且有缓存位置)"
        />
        <Dropdown
          value={injBase}
          options={[
            { value: '', label: '选择精灵形态' },
            ...(wildOptions || [])
              .filter((o) => injKind !== 'shiny' || o.shiny) // 异色只列有异色形态的精灵
              .map((o) => ({ value: o.base, label: `${o.name}(#${o.book})` })),
          ]}
          onChange={(v) => setInjBase(v)}
        />
        <Dropdown
          value={injKind}
          options={[
            { value: 'shiny', label: '异色' },
            { value: 'colorful', label: '炫彩' },
          ]}
          onChange={(v) => { setInjKind(v); setInjBase('') }}
        />
        {/* 炫彩色卡设置:仅炫彩投放时展示;选择后投出的精灵在角标/悬浮面板/详情里显示对应色卡 */}
        {injKind === 'colorful' && <GlassPicker value={injGlass} onChange={setInjGlass} />}
        <input
          className="input" type="number" min="1" max="200" value={injOffset}
          onChange={(e) => setInjOffset(e.target.value)} title="距玩家位置米数"
        />
        <input
          className="input" type="number" min="0" max="100" value={injLevel}
          onChange={(e) => setInjLevel(e.target.value)}
          title="等级(0=随机 30-60,指定 1-100 固定该等级)" placeholder="Lv(0=随机)"
        />
        <button className="btn primary" type="submit" disabled={!injAccount.trim() || !injBase}>
          投放
        </button>
      </form>
      {injErr && <p className="admin-error">{injErr}</p>}
      {injMsg && <p className="admin-hint" style={{ color: 'var(--green, #4caf50)' }}>{injMsg}</p>}
      {injects && injects.length > 0 && (
        <div className="admin-inject-list">
          <h4>当前注入中({injects.length})</h4>
          <ul>
            {injects.map((it) => (
              <li key={it.account + ':' + it.id}>
                <span className="admin-inject-name">{it.name}</span>
                <span className="admin-inject-kind">{(it.kinds || []).join('/') || '普通'}</span>
                {it.kind === 'flower' && <span className="admin-inject-flower">花种</span>}
                {it.kind !== 'flower' && <span className="muted admin-inject-scene">{it.sceneRes || '无底图'}</span>}
                <button className="btn ghost" onClick={() => revokeInject(it.account, it.id)}>撤销</button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}

// InjectFlowerCard 投放假炫彩花种:向目标成员的花种页注入一只特殊花种(默认 7 星,星级可自定义),
// 不要求其在线;生命周期由管理员在此手动撤销(见 InjectWildCard 里的「当前注入中」列表)。
export function InjectFlowerCard({ accounts, wildOptions, onInjectsChanged }) {
  const [flAccount, setFlAccount] = useState('')
  const [flBase, setFlBase] = useState('')
  const [flStar, setFlStar] = useState(7) // 花种星级 1-7
  const [flGlass, setFlGlass] = useState({ type: 'random', particle: 1, color: 1, hidden: 1 })
  const [flErr, setFlErr] = useState('')
  const [flMsg, setFlMsg] = useState('')

  const injectFlower = async (e) => {
    e.preventDefault()
    const account = flAccount.trim()
    if (!account || !flBase) return
    setFlErr(''); setFlMsg('')
    try {
      const { glassType, glassValue } = glassOf(flGlass)
      const res = await adminInjectFlower(account, Number(flBase), Number(flStar), glassType, glassValue)
      setFlMsg('已投放花种:' + (res.id || '') + '(npcLogicId=' + res.npcLogicId + ')')
      onInjectsChanged()
    } catch (err) {
      setFlErr(err.message || '投放失败')
    }
  }

  return (
    <div className="admin-card admin-rules">
      <h3>投放假炫彩花种</h3>
      <p className="admin-hint">
        向选定成员的花种页注入一只假花种(默认 7 星特殊花灵,星级可自定义),携带指定或
        随机炫彩色卡,卡片与真实花种无异,点开可查看完整色卡。不要求成员在线,不修改
        真实流量;生命周期由管理员在此手动撤销(见上方「当前注入中」列表)。
      </p>
      <form onSubmit={injectFlower} className="admin-inject-form">
        <Dropdown
          value={flAccount}
          options={accountOptions(accounts)}
          onChange={(v) => setFlAccount(v)}
          title="选择投放目标玩家(无需在线)"
        />
        <Dropdown
          value={flBase}
          options={[
            { value: '', label: '选择守护宠物' },
            ...(wildOptions || []).map((o) => ({ value: o.base, label: `${o.name}(#${o.book})` })),
          ]}
          onChange={(v) => setFlBase(v)}
        />
        <Dropdown
          value={flStar}
          options={[1, 2, 3, 4, 5, 6, 7].map((n) => ({ value: n, label: `${n} 星${n === 7 ? '(花灵 BOSS)' : ''}` }))}
          onChange={(v) => setFlStar(Number(v))}
          title="花种星级(1-7)"
        />
        <GlassPicker value={flGlass} onChange={setFlGlass} />
        <button className="btn primary" type="submit" disabled={!flAccount.trim() || !flBase}>
          投放
        </button>
      </form>
      {flErr && <p className="admin-error">{flErr}</p>}
      {flMsg && <p className="admin-hint" style={{ color: 'var(--green, #4caf50)' }}>{flMsg}</p>}
    </div>
  )
}
