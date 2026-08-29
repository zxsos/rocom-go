package pipeline

import (
	"time"

	"github.com/whoisnian/rocom-capture/internal/capture"
	"github.com/whoisnian/rocom-capture/internal/gcp"
	"github.com/whoisnian/rocom-capture/internal/pet"
	"github.com/whoisnian/rocom-capture/internal/store"
)

// ---- 精灵蛋(见 docs/data.md 3.6)----
//
// 蛋从四处露面,处理方式各异:
//   - 0x1344 背包分页全量:入库 + 末页对账(不在背包的删掉,与宠物列表同一套路)
//   - 0x0243 奖励通知:新得的蛋;flow_reason=223 即家园小窝下的蛋,顺手记双亲
//   - 0x0262 商店购买:远行商人的「神奇的蛋」等,新蛋只在这条回包里下发(不另发奖励通知)
//   - 0x0164 用道具(放蛋入孵蛋器)/0x0300 取出回包:都只当普通变化入库,不动 hatching 列
//   - 0x0312 孵化状态:进度更新 + 顶层 egg_gid[] 权威对账,**hatching 列只由它维护**(见 applyHatchStatus)
//
// hatching 列只由权威的 egg_gid 列表全量对账维护,两处下发点都收:
//   - 0x0102 登录数据(PetBackpackInfo.egg_gid):登录就有,不必等玩家打开孵蛋器面板
//   - 0x0312 开孵蛋器:带逐蛋进度,是最终的收敛点
// 放入/取出/破壳动作与背包快照(0x1344)的 start_hatch_time 都不可信
// (服务器取出蛋时不把它清零,只清进度),upsert 一律不碰该列、新行初始为 0。
// 任一处权威列表为空即表示孵蛋器空,把本账号所有在孵标记清零。
//   - 0x030b/0x030c 破壳:请求带 egg_gid,回包一到就把这颗蛋从库里删掉(它已不在背包里)
//
// 库里存的就是**背包现状**:页面只看背包,破壳/送人的蛋没人回看,故不留历史行。
// 双亲只在**收蛋那一刻**能确定:蛋 NPC 趴在母本的窝上,配对由服务器下发(见 home.go)。
// 快照落库后与宠物表脱钩,亲本日后被放生/赠送也不影响这颗蛋上已记录的双亲。

// eggSweep 累积一轮分页背包全量,末页对账(与 petSweep 同构,见 pets.go)。
type eggSweep struct {
	gids     map[uint32]bool
	order    []uint32 // 服务器下发顺序(背包里的原始次序,见 store.SetEggOrder)
	nextPage uint32
	valid    bool
	start    time.Time
}

// handleEgg 分发精灵蛋相关消息;返回是否已消费(消费了也仍会继续走宠物那一路,
// 因为破壳/奖励通知同时带着新宠物)。
func (p *Pipeline) handleEgg(m capture.Message, acc string) {
	sc := p.st.For(acc)
	switch {
	case m.Direction == gcp.C2S && m.Opcode == pet.OpCrackEggReq:
		if gid := pet.ParseCrackEggReq(m.AppBody); gid != 0 {
			p.conn(m.Session).crackEgg = gid // 等回包确认破壳成功再删
		}

	case m.Direction == gcp.S2C && m.Opcode == pet.OpLoginRsp:
		// 登录数据里就带着孵蛋器占用列表:不等玩家打开孵蛋器面板也能把在孵标记对齐。
		// 与 0x0312 是同一权威口径(见 pet.BackpackHatchSlots),两个下发点都收。
		if gids, ok := pet.BackpackHatchSlots(m.AppBody); ok {
			// 先记住:登录包往往先于背包分页到达,此刻库里还没有蛋,下面的对账改不动任何行;
			// 记住后由 upsertEggs 在蛋入库时逐颗判定(见 hatchGids 的注释)。
			known := make(map[uint32]bool, len(gids))
			for _, g := range gids {
				known[g] = true
			}
			p.conn(m.Session).hatchGids = known
			p.applyHatchSlots(sc, acc, gids, nil, nil, m.Time)
		}

	case m.Direction == gcp.S2C && m.Opcode == pet.OpGetBagItemInfoByPageRsp:
		p.applyEggPage(m, sc, acc)

	case m.Direction == gcp.S2C && m.Opcode == pet.OpCrackEggRsp:
		// 破壳成功(回包带孵出的宠物 gid):这颗蛋没了,当场删行,不必等下次开背包对账。
		if egg := p.conn(m.Session).crackEgg; egg != 0 && pet.ParseCrackEggRsp(m.AppBody) != 0 {
			sc.DeleteEgg(egg)
			p.conn(m.Session).crackEgg = 0
			p.srv.Hub().Broadcast("eggs", acc, map[string]any{"account": acc})
		}

	case m.Direction == gcp.S2C && (m.Opcode == pet.OpGoodsRewardNotify ||
		m.Opcode == pet.OpShopBuyItemRsp || m.Opcode == pet.OpUseBagItemRsp ||
		m.Opcode == pet.OpStopHatchRsp):
		// 收蛋 0x0243、购买 0x0262、放入孵蛋器 0x0164、取出 0x0300 都只当普通变化入库,
		// hatching 列不在这里动(权威状态只由 0x0312 对账维护,见 applyHatchStatus)。
		eggs := pet.ParseChangedEggs(m.AppBody)
		if len(eggs) == 0 {
			return
		}
		// 这几条只当普通变化入库;hatching 不动(权威状态只由 egg_gid 对账维护),
		// 故 known 传 nil,免得登录列表把刚取出/刚放入的状态盖回去。
		p.upsertEggs(sc, acc, eggs, m.Time, nil)
		if m.Opcode == pet.OpGoodsRewardNotify && pet.ParseFlowReason(m.AppBody) == pet.FlowReasonHomeLay {
			p.recordEggParents(m.Session, sc, eggs, m.Time)
		}

	case m.Direction == gcp.S2C && m.Opcode == pet.OpGetAllHatchStatusRsp:
		p.applyHatchStatus(m, sc, acc)
	}
}

// applyHatchStatus 处理孵化状态回包(0x0312):ret_info.changes 里的蛋先按常规入库
// (带精确的 last_hatch_update_sec),再用顶层 egg_gid[] 权威列表全量对账 hatching 列——
// 这是该列的唯一维护者(见 docs/data.md 3.6)。gids 为空(孵蛋器空)同样对账,
// 把本账号所有在孵标记清零(取出/破壳残留的最终收敛点)。
func (p *Pipeline) applyHatchStatus(m capture.Message, sc *store.Scoped, acc string) {
	eggs := pet.ParseChangedEggs(m.AppBody)
	skip := make(map[uint32]bool, len(eggs))
	for _, e := range eggs {
		skip[e.Gid] = true
	}
	if len(eggs) > 0 {
		// 0x0312 自带权威列表且随后就要全量对账,这里不必再按登录列表覆盖
		p.upsertEggs(sc, acc, eggs, m.Time, nil)
	}
	gids, secs := pet.ParseHatchStatus(m.AppBody)
	p.applyHatchSlots(sc, acc, gids, secs, skip, m.Time)
}

// applyHatchSlots 用权威的孵蛋器占用列表订正在孵标记,有改动就通知前端。
// gids 为空是有效输入(孵蛋器空)—— 此时会把本账号所有在孵标记清零;故「没解析出
// 背包」必须与「孵蛋器是空的」区分开,只有 ok==true 才调用本函数。
//
// 两处下发点:
//   - 登录 0x0102:不带逐蛋进度,secs/skip 传 nil → 只对账标记,进度留给随后到的 0x0312
//   - 开孵蛋器 0x0312:带 secs 与 skip(刚由 ret_info.changes 精确刷新过的蛋跳过)
func (p *Pipeline) applyHatchSlots(sc *store.Scoped, acc string, gids []uint32, secs []int32, skip map[uint32]bool, now time.Time) {
	if changed, err := sc.ReconcileHatching(gids, secs, skip, now.Unix()); err == nil && changed {
		p.srv.Hub().Broadcast("eggs", acc, map[string]any{"account": acc})
	}
}

// applyEggPage 处理背包分页全量:本页的蛋入库,收齐 1..total 后对账。
func (p *Pipeline) applyEggPage(m capture.Message, sc *store.Scoped, acc string) {
	eggs, page, total := pet.ParseBagEggs(m.AppBody)
	// 背包分页是蛋**首次入库**的地方。这里要带上登录时记住的孵蛋器列表:
	// 登录包先到、此刻库里还没有蛋,登录那次对账改不动任何行,只能由入库时逐颗判定。
	p.upsertEggs(sc, acc, eggs, m.Time, p.conn(m.Session).hatchGids)

	as := p.acct(acc)
	sw := as.eggSweep
	if page <= 1 || sw == nil { // 新一轮:从第 1 页起累积
		sw = &eggSweep{gids: map[uint32]bool{}, nextPage: 1, valid: page <= 1, start: m.Time}
		as.eggSweep = sw
	}
	if page != sw.nextPage { // 乱序/漏页:本轮不对账,避免误标
		sw.valid = false
	}
	sw.nextPage = page + 1
	for _, e := range eggs {
		if !sw.gids[e.Gid] {
			sw.order = append(sw.order, e.Gid)
		}
		sw.gids[e.Gid] = true
	}
	if total == 0 || page < total {
		return
	}
	if sw.valid {
		sc.PruneMissingEggs(sw.gids, sw.start.Unix())
		sc.SetEggOrder(sw.order) // 收齐了才落次序:半轮的顺序是错的
	}
	as.eggSweep = nil
	p.srv.Hub().Broadcast("eggs", acc, map[string]any{"account": acc})
}

// upsertEggs 把解析出的蛋转成展示模型入库,并通知前端刷新。
// now 是消息时刻(离线回放同样按包走,见 store.UpsertEggs)。
// known 是本连接记住的孵蛋器占用列表(登录数据给的权威口径,可能为 nil);
// 蛋入库时拿它逐颗判定在孵与否 —— 登录包通常先于背包分页到达,等它到时库里还没有蛋,
// 光靠登录那一次对账改不动任何行,必须由这里补上。
func (p *Pipeline) upsertEggs(sc *store.Scoped, acc string, eggs []pet.Egg, now time.Time, known map[uint32]bool) {
	if len(eggs) == 0 {
		return
	}
	views := make([]*pet.EggView, 0, len(eggs))
	for _, e := range eggs {
		views = append(views, pet.ToEggView(e, p.db))
	}
	// known 为 nil 时 UpsertEggs 不碰 hatching 列(权威状态只由 egg_gid 对账维护,
	// 背包快照与放入/取出动作回包的 start_hatch_time 都不可信——取出时不清零);
	// 非 nil 即登录数据给的权威列表,逐颗写定。
	if err := sc.UpsertEggs(views, now.Unix(), known); err == nil {
		p.srv.Hub().Broadcast("eggs", acc, map[string]any{"account": acc})
	}
}

// recordEggParents 给刚从小窝收上来的蛋记双亲快照。
//
// 认领靠「刚点的那颗蛋 NPC」(c2s 0x0137 记下的 pendingEgg):它挂在母本的窝上,母本由
// furniture_guid 定位,父本取服务器下发的配对候选。窝里有好几颗同种蛋、或没抓到那次交互时,
// 退一步按**蛋物品 id** 在当前家园里找唯一匹配的窝;仍不唯一就不记(宁缺毋错)。
func (p *Pipeline) recordEggParents(conn string, sc *store.Scoped, eggs []pet.Egg, now time.Time) {
	cs := p.conns[conn]
	if cs == nil || cs.home == nil {
		return
	}
	h := cs.home
	for _, e := range eggs {
		src := h.pendingEgg
		if src != nil && (src.itemID != e.ItemID || now.Sub(h.pendingSince) > pendingEggTTL) {
			src = nil // 点的那颗与发下来的对不上(或隔太久):不认
		}
		if src == nil {
			src = h.uniqueEggByItem(e.ItemID)
		}
		if src == nil {
			continue
		}
		if ps := p.parentsOf(sc, h, src.furniture, now); ps != nil {
			sc.SetEggParents(e.Gid, ps)
		}
		if h.pendingEgg == src {
			h.pendingEgg = nil
		}
	}
}

// uniqueEggByItem 在当前家园里按蛋物品 id 找唯一的那颗窝上蛋;不唯一返回 nil。
func (h *homeState) uniqueEggByItem(item uint32) *homeEgg {
	var found *homeEgg
	for _, e := range h.eggs {
		if e.itemID != item {
			continue
		}
		if found != nil {
			return nil // 多个候选:无从区分
		}
		found = e
	}
	return found
}

// parentsOf 组一颗蛋的双亲快照:母本是窝里那只,父本取配对候选(多于一个即串窝,标 Ambiguous)。
func (p *Pipeline) parentsOf(sc *store.Scoped, h *homeState, furniture uint64, now time.Time) *pet.EggParents {
	actor, mother := h.petAt(furniture)
	if mother == nil {
		return nil
	}
	out := &pet.EggParents{Mother: p.parentSnap(sc, mother.PetGid, mother.Name), RecordedAt: now.Unix()}
	for _, mate := range h.matesOf(actor) {
		mp := h.pets[mate]
		if mp == nil {
			continue
		}
		if s := p.parentSnap(sc, mp.PetGid, mp.Name); s != nil {
			out.Fathers = append(out.Fathers, *s)
		}
	}
	out.Ambiguous = len(out.Fathers) > 1
	return out
}

// parentSnap 取一只亲本在此刻的快照(个体属性来自宠物库;宠物尚未入库时至少留下 gid 与名字)。
func (p *Pipeline) parentSnap(sc *store.Scoped, gid uint32, name string) *pet.EggParent {
	s := &pet.EggParent{Gid: gid, Name: name}
	pp, err := sc.GetPet(gid)
	if err != nil || pp == nil {
		return s
	}
	pet.FillSizePercentile(p.db, pp)
	if s.Name == "" {
		s.Name = pp.Name
	}
	s.Species, s.ConfID, s.Img = pp.Species, pp.ConfID, pp.Image.Head
	s.Gender, s.HeightM, s.WeightKg = pp.Gender, pp.HeightM, pp.WeightKg
	s.HeightPct, s.WeightPct = pp.HeightPct, pp.WeightPct
	s.Voice, s.Nature, s.Talent = pp.Voice, pp.Nature, pp.TalentRank
	return s
}
