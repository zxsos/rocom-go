package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// 本文件锁住家园查询的三件容易悄悄坏掉的事:
//
//  1. **必须先领身份再查询** —— 上游按 clientId 限额,不领就能查的话,查到第 4 个
//     uid 才开始失败;这种"能跑但很快坏"的错,编译和测试都拦不住,只能靠
//     假上游在缺 Cookie 时返回 429 来钉。
//  2. **品种名与头像取自本地 gamedata** —— 上游给的是玩家昵称(「牢大」「秋天」),
//     不翻译就没法认;查不到时才回落到它的 defaultName。
//  3. **回源失败降级旧缓存** —— 上游是免费站,偶发失败是常态;一份 15 分钟前的
//     快照有用,而一个 502 没用。

// stubHomeUpstream 把上游换成假服务,并强制要求查询请求带上 Cookie。
func stubHomeUpstream(t *testing.T, query func(w http.ResponseWriter, r *http.Request)) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case homeQuotaPath:
			w.Header().Set("Set-Cookie", "clientId=test-abc; Path=/; Max-Age=86400")
			w.Write([]byte(`{"quota":{"remaining":3}}`))
		case homeQueryPath:
			// 缺 Cookie 直接 429:模拟上游限额,使"跳过领身份"必然暴露。
			if r.Header.Get("Cookie") == "" {
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":"quota exceeded"}`))
				return
			}
			query(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	old := homeQueryAPI
	homeQueryAPI = srv.URL
	t.Cleanup(func() { homeQueryAPI = old })
}

// homeUpstreamBody 是假上游的响应:两只精灵,一只本地认得(3123 雪影娃娃),
// 一只认不得(999999999),外加一株作物 —— 正好覆盖"命中 / 回落"两条路径。
const homeUpstreamBody = `{"fromCache":false,"data":{
 "uid":"5678116","homeName":"牢大","roomLevel":5,"homeLevel":25,"comfortLevel":75780,
 "homeExperience":2116674,"residentPetCount":2,"plantCount":1,
 "pets":[
  {"id":3123,"gid":37147,"name":"秋天","defaultName":"上游默认名","level":60,
   "genderText":"雌性","mutationName":"异色","status":1702,"feedRound":9},
  {"id":999999999,"gid":1,"name":"怪东西","defaultName":"未知道具","level":1,
   "genderText":"","mutationName":"","status":1700}
 ],
 "plants":[{"seedName":"恶魔雪茄","harvestNum":18,"ripeAt":"2026-08-12T10:53:15.000Z",
  "canStealAccount":6,"stolenAccount":0}]
}}`

func homeQueryGet(t *testing.T, s *Server, q string) (int, homeQueryOut) {
	t.Helper()
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/home/query?"+q, nil))
	var out homeQueryOut
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("解响应: %v (%s)", err, w.Body.String())
		}
	}
	return w.Code, out
}

func TestHomeQueryNormalizes(t *testing.T) {
	s := newTestServer(t)
	stubHomeUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(homeUpstreamBody))
	})

	code, got := homeQueryGet(t, s, "uid=5678116")
	if code != http.StatusOK {
		t.Fatalf("HTTP %d", code)
	}

	// 领身份这一步不能省:省了假上游就回 429,而 429 会走到"无缓存→502"。
	if got.HomeName != "牢大" || got.HomeLevel != 25 || got.RoomLevel != 5 || got.Comfort != 75780 {
		t.Errorf("家园字段不对: %+v", got)
	}
	if got.Cached {
		t.Error("首次查询不该命中本地缓存")
	}
	if got.Exp != 2116674 {
		t.Errorf("exp 应透传上游 homeExperience(2116674),实得 %d", got.Exp)
	}
	if got.FetchedAt == 0 {
		t.Error("fetchedAt 应给出回源时刻")
	}

	if len(got.Pets) != 2 {
		t.Fatalf("应有 2 只精灵,实得 %d", len(got.Pets))
	}
	known := got.Pets[0]
	// 本地名称库命中:species 必须是中文品种名。上游的 defaultName 故意写成
	// "上游默认名"(真实数据里它与本地名常常相同),否则"忘了查本地库、直接拿
	// defaultName"这条变异路径根本测不出来 —— 断言会一路绿灯。
	if known.Base != 3123 || known.Form != 37147 {
		t.Errorf("base/form 取错: %+v", known)
	}
	if known.Species != "雪影娃娃" {
		t.Errorf("species 应为本地品种名 雪影娃娃,实得 %q", known.Species)
	}
	if known.Head == "" {
		t.Error("本地认得的品种应带头像路径")
	}
	if known.Name != "秋天" || known.Level != 60 || known.Gender != "雌性" {
		t.Errorf("昵称/等级/性别取错: %+v", known)
	}
	if known.Mutation != "异色" {
		t.Errorf("变异取错: %+v", known)
	}
	// 状态走本地映射,不用上游 statusText(它对认不出的码拼「状态 1700」)。
	if known.Status != "可收取灵感" {
		t.Errorf("status 1702 应译为 可收取灵感,实得 %q", known.Status)
	}
	if known.Feed != 9 {
		t.Errorf("feedRound 取错: %+v", known)
	}
	// 异色个体必须给异色头像(3123 有专属异色图 3123_1)。
	// 若哪天改回普通图,编译与上面的 head!="" 断言都不会报警 —— 只有比具体文件名才拦得住。
	if known.Head != "HeadIcon/3123_1.webp" {
		t.Errorf("异色个体应给异色头像 HeadIcon/3123_1.webp,实得 %q", known.Head)
	}

	// 本地查不到的品种:species 必须回落到上游 defaultName,且不给头像。
	// 它的 status 是 1700 → 未喂食,顺带覆盖「非 1702 的映射」这条分支。
	unknown := got.Pets[1]
	if unknown.Status != "未喂食" {
		t.Errorf("status 1700 应译为 未喂食,实得 %q", unknown.Status)
	}
	if unknown.Species != "未知道具" {
		t.Errorf("本地查不到时应回落 defaultName,实得 %q", unknown.Species)
	}
	if unknown.Head != "" {
		t.Errorf("本地查不到时不该给头像,实得 %q", unknown.Head)
	}

	if len(got.Plants) != 1 {
		t.Fatalf("应有 1 株作物,实得 %d", len(got.Plants))
	}
	p := got.Plants[0]
	if p.SeedName != "恶魔雪茄" || p.Harvest != 18 || p.CanSteal != 6 || p.Stolen != 0 {
		t.Errorf("作物字段取错: %+v", p)
	}
	if p.RipeAt == 0 {
		t.Error("ripeAt 未从 RFC3339 解析出 Unix 秒")
	}

	// 二次查询应命中缓存:cached 为真,且假上游不再被调用(被调用也不会报错,
	// 故另用计数断言更稳 —— 见 TestHomeQueryFallsBackToCache)。
	if code2, got2 := homeQueryGet(t, s, "uid=5678116"); code2 != http.StatusOK || !got2.Cached {
		t.Errorf("二次查询应命中缓存: code=%d cached=%v", code2, got2.Cached)
	}
}

// TestHomeQueryForceBypassesCache 守 force=1 真正跳过缓存。
// 变异方向:把 force 判断写成恒假,页面上的「刷新」就永远拿不到新数据。
func TestHomeQueryForceBypassesCache(t *testing.T) {
	s := newTestServer(t)
	calls := 0
	stubHomeUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Write([]byte(homeUpstreamBody))
	})

	homeQueryGet(t, s, "uid=5678116")
	if calls != 1 {
		t.Fatalf("首次查询应回源 1 次,实得 %d", calls)
	}
	if code, got := homeQueryGet(t, s, "uid=5678116"); code != http.StatusOK || !got.Cached {
		t.Fatalf("二次查询应命中缓存: code=%d cached=%v", code, got.Cached)
	}
	if calls != 1 {
		t.Errorf("命中缓存时不该回源,calls=%d", calls)
	}
	if code, got := homeQueryGet(t, s, "uid=5678116&force=1"); code != http.StatusOK || got.Cached {
		t.Errorf("force=1 应回源且 cached=false: code=%d cached=%v", code, got.Cached)
	}
	if calls != 2 {
		t.Errorf("force=1 应触发第 2 次回源,calls=%d", calls)
	}
}

// TestHomeQueryFallsBackToCache 守「回源失败时降级旧缓存」。
// 变异方向:删掉这段降级逻辑,上游一抽风整个功能就不可用 —— 而这在本地测不出来。
func TestHomeQueryFallsBackToCache(t *testing.T) {
	s := newTestServer(t)
	fail := false
	stubHomeUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		if fail {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Write([]byte(homeUpstreamBody))
	})

	homeQueryGet(t, s, "uid=5678116")
	fail = true
	if code, got := homeQueryGet(t, s, "uid=5678116&force=1"); code != http.StatusOK || got.HomeName != "牢大" {
		t.Errorf("回源失败时应降级返回旧缓存: code=%d name=%q", code, got.HomeName)
	}
}

func TestHomeQueryUpstreamError(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantSub string
		notWant []string
	}{
		{
			name:   "422 少输一位 uid",
			status: http.StatusUnprocessableEntity,
			body:   `{"error":true,"url":"https://rocodex.org/api/home-query/query","statusCode":422,"statusMessage":"Unprocessable Entity","data":{"quota":{"remaining":3}}}`,
			// 关键:给的是「怎么办」,不是上游的内部状态码名
			wantSub: "UID 是否完整正确",
			notWant: []string{"Unprocessable", "rocodex.org", "statusCode", "quota"},
		},
		{
			name:    "400 上游中文提示直接采用",
			status:  http.StatusBadRequest,
			body:    `{"statusCode":400,"statusMessage":"请输入正确的玩家 UID"}`,
			wantSub: "请输入正确的玩家 UID",
			notWant: []string{"{"},
		},
		{
			name:    "502 不泄露原始 body",
			status:  http.StatusBadGateway,
			body:    `{"statusMessage":"internal detail: db-3 unavailable"}`,
			wantSub: "上游暂时不可用",
			notWant: []string{"db-3", "internal detail"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newTestServer(t)
			stubHomeUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(c.status)
				w.Write([]byte(c.body))
			})
			// force=1 且无缓存 → 必然回源失败,走 502 出去
			w := httptest.NewRecorder()
			s.mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/home/query?uid=90612933&force=1", nil))
			if w.Code != http.StatusBadGateway {
				t.Fatalf("HTTP %d,期望 502;body=%s", w.Code, w.Body.String())
			}
			got := w.Body.String()
			if !strings.Contains(got, c.wantSub) {
				t.Errorf("错误应含 %q,实得: %s", c.wantSub, got)
			}
			for _, bad := range c.notWant {
				if strings.Contains(got, bad) {
					t.Errorf("错误不该泄露上游细节 %q,实得: %s", bad, got)
				}
			}
		})
	}
}

// TestHomeQueryRejectsBadUID 守 uid 校验:非数字的 uid 必须在回源之前被挡下。
func TestHomeQueryRejectsBadUID(t *testing.T) {
	s := newTestServer(t)
	stubHomeUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("uid 非法时不该回源")
		w.Write([]byte(homeUpstreamBody))
	})
	for _, uid := range []string{"", "abc", "12 34", "0x10", "+123", "12a", "١٢٣"} { // 末项是阿拉伯数字字符
		if code, _ := homeQueryGet(t, s, "uid="+url.QueryEscape(uid)); code != http.StatusBadRequest {
			t.Errorf("uid=%q 应返回 400,实得 %d", uid, code)
		}
	}
}

// TestHomeQueryAcceptsAnyUIDLength 守「**不**按位数拦 uid」。
//
// uid 有 6 位也有 9 位(玩家实测),位数不是我们能假设的。早先在后端卡 1-20 位、
// 前端提示「少于 8 位似乎不完整」,都会把合法的老 uid 拦下 —— 而这正是
// 「看起来在校验、实际在误伤」的那类限制:它拦住的比它挡住的更多。
//
// 现在只校验「是纯数字」,位数交给上游判。
func TestHomeQueryAcceptsAnyUIDLength(t *testing.T) {
	for _, uid := range []string{"1", "123456", "100000002", "123456789012345678901"} {
		t.Run(uid, func(t *testing.T) {
			s := newTestServer(t)
			sent := ""
			stubHomeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				sent = string(b)
				w.Write([]byte(homeUpstreamBody))
			})
			code, _ := homeQueryGet(t, s, "uid="+uid)
			if code != http.StatusOK {
				t.Fatalf("uid=%s 不该因位数被拦,HTTP %d", uid, code)
			}
			// uid 必须原样送到上游(含超长的,不能溢出成别的数)
			if want := `{"uid":` + uid + `}`; sent != want {
				t.Errorf("发给上游的应是 %s,实得 %s", want, sent)
			}
		})
	}
}
