// Package pbdesc 提供 opcode -> protobuf 消息类型的运行时反射能力,供 pcapdump 精确解码。
//
// 数据是 scripts/gen_pbdesc.py 从游戏描述符 all.pb + ProtoCMD.lua 精简出的生成物:
//   - data/proto.desc.gz: FileDescriptorSet(仅保留 opcode 可达的消息/枚举,gzip)
//   - data/opmsg.json:    opcode -> 消息全名(如 786 -> .Next.ZoneGetAllHatchStatusRsp)
//
// 与 internal/pb(protoc 生成的静态结构体,只覆盖宠物相关几个消息)互补:
// 这里用 dynamicpb 动态解析全部协议消息,只用于调试转储,不参与线上解析路径。
package pbdesc

import (
	"compress/gzip"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

//go:embed data/proto.desc.gz data/opmsg.json
var dataFS embed.FS

// DB 是加载好的描述符集与 opcode 映射。
type DB struct {
	files *protoregistry.Files
	opMsg map[uint16]string
}

// Load 解压并解析内嵌描述符。
func Load() (*DB, error) {
	f, err := dataFS.Open("data/proto.desc.gz")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(raw, &fds); err != nil {
		return nil, err
	}
	files, err := protodesc.NewFiles(&fds)
	if err != nil {
		return nil, err
	}

	blob, err := dataFS.ReadFile("data/opmsg.json")
	if err != nil {
		return nil, err
	}
	var m map[string]string
	if err := json.Unmarshal(blob, &m); err != nil {
		return nil, err
	}
	opMsg := make(map[uint16]string, len(m))
	for k, v := range m {
		if op, err := strconv.ParseUint(k, 10, 16); err == nil {
			opMsg[uint16(op)] = v
		}
	}
	return &DB{files: files, opMsg: opMsg}, nil
}

// MessageName 返回 opcode 对应的消息全名(带前导点),未知返回空串。
func (db *DB) MessageName(op uint16) string { return db.opMsg[op] }

// Find 按消息全名(可省略前导点)查描述符。
func (db *DB) Find(name string) (protoreflect.MessageDescriptor, error) {
	if name == "" {
		return nil, fmt.Errorf("pbdesc: 消息名为空")
	}
	if name[0] == '.' {
		name = name[1:]
	}
	d, err := db.files.FindDescriptorByName(protoreflect.FullName(name))
	if err != nil {
		return nil, err
	}
	md, ok := d.(protoreflect.MessageDescriptor)
	if !ok {
		return nil, fmt.Errorf("pbdesc: %s 不是消息类型", name)
	}
	return md, nil
}

// FindOp 按 opcode 查消息描述符,未映射或查不到返回 nil。
func (db *DB) FindOp(op uint16) protoreflect.MessageDescriptor {
	md, err := db.Find(db.opMsg[op])
	if err != nil {
		return nil
	}
	return md
}

// New 按描述符创建可解码的动态消息。
func New(md protoreflect.MessageDescriptor) *dynamicpb.Message { return dynamicpb.NewMessage(md) }
