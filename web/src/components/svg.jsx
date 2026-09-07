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

// 家园(屋顶 + 墙体,与"实时地图"的图钉区分开)
export const IconHome = (p) => (
  <S {...p}>
    <path d="M3 10.5 12 4l9 6.5" />
    <path d="M5.5 9.6V20h13V9.6" />
    <path d="M10 20v-5h4v5" />
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

// 洛克贝(商店)。
//
// 原版是「单圆 + 中间竖线 + S 形」—— 那种画法在世界观里更像**通用货币符号**
// (¥ $ 那一类),而不是游戏里那枚有厚度的金币,小尺寸下尤其糊。
// 换成 Lucide coins 的「两枚叠币」:轮廓本身就是「钱」,18px 下也一眼认得出。
//
// 默认上 --c-wealth 财富金(项目语义色,见 base.css):金币在哪儿都是金色的,
// 这是**语义**而非装饰 —— 余额/价格处要一眼看出「这是洛克贝」。
// 需要跟随上下文色时显式传 color="currentColor" 覆盖。
export const IconCoin = ({ color = 'var(--c-wealth)', style, ...p }) => (
  // style 展开在后:调用方仍可用内联 style 覆盖这个默认色。
  // stroke 是 currentColor(见 S),故改 color 即改描边色。
  <S {...p} style={{ color, ...style }}>
    <path d="M13.744 17.736a6 6 0 1 1-7.48-7.48" />
    <path d="M15 6h1v4" />
    <path d="m6.134 14.768.866-.5 2 3.464" />
    <circle cx="16" cy="8" r="6" />
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

// 滑杆(设置面板标题:图层/筛选这一类开关集合的统称)
export const IconSliders = (p) => (
  <S {...p}>
    <path d="M4 6h9M17 6h3M4 12h3M11 12h9M4 18h9M17 18h3" />
    <circle cx="15" cy="6" r="2" />
    <circle cx="9" cy="12" r="2" />
    <circle cx="15" cy="18" r="2" />
  </S>
)

// 回转重置(圆弧 + 箭头,顺时针):涂色重置、跟走进度重置
export const IconRefresh = (p) => (
  <S {...p}>
    <path d="M20.5 12a8.5 8.5 0 1 1-2.8-6.3" />
    <path d="M20.5 4v5.5H15" />
  </S>
)

// 展开箭头(向下):折叠按钮的 ▾,旋转由各自的 CSS 负责
export const IconChevronDown = (p) => (
  <S {...p}>
    <path d="M6 9.5l6 6 6-6" />
  </S>
)

// ==================================================================
// 下面这批取自 Lucide(ISC 许可,https://lucide.dev),**自托管**为 JSX ——
// 与上面手写那批同为 24×24 / stroke-width 1.8 / currentColor,故混用看不出来。
//
// 为什么自托管而不装 npm 包:本工具跑在局域网、可能压根没外网(字体已全部
// 自托管,见 base.css 的 @font-face 注释),任何运行时取图标的方案都不可靠。
// 复制进来的只是 path 数据,零依赖、零运行时开销。
//
// 它们补的都是**原先只能用 emoji 顶上**的语义 —— emoji 不响应 currentColor,
// 主题一切换周围图标全变了色而它纹丝不动,且各家系统渲染不同(详见图标统一方案)。
// ==================================================================

// 奖牌(排行榜前三名 / 体型奖牌)
export const IconMedal = (p) => (
  <S {...p}>
    <path d="M7.21 15 2.66 7.14a2 2 0 0 1 .13-2.2L4.4 2.8A2 2 0 0 1 6 2h12a2 2 0 0 1 1.6.8l1.6 2.14a2 2 0 0 1 .14 2.2L16.79 15" />
    <path d="M11 12 5.12 2.2" />
    <path d="m13 12 5.88-9.8" />
    <path d="M8 7h8" />
    <circle cx="12" cy="17" r="5" />
    <path d="M12 18v-2h-.5" />
  </S>
)

// 皇冠(排行榜称号,如「富有」)
export const IconCrown = (p) => (
  <S {...p}>
    <path d="M11.562 3.266a.5.5 0 0 1 .876 0L15.39 8.87a1 1 0 0 0 1.516.294L21.183 5.5a.5.5 0 0 1 .798.519l-2.834 10.246a1 1 0 0 1-.956.734H5.81a1 1 0 0 1-.957-.734L2.02 6.02a.5.5 0 0 1 .798-.519l4.276 3.664a1 1 0 0 0 1.516-.294z" />
    <path d="M5 21h14" />
  </S>
)

// 包裹(宠物位置「盒子」)
export const IconPackage = (p) => (
  <S {...p}>
    <path d="M11 21.73a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73z" />
    <path d="M12 22V12" />
    <polyline points="3.29 7 12 12 20.71 7" />
    <path d="m7.5 4.27 9 5.15" />
  </S>
)

// 地球(宠物位置「大世界」)
export const IconGlobe = (p) => (
  <S {...p}>
    <circle cx="12" cy="12" r="10" />
    <path d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20" />
    <path d="M2 12h20" />
  </S>
)

// 沙漏(宠物位置「待同步」/ 孵化倒计时)
export const IconHourglass = (p) => (
  <S {...p}>
    <path d="M5 22h14" />
    <path d="M5 2h14" />
    <path d="M17 22v-4.172a2 2 0 0 0-.586-1.414L12 12l-4.414 4.414A2 2 0 0 0 7 17.828V22" />
    <path d="M7 2v4.172a2 2 0 0 0 .586 1.414L12 12l4.414-4.414A2 2 0 0 0 17 6.172V2" />
  </S>
)

// 邮箱(商家订阅的通知地址)
export const IconMail = (p) => (
  <S {...p}>
    <path d="m22 7-8.991 5.727a2 2 0 0 1-2.009 0L2 7" />
    <rect x="2" y="4" width="20" height="16" rx="2" />
  </S>
)

// 行李箱(商人商品图缺图兜底)
export const IconLuggage = (p) => (
  <S {...p}>
    <path d="M6 20a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2" />
    <path d="M8 18V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v14" />
    <path d="M10 20h4" />
    <circle cx="16" cy="20" r="2" />
    <circle cx="8" cy="20" r="2" />
  </S>
)

// 上扬(净赚)—— 排行榜「赚钱王」
export const IconTrendUp = (p) => (
  <S {...p}>
    <polyline points="22 7 13.5 15.5 8.5 10.5 2 17" />
    <polyline points="16 7 22 7 22 13" />
  </S>
)

// 下跌(净亏)—— 排行榜「败家子」
export const IconTrendDown = (p) => (
  <S {...p}>
    <polyline points="22 17 13.5 8.5 8.5 13.5 2 7" />
    <polyline points="16 17 22 17 22 11" />
  </S>
)

// 汉堡菜单(图层侧栏开关)
export const IconMenu = (p) => (
  <S {...p}>
    <line x1="4" x2="20" y1="12" y2="12" />
    <line x1="4" x2="20" y1="6" y2="6" />
    <line x1="4" x2="20" y1="18" y2="18" />
  </S>
)

// 星(花种星级 / 事件「仅看高亮」)。
// filled=true 时填充:星级是**数量**(1~5 颗),描边星在小尺寸下数不清几颗,
// 且「几颗星」这个语义本身就靠实心块来读;空心星用于「未选中」态。
// 填充色走 currentColor,故金/灰由调用方的 CSS 决定。
export const IconStar = ({ filled, ...p }) => (
  <S {...p} fill={filled ? 'currentColor' : 'none'}>
    <path d="M11.525 2.295a.53.53 0 0 1 .95 0l2.31 4.679a2.123 2.123 0 0 0 1.595 1.16l5.166.756a.53.53 0 0 1 .294.904l-3.736 3.638a2.123 2.123 0 0 0-.611 1.878l.882 5.14a.53.53 0 0 1-.771.56l-4.618-2.428a2.122 2.122 0 0 0-1.973 0L6.396 21.01a.53.53 0 0 1-.77-.56l.881-5.139a2.122 2.122 0 0 0-.611-1.879L2.16 9.795a.53.53 0 0 1 .294-.906l5.165-.755a2.122 2.122 0 0 0 1.597-1.16z" />
  </S>
)

// 收起箭头(向上):与 IconChevronDown 成对
export const IconChevronUp = (p) => (
  <S {...p}>
    <path d="m18 15-6-6-6 6" />
  </S>
)

// 复制(调试页复制解析结果 / hex)
export const IconCopy = (p) => (
  <S {...p}>
    <rect width="14" height="14" x="8" y="8" rx="2" ry="2" />
    <path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2" />
  </S>
)

// 垃圾篓(删除)
export const IconTrash = (p) => (
  <S {...p}>
    <path d="M3 6h18" />
    <path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6" />
    <path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2" />
    <line x1="10" x2="10" y1="11" y2="17" />
    <line x1="14" x2="14" y1="11" y2="17" />
  </S>
)
