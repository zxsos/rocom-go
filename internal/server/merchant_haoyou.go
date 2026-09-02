package server

// 远行商人「好游快爆源」:抓 onebiji(好游快爆 / 快爆工具箱的远行商人页面),
// 解析出**请求的那一档**的商品,归一化成与咸鱼源同形的 envelope 交给 merchantFetch 入库。
//
// 为什么必须归一化:库表 merchant_slots.data 存的是第三方原始 JSON,而
// merchantBodyHasItems(有货判定)、merchantNotify(订阅邮件)与前端 format.js 的
// unwrap 都按 `code∈{0,200} && data.items` 这一种壳来读。让两种格式并存的话这四处
// 都要分支判断,漏掉任何一处都是静默错乱(编译能过、页面看着也像那么回事)。
// 故这里产出与咸鱼源同形的 JSON,换取下游四处零改动 —— 归一化是本次唯一新增的复杂度。
//
// 页面语义(2026-09-02 整点实测,结论与原始时间线已沉淀进 docs/data.md):
//   - 页面一次给出**一个营业日**的若干档:每个商品条目所在的档由 data-time 标定,
//     它是**该档的结束时间(Unix 秒)**,开始时间 = 结束 − 4h;
//   - 只有**已结束的营业日**才四档齐全;当天只给已开市的档 —— 它不能预告未来档;
//   - 换天发生在开市整点(8:00)后约 30~60 秒,期间页面解析为 0 件,是正常的切换过程
//     (由 merchantFetch 的「空响应保留旧货单」保护兜住,**不要**当成站点改版去告警)。
//
// 与咸鱼源相比:无需 token、公开可抓。商品图与类别**都有**(图是 biligame patchwiki
// 的 100px 缩略图外链,类别是中文原样),字段并不比咸鱼源少 —— 少的只有「本轮倒计时」
// 与商人名等少数几项。商品的文字描述页面也给了(showShopinfo 第 4 个参数),但
// merchantItem 没有对应字段,暂不落库。

import (
	"context"
	"encoding/json"
	"fmt"
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
	// haoyouTimeout 抓取超时。页面较大(实测百 KB ~ 1MB 量级,随当天商品数浮动),
	// 比咸鱼源那个 JSON 接口(10s)略放宽。
	haoyouTimeout = 15 * time.Second
	haoyouMaxBody = 16 << 20 // 响应体上限,防异常大页把内存吃光
)

// haoyouURL 是页面地址(公开,无需鉴权)。
//
// 做成 var 而非 const:fetchHaoyou 直接打这个地址,单元测试必须能用 httptest 换掉
// (真地址会打到线上第三方站点,且返回什么取决于当时的货单,断言没法写)。
var haoyouURL = "https://www.onebiji.com/hykb_tools/comm/lkwgmerchant/preview.php?id=1&immgj=0"

// haoyouUA 抓取时带的 User-Agent。
//
// 不能省:部分站点对空 UA 直接返回 403,而「抓取失败」与「页面没数据」在日志里长得
// 一模一样,不查根因会误判成站点改版。
const haoyouUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/120"

var (
	// 页面结构(参考 kanzakine/Roco-API):每个商品是一个 class 含 all_show 的元素,
	// data-time 是**该档期的结束时间(Unix 秒)**;内部 shop_name / shop_price / em。
	//
	// ⚠️ `(?s)` 不能省:Go 的正则里 `.` **默认不匹配换行**,而这个块的内部
	// (商品名/价格/限购)是换行的。少了它会解析出 0 个档期,症状与"站点改版"
	// 一模一样 —— 极易误判。
	reHaoyouBlock = regexp.MustCompile(
		`(?s)<[^>]*class="all_show[^"]*"[^>]*data-time="(\d+)"[^>]*>(.*?)</li>`)
	reHaoyouName  = regexp.MustCompile(`class="shop_name[^"]*"[^>]*>([^<]+)<`)
	reHaoyouPrice = regexp.MustCompile(`class="shop_price[^"]*"[^>]*>([^<]+)<`)
	reHaoyouLimit = regexp.MustCompile(`<em>([^<]*)</em>`)

	// 商品图:必须限定在 gitem 内取 —— 块里**有两个** <img>,第二个是价格旁边那枚
	// 洛克贝图标(//res.3839pic.com/.../icon.png)。不限定的话会把币种图标当成商品图,
	// 页面上每件商品都显示同一个金币图标,而价格、名字全对,极难察觉。
	reHaoyouImg = regexp.MustCompile(`class="gitem"[^>]*>\s*<img[^>]*src="([^"]+)"`)

	// 商品类别:只在行内 onclick 的 showShopinfo 参数里(第 3 个参数)。
	// 只取前三个参数(图/名/类别)而不匹配到右括号:第 4 个是商品描述,里面可能出现
	// 单引号,匹配到它会截断出错。
	reHaoyouInfo = regexp.MustCompile(`showShopinfo\('([^']*)','([^']*)','([^']*)'`)
)

// haoyouGood 是页面上的一个商品条目(各项都是页面原文,未归一化)。
type haoyouGood struct {
	name  string
	price string
	limit string
	image string // 商品图,100px 缩略图外链(biligame patchwiki)
	kind  string // 商品类别,页面给的就是中文(如「炼金材料」),无需再翻译
}

// haoyouSlot 是页面上的一个档期(含该档的商品)。
type haoyouSlot struct {
	start, end time.Time
	goods      []haoyouGood
}

// haoyouLoc 游戏内档期按北京时间(页面上的 data-time 是秒级 Unix 时间戳,
// 换不换时区都一样,故这里固定 +8 而不依赖服务器本地时区 —— 云服务器常默认 UTC)。
var haoyouLoc = time.FixedZone("UTC+8", 8*3600)

// parseHaoyou 解析页面,返回按档期开始时间升序排列的档期列表。
//
// data-time 是**档期结束时间**,开始时间减 4 小时得到。用 data-time 而非页面上的
// 文字标签,是因为它是 Unix 时间戳,不受站点文案/时区表述影响。
func parseHaoyou(body []byte) []haoyouSlot {
	const slotLen = 4 * time.Hour
	byEnd := map[int64]*haoyouSlot{}
	var order []int64

	for _, m := range reHaoyouBlock.FindAllSubmatch(body, -1) {
		endSec, err := strconv.ParseInt(string(m[1]), 10, 64)
		if err != nil {
			continue
		}
		name := reHaoyouName.FindSubmatch(m[2])
		if name == nil {
			continue
		}
		g := haoyouGood{name: string(name[1])}
		if p := reHaoyouPrice.FindSubmatch(m[2]); p != nil {
			g.price = strings.TrimSpace(string(p[1]))
		}
		if l := reHaoyouLimit.FindSubmatch(m[2]); l != nil {
			g.limit = strings.TrimSpace(string(l[1]))
		}
		if img := reHaoyouImg.FindSubmatch(m[2]); img != nil {
			g.image = strings.TrimSpace(string(img[1]))
		}
		if info := reHaoyouInfo.FindSubmatch(m[0]); info != nil {
			g.kind = strings.TrimSpace(string(info[3]))
		}
		sl, ok := byEnd[endSec]
		if !ok {
			end := time.Unix(endSec, 0).In(haoyouLoc)
			sl = &haoyouSlot{start: end.Add(-slotLen), end: end}
			byEnd[endSec], order = sl, append(order, endSec)
		}
		sl.goods = append(sl.goods, g)
	}
	// 按结束时间升序输出,让同一档期的商品归到一起、档期顺序稳定。
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	out := make([]haoyouSlot, 0, len(order))
	for _, k := range order {
		out = append(out, *byEnd[k])
	}
	return out
}

// haoyouPrice 把页面上的价格文案转成整数(洛克贝)。
//
// 页面为显示紧凑把大数写成 "16w"(=160000),而前端直接渲染 `{it.price} 洛克贝`
// —— 不归一化的话页面上会显示「16w 洛克贝」。故这里必须把 w 单位拆开算回整数。
// 解析不出来时返回 0(页面少显示一个价格,而不是整条商品消失)。
func haoyouPrice(text string) int {
	s := strings.TrimSpace(text)
	s = strings.TrimPrefix(s, "价格：") // 页面价格是「价格：500」这种带前缀的文案
	s = strings.TrimPrefix(s, "价格:")
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if n := len(s); n > 1 && (s[n-1] == 'w' || s[n-1] == 'W') {
		v, err := strconv.ParseFloat(s[:n-1], 64)
		if err != nil {
			return 0
		}
		return int(v * 10000)
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return v
}

// haoyouLimit 从「限购100」这类文案里取出数字,取不到返回 0(不限购)。
func haoyouLimit(text string) int {
	var b strings.Builder
	for _, r := range strings.TrimSpace(text) {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	v, err := strconv.Atoi(b.String())
	if err != nil {
		return 0
	}
	return v
}

// haoyouTimeLabel 生成商品时段标签("08:00-12:00")。
//
// 末档(20:00 至次日 00:00)必须写成 "20:00-24:00":前端 format.js 的 SLOTS 与后端
// merchantMailSlots 都以 "20:00-24:00" 作为标准时段键,写成 "20:00-00:00" 虽然能过
// 格式校验,却匹配不上任何标准段 —— 商品会掉进「其他时段」栏,看起来像数据错了。
func haoyouTimeLabel(start, end time.Time) string {
	e := end.Format("15:04")
	if e == "00:00" {
		e = "24:00"
	}
	return start.Format("15:04") + "-" + e
}

// haoyouItem 把页面条目归一化成一个商品项。
//
// Image 填外链原样:merchantItem.Image 的约定就是「http(s) 外链原样,否则按本站
// /img/ 相对路径解析」,前端 imgSrc 与邮件 merchantMailItemImg 都按这条走(外链在
// 邮件里不进 CID 内嵌,直接输出 URL)。
//
// 代价要说清:引用外链意味着订阅者的邮件客户端与玩家的浏览器都会去向 biligame 要图
// —— 邮件客户端普遍会拦外链图(显示为裂图),且对方改路径或下线我们就跟着失效。
// 要彻底解决得把图抓回来存本地(/img/ 相对路径那套),那是另一件事,这里先直连。
func haoyouItem(g haoyouGood, start, end time.Time) merchantItem {
	return merchantItem{
		Name:      strings.TrimSpace(g.name),
		Kind:      strings.TrimSpace(g.kind),
		Price:     haoyouPrice(g.price),
		Limit:     haoyouLimit(g.limit),
		TimeLabel: haoyouTimeLabel(start, end),
		StartTime: start.UnixMilli(), // 毫秒(真实 Unix 毫秒),与咸鱼源口径一致
		EndTime:   end.UnixMilli(),
		Image:     haoyouImageURL(g.image),
	}
}

// haoyouImageURL 规范商品图地址。
//
// 页面上还混着 `//res.3839pic.com/...` 这种**协议相对** URL(目前只出在洛克贝图标上,
// 但站点若把商品图也改成这种写法,这里就是唯一要改的地方):不补协议的话前端
// imgSrc 的 `/^https?:\/\//` 判不出来,会被当成本地相对路径拼成 `/img///res...`。
func haoyouImageURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "//") {
		return "https:" + s
	}
	return s
}

// haoyouEnvelope 归一化后的响应体:与咸鱼源同形,故下游判定/邮件/前端均可直接读。
type haoyouEnvelope struct {
	Code int `json:"code"`
	Data struct {
		ItemCount    int            `json:"item_count"`
		MerchantName string         `json:"merchant_name"`
		Items        []merchantItem `json:"items"`
	} `json:"data"`
}

// haoyouEnvelopeJSON 构造归一化响应体。goods 为空时输出 items:[] —— 下游按
// items 非空判有货(merchantBodyHasItems),空数组会被正确判成「查过但无货」。
func haoyouEnvelopeJSON(goods []haoyouGood, start, end time.Time) (string, error) {
	var env haoyouEnvelope
	env.Code = 200
	env.Data.MerchantName = "远行商人" // 这个源没有商人名,用通用名兜底
	env.Data.Items = make([]merchantItem, 0, len(goods))
	for _, g := range goods {
		env.Data.Items = append(env.Data.Items, haoyouItem(g, start, end))
	}
	env.Data.ItemCount = len(env.Data.Items)
	b, err := json.Marshal(env)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// fetchHaoyou 抓取页面并返回归一化后的响应体。签名与 fetchXianyu 一致,
// 供 merchantFetch 按当前源分派。
//
// refresh 参数对 HTML 页面无意义:页面不带缓存参数,每次请求拿到的就是它当前的内容,
// 且它自带的 data-time 时间戳本身就是权威的档期标识(等价于强制回源),故忽略。
//
// 返回 (body, ok):ok=false 表示抓取失败(网络/HTTP/归一化出错),调用方按
// 「第三方不可用」处理、不写库;ok=true 时 body 一定可被下游解析(可能是空货单)。
func (s *Server) fetchHaoyou(slotStart time.Time, refresh bool) (string, bool) {
	_ = refresh

	page, err := haoyouFetchPage()
	if err != nil {
		log.Printf("merchantFetch 好游快爆源抓取失败: %v", err)
		return "", false
	}
	slots := parseHaoyou(page)
	if len(slots) == 0 {
		// 两种可能:换天空窗(整点后约 30~60 秒,实测)或站点结构变了。
		// 在此无法区分,故只记录、按「该档无货」返回 —— 由 merchantFetch 的
		// 空响应保护兜住(保留旧货单),不会把已有数据清掉。
		log.Printf("merchantFetch 好游快爆源解析到 0 件商品: 站点结构变了(选择器失效)或正在换天空窗")
	}
	for _, sl := range slots {
		if !sl.start.Equal(slotStart) {
			continue
		}
		out, err := haoyouEnvelopeJSON(sl.goods, sl.start, sl.end)
		if err != nil {
			log.Printf("merchantFetch 好游快爆源归一化失败: %v", err)
			return "", false
		}
		return out, true
	}
	// 页面上没有请求的这一档:休市时段(0-8 点)页面只显示昨天全天,当天的档要等
	// 开市后才出现。这是「该槽此刻无货」而非故障,故按 ok=true + 空货单返回。
	out, err := haoyouEnvelopeJSON(nil, slotStart, slotStart.Add(merchantSlotStep))
	if err != nil {
		log.Printf("merchantFetch 好游快爆源归一化失败: %v", err)
		return "", false
	}
	return out, true
}

// haoyouFetchPage 抓取页面原文(带 UA、超时与体积上限)。
func haoyouFetchPage() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), haoyouTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, haoyouURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", haoyouUA)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, haoyouMaxBody))
}
