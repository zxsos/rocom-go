package scene

import "testing"

// 用真实抓到的字段布局构造消息:0x1316/0x132c 的 field 1 是 battle_id(varint),
// 后面跟着各种字段(定长/变长/嵌套都混着),解析必须能跳过它们找到 field 1。
func TestParseBattleID(t *testing.T) {
	// 构造:field1(varint) + field2(定长64) + field3(嵌套 bytes) + field4(varint)
	// 首字段取真实抓到的值 3891795859771244535(varint: f7 ff 80 80 f0 f6 9b 81 36)
	body := []byte{}
	body = append(body, 0x08, 0xf7, 0xff, 0x80, 0x80, 0xf0, 0xf6, 0x9b, 0x81, 0x36) // 1
	body = append(body, 0x11, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08)       // 2: fixed64
	body = append(body, 0x1a, 0x03, 0x08, 0x96, 0x01)                               // 3: bytes{1:150}
	body = append(body, 0x20, 0x05)                                                 // 4: varint 5

	const want = uint64(3891795859771244535)
	if got := ParseBattleID(body); got != want {
		t.Errorf("battle_id 应为 %d,实得 %d", want, got)
	}
}

func TestParseBattleIDFieldOrder(t *testing.T) {
	// battle_id 不在开头:前面有其他字段,仍要能找到
	body := []byte{}
	body = append(body, 0x10, 0x01)                   // 2: varint
	body = append(body, 0x1a, 0x02, 0x08, 0x01)       // 3: bytes
	body = append(body, 0x08, 0x2a)                   // 1: 42
	body = append(body, 0x25, 0x00, 0x00, 0x00, 0x00) // 4: fixed32
	if got := ParseBattleID(body); got != 42 {
		t.Errorf("field 1 不在开头时也应解出 42,实得 %d", got)
	}
}

func TestParseBattleIDMalformed(t *testing.T) {
	// 截断 / 空 / 非法 wire type:一律返回 0,不能 panic(抓包里什么都可能来)
	for name, b := range map[string][]byte{
		"空":          {},
		"只有tag":      {0x08},
		"varint截断":   {0x08, 0xff},
		"非法wiretype": {0x0f},
	} {
		if got := ParseBattleID(b); got != 0 {
			t.Errorf("%s: 应返回 0,实得 %d", name, got)
		}
	}
}

func TestParseBattleIDWrongWireType(t *testing.T) {
	// field 1 不是 varint(这里是 fixed64, wire type 1):必须按类型**跳过**它继续找,
	// 而不是把定长那 8 字节当 varint 读 —— 那会解出一个纯属巧合的数。
	// 布局:1(fixed64) + 2(varint) + 1(varint,真正的 battle_id=7)
	body := []byte{}
	body = append(body, 0x09, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08) // 1: fixed64
	body = append(body, 0x10, 0x2a)                                           // 2: varint 42
	body = append(body, 0x08, 0x07)                                           // 1: varint 7
	if got := ParseBattleID(body); got != 7 {
		t.Errorf("field 1 为定长时应跳过并取到后面的 varint 7,实得 %d", got)
	}

	// 只有定长版 field 1、后面没有 varint 版:应返回 0,不得拿定长字节凑数
	if got := ParseBattleID(body[:9]); got != 0 {
		t.Errorf("只有定长 field 1 时应返回 0,实得 %d", got)
	}
}
