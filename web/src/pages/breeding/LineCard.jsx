import React from 'react'
import { imgURL } from '../../components/icons'
import { pctHot, voiceHot, fmtShortTime } from '../../utils/format'
import { fmtPct, goalBits, goalProgress, lineAvatar, lineStats, pendCounts } from './pets'

// 状态文案与后端 pet.BreedingActive/Done/Archived 对应;未知状态原样显示,不吞掉。
const STATUS = { active: '进行中', done: '已达成', archived: '归档' }

// LineCard 是一条培育线的列表卡片:品种 / 状态 / 目标 / 代数与历代最佳 / 离目标多远。
//
// 整卡是**一个 button**(不是 div + onClick):键盘可聚焦、回车可开、读屏会念成按钮。
// 内部一律用 span(button 的内容模型是短语内容),排布交给 CSS 的 display:flex ——
// 拿 div 塞进 button 虽然浏览器也能画,但那是无效 HTML,且会让「可点区域」的语义变糊。
//
// 进度条只画**填了的目标项**(口径同后端 Score):只想刷嗓音的线不该被没填的体重项拖着走。
export default function LineCard({ line, chains, matrix, onOpen }) {
  const st = lineStats(line)
  // 性格那一枚徽标要靠方阵才能把一组名字还原成「加物攻」(见 pets.goalNatureLabel)——
  // 拿不到方阵时它退化成「N 种性格」,而不是显示一个错的维度名。
  const bits = goalBits(line.goal, matrix)
  const bars = goalProgress(line)
  const pend = pendCounts(line.pending)
  // chains 只为「代数还是空的线」服务:那种没有亲本快照可拿,退回这个品种的蛋(见 pets.lineAvatar)。
  const avatar = lineAvatar(line, chains)
  const status = line.status || 'active'
  // 线认的是**品种**(进化链):chainName 把链上各阶段都列出来,只显示 species 会让人以为
  // 这条线只认那一个形态名(见 api.js 的 getBreeding 注释)。
  const kind = line.chainName || line.species || '未定品种'

  return (
    <button
      type="button"
      className={'br-card s-' + status + (pend.incubating + pend.claim > 0 ? ' has-pending' : '')}
      onClick={() => onOpen(line.id)}
      title={`${kind} · 第 ${st.gens} 代 · 点击展开这条培育线`}
    >
      <span className="br-card-top">
        {avatar
          ? <img className="br-card-img" src={imgURL(avatar)} alt="" draggable={false} />
          : <span className="br-card-img ph">?</span>}
        <span className="br-card-name">
          <span className="br-card-species">{kind}</span>
          <span className="br-card-sub muted">{st.gens} 代 · 最近 {fmtShortTime(line.updatedAt)}</span>
        </span>
        <span className={'br-status s-' + status}>{STATUS[status] || status}</span>
      </span>

      <span className="br-badges">
        {bits.map((b) => (
          <span key={b.k} className={'br-badge' + (b.on ? ' on' : '')} title={b.title}>
            <em>{b.k}</em>{b.v}
          </span>
        ))}
      </span>

      {bars.length > 0 ? (
        <span className="br-bars">
          {bars.map((b) => (
            <span key={b.k} className="br-bar" title={b.title}>
              <span className="br-bar-k">{b.k}</span>
              <span className="br-bar-t"><i className={'br-bar-f ' + b.cls} style={{ width: b.pct.toFixed(1) + '%' }} /></span>
              <span className="br-bar-v">{b.text}</span>
            </span>
          ))}
        </span>
      ) : (
        <span className="br-card-hint muted">还没定目标 —— 打开这条线填上要刷的属性,底下的选种建议才有意义</span>
      )}

      <span className="br-card-foot">
        {/* 待孵与待认领分开写:前者在等一颗蛋(可能永远等不到),后者在等子代进背包 ——
            混成一句「待认领 N」会让人以为每一条都要他操作。 */}
        {pend.incubating > 0 ? (
          <span className="br-pend incubating" title="已收蛋、还没孵:破壳后会自动补上孵出的那只">待孵 {pend.incubating}</span>
        ) : null}
        {pend.claim > 0 ? (
          <span className="br-pend" title="破壳已确认、子代还没认领:点进去指定是哪一只">待认领 {pend.claim}</span>
        ) : null}
        {pend.incubating === 0 && pend.claim === 0 ? <span /> : null}
        {st.gens > 0 ? (
          <span className="br-best">
            历代最佳
            <b className={voiceHot(st.bestVoice) || ''}>{st.bestVoice == null ? '—' : st.bestVoice}</b>
            <b className={pctHot(st.bestWeight) || ''}>{st.bestWeight == null ? '—' : fmtPct(st.bestWeight) + '%'}</b>
          </span>
        ) : <span className="br-best muted">还没有子代记录</span>}
      </span>
    </button>
  )
}
