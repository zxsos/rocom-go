package gamedata

import "testing"

// TestMapImageHD 守住「这张底图有没有高清版」的判定。
//
// 它决定前端显不显示「高清」开关(position payload 的 imgHd,见 server/payload.go):
// 判错的后果是——有开关却没素材时点了没反应,或该有开关的场景(抓过高清素材的
// 卡洛西亚大陆)凭空少了开关。素材由 scripts/fetch_bigmap_hd.py 产出,不随解包更新。
func TestMapImageHD(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 抓过高清素材:应返回 <底图名>_hd(不含扩展名,前端拼 /img/bigmap/<名>.webp)。
	if got := db.MapImageHD(10003, 0); got != "10003_hd" {
		t.Errorf("10003(卡洛西亚大陆)高清底图 = %q,期望 10003_hd", got)
	}
	// 没抓过的场景:返回空,前端据此不显示开关。
	if got := db.MapImageHD(10018, 0); got != "" {
		t.Errorf("10018(魔法学院)无高清素材,应返回空,实得 %q", got)
	}
	// 无底图场景:同样为空,且不得 panic。
	if got := db.MapImageHD(99999, 0); got != "" {
		t.Errorf("无底图场景应返回空,实得 %q", got)
	}
}
