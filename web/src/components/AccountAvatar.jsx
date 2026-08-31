import React, { useMemo, useState } from 'react'
import { IconLock } from './svg'

// 头像色相:8 个精选值,由账号 ID 哈希选中。
// 不用 0~360 全量色相 —— 中间地带会撞出浑浊、低对比的色(暗色主题下压不住底、
// 亮色主题下发灰),精选的这 8 个在两个主题下都够饱和、彼此也分得开。
const HUES = [210, 265, 340, 22, 45, 160, 190, 95]

// hueOf 由账号 ID 派生色相:31 进制字符串哈希取模,与渲染次数、列表顺序、账号增删
// 全都无关 —— 同一个账号永远同一个颜色,靠颜色认人这件事才成立。
const hueOf = (account) => {
  let h = 0
  for (let i = 0; i < account.length; i++) h = (h * 31 + account.charCodeAt(i)) >>> 0
  return HUES[h % HUES.length]
}

// initialOf 取昵称首字。用 Array.from 而不是 name[0]:后者按 UTF-16 码元切,
// emoji 等代理对字符会被切成半个,渲染出来是乱码。
const initialOf = (name) => {
  const ch = Array.from(name || '')[0] || '?'
  return ch.length === 1 && ch >= 'a' && ch <= 'z' ? ch.toUpperCase() : ch
}

// 头像尺寸位:**只有微信头像域名** thirdwx.qlogo.cn 的直链末段才是尺寸
// (0/46/64/96/132,见 docs/api/schemas.md)。原样用 132 在触发条上是 5 倍过采样 ——
// 最大的一处是手机 sheet 的 36px,2x 屏也只要 72px,故降到 96。
//
// ⚠️ 必须**按域名限定**,不能只判「末段是纯数字」:实测登录回包里还有另一类地址
// photo-prod.nrc.qq.com/<uid>/card/<一串数字>,末段那个是**图片 ID 而不是尺寸** ——
// 只判纯数字会把它改成 /96,请求直接 404,头像全变破图(原图 200 正常返回)。
// 故这里白名单式收紧:认不出域名就原样返回,宁可多下几 KB 也不能把头像搞没。
const AVATAR_SIZE = 96
const sized = (url) => {
  let host = ''
  try {
    host = new URL(url).hostname
  } catch {
    return url // 解析不了就别动,让 <img> 自己去失败
  }
  if (host !== 'thirdwx.qlogo.cn') return url
  return /^https:\/\/\S+\/\d+$/.test(url) ? url.replace(/\/\d+$/, '/' + AVATAR_SIZE) : url
}

// 左下角 PIN 锁:与在线小点(右下)分列两侧,各占一角,不会互相遮挡。
// 用页面底色描边把它从徽章上「抠」出来,否则深色徽章上深色的锁直接看不见。
// 锁**不带 .privacy**:它不敏感(只说明这个账号设了 PIN),且是扫视时要确认的东西。
const PinMark = () => (
  <i className="acct-avatar-pin" title="已设 PIN 保护">
    <IconLock size={9} />
  </i>
)

// AccountAvatar 账号头像「训练家徽章」:取到平台头像时盖在盘上,取不到则露出由账号 ID
// 派生色相的搪瓷渐变盘 + 昵称首字;右下角叠在线/离线小点,左下角叠 PIN 锁。
//
// 尺寸由父级继承下来的 --acct-avatar 控制(触发条 26px、桌面行 30px、手机 sheet 36px),
// 不通过 props 传 —— 同一枚徽章在三种容器里只是大小不同,交给 CSS 统一调。
//
// 首字层与头像层都挂 .privacy 参与全局截图防泄(见 styles/shell.css 的 html[data-privacy]):
// 首字和昵称一样是可识别信息,真人头像更是看图认人,糊掉才能防旁窥。
// 在线小点与 PIN 锁**刻意放在 .privacy 之外**:它们不敏感,且正是扫视时最先要确认的东西 ——
// 糊掉会逼着用户去点顶栏品牌名解除遮罩才能看在线状态,得不偿失。
export default function AccountAvatar({ account, name, online, avatar, pin }) {
  const hue = useMemo(() => hueOf(account || ''), [account])
  // 记「加载失败过的 URL」而不是一个布尔值:账号列表 15s 轮询一次,avatar 会随
  // 重新解析而变化 —— 用布尔值会把新的 URL 一并判死,头像再也回不来。
  const [failed, setFailed] = useState('')
  const src = avatar && avatar !== failed ? sized(avatar) : ''

  return (
    <span className="acct-avatar" style={{ '--acct-h': String(hue) }}>
      <span className="privacy acct-avatar-txt">{initialOf(name)}</span>
      {/* 真人头像**盖在**首字徽章上而不是替换它:取不到头像(游客号/未绑平台)、
          以及图片还在加载的那几百毫秒里,露出的仍是那枚首字徽章,不会先闪一个空洞。
          加载失败就撤掉 img,让徽章重新露出来(见 setFailed)。 */}
      {src && (
        <img
          className="privacy acct-avatar-img"
          src={src}
          alt=""
          draggable={false}
          loading="lazy"
          // 不给第三方 CDN 送 referrer:否则局域网地址会随图片请求一起外泄
          referrerPolicy="no-referrer"
          onError={() => setFailed(avatar)}
        />
      )}
      <i className={'acct-avatar-dot' + (online ? ' on' : '')} title={online ? '在线' : '离线'} />
      {pin && <PinMark />}
    </span>
  )
}
