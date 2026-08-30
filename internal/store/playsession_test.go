package store

import (
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

	sessions, err := st.ListPlaySessions("", 10)
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
