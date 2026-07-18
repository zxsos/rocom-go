// Package wire 提供无 schema 的 protobuf wire 级扫描辅助。
// 线上消息与 all.pb 定义存在版本错位、且 c2s 带子头/尾部 trailer,故各解析器不依赖
// 生成代码,直接按实测字段号在 wire 层取值(见 docs/protocol.md)。
package wire

import "google.golang.org/protobuf/encoding/protowire"

// ScanFields 遍历 b 的顶层字段:varint 字段回调 v,length-delimited 字段回调 val,
// 其余类型跳过。解码出错即静默停止(容忍尾部 tsf4g 等非 protobuf 残留)。
func ScanFields(b []byte, fn func(num protowire.Number, typ protowire.Type, val []byte, v uint64)) {
	rest := b
	for len(rest) > 0 {
		num, typ, n := protowire.ConsumeTag(rest)
		if n < 0 {
			return
		}
		rest = rest[n:]
		switch typ {
		case protowire.VarintType:
			v, m := protowire.ConsumeVarint(rest)
			if m < 0 {
				return
			}
			fn(num, typ, nil, v)
			rest = rest[m:]
		case protowire.BytesType:
			val, m := protowire.ConsumeBytes(rest)
			if m < 0 {
				return
			}
			fn(num, typ, val, 0)
			rest = rest[m:]
		default:
			m := protowire.ConsumeFieldValue(num, typ, rest)
			if m < 0 {
				return
			}
			rest = rest[m:]
		}
	}
}

// SubMsg 返回 b 里首个指定字段号的 length-delimited 值;没有返回 nil。
func SubMsg(b []byte, want protowire.Number) []byte {
	var found []byte
	ScanFields(b, func(num protowire.Number, typ protowire.Type, val []byte, _ uint64) {
		if found == nil && num == want && typ == protowire.BytesType {
			found = val
		}
	})
	return found
}

// Varint 返回 b 里首个指定字段号的 varint 值(无/类型不符则 false)。
func Varint(b []byte, want protowire.Number) (uint64, bool) {
	var out uint64
	var ok bool
	ScanFields(b, func(num protowire.Number, typ protowire.Type, _ []byte, v uint64) {
		if !ok && num == want && typ == protowire.VarintType {
			out, ok = v, true
		}
	})
	return out, ok
}

// Bytes 返回 b 里首个指定字段号的 length-delimited 值(无/类型不符则 false)。
func Bytes(b []byte, want protowire.Number) ([]byte, bool) {
	v := SubMsg(b, want)
	return v, v != nil
}

// Subs 返回 b 里指定字段号的所有 length-delimited 值。
func Subs(b []byte, want protowire.Number) [][]byte {
	var out [][]byte
	ScanFields(b, func(num protowire.Number, typ protowire.Type, val []byte, _ uint64) {
		if num == want && typ == protowire.BytesType {
			out = append(out, val)
		}
	})
	return out
}

// PackedVarints 把 length-delimited 值按 packed repeated varint 展开。
func PackedVarints(b []byte) []uint64 {
	var out []uint64
	for len(b) > 0 {
		v, n := protowire.ConsumeVarint(b)
		if n < 0 {
			return out
		}
		out = append(out, v)
		b = b[n:]
	}
	return out
}

// Walk 深度优先遍历 b 内所有 length-delimited 字段值(先访问、后下钻):
// 消息嵌套路径随版本/通道而异,递归尝试是在未知层级中定位目标子消息的通用手段。
// visit 返回 false 表示该值已被识别(或需要终止),不再深入其内部。
func Walk(b []byte, visit func(sub []byte) bool) {
	rest := b
	for len(rest) > 0 {
		num, typ, n := protowire.ConsumeTag(rest)
		if n < 0 {
			return
		}
		rest = rest[n:]
		if typ == protowire.BytesType {
			v, m := protowire.ConsumeBytes(rest)
			if m < 0 {
				return
			}
			if visit(v) {
				Walk(v, visit)
			}
			rest = rest[m:]
		} else {
			m := protowire.ConsumeFieldValue(num, typ, rest)
			if m < 0 {
				return
			}
			rest = rest[m:]
		}
	}
}
