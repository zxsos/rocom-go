import React from 'react'

// 线性 SVG 图标库(24x24,feather 风格,stroke=currentColor,由 CSS 继承颜色)。
// 替代顶栏/导航/列表中的老式 emoji 图标,主题切换时颜色自动跟随。
const S = ({ children, size = 18, ...rest }) => (
  <svg
    width={size} height={size} viewBox="0 0 24 24" fill="none"
    stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round"
    aria-hidden="true" focusable="false" {...rest}
  >
    {children}
  </svg>
)

// 背包(我的精灵)
export const IconBag = (p) => (
  <S {...p}>
    <path d="M6 7a6 6 0 0 1 12 0" />
    <path d="M4 7h16a1 1 0 0 1 1 1v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a1 1 0 0 1 1-1z" />
    <path d="M9 11a3 3 0 0 0 6 0" />
  </S>
)

// 爪印(宠物)
export const IconPaw = (p) => (
  <S {...p}>
    <circle cx="7" cy="10" r="2.4" />
    <circle cx="17" cy="10" r="2.4" />
    <circle cx="5" cy="15" r="2.6" />
    <circle cx="19" cy="15" r="2.6" />
    <path d="M12 15.4c-1.9 0-3.6 1-3.6 2.5 0 1.2 1.6 2.1 3.6 2.1s3.6-.9 3.6-2.1c0-1.5-1.7-2.5-3.6-2.5z" />
  </S>
)

// 精灵蛋
export const IconEgg = (p) => (
  <S {...p}>
    <path d="M12 3c3.4 0 6 3.5 6 7.2S15.4 21 12 21 6 14.2 6 10.2 8.6 3 12 3z" />
    <path d="M12 7.2c1.7 0 3 1.8 3 3.8" />
  </S>
)

// 炫彩星(四角星)
export const IconSparkle = (p) => (
  <S {...p}>
    <path d="M12 3l2.1 6.9L21 12l-6.9 2.1L12 21l-2.1-6.9L3 12l6.9-2.1z" />
  </S>
)

// 铃铛(捕获事件)
export const IconBell = (p) => (
  <S {...p}>
    <path d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9" />
    <path d="M13.7 21a2 2 0 0 1-3.4 0" />
  </S>
)

// 地图(实时地图)
export const IconMap = (p) => (
  <S {...p}>
    <path d="M21 10c0 7-9 13-9 13s-9-6-9-13a9 9 0 0 1 18 0z" />
    <circle cx="12" cy="10" r="3" />
  </S>
)

// 花种
export const IconFlower = (p) => (
  <S {...p}>
    <circle cx="12" cy="12" r="2.6" />
    <path d="M12 9.4a3 3 0 0 1 3-3 3 3 0 0 1-3 3z" />
    <path d="M14.6 12a3 3 0 0 1 3-3 3 3 0 0 1-3 3z" />
    <path d="M12 14.6a3 3 0 0 1 3 3 3 3 0 0 1-3-3z" />
    <path d="M9.4 12a3 3 0 0 1-3-3 3 3 0 0 1 3 3z" />
  </S>
)

// 草系徽章试炼:三章节点连成的路径(章末带一个旗点)
export const IconTrail = (p) => (
  <S {...p}>
    <path d="M5 19c3-6 6-8 7-8s2 4 4 4 3-3 3-3" />
    <circle cx="5" cy="19" r="1.8" />
    <circle cx="12" cy="11" r="1.8" />
    <path d="M19 4v7" />
    <path d="M19 4l3.2 2L19 8" />
  </S>
)

// 洛克贝(商店)
export const IconCoin = (p) => (
  <S {...p}>
    <circle cx="12" cy="12" r="9" />
    <path d="M12 7v10" />
    <path d="M15 9.5c-.5-1-1.7-1.5-3-1.5-1.7 0-3 .8-3 2s1.3 2 3 2 3 .8 3 2-1.3 2-3 2c-1.3 0-2.5-.5-3-1.5" />
  </S>
)

// 旅行箱(远行商人)
export const IconSuitcase = (p) => (
  <S {...p}>
    <rect x="3" y="7" width="18" height="13" rx="2" />
    <path d="M8 7V5a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
    <path d="M3 12h18" />
  </S>
)

// 奖杯(排行榜)
export const IconTrophy = (p) => (
  <S {...p}>
    <path d="M8 21h8" />
    <path d="M12 17v4" />
    <path d="M7 4h10v5a5 5 0 0 1-10 0z" />
    <path d="M7 5H4a2 2 0 0 0 2 4" />
    <path d="M17 5h3a2 2 0 0 1-2 4" />
  </S>
)

// 太阳(白天主题)
export const IconSun = (p) => (
  <S {...p}>
    <circle cx="12" cy="12" r="4.2" />
    <path d="M12 2.5v2.2M12 19.3v2.2M2.5 12h2.2M19.3 12h2.2M5.3 5.3l1.6 1.6M17.1 17.1l1.6 1.6M18.7 5.3l-1.6 1.6M6.9 17.1l-1.6 1.6" />
  </S>
)

// 月亮(夜间主题)
export const IconMoon = (p) => (
  <S {...p}>
    <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z" />
  </S>
)

// 显示器(跟随系统主题)
export const IconMonitor = (p) => (
  <S {...p}>
    <rect x="2.5" y="3.5" width="19" height="13.5" rx="2" />
    <path d="M8.5 21h7" />
    <path d="M12 17v4" />
  </S>
)

// 展开(进入网页全屏)
export const IconExpand = (p) => (
  <S {...p}>
    <path d="M8 3H5a2 2 0 0 0-2 2v3" />
    <path d="M21 8V5a2 2 0 0 0-2-2h-3" />
    <path d="M3 16v3a2 2 0 0 0 2 2h3" />
    <path d="M16 21h3a2 2 0 0 0 2-2v-3" />
  </S>
)

// 收起(退出网页全屏)
export const IconCompress = (p) => (
  <S {...p}>
    <path d="M8 3v3a2 2 0 0 1-2 2H3" />
    <path d="M21 8h-3a2 2 0 0 1-2-2V3" />
    <path d="M3 16h3a2 2 0 0 1 2 2v3" />
    <path d="M16 21v-3a2 2 0 0 1 2-2h3" />
  </S>
)

// 锁(PIN 保护)
export const IconLock = (p) => (
  <S {...p}>
    <rect x="3.5" y="11" width="17" height="10" rx="2" />
    <path d="M7.5 11V7a4.5 4.5 0 0 1 9 0v4" />
  </S>
)

// 关闭 ✕
export const IconClose = (p) => (
  <S {...p}>
    <path d="M6 6l12 12M18 6L6 18" />
  </S>
)

// 对勾(账号下拉的选中标记)
export const IconCheck = (p) => (
  <S {...p}>
    <path d="M20 6L9 17l-5-5" />
  </S>
)
