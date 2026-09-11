import React from 'react'
import { breedableEggGroups } from '../../constants'

// ContextMenu 列表项的右键/长按菜单(fixed 定位在指针处;打开/关闭逻辑在父级)。
//
// 菜单分两组:第一组「看这一只」(详情),第二组「按它的某个属性去找同类 / 找伴」。中间用
// 分隔线断开 —— 蛋组那两项是**动作**(跳培育页),与前面三项「就地加筛选条件」不是一类。
//
// 蛋组两项的可见性由 breedableEggGroups 决定:蛋组为空(超进化/分支形态)或就是「无蛋组」
// (游戏里那一组不可繁殖的)的宠物,既筛不出同类也配不出伴 —— 与其点了给一个空列表,不如
// 不给这两项。其余项照旧。
export default function ContextMenu({ menu, menuRef, onDetail, onFilterSame, onBreed }) {
  if (!menu) return null
  const p = menu.pet || {}
  const groups = breedableEggGroups(p.eggGroups)
  return (
    <div className="ctx-menu" ref={menuRef} style={{ left: menu.x, top: menu.y }} onClick={(e) => e.stopPropagation()}>
      <div className="ctx-item" onClick={() => onDetail(menu.gid)}>查看详情</div>
      <div className="ctx-sep" />
      <div className="ctx-item" onClick={() => onFilterSame({ search: p.species })}>筛选相同种类</div>
      <div className="ctx-item" onClick={() => onFilterSame({ nature: p.nature, natureExclude: '' })}>筛选相同性格</div>
      <div className="ctx-item" onClick={() => onFilterSame({ speciality: p.speciality })}>筛选相同特长</div>
      {groups.length > 0 && (
        <>
          <div className="ctx-sep" />
          {/* 两项都是蛋组操作,差别只在口径(全部相同 / 有一个相同)—— 光看标题认不出来,
              故 title 里各写一句。精确那项要顺手清掉 eggGroups:两种口径并存时以后端
              以精确为准,不清的话面板上会留着那排 chip,而它们其实不生效。 */}
          <div
            className="ctx-item"
            title="只留蛋组与它完全相同的宠物（每一组都要相同）"
            onClick={() => onFilterSame({ eggGroups: [], eggGroupsExact: groups })}
          >筛选相同蛋组</div>
          <div
            className="ctx-item"
            title="去培育页新建一条线，品种只列与它共蛋组的（配种要求母本与种公同蛋组）"
            onClick={() => onBreed(p)}
          >孵蛋配种</div>
        </>
      )}
    </div>
  )
}
