import React, { useEffect, useMemo, useRef, useState } from 'react'
import { getBoxes, getPet, getTeams } from '../api'
import { imgURL } from './icons'
import BoxMap from './BoxMap'
import LocTag from './LocTag'
import { useDialog } from '../hooks/useDialog'
import { buildContainers, boxIndexOf } from './boxLayout'

// PetLocateModal 仓库定位弹窗:把某只宠物放进「与宠物列表页一致」的仓库示意图里指认出来。
//
// 为什么需要它:选配建议只说「小母 V40 × 小公乙 V60」,玩家得进游戏把这两只找出来才能配。
// 名字与属性都在,唯独「在哪一格」没有 —— 详情弹窗答不了这个问题(它只给一行位置文案,
// 没有图),故这里直接复用宠物页那张 30 格图,把那格点亮。
//
// 只读:不提供挪格之类的写操作。定位过程中的误操作代价太高(挪错一格得回游戏里找回来),
// 而这里的作用只是「告诉玩家它在哪」。
export default function PetLocateModal({ gid, onClose, onDetail }) {
  const [cur, setCur] = useState(gid) // 当前指认的那只(点图上别的格子会换目标)
  const [pet, setPet] = useState(null)
  const [err, setErr] = useState(false)
  const [containers, setContainers] = useState([])
  const [idx, setIdx] = useState(0)
  const cardRef = useRef(null)
  const { ref: dialogRef, dialogProps } = useDialog('宠物位置')

  // 盒子与队伍只在挂载时拉一次:开着弹窗翻上/下一个容器不该每翻一次都重拉一份。
  useEffect(() => {
    let alive = true
    Promise.all([getBoxes(), getTeams()])
      .then(([boxes, teams]) => { if (alive) setContainers(buildContainers(teams, boxes)) })
      .catch(() => { if (alive) setContainers([]) })
    return () => { alive = false }
  }, [])

  // 位置要**现查**:宠物刚在游戏里挪过格时,快照里的盒位才是最新的(列表页也是这么做的)。
  useEffect(() => {
    let alive = true
    setErr(false)
    getPet(cur).then(
      (p) => { if (alive) setPet(p) },
      () => { if (alive) { setPet(null); setErr(true) } },
    )
    return () => { alive = false }
  }, [cur])

  // 自动切到它所在的容器(队伍排在最前,即下标 0)。依赖 containers:两者谁后到都再算一次。
  useEffect(() => {
    if (!pet || !containers.length) return
    const i = pet.team ? 0 : boxIndexOf(containers, pet.box && pet.box.boxId)
    if (i >= 0) setIdx(i)
  }, [pet, containers])

  // 位置在快照里找不到时也得说实话:可能刚挪过格、快照还没同步,别让玩家盯着一张
  // 不含它的图找半天(下面那张图仍然给出来,翻别的容器就能继续看)。
  const found = useMemo(() => {
    if (!pet) return true
    return containers.some((c) => (c.slots || []).includes(pet.gid))
  }, [pet, containers])

  useEffect(() => {
    const onKey = (e) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const nav = (d) => setIdx((i) => (containers.length ? (i + d + containers.length) % containers.length : 0))
  const active = containers[idx] || null

  // 点卡片与工具栏之外 → 关闭(与详情弹窗同一手感)。
  const onBackdrop = (e) => {
    if (cardRef.current && cardRef.current.contains(e.target)) return
    onClose()
  }

  return (
    <div className="detail-backdrop" ref={dialogRef} {...dialogProps} onClick={onBackdrop}>
      <div className="br-loc-card" ref={cardRef}>
        <div className="toolbar">
          <button className="btn" onClick={onClose}>← 返回</button>
          <div className="spacer" />
          {pet && onDetail ? (
            <button className="btn primary" onClick={() => onDetail(pet.gid)}>查看详情</button>
          ) : null}
        </div>

        <div className="br-loc-body">
          {err ? (
            <div className="empty">未找到该宠物</div>
          ) : (
            <>
              <div className="br-loc-head">
                {pet && pet.image && pet.image.head
                  ? <img className="br-loc-av" src={imgURL(pet.image.head)} alt="" />
                  : null}
                <div className="br-loc-title">
                  <b>{pet ? (pet.name || pet.species) : '定位中…'}</b>
                  {pet ? (
                    <span className="muted">
                      {pet.species}
                      {pet.gender ? ` · ${pet.gender}` : ''}
                      {pet.voice != null ? ` · V${pet.voice}` : ''}
                    </span>
                  ) : null}
                </div>
                <div className="spacer" />
                {pet ? <LocTag pet={pet} className="br-loc-tag" /> : null}
              </div>

              {/* 换目标后旧图先收起来:p.slots 还没换时高亮会短暂停在上一只身上 */}
              {pet && containers.length ? (
                <BoxMap container={active} selected={pet.gid} onCell={(g) => setCur(g)}
                  onPrev={() => nav(-1)} onNext={() => nav(1)} />
              ) : (
                <p className="br-hint muted">正在读取仓库位置…</p>
              )}

              {!found && containers.length ? (
                <p className="br-hint muted">
                  这张图里找不到它 —— 位置可能刚变、盒子快照还没同步(游戏里挪过格、或刚孵出来)。
                  翻一翻别的容器,或回宠物列表页刷新一次。
                </p>
              ) : (
                <p className="br-hint muted">
                  高亮格就是它现在的位置,与宠物列表页左上角那张图一致。
                  上/下一个可翻容器;点图里别的格子换成那一只。
                </p>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  )
}
