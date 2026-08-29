import React, { useCallback, useState } from 'react'
import { adminRules, adminSetRule, adminDeleteRule } from '../../api'
import Dropdown from '../../components/Dropdown'
import { useAdminFetch } from './useAdminFetch'

// RulesCard 黑白名单:黑名单账号的流量被完全丢弃(不统计、不显示在线);
// 白名单非空时只处理白名单内账号,黑名单优先。账号格式与线上一致,如 UID:10001。
// 规则列表与新增表单状态都是本卡片自持。
export default function RulesCard({ onUnauthed }) {
  // adminRules 返回 {rules:[...]},这里只取数组。
  const fetcher = useCallback(() => adminRules().then((d) => d.rules || []), [])
  const { data: rules, error, refresh } = useAdminFetch(fetcher, onUnauthed)
  const [rAccount, setRAccount] = useState('')
  const [rMode, setRMode] = useState('black')
  const [rNote, setRNote] = useState('')
  // 新增/删除共用一条错误提示:两者互斥(不会同时发生),无需分开存。
  const [ruleErr, setRuleErr] = useState('')

  const addRule = async (e) => {
    e.preventDefault()
    const account = rAccount.trim()
    if (!account) return
    try {
      await adminSetRule(account, rMode, rNote.trim())
      setRAccount('')
      setRNote('')
      refresh()
    } catch (err) {
      setRuleErr(err.message || '添加失败')
    }
  }

  const removeRule = async (account) => {
    try {
      await adminDeleteRule(account)
      refresh()
    } catch (err) {
      setRuleErr(err.message || '删除失败')
    }
  }

  return (
    <div className="admin-card admin-rules">
      <h3>黑白名单</h3>
      <p className="admin-hint">
        黑名单账号的流量被完全丢弃(不统计、不显示在线);白名单非空时只处理白名单内账号,
        黑名单优先。账号格式与线上一致,如 <code>UID:10001</code>。
      </p>
      <form onSubmit={addRule} className="admin-rule-form">
        <input
          className="input" type="text" placeholder="账号(如 UID:10001)" value={rAccount}
          onChange={(e) => setRAccount(e.target.value)}
        />
        <Dropdown
          value={rMode}
          options={[
            { value: 'black', label: '黑名单' },
            { value: 'white', label: '白名单' },
          ]}
          onChange={(v) => setRMode(v)}
        />
        <input
          className="input" type="text" placeholder="备注(可选)" value={rNote}
          onChange={(e) => setRNote(e.target.value)}
        />
        <button className="btn primary" type="submit" disabled={!rAccount.trim()}>添加</button>
      </form>
      {error && <p className="admin-error">{error.message}</p>}
      {ruleErr && <p className="admin-error">{ruleErr}</p>}
      {rules === null
        ? <p className="admin-hint">加载中…</p>
        : rules.length === 0
          ? <p className="admin-hint">暂无规则(当前所有账号均被处理)。</p>
          : (
            <ul className="admin-rule-list">
              {rules.map((r) => (
                <li key={r.account}>
                  <span className={'rule-badge ' + r.mode}>{r.mode === 'black' ? '黑' : '白'}</span>
                  <code className="rule-account">{r.account}</code>
                  {r.note && <span className="rule-note">{r.note}</span>}
                  <button className="btn ghost" onClick={() => removeRule(r.account)}>删除</button>
                </li>
              ))}
            </ul>
          )}
    </div>
  )
}
