package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"time"
)

// 远行商人:第三方 API(https://apii.xianyuw.cn/api/v1/rocom-merchant)的本地缓存代理。
//
// 业务模型(按游戏活动节奏):
//   - 每天 8:00 开张、0:00(24 点)收摊,8/12/16/20 四个整点各上架一轮新货,0 点后到次日
//     8:00 前打烊休市没有在售(页面显示刚结束营业日的全天回顾);
//   - 查询槽按 4h 对齐 8 点:8/12/16/20 四轮都回源第三方,次日 0/4 两个槽休市不查;
//   - 结果按槽缓存进 SQLite(store 的 merchant_slots),缓存保留 2 天,写入时顺手清理更早记录;
//   - 回源规则见 merchantShouldFetch,两条核心约束:
//   - **已结束的槽永不回源** —— 回源拿到的是「现在」的货单,写进历史槽就是伪造数据;
//   - **进行中的槽按 merchantRefetch 冷却重查、限 merchantRefetchWin 窗口** ——
//     第三方自己有缓存,轮次开始后新上的商品滞后才出现,只查一次会永久错过;
//     重查统一带 refresh=true 让它真正回源(见 merchantForceRefresh),否则
//     拿回的始终是它那份旧快照,重查等同空转;
//   - 触发回源两条路径:merchantLoop 轮询当前槽(覆盖「早上 8 点自动查第一次」与整点后的
//     密集重试,见 merchantPoll / merchantCatchupEvery),以及玩家打开页面时
//     handleMerchant 按当前时间补查/重查当前轮;
//   - 订阅提醒:有货槽写入后,对该槽**尚未通知过的商品**,对订阅者(邮箱+关键词,空关键词=全部)
//     发 QQ 邮箱邮件。去重粒度是**每槽每商品**且**不跨档** —— 同一批货隔档重新上架时要再
//     提醒一次(2026-09-03 实测 16:00 档货单与 08:00 档完全相同,跨档去重会整档静默),
//     依据与理由见 merchant_notify.go 的 merchantNotify。SMTP 未配置时静默跳过。
const (
	merchantOpenHour = 8                // 每天 8 点开张(8 点前休市)
	merchantSlotStep = 4 * time.Hour    // 查询槽跨度(8/12/16/20 四轮)
	merchantCheck    = 15 * time.Minute // 定时器检查间隔
	merchantSmtpHost = "smtp.qq.com"    // QQ 邮箱 SMTP(465 SSL)

	// merchantRefetch 是**进行中的槽**两次回源的最小间隔。
	//
	// 存在理由:第三方自己有缓存,轮次开始后新上架的商品要滞后一段时间才出现在它的响应里
	// —— 2026-08-30 实测:20:00 开轮,第三方那份快照到 20:56 才补全 3 件轮次专属货
	// (魔力果/火系粉尘/萌系粉尘)。只在槽开始时查一次的话,这部分商品永久错过,
	// 页面只有「强制刷新」才补得回来。
	//
	// 注:该实测发生在 refresh=false(拿第三方缓存快照)时;现已统一带 refresh=true
	// 回源(见 merchantForceRefresh),滞后时间应大幅缩短甚至消失。本冷却与
	// merchantRefetchWin 仍保留作为兜底 —— 第三方是否真会即时更新没有保证。
	//
	// 取值 10 分钟:一轮 4h 但只在前 90 分钟重查(merchantRefetchWin),故单轮至多 9 次,
	// 一天 4 轮 + 补查 ≈ 40 次/天,token 可控。注意它必须 ≥ merchantClaimCooldown
	// (merchant_notify.go),否则订阅提醒的正常补发会被认领机制误挡。
	merchantRefetch = 10 * time.Minute

	// merchantRefetchWin 是槽开始后允许重查的时间窗口,过了窗口该槽即固化。
	//
	// 兜底用:第三方滞后多久没有保证,不能让一个槽无限期地每 10 分钟烧一次 token。
	// 取 90 分钟 = 实测滞后 56 分钟 + 余量,且远小于槽长 4h,轮次后半段不再打扰第三方。
	merchantRefetchWin = 90 * time.Minute

	// merchantPoll 定时任务的轮询间隔:只做「判定要不要回源」,回不回由
	// merchantShouldFetch 说了算(它同时管着冷却与窗口),故调小它**不会**增加
	// 第三方请求 —— 额外的成本只是每秒若干次本地时间比较与一次 SQLite 主键读。
	//
	// 为什么是 15 秒而不是 15 分钟:第三方在整点后约 1 分钟才切换到新一轮
	// (两个源都实测确认),而 merchantCatchupEvery 是 30 秒 —— 轮询必须明显细于
	// 它,否则「每 30 秒重试」会被轮询粒度拖成「每 15 分钟」。
	merchantPoll = 15 * time.Second

	// merchantCatchupEvery 是「档期刚开始、还没拿到货单」时的重试间隔。
	//
	// 存在理由:两个源都在整点后约 1 分钟才切到新一轮(08:00 档实测 32~62 秒;
	// 12:00 换档同样约 1 分钟,已由订阅邮件「整点后 2 分 16 秒」佐证)。
	// 于是**整点后的第一次回源几乎必然拿到空货单** —— 若就此套用 merchantRefetch
	// 那个 10 分钟冷却,新货单要等到整点后 10 分钟才进库,比不轮询还慢。
	//
	// 故「空」在切换窗口内按「还没切」处理,用这个短间隔重试;一旦拿到货单
	// (empty=false),就回到 10 分钟冷却去追第三方滞后补上的新货 —— 两者语义不同,
	// 不能共用一套节奏。
	merchantCatchupEvery = 30 * time.Second

	// merchantCatchupWin 是切换窗口的长度(从槽开始算起)。
	//
	// 取 5 分钟 = 实测滞后约 1 分钟的 5 倍余量:源偶尔慢一点也能兜住,不至于退回
	// 10 分钟冷却。窗口内最坏多打 10 次第三方(5min/30s),加上之后的补货重查,
	// 单轮最坏约 18 次 —— 但这是**源异常**时的代价,正常情况下窗口内只打 2 次
	// (第一次空、第二次拿到),单轮总量与改前(约 9 次)基本一致。
	merchantCatchupWin = 5 * time.Minute

	// merchantForceRefresh 回源时是否带 refresh=true(让第三方绕过自己的缓存)。
	//
	// **开启原因**:第三方自己有缓存,轮次整点开始后新上架的商品要滞后才出现在它
	// 的响应里 —— 2026-08-30 实测 20:00 开轮,那份快照到 20:56 才补全 3 件轮次专属货。
	// 更关键的是:**带 refresh=false 时,重查拿到的仍是它缓存的旧快照** —— 于是在
	// 补全之前,无论重查多少次都是同一份不完整数据,「滞后 56 分钟」不是靠等就能熬
	// 过去的。带 refresh=true 才会真正回源,准点后很快就能拿到新货单(用户实测整点
	// 后约 1 分钟即可拿到)。
	//
	// 代价是每次回源都真正打到上游(烧 token),故仍需 merchantShouldFetch 控制次数
	// (一轮至多 9 次、一天 ≈40 次),不能因为开了它就放开重查。
	merchantForceRefresh = true
)

// 远行商人**数据源**:同一时刻只有一个生效,管理员可在管理面板切换(见
// handleAdminMerchantSource)。两个源是互相独立的第三方,不是同一站的两条路:
//
//   - merchantSrcXianyu 咸鱼源:第三方 JSON 接口,需 -egg-api-key。字段最全
//     (商人名/副标题/商品图/类别/本轮倒计时),默认源。
//   - merchantSrcHaoyou 好游快爆源:抓公开页面,**无需令牌** —— 这是它相对咸鱼源
//     的核心价值(2026-09-02 起咸鱼源实测返回 401)。商品名/价格/限购/商品图/类别
//     都有(图是外链),少的只是商人名、副标题、本轮倒计时;整点后约 30~60 秒切档,
//     与咸鱼源开了强制回源后的滞后同一量级(实测数据与时间线见 docs/data.md)。
//
// 两源形态完全不同(JSON 接口 vs HTML 页面),故在 merchantFetch 处按当前源分派到
// fetchXianyu / fetchHaoyou,二者都返回**同形的响应体**再往下走 —— 有货判定、
// 订阅邮件、前端渲染这四处因此不必分支。归一化层见 merchant_haoyou.go。
const (
	merchantSrcXianyu = "xianyu"
	merchantSrcHaoyou = "haoyou"
)

// merchantSrcDefault 库里没配置时生效的源。
const merchantSrcDefault = merchantSrcXianyu

// merchantSourceValid 判断源标识是否合法(管理端点写入前的校验入口)。
//
// 单独成函数而非 map 查表:标识只有两个、且要被三处(载入/切换/校验)共用,
// 用 map 反而多一个需要同步维护的清单。
func merchantSourceValid(src string) bool {
	return src == merchantSrcXianyu || src == merchantSrcHaoyou
}

// merchantNeedKey 该源是否必须配置第三方令牌。
func merchantNeedKey(src string) bool { return src == merchantSrcXianyu }

// merchantSourceName 源的中文展示名。
//
// 映射放在后端:前端切换时要 POST 标识,若这份映射只存在于前端,两边迟早漂移
// (改了一处忘了另一处,表现是「面板显示未知源」这类没人能一眼看懂的错)。
func merchantSourceName(src string) string {
	switch src {
	case merchantSrcXianyu:
		return "咸鱼源"
	case merchantSrcHaoyou:
		return "好游快爆源"
	}
	return src
}

// merchantSource 返回当前生效的数据源标识。
//
// 内存镜像而非每次读库:回源路径与 HTTP 处理器都会频繁问它(merchantEnsure 每 tick、
// handleMerchant 每次请求各一次),而它只在切源时才变 —— 没必要为一次几乎不变的
// 读取给每条请求加一条 SQL。
func (s *Server) merchantSource() string {
	s.merchantSrcMu.Lock()
	defer s.merchantSrcMu.Unlock()
	return s.merchantSrc
}

// merchantSetSource 切换数据源:落库 → 更新内存镜像 → 清空槽缓存 → 按新源重抓当前轮。
//
// 清缓存是必须的(理由见 store.ClearMerchantSlots):不清的话另一源格式的旧货单会被
// 当成新源的数据显示,页面顶部的来源标注也在说谎。代价是切源当天「昨日回顾」为空,
// 直到下一个营业日的档被缓存 —— 管理面板卡片里写明了这一点。
//
// 重抓放在 goroutine:抓第三方是秒级(页面比 JSON 接口还慢),同步做会让管理面板的
// 保存按钮一直转圈。切源后页面短暂为空是可接受的,下一个 15 分钟 tick 也会兜住。
func (s *Server) merchantSetSource(src string) error {
	if !merchantSourceValid(src) {
		return fmt.Errorf("未知的数据源 %q", src)
	}
	if err := s.store.SetMerchantSource(src); err != nil {
		return err
	}
	s.merchantSrcMu.Lock()
	s.merchantSrc = src
	s.merchantSrcMu.Unlock()
	if err := s.store.ClearMerchantSlots(); err != nil {
		return err
	}
	log.Printf("远行商人数据源已切换为 %s:已清空槽缓存,正在按新源重抓当前轮", src)
	go s.merchantEnsure(time.Now(), true)
	return nil
}

// merchantShouldForceRefresh 决定本次回源是否强制第三方刷新缓存。
//
// 抽成函数是为了让策略可调:目前一律 true(首查要真实数据、重查就是为了追滞后,
// 两者都必须强制;拿陈旧快照的重查没有意义)。若将来第三方对 refresh 单独限流,
// 可在此按「首查/重查」或时间窗口细分,而不必改调用点。
func merchantShouldForceRefresh() bool {
	return merchantForceRefresh
}

// merchantFetchURL 第三方接口地址。
//
// 做成 var 而非 const:merchantFetch 直接打这个地址,单元测试必须能用 httptest 换掉
// (真地址会打到线上、烧 token,见 merchant_fetch_test.go 的 fakeMerchantAPI)。
var merchantFetchURL = "https://apii.xianyuw.cn/api/v1/rocom-merchant"

// merchantLoc 固定北京时间(UTC+8):游戏按北京时间 8 点开张,第三方时间戳也是北京时区语义
// (fetched_at 为 UTC 的 8 点 = 北京 8 点)。不依赖服务器本地时区——云服务器常默认 UTC,
// 会导致 slot 与营业状态整体错位 8 小时(UTC 凌晨被误判「打烊」,永远不回源)。
var merchantLoc = time.FixedZone("CST", 8*3600)

// merchantSlotJSON 单个 4h 槽。性质分两种:
//   - 上架轮(8/12/16/20):该时段在售卖对应点位上架的商品,empty=查过但无货(不算休市);
//   - 打烊休市(次日 0/4,off=true):00:00~08:00 收摊打烊,没有在售,也不查询。
//
// merchant 是该槽第三方原始 JSON(仅上架轮有货时带)。
type merchantSlotJSON struct {
	Start    int64           `json:"start"`
	End      int64           `json:"end"`
	Label    string          `json:"label"`
	Empty    bool            `json:"empty"`
	Off      bool            `json:"off"` // true=打烊休市时段(0~8 点),不是商品轮
	Merchant json.RawMessage `json:"merchant,omitempty"`
}

// merchantRespJSON handleMerchant 的响应:status 供前端选择「营业中 / 昨日回顾」。
type merchantRespJSON struct {
	Now    int64              `json:"now"`
	Day    string             `json:"day"`    // 当前展示的营业日(YYYY-MM-DD;休市时指刚结束的营业日)
	Source string             `json:"source"` // 当前生效的数据源标识(xianyu/haoyou),前端据此标注来源
	Status string             `json:"status"`
	Today  []merchantSlotJSON `json:"today"` // 当天 6 个槽(升序,empty 标注休市;8 点前为空,看 prev)
	Prev   []merchantSlotJSON `json:"prev"`  // 仅 status=idle 时填充:昨日的 6 个槽(回顾用)
}

// merchantDayStart 返回 t 所在营业日的 0 点(按北京时间计算)。
func merchantDayStart(t time.Time) time.Time {
	t = t.In(merchantLoc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, merchantLoc)
}

// merchantDaySlots 返回营业日 6 个槽的开始时刻(对齐 8 点:8/12/16/20/0/4)。
func merchantDaySlots(day time.Time) []time.Time {
	return []time.Time{
		day.Add(8 * time.Hour),
		day.Add(12 * time.Hour),
		day.Add(16 * time.Hour),
		day.Add(20 * time.Hour),
		day.Add(24 * time.Hour),
		day.Add(28 * time.Hour), // 次日 4 点
	}
}

// merchantDayStatus 返回当前时刻的营业状态(按北京时间判定):
// open=营业中(8 点至次日 0 点前,显示今日已上架轮次),idle=打烊休市(0 点后到次日 8 点前,显示昨日回顾)。
func merchantDayStatus(now time.Time) string {
	if now.In(merchantLoc).Hour() < merchantOpenHour {
		return "idle"
	}
	return "open"
}

// merchantLoop 定时补查:按 merchantPoll 频繁轮询,是否真的回源由 merchantShouldFetch
// 判定(它管着切换窗口的密集重试与之后的补货冷却,故轮询快并不会多打第三方)。
//
// 轮询频率与「拿货单的速度」直接相关:第三方整点后约 1 分钟才切新一轮,而 merchantPoll
// 是 15 秒 —— 于是新档最长 15 秒被发现、约 30 秒内重试拿到(第三方切好后的下一次重试)。
// 改前是每 15 分钟撞一次运气,最坏要等 15 分钟。
//
// merchantResend(补发失败邮件的补扫)**不跟着变密**,仍按 merchantCheck 每 15 分钟一次:
// 它要遍历本营业日所有已开始的槽并读订阅列表,每分钟跑一次纯属浪费;而且补发本就是
// 兜底,晚十几分钟不影响。两者节奏不同,故分开计时而非共用一个 ticker。
func (s *Server) merchantLoop() {
	s.merchantEnsure(time.Now())
	t := time.NewTicker(merchantPoll)
	defer t.Stop()
	nextResend := time.Now().Add(merchantCheck)
	for now := range t.C {
		s.merchantEnsure(now)
		if !now.Before(nextResend) {
			nextResend = now.Add(merchantCheck)
			s.merchantResend(now)
		}
	}
}

// merchantResend 补扫本营业日已开始的有货槽,对「未通知且关键词命中」的订阅重发提醒。
// 用于兜底首次发信失败(SMTP 瞬断/授权码过期/被限流)与事后补发(如服务 8 点后才启动、
// 当天漏看)。幂等:merchantNotify 按已通知商品清单去重(见 store.MerchantNotifiedItems),
// 且同一槽在冷却内只认领一次(见 merchant_notify.go 的 merchantClaim),故本函数与
// merchantEnsure 的异步发信撞到同一槽也不会重复打扰;SMTP 未配置或打烊(0-8 点)时直接返回。
func (s *Server) merchantResend(now time.Time) {
	if !s.smtp.configured() {
		return
	}
	if merchantDayStatus(now) == "idle" {
		return
	}
	for _, st := range merchantDaySlots(merchantDayStart(now)) {
		if st.After(now) {
			break // 只扫已开始的槽(8/12/16/20 中开始时刻 ≤ now 的)
		}
		if empty, _, _, ok := s.store.GetMerchantSlot(st.Unix()); !ok || empty {
			continue // 未回源或无货,无提醒可发
		}
		s.merchantNotify(st)
	}
}

// merchantEnsure 按当前时间补齐缓存:营业中(8-24 点)只考虑「当前轮」(8/12/16/20 中
// 开始时刻 ≤ now 的最后一轮),按 merchantShouldFetch 判定是否回源;打烊(0-8 点)不查,
// 回顾数据看库。force=true 时跳过判定直接回源当前轮,供前端「强制刷新」用。
//
// **更早的轮一概不回源**:merchantFetch 是按「现在时刻」问第三方的,拿回来的是当前货单,
// 写进 8 点槽等于伪造历史(服务 16 点才启动时尤其明显 —— 旧实现会把 16 点的货单同时填进
// 8/12/16 三个槽)。宁可让历史轮显示「无数据」,也不能拿假数据充数。
func (s *Server) merchantEnsure(now time.Time, force ...bool) {
	// 只有咸鱼源需要令牌:好游快爆源抓公开页面,未配 -egg-api-key 时仍应正常定时
	// 补查 —— 否则「换源」就换不来任何数据,而这恰恰是换源最主要的用途。
	if merchantNeedKey(s.merchantSource()) && s.eggAPIKey == "" {
		return
	}
	if merchantDayStatus(now) == "idle" {
		return
	}
	day := merchantDayStart(now)
	slots := merchantDaySlots(day)

	// 当前轮 = 最后一个开始时刻 ≤ now 的可查槽(前 4 个);8 点前不会有。
	cur := merchantCurrentSlot(day, now)
	if cur < 0 {
		return
	}

	s.merchantMu.Lock()
	// 有货的新回源槽收集起来,锁外再补发订阅邮件(发信慢,别占锁)。
	var notify []time.Time
	if len(force) > 0 && force[0] {
		if ok, empty := s.merchantFetch(slots[cur], merchantShouldForceRefresh()); ok && !empty {
			notify = append(notify, slots[cur])
		}
	} else if s.merchantShouldFetch(slots[cur], now) {
		if ok, empty := s.merchantFetch(slots[cur], merchantShouldForceRefresh()); ok && !empty {
			notify = append(notify, slots[cur])
		}
	}
	s.merchantMu.Unlock()

	// 订阅邮件在后台 goroutine 发:SMTP 偶发慢/挂连接,同步发会阻塞「强制刷新」的 HTTP
	// 响应(前端 fetch 一直等)。sendMerchantMail 自带整体 deadline,这里异步双保险。
	go func() {
		for _, st := range notify {
			s.merchantNotify(st)
		}
	}()
}

// merchantCurrentSlot 返回当前轮的索引:8/12/16/20 四轮中开始时刻 ≤ now 的最后一轮;
// 8 点前(打烊)返回 -1。
//
// 「只回源当前轮」的另一半保障:更早的轮连被评估的机会都没有。与 merchantShouldFetch
// 的「已结束的槽永不回源」合起来才完整 —— 即便某天把这里的下标放开了,已结束的槽
// 也会被那条硬规则挡住。
func merchantCurrentSlot(day, now time.Time) int {
	cur := -1
	for i, st := range merchantDaySlots(day)[:4] {
		if !st.After(now) {
			cur = i
		}
	}
	return cur
}

// merchantSlotLive 判断某槽此刻是否仍在进行中(结束时刻晚于 now)。
//
// 它是所有回源判定的前置闸门:进行中的槽才有重查的意义,已结束的槽拿回来的是「现在」的货单。
func merchantSlotLive(slotStart, now time.Time) bool {
	return slotStart.Add(merchantSlotStep).After(now)
}

// merchantShouldFetch 判断某槽此刻是否该回源第三方。四条判据,依次是:
//
//  1. **已结束的槽永不回源**(merchantSlotLive)。这是硬规则,不是优化 ——
//     回源拿到的是当前货单,写进历史槽就是伪造数据。
//  2. **没查过的槽补查**(无缓存记录)。覆盖「服务在轮次中途才启动」与「8 点开张首查」。
//  3. **空货单 + 仍在切换窗口内** → 按 merchantCatchupEvery 密集重试。
//     这条是「整点后尽快拿到新货单」的关键(见下)。
//  4. **已查过(非空)且仍在窗口内、且距上次回源 ≥ merchantRefetch** → 重查。
//     覆盖第三方滞后补货:轮次开始后新上的商品要过一阵才出现在它的响应里。
//     窗口 merchantRefetchWin 之外一律不重查,避免一个槽无限期烧 token。
//
// 为什么第 3 条必须单独存在 —— 这是本次重构里唯一反直觉的地方:
// 两个源都在整点后约 1 分钟才切到新一轮,于是**整点后的第一次回源几乎必然拿到空**。
// 若此时套用第 4 条那个 10 分钟冷却,新货单就得等到整点后 10 分钟才进库 —— 比不
// 轮询还慢。故「还没拿到货单」与「已拿到、等补货」是两件事,必须分开定节奏:
// 前者是等第三方切换(几十秒量级),后者是等它补货(十分钟量级)。
func (s *Server) merchantShouldFetch(slotStart, now time.Time) bool {
	if !merchantSlotLive(slotStart, now) {
		return false
	}
	// 这里要读 empty:它区分「查过但无货」与「查过且有货」。注意它**不是**「数据里
	// 有没有商品」—— merchantFetch 在第三方返回空时会保留旧货单并返回 empty=true,
	// 但那种情况不写库,故库里的 empty 仍准确表示「该槽存的就是空货单」。
	empty, _, fetchedAt, ok := s.store.GetMerchantSlot(slotStart.Unix())
	if !ok {
		return true
	}
	if now.Sub(slotStart) >= merchantRefetchWin {
		return false
	}
	since := now.Sub(time.Unix(fetchedAt, 0))
	if empty && now.Sub(slotStart) < merchantCatchupWin {
		return since >= merchantCatchupEvery
	}
	return since >= merchantRefetch
}

// merchantFetch 回源第三方并写入槽缓存,顺带清理 2 天前的过期记录。
// 返回 (ok, empty):ok=拿到「第三方正常响应」(有货无货都算,仅网络/HTTP 层失败返回 false,
// 不写库);empty=该槽查过但无货。ok && !empty 时调用方应在锁外触发 merchantNotify。
//
// refresh 对应第三方的 refresh 参数:
// true = 让它绕过自己的缓存直接回源(拿到的是此刻真实货单),false = 拿它可能陈旧的快照。
// 取值策略见 merchantShouldForceRefresh。
func (s *Server) merchantFetch(slotStart time.Time, refresh bool) (bool, bool) {
	src := s.merchantSource()
	// 本轮第几次尝试:整点后第三方滞后切换时,「试了几次才拿到」是判断它是否异常的
	// 唯一依据 —— 只记总耗时看不出「第 4 次才拿到」与「一次命中」的区别。
	try := s.merchantTryInc(slotStart)

	// 两源形态不同,但都返回**同形**的响应体(好游快爆侧做了归一化,见
	// merchant_haoyou.go),故从这里往下无需再区分是哪个源。
	var body string
	var ok bool
	var tm merchantTiming
	if src == merchantSrcHaoyou {
		body, ok, tm = s.fetchHaoyou(slotStart, refresh)
	} else {
		body, ok, tm = s.fetchXianyu(slotStart, refresh)
	}
	if !ok {
		merchantLogFetch(slotStart, src, try, "回源失败", tm)
		return false, false
	}
	// 校验并判定有货/无货:第三方成功码不统一,实测 code=0 与 code=200 都表示成功,
	// 故 code∈{0,200} 且 data.items 非空视为有货,其余(无货/业务错误)记空。
	var out struct {
		Code int `json:"code"`
		Data struct {
			Items []json.RawMessage `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		log.Printf("merchantFetch JSON 解析失败: %v, 响应前 200 字节: %q", err, truncateBytes([]byte(body), 200))
		merchantLogFetch(slotStart, src, try, "JSON解析失败", tm)
		return false, false
	}
	empty := !((out.Code == 0 || out.Code == 200) && len(out.Data.Items) > 0)
	if empty {
		// 重查撞上第三方瞬时抽风(限流/业务错误/响应还没刷新)时,不能把已经展示的好数据
		// 覆盖成空 —— 那会让页面上明明还有的货单整片消失。保留旧货单,只把回源时刻推到
		// 现在(TouchMerchantSlot);不推的话重查冷却立刻失效,下一 tick 又判定该重查,
		// 于是一路回源到窗口结束,白烧 token。
		//
		// 代价是「商品真全下架了仍显示旧货单」。宁可多显示也不要清掉:后者是可见的数据丢失,
		// 前者最多让人多跑一趟。
		if _, old, _, ok := s.store.GetMerchantSlot(slotStart.Unix()); ok && merchantBodyHasItems(old) {
			merchantLogFetch(slotStart, src, try,
				fmt.Sprintf("空货单(保留既有 %d 件)", merchantBodyItemCount(old)), tm)
			if err := s.store.TouchMerchantSlot(slotStart.Unix()); err != nil {
				log.Printf("merchantFetch 刷新槽回源时刻失败: %v", err)
			}
			return true, true
		}
		merchantLogFetch(slotStart, src, try,
			fmt.Sprintf("空货单(code=%d items=%d)", out.Code, len(out.Data.Items)), tm)
	}
	if err := s.store.PutMerchantSlot(slotStart.Unix(), empty, body); err != nil {
		log.Printf("merchantFetch 写槽缓存失败: %v", err)
		return false, false
	}
	// 有货时补一条结果日志:此前只有失败/空货单才记,成功时日志一片空白,于是
	// 「拿到货了没、几点拿到的、比整点晚多久」都查不到,只能靠翻邮件或查库。
	// 空货单(empty)已在上面记过,不在这里重复。
	if !empty {
		merchantLogFetch(slotStart, src, try, fmt.Sprintf("有货 %d 件", len(out.Data.Items)), tm)
	}
	return true, empty
}

// merchantLogFetch 打一行回源结果:本轮第几次尝试 + 距整点多久 + 结果 + 各阶段耗时。
//
// 抽成函数是为了让所有出口(回源失败 / 解析错 / 空 / 有货)共用同一格式 —— 格式一旦
// 不统一,排查时就无法按 slot 把**一整轮的获取过程** grep 出来连着看。
//
// 整点后用 fmtDuration(拆成分秒)而非裸秒数:判断「慢不慢」时分秒直观得多,且它会把
// 负数钳到 0(测试会用未来槽,不钳会出现「整点后=-13108s」这种看着像 bug 的输出)。
func merchantLogFetch(slotStart time.Time, src string, try int, result string, tm merchantTiming) {
	log.Printf("merchantFetch slot=%s 源=%s 尝试#%d 整点后=%s 结果=%s [%s]",
		slotStart.Format("01-02 15:04"), merchantSourceName(src), try,
		fmtDuration(time.Since(slotStart)), result, tm)
}

// merchantBodyItemCount 返回缓存的第三方原始 JSON 里的商品件数(解析不出返回 0)。
// 供「空货单但保留了既有货单」的日志说明保留下来的究竟是几件。
func merchantBodyItemCount(data string) int {
	var out struct {
		Data struct {
			Items []json.RawMessage `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(data), &out); err != nil {
		return 0
	}
	return len(out.Data.Items)
}

// merchantTiming 是单次回源的 HTTP 各阶段耗时(各字段是**分段**值,相加即 Total)。
//
// 存在理由:回源慢的时候,「网络慢」与「第三方服务端慢」的应对完全不同 —— 前者换线路,
// 后者只能等它或改用 refresh=false 拿缓存。只记一个总耗时分不出这两种,而实测里
// 服务端那一段才是大头(咸鱼源 0.33s 里占约 0.29s)。分段口径与 curl -w 的
// time_* 一致,便于两边对照。
type merchantTiming struct {
	DNS   time.Duration // 域名解析
	Conn  time.Duration // TCP 连接建立
	TLS   time.Duration // TLS 握手
	Wait  time.Duration // 请求发出 → 收到首字节(第三方服务端处理)
	Read  time.Duration // 读响应体
	Total time.Duration // 全程
}

// String 输出 "dns=1ms 连接=8ms tls=19ms 服务端=293ms 读取=1ms 总计=322ms"。
func (t merchantTiming) String() string {
	// 连接复用(keep-alive)时 DNS/连接/TLS 三段都不触发、全为零。此时显式写
	// 「连接复用」而不是三个 0.0ms —— 后者会被读成「解析几乎不耗时」,而真相是
	// 这次压根没建连接。两者对优化的意义相反:省掉握手是好事,不该看成一个零值。
	if t.DNS == 0 && t.Conn == 0 && t.TLS == 0 {
		return fmt.Sprintf("连接复用 服务端=%s 读取=%s 总计=%s",
			merchantDur(t.Wait), merchantDur(t.Read), merchantDur(t.Total))
	}
	return fmt.Sprintf("dns=%s 连接=%s tls=%s 服务端=%s 读取=%s 总计=%s",
		merchantDur(t.DNS), merchantDur(t.Conn), merchantDur(t.TLS),
		merchantDur(t.Wait), merchantDur(t.Read), merchantDur(t.Total))
}

// merchantDur 把时长写成便于扫读的形式:不足 1ms 给一位小数,否则取整毫秒。
// 负值钳到 0:分段是相减算出来的,httptrace 的回调在某些路径(连接复用、无 DNS)
// 下不触发,相减得到负数 —— 那种「负耗时」看着像 bug,实际只是该段没走到。
func merchantDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000)
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}

// merchantHTTPGet 发起一次带阶段计时的 GET,返回响应体与各阶段耗时。
//
// timeout 是整体超时;maxBody 是响应体上限(防异常大响应吃内存);headers 是额外
// 请求头(好游快爆源必须带 UA,见 haoyouUA)。
//
// 非 200 时**仍然返回响应体**:HTTP 错误页/错误 JSON 的前两百字节是判断「令牌失效
// 还是接口改版」的唯一线索,丢掉就只能靠猜(见 fetchXianyu 的调用处)。
func merchantHTTPGet(rawURL string, timeout time.Duration, maxBody int64, headers map[string]string) ([]byte, merchantTiming, error) {
	var tm merchantTiming
	start := time.Now()
	var tDNS, tConn, tTLS, tFirst time.Time
	trace := &httptrace.ClientTrace{
		DNSStart:          func(httptrace.DNSStartInfo) { tDNS = time.Now() },
		DNSDone:           func(httptrace.DNSDoneInfo) { tm.DNS = time.Since(tDNS) },
		ConnectStart:      func(_, _ string) { tConn = time.Now() },
		ConnectDone:       func(_, _ string, _ error) { tm.Conn = time.Since(tConn) },
		TLSHandshakeStart: func() { tTLS = time.Now() },
		TLSHandshakeDone:  func(tls.ConnectionState, error) { tm.TLS = time.Since(tTLS) },
		GotFirstResponseByte: func() {
			tFirst = time.Now()
			// Wait 是首字节之前扣掉网络三段后剩下的部分 = 第三方服务端处理。
			tm.Wait = tFirst.Sub(start) - tm.DNS - tm.Conn - tm.TLS
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, tm, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req = req.WithContext(httptrace.WithClientTrace(ctx, trace))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		tm.Total = time.Since(start)
		return nil, tm, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	tm.Total = time.Since(start)
	// 首字节都没收到就失败了(超时/DNS 失败)时 tFirst 是零值,此时不能用它算读耗时。
	if !tFirst.IsZero() {
		tm.Read = time.Since(tFirst)
	}
	if err != nil {
		return nil, tm, err
	}
	if resp.StatusCode != http.StatusOK {
		return body, tm, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return body, tm, nil
}

// merchantTryInc 递增并返回某槽「本轮回源过几次」。
//
// 存在理由:整点后第三方滞后约 1 分钟才切到新一轮(两个源都实测确认),于是切换
// 窗口内的前几次回源**必然拿到空** —— 日志里若没有序号,「第 4 次才拿到」与
// 「一次命中」长得一模一样,而这正是判断「第三方慢」还是「我们没去查」的依据。
// 表只按当前营业日使用,顺手清掉往日条目,不会长驻增长。
func (s *Server) merchantTryInc(slotStart time.Time) int {
	s.merchantTryMu.Lock()
	defer s.merchantTryMu.Unlock()
	if s.merchantTries == nil {
		s.merchantTries = map[int64]int{}
	}
	yesterday := merchantDayStart(time.Now()).AddDate(0, 0, -1).Unix()
	for slot := range s.merchantTries {
		if slot < yesterday {
			delete(s.merchantTries, slot)
		}
	}
	s.merchantTries[slotStart.Unix()]++
	return s.merchantTries[slotStart.Unix()]
}

// fetchXianyu 回源咸鱼源(JSON 接口),返回响应原文与各阶段耗时。签名与 fetchHaoyou
// 一致,供 merchantFetch 按当前源分派。
//
// refresh 对应第三方的 refresh 参数:true = 让它绕过自己的缓存直接回源(拿到的是
// 此刻真实货单),false = 拿它可能陈旧的快照。取值策略见 merchantShouldForceRefresh。
func (s *Server) fetchXianyu(slotStart time.Time, refresh bool) (string, bool, merchantTiming) {
	params := url.Values{}
	params.Add("key", s.eggAPIKey)
	params.Add("format", "json")
	params.Add("refresh", strconv.FormatBool(refresh))

	body, tm, err := merchantHTTPGet(merchantFetchURL+"?"+params.Encode(), 10*time.Second, 1<<20, nil)
	if err != nil {
		log.Printf("merchantFetch 咸鱼源回源失败: %v, 响应前 200 字节: %q", err, truncateBytes(body, 200))
		return "", false, tm
	}
	return string(body), true, tm
}

// truncateBytes 截断字节串用于日志(避免刷屏),超长时加省略号。
func truncateBytes(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

// handleMerchant 返回当前营业日的槽缓存与状态,玩家打开页面时按当前时间补查缺失槽。
// 参数:force=1 强制回源当前可查槽(烧第三方 token,前端「强制刷新」用)。
// 响应结构见 merchantRespJSON。
func (s *Server) handleMerchant(w http.ResponseWriter, r *http.Request) {
	// 只有咸鱼源需要令牌:好游快爆源抓的是公开页面,没令牌也能查 —— 缺令牌时
	// 提示里给出这条路,免得管理员以为服务坏了只能去申请第三方令牌。
	if merchantNeedKey(s.merchantSource()) && s.eggAPIKey == "" {
		http.Error(w, "服务端未配置查询令牌(启动时加 -egg-api-key,或在管理面板切换到无需令牌的好游快爆源)",
			http.StatusServiceUnavailable)
		return
	}
	now := time.Now()
	s.merchantEnsure(now, r.URL.Query().Get("force") == "1")

	day := merchantDayStart(now)
	out := merchantRespJSON{
		Now:    now.Unix(),
		Source: s.merchantSource(),
		Status: merchantDayStatus(now),
	}
	if out.Status == "idle" {
		// 0-8 点打烊:展示刚结束的营业日(昨天 8 点开张那一轮)的全天回顾。
		day = day.AddDate(0, 0, -1)
		out.Prev = s.merchantSlotsOfDay(day)
	} else {
		out.Today = s.merchantSlotsOfDay(day)
	}
	out.Day = day.Format("2006-01-02")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(out)
}

// merchantSlotsOfDay 组装某营业日的 6 个槽:8/12/16/20 四轮读缓存(有货则带 merchant),
// 次日 0/4 两个槽是打烊休市(固定 empty,不查库)。
func (s *Server) merchantSlotsOfDay(day time.Time) []merchantSlotJSON {
	starts := merchantDaySlots(day)
	out := make([]merchantSlotJSON, 0, len(starts))
	for i, st := range starts {
		js := merchantSlotJSON{
			Start: st.Unix(),
			End:   st.Add(merchantSlotStep).Unix(),
			Label: st.Format("15:04") + "~" + st.Add(merchantSlotStep).Format("15:04"),
			Empty: true,
		}
		if i < 4 { // 8/12/16/20 四轮读缓存
			if empty, data, _, ok := s.store.GetMerchantSlot(st.Unix()); ok {
				// 自动修正历史误判:此前 code==200 被当失败写成 empty,读缓存时按原始 body 重新判定有货。
				if empty && merchantBodyHasItems(data) {
					empty = false
				}
				js.Empty = empty
				if !empty {
					js.Merchant = json.RawMessage(data)
				}
			}
		} else { // 次日 0/4 槽:00:00~08:00 打烊休市
			js.Off = true
		}
		out = append(out, js)
	}
	return out
}

// merchantBodyHasItems 判断缓存的第三方原始 JSON 是否实际有货:
// code∈{0,200}(第三方成功码不统一)且 data.items 非空。用于读取缓存时修正历史误判的空标记。
func merchantBodyHasItems(data string) bool {
	var out struct {
		Code int `json:"code"`
		Data struct {
			Items []json.RawMessage `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(data), &out); err != nil {
		return false
	}
	return (out.Code == 0 || out.Code == 200) && len(out.Data.Items) > 0
}

// merchantItem 第三方 items 中订阅邮件需要的字段。
