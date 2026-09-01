import React, { useCallback, useState } from 'react'
import { adminPendingAnnotations, adminReviewAnnotation } from '../../api'
import { useAdminFetch } from './useAdminFetch'

// AnnotationsCard 标注审核:玩家对协议里查不到名字的技能/特性 id 提交名字,通过后
// 全服共享(众包图鉴)。本卡片只处理 pending 的两类标注(技能 / 特性),可分别切换。
//
// 审核语义:通过一条时,**同一 (kind,code) 的其余待审自动被拒** —— 一个 id 只能有
// 一个被认可的答案(见 internal/store/annotations.go 的 ReviewAnnotation)。
// KINDS 是审核面板的类别切换项。默认「全部」(空 kind):玩家技能与特性都会提交,
// 只盯一类会漏掉另一半 —— 表现为「明明有人提交,面板却是空的」。
const KINDS = [
  { key: '', label: '全部' },
  { key: 'feature', label: '特性' },
  { key: 'skill', label: '技能' },
]

export default function AnnotationsCard({ onUnauthed }) {
  const [kind, setKind] = useState('')
  // kind 为空时两类都拉(后端按 kind 过滤,没有「全部」这个取值),合一份按时间倒序。
  const fetcher = useCallback(async () => {
    const kinds = kind ? [kind] : ['feature', 'skill']
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
        玩家提交的技能/特性标注。通过后全服可见(试炼等只给 id 的场景即显示名字);
        通过某条时,同一 id 的其余待审自动拒绝。
      </p>
      {/* 默认看「全部」而非某一类:玩家两种都会提交,只看一类会让人以为
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
                      {/* 类别徽标:看「全部」时必须能一眼分清是技能还是特性,
                          否则同一个 id 数字(如 288022 与 7020550)容易看混。 */}
                      <span className={'anno-review-kind kind-' + a.kind}>
                        {a.kind === 'skill' ? '技能' : '特性'}
                      </span>
                      <span className="anno-review-name">{a.name}</span>
                      <span className="anno-review-code">id {a.code}</span>
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
