// 解析渲染:把某 opcode 的 AppBody 解码为可读文本,供实时调试页按需查看服务器下发数据。
//
// 两级解码(与 cmd/pcapdump 同源,供服务端 API 复用):
//  1. 精确解码:按 opcode 查内嵌游戏描述符(proto.desc.gz),用 dynamicpb 解出带字段名/枚举名的树;
//  2. 通用 wire 级解码:不依赖 .proto 定义,只按字段编号渲染,自动跳过 c2s 子头、在 tsf4g 尾处停止。
//
// 精确解码失败(描述符对该版本对不上)时自动退回 wire 级,保证总有可读输出。
package pbdesc

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// getDB 惰性加载描述符集(服务生命周期内只加载一次)。
var (
	dbOnce sync.Once
	dbVal  *DB
	dbErr  error
)

func getDB() (*DB, error) {
	dbOnce.Do(func() { dbVal, dbErr = Load() })
	return dbVal, dbErr
}

// Render 把 opcode 的 AppBody 渲染成缩进解析树文本。dir 取 "c2s"/"s2c"
// (仅作起始偏移候选偏好,可空),body 是剥掉 gcp 固定头后的应用层字节。
// 精确解码失败时自动退回通用 wire 级解码;两者都无内容时返回空串。
func Render(op uint16, dir string, body []byte) string {
	var sb strings.Builder
	var md protoreflect.MessageDescriptor
	if db, err := getDB(); err == nil {
		md = db.FindOp(op)
	}
	if md == nil {
		sb.WriteString("# 未映射 opcode,wire 级解码:\n")
		renderWire(body, &sb)
		return sb.String()
	}
	hint := startHintS2C
	if dir == "c2s" {
		hint = startHintC2S
	}
	if typed := decodeTyped(md, body, hint); typed != nil {
		if n := len(body) - typed.start - typed.trailer; n == 0 {
			fmt.Fprintf(&sb, "# %s (空消息):\n", md.FullName())
			return sb.String()
		}
		fmt.Fprintf(&sb, "# %s (起始偏移 %d, 尾部 %dB%s):\n", md.FullName(),
			typed.start, typed.trailer, ifStr(typed.exact, "", ", 边界存疑"))
		renderMsg(typed.msg, 0, &sb)
		return sb.String()
	}
	fmt.Fprintf(&sb, "# %s 解码失败(描述符与该版本不符?),退回 wire 级:\n", md.FullName())
	renderWire(body, &sb)
	return sb.String()
}

func renderWire(body []byte, sb *strings.Builder) {
	start, fields, consumed := decodeAuto(body)
	fmt.Fprintf(sb, "  (起始偏移 %d, 已解码 %d/%dB):\n", start, consumed, len(body))
	renderFields(fields, 1, sb)
	if tr := body[start+consumed:]; len(tr) > 0 {
		fmt.Fprintf(sb, "  trailer(%dB): %s\n", len(tr), hexPreview(tr, 48))
	}
}

// ---- 精确解码 ----

const (
	maxStartProbe = 16 // 头部最多试探多少字节
	maxTailProbe  = 24 // tsf4g 标记之前最多回退多少字节
	// 实测的 proto 起始偏移:s2c 的 internal header 已由 gcp.AppBody 剥净;
	// c2s 还剩 6 字节子头(c0 50 00 00 00 XX)。仅作候选打分的偏好,解不出时照样全试。
	startHintS2C = 0
	startHintC2S = 6
)

type typedResult struct {
	msg     protoreflect.Message
	start   int  // 消息在 AppBody 中的起始偏移
	trailer int  // 尾部剩余字节数
	exact   bool // 回序列化长度与原字节一致(边界判定可信)
	hinted  bool // 起始偏移正好是该方向的常见值
}

// decodeTyped 用给定消息类型解 body,失败返回 nil。hint 是该方向常见的 proto 起始偏移
// (候选打平时优先它:短消息里头部字节常能凑出另一种同样合法的解法)。
func decodeTyped(md protoreflect.MessageDescriptor, body []byte, hint int) *typedResult {
	tail := indexOf(body, []byte("tsf4g"))
	ends := []int{}
	if tail >= 0 {
		for e := tail; e >= tail-maxTailProbe && e >= 0; e-- {
			ends = append(ends, e)
		}
	} else {
		ends = append(ends, len(body))
	}
	var best *typedResult
	for s := 0; s <= min(maxStartProbe, len(body)); s++ {
		for _, e := range ends {
			if e < s {
				continue
			}
			seg := body[s:e]
			m := New(md)
			if err := proto.Unmarshal(seg, m.Interface()); err != nil {
				continue
			}
			if len(m.GetUnknown()) > 0 { // 有未知字段说明边界或类型不对
				continue
			}
			out, err := proto.Marshal(m.Interface())
			exact := err == nil && len(out) == len(seg)
			cur := &typedResult{msg: m, start: s, trailer: len(body) - e, exact: exact, hinted: s == hint}
			if best == nil || better(cur, best, body) {
				best = cur
			}
		}
	}
	return best
}

// better 比较两个候选:空解码垫底(无字段的 REQ 才会落到它),
// 其次看回序列化长度是否一致,再看消费字节数,最后取起始更靠前者。
func better(a, b *typedResult, body []byte) bool {
	al, bl := len(body)-a.start-a.trailer, len(body)-b.start-b.trailer
	if (al == 0) != (bl == 0) {
		return bl == 0
	}
	if a.exact != b.exact {
		return a.exact
	}
	if al != bl {
		return al > bl
	}
	if a.hinted != b.hinted {
		return a.hinted
	}
	return a.start < b.start
}

// renderMsg 把动态消息渲染成 prototext 风格的缩进树(只列已置位的字段)。
func renderMsg(m protoreflect.Message, depth int, sb *strings.Builder) {
	ind := strings.Repeat("  ", depth)
	fields := m.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if !m.Has(fd) {
			continue
		}
		v := m.Get(fd)
		switch {
		case fd.IsMap():
			v.Map().Range(func(k protoreflect.MapKey, mv protoreflect.Value) bool {
				fmt.Fprintf(sb, "%s%s[%s]%s", ind, fd.Name(), k.String(), sep(fd.MapValue()))
				renderValue(fd.MapValue(), mv, depth, sb)
				return true
			})
		case fd.IsList():
			l := v.List()
			for j := 0; j < l.Len(); j++ {
				fmt.Fprintf(sb, "%s%s%s", ind, fd.Name(), sep(fd))
				renderValue(fd, l.Get(j), depth, sb)
			}
		default:
			fmt.Fprintf(sb, "%s%s%s", ind, fd.Name(), sep(fd))
			renderValue(fd, v, depth, sb)
		}
	}
	if unk := m.GetUnknown(); len(unk) > 0 {
		fmt.Fprintf(sb, "%s# 未知字段(%dB):\n", ind, len(unk))
		f, _ := scanProto(unk)
		renderFields(f, depth+1, sb)
	}
}

// sep 是字段名与值之间的分隔:子消息用 prototext 的 `name {`,标量用 `name: v`。
func sep(fd protoreflect.FieldDescriptor) string {
	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return " "
	}
	return ": "
}

func renderValue(fd protoreflect.FieldDescriptor, v protoreflect.Value, depth int, sb *strings.Builder) {
	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		sb.WriteString("{\n")
		renderMsg(v.Message(), depth+1, sb)
		fmt.Fprintf(sb, "%s}\n", strings.Repeat("  ", depth))
	case protoreflect.EnumKind:
		n := v.Enum()
		if ev := fd.Enum().Values().ByNumber(n); ev != nil {
			fmt.Fprintf(sb, "%s(%d)\n", ev.Name(), n)
		} else {
			fmt.Fprintf(sb, "%d\n", n)
		}
	case protoreflect.StringKind:
		fmt.Fprintf(sb, "%q\n", v.String())
	case protoreflect.BytesKind:
		b := v.Bytes()
		if utf8Text(b) {
			fmt.Fprintf(sb, "%q\n", string(b))
		} else {
			fmt.Fprintf(sb, "%s (%dB)\n", hexPreview(b, 64), len(b))
		}
	default:
		fmt.Fprintf(sb, "%s\n", v.String())
	}
}

// utf8Text 判断 bytes 字段是否是可读文本(玩家名/宠物名等常以 bytes 下发)。
func utf8Text(b []byte) bool {
	if len(b) == 0 || !utf8.Valid(b) {
		return false
	}
	for _, r := range string(b) {
		if r < 0x20 && r != '\n' && r != '\t' {
			return false
		}
	}
	return true
}

// ---- 通用 protobuf wire 解码 ----

type wireField struct {
	num  int
	wire int
	v    uint64 // varint / fixed
	data []byte // len-delimited 原始字节
}

// scanProto 从 b[0] 顺序解析字段,遇非法 wire 或截断即停,返回字段与已消费字节数。
func scanProto(b []byte) ([]wireField, int) {
	var out []wireField
	i := 0
	for i < len(b) {
		key, n := binary.Uvarint(b[i:])
		if n <= 0 {
			break
		}
		num, wire := int(key>>3), int(key&7)
		if num == 0 {
			break
		}
		j := i + n
		switch wire {
		case 0:
			v, m := binary.Uvarint(b[j:])
			if m <= 0 {
				return out, i
			}
			out = append(out, wireField{num, wire, v, nil})
			j += m
		case 1:
			if j+8 > len(b) {
				return out, i
			}
			out = append(out, wireField{num, wire, binary.LittleEndian.Uint64(b[j:]), nil})
			j += 8
		case 2:
			ln, m := binary.Uvarint(b[j:])
			if m <= 0 || j+m+int(ln) > len(b) {
				return out, i
			}
			j += m
			out = append(out, wireField{num, wire, 0, b[j : j+int(ln)]})
			j += int(ln)
		case 5:
			if j+4 > len(b) {
				return out, i
			}
			out = append(out, wireField{num, wire, uint64(binary.LittleEndian.Uint32(b[j:])), nil})
			j += 4
		default: // 3/4(group)/7 视为非 protobuf 边界
			return out, i
		}
		i = j
	}
	return out, i
}

// decodeAuto 在前 16 字节内寻找最佳起始偏移(跳过 c2s 子头),返回偏移/字段/消费字节数。
// 评分基于「解码质量」:真实起始解出的都是干净字段(标量、子消息、可见字符串),
// 而错位起始往往把一长段当作无法再解的 bytes blob —— 据此打分,blob 重罚,
// 平手时取「终点更接近 tsf4g 尾、字段更多」者。这比单纯比消费字节数稳健得多。
func decodeAuto(b []byte) (int, []wireField, int) {
	limit := min(16, len(b))
	tail := indexOf(b, []byte("tsf4g"))
	bestStart, bestConsumed := 0, 0
	var bestFields []wireField
	bestQ, bestEnd, bestNF := -1<<30, -1, -1
	for s := 0; s <= limit; s++ {
		f, c := scanProto(b[s:])
		if len(f) == 0 {
			continue
		}
		q := quality(f)
		end := s + c
		nearTail := tail >= 0 && end <= tail // 不越过尾标记者更可信
		better := q > bestQ ||
			(q == bestQ && nearTail && end > bestEnd) ||
			(q == bestQ && end == bestEnd && len(f) > bestNF)
		if better {
			bestStart, bestConsumed, bestFields, bestQ, bestEnd, bestNF = s, c, f, q, end, len(f)
		}
	}
	return bestStart, bestFields, bestConsumed
}

// quality 统计干净字段数减去 blob(无法解为子消息又不可见的 bytes)罚分。
func quality(fields []wireField) int {
	clean, blob := 0, 0
	for _, f := range fields {
		if f.wire != 2 {
			clean++
			continue
		}
		if sub, c := scanProto(f.data); c == len(f.data) && len(sub) > 0 {
			clean++
		} else if printable(f.data) {
			clean++
		} else {
			blob++
		}
	}
	return clean - 2*blob
}

// renderFields 把字段渲染成缩进树;len 字段优先尝试嵌套消息,其次可见字符串,否则 hex。
func renderFields(fields []wireField, depth int, sb *strings.Builder) {
	ind := strings.Repeat("  ", depth)
	for _, f := range fields {
		switch f.wire {
		case 0:
			fmt.Fprintf(sb, "%s#%d: %d\n", ind, f.num, f.v)
		case 1:
			fmt.Fprintf(sb, "%s#%d: 0x%016x (64bit)\n", ind, f.num, f.v)
		case 5:
			fmt.Fprintf(sb, "%s#%d: 0x%08x (32bit)\n", ind, f.num, uint32(f.v))
		case 2:
			if sub, c := scanProto(f.data); c == len(f.data) && len(sub) > 0 {
				fmt.Fprintf(sb, "%s#%d: {  (msg %dB)\n", ind, f.num, len(f.data))
				renderFields(sub, depth+1, sb)
				fmt.Fprintf(sb, "%s}\n", ind)
			} else if printable(f.data) {
				fmt.Fprintf(sb, "%s#%d: %q\n", ind, f.num, string(f.data))
			} else {
				fmt.Fprintf(sb, "%s#%d: %s (%dB)\n", ind, f.num, hexPreview(f.data, 48), len(f.data))
			}
		}
	}
}

// ---- 工具 ----

func hexPreview(b []byte, max int) string {
	s := hex.EncodeToString(b)
	if len(s) > max {
		return "0x" + s[:max] + "…"
	}
	return "0x" + s
}

func indexOf(b, sub []byte) int {
	if len(sub) == 0 || len(sub) > len(b) {
		return -1
	}
	for i := 0; i+len(sub) <= len(b); i++ {
		if string(b[i:i+len(sub)]) == string(sub) {
			return i
		}
	}
	return -1
}

func printable(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

func ifStr(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
