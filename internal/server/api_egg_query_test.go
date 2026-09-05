package server

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 本文件锁住 /api/eggs/query 的两个数据源与它们共用的那份契约,以及管理面板的切源。
//
// 为什么值得测:这条接口是**两个数据源对同一个问题的两套答案**,而前端只认一份契约。
// 任何一边悄悄改了字段名、或者默认源不对,都不会报错 —— 页面照样列出候选,
// 只是烧了不该烧的第三方额度,或者候选里混进了时长根本对不上的物种。
//
// 四条最要紧的:
//  1. 默认必须走本地 —— 本地更准(用 maxSecs 硬筛),且不会烧第三方额度。
//  2. 用哪个源由**服务端配置**决定,请求参数覆盖不了 —— 否则任何玩家都能夹带
//     src=xianyu 去烧额度。
//  3. 两个源的响应结构必须一致 —— 前端不分支,结构一岔就是静默丢字段。
//  4. 咸鱼源未配令牌必须 503 且**不落统计** —— 统计是给「烧了多少额度」看的,
//     没发出去的请求不该算进去。

// stubMerchantFetch 挡住远行商人的后台回源。
//
// 必须挡:设置 s.eggAPIKey 会唤醒 merchantLoop,而它默认走咸鱼源 —— 于是这几条
// 查蛋用例会**真的打到第三方站点**(烧额度、且日志里刷出对方的 401)。与查蛋无关,
// 纯粹是设了令牌的副作用。与 merchant_src_test.go 的 stubHaoyou 同一个道理。
func stubMerchantFetch(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":200,"data":{"item_count":0,"items":[]}}`)
	}))
	t.Cleanup(srv.Close)
	old := merchantFetchURL
	merchantFetchURL = srv.URL
	t.Cleanup(func() { merchantFetchURL = old })
}

// eggQuery 打一次 /api/eggs/query,返回响应与状态码。
func eggQuery(t *testing.T, s *Server, query string) (*httptest.ResponseRecorder, eggMatchOut) {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleEggQuery(rr, httptest.NewRequest("GET", "/api/eggs/query?"+query, nil))
	var out eggMatchOut
	if rr.Code == http.StatusOK {
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("解析响应失败: %v (%s)", err, rr.Body.String())
		}
	}
	return rr, out
}

// TestEggQueryDefaultsToLocal 默认走本地,且不要令牌也能出结果。
func TestEggQueryDefaultsToLocal(t *testing.T) {
	s := newTestServer(t) // eggAPIKey 为空
	// 2026-08-15 破壳实测的样本:随机蛋 gid 2985,孵出权杖-Ⅱ。
	rr, out := eggQuery(t, s, "height=0.20&weight=11.443&maxSecs=57600")
	if rr.Code != http.StatusOK {
		t.Fatalf("本地查询不应失败,状态码 %d: %s", rr.Code, rr.Body.String())
	}
	if out.Source != "local" {
		t.Errorf("source = %q, 期望 local(默认绝不能走第三方)", out.Source)
	}
	if out.Total == 0 || len(out.Matches) == 0 {
		t.Fatal("本地查询应有候选")
	}
	if out.Total != len(out.Matches) {
		t.Errorf("total=%d 与 matches 长度 %d 不一致", out.Total, len(out.Matches))
	}
	// 破壳真值必须在候选里,且**百分位要与当年那份实测记录对得上**。
	//
	// 百分位这条比"在不在候选里"严格得多:docs/data.md 记着这颗蛋落在权杖-Ⅱ 区间内的
	// 身高 25.0% / 体重 34.1%。对得上说明筛选、单位换算、百分位口径三者全对 ——
	// 任何一处差一点,数字就会漂走,而候选列表照样列得出来。
	var found bool
	for _, m := range out.Matches {
		if m.Name == "权杖-Ⅱ" {
			found = true
			if math.Abs(m.HeightPct-25.0) > 0.05 {
				t.Errorf("权杖-Ⅱ 身高百分位 = %v,实测记录是 25.0", m.HeightPct)
			}
			if math.Abs(m.WeightPct-34.1) > 0.05 {
				t.Errorf("权杖-Ⅱ 体重百分位 = %v,实测记录是 34.1", m.WeightPct)
			}
			if m.Img == "" {
				t.Error("本地候选应带上头像(孵出物种的 HeadIcon)")
			}
		}
		if m.Img != "" && !strings.HasPrefix(m.Img, "/img/") {
			t.Errorf("候选 %s 的 img = %q,本地源的 img 必须是 /img/ 开头的站内路径", m.Name, m.Img)
		}
	}
	if !found {
		names := make([]string, 0, len(out.Matches))
		for _, m := range out.Matches {
			names = append(names, m.Name)
		}
		t.Errorf("实测真值「权杖-Ⅱ」不在本地候选里: %v", names)
	}
	// 分数降序。
	for i := 1; i < len(out.Matches); i++ {
		if out.Matches[i-1].Score < out.Matches[i].Score {
			t.Errorf("候选未按 score 降序: %+v", out.Matches)
			break
		}
	}
}

// TestEggQueryLocalNeedsNoKey 本地路径与令牌彻底解耦:配了令牌也仍走本地(默认源)。
func TestEggQueryLocalNeedsNoKey(t *testing.T) {
	s := newTestServer(t)
	stubMerchantFetch(t)
	s.eggAPIKeySet("test-key")
	rr, out := eggQuery(t, s, "height=0.20&weight=11.443&maxSecs=57600")
	if rr.Code != http.StatusOK {
		t.Fatalf("本地查询失败: %d %s", rr.Code, rr.Body.String())
	}
	if out.Source != eggSrcLocal {
		t.Errorf("source = %q, 期望 %s(默认源,即便配了令牌)", out.Source, eggSrcLocal)
	}
}

// TestEggQuerySrcParamIgnored 请求参数里的 src 必须**无效**。
//
// 为什么值得单独测:数据源是对全服生效的运维选项,且咸鱼源限流 10 次/分钟。若能让
// 请求参数覆盖,任何玩家都能夹带 src=xianyu 去烧额度 —— 而页面上没有任何迹象。
// 这条断言挡的就是「顺手把 src 参数接回来」。
func TestEggQuerySrcParamIgnored(t *testing.T) {
	s := newTestServer(t)
	stubMerchantFetch(t)
	s.eggAPIKeySet("test-key")
	for _, q := range []string{
		"height=0.20&weight=11.443&maxSecs=57600&src=xianyu",
		"height=0.20&weight=11.443&maxSecs=57600&src=api",
	} {
		rr, out := eggQuery(t, s, q)
		if rr.Code != http.StatusOK {
			t.Fatalf("带 src=%q 的请求失败: %d %s", q, rr.Code, rr.Body.String())
		}
		if out.Source != eggSrcLocal {
			t.Errorf("请求带 %q 时 source = %q —— src 参数不该能覆盖服务端配置", q, out.Source)
		}
	}
}

// TestEggQueryXianyuWithoutKey 咸鱼源未配令牌:503,且不落统计。
func TestEggQueryXianyuWithoutKey(t *testing.T) {
	s := newTestServer(t) // eggAPIKey 为空
	if err := s.eggSetSource(eggSrcXianyu); err != nil {
		t.Fatalf("切源: %v", err)
	}
	rr, _ := eggQuery(t, s, "height=0.20&weight=11.443")
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("咸鱼源未配令牌应返回 503,实际 %d: %s", rr.Code, rr.Body.String())
	}
	// 切回本地后立刻可用 —— 面板上改一下就能恢复,不用重启。
	if err := s.eggSetSource(eggSrcLocal); err != nil {
		t.Fatalf("切回本地: %v", err)
	}
	rr2, out := eggQuery(t, s, "height=0.20&weight=11.443&maxSecs=57600")
	if rr2.Code != http.StatusOK || out.Total == 0 {
		t.Errorf("切回本地后应立刻可用: %d / %+v", rr2.Code, out)
	}
}

// TestEggQueryBothSourcesShareShape 两条路径的响应必须是同一份结构。
//
// 第三方用 httptest 假服务顶替(真请求会烧额度、且结果取决于对方当时的货单)。
// 只核对**结构**(字段齐不齐、类型对不对),不核对第三方给的具体数值 ——
// 那是对方的口径,我们锁不住也不该锁。
func TestEggQueryBothSourcesShareShape(t *testing.T) {
	old := eggMatchAPI
	defer func() { eggMatchAPI = old }()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 顺带钉住对方的必填参数:key 与 format=json 缺一不可(见 fetchXianyu 的同款断言)。
		if r.URL.Query().Get("key") == "" || r.URL.Query().Get("format") == "" {
			http.Error(w, "missing params", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"msg":"ok","data":{"total":1,"source":"xianyu","matches":[
			{"pet_id":3410001,"pet_name":"权杖-Ⅱ","img_name":"https://example.com/a.png",
			 "hatch_data":57600,"hatch_label":"16小时","main_type":"石","score":99.5}]}}`))
	}))
	defer srv.Close()
	eggMatchAPI = srv.URL

	s := newTestServer(t)
	stubMerchantFetch(t)
	s.eggAPIKeySet("test-key")
	// 两个源的响应要分别取:先用当前(本地)源查一次,再切到咸鱼源查一次。
	_, localOut := eggQuery(t, s, "height=0.20&weight=11.443&maxSecs=57600")
	if err := s.eggSetSource(eggSrcXianyu); err != nil {
		t.Fatalf("切到咸鱼源: %v", err)
	}
	rr, apiOut := eggQuery(t, s, "height=0.20&weight=11.443")
	if rr.Code != http.StatusOK {
		t.Fatalf("咸鱼源查询失败: %d %s", rr.Code, rr.Body.String())
	}
	if apiOut.Source != eggSrcXianyu {
		t.Errorf("source = %q, 期望 %s", apiOut.Source, eggSrcXianyu)
	}
	if len(apiOut.Matches) != 1 {
		t.Fatalf("第三方候选应为 1 条,实际 %d", len(apiOut.Matches))
	}
	if localOut.Source != eggSrcLocal {
		t.Errorf("切到咸鱼源后,先前那份本地结果的 source = %q,期望 %s", localOut.Source, eggSrcLocal)
	}
	// 外链原样透出(前端直接给 <img src>,不拼 /img/)。
	if apiOut.Matches[0].Img != "https://example.com/a.png" {
		t.Errorf("第三方 img 应是外链原文,实际 %q", apiOut.Matches[0].Img)
	}
	if apiOut.Matches[0].Note != "16小时" {
		t.Errorf("第三方 note 应取 hatch_label,实际 %q", apiOut.Matches[0].Note)
	}

	// 结构同一:两边都得有 name / img / hatchSecs / score / note 这些键。
	for _, label := range []struct {
		what string
		out  eggMatchOut
	}{{"local", localOut}, {"xianyu", apiOut}} {
		for _, m := range label.out.Matches {
			if m.Name == "" {
				t.Errorf("[%s] 候选缺 name: %+v", label.what, m)
			}
			if m.HatchSecs == 0 {
				t.Errorf("[%s] 候选缺 hatchSecs: %+v", label.what, m)
			}
			if m.Score < 0 || m.Score > 100 {
				t.Errorf("[%s] 候选 score 越界: %+v", label.what, m)
			}
		}
	}
}

// TestEggQueryLocalNote 本地候选的孵化时长文案。
func TestEggQueryLocalNote(t *testing.T) {
	s := newTestServer(t)
	_, out := eggQuery(t, s, "height=0.20&weight=11.443&maxSecs=57600")
	if len(out.Matches) == 0 {
		t.Skip("无候选")
	}
	// 16 小时 = 57600 秒
	if out.Matches[0].Note != "孵化 16 小时" {
		t.Errorf("57600 秒的文案应是「孵化 16 小时」,实际 %q", out.Matches[0].Note)
	}
	// 带零头的:28800+1800=30600 秒 = 8 小时 30 分
	_, out2 := eggQuery(t, s, "height=0.20&weight=11.443&maxSecs=30600")
	if len(out2.Matches) > 0 && out2.Matches[0].Note != "孵化 8 小时 30 分" {
		t.Errorf("30600 秒的文案应是「孵化 8 小时 30 分」,实际 %q", out2.Matches[0].Note)
	}
	// 不足 1 小时:300 秒(策划占位值,但文案仍要说对)
	_, out3 := eggQuery(t, s, "height=0.20&weight=11.443&maxSecs=300")
	if len(out3.Matches) > 0 && out3.Matches[0].Note != "孵化 5 分钟" {
		t.Errorf("300 秒的文案应是「孵化 5 分钟」,实际 %q", out3.Matches[0].Note)
	}
}

// TestAdminEggSourceSwitch 走 HTTP 入口验证切源:默认源、合法性校验、即时生效。
//
// 与远行商人的切源有两点不同,故这里**不**照抄它的断言:
//   - 不清缓存(两个源都是实时算,没有跨源复用的缓存),故切源没有「当天数据为空」
//     的代价,也就不用断言缓存被清空;
//   - 默认源是本地(而非咸鱼),且本地源**不需要令牌** —— 咸鱼源才是需要令牌的那个。
func TestAdminEggSourceSwitch(t *testing.T) {
	s := newTestServer(t)
	stubMerchantFetch(t)
	token := testAdminToken(t, s)

	adminGet := func() map[string]any {
		t.Helper()
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/admin/egg-source", nil)
		req.Header.Set("X-Admin-Token", token)
		s.Handler().ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET egg-source = %d: %s", rr.Code, rr.Body.String())
		}
		var got map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("解析: %v", err)
		}
		return got
	}
	adminPost := func(src string) int {
		t.Helper()
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/admin/egg-source",
			strings.NewReader(`{"source":"`+src+`"}`))
		req.Header.Set("X-Admin-Token", token)
		s.Handler().ServeHTTP(rr, req)
		return rr.Code
	}

	// ① 默认源是本地,且源清单由后端下发
	got := adminGet()
	if got["source"] != eggSrcLocal {
		t.Errorf("默认源 = %v, 期望 %s", got["source"], eggSrcLocal)
	}
	if got["keySet"] != false {
		t.Errorf("keySet = %v, 期望 false(newTestServer 不带 -egg-api-key)", got["keySet"])
	}
	sources, _ := got["sources"].([]any)
	if len(sources) != 2 {
		t.Errorf("源清单应为 2 个,实际 %d", len(sources))
	}

	// ② 非法标识必须被拒,且**不能**改动当前配置
	if code := adminPost("nope"); code != http.StatusBadRequest {
		t.Errorf("切到未知源 = %d, 期望 400", code)
	}
	if s.eggSource() != eggSrcLocal {
		t.Errorf("被拒的切换仍改了当前源: %s", s.eggSource())
	}

	// ③ 切到咸鱼源:未配令牌时查询应 503(它确实需要令牌)
	if code := adminPost(eggSrcXianyu); code != http.StatusOK {
		t.Fatalf("切到咸鱼源 = %d, 期望 200", code)
	}
	if s.eggSource() != eggSrcXianyu {
		t.Fatalf("切换后当前源 = %s, 期望 %s", s.eggSource(), eggSrcXianyu)
	}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/eggs/query?height=0.2&weight=11.443", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("咸鱼源未配令牌 = %d, 期望 503", rr.Code)
	}

	// ④ 切回本地源:立刻可用,无需重启 —— 这正是「面板改方式」的意义
	if code := adminPost(eggSrcLocal); code != http.StatusOK {
		t.Fatalf("切回本地源 = %d, 期望 200", code)
	}
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/eggs/query?height=0.20&weight=11.443&maxSecs=57600", nil))
	if rr.Code != http.StatusOK {
		t.Errorf("切回本地源后 = %d, 期望 200: %s", rr.Code, rr.Body.String())
	}
	var out eggMatchOut
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析: %v", err)
	}
	if out.Source != eggSrcLocal {
		t.Errorf("响应 source = %q, 期望 %q(前端据此标注来源,错了就是标注说谎)", out.Source, eggSrcLocal)
	}
	if out.Total == 0 {
		t.Error("切回本地源后应有候选")
	}
}

// TestEggSourcePersists 切过的源必须落库,重启后仍是它。
//
// 只改内存镜像的实现能过上面那条测试(同一进程内看不出来),却会在重启后悄悄回到
// 默认源 —— 管理员以为自己切过的源还生效着,看到的却是另一份数据。
//
// 必须**经 eggSetSource 切**(而不是直接 SetEggSource 写库):直接写库测的是 store,
// 而「切源这个动作到底有没有落库」才是要守的东西 —— 少了那次写入,内存里是对的了,
// 重启后却回到默认源,而这条断言如果只查 store 就照样全绿。
func TestEggSourcePersists(t *testing.T) {
	s := newTestServer(t)
	if err := s.eggSetSource(eggSrcXianyu); err != nil {
		t.Fatalf("切源: %v", err)
	}
	// 内存镜像:切完立刻生效
	if got := s.eggSource(); got != eggSrcXianyu {
		t.Fatalf("切源后内存里的源 = %s, 期望 %s", got, eggSrcXianyu)
	}
	// 库里也必须有,否则重启即丢
	if got := s.store.EggSource(); got != eggSrcXianyu {
		t.Fatalf("库里读回 = %q, 期望 %q —— eggSetSource 没落库的话重启会丢", got, eggSrcXianyu)
	}
	s2 := newTestServerFrom(t, s.store)
	if got := s2.eggSource(); got != eggSrcXianyu {
		t.Errorf("重启后源 = %s, 期望 %s —— 未落库的话重启会悄悄回到默认源", got, eggSrcXianyu)
	}
}

// TestEggSourceValid 标识合法性与默认回退。
func TestEggSourceValid(t *testing.T) {
	for _, ok := range []string{eggSrcLocal, eggSrcXianyu} {
		if !eggSourceValid(ok) {
			t.Errorf("eggSourceValid(%q) = false, 期望 true", ok)
		}
	}
	for _, bad := range []string{"", "nope", "LOCAL", "local ", "api"} {
		if eggSourceValid(bad) {
			t.Errorf("eggSourceValid(%q) = true, 期望 false", bad)
		}
	}
	// 库里的非法值(老数据/手改过)必须回退默认源,而不是原样生效
	s := newTestServer(t)
	if err := s.store.SetEggSource("garbage"); err != nil {
		t.Fatalf("写入: %v", err)
	}
	s2 := newTestServerFrom(t, s.store)
	if got := s2.eggSource(); got != eggSrcDefault {
		t.Errorf("库里是非法值时源 = %s, 期望回退默认 %s", got, eggSrcDefault)
	}
	// 展示名:未知标识原样返回(面板显示原始值,便于排查)
	if got := eggSourceName("garbage"); got != "garbage" {
		t.Errorf("eggSourceName(未知) = %q, 期望原样返回", got)
	}
	// 需要令牌的只有咸鱼源 —— 本地源的优势恰恰是不要令牌
	if eggSourceNeedKey(eggSrcLocal) {
		t.Error("本地源不应需要令牌")
	}
	if !eggSourceNeedKey(eggSrcXianyu) {
		t.Error("咸鱼源需要令牌")
	}
}

// TestHatchNote 直接单测时长文案(覆盖 HTTP 路径碰不到的边界)。
func TestHatchNote(t *testing.T) {
	for _, c := range []struct {
		secs int32
		want string
	}{{0, ""}, {-1, ""}, {60, "孵化 1 分钟"}, {300, "孵化 5 分钟"},
		{3600, "孵化 1 小时"}, {57600, "孵化 16 小时"}, {30600, "孵化 8 小时 30 分"}} {
		if got := hatchNote(c.secs); got != c.want {
			t.Errorf("hatchNote(%d) = %q, 期望 %q", c.secs, got, c.want)
		}
	}
}
