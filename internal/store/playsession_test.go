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

	// 关掉后重新上线 → 新会话(不复用已结束的)
	if err := st.StartPlaySession("conn-E", testAcc, now+120); err != nil {
		t.Fatalf("重新开启会话: %v", err)
	}
	if n := st.countOpen(testAcc); n != 1 {
		t.Fatalf("重新上线后进行中会话 = %d, 期望 1", n)
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
	// 每条必须「上线→下线」成对造:会话是账号级的,同一账号连续 StartPlaySession
	// 会被幂等挡住(见 TestStartPlaySessionPerAccount),只造得出一条。
	const per = 5
	type row struct {
		acc string
		ts  int64
	}
	var all []row
	for k, acc := range []string{testAcc, "UID:2"} {
		for i := 0; i < per; i++ {
			ts := base + int64(k)*3600 + int64(i)*60
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
