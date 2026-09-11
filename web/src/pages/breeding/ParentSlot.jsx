import React from 'react'
import PetPicker from '../../components/PetPicker'
import { imgURL } from '../../components/icons'
import { IconClose } from '../../components/svg'
import { eggGroupLabel } from '../../constants'
import { NestSprig, NestWreath } from './NestGrass'

// ParentSlot 亲本位上的一只(种母 / 种公):一张卡,里面是可选的下拉 + 蛋组标签。
//
// 为什么做成「卡」而不是两个并排的下拉:玩家要定的是**哪一对**,不是两个各自独立的属性。
// 成对等权重地摆出来,「母 × 公」这件事才在版面上成立(见 ParentRow 里那个连接符)。
//
// 为什么建线表单与线详情共用它:两处看到的必须是**同一张卡** —— 详情里那对亲本就是建线时
// 挑的那两个,样式或信息各写一份,读起来就是两回事了。卡片只认归一化后的 card 对象
// (两处数据形状不同:/api/breeding/pool 给 eggGroups 字符串数组、线里存的是 EggParent 快照),
// 摆平数据形状是调用方的事,组件不猜(见 parentCard)。
//
// 三种状态各自要说清不同的事:
//   - 未选(虚线框):「＋ 选种母」是动作,不是占位符 —— 第一眼就要知道这里可以点;
//   - 已选(主色描边):头像 / 名字 + 蛋组标签,右上角一个 × 能退回未选;
//   - 只读(disabled):线详情里的种母、以及还没选品种时的两位 —— 此时**不放下拉**,
//     直接把是谁写清楚;一个点不动的下拉比一句说明更让人困惑。
//     指定过但已不在库(card.released)时写「已不在库」,而不是留空:空着会被读成「没指定过」。
//
// 卡底还有一个「学院小窝」勾选(nestGid/nestTruth/nestPlanGid/onNest):小窝里那只参与孵蛋时
// 子代性格 100% 随它(见 docs/data.md 3.6)。放在这里而不是设置页,是因为决定「要不要吃这份
// 加成」的那一刻,玩家盯着的正是这对亲本 —— 而「把小窝给谁」就是这一步要做的决定。
//
// 小窝里是谁有**两个来源**,勾选必须让玩家分得清用的哪一个:
//   - 真值(nestGid 里由调用方取 nestPlanGid || nestTruth 传进来当生效值,另有 nestTruth 单独给);
//     它是游戏里的事实,由家园管线自动写,工具里改不了;
//   - 本条线的计划值(nestPlanGid):「打算把小窝给谁」,只用于还没真放进去时试算概率。
// 勾选写的是**计划值**(onNest 收到的 gid 就是计划给谁,0 = 清掉计划、回落到真值):
// 让「试算一下换谁更好」这件事在本页就能做,而不必先去游戏里放/抱一趟。计划值随线整条保存,
// 故这里只管把 gid 交给调用方,组件不碰接口。
//
// **互斥由后端保证**:小窝全库只存一个 gid,真值跟游戏走;计划值每条线各存一个,勾另一张卡
// 就是把计划换过去 —— 两处都不需要本地「只能勾一个」的状态(多一处就可能与后端不一致)。
export default function ParentSlot({
  role, card, options, value, onChange, disabled, disabledHint, badge, title, releasedHint,
  nestGid, nestTruth, nestPlanGid, onNest,
}) {
  const isMother = role === 'mother'
  const label = isMother ? '种母' : '种公'
  // released 的卡片没有 gid 可用(他已经不在库、没有快照),但它**不是空的** —— 空着会被读成
  // 「没指定过」,而这两件事在玩家眼里完全不同(见下面 meta 的处理)。
  const filled = !!(card && (card.gid || card.released))
  // 它是不是「**生效的**小窝里那只」:按 gid 比,不比名字 —— 同一只宠物改名/进化后仍是它
  // (名字只是展示)。勾选看的正是生效值(计划值优先,没计划才回落真值),与后端算概率时用的
  // 那个 gid(nestGid := line.NestPlanGid || nest.gid)是同一个,两处取值不同就会出现
  // 「卡上勾着、百分比却按另一只算」。
  const inNest = filled && !!card.gid && card.gid === nestGid
  // 勾上却**不能取消**:工具里没设计划、而游戏事实正是它 —— 取消计划落不到实处(取消后生效值
  // 又回到真值,还是它),要换一只只能去游戏里抱走,或在别的卡上勾一个计划。
  const lockedByTruth = filled && !nestPlanGid && nestTruth === card.gid && nestTruth !== 0
  // 勾上时那句小字要说清「这个勾是怎么来的」:按计划试算 vs 游戏实况。两者都写「100%」,
  // 但前者一换计划就变、后者要玩游戏里那只 —— 混成一句会让玩家以为勾了就等于放进去了。
  const onPlan = inNest && !!nestPlanGid && nestPlanGid === card.gid
  return (
    <div className={'br-parent' + (filled ? ' on' : '') + (disabled ? ' off' : '') + (card && card.released ? ' rel' : '')
      + (inNest ? ' nest-picked' : '')}
      title={title}>
      <div className="br-parent-head">
        <span className="br-parent-role">{label}</span>
        {badge ? <span className="br-parent-badge">{badge}</span> : null}
        {filled && !disabled ? (
          <button type="button" className="br-parent-x" onClick={() => onChange('')}
            title={`不选${label}`} aria-label={`不选${label}`}>
            <IconClose size={11} />
          </button>
        ) : null}
      </div>

      {disabled ? (
        <div className="br-parent-ro">
          {filled ? (
            <>
              {/* 只读位也套一层 .dropdown-av-wrap:草环认的是这一层(position: relative),
                  只读与可换两条路一致,否则详情里的种母选中小窝时草环无处可挂。 */}
              <span className="dropdown-av-wrap">
                {card.img ? <img className="dropdown-av" src={imgURL(card.img)} alt="" /> : null}
                {inNest ? <NestWreath /> : null}
              </span>
              <span className="dropdown-value">{card.name}</span>
            </>
          ) : <span className="dropdown-value">{`＋ 选${label}`}</span>}
        </div>
      ) : (
        <PetPicker
          value={value || ''} options={options} onChange={onChange}
          clearLabel={`不选${label}`}
          placeholder={filled ? '' : `＋ 选${label}`}
          avFrame={inNest ? <NestWreath /> : null}
        />
      )}

      {disabled && !filled && disabledHint ? <p className="br-parent-off">{disabledHint}</p> : null}
      {/* 指定过的那位已经不在库:名字那一行已经写了「已不在库」,这里只说清「他怎么了 + 这意味着什么」。
          可换的那侧(种公)由调用方给 releasedHint —— 他要说的是「换一位吧」,而不是「你没选」。 */}
      {releasedHint && !(card && card.released) ? <p className="br-parent-off">{releasedHint}</p> : null}

      {filled && !card.released ? (
        <div className="br-parent-meta">
          <span className="br-parent-species muted">{card.species || ''}</span>
          {(card.eggGroups || []).map((g) => (
            <span key={g} className="egg-group" title={`蛋组 · ${g}`}>{eggGroupLabel(g)}</span>
          ))}
        </div>
      ) : null}

      {/* 学院小窝:没有卡(位子空着)、或那位已不在库时不给勾 —— 两种情况下都没有「它」可放。
          onNest 收的是**要记进这条线的计划 gid**(取消时传 0),而不是一个布尔:调用方拿到的
          就是随线保存要的取值,不必再回头从 card 里找一遍是谁被勾了。 */}
      {filled && !card.released && onNest ? (
        <label className={'br-nest' + (inNest ? ' on' : '') + (onPlan ? ' plan' : '') + (lockedByTruth ? ' locked' : '')}
          title={lockedByTruth
            ? '游戏里它就在学院小窝里 —— 换一只得在游戏里放/抱走，或在别的卡上勾选试算'
            : inNest
              ? (onPlan
                ? `本线按计划把它当作小窝里那只试算:它参与孵蛋时子代性格 100% 随它${card.nature ? `（${card.nature}）` : ''}。这只是计划，要动游戏里的事实得去游戏里放/抱走`
                : `游戏里它就在学院小窝里:它参与孵蛋时子代性格 100% 随它${card.nature ? `（${card.nature}）` : ''}`)
              : '把它记作本线的计划:小窝里那只参与孵蛋时子代性格 100% 随它。这只是计划、只影响本线的试算，不会改动游戏里的实况'}>
          {/* 取反的是**组件自己的那个 inNest**,不是 e.target.checked:这个框是受控的
              (checked 由生效值决定),照着 DOM 的瞬时状态取值会在「点了但服务端没写成功」
              时把自己也绕进去 —— 服务端那份才是真的。lockedByTruth 时输入被禁用,故点不到。 */}
          <input type="checkbox" checked={inNest} disabled={lockedByTruth}
            onChange={() => onNest(inNest ? 0 : card.gid)} />
          {/* 草芽先于文字出现:这个开关管的就是「草」,图标与草环是同一种语言(见 NestGrass.jsx)。
              输入框仍在 label 里、仍是真的 checkbox —— 点整行都能切,读屏与验收脚本照旧。 */}
          <NestSprig />
          <span>学院小窝</span>
        </label>
      ) : null}
      {/* 结论写在按钮**外面**:它是一句解释(「子代性格 100% · 固执」),不是按钮的标签 ——
          塞进胶囊里,胶囊就会被这句话撑成整条横幅,那就不是一枚按钮了(见 4 倍截图)。
          计划态用虚线下划、实况态用实色,与按钮的虚线/实线边框是同一套语言。 */}
      {filled && !card.released && onNest && inNest ? (
        <p className={'br-nest-note' + (onPlan ? ' plan' : '')}>
          {card.nature ? `子代性格 100% · ${card.nature}` : '子代性格 100%'}
          {onPlan ? '（按计划试算）' : '（游戏实况）'}
        </p>
      ) : null}
    </div>
  )
}

// releasedCard 指定过、但已经不在宠物库的那位(放生 / 送人)在卡上也要有名字:后端只给
// 「motherReleased / fatherReleased + 那个 gid」,没有快照可用,故这里造一张只有名字的卡 ——
// 空着会被读成「没指定过」,而玩家想看的恰恰是「我原来配的是谁、他不在了」。
export function releasedCard() {
  return { gid: 0, name: '已不在库', species: '', img: '', gender: '', eggGroups: [], released: true }
}

// ParentRow 把两个亲本位摆成一对:母在左、公在右,中间一个连接符点明关系。
// 窄屏由 CSS 改成上下堆叠(连接符转竖),故这里只出结构,方向交给样式。
export function ParentRow({ mother, father }) {
  return (
    <div className="br-parents">
      {mother}
      <span className="br-parents-x" aria-hidden="true">×</span>
      {father}
    </div>
  )
}

// NestHint 学院小窝的当前状态:游戏真值是什么、本线的计划又是什么 —— 两者不一致时更要说清。
//
// 为什么除了卡上的勾选还要这一行:勾选只说得清「**这对亲本**里谁在小窝里」,而小窝里那只
// 完全可以不是这条线的亲本 —— 那时两张卡都没勾,玩家只会以为小窝空着,于是建议卡片上那些
// 100% 就成了没来由的数字(见 docs/data.md 3.6)。
//
// 「小窝里是谁」有两个来源(见 ParentSlot 的注释),这一行的全部价值就是把它们摆开:
//   - truth:游戏真值(响应顶层 nest),工具里改不了,要动得去游戏里放/抱;
//   - planGid/plan:本线的计划值(随线保存),只用于试算 —— 后端算概率用的生效值 =
//     planGid || truth.gid,故两者不一致时页面必须同时显示,否则玩家看到的 100% 无从解释。
// 入参刻意**不直接收 line**:建线表单那时根本还没有线,只能给真值 + 本地计划 —— 传对象进来
// 就得让表单伪造一个,那比这里多接几个标量麻烦得多。
//
// 四态 + 至多两个动作按钮:把「真值有/无 × 计划有/无/同」这几种情形各写明白,而不是留一句
// 含糊的「小窝里现在是 X」。两个手工兜底的按钮都有代价,故只在需要时出现:
//   记为游戏真值 → 写手工兜底(POST /api/nest),正常由家园管线自动维护;
//   改用游戏真值 / 清掉计划 → 把计划去掉,预测回落到真值。
export function NestHint({ truth, planGid = 0, plan, planReleased, onClearPlan, onCommitTruth }) {
  const tGid = (truth && truth.gid) || 0
  const tName = (truth && truth.name) || '已不在库的那只'
  const tNature = (truth && truth.nature) || ''
  const pGid = planGid || 0
  // 计划那只的快照可能缺失(已不在库 = nestPlanReleased,或后端暂时没给快照):给它一个明确的
  // 称谓,而不是让 B 位置空着 —— 空着会被读成「没设计划」,而这里的事实恰恰相反(设了,但那只
  // 要么没了要么查不到名字)。
  const pName = (plan && !planReleased && plan.name) || '已不在库的那只'
  const pNature = (!planReleased && plan && plan.nature) || ''
  const truthTxt = <>游戏里小窝现在是 <b>{tName}</b>{tNature ? `（${tNature}）` : ''}</>
  const planTxt = <>本线正按计划 <b>{pName}</b>{pNature ? `（${pNature}）` : ''} 试算</>

  let body
  let actions = null
  if (tGid && pGid && pGid !== tGid) {
    // 态 4:真值有 + 计划 ≠ 真值 —— 最要紧的一态,两个名字都得出现,并给「改用游戏真值」。
    body = <>{truthTxt}；{planTxt}</>
    actions = onClearPlan ? (
      <button type="button" className="btn small" onClick={() => onClearPlan()}>改用游戏真值</button>
    ) : null
  } else if (tGid && pGid && pGid === tGid) {
    // 态 3:真值有 + 计划 = 真值 —— 计划没带来任何差别,直说「也是它」,别让玩家以为还有两个源。
    body = <>{truthTxt} —— 本线的计划也是它</>
  } else if (tGid) {
    // 态 2:真值有、无计划 —— 本线按真值算,并点明「勾别的卡只是计划」。
    body = <>{truthTxt} —— 本线按它算；勾别的卡可以试算，但那只是计划</>
  } else if (pGid) {
    // 态 5:真值 0 + 有计划 —— 还没拿到小窝数据,本线按计划试算,并给「记为游戏真值」兜底。
    // 两个按钮都按「调用方给没给这个能力」决定出不出:建线表单那时还没有线,「记为游戏真值」
    // 无从落脚(它要 POST /api/nest 改全库真值),给一个点了没反应的按钮比不给更差。
    body = <>游戏里还没抓到小窝数据；{planTxt}</>
    const btns = []
    if (onCommitTruth) {
      btns.push(<button key="commit" type="button" className="btn small"
        onClick={() => onCommitTruth(pGid)}>记为游戏真值</button>)
    }
    if (onClearPlan) {
      btns.push(<button key="clear" type="button" className="btn small"
        onClick={() => onClearPlan()}>清掉计划</button>)
    }
    actions = btns.length ? btns : null
  } else {
    // 态 1:真值 0 且无计划 —— 空着,顺势把「勾上就吃这份加成」说清。
    body = <>学院小窝空着 —— 勾上某一位，它参与孵蛋时子代性格 100% 随它</>
  }
  // 「勾选只是计划、事实在游戏里」这句话**四态共用一个尾巴**而不是各写一遍:它说的是同一件事
  // (勾选 ≠ 放进游戏),每态都该看见、但重复四份既啰嗦又容易改漏一处。放在状态行末尾,读起来
  // 也正好接住上面那句结论。
  return (
    <p className="br-hint br-nest-hint muted">
      {body}；勾选只记本线的计划（试算用），要改游戏里的事实得去游戏里放/抱走。
      {actions ? <span className="br-nest-acts">{actions}</span> : null}
    </p>
  )
}

// parentCard 把两种数据形状归一成卡片要的那一份。
//   - 候选池(/api/breeding/pool):eggGroups 是**组名字符串数组**,直接可用;
//   - 线里存的快照(pet.EggParent):**没有蛋组字段**,蛋组要从候选池里按 gid 找出来传进来。
// 两种形状都要认:少认一种,实到卡上就是「有头像没蛋组」或反过来。
export function parentCard(p, eggGroups) {
  if (!p || !p.gid) return null
  const groups = eggGroups
    || (Array.isArray(p.eggGroups)
      ? p.eggGroups.map((g) => (typeof g === 'string' ? g : g && g.name))
      : [])
  return {
    gid: p.gid,
    name: p.name || p.species || `#${p.gid}`,
    species: p.species || '',
    img: (p.image && p.image.head) || p.img || '',
    gender: p.gender || '',
    eggGroups: groups.filter(Boolean),
    // nature 只有「学院小窝」那行提示要它(「子代性格 100% · 固执」):不填就等于玩家勾上以后
    // 还得自己回去翻这只的性格是什么,而那份预测本来就是他勾它的全部理由。
    // 两种形状都带这一项(/api/breeding/pool 的候选与线里存的 EggParent 快照都有)。
    nature: p.nature || '',
  }
}
