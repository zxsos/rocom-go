// 一键导入深链:把订阅地址包进各客户端注册的 URL scheme,让「点一下」代替
// 「复制 → 打开客户端 → 找到添加订阅 → 粘贴 → 保存」这一串。
//
// 写法都取自各家的公开说明,不是猜的:
//   - Clash 系(Clash Meta / Mihomo / Verge / FlClash / Stash):
//     `clash://install-config?url=<URI 编码后 url>` —— Clash Verge Rev 官方文档
//   - Hiddify:`hiddify://import/<订阅链接>#<名称>` —— Hiddify 官方 Wiki,且明确写了
//     订阅链接可以是 clash link,所以这里我们复用同一份 Clash YAML,不必再出一份 sing-box
//   - 小火箭:`shadowrocket://config/add/<url>` —— 社区手册(非官方文档)
//
// ⚠ 小火箭那条**尚未实测**:手册是社区维护的,且 iOS 上 scheme 失败时只会弹一个
// 「无法打开网页」—— 朋友看不懂,会以为链接坏了。故引导页必须同时给「复制链接」
// 这条人肉路径兜底(那里也写了怎么手动导入)。
//
// sing-box://import-remote-profile 暂未启用:仓库里记过两次「导入了却抓不到游戏流量」,
// 疑点在客户端侧而非格式,故等实测结论再开(见 docs/deploy.md)。

// deepLink 生成一键导入链接。kind 取 'clash' | 'hiddify' | 'shadowrocket'。
export function deepLink(kind, subUrl, name = 'ROCOM') {
  if (!subUrl) return ''
  switch (kind) {
    case 'clash':
      return 'clash://install-config?url=' + encodeURIComponent(subUrl)
    case 'hiddify':
      // 官方示例里订阅链接是**原样**拼在 import/ 后面的(不是 query 参数),照它写。
      return 'hiddify://import/' + subUrl + '#' + encodeURIComponent(name)
    case 'shadowrocket':
      return 'shadowrocket://config/add/' + subUrl
    default:
      return ''
  }
}

// CLIENTS 引导页上的按钮:按平台排序用。
// platform 用来在手机上把自己人排前面(iOS 首推小火箭,Android 首推 Clash Meta);
// 桌面端两个都能装,故都算 'any'。
export const CLIENTS = [
  {
    kind: 'shadowrocket',
    platform: 'ios',
    name: 'Shadowrocket(小火箭)',
    deep: 'shadowrocket',
    verified: false, // 待实测(见文件头说明)
    where: 'App Store 付费购买,iOS 专属',
  },
  {
    kind: 'clash',
    platform: 'android',
    name: 'Clash Meta / Mihomo',
    deep: 'clash',
    verified: true,
    where: 'Android:Clash Meta for Android / Mihomo;GitHub 免费下载',
  },
  {
    kind: 'clash',
    platform: 'android',
    name: 'FlClash',
    deep: 'clash',
    verified: true,
    where: 'Android / Windows / macOS;GitHub 免费下载',
  },
  {
    kind: 'clash',
    platform: 'desktop',
    name: 'Clash Verge Rev',
    deep: 'clash',
    verified: true,
    where: 'Windows / macOS / Linux;GitHub 免费下载',
  },
  {
    kind: 'hiddify',
    platform: 'any',
    name: 'Hiddify',
    deep: 'hiddify',
    verified: false, // 能导入,但「导入后是否采纳下发的分流规则」尚待实测
    where: 'iOS / Android / Windows / macOS;全平台免费',
  },
]

// guessPlatform 从 UA 猜平台,只用来给上面的按钮排序 —— 猜错了不影响可用性。
export function guessPlatform(ua = navigator.userAgent) {
  const s = String(ua || '')
  if (/iPhone|iPad|iPod|Macintosh/i.test(s)) return 'ios'
  if (/Android/i.test(s)) return 'android'
  return 'desktop'
}

// sortForPlatform 把当前平台最可能装得上的客户端排到前面。
export function sortForPlatform(platform = guessPlatform()) {
  const rank = (c) => {
    if (c.platform === platform) return 0
    if (c.platform === 'any') return 1
    return 2
  }
  return [...CLIENTS].sort((a, b) => rank(a) - rank(b))
}
