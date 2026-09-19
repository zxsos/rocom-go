import React, { useEffect, useMemo, useState } from 'react'
import { useParams } from 'react-router-dom'
import { subIntro } from '../api'
import { copyText } from '../utils/clipboard'
import QrCode from '../components/QrCode'
import { deepLink, sortForPlatform } from '../utils/sublinks'

// SubImport 引导页:朋友点开短链后看到的**唯一**一页。
//
// 为什么必须有它,而不是让短链直接弹进客户端:
// 一键深链(scheme)只在「客户端已安装」时成立。没装的话,iOS 弹一个「无法打开网页」、
// Android 多半毫无反应 —— 朋友不会知道自己缺的是客户端,只会认为这条链接是坏的,
// 然后就没有然后了。引导页把「点开 → 装/打开客户端 → 导入」这三步摊开摆在他面前,
// 并且在深链没反应时立刻给出人肉路径(复制链接)。
//
// 页面上明写三个坑,是因为它们都是「连上了却一条数据都没有」的成因,而那种故障
// 从朋友那侧完全无法自查(游戏照玩、网照上,只有你这边的面板是空的)。
export default function SubImport() {
  const { code } = useParams()
  const [info, setInfo] = useState(null) // null=加载中
  const [msg, setMsg] = useState('')
  const [stalled, setStalled] = useState(false)

  // 订阅地址:与当前页面同源同径。刻意不落库也不由后端下发 ——
  // 朋友从哪个地址打开这条链接,就该用哪个地址去拉配置(内网打开给内网、公网给公网)。
  const subUrl = useMemo(
    () => (code ? `${window.location.origin}/i/${encodeURIComponent(code)}` : ''),
    [code]
  )

  useEffect(() => {
    if (!code) return
    subIntro(code).then(setInfo).catch(() => setInfo({ ok: false }))
  }, [code])

  const clients = useMemo(() => sortForPlatform(), [])

  // 点了一键导入却还留在本页 → 客户端没装(或没认这个 scheme),马上改口让人复制。
  function openDeep(kind) {
    const url = deepLink(kind, subUrl)
    setStalled(false)
    window.location.href = url
    setTimeout(() => {
      if (!document.hidden) setStalled(true)
    }, 1500)
  }

  async function copy(url) {
    const ok = await copyText(url)
    setMsg(ok ? '已复制,去客户端里粘贴即可' : '复制失败:请长按上面的地址手动复制')
  }

  if (info === null) {
    return (
      <div className="sub-import">
        <div className="sub-import-card">
          <p className="sub-import-muted">正在确认这条链接…</p>
        </div>
      </div>
    )
  }

  if (!info.ok) {
    return (
      <div className="sub-import">
        <div className="sub-import-card">
          <h1 className="sub-import-title">这条链接用不了了</h1>
          <p className="sub-import-lead">
            它可能已经被发链接的人吊销或重新生成。找他要一条新的吧 —— 旧的就算能打开,
            服务端也不会再下发配置了。
          </p>
        </div>
      </div>
    )
  }

  const dead = info.revoked || info.expired

  return (
    <div className="sub-import">
      <div className="sub-import-card">
        <div className="sub-import-head">
          <h1 className="sub-import-title">
            {info.label ? `${info.label} 给你的节点` : '一条抓包节点'}
          </h1>
          <span className={`sub-import-badge ${dead ? 'bad' : 'good'}`}>
            {dead ? (info.revoked ? '已吊销' : '已过期') : '可用'}
          </span>
        </div>
        <p className="sub-import-lead">
          连上它,你手机上的游戏流量才会经过那台机器,宠物资讯才抓得到。
          只连上、不配置分流规则的话,游戏照玩,但对面一条数据都收不到。
        </p>

        {dead ? (
          <p className="sub-import-warn">
            这条链接已经{info.revoked ? '被吊销' : '过期'}了,下面是拿不到配置的。找发链接的人要一条新的。
          </p>
        ) : (
          <>
            <div className="sub-import-qr">
              <QrCode value={subUrl} size={210} />
              <p className="sub-import-muted">
                用另一台设备扫这个码,或把下面的链接发过去
              </p>
            </div>

            <div className="sub-import-url">
              <input
                type="text" readOnly value={subUrl}
                onFocus={(e) => e.target.select()}
              />
              <button className="btn primary" type="button" onClick={() => copy(subUrl)}>
                复制链接
              </button>
            </div>
            {msg && <p className="sub-import-muted">{msg}</p>}

            <h2 className="sub-import-h2">装了客户端?直接点</h2>
            <div className="sub-import-actions">
              {clients.map((c) => (
                <button
                  key={c.name} className="btn sub-import-client" type="button"
                  onClick={() => openDeep(c.deep)}
                >
                  <b>{c.name}</b>
                  <span>{c.verified ? '点这里一键导入' : '点这里试试(未实测)'}</span>
                </button>
              ))}
            </div>
            {stalled && (
              <p className="sub-import-warn">
                没跳转?说明你手机上还没装那个客户端 —— 先装一个(下面有去哪儿装),
                或者点上面的「复制链接」,进客户端里手动添加订阅。
              </p>
            )}

            <details className="sub-import-more">
              <summary>还没装客户端?去哪儿装</summary>
              <ul>
                {clients.map((c) => (
                  <li key={c.name}>
                    <b>{c.name}</b>:{c.where}
                  </li>
                ))}
              </ul>
              <p className="sub-import-muted">
                装好后回到这一页点对应按钮,或手动添加:客户端里找「配置 → 从 URL 导入 /
                添加订阅」,把上面那条链接粘进去。
              </p>
            </details>

            <h2 className="sub-import-h2">导入后请确认这三件事</h2>
            <ol className="sub-import-checks">
              <li>在客户端里<b>切换启用</b>刚导入的那份配置 —— 这类客户端能同时存好几份,导入不等于启用。</li>
              <li>找到「抓包通道」这个策略组,确认它选中的是 <b>ROCOM hy2</b>,不是 DIRECT。选 DIRECT 等于白连。</li>
              <li>
                进游戏<b>打开一次宠物仓库</b> —— 宠物列表是在那一步下发的,不打开的话对面收不到数据。
                (服务端认的是 {info.gamePort || 8195} 端口的游戏流量,已随配置下发。)
              </li>
            </ol>
          </>
        )}
      </div>
    </div>
  )
}
