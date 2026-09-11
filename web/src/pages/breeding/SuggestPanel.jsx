import React from 'react'
import { imgURL } from '../../components/icons'
import { pctHot, voiceHot } from '../../utils/format'
import { fmtPct, goalNatureLabel, goalReached, hasGoal, hasNatureGoal, lineStats } from './pets'

// SuggestPanel 选配建议:后端按这条线的目标算出的 top N 组合 + 回交对比。
//
// 两条**必须显式**的信息(否则这种面板最容易被当成实测结论):
//   1. 所有属性都是**理论值** —— 按双亲均值推算的确定值,不是孵出来就一定这样;
//   2. 「差距」是打分用的归一化值(0 最好),不是属性本身。
// 故每块都带着自己的口径说明,而不是只丢一排数字。
//
// 体重原先展示成区间(W 89.0~94.2%),现在只给理论单值。区间来自**实测浮动**(≈±2pp),
// 而浮动是每次孵蛋的随机噪声、不是可依赖的收益:画成区间会被读成「至少能到 89」,玩家照着
// 下界去判断够不够,结果每次都不够。理论单值 + 一句「实测有波动」才是它真实的可信度
// (后端仍在下发 weightHi:建议排序与回交对比要用它比「哪组更有戏」)。
//
// 嗓音另有一条硬规则要显式(见下面的可达性警示):子代 = 双亲均值**向零取整**,
// 目标落在上限时差一点就是永远差一点 —— 只按「差距」排序会把这种到不了的组合排进前几名。
export default function SuggestPanel({ line, matrix, suggesting, onPet, onLocate }) {
  const goal = line.goal || {}
  const list = line.suggest || []
  const bc = line.backcross
  const reach = line.reach
  // 目标性格在页面上的显示名:确切性格给名字,「正面加某维」给「加物攻」(见 pets.goalNatureLabel)。
  // 后端下发的 natureP 对两种情况都是「落在集合里」的概率,故文案跟着这个标签走。
  const natLabel = goalNatureLabel(goal, matrix)
  const hasNat = hasNatureGoal(goal)
  const bestVoice = lineStats(line).bestVoice

  if (!hasGoal(goal)) {
    return (
      <section className="br-panel">
        <div className="br-sec-head"><h3>选配建议</h3></div>
        <p className="br-hint muted">
          还没定目标 —— 没有目标时所有组合都「一样好」,排序没有意义。
          先在左边填上你要刷的属性(哪怕只填一项),这里就会按它排序。
        </p>
      </section>
    )
  }

  return (
    <section className="br-panel">
      <div className="br-sec-head">
        <h3>选配建议</h3>
        <span className="muted">
          {suggesting ? '按新目标重算中…' : `前 ${list.length} 组 · 只对填了的目标项打分`}
        </span>
      </div>

      {/* 状态是「已达成」时要说明是谁标的:它是写入时后端自动打上的(见 pet.AutoDoneOnReach),
          不是玩家自己记错了。而它**不会**自己变回去 —— 想接着刷得手动改成「进行中」,
          故这里要把「还能继续」这条出路一并写出来。 */}
      {line.status === 'done' ? (
        <p className="br-hint muted">
          这条线已标记为<b>已达成</b>:填了的目标项被某一代的同一只子代全部满足时,
          记录写入会自动打上这个标记。还想接着刷就改回「进行中」—— 改回去之后不会再被自动改回来。
        </p>
      ) : null}

      {/* 嗓音可达性:后端按现有候选算的下一代极限(reach)。够不着时必须在**列表之前**说清 ——
          排在下面只是一排「差 1」的数字,玩家会以为再配一代就好了。不可达时列表照样给
          (最接近的几组仍有参考价值),只是带着这句前提看。 */}
      {reach && list.length > 0 ? (
        reach.hit ? (
          <p className="br-reach ok" title="嗓音 = 双亲均值向零取整,现有候选里有正好命中的组合">
            嗓音目标 V{reach.target} <b>可达</b>:现有候选里已有能孵出它的组合。
          </p>
        ) : (
          <p className="br-reach warn" title="子代嗓音 = 双亲均值向零取整,任一方不到位就永远到不了">
            <b>V{reach.target} 够不着</b>:按现在这批候选,下一代{reach.best < reach.target ? '最高' : '最低'}只能到
            <b> V{reach.best}</b>。嗓音取双亲均值的向零取整,差一点也不行 ——
            先补一只 V{reach.target} 的候选再来配。
          </p>
        )
      ) : null}

      {list.length === 0 ? (
        <p className="br-hint muted">
          库里还没有可用的组合:种母要同一品种的雌性,种公要跟她同蛋组的雄性。
          在游戏里抓到 / 孵出这样的宠之后回来刷新即可。
        </p>
      ) : (
        <ul className="br-sugs">
          {list.map((s, i) => {
            const vt = voiceTag(goal, s)
            const ng = noGain(s.exp && s.exp.voice, bestVoice, goal.voice)
            return (
            <li key={(s.mother && s.mother.gid) + '-' + (s.father && s.father.gid) + '-' + i} className="br-sug">
              <span className="br-sug-rank">{i + 1}</span>
              <span className="br-sug-pair">
                <SugPet p={s.mother} side="♀" onLocate={onLocate} />
                <span className="br-x" aria-hidden="true">×</span>
                <SugPet p={s.father} side="♂" onLocate={onLocate} />
              </span>
              <span className="br-sug-exp" title={EXP_TITLE}>
                <span className="br-sug-exp-t">理论</span>
                <b className={voiceHot(s.exp.voice) || ''}>V{s.exp.voice}</b>
                <b className={pctHot(s.exp.weightPct) || ''}>W {fmtPct(s.exp.weightPct)}%</b>
                {hasNat ? (
                  <span className="br-sug-nature" title={natureTitle(s.exp)}>
                    {natLabel} {expPct(s.exp.natureP)}
                  </span>
                ) : null}
                {vt ? (
                  <span className={'br-sug-vt' + (vt.k === 'both' ? ' on' : '')} title={vt.title}>{vt.t}</span>
                ) : null}
                {/* 串窝 / 回交原先紧跟在「母 × 父」后面。现在那是一排等宽三列网格(卡 / × / 卡),
                    再往里塞元素会被当成第四列掉到下一行;它们本就是这一组组合自己的属性,
                    与预期值同一行读着更顺。 */}
                {s.ambiguous ? <span className="br-tag amb" title="这位母本有多个可配公:实际是谁等破壳后反推">串窝</span> : null}
                {s.backcross ? <span className="br-tag bc" title="父本就是这条线自己的子代">回交</span> : null}
                {ng ? (
                  <span className="br-tag ng" title={`预期 V${s.exp.voice} 不比历代最佳 V${bestVoice} 更接近目标 —— 嗓音是双亲均值的向零取整,换掉较差的那只才推得动`}>嗓音无提升</span>
                ) : null}
              </span>
              <span className="br-sug-score" title={`按你填的目标项归一化后的平均差距:${scoreText(s.score)}。0 = 完全命中,越小越好`}>
                <span className="br-bar-t"><i className="br-bar-f s" style={{ width: scoreBar(s.score) }} /></span>
                <span className="br-sug-gap">差 {scoreText(s.score)}</span>
              </span>
            </li>
            )
          })}
        </ul>
      )}

      {bc && (
        <div className={'br-back' + (bc.advised ? ' on' : '')}>
          <div className="br-back-head">
            <h4>回交还是换种</h4>
            <span className={'br-verdict ' + (bc.advised ? 'yes' : 'no')}>
              {bc.advised ? '更推荐回交' : '更推荐换种'}
            </span>
          </div>
          <p className="br-back-reason">{bc.reason}</p>
          <div className="br-back-cols">
            <div className={'br-back-col' + (bc.advised ? ' on' : '')}>
              <div className="br-back-t">
                回交 · 子代 × {bc.withParent ? bc.withParent.name : '—'}
              </div>
              <ExpLine exp={bc.exp} nature={natLabel} />
              {bc.withParent ? (
                <button type="button" className="br-back-p" onClick={() => onPet(bc.withParent.gid)}>
                  {bc.withParent.name} V{bc.withParent.voice}
                  {bc.withParent.weightPct != null ? ` W${fmtPct(bc.withParent.weightPct)}%` : ''}
                </button>
              ) : null}
            </div>
            <div className={'br-back-col' + (!bc.advised && bc.alt ? ' on' : '')}>
              <div className="br-back-t">
                换种 · 子代 × {bc.alt ? bc.alt.name : '—'}
              </div>
              {bc.alt ? <ExpLine exp={bc.altExp} nature={natLabel} /> : <p className="br-hint muted">库里没有可比的其它候选</p>}
              {bc.alt ? (
                <button type="button" className="br-back-p" onClick={() => onPet(bc.alt.gid)}>
                  {bc.alt.name} V{bc.alt.voice}
                  {bc.alt.weightPct != null ? ` W${fmtPct(bc.alt.weightPct)}%` : ''}
                </button>
              ) : null}
            </div>
          </div>
          <p className="br-hint muted">
            两组都是<b>理论值</b>(按双亲均值推算),按你填的那几项归一化后比出来的;
            实际孵出的个体会有波动,回交的价值主要在于把已经变好的那段基因固定下来。
          </p>
        </div>
      )}
    </section>
  )
}

// SugPet 建议行里的候选:一张**竖排**卡片(头像在上,名字、嗓音/体重、性格在下)。库里已查不到时不画成按钮。
//
// 头像不是装饰:同名同品种刷了一窝时,清单里的一行文字根本对不上仓库里的哪一只 ——
// 有了头像,玩家在游戏里翻盒子时才能一眼认出「就是这个」。点击也不再开详情,而是直接
// 弹出仓库示意图指出它在第几格(见 PetLocateModal)——「该配谁」之后紧接着的问题就是「它在哪」。
// 竖排而非横排是宽度逼出来的(右栏最窄 300px,父母两只各分不到 100px,而横排「头像 + ♀ +
// 名字 + V + W」要 180 余px);竖排后卡宽只由最长的一行数值决定,头像才放得下 44px。
// 理由详见 breeding.css 的 .br-sug-c。
function SugPet({ p, side, onLocate }) {
  if (!p) return <span className="br-sug-c ph">—</span>
  const clickable = !!(onLocate && p.gid)
  return (
    <button type="button" className={'br-sug-c' + (side === '♀' ? ' f' : ' m')}
      disabled={!clickable} onClick={() => clickable && onLocate(p.gid)}
      title={`${side === '♀' ? '种母' : '种公'} ${p.name}` + (p.nature ? ` · ${p.nature}` : '')
        + (clickable ? ' · 点击定位到仓库格位' : '')}>
      {/* 缺图时也占住 44px 的位置:两张卡一高一低,中间的 × 就会显得又歪了 */}
      {p.img
        ? <img className="br-sug-av" src={imgURL(p.img)} alt="" loading="lazy" />
        : <span className="br-sug-av ph">🐾</span>}
      <span className="br-sug-pn">
        <em>{side}</em>
        <span className="br-sug-n">{p.name}</span>
      </span>
      <span className="br-sug-pv">
        <span className={voiceHot(p.voice) || ''}>V{p.voice}</span>
        {p.weightPct != null ? <span className={pctHot(p.weightPct) || ''}>W{fmtPct(p.weightPct)}%</span> : null}
      </span>
      {/* 双亲的性格:选种时要看的第三个维度 —— 「固执 × 固执」与「胆小 × 急躁」的预期完全不同,
          而这一行原先只有 V/W,得点进详情才知道配的是哪两种性格。快照没有性格时留空
          (不画占位符):空行只会把整排卡片垫高。 */}
      {p.nature ? <span className="br-sug-nat" title={`性格 ${p.nature}`}>{p.nature}</span> : null}
    </button>
  )
}

// noGain 这一组的预期嗓音是否**推不动**了:不比这条线历代最佳子代更好。
//
// 判据与页面其它地方同源(**达标优先,其次离目标更近**,见 pets.goalReached):一个已达标的
// 预期相对未达标的最佳就是「有提升」,哪怕两者离目标一样远。都没达标时才比距离。
// 没填目标时退化为「不比最极端的那只更极端」。
//
// 嗓音 = 双亲均值向零取整,是确定值 —— 若这一组推不动手里最好的那只,再孵多少胎也不会更高,
// 想推进只能换掉较差的那只(拿子代回交,或引进一只更好的)。
//
// 与 voiceTag 一样是纯前端派生:契约快照不该为一句文案多长一个键。
function noGain(voice, best, target) {
  if (voice == null || best == null) return false
  if (target != null) {
    const vh = goalReached(voice, target, 'voice')
    const bh = goalReached(best, target, 'voice')
    if (vh !== bh) return !vh
    return Math.abs(voice - target) >= Math.abs(best - target)
  }
  return Math.abs(voice) <= Math.abs(best)
}

// voiceTag 这一组在嗓音上是否达标。纯前端判断,不往 Suggestion 里加字段 —— 契约快照
// 不该为一句文案多长两个键,而这些量(双亲嗓音、预期值)本来就在下发的那几个字段里。
//   both: 双亲嗓音**都已达标**。由「子代 = 双亲均值向零取整」可证此时子代必然也达标
//         (高目标:两数都不小于目标 → 均值也不小于;低目标:两数都不大于目标 → 均值也不大于)。
//   exp : 预期值达标(可能只有一个亲本达标,也可能两个都没达标但均值到位)。
// 方向见 pets.goalReached(高目标 ≥、低目标 ≤),不是「正好等于目标」。
function voiceTag(goal, s) {
  if (goal.voice == null || !s.exp) return null
  const hi = goal.voice
  const mv = s.mother ? s.mother.voice : null
  const fv = s.father ? s.father.voice : null
  if (mv != null && fv != null && goalReached(mv, hi, 'voice') && goalReached(fv, hi, 'voice')) {
    return { k: 'both', t: '双亲达标', title: `双亲嗓音都已达标(目标 V${hi})—— 子代取双亲均值的向零取整,双亲都到位时子代必然也达标` }
  }
  if (goalReached(s.exp.voice, hi, 'voice')) {
    return { k: 'exp', t: '预计达标', title: `按双亲均值推算,预期 V${s.exp.voice} 已达标(目标 V${hi})` }
  }
  return null
}

// ExpLine 一行理论值(回交对比里用)。
function ExpLine({ exp, nature }) {
  return (
    <div className="br-back-v">
      <b className={voiceHot(exp.voice) || ''}>V{exp.voice}</b>
      <b className={pctHot(exp.weightPct) || ''}>W {fmtPct(exp.weightPct)}%</b>
      {nature ? <span title={natureTitle(exp)}>{nature} {expPct(exp.natureP)}</span> : null}
    </div>
  )
}

// expPct 概率显示:不足 1% 的显示成「<1%」而不是「0.2%」——
// 那个小数位的精度是假的(它只是「全表种类数分之一」的估值),写出来反而像是算准了。
// 现口径下三档是 61% / 31% / 1%,这条路要等性格表长到 40 条以上才走得到。
function expPct(p) {
  if (p == null) return ''
  if (p < 0.01) return '<1%'
  return Math.round(p * 100) + '%'
}

// natureTitle 这个百分比是怎么来的(后端 pet.natureHitP):子代性格分三档定 ——
// 母本性格 30% / 父本性格 30% / 剩下 40% 从全部性格里重掷;两个 30% 是**互斥槽位**,
// 故双亲性格相同时那个性格是 60%(不是 51%)。重掷槽本身也可能掷中目标,
// 所以 31% / 61% 比 30% / 60% 各高一点点。
function natureTitle(exp) {
  switch (exp.natureFrom) {
    case 'parent': return '目标性格带在双亲身上:母 30% + 父 30%(双亲都带时合计 60%),剩下 40% 从全部性格里重掷 —— 这里的数字已含「重掷也可能掷中」那一份'
    case 'roll': return '双亲都没有这个性格:只剩 40% 的重掷槽可指望,按 30 种性格平分'
    default: return '理论性格命中率'
  }
}

// EXP_TITLE 理论值那一行的口径说明。挂在整行上而不是每个数字各一条 title:玩家要判断的是
// 「这排数字可不可信」,拆成三条只会逼他一个个去悬停,还容易只看其中一条就下结论。
const EXP_TITLE = '理论值:按双亲均值推算的确定值 —— 嗓音取均值向零取整,'
  + '体重取均值(实测约有 ±2pp 波动)。不是孵出来必然如此。'

// scoreText 归一化差距:百分比只留一位(它是各项目标的平均距离,不是属性值)。
const scoreText = (s) => (s == null ? '—' : (s * 100).toFixed(1) + '%')

// scoreBar 差距条宽度:差距越小条越长(与卡片上的「离目标多远」反向,注意别混)。
const scoreBar = (s) => Math.max(2, Math.min(100, (1 - (s || 0)) * 100)).toFixed(1) + '%'
