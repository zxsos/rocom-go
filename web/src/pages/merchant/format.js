import { fmtTime } from '../../utils/format'

// 远行商人页的纯展示逻辑(不含 React):第三方响应的解析、分栏、文案映射。
// 抽出来是因为它们与页面状态无关,单独放便于阅读与复用。
//
// 业务节奏:每天 8 点开张、0 点收摊,8/12/16/20 四个整点各上架一轮新货并售卖 4 小时;
// 只有 00:00~08:00 是打烊休市(页面此时显示昨日四轮全天回顾)。

// 商品图只认 http(s) 外链:两个数据源的图都是 patchwiki.biligame.com 的 https
// 直链(2026-09-03 用咸鱼源真实响应核对过),不存在本地相对路径。
//
// 早先这里还兼容「按 /img/ 相对路径解析」,那半条是死分支,已删。若第三方改回
// 相对路径,表现是商品图静默不显示(不报错),排查时先看 items[].image 的取值。
export const imgSrc = (it) => {
  const v = (it && it.image) || ''
  return /^https?:\/\//i.test(v) ? v : ''
}

// msTime 第三方的时间戳是毫秒,fmtTime 要秒,这里除 1000 再交给它。
export const msTime = (ms) => (ms ? fmtTime(ms / 1000) : '')

// 第三方商品 kind(英文) → 中文显示(与后端邮件 merchantKindTextMap 一致):
// prop 直接显示为「道具」等;未收录的未知值原样返回。
const KIND_TEXT = {
  prop: '道具', pet: '宠物', egg: '精灵蛋', fragment: '碎片',
  skin: '皮肤', cloth: '装扮', material: '材料', seed: '种子',
  fruit: '果实', food: '食物', gem: '宝石', diamond: '钻石',
  ticket: '票券', tool: '工具', equip: '装备', consumable: '消耗品',
  furniture: '家具', card: '卡片', scroll: '卷轴', key: '钥匙',
  medal: '奖牌', suit: '套装', decoration: '装饰', coin: '洛克贝',
}
export const kindText = (k) => KIND_TEXT[k] || k

// 推荐关键词:常见「值得买」商品词,点击即填入(自动补英文逗号,再点一次取消)。
export const SUB_PRESETS = ['球', '棱镜', '国王', '项链', '粉尘', '零碎', '相框', '魔镜', '钥匙']

// 关键词规范化:中文逗号/顿号/分号/句号/空白等间隔符统一成英文逗号,去空项。
// 后端按英文逗号分词(大小写不敏感子串匹配),这里保证前端提交的都是规范形式。
export const normKws = (s) =>
  String(s || '').replace(/[，、;；。\s]+/g, ',').split(',').map((x) => x.trim()).filter(Boolean).join(',')

// 营业状态 → 徽标文案与样式
export const STATUS = {
  open: { cls: 'ok', text: '营业中' },
  idle: { cls: 'off', text: '已打烊' },
}

// 后端 merchant 槽存的是第三方原始响应(带 code/msg/data 壳,见 api_merchant.go
// PutMerchantSlot(string(body))),业务字段(merchant_name/items/round…)都在 data 里。
// unwrap 取出 data 层;老缓存可能直接存裸 data,typeof 判断兜底兼容。
export const unwrap = (m) => (m && m.data && typeof m.data === 'object' ? m.data : m)

// count 统计某轮的第三方 item 数(优先 item_count,兜底数 items 数组)。
export const count = (raw) => {
  const m = unwrap(raw)
  return m ? (m.item_count ?? ((m.items && m.items.length) || 0)) : 0
}

// 标准售卖时段(北京时间):8/12/16/20 四轮,各 4 小时。
export const SLOTS = ['08:00-12:00', '12:00-16:00', '16:00-20:00', '20:00-24:00']

// parseSlots 解析商品售卖时段:优先用第三方 time_label("08:00-12:00 / …"),
// 解析失败时按 start_time/end_time(毫秒,北京时间)推断为单个时段串。
export function parseSlots(it) {
  const raw = it && it.time_label
  if (raw) {
    const slots = String(raw).split('/').map((s) => s.trim()).filter((s) => /^\d{2}:\d{2}-\d{2}:\d{2}$/.test(s))
    if (slots.length) return slots
  }
  const st = it && it.start_time, et = it && it.end_time
  if (!st || !et) return []
  const bj = (ms) => {
    const d = new Date(ms + 8 * 3600 * 1000) // 加 8h 读 UTC 组件 = 北京时间
    return `${String(d.getUTCHours()).padStart(2, '0')}:${String(d.getUTCMinutes()).padStart(2, '0')}`
  }
  const s = bj(st)
  let e = bj(et)
  if (e === '00:00') e = '24:00' // 结束恰在北京午夜 → 按 24:00
  return s === e ? [] : [`${s}-${e}`]
}

// groupBySlot 把商品按时段分栏:覆盖全部四段的商品归「全天」栏(不重复进各时段栏),
// 其余按各自时段归栏;时段串不在标准四段内的归「其他」栏。
export function groupBySlot(items) {
  const groups = SLOTS.map((s) => ({ slot: s, items: [] }))
  const allDay = []
  const other = []
  for (const it of items) {
    const slots = parseSlots(it)
    if (slots.length && SLOTS.every((s) => slots.includes(s))) {
      allDay.push(it)
      continue
    }
    let hit = false
    for (const s of slots) {
      const g = groups.find((x) => x.slot === s)
      if (g) { g.items.push(it); hit = true }
    }
    if (!hit) other.push(it)
  }
  return {
    allDay,
    groups: groups.filter((g) => g.items.length),
    other,
  }
}
