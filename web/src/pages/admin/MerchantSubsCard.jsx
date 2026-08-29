import React, { useCallback, useState } from 'react'
import { adminMerchantSubs, adminMerchantSubDelete, adminTestMail, getMerchant } from '../../api'
import { confirmDialog } from '../../components/confirm'
import { fmtShortTime } from '../../utils/format'
import { useAdminFetch } from './useAdminFetch'

// MerchantSubsCard 邮箱推送名单 + 发信配置自测。
// 玩家在「远行商人」页登记新货邮件提醒后,名单出现在这里;每轮(8/12/16/20 点)新增商品
// 上架时后端自动发信,0~8 点打烊不提醒。发件邮箱与授权码由 -merchant-smtp-user / -pass 配置。
// 名单、测试邮件表单、强制刷新按钮的状态都是本卡片自持。
export default function MerchantSubsCard({ onUnauthed }) {
  const { data: merchantSubs, error, refresh } = useAdminFetch(useCallback(() => adminMerchantSubs(), []), onUnauthed)
  const [subErr, setSubErr] = useState('')
  const [subMsg, setSubMsg] = useState('')
  const [testEmail, setTestEmail] = useState('')
  const [testSubject, setTestSubject] = useState('')
  const [testBody, setTestBody] = useState('')
  const [testBusy, setTestBusy] = useState(false)
  const [forceBusy, setForceBusy] = useState(false) // 强制刷新商人数据(回源第三方)

  // 发送测试邮件:验证 SMTP 配置(发件邮箱/授权码)是否可用;主题/正文可自填,留空用默认。
  const sendTestMail = async (e) => {
    e.preventDefault()
    setSubErr(''); setSubMsg('')
    const email = testEmail.trim()
    if (!email) return
    setTestBusy(true)
    try {
      await adminTestMail(email, testSubject.trim(), testBody.trim())
      setSubMsg('测试邮件已发送到 ' + email + ',请检查收件箱(含垃圾箱)。')
    } catch (err) {
      setSubErr(err.message || '发送失败')
    } finally {
      setTestBusy(false)
    }
  }

  // 强制刷新商人数据:绕过后端缓存,强制后端重新向第三方抓取当前轮(烧 token,仅维护用)。
  const forceMerchant = async () => {
    setSubErr(''); setSubMsg('')
    setForceBusy(true)
    try {
      const d = await getMerchant(true)
      setSubMsg('已强制刷新商人数据(' + (d.status === 'open' ? '当前营业中' : '当前打烊') + '),商人页下次打开即为最新。')
    } catch (err) {
      setSubErr(err.message || '强制刷新失败')
    } finally {
      setForceBusy(false)
    }
  }

  // 从推送名单删除订阅(不再向其发提醒)。
  const removeSub = async (email) => {
    setSubErr(''); setSubMsg('')
    if (!await confirmDialog({
      message: '确认删除 ' + email + ' 的订阅?删除后不再向其发送新货提醒。',
      okText: '删除', danger: true,
    })) return
    try {
      await adminMerchantSubDelete(email)
      setSubMsg('已删除订阅:' + email)
      refresh()
    } catch (err) {
      setSubErr(err.message || '删除失败')
    }
  }

  return (
    <div className="admin-card admin-rules admin-wide">
      <h3>邮箱推送名单(远行商人订阅)</h3>
      <p className="admin-hint">
        玩家在「远行商人」页登记新货邮件提醒后,名单出现在这里。每轮(8/12/16/20 点)新增商品
        上架时,后端自动向名单内邮箱发提醒;0~8 点打烊不提醒。发件邮箱与授权码由
        <code> -merchant-smtp-user / -merchant-smtp-pass </code>配置。
        {merchantSubs && !merchantSubs.configured && (
          <span style={{ color: 'var(--danger, #e5534b)' }}> ⚠ 服务端未配置 SMTP,发送会失败。</span>
        )}
      </p>
      {/* 测试邮件:验证发件配置,主题/正文可自填 */}
      <form onSubmit={sendTestMail} className="admin-test-form">
        <input
          className="input" type="email" placeholder="测试收件邮箱(如 123@qq.com)" value={testEmail}
          onChange={(e) => setTestEmail(e.target.value)}
        />
        <input
          className="input" placeholder="主题(留空用默认)" value={testSubject}
          onChange={(e) => setTestSubject(e.target.value)}
        />
        <textarea
          className="input" rows={3} placeholder="邮件内容(留空用默认)" value={testBody}
          onChange={(e) => setTestBody(e.target.value)}
        />
        <button className="btn primary" type="submit" disabled={!testEmail.trim() || testBusy}>
          {testBusy ? '发送中…' : '发送测试邮件'}
        </button>
      </form>
      {/* 强制刷新:回源第三方重抓商人数据(烧对方额度,仅维护用) */}
      <div className="admin-play-toolbar">
        <button className="btn" type="button" onClick={forceMerchant} disabled={forceBusy}
          title="绕过后端缓存,强制后端重新向第三方抓取当前轮商人数据(烧对方额度,非必要别点)">
          {forceBusy ? '强制刷新中…' : '强制刷新商人数据'}
        </button>
        <span className="admin-hint">绕过后端缓存,强制后端重新向第三方抓取当前轮商人数据(烧对方额度,非必要别点)。</span>
      </div>
      {error && <p className="admin-error">{error.message}</p>}
      {subErr && <p className="admin-error">{subErr}</p>}
      {subMsg && <p className="admin-hint" style={{ color: 'var(--green, #4caf50)' }}>{subMsg}</p>}
      {merchantSubs === null
        ? <p className="admin-hint">加载中…</p>
        : merchantSubs.subs.length === 0
          ? <p className="admin-hint">暂无订阅(玩家在远行商人页登记后显示)。</p>
          : (
            <table className="admin-play-table">
              <thead>
                <tr>
                  <th>账号</th>
                  <th>邮箱</th>
                  <th>关键词</th>
                  <th>订阅时间</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {merchantSubs.subs.map((s) => (
                  <tr key={s.account}>
                    <td>{s.account}</td>
                    <td>{s.email}</td>
                    <td>{s.keywords || <span className="muted">全部</span>}</td>
                    <td>{fmtShortTime(s.created_at)}</td>
                    <td>
                      <button className="btn ghost" onClick={() => removeSub(s.email)}>删除</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
    </div>
  )
}
