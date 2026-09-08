package pipeline

import (
	"testing"

	"github.com/whoisnian/rocom-capture/internal/shanyao"
)

// TestShanyaoSkillTypeIsDamType 盯住「技能系别取自静态表的哪个字段」。
//
// 静态表里 CalcSkill.Type 是「主动/被动」,系别在 DamType —— 两个字段挨在一起、
// 语义却完全不同。曾把 Type 当成系别填进 ShanyaoSkill.Type,而前端拿它去克制表里
// indexOf:查不到就静默退化成「无克制、无本系 1.25 加成」,页面上看不出异常。
//
// 契约 fixture(contractShanyao)是手工构造的,不经过这里,守不住这条 —— 故单独留测试。
//
// 样本「虫击」(7130150):官方条目 type=主动、damType=虫。
func TestShanyaoSkillTypeIsDamType(t *testing.T) {
	p, _ := newTestPipeline(t)

	got := p.shanyaoSkill(shanyao.Skill{ID: 7130150})

	if got.Type != "虫" {
		t.Errorf("技能系别 = %q, 期望「虫」—— 若这里是「主动」,说明又把 CalcSkill.Type 当成系别了", got.Type)
	}
}
