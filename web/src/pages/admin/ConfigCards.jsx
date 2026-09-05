import React, { useState } from 'react'
import { splitAddr, joinAddr, validatePort } from '../../utils/netaddr'

// 运行配置的两半:**发件邮箱**(普通设置)与**令牌 + SOCKS5 代理**(高级设置)。
//
// 它们改的是同一个 POST 接口的两组字段(见 Admin 的 saveConfig),界面上之所以拆开,
// 是因为**改错的代价不在一个量级**:
//   - 邮箱  配错了只是发不出订阅邮件,随时能改回来
//   - 代理  是手机把游戏流量送进本机的入口,配错(端口被占、白名单漏填)会让正在
//          代理的手机断网 —— 它常是那些流量的唯一通道
// 故按代价分页,不按接口形状堆在一张卡里。
//
// 生效代价也各不相同,界面上分别写清,不笼统一句「保存后重启」:
//   - 邮箱 / 令牌   改完**立即生效**(纯内存热更)
//   - SOCKS5 代理   改完**立即热重启**(它是独立 goroutine,不影响抓包与 Web 服务)
//   - Web 监听地址  **不在此处** —— 改它等于让正在处理你请求的服务器当场消失,
//                   故走 WebAddrCard 那套「试运行 → 确认」,不落进这张卡
//
// 落盘位置是 /etc/rocom.env(systemd 的 EnvironmentFile),由后端写入;前端不关心,
// 只在配置不可写时把后端的说明原样显示出来。

// useConfigForm 两张卡片共用的编辑状态机:草稿(null = 未编辑,显示后端脱敏值)、
// 保存中、成功/失败提示。分开写会在两处各自发明一次「保存失败要不要留着草稿」的答案,
// 这里统一成:**失败留着草稿**(不然白填一遍)、**成功后丢弃**(重新显示后端值)。
function useConfigForm(initial) {
  const [draft, setDraft] = useState(null)
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState('')
  return {
    value: draft ?? initial,
    dirty: draft !== null,
    busy, msg, err,
    edit: (patch) => { setDraft({ ...(draft ?? initial), ...patch }); setMsg(''); setErr('') },
    fail: (m) => { setMsg(''); setErr(m) },
    discard: () => { setDraft(null); setErr('') },
    submit: async (patch, onSave) => {
      setBusy(true); setMsg(''); setErr('')
      try {
        await onSave(patch)
        setDraft(null)
        setMsg('已保存并生效。')
      } catch (e) {
        setErr(e.message || '保存失败')
      } finally {
        setBusy(false)
      }
    },
  }
}

// Readonly 配置文件不可写时的说明:手动跑的进程通常没有 /etc/rocom.env,
// 与其让人改了存不下,不如一开始就说清该去哪儿改。
function Readonly({ path }) {
  return (
    <p className="admin-hint">
      配置文件不可写({path || '/etc/rocom.env'}),此处为只读。通常由 systemd 部署后才会
      生成该文件;手动运行二进制时请在服务器上直接编辑它,或用
      {' '}<code>sudo ./scripts/deploy.sh</code> 部署后再来改。
    </p>
  )
}

// Actions 保存 / 放弃一行 + 提示。按钮与提示的排布两处一致,抽出来免得各写各的。
function Actions({ busy, dirty, msg, err, onSave, onDiscard }) {
  return (
    <>
      <div className="admin-config-actions">
        <button className={'btn' + (busy ? ' is-loading' : '')} type="button" disabled={busy} onClick={onSave}>
          保存并生效
        </button>
        {dirty && <button className="btn" type="button" disabled={busy} onClick={onDiscard}>放弃修改</button>}
      </div>
      {err && <p className="admin-error">{err}</p>}
      {msg && <p className="admin-ok">{msg}</p>}
    </>
  )
}

// MailConfigCard 发件邮箱与 SMTP 授权码(「普通设置」分页)。
// 授权码只给「是否已设置」(后端脱敏),留空 = 不修改。
export function MailConfigCard({ config, error, onSave }) {
  const { value: f, dirty, busy, msg, err, edit, discard, submit } = useConfigForm({
    user: config?.smtpUser ?? '',
    pass: '',                          // 敏感项:留空 = 不修改
  })

  if (error || !config) {
    return (
      <div className="admin-card">
        <h3>发件邮箱</h3>
        {error ? <p className="admin-error">{error}</p> : <p className="admin-hint">加载中…</p>}
      </div>
    )
  }

  return (
    <div className="admin-card">
      <h3>发件邮箱</h3>
      <p className="admin-hint">
        远行商人新货提醒的发件账号。改动写入 <code>{config.path}</code> 并立即生效,无需重启。
      </p>
      {!config.writable ? <Readonly path={config.path} /> : (
        <>
          <label className="admin-field">
            <span>发件邮箱</span>
            <input
              type="text" value={f.user} placeholder="例如 123456@qq.com"
              onChange={(e) => edit({ user: e.target.value })}
            />
          </label>
          <label className="admin-field">
            <span>SMTP 授权码</span>
            <input
              type="password" value={f.pass} autoComplete="new-password"
              placeholder={config.smtpPassSet ? '已设置,留空表示不修改' : '未设置'}
              onChange={(e) => edit({ pass: e.target.value })}
            />
          </label>
          {config.smtpPassSet
            ? <p className="admin-hint">已设置授权码。留空则不改动它。</p>
            : <p className="admin-hint">未设置,订阅提醒不可用(商家数据本身不受影响)。</p>}
          <Actions
            busy={busy} dirty={dirty} msg={msg} err={err}
            onSave={() => submit({ smtpUser: f.user, smtpPass: f.pass }, onSave)}
            onDiscard={discard}
          />
        </>
      )}
    </div>
  )
}

// AdvConfigCard 第三方图鉴令牌 + 内置 SOCKS5 代理(「高级设置」分页)。
// 注意**不含** Web 监听地址 —— 那个改动会把管理员自己断开,走的是另一条
// 「试运行 → 确认」的链路,见 WebAddrCard。
export function AdvConfigCard({ config, error, onSave }) {
  const s5 = config?.socks5 ?? {}
  const addr = splitAddr(s5.addr)
  const { value: f, dirty, busy, msg, err, edit, fail, discard, submit } = useConfigForm({
    eggKey: '',                        // 敏感项:留空 = 不修改
    host: addr.host,
    port: addr.port,
    allow: s5.allow ?? '',
    block: s5.block ?? '',
    maxConns: s5.maxConns ?? 0,
    user: s5.user ?? '',
    pass: '',                          // 敏感项:留空 = 不修改
  })

  // 端口先自己验一遍再交给后端:后端是先落盘再起监听(见 api_admin_config.go),
  // bind 失败时 /etc/rocom.env 里已经是这份坏配置了 —— 服务**下次重启**就起不来,
  // 而那会儿管理员已经连不上面板,只能上服务器手改文件。
  const save = async () => {
    const host = String(f.host).trim()
    const port = String(f.port).trim()
    const bad = validatePort(port)
    if (bad) return fail(bad + '(0 = 由内核随机分配)')
    if (port === '' && host !== '') {
      return fail('已填监听 IP,端口不能留空(不启用请两个都留空)')
    }
    return submit({
      eggKey: f.eggKey,
      socks5: {
        addr: joinAddr(host, port),
        allow: f.allow,
        block: f.block,
        maxConns: Number(f.maxConns) || 0,
        user: f.user,
        pass: f.pass,
      },
    }, onSave)
  }

  if (error || !config) {
    return (
      <div className="admin-card admin-wide">
        <h3>令牌与代理</h3>
        {error ? <p className="admin-error">{error}</p> : <p className="admin-hint">加载中…</p>}
      </div>
    )
  }

  return (
    <div className="admin-card admin-wide">
      <h3>令牌与代理</h3>
      <p className="admin-hint">
        改动会写入 <code>{config.path}</code> 并立即生效:令牌纯热更,代理热重启(不影响抓包)。
        HTTPS 与抓包网卡属启动项,改它们需要编辑该文件后执行
        {' '}<code>systemctl restart rocom</code>;Web 监听地址可在下方「Web 服务」卡片里改。
      </p>

      {!config.writable ? <Readonly path={config.path} /> : (
        <>
          <div className="admin-config-group">
            <h4>第三方图鉴令牌</h4>
            <label className="admin-field">
              <span>API 令牌</span>
              <input
                type="password" value={f.eggKey} autoComplete="new-password"
                placeholder={config.eggKeySet ? '已设置,留空表示不修改' : '未设置'}
                onChange={(e) => edit({ eggKey: e.target.value })}
              />
            </label>
            <p className="admin-hint">
              查「神奇的蛋」可能物种、远行商人货单用。只存服务端、不下发前端,故此处不显示原文。
              {config.eggKeySet ? ' 已设置,留空则不改动。' : ' 未设置,相关查询不可用。'}
            </p>
          </div>

          <div className="admin-config-group">
            <h4>内置 SOCKS5 代理</h4>
            <p className="admin-hint">
              {s5.running
                ? <>当前运行中,实际监听 <code>{s5.realAddr}</code>。改端口会热重启,改其它项不中断连接。</>
                : '当前未启用。填端口即可开启(如 1080);留空 = 不启用。'}
            </p>
            <label className="admin-field">
              <span>监听 IP</span>
              <input
                type="text" value={f.host} placeholder="留空 = 监听所有网卡,如 127.0.0.1"
                onChange={(e) => edit({ host: e.target.value })}
              />
            </label>
            <label className="admin-field">
              <span>端口</span>
              <input
                type="number" min="0" max="65535" value={f.port} placeholder="如 1080;0 = 随机分配"
                onChange={(e) => edit({ port: e.target.value })}
              />
            </label>
            <label className="admin-field">
              <span>客户端白名单</span>
              <input
                type="text" value={f.allow} placeholder="逗号分隔,支持 IP 或 CIDR;留空 = 不限制"
                onChange={(e) => edit({ allow: e.target.value })}
              />
            </label>
            <label className="admin-field">
              <span>屏蔽域名</span>
              <input
                type="text" value={f.block} placeholder="逗号分隔;留空 = 不屏蔽"
                onChange={(e) => edit({ block: e.target.value })}
              />
            </label>
            <label className="admin-field">
              <span>并发上限</span>
              <input
                type="number" min="0" value={f.maxConns}
                onChange={(e) => edit({ maxConns: e.target.value })}
              />
            </label>
            <label className="admin-field">
              <span>认证用户名</span>
              <input
                type="text" value={f.user} placeholder="留空 = 无认证"
                onChange={(e) => edit({ user: e.target.value })}
              />
            </label>
            <label className="admin-field">
              <span>认证密码</span>
              <input
                type="password" value={f.pass} autoComplete="new-password"
                placeholder={s5.passSet ? '已设置,留空表示不修改' : '未设置'}
                onChange={(e) => edit({ pass: e.target.value })}
              />
            </label>
            <p className="admin-hint">
              ⚠ 公网暴露时务必填白名单:RFC 1929 的密码是明文传输的,挡得住扫描器,
              挡不住任何能碰到这段流量的人。
            </p>
          </div>

          <Actions busy={busy} dirty={dirty} msg={msg} err={err} onSave={save} onDiscard={discard} />
        </>
      )}
    </div>
  )
}
