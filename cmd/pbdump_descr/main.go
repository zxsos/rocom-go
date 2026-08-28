// 临时工具:从 pbdesc 描述符导出 handbook 相关消息的字段号(调试用,不提交)。
package main

import (
	"fmt"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/whoisnian/rocom-capture/internal/pbdesc"
)

func main() {
	db, err := pbdesc.Load()
	if err != nil {
		panic(err)
	}
	names := []string{
		".Next.ZoneLoginRsp",
		".Next.PlayerPetInfo",
		".Next.HandBook",
		".Next.HandBookRecordCollection",
		".Next.HandBookRecord",
		".Next.GlassInfo",
	}
	for _, n := range names {
		md, err := db.Find(n)
		if err != nil {
			fmt.Printf("== %s: <未找到>\n", n)
			continue
		}
		fmt.Printf("== %s\n", n)
		fields := md.Fields()
		for i := 0; i < fields.Len(); i++ {
			f := fields.Get(i)
			kt := ""
			if f.IsList() {
				kt = "[]"
			}
			ft := "?"
			switch f.Kind() {
			case protoreflect.MessageKind:
				ft = "." + string(f.Message().FullName())
			case protoreflect.EnumKind:
				ft = "." + string(f.Enum().FullName())
			default:
				ft = f.Kind().String()
			}
			fmt.Printf("  #%-2d %s%s %s%s\n", f.Number(), f.Name(), kt, ft, f.Cardinality())
		}
		_ = db
	}
}
