import React from 'react'
import { createPortal } from 'react-dom'
import { IconsContext } from '../context'
import { imgURL, useImgFallback, InlineIcon } from './icons'
import { GLASS_BG, GLASS_BG2, GLASS_PARTICLES, GLASS_COLORS, GLASS_HIDDEN } from '../data/glassConf'

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
export function MarkIcon({ src, title, fallback, cls }) {
  const [bad, onError] = useImgFallback(src)
  if (src && !bad) {
    return <img className="mark-img" src={imgURL(src)} alt={title} title={title} onError={onError} />
  }
  return <span className={'mark ' + cls} title={title}>{fallback}</span>
}

// RKPET_LINK 是姊妹项目 rocom-pets 的站点(https://github.com/whoisnian/rocom-pets)。
// 它能把这只宠物的**模型**按同一套炫彩渲成 3D,`/api/link` 是它给外部工具开的接口:
// 送游戏侧编号(形态 + 异色/炫彩),302 到对应的展示页。「形态编号 → 包名/资产名」与
// look 的写法都由它换算,这里不抄。
const RKPET_LINK = 'https://rkpet.whoisnian.com/api/link'

// rkpetURL 拼这只在 rkpet 的展示链接;缺形态编号或缺炫彩编号时返回 null。
// 后者是**老库**才会缺(glassType/glassValue 是后加的字段,早先入库的行里没有),
// 下次登录全量快照重写那一行就补齐了;这期间色卡照画,只是不给这条链接。
export function rkpetURL(p) {
  if (!p || !p.baseConfId || !p.glassType) return null
  const q = new URLSearchParams({ petbase: String(p.baseConfId) })
  if (p.shiny) q.set('shiny', '1')
  q.set('glass', `${p.glassType}:${p.glassValue}`)
  return `${RKPET_LINK}?${q}`
}

// glassMask 生成 CSS mask 层的行内样式:按素材 alpha 蒙版填色(color),素材拉伸铺满容器。
// 与客户端 UMG 一致(见 scripts/gen_glass.py 与 docs/data.md 3.5):
// 层1 底图 Bg 蒙版填 ui_color_2 → 层2 中层 Bg2 蒙版填 ui_color_1(顶部对齐,图内已含位置)→
// 层3 粒子大图蒙版填白。Bg/粒子为满幅(280x154),Bg2 只有上 108px(280x108,顶部对齐,
// 下面 46px 透明),故 Bg2 高度按 108/154 等比且顶部对齐——若 100% 100% 拉伸会把中层
// 图案拉到全高导致与游戏构图错位(top 传 true 的层走等比)。
export const glassMask = (img, color, top = false) => {
  const url = `url(${imgURL('dazzling/' + img)})`
  return {
    backgroundColor: color,
    WebkitMaskImage: url,
    maskImage: url,
    WebkitMaskRepeat: 'no-repeat',
    maskRepeat: 'no-repeat',
    WebkitMaskSize: top ? '100% calc(100% * 108 / 154)' : '100% 100%',
    maskSize: top ? '100% calc(100% * 108 / 154)' : '100% 100%',
    WebkitMaskPosition: top ? 'top center' : undefined,
    maskPosition: top ? 'top center' : undefined,
  }
}

// GlassZoom 炫彩色卡放大预览:点遮罩、点关闭按钮或按 Esc 关闭。大图保持 280:154 原始
// 构图(与 scripts/gen_glass.py 一致),普通炫彩三层合成、隐藏炫彩直接展示整图。
// 卡片下方另给一条 rkpet 链接,跳过去看同一套炫彩的 3D 效果(见 rkpetURL)。
function GlassZoom({ p, type, value, onClose }) {
  React.useEffect(() => {
    const onKey = (e) => { if (e.key === 'Escape') onClose() }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])
  let body
  if (type === 2) {
    const src = GLASS_HIDDEN[value]
    if (src) body = <img className="glass-zoom-card" src={imgURL('dazzling/' + src)} alt="炫彩色卡" />
  } else {
    const particle = GLASS_PARTICLES[value >> 20]
    const colors = GLASS_COLORS[value & 0xFFFFF]
    if (particle && colors) {
      body = (
        <span className="glass-zoom-card">
          <i style={glassMask(GLASS_BG, colors[1])} />
          <i style={glassMask(GLASS_BG2, colors[0], true)} />
          <i style={glassMask(particle, '#ffffff')} />
        </span>
      )
    }
  }
  if (!body) return null
  const rkpet = rkpetURL(p)
  // Portal 渲染到 body:彻底脱离宿主(宠物列表卡片/详情弹窗)DOM 树,避免两个问题——
  // 1. 移动端 .card:active { transform: scale(.995) } 会让祖先 transform 破坏本遮罩的
  //    position: fixed,按下瞬间遮罩被拉进卡片坐标系而偏移抖动;
  // 2. 触摸/点击事件冒泡到宿主的长按(宠物列表 450ms 弹菜单)与点击(selectPet/弹窗遮罩)
  //    导致点遮罩关闭时误弹菜单或连底层弹窗一起关。
  // 事件已在 body 层,stopPropagation 仅作防御,挡住冒泡到 #root 之上的全局监听。
  const stop = (e) => e.stopPropagation()
  return createPortal(
    <div
      className="glass-zoom-backdrop"
      onClick={(e) => { stop(e); if (e.target === e.currentTarget) onClose() }}
      onTouchStart={stop}
      onTouchMove={stop}
      onTouchEnd={stop}
    >
      <div className="glass-zoom">
        {body}
        <button className="icon-btn glass-zoom-close" onClick={(e) => { stop(e); onClose() }} title="关闭" aria-label="关闭">✕</button>
        {/* rkpet 链接放在**放大预览里**而不是色卡本身上:色卡那一下点击是本地放大
            (本工具原有交互),叠一个跳转会打架;收进预览里两级分离,也不会误触外链。
            只是个普通 <a>,不点不会有任何外部请求(本项目是局域网工具,断网照常用)。 */}
        {rkpet && (
          <a
            className="btn glass-zoom-rkpet"
            href={rkpet}
            target="_blank"
            rel="noopener noreferrer"
            title="在 rkpet 新窗口打开,看这只宠物同一套炫彩的 3D 效果(需浏览器支持 WebGPU)"
            onClick={stop}
          >
            在 rkpet 看 3D 效果 ↗
          </a>
        )}
      </div>
    </div>,
    document.body
  )
}

// GlassChip 渲染炫彩色卡缩略图:普通炫彩(glassType=1)用 CSS mask 按素材 alpha 蒙版三层填色
// 合成(Bg 填 ui_color_2 → Bg2 填 ui_color_1 → 粒子染白);隐藏炫彩(glassType=2)直接引用
// 整图(素材是完整美术图,前端无法重建);配置缺失时退化为原炫彩图标。className 供各场景
// (列表/详情/地图角标/花种角标)覆盖尺寸。点击色卡弹出 GlassZoom 放大预览(见上)。
export function GlassChip({ p, className }) {
  const icons = React.useContext(IconsContext)
  const [zoom, setZoom] = React.useState(false)
  const type = p && p.glassType
  const value = p && p.glassValue
  let card = null
  if (type === 2) {
    const src = GLASS_HIDDEN[value]
    if (src) card = <img src={imgURL('dazzling/' + src)} alt="炫彩" />
  } else if (type === 1 && value > 0) {
    const particle = GLASS_PARTICLES[value >> 20]
    const colors = GLASS_COLORS[value & 0xFFFFF]
    if (particle && colors) {
      card = (
        <>
          <i style={glassMask(GLASS_BG, colors[1])} />
          <i style={glassMask(GLASS_BG2, colors[0], true)} />
          <i style={glassMask(particle, '#ffffff')} />
        </>
      )
    }
  }
  if (!card) return <MarkIcon src={icons.colorful} title="炫彩" fallback="彩" cls="mark-colorful" />
  return (
    <>
      <span
        className={className || 'glass-chip'}
        title="炫彩色卡(点击放大)"
        role="button"
        tabIndex={0}
        onClick={() => setZoom(true)}
        onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); setZoom(true) } }}
      >
        {card}
      </span>
      {zoom && <GlassZoom p={p} type={type} value={value} onClose={() => setZoom(false)} />}
    </>
  )
}

// Marks 渲染异色/炫彩标记:保留游戏图标(炫彩图标 / 异色炫彩合成图标),并排渲染
// CSS mask 炫彩色卡(GlassChip),异色炫彩同样展示色卡;chip=false(蛋列表,按约定
// 蛋不展示色卡)时只显示图标。色卡仅在带有效炫彩数值时渲染,旧数据无数值时
// 只留图标,避免 GlassChip 回退图标与前面图标重复。
export function Marks({ p, chip = true }) {
  const icons = React.useContext(IconsContext)
  if (!p) return null
  const card = chip && p.glassType > 0 && p.glassValue > 0 ? <GlassChip key="card" p={p} /> : null
  if (p.shiny && p.colorful) {
    return (
      <>
        <MarkIcon src={icons.shinyColorful} title="异色炫彩" fallback="异彩" cls="mark-colorful" />
        {card}
      </>
    )
  }
  return (
    <>
      {p.shiny && <MarkIcon src={icons.shiny} title="异色" fallback="异" cls="mark-shiny" />}
      {p.colorful && <MarkIcon src={icons.colorful} title="炫彩" fallback="彩" cls="mark-colorful" />}
      {card}
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
