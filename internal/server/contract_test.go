package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/whoisnian/rocom-capture/internal/pet"
	"github.com/whoisnian/rocom-capture/internal/store"
)

// 本文件是前后端契约的护栏:锁定对外 JSON 的**字段名与结构**。
//
// 存在理由:后端 JSON 有一半不是由 Go struct 定义的 —— position / wildpets / home /
// flowers 四个实时接口的 payload 是管线里内联拼出的 map[string]any(见 pipeline/
// position.go:219 等),改错一个 key 前端就读到 undefined,而 Go 编译照样通过。
// 这类错误只有把真实响应落盘比对才能发现。
//
// 比对方式:先把响应反序列化再按缩进重排(canonical),故**只锁字段集合与取值,不锁字段
// 顺序** —— 前端按名取值,本就不依赖顺序;这样 struct 与 map 两种构造方式也不会产生
// 无意义的 diff。
//
// 更新 golden:UPDATE_CONTRACT=1 go test ./internal/server/ -run TestContract
// 更新后务必 review diff:那正是「对外契约变了」的清单。

const contractAcc = "UID:1"

// goldenDir 存放各接口响应快照。
var goldenDir = filepath.Join("testdata", "contract")

// checkGolden 比对响应与 golden 快照。scrub 用于抹掉时间戳一类每次都变的取值。
func checkGolden(t *testing.T, name string, body []byte, scrub func(string) string) {
	t.Helper()
	got := canonical(t, body)
	if scrub != nil {
		got = scrub(got)
	}
	path := filepath.Join(goldenDir, name+".json")
	if os.Getenv("UPDATE_CONTRACT") == "1" {
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatalf("建 golden 目录: %v", err)
		}
		if err := os.WriteFile(path, []byte(got+"\n"), 0o644); err != nil {
			t.Fatalf("写 golden %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 golden %s 失败(先跑 UPDATE_CONTRACT=1 生成): %v", path, err)
	}
	// 写入时补了尾随换行,比对前去掉,免得每次都因一个 \n 判不一致。
	if got != strings.TrimRight(string(want), "\n") {
		t.Errorf("%s 响应与 golden 不一致\n--- golden ---\n%s\n--- got ---\n%s", name, want, got)
	}
}

// canonical 把 JSON 反序列化后重新按缩进输出,键序归一(marshal map 时按字典序)。
func canonical(t *testing.T, body []byte) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("响应不是合法 JSON: %v\n%s", err, body)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("重排 JSON: %v", err)
	}
	return string(out)
}

// scrubTS 抹掉位置包的时间戳取值(字段名仍在,过期与否的行为差异见
// TestContractPositionStale)。替换为 0 而非占位符:golden 需保持合法 JSON,
// 才能被 docs/api/fields.json 一类的机器消费方直接解析。
var tsRe = regexp.MustCompile(`"(ts|tsMs)": \d+`)

func scrubTS(s string) string { return tsRe.ReplaceAllString(s, `"$1": 0`) }

// scrubDays 抹掉事件统计里近 30 天的日期标签。
//
// daily 每一天都带 "MM-DD" 标签,由 store 按**当前日期**生成(近 30 天滑动窗口),
// 故 golden 会随日期流逝而失效 —— 8/29 生成的快照到 8/30 就对不上了。
// 这类失效只在跨天时暴露:同日反复跑测试查不出来(初次生成时连跑三遍全绿,次日才炸)。
// 日期值本身不是契约(daily 的字段结构与 n 的取值才是),抹掉即可;替换值需保持 JSON 合法。
var dayRe = regexp.MustCompile(`"day": "\d{2}-\d{2}"`)

func scrubDays(s string) string { return dayRe.ReplaceAllString(s, `"day": "MM-DD"`) }

// —— 种子数据 ——

// seedContract 造一份确定性数据:两只宠物(一只有完整字段、一只最小)+ 盒位队伍 +
// 事件 + 蛋 + 图鉴炫彩。取值固定,不掺当前时间,否则 golden 每次都变。
func seedContract(t *testing.T, s *Server) {
	t.Helper()
	sc := s.store.For(contractAcc)

	full := &pet.Pet{
		Gid: 1001, ConfID: 2000672, BaseConfID: 3006,
		Species: "火神", Name: "小火", Level: 60,
		Gender: "♂", Nature: "固执", NatureID: 3,
		Types: []string{"火"}, HeightM: 1.8, WeightKg: 92.5, Voice: 12,
		TalentRank: "S", Medal: "大块头", PartnerMark: "首领",
		Speciality: "暴击", SpecialityID: 7, CatchTime: 1700000000,
		Shiny: true, BloodID: 3, Blood: "火",
		HP:        pet.Stat{Value: 300, TalentLv: 9, Nature: 1},
		Attack:    pet.Stat{Value: 250, TalentLv: 8},
		Defense:   pet.Stat{Value: 180, TalentLv: 5},
		SpAttack:  pet.Stat{Value: 210, TalentLv: 7},
		SpDefense: pet.Stat{Value: 160, TalentLv: 4},
		Speed:     pet.Stat{Value: 190, TalentLv: 6},
		SkillIDs:  []uint32{101, 102},
	}
	// 最小一只:只填主键,其余留零值,用于锁定 omitempty 的行为。
	min := &pet.Pet{Gid: 1002, ConfID: 3001, BaseConfID: 3001, Species: "水蓝蓝", Name: "小水", Level: 1}
	full.Image = s.db.PetImageByBase(full.BaseConfID, full.Shiny)
	min.Image = s.db.PetImage(min.ConfID, false)
	for _, p := range []*pet.Pet{full, min} {
		if _, err := sc.UpsertPet(p); err != nil {
			t.Fatalf("写入宠物 gid=%d: %v", p.Gid, err)
		}
	}

	if err := sc.ReplacePetBoxMetas([]pet.BoxMeta{{BoxID: 1, Name: "常用", Mark: 1}}); err != nil {
		t.Fatalf("写盒元数据: %v", err)
	}
	if err := sc.ReplacePetBoxes([]pet.BoxEntry{{Gid: 1002, BoxID: 1, Slot: 0, BoxName: "常用"}}); err != nil {
		t.Fatalf("写盒位: %v", err)
	}
	if err := sc.ReplacePetTeams([]pet.TeamEntry{{Gid: 1001, TeamIdx: 0, Pos: 2}}); err != nil {
		t.Fatalf("写队位: %v", err)
	}
	if err := sc.ReplacePetMedals([]pet.MedalOwn{{Gid: 1001, MedalID: 1}}); err != nil {
		t.Fatalf("写奖牌归属: %v", err)
	}
	if err := sc.ApplyBoxMoves([]pet.BoxEntry{{Gid: 1002, BoxID: 1, Slot: 0, BoxName: "常用"}}); err != nil {
		t.Fatalf("应用盒位: %v", err)
	}

	ev := &store.Event{Time: 1700000000, SubKind: "捕捉", Gid: 1001, Pet: full}
	if err := sc.AddEvent(ev); err != nil {
		t.Fatalf("写事件: %v", err)
	}

	// knownHatch 显式指定在孵:hatching 列平时只由 egg_gid 对账(ReconcileHatching)维护,
	// 这里是构造契约样本,直接给权威值更直观(等价于登录数据里带着这颗 gid)。
	if err := sc.UpsertEggs([]*pet.EggView{{
		Gid: 9001, ItemID: 5001, Name: "友爱天天的蛋", Species: "火神",
		HeightM: 0.3, WeightKg: 1.2, ObtainedAt: 1700000000, Src: 1, SrcName: "牧场",
		Hatching: true, HatchedSecs: 600, MaxSecs: 3600, HatchUpdate: 1700000000,
	}}, 1700000000, map[uint32]bool{9001: true}); err != nil {
		t.Fatalf("写蛋: %v", err)
	}

	if err := sc.ReplaceHandbookGlasses([]pet.GlassCollect{
		{PetBaseID: 3006, GlassType: 1, GlassValue: 131073},
		{PetBaseID: 3006, GlassType: 2, GlassValue: 2},
	}); err != nil {
		t.Fatalf("写图鉴炫彩: %v", err)
	}
}

// —— 各接口 ——

func get(t *testing.T, s *Server, target string) []byte {
	t.Helper()
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", target, nil))
	if rr.Code != 200 {
		t.Fatalf("GET %s 状态码 %d: %s", target, rr.Code, rr.Body)
	}
	return rr.Body.Bytes()
}

func TestContractPets(t *testing.T) {
	s := newTestServer(t)
	seedContract(t, s)
	checkGolden(t, "pets", get(t, s, "/api/pets?account="+contractAcc), nil)
	checkGolden(t, "pet-detail", get(t, s, "/api/pets/1001?account="+contractAcc), nil)
	checkGolden(t, "pet-page", get(t, s, "/api/pet-page?gid=1001&account="+contractAcc), nil)
	checkGolden(t, "stats", get(t, s, "/api/stats?account="+contractAcc), nil)
	checkGolden(t, "filter-options", get(t, s, "/api/filter-options?account="+contractAcc), nil)
	checkGolden(t, "boxes", get(t, s, "/api/boxes?account="+contractAcc), nil)
	checkGolden(t, "teams", get(t, s, "/api/teams?account="+contractAcc), nil)
}

func TestContractEvents(t *testing.T) {
	s := newTestServer(t)
	seedContract(t, s)
	checkGolden(t, "events", get(t, s, "/api/events?account="+contractAcc), nil)
	checkGolden(t, "events-stats", get(t, s, "/api/events/stats?account="+contractAcc), scrubDays)
}

func TestContractStatic(t *testing.T) {
	s := newTestServer(t)
	seedContract(t, s)
	checkGolden(t, "icons", get(t, s, "/api/icons"), nil)
	checkGolden(t, "name-options", get(t, s, "/api/name-options"), nil)
	checkGolden(t, "medals", get(t, s, "/api/medals"), nil)
	checkGolden(t, "evolution", get(t, s, "/api/evolution?base=3006"), nil)
}

func TestContractEggsAndGlasses(t *testing.T) {
	s := newTestServer(t)
	seedContract(t, s)
	checkGolden(t, "eggs", get(t, s, "/api/eggs?account="+contractAcc), nil)
	checkGolden(t, "handbook-glasses", get(t, s, "/api/handbook-glasses?account="+contractAcc), nil)
}

// —— 实时快照:四个 map[string]any payload,本次重构最易改坏的地方 ——

// contractPos 造一份与 pipeline/position.go:219 同形的位置 payload。
// 字段取值固定,ts/tsMs 由调用方给(过期与否决定 handlePosition 是否抹掉速度向量)。
func contractPos(tsMs int64) *PositionPayload {
	u, v, vu, vv := 0.5, 0.5, 0.0001, -0.0002
	return &PositionPayload{
		Account: contractAcc, SceneResID: 10003, SceneCfgID: 1001,
		SceneName: "卡洛西亚大陆", Img: "bigmap/10003.webp",
		X: 510000, Y: 612000, Z: 1200,
		U: &u, V: &v, VU: &vu, VV: &vv,
		Heading: 123.5, Stop: false, Paintable: true,
		Ts: tsMs / 1000, TsMs: tsMs,
		Path: []PositionPoint{{U: 0.49, V: 0.49}, {U: 0.5, V: 0.5}},
	}
}

// TestContractPositionFresh 锁定「位置未过期」的完整字段(含速度向量与轨迹)。
func TestContractPositionFresh(t *testing.T) {
	s := newTestServer(t)
	s.SetLastPosition(contractAcc, contractPos(time.Now().UnixMilli()))
	checkGolden(t, "position-fresh", get(t, s, "/api/position?account="+contractAcc), scrubTS)
}

// TestContractPositionStale 锁定过期分支:handlePosition 会抹掉 vu/vv/path,
// 前端据此「先静态回显,等下一个移动包接管」。这两个分支必须同时锁住。
func TestContractPositionStale(t *testing.T) {
	s := newTestServer(t)
	s.SetLastPosition(contractAcc, contractPos(time.Now().Add(-time.Hour).UnixMilli()))
	checkGolden(t, "position-stale", get(t, s, "/api/position?account="+contractAcc), scrubTS)
}

func TestContractWildPets(t *testing.T) {
	s := newTestServer(t)
	pct := 98.5
	s.SetLastWildPets(contractAcc, &WildPayload{
		Account:    contractAcc,
		SceneResID: 10003,
		Pets: []WildMark{{
			ID: "1234567890123456789", Name: "珀尔鼬", Img: "HeadIcon/3006.webp",
			Kinds: []string{"shiny", "big"}, U: 0.4, V: 0.6,
			X: 100, Y: 200, Z: 30, Lv: 45, Voice: 96,
			Height: 120, Weight: 8800, WeightPct: &pct,
			GlassType: 1, Glass: "暗夜拾光", GlassValue: 131073, Mutation: 1,
		}},
		AllPets: []WildAllMark{
			{ID: "2234567890123456789", Name: "鸭吉吉", Img: "HeadIcon/3001.webp", U: 0.7, V: 0.2},
		},
	})
	checkGolden(t, "wildpets", get(t, s, "/api/wildpets?account="+contractAcc), nil)
}

func TestContractHome(t *testing.T) {
	s := newTestServer(t)
	pct := 55.5
	s.SetLastHome(contractAcc, &HomePayload{
		Account: contractAcc,
		// 这四个字段只在玩家确实在家园时下发,且**同进同退**(见 HomePayload.Meta):
		// 在家园时四个都带(值即使为 0/false),不在家园时整体缺席。
		// 原先 golden 只造了「不在家园」一种形态,漏掉了这一支 —— 是前端 [A5] §4 指出的。
		HomeMeta: &HomeMeta{SceneResID: 10003, Level: 5, RoomLevel: 2, CouplesStale: false},
		Nests: []NestMark{
			{ID: "998877665544332211", U: 0.3, V: 0.8, X: 10, Y: 20, Name: "精灵小窝",
				Pet: &NestPet{Gid: 1001, Name: "小火", Species: "火神", Level: 60,
					Voice: 12, WeightPct: &pct, Mates: []NestMate{{Gid: 1002, Name: "小水"}}}},
			{ID: "998877665544332212", U: 0.6, V: 0.4, X: 30, Y: 40, Name: "精灵小窝",
				Egg: &NestEgg{ItemID: 5001, Name: "友爱天天的蛋", Icon: "egg/5001.webp"}},
		},
	})
	checkGolden(t, "home", get(t, s, "/api/home?account="+contractAcc), nil)
}

// contractFlowers 造一份花种分组:cur / worlds 是后端内部字段,handleFlowers 必须
// 把它们剥掉(见 flowerView),只有 /api/flowers/slots 才透传 worlds。
func contractFlowers() *FlowerPayload {
	item := func(id uint32, name string, owner uint64) FlowerItem {
		return FlowerItem{
			ID: id, Name: name, Img: "HeadIcon/3006.webp", Star: 7, Blood: 3,
			BloodName: "火", BloodIcon: "blood/3.webp", NpcLogicID: uint64(id) * 10,
			ChallengeCount: 2, EndTs: 1700086400, SpecSeedID: 0, ActivityID: 7,
			OwnerUserID: owner, Detail: true, Lv: 60,
			GlassType: 1, Glass: "暗夜拾光", GlassValue: 131073,
			BindName: "火神", BindImg: "HeadIcon/3006.webp", BindEvo: 2,
			MedalName: "大块头", MedalIcon: "medal/1.webp",
		}
	}
	return &FlowerPayload{
		Account: contractAcc,
		Cur:     "self",
		Flowers: []FlowerItem{item(7001, "火神", 0)},
		Worlds: FlowerWorlds{
			"self":            &FlowerWorld{TS: 1700000000, Flowers: []FlowerItem{item(7001, "火神", 0)}},
			"owner:10001": &FlowerWorld{TS: 1700000100, Flowers: []FlowerItem{item(7002, "水蓝蓝", 10001)}},
		},
	}
}

func TestContractFlowers(t *testing.T) {
	s := newTestServer(t)
	s.SetLastFlowers(contractAcc, contractFlowers())
	// 关键:flowers 输出里不应出现 cur / worlds。
	checkGolden(t, "flowers", get(t, s, "/api/flowers?account="+contractAcc), nil)
	checkGolden(t, "flowers-slots", get(t, s, "/api/flowers/slots?account="+contractAcc), nil)
}

// TestContractHomeEmpty 锁定「不在家园」分支:只有 account + nests,
// HomeMeta 整体缺席(四个元信息字段都不该出现)。
//
// 原先只有上面「在家园」一份 golden,不在家园这一支完全没被契约覆盖 ——
// 若将来误把 Meta 填成零值(而非留 nil),四个字段会凭空出现且 golden 不会报警。
// 由前端 [A5] §4 指出。
func TestContractHomeEmpty(t *testing.T) {
	s := newTestServer(t)
	s.SetLastHome(contractAcc, &HomePayload{Account: contractAcc, Nests: []NestMark{}})
	var got map[string]any
	if err := json.Unmarshal(get(t, s, "/api/home?account="+contractAcc), &got); err != nil {
		t.Fatalf("解析: %v", err)
	}
	want := map[string]bool{"account": true, "nests": true}
	for k := range got {
		if !want[k] {
			t.Errorf("/api/home(不在家园) 多了字段 %q", k)
		}
	}
	for k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("/api/home(不在家园) 少了字段 %q", k)
		}
	}
	for _, meta := range []string{"sceneResId", "level", "roomLevel", "couplesStale"} {
		if _, ok := got[meta]; ok {
			t.Errorf("/api/home(不在家园) 不该有 %q", meta)
		}
	}
}

// TestContractFlowersHiddenFields 单独断言 cur/worlds 不外泄 —— golden 里若出现即为回归,
// 但字段名本身值得一条显式断言,免得 golden 被无脑 UPDATE 覆盖。
func TestContractFlowersHiddenFields(t *testing.T) {
	s := newTestServer(t)
	s.SetLastFlowers(contractAcc, contractFlowers())
	var got map[string]any
	if err := json.Unmarshal(get(t, s, "/api/flowers?account="+contractAcc), &got); err != nil {
		t.Fatalf("解析: %v", err)
	}
	for _, hidden := range []string{"cur", "worlds"} {
		if _, ok := got[hidden]; ok {
			t.Errorf("/api/flowers 泄漏内部字段 %q", hidden)
		}
	}
}
