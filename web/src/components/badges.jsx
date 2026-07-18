import React from 'react'
import { IconsContext } from '../context'
import { imgURL, useImgFallback, InlineIcon } from './icons'

// 宠物名称行内的各种小徽标(性别/异色炫彩/血脉/形态/蛋组/系别/搭档标记)。

// Gender 渲染性别符号(♂ 蓝、♀ 粉,加大加粗,字体差异下也易辨)。
export function Gender({ g }) {
  if (g !== '♂' && g !== '♀') return null
  return <span className={'gender ' + (g === '♂' ? 'male' : 'female')}>{g}</span>
}

// Form 渲染地区/季节形态徽标(普通宠物为空)。
export function Form({ form }) {
  if (!form) return null
  return <span className="mark mark-form" title="形态">{form}</span>
}

// MarkIcon 渲染单个异色/炫彩标记图;无图或加载失败退化为原文字徽标(异/彩)。
function MarkIcon({ src, title, fallback, cls }) {
  const [bad, onError] = useImgFallback(src)
  if (src && !bad) {
    return <img className="mark-img" src={imgURL(src)} alt={title} title={title} onError={onError} />
  }
  return <span className={'mark ' + cls} title={title}>{fallback}</span>
}

// Marks 渲染异色/炫彩标记(优先游戏图标;两者兼具用合成的异色炫彩图)。
export function Marks({ p }) {
  const icons = React.useContext(IconsContext)
  if (!p) return null
  if (p.shiny && p.colorful && icons.shinyColorful) {
    return <MarkIcon src={icons.shinyColorful} title="异色炫彩" fallback="异彩" cls="mark-colorful" />
  }
  return (
    <>
      {p.shiny && <MarkIcon src={icons.shiny} title="异色" fallback="异" cls="mark-shiny" />}
      {p.colorful && <MarkIcon src={icons.colorful} title="炫彩" fallback="彩" cls="mark-colorful" />}
    </>
  )
}

// Blood 渲染血脉(主图标 + 中文短名);iconOnly=仅图标(列表用,名称落到 title)。
export function Blood({ p, iconOnly }) {
  if (!p || !p.blood) return null
  return (
    <span className="blood" title={'血脉 ' + p.blood}>
      <InlineIcon src={p.bloodIcon} className="blood-ic" alt={p.blood} />{!iconOnly && p.blood}
    </span>
  )
}

// EggGroups 展示宠物蛋组(繁殖组)标签,每个组名 hover 显示官方描述;无蛋组返回 null。
export function EggGroups({ groups }) {
  if (!groups || !groups.length) return null
  return (
    <span className="egg-groups">
      {groups.map((g) => (
        <span key={g.id} className="egg-group" title={g.desc ? `蛋组 · ${g.desc}` : '蛋组'}>{g.name}</span>
      ))}
    </span>
  )
}

// Types 渲染系别(icons 与 types 一一对应,前置属性小图);plain=去掉色块背景,仅图标+文字。
export function Types({ types, icons, plain }) {
  const list = types || []
  const cls = plain ? 'type type-plain' : 'type'
  return (
    <>
      {list.map((t, i) => (
        <span key={i} className={cls} data-t={t}>
          <InlineIcon src={icons && icons[i]} className="type-ic" alt="" />{t}
        </span>
      ))}
      {list.length === 0 && <span className="muted">-</span>}
    </>
  )
}

// PetMark 渲染搭档标记徽章(橙色外框底 img_collect + 白色标记符号),叠在头像左上角;
// 无标记(值 0=无)或缺符号图时不渲染。
export function PetMark({ p }) {
  const icons = React.useContext(IconsContext)
  if (!p || !p.partnerMarkIcon || p.partnerMark === '无') return null
  return (
    <span className="pet-mark" title={p.partnerMark}>
      {icons.partnerFrame && <img className="pet-mark-frame" src={imgURL(icons.partnerFrame)} alt="" />}
      <img className="pet-mark-ic" src={imgURL(p.partnerMarkIcon)} alt={p.partnerMark} />
    </span>
  )
}
