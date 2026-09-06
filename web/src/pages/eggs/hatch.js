// 孵化进度的本地外推。
//
// 服务器只在下发那一刻给出 hatchedSecs(以及它的计算时刻 hatchUpdate),之后不再推送
// (进度只在开孵蛋器/开背包时才下发,没有被动推送)。要让页面上的进度条动起来,只能本地按
// 「当前值 + 倍率 × 已过秒数」外推。
//
// —— 倍率从哪来:后端给,前端不估 ——
//
// 倍率由两部分构成,**性质不同、来源不同**:
//
//	活动倍率  = 每周「孵蛋加速日」(北京时间周五 04:00 ~ 周一 04:00)→ 5 倍,否则 1 倍
//	在线加成  = 玩家移动 / 挂风场 / 孵化宝典,只在**在线**时叠加
//
// 活动倍率**不靠测量**:它是固定的每周时间表,服务器在窗口内照此推进,**玩家离线时
// 也一样**。故后端按时刻直接算出来下发(见 internal/pet.HatchActivityRate),离线
// 外推因此天然准确 —— 不需要攒样本,也没有「刚打开页面还没校准」的冷启动。
//
// 在线加成**不进 ETA**:它随速度与移动方式而变(实测同一份 pcap:静止时差分精确
// 5.00,移动时 16.9~25.8),样本不足以定出可信的数,拿它外推会虚报「可破壳」。
//
// 这条划分是踩过坑才明确的。早先把两者混成一个数去「测」(对相邻两次进度下发做差分、
// 取中位数),结果是:样本被移动时段的加速污染(16.9/130 混在里面)、需要攒够 3 个
// 样本才敢给值(冷启动慢)、离线那段完全算不了 —— 都不是噪声问题,是把两个不同性质
// 的量当成了一个。详见 docs/data.md 3.6。
//
// 中间还走过一段弯路(2026-09-05 已回退):后端按「此刻在不在动」乘一个固定增益
// 4.2(5 × 4.2 = 21)实时算倍率。那等价于把上面那个没有定值的量**当成有定值**,
// 结果 ETA 每几秒在 5 倍与 21 倍之间跳 —— 既不准(实测移动档 16.90~27.32,单一
// 增益覆盖不了),也不稳(实测六份 pcap 共翻转 212 次,最密的一份平均 3.5 秒一次)。
//
// 现在的做法:**默认只按活动倍率(1x/5x)外推**,玩家想要更准就自己在页面点
// 「开始测速」—— 去游戏里开两次孵蛋器,后端取两次进度的差分,测出多少用多少
// (实测值,不虚报)。测速结果落库,**玩家下线即清**(倍率依赖他此刻的状态)。
// 见 store/hatch_speed.go 与 docs/data.md 3.6。

// RATE_MIN 是倍率的下限,只用于防御后端数据异常(如字段缺失被解成 0)。
// 除了「加速」没见过别的方向,倍率 <1 只可能是数据出错。
const RATE_MIN = 1

// ---- 实时倍率:加速日时间表 × 此刻是否在动 ----
//
// 前端自己算而不是用后端给的数,是因为倍率会随**时间推移**自己变(比如挂着页面
// 跨过周一 04:00,加速日结束)。用后端某次请求返回的值,那一刻之后就是过期数字。
// 口径与后端 pet.HatchRate 完全一致,两边改一处都要同步改另一处。

// ACTIVITY_RATE 是加速日期间的活动倍率(其余时间 1),来自活动文案
// 「背包孵化精灵速度提升至500%」。见 docs/data.md 3.6。
const ACTIVITY_RATE = 5

// CST_OFFSET 是北京时间的时区偏移(秒)。加速日窗口按北京时间判定,与浏览器所在
// 时区无关 —— 直接用本地时间算会整体偏移,正好跨过 04:00 边界。
const CST_OFFSET = 8 * 3600

// MIN_SAMPLE_GAP 是测速两次采样的最小间隔(秒),与后端 hatchSpeedMinGap 同值。
// 服务器约按 5 秒步长结算进度,间隔太短的差分是假值(实测 1s 区间会算出 5.00 或
// 67.50),见 docs/pcap-2026-09-05-hatch-rate.md §5。前端据此提示玩家「可以开第二次了」。
export const MIN_SAMPLE_GAP = 10

// activityRate 返回 ts(unix 秒)时刻的活动倍率:加速日窗口内 5,否则 1。
// 窗口 = 北京时间**周五 04:00 ~ 周一 04:00**(周一 04:00 整点结束)。
export function activityRate(ts) {
  const t = new Date((ts + CST_OFFSET) * 1000) // 转到北京时间再看星期与时分
  const sec = t.getUTCHours() * 3600 + t.getUTCMinutes() * 60 + t.getUTCSeconds()
  const day = t.getUTCDay() // 0=周日 … 5=周五
  const START = 4 * 3600
  switch (day) {
    case 5: return sec >= START ? ACTIVITY_RATE : 1               // 周五 04:00 起
    case 6: case 0: return ACTIVITY_RATE                          // 整个周末
    case 1: return sec < START ? ACTIVITY_RATE : 1                // 到周一 04:00 止
    default: return 1
  }
}

// effectiveRate 返回 now(毫秒)时刻实际用于外推的倍率。
//
// 优先级:**玩家实测 > 活动倍率时间表**。
//   - 有实测值(玩家点过「开始测速」并开够两次孵蛋器)→ 用它,它是实测的,
//     天然包含此刻的全部加成(活动 + 移动 + 风场),不需要分别建模;
//   - 否则回落到活动倍率(1x/5x),按时刻算 —— 保守但绝不虚报。
//
// speed 是后端给的测速状态 {state, rate, t1, measuredAt};未测速时 state='idle'。
// 之所以不前端自己算倍率:两次进度采样只有后端全程看得见(进度没有被动推送),
// 前端手里通常只有最后一次快照,差不出分。
export function effectiveRate(now, speed, activity) {
  if (speed && speed.state === 'done' && speed.rate > 0) {
    return { rate: speed.rate, measured: true, activity }
  }
  return { rate: activity, measured: false, activity }
}

// clampRate 把倍率收进合理区间(仅作防御,正常路径下后端给的就是 1 或 5)。
export function clampRate(r) {
  // 先处理 NaN(它不参与任何大小比较,单独挡掉);±Infinity 走下面的大小比较
  // 自然落到下限或保持不变,不必单列 —— 用 Number.isFinite 一并判非法会把 +∞
  // 也塞到下限,而它明明不该是 1(测试因此抓到过一次)。
  if (Number.isNaN(r)) return RATE_MIN
  if (r < RATE_MIN) return RATE_MIN
  return r
}

// hatchProgress 返回 {pct, secs, rate} —— 外推到 now(毫秒)的孵化进度;
// 不在孵蛋器里返回 null。
//
// rate 是**后端**给的孵化倍率(每过 1 真实秒推进多少孵化秒)。它按加速日时间表算出,
// 离线也准;玩家此刻若在移动,实际会更快(见文件头),但那部分不进本函数的外推 ——
// 宁可慢一点,也不要虚报「可破壳」。
//
// ⚠️ **secs 是「孵化秒」,不是真实秒**:它是进度条用的量纲(与 maxSecs 同一把尺子),
// 加速期间 1 真实秒推进 rate 个孵化秒。要算「还要多久」的**真实**时间,必须除以 rate
// (见 EggList 的 etaText)—— 直接拿 maxSecs − secs 当秒数,5 倍加速下就会把
// 完成时刻报成 5 倍远,而 1 倍时两者恰好相等、看不出来。
//
// **没有采样就返回 null,绝不外推**:hatchUpdate 为 0 表示这颗蛋从未有过进度采样
// (登录包 0x0102 只给「哪些蛋在孵」、不带逐蛋进度,进度要等开孵蛋器 0x0312 或开
// 背包 0x1344)。此时若按 elapsed = now - 0 外推,得到的是十几亿秒,进度直接顶满 ——
// 而 EggList 把「在孵且进度满」的蛋两栏都不显示,蛋就这样凭空消失了。
// 返回 null 时页面按「在孵、进度未知」处理(见 EggList 的分栏与 EggCard 的展示)。
export function hatchProgress(egg, now, rate) {
  if (!egg || !egg.hatching || !egg.maxSecs) return null
  // 外推起点:优先用采样时刻;没有采样时退回**入孵时刻**。
  //
  // 后端对「进度为 0」的蛋会把 HatchUpdate 一起清零(见 store/egg.go 的 UpsertEggs),
  // 而刚放进去的蛋 hatchedSecs 恰好是 0 —— 若这里照 hatchUpdate=0 返回 null,这颗蛋就
  // **连进度条都没有**:玩家刚放上蛋看不到 0% 也看不到预计,得手动重开一次孵蛋器
  // (服务器重算进度)才出现。而此刻进度是**确定的 0**、入孵时刻也是准确的,
  // 从 startHatch 起算即可 —— 那正是「放入的那一刻」。
  //
  // 这与被实测否掉的「单点法」不是一回事:单点法是拿 v/elapsed **反推倍率**(跑动那段
  // 被平摊进去,虚报成 8~9 倍);这里倍率由后端给,只是从一个确定的零点起算。
  //
  // 安全性:本分支只在 hatching 为真时才会走到(上面已过滤),而 hatchHatching 列只由
  // 权威的 egg_gid 列表对账维护 —— 取出后 startHatch 是残留值,但那颗蛋的 hatching
  // 已被清成 false,故不会拿残留时刻外推(测试守着)。
  const from = egg.hatchUpdate || egg.startHatch
  if (!from) return null // 既无采样也无入孵时刻:真的无从算起,不外推免得顶满
  const r = clampRate(rate || 1)
  const elapsed = Math.max(0, Math.floor(now / 1000) - from)
  const secs = Math.min(egg.maxSecs, (egg.hatchedSecs || 0) + elapsed * r)
  const pct = Math.floor(Math.min(100, (secs / egg.maxSecs) * 100))
  return { pct, secs, rate: r }
}

// remainRealSecs 把「还剩多少孵化秒」折算成**真实**秒数。
//
// 它是 etaText/etaTitle 唯一该用的换算:进度条看孵化秒,倒计时看真实秒,两者
// 差一个 rate。抽出来是为了让这个换算只有一处 —— 分写两遍必有一处漏掉。
export function remainRealSecs(egg, p) {
  if (!egg || !p) return 0
  return Math.max(0, (egg.maxSecs - p.secs) / (p.rate || 1))
}
