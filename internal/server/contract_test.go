package server

import (
	"bytes"
	"context"
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

func TestContractTrial(t *testing.T) {
	s := newTestServer(t)
	s.SetLastTrial(contractAcc, contractTrial())
	checkGolden(t, "trial", get(t, s, "/api/trial?account="+contractAcc), nil)
}

// scrubEncTime 抹掉遇见记录里的时间取值。
//
// 顶层 ts 与每只精灵的 time 都取自「入库时刻」,每次跑测试都不同。契约锁的是
// **键在不在**(kind/time 是可选指针,键出现与否本身就是契约的一部分),
// 不是几点几分,故抹成 0 —— 与 scrubTS 同理,替换值须保持 JSON 合法。
var encTimeRe = regexp.MustCompile(`"time": \d+`)

func scrubEncTime(s string) string {
	return encTimeRe.ReplaceAllString(scrubTS(s), `"time": 0`)
}

// TestContractTrialEncounters 锁定「遇见记录」的响应结构。
//
// 三条种子各守一条契约,改之前先想清楚守的是什么:
//  1. 3001 记为**普通战**(kind=0):守 kind/time 用**指针**而非 omitempty 值类型 ——
//     取值 0 时键必须仍在,否则前端分不清「普通战遇到过」与「压根没遇到」。
//     这是全接口最易改坏的一处,Go 编译发现不了,肉眼看 JSON 也容易漏。
//  2. 8101 记为首领:22 名首领三章共用,故三张图里都会出现同一批。
//  3. 3005 只记在第 2 章:它在第 3 章池里也有,用来守「每章独立计算」——
//     第 3 章那张图里 3005 必须仍显示未遇见。
//  4. 3027 / 5061 是**池外**遭遇:守 extra 组。这俩是回放实测撞上的真实例子 ——
//     3027 是 NPC 战、5061 是最终 BOSS(敌方式斗酷猫),静态配置没有第 7 层的
//     精灵池,按旧逻辑会静默丢失:用户明明遇到过,图上却永远显示未遇见。
func TestContractTrialEncounters(t *testing.T) {
	s := newTestServer(t)
	for _, w := range []struct {
		ch    uint32
		kind  uint32
		bases []uint32
	}{
		{1, 0, []uint32{3001}},
		{1, 1, []uint32{8101}},
		{2, 0, []uint32{3005}},
		{1, 2, []uint32{3027}}, // NPC 战 —— 不在普通池也不在首领池
		{3, 3, []uint32{5061}}, // 最终 BOSS —— 同上
	} {
		// ts 固定:这是「战斗发生的时刻」(见 AddTrialEncounters),
		// golden 里由 scrubEncTime 抹成 0,故取值本身不进契约。
		if err := s.store.AddTrialEncounters(contractAcc, w.ch, w.kind, w.bases, 1700000000); err != nil {
			t.Fatalf("写遇见记录(第%d章 %v): %v", w.ch, w.bases, err)
		}
	}
	checkGolden(t, "trial-encounters",
		get(t, s, "/api/trial/encounters?account="+contractAcc), scrubEncTime)
}

// TestContractTrialEncountersExtra 单独锁 extra 组的**语义**:不计入 total/seen。
//
// golden 只能看出「extra 里有 3027」,看不出「它没有把 seen 加一」—— 而这个区别
// 正是设计意图:total/seen 的口径是「池子里还剩多少」,把来源不明的条目塞进分母,
// 进度百分比就会失去意义(golden 不会报警,因为它只比对结构)。
// 故这里直接断言计数,把这条口径钉死。
func TestContractTrialEncountersExtra(t *testing.T) {
	s := newTestServer(t)
	// 先量一份基线:没有任何记录时三章的 total
	var base [4]uint32
	var got struct {
		Chapters []struct {
			Chapter uint32 `json:"chapter"`
			Total   uint32 `json:"total"`
			Seen    uint32 `json:"seen"`
			Extra   []struct {
				Base uint32 `json:"base"`
				Seen bool   `json:"seen"`
				Kind *uint32
			} `json:"extra"`
		} `json:"chapters"`
	}
	if err := json.Unmarshal(get(t, s,
		"/api/trial/encounters?account="+contractAcc), &got); err != nil {
		t.Fatalf("解析: %v", err)
	}
	for _, c := range got.Chapters {
		if c.Chapter >= 1 && c.Chapter <= 3 {
			base[c.Chapter] = c.Total
		}
	}

	// 记一条池外遭遇(NPC 战 3027,第 1 章)
	if err := s.store.AddTrialEncounters(contractAcc, 1, 2, []uint32{3027}, 1700000000); err != nil {
		t.Fatalf("写遇见记录: %v", err)
	}
	got.Chapters = nil
	if err := json.Unmarshal(get(t, s,
		"/api/trial/encounters?account="+contractAcc), &got); err != nil {
		t.Fatalf("解析: %v", err)
	}

	var ch1 *struct {
		Chapter uint32 `json:"chapter"`
		Total   uint32 `json:"total"`
		Seen    uint32 `json:"seen"`
		Extra   []struct {
			Base uint32 `json:"base"`
			Seen bool   `json:"seen"`
			Kind *uint32
		} `json:"extra"`
	}
	for i := range got.Chapters {
		if got.Chapters[i].Chapter == 1 {
			ch1 = &got.Chapters[i]
		}
	}
	if ch1 == nil {
		t.Fatal("第1章缺失")
	}
	// 池外遭遇进了 extra
	if len(ch1.Extra) != 1 || ch1.Extra[0].Base != 3027 {
		t.Fatalf("extra 应为 [3027], 实际 %+v", ch1.Extra)
	}
	if ch1.Extra[0].Kind == nil || *ch1.Extra[0].Kind != 2 {
		t.Errorf("extra 的 kind 应为 2(NPC 战), 实际 %v", ch1.Extra[0].Kind)
	}
	// 但**不该**改变 total/seen —— 这是本测试存在的全部理由
	if ch1.Total != base[1] {
		t.Errorf("extra 不该计入 total: 基线 %d, 现在 %d", base[1], ch1.Total)
	}
	if ch1.Seen != 0 {
		t.Errorf("extra 不该计入 seen: 实际 %d, 期望 0", ch1.Seen)
	}
}

// TestContractTrialEncountersEmpty 锁定「一条遇见记录都没有」时的响应。
//
// 结论先行:**空账号下 chapters 照样存在**。精灵池来自静态配置(gamedata.TrialPool),
// 与数据库无关 —— 只要 trial.json 在,三章的池就是满的,Total 恒 > 0。
// 「还没有任何遇见记录」表现为每只 seen=false 且不带 kind/time,而非 chapters 缺席。
//
// 这条容易被想当然:Chapters 上挂着 `omitempty`,会让人以为无数据时键会消失。
// 写本测试时正是这么假设的,跑出来才发现是错的 —— 那个 omitempty 因此是个**死标签**。
// 留着不删是为了不无谓改动对外契约,但别指望它,真要判空请看 books.length。
// 前端 EncountersView 的「没有试炼精灵池数据」分支,触发条件是静态配置缺失
// (chapters 为空),不是「没打过试炼」。
func TestContractTrialEncountersEmpty(t *testing.T) {
	s := newTestServer(t)
	var got map[string]any
	if err := json.Unmarshal(get(t, s,
		"/api/trial/encounters?account="+contractAcc), &got); err != nil {
		t.Fatalf("解析: %v", err)
	}
	want := map[string]bool{"account": true, "ts": true, "updated": true, "chapters": true}
	for k := range got {
		if !want[k] {
			t.Errorf("/api/trial/encounters(无记录) 多了字段 %q", k)
		}
	}
	for k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("/api/trial/encounters(无记录) 少了字段 %q", k)
		}
	}
	books, _ := got["chapters"].([]any)
	if len(books) != 3 {
		t.Fatalf("无记录时仍应有 3 章(池来自静态配置), 实际 %d", len(books))
	}
	// 三章都必须是「整章未遇见」,且不带 kind/time。
	for _, b := range books {
		book, _ := b.(map[string]any)
		if seen, _ := book["seen"].(float64); seen != 0 {
			t.Errorf("第%v章 无记录时 seen 应为 0, 实际 %v", book["chapter"], seen)
		}
		if total, _ := book["total"].(float64); total == 0 {
			t.Errorf("第%v章 无记录时 total 不该为 0(池来自静态配置)", book["chapter"])
		}
		for _, p := range append(
			book["normal"].([]any), book["boss"].([]any)...) {
			pet, _ := p.(map[string]any)
			if s, _ := pet["seen"].(bool); s {
				t.Errorf("无记录时 base=%v 不该是 seen", pet["base"])
			}
			if _, ok := pet["kind"]; ok {
				t.Errorf("无记录时 base=%v 不该带 kind 键", pet["base"])
			}
			if _, ok := pet["time"]; ok {
				t.Errorf("无记录时 base=%v 不该带 time 键", pet["base"])
			}
		}
	}
}

// contractTrial 造一份试炼快照:进行中的一局 + 账号档案。
func contractTrial() *TrialPayload {
	return &TrialPayload{
		Account: contractAcc,
		Ts:      1700000000,
		Active:  true,
		Run: &TrialRun{
			TrialID: 10002, SlotID: 1000, SlotName: "普系",
			ChapterID: 3001, ChapterIdx: 2, NodeIndex: 7, Coin: 12,
			Chapters: []uint32{3000, 3001, 3002},
			Effects:  []uint32{1001, 1008},
			Boss:     false,
			// node_index 7 = NPC 层(层类型的映射见 gamedata/trial.go),
			// 故下面带上第 7 层的候选阵容;其余层不会带 opponents。
			Floor: "npc", FloorLabel: "NPC",
			ChapterName: "记忆中的巨石阵",
			Opponents: []TrialOpponent{
				{ID: 310005, Name: "易西", Pets: []TrialOppPet{
					{Base: 3031, Name: "奇丽花", Img: "HeadIcon/3031.webp"},
					{Base: 3067, Name: "卷毛鸭", Img: "HeadIcon/3067.webp"},
					{Base: 3027, Name: "蒲公英娃娃"}, // 无头像:形态没图时 img 缺失
				}},
			},
			Pet: &TrialPet{
				Gid: 133, Name: "黑猫巫师", Species: "黑猫巫师", Img: "HeadIcon/3569.webp",
				Level: 60, HP: 264, MaxHP: 389, Energy: 10, Growth: 2,
				Skills: []TrialSkill{
					{ID: 7020500, Name: "乱打", Power: 25, Cost: 4, Fusion: 1, Slot: 2, Merged: []uint32{7090100}},
					// 7880058 是魔能爆的**试炼态 id**(开局零融合时就存在,融合也不改 id),
					// 见 gen_skills.py 的 EXTRA_SKILL_IDS
					{ID: 7880058, Name: "魔能爆", Power: 20, Cost: 0, Slot: 2},
					// 资料站未收录的新技能:查不到名,name 缺失,前端回退显示 id
					{ID: 7999999, Power: 10, Cost: 1, Slot: 3},
				},
				Features: []uint32{288135, 288001},
				Shards:   []uint32{2016, 3005},
				Equipped: []uint32{1, 2},
			},
			Options: []TrialOption{
				{Slot: 1, Event: 110061, Reward: 7110340, Level: 40, EventCost: 1, RewardCost: 4, Extra: []uint32{2016}},
				{Slot: 2, Event: 100017, Reward: 7040220, Level: 40},
			},
			RefreshCost: 2,
			Reward:      &TrialReward{Event: 110005, ID: 288001, Extra: []uint32{2016}, Coin: 10},
			Shop: []TrialShopItem{
				{Type: 2, ID: 288154, Price: 6, Index: 4},
				{Type: 3, ID: 2016, Price: 4, Index: 5, Bought: true},
			},
			Log: []TrialLogEntry{
				{Ts: 1700000000, Kind: "node", Label: "推进节点", IDs: []uint32{3001, 3}},
				{Ts: 1700000001, Kind: "reward", Label: "直接收下", IDs: []uint32{288001}, Action: 2},
			},
		},
		History: &TrialHistory{
			ChallengeInc: 251, Total: 251, Wins: 23, Cleared: []uint32{10000, 10001, 10002},
			Recent: []TrialReview{
				{SettleAt: 1699999999, PetBaseID: 3569, PetName: "黑猫巫师", PetLevel: 60, TrialID: 10002, Victory: true, Duration: 1439, SlotID: 1000},
			},
			TopPets: []TrialTopPet{
				{PetBaseID: 3141, Name: "花衣蝶", Img: "HeadIcon/3141.webp", Count: 56},
			},
			Slots: []TrialSlot{
				{SlotID: 1000, DamType: 2, DamName: "普", Cleared: 3},
			},
			Logs: []TrialLogBook{
				{LogConfID: 100, Discovered: 167, Total: 210, Unlocked: true},
			},
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

// —— 标注模式(众包图鉴)——
//
// 契约要点是「**只下发已审核的**」:玩家提交的标注在管理员审核前不能出现在
// /api/annotations 里(否则玩家 A 的猜测会被全服当成事实)。这条语义光看 golden
// 看不出来(只看到 approved 那一条),故 TestAnnotationsReviewFlow 另作行为断言。

// seedAnnotations 造三条标注:特性已审核 1 条、特性待审 1 条、技能已审核 1 条。
// 待审那条是为了证明它**不会**出现在 GET /api/annotations 的响应里。
func seedAnnotations(t *testing.T, s *Server) {
	t.Helper()
	if _, err := s.store.SubmitAnnotation(store.Annotation{
		Kind: "feature", Code: 288135, Name: "助燃", Desc: "使用火系技能后，获得双攻+20%", Submitter: "UID:1",
	}); err != nil {
		t.Fatalf("写已审核特性标注: %v", err)
	}
	if _, err := s.store.SubmitAnnotation(store.Annotation{
		Kind: "feature", Code: 288001, Name: "待审名字", Desc: "不该出现在响应里", Submitter: "UID:2",
	}); err != nil {
		t.Fatalf("写待审特性标注: %v", err)
	}
	if _, err := s.store.SubmitAnnotation(store.Annotation{
		Kind: "skill", Code: 7999999, Name: "新技能名", Desc: "资料站未收录", Submitter: "UID:1",
	}); err != nil {
		t.Fatalf("写已审核技能标注: %v", err)
	}
	// 前两条(按插入序 id=1/2)里只审通过 id=1,以及技能那条 id=3。
	for _, id := range []int64{1, 3} {
		if err := s.store.ReviewAnnotation(id, true, "admin"); err != nil {
			t.Fatalf("审核 id=%d: %v", id, err)
		}
	}
}

// scrubAnnotationTime 抹掉响应里的时间取值:顶层 ts 是响应时刻,createdAt 是提交时刻,
// 两者每次跑都不同(与 scrubTS 同理,替换值保持 JSON 合法)。
var annotationTimeRe = regexp.MustCompile(`"(ts|createdAt)": \d+`)

func scrubAnnotationTime(s string) string { return annotationTimeRe.ReplaceAllString(s, `"$1": 0`) }

func TestContractAnnotations(t *testing.T) {
	s := newTestServer(t)
	seedAnnotations(t, s)
	checkGolden(t, "annotations-feature",
		get(t, s, "/api/annotations?kind=feature"), scrubAnnotationTime)
	checkGolden(t, "annotations-skill",
		get(t, s, "/api/annotations?kind=skill"), scrubAnnotationTime)
}

// TestAnnotationsReviewFlow 钉住审核语义:
//   1. 未审核的标注不下发(golden 看不出「它本可以在却没在」,这里显式断言);
//   2. 通过某条时,同一 (kind,code) 的其余待审自动转 rejected —— 一个 id 只有一个答案。
func TestAnnotationsReviewFlow(t *testing.T) {
	s := newTestServer(t)
	for _, name := range []string{"甲", "乙"} {
		if _, err := s.store.SubmitAnnotation(store.Annotation{
			Kind: "feature", Code: 288022, Name: name, Submitter: "UID:1",
		}); err != nil {
			t.Fatalf("提交标注 %s: %v", name, err)
		}
	}

	var got struct {
		Items []struct {
			Code int64  `json:"code"`
			Name string `json:"name"`
		} `json:"items"`
	}
	// ① 都还在待审,响应应为空
	if err := json.Unmarshal(get(t, s, "/api/annotations?kind=feature"), &got); err != nil {
		t.Fatalf("解析: %v", err)
	}
	if len(got.Items) != 0 {
		t.Fatalf("待审标注不该下发,实际 %d 条: %+v", len(got.Items), got.Items)
	}

	// ② 审通过第一条(id=1),第二条(id=2)应自动转 rejected
	if err := s.store.ReviewAnnotation(1, true, "admin"); err != nil {
		t.Fatalf("审核: %v", err)
	}
	got.Items = nil
	if err := json.Unmarshal(get(t, s, "/api/annotations?kind=feature"), &got); err != nil {
		t.Fatalf("解析: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Name != "甲" {
		t.Fatalf("应只剩通过的「甲」一条,实际 %+v", got.Items)
	}
	// ③ 被自动拒绝的那条确实转成了 rejected(而不是仍留在 pending 里)
	if items, err := s.store.PendingAnnotations("feature"); err != nil {
		t.Fatalf("查待审: %v", err)
	} else if len(items) != 0 {
		t.Fatalf("同 code 的其余待审应被自动拒绝,仍有 %d 条 pending", len(items))
	}
}

// TestAnnotationsFilterByKind 钉住 kind 过滤:技能标注不出现在特性列表里(反之亦然)。
// 两者共用一张表,漏掉 WHERE kind 会互相串味 —— 而 golden 里 skill/feature 是分开的
// 两个快照,恰好掩盖这类串味(串了也只是各自多一条,结构仍对得上)。
func TestAnnotationsFilterByKind(t *testing.T) {
	s := newTestServer(t)
	seedAnnotations(t, s)
	for _, c := range []struct {
		kind     string
		wantCode int64
	}{{"feature", 288135}, {"skill", 7999999}} {
		var got struct {
			Items []struct {
				Code int64 `json:"code"`
			} `json:"items"`
		}
		if err := json.Unmarshal(get(t, s, "/api/annotations?kind="+c.kind), &got); err != nil {
			t.Fatalf("解析 %s: %v", c.kind, err)
		}
		if len(got.Items) != 1 || got.Items[0].Code != c.wantCode {
			t.Fatalf("kind=%s 应只有 %d 一条,实际 %+v", c.kind, c.wantCode, got.Items)
		}
	}
}

// TestAnnotationSubmit 钉住玩家提交入口的校验与去重:
//   1. 合法提交 → 进待审,不下发(golden 那套只覆盖 GET,提交路径是玩家唯一写入口);
//   2. 非法 kind / code / name → 400(前端弹窗依赖这些状态码给出可读提示);
//   3. 同一人对同一 (kind,code,name) 重复提交 → 409(防刷)。
func TestAnnotationSubmit(t *testing.T) {
	s := newTestServer(t)
	postJSON := func(body string) int {
		t.Helper()
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/annotations?account="+contractAcc, strings.NewReader(body))
		s.Handler().ServeHTTP(rr, req)
		return rr.Code
	}

	// ① 非法输入一律 400
	for _, bad := range []string{
		`{"kind":"xxx","code":288135,"name":"助燃"}`, // kind 不在 skill/feature
		`{"kind":"feature","code":0,"name":"助燃"}`,   // code 非正
		`{"kind":"feature","code":288135,"name":""}`, // 空名字
	} {
		if code := postJSON(bad); code != 400 {
			t.Errorf("非法提交 %s 应 400,实际 %d", bad, code)
		}
	}

	// ② 合法提交 → 200,且**不下发**(待审)
	ok := `{"kind":"feature","code":288135,"name":"助燃","desc":"使用火系技能后，获得双攻+20%"}`
	if code := postJSON(ok); code != 200 {
		t.Fatalf("合法提交应 200,实际 %d", code)
	}
	var got struct {
		Items []struct {
			Code int64 `json:"code"`
		} `json:"items"`
	}
	if err := json.Unmarshal(get(t, s, "/api/annotations?kind=feature"), &got); err != nil {
		t.Fatalf("解析: %v", err)
	}
	if len(got.Items) != 0 {
		t.Fatalf("刚提交的标注还在待审,不该下发,实际 %+v", got.Items)
	}

	// ③ 重复提交 → 409(UNIQUE 约束)
	if code := postJSON(ok); code != 409 {
		t.Errorf("重复提交应 409,实际 %d", code)
	}
}

// TestAnnotationReviewBroadcasts 钉住「审核后要广播」这条链路。
//
// 为什么必须广播:标注是全服共享的,而前端只在 App 挂载时拉一次。不广播的话
// 管理员审完之后,玩家不手动刷新浏览器就看不到任何变化 —— 提交的人得不到反馈
// (只会以为没生效),下一个遇到同一 id 的人又会再标一次,众包就空转了。
// 这条链路坏掉时接口照样 200、数据照样入库,只有 SSE 静默不响,故必须测。
func TestAnnotationReviewBroadcasts(t *testing.T) {
	s := newTestServer(t)
	sub := s.Hub().subscribe()
	defer s.Hub().unsubscribe(sub)

	if _, err := s.store.SubmitAnnotation(store.Annotation{
		Kind: "feature", Code: 288135, Name: "助燃", Submitter: "UID:1",
	}); err != nil {
		t.Fatalf("提交标注: %v", err)
	}

	// 审核(通过):走 HTTP 入口,顺带确认管理员鉴权之外的整条链路
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/admin/annotations/1/review", strings.NewReader(`{"approve":true}`))
	req.Header.Set("X-Admin-Token", testAdminToken(t, s))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("审核应 200,实际 %d: %s", rr.Code, rr.Body)
	}

	// 广播应当到达订阅者;account 为空(全服共享,不按账号分发)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, ok := sub.pop(ctx)
	if !ok {
		t.Fatal("审核后没有收到任何广播 —— 前端不会刷新,标注对玩家不可见")
	}
	if got.typ != "annotations" {
		t.Errorf("广播类型应为 annotations,实际 %q", got.typ)
	}
	if got.account != "" {
		t.Errorf("共享标注的广播不该带账号(会被前端按账号过滤掉),实际 %q", got.account)
	}
}

// testAdminToken 设好管理员密码并返回令牌,供需要鉴权的测试用例使用。
func testAdminToken(t *testing.T, s *Server) string {
	t.Helper()
	var res struct {
		Token string `json:"token"`
	}
	rr := httptest.NewRecorder()
	body := strings.NewReader(`{"password":"` + testAdminPw + `"}`)
	s.Handler().ServeHTTP(rr, httptest.NewRequest("POST", "/api/admin/setup", body))
	if rr.Code != 200 {
		t.Fatalf("设置管理员密码: %d %s", rr.Code, rr.Body)
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("解析令牌: %v", err)
	}
	return res.Token
}

const testAdminPw = "contract-test-pw"

// TestAnnotationNameCleaned 提交的名字必须**去掉所有空白** —— 这是中文游戏,
// 技能/特性名里没有合法空格(现有 807 个名字含空格的为 0)。
//
// 起因:wiki 图鉴页为排版在字间插空格,玩家照抄后提交成「魔 法 增 效 」,
// 末尾还常带一个空格。标注是给全服看名字的,名字脏了等于白标。
//
// 每个用例用**不同的 code**:清洗会把这些变体都归一成「魔法增效」,同一个 code
// 下第二次提交会被 UNIQUE 约束判为重复(409)—— 那其实反过来证明清洗生效了,
// 但会淹没「存成了什么」这个断言,故错开 code 让每例独立。
func TestAnnotationNameCleaned(t *testing.T) {
	s := newTestServer(t)
	post := func(code int64, name string) (status int, stored string) {
		body, _ := json.Marshal(map[string]any{
			"kind": "skill", "code": code, "name": name,
		})
		rr := httptest.NewRecorder()
		s.Handler().ServeHTTP(rr, httptest.NewRequest("POST",
			"/api/annotations?account="+contractAcc, bytes.NewReader(body)))
		var out struct {
			Item struct{ Name string `json:"name"` } `json:"item"`
		}
		_ = json.Unmarshal(rr.Body.Bytes(), &out)
		return rr.Code, out.Item.Name
	}

	cases := []struct {
		in   string
		want string
	}{
		{"魔 法 增 效", "魔法增效"},    // 字间空格(wiki 排版)
		{"魔 法 增 效 ", "魔法增效"},   // 末尾还有空格
		{"  魔法增效  ", "魔法增效"},   // 首尾空格
		{"魔法增效", "魔法增效"},        // 干净的输入不受影响
		{"魔\u3000法增效", "魔法增效"},  // 全角空格 U+3000
		{"魔法\t增效", "魔法增效"},     // 制表符
		{"魔 法  增   效", "魔法增效"}, // 多个连续空白
	}
	for i, c := range cases {
		// 7880001 起顺延,避开别处占用的 code
		status, got := post(int64(7880010+i), c.in)
		if status != 200 {
			t.Errorf("第 %d 例 %q 提交失败: %d", i+1, c.in, status)
			continue
		}
		if got != c.want {
			t.Errorf("第 %d 例: 提交 %q 存成 %q, 期望 %q", i+1, c.in, got, c.want)
		}
	}
}

// TestAnnotationNameCleanedDeduplicates 同一个 id 下,「魔 法 增 效」与「魔法增效」
// 清洗后相同 → 第二次提交应判重复(409),而不是存成两条。
// 这条守住 UNIQUE 约束与清洗的配合:若清洗只去首尾,两条会并存,页面上同一个
// 技能会显示两个写法不同的名字。
func TestAnnotationNameCleanedDeduplicates(t *testing.T) {
	s := newTestServer(t)
	post := func(name string) int {
		body, _ := json.Marshal(map[string]any{
			"kind": "skill", "code": 7880050, "name": name,
		})
		rr := httptest.NewRecorder()
		s.Handler().ServeHTTP(rr, httptest.NewRequest("POST",
			"/api/annotations?account="+contractAcc, bytes.NewReader(body)))
		return rr.Code
	}
	if got := post("魔 法 增 效"); got != 200 {
		t.Fatalf("首次提交: %d, 期望 200", got)
	}
	if got := post("魔法增效"); got != 409 {
		t.Errorf("清洗后重复的提交 = %d, 期望 409(清洗应让两者归一)", got)
	}
}

// TestCleanAnnotationName 直接测清洗函数(上面的用例走 HTTP,失败时看不出
// 是清洗的问题还是提交路径的问题)。
func TestCleanAnnotationName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"   ", ""},          // 全是空白 → 空(会被「名字不能为空」拦下)
		{"魔法增效", "魔法增效"}, // 无空白原样返回
		{"魔 法 增 效", "魔法增效"},
		{"魔　法增效", "魔法增效"},
		{"a b", "ab"}, // 英文间的空格也一并去掉(现有名字无此情形,见函数注释)
	}
	for _, c := range cases {
		if got := cleanAnnotationName(c.in); got != c.want {
			t.Errorf("cleanAnnotationName(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}
