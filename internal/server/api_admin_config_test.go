package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whoisnian/rocom-capture/internal/envfile"
)

// 本文件守管理面板改配置这条链路的三条不变量:
//   1. **敏感项不回显** —— 授权码/代理密码/图鉴令牌只给「是否已设置」
//   2. **先落盘、再热更** —— 顺序反了会「重启后配置丢失」
//   3. **落盘失败就不改内存** —— 否则界面显示已生效、重启后回到旧值

// newConfigTestServer 造一个指向临时 env 文件的 Server,并登录(拿到管理员令牌)。
func newConfigTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	s := newTestServer(t)
	envPath := filepath.Join(t.TempDir(), "rocom.env")
	if err := os.WriteFile(envPath, []byte("# rocom-capture 运行参数\nROCOM_IFACE=eth0\nROCOM_SOCKS5_ADDR=\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.setEnvPath(envPath)
	s.loginAdminForTest(t)
	return s, envPath
}

// loginAdminForTest 走真实的 setup 流程拿到令牌,避免绕过鉴权(端点必须要求管理员)。
func (s *Server) loginAdminForTest(t *testing.T) {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleAdminSetup(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/api/admin/setup",
			strings.NewReader(`{"password":"test-pass"}`)))
	// setup 只在未配置时成功;已配置则改用登录
	rr = httptest.NewRecorder()
	s.handleAdminLogin(rr, httptest.NewRequest(http.MethodPost, "/api/admin/login",
		strings.NewReader(`{"password":"test-pass"}`)))
	if rr.Code != http.StatusOK {
		t.Fatalf("管理员登录失败: %d %s", rr.Code, rr.Body.String())
	}
	var got struct{ Token string }
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	s.adminToken = got.Token
}

// adminReq 构造一个带管理员令牌的请求。
func adminReq(method, body string) *http.Request {
	r := httptest.NewRequest(method, "/api/admin/config", strings.NewReader(body))
	r.Header.Set("X-Admin-Token", "")
	return r
}

func (s *Server) doConfig(t *testing.T, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "/api/admin/config", strings.NewReader(body))
	r.Header.Set("X-Admin-Token", s.adminToken)
	rr := httptest.NewRecorder()
	s.handleAdminConfig(rr, r)
	return rr
}

// TestConfigGetFallsBackToEnv 守「内存为空时回退 env」这条取值顺序。
//
// 真实部署下内存与 env 一致(都由 run.sh 从同一份 env 喂进来),但进程若是手动跑的
// (离线回放、调试),内存里的 flag 初值是空的而 /etc/rocom.env 里其实有配置。
// 只读内存会让面板显示「未设置」—— 管理员以为从没配过,于是重复填一遍,
// 而这一填就把「留空=不修改」绕过去了。
func TestConfigGetFallsBackToEnv(t *testing.T) {
	s, envPath := newConfigTestServer(t)
	// 内存为空,只写 env(模拟手动跑二进制、未经 run.sh 转 flag)
	s.smtp.setCredentials("", "")
	s.eggAPIKeySet("")
	f, err := envfile.Load(envPath)
	if err != nil {
		t.Fatal(err)
	}
	f.Set(envSMTPUser, "from-env@qq.com")
	f.Set(envSMTPPass, "env-pass")
	f.Set(envEggAPIKey, "env-key")
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}

	rr := s.doConfig(t, http.MethodGet, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", rr.Code, rr.Body.String())
	}
	var got configJSON
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.SMTPUser != "from-env@qq.com" {
		t.Errorf("smtpUser = %q,期望回退到 env 的 from-env@qq.com", got.SMTPUser)
	}
	if !got.SMTPPassSet || !got.EggKeySet {
		t.Errorf("env 里有值时标志位应为 true: smtpPass=%v eggKey=%v", got.SMTPPassSet, got.EggKeySet)
	}
	// 仍不能泄露原文
	if strings.Contains(rr.Body.String(), "env-pass") || strings.Contains(rr.Body.String(), "env-key") {
		t.Error("回退 env 时泄露了密钥原文")
	}
}

// TestConfigGetPrefersMemory 与上一条互补:内存有值时以内存为准(面板刚改过的要能显示出来)。
func TestConfigGetPrefersMemory(t *testing.T) {
	s, envPath := newConfigTestServer(t)
	f, _ := envfile.Load(envPath)
	f.Set(envSMTPUser, "stale@qq.com")
	f.Set(envSMTPPass, "stale-pass")
	f.Save()

	s.smtp.setCredentials("fresh@qq.com", "fresh-pass") // 内存已被热更
	rr := s.doConfig(t, http.MethodGet, "")
	var got configJSON
	json.Unmarshal(rr.Body.Bytes(), &got)
	if got.SMTPUser != "fresh@qq.com" {
		t.Errorf("smtpUser = %q,内存有值时应以内存为准(fresh@qq.com)", got.SMTPUser)
	}
	if !got.SMTPPassSet {
		t.Error("内存有密码时 smtpPassSet 应为 true")
	}
}

func TestConfigRequiresAdmin(t *testing.T) {
	s := newTestServer(t)
	rr := httptest.NewRecorder()
	s.handleAdminConfig(rr, httptest.NewRequest(http.MethodGet, "/api/admin/config", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("未登录应 401,实际 %d", rr.Code)
	}
}

// TestConfigGetNoSecrets 守不变量 1:敏感项绝不回显原文。
// 这些凭据一旦经 API 下发,就会出现在浏览器响应、前端内存、可能的截图里 ——
// 而它们能用来发信、能用来刷第三方图鉴配额。
func TestConfigGetNoSecrets(t *testing.T) {
	s, _ := newConfigTestServer(t)
	s.smtp.setCredentials("sender@qq.com", "super-secret-auth-code")
	s.eggAPIKeySet("super-secret-egg-key")
	if err := os.WriteFile(s.envPath, []byte("ROCOM_SOCKS5_PASS=proxy-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	rr := s.doConfig(t, http.MethodGet, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, secret := range []string{"super-secret-auth-code", "super-secret-egg-key", "proxy-secret"} {
		if strings.Contains(body, secret) {
			t.Errorf("响应泄露了密钥 %q:\n%s", secret, body)
		}
	}
	// 但「是否已设置」要给对
	var got configJSON
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.SMTPUser != "sender@qq.com" {
		t.Errorf("smtpUser = %q,邮箱不算敏感应回显", got.SMTPUser)
	}
	if !got.SMTPPassSet || !got.EggKeySet || !got.Socks5.PassSet {
		t.Errorf("已设置的敏感项应报 true: smtpPass=%v eggKey=%v socks5Pass=%v",
			got.SMTPPassSet, got.EggKeySet, got.Socks5.PassSet)
	}
}

// TestConfigPostHotApplies 守不变量 2:保存到 env 且立即热更。
func TestConfigPostHotApplies(t *testing.T) {
	s, envPath := newConfigTestServer(t)

	rr := s.doConfig(t, http.MethodPost, `{"smtpUser":"a@qq.com","smtpPass":"newpass","eggKey":"newkey"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST: %d %s", rr.Code, rr.Body.String())
	}
	// 内存即时生效(不必重启)
	if user, pass := s.smtp.credentials(); user != "a@qq.com" || pass != "newpass" {
		t.Errorf("内存未热更: user=%q pass=%q", user, pass)
	}
	if !s.smtp.configured() {
		t.Error("configured() 应在改完后立即为 true")
	}
	if s.eggAPIKeyGet() != "newkey" {
		t.Errorf("图鉴令牌未热更: %q", s.eggAPIKeyGet())
	}
	// 且落了盘(重启后仍在)
	f, err := envfile.Load(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := f.Get(envSMTPUser); v != "a@qq.com" {
		t.Errorf("env 未落盘 smtpUser: %q", v)
	}
	if v, _ := f.Get(envEggAPIKey); v != "newkey" {
		t.Errorf("env 未落盘 eggKey: %q", v)
	}
}

// TestConfigPostBlankKeepsExisting 守「留空=不修改」这条前端约定。
// 若把空串当清空,管理员只想改邮箱就会顺手把密码清掉。
func TestConfigPostBlankKeepsExisting(t *testing.T) {
	s, _ := newConfigTestServer(t)
	s.smtp.setCredentials("old@qq.com", "keep-me")
	s.eggAPIKeySet("keep-key")

	rr := s.doConfig(t, http.MethodPost, `{"smtpUser":"new@qq.com"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST: %d %s", rr.Code, rr.Body.String())
	}
	user, pass := s.smtp.credentials()
	if user != "new@qq.com" || pass != "keep-me" {
		t.Errorf("留空应保留原密码: user=%q pass=%q", user, pass)
	}
	if s.eggAPIKeyGet() != "keep-key" {
		t.Errorf("留空应保留原令牌: %q", s.eggAPIKeyGet())
	}
}

// TestConfigPostValidatesBeforeWrite 守「校验在落盘之前」:
// 写进去一个起不来的配置,服务下次重启就起不来,而那时管理员已经连不上面板。
func TestConfigPostValidatesBeforeWrite(t *testing.T) {
	s, envPath := newConfigTestServer(t)
	before, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}

	// 有用户名无密码
	rr := s.doConfig(t, http.MethodPost, `{"socks5":{"addr":":1080","user":"u"}}`)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("非法配置应 400,实际 %d: %s", rr.Code, rr.Body.String())
	}
	// 白名单格式错误
	rr = s.doConfig(t, http.MethodPost, `{"socks5":{"addr":":1080","allow":"not-an-ip"}}`)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("非法白名单应 400,实际 %d", rr.Code)
	}
	after, _ := os.ReadFile(envPath)
	if string(before) != string(after) {
		t.Errorf("校验失败却写了文件:\n--- 前 ---\n%s\n--- 后 ---\n%s", before, after)
	}
}

// TestConfigRequiresUserWithPass 邮箱与代理都要求「用户名非空时密码也非空」。
func TestConfigRequiresUserWithPass(t *testing.T) {
	s, _ := newConfigTestServer(t)
	rr := s.doConfig(t, http.MethodPost, `{"smtpUser":"a@qq.com","smtpPass":""}`)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("已填邮箱未填授权码应 400,实际 %d", rr.Code)
	}
}

// TestConfigNotWritableIsReadOnly 守「改不动就别装能改」:
// 手动跑的进程通常没有 /etc/rocom.env,此时保存必须明确失败,而不是假装成功。
func TestConfigNotWritableIsReadOnly(t *testing.T) {
	s := newTestServer(t)
	s.setEnvPath(filepath.Join(t.TempDir(), "nonexistent.env"))
	s.loginAdminForTest(t)

	if s.configWritable() {
		t.Fatal("不存在的文件不应报可写")
	}
	rr := s.doConfig(t, http.MethodGet, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("只读时 GET 仍应可用: %d", rr.Code)
	}
	var got configJSON
	json.Unmarshal(rr.Body.Bytes(), &got)
	if got.Writable {
		t.Error("writable 应为 false,供前端降级为只读")
	}
	rr = s.doConfig(t, http.MethodPost, `{"smtpUser":"a@qq.com","smtpPass":"p"}`)
	if rr.Code == http.StatusOK {
		t.Error("不可写时 POST 应失败")
	}
}

// TestConfigSocks5Restart 守 T2:改代理配置会重启代理(或按新配置启动)。
func TestConfigSocks5Restart(t *testing.T) {
	s, envPath := newConfigTestServer(t)

	rr := s.doConfig(t, http.MethodPost,
		`{"socks5":{"addr":"127.0.0.1:0","maxConns":8,"user":"u1","pass":"p1"}}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST: %d %s", rr.Code, rr.Body.String())
	}
	if addr, ok := s.socks5Mgr.Running(); !ok {
		t.Fatalf("代理未启动: %v", addr)
	}
	// 改密码:地址不变,应只换参数、不重建监听(重建会撞 address already in use)
	rr = s.doConfig(t, http.MethodPost,
		`{"socks5":{"addr":"127.0.0.1:0","maxConns":8,"user":"u1","pass":"p2"}}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("改密码失败(同端口不应重建监听): %d %s", rr.Code, rr.Body.String())
	}
	// 关掉:addr 留空
	rr = s.doConfig(t, http.MethodPost, `{"socks5":{"addr":""}}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("禁用代理失败: %d %s", rr.Code, rr.Body.String())
	}
	if _, ok := s.socks5Mgr.Running(); ok {
		t.Error("addr 为空时代理应已停止")
	}
	// 且写进了 env(重启后不会自己又起来)
	f, _ := envfile.Load(envPath)
	if v, _ := f.Get(envSocks5Addr); v != "" {
		t.Errorf("env 里代理地址应已清空,实际 %q", v)
	}
}

// TestConfigSaveFailureDoesNotHotApply 守不变量 3:落盘失败就别改内存。
// 否则界面显示「已生效」、重启后却回到旧值 —— 而管理员不会在改完的那一刻去重启验证。
//
// 用 envfile 的 testBeforeRename 注入失败。**不能**靠把目录改只读来制造失败:
// 本服务以 root 运行(root 要抓包),而 root 不受目录写权限限制,chmod 500 照样写成功 ——
// 那样这个用例会一路绿灯却什么都没验证(这一点踩过,日志里会打印「管理面板更新配置」)。
func TestConfigSaveFailureDoesNotHotApply(t *testing.T) {
	s, envPath := newConfigTestServer(t)
	s.smtp.setCredentials("old@qq.com", "old-pass")
	s.eggAPIKeySet("old-key")
	before, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}

	envfile.SetTestBeforeRename(func() error { return errors.New("注入: 保存失败") })
	defer envfile.SetTestBeforeRename(nil)

	rr := s.doConfig(t, http.MethodPost, `{"smtpUser":"new@qq.com","smtpPass":"new-pass","eggKey":"new-key"}`)
	if rr.Code == http.StatusOK {
		t.Fatal("保存失败时应返回错误,实际 200(注入未生效?)")
	}
	// 内存必须原封不动
	if user, pass := s.smtp.credentials(); user != "old@qq.com" || pass != "old-pass" {
		t.Errorf("落盘失败却改了内存: user=%q pass=%q", user, pass)
	}
	if s.eggAPIKeyGet() != "old-key" {
		t.Errorf("落盘失败却改了令牌: %q", s.eggAPIKeyGet())
	}
	// 文件也不能被写坏
	after, _ := os.ReadFile(envPath)
	if string(before) != string(after) {
		t.Errorf("保存失败却改动了文件:\n--- 前 ---\n%s\n--- 后 ---\n%s", before, after)
	}
}
