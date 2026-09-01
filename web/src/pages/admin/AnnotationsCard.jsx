import React, { useCallback, useState } from 'react'
import { adminPendingAnnotations, adminReviewAnnotation } from '../../api'
import { ANNOTATION_KINDS } from '../../components/annotations'
import { useAdminFetch } from './useAdminFetch'

// AnnotationsCard 标注审核:玩家对协议里查不到名字的 id 提交名字,通过后
// 全服共享(众包图鉴)。本卡片处理 pending 的三类标注(技能 / 特性 / 精灵),可分别切换。
//
// 三类对象(kind)各有各的「缺什么」:
//   skill / feature  协议给了 id、缺名字;
//   event            草系试炼的事件 → 对应哪只精灵,协议连对象都没给。
// 第三类与前两类共用同一张表、同一套审核流程 —— 故**每加一个 kind 都要改这里**,
// 否则玩家提交成功、面板却永远显示「暂无待审核」,数据躺在库里没人看得见。
//
// 审核语义:通过一条时,**同一 (kind,code) 的其余待审自动被拒** —— 一个 id 只能有
// 一个被认可的答案(见 internal/store/annotations.go 的 ReviewAnnotation)。
// 类别列表与文案一律从 annotations.jsx 的 ANNOTATION_KINDS 派生(那是唯一登记处):
// 新增 kind 时若在别处各写一份,漏掉的那处症状是**静默的** ——
// 玩家提交成功、数据进了库,但面板永远显示「暂无待审核」,没有任何报错指向真正原因。
//
// KINDS 是切换按钮(首项是「全部」);ALL_KINDS 是「全部」时实际去拉的列表
// (后端按 kind 过滤,没有「全部」这个取值)。二者同源于一处,不会再走散。
const KINDS = [{ key: '', label: '全部' }, ...ANNOTATION_KINDS]
const ALL_KINDS = ANNOTATION_KINDS.map((k) => k.key)
// 徽标文案查表。**不能写成三元**:「不是技能就是特性」在加了 event 之后
// 会把精灵标注显示成「特性」,审核者据此判断必错。
const KIND_LABEL = Object.fromEntries(ANNOTATION_KINDS.map((k) => [k.key, k.label]))
const CODE_LABEL = Object.fromEntries(ANNOTATION_KINDS.map((k) => [k.key, k.codeLabel]))

export default function AnnotationsCard({ onUnauthed }) {
  const [kind, setKind] = useState('')
  const fetcher = useCallback(async () => {
    const kinds = kind ? [kind] : ALL_KINDS
    const lists = await Promise.all(kinds.map((k) => adminPendingAnnotations(k)))
    return lists.flatMap((d) => d.items || []).sort((a, b) => b.id - a.id)
  }, [kind])
  const { data: items, error, loading, refresh } = useAdminFetch(fetcher, onUnauthed, kind)
  const [err, setErr] = useState('')      // 审核操作的错误提示(列表错误走 error)
  const [busyId, setBusyId] = useState(0) // 正在提交的条目,防重复点击

  const review = async (id, approve) => {
    setErr('')
    setBusyId(id)
    try {
      await adminReviewAnnotation(id, approve)
      refresh()
    } catch (e) {
      setErr(e.message || '审核失败')
    } finally {
      setBusyId(0)
    }
  }

  return (
    // admin-wide:审核要读「名字 + 描述 + 提交者」再决定,窄卡片(自动网格给的
    // 半屏宽)里描述会折成好几行、按钮挤成一团。跨整行才有足够横向空间。
    <div className="admin-card admin-annotations admin-wide">
      <h3>标注审核</h3>
      <p className="admin-hint">
        玩家提交的技能/特性/精灵标注。通过后全服可见(试炼等只给 id 的场景即显示名字);
        通过某条时,同一 id 的其余待审自动拒绝。
        精灵那类标的是「草系试炼事件 → 哪只精灵」,通过后试炼页显示头像并带出特性名。
      </p>
      {/* 默认看「全部」而非某一类:玩家三类都会提交,只看一类会让人以为
          「别人提交的没进来」—— 那是误判,数据其实在另一类里。 */}
      <div className="anno-review-kinds">
        {KINDS.map(({ key: k, label }) => (
          <button
            key={k}
            type="button"
            className={'btn' + (kind === k ? ' primary' : '')}
            onClick={() => setKind(k)}
          >
            {label}
          </button>
        ))}
      </div>
      {/* 401 会由 useAdminFetch 上报、Admin 把整页踢回登录页,**不会**走到这里。
          故凡是能显示出来的 error 都是 500/网络一类:给出「数据没拉到」而非
          「没有待审」—— 两者在用户看来都是「列表空的」,但前者是故障。 */}
      {error && <p className="admin-error">待审列表加载失败:{error.message}(不是没有待审)</p>}
      {err && <p className="admin-error">{err}</p>}
      {loading
        ? <p className="admin-hint">加载中…</p>
        : !items || items.length === 0
          ? <p className="admin-hint">暂无待审核标注。</p>
          : (
            <div className="anno-review">
              {items.map((a) => (
                <div key={a.id} className="anno-review-row">
                  <div className="anno-review-main">
                    <div className="anno-review-line">
                      {/* 类别徽标:看「全部」时必须能一眼分清是哪一类,
                          否则同一个数字(如 288022 与 7020550)容易看混。 */}
                      <span className={'anno-review-kind kind-' + a.kind}>
                        {KIND_LABEL[a.kind] || a.kind}
                      </span>
                      <span className="anno-review-name">{a.name}</span>
                      <span className="anno-review-code">{CODE_LABEL[a.kind] || 'id'} {a.code}</span>
                    </div>
                    {a.desc && <div className="anno-review-desc">{a.desc}</div>}
                    <div className="anno-review-sub">由 {a.submitter} 提交</div>
                  </div>
                  <button
                    className="btn primary"
                    disabled={busyId === a.id}
                    onClick={() => review(a.id, true)}
                  >
                    通过
                  </button>
                  <button
                    className="btn danger"
                    disabled={busyId === a.id}
                    onClick={() => review(a.id, false)}
                  >
                    拒绝
                  </button>
                </div>
              ))}
            </div>
          )}
    </div>
  )
}
