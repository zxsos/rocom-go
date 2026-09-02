package server

import (
	"encoding/json"
	"math"
	mathrand "math/rand"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/whoisnian/rocom-capture/internal/pet"
)

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
			name := e.mark.Name
			kinds := e.mark.Kinds
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
	id            string    // 前端标记 id,前缀 admin-inject-
	account       string    // 所属账号
	sceneRes      int32     // 投放时所在场景(花种注入恒为 0)
	x, y          int32     // 投放世界坐标(厘米),用于距离判定(花种注入恒为 0)
	mark          *WildMark // 广播给前端的标记载荷(花种注入仅作记录,不广播 wildpets)
	created       time.Time // 投放时刻
	nearSec       int       // 距离玩家 <10 米累计的秒数(连续靠近 10 秒触发自动撤销)
	kind          string    // 注入类型:wild=野生精灵 / flower=花种
	flowerLogicID uint64    // kind=flower 时花种 npc_logic_id,撤销时据此从花种分组删除
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
	injectNearMeters    = 10.0
	injectNearTotalSec  = 10
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

	pos := s.snap.getPos(req.Account)
	if pos == nil {
		http.Error(w, "该账号暂无已知位置,请等其进入有底图的场景后再投放", 400)
		return
	}
	sceneRes := pos.SceneResID
	if sceneRes == 0 {
		http.Error(w, "该账号当前场景无 scene_res 信息", 400)
		return
	}
	x, y, z := pos.X, pos.Y, pos.Z
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
	mark := WildMark{
		ID:         id,
		Name:       info.Name,
		BaseConfID: req.Base,            // 色卡的「在 rkpet 看 3D 效果」外链要用;这里直接就是形态编号
		Shiny:      req.Kind == "shiny", // 同上:异色要给外链加 shiny=1,否则 3D 是普通配色
		Img:        s.db.PetImageByBase(req.Base, req.Kind == "shiny").Head,
		Kinds:      []string{req.Kind},
		U:          u,
		V:          v,
		X:          wx,
		Y:          wy,
		Z:          wz,
		Lv:         req.Level,
		Voice:      voice,
		Height:     height,
		Weight:     weight,
		Mutation:   mutation,
		Inject:     true, // 前端据此显示撤销按钮与视觉提示
	}
	// 体重百分位与真实野生宠同一口径(pet.SizePercentile),前端资料卡才能显示「体重 xx%」。
	if info.WeightHigh > info.WeightLow {
		if pct := pet.SizePercentile(float64(weight)/1000,
			float64(info.WeightLow)/1000, float64(info.WeightHigh)/1000); pct != nil {
			mark.WeightPct = pct
		}
	}
	if req.Kind == "colorful" {
		glassDesc := s.db.GlassDesc(glassType, glassValue)
		if glassDesc == "" {
			glassDesc = "炫彩"
		}
		mark.Glass = glassDesc
		mark.GlassType = glassType
		mark.GlassValue = glassValue
	}

	// 记入 injects(生命周期管理),并合并进 lastWild 缓存供回显。
	entry := &injectEntry{
		id: id, account: req.Account, sceneRes: sceneRes,
		x: wx, y: wy, mark: &mark, created: time.Now(), kind: "wild",
	}
	s.injectMu.Lock()
	s.injects[req.Account] = append(s.injects[req.Account], entry)
	s.injectMu.Unlock()

	// 读-改-写由 snapshotStore 在锁内完成(见 mergeWild),外部拿不到 wildMu
	s.snap.mergeWild(req.Account, sceneRes, []WildMark{mark})

	s.hub.Broadcast("wildpets", req.Account, WildPayload{
		Account: req.Account, SceneResID: sceneRes,
		Pets: []WildMark{mark}, AllPets: []WildAllMark{},
		Inject: true,
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
		ID:         req.Base,
		Name:       info.Name,
		Img:        head,
		Star:       star,
		Blood:      blood,
		BloodName:  s.db.BloodName(blood),
		BloodIcon:  s.db.BloodIcon(blood),
		NpcLogicID: uint64(now.UnixNano()),
		EndTs:      uint64(now.Add(3 * 24 * time.Hour).Unix()), // 3 天活动倒计时
		SpecSeedID: specSeedID,
		ActivityID: 1,
		Detail:     true,
		Lv:         uint32(randRange(40, 70)),
		GlassType:  glassType,
		Glass:      glassDesc,
		GlassValue: glassValue,
		BindName:   info.Name,
		BindImg:    head,
	}
	s.InjectFlowerItem(req.Account, f)

	id := "admin-inject-" + strconv.FormatInt(now.UnixNano(), 36)
	s.injectMu.Lock()
	s.injects[req.Account] = append(s.injects[req.Account], &injectEntry{
		id: id, account: req.Account, mark: &WildMark{
			ID: id, Name: info.Name + "(花种)", Kinds: []string{"colorful"},
			Glass: glassDesc, GlassType: glassType, GlassValue: glassValue,
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
	// 读-改-写由 snapshotStore 在锁内完成(见 dropWild),外部拿不到 wildMu
	if !s.snap.dropWild(acc, id) {
		return true
	}
	cur := s.snap.getWild(acc)
	payload := WildPayload{Account: acc, SceneResID: cur.SceneResID, Pets: cur.Pets, AllPets: []WildAllMark{}, InjectRevoke: id}
	s.hub.Broadcast("wildpets", acc, payload)
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
		var revoke []struct{ acc, id string }
		for acc, list := range s.injects {
			// 玩家位置(只读缓存)。
			pos := s.snap.getPos(acc)
			var px, py int32
			var pScene int32
			if pos != nil {
				px, py, pScene = pos.X, pos.Y, pos.SceneResID
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
		}
		s.injectMu.Unlock()
		for _, r := range revoke {
			s.removeInject(r.acc, r.id)
		}
	}
}
