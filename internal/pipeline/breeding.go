package pipeline

import (
	"fmt"
	"log"
	"time"

	"github.com/zxsos/roco-go/internal/pet"
	"github.com/zxsos/roco-go/internal/store"
)

// 培育线的自动记录(培育页,见 internal/pet/breeding.go 与 docs/data.md 3.6)。
//
// 一代的生命周期分三段,**段与段之间的关联都落在库里**,不再依赖进程内存:
//
//  1. 收蛋(recordLay):小窝收蛋那一刻双亲最全 —— 母本就趴在窝上、配对刚由服务器下发。
//     当场建/找线并追加一条「待孵」代,把 lineId/gen 写回蛋行,随即广播:玩家正盯着小窝,
//     培育页立刻多出一条记录(这就是本次要的「收蛋就有反馈」);
//  2. 破壳(recordHatch):回包给出孵出的 gid,按蛋行上的 lineId/gen 定位那一代,把 gid 记上;
//  3. 认领(claimHatchedChild):子代进背包时补上快照,这一代才算完整。
//
// 为什么把关联落库:收蛋与破壳之间隔着**整个孵化倒计时**(几小时到几天),破壳与认领虽然只差
// 一瞬,却也可能被掉线、重启、离线回放打断。原先第 2→3 步靠 connState 里的内存配对 + 15 分钟
// 保鲜期,一断就丢一代 —— 而 gid 早就随那一代落了库,按它反查即可,根本不需要保鲜期。
//
// 万一某段没走到(离线、漏包)就停在原地等玩家:待孵的代一直显示「待孵」,破壳后一直显示
// 「待认领」—— 不拿「窗口内新出现的同种宠物」硬猜,错认一只会把整条培育史带偏,而培育史的
// 价值全在「这一代的双亲与子代到底是谁」。

// hatchClaim 是破壳那一刻确认下来的配对:哪条线的第几代,在等哪个 gid 的子代入库。
//
// 它只是**快路径**(同一次破壳的后续消息立刻认领,省一次查询);连不上的时候还有慢路径
// (store.FindLineClaimingChild 按 gid 反查),故不再需要 at 与保鲜期 —— gid 是唯一实例 id,
// 匹配本身就是精确的,时间窗口只会让跨会话的认领静默失败。
type hatchClaim struct {
	lineID string
	gen    int
	child  uint32
}

// recordLay 收蛋即记:建/找线并追加一条「待孵」代,回填 ps.LineID / ps.Gen。
//
// 必须在 SetEggParents **之前**调用并回填:那条 SQL 带 `parents IS NULL OR parents=''`
// 只写一次,lineId/gen 得搭双亲快照那一趟车一起落库,事后补不进去(见 store/egg.go)。
//
// 宁缺毋错(与 recordEggParents 同一口径):母本缺失、品种认不出,一律不建代;任何失败都
// 不影响蛋入库与 eggs 广播 —— 记线是锦上添花,蛋与双亲本身才是玩家要看到的。
//
// fallbackSpecies 用于母本快照里没有物种时兜底(收蛋那一刻她还没进宠物库,快照只有 gid
// 与名字):蛋的物种就是母本的物种,故拿蛋自己的物种名认品种是成立的。
func (p *Pipeline) recordLay(sc *store.Scoped, acc string, eggGid uint32, ps *pet.EggParents, fallbackSpecies string, now time.Time) {
	if sc == nil || ps == nil || ps.Mother == nil || eggGid == 0 {
		return
	}
	species := ps.Mother.Species
	if species == "" {
		species = fallbackSpecies
	}
	if species == "" {
		return // 连品种都认不出:这条史无从归属(蛋的物种随母本)
	}
	// 品种取自**母本快照的 Evo**(蛋的物种随母本):链口径下「阿米亚特」与「罗隐」是同一条线,
	// 按物种名找线会把它们劈成两条。Evo==0(无链形态)时退回按名字,见 pet.ChainRef。
	ref := pet.ChainRefOf(p.db, ps.Mother.Evo, species)
	hit, parent, err := sc.FindLineForMother(ps.Mother.Gid, ref)
	if err != nil {
		log.Printf("用户 %s 查种母 %d(品种 %s Evo=%d)的培育线失败: %v",
			acc, ps.Mother.Gid, species, ref.Evo, err)
		return
	}
	// 三级归属,次序不能换(见 store.FindLineForMother 的注释):
	//   ① hit    这只种母身上已经挂了一条线 → 直接沿用,不换种母就不换线;
	//   ② parent 她是某条线孵出来的 → 开**子线**接在那条之后(子代接班当种母);
	//   ③ 都没有 → 外来血脉(野外抓的),开**独立新线**。不去猜挂到哪条已有线下:
	//      同品种 5 条线里挑一条就是猜,猜错是把一颗蛋记进别人的培育史。
	line := hit
	if line == nil {
		id := autoLineID(ref.Evo, species, ps.Mother.ConfID, ps.Mother.Gid, now)
		if parent != nil {
			line = pet.NewChildLine(id, parent, ps, now.Unix())
		} else {
			// 目标留空,由玩家回头再定 —— 没目标不耽误记录,反而能先攒下几代再决定往哪刷。
			line = pet.NewAutoLine(id, ps, now.Unix())
		}
	}
	// 老线 / 手工建的线没有 MotherGid:此刻把这只种母**固定**下来。
	//
	// 不固定的话每次收蛋都要重走一遍「末代派生 → 空线兜底」,而兜底那一条(同品种取最近更新
	// 的)仍然是会漂移的 —— 漂移正是本次要去掉的东西。固定之后,这条线从此只认这只种母,
	// 除非玩家换种母(那会开子线或独立新线)。
	if line.MotherGid == 0 {
		line.MotherGid = ps.Mother.Gid
	}
	gen := pet.AppendPending(line, ps, eggGid, now.Unix())
	if gen == 0 {
		return
	}
	line.UpdatedAt = now.Unix()
	if err := sc.UpsertBreedingLine(line); err != nil {
		log.Printf("用户 %s 记录培育线 %s 第 %d 代失败: %v", acc, line.ID, gen, err)
		return
	}
	ps.LineID, ps.Gen = line.ID, gen
	// 收蛋这一刻就推一次:玩家正盯着小窝,培育页随即多出一条「待孵」—— 要的就是这个反馈。
	p.srv.Hub().Broadcast("breeding", acc, map[string]any{"account": acc})
}

// recordHatch 破壳回包给出孵出的 gid 时,把它记到那一代上(子代快照要等它进背包)。
//
// 必须在 DeleteEgg **之前**调用:lineId/gen 与双亲快照只存在蛋那一行上。
// childGid 为 0(回包没给出子代)时不记 —— 认不了子代的一代只能停在「待孵」。
//
// 蛋行上没记 lineId/gen 时**回退成就地建代**:收蛋那次没抓到双亲、或这颗蛋是这次改动之前
// 收的老蛋 —— 破壳那一刻至少知道双亲与物种,记下来总比丢了好(与升级前的行为一致)。
func (p *Pipeline) recordHatch(cs *connState, sc *store.Scoped, acc string, eggGid, childGid uint32, now time.Time) {
	if cs == nil || eggGid == 0 || childGid == 0 {
		return
	}
	info, err := sc.GetEggBreedingInfo(eggGid)
	if err != nil {
		log.Printf("用户 %s 取蛋 %d 的物种与双亲失败: %v", acc, eggGid, err)
		return
	}
	// 蛋上没有双亲快照(没抓到小窝那次交互,或这蛋不是家园产的):没有可记的一代。
	if info == nil || info.Parents == nil || info.Parents.Mother == nil {
		return
	}
	ps := info.Parents
	// 收蛋时没记上(老蛋 / 那次没抓到双亲)就在此刻补建 —— 与升级前的行为一致。
	if ps.LineID == "" || ps.Gen == 0 {
		p.recordLay(sc, acc, eggGid, ps, info.Species, now)
	}
	if ps.LineID == "" || ps.Gen == 0 {
		return
	}
	line, err := sc.GetBreedingLine(ps.LineID)
	if err != nil {
		log.Printf("用户 %s 取培育线 %s 失败: %v", acc, ps.LineID, err)
		return
	}
	if line == nil {
		return // 线被玩家删了:这一代随之消失,正常
	}
	if !pet.MarkHatched(line, ps.Gen, childGid) {
		return // 那一代已被丢弃或认领过:不再改
	}
	line.UpdatedAt = now.Unix()
	if err := sc.UpsertBreedingLine(line); err != nil {
		log.Printf("用户 %s 记录培育线 %s 第 %d 代破壳失败: %v", acc, line.ID, ps.Gen, err)
		return
	}
	cs.hatch = &hatchClaim{lineID: line.ID, gen: ps.Gen, child: childGid}
	p.srv.Hub().Broadcast("breeding", acc, map[string]any{"account": acc})
}

// claimHatchedChild 把刚入库的新宠物认领给等着它的那一代。
//
// 两条路径,结论一致、共用 pet.claimAt:
//   - 快路径:连接还在(同一次破壳的后续消息),cs.hatch 里就有 lineID/gen,直接定位;
//   - 慢路径:连接已不在(离线回放、进程重启、玩家在别的设备上孵的蛋),按 gid 去库里
//     反查「哪条线在等它」(store.FindLineClaimingChild)—— gid 早就随那一代落了库。
//
// 原来的 15 分钟保鲜期一并删掉:gid 是唯一实例 id、不会复用,匹配本身是精确的,
// 时间窗口是多余的保险,却会让跨会话的认领**静默失败**(那正是丢一代的成因)。
func (p *Pipeline) claimHatchedChild(cs *connState, sc *store.Scoped, acc string, pp *pet.Pet, now time.Time) {
	if pp == nil {
		return
	}
	var line *pet.BreedingLine
	var gen int
	if cs != nil && cs.hatch != nil && cs.hatch.child == pp.Gid {
		// 快路径:先清再查 —— 认领失败也不该再拿它去试下一只宠物。
		h := cs.hatch
		cs.hatch = nil
		var err error
		line, err = sc.GetBreedingLine(h.lineID)
		if err != nil {
			log.Printf("用户 %s 取培育线 %s 失败: %v", acc, h.lineID, err)
			return
		}
		gen = h.gen
	} else {
		// 慢路径:只在有代等着这只宠时才查得到,查不到就是普通的新宠物。
		var err error
		line, err = sc.FindLineClaimingChild(pp.Gid)
		if err != nil {
			log.Printf("用户 %s 查等待子代 %d 的培育线失败: %v", acc, pp.Gid, err)
			return
		}
	}
	if line == nil {
		return // 没有代在等它(普通新宠物),或线被玩家删了:都不算异常
	}
	pet.FillSizePercentile(p.db, pp)
	snap := pet.ParentSnapshot(pp)
	// 认领之前先算一次是否已达成:自动置 done 只在「由未达成变达成」时动手,否则玩家把线
	// 手动改回进行中之后,每次破壳都会被掰回 done(见 pet.AutoDoneOnReach)。
	reachedBefore := pet.ReachGoal(line)
	// 快路径按代数认领(破壳时刚定下的),慢路径按 gid 认领 —— 两处共用 pet.claimAt。
	var ok bool
	if gen != 0 {
		ok = pet.ClaimGeneration(line, gen, snap)
	} else {
		ok = pet.ClaimChild(line, pp.Gid, snap)
	}
	if !ok {
		return // 这一代已被认领(同一只宠物经多个 opcode 重复下发):不再改
	}
	line.UpdatedAt = now.Unix()
	if pet.AutoDoneOnReach(line, reachedBefore) {
		log.Printf("用户 %s 的培育线 %s 已刷到目标,自动标记为已达成", acc, line.ID)
	}
	if err := sc.UpsertBreedingLine(line); err != nil {
		log.Printf("用户 %s 认领培育线 %s 的子代失败: %v", acc, line.ID, err)
		return
	}
	p.srv.Hub().Broadcast("breeding", acc, map[string]any{"account": acc})
}

// claimPendingChildren 全量对账收尾后补扫一次:把「记着子代 gid、却还没快照」的代补上。
//
// 为什么需要它:认领挂在 applyNewPet(增量消息 + isNew)里,而**断网期间孵出的子代**是随
// 背包分页全量(applyPetPage)进库的 —— 那条路不调认领,于是那一代永远停在「待认领」。
// 培育线是**事件流**产物:宠物与蛋都有全量对账可以自愈,唯独它没有,故缺口只能在这里补。
//
// 时点必须是**全量对账之后**:对账前宠物库可能还没收全,查不到不代表那只宠不在了;
// 查不到的处理是「跳过并留给页面提示」,绝不猜、绝不自动丢弃 —— 认错一只比不认更糟。
//
// 仍用 pet.ClaimChild(按 gid),与 claimHatchedChild 的慢路径共用 claimAt:搬动逻辑只有一处。
func (p *Pipeline) claimPendingChildren(sc *store.Scoped, acc string, now time.Time) {
	lines, err := sc.ListBreedingLines()
	if err != nil {
		log.Printf("用户 %s 补扫认领时读培育线失败: %v", acc, err)
		return
	}
	for _, l := range lines {
		// 先收集再逐个认领:ClaimChild 会从 Pending 里删元素,边遍历边改会错乱。
		var gids []uint32
		for _, g := range l.Pending {
			if g.ChildGid != 0 && g.Child == nil {
				gids = append(gids, g.ChildGid)
			}
		}
		if len(gids) == 0 {
			continue
		}
		reachedBefore := pet.ReachGoal(l)
		claimed := 0
		for _, gid := range gids {
			pp, err := sc.GetPet(gid)
			if err != nil {
				log.Printf("用户 %s 补扫认领取宠物 %d 失败: %v", acc, gid, err)
				continue
			}
			if pp == nil {
				// 不在库:放生/送人,或断网期间入库又放生(工具从未见过它)。
				// 留着 ChildGid 让页面说清「孵出的 #gid 已不在库」,由玩家决定补录还是丢弃。
				continue
			}
			pet.FillSizePercentile(p.db, pp)
			if !pet.ClaimChild(l, gid, pet.ParentSnapshot(pp)) {
				continue
			}
			claimed++
		}
		if claimed == 0 {
			continue
		}
		l.UpdatedAt = now.Unix()
		if pet.AutoDoneOnReach(l, reachedBefore) {
			log.Printf("用户 %s 的培育线 %s 已刷到目标,自动标记为已达成", acc, l.ID)
		}
		if err := sc.UpsertBreedingLine(l); err != nil {
			log.Printf("用户 %s 补扫认领后保存培育线 %s 失败: %v", acc, l.ID, err)
			continue
		}
		log.Printf("用户 %s 补扫认领 %d 代(断网/漏包期间孵出的子代)", acc, claimed)
		p.srv.Hub().Broadcast("breeding", acc, map[string]any{"account": acc})
	}
}

// autoLineID 造一条自动建的线的 id。
//
// 带时刻后缀而不是「auto-<品种>」:同品种的线可以被归档后再开新的,固定 id 会把上一次的
// 历史覆盖掉(线是同 id 覆盖写),而培育史恰恰是不能丢的东西。
//
// **必须带种母 gid**:线的身份已经改成种母(见 pet.BreedingLine.MotherGid),同一个品种
// 可以同时有几条线在推进(几个窝、几只母本各孵各的)。键里没有种母的话,同一秒内收的两颗
// 蛋会算出同一个 id —— 而线是同 id 覆盖写,后写的那条会把前一条**整条培育史抹掉**。
func autoLineID(evo uint32, species string, confID, motherGid uint32, now time.Time) string {
	key := species
	switch {
	case evo != 0:
		// 链口径下品种就是链,用链 id 当键:形态名会随「记录时那只处在哪个阶段」而变
		// (同一条链先记成阿米亚特、后记成罗隐),拿名字当键会让同品种的自动线落到不同 id 上。
		key = fmt.Sprint(evo)
	case key == "":
		key = fmt.Sprint(confID) // 品种名可能带空格/多语言,用 conf_id 更稳
	}
	return fmt.Sprintf("auto-%s-%d-%d", key, motherGid, now.Unix())
}
