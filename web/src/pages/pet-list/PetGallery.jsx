import React from 'react'
import PetCard from './PetCard'

// PetGallery 陈列视图:自适应网格(见 list.css 的 .pet-grid),卡片宽度按可用空间自动
// 决定列数,从窄屏单列到宽屏 5 列都无需断点 —— 断点那几档跳变没有依据,
// 而"每张卡至少多宽才好看"是个连续约束。
//
// 空列表与骨架屏不在这里:它们由 PetList 统一渲染在两个视图之下,
// 免得两个视图各写一份「没有匹配的宠物」。
export default function PetGallery({ pets, selected, itemProps }) {
  return (
    <div className="pet-grid">
      {pets.map((p) => (
        <PetCard key={p.gid} p={p} selected={p.gid === selected} itemProps={itemProps} />
      ))}
    </div>
  )
}
