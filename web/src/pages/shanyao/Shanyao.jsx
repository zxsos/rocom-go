import React, { useCallback, useContext, useEffect, useState } from 'react'
import { getCalcRules, getShanyao, subscribe } from '../../api'
import { AccountContext } from '../../context'
import { useAsyncData } from '../../hooks/useAsyncData'
import ArenaField, { keyOf } from './ArenaField'
import DamagePanel from './DamagePanel'

// 闪耀大赛(隐藏模块):同步「当前这一局 PVP」。
//
// 数据来源与更新时机(全部被动,靠抓包,不碰游戏客户端):
//   - 载入时 GET /api/shanyao 回显后端缓存的最近一份快照;
//   - 之后由 SSE 的 shanyao 消息**整份覆盖**(服务器每次都发全量,前端不做增量合并);
//   - 页面只展示,不向游戏发任何指令。
//
// 隐藏入口:导航里没有这一项,需手动输入 #/shanyao(与 #/debug、#/admin 同款做法)。
export default function Shanyao() {
  const account = useContext(AccountContext)
  const { data, setData, refresh, loading } = useAsyncData(useCallback(() => getShanyao(), []), { reloadKey: account })
  const [selection, setSelection] = useState({ atkKey: '', defKey: '', skillId: '' })
  // 规则常量(克制表等)不随账号:拉一次即可,换账号不必重取。
  const { data: rules } = useAsyncData(useCallback(() => getCalcRules(), []), {})

  // 断线重连时补拉一次:SSE 断开期间的消息全丢了,不补就一直停在旧战局。
  useEffect(() => subscribe('shanyao', (d) => setData(d), { onOpen: refresh }), [setData, refresh])

  // 换了新的一局就清掉选中:上一局选的精灵在新局里已经不在场了。
  useEffect(() => { setSelection({ atkKey: '', defKey: '', skillId: '' }) }, [data?.battleId, account])

  const pick = useCallback((patch) => setSelection((s) => ({ ...s, ...patch })), [])
  const onSelect = useCallback((which, side, pet) => {
    // 点卡片即把它塞进对应的一侧(攻方取我方、守方取对手是默认直觉,但两边都能选)。
    setSelection((s) => (which === 'atk' ? { ...s, atkKey: keyOf(side, pet) } : { ...s, defKey: keyOf(side, pet) }))
  }, [])

  const self = data?.self
  const foe = data?.foe
  const hasBattle = !!data && (data.active || (self && self.pets && self.pets.length))

  return (
    <div className="shanyao-page">
      <div className="toolbar">
        <h3 style={{ margin: 0 }}>闪耀大赛</h3>
        <span className="muted toolbar-hint">
          {data?.active ? '正在同步游戏内的对战' : hasBattle ? '最近一局已结束' : '当前没有进行中的对战'}
        </span>
        <div className="spacer" />
        {data?.mode ? <span className="muted">模式 {data.mode}</span> : null}
      </div>

      {!hasBattle ? (
        <div className="sy-empty">
          <p className="sy-empty-t">当前没有进行中的对战</p>
          <p className="muted">
            进入 PVP（排位 / 资格赛）后，双方阵容会在这里按「左下我方、右上他方」自动排开；
            血量随每回合刷新。本模块仅读取抓包数据，不会向游戏发送任何指令。
          </p>
          {loading ? <p className="muted">载入中…</p> : null}
        </div>
      ) : (
        <>
          <ArenaField self={self} foe={foe} round={data?.round} selection={selection} onSelect={onSelect} />
          <DamagePanel self={self} foe={foe} selection={selection} onPick={pick} rules={rules} />
        </>
      )}

      <p className="sy-foot muted">
        数据来源：进战（0x1316）→ 回合开始（0x131a，补齐对手阵容与在场状态）→ 演出（0x1324，当前血量）→
        结算（0x132c）。对手的六维与天赋服务端不下发，未出场精灵的等级/技能也可能缺失，这些字段一律留空显示「—」，不做推测。
      </p>
    </div>
  )
}
