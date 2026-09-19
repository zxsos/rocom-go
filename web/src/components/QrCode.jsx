import { QRCodeSVG } from 'qrcode.react'

// QrCode 把一段文本画成二维码。
//
// 只做一件事:让「扫一下」成为最省事的入口。装在码里的永远是一个**短链**,
// 不是整份配置 —— 配置里带明文密码,而二维码会被截图、会被转发,塞进去等于把
// 密码印在明信片上;何况整份配置的体积会让码密到扫不出来。
//
// 纠错给到 H 级(30%):手机上扫的是屏幕上的码,可能有反光、可能被手指挡住一角,
// 而扫不出来的代价是朋友以为链接坏了、直接放弃 —— 这是整条链路里最不该失败的一环。
export default function QrCode({ value, size = 220, className = '' }) {
  if (!value) return null
  return (
    <div className={`qrcode ${className}`} style={{ width: size, height: size }}>
      <QRCodeSVG
        value={value}
        size={size}
        level="H"
        marginSize={1}
        bgColor="transparent"
        fgColor="currentColor"
      />
    </div>
  )
}
