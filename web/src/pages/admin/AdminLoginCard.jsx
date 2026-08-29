import React from 'react'

// AdminLoginCard 登录/初始化密码卡片:首次进入引导设密码,之后凭密码登录。
// 纯展示,密码等状态留在 Admin(登出时统一清理)。
export default function AdminLoginCard({
  configured, password, confirmPw, error,
  onPasswordChange, onConfirmChange, onSubmit,
}) {
  return (
    <div className="admin-page admin-login">
      <div className="admin-card">
        <h2>{configured ? '管理员登录' : '设置管理员密码'}</h2>
        <p className="admin-hint">
          {configured
            ? '输入管理员密码进入管理面板'
            : '首次进入,请设置管理员密码(至少 4 位)'}
        </p>
        <form onSubmit={onSubmit}>
          <input
            className="input" type="password" placeholder="密码" value={password}
            onChange={(e) => onPasswordChange(e.target.value)} autoFocus
          />
          {!configured && (
            <input
              className="input" type="password" placeholder="确认密码" value={confirmPw}
              onChange={(e) => onConfirmChange(e.target.value)}
            />
          )}
          {error && <p className="admin-error">{error}</p>}
          <button className="btn primary" type="submit" disabled={!password}>
            {configured ? '登录' : '设置并进入'}
          </button>
        </form>
      </div>
    </div>
  )
}
