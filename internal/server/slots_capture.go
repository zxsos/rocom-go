package server

// 远行商人「定点密集抓取」临时模式:对准下一个档期整点(8/12/16/20),
// 整点前 2 分钟起每 30 秒抓一次 onebiji 页面,到整点后 7 分钟停,
// **每条日志都带抓取时刻**,不做任何判断、不触发通知、不写数据库。
//
// 目的:onebiji(快爆工具箱)这个源一次给出全天四档、每档带精确时间戳
// (data-time = 该档结束时间)。若它在整点前就给出了下一档,就能绕开现用
// xianyuw API 的缓存滞后(实测 6~56 分钟,见 AI_merchant_probe.md)。
// 要回答的是:**下一档商品第一次出现在什么时刻** —— 故需要分钟级密集采样,
// 且每条记录都必须带时间,否则事后无法判断。
//
// 为什么只打日志不落文件:结论只需要「哪一刻出现了什么」,日志足够;
// 存原始页面反而要处理路径与磁盘占用(单页约 47KB,一天四档近百次请求)。
// 需要原始页面时用 scripts/probe_onebiji.py 单独抓。
//
// 与 -merchant-probe 的区别:那个是「抢单」(砸我们现用的 API,拿到就停、
// 会触发订阅提醒);这个是「观察第三方页面长什么样」,只读、不改变任何业务状态。
//
// 删除清单(本模式是临时验证,结论出来后整体移除):
//   - 本文件
//   - cmd/rocom-capture/main.go 的 -slots-capture flag 与调用
//   - scripts/probe_onebiji.py、scripts/capture_slots.py
//   - docs/merchant-onebiji-probe.md(或转成正式说明并入 docs/data.md)
// 详见 docs/merchant-onebiji-probe.md。

import (
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// 观测窗口:整点前 2 分钟(看页面会不会提前给出下一档)~ 整点后 7 分钟。
	// 提前量按用户指定的 07:58~08:07 定;四档(8/12/16/20)通用。
	slotsBefore = 2 * time.Minute
	slotsAfter  = 7 * time.Minute
	slotsEvery  = 30 * time.Second
)

// onebijiURL 是要观察的第三方页面(公开,无需鉴权)。
const onebijiURL = "https://www.onebiji.com/hykb_tools/comm/lkwgmerchant/preview.php?id=1&immgj=0"

var (
	// 页面结构(参考 kanzakine/Roco-API):每个商品是一个 class 含 all_show 的元素,
	// data-time 是**该档期的结束时间(Unix 秒)**;内部 shop_name / shop_price / em。
	//
	// ⚠️ `(?s)` 不能省:Go 的正则里 `.` **默认不匹配换行**,而这个块的内部
	// (商品名/价格/限购)是换行的。少了它会解析出 0 个档期,症状与"站点改版"
	// 一模一样 —— 极易误判。Python 版靠 re.S 达到同样效果。
	reSlotBlock = regexp.MustCompile(
		`(?s)<[^>]*class="all_show[^"]*"[^>]*data-time="(\d+)"[^>]*>(.*?)</li>`)
	reSlotName  = regexp.MustCompile(`class="shop_name[^"]*"[^>]*>([^<]+)<`)
	reSlotPrice = regexp.MustCompile(`class="shop_price[^"]*"[^>]*>([^<]+)<`)
	reSlotLimit = regexp.MustCompile(`<em>([^<]*)</em>`)
)

// onebijiGood 是页面上的一个商品条目。
type onebijiGood struct {
	name  string
	price string
	limit string
}

// StartSlotsCapture 启动定点抓取。在独立 goroutine 跑,不阻塞服务启动。
//
// 服务部署后即可挂着等:若现在距下一档还早(比如凌晨部署等 8 点),
// goroutine 会一直睡到整点前 2 分钟,期间不占用任何资源。
func (s *Server) StartSlotsCapture() {
	slot := nextProbeSlot(time.Now())
	from, to := slot.Add(-slotsBefore), slot.Add(slotsAfter)
	log.Printf("远行商人档期抓取已启用: 目标档期 %s,窗口 %s ~ %s,每 %v 一次(只读观察,不影响业务)",
		slot.Format("2006-01-02 15:04"), from.Format("15:04:05"), to.Format("15:04:05"), slotsEvery)
	go s.runSlotsCapture(from, to)
}

// runSlotsCapture 在窗口内按固定间隔抓取并打印。
//
// **不做任何判断** —— 此前在 Python 脚本里加过「是否包含当前档」的判定,
// 结果拿早已开市的档期去测时算出「滞后 346 分钟」这种假结论(见
// docs/merchant-onebiji-probe.md 第 8 节)。本模式只负责如实记录每个时刻
// 页面上有什么,结论由人(或事后脚本)从日志里读。
func (s *Server) runSlotsCapture(from, to time.Time) {
	if wait := time.Until(from); wait > 0 {
		time.Sleep(wait)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	for now := time.Now(); !now.After(to); now = time.Now() {
		s.captureOnebiji(client, now)
		next := time.Now().Add(slotsEvery)
		if next.After(to) {
			break
		}
		time.Sleep(time.Until(next))
	}
	log.Printf("远行商人档期抓取结束: 窗口 %s ~ %s 已走完",
		from.Format("15:04:05"), to.Format("15:04:05"))
}

// captureOnebiji 抓一次并打印:抓取时刻 + 该页面上的全部档期与商品。
func (s *Server) captureOnebiji(client *http.Client, now time.Time) {
	stamp := now.Format("15:04:05")
	// 必须带 UA:部分站点对空 UA 直接返回 403,而"抓取失败"与"页面没数据"
	// 在日志里长得一样,不查根因会误判成站点改版。
	req, err := http.NewRequest(http.MethodGet, onebijiURL, nil)
	if err != nil {
		log.Printf("[档期抓取 %s] 构造请求失败: %v", stamp, err)
		return
	}
	req.Header.Set("User-Agent",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/120")
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[档期抓取 %s] 抓取失败: %v", stamp, err)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		log.Printf("[档期抓取 %s] 读取响应失败: %v", stamp, err)
		return
	}

	slots := parseOnebiji(body)
	if len(slots) == 0 {
		log.Printf("[档期抓取 %s] 解析到 0 件商品 —— 站点结构变了(选择器失效)或该时段无数据", stamp)
		return
	}
	for _, sl := range slots {
		log.Printf("[档期抓取 %s] 档期 %s ~ %s:%d 件", stamp,
			sl.start.Format("01-02 15:04"), sl.end.Format("15:04"), len(sl.goods))
		for _, g := range sl.goods {
			log.Printf("[档期抓取 %s]     %s %s %s", stamp, g.name, g.price, g.limit)
		}
	}
}

// onebijiSlot 是页面上的一个档期(含该档的商品)。
type onebijiSlot struct {
	start, end time.Time
	goods      []onebijiGood
}

// parseOnebiji 解析页面,返回按档期开始时间排序的档期列表。
//
// data-time 是**档期结束时间**,开始时间减 4 小时得到。用 data-time 而非
// 页面上的文字标签,是因为它是 Unix 时间戳,不受站点文案/时区表述影响。
func parseOnebiji(body []byte) []onebijiSlot {
	const slotLen = 4 * time.Hour
	loc := time.FixedZone("UTC+8", 8*3600) // 游戏内档期按北京时间
	byEnd := map[int64]*onebijiSlot{}
	var order []int64

	for _, m := range reSlotBlock.FindAllSubmatch(body, -1) {
		endSec, err := strconv.ParseInt(string(m[1]), 10, 64)
		if err != nil {
			continue
		}
		name := reSlotName.FindSubmatch(m[2])
		if name == nil {
			continue
		}
		g := onebijiGood{name: string(name[1])}
		// 页面里价格是「价格：500」这种带前缀的文案,去掉前缀只留数字 ——
		// 日志是要人工比对时间线的,多一列无用文案只会碍眼。
		if p := reSlotPrice.FindSubmatch(m[2]); p != nil {
			g.price = strings.TrimPrefix(
				strings.TrimSpace(string(p[1])), "价格：")
		}
		if l := reSlotLimit.FindSubmatch(m[2]); l != nil {
			g.limit = string(l[1])
		}
		sl, ok := byEnd[endSec]
		if !ok {
			end := time.Unix(endSec, 0).In(loc)
			sl = &onebijiSlot{start: end.Add(-slotLen), end: end}
			byEnd[endSec], order = sl, append(order, endSec)
		}
		sl.goods = append(sl.goods, g)
	}
	// 按结束时间升序输出,让日志里档期的先后顺序稳定
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	out := make([]onebijiSlot, 0, len(order))
	for _, k := range order {
		out = append(out, *byEnd[k])
	}
	return out
}
