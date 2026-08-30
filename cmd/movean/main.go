// movean 把抓包里的移动包导成「前端拿到的形状」的 JSON,供运动平滑算法离线度量。
//
//   go run ./cmd/movean -out fixtures.json a.pcap b.pcap ...
//
// 存在理由:web/scripts/verify-motion.mjs 要在没有 pcap、没有后端的环境下复现
// 「箭头平不平」的度量,它吃的 fixture 必须由**真实抓包**派生,且投影口径必须与
// pipeline 推给前端的完全一致 —— 自己另写一份解析迟早会和后端跑偏,度量的就成了假数据。
// 故本工具直接复用 internal/scene 的解析与 internal/gamedata 的投影(同 buildPos)。
//
// 输出字段语义见 web/scripts/fixtures/README.md。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"time"

	"github.com/whoisnian/rocom-capture/internal/capture"
	"github.com/whoisnian/rocom-capture/internal/gamedata"
	"github.com/whoisnian/rocom-capture/internal/scene"
)

// minSegSpan 与 pipeline.buildPos 的判据一致:SegSpan 小于它就不下发轨迹。
const minSegSpan = 0.6

type pathPt struct {
	T float64 `json:"t"` // 服务器时刻(秒,相对首包)。后端暂不下发,前端按弧长回放;度量求真值要用
	U float64 `json:"u"`
	V float64 `json:"v"`
}

type pkt struct {
	T    float64  `json:"t"`
	U    float64  `json:"u"`
	V    float64  `json:"v"`
	VU   float64  `json:"vu,omitempty"`
	VV   float64  `json:"vv,omitempty"`
	Stop bool     `json:"stop,omitempty"`
	Mode int32    `json:"mode"`
	Path []pathPt `json:"path,omitempty"`
}

type out struct {
	Side float64 `json:"side"` // 场景边长(厘米)
	Res  int32   `json:"res"`  // scene_res_cfg_id
	Src  string  `json:"src"`  // 源 pcap 名,便于追溯
	Pkts []pkt   `json:"pkts"`
}

func main() {
	outPath := flag.String("out", "", "输出 JSON 路径(默认标准输出)")
	flag.Parse()
	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "用法: movean [-out f.json] <pcap> [pcap...]")
		os.Exit(2)
	}
	db, err := gamedata.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "加载名称库失败:", err)
		os.Exit(1)
	}

	var all []out
	for _, f := range files {
		o, err := extract(db, f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", f, err)
			continue
		}
		if len(o.Pkts) == 0 {
			fmt.Fprintf(os.Stderr, "%s: 无可用移动包(该场景无底图?)\n", f)
			continue
		}
		all = append(all, o)
		report(o)
	}
	if len(all) == 0 {
		fmt.Fprintln(os.Stderr, "没有任何可用数据")
		os.Exit(1)
	}

	var w *os.File = os.Stdout
	if *outPath != "" {
		w, err = os.Create(*outPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "创建输出文件失败:", err)
			os.Exit(1)
		}
		defer w.Close()
	}
	// 单文件时直接导出该份;多文件时每份分开跑(各场景 side 可能不同,合并无意义)。
	enc := json.NewEncoder(w)
	enc.SetIndent("", "")
	if len(all) == 1 {
		if err := enc.Encode(all[0]); err != nil {
			fmt.Fprintln(os.Stderr, "写 JSON 失败:", err)
			os.Exit(1)
		}
		return
	}
	if err := enc.Encode(all); err != nil {
		fmt.Fprintln(os.Stderr, "写 JSON 失败:", err)
		os.Exit(1)
	}
}

func extract(db *gamedata.DB, file string) (out, error) {
	eng := capture.NewEngine(8195)
	go func() {
		if err := eng.RunOffline(file); err != nil {
			fmt.Fprintf(os.Stderr, "%s: 回放失败: %v\n", file, err)
		}
	}()

	var o out
	o.Src = file
	var res int32
	var t0 float64
	first := true
	for m := range eng.Out {
		if m.Opcode != scene.OpSceneMoveReq {
			continue
		}
		mr, ok := scene.ParseMoveReq(m.AppBody)
		if !ok {
			continue
		}
		if res == 0 {
			res = db.DefaultSceneRes(mr.SceneCfgID)
		}
		o.Res = res
		mi, hasMap := db.MapInfo(uint32(res))
		if !hasMap || mi.Side == 0 {
			continue
		}
		o.Side = float64(mi.Side)
		u, v, ok := db.Project(uint32(res), mr.Pos.X, mr.Pos.Y)
		if !ok {
			continue
		}
		ts := float64(m.Time.UnixNano()) / 1e9
		if first {
			t0, first = ts, false
		}
		p := pkt{
			T:    round(ts-t0, 3),
			U:    round(u, 9),
			V:    round(v, 9),
			Stop: mr.StopMove,
			Mode: mr.MoveMode,
		}
		if !mr.StopMove {
			p.VU = round(float64(mr.Speed.X)/float64(mi.Side), 12)
			p.VV = round(float64(mr.Speed.Y)/float64(mi.Side), 12)
		}
		if mr.SegSpan() >= minSegSpan {
			pts := make([]pathPt, 0, len(mr.Segs)+1)
			lastT := -math.MaxFloat64
			for _, sg := range mr.Segs {
				su, sv, ok := db.Project(uint32(res), sg.Pos.X, sg.Pos.Y)
				if !ok {
					continue
				}
				st := round(float64(sg.TimeStamp)/1000-t0, 3)
				if st <= lastT { // 时间戳非严格递增(相邻采样同毫秒)时强制单调,否则真值插值会除零
					st = lastT + 0.001
				}
				lastT = st
				pts = append(pts, pathPt{st, round(su, 9), round(sv, 9)})
			}
			// buildPos 会把 to_pos 补作轨迹末点;它比末个 seg 略新,故强制不早于末 seg
			if len(pts) > 0 {
				pt := round(ts-t0, 3)
				if pt <= lastT {
					pt = lastT + 0.05
				}
				pts = append(pts, pathPt{pt, round(u, 9), round(v, 9)})
			}
			if len(pts) >= 2 {
				p.Path = pts
			}
		}
		o.Pkts = append(o.Pkts, p)
	}
	return o, nil
}

func round(x float64, digits int) float64 {
	p := math.Pow(10, float64(digits))
	return math.Round(x*p) / p
}

// report 打一份数据画像:判断这份抓包覆盖了哪些移动特征,够不够拿来调参。
func report(o out) {
	pk := o.Pkts
	if len(pk) == 0 {
		return
	}
	speedOf := func(p pkt) float64 { return math.Hypot(p.VU, p.VV) * o.Side } // cm/s
	var gaps []float64
	for i := 1; i < len(pk); i++ {
		gaps = append(gaps, pk[i].T-pk[i-1].T)
	}
	q := func(xs []float64, p float64) float64 {
		if len(xs) == 0 {
			return 0
		}
		s := make([]float64, len(xs))
		copy(s, xs)
		sort.Float64s(s)
		return s[int(p*float64(len(s)-1))]
	}
	stops := 0
	var maxV float64
	modes := map[int32]int{}
	withPath := 0
	for _, p := range pk {
		if p.Stop {
			stops++
		}
		if v := speedOf(p); v > maxV {
			maxV = v
		}
		modes[p.Mode]++
		if len(p.Path) >= 2 {
			withPath++
		}
	}
	// stop 之后的沉默时长:玩家可能已在跑,但没有任何包到达 —— 这是前端无从得知的固有延迟
	var silence []float64
	for i, p := range pk {
		if !p.Stop {
			continue
		}
		if i+1 < len(pk) && pk[i+1].Stop {
			continue // 连发 stop 不算
		}
		if i+1 < len(pk) {
			silence = append(silence, pk[i+1].T-p.T)
		}
	}
	fmt.Fprintf(os.Stderr, "%s: %d 包 / %.1fs  间隔中位 %.2fs p95 %.2fs | stop %d (%.0f%%) | 带轨迹 %d | 峰值 %.0f cm/s | 模式 ",
		o.Src, len(pk), pk[len(pk)-1].T-pk[0].T, q(gaps, .5), q(gaps, .95),
		stops, 100*float64(stops)/float64(len(pk)), withPath, maxV)
	ks := make([]int, 0, len(modes))
	for k := range modes {
		ks = append(ks, int(k))
	}
	sort.Ints(ks)
	for _, k := range ks {
		fmt.Fprintf(os.Stderr, "%d×%d ", k, modes[int32(k)])
	}
	if len(silence) > 0 {
		// silence 按时间序排列,不是排序的 —— 取最大值要遍历,不能取末元素
		var mx float64
		for _, s := range silence {
			if s > mx {
				mx = s
			}
		}
		fmt.Fprintf(os.Stderr, "| stop 后沉默 n=%d 中位 %.2fs p95 %.2fs 最大 %.2fs",
			len(silence), q(silence, .5), q(silence, .95), mx)
	}
	fmt.Fprintln(os.Stderr)
}

var _ = time.Now
