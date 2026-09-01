package gamedata

import "testing"

// 本文件验证草系徽章试炼的静态配置(scripts/gen_trial.py 产出)。
// 数据源是玩家维护的 wiki,内容会随版本变,故只断言**结构**与已实测的事实,
// 不断言具体的精灵名单(那会让 wiki 一更新测试就红)。

func TestTrialLoaded(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if db.trial == nil {
		t.Fatal("trial 未加载")
	}
	if len(db.trial.floors) == 0 {
		t.Fatal("floors 为空")
	}
	if len(db.trial.bosses) == 0 {
		t.Error("bosses 为空 —— 第 4 层候选池会缺失")
	}
	if len(db.trial.pools) == 0 {
		t.Error("pools 为空")
	}
	if len(db.trial.npc) == 0 {
		t.Error("npc 为空 —— 第 7 层候选阵容会缺失")
	}
	t.Logf("floors %d 段, 章节 %d, 池 %d 章, 首领 %d, 难度 %d",
		len(db.trial.floors), len(db.trial.chapters), len(db.trial.pools),
		len(db.trial.bosses), len(db.trial.npc))
}

// TestTrialFloors 守护层类型的对应关系 —— 这是本次最关键的实测结论。
//
// wiki 说每章 7 层,协议里每章是 8 个节点(node_index 0~7),直接套用会错位。
// 对应关系由两条独立证据确定(见 scripts/gen_trial.py 文件头):
//  1. node_index 6 之后有一段无战斗的空档,对上 wiki 的「6 层无精灵」;
//  2. node_index 7 的战斗对手是 NPC「草系研究员」,对上 wiki 的「7 层 NPC」。
//
// 本测试防止这个映射被误改回「照抄 wiki 的 7 层」。
func TestTrialFloors(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []FloorType{
		FloorStart,    // 0 章节起点
		FloorNormal,   // 1
		FloorNormal,   // 2
		FloorNormal,   // 3
		FloorBoss,     // 4 首领池
		FloorNormal,   // 5
		FloorMerchant, // 6 无精灵
		FloorNPC,      // 7 NPC 阵容
	}
	if len(db.trial.floors) != len(want) {
		t.Fatalf("floors 有 %d 段, 期望 %d(协议每章 8 个节点)", len(db.trial.floors), len(want))
	}
	for i, w := range want {
		if got := db.TrialFloor(uint32(i)); got != w {
			t.Errorf("TrialFloor(%d) = %q, 期望 %q", i, got, w)
		}
	}
	// 每个已知类型都要有中文名(前端直接展示,空串会显示成空白)
	for i := range want {
		if l := db.TrialFloor(uint32(i)).Label(); l == "" {
			t.Errorf("第 %d 层的 Label 为空", i)
		}
	}
	// 越界必须是未知,不能 panic 也不能绕回
	if got := db.TrialFloor(99); got != FloorUnknown {
		t.Errorf("越界应返回 FloorUnknown, 得到 %q", got)
	}
	if FloorUnknown.Label() != "" {
		t.Error("FloorUnknown 的 Label 应为空串")
	}
}

// TestTrialChapterName 断言按**章节序号**(1 起)能查到章节名。
// 这里刻意不用 wiki 的 chapter id —— 三套编号互不通用(协议 3000/3001/3002、
// wiki 池 1000/1001/1002、wiki 各难度 1000/2000/3000 段),见 gen_trial.py。
func TestTrialChapterName(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for n := uint32(1); n <= 3; n++ {
		if got := db.TrialChapterName(n); got == "" {
			t.Errorf("第 %d 章查不到名字", n)
		} else {
			t.Logf("第%d章: %s", n, got)
		}
	}
	if got := db.TrialChapterName(99); got != "" {
		t.Errorf("不存在的章应返回空串, 得到 %q", got)
	}
}

// TestTrialNPCByMode 断言 NPC 候选阵容按「难度 + 章序号」可查,
// 并守护一条实测规律:**进阶难度会累积低难度的阵容**
// (基础 2/2/1 → 进阶1 4/4/1 → 进阶2 6/6/1)。
// 这条规律让「进阶2 第1章有 6 套候选」这件事可预期,若哪天不再累积,说明改版了。
func TestTrialNPCByMode(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	counts := map[uint32]map[uint32]int{}
	for _, mode := range []uint32{10000, 10001, 10002} {
		counts[mode] = map[uint32]int{}
		for ch := uint32(1); ch <= 3; ch++ {
			ops := db.TrialNPCOpponents(mode, ch)
			if len(ops) == 0 {
				t.Errorf("难度 %d 第 %d 章没有候选阵容", mode, ch)
				continue
			}
			counts[mode][ch] = len(ops)
			for _, o := range ops {
				if o.Name == "" {
					t.Errorf("难度 %d 第 %d 章有阵容缺 NPC 名: %+v", mode, ch, o)
				}
				if len(o.Pets) == 0 {
					t.Errorf("难度 %d 第 %d 章 [%d] 阵容为空", mode, ch, o.ID)
				}
			}
		}
	}
	t.Logf("候选套数: %v", counts)
	// 累积规律:进阶难度在同章不应少于低难度
	for _, ch := range []uint32{1, 2} {
		if counts[10001][ch] < counts[10000][ch] {
			t.Errorf("进阶1 第%d章 (%d 套) 少于基础 (%d 套) —— 累积规律可能变了",
				ch, counts[10001][ch], counts[10000][ch])
		}
		if counts[10002][ch] < counts[10001][ch] {
			t.Errorf("进阶2 第%d章 (%d 套) 少于进阶1 (%d 套) —— 累积规律可能变了",
				ch, counts[10002][ch], counts[10001][ch])
		}
	}
	// 未知难度/章返回 nil,不 panic
	if got := db.TrialNPCOpponents(99999, 1); got != nil {
		t.Errorf("未知难度应返回 nil, 得到 %v", got)
	}
}

// TestTrialBosses 断言首领池非空且无重复 —— 它是第 4 层的候选,
// 若出现重复会让前端渲染出两个一样的选项。
// 数量固定 22(页面标注),wiki 数据不全时靠我方名称库兜底(见 gen_trial.py)。
func TestTrialBosses(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	bosses := db.TrialBosses()
	if len(bosses) != 22 {
		t.Errorf("首领 %d 名, 期望 22", len(bosses))
	}
	seen := map[uint32]bool{}
	for _, b := range bosses {
		if seen[b] {
			t.Errorf("首领 %d 重复", b)
		}
		seen[b] = true
		if _, ok := db.PetBase(b); !ok {
			t.Errorf("首领 %d 不在我方 petbase 里(会显示不出名字/头像)", b)
		}
	}
}

// TestTrialBossesAreBattleForms 守护一个踩过的坑:**首领必须用战斗形态的 id**。
//
// 每只首领在我方 petbase 里有三套形态(以女王蜂为例):
//   - 5015 普通
//   - 4021 首领形态      ← wiki 的 pets 表记的是这一套
//   - 8107 草系徽章-首领形态  ← **战斗里实际出现的是这一套**
//
// 早先照抄 wiki 的 base_id(4021 那套),结果「战斗中遇到的首领」与首领池对不上
// (0x1316 的 enemy_team 给的是 8107)—— 记录遇见进度时会永远匹配不上。
// 故这里要求全部落在 8101~8122,且形态名带「草系徽章-首领形态」。
func TestTrialBossesAreBattleForms(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, b := range db.TrialBosses() {
		if b < 8100 || b > 8130 {
			t.Errorf("首领 %d 不在 8101~8122 区间 —— 用了非战斗形态的 id(见本测试的说明)", b)
			continue
		}
		info, ok := db.PetBase(b)
		if !ok {
			t.Errorf("首领 %d 不在我方 petbase 里", b)
			continue
		}
		if info.Form != "草系徽章-首领形态" {
			t.Errorf("首领 %d 的形态是 %q, 期望「草系徽章-首领形态」", b, info.Form)
		}
	}
}

// TestTrialBossesNotInPools 断言首领不在各章普通池里 —— 两者是不同的遭遇来源
// (首领来自第 4 层的 22 人名单,普通池来自 1/2/3/5 层),混在一起会让
// 「遇见进度」重复计数。
func TestTrialBossesNotInPools(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	boss := map[uint32]bool{}
	for _, b := range db.TrialBosses() {
		boss[b] = true
	}
	for ch, pool := range db.trial.pools {
		for _, p := range pool {
			if boss[p] {
				t.Errorf("第 %d 章普通池里混进了首领 %d", ch, p)
			}
		}
	}
}
