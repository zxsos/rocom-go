package server

// =============================================================================
// 远行商人「货单滞后探测」—— 临时测试模式,拿到数据后整体删除。
//
// 目的:测量第三方 API 在轮次整点之后,货单从「上一轮」切成「本轮」到底滞后多久。
// 这是 merchantRefetch / merchantRefetchWin 该设多大的唯一依据 —— 现在这两个常量
// (10 分钟 / 90 分钟)是拍的,2026-08-30 那次故障实测滞后 56 分钟,说明拍得不准。
//
// 原理:从档期整点起按 5s/10s 交替高频回源,记录每次拿到的商品指纹。指纹发生变化
// 的那一行就是第三方切单的时刻,其「距整点秒数」即滞后。全程纯记录,不做任何判定。
//
// 删除清单见仓库根目录 AI_merchant_probe.md —— 那边写了要删哪几处、怎么验证删干净了。
// =============================================================================

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// merchantProbeOn 测试模式开关。
//
// 做成**包级** atomic 而不是 Server 字段,是为了让「删掉测试模式」不必改 Server 结构体:
// 删掉本文件 + main.go 的 flag + 三处短路(见下)即可,生产代码零残留。生产只有
// 一个 Server 实例,包级变量的实际影响面与结构体字段完全相同。
// 先例:merchantFetchURL 同样是包级 var(单元测试要换掉它,见 merchant_fetch_test.go)。
var merchantProbeOn atomic.Bool

// probeIntervals 探测间隔序列,循环使用:5s / 10s 交替 → 每 15 秒 2 次 = 每分钟 8 次。
//
// 交替而非固定间隔,是为了在同样的次数预算下让**头 30 秒更密**:滞后若发生在秒级
// (第三方整点立刻刷),固定 10s 会看不清;而固定 5s 又让后半段白白多烧一倍token。
var probeIntervals = [...]time.Duration{5 * time.Second, 10 * time.Second}

const (
	// probeSettle 检测到货单切换后再多探这么久,确认新货单稳定(补货可能分批到达)。
	probeSettle = 5 * time.Minute

	// probeMaxDuration 探测总时长的硬上限:一直没检测到切换时的兜底退出。
	//
	// 取 90 分钟:覆盖 2026-08-30 实测的 56 分钟滞后并留足余量。一轮 90 分钟 × 8 次/分
	// = 720 次请求,只在探测当天发生,可接受。
	probeMaxDuration = 90 * time.Minute

	// probeMaxLag 允许的落后量:单次探测 + 发信耗时超过间隔时会落后于计划时刻,
	// 落后太多说明节奏已经失控,重新对齐到当前时刻而不是补跑一堆迟到的探测。
	probeMaxLag = 30 * time.Second
)

// probeState 一次探测的运行状态。
type probeState struct {
	base     string    // 基线指纹:**首次成功**探测拿到的货单指纹
	switched bool      // 是否已检测到货单切换
	switchAt time.Time // 切换发生的时刻
	n        int       // 探测总次数(含失败的,仅用于收尾日志)
}

// StartMerchantProbe 启动探测。mode:
//   - "auto":对准下一个档期整点(8/12/16/20)开始,服务可以先起着等;
//   - "now" :立即开始(把「现在」当作整点,用于随时手动测一轮)。
//
// 探测在独立 goroutine 里跑,不阻塞服务启动;探测期间页面与订阅提醒照常工作
// (merchantEnsure 的定时回源被短路,回源由探测独占,见 merchant.go)。
func (s *Server) StartMerchantProbe(mode string) {
	merchantProbeOn.Store(true)
	now := time.Now()
	var slot time.Time
	switch mode {
	case "now":
		slot = now
	case "auto":
		slot = nextProbeSlot(now)
		if slot.Sub(now) > 6*time.Hour { // 今天四档都没了(0-8 点打烊),等太久没意义
			log.Printf("远行商人探测: 距下一档期 %v,过久,已放弃。改用 -merchant-probe=now 手动测", slot.Sub(now).Truncate(time.Second))
			merchantProbeOn.Store(false)
			return
		}
	default:
		log.Fatalf("-merchant-probe 只接受 auto / now,收到 %q", mode)
	}
	log.Printf("远行商人探测已启用: 档期 %s(距现在 %v),节奏 5s/10s 交替(8 次/分),最长 %v",
		slot.Format("2006-01-02 15:04:05"), slot.Sub(now).Truncate(time.Second), probeMaxDuration)
	go s.runMerchantProbe(slot)
}

// nextProbeSlot 返回下一个档期开始时刻(8/12/16/20 中晚于 now 的第一个)。
// 今天四档都过了则返回明天 8 点。
func nextProbeSlot(now time.Time) time.Time {
	day := merchantDayStart(now)
	for _, st := range merchantDaySlots(day)[:4] { // 只看前四个上架轮,0/4 点是打烊槽
		if st.After(now) {
			return st
		}
	}
	return merchantDaySlots(day.AddDate(0, 0, 1))[0]
}

// runMerchantProbe 探测主循环:按 probeIntervals 的节奏在**绝对计划时刻**回源,
// 直到检测到货单切换后再稳定 probeSettle,或跑满 probeMaxDuration。
//
// 用绝对计划时刻而非「探测完睡一个间隔」:后者会把 HTTP 与发信的耗时累加进节奏,
// 跑一小时后漂移可达分钟级,「距整点秒数」这一列就失去意义了。
func (s *Server) runMerchantProbe(slot time.Time) {
	// 探测结束(无论正常收尾、跑满上限还是日志建不出来)都必须关掉开关:
	// 开着它 merchantEnsure 的定时补查会被一直短路,服务就再也不会自己回源了,
	// 页面数据从此停滞 —— 这是最容易漏掉的收尾。
	defer merchantProbeOn.Store(false)

	path := fmt.Sprintf("merchant-probe-%s.log", slot.Format("20060102-1504"))
	f, err := os.Create(path)
	if err != nil {
		log.Printf("远行商人探测日志创建失败(%s): %v", path, err)
		return
	}
	defer f.Close()

	fmt.Fprintf(f, "# 远行商人货单滞后探测\n")
	fmt.Fprintf(f, "# 档期整点: %s (Unix %d, 时区 %s)\n", slot.Format("2006-01-02 15:04:05"), slot.Unix(), merchantLoc.String())
	fmt.Fprintf(f, "# 探测节奏: 5s / 10s 交替 = 每分钟 8 次;上限 %v;切换后延测 %v\n", probeMaxDuration, probeSettle)
	fmt.Fprintf(f, "#\n")
	fmt.Fprintf(f, "# 列: 探测时刻 | 距整点(秒) | 回源 | 有货 | 商品数 | 指纹 | time_label | 商品名\n")
	fmt.Fprintf(f, "# 读法: 指纹发生变化的那一行 = 第三方把货单切到本轮的时刻,\n")
	fmt.Fprintf(f, "#       那一行的「距整点」就是滞后秒数,即 merchantRefetchWin 的下界。\n")
	fmt.Fprintf(f, "#       回源=fail 表示网络/HTTP 层失败(该次未写库,指纹沿用上次)。\n\n")

	st := &probeState{}
	plan := slot
	for i := 0; ; i++ {
		if wait := time.Until(plan); wait > 0 {
			time.Sleep(wait)
		} else if lag := -time.Until(plan).Seconds(); lag > probeMaxLag.Seconds() {
			log.Printf("远行商人探测落后计划 %.0fs,重新对齐", lag)
			plan = time.Now()
		}

		now := time.Now()
		s.probeOnce(f, slot, now, st)
		f.Sync() // 探测要跑一小时,崩了也不能丢已采到的数据

		if st.switched && now.After(st.switchAt.Add(probeSettle)) {
			break
		}
		if now.After(slot.Add(probeMaxDuration)) {
			break
		}
		plan = plan.Add(probeIntervals[i%len(probeIntervals)])
	}

	abs, _ := filepath.Abs(path)
	if st.switched {
		log.Printf("远行商人探测结束: 货单在整点后 %.0f 秒切换(%s),共 %d 次探测,日志 %s",
			st.switchAt.Sub(slot).Seconds(), st.switchAt.Format("15:04:05"), st.n, abs)
	} else {
		log.Printf("远行商人探测结束: %v 内未检测到货单切换(滞后超过上限或本轮货单与上一轮相同),共 %d 次探测,日志 %s",
			probeMaxDuration, st.n, abs)
	}
	s.probeSummaryMail(slot, st, abs)
}

// probeSummaryMail 探测结束时给订阅者发一封汇总邮件。
//
// 存在理由:探测期间若第三方迟迟不切单,merchantNotify 的 seen 会把上一轮旧货全挡住,
// 一封新货提醒都发不出来 —— 收件人那边完全没有任何东西可转交,只能去翻日志。
// 汇总邮件无论切没切单都会发,把结论(滞后多少秒)直接送到收件人手上。
//
// 走 s.smtp 直接发而**不经 merchantNotify**:汇总不是新货提醒,不能写进
// merchant_notified 污染商品级去重(否则探测里的商品会被记成「已通知」)。
func (s *Server) probeSummaryMail(slot time.Time, st *probeState, logPath string) {
	if !s.smtp.configured() {
		log.Printf("远行商人探测汇总: SMTP 未配置,结论见日志 %s", logPath)
		return
	}
	subs, err := s.store.ListMerchantSubs()
	if err != nil {
		log.Printf("远行商人探测汇总: 读取订阅名单失败: %v(结论见日志 %s)", err, logPath)
		return
	}
	if len(subs) == 0 {
		log.Printf("远行商人探测汇总: 无订阅者,跳过邮件(结论见日志 %s)", logPath)
		return
	}

	var b strings.Builder
	b.WriteString("远行商人货单滞后探测汇总\n\n")
	b.WriteString("- 探测档期: " + slot.Format("2006-01-02 15:04") + "(北京时间)\n")
	b.WriteString("- 探测次数: " + fmt.Sprintf("%d 次", st.n) + "(5s/10s 交替,8 次/分)\n")
	if st.switched {
		lag := st.switchAt.Sub(slot)
		b.WriteString("- 结论: 货单在整点后 " + fmtDuration(lag) + " 切换\n")
		b.WriteString("- 切换时刻: " + st.switchAt.Format("15:04:05") + "\n")
		b.WriteString("\n第三方滞后的真实值就是上面这个数。它是 merchantRefetch /\n")
		b.WriteString("merchantRefetchWin 该设多大的唯一依据(见 merchant.go)。\n")
	} else {
		b.WriteString("- 结论: " + probeMaxDuration.String() + " 内未检测到货单切换\n")
		b.WriteString("\n两种可能:第三方滞后超过探测上限(需调大 probeMaxDuration 重测),或\n")
		b.WriteString("本轮货单与上一轮完全相同。区分方法看日志的 time_label 列:若始终停在\n")
		b.WriteString("上一轮的时段标签,就是滞后;若已变成本轮标签但商品名集合没变,就是相同。\n")
	}
	b.WriteString("\n- 原始时间线: " + logPath + "\n")
	b.WriteString("\n本邮件由探测模式自动发出,不代表新货上架,也不影响订阅提醒。\n")

	// 用 sendMerchantMail(纯文本入口)而非 sendMerchantMailHTML:前者内部会调
	// merchantMailBody 套上邮件模板,后者**假定传进来的已经是完整 HTML**、不再套。
	// 两个都调会把模板嵌套两次(正文里冒出第二个金色标题栏)。
	// 正文里 "- " 开头的行会被 merchantMailBody 渲染成项目符号。
	subject := "[探测汇总] 远行商人货单滞后 " + slot.Format("01-02 15:04") + " 档"
	for _, sub := range subs {
		if err := s.smtp.sendMerchantMail(sub.Email, subject, b.String()); err != nil {
			log.Printf("远行商人探测汇总 发信失败 to=%s: %v", sub.Email, err)
		} else {
			log.Printf("远行商人探测汇总 已发往 %s", sub.Email)
		}
	}
}

// probeOnce 执行一次探测:回源 → 记录一行日志 → 有货则走订阅提醒(新商品才发信)。
//
// 回源与 merchantEnsure 共用 s.merchantMu,故探测与页面请求不会并发打第三方。
// 这里**绕过 merchantShouldFetch 的冷却与窗口判定**(测试模式下该函数被短路,
// 见 merchant.go):探测的目的就是无视这些拍脑袋的常量,按固定节奏无脑采样。
func (s *Server) probeOnce(f *os.File, slot, now time.Time, st *probeState) {
	s.merchantMu.Lock()
	ok, empty := s.merchantFetch(slot)
	s.merchantMu.Unlock()

	var names []string
	var label string
	var n int
	if _, data, _, has := s.store.GetMerchantSlot(slot.Unix()); has {
		n, names, label = probeSummarize(data)
	}
	fp := probeFingerprint(names)

	st.n++
	if !ok {
		// 回源失败不写库,指纹无从比对,单独记一行(不设基线 —— 基线只能来自成功探测,
		// 否则「第一次失败 + 第二次成功」会被误判成切单)。
		fmt.Fprintf(f, "%s | %+7.1f | fail |     |       |        |              |\n",
			now.Format("15:04:05.000"), now.Sub(slot).Seconds())
		return
	}
	// 基线取**首次成功**探测的指纹;之后的每一次与它比对,变了就是切单。
	// 用 base == "" 判定而非 n == 1:上面失败的那次也计了 n,计数与「成功次数」不是一回事。
	if st.base == "" {
		st.base = fp
	}
	mark := " "
	if fp != st.base {
		mark = "*" // 与基线不同
		if !st.switched {
			st.switched, st.switchAt = true, now
			log.Printf("远行商人探测: 货单在整点后 %.0f 秒切换(%s),商品 %d 件",
				now.Sub(slot).Seconds(), now.Format("15:04:05"), n)
		}
	}
	goods := "无货"
	if !empty {
		goods = "有货"
	}
	fmt.Fprintf(f, "%s%s | %+7.1f | ok   | %s | %5d | %s | %s | %s\n",
		now.Format("15:04:05.000"), mark, now.Sub(slot).Seconds(), goods, n, fp,
		padRight(label, 12), strings.Join(names, ","))

	if !empty {
		// 商品级去重(merchant_notified)+ 本营业日更早轮的 seen 都在 merchantNotify 里,
		// 探测拿到的若仍是上一轮货单,会被 seen 挡住不发信 —— 只在真正切到本轮新货时才打扰订阅者。
		// merchantClaim 的认领冷却在测试模式下被短路(见 merchant_notify.go),否则切换后
		// 陆续补到的商品会被 10 分钟冷却挡住,探测就白测了。
		s.merchantNotify(slot)
	}
}

// probeSummarize 从第三方原始 JSON 里提取 (商品数, 商品名有序列表, 首个商品的 time_label)。
//
// 商品名**排序**后返回:第三方返回顺序不稳定,直接拼接会让同一份货单算出不同指纹,
// 切单判定就废了。time_label 是判定货单属于哪个时段的关键(见方案第二层),一并记录。
func probeSummarize(data string) (int, []string, string) {
	var out struct {
		Data struct {
			Items []struct {
				Name      string `json:"name"`
				TimeLabel string `json:"time_label"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(data), &out); err != nil {
		return 0, nil, ""
	}
	names := make([]string, 0, len(out.Data.Items))
	label := ""
	for i, it := range out.Data.Items {
		if it.Name != "" {
			names = append(names, it.Name)
		}
		if i == 0 {
			label = it.TimeLabel
		}
	}
	sort.Strings(names)
	return len(out.Data.Items), names, label
}

// probeFingerprint 商品名集合的短指纹(排序后取 sha256 前 6 位十六进制)。
func probeFingerprint(names []string) string {
	if len(names) == 0 {
		return "------" // 无货也占位,保持日志列对齐、便于肉眼扫
	}
	sum := sha256.Sum256([]byte(strings.Join(names, "\x00")))
	return hex.EncodeToString(sum[:])[:6]
}

// padRight 按**显示宽度**(中文算 2)右填充空格,用于日志列对齐。
func padRight(s string, width int) string {
	pad := width - displayWidth(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}

// displayWidth 字符串的终端显示宽度:CJK 与全角标点算 2,其余算 1。
func displayWidth(s string) int {
	var n int
	for _, r := range s {
		switch {
		case r >= 0x1100 && (r <= 0x115F || r == 0x2329 || r == 0x232A ||
			(r >= 0x2E80 && r <= 0xA4CF && r != 0x303F) ||
			(r >= 0xAC00 && r <= 0xD7A3) || (r >= 0xF900 && r <= 0xFAFF) ||
			(r >= 0xFE30 && r <= 0xFE6F) || (r >= 0xFF00 && r <= 0xFF60) ||
			(r >= 0xFFE0 && r <= 0xFFE6)):
			n += 2
		default:
			n++
		}
	}
	return n
}
