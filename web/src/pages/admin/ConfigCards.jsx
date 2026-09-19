import React, { useEffect, useState } from 'react'
import { splitAddr, joinAddr, validatePort } from '../../utils/netaddr'
import { adminHy2Link } from '../../api'
import { copyText } from '../../utils/clipboard'

// 运行配置的两半:**发件邮箱**(普通设置)与**令牌 + hysteria2 代理**(高级设置)。
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
//   - hysteria2 代理 改完**立即生效**(它是独立 goroutine;改密码/白名单连重启都不用,
//                    只有改端口才热重启,且不影响抓包与 Web 服务)
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

// AdvConfigCard 内嵌 hysteria2 代理(「高级设置」分页)。
// 注意**不含** Web 监听地址 —— 那个改动会把管理员自己断开,走的是另一条
// 「试运行 → 确认」的链路,见 WebAddrCard。
export function AdvConfigCard({ config, error, onSave }) {
  const hy = config?.hy2 ?? {}
  const addr = splitAddr(hy.addr)
  const { value: f, dirty, busy, msg, err, edit, fail, discard, submit } = useConfigForm({
    host: addr.host,
    port: addr.port,
    allow: hy.allow ?? '',
    block: hy.block ?? '',
    maxConns: hy.maxConns ?? 0,
    advertise: hy.advertise ?? '',
    pass: '',                          // 敏感项:留空 = 不修改
  })

  // 导入链接由服务端拼:密码、实际端口、证书能不能验,三样都只在服务端齐。
  // 主机这一项它猜不到,所以先取浏览器地址栏(从哪打开就连哪),再让「对外地址」压过它。
  const adv = splitAddr(hy.advertise)
  const [hostBox, setHostBox] = useState('')   // 编辑中
  const [applied, setApplied] = useState('')   // 已提交(失焦/回车)—— 避免每敲一个字符打一次接口
  const effHost = applied.trim() || adv.host || globalThis.location?.hostname || ''
  const [link, setLink] = useState(null)
  const [linkErr, setLinkErr] = useState('')
  const [copiedSub, setCopiedSub] = useState('')   // 订阅地址的复制结果

  // deps 用 config 本身:面板保存成功后 Admin 会重新拉配置(新对象),这里就跟着重算。
  // 「改了端口链接也同步」靠的是这个,而不是让人记得手动点一次。
  useEffect(() => {
    if (!config?.hy2?.running || !effHost) {
      setLink(null); setLinkErr(''); return
    }
    let dead = false
    adminHy2Link(effHost)
      .then((d) => { if (!dead) { setLink(d); setLinkErr('') } })
      .catch((e) => { if (!dead) { setLink(null); setLinkErr(e.message || '生成失败') } })
    return () => { dead = true }
  }, [config, effHost])

  const copySub = async () => {
    if (!link?.sub) return
    setCopiedSub((await copyText(link.sub)) ? '已复制' : '复制失败,请手动选中')
  }

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
      hy2: {
        addr: joinAddr(host, port),
        allow: f.allow,
        block: f.block,
        maxConns: Number(f.maxConns) || 0,
        advertise: String(f.advertise).trim(),
        pass: f.pass,
      },
    }, onSave)
  }

  if (error || !config) {
    return (
      <div className="admin-card admin-wide">
        <h3>代理</h3>
        {error ? <p className="admin-error">{error}</p> : <p className="admin-hint">加载中…</p>}
      </div>
    )
  }

  return (
    <div className="admin-card admin-wide">
      <h3>代理</h3>
      <p className="admin-hint">
        改动会写入 <code>{config.path}</code> 并立即生效:密码/白名单等直接换参数,
        改端口才热重启(都不影响抓包)。带宽与 HTTPS、抓包网卡一样属**启动项**,
        改它们需要编辑该文件后执行{' '}<code>systemctl restart rocom-go</code>;
        Web 监听地址可在下方「Web 服务」卡片里改。
      </p>

      {!config.writable ? <Readonly path={config.path} /> : (
        <>
          <div className="admin-config-group">
            <h4>内嵌 hysteria2 代理(UDP)</h4>
            <p className="admin-hint">
              {hy.running
                ? <>当前运行中,实际监听 <code>{hy.realAddr}</code>。改端口会热重启,改其它项不中断连接。</>
                : '当前未启用。填端口即可开启(如 11443);留空 = 不启用。'}
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
                type="number" min="0" max="65535" value={f.port} placeholder="如 11443;0 = 随机分配"
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
              <span>认证密码</span>
              <input
                type="password" value={f.pass} autoComplete="new-password"
                placeholder={hy.passSet ? '已设置,留空表示不修改' : '未设置'}
                onChange={(e) => edit({ pass: e.target.value })}
              />
            </label>
            <label className="admin-field">
              <span>对外地址</span>
              <input
                type="text" value={f.advertise}
                placeholder="host 或 host:端口;只填 :端口 表示仅改端口;留空 = 不固定"
                onChange={(e) => edit({ advertise: e.target.value })}
              />
            </label>
            <p className="admin-hint">
              ⚠ 公网暴露时务必填白名单。hy2 的密码在隧道内部传输(不像 SOCKS5 那样明文),
              但这只挡得住「连进来」,挡不住扫描器把 UDP 端口打满或拿它当跳板 ——
              白名单才是把攻击面缩到手机出口 IP 的那道防线。
              另:手机端须用支持 hysteria2 的客户端(Clash Meta / 小火箭 / sing-box),
              防火墙与云安全组放行的是 <b>UDP</b> 而非 TCP。
            </p>
          </div>

          <Hy2LinkCard
            running={!!hy.running}
            using={effHost}
            hostBox={hostBox}
            onHost={(e) => setHostBox(e.target.value)}
            onCommit={() => setApplied(hostBox)}
            link={link}
            error={linkErr}
            copiedSub={copiedSub}
            onCopySub={copySub}
          />

          <Actions busy={busy} dirty={dirty} msg={msg} err={err} onSave={save} onDiscard={discard} />
        </>
      )}
    </div>
  )
}

// Hy2LinkCard 手机导入用的订阅地址(整份配置,不只是节点)。
//
// 为什么值得单独做:配置里三样东西容易填错 —— 密码(要不要转义)、端口
// (监听值与被 NAT 映射后的对外值可能不是一个)、要不要跳过证书校验。
// 三样分别散落在配置文件、运行中的监听地址与证书 SAN 里,让人照文档手拼迟早拼错,
// 而拼错的表象是「手机连不上」,看不出是哪儿错。故一律由服务端算好,这里只展示。
//
// 也只给订阅、不再给「只含节点」的单条链接:抓包成立的前提是游戏那一条 TCP 流量被
// 送进代理,而那取决于客户端的**分流规则** —— 规则塞不进一条 node 链接里。只导入 node
// 的人会得到「手机连上了、游戏也能玩、面板上一条数据都没有」,且几乎无法自查。
// 留两条并列,被选错的那条所坑的正是这一点,故只留不会错的那一条。
//
// 它只读、不参与保存流程,所以刻意不混进上面那套 useConfigForm 的编辑状态机。
function Hy2LinkCard({ running, using, hostBox, onHost, onCommit, link, error, copiedSub, onCopySub }) {
  const placeholder = using || '例如 2002666.xyz'
  if (!running) {
    return (
      <div className="admin-config-group">
        <h4>导入配置</h4>
        <p className="admin-hint">代理未运行,没有可导入的配置。填好端口启用后这里会自动出现。</p>
      </div>
    )
  }
  return (
    <div className="admin-config-group">
      <h4>导入配置</h4>
      <p className="admin-hint">
        手机 / 电脑上的 Clash Meta、FlClash、Mihomo、小火箭直接导入即可。端口取的是
        <b>实际监听值</b>,在上面改了端口保存后,地址不变、内容自动跟着变。
      </p>
      <label className="admin-field">
        <span>手机要打的地址</span>
        <input
          type="text" value={hostBox} placeholder={placeholder + '(默认:当前面板地址)'}
          onChange={onHost} onBlur={onCommit}
          onKeyDown={(e) => { if (e.key === 'Enter') onCommit() }}
        />
      </label>
      {error && <p className="admin-error">{error}</p>}
      {link && (link.sub ? (
        <>
          <label className="admin-field">
            <span>订阅地址</span>
            <input
              className="admin-link" type="text" readOnly value={link.sub}
              onFocus={(e) => e.target.select()}
            />
          </label>
          <div className="admin-config-actions">
            <button className="btn" type="button" onClick={onCopySub}>复制订阅地址</button>
          </div>
          <p className="admin-hint">
            Clash Meta / FlClash / Mihomo:配置 → 从 URL 导入;小火箭:配置 → 添加配置。
          </p>
          <p className="admin-hint">
            ⚠ 导入后<b>必须切换启用新配置</b> —— 这类客户端能同时存好几份,导入不等于启用;
            旧的(没有分流规则的那份)要停用,否则互相盖。
          </p>
          <p className="admin-hint">
            当前指向 <code>{link.host}:{link.port}</code>
            {link.secure
              ? ' —— 主机在证书里,客户端可校验证书'
              : ' —— 证书里没有这个主机名,客户端会跳过校验(按 IP 连时属正常)'}
            {copiedSub && <> · {copiedSub}</>}
          </p>
          <p className="admin-hint">
            ⚠ 地址里带着明文密码,别截图、别贴到群里。换密码后旧地址立即失效,需要重新复制一条。
          </p>
        </>
      ) : (
        <p className="admin-hint">
          先在下面填一个代理密码并保存,才会生成订阅地址 —— 没有密码就没有可下发的节点。
        </p>
      ))}
    </div>
  )
}
