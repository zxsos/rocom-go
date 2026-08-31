package server

import (
	"net/http"
	"sync"
	"time"

	"github.com/whoisnian/rocom-capture/internal/store"
)

// 本文件承载 Server 上几组互不相关的状态,各自带自己的锁。
//
// 背景:原先这些都平铺在 Server 结构体里(见 server.go 的 Server 定义),
// 6 类职责共用 8 把锁,读代码分不清哪把锁保护什么、也容易顺手拿错锁。
// 拆出来后每组的锁与数据相邻,职责边界由类型表达,而不是靠注释约定。
//
// 拆分原则:只搬「数据 + 它自己的锁 + 只碰这组数据的操作」。
// 需要跨两组数据协作的逻辑仍留在 Server 上,由它按正确顺序取锁 —— 类型边界挡不住
// 死锁,但至少让「谁和谁会同时被拿」变得显式。

// onlineTracker 记录各账号最近活跃时刻,供「在线」判定。
type onlineTracker struct {
	mu       sync.Mutex
	lastSeen map[string]int64 // 账号 -> 最近活跃 Unix 秒(pipeline 上报,/api/accounts 据此标在线)
}

func newOnlineTracker() *onlineTracker {
	return &onlineTracker{lastSeen: map[string]int64{}}
}

// onlineWindow 是「在线」判定窗口(秒):账号最近这段秒数内有流量即算在线。
// 游戏客户端保持连接时约 1.6s 一条心跳,断线/关游戏后不再有消息,30s 足够区分。
const onlineWindow = 30

// touch 记录账号最近活跃时刻(Unix 秒)。pipeline 对每条可归属消息调用;
// 离线回放时消息自带历史时间戳,账号如实显示离线。
func (o *onlineTracker) touch(acc string, ts int64) {
	o.mu.Lock()
	if o.lastSeen == nil {
		o.lastSeen = map[string]int64{}
	}
	o.lastSeen[acc] = ts
	o.mu.Unlock()
}

// online 判定账号是否在线(最近 onlineWindow 秒内有流量),handleAccounts 逐账号合并。
func (o *onlineTracker) online(acc string) bool {
	o.mu.Lock()
	ts, ok := o.lastSeen[acc]
	o.mu.Unlock()
	return ok && time.Now().Unix()-ts <= onlineWindow
}

// latest 返回最近活跃的账号与其时刻;无记录时返回空串。
func (o *onlineTracker) latest() (acc string, ts int64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for a, t := range o.lastSeen {
		if t > ts {
			ts, acc = t, a
		}
	}
	return acc, ts
}

// forget 删除账号记录(删账号时用)。
func (o *onlineTracker) forget(acc string) {
	o.mu.Lock()
	delete(o.lastSeen, acc)
	o.mu.Unlock()
}

// acctResolver 推导请求指向的账号:优先 ?account=,缺省回退最近活跃账号。
//
// 回退路径优化:原先每次都调 ListAccounts()(LEFT JOIN pets + GROUP BY + ORDER BY 全表扫),
// 前端首次加载时并行发 5-8 个 API、每个都触发一次,多账号场景下是明显的延迟来源。
// 现改为优先从 onlineTracker 推导最近活跃账号(5s 缓存),仅在无流量(刚启动/纯离线回放)
// 时才回退到 ListAccounts() 查库。
type acctResolver struct {
	mu   sync.Mutex
	val  string
	time time.Time

	online *onlineTracker
	store  *store.Store
}

func newAcctResolver(online *onlineTracker, st *store.Store) *acctResolver {
	return &acctResolver{online: online, store: st}
}

const acctCacheTTL = 5 * time.Second

// resolve 返回请求指向的账号;库空且无活跃账号时返回空串。
func (a *acctResolver) resolve(r *http.Request) string {
	if v := r.URL.Query().Get("account"); v != "" {
		return v
	}
	// 快路径:5s 内复用缓存结果,避免并发请求重复推导
	a.mu.Lock()
	if a.val != "" && time.Since(a.time) < acctCacheTTL {
		v := a.val
		a.mu.Unlock()
		return v
	}
	a.mu.Unlock()

	// 从在线表找最近活跃账号(内存,无查库)
	if best, _ := a.online.latest(); best != "" {
		a.mu.Lock()
		a.val, a.time = best, time.Now()
		a.mu.Unlock()
		return best
	}

	// 在线表为空(刚启动/纯离线回放):回退到查库
	accs, err := a.store.ListAccounts()
	if err != nil || len(accs) == 0 {
		return ""
	}
	a.mu.Lock()
	a.val, a.time = accs[0].Account, time.Now()
	a.mu.Unlock()
	return accs[0].Account
}

// snapshotStore 存放各账号的「最近一次」实时快照,供页面加载时即时回显,
// 不必等下一条推送(位置要等下一次移动、野生宠要等下一条 AOI 通知)。
//
// 四类数据**互不相干**,原先共用 Server 上一把 posMu(见重构前的 server.go),
// 读代码分不清保护范围、也容易顺手拿错锁;现各自一把锁,读写路径更短。
type snapshotStore struct {
	posMu    sync.Mutex
	wildMu   sync.Mutex
	homeMu   sync.Mutex
	flowerMu sync.Mutex
	trialMu  sync.Mutex

	pos    map[string]*PositionPayload // 账号 -> 最近一次位置
	wild   map[string]*WildPayload     // 账号 -> 最近一次野生宠物标记
	home   map[string]*HomePayload     // 账号 -> 最近一次家园小窝图层
	flower map[string]*FlowerPayload   // 账号 -> 最近一次花种 BOSS 分组
	trial  map[string]*TrialPayload    // 账号 -> 最近一次草系试炼状态
}

func newSnapshotStore() *snapshotStore {
	return &snapshotStore{
		pos:    map[string]*PositionPayload{},
		wild:   map[string]*WildPayload{},
		home:   map[string]*HomePayload{},
		flower: map[string]*FlowerPayload{},
		trial:  map[string]*TrialPayload{},
	}
}

// 空账号一律忽略:管线对无法归属的消息传空串,写进去只会留下取不出的垃圾。
func (sn *snapshotStore) setPos(acc string, v *PositionPayload) {
	if acc == "" || v == nil {
		return
	}
	sn.posMu.Lock()
	sn.pos[acc] = v
	sn.posMu.Unlock()
}

func (sn *snapshotStore) getPos(acc string) *PositionPayload {
	sn.posMu.Lock()
	defer sn.posMu.Unlock()
	return sn.pos[acc]
}

func (sn *snapshotStore) setWild(acc string, v *WildPayload) {
	if acc == "" || v == nil {
		return
	}
	sn.wildMu.Lock()
	sn.wild[acc] = v
	sn.wildMu.Unlock()
}

func (sn *snapshotStore) getWild(acc string) *WildPayload {
	sn.wildMu.Lock()
	defer sn.wildMu.Unlock()
	return sn.wild[acc]
}

func (sn *snapshotStore) setHome(acc string, v *HomePayload) {
	if acc == "" || v == nil {
		return
	}
	sn.homeMu.Lock()
	sn.home[acc] = v
	sn.homeMu.Unlock()
}

func (sn *snapshotStore) getHome(acc string) *HomePayload {
	sn.homeMu.Lock()
	defer sn.homeMu.Unlock()
	return sn.home[acc]
}

func (sn *snapshotStore) setFlower(acc string, v *FlowerPayload) {
	if acc == "" || v == nil {
		return
	}
	sn.flowerMu.Lock()
	sn.flower[acc] = v
	sn.flowerMu.Unlock()
}

func (sn *snapshotStore) getFlower(acc string) *FlowerPayload {
	sn.flowerMu.Lock()
	defer sn.flowerMu.Unlock()
	return sn.flower[acc]
}

func (sn *snapshotStore) setTrial(acc string, v *TrialPayload) {
	if acc == "" || v == nil {
		return
	}
	sn.trialMu.Lock()
	sn.trial[acc] = v
	sn.trialMu.Unlock()
}

func (sn *snapshotStore) getTrial(acc string) *TrialPayload {
	sn.trialMu.Lock()
	defer sn.trialMu.Unlock()
	return sn.trial[acc]
}

// injectFlower 在锁内把一个花种插入某账号快照的「当前分组」与「自己世界」槽,
// 返回新快照供广播(与 InjectFlowerItem 的语义一致:新花种排在最前)。
//
// 并发安全:flower 里的 map 由 pipeline 与 server 共享,一经发布即视为不可变;
// 任何修改都必须「构建新 map 再替换引用」,严禁原地改动 —— 否则与管线/HTTP 读取方
// 并发时会触发 Go map 并发读写 fatal error(整个进程崩溃,不是可 recover 的 panic)。
func (sn *snapshotStore) injectFlower(acc string, f FlowerItem) *FlowerPayload {
	sn.flowerMu.Lock()
	defer sn.flowerMu.Unlock()
	old := sn.flower[acc]
	res := &FlowerPayload{Account: acc, Cur: "self"}
	flowers := []FlowerItem{f}
	worlds := FlowerWorlds{}
	if old != nil {
		if old.Cur != "" {
			res.Cur = old.Cur
		}
		flowers = append(flowers, old.Flowers...)
		for k, v := range old.Worlds {
			worlds[k] = v
		}
	}
	// 同步自己世界槽:管理页切到「自己世界」槽位时数据一致;槽不存在则创建。
	// 复制槽 map 再插新花种,不原地改 pipeline 持有的槽(已发布数据不可变)。
	if self, ok := worlds["self"]; ok && self != nil {
		ns := *self // 复制槽再改,不原地改 pipeline 持有的槽
		ns.Flowers = append([]FlowerItem{f}, self.Flowers...)
		ns.TS = time.Now().Unix()
		worlds["self"] = &ns
	} else {
		worlds["self"] = &FlowerWorld{Flowers: []FlowerItem{f}, TS: time.Now().Unix()}
	}
	res.Flowers = flowers
	res.Worlds = worlds
	sn.flower[acc] = res
	return res
}

// dropFlower 在锁内从某账号花种快照删除指定 npc_logic_id 的花种(撤销投放),
// 同步清理「自己世界」槽。返回 (新快照, 是否真的删掉了)。
func (sn *snapshotStore) dropFlower(acc string, logicID uint64) (*FlowerPayload, bool) {
	sn.flowerMu.Lock()
	defer sn.flowerMu.Unlock()
	old := sn.flower[acc]
	if old == nil {
		return nil, false
	}
	flowers := old.Flowers
	removed := false
	nf := make([]FlowerItem, 0, len(flowers))
	for _, it := range flowers {
		if it.NpcLogicID == logicID {
			removed = true
			continue
		}
		nf = append(nf, it)
	}
	if !removed {
		return nil, false
	}
	np := *old
	np.Flowers = nf
	// 自己世界槽同步删;槽空了就整个删掉(前端按槽是否存在显示)
	if self, ok := old.Worlds["self"]; ok && self != nil {
		slot := make([]FlowerItem, 0, len(self.Flowers))
		for _, it := range self.Flowers {
			if it.NpcLogicID != logicID {
				slot = append(slot, it)
			}
		}
		ns := *self
		ns.Flowers = slot
		ns.TS = time.Now().Unix()
		nw := make(FlowerWorlds, len(old.Worlds))
		for k, v := range old.Worlds {
			nw[k] = v
		}
		nw["self"] = &ns
		np.Worlds = nw
	}
	sn.flower[acc] = &np
	return &np, true
}

// mergeWild 在锁内把一批标记并入某账号的野生宠快照(管理员投放注入精灵用),
// 返回合并后的完整快照供广播。
//
// 收敛成方法的原因同 dropFlowerWorld:原先这段读-改-写在 handler 里手动 posMu.Lock()
// 后直接读写 lastWild,字段收进本类型后外部已拿不到锁,只能由本类型保证原子性。
func (sn *snapshotStore) mergeWild(acc string, sceneRes int32, marks []WildMark) *WildPayload {
	sn.wildMu.Lock()
	defer sn.wildMu.Unlock()
	cur := sn.wild[acc]
	if cur == nil {
		cur = &WildPayload{Account: acc, SceneResID: sceneRes,
			Pets: []WildMark{}, AllPets: []WildAllMark{}}
	}
	cur.Pets = append(cur.Pets, marks...)
	sn.wild[acc] = cur
	return cur
}

// dropWild 在锁内从某账号野生宠快照移除指定 id 的标记(撤销注入用),
// 返回是否真的移除了。
func (sn *snapshotStore) dropWild(acc, id string) bool {
	sn.wildMu.Lock()
	defer sn.wildMu.Unlock()
	cur := sn.wild[acc]
	if cur == nil {
		return false
	}
	found := false
	out := make([]WildMark, 0, len(cur.Pets))
	for _, p := range cur.Pets {
		if p.ID == id {
			found = true
			continue
		}
		out = append(out, p)
	}
	if !found {
		return false
	}
	cur.Pets = out
	sn.wild[acc] = cur
	return true
}

// dropFlowerWorld 原子删除某账号花种快照里的一个世界存档槽,返回是否真的删掉了。
//
// 必须整个读-改-写都在锁内完成:原先这段逻辑在 handler 里手动 posMu.Lock() 后
// 直接读写 s.lastFlowers,快照字段收进本类型后外部已拿不到它的锁,
// 故收敛成一个方法,由本类型保证原子性。
func (sn *snapshotStore) dropFlowerWorld(acc, key string) bool {
	sn.flowerMu.Lock()
	defer sn.flowerMu.Unlock()
	old := sn.flower[acc]
	if old == nil {
		return false
	}
	if _, exists := old.Worlds[key]; !exists {
		return false
	}
	// 复制槽表再删,不原地改已发布的共享表(管线/HTTP/广播读取方均无锁)。
	nw := make(FlowerWorlds, len(old.Worlds))
	for k, v := range old.Worlds {
		if k == key {
			continue
		}
		nw[k] = v
	}
	np := *old
	np.Worlds = nw
	if old.Cur == key {
		np.Cur = ""
	}
	sn.flower[acc] = &np
	return true
}

// forget 删除账号的全部快照(删账号时用)。
func (sn *snapshotStore) forget(acc string) {
	sn.posMu.Lock()
	delete(sn.pos, acc)
	sn.posMu.Unlock()
	sn.wildMu.Lock()
	delete(sn.wild, acc)
	sn.wildMu.Unlock()
	sn.homeMu.Lock()
	delete(sn.home, acc)
	sn.homeMu.Unlock()
	sn.flowerMu.Lock()
	delete(sn.flower, acc)
	sn.flowerMu.Unlock()
	sn.trialMu.Lock()
	delete(sn.trial, acc)
	sn.trialMu.Unlock()
}

// smtpSender 串行发送远行商人订阅邮件。
//
// 串行(而非并发)是刻意的:QQ 邮箱对并发连接敏感,容易判为异常触发限流。
type smtpSender struct {
	mu   sync.Mutex
	user string // 发件 QQ 邮箱;空=订阅提醒不可用
	pass string // SMTP 授权码

	// 测试注入:非 nil 时 smtpSendMail 改走它而不真连 QQ SMTP(见 merchant_notify_test.go)。
	sendFn func(to, subject, html string, imgs []merchantMailImg) error
}

func newSMTPSender(user, pass string) *smtpSender {
	return &smtpSender{user: user, pass: pass}
}

// configured 返回 SMTP 是否已配置。
func (m *smtpSender) configured() bool { return m.user != "" && m.pass != "" }
