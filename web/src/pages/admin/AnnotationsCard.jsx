import React, { useCallback, useState } from 'react'
import { adminPendingAnnotations, adminReviewAnnotation } from '../../api'
import { useAdminFetch } from './useAdminFetch'

// AnnotationsCard 标注审核:玩家对协议里查不到名字的技能/特性 id 提交名字,通过后
// 全服共享(众包图鉴)。本卡片只处理 pending 的两类标注(技能 / 特性),可分别切换。
//
// 审核语义:通过一条时,**同一 (kind,code) 的其余待审自动被拒** —— 一个 id 只能有
// 一个被认可的答案(见 internal/store/annotations.go 的 ReviewAnnotation)。
export default function AnnotationsCard({ onUnauthed }) {
  const [kind, setKind] = useState('feature')
  const fetcher = useCallback(() => adminPendingAnnotations(kind).then((d) => d.items || []), [kind])
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
    <div className="admin-card admin-annotations">
      <h3>标注审核</h3>
      <p className="admin-hint">
        玩家提交的技能/特性标注。通过后全服可见(试炼等只给 id 的场景即显示名字);
        通过某条时,同一 id 的其余待审自动拒绝。
      </p>
      <div className="anno-review-kinds">
        {['feature', 'skill'].map((k) => (
          <button
            key={k}
            type="button"
            className={'btn' + (kind === k ? ' primary' : '')}
            onClick={() => setKind(k)}
          >
            {k === 'feature' ? '特性' : '技能'}
          </button>
        ))}
      </div>
      {error && <p className="admin-error">{error.message}</p>}
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
                    <div>
                      <span className="anno-review-name">{a.name}</span>
                      <span className="anno-review-code"> · id {a.code}</span>
                    </div>
                    {a.desc && <div className="anno-review-desc">{a.desc}</div>}
                    <div className="anno-review-sub">由 {a.submitter}</div>
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
