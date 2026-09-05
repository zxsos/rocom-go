// 监听地址("IP:端口")的拆分与拼合 —— Web 服务与 SOCKS5 代理两处共用。
//
// 后端存的是一个 Go 形态的 listen 地址("host:port"),但那个冒号对使用者毫无意义,
// 还常被误读成「要连过去的地址」。故界面上一律拆成两格,**端口只填数字**:
//   IP    留空 = 监听所有网卡;填 127.0.0.1 则只有本机连得上。
//   端口  纯数字(0 = 由内核随机分配,实际端口看界面上回显的「当前监听」);留空 = 不启用。

// splitAddr 拆出 IP 与端口。
//
// 反解要认 IPv6 的方括号:"[::1]:1080" 里的冒号不止一个,取最后一个会切错。
export function splitAddr(addr) {
  const s = String(addr ?? '').trim()
  if (s === '') return { host: '', port: '' }
  if (s.startsWith('[')) {
    const end = s.indexOf(']')
    if (end >= 0) return { host: s.slice(0, end + 1), port: s.slice(end + 2) }
  }
  const i = s.lastIndexOf(':')
  if (i < 0) return { host: s, port: '' }
  return { host: s.slice(0, i), port: s.slice(i + 1) }
}

// joinAddr 拼回后端要的形态。没填端口即「不启用」(空串),此时填了 IP 也一并丢掉 ——
// 只有 IP 没有端口不是个合法的监听地址(表单已拦下,这里是第二道)。
export function joinAddr(host, port) {
  const p = String(port ?? '').trim()
  if (p === '') return ''
  const h = String(host ?? '').trim()
  if (h === '') return ':' + p
  // 裸 IPv6(如 ::1)必须加方括号,否则与端口的冒号混在一起无法解析
  return (h.includes(':') && !h.startsWith('[') ? '[' + h + ']' : h) + ':' + p
}

// validatePort 校验端口文本:空串合法(表示不启用),否则须为 0~65535 的纯数字。
// 错误信息直接面向用户,故返回文案而非布尔。
export function validatePort(port) {
  const p = String(port ?? '').trim()
  if (p === '') return ''
  if (!/^\d+$/.test(p)) return '端口须填 0~65535 的数字'
  const n = Number(p)
  if (n > 65535) return '端口须填 0~65535 的数字'
  return ''
}
