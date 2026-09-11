package store

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/zxsos/rocom-go/internal/pet"
)

// Filter 是宠物列表的筛选/排序/分页参数。
type Filter struct {
	Search string   // 名称/种类模糊
	Types  []string // 系别(任一匹配)
	Nature string   // 单个性格(精确等值)
	// NatureIn 性格多选:命中其中任一即算命中。
	//
	// 存在理由:性格是 6×6 方阵(行=+10% 维度、列=-10% 维度,30 个非中性性格填满,
	// 对角线空),前端做成矩阵后支持三种选法 —— 点格子(精确一个)、点行头(某维度↑
	// 的 5 个)、点列头(某维度↓ 的 5 个),后两种天然是多选。
	// 与 Nature 互斥:两者都给时以 NatureIn 为准(前端只会给一个)。
	NatureIn      []string
	NatureExclude []string // 性格"其他":排除这些热门性格
	Gender        string
	TalentRank    string
	MedalIDs      []uint32 // 拥有该奖牌(pet_medal 里含任一 id)即命中;由服务层将奖牌名解析为 id
	// 奖牌特征:按体重百分位/嗓音的数值判定(与地图奖牌筛选同口径),可多选,多选=同时满足。
	// 值为 0 表示该项未启用(边界值均为非 0 阈值)。
	WeightPctMin int // 大块头:体重百分位下限(weight_pct>=?)
	WeightPctMax int // 小不点:体重百分位上限(weight_pct<=?)
	VoiceMin     int // 婉转声:嗓音下限(voice>=?)
	VoiceMax     int // 粗嗓门:嗓音上限(voice<=?)
	Speciality   string
	// EggGroups 蛋组名多选:宠物拥有其中**任一**即命中(OR)。
	//
	// 为什么是 OR 而不是「同时拥有」(AND):一只宠物最多 2 个蛋组,AND 下选到第
	// 三个就必然空结果,而面板并未禁止选三个 —— 用户只会以为筛选坏了。蛋组的实际
	// 用法是「这两组里有哪些宠物」,OR 才对得上意图。
	//
	// 与 Types 刻意不同:系别是宠物的**并列**属性(双系宠物要靠 AND 收窄),
	// 但两者在前端都是 chip 多选,故各自的口径都写进了 hint 文案。
	EggGroups []string
	// EggGroupsExact 蛋组**集合相等**:候选的蛋组必须与目标一模一样(顺序无关)。
	//
	// 与上面 EggGroups 是**两种口径**,服务于两种意图:上面那套是筛选面板的「这几组里
	// 有哪些」,这里的来自列表长按的「跟我这一套蛋组相同的还有谁」—— 配种要的是同蛋组,
	// 多一组少一组都不是同一个组合。故这里是 AND + 个数相等,不是 OR。
	//
	// 两者同时给出时以**本字段为准**(与 Nature/NatureIn 同法):前端只会给一个,而
	// 叠加生效会让用户看到「筛选面板上选中的组没生效」这种查不出来的怪结果。
	EggGroupsExact []string
	PartnerMark    string
	Shiny          string // "", "1", "0"
	Colorful       string // "", "1", "0"
	Form           string // 地区/季节形态名(精确匹配)
	Box            string // 宠物盒,形如 "13-性格1"(取前导整数为 box_id 过滤)
	CatchAfter     int64  // 捕捉时间下限(unix 秒;>0 时筛 catch_time>=该值,由前端按所选区间算)
	LevelMin       int
	LevelMax       int
	Sort           string
	Order          string
	Page           int
	PageSize       int
}

var sortColumns = map[string]string{
	"gid": "gid", "level": "level", "catchTime": "catch_time",
	// 体重/身高按「形态范围内百分位」排序,便于跨种族找相对自身偏大/偏小的宠物(缺范围者排末尾)。
	"weight": "weight_pct", "height": "height_pct", "voice": "voice",
	"hp": "hp", "attack": "attack", "defense": "defense",
	"spAttack": "sp_attack", "spDefense": "sp_defense", "speed": "speed",
	"name": "name", "species": "species",
}

// buildWhere 由筛选条件构造 WHERE 子句与参数(列名均属 pets 表)。首谓词恒为
// account=?(account 作 args[0]);盒子筛选子查询用相关子查询 account=pets.account
// 收窄到同账号,避免额外占位符。
func buildWhere(f Filter, account string) (string, []any) {
	where := []string{"account=?"}
	args := []any{account}
	if f.Search != "" {
		where = append(where, "(name LIKE ? OR species LIKE ?)")
		args = append(args, "%"+f.Search+"%", "%"+f.Search+"%")
	}
	addEq := func(col, val string) {
		if val != "" {
			where = append(where, col+"=?")
			args = append(args, val)
		}
	}
	// 性格:多选优先(前端的矩阵只在点单格时才给 Nature,那时 NatureIn 为空)。
	// 用 addIn 与 NatureExclude 同一套占位符拼法,保持 SQL 构造只有一种写法。
	addIn := func(col string, vals []string) {
		if len(vals) == 0 {
			return
		}
		ph := make([]string, len(vals))
		for i, v := range vals {
			ph[i] = "?"
			args = append(args, v)
		}
		where = append(where, col+" IN ("+strings.Join(ph, ",")+")")
	}
	if len(f.NatureIn) > 0 {
		addIn("nature", f.NatureIn)
	} else {
		addEq("nature", f.Nature)
	}
	if len(f.NatureExclude) > 0 {
		ph := make([]string, len(f.NatureExclude))
		for i, n := range f.NatureExclude {
			ph[i] = "?"
			args = append(args, n)
		}
		where = append(where, "nature NOT IN ("+strings.Join(ph, ",")+")")
	}
	addEq("gender", f.Gender)
	addEq("talent_rank", f.TalentRank)
	addEq("speciality", f.Speciality)
	addEq("partner_mark", f.PartnerMark)
	addEq("form", f.Form)
	if f.Shiny == "1" {
		where = append(where, "shiny=1")
	} else if f.Shiny == "0" {
		where = append(where, "shiny=0")
	}
	if f.Colorful == "1" {
		where = append(where, "colorful=1")
	} else if f.Colorful == "0" {
		where = append(where, "colorful=0")
	}
	if f.CatchAfter > 0 {
		where = append(where, "catch_time>=?")
		args = append(args, f.CatchAfter)
	}
	if f.LevelMin > 0 {
		where = append(where, "level>=?")
		args = append(args, f.LevelMin)
	}
	if f.LevelMax > 0 {
		where = append(where, "level<=?")
		args = append(args, f.LevelMax)
	}
	// 奖牌特征:体重百分位/嗓音数值条件(与地图奖牌筛选同口径;多选=同时满足)。
	// 注意 weight_pct 可空(缺形态范围),NULL 参与比较结果为假,天然不命中,与地图行为一致。
	if f.WeightPctMin > 0 {
		where = append(where, "weight_pct>=?")
		args = append(args, f.WeightPctMin)
	}
	if f.WeightPctMax > 0 {
		where = append(where, "weight_pct<=?")
		args = append(args, f.WeightPctMax)
	}
	if f.VoiceMin > 0 {
		where = append(where, "voice>=?")
		args = append(args, f.VoiceMin)
	}
	if f.VoiceMax < 0 {
		where = append(where, "voice<=?")
		args = append(args, f.VoiceMax)
	}
	for _, t := range f.Types { // types 存为 JSON 数组，用 LIKE 匹配带引号的元素
		where = append(where, "types LIKE ?")
		args = append(args, "%\""+t+"\"%")
	}
	// egg_groups 是 JSON 组名数组:两种口径都用 LIKE 匹配带引号的元素(口径差异见 Filter 的字段注释)。
	if len(f.EggGroupsExact) > 0 {
		// 精确口径:先用 json_array_length 把「组数不同」的排除掉,再逐组 AND 命中。
		// 两段缺一不可 —— 只写 LIKE(AND) 会放进「多带一组」的宠物,只写长度会把
		// 任意一个组数相同的都放进来。
		//
		// json_array_length 对 NULL/非 JSON 返回 NULL、对空数组返回 0:空蛋组的宠物
		// (超进化/分支形态,见 gamedata.IsInfertile 的注释)在两者下都不命中 —— 正是
		// 想要的,「没有蛋组」不该被当成「一套叫无蛋组的蛋组」。
		and := []string{"json_array_length(egg_groups)=?"}
		args = append(args, len(f.EggGroupsExact))
		for _, g := range f.EggGroupsExact {
			and = append(and, "egg_groups LIKE ?")
			args = append(args, "%\""+g+"\"%")
		}
		where = append(where, "("+strings.Join(and, " AND ")+")")
	} else if len(f.EggGroups) > 0 {
		or := make([]string, len(f.EggGroups))
		for i, g := range f.EggGroups {
			or[i] = "egg_groups LIKE ?"
			args = append(args, "%\""+g+"\"%")
		}
		where = append(where, "("+strings.Join(or, " OR ")+")")
	}
	if len(f.MedalIDs) > 0 { // 拥有任一目标奖牌即命中(关联 pet_medal,限本账号)
		ph := make([]string, len(f.MedalIDs))
		for i, id := range f.MedalIDs {
			ph[i] = "?"
			args = append(args, id)
		}
		where = append(where, "gid IN (SELECT gid FROM pet_medal WHERE medal_id IN ("+strings.Join(ph, ",")+") AND account=pets.account)")
	}
	if f.Box != "" { // 取前导整数为 box_id,关联 pet_box 表(限本账号)
		idStr := f.Box
		if i := strings.IndexByte(idStr, '-'); i >= 0 {
			idStr = idStr[:i]
		}
		if id, err := strconv.Atoi(idStr); err == nil {
			where = append(where, "gid IN (SELECT gid FROM pet_box WHERE box_id=? AND account=pets.account)")
			args = append(args, id)
		}
	}
	return " WHERE " + strings.Join(where, " AND "), args
}

// 位置排序键:大世界队伍在前(team_idx*6+pos),其后按盒子(1000+box_id*100+slot),其余末尾。
// 子查询用相关条件 account=pets.account 限本账号,无需额外占位符。
const boxPosExpr = `COALESCE(` +
	`(SELECT team_idx*6+pos FROM pet_team WHERE pet_team.gid=pets.gid AND pet_team.account=pets.account),` +
	`(SELECT 1000+box_id*100+slot FROM pet_box WHERE pet_box.gid=pets.gid AND pet_box.account=pets.account),999999)`

// buildOrder 构造 ORDER BY 表达式(含 gid 兜底,保证稳定顺序)。
func buildOrder(f Filter) string {
	dir := "ASC"
	if strings.EqualFold(f.Order, "desc") {
		dir = "DESC"
	}
	if f.Sort == "boxpos" {
		return boxPosExpr + " " + dir + ", gid"
	}
	col := sortColumns[f.Sort]
	if col == "" {
		col = "gid"
	}
	// 百分位列可空(缺形态范围),用 `col IS NULL` 前置把 NULL 无论升降都排到末尾。
	if strings.HasSuffix(col, "_pct") {
		return col + " IS NULL, " + col + " " + dir + ", gid"
	}
	return col + " " + dir + ", gid"
}

func clampPageSize(n int) int {
	if n <= 0 || n > 200 {
		return 12
	}
	return n
}

// PetPage 返回 gid 在本账号当前筛选+排序下所处的页码(1 起)及是否命中筛选。
// found=false 表示该宠物不在当前筛选结果内(此时 page 退回 1,调用方可据此决定是否清空筛选)。
//
// 优化:原来查出全部匹配 gid 再在 Go 侧遍历计数,宠物多时是全表扫描 + 全量传输。
// 现改为用子查询直接 COUNT 出该 gid 之前(含自身)有多少行,除以 pageSize 得页码,
// 单条 SQL 完成,无需传输全部 gid。
func (sc *Scoped) PetPage(gid uint32, f Filter) (page int, found bool) {
	whereSQL, args := buildWhere(f, sc.account)
	order := buildOrder(f)
	pageSize := clampPageSize(f.PageSize)

	// COUNT 该 gid 之前(含自身)的行数:用子查询统计排序后排在 <= 该 gid 位置的行数。
	// 主查询的 WHERE + ORDER BY 完全复用 ListPets,保证页码口径一致。
	// 排序键可能含多列(如 "level DESC, gid"),子查询条件需对齐排序方向。
	// 简化实现:先查该 gid 是否命中筛选(存在性),再查其排序位置。
	existArgs := append(append([]any{}, args...), gid)
	var exists int
	err := sc.rdb.QueryRow("SELECT 1 FROM pets"+whereSQL+" AND gid=?", existArgs...).Scan(&exists)
	if err != nil {
		return 1, false // 不命中筛选或查询失败
	}

	// 查该 gid 在当前排序下的行号(1 起):构造一个与主查询同 WHERE + ORDER BY 的子查询,
	// 统计排在目标行前面(不含自身)的行数。排序表达式复用 buildOrder,但去掉末尾的 ", gid"
	// (gid 兜底保证稳定顺序,行号计数时整条 ORDER BY 都参与,故直接用完整 order)。
	// 行号 = 排在前面的行数 + 1;页码 = ceil(行号 / pageSize)。
	// 用 ROW_NUMBER() 窗口函数(SQLite 3.25+ 支持)一次查出。
	rankArgs := append(append([]any{}, args...), gid)
	var rank int
	err = sc.rdb.QueryRow(`
SELECT rn FROM (
  SELECT gid, ROW_NUMBER() OVER (ORDER BY `+order+`) AS rn
  FROM pets`+whereSQL+`
) WHERE gid=?`, rankArgs...).Scan(&rank)
	if err != nil || rank == 0 {
		return 1, false
	}
	return (rank-1)/pageSize + 1, true
}

// ListPets 按筛选条件返回本账号宠物列表与命中总数。
//
// 优化:原来先 COUNT(*) 再 SELECT data,两条查询各扫一遍匹配行。
// 现改为用 COUNT(*) OVER() 窗口函数在分页查询里同时拿到总数,单条 SQL 完成。
// SQLite 3.25+ 支持窗口函数,modernc.org/sqlite 内置的 SQLite 版本远高于此。
func (sc *Scoped) ListPets(f Filter) (pets []*pet.Pet, total int, err error) {
	whereSQL, args := buildWhere(f, sc.account)

	pageSize := clampPageSize(f.PageSize)
	page := f.Page
	if page < 1 {
		page = 1
	}

	q := "SELECT data, COUNT(*) OVER() FROM pets" + whereSQL + " ORDER BY " + buildOrder(f) + " LIMIT ? OFFSET ?"
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := sc.rdb.Query(q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var data string
		var cnt int
		if err := rows.Scan(&data, &cnt); err != nil {
			return nil, 0, err
		}
		total = cnt // 每行都带同样的 total,取最后一次即可
		var p pet.Pet
		if json.Unmarshal([]byte(data), &p) == nil {
			pets = append(pets, &p)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	sc.attachLocations(pets)
	return pets, total, nil
}

// attachLocations 给一页宠物批量注入盒子/队伍位置(各一次查询,按 gid 映射)。
// 每个查询各构一份 args(account 置首),避免共享 slice 被 append 污染。
func (sc *Scoped) attachLocations(pets []*pet.Pet) {
	if len(pets) == 0 {
		return
	}
	byGid := make(map[uint32]*pet.Pet, len(pets))
	ph := make([]string, len(pets))
	gidArgs := make([]any, len(pets))
	for i, p := range pets {
		byGid[p.Gid] = p
		ph[i] = "?"
		gidArgs[i] = p.Gid
	}
	in := "(" + strings.Join(ph, ",") + ")"
	argsWith := func() []any { return append([]any{sc.account}, gidArgs...) }

	if rows, err := sc.rdb.Query(`SELECT gid,box_id,slot,box_name,mark FROM pet_box WHERE account=? AND gid IN `+in, argsWith()...); err == nil {
		for rows.Next() {
			var gid uint32
			var boxID, slot, mark int32
			var name string
			if rows.Scan(&gid, &boxID, &slot, &name, &mark) == nil {
				if p := byGid[gid]; p != nil {
					p.Box = &pet.PetBoxLoc{BoxID: boxID, Slot: slot, BoxName: name, Mark: pet.MarkName(mark)}
				}
			}
		}
		rows.Close()
	}
	if rows, err := sc.rdb.Query(`SELECT gid,team_idx,pos FROM pet_team WHERE account=? AND gid IN `+in, argsWith()...); err == nil {
		for rows.Next() {
			var gid uint32
			var teamIdx, pos int32
			if rows.Scan(&gid, &teamIdx, &pos) == nil {
				if p := byGid[gid]; p != nil {
					p.Team = &pet.PetTeamLoc{TeamIdx: teamIdx, Pos: pos}
				}
			}
		}
		rows.Close()
	}
	// 拥有的奖牌(覆盖 ToPet 里仅佩戴的那枚);先清空有 pet_medal 记录的宠物再填,避免回退。
	if rows, err := sc.rdb.Query(`SELECT gid,medal_id FROM pet_medal WHERE account=? AND gid IN `+in+` ORDER BY medal_id`, argsWith()...); err == nil {
		seen := map[uint32]bool{}
		for rows.Next() {
			var gid, mid uint32
			if rows.Scan(&gid, &mid) == nil {
				if p := byGid[gid]; p != nil {
					if !seen[gid] {
						p.MedalIDs = nil
						seen[gid] = true
					}
					p.MedalIDs = append(p.MedalIDs, mid)
				}
			}
		}
		rows.Close()
	}
}

// CountPets 返回本账号宠物总数。
func (sc *Scoped) CountPets() (int, error) {
	var n int
	err := sc.rdb.QueryRow("SELECT COUNT(*) FROM pets WHERE account=?", sc.account).Scan(&n)
	return n, err
}

// OwnedMedalIDs 返回本账号所有宠物拥有过的奖牌 id(去重升序),供服务层映射为名称做筛选下拉。
func (sc *Scoped) OwnedMedalIDs() []uint32 {
	rows, err := sc.rdb.Query(`SELECT DISTINCT medal_id FROM pet_medal WHERE account=? ORDER BY medal_id`, sc.account)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []uint32
	for rows.Next() {
		var id uint32
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

// filterCols 是筛选下拉的各维度:前端键 → pets 列名。顺序即 SELECT 的列顺序。
//
// 这里**没有 species**:宠物列表的筛选面板不用它,而培育页要的「品种」是按**进化链**归并的
// (见 gamedata.ChainOptions)——不是原始形态名。那个维度由服务层在 handleFilterOptions 里
// 另外合成(chains):它给的不是一列字符串而是一串结构化选项(链 id / 链首 / 头像 / 只数),
// 塞不进这张「列 → 值集合」的表。
var filterCols = []struct{ key, col string }{
	{"nature", "nature"},
	{"talentRank", "talent_rank"},
	{"speciality", "speciality"},
	{"partnerMark", "partner_mark"},
	{"form", "form"},
}

// PetBaseIDs 返回本账号全部宠物**当前形态**的 base_conf_id,逐只给出(同一形态出现几次即几只)。
//
// 给培育页的「品种」下拉用(见 gamedata.ChainOptions):只列库里确实有的品种 —— 蛋的物种随
// 母本,选了库里没有的品种就永远配不出任何组合。与 ListAllPets 的差别是不解 data JSON:
// 几百只里多数只贡献一个 id,而品种由 gamedata 按 id 归并,不必知道每只的其它字段。
//
// 必须 ORDER BY:归并「同名多形态」时(无链形态只能按名字认),链首 = **先遇到**的那个形态,
// 而它的头像与蛋图就是这个选项的图。不定序的话同一份数据每次查询都可能归到另一个形态上,
// 页面上「这个品种长什么样」会随刷新跳变(实测海枝枝 4 个形态、公平鸽 2 个)。
func (sc *Scoped) PetBaseIDs() []uint32 {
	rows, err := sc.rdb.Query(`SELECT base_conf_id FROM pets WHERE account=? ORDER BY base_conf_id`, sc.account)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []uint32
	for rows.Next() {
		var id uint32
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

// FilterOptions 返回本账号各维度的可选值(用于前端筛选下拉)。
//
// 各维度合到一次扫描里,去重与排序放到 Go 侧做:原先每维一条 SELECT DISTINCT,几条各扫一遍
// 全表(这几列除 form 外都没有索引),同一批页就白读了五遍(本机 5.1ms → 1.3ms)。
// SQLite 默认 BINARY 排序与 Go 的字符串比较都是按 UTF-8 字节序,换到 Go 侧排结果不变。
func (sc *Scoped) FilterOptions() map[string][]string {
	out := map[string][]string{}
	cols := make([]string, len(filterCols))
	for i, fc := range filterCols {
		cols[i] = "COALESCE(" + fc.col + ",'')"
	}
	if rows, err := sc.rdb.Query("SELECT "+strings.Join(cols, ",")+" FROM pets WHERE account=?", sc.account); err == nil {
		seen := make([]map[string]bool, len(filterCols))
		for i := range seen {
			seen[i] = map[string]bool{}
		}
		vals := make([]string, len(filterCols))
		ptrs := make([]any, len(filterCols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		for rows.Next() {
			if rows.Scan(ptrs...) != nil {
				continue
			}
			for i, v := range vals {
				if v != "" && !seen[i][v] {
					seen[i][v] = true
					out[filterCols[i].key] = append(out[filterCols[i].key], v)
				}
			}
		}
		rows.Close()
		for _, fc := range filterCols {
			sort.Strings(out[fc.key])
		}
	}
	// 宠物盒:取 pet_box 里出现的盒子,形如 "13-性格1"(未命名 → "18-盒18")。
	if rows, err := sc.rdb.Query(`SELECT DISTINCT box_id, box_name FROM pet_box WHERE account=? ORDER BY box_id`, sc.account); err == nil {
		for rows.Next() {
			var id int
			var name string
			if rows.Scan(&id, &name) == nil {
				if name == "" {
					name = fmt.Sprintf("盒%d", id)
				}
				out["box"] = append(out["box"], fmt.Sprintf("%d-%s", id, name))
			}
		}
		rows.Close()
	}
	return out
}
