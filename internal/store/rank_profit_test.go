package store

import (
	"testing"
	"time"
)

// 本文件锁住盈亏榜的**按日**口径。
//
// 背景(用户报告):某人一天没登录,却仍是「赚钱最多」—— 因为旧实现拿「首次快照」
// 当基线,算的是**累计**盈亏;而快照只在登录时写入,不登录的人余额与基线一起冻结
// 在历史峰值,于是「曾经赚过」的人永远霸榜。排行榜要看的是「谁**今天**在赚钱」,
// 故改为按自然日(北京时间)切分,与称号评选(赚钱王按前一日净变化)口径一致。

// rankToday 返回北京时间今天 h 点 m 分的时间戳(相对「今天」取,避免写死日期过期)。
func rankToday(h, m int) int64 {
	n := time.Now().In(rankLoc)
	return time.Date(n.Year(), n.Month(), n.Day(), h, m, 0, 0, rankLoc).Unix()
}

// rankYesterday 返回北京时间昨天 h 点的时间戳。
func rankYesterday(h int) int64 {
	n := time.Now().In(rankLoc).AddDate(0, 0, -1)
	return time.Date(n.Year(), n.Month(), n.Day(), h, 0, 0, 0, rankLoc).Unix()
}

// TestLeaderboardProfitIsDaily 当日盈亏 = 当前 - 今日起点,不是累计。
//
// 「昨天赚、今天没动」的账号,今日盈亏应为 0(带昨夜余额进今天),
// 而不是把昨天的收益反复计入 —— 那正是旧实现的毛病。
func TestLeaderboardProfitIsDaily(t *testing.T) {
	st := newTestStore(t)
	mk := func(acc, name string, coins int64) {
		if err := st.UpsertAccount(acc, name); err != nil {
			t.Fatalf("建账号 %s: %v", acc, err)
		}
		if err := st.SetAccountCoins(acc, coins); err != nil {
			t.Fatalf("设洛克贝 %s: %v", acc, err)
		}
		// SetAccountCoins 会按「现在」插一条快照,这里改成我们要的时刻
		if _, err := st.db.Exec(`DELETE FROM coin_snapshots WHERE account=?`, acc); err != nil {
			t.Fatalf("清快照 %s: %v", acc, err)
		}
	}
	put := func(acc string, coins, ts int64) {
		if _, err := st.db.Exec(`INSERT INTO coin_snapshots(account, coins, ts) VALUES(?,?,?)`,
			acc, coins, ts); err != nil {
			t.Fatalf("插快照: %v", err)
		}
	}

	// 甲:今天 9 点 1000 → 现在 1500,当日 +500
	mk("UID:1", "甲", 1500)
	put("UID:1", 1000, rankToday(9, 0))
	put("UID:1", 1500, rankToday(10, 0))

	// 乙:昨天赚到 5000,今天**没登录**(无今日快照)→ 当日盈亏 0
	mk("UID:2", "乙", 5000)
	put("UID:2", 100, rankYesterday(9))
	put("UID:2", 5000, rankYesterday(20)) // 昨夜最后一条 = 5000,带入今天

	entries, err := st.Leaderboard()
	if err != nil {
		t.Fatalf("Leaderboard: %v", err)
	}
	by := map[string]RankEntry{}
	for _, e := range entries {
		by[e.Account] = e
	}

	if got := by["UID:1"].Profit; got != 500 {
		t.Errorf("甲当日盈亏 = %d, 期望 500(今日起点 1000 → 现在 1500)", got)
	}
	if got := by["UID:2"].Profit; got != 0 {
		t.Errorf("乙当日盈亏 = %d, 期望 0(今天没登录,带昨夜 5000 进今天)", got)
	}
	// 关键:乙昨天赚了 4900,但今日盈亏为 0,不该超过今天真赚了 500 的甲
	if by["UID:2"].Profit >= by["UID:1"].Profit {
		t.Errorf("不登录的乙(profit=%d)不该排在当日赚钱的甲(profit=%d)之前",
			by["UID:2"].Profit, by["UID:1"].Profit)
	}
}

// TestLeaderboardBaselineCarriesYesterday 今天还没登录时,基线带**昨夜最后一条**,
// 而不是今天的 accounts.coins —— 否则一登录盈亏就被抹成 0。
func TestLeaderboardBaselineCarriesYesterday(t *testing.T) {
	st := newTestStore(t)
	if err := st.UpsertAccount("UID:9", "甲"); err != nil {
		t.Fatalf("建账号: %v", err)
	}
	put := func(coins, ts int64) {
		if _, err := st.db.Exec(`INSERT INTO coin_snapshots(account, coins, ts) VALUES(?,?,?)`,
			"UID:9", coins, ts); err != nil {
			t.Fatalf("插快照: %v", err)
		}
	}
	put(1000, rankYesterday(9))
	put(1200, rankYesterday(22)) // 昨夜收在 1200
	// 今天的 accounts.coins 已被更新为 1500(登录过),但快照还没写
	if _, err := st.db.Exec(`UPDATE accounts SET coins=1500, has_coins=1 WHERE account='UID:9'`); err != nil {
		t.Fatalf("设洛克贝: %v", err)
	}

	e := st.AccountRank("UID:9")

	if e.Baseline != 1200 {
		t.Errorf("基线 = %d, 期望 1200(带昨夜最后一条,而非首次 1000 或当前 1500)", e.Baseline)
	}
	if e.Profit != 300 {
		t.Errorf("盈亏 = %d, 期望 300(1500 - 1200)", e.Profit)
	}
}

// TestLeaderboardNoSnapshotProfitZero 从未记录过快照:基线兜底为当前 coins,盈亏 0。
// 保护兜底路径 —— 新账号首次登录当天不该显示一个莫名其妙的盈亏数。
func TestLeaderboardNoSnapshotProfitZero(t *testing.T) {
	st := newTestStore(t)
	if err := st.UpsertAccount("UID:8", "新号"); err != nil {
		t.Fatalf("建账号: %v", err)
	}
	if _, err := st.db.Exec(`UPDATE accounts SET coins=777, has_coins=1 WHERE account='UID:8'`); err != nil {
		t.Fatalf("设洛克贝: %v", err)
	}

	e := st.AccountRank("UID:8")

	if e.Baseline != 777 || e.Profit != 0 {
		t.Errorf("无快照时 baseline=%d profit=%d, 期望 (777, 0)", e.Baseline, e.Profit)
	}
}

// TestRankDayStartUsesBeijingTime 分日必须用北京时间:服务器本地常是 UTC,
// 若按本地日期切,北京时间 0~8 点会被算成前一天。
func TestRankDayStartUsesBeijingTime(t *testing.T) {
	// 北京时间 2026-09-01 07:00(UTC 2026-08-31 23:00)—— 日期在 UTC 下是前一天
	n := time.Date(2026, 9, 1, 7, 0, 0, 0, rankLoc)
	got := rankDayStart(n)

	want := time.Date(2026, 9, 1, 0, 0, 0, 0, rankLoc).Unix()
	if got != want {
		t.Errorf("rankDayStart = %d, 期望 %d(北京时间当天 00:00,而非 UTC 日期)",
			got, want)
	}
}
