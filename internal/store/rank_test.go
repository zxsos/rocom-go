package store

import (
	"testing"
	"time"
)

// TestSettleRankTitles 验证每日称号结算:
//   - 大富翁 = 前一日结束金币最多(无快照时退回 accounts.coins)
//   - 赚钱王 = 前一日净赚最多(须 >0)
//   - 败家子 = 前一日净亏最多(须 <0,并列取账号字典序小者)
//   - 退出排行榜(rank_join=0)与当日未登录者不参与评选
func TestSettleRankTitles(t *testing.T) {
	st := newTestStore(t)
	loc := time.FixedZone("CST", 8*3600)
	day := func(d, h int) int64 {
		return time.Date(2026, 8, d, h, 0, 0, 0, loc).Unix()
	}
	// 前一日 = 2026-08-27,归属日(结算日) = 2026-08-28
	put := func(account string, coins, ts int64) {
		if _, err := st.db.Exec(`INSERT INTO coin_snapshots(account, coins, ts) VALUES(?,?,?)`,
			account, coins, ts); err != nil {
			t.Fatalf("插快照: %v", err)
		}
	}
	mk := func(account, name string) {
		if err := st.UpsertAccount(account, name); err != nil {
			t.Fatalf("建账号 %s: %v", account, err)
		}
	}

	mk("UID:1", "阿甲") // 当日净赚 +100 → 赚钱王
	put("UID:1", 100, day(27, 10))
	put("UID:1", 200, day(27, 20))
	mk("UID:2", "阿乙") // 当日净亏 -100 → 败家子(与丙并列,字典序胜出)
	put("UID:2", 500, day(27, 9))
	put("UID:2", 400, day(27, 19))
	mk("UID:3", "阿丙") // 当日净亏 -100(并列败家子)
	put("UID:3", 1000, day(27, 11))
	put("UID:3", 900, day(27, 21))
	mk("UID:4", "阿丁") // 无快照,只有 accounts.coins → 大富翁(兜底路径)
	if _, err := st.db.Exec(`UPDATE accounts SET coins=3000, has_coins=1 WHERE account='UID:4'`); err != nil {
		t.Fatalf("设丁的金币: %v", err)
	}
	mk("UID:5", "阿戊") // 仅结算日前一天之前有快照(当日未登录)→ 不参与盈亏,金币兜底算大富翁候选
	put("UID:5", 2000, day(26, 10))
	put("UID:5", 2500, day(26, 20))
	mk("UID:6", "阿己") // 已退出排行榜 → 完全排除
	put("UID:6", 999999, day(27, 12))
	if err := st.SetAccountRankJoin("UID:6", false); err != nil {
		t.Fatalf("退出排行榜: %v", err)
	}

	if err := st.SettleRankTitles("2026-08-28"); err != nil {
		t.Fatalf("结算: %v", err)
	}
	got, err := st.RankTitles("2026-08-28")
	if err != nil {
		t.Fatalf("读称号: %v", err)
	}
	byTitle := map[string]RankTitleRow{}
	for _, r := range got {
		byTitle[r.Title] = r
	}
	want := map[string]struct{ account, name string }{
		"大富翁": {"UID:4", "阿丁"}, // 3000 兜底最大;阿戊 2500 次之
		"赚钱王": {"UID:1", "阿甲"}, // +100
		"败家子": {"UID:2", "阿乙"}, // -100(与阿丙并列,UID 小者胜)
	}
	for title, w := range want {
		r, ok := byTitle[title]
		if !ok {
			t.Fatalf("缺少称号 %s,实得 %+v", title, got)
		}
		if r.Account != w.account || r.Name != w.name {
			t.Errorf("%s = %s(%s),期望 %s(%s)", title, r.Name, r.Account, w.name, w.account)
		}
	}
	// 幂等:重复结算结果一致(不残留旧行)
	if err := st.SettleRankTitles("2026-08-28"); err != nil {
		t.Fatalf("重复结算: %v", err)
	}
	got2, err := st.RankTitles("2026-08-28")
	if err != nil {
		t.Fatalf("重复结算后读称号: %v", err)
	}
	if len(got2) != len(got) {
		t.Fatalf("重复结算后 %d 条,期望 %d 条", len(got2), len(got))
	}
}
