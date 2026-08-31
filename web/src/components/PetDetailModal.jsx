import React, { useState, useEffect, useRef } from 'react'
import { toPng } from 'html-to-image'
import { getPet, getMedals, getEvolution, subscribe } from '../api'
import { InlineIcon, ImgAvatar } from './icons'
import { Skeleton } from './Skeleton'
import { Types, Marks, Gender, Form, Blood, EggGroups } from './badges'
import { Portrait } from './avatar'
import { toast } from './toast'
import { useDialog } from '../hooks/useDialog'
import { StatRadar, StatRange } from './stats'
import { locTag, fmtTime, voiceHot, pctHot } from '../utils/format'

// PetDetailModal 宠物详情弹窗:覆盖在当前页面之上,不打断底层正在操作的列表/事件页。
// 点击卡片外区域、按 Esc、点返回均触发 onClose。
export function PetDetailModal({ gid, onClose }) {
  const [pet, setPet] = useState(null)
  const [err, setErr] = useState(false)
  const [medals, setMedals] = useState([])
  const [chain, setChain] = useState([])
  const cardRef = useRef(null)
  // 焦点管理(P5.4.3):打开即聚焦弹窗内第一个可聚焦元素(「← 返回」按钮),
  // Tab 圈在弹窗内,关闭后焦点还给点开它的那张卡片。
  const { ref: dialogRef, dialogProps } = useDialog(pet?.name || pet?.species || '宠物详情')

  useEffect(() => {
    setPet(null)
    setErr(false)
    setChain([])
    getPet(gid).then(setPet).catch(() => setErr(true))
  }, [gid])
  useEffect(() => { getMedals().then(setMedals).catch(() => {}) }, [])
  // 实时:进化/换牌/改标记等就地更新会广播完整宠物(带 gid,已按账号过滤),
  // 命中当前详情 gid 时静默重拉,弹窗内容随之刷新(不置 null,避免闪烁)。
  useEffect(() => subscribe('pet', (d) => {
    if (d && String(d.gid) === String(gid)) getPet(gid).then(setPet).catch(() => {})
  }), [gid])
  // 进化链:按当前形态 petbase(base_conf_id)拉取整条链
  const baseConfId = pet?.baseConfId
  useEffect(() => {
    if (baseConfId) getEvolution(baseConfId).then((c) => setChain(c || [])).catch(() => {})
  }, [baseConfId])
  // Esc 关闭弹窗
  useEffect(() => {
    const onKey = (e) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const exportImg = () => {
    if (!cardRef.current) return
    toPng(cardRef.current, { pixelRatio: 2, backgroundColor: '#0e1116', cacheBust: true })
      .then((url) => {
        const a = document.createElement('a')
        a.href = url
        a.download = `${pet.name || pet.species}_${pet.gid}.png`
        a.click()
      })
      .catch(() => toast('导出失败,请重试', 4000)) // toast 而非 alert:不阻塞、样式跟主题
  }

  // 点击卡片与工具栏之外的区域 → 关闭
  const onBackdrop = (e) => {
    if (cardRef.current && cardRef.current.contains(e.target)) return
    if (e.target.closest && e.target.closest('.toolbar')) return
    onClose()
  }

  if (err || !pet) return (
    <div className="detail-backdrop" ref={dialogRef} {...dialogProps} onClick={onClose}>
      {/* 骨架屏(P5.4.2):复用 .detail-card / .detail-body / .kv 的真实布局类搭出同构骨架
          (卡片圆角、16px 内边距、kv 两列网格都由它们给),数据到位即就地填色。 */}
      <div className="detail-wrap">
        {err
          ? <div className="empty">未找到该宠物</div>
          : (
            <div className="detail-card">
              <div className="detail-body" role="status" aria-label="加载中">
                <Skeleton h={18} w={170} />
                <Skeleton h={200} />
                <Skeleton h={29} w={210} />
                <div className="kv">
                  {Array.from({ length: 8 }, (_, i) => <Skeleton key={i} h={50} />)}
                </div>
              </div>
            </div>
          )}
      </div>
    </div>
  )

  const ownedMedals = medals.filter((m) => (pet.medalIds || []).includes(m.id))

  return (
    <div className="detail-backdrop" ref={dialogRef} {...dialogProps} onClick={onBackdrop}>
      <div className="detail-wrap">
      <div className="toolbar">
        <button className="btn" onClick={onClose}>← 返回</button>
        <div className="spacer" />
        <button className="btn primary" onClick={exportImg}>保存为图片</button>
      </div>

      <div className="detail-card" ref={cardRef}>
        <div className="detail-head">
          <span className="detail-no">No.{pet.gid}{pet.book ? ` · 图鉴#${pet.book}` : ''}</span>
          <span>{pet.species} <Gender g={pet.gender} /></span>
        </div>
        <Portrait p={pet} />
        <div className="detail-title">
          <h2>{pet.name || pet.species}</h2>
          <span className="lv">Lv.{pet.level}</span>
          <Marks p={pet} />
          <Form form={pet.form} />
        </div>

        <div className="detail-body">
          <div className="detail-tags">
            {pet.talentRank && <span className={'pill' + (pet.talentRank === '了不起的天分' ? ' pill-gold' : '')}>{pet.talentRank}</span>}
            <Types types={pet.types} icons={pet.typeIcons} plain />
            <Blood p={pet} />
          </div>

          <StatRadar p={pet} />

          <div className="kv">
            <Item k="性格" v={pet.nature} />
            <Item k="特长" v={pet.speciality || '无'} />
            <Item k="蛋组" v={pet.eggGroups?.length ? <EggGroups groups={pet.eggGroups} /> : '未知'} />
            <Item k="身高" v={<StatRange value={pet.heightM} min={pet.heightMin} max={pet.heightMax} pct={pet.heightPct} unit=" m" />} />
            <Item k="体重" v={<span className={pctHot(pet.weightPct)}><StatRange value={pet.weightKg} min={pet.weightMin} max={pet.weightMax} pct={pet.weightPct} unit=" kg" /></span>} />
            <Item k="声音" v={<span className={voiceHot(pet.voice)}>{pet.voice}</span>} />
            <Item k="位置" v={locTag(pet)} />
            <Item k="捕捉时间" v={fmtTime(pet.catchTime)} title={fmtTime(pet.catchTime)} />
          </div>

          {chain.length > 1 && <EvoChain chain={chain} current={pet.baseConfId} form={pet.form} />}

          <SkillBlock
            title="天生技能"
            note="（升级习得）"
            skills={pet.skills}
            withLevel
          />
          <SkillBlock
            title="可学技能"
            note="（技能石）"
            skills={pet.learnable}
          />
          <SkillBlock
            title="血脉技能"
            note="（血脉获得）"
            skills={pet.bloodline}
          />

          {ownedMedals.length > 0 && (
            <div>
              <div className="muted" style={{ marginBottom: 6 }}>奖牌墙</div>
              <div className="medals">
                {ownedMedals.map((m) => (
                  <div key={m.id} className="medal medal-tip" data-tip={m.name + (m.desc ? '：' + m.desc : '')}>
                    {m.icon ? <InlineIcon src={m.icon} className="medal-ic" alt={m.name} /> : '🏅'}
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      </div>
      </div>
    </div>
  )
}

// evoStages 把进化链(后端已按 阶段,图鉴号 排序)按阶段分组:每组=同一进化阶段的形态,
// 同组有多项即分支进化(如三阶的 翠顶夫人/黑羽夫人)。返回 [[stage1 形态...], [stage2...], ...]。
function evoStages(chain) {
  const stages = []
  for (const s of chain) {
    const last = stages[stages.length - 1]
    if (last && last[0].stage === s.stage) last.push(s)
    else stages.push([s])
  }
  return stages
}

// SkillRow 一行技能:「技能名 · 属性 · 威力/能耗」,悬浮出完整效果描述。
// 天生技能额外在左侧显示学会等级(其余两类没有等级这个概念)。
function SkillRow({ s, level }) {
  return (
    <div className="skill-row" title={s.effect || ''}>
      {level !== undefined ? (
        <span className="skill-lv">Lv{level}</span>
      ) : (
        <span className="skill-lv" />
      )}
      <span className="skill-name">{s.name}</span>
      {s.elem && <span className="skill-elem">{s.elem}</span>}
      <span className="skill-num">
        {s.power && s.power !== '—' ? `威力 ${s.power}` : '—'}
        {s.cost !== '' && s.cost !== undefined ? ` · 能耗 ${s.cost}` : ''}
      </span>
    </div>
  )
}

// SkillBlock 一个技能区块(天生 / 可学 / 血脉)。
//
// 三点要说清,免得被误读成「这只宠物当前带的技能」:
//   - 这些是**该形态可获得的技能**(可换配置),不是个体当前携带的 —— 后端不存
//     个体技能(见 git 0762eb6),技能也可经技能石等途径更换。
//   - 三类按获取途径划分,几乎互斥(天生∩技能石 3 条、与血脉 0 条),故并列不去重。
//   - 数据来自第三方资料站(过渡方案),只覆盖部分形态;没数据时整块不显示。
function SkillBlock({ title, note, skills, withLevel }) {
  if (!skills || !skills.length) return null
  return (
    <div>
      <div className="muted" style={{ marginBottom: 6 }}>
        {title}
        <span className="skills-note">{note}</span>
      </div>
      <div className="skills">
        {skills.map((s, i) => (
          <SkillRow key={i} s={s} level={withLevel ? s.level : undefined} />
        ))}
      </div>
    </div>
  )
}

// EvoChain 进化链:各阶段头像横向排列,当前形态高亮;同阶段多形态=分支进化,纵向并列。
function EvoChain({ chain, current, form }) {
  return (
    <div>
      <div className="muted" style={{ marginBottom: 6 }}>进化链{form ? `（${form}）` : ''}</div>
      <div className="evo-chain">
        {evoStages(chain).map((forms, i) => (
          <React.Fragment key={forms[0].stage}>
            {i > 0 && <span className="evo-arrow">→</span>}
            <div className="evo-stage">
              {forms.map((s) => (
                <div key={s.petbase} className={'evo-step' + (s.petbase === current ? ' on' : '')} title={`图鉴#${s.book}`}>
                  <ImgAvatar src={s.image && s.image.head} alt={s.name} className="evo-avatar" />
                  <div className="evo-name">{s.name}</div>
                </div>
              ))}
            </div>
          </React.Fragment>
        ))}
      </div>
    </div>
  )
}

function Item({ k, v, title }) {
  return (
    <div className="item">
      <div className="k">{k}</div>
      <div className="v" title={title}>{v ?? '-'}</div>
    </div>
  )
}
