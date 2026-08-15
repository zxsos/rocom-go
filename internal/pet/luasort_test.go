package pet

import (
	"fmt"
	"math/rand"
	"os/exec"
	"strings"
	"testing"
)

// TestLuaSortMatchesLua 与真实的 lua5.4 对拍:同一批(带大量相同键的)元素、同一个比较函数,
// 两边排完的**下标排列**必须逐位一致——精灵蛋页面靠这个复现游戏内那些「键完全相同」的蛋的顺序。
// 机器上没装 lua5.4 就跳过(CI/别人的机器不该因此挂掉)。
func TestLuaSortMatchesLua(t *testing.T) {
	lua, err := exec.LookPath("lua5.4")
	if err != nil {
		t.Skip("没装 lua5.4,跳过对拍")
	}
	rng := rand.New(rand.NewSource(20260815))
	for round := range 60 {
		n := 2 + rng.Intn(40)
		keys := make([]int, n)
		for i := range keys {
			keys[i] = rng.Intn(1 + n/3) // 键的取值范围远小于元素数 ⇒ 大量相等
		}
		desc := round%2 == 0

		// Go 侧:下标 1 起,元素用 id(初始 1..n)标识,排完看 id 的排列。
		ids := make([]int, n)
		ord := make([]int, n)
		for i := range ids {
			ids[i], ord[i] = i+1, keys[i]
		}
		luaSort(n, func(i, j int) bool {
			if desc {
				return ord[i-1] > ord[j-1]
			}
			return ord[i-1] < ord[j-1]
		}, func(i, j int) {
			ids[i-1], ids[j-1] = ids[j-1], ids[i-1]
			ord[i-1], ord[j-1] = ord[j-1], ord[i-1]
		})

		got := make([]string, n)
		for i, id := range ids {
			got[i] = fmt.Sprint(id)
		}
		if want := luaRefSort(t, lua, keys, desc); strings.Join(got, ",") != want {
			t.Fatalf("第 %d 轮(n=%d desc=%v)\n keys=%v\n  go = %s\n lua = %s",
				round, n, desc, keys, strings.Join(got, ","), want)
		}
	}
}

// luaRefSort 用真实 lua5.4 跑一遍同样的排序,返回排完的 id 序列(逗号分隔)。
func luaRefSort(t *testing.T, lua string, keys []int, desc bool) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("local t = {")
	for i, k := range keys {
		fmt.Fprintf(&b, "{id=%d,k=%d},", i+1, k)
	}
	b.WriteString("}\n")
	if desc {
		b.WriteString("table.sort(t, function(a,b) return a.k > b.k end)\n")
	} else {
		b.WriteString("table.sort(t, function(a,b) return a.k < b.k end)\n")
	}
	b.WriteString("local o = {} for i,v in ipairs(t) do o[i] = v.id end print(table.concat(o, ','))")
	out, err := exec.Command(lua, "-e", b.String()).Output()
	if err != nil {
		t.Fatalf("跑 lua5.4 失败: %v", err)
	}
	return strings.TrimSpace(string(out))
}
