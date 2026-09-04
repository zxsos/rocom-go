import React from 'react'
import { imgURL, useImgFallback, useImgReady, InlineIcon } from '../../components/icons'
import { Types, Marks, Gender, Blood, PetMark } from '../../components/badges'
import { SixGrid, Measure } from './metrics'
import { locTag, voiceHot, fmtShortTime } from '../../utils/format'

// PetCard 陈列视图里的一张宠物卡。与旧版移动卡片(PetCards.jsx)的区别不是"换个皮":
//
//   - **宠物图当主角**:旧版用 34px 头像,而 portraitSmall(全身像)早就随包下发、
//     此前只在详情弹窗里露过面。找宠物时认的是形象,不是那行小字。
//   - **数字改成读数**:六维从「三列纯文字」变成定标条(见 metrics.jsx 的 SixGrid),
//     体重/身高从「值 + 百分位两行」变成带游标的标尺 —— 扫视时读的是位置不是数字。
//   - **不再堆「标签：值」**:性格/特长/奖牌/血脉收成一行胶囊,体型与声音各占一行。
//
// 卡片是**正方形图 + 数据区**的纵向结构,在 240~300px 宽的自适应网格里排开:
// 一屏能看到十几只,异色/炫彩的角标在整片卡片里是能"扫"出来的。

// portrait 取全身像;没有则退大头像、再退小头像(与 components/avatar.jsx 同序)。
// 三者都缺(未上线形态/缺源)时组件回退 emoji,不留空洞。
const portrait = (p) => (p.image && (p.image.portraitSmall || p.image.bigHead || p.image.head)) || ''

export default function PetCard({ p, selected, itemProps }) {
  const src = portrait(p)
  const [bad, onError] = useImgFallback(src)
  const [ready, onLoad, onRef] = useImgReady(src)

  return (
    <article className={'pet-card' + (selected ? ' on' : '')} {...itemProps(p)}>
      <div className="pt-media">
        {src && !bad
          ? (
            <img
              ref={onRef}
              className={'pt-img' + (ready ? '' : ' img-ph')}
              src={imgURL(src)} alt={p.species} loading="lazy"
              onLoad={onLoad} onError={onError}
            />
          )
          : <span className="pt-fallback">{p.shiny ? '✨' : '🐾'}</span>}

        {/* —— 四角徽标:把「属性」压到图上,数据区只留数值 ——
            原先它们挤在数据区一行胶囊里(nowrap + 截断),形态(「夏日的样子」)
            与奖牌常被切掉。移到图上是游戏宠物卡的通行做法:图片四周本来就
            是透明区,四角压标不挡主体,且属性与形象同处一眼扫完。

            四角分工按「语义相近的挨在一起」:
              左上 搭档标记 —— 玩家自己打的标,独占一角免得和稀有度混
              右上 系别(≤2)/形态/血脉 —— 都是「它是什么」
              左下 等级/性格/特长 —— 都是「它有多强」
              右下 异色·炫彩 —— 稀有度,与左上的搭档对角呼应

            **一律竖排**:图区高 104(桌面)/204(移动)、宽 204~241(桌面)/
            96(移动),横排放三个必然溢出;竖排只占 ~40px 宽,且移动端靠
            高 204px 也放得下,两端可用同一套结构。 */}
        {/* 左右两栏而非四个独立角:右栏(系别/形态/血脉 + 异色炫彩)与左栏
            (搭档 + 等级/性格/特长)各自 space-between 上下顶开。四个独立
            absolute 角在 104px 图区里会垂直撞上(实测右上 70px + 右下 32px
            + 边距 > 104),两栏布局则**结构上不可能重叠**。 */}
        <div className="pt-side left">
          <div className="pt-corner tl"><PetMark p={p} /></div>
          <div className="pt-corner bl">
            <span className="pt-chip pt-lv">Lv.{p.level}</span>
            {p.nature && <span className="pt-chip" title="性格">{p.nature}</span>}
            {p.speciality && p.speciality !== '无' && <span className="pt-chip" title="特长">{p.speciality}</span>}
          </div>
        </div>

        <div className="pt-side right">
          <div className="pt-corner tr">
            <Types types={p.types} icons={p.typeIcons} />
            {/* 形态名可能很长(「夏日的样子」),故给上限 + 省略号兜底;
                title 保留全文,悬停可读(原先在数据区被硬截断且无从查看)。 */}
            {p.form && <span className="pt-chip" title={'形态：' + p.form}>{p.form}</span>}
            {p.blood && <Blood p={p} />}
          </div>
          {/* 异色/炫彩横排:竖排会把 3 个标记拉成 32px 高,在 104px 图区里
              吃掉右栏下半 —— 横排只占 15px,且三者同属稀有度,平级并排更合理。 */}
          <div className="pt-corner br row"><Marks p={p} /></div>
        </div>
      </div>

      <div className="pt-body">
        {/* 系别/形态/血脉已移到图上,名字行不再需要右侧栏 —— 名字占满整行,
            长昵称可显示的字更多(原上限 55%)。 */}
        <div className="pt-head">
          <div className="pt-head-text">
            <div className="pt-name">{p.name || p.species}<Gender g={p.gender} /></div>
            <div className="pt-sub">{p.species}{p.book ? ` · #${p.book}` : ''}</div>
          </div>
        </div>

        <SixGrid p={p} />

        {/* 奖牌与蛋组留在数据区:它们名长(「大块头」/「陆上/天空」)、出现率低,
            压在图上会挤掉高频属性。原先混在单行胶囊里被 nowrap 截断(奖牌显示
            不全即由此),现在独占一行、各自可省略,不再互相挤掉。
            显示**组名**而非组数:个数(「蛋组 2」)不含任何可用信息 ——
            玩家要的是「陆上/天空」这些组名,它们对应繁殖范围。 */}
        <div className="pt-meta">
          {p.medal && (
            <span className="pt-meta-i" title={p.medalDesc || '佩戴奖牌'}>
              <InlineIcon src={p.medalIcon} className="pt-medal-ic" alt="" />{p.medal}
            </span>
          )}
          {p.eggGroups?.length > 0 && (
            <span className="pt-meta-i muted" title={'蛋组：' + p.eggGroups.map((g) => (g.desc ? `${g.name}(${g.desc})` : g.name)).join(' / ')}>
              蛋组 {p.eggGroups.map((g) => g.name).join('/')}
            </span>
          )}
        </div>

        {/* 体重/身高各占一行,**不并排**:并排时每半只有 ~105px(200px 卡宽),
            而一行要放「标签 22 + 值 44 + 标尺 ≥20 + 百分位 33 + 间距 12」≈131px,
            实测溢出(scrollWidth 129 > clientWidth 105),百分位数字被标尺压住重合。
            四项里砍任何一项都会丢信息(百分位正是找大块头/小不点的判据),
            故让出一行:全宽 190px 下四项从容排开,标尺还能拉长更易读位置。
            代价 +21px(单卡的 6.8%),由上面名字与系别合并(-19px)大抵抵消。 */}
        <Measure label="体重" value={p.weightKg} unit="kg" min={p.weightMin} max={p.weightMax} pct={p.weightPct} />
        <Measure label="身高" value={p.heightM} unit="m" min={p.heightMin} max={p.heightMax} pct={p.heightPct} />

        <div className="pt-foot">
          <span className="pt-voice">声 <b className={voiceHot(p.voice)}>{p.voice}</b></span>
          <span className="pt-loc" title={locTag(p)}>{locTag(p)}</span>
          <span className="pt-time muted">{fmtShortTime(p.catchTime)}</span>
        </div>
      </div>
    </article>
  )
}
