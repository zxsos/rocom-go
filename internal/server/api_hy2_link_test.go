package server

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/zxsos/rocom-go/internal/hy2"
)

// 本文件守导入链接端点。它把**明文密码**返回给浏览器(链接里必须带着它才能一键导入),
// 所以最要紧的两条是:必须管理员鉴权、必须 no-store。

// doLink 以管理员身份请求链接;token 留空则模拟未登录。
func (s *Server) doLink(t *testing.T, query string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/api/admin/hy2/link"+query, nil)
	r.Header.Set("X-Admin-Token", s.adminToken)
	rr := httptest.NewRecorder()
	s.handleHy2Link(rr, r)
	return rr
}

// startHy2 通过面板那条真实链路启动代理(POST 配置),顺带覆盖「改配置即热重启」的既有契约。
func startHy2(t *testing.T, s *Server, body string) string {
	t.Helper()
	hy2TestCert(t)
	s.hy2Mgr.SetCert(testHy2Cert)
	rr := s.doConfig(t, http.MethodPost, body)
	if rr.Code != http.StatusOK {
		t.Fatalf("启动代理失败: %d %s", rr.Code, rr.Body.String())
	}
	return rr.Body.String()
}

func linkPort(t *testing.T, realAddr string) int {
	t.Helper()
	_, p, err := net.SplitHostPort(realAddr)
	if err != nil {
		t.Fatalf("实际监听地址异常: %q", realAddr)
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		t.Fatalf("端口解析失败: %q", p)
	}
	return n
}

func TestHy2LinkRequiresAdmin(t *testing.T) {
	s, _ := newConfigTestServer(t)
	startHy2(t, s, `{"hy2":{"addr":"127.0.0.1:0","pass":"p1"}}`)
	s.adminToken = "" // 退回未登录
	rr := s.doLink(t, "?host=1.2.3.4")
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("未登录应 401,得到 %d(%s 里可能有明文密码)", rr.Code, rr.Body.String())
	}
}

func TestHy2LinkNotRunning(t *testing.T) {
	s, _ := newConfigTestServer(t)
	rr := s.doLink(t, "?host=1.2.3.4")
	if rr.Code != http.StatusConflict {
		t.Errorf("代理未运行应 409 并说明原因,得到 %d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "面板") {
		t.Errorf("报错该告诉人去哪儿启用: %q", rr.Body.String())
	}
}

// TestHy2LinkUsesLivePort 是「面板改端口后链接跟着变」这条需求的落点:
// 端口取自运行中的监听地址,不取自 env —— 面板改端口是热重启,链接必须跟着实际值走。
func TestHy2LinkUsesLivePort(t *testing.T) {
	s, _ := newConfigTestServer(t)
	startHy2(t, s, `{"hy2":{"addr":"127.0.0.1:0","pass":"p1"}}`)
	addr, ok := s.hy2Mgr.Running()
	if !ok {
		t.Fatal("代理应已启动")
	}
	want := linkPort(t, addr)

	rr := s.doLink(t, "?host=203.0.113.10")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", rr.Code, rr.Body.String())
	}
	var got hy2LinkJSON
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Port != want {
		t.Errorf("链接端口 = %d, want 实际监听端口 %d", got.Port, want)
	}
	if got.Host != "203.0.113.10" {
		t.Errorf("Host = %q", got.Host)
	}
	if !strings.HasPrefix(got.Link, "hysteria2://p1@203.0.113.10:") {
		t.Errorf("链接形态不对: %q", got.Link)
	}
	// 测试证书只有 127.0.0.1 的 IP SAN,换别的地址就得让客户端跳过校验
	if !strings.Contains(got.Link, "insecure=1") || got.Secure {
		t.Errorf("主机不在证书里时应带 insecure=1: %q secure=%v", got.Link, got.Secure)
	}
	if got := rr.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("带密码的响应必须 no-store,得到 %q", got)
	}
}

// TestHy2LinkSecureWhenHostnameMatchesCert 守「能验签就不带 insecure」:
// 经域名(LE 证书)访问时不该顺手关掉校验。
func TestHy2LinkSecureWhenHostnameMatchesCert(t *testing.T) {
	s, _ := newConfigTestServer(t)
	startHy2(t, s, `{"hy2":{"addr":"127.0.0.1:0","pass":"p1"}}`)
	rr := s.doLink(t, "?host=127.0.0.1") // hy2TestCert 的 SAN 里有它
	var got hy2LinkJSON
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.Link, "insecure") || !got.Secure {
		t.Errorf("主机能被证书验上时不该带 insecure: %q secure=%v", got.Link, got.Secure)
	}
}

// TestHy2LinkAdvertiseOverrides 覆盖 NAT 场景:对外端口与监听端口不同、或主机要固定。
func TestHy2LinkAdvertiseOverrides(t *testing.T) {
	cases := []struct {
		name      string
		advertise string
		query     string
		wantHost  string
		wantPort  int // 0 = 跟实际监听一致
		wantCode  int
	}{
		{name: "只固定端口", advertise: ":40000", query: "?host=1.2.3.4", wantHost: "1.2.3.4", wantPort: 40000, wantCode: http.StatusOK},
		{name: "只固定主机", advertise: "rocom.example", query: "?host=1.2.3.4", wantHost: "rocom.example", wantCode: http.StatusOK},
		{name: "配置优先于请求", advertise: "a.example:40001", query: "?host=1.2.3.4", wantHost: "a.example", wantPort: 40001, wantCode: http.StatusOK},
		{name: "两者皆空", advertise: "", query: "", wantCode: http.StatusBadRequest},
		{name: "坏值", advertise: "host:99999", query: "?host=1.2.3.4", wantCode: http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, envPath := newConfigTestServer(t)
			startHy2(t, s, `{"hy2":{"addr":"127.0.0.1:0","pass":"p1"}}`)
			realAddr, _ := s.hy2Mgr.Running()
			if c.advertise != "" {
				writeAdvEnv(t, envPath, c.advertise)
			}
			rr := s.doLink(t, c.query)
			if rr.Code != c.wantCode {
				t.Fatalf("code = %d, want %d: %s", rr.Code, c.wantCode, rr.Body.String())
			}
			if c.wantCode != http.StatusOK {
				return
			}
			var got hy2LinkJSON
			if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Host != c.wantHost {
				t.Errorf("Host = %q, want %q", got.Host, c.wantHost)
			}
			wantPort := c.wantPort
			if wantPort == 0 {
				wantPort = linkPort(t, realAddr)
			}
			if got.Port != wantPort {
				t.Errorf("Port = %d, want %d", got.Port, wantPort)
			}
		})
	}
}

// writeAdvEnv 直接改 env 里的对外地址:走 POST 的话坏值会被当场挡下(那正是要测的),
// 所以「env 里已经存着坏值」这种历史遗留只能手工造出来。
func writeAdvEnv(t *testing.T, envPath, v string) {
	t.Helper()
	b, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	out := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(envHy2Advertise) + `=.*$`)
	if out.Match(b) {
		b = out.ReplaceAll(b, []byte(envHy2Advertise+"="+v))
	} else {
		b = append(b, ("\n" + envHy2Advertise + "=" + v + "\n")...)
	}
	if err := os.WriteFile(envPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestHy2LinkAdvertiseSavedByPanel 守面板链路:提交的对外地址要落盘并回显,
// 否则「改了端口能同步到链接」只在这一轮有效,重启就丢。
func TestHy2LinkAdvertiseSavedByPanel(t *testing.T) {
	s, envPath := newConfigTestServer(t)
	startHy2(t, s, `{"hy2":{"addr":"127.0.0.1:0","pass":"p1","advertise":"pub.example:44444"}}`)
	f, _ := os.ReadFile(envPath)
	if !strings.Contains(string(f), envHy2Advertise+"=pub.example:44444") {
		t.Errorf("对外地址未落盘:\n%s", f)
	}
	rr := s.doConfig(t, http.MethodGet, "")
	var cfg configJSON
	if err := json.Unmarshal(rr.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Hy2.Advertise != "pub.example:44444" {
		t.Errorf("回显 Advertise = %q", cfg.Hy2.Advertise)
	}
	// 落盘的值确实被链接用上(证明读的是文件而非仅请求参数)
	lr := s.doLink(t, "?host=ignored.example")
	var got hy2LinkJSON
	if err := json.Unmarshal(lr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Host != "pub.example" || got.Port != 44444 {
		t.Errorf("链接未采用对外地址: %+v", got)
	}
}

// 手动部署(跑二进制、没有 /etc/rocom.env)时面板是只读的,但链接仍要能生成 ——
// 密码只在内存里,取 env 会得到一条没有认证段、导入必连不上的链接。
func TestHy2LinkPasswordComesFromMemory(t *testing.T) {
	s := newTestServer(t)
	s.loginAdminForTest(t)
	s.setEnvPath(filepath.Join(t.TempDir(), "nonexistent.env"))
	hy2TestCert(t)
	s.hy2Mgr.SetCert(testHy2Cert)
	if err := s.hy2Mgr.Start(hy2.Config{Addr: "127.0.0.1:0", Password: "from-flag", MaxConns: 8}); err != nil {
		t.Fatal(err)
	}
	rr := s.doLink(t, "?host=1.2.3.4")
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "from-flag@") {
		t.Errorf("链接没带上内存里的密码: %s", rr.Body.String())
	}
}
