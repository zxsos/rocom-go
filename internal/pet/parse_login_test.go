package pet

import (
	"strings"
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
	// 实测 vitem_list:下标 1 = 洛克贝 19517164,其余用真实值填充(下标 3/7/10/17 等)。
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
		t.Fatalf("期望洛克贝 19517164,实际 %d", coins)
	}
}

// realAvatarURL / realAvatarURLQQ 是实测登录回包里的 plat_avatar_url(见 0x0102.md):
// 微信渠道 https(末段 132)与 QQ 渠道 http(末段 100)各一条,域名、末段尺寸位都是线上真实形态。
const (
	realAvatarURL = "https://thirdwx.qlogo.cn/mmopen/vi_32/WQeT2Lics9YRUhcrGicibpbPbvtK7Pqr5LFX1jWsG2rPXK1DLa6NujQdYLqmzkxWGvl5XDMtK5Xey47IJlGAvm14lWfNsvd2QXicNOehwH9BBBo/132"
	realAvatarURLQQ = "http://thirdqq.qlogo.cn/ek_qqapp/AQMD9W5crlDB8liaj3YHgPHqtIVzZBnrR7OibvFicaiaCZTfTx4Ny56xnjZrvfKcYibPSaLVIJ9Ry8rOn9rrgdKu5GhKE5PfppZ3Ek64GKiazMlsyMwia8k6Gk/100"
)

// buildLoginAvatarBody 拼登录 body:{ #2: LoginData{ #1: base{ #7: addi{ #3: <payload> } } } }。
// 解析按特征定位而非字段号(真实 wire 与描述符有版本偏移),故这里字段号随便取 ——
// 只要嵌套在 bytes 里就该被 Walk 翻到。base 内另塞昵称 bytes,确认它不会被误当 URL。
func buildLoginAvatarBody(payload string) []byte {
	base := protowire.AppendBytes(protowire.AppendTag(nil, 1, protowire.BytesType), []byte("示例玩家"))
	addi := protowire.AppendBytes(protowire.AppendTag(nil, 3, protowire.BytesType), []byte(payload))
	base = protowire.AppendBytes(protowire.AppendTag(base, 7, protowire.BytesType), addi)
	data := protowire.AppendBytes(protowire.AppendTag(nil, 1, protowire.BytesType), base)
	return protowire.AppendBytes(protowire.AppendTag(nil, 2, protowire.BytesType), data)
}

func TestParseLoginAvatar(t *testing.T) {
	for name, url := range map[string]string{
		"微信渠道 https": realAvatarURL,
		"QQ 渠道 http":  realAvatarURLQQ,
	} {
		got, ok := ParseLoginAvatar(buildLoginAvatarBody(url))
		if !ok {
			t.Fatalf("%s: 期望解析成功,实际 ok=false", name)
		}
		if got != url {
			t.Fatalf("%s: 期望 %q,实际 %q", name, url, got)
		}
	}
}

func TestParseLoginAvatarRejects(t *testing.T) {
	long := "https://thirdwx.qlogo.cn/mmopen/vi_32/" + strings.Repeat("a", 80)
	cases := []struct {
		name    string
		payload string
	}{
		{"无 URL", "示例玩家"},
		{"非白名单 http 域名", strings.Replace(long, "https://thirdwx.qlogo.cn", "http://thirdwx.example.com", 1)},
		{"资料页名片照非头像", "https://photo-prod.nrc.qq.com/100000002/card/1000000021785331836626"},
		{"过短", "https://a.cn"},
		{"含空格", strings.Replace(long, "mmopen", "mm open", 1)},
		{"含换行(日志注入)", long + "\nSetAccountAvatar 失败"},
		{"含非 ASCII", long + "头像"},
		{"超长", "https://thirdwx.qlogo.cn/" + strings.Repeat("a", 600)},
	}
	for _, c := range cases {
		if got, ok := ParseLoginAvatar(buildLoginAvatarBody(c.payload)); ok {
			t.Errorf("%s: 期望解析失败,实际 ok=true url=%q", c.name, got)
		}
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
