package store

import (
	"sort"
	"strconv"
	"testing"
	"time"
)

// TestListPlaySessionsWithNullDuration 复现「管理页面登录统计」的扫错:进行中会话的
// duration 列为 NULL(下线时才写入),直接扫入 int64 会报
// "converting NULL to int64 is unsupported"。修复后应正常返回,Online=true、Duration=0。
func TestListPlaySessionsWithNullDuration(t *testing.T) {
	st := newTestStore(t)
	now := time.Now().Unix()

	if err := st.StartPlaySession("conn-1", testAcc, now); err != nil {
		t.Fatalf("开启进行中会话: %v", err)
	}
	if err := st.StartPlaySession("conn-2", testAcc, now); err != nil {
		t.Fatalf("开启已结束会话: %v", err)
	}
	if err := st.EndPlaySession("conn-2", now+120); err != nil {
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
