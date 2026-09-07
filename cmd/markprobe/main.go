// markprobe 从 pcap 里抽「技能施放 → 目标 buff 变化」的对应关系,
// 供 scripts/gen_markids.py 建立 buff_id → 印记/状态名 的映射。
//
// 为什么需要它:协议里印记是数字 buff_id,而 roco-calculator 的印记 id 是字符串
// slug,两者对不上。唯一的桥是「技能描述说获得 N 层 X,实际就出现了层数为 N 的
// buff」—— 本程序把这条线索抽成 TSV,交给生成脚本去配层数。
//
// 用法: go run ./cmd/markprobe -pcap <文件> > /tmp/marks.tsv
// 输出: skill_id / target_pet / buff_id / stack(TSV,表头一行)
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/whoisnian/rocom-capture/internal/capture"
	"github.com/whoisnian/rocom-capture/internal/shanyao"
	"github.com/whoisnian/rocom-capture/internal/wire"
)

// 0x1324 perform_info 的字段号(与 internal/shanyao/parse.go 同源):
//   skill_cast(3){caster_id(1) target_id(2) skill_id(3)}
//   buff_change(4){target_id(2) buff_id(3) buff_info(8){buff_id(2) stack(4)}}
const (
	fPerformCmd   = 1
	fPerformInfo  = 2
	fSkillCast    = 3
	fSCTarget     = 2
	fSCSkillID    = 3
	fPIBuffChange = 4
	fBCTarget     = 2
	fBCBuffID     = 3
	fBCInfo       = 8
	fBuffID       = 2
	fBuffStack    = 4
)

func replay(path string, port int) <-chan capture.Message {
	eng := capture.NewEngine(port)
	go func() {
		if err := eng.RunOffline(path); err != nil {
			fmt.Fprintln(os.Stderr, "回放失败:", err)
		}
	}()
	return eng.Out
}

func main() {
	pcapPath := flag.String("pcap", "", "pcap 文件路径")
	port := flag.Int("port", 8195, "游戏服务器端口")
	flag.Parse()
	if *pcapPath == "" {
		flag.Usage()
		os.Exit(2)
	}
	fmt.Println("skill_id\ttarget_pet\tbuff_id\tstack")
	for m := range replay(*pcapPath, *port) {
		if m.Opcode != shanyao.OpBattlePerformStartNotify {
			continue
		}
		cmd := wire.SubMsg(m.AppBody, fPerformCmd)
		if cmd == nil {
			continue
		}
		// 技能施放与 buff 变化常落在**不同的 perform_info** 里(一段演出一段),
		// 故先按整条消息收集「目标 → 技能」,再给 buff 变化找归属。
		castOf := map[uint64]uint64{}
		for _, pi := range wire.Subs(cmd, fPerformInfo) {
			for _, sc := range wire.Subs(pi, fSkillCast) {
				t, _ := wire.Varint(sc, fSCTarget)
				if s, ok := wire.Varint(sc, fSCSkillID); ok && t != 0 {
					castOf[t] = s
				}
			}
		}
		for _, pi := range wire.Subs(cmd, fPerformInfo) {
			for _, bc := range wire.Subs(pi, fPIBuffChange) {
				t, _ := wire.Varint(bc, fBCTarget)
				id, okID := wire.Varint(bc, fBCBuffID)
				var st uint64
				if info := wire.SubMsg(bc, fBCInfo); info != nil {
					if v, ok := wire.Varint(info, fBuffStack); ok {
						st = v
					}
					if !okID {
						id, okID = wire.Varint(info, fBuffID)
					}
				}
				if !okID || t == 0 {
					continue
				}
				// skill_id 为 0 = 该段演出里没找到打到这个目标的技能(归属不明,
				// 生成脚本会跳过这些行)。
				fmt.Printf("%d\t%d\t%d\t%d\n", castOf[t], t, id, st)
			}
		}
	}
}
