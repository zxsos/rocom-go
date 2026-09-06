package store

import (
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// 孵化倍率**实测**(玩家主动测速,见 docs/data.md 3.6 与 pipeline/eggs.go 的调用点)。
//
// 为什么要玩家手动测:倍率由「活动倍率(固定时间表)+ 在线行为加成(移动/挂风场/
// 孵化宝典)」两部分构成,前者能按时刻算死(1x / 5x),后者**测不准也没法定** ——
// 六份 pcap 实测移动时 16.90~27.32(中位 21.96,标准差 3.38),且同速度样本噪声
// (极差 8.10)大于速度效应(4.12),与「由速度决定」矛盾(见 docs/pcap-2026-09-05-
// hatch-rate.md §4.2)。若后端自动按移动状态乘一个固定增益,就是拿一个没有可信定值的
// 数去外推 —— 会在一半样本上虚报「快好了」。
//
// 故改成:**默认只按活动倍率(1x/5x)外推(保守、不虚报),玩家想要更准的 ETA 就自己
// 测一次**。测法是玩家在孵蛋页点「开始测速」,然后去游戏里打开**两次**孵蛋器面板
// (两次都触发 0x0312 下发进度),后端取两次的差分 Δv/Δt。测出多少就是多少 ——
// 它天然包含此刻的全部加成(活动 + 移动 + 风场),不需要分别建模。
//
// 为什么必须玩家配合点两次:进度只在开孵蛋器(0x0312)或开背包(0x1344)时下发,
// **没有被动推送**。后端要差分就得等玩家自己触发两次,别无他法。
//
// 结果**落库**而非纯内存:页面刷新、服务重启都该保留(测一次不容易)。
// 清除时机只有一个 —— 玩家**下线**(pipeline.settleSessions),因为倍率依赖他此刻的
// 状态,换个人/下次上线都不作数(见 ClearStaleHatchSpeed)。

// HatchSpeedState 是测速的状态机。
type HatchSpeedState string

const (
	HatchSpeedIdle  HatchSpeedState = "idle"  // 未测速(默认按活动倍率外推)
	HatchSpeedArmed HatchSpeedState = "armed" // 已点「开始测速」,等第一次采样
	HatchSpeedFirst HatchSpeedState = "first" // 已采第一次,等第二次
	HatchSpeedDone  HatchSpeedState = "done"  // 已完成,rate 有效
)

// hatchSpeedMinGap 是两次采样的最小间隔(秒)。
//
// 服务器约按 5 秒步长结算进度,故 <10s 的差分区间给出的是**假值**:实测 1s 区间
// 会算出 5.00(未跨步长,像"没加速")或 67.50(跨了步长,像"异常跳变"),
// 见 docs/pcap-2026-09-05-hatch-rate.md §5。取 10s 为下限,短于它的第二次采样
// 不当作第二次,而是**重置起点**(把这次当新的第一次)—— 玩家多开几次总能测出来,
// 比卡在"间隔太短"上强。
//
// 间隔越长越准(§4.3:dt=20s 时误差 ±1.2,dt=50s 时 ±0.5),故前端会提示尽量多隔一会。
const hatchSpeedMinGap = 10

// HatchSpeedTest 是给前端的测速状态快照。
type HatchSpeedTest struct {
	State      HatchSpeedState `json:"state"`
	Rate       float64         `json:"rate,omitempty"`       // 测出的倍率(done 时有效)
	T1         int64           `json:"t1,omitempty"`         // 第一次采样时刻(前端据此显示已等多久)
	MeasuredAt int64           `json:"measuredAt,omitempty"` // 完成时刻
}

// hatchSpeedRow 是库里的一行。
type hatchSpeedRow struct {
	armedAt    int64
	t1         int64
	s1         string // JSON map[gid]hatchedSecs
	t2         int64
	s2         string
	rate       float64
	measuredAt int64
}

// GetHatchSpeed 读当前测速状态;从未测过返回 idle。
func (sc *Scoped) GetHatchSpeed() (HatchSpeedTest, error) {
	var r hatchSpeedRow
	err := sc.rdb.QueryRow(
		`SELECT armed_at, t1, s1, t2, s2, rate, measured_at FROM hatch_speed WHERE account=?`,
		sc.account).Scan(&r.armedAt, &r.t1, &r.s1, &r.t2, &r.s2, &r.rate, &r.measuredAt)
	if err == sql.ErrNoRows {
		return HatchSpeedTest{State: HatchSpeedIdle}, nil
	}
	if err != nil {
		return HatchSpeedTest{State: HatchSpeedIdle}, err
	}
	return r.view(), nil
}

// view 把一行转成给前端的快照。
func (r hatchSpeedRow) view() HatchSpeedTest {
	switch {
	case r.armedAt == 0:
		return HatchSpeedTest{State: HatchSpeedIdle}
	case r.t1 == 0:
		return HatchSpeedTest{State: HatchSpeedArmed}
	case r.rate > 0 && r.measuredAt > 0:
		return HatchSpeedTest{State: HatchSpeedDone, Rate: r.rate, T1: r.t1, MeasuredAt: r.measuredAt}
	default:
		return HatchSpeedTest{State: HatchSpeedFirst, T1: r.t1}
	}
}

// ArmHatchSpeed 开始(或重新开始)一次测速:清空上次结果,等第一次采样。
func (sc *Scoped) ArmHatchSpeed(ts int64) error {
	_, err := sc.db.Exec(`
INSERT INTO hatch_speed(account, armed_at, t1, s1, t2, s2, rate, measured_at, updated_at)
VALUES(?,?,0,'',0,'',0,0,?)
ON CONFLICT(account) DO UPDATE SET
  armed_at=excluded.armed_at, t1=0, s1='', t2=0, s2='', rate=0, measured_at=0,
  updated_at=excluded.updated_at`, sc.account, ts, ts)
	return err
}

// CancelHatchSpeed 取消测速(玩家主动放弃),回到 idle。
func (sc *Scoped) CancelHatchSpeed() error {
	_, err := sc.db.Exec(`
UPDATE hatch_speed SET armed_at=0, t1=0, s1='', t2=0, s2='', rate=0, measured_at=0, updated_at=?
WHERE account=?`, time.Now().Unix(), sc.account)
	return err
}

// RecordHatchSample 收一次孵化进度采样(0x0312 下发时由 pipeline 调用),返回状态是否变化。
//
// secs 是本次下发里**在孵蛋**的 gid → hatchedSecs。同一批几颗蛋共享同一次服务器结算
// (实测三颗不同 maxSecs 的蛋同秒各 +10s),故逐颗算出的差分先取中位数再落定 ——
// 一颗蛋中途被取出/放入造成的跳变不会带歪结果。
//
// 只在已 armed 时才有动作;未测速时调用它是空操作(进度采样本身照常入库)。
func (sc *Scoped) RecordHatchSample(ts int64, secs map[uint32]int32) (bool, error) {
	if len(secs) == 0 {
		return false, nil
	}
	// 读-改-写必须原子:采样由抓包管线串行调用,但状态同时被 HTTP 读,故走事务。
	tx, err := sc.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var r hatchSpeedRow
	err = tx.QueryRow(
		`SELECT armed_at, t1, s1, t2, s2, rate, measured_at FROM hatch_speed WHERE account=?`,
		sc.account).Scan(&r.armedAt, &r.t1, &r.s1, &r.t2, &r.s2, &r.rate, &r.measuredAt)
	if err == sql.ErrNoRows {
		return false, nil // 未 armed:不建行,避免给每个人留一条空记录
	}
	if err != nil {
		return false, err
	}
	if r.armedAt == 0 {
		return false, nil // 未测速
	}

	blob, err := json.Marshal(secs)
	if err != nil {
		return false, err
	}
	// 已完成:不再改动,免得玩家开着面板时新采样把结果冲掉
	if r.rate > 0 && r.measuredAt > 0 {
		return false, nil
	}

	// 写起点:第一次采样、间隔不足要重置、旧样本坏了要重置,三种情况动作一样
	setFirst := func() (bool, error) {
		if _, err := tx.Exec(
			`UPDATE hatch_speed SET t1=?, s1=?, updated_at=? WHERE account=?`,
			ts, string(blob), ts, sc.account); err != nil {
			return false, err
		}
		return true, tx.Commit()
	}

	if r.t1 == 0 { // 第一次
		return setFirst()
	}

	// 第二次:间隔不够就重置起点(见 hatchSpeedMinGap)
	if ts-r.t1 < hatchSpeedMinGap {
		return setFirst()
	}
	var prev map[uint32]int32
	if err := json.Unmarshal([]byte(r.s1), &prev); err != nil {
		// 旧样本坏了:拿这次当新的第一次,不必让整次测速失败
		return setFirst()
	}
	rate, ok := medianHatchRate(prev, secs, ts-r.t1)
	if !ok {
		return false, nil // 两批蛋对不上(中途换过蛋),保持等第二次
	}
	_, err = tx.Exec(
		`UPDATE hatch_speed SET t2=?, s2=?, rate=?, measured_at=?, updated_at=? WHERE account=?`,
		ts, string(blob), rate, ts, ts, sc.account)
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// medianHatchRate 对两次采样里**同一批蛋**的差分取中位数。
// 只要有一颗粒子的进度倒退(换蛋/取出重放),那颗就跳过;全部对不上返回 false。
func medianHatchRate(a, b map[uint32]int32, dt int64) (float64, bool) {
	if dt <= 0 {
		return 0, false
	}
	rs := make([]float64, 0, len(a))
	for gid, v1 := range a {
		v2, ok := b[gid]
		if !ok {
			continue
		}
		dv := int64(v2) - int64(v1)
		if dv < 0 {
			continue
		}
		rs = append(rs, float64(dv)/float64(dt))
	}
	if len(rs) == 0 {
		return 0, false
	}
	sort.Float64s(rs)
	n := len(rs)
	if n%2 == 1 {
		return rs[n/2], true
	}
	return (rs[n/2-1] + rs[n/2]) / 2, true
}

// ClearStaleHatchSpeed 清除「已下线」账号的测速结果:active 之外的账号一律清。
//
// 时机与游玩会话的下线判定一致(pipeline.settleSessions 调 EndStalePlaySessions
// 的同一处):倍率依赖玩家**此刻**的状态(在不在动、有没有挂风场),下线后他再上线
// 时状态已变,旧结果不作数 —— 留着会让新会话的 ETA 按上一轮的倍率外推,那是虚报。
//
// active 为空表示当前无人在线,此时清除全部(服务重启后内存连接表已空,库里的
// 结果无人认领,同样不该留)。
func (s *Store) ClearStaleHatchSpeed(active []string) error {
	q := `UPDATE hatch_speed SET armed_at=0, t1=0, s1='', t2=0, s2='', rate=0, measured_at=0,
  updated_at=?`
	args := []any{time.Now().Unix()}
	if len(active) > 0 {
		q += ` WHERE account NOT IN (?` + strings.Repeat(",?", len(active)-1) + `)`
		for _, a := range active {
			args = append(args, a)
		}
	}
	_, err := s.db.Exec(q, args...)
	return err
}
