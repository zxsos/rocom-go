package pipeline

import "testing"

// TestTrialLogNameResolved 锁住「流水里查得到名的 id 由后端补名」:
//
// bless(选技能)的 ids = [effect, 技能],上方技能卡按官方 SKILL_CONF 给了名字,
// 流水里却只剩「0 / 7020440」这种原样透传的数字 —— 名是同源同表,漏的只是推送
// 前没查这一下。reward/shop 同理。查不到的 id(如特性 288xxx)没有名称表,
// 不补名,由前端回退显示原始数字。
func TestTrialLogNameResolved(t *testing.T) {
	p, _ := newTestPipeline(t)
	catalog := p.db.SkillCatalog()
	if len(catalog) == 0 {
		t.Skip("技能目录为空(skills.json 未生成?)")
	}
	id, want := catalog[0].ID, catalog[0].Name
	if id == 0 || want == "" {
		t.Skip("技能目录第一条没有可查名样本")
	}

	r := trialRunWithSelection(nil)
	// state 里按时间正序追加(先 bless 后 reward);推送时才倒成最新在前
	r.log = []trialLogEntry{
		{ts: 1, kind: "bless", label: "选择技能", ids: []uint32{0, id}},
		{ts: 2, kind: "reward", label: "直接收下", ids: []uint32{id}},
	}
	out := p.trialRunPayload(r, nil)
	if len(out.Log) != 2 {
		t.Fatalf("流水条数 = %d,期望 2(最新在前)", len(out.Log))
	}
	if out.Log[0].Kind != "reward" || out.Log[1].Kind != "bless" {
		t.Fatalf("流水顺序 = %s,%s,期望 reward,bless", out.Log[0].Kind, out.Log[1].Kind)
	}
	for i, e := range out.Log {
		if e.Name != want {
			t.Errorf("log[%d](%s) name = %q,期望 %q(技能 id %d 应查官方 SKILL_CONF)",
				i, e.Kind, e.Name, want, id)
		}
	}
	// 技能 id 本身仍保留在 ids 里(title/对照抓包用)
	if out.Log[1].IDs[1] != id {
		t.Errorf("bless 流水 ids 被改掉: %v,期望末尾仍是 %d", out.Log[1].IDs, id)
	}
}
