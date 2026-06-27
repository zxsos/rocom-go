package pet

import (
	"github.com/whoisnian/rocom-capture/internal/gamedata"
	"github.com/whoisnian/rocom-capture/internal/pb"
)

// Stat 是一项六维属性。
type Stat struct {
	Value     int32 `json:"value"`     // 面板基础值
	Talent    int32 `json:"talent"`    // 个体值(天赋)
	NatureAdd int32 `json:"natureAdd"` // 性格修正(正升负降)
}

// Pet 是用于前端展示/存储的业务模型(已中文化)。
type Pet struct {
	Gid     uint32 `json:"gid"`     // 唯一实例 id
	ConfID  uint32 `json:"confId"`  // 种类配置 id
	Species string `json:"species"` // 种类名
	Name    string `json:"name"`    // 昵称
	Level   uint32 `json:"level"`

	NatureID uint32   `json:"natureId"`
	Nature   string   `json:"nature"` // 性格名
	Gender   string   `json:"gender"` // ♂ / ♀
	Types    []string `json:"types"`  // 系别中文(可多系)

	HeightM  float64 `json:"heightM"`  // 身高(米)
	WeightKg float64 `json:"weightKg"` // 体重(千克)
	Voice    int32   `json:"voice"`    // 声音值

	TalentRank      string `json:"talentRank"` // 天分评价
	Medal           string `json:"medal"`      // 佩戴奖牌名
	MedalDesc       string `json:"medalDesc"`
	WearMedalConfID uint32 `json:"wearMedalConfId"`
	PartnerMark     string `json:"partnerMark"`  // 标记
	Speciality      string `json:"speciality"`   // 特长
	SpecialityID    uint32 `json:"specialityId"`

	CatchTime int64 `json:"catchTime"` // 捕捉时间(unix 秒)
	Shiny     bool  `json:"shiny"`     // 异色/变异

	HP        Stat `json:"hp"`
	Attack    Stat `json:"attack"`     // 物攻
	Defense   Stat `json:"defense"`    // 物防
	SpAttack  Stat `json:"spAttack"`   // 魔攻
	SpDefense Stat `json:"spDefense"`  // 魔防
	Speed     Stat `json:"speed"`

	SkillIDs []uint32 `json:"skillIds"`
}

func toStat(a *pb.PetAttributeData) Stat {
	if a == nil {
		return Stat{}
	}
	return Stat{
		Value:     int32(a.GetBaseValue()),
		Talent:    int32(a.GetTalent()),
		NatureAdd: a.GetTalentAddValue(),
	}
}

// ToPet 把解码后的 PetData 结合名称库转成业务模型。
func ToPet(p *pb.PetData, db *gamedata.DB) *Pet {
	types := make([]string, 0, len(p.GetSkillDamType()))
	for _, t := range p.GetSkillDamType() {
		if name := db.SkillDamType(int32(t)); name != "" {
			types = append(types, name)
		}
	}

	out := &Pet{
		Gid:      p.GetGid(),
		ConfID:   p.GetConfId(),
		Species:  db.Species(p.GetConfId()),
		Name:     string(p.GetName()),
		Level:    p.GetLevel(),
		NatureID: p.GetNature(),
		Nature:   db.Nature(p.GetNature()),
		Gender:   gamedata.GenderName(p.GetGender()),
		Types:    types,
		HeightM:  float64(p.GetHeight()) / 100,
		WeightKg: float64(p.GetWeight()) / 1000,
		Voice:    p.GetVoice(),

		TalentRank:      db.TalentRate(p.GetTalentRank()),
		WearMedalConfID: p.GetWearMedalConfId(),
		PartnerMark:     db.PartnerMark(int32(p.GetPartnerMark())),
		SpecialityID:    p.GetSpecialityId(),
		Speciality:      db.Speciality(p.GetSpecialityId()),

		CatchTime: int64(p.GetAddTime()),
		Shiny:     p.GetMutationType() != 0,
	}

	if m, ok := db.Medal(p.GetWearMedalConfId()); ok {
		out.Medal = m.Name
		out.MedalDesc = m.Desc
	}

	if attr := p.GetAttributeInfo(); attr != nil {
		out.HP = toStat(attr.GetHp())
		out.Attack = toStat(attr.GetAttack())
		out.Defense = toStat(attr.GetDefense())
		out.SpAttack = toStat(attr.GetSpecialAttack())
		out.SpDefense = toStat(attr.GetSpecialDefense())
		out.Speed = toStat(attr.GetSpeed())
	}

	if sk := p.GetSkill(); sk != nil {
		for _, s := range sk.GetSkillData() {
			out.SkillIDs = append(out.SkillIDs, s.GetId())
		}
	}
	return out
}
