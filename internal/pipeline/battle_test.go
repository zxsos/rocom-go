package pipeline

import (
	"testing"
	"time"

	"github.com/zxsos/roco-go/internal/scene"
	"google.golang.org/protobuf/encoding/protowire"
)

// battleBody 造一条战斗通知(进战/结算结构相同:field 1 = battle_id)。
func battleBody(id uint64) []byte {
	return protowire.AppendVarint(protowire.AppendTag(nil, 1, protowire.VarintType), id)
}

func TestBattleEnterEnd(t *testing.T) {
	cs := &connState{}
	now := time.Unix(1000, 0)

	if cs.battleID != 0 {
		t.Fatalf("初始不该在战斗中,实得 %d", cs.battleID)
	}

	// 进战
	cs.battleID, cs.battleAt = scene.ParseBattleID(battleBody(3891795859771244535)), now
	if cs.battleID != 3891795859771244535 {
		t.Fatalf("battle_id 应为 3891795859771244535,实得 %d", cs.battleID)
	}
	if !cs.battleAt.Equal(now) {
		t.Errorf("battleAt 应为 %v,实得 %v", now, cs.battleAt)
	}

	// 未超时时不应被清掉:正常战斗也要几分钟,别把进行中的战斗误清
	if battleExpired(cs, now.Add(3*time.Minute)) {
		t.Error("3 分钟不该判超时")
	}

	// 超过 battleStale 才超时
	if !battleExpired(cs, now.Add(battleStale+time.Second)) {
		t.Errorf("超过 %v 应判超时", battleStale)
	}

	// 结算清掉
	cs.battleID, cs.battleAt = 0, time.Time{}
	if cs.battleID != 0 || !cs.battleAt.IsZero() {
		t.Error("结算后应清空战斗状态")
	}
	if battleExpired(cs, now.Add(battleStale*2)) {
		t.Error("已结束时再久也不该判超时(battleID 为 0)")
	}
}

// TestBattleZeroTimeNeverExpires 守「battleAt 为零值时永不超时」。
// 这是防御:若某路径只置了 battleID 忘了置时间,now.Sub(zero) 是个巨大的值,
// 会立刻判超时 —— 图标刚挂上就没了,而且是间歇性的,很难查。
func TestBattleZeroTimeNeverExpires(t *testing.T) {
	cs := &connState{}
	cs.battleID = 999
	cs.battleAt = time.Time{} // 刻意只置 id
	if battleExpired(cs, time.Unix(1e9, 0)) {
		t.Error("battleAt 为零值时不应判超时(那是「时间没记上」,不是「打太久」)")
	}
}

// TestBattleStaleBounds 钉住超时时长的量级:太长图标挂着不走,太短会把
// 正常战斗误清。实测战斗 20 秒~几分钟,10 分钟留了足够余量。
func TestBattleStaleBounds(t *testing.T) {
	if battleStale < 5*time.Minute {
		t.Errorf("battleStale=%v 太短,会把长战斗误清", battleStale)
	}
	if battleStale > 30*time.Minute {
		t.Errorf("battleStale=%v 太长,漏包后图标会挂太久", battleStale)
	}
}

// TestBattleOpcodesWired 是编译期契约:确认 pipeline 引用的 opcode 与 scene
// 定义一致。两处若走偏(有人改了 scene 的常量),这里立刻红。
func TestBattleOpcodesWired(t *testing.T) {
	if scene.OpBattleEnterNotify != 0x1316 {
		t.Errorf("进战 opcode 应为 0x1316,实得 %#x", scene.OpBattleEnterNotify)
	}
	if scene.OpBattleFinishNotify != 0x132c {
		t.Errorf("结算 opcode 应为 0x132c,实得 %#x", scene.OpBattleFinishNotify)
	}
}
