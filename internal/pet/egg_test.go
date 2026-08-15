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
