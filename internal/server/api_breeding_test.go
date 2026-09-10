package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/whoisnian/rocom-capture/internal/pet"
)

// 本文件锁「自动标记已达成」在**接口层**的接线。判定本身与防抖规则的单测在
// internal/pet/breeding_test.go(TestReachGoal / TestAutoDoneOnReach),这里只验两条 HTTP
// 写入路径真的调用了它 —— 判定写得再对,某条路径忘了接,前端表现就是「刷到了却一直停在进行中」,
// 而 Go 编译、pet 包单测、别的接口测试全都照样绿。
//
// 破壳认领那条路径(pipeline.claimHatchedChild)走的是同一份 pet.AutoDoneOnReach,但它要
// 构造管线连接与破壳回包,成本远高于收益:三条路径的分歧只在「before 从哪来」,而那段逻辑
// 与这里一脉相承(都是先读旧线、再比转变)。

// postJSON 发一个 JSON 请求并返回状态码与响应体。不复用 contract_test.go 的 get:
// 那个只支持 GET,且它对非 200 直接 Fatal —— 这里需要看到状态码才能验「拒绝非法输入」。
func postJSON(t *testing.T, s *Server, target string, body any) (int, []byte) {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("序列化请求体: %v", err)
	}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("POST", target, bytes.NewReader(buf)))
	return rr.Code, rr.Body.Bytes()
}

// saveLine 走 POST /api/breeding 整条覆盖写,返回服务端落库后的那条线。
// 落库后的线与请求体的差别(状态被自动改写、时间戳被补齐)正是本文件要验的东西。
func saveLine(t *testing.T, s *Server, l pet.BreedingLine) pet.BreedingLine {
	t.Helper()
	code, body := postJSON(t, s, "/api/breeding?account="+contractAcc, l)
	if code != 200 {
		t.Fatalf("POST /api/breeding 状态码 %d: %s", code, body)
	}
	var out pet.BreedingLine
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	return out
}

// TestBreedingSaveAutoDone 手动补录(与改目标共用的整条覆盖写)提交后自动置 done,
// 以及那条最要紧的防抖:玩家手动改回「进行中」后不再被覆盖。
func TestBreedingSaveAutoDone(t *testing.T) {
	s := newTestServer(t)
	voice, wpct := int32(100), 90.0
	child := pet.EggParent{Gid: 2001, Name: "小后代", Species: "火神", Voice: 100, WeightPct: &wpct}
	line := pet.BreedingLine{
		ID: "save-auto-done", Species: "火神", Status: pet.BreedingActive,
		Goal: pet.BreedingGoal{Voice: &voice, WeightPct: &wpct},
		Gens: []pet.Generation{{Gen: 1, Source: pet.GenSourceManual, Child: &child}},
	}

	if got := saveLine(t, s, line); got.Status != pet.BreedingDone {
		t.Errorf("补录一代且已达标后状态 = %q, 期望自动置 %q", got.Status, pet.BreedingDone)
	}

	// 玩家手动把状态改回「进行中」(想接着刷更高的体重):条件早已成立,不该被掰回 done。
	// 这是 AutoDoneOnReach 判「转变」而非「结果」的全部意义所在。
	back := line
	back.Status = pet.BreedingActive
	if got := saveLine(t, s, back); got.Status != pet.BreedingActive {
		t.Errorf("手动改回进行中后被自动改成了 %q —— 玩家的选择被覆盖了", got.Status)
	}

	// 另一条**从未存过**的线、子代没达标:不该被误判成已达成
	// (before 与 after 都是 false —— 少了它,「新线一律置 done」这种反向错误测不出来)。
	far := 100.0
	miss := pet.BreedingLine{
		ID: "save-unreached", Species: "火神", Status: pet.BreedingActive,
		Goal: pet.BreedingGoal{Voice: &voice, WeightPct: &far},
		Gens: []pet.Generation{{Gen: 1, Source: pet.GenSourceManual, Child: &child}},
	}
	if got := saveLine(t, s, miss); got.Status != pet.BreedingActive {
		t.Errorf("未达标的线状态 = %q, 期望维持 %q", got.Status, pet.BreedingActive)
	}
}

// TestBreedingClaimAutoDone 手动认领待认领的一代后同样要自动置 done。
//
// 认领是最常走的那条路(破壳记下一代的待认领项,玩家再指定是哪只),漏了它等于这个功能
// 只在手动补录时生效。
func TestBreedingClaimAutoDone(t *testing.T) {
	s := newTestServer(t)
	sc := s.store.For(contractAcc)
	// 认领用的子代快照由服务端按 childGid 现取,故先入库一只嗓音满值的宠物。
	p := &pet.Pet{
		Gid: 3001, ConfID: 2000672, BaseConfID: 3006,
		Species: "火神", Name: "小后代", Level: 30, Gender: "♂",
		Nature: "固执", HeightM: 1.5, WeightKg: 90, Voice: 100,
	}
	if _, err := sc.UpsertPet(p); err != nil {
		t.Fatalf("写宠物: %v", err)
	}

	target := int32(100)
	saveLine(t, s, pet.BreedingLine{
		ID: "claim-auto-done", Species: "火神", Status: pet.BreedingActive,
		Goal: pet.BreedingGoal{Voice: &target},
		Pending: []pet.Generation{{
			Gen: 1, Source: pet.GenSourceAuto,
			Mother: &pet.EggParent{Gid: 2001, Name: "小母", Species: "火神"},
		}},
	})

	code, body := postJSON(t, s, "/api/breeding/claim?account="+contractAcc,
		map[string]any{"id": "claim-auto-done", "gen": 1, "childGid": 3001})
	if code != 200 {
		t.Fatalf("POST /api/breeding/claim 状态码 %d: %s", code, body)
	}
	var got pet.BreedingLine
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if len(got.Gens) != 1 || got.Gens[0].Child == nil {
		t.Fatalf("认领后代数 = %+v, 期望第 1 代已带上子代", got.Gens)
	}
	if got.Gens[0].Child.Voice != 100 {
		t.Errorf("认领的子代嗓音 = %d, 期望 100(快照取自库里那只)", got.Gens[0].Child.Voice)
	}
	if got.Status != pet.BreedingDone {
		t.Errorf("认领达标子代后状态 = %q, 期望自动置 %q", got.Status, pet.BreedingDone)
	}
}
