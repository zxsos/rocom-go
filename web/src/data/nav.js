import {
  IconBag, IconPaw, IconEgg, IconSparkle, IconBell,
  IconMap, IconFlower, IconCoin, IconSuitcase, IconTrophy, IconTrail,
} from '../components/svg'

// 一级导航;带 children 的分组项渲染为 2 级菜单(顶栏 hover 下拉 / 底部 tab 弹出面板)。
// 收纳逻辑:功能按「对游戏做了什么」归为三组——我的精灵(收集养成)、世界(探索家园)、商人(洛克贝买卖比拼)。
export const NAV = [
  {
    label: '我的精灵', icon: IconBag,
    children: [
      { to: '/pets', label: '精灵列表', icon: IconPaw },
      { to: '/eggs', label: '精灵蛋', icon: IconEgg },
      { to: '/handbook', label: '炫彩图鉴', icon: IconSparkle },
      { to: '/events', label: '捕获事件', icon: IconBell },
    ],
  },
  {
    label: '世界', icon: IconMap,
    children: [
      { to: '/map', label: '实时地图', icon: IconMap },
      { to: '/flowers', label: '花种', icon: IconFlower },
      { to: '/trial', label: '草系试炼', icon: IconTrail },
    ],
  },
  {
    label: '商人', icon: IconCoin,
    children: [
      { to: '/merchant', label: '远行商人', icon: IconSuitcase },
      { to: '/leaderboard', label: '排行榜', icon: IconTrophy },
    ],
  },
]

// uidOf 从账号键 "UID:<user_id>" 取出 user_id(用于展示 nickname(user_id))。
export const uidOf = (acc) => (acc || '').replace(/^UID:/, '')
