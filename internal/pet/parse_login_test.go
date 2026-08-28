package pet

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

// buildVitemInfo 拼 vitem_info 消息:field3=82 槽 vitem_list,field4=81 槽 liabilities_num
// (真实 wire 字段号比 all.pb 描述符偏移 +2;槽数按 2026-08 线上实际值,解析不应依赖具体槽数)。
func buildVitemInfo(list []uint64) []byte {
	add := func(b []byte, n protowire.Number, v uint64) []byte {
		return protowire.AppendVarint(protowire.AppendTag(b, n, protowire.VarintType), v)
	}
	var b []byte
	for _, v := range list {
		b = add(b, 3, v)
	}
	for i := 0; i < 81; i++ {
		b = add(b, 4, uint64(i))
	}
	return b
}

// buildLoginBody 拼登录 body:{ #2: LoginData{ #1: base{ #N: vitem_info } } }。
func buildLoginBody(vitem []byte) []byte {
	base := protowire.AppendBytes(protowire.AppendTag(nil, 1, protowire.BytesType),
		protowire.AppendVarint(protowire.AppendTag(nil, 1, protowire.VarintType), 10001))
	base = protowire.AppendBytes(protowire.AppendTag(base, 7, protowire.BytesType), vitem)
	data := protowire.AppendBytes(protowire.AppendTag(nil, 1, protowire.BytesType), base)
	return protowire.AppendBytes(protowire.AppendTag(nil, 2, protowire.BytesType), data)
}

func TestParseLoginCoins(t *testing.T) {
	// 实测 vitem_list:下标 1 = 金币 19517164,其余用真实值填充(下标 3/7/10/17 等)。
	list := make([]uint64, 82)
	real := map[int]uint64{1: 19517164, 3: 59, 7: 65380, 10: 50, 13: 3, 16: 62, 17: 5855, 20: 3, 46: 108, 50: 206, 51: 2770, 52: 5, 53: 1, 66: 1, 67: 1, 71: 575, 72: 1150, 74: 8, 78: 436, 81: 2318}
	for i, v := range real {
		list[i] = v
	}
	coins, ok := ParseLoginCoins(buildLoginBody(buildVitemInfo(list)))
	if !ok {
		t.Fatalf("期望解析成功,实际 ok=false")
	}
	if coins != 19517164 {
		t.Fatalf("期望金币 19517164,实际 %d", coins)
	}
}

func TestParseLoginCoinsRejectsSmallArray(t *testing.T) {
	// vitem_list 不足 60 槽(vitem_info 特征不满足)时不应误判为命中。
	vitem := buildVitemInfo([]uint64{0, 19517164})
	body := protowire.AppendBytes(protowire.AppendTag(nil, 2, protowire.BytesType),
		protowire.AppendBytes(protowire.AppendTag(nil, 1, protowire.BytesType),
			protowire.AppendBytes(protowire.AppendTag(nil, 7, protowire.BytesType), vitem)))
	if coins, ok := ParseLoginCoins(body); ok {
		t.Fatalf("期望解析失败,实际 coins=%d ok=true", coins)
	}
}
