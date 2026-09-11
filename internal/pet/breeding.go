package pet

import (
	"math"
	"sort"

	"github.com/zxsos/roco-go/internal/gamedata"
)

// —— 培育线(breeding line):一次「选种 → 孵蛋 → 看结果」的迭代过程 ——
//
// 玩法事实(见 docs/data.md 3.6,全部来自实测):
//   - 蛋的**物种必定随母本**,故一条培育线 = 一个品种;
//   - 嗓音 = 双亲均值向零取整(正数向下、负数向上;已由 parentVoice 落地);
//   - 体重在**双亲百分位均值**上下浮动(两条实测样本分别高 1.72pp 与 0.25pp);
//   - 性格按固定分档继承:母 30% / 父 30% / 其余 40% 重掷(见下面的常量);
//     天分同样有概率继承双亲(实测吻合),但比例未知,故本包不预测它。
//
// 这个包只放**纯计算**:不碰数据库、不碰 IO,便于单测。亲本快照直接复用 EggParent
// (收蛋那一刻的快照,亲本被放生也不影响),不另立结构。

// 培育线状态。
const (
	BreedingActive   = "active"   // 进行中
	BreedingDone     = "done"     // 已达成目标
	BreedingArchived = "archived" // 归档(不删,只是不再出现在进行中)
)

// 一代记录的来源:自动(破壳时管线记下)还是手动补录。
const (
	GenSourceAuto   = "auto"
	GenSourceManual = "manual"
)

// BreedingGoal 是这条线要培育成什么样。三项都可空 —— 只填关心的,打分时也只对填了的项计分。
type BreedingGoal struct {
	Voice     *int32   `json:"voice,omitempty"`     // 目标嗓音(-100~100)
	WeightPct *float64 `json:"weightPct,omitempty"` // 目标体重百分位(0~100)
	Nature    string   `json:"nature,omitempty"`    // 目标性格名(精确匹配那一个)
	// NatureIn 是**可接受的性格集合** —— 「性格正面加某维」用它:该维度 +10% 的那 5 个性格名
	// (前端按 /api/name-options 的 6×6 方阵取整行,行序 = 维度编号 1-6,见 gamedata.NatureMatrix)。
	//
	// 为什么存**展开后的名字集合**而不是维度编号:子代/亲本快照里只有性格**名**(EggParent.Nature),
	// 判定「这只算不算达标」要拿名字比;而要由维度编号反查名字,得把这个包从纯计算变成依赖
	// gamedata 的查表(见文件头的「只放纯计算」)。名字集合还顺带让「精确性格」成为它的一个特例,
	// 且与宠物列表的性格筛选(store.Filter.NatureIn,点行头 = 该维的 5 个名字)同一套口径。
	//
	// 与 Nature 同时填时按**并集**算:改目标时留下的另一半不该被悄悄吃掉。通常只填一个。
	NatureIn []string `json:"natureIn,omitempty"`
	// Gender 是**目标性别**(♂ / ♀,空 = 不限)。它只参与「达标」判定与页面提示,**不参与选种
	// 建议的打分**:子代性别在破壳那一刻才由服务器定下,协议里没有任何字段能让它在收蛋/孵蛋前被
	// 推出来(与物种/身高体重不同 —— 那些下蛋时就已经定死、蛋本身也有迹可循)。双亲怎么配都不影响
	// 它,故打分时无从区分,加进去只会给每条建议加同一个常数、把「差多少」这个数搅浑。
	Gender string `json:"gender,omitempty"`
}

// natures 目标性格集合(去重、去空),Nature 与 NatureIn 合并后的那份。
func (g BreedingGoal) natures() []string {
	var out []string
	seen := make(map[string]bool, 1+len(g.NatureIn))
	add := func(n string) {
		if n == "" || seen[n] {
			return
		}
		seen[n] = true
		out = append(out, n)
	}
	add(g.Nature)
	for _, n := range g.NatureIn {
		add(n)
	}
	return out
}

// hasNatureGoal 是否填了性格目标(精确那一个,或「正面加某维」的那一组)。
func (g BreedingGoal) hasNatureGoal() bool { return len(g.natures()) > 0 }

// hasGenderGoal 是否填了性别目标(见 BreedingGoal.Gender)。
func (g BreedingGoal) hasGenderGoal() bool { return g.Gender != "" }

// hitNature 某个性格名是否落在目标集合里。空名(快照缺性格)一律不命中 ——
// 拿空串去比会让「测不出性格」被当成「正好是我要的那个」。
func (g BreedingGoal) hitNature(name string) bool {
	if name == "" {
		return false
	}
	for _, n := range g.natures() {
		if n == name {
			return true
		}
	}
	return false
}

// Generation 是一代培育:母 × 父 → 子代。
//
// Fathers 保留串窝时的**全部候选**(服务器下发的 lay_egg_couple 可能有多个),Father 是
// 实际采用者(自动记录时若无法确定则为空,由用户在页面认领)。子代 Child 同理可为空 ——
// 破壳那一刻只知道双亲,子代要等它真正进背包(见「待认领」)。
type Generation struct {
	Gen       int         `json:"gen"`                 // 第几代(1 起)
	Mother    *EggParent  `json:"mother,omitempty"`    // 种母(决定物种)
	Father    *EggParent  `json:"father,omitempty"`    // 种父(实际采用者)
	Fathers   []EggParent `json:"fathers,omitempty"`   // 全部父本候选(串窝时 >1)
	Child     *EggParent  `json:"child,omitempty"`     // 子代(可为空 = 待认领)
	// EggGid 是这一代来自哪颗蛋(收蛋即记时写下,见 pipeline.recordLay)。
	//
	// 两个用处:① **去重键** —— 0x0243 重放(同一颗蛋被重复下发)不该建出两代,
	// 追加前按它查一遍;② 溯源 —— 页面能说清「这一代是哪颗蛋孵的」。
	EggGid uint32 `json:"eggGid,omitempty"`
	// ChildGid 是破壳回包给出的、孵出来的那只的 gid。
	//
	// 它与 Child 的区别是**时刻**:破壳那一瞬间只知道 gid(子代还没进背包,同一条消息
	// 先过蛋这一路、再过宠物那一路),故先记 gid,等它入库拿到快照了才填 Child(认领)。
	// 有它无 Child 的那一段就是「待认领」:页面能直接写出「孵出的是 gid X」,
	// 而 gid 落了库,跨会话、重启、离线回放都丢不了 —— 这正是它替代进程内存配对的原因。
	ChildGid  uint32 `json:"childGid,omitempty"`
	Backcross bool   `json:"backcross,omitempty"` // 这一代是否回交(子代 × 亲本)
	Source    string      `json:"source"`              // auto | manual
	At        int64       `json:"at,omitempty"`        // 记录时刻(Unix 秒)
	Note      string      `json:"note,omitempty"`
}

// BreedingLine 是一条培育线。代数整份存 JSON(一条线不过几十代,拆表要 JOIN、删线要级联,
// 不如整体读写;投影列只供列表排序用,见 store/breeding.go)。
type BreedingLine struct {
	ID string `json:"id"`
	// Evo 是这条线认的**品种**:进化链 id(见 gamedata.ChainOf);0 = 该形态没有链,按 Species 名字认。
	//
	// 为什么品种不再只是一个名字:一条链上的各阶段是同一个品种(蛋的物种随母本,链上任一阶段
	// 的 ♀ 都能当种母),而同一只精灵的不同形态各占一条链(嗜波螺「本来的样子」/「被污染的样子」)。
	// 口径与匹配见 ChainRef —— 那里也说了为什么不能按名字认。
	//
	// Species 保留:无链时按它匹配(Evo==0);有链时它只是「建立这条线时那个形态的名字」,
	// 展示用 ChainLabelOf,匹配不再看它。
	Evo       uint32       `json:"evo,omitempty"`
	Species   string       `json:"species"`          // 品种名(随母本;无链时即匹配键)
	ConfID    uint32       `json:"confId,omitempty"` // 品种 conf_id
	// MotherGid 这条线**固定在哪个种母身上**(个体 gid)。
	//
	// 为什么线的身份是种母而不是品种:蛋趴在谁的窝上是个**确定的事实**,而「同品种最近更新的
	// 那条线」是猜的 —— 同一个品种同时开几个窝、几只母本各孵各的时,那几颗蛋会被全记到一条
	// 线上,而且记到哪条还会随 updated_at 漂移。换成种母后,同一品种的几条线天然互不干扰。
	//
	// 0 = 还没固定(手工建的、一代都没有的线)。老线也都没有这个字段,由 MotherGidOf 从末代
	// 派生 —— 与 DeriveChain 同一套路子:老数据读取时补,不做迁移。
	MotherGid uint32 `json:"motherGid,omitempty"`
	// ParentLineID 子线指向的**母线** id;空 = 不是子线。
	//
	// 什么时候会有子线:拿这条线孵出的子代当新母本(选育的核心操作)—— 换了种母就换了线,
	// 但她是这条线的孩子,故挂上来:代数接着母线数(见 GenBase),页面沿链把培育史串起来。
	// 野外抓来的母本与任何线都没有血缘,不会走到这里 —— 那种是**独立新线**(见 NewAutoLine)。
	ParentLineID string `json:"parentLineId,omitempty"`
	// GenBase 本线代数**接在哪个数之后**:子线 = 母线最后一代的代数(见 NewChildLine),
	// 故子线的第一代是 GenBase+1,而不是 1。普通线为 0(从 1 开始)。
	//
	// 有了它,代数在整条谱系上是连续的 —— 而物理合并(把子线并进母线)也就只是把 Gens
	// 原样搬过去,不必再重排,那正是「合并不可逆」这个顾虑的解法。
	GenBase      int            `json:"genBase,omitempty"`
	Goal         BreedingGoal   `json:"goal"`
	Status       string         `json:"status"`
	Gens         []Generation   `json:"gens"`
	Pending      []Generation   `json:"pending,omitempty"` // 待认领子代的一代
	CreatedAt    int64          `json:"createdAt"`
	UpdatedAt    int64          `json:"updatedAt"`
}

// MotherGidOf 这条线**当前**的种母 gid:新线存了就直接用,老线按末代派生。
//
// 老线为什么能派生:它每一代都存着 Mother 快照(含 gid),末代那只就是当前的种母。这与
// DeriveChain(老线按名字补出品种身份)同一套路子 —— 老数据读取时补、不落库,不做迁移。
//
// 两处容错:
//   - 末代可能在 Pending 里(待认领的代同样带母本),扫描口径与 NextGen 一致;
//   - 某代的 Mother 可能整个缺失 —— handleBreedingSave 是整条覆盖写、不校验 Gens,手工
//     构造的请求能把没有母本的代提交上来(前端的「必选种母」只是客户端校验)。故跳过它们
//     往前找,全都没有才返回 0。
//
// 返回 0 表示「这条线还没固定种母」(手工建的空线),此时按品种兜底。
func MotherGidOf(l *BreedingLine) uint32 {
	if l == nil {
		return 0
	}
	if l.MotherGid != 0 {
		return l.MotherGid
	}
	gid, gen := uint32(0), 0
	for _, g := range l.Gens {
		if g.Mother != nil && g.Mother.Gid != 0 && g.Gen >= gen {
			gid, gen = g.Mother.Gid, g.Gen
		}
	}
	for _, g := range l.Pending {
		if g.Mother != nil && g.Mother.Gid != 0 && g.Gen >= gen {
			gid, gen = g.Mother.Gid, g.Gen
		}
	}
	return gid
}

// DescendantGids 这条线历代子代的 gid 集合(已认领的)。
//
// 两个用处:① 建议里若父本落在其中,那便是回交(见 Suggest);② 换种母时判「新母本是不是
// 这条线孵出来的」—— 是就自动开子线,不是(野外抓的)就开独立新线。
func DescendantGids(l *BreedingLine) map[uint32]bool {
	out := make(map[uint32]bool)
	if l == nil {
		return out
	}
	for _, g := range l.Gens {
		if g.Child != nil && g.Child.Gid != 0 {
			out[g.Child.Gid] = true
		}
	}
	return out
}

// —— 预测 ——
//
// 体重的乐观上界(百分点):两条实测都比双亲均值高(94.610→96.332 是 +1.72pp,
// 99.754→100 是 +0.25pp),故除均值外再给一个「最好能到哪」的上界,而不是假装精确。
//
// ⚠️ 它**只服务预测**(Expectation.WeightHi 与建议排序的次键),不再是「达标」的容差 ——
// 达标口径见 reached:按目标方向比(高目标 ≥、低目标 ≤),没有正负容差。
const weightOptimisticPP = 2.0

// —— 达标判定:按目标方向「达到即达标」——
//
// 嗓音(-100~100)与体重百分位(0~100)都是**有极值的轴**,玩家的目标是在轴上挑一个点:
// 「96 声」想往上够、「-100 声」想往下够、「0% 体重」想要最轻。故方向由目标偏向哪一端决定,
// 而不是一律 ≥ —— 一律 ≥ 会让「极限嗓音 -100」「极限体重 0%」这种往低刷的目标变成**任何
// 个体都达标**(等于目标失效),那一侧的模板也就废了。
//
// 目标正好落在轴中心(嗓音 0 / 体重 50)时没有方向可言,按精确相等(罕见边界)。
//
// 这套口径是**前后端唯一真源**:web/src/pages/breeding/pets.js 的 goalReached 必须与它逐字
// 一致 —— 页面上的「达标」用的正是这里判 done 的同一判定,两处不等会冒出「页面写着达标、
// 状态还停在进行中」。docs/data.md 3.6 有说明。
func reached(cur, goal, lo, hi float64) bool {
	center := (lo + hi) / 2
	switch {
	case goal > center:
		return cur >= goal
	case goal < center:
		return cur <= goal
	default:
		return cur == goal
	}
}

// voiceReached 嗓音是否达标(轴 = VoiceLow~VoiceHigh)。
func voiceReached(cur, goal int32) bool {
	return reached(float64(cur), float64(goal), VoiceLow, VoiceHigh)
}

// weightReached 体重百分位是否达标(轴 = 0~100)。
func weightReached(cur, goal float64) bool {
	return reached(cur, goal, 0, 100)
}

// —— 性格遗传(玩法规则,见 docs/data.md 3.6)——
//
// 子代性格 = **母本性格 30% / 父本性格 30% / 剩下 40% 从全部性格里重掷**。
//
// 两个 30% 是**两个互斥的槽位**,不是两次独立判定:双亲性格相同时那个性格占满 60%
// (「两个都是则 60%」说的正是这个;按独立事件算会得到 1 − 0.7 × 0.7 = 51%,与规则不符)。
//
// 重掷槽按全表均匀估 —— 它本身也可能掷中目标性格,故命中率一律带上 0.4/30 这一份。
const (
	natureInheritChance = 0.30 // 单个亲本的性格被子代继承的概率(母、父各占一个槽位)
	natureRollChance    = 0.40 // 两个槽位都没中:从全部性格里重掷一个
	// natureCount 是**去重后**的性格种类数(重掷槽的分母):30。
	//
	// ⚠️ 它不是 names.json 的 nature 表**行数**(31):那张表里 id 28 与 id 31 同为「平和」
	// (+生命 −魔攻),且实测 7545 只宠物的性格 id 分布里 **31 一次都没出现过** —— 它是配置
	// 残留,永远不会落到宠物身上,故平和也不该按「占两个 id」加权。
	// 拿行数当种类数会把重掷那一份算小(1/31 而非 1/30),而这种偏差**不报错**:
	// 页面只是显示 61.29% 而不是 61.33%,谁也看不出来。
	//
	// 表变长变短时这里要跟着改(与 docs/data.md 的条数一起),由 internal/gamedata 的
	// TestNatureUniqueNames 守着:它断言去重后的名字数仍是 30,改了表会红而不是静静偏掉。
	natureCount = 30
)

// Expectation 是某对双亲**预期**孵出什么。全部是预测值,不是实测值 —— UI 上要能看出这一点。
type Expectation struct {
	Voice     int32   `json:"voice"`     // 向零取整((母+父)/2)
	WeightPct float64 `json:"weightPct"` // 双亲百分位均值
	// WeightHi 是乐观上界(实测略高于均值)。**页面不再把它画成区间**:浮动是实测噪声、不是
	// 可依赖的收益,`89.0~94.2%` 会被读成「至少能到 89」。它仍要留在响应里 —— 回交对比与
	// 建议排序(lessSuggestion)都在用它当「哪组更有戏」的次序。
	WeightHi   float64 `json:"weightHi"`
	NatureP    float64 `json:"natureP"`    // 命中目标性格的概率(母/父各 30%,再加重掷槽的 0.4/30)
	NatureFrom string  `json:"natureFrom"` // parent=双亲之一有(双亲都带即 60% 那档) / roll=只能等重掷 / none=没设目标
}

// pctOf 取百分位指针的值,nil(该形态没有取值范围)视为 0。
func pctOf(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// meanPct 双亲百分位均值:只有一方已知就取那一方(另一方按同值处理),都未知则 0。
func meanPct(a, b *float64) float64 {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return *b
	}
	if b == nil {
		return *a
	}
	return (*a + *b) / 2
}

// Predict 预测某对双亲孵出的子代。目标 g 只影响性格命中概率(嗓音/体重与目标无关)。
//
// 逐组合调用时请改用 predictWith + 预算好的目标集合:本函数每次都会重新解析一遍
// 目标集合(见 predictWith 的注释)。
func Predict(mother, father EggParent, g BreedingGoal) Expectation {
	return predictWith(mother, father, g.natures())
}

// predictWith 与 Predict 同义,但目标集合由调用方**预算好**传入。
//
// 为什么需要它:`natures()` 每次都要建一个 map 去重、再 append 出一个切片;而一次
// Predict 里它会被调到 4 次(是否填了性格目标、命中概率、两个亲本各自是否命中),
// 加上 Score 里的 1 次 —— 一个组合就是 5 次分配。选种建议要跑几万个组合,那里
// 是纯粹的白花钱。故热点循环在**循环外**解析一次,循环内一律走本函数。
func predictWith(mother, father EggParent, targets []string) Expectation {
	e := Expectation{
		Voice:     voiceOf(mother, father),
		WeightPct: meanPct(mother.WeightPct, father.WeightPct),
	}
	e.WeightHi = math.Min(100, e.WeightPct+weightOptimisticPP)
	switch {
	case len(targets) == 0:
		e.NatureFrom = "none"
	default:
		e.NatureP = natureHitPWith(mother, father, targets)
		// parent = 命中的主要来源是那两个 30% 槽位(双亲都带时就是 60%);roll = 双亲都没有,
		// 只剩 40% 的重掷槽可指望。两者都还要加上重掷槽本身掷中的那一份。
		if hitTargets(mother.Nature, targets) || hitTargets(father.Nature, targets) {
			e.NatureFrom = "parent"
		} else {
			e.NatureFrom = "roll"
		}
	}
	return e
}

// voiceOf 子代嗓音 = 双亲均值的**向零取整**(正数向下、负数向上取整;见文件头与 docs/data.md 3.6)。
//
// 单独拎出来是因为 VoiceReachOf 要遍历全部组合却只关心嗓音 —— 走 Predict 会为每个组合
// 多算体重、多构造一个 Expectation。取整方向在**负数**区间是关键:实测是「朝零取整」
// (≈整数除法,与 C++ 侧 `(a+b)/2` 同源),故 -99.5 记 -99 而不是 -100 —— 这正好就是 Go
// 整数除法的行为,直接 `(a+b)/2` 即可,**不要**改成 math.Floor(那会把负数再往下取一档)。
func voiceOf(mother, father EggParent) int32 {
	return (mother.Voice + father.Voice) / 2
}

// hitTargets 性格名是否落在目标集合里(见 BreedingGoal.hitNature)。
func hitTargets(name string, targets []string) bool {
	if name == "" {
		return false
	}
	for _, t := range targets {
		if t == name {
			return true
		}
	}
	return false
}

// natureHitP 子代性格落在**目标集合**里的概率:重掷槽按集合大小分摊(0.4 × |集合| / 全表条数,
// 任何性格都能从它掷出来),再加上双亲各自带集合内性格时的那 30%。
//
// 上限钳到 1:集合被手改成一堆重复/超长的名字时,公式加起来会超过 1,而概率超过 1 只会在
// 页面上显示成「120%」这种一眼假的数字。
func natureHitP(mother, father EggParent, g BreedingGoal) float64 {
	return natureHitPWith(mother, father, g.natures())
}

// natureHitPWith 见 natureHitP,目标集合由调用方预算(见 predictWith 的注释)。
func natureHitPWith(mother, father EggParent, targets []string) float64 {
	if len(targets) == 0 {
		return 0
	}
	p := natureRollChance * float64(len(targets)) / natureCount
	for _, t := range targets {
		if mother.Nature == t {
			p += natureInheritChance
		}
		if father.Nature == t {
			p += natureInheritChance
		}
	}
	return math.Min(1, p)
}

// Score 期望值与目标的差距(0~1,越小越好)。**只对填了的目标项计分**,未填的维度不参与,
// 否则「只想刷嗓音」的线会被没填的体重项拖着走。
//
// 方向化后**已达标的项记 0 分**(见 reached):「≥ 目标」与「正好 = 目标」都是好结果,不该因为
// 「超得越多越远」把达标的组合排到后面。未达标的项才按离目标的归一化距离计分。
//
// **性别目标不在这里计分**(见 BreedingGoal.Gender):它在破壳前不可预测,双亲怎么配都一样,
// 计进去只会给每条建议加同一个常数。
func Score(e Expectation, g BreedingGoal) float64 {
	return scoreWith(e, g, g.natures())
}

// scoreWith 与 Score 同义,目标集合由调用方预算(理由见 predictWith 的注释:
// Score 原本每调一次都要重新解析一遍目标集合)。
func scoreWith(e Expectation, g BreedingGoal, targets []string) float64 {
	var sum, n float64
	// 每项都进分母(达标也是「填了的一项」);只有未达标才累积距离 —— **达标即满分**。
	if g.Voice != nil {
		n++
		if !voiceReached(e.Voice, *g.Voice) {
			sum += math.Abs(float64(e.Voice)-float64(*g.Voice)) / float64(VoiceHigh-VoiceLow)
		}
	}
	if g.WeightPct != nil {
		n++
		if !weightReached(e.WeightPct, *g.WeightPct) {
			sum += math.Abs(e.WeightPct-*g.WeightPct) / 100
		}
	}
	if len(targets) > 0 {
		n++
		// 性格是概率、没有「值」可判方向:仍按「离命中还差多少」计分(全中为 0)。
		sum += 1 - e.NatureP
	}
	if n == 0 {
		return 0 // 什么目标都没填:所有组合等价
	}
	return sum / n
}

// —— 种公 / 种母建议 ——

// Candidate 一位候选种母及其可配的父本(串窝时多个,来自服务器的 lay_egg_couple)。
type Candidate struct {
	Mother EggParent `json:"mother"`
	// FatherIdx 可配父本在**所属 Pool.Fathers** 里的下标,不是父本本身。
	//
	// 为什么存下标而不是各存一份 EggParent:同一位种公能给多位母本配对,逐母本存一份
	// 等于把「母本数 × 父本数」整个物化一遍 —— 实测 1000 只宠物(500♀ × 500♂)时是
	// 41MB,而 Suggest 与 VoiceReachOf 只是遍历它算个数,根本不需要人手一份拷贝。
	// 存下标后父本常驻一份(几百项,L2 装得下),遍历从「扫几十 MB 结构体」变成
	// 「扫几 MB 整数」。
	FatherIdx []int32 `json:"fatherIdx"`
	Ambiguous bool    `json:"ambiguous"` // 父本不唯一:实际是谁只能等破壳后反推
}

// Pool 一个品种的候选池:候选母本 + 全池**共用**的父本表(见 Candidate.FatherIdx)。
//
// 遍历组合请走 Cands[i].FatherIdx 去 Fathers 里取,别假定候选自带父本 —— 那正是去物化
// 要去掉的那份拷贝。
type Pool struct {
	Cands   []Candidate
	Fathers []EggParent
}

// Suggestion 一个推荐组合:谁配谁、预期出什么、离目标多远。
type Suggestion struct {
	Mother    EggParent   `json:"mother"`
	Father    EggParent   `json:"father"`
	Exp       Expectation `json:"exp"`
	Score     float64     `json:"score"`
	Ambiguous bool        `json:"ambiguous,omitempty"` // 该母本的父本候选不唯一
	Backcross bool        `json:"backcross,omitempty"` // 父本就是这条线自己的子代(回交)
}

// Suggest 对全部候选组合打分,返回**最好的前 n 个**(升序,n<=0 时全给)。
//
// 串窝的母本会为每个父本候选各出一个组合 —— 不能替玩家猜实际是哪个,故都列出来并标
// Ambiguous,由他按自己的窝位判断。
//
// childGids 是这条线历代子代的 gid 集合:父本落在里面即为**回交**(子代 × 亲本那样往上
// 倒着配),此时给组合标上 Backcross。前端那颗「回交」标签此前一直是死的 —— 后端从没
// 赋过值,而"这一组是不是回交"只有线自己知道(候选池里看不出来)。
func Suggest(pool Pool, g BreedingGoal, n int, childGids map[uint32]bool) []Suggestion {
	// 目标集合在循环外解析一次:每个组合都要用它算命中概率与打分,重复解析是白花钱
	// (见 predictWith 的注释)。
	targets := g.natures()
	var out []Suggestion
	if n > 0 {
		out = make([]Suggestion, 0, n)
	}
	for _, c := range pool.Cands {
		for _, idx := range c.FatherIdx {
			f := pool.Fathers[idx]
			e := predictWith(c.Mother, f, targets)
			s := Suggestion{
				Mother:    c.Mother,
				Father:    f,
				Exp:       e,
				Score:     scoreWith(e, g, targets),
				Ambiguous: c.Ambiguous,
				Backcross: childGids[f.Gid],
			}
			if n > 0 {
				out = topNInsert(out, s, n)
				continue
			}
			out = append(out, s)
		}
	}
	if n <= 0 {
		// n<=0 表示「全给」(只有测试用):规模小,走原路的全量稳定排序。
		sortSuggestions(out)
		return out
	}
	return out
}

// topNInsert 把 s 插进**升序**的前 N 名候选池,池长不超过 n;返回池(可能被原样返回)。
//
// 为什么单趟维护而不是全量排序:组合数是母本数 × 父本数,几百只宠物就上万、上千只就几十万。
// 原先先全量物化再对**全部**组合做插入排序(O(k²)),1 万组合实测 700ms、25 万组合要几分钟;
// 而且要的只是前 5 条,却给每个组合都拷了一份完整的 Suggestion(内含母/父两个 EggParent)。
// 单趟维护长度 n 的池后是 O(k·n)(n 为常数,等价 O(k)),内存从 O(k) 降到 O(n)。
//
// **语义必须与「全量稳定排序后取前 n」完全一致**,否则契约 golden 里那几组建议会变:
//   - 插入位置 = 第一个**不比 s 更好**的元素之前,故等价元素(Score/WeightHi/Voice 三键全等)
//     时 s 落在**后面**,不抢占先来者的位置 —— 与插入排序的稳定语义一致;
//   - 池满时只有 s **严格**优于末位才替换,等价则淘汰 s(稳定排序下后到的等价元素排在第 n 之后)。
func topNInsert(top []Suggestion, s Suggestion, n int) []Suggestion {
	i := len(top)
	for i > 0 && lessSuggestion(s, top[i-1]) {
		i--
	}
	if len(top) >= n {
		if i >= n {
			return top // 不比末位好(或与之等价):挤不进去
		}
		copy(top[i+1:], top[i:n-1]) // 末位被挤出
		top[i] = s
		return top
	}
	top = append(top, s)
	copy(top[i+1:], top[i:])
	top[i] = s
	return top
}

// sortSuggestions 按差距升序;同分时按「体重上界更高、嗓音更极端」排前,让结果稳定可复现。
//
// 插入排序是**稳定**的:等价元素保留输入次序(比较用的是严格 < >,相等即不交换)。
// 这个性质被上面的 topNInsert 与契约 golden 依赖,换成不稳定的排序会让输出次序漂移。
func sortSuggestions(s []Suggestion) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && lessSuggestion(s[j], s[j-1]); j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func lessSuggestion(a, b Suggestion) bool {
	if a.Score != b.Score {
		return a.Score < b.Score
	}
	if a.Exp.WeightHi != b.Exp.WeightHi {
		return a.Exp.WeightHi > b.Exp.WeightHi
	}
	return a.Exp.Voice > b.Exp.Voice
}

// —— 目标可达性 ——

// VoiceReach 是「拿现有候选去配,下一代嗓音最远能到哪」的结论。
//
// 为什么要单独算:子代嗓音 = 双亲均值的**向零取整**(见 Predict),不是「子代能超过双亲」。
// 于是高目标很难够:目标 +100 只有 100 × 100 孵得出,任一方不足 100 都只能无限接近
// (99 × 100 → 99,再迭代也一样)。负向是对称的 —— 取整朝零,`-100 × -99 → -99` 够不到目标
// -100,只有 -100 × -100 才行。没有这个结论时,页面会把「99 × 100 → 预期 99、差 1」照常
// 排进前几名:看着在进步,实则永远到不了,玩家会照着它白配好几代。
type VoiceReach struct {
	Target int32 `json:"target"` // 这条线的目标嗓音
	Best   int32 `json:"best"`   // 现有候选在**目标方向上**能达到的极值:高目标取最高、低目标取最低、中心取最近
	Hit    bool  `json:"hit"`    // 该极值本身是否已达标(与 goalHit 同一判定,见 reached)
}

// VoiceReachOf 遍历候选池求可达性,枚举口径与 Suggest 完全一致(每个母本 × 它的每个父本
// 候选)—— 页面推荐的正是这批组合,结论必须说的是同一批,不能另立一套筛选。
//
// Best 取**目标方向上的极值**(见 betterReachVoice),不能一律取「离目标最近」:方向化达标下
// 最近的那个可能落在目标的反面 —— 可达集 {95,100}、目标 96 时最近的是 95(判够不着),而 100
// 其实已经达标。Hit 用与 goalHit 同一个 reached 判定。
//
// 空池返回 Best=Target、Hit=false:没有候选时「够不着」比「达到了」诚实(前端据 Hit 提示),
// 否则一条刚建的空线会被报成「目标已可达」。
func VoiceReachOf(pool Pool, target int32) VoiceReach {
	out := VoiceReach{Target: target, Best: target}
	found := false
	for _, c := range pool.Cands {
		for _, idx := range c.FatherIdx {
			// 只关心嗓音,故直接算而不用 Predict:那会为每个组合多算体重、多构造一个
			// Expectation —— 而这里要遍历全部组合(上千只宠物时是几十万次)。
			v := voiceOf(c.Mother, pool.Fathers[idx])
			if !found || betterReachVoice(v, out.Best, target) {
				out.Best, found = v, true
			}
		}
	}
	out.Hit = found && voiceReached(out.Best, target)
	return out
}

// betterReachVoice v 是否比 cur 更该被记作「目标方向上的极值」:高目标看谁更大、低目标看谁更小、
// 中心看谁离目标更近。与 reached 的方向口径一一对应。
func betterReachVoice(v, cur, target int32) bool {
	const center int32 = (VoiceLow + VoiceHigh) / 2
	switch {
	case target > center:
		return v > cur
	case target < center:
		return v < cur
	default:
		return abs32(v-target) < abs32(cur-target)
	}
}

// —— 回交建议 ——

// Backcross 回交 vs 换种的对比。**只给数字与理由,不替玩家决定**:回交能巩固已有的好基因,
// 但会放弃从别的候选那里一次性拉近目标的机会,两种取舍都常见。
type Backcross struct {
	Advised    bool        `json:"advised"`              // 是否更推荐回交
	Reason     string      `json:"reason"`               // 一句话理由
	WithParent *EggParent  `json:"withParent,omitempty"` // 建议回交用的亲本
	Exp        Expectation `json:"exp"`                  // 回交的预期(子代 × 该亲本)
	Alt        *EggParent  `json:"alt,omitempty"`        // 换成别的候选时最好的一只
	AltExp     Expectation `json:"altExp,omitempty"`     // 换候选的预期
}

// BackcrossAdvice 拿最新一代的子代去比:是「子代 × 亲本」回交更接近目标,还是「换一只种公」更接近。
//
// 子代为最新一代的 Child;parentA/parentB 是它的双亲;pool 是可替换的候选(不含子代自己)。
// 没有任何可比对象(没有子代、或池子空)时 Advised=false 且 Reason 说明原因。
func BackcrossAdvice(child, mother, father EggParent, pool []EggParent, g BreedingGoal) Backcross {
	out := Backcross{}
	if child.Gid == 0 && child.Voice == 0 && child.Name == "" {
		out.Reason = "还没有破壳的子代,先孵出一只再谈回交"
		return out
	}
	// 回交对象是与子代**性别相反**的那个亲本(游戏里只能这样配)。性别缺失时退回按
	// 「离目标更近」挑 —— 与升级前一致,宁可挑错一个也不能不给建议。
	best := backcrossParent(child, mother, father, g)
	out.WithParent = &best
	out.Exp = pairExpectation(child, best, g)

	// 换种:池子里与子代配对后最好的一只(排除子代自己与它的两个亲本)。
	var alt *EggParent
	var altExp Expectation
	altScore := math.MaxFloat64
	for i := range pool {
		c := pool[i]
		if c.Gid == child.Gid || c.Gid == mother.Gid || c.Gid == father.Gid {
			continue
		}
		e := pairExpectation(child, c, g)
		if s := Score(e, g); s < altScore {
			altScore, altExp, alt = s, e, &pool[i]
		}
	}
	if alt != nil {
		out.Alt = alt
		out.AltExp = altExp
	}
	backScore := Score(out.Exp, g)
	switch {
	case alt == nil:
		out.Advised = true
		out.Reason = "没有别的候选可比,继续用子代回交亲本"
	case backScore < altScore:
		out.Advised = true
		out.Reason = "回交更接近目标:子代已优于双亲之一,回配能把这个优势固定下来"
	case backScore > altScore:
		out.Reason = "换种更接近目标:现有候选里有比回交更能拉近差距的"
	default:
		out.Advised = true
		out.Reason = "回交与换种预期相当,回交更稳(基因已在手上)"
	}
	return out
}

// betterParent 在两只亲本里挑与目标更近的那只(差距相同时挑嗓音更极端的)。
func betterParent(a, b EggParent, g BreedingGoal) EggParent {
	da, db := parentDistance(a, g), parentDistance(b, g)
	if da == db {
		if abs32(b.Voice) > abs32(a.Voice) {
			return b
		}
		return a
	}
	if db < da {
		return b
	}
	return a
}

// backcrossParent 挑与子代配对的那个亲本:**性别必须相反**(游戏里只能这样配)。
//
// 子代是 ♀ 就只能回配父亲,是 ♂ 就只能回配母亲 —— 这不是偏好而是规则。升级前一律
// 按「离目标更近」挑,挑到同性亲本时整条回交建议都是反的(♂ 子代 × ♂ 亲本根本配不出蛋)。
//
// 性别缺失(老快照没记)时退回 betterParent:那时无从判断,按老口径给一个数总比不给好。
func backcrossParent(child, mother, father EggParent, g BreedingGoal) EggParent {
	switch child.Gender {
	case "♀":
		if father.Gid != 0 {
			return father
		}
	case "♂":
		if mother.Gid != 0 {
			return mother
		}
	}
	return betterParent(mother, father, g)
}

// pairExpectation 按**性别**把子代与另一半摆进 Predict 的母/父位。
//
// 蛋的物种必定随母本(见文件头),故 Predict 的第一个参数必须是母本。子代是 ♂ 时它只能
// 当父本、另一半才是母本 —— 摆反了整个预期都是错的:不仅数值按错的双亲算,连「孵出来
// 是不是这个品种」都变了(♂ 子代配一只别品种的 ♀,孵出的就是那只 ♀ 的品种)。
func pairExpectation(child, other EggParent, g BreedingGoal) Expectation {
	if child.Gender == "♂" {
		return Predict(other, child, g)
	}
	return Predict(child, other, g)
}

// parentDistance 亲本本身离目标多远(与 Predict 同一套计分口径,便于比较)。
func parentDistance(p EggParent, g BreedingGoal) float64 {
	return Score(Expectation{Voice: p.Voice, WeightPct: pctOf(p.WeightPct), NatureP: naturePOf(p, g)}, g)
}

func naturePOf(p EggParent, g BreedingGoal) float64 {
	if g.hitNature(p.Nature) {
		return 1
	}
	return 0
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// —— 汇总(供列表页与存储投影列)——

// LineStats 汇总一条线:代数、历代最佳嗓音、历代最佳体重百分位。
//
// 「最佳」按目标算:**达标优先,其次离目标最近**(方向化后,一个已达标的 97 不该被一个没达标
// 的 96 挤掉 —— 那正是「只取离目标最近」会露出的错);没填目标时嗓音取绝对值最大的、
// 体重取最重的 —— 收集向的玩法要的就是极端个体。
func LineStats(l *BreedingLine) (gens int, bestVoice *int32, bestWeight *float64) {
	if l == nil {
		return 0, nil, nil
	}
	gens = len(l.Gens)
	for i := range l.Gens {
		c := l.Gens[i].Child
		if c == nil {
			continue
		}
		if bestVoice == nil || closerVoice(*c, bestVoice, l.Goal) {
			v := c.Voice
			bestVoice = &v
		}
		if c.WeightPct != nil && (bestWeight == nil || closerWeight(*c.WeightPct, bestWeight, l.Goal)) {
			w := *c.WeightPct
			bestWeight = &w
		}
	}
	return gens, bestVoice, bestWeight
}

// closerVoice 这一代的嗓音是否比已记录的最佳值更值得留下。
//
// 目标是「**达标优先**,其次离目标更近」:方向化后一个已达标的 97 显然比一个没达标的 96
// 更值得留 —— 而「只取离目标最近」会把 96 选出来,让卡片写着一个未达标的「最佳」。
// 两者达标状态相同(都中或都不中)时才比距离。没填目标时仍取绝对值最大的(收集向玩法)。
func closerVoice(c EggParent, best *int32, g BreedingGoal) bool {
	if g.Voice != nil {
		ch, bh := voiceReached(c.Voice, *g.Voice), voiceReached(*best, *g.Voice)
		if ch != bh {
			return ch
		}
		return math.Abs(float64(c.Voice-*g.Voice)) < math.Abs(float64(*best-*g.Voice))
	}
	return abs32(c.Voice) > abs32(*best)
}

// closerWeight 同 closerVoice,体重那一路。
func closerWeight(pct float64, best *float64, g BreedingGoal) bool {
	if g.WeightPct != nil {
		ch, bh := weightReached(pct, *g.WeightPct), weightReached(*best, *g.WeightPct)
		if ch != bh {
			return ch
		}
		return math.Abs(pct-*g.WeightPct) < math.Abs(*best-*g.WeightPct)
	}
	return pct > *best
}

// —— 目标达成 ——

// ReachGoal 这条线是否已「达成」:存在某一代的已认领子代**同时**满足全部已填的目标项。
//
// 为什么要求同一代同时满足,而不是「各目标分别被不同子代满足」:玩家的意图是「培育出一只
// 大块头婉转声就停」—— 要的是那一只,不是「嗓音达标的一只 + 体重达标的另一只」。
//
// 各项口径(全部走 reached 的方向化判定):
//   - 嗓音:目标 > 0 时「≥ 目标」算到,目标 < 0 时「≤ 目标」算到,目标 = 0 时精确相等。
//     子代嗓音 = 双亲均值的向零取整,是确定值(见 Predict),方向化后「97 对目标 96」算达标。
//   - 体重:同方向化(轴 0~100,中心 50)。**没有 ±2pp 容差** —— 那只是预测的乐观上界
//     (Expectation.WeightHi),不再参与达标。
//   - 性格落在目标集合里。它本就是概率继承,到了就是到了;「正面加某维」时集合里那 5 个都算。
//   - 性别相符(♂ / ♀)。破壳前不可预测,但破壳后是确定的 —— 判定它不涉及概率(快照缺性别不算命中)。
//
// 目标一项未填时恒为 false:没有目标的线谈不上「达成」,否则新建的空线会被立刻判成达成。
func ReachGoal(l *BreedingLine) bool {
	if l == nil {
		return false
	}
	if l.Goal.Voice == nil && l.Goal.WeightPct == nil && !l.Goal.hasNatureGoal() && !l.Goal.hasGenderGoal() {
		return false
	}
	for i := range l.Gens {
		if c := l.Gens[i].Child; c != nil && goalHit(*c, l.Goal) {
			return true
		}
	}
	return false
}

// goalHit 一只子代是否同时满足全部已填目标项(方向化达标,见 reached)。
func goalHit(c EggParent, g BreedingGoal) bool {
	if g.Voice != nil && !voiceReached(c.Voice, *g.Voice) {
		return false
	}
	if g.WeightPct != nil {
		// 体重未知(该形态没有取值范围)不算命中:拿 0 去比会把「测不出」当成「最轻的」。
		if c.WeightPct == nil || !weightReached(*c.WeightPct, *g.WeightPct) {
			return false
		}
	}
	if g.hasNatureGoal() && !g.hitNature(c.Nature) {
		return false
	}
	if g.hasGenderGoal() && c.Gender != g.Gender {
		return false
	}
	return true
}

// AutoDoneOnReach 在「由未达成变为达成」时把线自动置为已达成,返回是否改了状态。
// before 是本次改动**之前** ReachGoal 的结果 —— 只有转变才动手。
//
// 为什么判转变、而不是「只要达成就是 done」:玩家可以手动把线改回「进行中」(想接着刷更
// 高的数值)。若只看结果,下一次写入又会把它掰回 done,玩家的选择等于无效。
//
// 只在「进行中」时改:归档的线不该被自动翻出来(玩家手动设的其它状态同理)。
//
// 副作用(调用方需知):破壳的自动记录只记到「进行中」的线(见 pipeline/breeding.go),
// 置 done 等于这条线不再自动记录下一代 —— 与「刷到就停」的意图一致,但玩家若想继续,
// 得自己把它改回进行中(那时也不会再被自动改回去,见上面的转变判定)。
func AutoDoneOnReach(l *BreedingLine, before bool) bool {
	if l == nil || before || l.Status != BreedingActive || !ReachGoal(l) {
		return false
	}
	l.Status = BreedingDone
	return true
}

// —— 候选池(从宠物库里挑种公种母)——

// ParentSnapshot 把库里的个体转成亲本快照,供建议的候选池使用。
//
// 字段口径与 pipeline/eggs.go 的 parentSnap 一致 —— 那边是**收蛋那一刻**落库的快照(亲本
// 之后被放生也不影响),这里是页面实时算的,故各自成型:后者要处理「库里已无此宠」。
//
// 唯一的差别是 Evo:那边要它(破壳自动建线时靠它认品种,见 NewAutoLine),而这里拿不到
// —— 本函数没有 gamedata,且候选/子代快照都不参与认品种(线的身份在建线时就定下了)。
// 补录时它会被原样提交回后端存进线里,缺这一项不影响任何匹配。
func ParentSnapshot(p *Pet) EggParent {
	if p == nil {
		return EggParent{}
	}
	return EggParent{
		Gid: p.Gid, Name: p.Name, Species: p.Species, ConfID: p.ConfID,
		Img: p.Image.Head, Gender: p.Gender,
		HeightM: p.HeightM, WeightKg: p.WeightKg,
		HeightPct: p.HeightPct, WeightPct: p.WeightPct,
		Voice: p.Voice, Nature: p.Nature, Talent: p.TalentRank,
	}
}

// FillParentSnapshot 用库里的个体把快照**空缺**的固有属性补上,返回是否补过任何一项。
//
// 补这些(个体固有、不随时间与形态变化):Nature、Voice、Talent、HeightM/WeightKg
// 与它们的百分位。src 需已按当前 gamedata 算过百分位(见 FillSizePercentile)。
//
// **不补** Species / Evo / ConfID / Img:它们随进化形态变,而快照记的是**收蛋那一刻**
// 的样子 —— 当时是阿米亚特、如今已进化成罗隐,回填就会把史实改成现状,那正是
// ChainRef 认品种所依赖的那份凭据(见 EggParent.Evo 的注释)。空着只是显示少一行,
// 补错则是把整条培育史的身份换掉。
//
// 判据用「该字段为空」(数值字段即 0)。嗓音恰好为 0 的历史快照会被误当成缺失再补一次,
// 但补进去的仍是同一个人当时/现在的嗓音,且本函数只用于**读取时投影**(不落库),
// 真要写进历史得玩家在页面上点一下确认。
func FillParentSnapshot(snap *EggParent, src *Pet) bool {
	if snap == nil || src == nil {
		return false
	}
	filled := false
	if snap.Nature == "" && src.Nature != "" {
		snap.Nature, filled = src.Nature, true
	}
	if snap.Voice == 0 && src.Voice != 0 {
		snap.Voice, filled = src.Voice, true
	}
	if snap.Talent == "" && src.TalentRank != "" {
		snap.Talent, filled = src.TalentRank, true
	}
	if snap.HeightM == 0 && src.HeightM != 0 {
		snap.HeightM, filled = src.HeightM, true
	}
	if snap.WeightKg == 0 && src.WeightKg != 0 {
		snap.WeightKg, filled = src.WeightKg, true
	}
	if snap.HeightPct == nil && src.HeightPct != nil {
		snap.HeightPct, filled = src.HeightPct, true
	}
	if snap.WeightPct == nil && src.WeightPct != nil {
		snap.WeightPct, filled = src.WeightPct, true
	}
	return filled
}

// EggGroupsMatch 两串蛋组是否有交集:公母至少要共用一个蛋组才能配对产蛋。空串按「双方都
// 不知道该宠的蛋组」处理,一律不匹配 —— 拿不确定的数据去推荐配对,只会给出配不出来的组合。
func EggGroupsMatch(a, b []gamedata.EggGroup) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	for _, x := range a {
		for _, y := range b {
			if x.Name != "" && x.Name == y.Name {
				return true
			}
		}
	}
	return false
}

// ChainRef 是一条培育线认的**品种**:进化链 id(Evo),或(该形态没有链时)名字 Species。
//
// 为什么不能按名字认 —— 升级前就是这么做的,而它配不出候选:
//   - 一条链上的各阶段是**同一个**品种:蛋的物种随母本(docs/data.md 3.6),链上任一阶段的 ♀
//     都能当种母。按名字比会把链上其它阶段全漏掉 —— 线记的是罗隐,而窝里那只阿米亚特♀
//     就被判成「别的品种」,库里明明有却配不出任何组合。
//   - 反过来,同一只精灵的不同**形态**各占一条链(嗜波螺「本来的样子」3508 与「被污染的样子」
//     3511),链 id 天然把它们分开,故身份里不必再带「形态」这一维。
//
// Evo==0 的形态没有链(实测 1147 条 petbase 里 526 条:首领/活动/内部占位形态),此时退回按
// **名字**比 —— 与升级前一致,这类品种本就没有「链上其它阶段」可言。
type ChainRef struct {
	Evo     uint32
	Species string
	// Bases 是链上全部形态的 petbase id(Evo==0 时为空,匹配走 Species)。
	//
	// 为什么是形态集合而不是「比链 id」:个体身上只有 base_conf_id(当前形态)与形态名,
	// 协议里根本没有链 id —— 认它属不属于这条链,只能拿这个集合去撞。由 ChainRefOf 从
	// gamedata 展开,别在别处手工构造。
	Bases map[uint32]bool
}

// ChainRefOf 由品种标识(链 id + 名字)建匹配引用。
//
// 链 id 缺失时按名字补一次(gamedata.ChainByName):手上只有一个名字的来源有两处 ——
// 链口径之前建的老线(只存了 species),以及收蛋时母本还没入库、快照里没算上 Evo 的那种
// (见 pipeline.parentSnap)。不补的话,同品种的一边是链、一边是名字,Same 直接 false,
// 表现就是「破壳后又另开一条线」。
//
// 补得对不对由 ChainByName 把关:同名形态不并属一条链时它返回 0,这里就退回按名字匹配
// (与升级前一致,不会把一只同名个体错判成别的品种而漏掉)。
func ChainRefOf(db *gamedata.DB, evo uint32, species string) ChainRef {
	if evo == 0 && species != "" {
		evo = db.ChainByName(species)
	}
	r := ChainRef{Evo: evo, Species: species}
	if evo == 0 {
		return r
	}
	members := db.ChainMembers(evo)
	r.Bases = make(map[uint32]bool, len(members))
	for _, base := range members {
		r.Bases[base] = true
	}
	return r
}

// Match 某只个体是否属于这个品种。
//
// 它必须与 gamedata.ChainOptions 的去重口径**完全一致**:下拉里能选到的品种,这里就得配得出
// 候选;两边各写一套迟早分叉,而分叉的表现是「选了品种却一只候选都没有」。
func (r ChainRef) Match(p *Pet) bool {
	if p == nil {
		return false
	}
	if r.Evo == 0 {
		return r.Species != "" && p.Species == r.Species
	}
	return r.Bases[p.BaseConfID]
}

// Empty 这个引用还没有品种(新建的空线还没填品种)。
func (r ChainRef) Empty() bool { return r.Evo == 0 && r.Species == "" }

// Same 两个品种引用说的是不是同一个品种(口径与 Match 一致:有链比链、无链比名字)。
//
// 给「按品种找线」用(见 store.FindActiveBreedingLine):线存的是品种标识、拿到的是一对快照,
// 两边都得先化成引用才能比 —— 直接比结构体会被 Bases 里那张 map 骗到(相同品种的两个引用
// 各持一份 map,永远不等)。
func (r ChainRef) Same(o ChainRef) bool {
	if r.Evo != 0 || o.Evo != 0 {
		return r.Evo == o.Evo
	}
	return r.Species != "" && r.Species == o.Species
}

// DeriveChain 给**只有名字**的线补出品种身份(进化链 id);已有身份、或名字反查不到时不动。
//
// 两类调用方:① 链口径之前建的老线,库里只存了 species(读取时补,见 store 的三个读入口
// 与 handleBreedingSave);② 前端漏带 evo 的手工请求。与身高体重百分位同一套「从 gamedata
// 推导、不进库」的做法 —— 老线不必迁移,玩家下一次保存这条线时整条覆盖写自然带上。
//
// 口径与 ChainRefOf 完全一致(名字 → 链,由 gamedata.ChainByName 判定),两处若是各推导一套,
// 读出来的线与比对的引用就会各认一个品种。
//
// **名字有歧义时不猜**(ChainByName 返回 0):老线可能记的是某只同名变体,猜错会让线从此
// 配不出它本该配的那只 —— 而退回按名字匹配(与升级前一致)至少不会更差,玩家也可以在页面上
// 的品种下拉里明确选一次(那里给的是链,选定后 evo 非 0,本节不再介入)。
func DeriveChain(db *gamedata.DB, l *BreedingLine) {
	if db == nil || l == nil || l.Evo != 0 || l.Species == "" {
		return
	}
	l.Evo = db.ChainByName(l.Species)
}

// BreedPool 为某条培育线挑候选:库里同品种(链)的 ♀ 当种母,与母本共用蛋组的 ♂ 当种公。
//
// 为什么种母限定「同品种」:蛋的物种必定随母本(docs/data.md 3.6),想要这个品种的子代,
// 母本只能是这个品种。父本则只看蛋组 —— 父亲是谁不影响物种,只要能配上就有意义。
//
// 「同品种」的口径见 ChainRef:一条链上任一阶段的 ♀ 都算 —— 库里那只早就进化成罗隐了,
// 拿它配照样出这个品种;只按名字比的话这种母本一只都找不到。
//
// 配不出父本的母本不返回(没有可推荐组合,列出来只是噪音)。候选的 Ambiguous 表示「这位
// 母本有多个可配种公」,由玩家自己挑一只放进小窝,而不是我们替他选。
func BreedPool(ref ChainRef, pets []*Pet) Pool {
	return NewPoolSource(pets).BreedPool(ref)
}

// PoolSource 一次请求内**跨培育线共用**的候选素材:雄性快照与它们的蛋组掩码。
//
// 为什么单独拎出来:这两样只按**性别**筛,与品种(ref)无关 —— 同一份宠物库下每条线算出来
// 的雄性集合完全一样,而 handleBreeding 要为每个账号的全部线各建一次候选池。逐线各建
// 一遍等于把几百次 ParentSnapshot 与蛋组掩码重复做 N 遍(线通常个位数)。
//
// 与 handleBreeding 里已有的 pets / byGid 是同一层东西:一次请求建一次,给全部线用。
type PoolSource struct {
	pets     []*Pet // 母本仍要按 ref 筛,故全库留一份(只用于读)
	males    []EggParent
	maleMk   []uint64                  // 与 males 一一对应的蛋组掩码(见 maskOf)
	maleEggs [][]gamedata.EggGroup     // 同上:掩码不可用时的退回路径要用(见 eggMatch)
	// bitOf 组名 → 位。为 nil 表示蛋组种类超过 64,掩码装不下,一律退回字符串比较。
	bitOf map[string]uint64
}

// NewPoolSource 按一份宠物库建候选素材。蛋组在这里**一次性**编号成位(见 eggMatch)。
func NewPoolSource(pets []*Pet) PoolSource {
	src := PoolSource{pets: pets}
	seen := make(map[string]bool, 16)
	for _, p := range pets {
		for _, g := range p.EggGroups {
			if g.Name != "" {
				seen[g.Name] = true
			}
		}
	}
	// 掩码只装得下 64 种组。超了就整体退回字符串比较 —— 而不是给一部分宠物用掩码、
	// 另一部分不用:两条路算出来的「能不能配」必须时刻一致,混着用迟早分叉。
	if len(seen) <= 64 {
		src.bitOf = make(map[string]uint64, len(seen))
		var bit uint64 = 1
		for n := range seen {
			src.bitOf[n] = bit
			bit <<= 1
		}
	}
	for _, p := range pets {
		if p.Gender != "♂" {
			continue
		}
		src.males = append(src.males, ParentSnapshot(p))
		src.maleMk = append(src.maleMk, src.maskOf(p.EggGroups))
		src.maleEggs = append(src.maleEggs, p.EggGroups)
	}
	return src
}

// maskOf 蛋组 → 位掩码。未登记的名字(含空名)贡献 0 位,与 EggGroupsMatch 跳过空名同义。
func (src PoolSource) maskOf(gs []gamedata.EggGroup) uint64 {
	if src.bitOf == nil {
		return 0
	}
	var m uint64
	for _, g := range gs {
		m |= src.bitOf[g.Name]
	}
	return m
}

// eggMatch 这位母本与第 maleIdx 位种公能不能配上(蛋组有交集)。
//
// **必须与 EggGroupsMatch 同义**,由 TestEggMatchSameAsGroupsMatch 逐对钉住。
//
// 走掩码是为了内层的「每只母本 × 全部雄性」循环 —— 那是全库唯一的平方级热点,嵌套比名字
// 时每对要做几次字符串 ==,换成掩码后只剩一次 AND。mMasks 是母本那侧**预算好**的掩码:
// 一位母本要跟几百只公比,不预算就是每位母本重算几百遍。
//
// 掩码不可用时(蛋组种类 > 64)原样退回 EggGroupsMatch —— 口径只有一份,不重写第二套。
func (src PoolSource) eggMatch(mEggs []gamedata.EggGroup, mMask uint64, maleIdx int) bool {
	if src.bitOf == nil {
		return EggGroupsMatch(mEggs, src.maleEggs[maleIdx])
	}
	return mMask&src.maleMk[maleIdx] != 0
}

// BreedPool 为本品种挑候选:库里同品种(链)的 ♀ 当种母,与母本共用蛋组的 ♂ 当种公。
//
// 返回的 Pool 里父本只有**一份**(见 Candidate.FatherIdx)。
func (src PoolSource) BreedPool(ref ChainRef) Pool {
	if ref.Empty() {
		return Pool{}
	}
	out := Pool{Fathers: src.males}
	var idxs []int32
	for _, m := range src.pets {
		if !ref.Match(m) || m.Gender != "♀" {
			continue
		}
		mMask := src.maskOf(m.EggGroups)
		for i, snap := range src.males {
			// 不能跟自己配(同一个体只有一个 gid)。
			if snap.Gid == m.Gid {
				continue
			}
			if !src.eggMatch(m.EggGroups, mMask, i) {
				continue
			}
			idxs = append(idxs, int32(i))
		}
		if len(idxs) == 0 {
			continue
		}
		c := Candidate{Mother: ParentSnapshot(m), FatherIdx: make([]int32, len(idxs))}
		copy(c.FatherIdx, idxs)
		idxs = idxs[:0]
		c.Ambiguous = len(c.FatherIdx) > 1
		out.Cands = append(out.Cands, c)
	}
	return out
}

// eggGroupNames 只取蛋组的社区名。desc 是官方描述,选种/补录都用不上,下发出去只是白占
// payload —— 库里近千只,每只两个蛋组就多出两条长文案。
func eggGroupNames(gs []gamedata.EggGroup) []string {
	if len(gs) == 0 {
		return nil
	}
	out := make([]string, 0, len(gs))
	for _, g := range gs {
		if g.Name != "" {
			out = append(out, g.Name)
		}
	}
	return out
}

// PetCandidate 是补录候选池下发用的精简宠物:取选种与记录快照所需的最小集。
//
// 为什么不直接下发 pet.Pet:整只里绝大部分字段用不上(六维、奖牌、各尺寸图片、盒位、
// 捕捉时间……),而候选池是**按只**下发的 —— 近千只每只少几 KB 就是几 MB 的响应。
// 结构内嵌 EggParent:它正是前端 toParent 要提交的那份快照形状,两处共用一份定义,
// 键名不会各写一套(否则补录提交上去的 voice / weightPct 之类对不上后端字段)。
type PetCandidate struct {
	EggParent
	EggGroups []string `json:"eggGroups,omitempty"` // 蛋组社区名(前端据此按母本蛋组过滤种公)
}

func petCandidate(p *Pet) PetCandidate {
	return PetCandidate{EggParent: ParentSnapshot(p), EggGroups: eggGroupNames(p.EggGroups)}
}

// BreedCandidates 按品种(链)派生补录用的三类候选:种母、种公、子代。
//
// 与 BreedPool 的分工:BreedPool 服务于**建议排序** —— 它按每条线自己的母本逐只配对,
// 并把配不出父本的母本整只丢掉;这里服务于**手动补录**的候选清单 —— 补录记的是玩家
// 亲眼见过的事实(很久以前孵的、蛋组数据缺失、用了道具),工具认为「配不出来」的组合
// 同样必须能填,故不做那种收缩。范围与 pets.js 的三个纯函数一一对应:
// 种母 = 同品种 ♀、子代 = 同品种、种公 = ♂ 再按该品种雌性的蛋组粗筛。
//
// 「同品种」的口径见 ChainRef(链上任一阶段都算),与 BreedPool 共用同一个 ref —— 两处
// 若各写一套,「建议里有它、补录里没有」这种不一致会让玩家以为补录漏了候选。
//
// 种公的粗筛用该品种**全部雌性的蛋组并集**,且任意一只雌性的蛋组未知时**整个不筛**。
// 理由:这只是把明显无关的雄性挡在 payload 外的粗筛,某只母本到底能配哪些公,由前端
// 按所选母本再过滤一次(fatherCandidates)。粗筛必须宁多勿漏 —— 漏掉一只,玩家就再也
// 选不到它;而蛋组未知的雌性在前端口径里是「不限蛋组」,这里若跟着筛就把她的公清空了。
//
// 粗筛停用时退回「同品种雄性」,而不是「全库雄性」:后者在某个品种一只雌性都没有时会
// 把几千只别的品种的公塞进 payload;品种根本不存在(玩家新建了库里没有的品种,或刚把
// 最后一只进化/放生掉)时更是应当一只都不给。
//
// 顺序按「名字升序、同名按 gid 降序」:同品种同名的个体很多(刷了一窝),只有稳定且可
// 预期的顺序,下拉里的位置才有意义;同名的把最新获得的排在前面,更可能是要找的那只。
func BreedCandidates(ref ChainRef, pets []*Pet) (mothers, fathers, kids []PetCandidate) {
	if ref.Empty() {
		return nil, nil, nil
	}
	var femaleEggs []gamedata.EggGroup
	knownAll := true
	for _, p := range pets {
		if !ref.Match(p) {
			continue
		}
		kids = append(kids, petCandidate(p))
		if p.Gender == "♀" {
			mothers = append(mothers, petCandidate(p))
			femaleEggs = append(femaleEggs, p.EggGroups...)
			if len(p.EggGroups) == 0 {
				knownAll = false
			}
		}
	}
	filterEggs := len(femaleEggs) > 0 && knownAll
	for _, p := range pets {
		if p.Gender != "♂" {
			continue
		}
		if filterEggs {
			if !EggGroupsMatch(femaleEggs, p.EggGroups) {
				continue
			}
		} else if !ref.Match(p) {
			continue // 蛋组判不了时退回同品种,别把全库雄性都当候选(见上面的注释)
		}
		fathers = append(fathers, petCandidate(p))
	}
	sortCandidates(mothers)
	sortCandidates(fathers)
	sortCandidates(kids)
	return mothers, fathers, kids
}

// sortCandidates 名字升序、同名按 gid 降序(见 BreedCandidates 的理由)。
func sortCandidates(cs []PetCandidate) {
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].Name != cs[j].Name {
			return cs[i].Name < cs[j].Name
		}
		return cs[i].Gid > cs[j].Gid
	})
}

// LatestChild 取最新一代里已认领的子代及其双亲,供回交建议使用;还没有子代时返回 false。
func LatestChild(l *BreedingLine) (child, mother, father EggParent, ok bool) {
	if l == nil {
		return EggParent{}, EggParent{}, EggParent{}, false
	}
	for i := len(l.Gens) - 1; i >= 0; i-- {
		g := l.Gens[i]
		if g.Child == nil || g.Child.Gid == 0 {
			continue
		}
		c := *g.Child
		m, f := EggParent{}, EggParent{}
		if g.Mother != nil {
			m = *g.Mother
		}
		if g.Father != nil {
			f = *g.Father
		}
		return c, m, f, true
	}
	return EggParent{}, EggParent{}, EggParent{}, false
}

// —— 一条线的演进(纯函数:只改这条线,不做 IO)——

// NextGen 返回接下来该分配的代数:已记代数与待认领代数取大者 +1。
//
// 用「取大」而不是「已记代数 +1」:待认领的那一代早就占了号,若下一颗蛋按已记代数发号,
// 两代就会同号 —— 认领按代数定位,便会补到错误的一代上。
func NextGen(l *BreedingLine) int {
	gen := 0
	if l != nil {
		gen = l.GenBase // 子线接在母线之后:本线第一代是 GenBase+1
	}
	for _, g := range l.Gens {
		if g.Gen > gen {
			gen = g.Gen
		}
	}
	for _, g := range l.Pending {
		if g.Gen > gen {
			gen = g.Gen
		}
	}
	return gen + 1
}

// AppendPending 追加一代「双亲已知、子代待认领」的记录,返回它的代数(无可记的返回 0)。
//
// 为什么要先落这一代、子代晚一步补:破壳回包只给出子代 gid,那一刻它还没入库(同一条消息先过
// 蛋这一路、再过宠物那一路,见 pipeline.handle),嗓声音体重百分位这些要进快照的属性都取不到。
// 中途没等到子代(离线、漏包)就留在 Pending 里由玩家在页面上指定 —— 不拿「窗口内新出现的同种
// 宠物」硬猜,猜错会污染整条培育史,而它正是这个页面的全部价值所在。
//
// eggGid 是这颗蛋自己的 gid(收蛋即记时给,见 Generation.EggGid)。非 0 时先查重:
// 同一颗蛋被重复下发(0x0243 重放、或收蛋与破壳两条路都走到)只记一代 —— 重放会让
// 时间线上多出一串双亲一模一样、永远等不到子代的代,而玩家无从分辨哪条是真的。
// 已经记过就返回那一代的代数(调用方据此回填蛋行,幂等)。
func AppendPending(l *BreedingLine, ps *EggParents, eggGid uint32, at int64) int {
	if l == nil || ps == nil || ps.Mother == nil {
		return 0 // 母本未知:这条史无从归属(蛋的物种随母本)
	}
	if eggGid != 0 {
		if g, ok := FindGenByEgg(l, eggGid); ok {
			return g.Gen
		}
	}
	g := Generation{
		Gen:     NextGen(l),
		Mother:  ps.Mother,
		Fathers: ps.Fathers,
		EggGid:  eggGid,
		Source:  GenSourceAuto,
		At:      at,
	}
	// 只有一个父本候选时当场定下来;串窝(多个候选)留空 —— 实际是谁只有玩家自己知道。
	if len(ps.Fathers) == 1 {
		f := ps.Fathers[0]
		g.Father = &f
	}
	l.Pending = append(l.Pending, g)
	return g.Gen
}

// FindGenByEgg 找这条线里由某颗蛋记下的那一代(待认领与已认领都算),没有返回 false。
//
// 已认领的也要查:一颗蛋只可能孵出一只,若它已经在正式代数里,重放就不该再建一代。
func FindGenByEgg(l *BreedingLine, eggGid uint32) (Generation, bool) {
	if l == nil || eggGid == 0 {
		return Generation{}, false
	}
	for _, g := range l.Pending {
		if g.EggGid == eggGid {
			return g, true
		}
	}
	for _, g := range l.Gens {
		if g.EggGid == eggGid {
			return g, true
		}
	}
	return Generation{}, false
}

// MergeChildLine 把子线**物理并入**母线:子线的历代原样搬过去,子线随后由调用方删掉。
//
// 为什么能「原样搬」而不用重排代数:子线的代数本来就是接着母线数的(见 NewChildLine 的
// GenBase),两边的代数不会撞,合并后按代数排一遍即可。这正是 GenBase 的第二个用处 ——
// 没有它,合并就得改子线每一代的 gen,那是个不可逆、且改错了无从察觉的操作。
//
// 搬什么:Gens 与 Pending 全搬(那是子线自己孵出来的历史)。**目标不搬** —— 子线的目标是
// 从母线继承来的,母线自己那份才是权威的,搬回去等于拿副本覆盖原件。
//
// **孙辈要由调用方重挂**:以这条子线为 ParentLineID 的线若不改挂到母线上,谱系视图上就
// 从这一代断掉(它们会指向一条已经不存在的线)。本函数只改传进来的这两条,碰不到全库。
//
// 返回是否真的搬了。**不可逆** —— 子线删掉就没有了,故调用方必须二次确认(见
// handleBreedingMerge)。子线不是这条线的孩子、或本来就没有代数,都返回 false 且不动手。
func MergeChildLine(parent, child *BreedingLine) bool {
	if parent == nil || child == nil || parent.ID == "" || child.ID == "" {
		return false
	}
	if child.ParentLineID != parent.ID || (len(child.Gens) == 0 && len(child.Pending) == 0) {
		return false
	}
	parent.Gens = append(parent.Gens, child.Gens...)
	sortGenerations(parent.Gens)
	parent.Pending = append(parent.Pending, child.Pending...)
	sortGenerations(parent.Pending)
	child.Gens, child.Pending = nil, nil
	return true
}

// MarkHatched 破壳回包给出孵出的 gid 时,把它记到那一代上。
//
// 这一代此刻必定还在 Pending —— 待孵的代只有认领(拿到子代快照)才挪进 Gens,而快照
// 要等子代进背包,故破壳后、认领前它仍在 Pending 里,只是多了一个 ChildGid。
// 找不到该代(这一代已被玩家丢弃,或蛋上没记 lineId/gen 走了兜底分支)返回 false。
func MarkHatched(l *BreedingLine, gen int, childGid uint32) bool {
	if l == nil || gen <= 0 || childGid == 0 {
		return false
	}
	for i := range l.Pending {
		if l.Pending[i].Gen == gen {
			if l.Pending[i].ChildGid == childGid {
				return false // 已经记过(同一颗蛋重复破壳):不再改
			}
			l.Pending[i].ChildGid = childGid
			return true
		}
	}
	return false
}

// ClaimGeneration 把待认领的第 gen 代补上子代并挪进正式代数;找不到该代返回 false。
//
// 认领不改这一代的 Source:它记的是「这一代最初怎么进来的」(管线自动记的,还是玩家补录的),
// 认领只补全子代,不改变它的出身。
func ClaimGeneration(l *BreedingLine, gen int, child EggParent) bool {
	if l == nil {
		return false
	}
	for i, g := range l.Pending {
		if g.Gen == gen {
			return claimAt(l, i, child)
		}
	}
	return false
}

// ClaimChild 按**子代 gid** 认领:把等着这只宠的那一代补上快照(慢路径)。
//
// 与 ClaimGeneration 的分工:那个按代数定位(快路径 —— 破壳时刚写下 lineID/gen,
// 连接还在,代数就是现成的);这个按 gid 定位,用于**连接已经不在**的情形 ——
// 离线回放、进程重启、玩家在别的设备上孵的蛋。此时手头只有「这只宠物是谁」,
// 而 gid 早就随那一代落了库,按它找即可,gid 是唯一实例 id 不会撞。
//
// 两处共用 claimAt:搬动逻辑只该有一份(见 api_breeding.go 的注释)。
func ClaimChild(l *BreedingLine, childGid uint32, child EggParent) bool {
	if l == nil || childGid == 0 {
		return false
	}
	for i := range l.Pending {
		if l.Pending[i].ChildGid == childGid && l.Pending[i].Child == nil {
			return claimAt(l, i, child)
		}
	}
	return false
}

// claimAt 把 Pending[idx] 补上子代并挪进正式代数。**搬动只在这里发生一次** ——
// 手动认领(/api/breeding/claim)、管线快路径、慢路径三条路都落到它。
func claimAt(l *BreedingLine, idx int, child EggParent) bool {
	g := l.Pending[idx]
	g.Child = &child
	l.Pending = append(l.Pending[:idx], l.Pending[idx+1:]...)
	l.Gens = append(l.Gens, g)
	sortGenerations(l.Gens)
	return true
}

// sortGenerations 按代数升序。用插入排序而非 sort.Slice:序列几乎总是已有序(只有认领时插
// 一条待认领的代进来),插入排序在这种输入上就是一次遍历,也不为几十条记录去分配闭包。
func sortGenerations(gens []Generation) {
	for i := 1; i < len(gens); i++ {
		for j := i; j > 0 && gens[j].Gen < gens[j-1].Gen; j-- {
			gens[j], gens[j-1] = gens[j-1], gens[j]
		}
	}
}

// NewAutoLine 用一次破壳的双亲快照开一条新线(该品种还没有进行中的线时自动建)。
//
// 目标留空:BreedingGoal 三项皆可为空,先记下「孵了什么」,目标等玩家回头再定 ——
// 「从一次孵蛋开始」比「先建线再孵」顺手,而没定的目标也不影响时间线的记录。
//
// 品种身份取自**母本的快照**(Species + Evo):蛋的物种随母本,故这条线认的就是母本那
// 个品种。快照里的 Evo 是收蛋那一刻按母本形态算的(见 pipeline 的 parentSnap),这正是
// 「亲本之后进化/被放生也不影响认品种」的原因。
func NewAutoLine(id string, ps *EggParents, at int64) *BreedingLine {
	l := &BreedingLine{ID: id, Status: BreedingActive, CreatedAt: at, UpdatedAt: at}
	if ps != nil && ps.Mother != nil {
		l.Evo = ps.Mother.Evo
		l.Species = ps.Mother.Species
		l.ConfID = ps.Mother.ConfID
		l.MotherGid = ps.Mother.Gid
	}
	return l
}

// NewChildLine 开一条**子线**:换上的新种母是 parent 孵出来的子代时走这里。
//
// 为什么换了种母就得换行:线的身份是种母(见 MotherGid),换了就是另一条线 —— 这是「同一
// 品种几条线互不干扰」的代价,也是它的前提。故这里不接着母线记,而是挂成子线:
//   - ParentLineID 指向母线,页面沿链把培育史串起来;
//   - 继承母线的目标(换种母不是换目标,玩家没必要重填一遍);
//   - GenBase 取母线最后一代的代数,故子线的第一代接着往下数,代数在整条谱系上连续。
//
// **只继承目标**:Gens / Pending 是母线的历史,由 ParentLineID 引用,不复制过来 —— 复制就
// 成了两份,将来物理合并时无从判断哪份是真的。
//
// 野外抓来的、与任何线都没有血缘的母本**不**走这里,而是 NewAutoLine 开独立新线。
func NewChildLine(id string, parent *BreedingLine, ps *EggParents, at int64) *BreedingLine {
	l := NewAutoLine(id, ps, at)
	if parent == nil {
		return l
	}
	l.ParentLineID = parent.ID
	l.Goal = parent.Goal
	l.GenBase = NextGen(parent) - 1 // 母线最后一代的代数
	return l
}
