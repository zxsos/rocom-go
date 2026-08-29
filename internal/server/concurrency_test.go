package server

import (
	"net/http/httptest"
	"sync"
	"testing"
)

// 本文件压测阶段 2 抽出并加锁的状态(online / accounts / smtp / pos)。
//
// 存在理由:golden 测试只锁 JSON 形状,**锁不住并发正确性** —— 而阶段 2 改的正是锁。
// 现有测试全是单线程顺序执行,根本不会触发竞态,靠 go test 全绿无法证明改动安全。
// 故补一组并发压测,并用 -race 跑(见 Makefile / CI)。

// concurrentN 是并发度:取 32 而非 8,为了放大交错概率,让 -race 更容易抓到问题。
const concurrentN = 32

// TestConcurrentAccountResolve 并发推导账号:打 5s 缓存路径与回退路径。
func TestConcurrentAccountResolve(t *testing.T) {
	s := newTestServer(t)
	// 让 online 表非空,以覆盖「从在线表推导」的快路径
	s.TouchAccount("UID:1", 1700000000)

	var wg sync.WaitGroup
	for i := 0; i < concurrentN; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// 一半带 ?account=(走显式路径),一半不带(走缓存/回退路径)
			target := "/api/pets"
			if i%2 == 0 {
				target += "?account=UID:1"
			}
			rr := httptest.NewRecorder()
			s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", target, nil))
			if rr.Code != 200 {
				t.Errorf("GET %s 状态码 %d", target, rr.Code)
			}
		}(i)
	}
	wg.Wait()

	if got := s.acct(httptest.NewRequest("GET", "/api/pets", nil)); got != "UID:1" {
		t.Errorf("缺省回退账号 = %q, 期望 UID:1", got)
	}
}

// TestConcurrentAccountResolveNoTraffic 压「在线表为空」的回退路径:
// 并发请求同时落到 ListAccounts() 查库,检验 acctResolver 的缓存写法不会出错。
func TestConcurrentAccountResolveNoTraffic(t *testing.T) {
	s := newTestServer(t)
	// 账号需显式创建:UpsertPet 只写 pets 表,而 ListAccounts 查的是 accounts 表
	// (LEFT JOIN pets)。只写宠物不建账号,回退路径会查到空。
	if err := s.store.UpsertAccount("UID:2", "测试账号"); err != nil {
		t.Fatalf("建账号: %v", err)
	}

	var wg sync.WaitGroup
	got := make([]string, concurrentN)
	for i := 0; i < concurrentN; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got[i] = s.acct(httptest.NewRequest("GET", "/api/pets", nil))
		}(i)
	}
	wg.Wait()

	// 在线表为空 → 回退查库,所有并发请求应得到同一个账号
	for i, v := range got {
		if v != got[0] {
			t.Fatalf("并发回退结果不一致: got[0]=%q got[%d]=%q", got[0], i, v)
		}
	}
	if got[0] != "UID:2" {
		t.Errorf("回退账号 = %q, 期望 UID:2", got[0])
	}
}

// TestConcurrentTouchAndOnline 并发写在线表 + 并发读在线态。
func TestConcurrentTouchAndOnline(t *testing.T) {
	s := newTestServer(t)
	var wg sync.WaitGroup
	for i := 0; i < concurrentN; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			acc := "UID:" + string(rune('A'+i%8))
			s.TouchAccount(acc, int64(1700000000+i))
			_ = s.AccountOnline(acc) // 与上面的写并发交叉
		}(i)
	}
	wg.Wait()
}

// TestConcurrentPositionSnapshot 并发读写位置快照(由 snapshotStore 的 posMu 保护)。
func TestConcurrentPositionSnapshot(t *testing.T) {
	s := newTestServer(t)
	var wg sync.WaitGroup
	for i := 0; i < concurrentN; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			acc := "UID:1"
			if i%2 == 0 {
				s.SetLastPosition(acc, contractPos(1700000000000+int64(i)))
				return
			}
			rr := httptest.NewRecorder()
			s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/position?account="+acc, nil))
			if rr.Code != 200 {
				t.Errorf("GET /api/position 状态码 %d", rr.Code)
			}
		}(i)
	}
	wg.Wait()
}

// TestMixedEndpointConcurrency 模拟前端首屏:并发请求多个接口,覆盖账号推导与各类锁。
func TestMixedEndpointConcurrency(t *testing.T) {
	s := newTestServer(t)
	seedContract(t, s)
	s.TouchAccount(contractAcc, 1700000000)
	s.SetLastPosition(contractAcc, contractPos(1700000000000))
	s.SetLastWildPets(contractAcc, &WildPayload{Account: contractAcc, SceneResID: 10003, Pets: []WildMark{}, AllPets: []WildAllMark{}})
	s.SetLastFlowers(contractAcc, contractFlowers())

	targets := []string{
		"/api/pets", "/api/stats", "/api/events", "/api/events/stats",
		"/api/filter-options", "/api/boxes", "/api/teams", "/api/eggs",
		"/api/position", "/api/wildpets", "/api/flowers", "/api/flowers/slots",
		"/api/handbook-glasses", "/api/icons", "/api/medals", "/api/name-options",
	}
	var wg sync.WaitGroup
	for i := 0; i < concurrentN; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			target := targets[i%len(targets)] + "?account=" + contractAcc
			rr := httptest.NewRecorder()
			s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", target, nil))
			if rr.Code != 200 {
				t.Errorf("GET %s 状态码 %d: %s", target, rr.Code, rr.Body)
			}
		}(i)
	}
	wg.Wait()
}
