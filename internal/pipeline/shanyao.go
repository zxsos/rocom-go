package pipeline

import (
	"time"

	"github.com/whoisnian/rocom-capture/internal/capture"
	"github.com/whoisnian/rocom-capture/internal/gamedata"
	"github.com/whoisnian/rocom-capture/internal/gcp"
	"github.com/whoisnian/rocom-capture/internal/server"
	"github.com/whoisnian/rocom-capture/internal/shanyao"
)

// ---- 闪耀大赛:同步「当前这一局 PVP」(隐藏页 #/shanyao)----
//
// 与草系试炼(trial.go)同一路数:解析与状态机在 internal/shanyao,这里只做
// 挂接与广播。状态挂在**账号**上(acctState.sy):一局跨传送/换场景,且断线
// 重连后服务器会重发战局,按连接存会在这些时刻丢掉。
//
// 触发的 opcode 全是 s2c 广播:
//
//	0x1316 进战(开局)  0x131a 回合开始(补齐对手阵容 + 回合号)
//	0x1324 演出(血量)  0x132c 结算(终局)
//
// 只有状态真的变了才广播:一局里 0x1324 近百条,而大多数只改一两处,
// 每条都整份重发会让前端无谓重渲染(状态机的 dirty 判定见 internal/shanyao)。

// handleShanyao 处理战斗相关广播,维护当前战局。
//
// 必须在 handleScene **之前**调用:0x132c 同时是场景消息(清野怪标记),
// handleScene 命中即返回(见 pipeline.go 的 handle)。
func (p *Pipeline) handleShanyao(m capture.Message, acc string) {
	if m.Direction != gcp.S2C {
		return
	}
	st := p.acct(acc)
	switch m.Opcode {
	case shanyao.OpBattleEnterNotify:
		e, ok := shanyao.ParseEnter(m.AppBody)
		if !ok {
			return
		}
		if st.sy == nil {
			st.sy = shanyao.NewTracker()
		}
		if st.sy.OnEnter(e) {
			p.pushShanyao(acc, st.sy)
		}
	case shanyao.OpBattleRoundStartNotify:
		if st.sy == nil {
			return // 没在局内(漏了进战包):不凭空造一局
		}
		// 回合包按 uin 分边(见 ParseRound 的注释),故必须给出本方 uin。
		uid, ok := uidFromAcc(acc)
		if !ok {
			return
		}
		r, ok := shanyao.ParseRound(m.AppBody, uint64(uid))
		if !ok {
			return
		}
		if st.sy.OnRound(r) {
			p.pushShanyao(acc, st.sy)
		}
	case shanyao.OpBattlePerformStartNotify:
		if st.sy == nil {
			return
		}
		pf, ok := shanyao.ParsePerform(m.AppBody)
		if !ok {
			return
		}
		if st.sy.OnPerform(pf) {
			p.pushShanyao(acc, st.sy)
		}
	case shanyao.OpBattleFinishNotify:
		if st.sy == nil {
			return
		}
		f, ok := shanyao.ParseFinish(m.AppBody)
		if !ok {
			return
		}
		if st.sy.OnFinish(f) {
			p.pushShanyao(acc, st.sy)
		}
	}
}

// shanyaoStats 把协议六维转成固定顺序的数组;没下发返回 nil(前端据此显示「—」)。
func shanyaoStats(st shanyao.PetStats) *server.ShanyaoStats {
	if !st.Has {
		return nil
	}
	lines := []shanyao.StatLine{st.HP, st.PhysicalAttack, st.MagicalAttack, st.PhysicalDefense, st.MagicalDefense, st.Speed}
	out := &server.ShanyaoStats{}
	for _, l := range lines {
		out.Race = append(out.Race, l.Race)
		out.Talent = append(out.Talent, l.Talent)
		out.BaseValue = append(out.BaseValue, l.BaseValue)
		out.EffortAdd = append(out.EffortAdd, l.EffortAdd)
	}
	return out
}

// raceSlice 按六维键的固定顺序展开静态种族值。
func raceSlice(race map[string]int) []uint32 {
	out := make([]uint32, 0, len(gamedata.CalcStatKeys()))
	for _, k := range gamedata.CalcStatKeys() {
		v := race[k]
		if v < 0 {
			v = 0
		}
		out = append(out, uint32(v))
	}
	return out
}

// shanyaoSkill 组一个技能槽:对局内数值优先,静态表兜底,并标出来源。
func (p *Pipeline) shanyaoSkill(sk shanyao.Skill) server.ShanyaoSkill {
	out := server.ShanyaoSkill{ID: sk.ID, Name: p.db.SkillName(sk.ID), Hits: sk.Hits}
	static, hasStatic := p.db.CalcSkillOf(sk.ID)
	if hasStatic {
		out.Type, out.Category = static.Type, static.Category
	}
	switch {
	case sk.HasPower:
		v := sk.Power
		out.Power, out.Source = &v, "battle"
	case hasStatic && static.BasePower != nil:
		v := int32(*static.BasePower)
		out.Power, out.Source = &v, "static"
	}
	// 动态规则的技能:basePower 不能直接当威力用,把 ruleId 一并给前端,由它按规则重算。
	if hasStatic && static.RuleID != nil {
		out.RuleID = static.RuleID
	}
	switch {
	case sk.Cost != 0:
		v := sk.Cost
		out.Cost = &v
	case hasStatic && static.Cost != nil:
		v := uint32(*static.Cost)
		out.Cost = &v
	}
	return out
}

// pushShanyao 组一份战局快照,缓存并广播。
func (p *Pipeline) pushShanyao(acc string, tr *shanyao.Tracker) {
	s := tr.Snapshot()
	payload := &server.ShanyaoPayload{
		Account:  acc,
		Ts:       time.Now().Unix(),
		Active:   s.Active,
		Finished: s.Finished,
		BattleID: s.BattleID,
		Mode:     s.Mode,
		Round:    s.Round,
		Result:   s.Result,
		Self:     p.shanyaoSide(s.Self),
		Foe:      p.shanyaoSide(s.Foe),
	}
	p.srv.SetLastShanyao(acc, payload)
	p.srv.Hub().Broadcast("shanyao", acc, payload)
}

// shanyaoSide 把一方转成对外载荷:补上名称与头像。
//
// 对手的宠物信息是残缺的(服务端不下发六维,未出场的连形态都不给),故各字段
// 一律「查到才填」,查不到留空让前端显示占位 —— 不猜、不补 0。
func (p *Pipeline) shanyaoSide(s shanyao.Side) *server.ShanyaoSide {
	out := &server.ShanyaoSide{UIN: s.UIN, Name: s.Name, Level: s.Level}
	if s.HasHP { // 体力可以是 0(打输就是 0),故看「有没有给」而不是值是否为正
		v := s.HP
		out.HP = &v
	}
	if s.HPMax > 0 {
		v := s.HPMax
		out.HPMax = &v
	}
	for _, pet := range s.Pets {
		item := server.ShanyaoPet{
			GID:        pet.GID,
			PetID:      pet.PetID,
			BaseConfID: pet.BaseConfID,
			Name:       pet.Name,
			Level:      pet.Level,
			Gender:     pet.Gender,
			Nature:     pet.Nature,
			Blood:      pet.Blood,
			Dead:       pet.Dead,
			OnField:    pet.OnField,
		}
		// 炫彩判据必须是 mutation_type & 8(见 internal/shanyao/opcodes.go 的警示注释)。
		item.Shiny = pet.Mutation&shanyao.MutationShinyBit != 0
		if pet.BaseConfID != 0 {
			if item.Name == "" {
				item.Name = p.db.PetFullName(pet.BaseConfID)
			}
			item.Species = p.db.PetFullName(pet.BaseConfID)
			// 炫彩优先取异色头像;没有专属异色图时 imageOf 自己会回退普通图。
			item.Img = p.db.PetImageByBase(pet.BaseConfID, item.Shiny).Head
		}
		if item.Img == "" && pet.ConfID != 0 {
			item.Img = p.db.PetImage(pet.ConfID, item.Shiny).Head
		}
		if pet.Nature != 0 {
			item.NatureName = p.db.Nature(pet.Nature)
		}
		if pet.Blood != 0 {
			item.BloodName = p.db.BloodName(pet.Blood)
		}
		for _, d := range pet.DamTypes {
			item.DamTypes = append(item.DamTypes, d)
			item.DamNames = append(item.DamNames, p.db.SkillDamType(d))
		}
		if pet.GlassType != 0 {
			item.GlassType, item.GlassValue = pet.GlassType, pet.GlassValue
			item.GlassName = p.db.GlassDesc(int32(pet.GlassType), int32(pet.GlassValue))
		}
		if pet.HasHP {
			v := pet.HP
			item.HP = &v
		}
		if pet.HPMax > 0 {
			v := pet.HPMax
			item.HPMax = &v
		}
		item.Stats = shanyaoStats(pet.Stats)
		if race, ok := p.db.CalcRaceOf(pet.BaseConfID); ok {
			item.RaceStats = raceSlice(race.Race)
		}
		// 「推算」= 协议没给六维,只能由种族值推;UI 必须标注,不能冒充精确值。
		item.Estimated = !pet.Stats.Has
		item.NatureMult = p.db.NatureMultipliers(pet.Nature)
		if pet.HasEnergy {
			v := pet.Energy
			item.Energy = &v
		}
		if t := p.db.FeatureNameOfBase(pet.BaseConfID); t != "" {
			item.Trait = t
		}
		for _, b := range pet.Buffs {
			buf := server.ShanyaoBuff{ID: b.BuffID, Stacks: b.Stacks, Type: b.Type, Skill: b.Skill}
			if n, ok := p.db.MarkNameOfBuff(b.BuffID); ok {
				buf.Name = n
			}
			item.Buffs = append(item.Buffs, buf)
		}
		for _, sk := range pet.Skills {
			item.Skills = append(item.Skills, p.shanyaoSkill(sk))
		}
		out.Pets = append(out.Pets, item)
	}
	return out
}
