import React, { useState, useEffect, useContext, useMemo, useCallback } from 'react'
import { getFlowers, getFlowerSlots, deleteFlowerSlot, subscribe } from '../api'
import { AccountContext, IconsContext } from '../context'
import { fmtTime } from '../utils/format'
import { ImgAvatar } from '../components/icons'
import { GlassChip, MarkIcon } from '../components/badges'

// 花种页面:渲染 s2c 0x0375 下发的 flower_npcs(花灵)活动 BOSS 分组。
// 只显示花种;world_leader_npcs(世界 BOSS)与 legendary_npcs(传说 NPC)在解析层就丢弃了。
// 数据源:进页面先 GET /api/flowers 回显最近一次分组,之后订阅 SSE flowers 实时覆盖
// (游戏内每打开一次花种面板,服务器就会整组重发 0x0375)。
// 游戏内点击地图上的花种时,服务器会额外下发 0x0338 单只详情(等级/炫彩/绑定宠物/奖牌),
// 由后端合并进对应卡片后经同一 SSE 刷新;未点过的花种这些字段为空。
export default function Flowers() {
  const account = useContext(AccountContext)
  const icons = useContext(IconsContext)
  const [data, setData] = useState(null) // null = 尚未收到任何 0x0375
  const [now, setNow] = useState(() => Date.now())
  // 世界存档槽位:slots=槽列表(null=加载中),selKey=选中视图(默认 __current__=当前世界实时数据),
  // slotMsg=删除结果提示。下拉含「当前世界」(实时,显示归属 id)与各存档槽。
  const [slots, setSlots] = useState(null)
  const [selKey, setSelKey] = useState('__current__')
  const [slotMsg, setSlotMsg] = useState('')

  useEffect(() => {
    getFlowers().then((v) => { if (v) setData(v) }).catch(() => {})
  }, [account])

  // loadSlots 拉取世界存档槽位列表;切账号/删除后重新拉取。选中项由下方 selKey 修正 effect 统一管理。
  const loadSlots = useCallback(() => {
    getFlowerSlots().then((v) => {
      setSlots((v && v.slots) || [])
    }).catch(() => setSlots([]))
  }, [])

  useEffect(() => { loadSlots() }, [account, loadSlots])

  async function handleDeleteSlot() {
    if (!selKey.startsWith('owner:')) return
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
      // 广播带完整 worlds 存档表:本地同步槽列表,选中槽随实时推送保持最新。
      const worlds = m.data && m.data.worlds
      if (worlds) {
        const list = Object.entries(worlds).map(([key, w]) => ({
          key,
          name: key === 'self' ? '自己世界' : key.startsWith('owner:') ? '好友 UID:' + key.slice(6) : key,
          ts: (w && w.ts) || 0,
          flowers: (w && w.flowers) || [],
        }))
        setSlots(list)
      }
    })
  }, [account])

  // 活动结束倒计时随时间走:秒级刷新(卡片量少,重渲染开销可忽略)。
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [])

  const flowers = useMemo(() => (data && data.flowers) || [], [data])
  // 当前账号自己的 uid(account 形如 "UID:<uid>"),供自己世界槽显示 id。
  const myUID = useMemo(() => {
    const m = /^UID:(\d+)/.exec(account || '')
    return m ? m[1] : ''
  }, [account])
  // 当前世界归属 id:实时花种列表第一个非 0 ownerUserId(有 id=好友世界);全 0=自己世界,取自己 uid。
  const curOwnerID = useMemo(() => {
    for (const f of flowers) if (f.ownerUserId) return String(f.ownerUserId)
    return myUID
  }, [flowers, myUID])
  // 当前世界对应的存档槽 key:有归属 id → owner:<uid>;全 0(自己世界)→ self。
  const curKey = curOwnerID ? 'owner:' + curOwnerID : 'self'
  // 选中修正:当前世界对应槽(self/owner:<uid>)存在时默认选中它(=当前世界,避免与「当前世界」项
  // 重复显示);否则保持已选中的其他槽;都不是则回退「当前世界」实时项。
  useEffect(() => {
    setSelKey((k) => {
      if (slots && k !== '__current__' && slots.some((s) => s.key === k)) return k
      if (slots && slots.some((s) => s.key === curKey)) return curKey
      return '__current__'
    })
  }, [slots, curKey])
  // 下拉选项:当前世界已有对应槽时不重复显示「当前世界」项(该槽即当前世界,实时数据已同步进槽);
  // 否则(未建档/花全被采完未更新槽)单独显示「当前世界 (id)」实时项。
  const viewOptions = useMemo(() => {
    const real = slots || []
    if (real.some((s) => s.key === curKey)) return real
    return [{ key: '__current__', name: curOwnerID ? `当前世界 (${curOwnerID})` : '当前世界', flowers }, ...real]
  }, [slots, curKey, curOwnerID, flowers])
  // 当前视图:__current__=实时当前世界;否则选中的存档槽。花种按特殊(最多 3 只)/普通(最多 20 只)分组展示。
  const view = useMemo(() => {
    if (selKey !== '__current__') {
      const sel = slots && slots.find((s) => s.key === selKey)
      if (sel) return { name: sel.key === 'self' && myUID ? `自己世界 (${myUID})` : sel.name, flowers: sel.flowers || [] }
    }
    return { name: curOwnerID ? `当前世界 (${curOwnerID})` : '当前世界', flowers }
  }, [selKey, slots, flowers, curOwnerID, myUID])
  const viewSpecials = view.flowers.filter((f) => f.specSeedId > 0)
  const viewNormals = view.flowers.filter((f) => !(f.specSeedId > 0))

  return (
    <div className="flowers-page">
      <div className="toolbar">
        <h3 style={{ margin: 0 }}>花种</h3>
        <span className="muted toolbar-hint">打开面板自动更新,点地图花种看详情</span>
        <div className="spacer" />
        <span className="muted">共 {view.flowers.length} 只花灵</span>
      </div>
      {/* 视图切换:默认当前世界(实时),可切到世界存档槽;切槽后只展示该槽,删除后回访重新建档 */}
      <div className="slot-bar">
        <select
          className="select"
          value={selKey}
          onChange={(e) => setSelKey(e.target.value)}
          disabled={!slots}
        >
          {viewOptions.map((s) => (
            <option key={s.key} value={s.key}>
              {s.key === 'self' && myUID ? `自己世界 (${myUID})` : s.name} ({s.flowers.length})
            </option>
          ))}
        </select>
        <button className="btn ghost" onClick={handleDeleteSlot} disabled={!selKey.startsWith('owner:')}>
          删除该槽
        </button>
        {slotMsg && <span className={'slot-msg' + (slotMsg.startsWith('已') ? '' : ' slot-msg-err')}>{slotMsg}</span>}
      </div>
      {selKey === '__current__' && !data ? (
        <div className="empty">尚未收到花种数据:游戏内打开一次花种面板后自动显示…</div>
      ) : (
        <>
          <div className="flowers-group">
            <h4 className="flowers-group-t">{view.name}({view.flowers.length})</h4>
          </div>
          {viewSpecials.length > 0 && (
            <section className="flowers-group">
              <h4 className="flowers-group-t">特殊花种(7 星,{viewSpecials.length})</h4>
              <div className="flower-grid">
                {viewSpecials.map((f) => <FlowerCard key={flowerKey(f)} f={f} now={now} />)}
              </div>
            </section>
          )}
          <section className="flowers-group">
            <h4 className="flowers-group-t">普通花种({viewNormals.length})</h4>
            <div className="flower-grid">
              {viewNormals.map((f) => <FlowerCard key={flowerKey(f)} f={f} now={now} />)}
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
          炫彩只放游戏炫彩图标(普通炫彩粉紫 / 隐藏炫彩金色由角标底色区分);
          完整色卡在下方信息区大图展示,角标不再贴小色卡。无炫彩标「普通」 */}
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
          {f.glassType === 1 || f.glassType === 2
            ? <MarkIcon src={icons.colorful} title="炫彩" fallback="彩" cls="mark-colorful" />
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
        {/* 炫彩/隐藏炫彩:卡片内展示完整大色卡(角标小色卡看不清配色,这里铺满信息列可细看)。
            后端在 glassType != 0 时才带 glassValue,故此处判断即可。 */}
        {f.glassType > 0 && f.glassValue > 0 && (
          <div className="flower-glass-bar">
            <GlassChip p={f} className="flower-glass-chip" />
            {f.glass && <span className="flower-glass-t" title={f.glass}>{f.glass}</span>}
          </div>
        )}
      </div>
    </div>
  )
}
