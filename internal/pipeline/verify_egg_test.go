package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/whoisnian/rocom-capture/internal/gcp"
	"github.com/whoisnian/rocom-capture/internal/pet"
)

// 验证「库里已有蛋」这个常见场景下,登录后能否让前端刷新。
// 这是用户最可能遇到的情况:服务跑了一阵、库里有蛋、玩家重新登录游戏。
func TestVerifyLoginRefreshWithExistingEggs(t *testing.T) {
	p, srv := newTestPipeline(t)
	pop, cancel := srv.Hub().SubscribeForTest()
	defer cancel()
	typed := make(chan string, 64)
	go func() {
		for {
			typ, ok := pop(context.Background())
			if !ok {
				return
			}
			typed <- typ
		}
	}()

	// 预置一颗已在库的蛋,且 hatching 列**已经是 1**。
	//
	// 这一步必须传 knownHatch(而不是让 hatching 保持 0):用户场景里这些蛋早就
	// 标着在孵了,登录时「状态本就一致」→ ReconcileHatching 返回 changed=false
	// → 不会广播,前端也就不知道要重拉 —— 这才是「登录后孵蛋器没刷新」的真实成因。
	// 若预置成 hatching=0,登录时会因订正标记而 changed=true 自然广播,测试就成了
	// 「永远绿灯」(变异测试验证过:去掉登录广播它照样通过)。
	eggs := []*pet.EggView{{
		Gid: 3259, ItemID: 5001, Name: "友爱天天的蛋", Species: "火神",
		MaxSecs: 3600, HatchedSecs: 100, Hatching: true,
	}}
	known := map[uint32]bool{3259: true}
	if err := p.st.For(testAcc).UpsertEggs(eggs, time.Now().Unix(), known); err != nil {
		t.Fatalf("预置蛋: %v", err)
	}
	// 消化预置产生的广播
	drainTyped(typed)

	// 玩家重新登录:登录包带孵蛋器列表(含这颗蛋)
	p.handle(msg(gcp.S2C, pet.OpLoginRsp, loginWithHatchBody(1, "测试", []uint64{3259})))

	// 关键:前端应收到 eggs 通知去重拉
	n := 0
	for {
		select {
		case v := <-typed:
			if v == "eggs" {
				n++
			}
		case <-time.After(120 * time.Millisecond):
			if n == 0 {
				t.Error("登录后应广播一次 eggs 让前端重拉 —— 状态一致时不会自然广播,这正是「登录后没刷新」的成因")
			}
			return
		}
	}
}

// drainTyped 清空已收到的广播。
func drainTyped(ch <-chan string) {
	for {
		select {
		case <-ch:
		case <-time.After(60 * time.Millisecond):
			return
		}
	}
}
