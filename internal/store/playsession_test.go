package store

import (
	"sort"
	"strconv"
	"testing"
	"time"
)

// TestStartPlaySessionPerAccount 校验会话是**账号级**而非连接级的。
//
// 背景(用户实测的游玩记录):一个玩家会同时保持多条 TCP 连接(2~3 条),重连还换端口。
// 按连接记一次在线会被拆成多条时间重叠的会话 —— 管理后台就是「好几条并行的在线中」
// 加一堆几秒的碎片,「今日游玩时长」还会把并行的几条累加,直接翻倍。
// 这条测试锁住:同一账号无论多少条连接、开多少次,进行中会话恒定只有一条。
func TestStartPlaySessionPerAccount(t *testing.T) {
	st := newTestStore(t)
	now := time.Now().Unix()

	// 模拟三条连接几乎同时到达(重连/多连接)
	for _, c := range []string{"conn-A", "conn-B", "conn-C"} {
		if err := st.StartPlaySession(c, testAcc, now); err != nil {
			t.Fatalf("开启会话 %s: %v", c, err)
		}
	}
	if n := st.countOpen(testAcc); n != 1 {
		t.Fatalf("同一账号 3 条连接后, 进行中会话 = %d, 期望 1", n)
	}

	// 另一账号不受影响(不能误把别人的会话挡掉)
	if err := st.StartPlaySession("conn-D", "UID:2", now); err != nil {
		t.Fatalf("开启另一账号会话: %v", err)
	}
	if n := st.countOpen("UID:2"); n != 1 {
		t.Fatalf("另一账号进行中会话 = %d, 期望 1", n)
	}

	// 结束其中一条连接所在的会话 → 按账号结束,该账号的会话关闭,另一账号不受影响
	if err := st.EndAccountSessions(testAcc, now+60); err != nil {
		t.Fatalf("结束账号会话: %v", err)
	}
	if n := st.countOpen(testAcc); n != 0 {
		t.Errorf("%s 结束后仍有 %d 条进行中会话", testAcc, n)
	}
	if n := st.countOpen("UID:2"); n != 1 {
		t.Errorf("UID:2 被误伤, 进行中会话 = %d, 期望 1", n)
	}

	// 隔了**超过**合并窗口再上线 → 确实是新会话(不复用上一条)。
	// 间隔必须 > sessionMergeWindow:否则会被当作断线重连而续上旧会话
	// (那是有意的合并行为,由 TestStartPlaySessionMergesReconnect 单独覆盖)。
	if err := st.StartPlaySession("conn-E", testAcc, now+int64(2*sessionMergeWindow/time.Second)); err != nil {
		t.Fatalf("重新开启会话: %v", err)
	}
	if n := st.countOpen(testAcc); n != 1 {
		t.Fatalf("重新上线后进行中会话 = %d, 期望 1", n)
	}
	got, err := st.ListPlaySessions(testAcc, 10, 0)
	if err != nil {
		t.Fatalf("列出: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("超出合并窗口应得到 2 条独立会话, 实得 %d: %+v", len(got), got)
	}
}

// TestEndStalePlaySessions 校验「账号已不活跃 → 记下线」的判定。
// 这是离线判定的核心:只要该账号还有**任意一条**连接活跃,就不能判下线
// (关掉其中一条不等于下线,正是碎片会话的来源);反之必须及时记下线,
// 包括服务重启后内存连接表清空、库里无人认领的悬挂会话。
func TestEndStalePlaySessions(t *testing.T) {
	st := newTestStore(t)
	now := time.Now().Unix()

	for _, acc := range []string{testAcc, "UID:2"} {
		if err := st.StartPlaySession("c-"+acc, acc, now); err != nil {
			t.Fatalf("开启 %s: %v", acc, err)
		}
	}

	// 两个账号都活跃 → 一个都不结束
	if err := st.EndStalePlaySessions([]string{testAcc, "UID:2"}, now+10); err != nil {
		t.Fatalf("扫活跃账号: %v", err)
	}
	if n := st.countOpen(testAcc); n != 1 {
		t.Errorf("活跃账号被误判下线: %s 进行中 = %d", testAcc, n)
	}

	// 只有 UID:2 还活跃 → testAcc 记下线,下线时刻与时长都要对
	if err := st.EndStalePlaySessions([]string{"UID:2"}, now+100); err != nil {
		t.Fatalf("扫部分活跃: %v", err)
	}
	if n := st.countOpen(testAcc); n != 0 {
		t.Fatalf("不活跃账号未记下线: %s 进行中 = %d", testAcc, n)
	}
	got, err := st.ListPlaySessions(testAcc, 10, 0)
	if err != nil {
		t.Fatalf("列出: %v", err)
	}
	if len(got) != 1 || got[0].LogoutTime == nil || *got[0].LogoutTime != now+100 {
		t.Errorf("下线时刻不对: %+v", got)
	} else if got[0].Duration != 100 {
		t.Errorf("时长 = %d, 期望 100", got[0].Duration)
	}
	if n := st.countOpen("UID:2"); n != 1 {
		t.Errorf("活跃账号被误伤: UID:2 进行中 = %d", n)
	}

	// 空活跃集合(服务重启后内存连接表为空)→ 全部记下线,不留悬挂
	if err := st.EndStalePlaySessions(nil, now+200); err != nil {
		t.Fatalf("扫空活跃集合: %v", err)
	}
	if n := st.countOpen("UID:2"); n != 0 {
		t.Errorf("重启后悬挂会话未清理: UID:2 进行中 = %d", n)
	}
}

// countOpen 数某账号进行中(logout_time IS NULL)的会话条数。
func (s *Store) countOpen(account string) int {
	var n int
	if err := s.rdb.QueryRow(
		`SELECT COUNT(*) FROM play_sessions WHERE account=? AND logout_time IS NULL`,
		account).Scan(&n); err != nil {
		return -1
	}
	return n
}

// TestListPlaySessionsWithNullDuration 复现「管理页面登录统计」的扫错:进行中会话的
// duration 列为 NULL(下线时才写入),直接扫入 int64 会报
// "converting NULL to int64 is unsupported"。修复后应正常返回,Online=true、Duration=0。
func TestListPlaySessionsWithNullDuration(t *testing.T) {
	st := newTestStore(t)
	now := time.Now().Unix()

	// 会话是账号级的:同一账号再开一条只会被幂等挡住,故这里用两个账号造出
	// 「一进行中 + 一已结束」两种形态(见 TestStartPlaySessionPerAccount)。
	if err := st.StartPlaySession("conn-1", testAcc, now); err != nil {
		t.Fatalf("开启进行中会话: %v", err)
	}
	if err := st.StartPlaySession("conn-2", "UID:2", now); err != nil {
		t.Fatalf("开启已结束会话: %v", err)
	}
	if err := st.EndAccountSessions("UID:2", now+120); err != nil {
		t.Fatalf("结束会话: %v", err)
	}

	sessions, err := st.ListPlaySessions("", 10, 0)
	if err != nil {
		t.Fatalf("列出会话: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("会话数 = %d, 期望 2", len(sessions))
	}
	// 已结束:duration 已写入;进行中:Online=true、Duration=0(NULL 兜底)。
	var ended, ongoing *PlaySession
	for i := range sessions {
		if sessions[i].Online {
			ongoing = &sessions[i]
		} else {
			ended = &sessions[i]
		}
	}
	if ended == nil || ongoing == nil {
		t.Fatalf("应各有一进行中与一已结束会话, 实得 %+v", sessions)
	}
	if ended.Online {
		t.Errorf("已结束会话 Online 应为 false")
	}
	if ended.Duration != 120 {
		t.Errorf("已结束会话 Duration = %d, 期望 120", ended.Duration)
	}
	if !ongoing.Online {
		t.Errorf("进行中会话 Online 应为 true")
	}
	if ongoing.Duration != 0 {
		t.Errorf("进行中会话 Duration = %d, 期望 0(NULL 兜底)", ongoing.Duration)
	}

	// 汇总侧同样不能炸(内部对 duration 已做 CASE/COALESCE)。
	if _, err := st.PlaySessionSummary(); err != nil {
		t.Fatalf("汇总: %v", err)
	}
}

// TestListPlaySessionsPaging 校验游玩记录分页(管理后台「游玩记录」表格):
// 逐页翻过去必须**不重不漏**地覆盖全部会话,且按登录时间倒序 —— 排序不稳会让翻页
// 出现重复行或漏行,是最典型的分页缺陷,故这里按整段序列比对而非只看条数。
// 同时校验 CountPlaySessions 与 ListPlaySessions 的筛选口径一致:
// 两者不一致会导致末页翻空(总数偏大)或有记录永远翻不到(总数偏小)。
func TestListPlaySessionsPaging(t *testing.T) {
	st := newTestStore(t)
	base := time.Now().Add(-time.Hour).Unix()

	// 两个账号各 5 条,登录时间**不重叠**(第二个账号整体后移一小时):
	// 全局严格有序,倒序后每条的期望位置唯一 —— 若用交错/相同的时间戳,同刻内的先后
	// 由 SQLite 决定,期望序列就写不准,测试会变成噪音而非护栏。
	//
	// 相邻两条的**间隔必须 > sessionMergeWindow**:本用例验的是分页,若间隔落在
	// 合并窗口内,这些会话会被 StartPlaySession 当作断线重连而续上,条数随之减少
	// (改这个功能时这条测试先红,正是合并生效的信号)。每条在线 30s、间隔 10 分钟。
	//
	// 每条必须「上线→下线」成对造:会话是账号级的,同一账号连续 StartPlaySession
	// 会被幂等挡住(见 TestStartPlaySessionPerAccount),只造得出一条。
	const per = 5
	const gap = int64(10 * time.Minute / time.Second) // 远大于 sessionMergeWindow
	type row struct {
		acc string
		ts  int64
	}
	var all []row
	for k, acc := range []string{testAcc, "UID:2"} {
		for i := 0; i < per; i++ {
			ts := base + int64(k)*3600 + int64(i)*gap
			if err := st.StartPlaySession("c-"+acc+"-"+strconv.Itoa(i), acc, ts); err != nil {
				t.Fatalf("开启会话 %s#%d: %v", acc, i, err)
			}
			if err := st.EndAccountSessions(acc, ts+30); err != nil {
				t.Fatalf("结束会话 %s#%d: %v", acc, i, err)
			}
			all = append(all, row{acc, ts})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ts > all[j].ts })
	wantAll := make([]string, 0, len(all)) // 期望序列:"账号#登录时间"
	for _, r := range all {
		wantAll = append(wantAll, r.acc+"#"+strconv.FormatInt(r.ts, 10))
	}

	total, err := st.CountPlaySessions("")
	if err != nil {
		t.Fatalf("总数: %v", err)
	}
	if total != per*2 {
		t.Fatalf("总数 = %d, 期望 %d", total, per*2)
	}

	// 每页 3 条翻完全部:3+3+3+1
	const size = 3
	var got []string
	for offset := 0; ; offset += size {
		page, err := st.ListPlaySessions("", size, offset)
		if err != nil {
			t.Fatalf("offset=%d: %v", offset, err)
		}
		if len(page) == 0 {
			break
		}
		if len(page) > size {
			t.Fatalf("offset=%d 返回 %d 条, 超过每页 %d", offset, len(page), size)
		}
		for _, ps := range page {
			got = append(got, ps.Account+"#"+strconv.FormatInt(ps.LoginTime, 10))
		}
		if len(got) > total { // 兜住末页不收敛的情形,免得死循环
			t.Fatalf("翻页超出总数: 已取 %d > 总数 %d", len(got), total)
		}
	}
	if len(got) != len(wantAll) {
		t.Fatalf("翻页共取到 %d 条, 期望 %d", len(got), len(wantAll))
	}
	for i := range wantAll {
		if got[i] != wantAll[i] {
			t.Errorf("第 %d 条 = %s, 期望 %s", i, got[i], wantAll[i])
		}
	}

	// 筛选口径:按账号过滤后,总数与逐页翻出来的条数必须一致。
	n1, err := st.CountPlaySessions(testAcc)
	if err != nil {
		t.Fatalf("按账号总数: %v", err)
	}
	if n1 != per {
		t.Errorf("账号 %s 总数 = %d, 期望 %d", testAcc, n1, per)
	}
	got1, err := st.ListPlaySessions(testAcc, 100, 0)
	if err != nil {
		t.Fatalf("按账号列出: %v", err)
	}
	if len(got1) != n1 {
		t.Errorf("按账号列出 %d 条, 与总数 %d 不一致", len(got1), n1)
	}
	for _, ps := range got1 {
		if ps.Account != testAcc {
			t.Errorf("筛选失效: 混入账号 %s", ps.Account)
		}
	}

	// ID 必须非空且在同一结果集内唯一:前端列表的 React key 只用它
	// (见 PlaySession.ID 的警示注释)。同一账号同一秒内可有多条会话,若退回用
	// account+loginTime 拼 key 会撞车 —— 这里守住 key 的来源。
	seen := map[int64]bool{}
	for _, ps := range got1 {
		if ps.ID == 0 {
			t.Errorf("会话 %s#%d 的 ID 为 0, 前端 key 会失效", ps.Account, ps.LoginTime)
		}
		if seen[ps.ID] {
			t.Errorf("ID %d 重复, 不能用作 React key", ps.ID)
		}
		seen[ps.ID] = true
	}

	// 越界 offset:不该报错,应返回空(前端据此收尾)。
	empty, err := st.ListPlaySessions("", size, total+10)
	if err != nil {
		t.Fatalf("越界 offset: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("越界 offset 返回 %d 条, 期望 0", len(empty))
	}
}

// TestStartPlaySessionMergesReconnect 校验断线重连的合并(本次需求的主体)。
//
// 背景(用户实测的游玩记录):玩家短暂断线(切网络/客户端重启/手机切后台)后几秒到
// 几分钟内重连,而 settleSessions 在「账号再无活跃连接」时立刻记下线 —— 同一次在线
// 被拆成两条,管理后台里就是一堆几秒/几十秒的碎片(3 秒、1 分 12 秒、1 分 39 秒…),
// 与真正的「下线又上线」无从区分。
//
// 合并后:记录只剩一条,login 是最早那次上线,duration 按「最终下线 − 首次登录」算。
func TestStartPlaySessionMergesReconnect(t *testing.T) {
	st := newTestStore(t)
	defer st.Close()
	base := time.Now().Add(-time.Hour).Unix()
	const win = int64(sessionMergeWindow / time.Second)

	// 第 1 次在线:0 ~ 600s
	if err := st.StartPlaySession("c1", testAcc, base); err != nil {
		t.Fatalf("首次上线: %v", err)
	}
	if err := st.EndAccountSessions(testAcc, base+600); err != nil {
		t.Fatalf("首次下线: %v", err)
	}
	// 断线 60s 后重连(远小于 5 分钟窗口),再玩 300s
	if err := st.StartPlaySession("c2", testAcc, base+660); err != nil {
		t.Fatalf("重连: %v", err)
	}
	if n := st.countOpen(testAcc); n != 1 {
		t.Fatalf("重连后进行中会话 = %d, 期望 1(续上而非新建)", n)
	}
	if err := st.EndAccountSessions(testAcc, base+960); err != nil {
		t.Fatalf("二次下线: %v", err)
	}

	got, err := st.ListPlaySessions(testAcc, 10, 0)
	if err != nil {
		t.Fatalf("列出: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("重连应合并为 1 条, 实得 %d: %+v", len(got), got)
	}
	if got[0].LoginTime != base {
		t.Errorf("合并后 login = %d, 期望保持首次上线 %d", got[0].LoginTime, base)
	}
	if got[0].LogoutTime == nil || *got[0].LogoutTime != base+960 {
		t.Errorf("合并后 logout = %v, 期望 %d", got[0].LogoutTime, base+960)
	}
	// 时长 = 最终下线 − 首次登录,即含中间那段断线(见 sessionMergeWindow 注释)
	if got[0].Duration != 960 {
		t.Errorf("合并后 duration = %d, 期望 960", got[0].Duration)
	}

	// 窗口边界:刚好等于窗口 → 合并;超过 1 秒 → 不合并
	for _, tc := range []struct {
		name  string
		gap   int64
		wantN int
	}{
		{"间隔 = 窗口(边界内)", win, 1},
		{"间隔 = 窗口 + 1s(边界外)", win + 1, 2},
	} {
		st2 := newTestStore(t)
		b := time.Now().Add(-2 * time.Hour).Unix()
		if err := st2.StartPlaySession("a", "UID:9", b); err != nil {
			t.Fatalf("%s: 上线: %v", tc.name, err)
		}
		if err := st2.EndAccountSessions("UID:9", b+10); err != nil {
			t.Fatalf("%s: 下线: %v", tc.name, err)
		}
		if err := st2.StartPlaySession("b", "UID:9", b+10+tc.gap); err != nil {
			t.Fatalf("%s: 重连: %v", tc.name, err)
		}
		n := st2.countOpen("UID:9")
		// 合并时:只有 1 条(续上的);不合并时:1 条进行中 + 1 条已结束
		all, err := st2.ListPlaySessions("UID:9", 10, 0)
		if err != nil {
			t.Fatalf("%s: 列出: %v", tc.name, err)
		}
		if len(all) != tc.wantN {
			t.Errorf("%s: 会话数 = %d, 期望 %d", tc.name, len(all), tc.wantN)
		}
		if n != 1 {
			t.Errorf("%s: 进行中 = %d, 期望 1", tc.name, n)
		}
		st2.Close()
	}
}

// TestStartPlaySessionMergeGuards 守住合并的两条边界,防止"好心办坏事":
//   - 时钟回拨/乱序回放(ts 早于上次下线)不得把旧会话重新打开;
//   - 别的账号的会话不得被误续。
func TestStartPlaySessionMergeGuards(t *testing.T) {
	st := newTestStore(t)
	defer st.Close()
	base := time.Now().Add(-time.Hour).Unix()

	// 账号 A 先玩一段并下线
	if err := st.StartPlaySession("c1", testAcc, base); err != nil {
		t.Fatalf("上线: %v", err)
	}
	if err := st.EndAccountSessions(testAcc, base+100); err != nil {
		t.Fatalf("下线: %v", err)
	}
	// 时钟回拨:ts 比上次下线还早 50s(负间隔)。负间隔恒 ≤ 窗口,
	// 没有下界保护就会把这条已结束的旧会话重新打开。
	if err := st.StartPlaySession("c2", testAcc, base+50); err != nil {
		t.Fatalf("回拨后上线: %v", err)
	}
	all, err := st.ListPlaySessions(testAcc, 10, 0)
	if err != nil {
		t.Fatalf("列出: %v", err)
	}
	// 期望:旧的保持已结束(1 条) + 新建的 1 条 = 2 条
	if len(all) != 2 {
		t.Fatalf("时钟回拨时应新建而非重开旧会话, 实得 %d 条: %+v", len(all), all)
	}
	var reopened bool
	for _, ps := range all {
		if ps.LoginTime == base && ps.LogoutTime == nil {
			reopened = true
		}
	}
	if reopened {
		t.Errorf("旧会话被重新打开(应被负间隔保护挡住): %+v", all)
	}

	// 跨账号:UID:2 重连不得续上 testAcc 的会话
	if err := st.StartPlaySession("c3", testAcc, base+200); err != nil {
		t.Fatalf("testAcc 再上线: %v", err)
	}
	if err := st.EndAccountSessions(testAcc, base+250); err != nil {
		t.Fatalf("testAcc 下线: %v", err)
	}
	if err := st.StartPlaySession("c4", "UID:2", base+260); err != nil {
		t.Fatalf("UID:2 上线: %v", err)
	}
	got, err := st.ListPlaySessions("UID:2", 10, 0)
	if err != nil {
		t.Fatalf("列出 UID:2: %v", err)
	}
	if len(got) != 1 || got[0].Account != testAcc && got[0].Account != "UID:2" {
		t.Fatalf("UID:2 会话异常: %+v", got)
	}
	for _, ps := range got {
		if ps.Account == "UID:2" && ps.LoginTime != base+260 {
			t.Errorf("UID:2 续上了别人的会话: login=%d, 期望 %d", ps.LoginTime, base+260)
		}
	}
}

// TestMergeRecentPlaySessions 校验历史碎片的批量合并(启动时跑一次,清理存量数据)。
func TestMergeRecentPlaySessions(t *testing.T) {
	st := newTestStore(t)
	defer st.Close()
	base := time.Now().Add(-time.Hour).Unix()
	const win = int64(sessionMergeWindow / time.Second)

	// 造三条「间隔 60s」的历史碎片(直接 INSERT,绕开 StartPlaySession 的写入时合并,
	// 模拟本功能上线前积累的存量数据)
	segs := [][2]int64{
		{base, base + 600},         // 0 ~ 600
		{base + 660, base + 1200},  // 间隔 60s → 合并
		{base + 1260, base + 1800}, // 间隔 60s → 合并
	}
	for i, s := range segs {
		if _, err := st.db.Exec(
			`INSERT INTO play_sessions(conn_id, account, login_time, logout_time, duration) VALUES(?,?,?,?,?)`,
			"old-"+strconv.Itoa(i), testAcc, s[0], s[1], s[1]-s[0]); err != nil {
			t.Fatalf("插入历史会话 %d: %v", i, err)
		}
	}
	// 另一账号的碎片,间隔同样 60s(验证按账号分别合并)
	for i, s := range segs {
		if _, err := st.db.Exec(
			`INSERT INTO play_sessions(conn_id, account, login_time, logout_time, duration) VALUES(?,?,?,?,?)`,
			"o2-"+strconv.Itoa(i), "UID:2", s[0], s[1], s[1]-s[0]); err != nil {
			t.Fatalf("插入 UID:2 历史会话 %d: %v", i, err)
		}
	}

	n, err := st.MergeRecentPlaySessions(sessionMergeWindow)
	if err != nil {
		t.Fatalf("合并: %v", err)
	}
	// 每个账号 3 条 → 1 条,共合并掉 4 条
	if n != 4 {
		t.Errorf("合并掉 %d 条, 期望 4", n)
	}
	for _, acc := range []string{testAcc, "UID:2"} {
		got, err := st.ListPlaySessions(acc, 10, 0)
		if err != nil {
			t.Fatalf("列出 %s: %v", acc, err)
		}
		if len(got) != 1 {
			t.Fatalf("%s 合并后 = %d 条, 期望 1: %+v", acc, len(got), got)
		}
		if got[0].LoginTime != base || got[0].LogoutTime == nil || *got[0].LogoutTime != base+1800 {
			t.Errorf("%s 合并后区间 = %d ~ %v, 期望 %d ~ %d",
				acc, got[0].LoginTime, got[0].LogoutTime, base, base+1800)
		}
		if got[0].Duration != 1800 {
			t.Errorf("%s 合并后 duration = %d, 期望 1800", acc, got[0].Duration)
		}
	}

	// 幂等:再跑一次不应再合并(已无可合并项)
	n2, err := st.MergeRecentPlaySessions(sessionMergeWindow)
	if err != nil {
		t.Fatalf("二次合并: %v", err)
	}
	if n2 != 0 {
		t.Errorf("二次合并掉 %d 条, 期望 0(幂等)", n2)
	}

	// 间隔超过窗口的不得合并
	st2 := newTestStore(t)
	defer st2.Close()
	b2 := time.Now().Add(-2 * time.Hour).Unix()
	for i, s := range [][2]int64{{b2, b2 + 10}, {b2 + 10 + win + 1, b2 + 10 + win + 20}} {
		if _, err := st2.db.Exec(
			`INSERT INTO play_sessions(conn_id, account, login_time, logout_time, duration) VALUES(?,?,?,?,?)`,
			"x-"+strconv.Itoa(i), "UID:3", s[0], s[1], s[1]-s[0]); err != nil {
			t.Fatalf("插入: %v", err)
		}
	}
	if n, err := st2.MergeRecentPlaySessions(sessionMergeWindow); err != nil {
		t.Fatalf("合并: %v", err)
	} else if n != 0 {
		t.Errorf("超过窗口仍合并了 %d 条, 期望 0", n)
	}
}

// TestMergeRecentPlaySessionsSkipsOverlapping 时间**重叠**的两段不得被合并。
//
// 守住 MergeRecentPlaySessions 里的 `cur.login >= keep.logout`:重叠时
// `login − logout` 为负,而负间隔**永远 ≤ 窗口** —— 少了这个下界判定,重叠(并行)
// 的两段会被当成「间隔极短的重连」吞掉,合并后 duration 按「下线 − 登录」算会凭空变长。
// 当前设计下同一账号不该出现重叠的已结束会话(会话是账号级的、StartPlaySession 幂等),
// 这条属于防御;但它极易被当成冗余条件删掉,故钉住(变异测试证实:删掉它现有用例全绿)。
func TestMergeRecentPlaySessionsSkipsOverlapping(t *testing.T) {
	st := newTestStore(t)
	defer st.Close()
	base := time.Now().Add(-time.Hour).Unix()

	// 长的一段包住短的一段(按 login_time 排序后:长段在前,短段的 login 落在长段内)
	overlaps := [][2]int64{
		{base, base + 1000},
		{base + 100, base + 200},
	}
	for i, s := range overlaps {
		if _, err := st.db.Exec(
			`INSERT INTO play_sessions(conn_id, account, login_time, logout_time, duration) VALUES(?,?,?,?,?)`,
			"ov-"+strconv.Itoa(i), testAcc, s[0], s[1], s[1]-s[0]); err != nil {
			t.Fatalf("插入重叠会话 %d: %v", i, err)
		}
	}

	n, err := st.MergeRecentPlaySessions(sessionMergeWindow)
	if err != nil {
		t.Fatalf("合并: %v", err)
	}
	if n != 0 {
		t.Errorf("重叠会话被合并掉 %d 条, 期望 0", n)
	}
	got, err := st.ListPlaySessions(testAcc, 10, 0)
	if err != nil {
		t.Fatalf("列出: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("会话数 = %d, 期望 2(重叠段应各自保留)", len(got))
	}
}

// TestMergeRecentPlaySessionsSkipsOngoing 合并只动已结束的会话:
// 进行中的会话正被 StartPlaySession 当作「重开目标」,改它会让在线状态错乱。
func TestMergeRecentPlaySessionsSkipsOngoing(t *testing.T) {
	st := newTestStore(t)
	defer st.Close()
	base := time.Now().Add(-time.Hour).Unix()

	// 一条已结束 + 一条进行中(进行中在它之后 60s,间隔在窗口内)。
	//
	// 进行中那条必须**直接 INSERT**,不能用 StartPlaySession —— 后者自己就会把新会话
	// 续到刚结束的那条上(那正是合并功能),库里于是只剩一条,测不到「跳过进行中」
	// 这件事。这里要模拟的是:存量数据里已有一条进行中会话时,批量合并不能动它。
	if _, err := st.db.Exec(
		`INSERT INTO play_sessions(conn_id, account, login_time, logout_time, duration) VALUES(?,?,?,?,?)`,
		"old", testAcc, base, base+100, 100); err != nil {
		t.Fatalf("插入已结束: %v", err)
	}
	if _, err := st.db.Exec(
		`INSERT INTO play_sessions(conn_id, account, login_time) VALUES(?,?,?)`,
		"live", testAcc, base+160); err != nil {
		t.Fatalf("插入进行中: %v", err)
	}

	n, err := st.MergeRecentPlaySessions(sessionMergeWindow)
	if err != nil {
		t.Fatalf("合并: %v", err)
	}
	if n != 0 {
		t.Errorf("合并掉 %d 条, 期望 0(进行中会话不得参与合并)", n)
	}
	if got := st.countOpen(testAcc); got != 1 {
		t.Errorf("进行中会话 = %d, 期望 1(未被合并破坏)", got)
	}
	all, err := st.ListPlaySessions(testAcc, 10, 0)
	if err != nil {
		t.Fatalf("列出: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("会话数 = %d, 期望 2", len(all))
	}
}
