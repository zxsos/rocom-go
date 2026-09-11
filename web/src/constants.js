// 游戏侧固定枚举(名称表随游戏版本变化时同步更新)。

// 固定系别列表(用于筛选)。
export const ALL_TYPES = ['普', '草', '火', '水', '光', '地', '冰', '龙', '电', '毒', '虫', '武', '翼', '萌', '幽', '恶', '机械', '幻']

// 蛋组(繁殖组)社区流行名,按 id 1-15 顺序;"未发现"(id 1)为不可繁殖,置于末尾。
//
// ⚠️ 只有这一项**值/显示名分开**:"未发现"是游戏配置里的原话(EGG_GROUP id=1 的 name 与 desc
// 都是这四个字 —— 迪莫、帕尔萨斯/圣羽翼王那一系,进不了小窝配种),而玩家口径里它读作"无蛋组"。
//
// 为什么不能直接把常量改成"无蛋组":蛋组 chip 的取值**同时就是发给后端的查询值**,而
// egg_groups 列里存的是官方名 —— 发 eggGroups=无蛋组 出去,后端 egg_groups LIKE '%"无蛋组"%'
// 一条都匹配不到,筛选会**静默变空**(面板上 chip 照常高亮,结果一只都没有)。
// 故:value 恒为官方名(查询依据),label 只给人看。
export const EGG_GROUP_UNKNOWN = '未发现'
export const EGG_GROUP_NONE_LABEL = '无蛋组'

// eggGroupLabel 把**库里的蛋组名**转成玩家口径的显示名:官方名"未发现"与空值都读作"无蛋组"
// (前者是不可繁殖那一组,后者是压根没配蛋组的形态 —— 对玩家是同一件事:没有能拿来配种的组)。
export function eggGroupLabel(name) {
  return !name || name === EGG_GROUP_UNKNOWN ? EGG_GROUP_NONE_LABEL : name
}

// breedableEggGroups 取一只宠物**能用于配种**的蛋组名(剔除"无蛋组"那一套);没有则返回空数组。
//
// 列表长按菜单用它决定要不要给出蛋组两项,培育页深链用它收窄品种候选 —— 两处都不能把
// "无蛋组"当成一个正常蛋组:它既筛不出同类(不可繁殖的宠物之间配不出蛋),也配不出同伴。
export function breedableEggGroups(groups) {
  return (groups || [])
    .map((g) => (typeof g === 'string' ? g : g && g.name))
    .filter((n) => n && n !== EGG_GROUP_UNKNOWN)
}

// ALL_EGG_GROUPS 是筛选面板的蛋组选项:{ value: 发给后端的官方名, label: 显示名 }。
export const ALL_EGG_GROUPS = [
  '巨灵', '两栖', '昆虫', '天空', '动物', '妖精', '植物', '拟人', '软体', '大地', '魔力', '海洋', '龙', '机械', EGG_GROUP_UNKNOWN,
].map((value) => ({ value, label: eggGroupLabel(value) }))

// 热门性格(列表筛选、事件高亮点选用)及其六维影响。其余归入"其他"。
export const HOT_NATURES = [
  ['开朗', '速度↑魔攻↓'],
  ['胆小', '速度↑物攻↓'],
  ['固执', '物攻↑魔攻↓'],
  ['聪明', '魔攻↑物攻↓'],
  ['平和', '生命↑魔攻↓'],
  ['踏实', '生命↑速度↓'],
  ['沉默', '生命↑物攻↓'],
  ['急躁', '速度↑物防↓'],
]
export const HOT_NATURE_NAMES = HOT_NATURES.map((n) => n[0])
