import React from 'react'
import { GLASS_PARTICLES, GLASS_COLORS, GLASS_HIDDEN } from '../../data/glassConf'
import { imgURL } from '../../components/icons'
import Dropdown from '../../components/Dropdown'

// fmtGlassName 从素材文件名里抠出展示名("img_dazzling_Bg3_png-四角星.png" -> "四角星")。
const fmtGlassName = (name) => (name.split('-')[1] || name).replace(/\.png$/, '')

// glassOf 把色卡选择器的选择换算成后端接口的 glassType/glassValue。
// random -> 0/0(后端随机合法色卡);common -> 1 + (粒子id<<20)|配色id;hidden -> 2 + 赛季id(1/2/3、1000)。
export const glassOf = (g) => {
  if (g.type === 'common') return { glassType: 1, glassValue: (g.particle << 20) | g.color }
  if (g.type === 'hidden') return { glassType: 2, glassValue: g.hidden }
  return { glassType: 0, glassValue: 0 }
}

// GlassPicker 炫彩色卡选择器:随机 / 普通炫彩(粒子+配色) / 隐藏炫彩(赛季整图)。
// value={type, particle, color, hidden};普通/隐藏模式附带小预览(双色渐变块 / 整图缩略)。
export default function GlassPicker({ value, onChange }) {
  const set = (patch) => onChange({ ...value, ...patch })
  return (
    <div className="admin-glass-picker">
      <Dropdown
        value={value.type}
        options={[
          { value: 'random', label: '随机色卡' },
          { value: 'common', label: '普通炫彩' },
          { value: 'hidden', label: '隐藏炫彩' },
        ]}
        onChange={(v) => set({ type: v })}
        title="炫彩色卡类型"
      />
      {value.type === 'common' && (
        <>
          <Dropdown
            value={value.particle}
            options={Object.entries(GLASS_PARTICLES).map(([id, name]) => ({ value: Number(id), label: fmtGlassName(name) }))}
            onChange={(v) => set({ particle: Number(v) })}
            title="粒子形状"
          />
          <Dropdown
            value={value.color}
            options={Object.keys(GLASS_COLORS).map((id) => ({ value: Number(id), label: `配色${id}` }))}
            onChange={(v) => set({ color: Number(v) })}
            title="配色(共 39 组)"
          />
          <span
            className="admin-glass-preview"
            style={{ background: `linear-gradient(90deg, ${GLASS_COLORS[value.color][0]}, ${GLASS_COLORS[value.color][1]})` }}
            title={`配色${value.color} 粒子${value.particle}`}
          />
        </>
      )}
      {value.type === 'hidden' && (
        <>
          <Dropdown
            value={value.hidden}
            options={Object.entries(GLASS_HIDDEN).map(([id, name]) => ({ value: Number(id), label: fmtGlassName(name) }))}
            onChange={(v) => set({ hidden: Number(v) })}
            title="隐藏炫彩(赛季整图)"
          />
          <img className="admin-glass-preview" src={imgURL('dazzling/' + GLASS_HIDDEN[value.hidden])} alt="隐藏炫彩" title="隐藏炫彩整图" />
        </>
      )}
    </div>
  )
}
