package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// 家园查询:按 uid 查**任意玩家**的家园快照,回源 rocodex.org 的免费接口。
//
// 与 GET /api/home 是两回事,别混:
//
//	/api/home       自己家园的小窝图层与下蛋配对,数据来自抓包(见 scene/home.go)
//	/api/home/query 任意 uid 的家园快照,回源第三方(本文件),与抓包无关
//
// 上游按 clientId 限额(每天 3 次),但身份可以现领:先 GET /quota 取 Set-Cookie 里的
// clientId,再带着它 POST /query —— 新身份的额度是满的。不领 cookie 直接查,
// 查到第 4 个 uid 就开始失败。这是本文件唯一反直觉的地方。
//
// 上游没有契约保证(免费站,随时可能加验证或改字段),故本接口**只作增强不做依赖**:
// 拿不到数据时页面照常可用,不会牵连抓包与主流程。
var homeQueryAPI = "https://rocodex.org"

const (
	homeQuotaPath = "/api/home-query/quota"
	homeQueryPath = "/api/home-query/query"

	// homeQueryTTL 本地缓存时长。上游自己也缓存约 20 分钟(响应里的 cacheExpiresAt),
	// 这里取 15 分钟:既免掉重复回源,又不会比上游更陈旧。
	homeQueryTTL = 15 * time.Minute

	// homeQueryTimeout 单次回源上限。两步请求(领身份 + 查询)合计不得超过它。
	homeQueryTimeout = 20 * time.Second

	// homeCacheMax 缓存的 uid 上限。接口不鉴权,被人换着 uid 刷会无界增长;
	// 满了丢最旧的一条(查询对象是随机的,做 LRU 不划算)。
	homeCacheMax = 256
)

// homeCacheItem 是某个 uid 最近一次回源的**原始**响应。
//
// 存原始字节而非归一化结果:归一化要用本地名称库补中文名与头像,缓存成品的话
// 名称库更新后旧缓存仍是旧名字;存原始响应则每次命中都按当前库重新翻译。
// 代价只是每命中一次多解一遍 JSON(单份几 KB,可忽略)。
type homeCacheItem struct {
	at   time.Time
	body []byte
}

// homeQueryOut 是按 uid 查询家园的响应契约。
type homeQueryOut struct {
	UID       string `json:"uid"`
	HomeName  string `json:"homeName"`
	HomeLevel uint32 `json:"homeLevel"`
	RoomLevel uint32 `json:"roomLevel"`
	Comfort   uint32 `json:"comfort"`
	Exp       uint32 `json:"exp"` // 家园经验(上游 homeExperience)

	Pets      []homeQueryPet   `json:"pets"`
	Plants    []homeQueryPlant `json:"plants"`
	Cached    bool             `json:"cached"`    // 是否命中本地缓存(force=1 时恒为假)
	FetchedAt int64            `json:"fetchedAt"` // 本份数据回源时刻(Unix 秒)
}

// homeQueryPet 是一只驻守精灵。
//
// base 与 form 的区别是理解上游数据的关键,别弄反:
//
//	base  品种 petbase_id —— **与本地 gamedata 同一套 id**,故能直接查名/查图
//	form  形态 id(上游的 gid)—— 同品种不同形态各有一个,炫彩/异色绑在这一层
//
// 实测(uid 5678116):base 3123→雪影娃娃、3121→大耳帽兜、3189→酷拉,与
// names.json 的 species 表逐条对得上;而 3121 这个 base 同时有 53264/54509
// 两个 form。早期版本曾误把 gid 当 petbase_id,结果所有名字都查不到。
type homeQueryPet struct {
	Base     uint32 `json:"base"`
	Form     uint32 `json:"form"`
	Name     string `json:"name"`    // 玩家起的昵称
	Species  string `json:"species"` // 品种名(本地名称表;查不到时回落上游 defaultName)
	Level    uint32 `json:"level"`
	Gender   string `json:"gender,omitempty"`   // 雄性/雌性
	Mutation string `json:"mutation,omitempty"` // 异色/异色炫彩;普通个体为空
	Status   string `json:"status,omitempty"`   // 未喂食/已喂食/可收取灵感;未收录的状态码为空
	Head     string `json:"head,omitempty"`     // 小头像(相对 /img/);品种未知时为空。**异色个体给异色图**
	Book     uint32 `json:"book,omitempty"`     // 图鉴编号(排序用)
	Feed     uint32 `json:"feed,omitempty"`     // 喂食轮次(上游 feedRound)。**不是时间**,语义待确认
}

// homeStatusName 把上游 status 数字翻成游戏里的中文说法。
//
// 三个取值由玩家在游戏内比对确认(2026-09,uid 100000002 与 5678116 两份样本):
//
//	1700 未喂食   1701 已喂食   1702 可收取灵感
//
// 未收录的码一律返回空串:上游自己的 statusText 会把认不出的码拼成「状态 1700」
// 这种半吊子文案,显示给玩家毫无意义 —— 宁可留空,也别把内部枚举当文案给人看。
func homeStatusName(code uint32) string {
	switch code {
	case 1700:
		return "未喂食"
	case 1701:
		return "已喂食"
	case 1702:
		return "可收取灵感"
	}
	return ""
}

// homeQueryPlant 是一株作物。
//
// 只有 ripeAt 没有「是否已成熟」:与其猜一个可能错的布尔量,不如只给时刻让前端
// 自己去比。宁可少给一个字段,也别给一个错的字段。
//
// 关于上游的 state:现已观察到两个取值 —— 1=生长中(harvestNum 0、ripeAt 在未来)、
// 2=可收获(harvestNum>0、ripeAt 已过),两者与 ripeAt 自洽。但样本只有这两种,
// 仍不足以定死语义,故不据此下发布尔字段(比对 ripeAt 与当前时刻即可得同样结论)。
type homeQueryPlant struct {
	SeedName string `json:"seedName"`
	Harvest  uint32 `json:"harvest"` // 产量
	RipeAt   int64  `json:"ripeAt"`  // 成熟时刻(Unix 秒);0=上游没给或格式不认
	CanSteal uint32 `json:"canSteal"`
	Stolen   uint32 `json:"stolen"`
}

// homeQueryResp 是上游响应的外层结构(仅用于解析,不透传前端)。
type homeQueryResp struct {
	FromCache bool `json:"fromCache"` // 上游是否返回了它自己的缓存
	Data      struct {
		UID              string             `json:"uid"`
		HomeName         string             `json:"homeName"`
		RoomLevel        uint32             `json:"roomLevel"`
		HomeLevel        uint32             `json:"homeLevel"`
		ComfortLevel     uint32             `json:"comfortLevel"`
		HomeExperience   uint32             `json:"homeExperience"` // 家园经验
		ResidentPetCount uint32             `json:"residentPetCount"`
		PlantCount       uint32             `json:"plantCount"`
		Pets             []homeQueryUpPet   `json:"pets"`
		Plants           []homeQueryUpPlant `json:"plants"`
	} `json:"data"`
}

// homeQueryUpPet 是上游的一只驻守精灵。
type homeQueryUpPet struct {
	ID           uint32 `json:"id"`          // 品种 petbase_id
	Gid          uint32 `json:"gid"`         // 形态 id
	Name         string `json:"name"`        // 玩家昵称
	DefaultName  string `json:"defaultName"` // 品种默认名(本地查不到时兜底)
	Level        uint32 `json:"level"`
	GenderText   string `json:"genderText"`
	MutationName string `json:"mutationName"` // 异色/异色炫彩/空
	Status       uint32 `json:"status"`       // 1700 未喂食 / 1701 已喂食 / 1702 可收取灵感
	FeedRound    uint32 `json:"feedRound"`    // 喂食轮次(不是时间,语义待确认)
}

// homeQueryUpPlant 是上游的一株作物。
type homeQueryUpPlant struct {
	SeedName        string `json:"seedName"`
	HarvestNum      uint32 `json:"harvestNum"`
	RipeAt          string `json:"ripeAt"` // RFC3339
	CanStealAccount uint32 `json:"canStealAccount"`
	StolenAccount   uint32 `json:"stolenAccount"`
}

// handleHomeQuery 按 uid 查询任意玩家的家园快照。
//
// 参数:
//
//	uid=    必填的非空纯数字(不限位数:实测有 6 位与 9 位)
//	force=1 可选,跳过本地缓存强制回源
//
// 回源失败时若手上有旧缓存,**降级返回旧数据**而不是报错:上游是免费站,
// 偶尔抽风是常态,而一份 15 分钟前的家园快照对使用者仍然有用。
// 只有在从没成功过时才 502。
func (s *Server) handleHomeQuery(w http.ResponseWriter, r *http.Request) {
	uid := r.URL.Query().Get("uid")
	if !homeUIDOK(uid) {
		http.Error(w, "uid 必须是纯数字", http.StatusBadRequest)
		return
	}

	if r.URL.Query().Get("force") != "1" {
		if it, ok := s.homeCacheGet(uid); ok && time.Since(it.at) < homeQueryTTL {
			s.writeHomeQuery(w, it.body, it.at, true)
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), homeQueryTimeout)
	defer cancel()
	body, err := homeQueryFetch(ctx, uid)
	if err != nil {
		if it, ok := s.homeCacheGet(uid); ok {
			s.writeHomeQuery(w, it.body, it.at, true) // 见上方注释:过期数据好过报错
			return
		}
		http.Error(w, "查询家园失败: "+err.Error(), http.StatusBadGateway)
		return
	}
	now := time.Now()
	s.homeCacheSet(uid, body)
	s.writeHomeQuery(w, body, now, false)
}

// writeHomeQuery 把上游原始响应翻译成对外契约并写出。
func (s *Server) writeHomeQuery(w http.ResponseWriter, body []byte, at time.Time, cached bool) {
	var up homeQueryResp
	if err := json.Unmarshal(body, &up); err != nil {
		http.Error(w, "上游响应解析失败: "+err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, s.homeQueryNormalize(up, at, cached))
}

// homeQueryNormalize 归一化:补本地中文名与头像,丢弃用不上的字段。
//
// 这是接第三方源唯一值得做的一步 —— 直接用上游的 name(玩家昵称)会满屏
// 「牢大」「秋天」认不出物种,而它的 icon 是外链、随时可能挂。换成
// 本地 gamedata 后,名字与头像都跟着仓库里的解包数据走,不依赖对方。
func (s *Server) homeQueryNormalize(up homeQueryResp, at time.Time, cached bool) homeQueryOut {
	d := up.Data
	pets := make([]homeQueryPet, 0, len(d.Pets))
	for _, p := range d.Pets {
		it := homeQueryPet{
			Base:     p.ID,
			Form:     p.Gid,
			Name:     p.Name,
			Level:    p.Level,
			Gender:   p.GenderText,
			Mutation: p.MutationName,
			Status:   homeStatusName(p.Status), // 上游的 statusText 认不出的码会拼成「状态 1700」,不用它
			Feed:     p.FeedRound,
			Species:  p.DefaultName, // 兜底:本地查不到时至少显示上游给的默认名
		}
		if b, ok := s.db.PetBase(p.ID); ok {
			it.Species = b.Name
			it.Book = b.Book
			// 异色(含异色炫彩)取异色头像:全库仅 19% 的品种有专属异色图,
			// 没有的由 imageOf 自动回退普通图,不会开天窗。
			it.Head = s.db.PetImageByBase(p.ID, strings.Contains(p.MutationName, "异色")).Head
		}
		pets = append(pets, it)
	}

	plants := make([]homeQueryPlant, 0, len(d.Plants))
	for _, p := range d.Plants {
		it := homeQueryPlant{
			SeedName: p.SeedName,
			Harvest:  p.HarvestNum,
			CanSteal: p.CanStealAccount,
			Stolen:   p.StolenAccount,
		}
		if t, err := time.Parse(time.RFC3339, p.RipeAt); err == nil {
			it.RipeAt = t.Unix()
		}
		plants = append(plants, it)
	}

	return homeQueryOut{
		UID:       d.UID,
		HomeName:  d.HomeName,
		HomeLevel: d.HomeLevel,
		RoomLevel: d.RoomLevel,
		Comfort:   d.ComfortLevel,
		Exp:       d.HomeExperience,
		Pets:      pets,
		Plants:    plants,
		Cached:    cached,
		FetchedAt: at.Unix(),
	}
}

// homeQueryFetch 回源一次,返回上游原始 JSON。
//
// 两步必须同 cookie:先领身份、再用它查。中间的 quota 响应体不用读(只要它的
// Set-Cookie),但**必须读完并关闭**才能复用连接;这里直接丢弃。
func homeQueryFetch(ctx context.Context, uid string) ([]byte, error) {
	const ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	base := map[string]string{
		"User-Agent": ua,
		"Referer":    homeQueryAPI + "/home-query",
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, homeQueryAPI+homeQuotaPath, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range base {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("领取查询身份失败: %w", err)
	}
	// Set-Cookie 可能带多个属性段(clientId=...; Path=/; Max-Age=...),只取第一段。
	cookie := strings.Split(resp.Header.Get("Set-Cookie"), ";")[0]
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	resp.Body.Close()

	// 直接把已校验为纯数字的 uid 拼进 JSON,不做 ParseUint:
	// uid 位数不定(实测有 6 位与 9 位),超长的转 uint64 会溢出报错,
	// 而那不是用户的错 —— 上游自己会判,判不了再由它给 400。
	// 拼接安全的前提是 homeUIDOK 已确保 uid 只含 0-9。
	payload := `{"uid":` + uid + `}`
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, homeQueryAPI+homeQueryPath, strings.NewReader(payload))
	if err != nil {
		return nil, err
	}
	for k, v := range base {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("查询家园失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 上限 1MB,防异常大响应
	if err != nil {
		return nil, fmt.Errorf("读取上游响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, homeUpstreamError(resp.StatusCode, body)
	}
	// 别把错误页当成数据:上游过载时可能返回 200 + HTML,先确认能解析再往下走。
	var probe homeQueryResp
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil, fmt.Errorf("上游响应不是约定结构: %w", err)
	}
	return body, nil
}

// homeUpstreamError 把上游的非 200 响应翻成人话。
//
// 存在理由:上游 422 会返回一大坨 JSON(带它自己的 URL、statusCode、quota 明细),
// 原样透传就是给用户甩一屏内部细节 —— 实测玩家遇到的是「少输一位 uid」这种
// 再普通不过的失误,却要读一整段 {error:true,url:…,data:{quota:{…}}}。
//
// 上游的 statusMessage 质量参差,分三种处理:
//   - 400「请输入正确的玩家 UID」:中文人话,直接采用;
//   - 422「Unprocessable Entity」:英文废话,换成我们自己的文案;
//   - 其余(5xx 等):不猜,给带状态码的兜底,且**不附原始 body**。
//
// ⚠️ 不把 body 拼进错误:那会再次把上游内部细节漏给前端。
func homeUpstreamError(status int, body []byte) error {
	var up struct {
		StatusMessage string `json:"statusMessage"`
	}
	_ = json.Unmarshal(body, &up) // 解析失败就当没有,走下面的兜底

	// 422:格式像 uid 但查不到这个人(实测 8 位的 90612933 即此;9 位的 100000002 正常)。
	if status == http.StatusUnprocessableEntity {
		return errors.New("查不到该玩家的家园数据,请确认 UID 是否完整正确")
	}
	if msg := strings.TrimSpace(up.StatusMessage); msg != "" && status < 500 {
		return fmt.Errorf("%s(%d)", msg, status)
	}
	return fmt.Errorf("上游暂时不可用(%d)", status)
}

// homeUIDOK 校验 uid 是否为**非空纯数字**。
//
// 刻意**不校验位数**:uid 有 6 位也有 9 位(玩家实测),位数不是我们能假设的 ——
// 早先卡 1-20 位、以及前端提示「少于 8 位似乎不完整」,都会把合法的老 uid 拦下,
// 而真正该判的是上游(它对查不到的 uid 给 422、对格式错的给 400,两条都已有
// 中文文案)。这里只挡「不是数字」—— 因为 uid 会直接拼进上游请求体。
func homeUIDOK(uid string) bool {
	if uid == "" {
		return false
	}
	for _, c := range uid {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (s *Server) homeCacheGet(uid string) (homeCacheItem, bool) {
	s.homeCacheMu.Lock()
	defer s.homeCacheMu.Unlock()
	it, ok := s.homeCache[uid]
	return it, ok
}

func (s *Server) homeCacheSet(uid string, body []byte) {
	s.homeCacheMu.Lock()
	defer s.homeCacheMu.Unlock()
	if len(s.homeCache) >= homeCacheMax {
		oldest := ""
		var oldestAt time.Time
		for k, v := range s.homeCache {
			if oldest == "" || v.at.Before(oldestAt) {
				oldest, oldestAt = k, v.at
			}
		}
		delete(s.homeCache, oldest)
	}
	s.homeCache[uid] = homeCacheItem{at: time.Now(), body: body}
}
