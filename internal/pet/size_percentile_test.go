package pet

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// 本文件守的是**体形百分位的舍入精度**:它够不够细由数据本身决定,不能拍脑袋。
//
// 为什么要紧:奖牌四件套按百分位**阈值**判(大块头 98、小不点 2,见 pipeline/wildpets.go
// 与 store/query.go),而百分位是从**整数体重**反算的 —— cur 只能是 lo+k(单位 0.001 kg),
// 可达值只落在间距 100/(high-low) 的格点上。舍入窗口一旦宽过「边界外最近那个格点到边界的
// 距离」,就会把格点挪过阈值:
//
//	3 位(半径 0.0005pp)→ 12 条形态记录跨界:迷嶂布莱克 866.361 kg = 97.999505%,
//	  舍成 98.000 被判成大块头 —— 而它离真边界(866.362 kg)还差 1 克。
//	2 位(0.005pp)→ 257 条跨界。
//	4 位(0.00005pp)→ 全量 1147 条形态记录零跨界,最紧的一处仍留 6.9 倍余量。
//
// 将来新赛季加入区间更窄的形态时,这两个用例会直接报红提示提高位数 ——
// 而不是悄悄在图上描一个不该有的红圈。

// namesFile 是形态区间与奖牌窗口的原始数据(仓库内提交的生成物,与 embed 的那份同源)。
//
// 读原始 JSON 而不走 gamedata.Load():要的是**全量**形态,而 gamedata 只暴露按 id 查询
// (PetBase/PetBaseOf),没有遍历接口 —— 不为一条测试加生产 API。
var namesFile = filepath.Join("..", "gamedata", "data", "names.json")

type rawNames struct {
	PetBase map[string]struct {
		N  string `json:"n"`
		WL uint32 `json:"wl"` // 体重下限(0.001 kg)
		WH uint32 `json:"wh"` // 体重上限
	} `json:"petbase"`
	SizeMedals []struct {
		N  string `json:"n"`
		D  int32  `json:"d"`  // 判定维度:2=体重百分位 3=嗓音
		Lo int    `json:"lo"` // 百分位窗口(含)
		Hi int    `json:"hi"`
	} `json:"size_medals"`
}

func loadRawNames(t *testing.T) rawNames {
	t.Helper()
	b, err := os.ReadFile(namesFile)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", namesFile, err)
	}
	var v rawNames
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("解析 %s 失败: %v", namesFile, err)
	}
	if len(v.PetBase) == 0 || len(v.SizeMedals) == 0 {
		t.Fatalf("%s 里 petbase(%d)或 size_medals(%d)为空", namesFile, len(v.PetBase), len(v.SizeMedals))
	}
	return v
}

// weightWindows 返回体重类奖牌的百分位窗口(嗓音那两枚不经 SizePercentile:voice 是整数
// 原值、阈值也是整数,不存在舍入问题)。
func weightWindows(data rawNames) [][2]int {
	var out [][2]int
	for _, m := range data.SizeMedals {
		if m.D == 2 {
			out = append(out, [2]int{m.Lo, m.Hi})
		}
	}
	return out
}

// nearestOutside 返回窗口边界的**外侧**最近格点:Lo 取下方最近、Hi 取上方最近。
// 只返回确实存在的那侧(0 的下方、100 的上方都不存在)。
// 判定用整数算(k*100 与 edge*R 比),避免浮点把正落在边界上的格点算成外侧。
func nearestOutside(edge, R int, below bool, k int) bool {
	if below {
		return k*100 < edge*R
	}
	return k*100 > edge*R
}

// TestSizePercentileRoundKeepsMedalVerdict 遍历全部形态 × 体重奖牌窗口的**边界邻域**,
// 逐个格点比对「精确判定」与「SizePercentile 舍入后的判定」,要求完全一致。
// 邻域取窗口两端 ±2 个格点:舍入只有可能在这一小段上翻盘。
func TestSizePercentileRoundKeepsMedalVerdict(t *testing.T) {
	data := loadRawNames(t)
	windows := weightWindows(data)

	forms, checks, flips := 0, 0, 0
	for id, base := range data.PetBase {
		if base.WH <= base.WL {
			continue
		}
		forms++
		lo, hi := base.WL, base.WH
		R := int(hi - lo)
		for _, w := range windows {
			for _, edge := range w {
				// 找边界附近的格点(含边界本身):k = edge*R/100 左右各两个。
				center := float64(edge) * float64(R) / 100
				for dk := -2; dk <= 2; dk++ {
					k := int(center) + dk
					if k < 0 || k > R {
						continue
					}
					checks++
					// 精确判定(整数运算):pct = k*100/R 落在 [Lo,Hi] ⇔ k*100 ∈ [Lo*R, Hi*R]。
					exact := k*100 >= w[0]*R && k*100 <= w[1]*R
					p := SizePercentile(float64(lo+uint32(k))/1000, float64(lo)/1000, float64(hi)/1000)
					if p == nil {
						t.Fatalf("%s(%s) 区间 %d~%d 算不出百分位", id, base.N, lo, hi)
					}
					rounded := *p >= float64(w[0]) && *p <= float64(w[1])
					if rounded == exact {
						continue
					}
					flips++
					if flips <= 10 {
						t.Errorf("舍入改变判定:%s(%s,%.3f~%.3f kg) w=%.3f kg → 精确 %.6f%%(命中=%v),"+
							"舍入后 %.4f%%(命中=%v)",
							id, base.N, float64(lo)/1000, float64(hi)/1000,
							float64(lo+uint32(k))/1000, float64(k)*100/float64(R), exact, *p, rounded)
					}
				}
			}
		}
	}
	if flips > 10 {
		t.Errorf("…共 %d 处判定被舍入改变(精度不够,请提高 SizePercentile 的小数位数)", flips)
	}
	t.Logf("遍历 %d 个形态的 %d 个边界邻域点,舍入翻盘 %d 处", forms, checks, flips)
}

// TestSizePercentileBoundaryMargin 把「精度够用」量化成断言:当前位数(4 位)的舍入半径必须
// **小于**任意形态下「窗口外侧最近格点到边界」的距离。半格 0.00005pp vs 实测最紧 0.000345pp,
// 余量约 7 倍。降位数会立刻打破它 —— 这正是要守的。
func TestSizePercentileBoundaryMargin(t *testing.T) {
	const halfWindow = 0.00005 // 4 位小数的舍入半径(0.0001 / 2)
	data := loadRawNames(t)
	windows := weightWindows(data)

	tightest, who := 1e9, ""
	for id, base := range data.PetBase {
		if base.WH <= base.WL {
			continue
		}
		R := int(base.WH - base.WL)
		for _, w := range windows {
			// 两端各自的外侧最近格点:Lo 往小找、Hi 往大找。
			for i, edge := range w {
				below := i == 0
				// 从 floor(edge*R/100) 出发,朝外侧逐个试探(至多两个就够)。
				k := int(float64(edge) * float64(R) / 100)
				hit := -1
				for step := 0; step <= 2 && hit < 0; step++ {
					cand := k - step
					if !below {
						cand = k + step
					}
					if cand < 0 || cand > R {
						continue
					}
					if nearestOutside(edge, R, below, cand) {
						hit = cand
					}
				}
				if hit < 0 {
					continue // 该侧没有格点(0 的下方、100 的上方)
				}
				dist := float64(hit)*100/float64(R) - float64(edge)
				if below {
					dist = -dist
				}
				if dist < tightest {
					tightest, who = dist, fmt.Sprintf("%s(%s) %.3f~%.3f kg", id, base.N,
						float64(base.WL)/1000, float64(base.WH)/1000)
				}
			}
		}
	}
	if tightest <= halfWindow {
		t.Errorf("窗口外侧最近格点距边界仅 %.6fpp(%s),已不大于舍入半径 %.6fpp —— "+
			"该形态的个体会被舍入挪过阈值,请提高 SizePercentile 的精度", tightest, who, halfWindow)
	}
	t.Logf("窗口外侧最近格点距边界 %.6fpp(%s);舍入半径 %.6fpp,余量 %.1f 倍",
		tightest, who, halfWindow, tightest/halfWindow)
}
