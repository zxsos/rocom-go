package server

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/whoisnian/rocom-capture/internal/envfile"
)

// 本文件守「管理面板改 Web 监听地址」这条链路的四条不变量。
// 它们全都是**只在这条链路上成立**的性质:编译、契约、其它用例都抓不到 ——
// 而每一条被破坏的后果都是「管理员把自己锁在面板外面」,故必须逐个钉死:
//
//  1. **bind 失败不得动现状** —— 新地址起不来时,旧监听必须照常服务、配置文件不得改动
//  2. **试运行不落盘** —— 未确认前配置文件一个字节都不能变(改错的代价恒为零)
//  3. **确认才切换** —— 确认后落盘新地址,且旧监听停止、新监听成为唯一
//  4. **超时自动回滚** —— 无人确认时新监听自己收掉,旧地址与配置文件都不受影响
//
// 第 1 条是「不会变砖」的全部依据:它是唯一一条在「新地址不可用」时保护管理员的。

// freePort 取一个当前空闲的端口。
//
// 刻意用「bind 到 :0 再关掉」的方式取号,而不是挑一个固定端口:固定端口在并发
// 跑测试(go test ./... 会并行多个包)或机器上恰好有服务占用时会撞车,
// 表现为偶发失败 —— 而这类偶发最容易被当成「环境问题」忽略掉。
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// newWebTestServer 造一个托管了真实监听的 Server:初始监听回环随机端口,
// env 文件可写,且已登录。
func newWebTestServer(t *testing.T) (*Server, string, int) {
	t.Helper()
	s, envPath := newConfigTestServer(t)
	web := NewWebServer(s.Handler(), nil)
	s.SetWebServer(web)
	port := freePort(t)
	if err := web.Listen("127.0.0.1:" + strconv.Itoa(port)); err != nil {
		t.Fatalf("初始监听失败: %v", err)
	}
	t.Cleanup(func() {
		// 用例跑完必须收掉监听:Server 的后台 goroutine 无从停止,而监听占着的端口
		// 会让 t.TempDir() 之外的资源泄漏 —— 并行跑包时表现为端口冲突。
		if cur := web.cur; cur != nil {
			cur.close(0)
		}
		if p := web.pending; p != nil {
			p.close(0)
		}
	})
	return s, envPath, port
}

// get 向真实监听发一个请求,返回状态码。err != nil 均视为「连不上」(端口已关)。
func probe(port int) (code int, err error) {
	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/api/admin/status")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func (s *Server) doWebAddr(t *testing.T, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("X-Admin-Token", s.adminToken)
	rr := httptest.NewRecorder()
	switch path {
	case "/api/admin/web-addr":
		s.handleAdminWebAddr(rr, r)
	case "/api/admin/web-addr/confirm":
		s.handleAdminWebAddrConfirm(rr, r)
	case "/api/admin/web-addr/revert":
		s.handleAdminWebAddrRevert(rr, r)
	default:
		t.Fatalf("未知路径 %s", path)
	}
	return rr
}

// TestWebAddrBindFailureKeepsServing 守不变量 1:新地址 bind 失败时,旧监听照常服务、
// 配置文件一字不改。
//
// 这是整条链路最重要的一条:新端口被占是改端口时最常见的失败,而它的后果原本是
// 「面板当场消失」。先起新的、起不来就不碰旧的,才让这个失败退化成一句报错。
func TestWebAddrBindFailureKeepsServing(t *testing.T) {
	s, envPath, port := newWebTestServer(t)
	before, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	// 占住一个端口,再让面板去改到它
	taken := freePort(t)
	busy, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(taken))
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()

	rr := s.doWebAddr(t, "/api/admin/web-addr", `{"addr":"127.0.0.1:`+strconv.Itoa(taken)+`"}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("端口被占应 400,实际 %d: %s", rr.Code, rr.Body.String())
	}
	// 旧监听必须还在服务 —— 管理员正是靠它才能看到上面那句报错
	code, err := probe(port)
	if err != nil || code != http.StatusOK {
		t.Errorf("bind 失败后旧地址应照常服务,实际 code=%d err=%v", code, err)
	}
	after, _ := os.ReadFile(envPath)
	if string(before) != string(after) {
		t.Errorf("bind 失败却改了配置文件:\n--- 前 ---\n%s\n--- 后 ---\n%s", before, after)
	}
}

// TestWebAddrTrialDoesNotPersist 守不变量 2:试运行期间新旧并存,配置文件不动。
func TestWebAddrTrialDoesNotPersist(t *testing.T) {
	s, envPath, port := newWebTestServer(t)
	before, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	next := freePort(t)

	rr := s.doWebAddr(t, "/api/admin/web-addr", `{"addr":"127.0.0.1:`+strconv.Itoa(next)+`"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("试运行应 200,实际 %d: %s", rr.Code, rr.Body.String())
	}
	var got struct {
		Addr     string `json:"addr"`
		RealAddr string `json:"realAddr"`
		Port     int    `json:"port"`
		Deadline int64  `json:"deadline"`
		Handoff  string `json:"handoff"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Handoff == "" {
		t.Error("试运行应返回交接码(新源凭它确认并接上会话)")
	}
	if got.Port != next {
		t.Errorf("响应的 port = %d,期望 %d(前端据此跳转)", got.Port, next)
	}
	if got.Deadline <= time.Now().Unix() {
		t.Errorf("deadline = %d,应在未来(前端据此倒计时)", got.Deadline)
	}
	// 新旧**同时**可访问:这是管理员能去新地址确认的前提
	for _, p := range []int{port, next} {
		if code, err := probe(p); err != nil || code != http.StatusOK {
			t.Errorf("试运行期间端口 %d 应可访问,实际 code=%d err=%v", p, code, err)
		}
	}
	// 配置文件不能动:此刻一切都可逆
	after, _ := os.ReadFile(envPath)
	if string(before) != string(after) {
		t.Errorf("试运行却写了配置文件:\n--- 前 ---\n%s\n--- 后 ---\n%s", before, after)
	}
}

// TestWebAddrConfirmSwitchesAndPersists 守不变量 3:确认后落盘并切换,旧监听停止。
func TestWebAddrConfirmSwitchesAndPersists(t *testing.T) {
	s, envPath, port := newWebTestServer(t)
	next := freePort(t)
	if rr := s.doWebAddr(t, "/api/admin/web-addr", `{"addr":"127.0.0.1:`+strconv.Itoa(next)+`"}`); rr.Code != http.StatusOK {
		t.Fatalf("试运行失败: %d %s", rr.Code, rr.Body.String())
	}

	rr := s.doWebAddr(t, "/api/admin/web-addr/confirm", `{}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("确认应 200,实际 %d: %s", rr.Code, rr.Body.String())
	}
	// 落盘(重启后仍在新地址)
	f, err := envfile.Load(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := f.Get(envWebAddr); v != "127.0.0.1:"+strconv.Itoa(next) {
		t.Errorf("env 未落盘新地址,实际 %q", v)
	}
	// 新地址服务、旧地址停止(旧端口不关的话,下次改回去会撞 address already in use)
	if code, err := probe(next); err != nil || code != http.StatusOK {
		t.Errorf("确认后新地址应可访问,实际 code=%d err=%v", code, err)
	}
	// 旧监听是异步收尾的(等 webOldShutdownGrace),轮询等它关掉
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := probe(port); err != nil {
			break // 连不上 = 已停止
		}
		if time.Now().After(deadline) {
			t.Error("确认后旧地址仍在服务")
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	// 待确认状态应已清空
	if _, _, pend := s.web.Status(); pend != nil {
		t.Errorf("确认后不应还有待确认项: %+v", pend)
	}
}

// TestWebAddrConfirmKeepsPendingOnSaveFailure 守「落盘失败就不切换」:
// 界面若显示新端口能用、重启后却回到旧端口,管理员不会在改完的那一刻去重启验证。
func TestWebAddrConfirmKeepsPendingOnSaveFailure(t *testing.T) {
	s, _, port := newWebTestServer(t)
	next := freePort(t)
	if rr := s.doWebAddr(t, "/api/admin/web-addr", `{"addr":"127.0.0.1:`+strconv.Itoa(next)+`"}`); rr.Code != http.StatusOK {
		t.Fatalf("试运行失败: %d %s", rr.Code, rr.Body.String())
	}

	envfile.SetTestBeforeRename(func() error { return errors.New("注入: 保存失败") })
	defer envfile.SetTestBeforeRename(nil)

	rr := s.doWebAddr(t, "/api/admin/web-addr/confirm", `{}`)
	if rr.Code == http.StatusOK {
		t.Fatal("落盘失败时确认应报错,实际 200(注入未生效?)")
	}
	// 待确认项要留着(管理员修好权限可以重试)
	if _, _, pend := s.web.Status(); pend == nil {
		t.Error("落盘失败后待确认项应保留,便于重试")
	}
	// **当前地址必须还是旧的**。这一条专盯 ApplyPending 里的顺序:
	// 若先切换再落盘,落盘失败时新地址已经悄悄转正 —— 而配置文件里还是旧地址,
	// 于是「面板看着在新端口、重启后回到旧端口」,正是「先落盘再改内存」要防的事。
	// 只验「旧端口还在服务」抓不到它(旧监听此时还没被关),必须查当前地址。
	if addr, _, _ := s.web.Status(); addr != "127.0.0.1:"+strconv.Itoa(port) {
		t.Errorf("落盘失败后当前监听地址应保持旧值 %d,实际 %q", port, addr)
	}
	if code, err := probe(port); err != nil || code != http.StatusOK {
		t.Errorf("落盘失败后旧地址应照常服务,实际 code=%d err=%v", code, err)
	}
}

// TestWebAddrRevert 守回滚:关掉新监听,旧地址与配置文件都不受影响。
func TestWebAddrRevert(t *testing.T) {
	s, envPath, port := newWebTestServer(t)
	before, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	next := freePort(t)
	if rr := s.doWebAddr(t, "/api/admin/web-addr", `{"addr":"127.0.0.1:`+strconv.Itoa(next)+`"}`); rr.Code != http.StatusOK {
		t.Fatalf("试运行失败: %d %s", rr.Code, rr.Body.String())
	}

	rr := s.doWebAddr(t, "/api/admin/web-addr/revert", `{}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("回滚应 200,实际 %d: %s", rr.Code, rr.Body.String())
	}
	if _, err := probe(next); err == nil {
		t.Error("回滚后新地址应已停止监听")
	}
	if code, err := probe(port); err != nil || code != http.StatusOK {
		t.Errorf("回滚后旧地址应照常服务,实际 code=%d err=%v", code, err)
	}
	after, _ := os.ReadFile(envPath)
	if string(before) != string(after) {
		t.Errorf("回滚却改了配置文件:\n--- 前 ---\n%s\n--- 后 ---\n%s", before, after)
	}
}

// TestWebAddrTrialExpires 守不变量 4:超时自动回滚。
//
// 压短 webTrialTimeout 而不是睡 90 秒 —— 那样这条用例要么慢到没人愿意跑,
// 要么被人注释掉,两种结局都等于没写。
func TestWebAddrTrialExpires(t *testing.T) {
	old := webTrialTimeout
	webTrialTimeout = 150 * time.Millisecond
	defer func() { webTrialTimeout = old }()

	s, envPath, port := newWebTestServer(t)
	before, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	next := freePort(t)
	if rr := s.doWebAddr(t, "/api/admin/web-addr", `{"addr":"127.0.0.1:`+strconv.Itoa(next)+`"}`); rr.Code != http.StatusOK {
		t.Fatalf("试运行失败: %d %s", rr.Code, rr.Body.String())
	}

	time.Sleep(400 * time.Millisecond)
	if _, err := probe(next); err == nil {
		t.Error("超时后新地址应已自动停止")
	}
	if code, err := probe(port); err != nil || code != http.StatusOK {
		t.Errorf("超时后旧地址应照常服务,实际 code=%d err=%v", code, err)
	}
	after, _ := os.ReadFile(envPath)
	if string(before) != string(after) {
		t.Errorf("自动回滚却改了配置文件:\n--- 前 ---\n%s\n--- 后 ---\n%s", before, after)
	}
}

// TestWebAddrRejectsInvalid 守校验:非法地址一律 400,且不得起监听、不得落盘。
func TestWebAddrRejectsInvalid(t *testing.T) {
	s, envPath, port := newWebTestServer(t)
	before, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, addr := range []string{"", "127.0.0.1", "127.0.0.1:99999", "127.0.0.1:abc"} {
		rr := s.doWebAddr(t, "/api/admin/web-addr", `{"addr":"`+addr+`"}`)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("addr=%q 应 400,实际 %d", addr, rr.Code)
		}
	}
	// 与当前地址相同也不该起试运行(多半是重复点了保存)
	rr := s.doWebAddr(t, "/api/admin/web-addr", `{"addr":"127.0.0.1:`+strconv.Itoa(port)+`"}`)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("与当前地址相同应 400,实际 %d", rr.Code)
	}
	if code, err := probe(port); err != nil || code != http.StatusOK {
		t.Errorf("连续非法请求后旧地址应照常服务,实际 code=%d err=%v", code, err)
	}
	after, _ := os.ReadFile(envPath)
	if string(before) != string(after) {
		t.Errorf("非法请求却改了配置文件")
	}
}

// TestWebAddrRequiresAdmin 端点必须要求管理员:监听地址不是可公开探测的信息。
func TestWebAddrRequiresAdmin(t *testing.T) {
	s := newTestServer(t)
	s.SetWebServer(NewWebServer(s.Handler(), nil))
	for _, path := range []string{"/api/admin/web-addr", "/api/admin/web-addr/confirm", "/api/admin/web-addr/revert"} {
		rr := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		switch path {
		case "/api/admin/web-addr":
			s.handleAdminWebAddr(rr, r)
		case "/api/admin/web-addr/confirm":
			s.handleAdminWebAddrConfirm(rr, r)
		case "/api/admin/web-addr/revert":
			s.handleAdminWebAddrRevert(rr, r)
		}
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("%s 未登录应 401,实际 %d", path, rr.Code)
		}
	}
}

// TestWebAddrConfirmWithoutPending 没有待确认项时确认/回滚应 409(而非静默成功)。
func TestWebAddrConfirmWithoutPending(t *testing.T) {
	s, _, _ := newWebTestServer(t)
	if rr := s.doWebAddr(t, "/api/admin/web-addr/confirm", `{}`); rr.Code != http.StatusConflict {
		t.Errorf("无待确认项时确认应 409,实际 %d", rr.Code)
	}
	if rr := s.doWebAddr(t, "/api/admin/web-addr/revert", `{}`); rr.Code != http.StatusConflict {
		t.Errorf("无待确认项时回滚应 409,实际 %d", rr.Code)
	}
}

// TestWebAddrConfigShowsPending 守 GET 带出待确认状态:管理员刷新页面后
// 要能看到「还有一次试运行等着确认」,否则倒计时一过就莫名其妙回退了。
func TestWebAddrConfigShowsPending(t *testing.T) {
	s, _, port := newWebTestServer(t)
	next := freePort(t)
	if rr := s.doWebAddr(t, "/api/admin/web-addr", `{"addr":"127.0.0.1:`+strconv.Itoa(next)+`"}`); rr.Code != http.StatusOK {
		t.Fatalf("试运行失败: %d %s", rr.Code, rr.Body.String())
	}
	rr := s.doConfig(t, http.MethodGet, "")
	var got configJSON
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Web.Addr != "127.0.0.1:"+strconv.Itoa(port) {
		t.Errorf("web.addr = %q,期望当前监听地址", got.Web.Addr)
	}
	if got.Web.Pending == nil {
		t.Fatal("有待确认项时 GET 应带出 pending")
	}
	if got.Web.Pending.Port != next {
		t.Errorf("pending.port = %d,期望 %d", got.Web.Pending.Port, next)
	}
}

// TestWebAddrHandoff 守交接码这条路径 —— 它是「换端口后仍能无缝接管」的关键:
// 令牌按源隔离,新地址上没有会话,故确认必须先于鉴权、且不能要求令牌。
//
// 顺带守两件事:码是一次性的(链接被转发、被多开一个标签页都不该二次生效),
// 以及错的码一律 403(它是唯一一条**不要求令牌**就能进的确认路径)。
func TestWebAddrHandoff(t *testing.T) {
	s, envPath, port := newWebTestServer(t)
	next := freePort(t)
	rr := s.doWebAddr(t, "/api/admin/web-addr", `{"addr":"127.0.0.1:`+strconv.Itoa(next)+`"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("试运行失败: %d %s", rr.Code, rr.Body.String())
	}
	var tried struct {
		Handoff string `json:"handoff"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &tried); err != nil {
		t.Fatal(err)
	}

	// 错的码:不得生效,也不得把待确认项弄丢
	rr = s.doWebAddr(t, "/api/admin/web-addr/confirm", `{"handoff":"00000000000000000000000000000000"}`)
	if rr.Code != http.StatusForbidden {
		t.Errorf("错误交接码应 403,实际 %d", rr.Code)
	}
	if _, _, pend := s.web.Status(); pend == nil {
		t.Error("交接码错误不应清掉待确认项")
	}

	// 对的码:**不带管理员令牌**也要能确认(新源上本来就没有会话)
	rr = httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/admin/web-addr/confirm",
		strings.NewReader(`{"handoff":"`+tried.Handoff+`"}`))
	s.handleAdminWebAddrConfirm(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("凭交接码确认应 200,实际 %d: %s", rr.Code, rr.Body.String())
	}
	var got struct {
		OK    bool   `json:"ok"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Token == "" {
		t.Error("交接码确认应返回管理员令牌(新源要能接着用,而不是被踢回登录页)")
	}
	s.adminMu.Lock()
	same := s.adminToken == got.Token
	s.adminMu.Unlock()
	if !same {
		t.Error("返回的令牌与当前会话不一致")
	}
	// 确认的实质效果与其它路径一致:落盘 + 切换
	f, err := envfile.Load(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := f.Get(envWebAddr); v != "127.0.0.1:"+strconv.Itoa(next) {
		t.Errorf("env 未落盘新地址,实际 %q", v)
	}
	if code, err := probe(next); err != nil || code != http.StatusOK {
		t.Errorf("确认后新地址应可访问,实际 code=%d err=%v", code, err)
	}

	// 一次性:同一个码再来一次必须拒绝(待确认项已消失 → 409)
	rr = s.doWebAddr(t, "/api/admin/web-addr/confirm", `{"handoff":"`+tried.Handoff+`"}`)
	if rr.Code != http.StatusConflict {
		t.Errorf("交接码重复使用应 409,实际 %d", rr.Code)
	}
	if _, _, pend := s.web.Status(); pend != nil {
		t.Error("确认后不应还有待确认项")
	}
	_ = port
}

// TestWebAddrHandoffSingleUse 守「交接码用过即废」。
//
// 必须让**待确认项还在**时重试才能验出这一条:确认成功会把待确认项一起清掉,
// 那时同一个码自然也是废的(409),删掉清零那行照样通过 —— 只有「落盘失败、
// 待确认项保留」这一种情形下重试,才能真正区分「码作废了」与「恰好没待确认项了」。
func TestWebAddrHandoffSingleUse(t *testing.T) {
	s, _, _ := newWebTestServer(t)
	next := freePort(t)
	rr := s.doWebAddr(t, "/api/admin/web-addr", `{"addr":"127.0.0.1:`+strconv.Itoa(next)+`"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("试运行失败: %d %s", rr.Code, rr.Body.String())
	}
	var tried struct {
		Handoff string `json:"handoff"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &tried); err != nil {
		t.Fatal(err)
	}

	envfile.SetTestBeforeRename(func() error { return errors.New("注入: 保存失败") })
	defer envfile.SetTestBeforeRename(nil)

	body := `{"handoff":"` + tried.Handoff + `"}`
	if rr := s.doWebAddr(t, "/api/admin/web-addr/confirm", body); rr.Code == http.StatusOK {
		t.Fatal("落盘失败时凭交接码确认应报错")
	}
	if _, _, pend := s.web.Status(); pend == nil {
		t.Fatal("落盘失败后待确认项应保留(否则本用例验不到一次性)")
	}
	// 同一个码再来一次:已作废,应 403 —— 而不是再走一遍落盘
	if rr := s.doWebAddr(t, "/api/admin/web-addr/confirm", body); rr.Code != http.StatusForbidden {
		t.Errorf("已用过的交接码应 403,实际 %d", rr.Code)
	}
}

// TestShowAddr 守「启动日志里的地址能照着点开」。
//
// 那一行(http://localhost:4939)是部署后唯一一次告诉人该访问哪儿的地方。监听所有网卡
// 时内核给回的是 [::]:4939,原样拼进去就成了 "localhost[::]:4939" —— 编译期抓不到,
// 人还得自己猜那个方括号是什么意思。
func TestShowAddr(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"[::]:4939", ":4939"},               // 监听所有网卡(填 :4939 或 0.0.0.0:4939 都是它)
		{"0.0.0.0:4939", ":4939"},            // 同上,只绑 IPv4
		{"127.0.0.1:4939", "127.0.0.1:4939"}, // 只本机:原样保留,「只本机连得上」是它想说的事
		{"[::1]:4939", "[::1]:4939"},         // IPv6 回环:方括号不能丢,丢了解不开
	} {
		a, err := net.ResolveTCPAddr("tcp", c.in)
		if err != nil {
			t.Fatalf("解析 %q: %v", c.in, err)
		}
		if got := showAddr(a); got != c.want {
			t.Errorf("showAddr(%q) = %q,期望 %q", c.in, got, c.want)
		}
	}
}

// TestWebAddrNotWritable 守「改不动就别装能改」:env 不可写时试运行直接失败,
// 而不是让人改完才发现重启后回到旧端口。
func TestWebAddrNotWritable(t *testing.T) {
	s := newTestServer(t)
	s.setEnvPath(filepath.Join(t.TempDir(), "nonexistent.env"))
	s.loginAdminForTest(t)
	s.SetWebServer(NewWebServer(s.Handler(), nil))

	rr := s.doWebAddr(t, "/api/admin/web-addr", `{"addr":"127.0.0.1:`+strconv.Itoa(freePort(t))+`"}`)
	if rr.Code == http.StatusOK {
		t.Error("配置文件不可写时试运行应失败")
	}
}
