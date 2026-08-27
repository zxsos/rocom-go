package server

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	mathrand "math/rand"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/whoisnian/rocom-capture/internal/pet"
)

// 管理员认证:隐式面板(前端导航不显示,需手动输入 #/admin)。
// 首次进入引导设置密码(存 PBKDF2-SHA256 哈希),之后凭密码登录,成功签发内存令牌(服务重启即失效)。

const (
	adminIter    = 600_000 // PBKDF2 迭代次数
	adminSaltN   = 16      // 盐字节数
	adminTokenN  = 32      // 令牌字节数
	adminMinPass = 4       // 密码最短长度
)

// hashAdminPassword 用 PBKDF2-SHA256 派生密码哈希,输出 "pbkdf2$iter$saltB64$hashB64"。
func hashAdminPassword(pw string) (string, error) {
	salt := make([]byte, adminSaltN)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	sum, err := pbkdf2.Key(sha256.New, pw, salt, adminIter, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2$%d$%s$%s", adminIter,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(sum)), nil
}

// verifyAdminPassword 校验密码与存储哈希是否一致。
func verifyAdminPassword(pw, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter <= 0 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// authed 校验请求是否携带当前有效管理员令牌。
func (s *Server) authed(r *http.Request) bool {
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	return s.adminToken != "" && subtle.ConstantTimeCompare(
		[]byte(s.adminToken), []byte(r.Header.Get("X-Admin-Token"))) == 1
}

func newAdminToken() string {
	b := make([]byte, adminTokenN)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// handleAdminStatus 返回密码是否已配置、当前是否已登录。
func (s *Server) handleAdminStatus(w http.ResponseWriter, r *http.Request) {
	configured, err := s.store.AdminConfigured()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"configured": configured, "authed": s.authed(r)})
}

// handleAdminSetup 首次设置管理员密码(已配置则拒绝)。
func (s *Server) handleAdminSetup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	configured, err := s.store.AdminConfigured()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if configured {
		http.Error(w, "already configured", 409)
		return
	}
	if len(req.Password) < adminMinPass {
		http.Error(w, "password too short", 400)
		return
	}
	hash, err := hashAdminPassword(req.Password)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := s.store.SetAdminPassword(hash); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	token := newAdminToken()
	s.adminMu.Lock()
	s.adminToken = token
	s.adminMu.Unlock()
	writeJSON(w, map[string]any{"token": token})
}

// handleAdminLogin 密码登录,签发内存令牌。
func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	stored := s.store.AdminPassHash()
	if stored == "" || !verifyAdminPassword(req.Password, stored) {
		http.Error(w, "wrong password", 401)
		return
	}
	token := newAdminToken()
	s.adminMu.Lock()
	s.adminToken = token
	s.adminMu.Unlock()
	writeJSON(w, map[string]any{"token": token})
}

// handleAdminLogout 注销管理员会话。
func (s *Server) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	if !s.authed(r) {
		http.Error(w, "unauthorized", 401)
		return
	}
	s.adminMu.Lock()
	s.adminToken = ""
	s.adminMu.Unlock()
	writeJSON(w, map[string]any{"ok": true})
}

// handleAdminPlaceholder 管理员面板占位接口(其余功能待实现,统一走此处扩展)。
func (s *Server) handleAdminPlaceholder(w http.ResponseWriter, r *http.Request) {
	if !s.authed(r) {
		http.Error(w, "unauthorized", 401)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "placeholder": true, "now": time.Now().Unix()})
}

// handleAdminStats 返回全部成员抓捕情况的图表数据(近30天时间轴,按账号聚合)。
func (s *Server) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	st, err := s.store.AdminStats()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, st)
}

// handleAdminPlaySessions 返回游玩记录(管理后台「游玩记录」):会话明细列表 + 汇总
// (当前在线/今日/近14天每日)。查询参数:account=账号过滤、limit=条数(默认200,上限1000)。
func (s *Server) handleAdminPlaySessions(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	acc := strings.TrimSpace(r.URL.Query().Get("account"))
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	sessions, err := s.store.ListPlaySessions(acc, limit)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	summary, err := s.store.PlaySessionSummary()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"sessions": sessions, "summary": summary})
}

// handleAdminEggStats 返回查蛋 API(第三方图鉴)使用统计:累计/今日次数、成功率、
// 近 14 天每日、按账号排行、最近明细。keySet 告知服务端是否配置 -egg-api-key。
func (s *Server) handleAdminEggStats(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	st, err := s.store.EggQueryStats()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	st.KeySet = s.eggAPIKey != ""
	writeJSON(w, st)
}

// requireAdmin 校验管理员会话,未登录则回 401 并返回 false。
func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !s.authed(r) {
		http.Error(w, "unauthorized", 401)
		return false
	}
	return true
}

// handleAdminRules 列出全部黑白名单规则。
func (s *Server) handleAdminRules(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	rules, err := s.store.ListRules()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"rules": rules})
}

// handleAdminRuleSet 新增/更新一条黑白名单规则。
func (s *Server) handleAdminRuleSet(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var req struct {
		Account string `json:"account"`
		Mode    string `json:"mode"`
		Note    string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	req.Account = strings.TrimSpace(req.Account)
	if req.Account == "" {
		http.Error(w, "account required", 400)
		return
	}
	if req.Mode != "black" && req.Mode != "white" {
		http.Error(w, "mode must be black or white", 400)
		return
	}
	if err := s.store.SetRule(req.Account, req.Mode, req.Note); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleAdminRuleDelete 删除一条黑白名单规则(?account=xxx)。
func (s *Server) handleAdminRuleDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	acc := strings.TrimSpace(r.URL.Query().Get("account"))
	if acc == "" {
		http.Error(w, "account required", 400)
		return
	}
	if err := s.store.DeleteRule(acc); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleAdminWildPetOptions 列出可投放的野生宠物形态(管理员面板下拉用)。
func (s *Server) handleAdminWildPetOptions(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	writeJSON(w, map[string]any{"options": s.db.WildPetOptions()})
}

// handleAdminListInjects 列出当前全部注入中的精灵(管理面板撤销用)。
// 只读 injects 内存态,不落盘;玩家换场景或靠近 10 米 10 秒后会自动消失,列表随之减少。
func (s *Server) handleAdminListInjects(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	s.injectMu.Lock()
	out := make([]map[string]any, 0)
	for acc, list := range s.injects {
		for _, e := range list {
			name, _ := e.mark["n"].(string)
			kinds, _ := e.mark["kinds"].([]string)
			out = append(out, map[string]any{
				"account":  acc,
				"id":       e.id,
				"name":     name,
				"kinds":    kinds,
				"sceneRes": e.sceneRes,
				"created":  e.created.Unix(),
				"kind":     e.kind, // wild=野生精灵 / flower=花种
			})
		}
	}
	s.injectMu.Unlock()
	// 按账号、投放时间排序,方便管理面板分组查看。
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		aa, _ := a["account"].(string)
		bb, _ := b["account"].(string)
		if aa != bb {
			return aa < bb
		}
		ta, _ := a["created"].(int64)
		tb, _ := b["created"].(int64)
		return ta < tb
	})
	writeJSON(w, map[string]any{"injects": out})
}

// injectEntry 是一只已注入的稀有野生精灵(管理员投放,有生命周期)。
type injectEntry struct {
	id       string // 前端标记 id,前缀 admin-inject-
	account  string // 所属账号
	sceneRes int32  // 投放时所在场景(花种注入恒为 0)
	x, y     int32  // 投放世界坐标(厘米),用于距离判定(花种注入恒为 0)
	mark     map[string]any // 广播给前端的标记载荷(花种注入仅作记录,不广播 wildpets)
	created  time.Time      // 投放时刻
	nearSec  int            // 距离玩家 <10 米累计的秒数(连续靠近 10 秒触发自动撤销)
	kind          string // 注入类型:wild=野生精灵 / flower=花种
	flowerLogicID uint64 // kind=flower 时花种 npc_logic_id,撤销时据此从花种分组删除
}

// randRange 返回 [lo, hi] 闭区间的随机整数。注入精灵的个体值(嗓音/身高/体重)用它取
// 合法范围内的随机值,模拟真实野生精灵的个体差异;hi <= lo 时返回 lo(配置异常兜底)。
func randRange(lo, hi int32) int32 {
	if hi <= lo {
		return lo
	}
	return lo + int32(mathrand.Int63n(int64(hi)-int64(lo)+1))
}

// 靠近判定阈值:玩家距注入精灵 < nearMeters 米且持续 nearTotalSec 秒即自动消失。
const (
	injectNearMeters  = 10.0
	injectNearTotalSec = 10
	injectSweepInterval = 2 * time.Second // sweep 周期
)

// handleAdminInjectWild 向指定成员的实时地图注入一只稀有野生精灵(异色/炫彩)。
// 位置取该账号最近一次缓存位置,按场景投影到 u/v;广播一条 wildpets 消息给前端,
// 前端地图页与现有野生宠标记一起渲染。不修改游戏真实流量、不影响抓包库。
// 生命周期:管理员可主动撤销(DELETE),或玩家连续靠近 10 米内 10 秒后自动消失。
func (s *Server) handleAdminInjectWild(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var req struct {
		Account      string `json:"account"`
		Base         uint32 `json:"base"`
		Kind         string `json:"kind"`         // shiny | colorful
		OffsetMeters int32  `json:"offsetMeters"` // 投放点距玩家位置的米数(默认 30)
		Level        int32  `json:"level"`        // 注入精灵等级(0=随机,取 30-60)
		GlassType    int32  `json:"glassType"`    // 炫彩色卡类型(kind=colorful 时生效:1=普通 2=隐藏;0=随机)
		GlassValue   int32  `json:"glassValue"`   // 炫彩色卡数值(普通=(粒子id<<20)|配色id;隐藏=1/2/3 赛季、1000)
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	req.Account = strings.TrimSpace(req.Account)
	if req.Account == "" {
		http.Error(w, "account required", 400)
		return
	}
	if req.Base == 0 {
		http.Error(w, "base required", 400)
		return
	}
	if req.Kind != "shiny" && req.Kind != "colorful" {
		http.Error(w, "kind must be shiny or colorful", 400)
		return
	}
	if req.OffsetMeters == 0 {
		req.OffsetMeters = 30
	}
	if req.OffsetMeters < 1 || req.OffsetMeters > 200 {
		http.Error(w, "offsetMeters out of range (1-200)", 400)
		return
	}

	s.posMu.Lock()
	pos := s.lastPos[req.Account]
	s.posMu.Unlock()
	if pos == nil {
		http.Error(w, "该账号暂无已知位置,请等其进入有底图的场景后再投放", 400)
		return
	}
	sceneRes, ok := pos["sceneResId"].(int32)
	if !ok || sceneRes == 0 {
		http.Error(w, "该账号当前场景无 scene_res 信息", 400)
		return
	}
	x, _ := pos["x"].(int32)
	y, _ := pos["y"].(int32)
	z, _ := pos["z"].(int32)
	// 投放点取玩家位置为球心、半径=设定距离的球面上随机一点:xyz 三轴随机加减偏移,
	// 与玩家的距离恰为设定值,而非固定往右下角飘。
	off := float64(req.OffsetMeters) * 100 // 厘米
	theta := mathrand.Float64() * 2 * math.Pi
	phi := math.Acos(2*mathrand.Float64() - 1)
	wx := x + int32(math.Round(off*math.Sin(phi)*math.Cos(theta)))
	wy := y + int32(math.Round(off*math.Sin(phi)*math.Sin(theta)))
	wz := z + int32(math.Round(off*math.Cos(phi)))
	u, v, ok := s.db.Project(uint32(sceneRes), wx, wy)
	if !ok {
		http.Error(w, "当前场景无底图,无法投放(地图页没有可投影的坐标)", 400)
		return
	}

	info, ok := s.db.PetBase(req.Base)
	if !ok {
		http.Error(w, "unknown petbase", 400)
		return
	}
	// 异色投放要求该形态确有可用的异色小头像,否则 mutation 标了异色但头像仍是普通,
	// 前端看起来像「异色没生效」。前端下拉已过滤,这里兜底再校验一次。
	if req.Kind == "shiny" && !s.db.HasShinyImage(req.Base) {
		http.Error(w, "该形态没有异色形态,无法投放异色", 400)
		return
	}
	// 炫彩投放取普通小头像,要求该形态确有可用的普通小头像,否则注入后地图标记显示不出图。
	if req.Kind == "colorful" && !s.db.HasHeadImage(req.Base) {
		http.Error(w, "该形态没有可用的头像,无法投放", 400)
		return
	}
	mutation := int32(0)
	if req.Kind == "shiny" {
		mutation = 1 // scene.MutationShiny
	}
	glassType := int32(0)
	glassValue := int32(0)
	if req.Kind == "colorful" {
		// 炫彩色卡设置:管理员可指定类型+数值(普通=粒子×配色打包,隐藏=赛季/黑白);
		// 都不传(0/0)时随机一个合法色卡,模拟真实炫彩的多样性。
		glassType, glassValue = req.GlassType, req.GlassValue
		if glassType == 0 && glassValue == 0 {
			glassType, glassValue = s.db.RandGlass()
		} else if !s.db.GlassValid(glassType, glassValue) {
			http.Error(w, "invalid glass color card", 400)
			return
		}
	}
	// 嗓音/身高/体重用合法范围内的随机个体值,模拟真实野生精灵的个体差异
	// (固定取 0 或中值会让每只假精灵都一样,一眼假)。
	weight := randRange(int32(info.WeightLow), int32(info.WeightHigh))
	height := randRange(int32(info.HeightLow), int32(info.HeightHigh))
	voice := randRange(-100, 100)
	// 等级同样随机化:管理员未指定(0)时在 30-60 常见野生等级内随机,
	// 避免所有注入精灵都固定同级(一眼假)。显式指定则校验 1-100。
	if req.Level == 0 {
		req.Level = randRange(30, 60)
	} else if req.Level < 1 || req.Level > 100 {
		http.Error(w, "level out of range (1-100)", 400)
		return
	}

	id := "admin-inject-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	mark := map[string]any{
		"id":       id,
		"n":        info.Name,
		"img":      s.db.PetImageByBase(req.Base, req.Kind == "shiny").Head,
		"kinds":    []string{req.Kind},
		"u":        u,
		"v":        v,
		"x":        wx,
		"y":        wy,
		"z":        wz,
		"lv":       req.Level,
		"voice":    voice,
		"height":   height,
		"weight":   weight,
		"mutation": mutation,
		"inject":   true, // 前端据此显示撤销按钮与视觉提示
	}
	// 体重百分位与真实野生宠同一口径(pet.SizePercentile),前端资料卡才能显示「体重 xx%」。
	if info.WeightHigh > info.WeightLow {
		if pct := pet.SizePercentile(float64(weight)/1000,
			float64(info.WeightLow)/1000, float64(info.WeightHigh)/1000); pct != nil {
			mark["weightPct"] = *pct
		}
	}
	if req.Kind == "colorful" {
		glassDesc := s.db.GlassDesc(glassType, glassValue)
		if glassDesc == "" {
			glassDesc = "炫彩"
		}
		mark["glass"] = glassDesc
		mark["glassType"] = glassType
		mark["glassValue"] = glassValue
	}

	// 记入 injects(生命周期管理),并合并进 lastWild 缓存供回显。
	entry := &injectEntry{
		id: id, account: req.Account, sceneRes: sceneRes,
		x: wx, y: wy, mark: mark, created: time.Now(), kind: "wild",
	}
	s.injectMu.Lock()
	s.injects[req.Account] = append(s.injects[req.Account], entry)
	s.injectMu.Unlock()

	s.posMu.Lock()
	cur, _ := s.lastWild[req.Account].(map[string]any)
	if cur == nil {
		cur = map[string]any{
			"account": req.Account, "sceneResId": sceneRes,
			"pets": []map[string]any{}, "allPets": []map[string]any{},
		}
	}
	pets, _ := cur["pets"].([]map[string]any)
	if pets == nil {
		pets = []map[string]any{}
	}
	pets = append(pets, mark)
	cur["pets"] = pets
	s.lastWild[req.Account] = cur
	s.posMu.Unlock()

	s.hub.Broadcast("wildpets", req.Account, map[string]any{
		"account": req.Account, "sceneResId": sceneRes,
		"pets": []map[string]any{mark}, "allPets": []map[string]any{},
		"inject": true,
	})
	writeJSON(w, map[string]any{"ok": true, "id": id, "u": u, "v": v})
}

// handleAdminInjectFlower 向指定成员的花种页注入一只假炫彩花种(花灵 BOSS,默认 7 星特殊花种,
// 星级可自定义,随机血脉/等级,携带管理员指定或随机的炫彩色卡)。不修改游戏真实流量:直接把花种插入
// server 缓存的最远花种分组并广播 flowers,花种页立即显示,与真实花种卡片无异。
// 生命周期:仅由管理员主动撤销(记入 injects,kind=flower,花种不在地图上,无靠近/换场景判定)。
func (s *Server) handleAdminInjectFlower(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var req struct {
		Account    string `json:"account"`
		Base       uint32 `json:"base"`      // 守护宠物 petbase id
		Star       uint32 `json:"star"`      // 花种星级 1-7;0=默认 7
		GlassType  int32  `json:"glassType"` // 炫彩色卡类型(1=普通 2=隐藏;0=随机)
		GlassValue int32  `json:"glassValue"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	req.Account = strings.TrimSpace(req.Account)
	if req.Account == "" {
		http.Error(w, "account required", 400)
		return
	}
	if req.Base == 0 {
		http.Error(w, "base required", 400)
		return
	}
	star := req.Star
	if star == 0 {
		star = 7
	} else if star > 7 {
		http.Error(w, "star must be 1-7", 400)
		return
	}
	info, ok := s.db.PetBase(req.Base)
	if !ok {
		http.Error(w, "unknown petbase", 400)
		return
	}
	if !s.db.HasHeadImage(req.Base) {
		http.Error(w, "该形态没有可用的头像,无法投放", 400)
		return
	}
	// 炫彩色卡设置:同野生精灵投放,指定类型+数值或随机一个合法色卡。
	glassType, glassValue := req.GlassType, req.GlassValue
	if glassType == 0 && glassValue == 0 {
		glassType, glassValue = s.db.RandGlass()
	} else if !s.db.GlassValid(glassType, glassValue) {
		http.Error(w, "invalid glass color card", 400)
		return
	}
	glassDesc := s.db.GlassDesc(glassType, glassValue)
	if glassDesc == "" {
		glassDesc = "炫彩"
	}
	head := s.db.PetImageByBase(req.Base, false).Head
	blood := uint32(randRange(1, 24))
	now := time.Now()
	// 分组跟随星级:7 星归特殊花种组(specSeedId>0),1-6 星按普通花种组(specSeedId=0)。
	specSeedID := uint32(0)
	if star == 7 {
		specSeedID = 1
	}
	f := FlowerItem{
		ID:          req.Base,
		Name:        info.Name,
		Img:         head,
		Star:        star,
		Blood:       blood,
		BloodName:   s.db.BloodName(blood),
		BloodIcon:   s.db.BloodIcon(blood),
		NpcLogicID:  uint64(now.UnixNano()),
		EndTs:       uint64(now.Add(3 * 24 * time.Hour).Unix()), // 3 天活动倒计时
		SpecSeedID:  specSeedID,
		ActivityID:  1,
		Detail:      true,
		Lv:          uint32(randRange(40, 70)),
		GlassType:   glassType,
		Glass:       glassDesc,
		GlassValue:  glassValue,
		BindName:    info.Name,
		BindImg:     head,
	}
	s.InjectFlowerItem(req.Account, f)

	id := "admin-inject-" + strconv.FormatInt(now.UnixNano(), 36)
	s.injectMu.Lock()
	s.injects[req.Account] = append(s.injects[req.Account], &injectEntry{
		id: id, account: req.Account, mark: map[string]any{
			"id": id, "n": info.Name + "(花种)", "kinds": []string{"colorful"},
			"glass": glassDesc, "glassType": glassType, "glassValue": glassValue,
		},
		created: now, kind: "flower", flowerLogicID: f.NpcLogicID,
	})
	s.injectMu.Unlock()
	writeJSON(w, map[string]any{"ok": true, "id": id, "npcLogicId": f.NpcLogicID})
}

// handleAdminRevokeInject 撤销一只注入精灵(?account=xxx&id=yyy)。
// 从 injects 与 lastWild 缓存里删掉,再广播一条 wildpets(不含该 id)让前端清掉标记。
func (s *Server) handleAdminRevokeInject(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	acc := strings.TrimSpace(r.URL.Query().Get("account"))
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if acc == "" || id == "" {
		http.Error(w, "account and id required", 400)
		return
	}
	if !s.removeInject(acc, id) {
		http.Error(w, "not found", 404)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// removeInject 删除一只注入精灵,同步清理 lastWild 缓存并广播当前列表。
// 找到并删除返回 true。
func (s *Server) removeInject(acc, id string) bool {
	s.injectMu.Lock()
	list := s.injects[acc]
	var removed injectEntry
	found := false
	for i, e := range list {
		if e.id == id {
			removed = *e
			list = append(list[:i], list[i+1:]...)
			if len(list) == 0 {
				delete(s.injects, acc)
			} else {
				s.injects[acc] = list
			}
			found = true
			break
		}
	}
	s.injectMu.Unlock()
	if !found {
		return false
	}
	// 花种注入:从花种分组删除并广播 flowers(不涉及地图位置/缓存)。
	if removed.kind == "flower" {
		if removed.flowerLogicID != 0 {
			s.RemoveFlowerItem(acc, removed.flowerLogicID)
		}
		return true
	}
	s.posMu.Lock()
	cur, _ := s.lastWild[acc].(map[string]any)
	if cur != nil {
		pets, _ := cur["pets"].([]map[string]any)
		out := make([]map[string]any, 0, len(pets))
		for _, p := range pets {
			if p["id"] != id {
				out = append(out, p)
			}
		}
		cur["pets"] = out
		sceneRes, _ := cur["sceneResId"].(int32)
		s.posMu.Unlock()
		s.hub.Broadcast("wildpets", acc, map[string]any{
			"account": acc, "sceneResId": sceneRes,
			"pets": out, "allPets": []map[string]any{},
			"injectRevoke": id, // 前端按 id 立即撤掉该标记
		})
	} else {
		s.posMu.Unlock()
	}
	return true
}

// sweepInjects 周期检查注入精灵生命周期:玩家连续靠近 10 米内 10 秒 → 自动撤销。
// 也清理「玩家已换场景」的注入(不再可见,留着没意义)。
func (s *Server) sweepInjects() {
	ticker := time.NewTicker(injectSweepInterval)
	defer ticker.Stop()
	for range ticker.C {
		// 取快照避免长持锁。
		s.injectMu.Lock()
		type snap struct{ acc, id string; x, y int32; scene int32 }
		var todo []snap
		var revoke []struct{ acc, id string }
		for acc, list := range s.injects {
			// 玩家位置(只读缓存)。
			s.posMu.Lock()
			pos := s.lastPos[acc]
			s.posMu.Unlock()
			var px, py int32
			var pScene int32
			if pos != nil {
				px, _ = pos["x"].(int32)
				py, _ = pos["y"].(int32)
				pScene, _ = pos["sceneResId"].(int32)
			}
			for _, e := range list {
				// 花种注入不在地图上,不参与靠近/换场景生命周期,只由管理员主动撤销。
				if e.kind == "flower" {
					continue
				}
				// 换场景:撤销(标记无法在新场景投影,留着会错位)。
				if pScene != 0 && pScene != e.sceneRes {
					revoke = append(revoke, struct{ acc, id string }{acc, e.id})
					continue
				}
				if pos == nil {
					continue
				}
				dx := float64(px-e.x) / 100.0 // 厘米→米
				dy := float64(py-e.y) / 100.0
				dist := dx*dx + dy*dy
				if dist <= injectNearMeters*injectNearMeters {
					e.nearSec += int(injectSweepInterval.Seconds())
					if e.nearSec >= injectNearTotalSec {
						revoke = append(revoke, struct{ acc, id string }{acc, e.id})
					}
				} else {
					e.nearSec = 0
				}
			}
			_ = todo
		}
		s.injectMu.Unlock()
		for _, r := range revoke {
			s.removeInject(r.acc, r.id)
		}
	}
}
