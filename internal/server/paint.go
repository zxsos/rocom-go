package server

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/whoisnian/rocom-capture/internal/store"
)

// ---- 涂地(实时地图页的「已扫过」覆盖图层,见 docs/data.md 3.8)----
//
// 涂的依据是**实际下发过的野生宠**,不是「离玩家多近」:每收到一只野生宠实体,就把
// 「玩家 ↔ 这只宠」之间的走廊涂上——那条线上的东西此刻确确实实到我手里了,稀有个体若在,
// 野生宠物图层早就标出来了。于是涂出来的形状自然贴着真实下发情况:
//
//   - 有宠的方向涂到宠那儿(AOI 实测 80m,首领更远,但这里不写死任何距离,给多远涂多远);
//   - 一只都没下发的方向(水面、城镇、山崖这类根本不刷宠的地方)**不涂**——那儿本来也没什么可找的。
//
// 剩下没涂的,就是「还没扫过、值得去」的地方。
//
// 表示法是**格子位图**,不是轨迹/连线:
//   - 幂等——同一片地方来回走多少趟、同一只宠反复入视野,都只是那几位,存储与推送不随时间增长;
//   - 增量极小——走廊多半早涂过了,算完发现「没有新格」就直接返回,不广播、不落盘;
//   - 前端可直接当图用:一张 w*h 的 canvas,按格填色后交给 CSS 缩放,平移缩放零开销。
//
// 状态由 server 独占(paintState 自带锁):抓包管线在消费循环里调 PaintSeen 记录,HTTP handler
// 读同一份内存,故页面拿到的一定是最新的;SQLite 只是重启后的持久化,落盘按下面两条线攒批。
const (
	// paintCell 是一格的世界边长(厘米)。8m 是「够细」与「够小」的折中:走廊半宽 12m,格子
	// 取其一半上下,带子的边在图上看已不生硬;大陆(边长 4080m)一张图 510*510 格 = 32.5KB,
	// base64 后 43KB,进一次场景拉一次,可以接受。再细一档(4m)位图就要四倍,不值。
	paintCell = 800
	// paintCorridor 是「玩家 ↔ 野生宠」走廊的半宽(厘米),即涂出来的带子宽 24m。
	// 它不是判定距离(距离由实际下发的宠物给),只是「一条视线代表多宽的一片算看过了」:
	// 取 12m 是因为野生宠在刷新点附近晃动本就有几米误差,再窄了图上只剩蛛网。
	paintCorridor = 1200
	// paintSafe 是**贴身安全半径**(厘米):玩家走过的路两侧这么宽,不管有没有宠物下发都算扫过。
	// 地形/剧情限制的区域(峭壁、城镇、剧情场地)本来就不刷宠,只画「玩家↔宠物」的走廊会让
	// 这些地方永远空着,人来回走也不见涂,反倒像是没扫过。
	//
	// 15m 这个数是从历史流量统计出来的,不是拍的:24 份 pcap 里 258 次 AOI 补发,「下发那一刻
	// 玩家离它多远」的下沿是 **17.1m**(p2=17.1、p5=26.4);唯一比它更近的一例是 3.1m,
	// 发生在捕捉那份 pcap 里同一只宠脚下重新下发——那属于「刷新点就在脚底下」,任何半径都挡不住,
	// 且真站在它身上时屏幕里早看见了。取 15m 留 2m 余量:**历史上从没有过「玩家已进到 15m
	// 以内、某只野生宠才姗姗下发」**,故 15m 内没冒出宠物,就可以判定那儿确实没有。
	// (仍有一个理论缺口:高速飞行擦过时可能整只都没等到下发,见 docs/data.md 3.8 的说明。)
	paintSafe = 1500
	// paintSaveEvery 是落盘间隔:涂地是「越攒越多」的状态,丢掉最后几秒无所谓,不必每格都写库。
	paintSaveEvery = 10 * time.Second
	// paintSaveCells 是另一条落盘触发线:攒够这么多新格子就写一次。离线回放几秒钟就跑完
	// 一整份 pcap,按时间根本攒不到一次落盘,故同时按「涂了多少」计。
	paintSaveCells = 512
)

// paintKey 是一张覆盖位图的身份:账号 + 场景 + 分层(0=地表)。分层地图(洞穴/楼层)与地表
// 是两个空间,AOI 互不相通(见 docs/data.md 3.4 的洞穴层守卫),故各涂各的。
type paintKey struct {
	acc   string
	res   int32
	layer int32
}

// paintGrid 是一张覆盖位图的内存态。
type paintGrid struct {
	w, h    int
	bits    []byte
	dirty   bool
	unsaved int       // 自上次落盘以来新涂的格子数(见 paintSaveCells)
	saved   time.Time // 最近一次落盘时刻
}

// savePaint 把位图写回库并清掉脏标记。调用方须持有 s.paint.mu。
func (s *Server) savePaint(k paintKey, g *paintGrid) {
	s.store.SavePaint(k.acc, k.res, k.layer, store.PaintGrid{W: g.w, H: g.h, Cells: g.bits})
	g.dirty, g.unsaved, g.saved = false, 0, time.Now()
}

// paintState 是全部覆盖位图(按 paintKey)。零值可用。
type paintState struct {
	mu    sync.Mutex
	grids map[paintKey]*paintGrid
}

// paintDims 按场景底图算出格子数(底图是正方形地块,故行列同数)。无底图/非大世界返回 ok=false:
// 家园那几张图边长才 120m,且家园里的宠物是自己的、不是野生宠,涂它没有意义。
func (s *Server) paintDims(res int32) (int, bool) {
	mi, ok := s.db.MapInfo(uint32(res))
	if !ok || !mi.World || mi.Side <= 0 {
		return 0, false
	}
	n := int((mi.Side + paintCell - 1) / paintCell)
	if n <= 0 {
		return 0, false
	}
	return n, true
}

// Paintable 报告某场景能不能涂地(有大地图底图的大世界场景才能)。位置推送带上这一位,
// 前端就不必为了知道「这儿能不能涂」而先拉一次位图(图层关着时本来不该拉)。
func (s *Server) Paintable(res int32) bool {
	_, ok := s.paintDims(res)
	return ok
}

// paintGridFor 取(必要时从库加载/新建)某张覆盖位图。调用方须持有 s.paint.mu。
func (s *Server) paintGridFor(k paintKey, n int) *paintGrid {
	if s.paint.grids == nil {
		s.paint.grids = map[paintKey]*paintGrid{}
	}
	if g := s.paint.grids[k]; g != nil {
		return g
	}
	g := &paintGrid{w: n, h: n, bits: make([]byte, (n*n+7)/8), saved: time.Now()}
	// 库里存的若是另一套格子尺寸(改过 paintCell),尺寸对不上就当没有,重新开始涂。
	if old, ok := s.store.LoadPaint(k.acc, k.res, k.layer); ok && old.W == n && old.H == n &&
		len(old.Cells) == len(g.bits) {
		copy(g.bits, old.Cells)
	}
	s.paint.grids[k] = g
	return g
}

// PaintSeen 涂两样东西:玩家走过的路两侧 paintSafe 的贴身安全带,以及「玩家 → 当前视野里
// 每一只野生宠」的走廊。新涂上的格子经 SSE(paint)推给前端。由抓包管线在每个移动包、
// 以及每次收到实体进/离通知时调用(见 pipeline/position.go 与 pipeline/wildpets.go)。
//
// path 是玩家自上一包以来的轨迹(世界坐标厘米,末点即当前位置;心跳空窗里带上补报的轨迹点,
// 免得两个包之间跳出一段没涂的空);pets 是此刻在 AOI 里的野生宠坐标;layer 为分层 id(0=地表)。
// 这一带一只宠都没下发时只留下贴身那条带子——涂色的依据是「实际下发过什么」,不是「离我多近」。
func (s *Server) PaintSeen(acc string, res, layer int32, path [][2]int32, pets [][2]int32) {
	if acc == "" || len(path) == 0 {
		return
	}
	n, ok := s.paintDims(res)
	if !ok {
		return
	}
	mi, _ := s.db.MapInfo(uint32(res))
	k := paintKey{acc: acc, res: res, layer: layer}

	s.paint.mu.Lock()
	g := s.paintGridFor(k, n)
	var added []int32
	// 贴身安全带:沿轨迹逐段涂(单点时就是以玩家为心的一个小圆)
	for i := 1; i < len(path); i++ {
		added = stamp(g, n, mi.OX, mi.OY, path[i-1], path[i], paintSafe, added)
	}
	if len(path) == 1 {
		added = stamp(g, n, mi.OX, mi.OY, path[0], path[0], paintSafe, added)
	}
	// 视野里每只野生宠一条走廊(从玩家当前位置画到它那儿)
	cur := path[len(path)-1]
	for _, pt := range pets {
		added = stamp(g, n, mi.OX, mi.OY, cur, pt, paintCorridor, added)
	}
	if len(added) == 0 {
		s.paint.mu.Unlock()
		return // 这些走廊早涂过了:不广播不落盘(绝大多数调用走这条)
	}
	g.dirty = true
	g.unsaved += len(added)
	if g.unsaved >= paintSaveCells || time.Since(g.saved) >= paintSaveEvery {
		s.savePaint(k, g)
	}
	s.paint.mu.Unlock()

	// 增量只发新格子的下标(常态几个到几十个),前端照着填色即可。
	s.hub.Broadcast("paint", acc, map[string]any{
		"res": res, "layer": layer, "w": n, "h": n, "cells": added,
	})
}

// stamp 涂一条 A→B 的带子(到线段距离 ≤ r 的格子全涂上),新涂的格子下标追加进 added 返回。
// 两端各带半个圆帽,故 A=B 时就是以 A 为心、半径 r 的圆。
//
// 只扫线段的包围盒(80m 的走廊约 12×3 格,最多百来格),先看位、涂过就跳,没涂过的才算一次
// 点到线段的距离。调用方对每只宠物、每段轨迹各调一次。
func stamp(g *paintGrid, n int, ox, oy int32, a, b [2]int32, r int32, added []int32) []int32 {
	ax, ay, bx, by := a[0], a[1], b[0], b[1]
	x0 := floorDiv(min(ax, bx)-r-ox, paintCell)
	x1 := floorDiv(max(ax, bx)+r-ox, paintCell)
	y0 := floorDiv(min(ay, by)-r-oy, paintCell)
	y1 := floorDiv(max(ay, by)+r-oy, paintCell)
	for gy := y0; gy <= y1; gy++ {
		if gy < 0 || gy >= n {
			continue
		}
		for gx := x0; gx <= x1; gx++ {
			if gx < 0 || gx >= n {
				continue
			}
			idx := gy*n + gx
			if g.bits[idx>>3]&(1<<(idx&7)) != 0 {
				continue // 已经涂过
			}
			cxw := float64(ox + int32(gx)*paintCell + paintCell/2) // 格心世界坐标
			cyw := float64(oy + int32(gy)*paintCell + paintCell/2)
			if segDist2(cxw, cyw, float64(ax), float64(ay), float64(bx), float64(by)) > float64(r)*float64(r) {
				continue
			}
			g.bits[idx>>3] |= 1 << (idx & 7)
			added = append(added, int32(idx))
		}
	}
	return added
}

// segDist2 返回点 (px,py) 到线段 A-B 的距离平方(投影参数夹到 [0,1],故两端是圆帽)。
func segDist2(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	t := 0.0
	if l2 := dx*dx + dy*dy; l2 > 0 {
		t = ((px-ax)*dx + (py-ay)*dy) / l2
		t = max(0, min(1, t))
	}
	qx, qy := ax+t*dx, ay+t*dy
	return (px-qx)*(px-qx) + (py-qy)*(py-qy)
}

// ResetPaint 清空某账号某场景某层的涂地(内存 + 库)。
func (s *Server) ResetPaint(acc string, res, layer int32) {
	s.paint.mu.Lock()
	delete(s.paint.grids, paintKey{acc: acc, res: res, layer: layer})
	s.paint.mu.Unlock()
	s.store.ClearPaint(acc, res, layer)
}

// FlushPaint 把还没落盘的覆盖位图写回库(进程退出前调一次,免得丢掉最后这十几秒)。
func (s *Server) FlushPaint() {
	s.paint.mu.Lock()
	defer s.paint.mu.Unlock()
	for k, g := range s.paint.grids {
		if g.dirty {
			s.savePaint(k, g)
		}
	}
}

// handlePaint 返回某场景某层(?res=&layer=)的整张覆盖位图,供地图页加载时铺底;
// 之后的变化走 SSE(paint)增量。无底图的场景返回 w=0(前端据此不画涂地层)。
//
// 位图直接 base64:大陆整张 32.5KB → base64 43KB,长度恒定(不随涂了多少变),
// 比「已涂格子下标列表」在涂开之后小得多(涂满就是 26 万个下标),且前端解码即是可按位读的
// Uint8Array。每进一个场景拉一次,之后只走增量。
func (s *Server) handlePaint(w http.ResponseWriter, r *http.Request) {
	res, layer, err := paintParams(r)
	if err != nil {
		http.Error(w, "bad res", http.StatusBadRequest)
		return
	}
	n, ok := s.paintDims(res)
	if !ok {
		writeJSON(w, map[string]any{"res": res, "layer": layer, "w": 0, "h": 0})
		return
	}
	acc := s.acct(r)
	s.paint.mu.Lock()
	g := s.paintGridFor(paintKey{acc: acc, res: res, layer: layer}, n)
	cells := base64.StdEncoding.EncodeToString(g.bits)
	s.paint.mu.Unlock()
	writeJSON(w, map[string]any{
		"res": res, "layer": layer, "w": n, "h": n,
		"cell": paintCell, "corridor": paintCorridor, "safe": paintSafe, "cells": cells,
	})
}

// handlePaintReset 清空某场景某层的涂地(?res=&layer=),并广播给同账号的其它页面。
func (s *Server) handlePaintReset(w http.ResponseWriter, r *http.Request) {
	res, layer, err := paintParams(r)
	if err != nil {
		http.Error(w, "bad res", http.StatusBadRequest)
		return
	}
	acc := s.acct(r)
	s.ResetPaint(acc, res, layer)
	s.hub.Broadcast("paint", acc, map[string]any{"res": res, "layer": layer, "reset": true})
	w.WriteHeader(http.StatusNoContent)
}

// floorDiv 是向下取整的整除(Go 的 / 是向零截断):玩家可能站在底图左/上边界之外
// (地块只是地图的一块),此时 (x-ox) 为负,截断会把搜索窗口挪错一格。
func floorDiv(a, b int32) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return int(q)
}

// paintParams 取涂地接口的场景/分层参数(layer 缺省为 0 = 地表)。
func paintParams(r *http.Request) (res, layer int32, err error) {
	v, err := strconv.ParseInt(r.URL.Query().Get("res"), 10, 32)
	if err != nil {
		return 0, 0, err
	}
	l, _ := strconv.ParseInt(r.URL.Query().Get("layer"), 10, 32)
	return int32(v), int32(l), nil
}
