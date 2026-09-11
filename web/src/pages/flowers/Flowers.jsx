import React, { useState, useEffect, useContext, useMemo, useCallback } from 'react'
import { getFlowers, getFlowerSlots, deleteFlowerSlot, subscribe } from '../../api'
import { AccountContext, IconsContext } from '../../context'
import { fmtTime } from '../../utils/format'
import { useAsyncData, useInterval } from '../../hooks/useAsyncData'
import { ImgAvatar } from '../../components/icons'
import { GlassChip, MarkIcon } from '../../components/badges'
import { IconStar } from '../../components/svg'
import Dropdown from '../../components/Dropdown'

// 花种页面:渲染 s2c 0x0375 下发的 flower_npcs(花灵)活动 BOSS 分组。
// 只显示花种;world_leader_npcs(世界 BOSS)与 legendary_npcs(传说 NPC)在解析层就丢弃了。
// 数据源:进页面先 GET /api/flowers 回显最近一次分组,之后订阅 SSE flowers 实时覆盖
// (游戏内每打开一次花种面板,服务器就会整组重发 0x0375)。
// 游戏内点击地图上的花种时,服务器会额外下发 0x0338 单只详情(等级/炫彩/绑定宠物/奖牌),
// 由后端合并进对应卡片后经同一 SSE 刷新;未点过的花种这些字段为空。
// 槽名里的 UID 片段:**只挂 .privacy,不做文字脱敏**。
//
// 与账号下拉保持一致(AccountSelect.jsx:87 的 account-item-uid 也是 uidOf(...) + .privacy):
// 本项目的截图防泄规范是「遮罩即保护,不重复脱敏」——默认常驻模糊,点顶栏品牌名
// 「rocom-go」才解除。脱敏与遮罩叠着用会废掉这个开关:默认糊着时星号看不见(多余),
// 而解除后(用户正是想确认「这是哪个好友」)看到的仍是 839***713,还是认不出来。
//
// 等宽字体是为了多条槽名的 UID 段宽度一致、下拉里对得齐。
const SlotUid = ({ uid }) => <span className="privacy slot-uid">{uid}</span>

// 槽展示名:一律由前端按 key 现算,**不用**后端 /api/flowers/slots 返回的 name 字段。
// 原因:后端 flowerSlotName 拼的是原始 uid(如「好友 UID:10001」未脱敏),而 SSE 广播
// 的 worlds 由前端自己转换、是脱敏的 —— 同一个下拉里两条路径显示不一致,且删除槽后走
// REST 重取时,名字会从脱敏突然变成完整 uid(可观察的跳变)。脱敏属展示层职责,故归一到前端。
const slotLabel = (key, myUID) => {
  if (key === 'self') return myUID ? <>自己世界 (<SlotUid uid={myUID} />)</> : '自己世界'
  if (key.startsWith('owner:')) return <>好友 UID:<SlotUid uid={key.slice(6)} /></>
  return key
}

// 当前世界的实时项:key 不落在任何槽里时的占位文案。
const currentLabel = (ownerID) =>
  ownerID ? <>当前世界 (<SlotUid uid={ownerID} />)</> : '当前世界'

export default function Flowers() {
  const account = useContext(AccountContext)
  // data:null = 尚未收到任何 0x0375
  const { data, setData } = useAsyncData(useCallback(() => getFlowers(), []), { reloadKey: account })
  const [now, setNow] = useState(() => Date.now())
  // 世界存档槽位列表:null=加载中。选中视图 selKey 默认 __current__=当前世界实时数据
  // (下拉含「当前世界」与各存档槽),slotMsg=删除结果提示。
  const [selKey, setSelKey] = useState('__current__')
  const [slotMsg, setSlotMsg] = useState('')

  const { data: slots, setData: setSlots, refresh: loadSlots } = useAsyncData(
    useCallback(async () => (await getFlowerSlots()).slots || [], []),
    { reloadKey: account },
  )

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

  useEffect(() => subscribe('flowers', (d) => {
    setData(d)
    // 广播带完整 worlds 存档表:本地同步槽列表,选中槽随实时推送保持最新。
    // 只存 key,展示名在渲染时由 slotLabel 现算(它依赖 myUID,且要用后端给不得的脱敏)。
    const worlds = d && d.worlds
    if (worlds) setSlots(Object.entries(worlds).map(([key, w]) => ({
      key,
      ts: (w && w.ts) || 0,
      flowers: (w && w.flowers) || [],
    })))
  }), [setData, setSlots])

  // 活动结束倒计时随时间走:秒级刷新(卡片量少,重渲染开销可忽略)。
  useInterval(() => setNow(Date.now()), 1000)

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
  // 标签一律走 slotLabel 现算 —— slots 可能来自 REST,其 name 未脱敏(见 slotLabel 的说明)。
  const viewOptions = useMemo(() => {
    const real = (slots || []).map((s) => ({ ...s, label: slotLabel(s.key, myUID) }))
    if (real.some((s) => s.key === curKey)) return real
    return [{ key: '__current__', label: currentLabel(curOwnerID), flowers }, ...real]
  }, [slots, curKey, curOwnerID, flowers, myUID])
  // 当前视图:__current__=实时当前世界;否则选中的存档槽。花种按特殊(最多 3 只)/普通(最多 20 只)分组展示。
  //
  // 只返回 flowers,**不返回 label**:视图名已由上面的下拉框显示(它的选项就是
  // slotLabel 现算的),在这里再算一份是重复,且多算一份 label 会让这段 memo
  // 依赖 myUID/curOwnerID —— 那两个一变(比如后端补到自己的 UID)整个视图就重算。
  const view = useMemo(() => {
    if (selKey !== '__current__') {
      const sel = slots && slots.find((s) => s.key === selKey)
      if (sel) return sel.flowers || []
    }
    return flowers
  }, [selKey, slots, flowers])
  const viewSpecials = view.filter((f) => f.specSeedId > 0)
  const viewNormals = view.filter((f) => !(f.specSeedId > 0))

  return (
    <div className="flowers-page">
      <div className="toolbar">
        <h3 style={{ margin: 0 }}>花种</h3>
        <span className="muted toolbar-hint">打开面板自动更新,点地图花种看详情</span>
        <div className="spacer" />
        <span className="muted">共 {view.length} 只花灵</span>
      </div>
      {/* 视图切换:默认当前世界(实时),可切到世界存档槽;切槽后只展示该槽,删除后回访重新建档 */}
      <div className="slot-bar">
        <Dropdown
          className="slot-select"
          options={viewOptions.map((o) => ({
            value: o.key,
            label: o.label,
            count: o.flowers.length,
          }))}
          value={selKey}
          onChange={setSelKey}
          disabled={!slots}
          placeholder="加载中…"
          title="切换视图:当前世界 / 世界存档槽"
        />
        <button className="btn ghost danger" onClick={handleDeleteSlot} disabled={!selKey.startsWith('owner:')}>
          删除该槽
        </button>
        {slotMsg && <span className={'slot-msg' + (slotMsg.startsWith('已') ? '' : ' slot-msg-err')}>{slotMsg}</span>}
      </div>
      {selKey === '__current__' && !data ? (
        <div className="empty">尚未收到花种数据:游戏内打开一次花种面板后自动显示…</div>
      ) : (
        <>
          {/* 这里**刻意没有**「视图名(总数)」的标题:
              视图名就是上面下拉框的选中项,总数在顶栏「共 N 只花灵」,
              且下面「特殊(N)」「普通(N)」相加也是它 ——
              再添一行就成了插槽下方连着三行标题,首行纯属重复。别加回去。 */}
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
  // 星级是**数量**(1~5 颗),故用实心星:描边星在 12px 下数不清几颗,
  // 而「几颗星」这个语义本身就靠实心块来读。原先是 '★'.repeat(n),
  // 那是字形,不响应 currentColor 且各家系统字重不同。
  const stars = (f.star || 0) > 0
    ? Array.from({ length: f.star }, (_, i) => <IconStar key={i} size={11} filled />)
    : null
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
          {f.challengeCount > 0 && (
            <span className="muted flower-challenge" title="本账号累计挑战该花种品种的次数,花种消失后保留">
              挑战 {f.challengeCount} 次
            </span>
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
