package server

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zxsos/rocom-go/internal/pet"
	"github.com/zxsos/rocom-go/internal/store"
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
	// 顺手剥掉全部 \r:core.autocrlf=true 的机器 checkout 会把 golden 的每行行尾
	// 都转成 CRLF(不只是尾行),留着会导致所有契约测试误报不一致。
	wantStr := strings.TrimRight(strings.ReplaceAll(string(want), "\r", ""), "\n")
	if got != wantStr {
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
	// hatchRate 与 hatchSpeed 随**当前时刻/玩家实测**而变(活动倍率按每周时间表算,
	// 见 pet.HatchActivityRate),不抹掉的话 golden 会在周五~周日变成另一份、契约测试
	// 周期性变红。它们本身由 TestHatchRateContract 单独按固定时刻锁定。
	checkGolden(t, "eggs", get(t, s, "/api/eggs?account="+contractAcc), scrubHatch)
	checkGolden(t, "handbook-glasses", get(t, s, "/api/handbook-glasses?account="+contractAcc), nil)
}

// scrubHatch 抹掉孵化倍率相关的两个随时间/状态而变的取值(字段名仍在,键出现与否
// 本身是契约的一部分)。抹成 0 而非占位符:golden 需保持合法 JSON,才能被
// docs/api/fields.json 一类的机器消费方直接解析。
func scrubHatch(s string) string {
	s = regexp.MustCompile(`"hatchRate": [\d.]+`).ReplaceAllString(s, `"hatchRate": 0`)
	// activityRate 是「本周是否处在孵化活动期」的倍率:周五 04:00 ~ 周一 04:00 是 5,
	// 其余日子是 1(pet.HatchActivityRate)。它随**跑测试的时刻**变 —— 漏掉这一行时,
	// golden 会在周一~周四变成 1 而周期性变红(实测周二跑就红)。取值由
	// TestHatchRateContract 按固定时刻单独锁定,这里只需保证**键在**。
	s = regexp.MustCompile(`"activityRate": [\d.]+`).ReplaceAllString(s, `"activityRate": 0`)
	// 实测倍率:玩家测过才有值,且值随他当时状态而变,同样抹掉
	return regexp.MustCompile(`"rate": [\d.]+`).ReplaceAllString(s, `"rate": 0`)
}

// TestScrubHatchIsWeekdayProof: scrubHatch 必须把工作日与周末的响应抹成**同一份**。
//
// 存在的理由:activityRate 在周五 04:00~周一 04:00 是 5、其余是 1,漏抹它时
// golden 会随星期变 —— 而且只在周一~周四暴露(周五生成的人当天跑是绿的)。
// 这条不依赖系统时间,任何一天跑都能抓住「漏抹一个随时间变的字段」。
func TestScrubHatchIsWeekdayProof(t *testing.T) {
	weekday := `{"activityRate": 1, "hatchRate": 1.5, "rate": 2}`
	weekend := `{"activityRate": 5, "hatchRate": 1.5, "rate": 2}`
	gotDay, gotEnd := scrubHatch(weekday), scrubHatch(weekend)
	if gotDay != gotEnd {
		t.Errorf("同一份响应在工作日/周末应抹成一致:\n  工作日 %s\n  周末   %s", gotDay, gotEnd)
	}
	// 抹的是**值**不是键:契约守的是「这个字段在不在」。
	for _, key := range []string{`"activityRate": 0`, `"hatchRate": 0`, `"rate": 0`} {
		if !strings.Contains(gotEnd, key) {
			t.Errorf("抹值后应保留键 %s,实得 %s", key, gotEnd)
		}
	}
}

// TestHatchRateContract 按固定时刻锁定孵化倍率的**取值**(上面的 golden 抹掉了它)。
//
// 这是本契约里唯一会随时间变化的字段,而它恰恰是玩家最关心的(加速日 ETA 差 5 倍),
// 故单独钉:窗口内 5、窗口外 1。时刻写死,不掺 time.Now()。
func TestHatchRateContract(t *testing.T) {
	s := newTestServer(t)
	seedContract(t, s)
	// 北京时间的周六 12:00 与周二 12:00(见 pet.HatchActivityRate 的窗口定义)
	sat := time.Date(2026, 9, 5, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60)).Unix()
	tue := time.Date(2026, 9, 8, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60)).Unix()
	if got := pet.HatchActivityRate(sat); got != 5 {
		t.Errorf("周六 12:00(加速窗口内)倍率 = %v,期望 5", got)
	}
	if got := pet.HatchActivityRate(tue); got != 1 {
		t.Errorf("周二 12:00(窗口外)倍率 = %v,期望 1", got)
	}
}

// —— 实时快照:四个 map[string]any payload,本次重构最易改坏的地方 ——

// contractPos 造一份与 pipeline/position.go:219 同形的位置 payload。
// 字段取值固定,ts/tsMs 由调用方给(过期与否决定 handlePosition 是否抹掉速度向量)。
func contractPos(tsMs int64) *PositionPayload {
	u, v, vu, vv := 0.5, 0.5, 0.0001, -0.0002
	return &PositionPayload{
		Account: contractAcc, SceneResID: 10003, SceneCfgID: 1001,
		SceneName: "卡洛西亚大陆", Img: "bigmap/10003.webp", ImgHd: "bigmap/10003_hd.webp",
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

// TestContractGathers 钉住实时采集物端点的字段名。
//
// 与 TestContractWildPets 同理:键名写错(gathers 写成 gather)前端读到 undefined,
// 而 Go 编译照样过。这层是新增的,且**静默失败时与「附近没有采集物」无法区分**
// (页面只是空着),故必须由 golden 钉住它真的产出内容。
func TestContractGathers(t *testing.T) {
	s := newTestServer(t)
	s.SetLastGathers(contractAcc, &GatherPayload{
		Account:    contractAcc,
		SceneResID: 10003,
		Gathers: []GatherMark{
			// 正常形态:品种名与图标都在(图标须是拼好的路径,不是原始文件名)。
			{ID: "9284181347180715165", R: 801079, N: "可可果树",
				Icon: "worldmap/100211.webp", U: 0.4, V: 0.6, X: 386181, Y: 630422, Z: 6468},
			// 品种图标缺失:icon 应缺席(omitempty),前端据此回退到通用标记。
			{ID: "9284181347180715166", R: 801078, N: "无花果树",
				U: 0.5, V: 0.3, X: 394284, Y: 634246, Z: 7607},
		},
	})
	checkGolden(t, "gathers", get(t, s, "/api/gathers?account="+contractAcc), nil)
}

// TestContractGathersNull 从未收到过实体时返回 null(而非空对象)。
// 前端据 null 与 []gathers 区分「还没有数据」与「附近确实没有」。
func TestContractGathersNull(t *testing.T) {
	s := newTestServer(t)
	checkGolden(t, "gathers-null", get(t, s, "/api/gathers?account="+contractAcc), nil)
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

// TestContractTrial 锁定试炼响应的结构(含节点事件、宠物、商店)。
//
// 宠物是**普通**的(shiny=false)。这条 golden 单独看有个盲点:若哪天有人把异色
// 判断写回硬编码 false,这条仍旧通过 —— 它本来就是 false。故另有
// TestContractTrialShiny 专门守异色那条路,**两条必须成对看**。
func TestContractTrial(t *testing.T) {
	s := newTestServer(t)
	s.SetLastTrial(contractAcc, contractTrial())
	checkGolden(t, "trial", get(t, s, "/api/trial?account="+contractAcc), nil)
}

// TestContractTrialShiny 守住「选异色精灵进试炼,头像得是异色的」。
//
// 这是真实踩过的坑:试炼带的是玩家**自己的**精灵,异色/炫彩原样带进去,
// 而外观标志(mutation_type)只存在于内嵌 PetData 里 —— 解析时漏掉它,
// 取图就一律按非异色取,异色精灵在试炼页显示成普通头像。
//
// 与 TestContractTrial 成对存在:那条是普通宠(shiny=false),这条是异色宠。
// 少任何一条,「把 shiny 写死成 false」这种改动都能静默溜过去。
func TestContractTrialShiny(t *testing.T) {
	s := newTestServer(t)
	// 3044 是实测**确有异色头像**的形态:普通 3044.webp / 异色 3044_1.webp。
	// 选一只真有素材的,否则取图会静默回退普通图、golden 里的 img 与普通宠一样,
	// 这条测试就守不住任何东西了(这正是异色头像素材不全时最容易踩的假阳性)。
	const shinyBase = uint32(3044)
	normal, shinyImg := s.db.PetImageByBase(shinyBase, false), s.db.PetImageByBase(shinyBase, true)
	if normal.Head == "" || shinyImg.Head == "" || normal.Head == shinyImg.Head {
		t.Fatalf("base %d 的异色头像素材缺失(普通=%q 异色=%q) —— 换一只有异色图的形态",
			shinyBase, normal.Head, shinyImg.Head)
	}
	p := contractTrial()
	p.Run.Pet = &TrialPet{
		Gid: 133, Name: "异色的它", Species: "异色的它", Img: shinyImg.Head,
		Level: 60, HP: 264, MaxHP: 389, Energy: 10, Growth: 2,
		Features: []uint32{288135},
		// 异色(bit0) + 炫彩(bit3)同时成立:位标志互不排斥,
		// 异色炫彩精灵两者都为真,别写成互斥的三元。
		Shiny: true, Colorful: true,
		GlassType: 1, GlassValue: 0x00010002,
	}
	s.SetLastTrial(contractAcc, p)
	checkGolden(t, "trial-shiny", get(t, s, "/api/trial?account="+contractAcc), nil)
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
	// 顶层字段清单要跟 payload 同步加:source/activity 是官方配置提交(2026-09)加的,
	// 白名单漏了它们会把「本来就是契约」的字段误报成多余。
	want := map[string]bool{
		"account": true, "ts": true, "updated": true, "chapters": true,
		"source": true, "activity": true,
	}
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
				// 天生 vs 试炼中获得的拆分(局级 initial_feature_ids)
				InnateFeatures: []uint32{288135},
				GainedFeatures: []uint32{288001},
				// 天生那条的名字:用「精灵 → 特性」表桥接出来的(黑猫巫师 → 预警)
				FeatureNames: map[uint32]string{288135: "预警"},
				Shards:       []uint32{2016, 3005},
				Equipped:     []uint32{1, 2},
			},
			Options: []TrialOption{
				// 抽取池:1 个特性 + 4 个技能(「换奖励」就在这 5 个里重抽)。
				// used 里那条是本节点已抽过的,重掷不会再出,前端据此压暗。
				{Slot: 1, Event: 110061, Reward: 7110340, Level: 40, EventCost: 1, RewardCost: 4,
					Extra: []uint32{2016},
					Pool:  []uint32{288135, 7110340, 7020430, 7020440, 7160140},
					Used:  []uint32{7040220},
					// 技能名来自 skills.json;特性名来自「精灵 → 特性」表(事件能映射出精灵才有)
					Names: map[uint32]string{7110340: "超导加速", 7020430: "见招拆招",
						7020440: "触底强击", 7160140: "超级糖果"},
					// 事件对应哪只精灵协议不给,由官方事件表给出 —— 表内有才有 pet
					Pet: &TrialOppPet{Base: 3031, Name: "奇丽花", Img: "HeadIcon/3031.webp"},
				},
				// 官方表外的事件:pet 缺失、names 缺失,前端显示「事件 id」占位
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

// TestContractBreeding 锁定 /api/breeding 的 JSON 形状。
//
// 这条接口比别的更需要 golden:响应里一半是**算出来的**(每条线的 suggest / backcross),
// 而它们的字段名只写在 pet.Suggestion / pet.Backcross 上 —— 改错了前端就读到 undefined,
// 页面上表现为「建议面板整块空白」,而 Go 编译与别的接口的测试都照样全绿。
//
// 样本刻意造出「父本候选不止一只」(串窝 → ambiguous)与「子代优于双亲」(触发回交对比),
// 让 suggest 与 backcross 两块都进 golden,而不是被 omitempty 悄悄省掉 —— 省掉的那部分
// 正是最容易写错的部分。
func TestContractBreeding(t *testing.T) {
	s := newTestServer(t)
	seedContract(t, s)
	seedBreeding(t, s)
	checkGolden(t, "breeding", get(t, s, "/api/breeding?account="+contractAcc), nil)
}

// TestContractBreedingPool 锁定 /api/breeding/pool 的候选池形状。
//
// 与 breeding 同理:候选池的字段名写在 pet.PetCandidate **内嵌**的 pet.EggParent 上,
// 内嵌一旦改成具名字段(或某个 json tag 被删),JSON 就多出一层或换个键名,前端读到
// undefined、补录面板三个下拉全空,而 Go 编译与别的接口的测试都照样绿。
//
// 样本复用 seedBreeding 的候选宠(同品种 ♀/♂ 且蛋组齐备),不另造:那批人正是补录面板
// 要选的对象,而蛋组缺失会让种公队列整体退化(见 pet.BreedCandidates),golden 就锁不住粗筛。
//
// 参数用**品种标识** evo + species(前端就是这么发的,见 pet.ChainRef):只给 species 时服务端
// 也会按名字补出链,但那条路不在这里锁 —— 这里要锁的是「链口径下候选池按链收人」。
func TestContractBreedingPool(t *testing.T) {
	s := newTestServer(t)
	seedContract(t, s)
	seedBreeding(t, s)
	evo := s.db.ChainOf(3006) // 火神所在的那条链;种子宠的 base_conf_id 都是 3006
	if evo == 0 {
		t.Fatal("火神没有进化链,候选池样本失去意义")
	}
	url := fmt.Sprintf("/api/breeding/pool?evo=%d&species=火神&account=%s", evo, contractAcc)
	checkGolden(t, "breeding-pool", get(t, s, url), nil)
}

// seedBreeding 造一条培育线及它需要的候选宠。
//
// 候选宠写在这个用例里、不写进 seedContract:后者是别的 golden 的公共底料,
// 往里加宠会连带改掉 pets / stats / boxes 一串快照(见 TestContractPets)。
func seedBreeding(t *testing.T, s *Server) {
	t.Helper()
	sc := s.store.For(contractAcc)

	// 2001 种母、2002 与 2003 两只 ♂ 同品种同蛋组 —— 与 seedContract 的 1001(火神 ♂)一起,
	// 这条线就有 3 位父本候选,正对应「串窝」那种情形。
	mk := func(gid uint32, name, gender string, voice int32) *pet.Pet {
		p := &pet.Pet{
			Gid: gid, ConfID: 2000672, BaseConfID: 3006,
			Species: "火神", Name: name, Level: 30, Gender: gender,
			Nature: "固执", HeightM: 1.5, WeightKg: 88, Voice: voice, TalentRank: "A",
		}
		p.Image = s.db.PetImageByBase(p.BaseConfID, false)
		pet.FillSizePercentile(s.db, p) // 百分位按当前 gamedata 现算,不落库(同 handleBreeding)
		// 蛋组必须在这里补上:管线抓到的宠是经 pet.FromProto 转换来的,那里会注入蛋组
		// (model.go 的 db.PetEggGroups(base));手搓的宠少了它,BreedPool 就一只都配不出,
		// golden 里的 suggest 会被 omitempty 静默省掉 —— 那样这条契约就白锁了。
		p.EggGroups = s.db.PetEggGroups(p.BaseConfID)
		return p
	}
	mother, father, child := mk(2001, "小母", "♀", 40), mk(2002, "小公乙", "♂", -20), mk(2003, "小子", "♂", 88)
	// 2004 是「换种」要用的备选:没有它的话回交建议会走进「没有别的候选可比」那条捷径,
	// 而两栏对比(回交 vs 换种)才是这块 UI 的主体 —— 那部分字段必须进 golden。
	extra := mk(2004, "小公丙", "♂", 60)
	for _, p := range []*pet.Pet{mother, father, child, extra} {
		if _, err := sc.UpsertPet(p); err != nil {
			t.Fatalf("写入宠物 gid=%d: %v", p.Gid, err)
		}
	}

	// 目标定成「越高越好」,而子代(88)已优于双亲均值 → 回交建议会给出对比,而不是空壳。
	//
	// 性格目标两种写法都给上:`nature` 精确一个,`natureIn` 是「性格正面加物攻」那一整行
	// (该维 +10% 的 5 个名字,前端按 6×6 方阵的整行展开,见 pet.BreedingGoal)。**两种都要出现在
	// golden 里** —— docs/api/fields.json 是从 golden 样本生成的,只给 nature 的话新字段会整条消失。
	voice, wpct := int32(96), 98.0
	natureRow := []string{"逞强", "固执", "大胆", "调皮", "勇敢"}
	line := &pet.BreedingLine{
		ID: "contract-line", Species: "火神", ConfID: 3006,
		Goal:   pet.BreedingGoal{Voice: &voice, WeightPct: &wpct, Nature: "固执", NatureIn: natureRow},
		Status: pet.BreedingActive,
		// 第 1 代是已认领的完整记录(手动补录);第 2 代是**待认领**的一代 ——
		// 破壳时只知道双亲、子代还没认领,这一支的字段(pending / fathers 多候选 / 无 child)
		// 与已认领那支完全不同,故两者都要有。
		Gens: []pet.Generation{{
			Gen: 1, Mother: parent(mother), Father: parent(father), Child: parent(child),
			Source: pet.GenSourceManual, At: 1700000000, Note: "手动补录样本",
		}},
		Pending: []pet.Generation{{
			Gen: 2, Mother: parent(mother),
			Fathers: []pet.EggParent{*parent(father), *parent(extra)},
			Source:  pet.GenSourceAuto, At: 1700000100,
		}},
		CreatedAt: 1700000000, UpdatedAt: 1700000100,
	}

	// 第 3 代是**待孵**的一代(收了蛋、还没孵),并留一颗真蛋在库里 —— 这一支要进 golden:
	// 响应里的 eggs(回查蛋表给的体重/推算嗓音/奖牌)是读取时算出来的,单测守不住
	// 「响应里到底有没有这个字段」,只有 golden 能。嗓音由双亲推出,故一定算得出来。
	const eggGid = 9101
	eggWp := 99.0
	if err := sc.UpsertEggs([]*pet.EggView{{
		Gid: eggGid, ItemID: 107003, ConfID: 3006001, Name: "火神的蛋", Species: "火神",
		Icon: "egg/egg_huoshen.webp", WeightKg: 4.2, HeightM: 0.28, WeightPct: &eggWp,
		ObtainedAt: 1700000050,
	}}, 1700000050, nil); err != nil {
		t.Fatalf("写蛋 %d: %v", eggGid, err)
	}
	if err := sc.SetEggParents(eggGid, &pet.EggParents{
		Mother:  parent(mother),
		Fathers: []pet.EggParent{*parent(father)},
	}); err != nil {
		t.Fatalf("记双亲: %v", err)
	}
	line.Pending = append(line.Pending, pet.Generation{
		Gen: 3, Mother: parent(mother), Father: parent(father), EggGid: eggGid,
		Source: pet.GenSourceAuto, At: 1700000050,
	})
	// 第 4 代:孵出的那只已不在宠物库(放生/送人)—— 后端要把它列进 lostChildGens,
	// 前端据此说「已不在库」,而不是笼统地挂在「待认领」下让玩家空等。
	line.Pending = append(line.Pending, pet.Generation{
		Gen: 4, Mother: parent(mother), Father: parent(father), ChildGid: 9999,
		Source: pet.GenSourceAuto, At: 1700000200,
	})

	if err := sc.UpsertBreedingLine(line); err != nil {
		t.Fatalf("写培育线: %v", err)
	}
}

// parent 取一份亲本快照。走 pet.ParentSnapshot 而不是手搓字段:线上记的双亲就是它产生的,
// 手搓一份的话这里通过、线上仍可能少字段。
func parent(p *pet.Pet) *pet.EggParent {
	snap := pet.ParentSnapshot(p)
	return &snap
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
