// Package pipeline 消费抓包引擎解密出的应用层消息流:归属账号、更新宠物库、产生获得事件,
// 维护实时地图与眠枭之星状态,并经 server 广播给前端。
// 文件划分:pets(宠物入库/事件)/ position(实时地图/分层)/ stars(眠枭之星判定)。
package pipeline

import (
	"encoding/hex"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/whoisnian/rocom-capture/internal/capture"
	"github.com/whoisnian/rocom-capture/internal/gamedata"
	"github.com/whoisnian/rocom-capture/internal/gcp"
	"github.com/whoisnian/rocom-capture/internal/pb"
	"github.com/whoisnian/rocom-capture/internal/pet"
	"github.com/whoisnian/rocom-capture/internal/scene"
	"github.com/whoisnian/rocom-capture/internal/server"
	"github.com/whoisnian/rocom-capture/internal/store"
)

// grace 是「初始快照」的判定余量(秒):add_time 早于服务启动前 grace 的宠物视为存量仓库,
// 不产生获得事件;服务运行期间新捕捉的宠物 add_time≈当前,才推事件。
const grace = 120

// Pipeline 持有消费循环的全部依赖与状态。非并发安全:所有状态只在 Run 的单 goroutine 内读写。
type Pipeline struct {
	st      *store.Store
	db      *gamedata.DB
	srv     *server.Server
	startTS int64

	// connAccount: GCP 连接(connID)→账号("UID:"+user_id)。同一客户端 IP 可能同时跑多个
	// 账号(不同设备经 NAT 同 IP、或不同游戏服),故按 user_id 而非 IP 归属:抓到某连接的
	// LOGIN_RSP 时解析 user_id 建映射。登录回包自身也带背包/队伍/奖牌快照,须先登记再归属。
	// 从库中预热已知映射:配合会话密钥缓存,抓包服务重启后无需再等登录回包即可归属消息。
	connAccount map[string]string
	conns       map[string]*connState
	accts       map[string]*acctState
}

// connState 是单条 GCP 连接的实时地图状态。场景 res 与区域只在切场景/跨触发体时下发、
// 游戏中途不重发,故经 store 的 sessions 表落盘,重启后预热恢复(否则虽能解密移动包,
// 却因不知当前 res 而无法定位底图——移动包只带 scene_cfg_id)。
type connState struct {
	res  int32 // 当前 scene_res_cfg_id(s2c 进入/传送更新;0=未知)
	room int32 // 家园房屋等级(家园室内选分层底图,非家园为 0)
	// areas: area_func_id → 该 func 下已进入的 area_id 集合。由服务器的区域进/出事件维护
	// (见 scene.ParseAreaActs),是「玩家当前在洞穴/几楼」的权威依据。一个 func 可含多个
	// area(如信仰者村落一层同时进入 541030265/541030499),故按 func 存 area 集合:
	// 离开其中一个仍在该层,集合空了才算离开。
	areas      map[uint32]map[uint32]bool
	layer      *layerState               // 分层地图去抖状态(见 layerDebounce)
	stars      *starTracker              // 眠枭之星观测态(换场景/传送即重置)
	wilds      *wildTracker              // 野生宠物图层观测态(同上,见 wildpets.go)
	pos        scene.Position            // 最近一次移动包/传送落点的玩家世界坐标(涂地要从这儿画到宠物那儿)
	wildSeen   map[uint64]scene.Position // 当前 AOI 里**全部**野生宠实体的位置(涂地用,不只稀有那几只)
	pendantRid int32                     // 最近一次挂件交互(0x0272)的刷新行 id,等回包(0x0273)确认
	home       *homeState                // 家园小窝图层状态(仅在家园场景内非空,见 home.go)
	crackEgg   uint32                    // 最近一次破壳请求(0x030b)的 egg_gid,回包确认后把这颗蛋删掉
	// 游玩会话跟踪(管理后台「游玩记录」,见 playsession.go):last 是该连接最后一条可归属
	// 消息的墙钟时刻,兜底扫描据此判定下线(长时间无流量);sessionOpen 表示已有进行中会话,
	// handle 里用它避免每条消息都查库(状态翻转才读写 store)。
	last        time.Time
	sessionOpen bool
}

// acctState 是单个账号的消费状态。
type acctState struct {
	sweep *petSweep // 正在累积的分页宠物列表快照(末页对账,见 petSweep)
	// zoneGot: camp → 该区已收集数合计(服务器口径,各形态相加);nil=尚未从库预热。
	// 用作星星判定的守卫(见 sweepStars)。
	zoneGot map[int32]int32
	// starKnown: 已确认的星星状态(库内快照,只写增量);nil=尚未从库预热。
	starKnown map[int32]int
	eggSweep  *eggSweep               // 正在累积的分页背包快照(末页对账,见 eggs.go)
	lastPos   *server.PositionPayload // 最近推送的位置载荷(layerOnly 更新时合并回缓存)
	// lastFlowerLogicID: 最近一次选中进入战斗的花种 npc_logic_id(c2s 0x034E)。
	// 捕捉成功(0x132c 内嵌 goods_reward 的新宠物 catch_way=4)时据此清掉该花的 0x0338 详情,需重新点击查看。
	lastFlowerLogicID uint64
	// lastSeen: 最近一条可归属消息的应用层时间戳(秒)。与 server 在线表同步时按秒去重,
	// 免得移动包 8 条/秒高频刷锁——在线判定只认 30s 窗口,同秒内的更新没有意义。
	lastSeen int64
}

// New 创建消费管线并从库中预热连接归属/场景状态(抓包服务重启后无缝续接)。
func New(st *store.Store, db *gamedata.DB, srv *server.Server) *Pipeline {
	p := &Pipeline{
		st: st, db: db, srv: srv,
		startTS:     time.Now().Unix() - grace,
		connAccount: map[string]string{},
		conns:       map[string]*connState{},
		accts:       map[string]*acctState{},
	}
	if saved, err := st.LoadSessionAccounts(); err == nil {
		p.connAccount = saved
	}
	if saved, err := st.LoadSessionScenes(); err == nil {
		for id, s := range saved {
			cs := p.conn(id)
			// 预热的连接把「最后活跃」置为当下:游戏仍在线时消息会持续刷新它(不误杀),
			// 已下线/已关闭的则被兜底扫描按超时补记下线(见 sweepOnce)。
			cs.last = time.Now()
			cs.res, cs.room = s.Res, s.Room
			for fn, ids := range s.Areas {
				set := map[uint32]bool{}
				for _, a := range ids {
					set[a] = true
				}
				if len(set) > 0 {
					if cs.areas == nil {
						cs.areas = map[uint32]map[uint32]bool{}
					}
					cs.areas[fn] = set
				}
			}
		}
	}
	return p
}

// conn 返回(必要时创建)某连接的状态。
func (p *Pipeline) conn(id string) *connState {
	cs := p.conns[id]
	if cs == nil {
		cs = &connState{}
		p.conns[id] = cs
	}
	return cs
}

// acct 返回(必要时创建)某账号的状态。
func (p *Pipeline) acct(acc string) *acctState {
	as := p.accts[acc]
	if as == nil {
		as = &acctState{}
		p.accts[acc] = as
	}
	return as
}

// sessionIdleTimeout 是游玩会话的「无流量判定下线」阈值:游戏心跳约 1.6s 一条,超过该时长
// 完全没有可归属消息(切后台/关游戏/断网)即视为下线,结束会话。比 capture 的 closeIdle
// (2min,空闲关流)更早兜底;结束后该连接再有消息(回前台/重连)会自动重开新会话,不丢游玩时间。
const sessionIdleTimeout = 90 * time.Second

// sweepInterval 是兜底扫描周期。
const sweepInterval = 30 * time.Second

// Run 消费 eng.Out 与连接断开通知,直到消息通道关闭(离线回放结束时)。期间定期兜底扫描
// 游玩会话。所有状态只在当前 goroutine 内读写,故无并发问题。
func (p *Pipeline) Run(eng *capture.Engine) {
	sweep := time.NewTicker(sweepInterval)
	defer sweep.Stop()
	for {
		select {
		case m, ok := <-eng.Out:
			if !ok {
				// 离线回放结束:把剩余连接断开通知处理完(正常退出,不留悬挂会话)。
				for cid := range eng.CloseCh {
					p.onConnClose(cid)
				}
				return
			}
			p.handle(m)
		case cid := <-eng.CloseCh:
			p.onConnClose(cid)
		case <-sweep.C:
			p.sweepOnce(time.Now())
		}
	}
}

// onConnClose 处理一条连接断开(capture 检测到 TCP 结束或空闲超时):结束该连接的游玩会话、
// 记录下线时间。其余连接状态保留——断线重连/挂后台回前台后,该连接再有消息会在 handle 里
// 自动重开新会话,游玩时间不丢。
func (p *Pipeline) onConnClose(connID string) {
	if err := p.st.EndPlaySession(connID, time.Now().Unix()); err != nil {
		log.Printf("EndPlaySession 失败: %v", err)
	}
	if cs := p.conns[connID]; cs != nil {
		cs.sessionOpen = false
	}
}

// sweepOnce 兜底扫描游玩会话:内存里超过 sessionIdleTimeout 无消息的连接补记下线,并对库中
// 悬挂的进行中会话(登录已超 24h)强制结束,避免管理后台永远显示「在线中」。
func (p *Pipeline) sweepOnce(now time.Time) {
	for id, cs := range p.conns {
		if cs.sessionOpen && !cs.last.IsZero() && now.Sub(cs.last) > sessionIdleTimeout {
			if err := p.st.EndPlaySession(id, now.Unix()); err != nil {
				log.Printf("EndPlaySession 失败: %v", err)
			}
			cs.sessionOpen = false
		}
	}
	nowTS := now.Unix()
	if err := p.st.ForceEndStaleSessions(nowTS-24*3600, nowTS); err != nil {
		log.Printf("ForceEndStaleSessions 失败: %v", err)
	}
	// 花种挑战计数兜底清理:活动结束后花种从分组消失,0x0375 不再触发实时删除,
	// 这里按记录的 end_ts 统一清掉过期品种的计数(见 boss.go onBossNpcInfo)。
	if err := p.st.DeleteExpiredFlowerChallenges(nowTS); err != nil {
		log.Printf("DeleteExpiredFlowerChallenges 失败: %v", err)
	}
}

func (p *Pipeline) handle(m capture.Message) {
	// 登录回包:解析 user_id → 账号并登记 connID 映射(必须在下面归属 acc 之前)。
	if m.Direction == gcp.S2C && m.Opcode == pet.OpLoginRsp {
		p.registerLogin(m)
	}
	acc := p.connAccount[m.Session]

	// 黑白名单拦截:黑名单账号、或白名单非空且不在白名单内的账号,一律丢弃(不广播、不统计、
	// 不更新在线状态)。规则在管理面板维护,store 内存缓存实时生效。
	if acc != "" && !p.allowed(acc) {
		return
	}

	// debug 页面:广播所有应用层消息,按来源账号归属(登录前无法归属的连接消息 acc="" 作全局)。
	// 订阅端据此只推当前账号的调试流;账号也放进 data 供页面列展示。
	p.srv.Hub().Broadcast("debug", acc, map[string]any{
		"time":    m.Time.Unix(),
		"dir":     m.Direction.String(),
		"opcode":  fmt.Sprintf("0x%04x", m.Opcode),
		"name":    p.srv.OpcodeName(m.Opcode),
		"account": acc,
		"hex":     hex.EncodeToString(m.AppBody), // 调试页点开行时按需解析(见 api_debug.go)
	})
	if acc == "" {
		return // 尚未见到该连接的登录(无法归属 user_id),丢弃
	}
	// 账号在线状态:把最近活跃时刻上报 server(供 /api/accounts 标注在线)。用消息自身
	// 时间戳——实时抓包即当下;离线回放按文件时刻,历史账号如实显示离线。按秒去重再刷锁。
	if now := m.Time.Unix(); now > p.acct(acc).lastSeen {
		p.acct(acc).lastSeen = now
		p.srv.TouchAccount(acc, now)
	}

	// 游玩会话跟踪(管理后台「游玩记录」):连接一旦有可归属消息且尚无进行中会话就开一条,
	// 并记录最后活跃时刻(墙钟)。登录后首条消息自动开;连接断开/长时间无流量(兜底)结束会话
	// 后,该连接再有消息(挂后台回前台、断线重连、重启预热续接)会重新开一条,游玩时间不丢。
	cs := p.conn(m.Session)
	cs.last = time.Now()
	if !cs.sessionOpen {
		cs.sessionOpen = true
		if err := p.st.StartPlaySession(m.Session, acc, time.Now().Unix()); err != nil {
			log.Printf("StartPlaySession 失败: %v", err)
		}
	}

	// 去抖中的层变化需要「过一会儿再看一眼」才能采纳,而玩家可能站着不动、迟迟没有下一个移动包。
	// 故借该连接的任意一条消息(心跳等,实测约 1.6s 一条)把去抖推进到底。
	if cs := p.conns[m.Session]; cs != nil && cs.layer != nil && !cs.layer.since.IsZero() {
		p.settleLayer(m.Session, acc, m.Time)
	}

	// 精灵蛋与宠物、场景都相关(破壳回包同时带新宠物,收蛋通知也走奖励通道),
	// 故不参与「消费即返回」的分发,单独过一遍。
	p.handleEgg(m, acc)

	if p.handleScene(m, acc) {
		// 0x132c 战斗结束通知同时是「场景」与「宠物」两路消息:场景侧清野怪标记
		// (onBattleFinish),宠物侧解析内嵌 goods_reward 的新宠物入库/清花种详情
		// (applyNewPet,实测花种捕捉 catch_way=4 与传说精灵 catch_way=5 都经此下发)。
		// handleScene 命中即返回,故这里对 0x132c 放行到 handlePet。
		if m.Direction == gcp.S2C && m.Opcode == pet.OpBattleFinishNotify {
			p.handlePet(m, acc)
		}
		return
	}
	p.handlePet(m, acc)
}

// allowed 判断账号是否允许处理:黑名单优先拒绝;白名单非空时仅白名单内账号放行;无规则时
// 只要白名单非空就拒绝(白名单为开关,不为空即只统计白名单账号),否则放行。
func (p *Pipeline) allowed(acc string) bool {
	switch p.st.RuleMode(acc) {
	case "black":
		return false
	case "white":
		return true
	}
	return !p.st.WhiteListNonEmpty()
}

// registerLogin 从登录回包解析 user_id/昵称,登记连接归属并落盘。
func (p *Pipeline) registerLogin(m capture.Message) {
	id, name, ok := pet.ParseLoginAccount(m.AppBody)
	if !ok {
		return
	}
	acc := "UID:" + strconv.FormatUint(id, 10)
	nick := name
	if nick == "" {
		nick = "?"
	}
	if p.connAccount[m.Session] != acc { // 同一登录会重复下发,仅首次记日志并落盘映射
		log.Printf("用户 %s (%s) 登录成功 [%s]", acc, nick, m.Session)
		p.st.SaveSessionAccount(m.Session, acc)
		// 同一连接切换账号(退出登录换号):结束旧账号的进行中会话,并把 sessionOpen 置 false,
		// 本消息后续流程(handle 的游玩会话跟踪)会为新账号重新开一条。
		if err := p.st.EndPlaySession(m.Session, time.Now().Unix()); err != nil {
			log.Printf("EndPlaySession 失败: %v", err)
		}
		if cs := p.conns[m.Session]; cs != nil {
			cs.sessionOpen = false
		}
	}
	p.connAccount[m.Session] = acc
	if name == "" {
		name = acc
	}
	p.st.UpsertAccount(acc, name)
	if coins, ok := pet.ParseLoginCoins(m.AppBody); ok {
		log.Printf("登录回包解析洛克贝 [%s] coins=%d", acc, coins)
		if err := p.st.SetAccountCoins(acc, coins); err != nil {
			log.Printf("SetAccountCoins 失败: %v", err)
		}
	}
}

// PetTotal 返回全部账号的宠物数合计(离线回放结束时的汇总日志用)。
func (p *Pipeline) PetTotal() int {
	accs, _ := p.st.ListAccounts()
	n := 0
	for _, a := range accs {
		n += a.PetCount
	}
	return n
}

// AccountCount 返回已知账号数。
func (p *Pipeline) AccountCount() int {
	accs, _ := p.st.ListAccounts()
	return len(accs)
}

// catchWayName 由 catch_way 推断获得方式(实测:1=捕捉、3=孵蛋;其余未知归"获得")。
// 例外:共同捕捉转赠的宠物 catch_way 仍是 1,但对接收方应记「赠送获得」而非「捕捉」——
// 据 together_catch_info 区分(related_uin=接收方、catched_uin=捕捉方):本账号是接收方且非捕捉方即为受赠。
func catchWayName(pd *pb.PetData, acc string) string {
	if tci := pd.GetTogetherCatchInfo(); tci != nil {
		if uid, ok := uidFromAcc(acc); ok &&
			tci.GetRelatedUin() == uid && tci.GetCatchedUin() != 0 && tci.GetCatchedUin() != uid {
			return "赠送获得"
		}
	}
	switch pd.GetCatchWay() {
	case 1, 4, 5:
		return "捕捉" // 1=普通/战斗外捕捉, 4=花种(稀兽)战斗内捕捉, 5=传说精灵战后(耗体力)捕捉
	case 3:
		return "孵蛋"
	default:
		return "获得"
	}
}

// uidFromAcc 从账号标识("UID:<user_id>")取回 user_id。
func uidFromAcc(acc string) (uint32, bool) {
	s, ok := strings.CutPrefix(acc, "UID:")
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(v), true
}
