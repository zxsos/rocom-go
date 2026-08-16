package server

import (
	"encoding/base64"
	"encoding/json"
	"math/bits"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/whoisnian/rocom-capture/internal/gamedata"
	"github.com/whoisnian/rocom-capture/internal/store"
)

// 卡洛西亚大陆:ox=306000 oy=408000 side=408000(见 names.json 的 maps)。取地块正中的一点当玩家位置。
const (
	testRes = 10003
	testAcc = "UID:1"
	testOX  = 306000
	testOY  = 408000
	testX   = testOX + 408000/2
	testY   = testOY + 408000/2
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	db, err := gamedata.Load()
	if err != nil {
		t.Fatalf("加载名称库: %v", err)
	}
	st, err := store.New(filepath.Join(t.TempDir(), "t.db"), db)
	if err != nil {
		t.Fatalf("打开数据库: %v", err)
	}
	return New(st, NewHub(), db)
}

// grid 经 HTTP 接口取某张覆盖位图(顺带验一遍 base64 编码),返回位图与边长(格)。
func grid(t *testing.T, s *Server, layer int32) (cells []byte, dim int) {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handlePaint(rr, httptest.NewRequest("GET", "/api/paint?res=10003&layer="+
		strconv.Itoa(int(layer))+"&account="+testAcc, nil))
	var got struct {
		W, H  int
		Cells string
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析回包: %v (%s)", err, rr.Body)
	}
	raw, err := base64.StdEncoding.DecodeString(got.Cells)
	if err != nil {
		t.Fatalf("解 base64: %v", err)
	}
	return raw, got.W
}

// painted 取某张覆盖位图已涂的格子数。
func painted(t *testing.T, s *Server, layer int32) (n, dim int) {
	t.Helper()
	cells, dim := grid(t, s, layer)
	for _, b := range cells {
		n += bits.OnesCount8(b)
	}
	return n, dim
}

// at 报告某个世界坐标所在的格子涂没涂上(只看地表那张)。
func at(t *testing.T, s *Server, x, y int32) bool {
	t.Helper()
	cells, dim := grid(t, s, 0)
	gx, gy := int(x-testOX)/paintCell, int(y-testOY)/paintCell
	if gx < 0 || gy < 0 || gx >= dim || gy >= dim {
		t.Fatalf("坐标越界: %d,%d", x, y)
	}
	idx := gy*dim + gx
	return cells[idx>>3]&(1<<(idx&7)) != 0
}

// TestPaintSafeOnly:一只野生宠都没下发时只留贴身那条安全带(地形/剧情限制的区域不刷宠,
// 光靠走廊会永远空着)。面积应当是一个 15m 的圆,而不是 80m 那种大圆。
func TestPaintSafeOnly(t *testing.T) {
	s := newTestServer(t)
	s.PaintSeen(testAcc, testRes, 0, [][2]int32{{testX, testY}}, nil)
	n, _ := painted(t, s, 0)
	if n < 8 || n > 16 { // π*15²/8² ≈ 11 格
		t.Errorf("没有宠物时应只涂出一个 15m 的圆(约 11 格),实为 %d 格", n)
	}
	if !at(t, s, testX+1000, testY) {
		t.Error("安全半径内(10m)该涂上")
	}
	if at(t, s, testX+2500, testY) {
		t.Error("安全半径外(25m)不该涂——那儿没有宠物下发过")
	}
}

// TestPaintSafePath:贴身安全带沿「上一包 → 这一包」那段路涂,心跳空窗跳出几十米也不会断。
func TestPaintSafePath(t *testing.T) {
	s := newTestServer(t)
	s.PaintSeen(testAcc, testRes, 0, [][2]int32{{testX, testY}, {testX + 5000, testY}}, nil)
	for _, dx := range []int32{0, 1000, 2500, 4000, 5000} {
		if !at(t, s, testX+dx, testY) {
			t.Errorf("轨迹上 %dm 处该涂上", dx/100)
		}
	}
	if at(t, s, testX+2500, testY+2500) {
		t.Error("离轨迹 25m 处不该涂")
	}
}

// TestPaintCorridor:涂出来的是「玩家 → 宠物」那条带子:线上涂了、宠物脚下涂了,
// 侧向超出半宽的没涂,宠物之外的延长线上也没涂。
func TestPaintCorridor(t *testing.T) {
	s := newTestServer(t)
	const d = 6000 // 宠物在正东 60m
	s.PaintSeen(testAcc, testRes, 0, [][2]int32{{testX, testY}}, [][2]int32{{testX + d, testY}})

	if !at(t, s, testX+d/2, testY) {
		t.Error("玩家与宠物之间的中点该涂上")
	}
	if !at(t, s, testX+d, testY) {
		t.Error("宠物脚下该涂上")
	}
	if !at(t, s, testX, testY) {
		t.Error("玩家脚下该涂上(走廊两端是圆帽)")
	}
	if at(t, s, testX+d/2, testY+3000) { // 侧向 30m,远超半宽 12m
		t.Error("走廊之外(侧向 30m)不该涂")
	}
	if at(t, s, testX+d+3000, testY) { // 宠物再往前 30m
		t.Error("宠物之外的延长线不该涂")
	}
	// 面积量级:60m 长 × 24m 宽 + 两个半圆帽 ≈ 1890 m²,再加贴身的 15m 圆,一格 64 m² ⇒ 约 30 格
	n, dim := painted(t, s, 0)
	if dim != 510 {
		t.Errorf("格子数应为 510(边长 408000 / 800),实为 %d", dim)
	}
	if n < 20 || n > 50 {
		t.Errorf("一条 60m 的走廊应涂约 30 格,实为 %d", n)
	}

	// 幂等:同一个玩家位置、同一只宠再来一次,不该有任何新格子
	s.PaintSeen(testAcc, testRes, 0, [][2]int32{{testX, testY}}, [][2]int32{{testX + d, testY}})
	if n2, _ := painted(t, s, 0); n2 != n {
		t.Errorf("同样的视线重复涂不该有新格子: %d -> %d", n, n2)
	}
	// 换个方向的宠物:新走廊,格子变多
	s.PaintSeen(testAcc, testRes, 0, [][2]int32{{testX, testY}}, [][2]int32{{testX, testY + d}})
	if n3, _ := painted(t, s, 0); n3 <= n {
		t.Errorf("另一个方向的宠物应涂上新格子: %d -> %d", n, n3)
	}
}

// TestPaintDelta:新涂的格子经 SSE 推出,下标落在位图范围内;没有新格子时不广播。
func TestPaintDelta(t *testing.T) {
	s := newTestServer(t)
	ch := s.hub.subscribe()
	defer s.hub.unsubscribe(ch)

	pets := [][2]int32{{testX + 5000, testY}}
	here := [][2]int32{{testX, testY}}
	s.PaintSeen(testAcc, testRes, 0, here, pets)
	var msg struct {
		Type string
		Data struct {
			W, H  int
			Cells []int32
		}
	}
	select {
	case m := <-ch:
		if err := json.Unmarshal(m.data, &msg); err != nil {
			t.Fatalf("解析广播: %v", err)
		}
	default:
		t.Fatal("涂上新格子却没有广播")
	}
	if msg.Type != "paint" || len(msg.Data.Cells) == 0 {
		t.Fatalf("广播内容不对: %+v", msg)
	}
	for _, idx := range msg.Data.Cells {
		if idx < 0 || int(idx) >= msg.Data.W*msg.Data.H {
			t.Fatalf("格子下标越界: %d(共 %d 格)", idx, msg.Data.W*msg.Data.H)
		}
	}
	s.PaintSeen(testAcc, testRes, 0, here, pets) // 没有新格子
	select {
	case m := <-ch:
		t.Fatalf("同样的视线不该再广播: %s", m.data)
	default:
	}
}

// TestPaintLayers:地表与分层地图(洞穴/楼层)各涂各的——两个空间 AOI 不相通。
func TestPaintLayers(t *testing.T) {
	s := newTestServer(t)
	pets := [][2]int32{{testX + 5000, testY}}
	here := [][2]int32{{testX, testY}}
	s.PaintSeen(testAcc, testRes, 0, here, pets)
	if n, _ := painted(t, s, 3); n != 0 {
		t.Errorf("地表涂的不该算进 3 层,实为 %d 格", n)
	}
	s.PaintSeen(testAcc, testRes, 3, here, pets)
	if n, _ := painted(t, s, 3); n == 0 {
		t.Error("3 层涂过之后不该还是空的")
	}
}

// TestPaintPersistReset:落盘后能原样读回;重置把内存与库一起清掉。
func TestPaintPersistReset(t *testing.T) {
	s := newTestServer(t)
	s.PaintSeen(testAcc, testRes, 0, [][2]int32{{testX, testY}}, [][2]int32{{testX + 5000, testY}})
	n, _ := painted(t, s, 0)
	s.FlushPaint()

	g, ok := s.store.LoadPaint(testAcc, testRes, 0)
	if !ok || g.W != 510 {
		t.Fatalf("落盘后应能读回位图: ok=%v w=%d", ok, g.W)
	}
	saved := 0
	for _, b := range g.Cells {
		saved += bits.OnesCount8(b)
	}
	if saved != n {
		t.Errorf("库里的格子数与内存不一致: %d != %d", saved, n)
	}

	s.ResetPaint(testAcc, testRes, 0)
	if n2, _ := painted(t, s, 0); n2 != 0 {
		t.Errorf("重置后应为空,实为 %d 格", n2)
	}
	if _, ok := s.store.LoadPaint(testAcc, testRes, 0); ok {
		t.Error("重置后库里不该还留着行")
	}
}

// TestPaintNoBaseMap:没有大地图底图的场景(家园/副本)不涂,接口回 w=0 让前端别画。
func TestPaintNoBaseMap(t *testing.T) {
	s := newTestServer(t)
	s.PaintSeen(testAcc, 30001, 0, [][2]int32{{0, 0}}, [][2]int32{{100, 100}}) // 家园室内:有底图但不是大世界
	rr := httptest.NewRecorder()
	s.handlePaint(rr, httptest.NewRequest("GET", "/api/paint?res=30001&account="+testAcc, nil))
	var got struct{ W int }
	json.Unmarshal(rr.Body.Bytes(), &got)
	if got.W != 0 {
		t.Errorf("家园不该有涂地位图,实为 w=%d", got.W)
	}
}
