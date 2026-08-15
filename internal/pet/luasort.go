package pet

// Lua 5.4 的 table.sort 复刻(ltablib.c 的 auxsort/partition)。
//
// 为什么要复刻:精灵蛋页面的排序是照着客户端 BagModuleData 的比较函数写的(见 SortEggs),
// 但比较函数分不出高低的那些蛋——同一时刻入包的两颗同种蛋,品类/品质/物品排序号/获得时间
// 全都相等——最终排成什么样,完全取决于**排序算法本身**:Lua 的 table.sort 是快排,不稳定,
// 同一批数据换个方向排,相等的两个元素可能就换了位置(玩家实测:品质↑↓与时间↑都是 A、B,
// 只有时间↓是 B、A)。用 Go 的 sort.SliceStable 只能得到「永远保持原序」,与游戏对不上。
//
// 好在 Lua 5.4 的实现是**确定性**的:只有分区严重失衡(`(up-lo)/128 > n`)或区间长于
// RANLIMIT(100)时才会引入随机枢轴,几十颗蛋根本走不到那两条路,故照抄即可逐位复现。
// 与真实 lua5.4 的对拍见 luasort_test.go。
//
// 索引一律 **1 起**(与 C 版一致,省得来回换算),调用方在 less/swap 里减一。

// luaSort 按 Lua 5.4 的 table.sort 排列 n 个元素(less/swap 的下标从 1 起)。
func luaSort(n int, less func(i, j int) bool, swap func(i, j int)) {
	if n > 1 {
		luaAuxsort(1, n, less, swap)
	}
}

// luaAuxsort 即 ltablib.c 的 auxsort(去掉随机枢轴那一支:见文件头说明)。
func luaAuxsort(lo, up int, less func(i, j int) bool, swap func(i, j int)) {
	for lo < up {
		// 先把 a[lo]、a[p]、a[up] 三者排好,取中位数作枢轴。
		if less(up, lo) {
			swap(lo, up)
		}
		if up-lo == 1 {
			return
		}
		p := (lo + up) / 2
		if less(p, lo) {
			swap(p, lo)
		} else if less(up, p) {
			swap(p, up)
		}
		if up-lo == 2 {
			return
		}
		swap(p, up-1) // 枢轴挪到 up-1(partition 的不变式要求它在那儿)
		p = luaPartition(lo, up, less, swap)
		// a[lo .. p-1] <= a[p] <= a[p+1 .. up];短的那半递归,长的那半继续循环。
		if p-lo < up-p {
			luaAuxsort(lo, p-1, less, swap)
			lo = p + 1
		} else {
			luaAuxsort(p+1, up, less, swap)
			up = p - 1
		}
	}
}

// luaPartition 即 ltablib.c 的 partition:枢轴在 a[up-1],返回它最终落到的位置。
// C 版在比较函数不自洽时报「invalid order function」,这里改为就地收手(不 panic)。
func luaPartition(lo, up int, less func(i, j int) bool, swap func(i, j int)) int {
	pivot := up - 1 // 枢轴始终待在这儿:下面的交换只动 [lo, up-2]
	i, j := lo, up-1
	for {
		for i++; less(i, pivot); i++ {
			if i >= up-1 {
				return i
			}
		}
		for j--; j > lo && less(pivot, j); j-- {
		}
		if j < i {
			swap(up-1, i)
			return i
		}
		swap(i, j)
	}
}
