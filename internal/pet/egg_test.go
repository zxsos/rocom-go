package pet

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

// eggBagItem 拼一件带 egg_data 的 BagItem:gid(1)/id(2)/update_time(4)/type(14)/egg_data(15)。
func eggBagItem(gid, id uint32, updated int32, brief []byte) []byte {
	b := protowire.AppendVarint(protowire.AppendTag(nil, 1, protowire.VarintType), uint64(gid))
	b = protowire.AppendVarint(protowire.AppendTag(b, 2, protowire.VarintType), uint64(id))
	b = protowire.AppendVarint(protowire.AppendTag(b, 4, protowire.VarintType), uint64(uint32(updated)))
	b = protowire.AppendVarint(protowire.AppendTag(b, 14, protowire.VarintType), EggItemType)
	return protowire.AppendBytes(protowire.AppendTag(b, 15, protowire.BytesType), brief)
}

// eggBrief 拼 PetEggBrief:conf_id(1)/height(2)/weight(3)/hatched(4)/update(5)/max(6)/start(9)/src(10)。
func eggBrief(conf uint32, h, w, hatched, update, max, start, src int32) []byte {
	add := func(b []byte, n protowire.Number, v uint64) []byte {
		return protowire.AppendVarint(protowire.AppendTag(b, n, protowire.VarintType), v)
	}
	b := add(nil, 1, uint64(conf))
	b = add(b, 2, uint64(uint32(h)))
	b = add(b, 3, uint64(uint32(w)))
	b = add(b, 4, uint64(uint32(hatched)))
	b = add(b, 5, uint64(uint32(update)))
	b = add(b, 6, uint64(uint32(max)))
	b = add(b, 9, uint64(uint32(start)))
	return add(b, 10, uint64(uint32(src)))
}

func TestParseChangedEggs(t *testing.T) {
	// ret_info(1).goods_change_info(4).changes(1).bag_item(4)
	item := eggBagItem(3093, 107028, 1786770791, eggBrief(3062001, 59, 21957, 0, 0, 57600, 0, 6))
	chg := protowire.AppendBytes(protowire.AppendTag(nil, 1, protowire.BytesType),
		protowire.AppendBytes(protowire.AppendTag(nil, 4, protowire.BytesType), item))
	ret := protowire.AppendBytes(protowire.AppendTag(nil, 4, protowire.BytesType), chg)
	body := protowire.AppendBytes(protowire.AppendTag(nil, 1, protowire.BytesType), ret)

	eggs := ParseChangedEggs(body)
	if len(eggs) != 1 {
		t.Fatalf("蛋数 = %d, want 1", len(eggs))
	}
	e := eggs[0]
	if e.Gid != 3093 || e.ItemID != 107028 || e.ConfID != 3062001 ||
		e.Height != 59 || e.Weight != 21957 || e.MaxSec != 57600 || e.Src != 6 {
		t.Errorf("蛋 = %+v", e)
	}
	if e.UpdateTime != 1786770791 {
		t.Errorf("获得时间 = %d", e.UpdateTime)
	}
	if e.Hatching() {
		t.Error("start_hatch_time=0 不该算在孵")
	}
	// 非蛋物品(无 egg_data)不该混进来
	plain := protowire.AppendVarint(protowire.AppendTag(nil, 1, protowire.VarintType), 5)
	if got := ParseChangedEggs(plain); len(got) != 0 {
		t.Errorf("非蛋消息解出了 %d 颗", len(got))
	}
}

func TestParseBagEggsPaging(t *testing.T) {
	// bag_info(4).item_list(3){type(1), items(2)};另放一组非蛋类型确认被整组跳过。
	eggList := protowire.AppendVarint(protowire.AppendTag(nil, 1, protowire.VarintType), EggItemType)
	eggList = protowire.AppendBytes(protowire.AppendTag(eggList, 2, protowire.BytesType),
		eggBagItem(3017, 310049, 1786408271, eggBrief(0, 22, 1709, 20060, 1786738056, 43200, 1786736669, 0)))
	other := protowire.AppendVarint(protowire.AppendTag(nil, 1, protowire.VarintType), 3)
	other = protowire.AppendBytes(protowire.AppendTag(other, 2, protowire.BytesType),
		eggBagItem(1, 2, 3, eggBrief(1, 1, 1, 0, 0, 0, 0, 0)))

	bag := protowire.AppendBytes(protowire.AppendTag(nil, 3, protowire.BytesType), eggList)
	bag = protowire.AppendBytes(protowire.AppendTag(bag, 3, protowire.BytesType), other)
	body := protowire.AppendVarint(protowire.AppendTag(nil, 2, protowire.VarintType), 3) // total_page
	body = protowire.AppendVarint(protowire.AppendTag(body, 3, protowire.VarintType), 2) // req_page
	body = protowire.AppendBytes(protowire.AppendTag(body, 4, protowire.BytesType), bag)

	eggs, page, total := ParseBagEggs(body)
	if page != 2 || total != 3 {
		t.Errorf("分页 = %d/%d, want 2/3", page, total)
	}
	if len(eggs) != 1 || eggs[0].Gid != 3017 {
		t.Fatalf("蛋 = %+v", eggs)
	}
	if !eggs[0].Hatching() || eggs[0].HatchedSec != 20060 {
		t.Errorf("孵化状态 = %+v", eggs[0])
	}
}

func TestParseCrackEgg(t *testing.T) {
	// c2s 破壳请求:6 字节子头 + egg_gid(1) + select_ball_gid(2)
	req := append([]byte{0xc0, 0x50, 0, 0, 0, 0x21},
		protowire.AppendVarint(protowire.AppendTag(nil, 1, protowire.VarintType), 3083)...)
	req = protowire.AppendVarint(protowire.AppendTag(req, 2, protowire.VarintType), 2092)
	if got := ParseCrackEggReq(req); got != 3083 {
		t.Errorf("egg_gid = %d, want 3083", got)
	}
	rsp := protowire.AppendVarint(protowire.AppendTag(nil, 2, protowire.VarintType), 39322)
	if got := ParseCrackEggRsp(rsp); got != 39322 {
		t.Errorf("hatched_pet_gid = %d, want 39322", got)
	}
}

func TestParseFlowReason(t *testing.T) {
	body := protowire.AppendVarint(protowire.AppendTag(nil, 3, protowire.VarintType), FlowReasonHomeLay)
	if got := ParseFlowReason(body); got != FlowReasonHomeLay {
		t.Errorf("flow_reason = %d, want %d", got, FlowReasonHomeLay)
	}
}

func TestSortEggs(t *testing.T) {
	// 复刻游戏内「品质排序」的键:品类升 → 品质降 → 物品排序号升 → 获得时间降。
	mk := func(name string, order, quality, sortID int32, at int64) *EggView {
		return &EggView{Name: name, TypeOrder: order, Quality: quality, SortID: sortID, ObtainedAt: at}
	}
	eggs := []*EggView{
		mk("普通-新", 100000, 4, 650100, 300),
		mk("异色-小号", 104, 5, 630082, 100),
		mk("普通-旧", 100000, 4, 650100, 200),
		mk("异色-大号", 104, 5, 630105, 500),
		mk("唯一", 101, 5, 640001, 50),
	}
	SortEggs(eggs, "quality", false)
	var got []string
	for _, e := range eggs {
		got = append(got, e.Name)
	}
	want := []string{"唯一", "异色-小号", "异色-大号", "普通-新", "普通-旧"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("品质排序 = %v, want %v", got, want)
		}
	}
	// 获取时间:只看 update_time 降;asc 即游戏里那个反向开关。
	SortEggs(eggs, "obtained", false)
	if eggs[0].Name != "异色-大号" || eggs[len(eggs)-1].Name != "唯一" {
		t.Errorf("时间排序 = %s … %s", eggs[0].Name, eggs[len(eggs)-1].Name)
	}
	SortEggs(eggs, "obtained", true)
	if eggs[0].Name != "唯一" {
		t.Errorf("反向时间排序 = %s", eggs[0].Name)
	}
}

func TestSortHatchingEggs(t *testing.T) {
	// 孵蛋器槽位按入孵时刻升序,与传入的背包次序无关(实测那三颗:背包次序恰是入孵次序的倒序)。
	eggs := []*EggView{
		{Gid: 3104, StartHatch: 1786858404},
		{Gid: 3109, StartHatch: 1786858369},
		{Gid: 3110, StartHatch: 1786855793},
	}
	SortHatchingEggs(eggs)
	want := []uint32{3110, 3109, 3104}
	for i := range want {
		if eggs[i].Gid != want[i] {
			t.Fatalf("槽位顺序 = %d,%d,%d, want %v", eggs[0].Gid, eggs[1].Gid, eggs[2].Gid, want)
		}
	}
}

func TestSortEggsTie(t *testing.T) {
	// 同一时刻入包的两颗同种蛋:所有键都分不出高低,谁在前由排序算法决定——我们复刻的是
	// 客户端那套 Lua table.sort(见 luasort.go 与其对拍测试)。只有两颗时它不会动相等项,
	// 故正反两个方向都应保持传入的背包原始次序(元素更多时会像游戏里那样出现换位)。
	mk := func(gid uint32) *EggView {
		return &EggView{Gid: gid, Name: "神奇的蛋", TypeOrder: 100000, Quality: 4, SortID: 606100, ObtainedAt: 100}
	}
	for _, by := range []string{"quality", "obtained"} {
		for _, asc := range []bool{false, true} {
			eggs := []*EggView{mk(2994), mk(2996)} // 背包次序:2994 在前
			SortEggs(eggs, by, asc)
			if eggs[0].Gid != 2994 || eggs[1].Gid != 2996 {
				t.Errorf("%s asc=%v = %d,%d,应保持背包次序 2994,2996", by, asc, eggs[0].Gid, eggs[1].Gid)
			}
		}
	}
}
