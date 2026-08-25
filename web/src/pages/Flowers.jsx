import React, { useState, useEffect, useContext, useMemo, useCallback } from 'react'
import { getFlowers, getFlowerSlots, deleteFlowerSlot, subscribe } from '../api'
import { AccountContext, IconsContext } from '../context'
import { fmtTime } from '../utils/format'
import { ImgAvatar, imgURL } from '../components/icons'

// 花种页面:渲染 s2c 0x0375 下发的 flower_npcs(花灵)活动 BOSS 分组。
// 只显示花种;world_leader_npcs(世界 BOSS)与 legendary_npcs(传说 NPC)在解析层就丢弃了。
// 数据源:进页面先 GET /api/flowers 回显最近一次分组,之后订阅 SSE flowers 实时覆盖
// (游戏内每打开一次花种面板,服务器就会整组重发 0x0375)。
// 游戏内点击地图上的花种时,服务器会额外下发 0x0338 单只详情(等级/炫彩/绑定宠物/奖牌),
// 由后端合并进对应卡片后经同一 SSE 刷新;未点过的花种这些字段为空。
export default function Flowers() {
  const account = useContext(AccountContext)
  const [data, setData] = useState(null) // null = 尚未收到任何 0x0375
  const [now, setNow] = useState(() => Date.now())
  // 世界存档槽位管理:slots=槽列表(null=加载中),selKey=当前选中槽,slotMsg=删除结果提示。
  const [slots, setSlots] = useState(null)
  const [selKey, setSelKey] = useState('')
  const [slotMsg, setSlotMsg] = useState('')

  useEffect(() => {
    getFlowers().then((v) => { if (v) setData(v) }).catch(() => {})
  }, [account])

  // loadSlots 拉取世界存档槽位列表;切账号/删除后重新拉取,选中项保持有效否则回退到第一项。
  const loadSlots = useCallback(() => {
    getFlowerSlots().then((v) => {
      const list = (v && v.slots) || []
      setSlots(list)
      setSelKey((k) => (k && list.some((s) => s.key === k) ? k : (list[0] && list[0].key) || ''))
    }).catch(() => setSlots([]))
  }, [])

  useEffect(() => { loadSlots() }, [account, loadSlots])

  async function handleDeleteSlot() {
    if (!selKey || selKey === 'self') return
    setSlotMsg('')
    try {
      await deleteFlowerSlot(selKey)
      setSlotMsg('已删除槽 ' + selKey + ',回访该世界会重新建档')
      loadSlots()
    } catch (e) {
      setSlotMsg(e.message)
    }
  }

  useEffect(() => {
    return subscribe((m) => {
      if (m.type !== 'flowers') return
      if (m.account && m.account !== account) return
      setData(m.data)
    })
  }, [account])

  // 活动结束倒计时随时间走:秒级刷新(卡片量少,重渲染开销可忽略)。
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [])

  const flowers = useMemo(() => (data && data.flowers) || [], [data])
  const specials = flowers.filter((f) => f.specSeedId > 0)
  const normals = flowers.filter((f) => !(f.specSeedId > 0))

  return (
    <div className="flowers-page">
      <div className="toolbar">
        <h3 style={{ margin: 0 }}>花种</h3>
        <span className="muted toolbar-hint">打开面板自动更新,点地图花种看详情</span>
        <div className="spacer" />
        {data && <span className="muted">共 {flowers.length} 只花灵</span>}
      </div>
      {/* 世界存档槽位管理:下拉选择槽查看存储的花种,可手动删除(自己世界 self 不可删) */}
      <section className="slot-manager">
        <div className="slot-toolbar">
          <h4 style={{ margin: 0 }}>世界存档槽位</h4>
          <select
            className="select"
            value={selKey}
            onChange={(e) => setSelKey(e.target.value)}
            disabled={!slots || slots.length === 0}
          >
            {!slots && <option value="">加载中…</option>}
            {slots && slots.length === 0 && <option value="">暂无槽位</option>}
            {slots && slots.map((s) => (
              <option key={s.key} value={s.key}>
                {s.name} ({s.flowers.length})
              </option>
            ))}
          </select>
          <button className="btn ghost" onClick={handleDeleteSlot} disabled={!selKey || selKey === 'self'}>
            删除该槽
          </button>
          <span className="muted slot-hint">0=自己世界,有 id=好友世界;删除后回访该世界重新建档</span>
          {slotMsg && <span className={'slot-msg' + (slotMsg.startsWith('已') ? '' : ' slot-msg-err')}>{slotMsg}</span>}
        </div>
        {selKey && (() => {
          const sel = slots.find((s) => s.key === selKey)
          if (!sel) return null
          return (
            <div className="flowers-group">
              <h4 className="flowers-group-t">槽「{sel.name}」存储的花种({sel.flowers.length})</h4>
              <div className="flower-grid">
                {sel.flowers.map((f) => <FlowerCard key={flowerKey(f)} f={f} now={now} />)}
              </div>
            </div>
          )
        })()}
      </section>
      {!data ? (
        <div className="empty">尚未收到花种数据:游戏内打开一次花种面板后自动显示…</div>
      ) : (
        <>
          {specials.length > 0 && (
            <section className="flowers-group">
              <h4 className="flowers-group-t">特殊花种(7 星)</h4>
              <div className="flower-grid">
                {specials.map((f) => <FlowerCard key={flowerKey(f)} f={f} now={now} />)}
              </div>
            </section>
          )}
          <section className="flowers-group">
            <h4 className="flowers-group-t">普通花种</h4>
            <div className="flower-grid">
              {normals.map((f) => <FlowerCard key={flowerKey(f)} f={f} now={now} />)}
            </div>
          </section>
        </>
      )}
    </div>
  )
}

// flowerKey 生成卡片稳定唯一 key:优先 npcLogicId(每只花种唯一,服务器重发面板时不变),
// 无则退回 id-blood(旧数据兼容)。
function flowerKey(f) {
  return f.npcLogicId ? `log-${f.npcLogicId}` : `${f.id}-${f.blood}`
}

// fmtLeft 把活动结束时间渲染为剩余倒计时;未设置返回 null,已结束返回 ended 标记。
function fmtLeft(endTs, nowMs) {
  if (!endTs) return null
  const s = Math.floor(endTs - nowMs / 1000)
  if (s <= 0) return { ended: true, text: '已结束' }
  const d = Math.floor(s / 86400)
  const hh = String(Math.floor((s % 86400) / 3600)).padStart(2, '0')
  const mm = String(Math.floor((s % 3600) / 60)).padStart(2, '0')
  const ss = String(s % 60).padStart(2, '0')
  return { ended: false, text: d > 0 ? `剩 ${d} 天 ${hh}:${mm}:${ss}` : `剩 ${hh}:${mm}:${ss}` }
}

function FlowerCard({ f, now }) {
  const icons = useContext(IconsContext)
  const stars = (f.star || 0) > 0 ? '★'.repeat(f.star) : ''
  const left = fmtLeft(f.endTs, now)
  // 详情字段:点过地图花种后由 0x0338 合并进来;未点过全空(普通花种绑定/奖牌恒为空)。
  const hasDetail = f.detail || f.lv > 0 || f.glass || f.bindName || f.medalName
  // 查看状态:已点过(=有 0x0338 详情)的花种——
  // 有炫彩(普通/隐藏)高亮;无炫彩置灰表示已查看;捕捉后(详情被清)恢复默认。
  const colorful = f.detail && (f.glassType === 1 || f.glassType === 2)
  return (
    <div
      className={
        'flower-card' +
        (f.specSeedId > 0 ? ' flower-special' : '') +
        (colorful ? ' flower-card-colorful' : f.detail ? ' flower-card-viewed' : '')
      }
    >
      {/* 右上角标记:已点过(=有 0x0338 详情)才显示——
          炫彩用游戏图标,普通炫彩粉紫 / 隐藏炫彩金色;无炫彩标「普通」 */}
      {f.detail && (
        <span
          className={
            'flower-corner' +
            (f.glassType === 1 ? ' flower-corner-colorful'
              : f.glassType === 2 ? ' flower-corner-hidden'
                : ' flower-corner-plain')
          }
          title={
            f.glassType === 2 ? `隐藏炫彩 · ${f.glass}` :
            f.glassType === 1 ? `炫彩 · ${f.glass}` : '普通(无炫彩)'
          }
        >
          {(f.glassType === 1 || f.glassType === 2) && icons.colorful
            ? <img src={imgURL(icons.colorful)} alt="炫彩" />
            : '普通'}
        </span>
      )}
      <ImgAvatar src={f.img} alt={f.name} className="flower-img" />
      <div className="flower-info">
        <div className="flower-name" title={f.name}>{f.name || '未知花灵'}</div>
        <div className="flower-meta">
          {stars && <span className="flower-star" title={`${f.star} 星`}>{stars}</span>}
          <span className="flower-blood" title={'血脉 ' + (f.bloodName || f.blood)}>
            {f.bloodIcon && <ImgAvatar src={f.bloodIcon} alt={f.bloodName || ''} className="flower-blood-ic" />}
            {f.bloodName || f.blood || '-'}
          </span>
        </div>
        <div className="flower-meta">
          {left ? (
            <span className={'flower-left' + (left.ended ? ' ended' : '')} title={`结束 ${fmtTime(f.endTs)}`}>
              {left.text}
            </span>
          ) : (
            <span className="muted">结束 {fmtTime(f.endTs)}</span>
          )}
        </div>
        {hasDetail && (
          <div className="flower-detail">
            {f.lv > 0 && <span className="flower-chip" title="等级">Lv {f.lv}</span>}
            {f.bindName && (
              <span
                className="flower-chip flower-bind"
                title={f.bindEvo > 0 ? `绑定守护宠物,进化阶段 ${f.bindEvo}` : '绑定守护宠物'}
              >
                <ImgAvatar src={f.bindImg} alt={f.bindName} className="flower-chip-img" />
                绑定 {f.bindName}
              </span>
            )}
            {f.medalName && (
              <span className="flower-chip flower-medal" title="绑定宠物佩戴的奖牌">
                {f.medalIcon && <ImgAvatar src={f.medalIcon} alt={f.medalName} className="flower-chip-img" />}
                {f.medalName}
              </span>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
