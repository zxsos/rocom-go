package server

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 本文件锁住 /api/eggs/query 的本地源行为与那份对外契约。
//
// 为什么值得测:这条接口的结论直接告诉玩家「这颗蛋可能孵出谁」,而它完全建立在
// PET_EGG_CONF 的区间换算上。任何一处单位换算或百分位口径漂了,页面照样列出候选、
// 状态码照样 200,只是候选里混进了孵化时长根本对不上的物种 —— 没有症状的错最難发现。
//
// 三条最要紧的:
//  1. 结果必须来自本地解包数据 —— 零外部依赖、不烧任何第三方额度。
//  2. 用哪个源**不由请求参数决定** —— src 参数覆盖不了服务端行为(理由见 SrcParamIgnored)。
//  3. 实测真值与百分位必须对上 docs/data.md 的记录 —— 这是筛选、单位、口径三者全对的证据。

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
	s := newTestServer(t) // 未做任何配置:无 SMTP、无管理员、无令牌
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

// TestEggQuerySrcParamIgnored 请求参数里的 src 必须**无效**。
//
// 为什么在只剩一个源之后仍然保留这条:接口上留着 src 参数这件事本身就是一个邀请 ——
// 下一个人很容易「顺手把它接上」。接上的那一刻,查询就从零成本本地计算变成打第三方
// 的付费调用,而且是被任意玩家可触发的。这条 5 行断言挡的就是那一次顺手,
// 它保证的不变量(客户端选不了数据源)比那个已经被删掉的源活得更久。
func TestEggQuerySrcParamIgnored(t *testing.T) {
	s := newTestServer(t)
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
