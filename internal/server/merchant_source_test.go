package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 商人只剩好游快爆一个源(v4.2.3 移除咸鱼源)。本文件锁两件只在这个前提下成立的事:
//
//  1. **无需任何配置就能查** —— 那个需要令牌的源已经不在了,若哪天「未配令牌直接 503」
//     的短路被无意加回来(或 handleMerchant 上又挂了别的前置条件),商人页会整片打不开,
//     而编译和契约测试都照样通过。
//  2. **响应 source 恒为 haoyou** —— 前端顶部拿它标注数据来源,标错等于在说谎。
//
// 两个用例都用空货单页作桩:这里断言的是「可达性」与「来源标注」,与抓到几件货无关。
// 若改成播种真实档期,就得跟着当前时刻算槽 —— 而 0-8 点休市时 merchantEnsure 会直接
// 早退,测试结论会取决于它几点被跑起来。

// TestMerchantNeedsNoConfiguration 未做任何配置(无 SMTP、无管理员、无令牌)也应当能查。
func TestMerchantNeedsNoConfiguration(t *testing.T) {
	s := newTestServer(t)
	fakeHaoyouAPI(t, haoyouPage(), http.StatusOK)

	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/merchant", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/merchant = %d, 期望 200(单一源无需任何令牌/配置)", rr.Code)
	}
	if src := merchantRespSource(t, rr); src != merchantSrcHaoyou {
		t.Errorf("响应 source = %q, 期望 %q(前端据此标注来源,错了就是标注说谎)", src, merchantSrcHaoyou)
	}
}

// TestMerchantSourceIgnoresStaleDBRow 确认响应的 source 不依赖库里的历史值。
//
// merchant_source 表按决定保留(不动 schema、只停止读写),里面可能仍存着旧环境写下的
// 值,甚至是从前那个已移除的源标识。代码不再读它,所以陈旧行不能影响任何输出 ——
// 否则升级到本版的那台机器,面板会显示一个已经不存在的数据源。
func TestMerchantSourceIgnoresStaleDBRow(t *testing.T) {
	s := newTestServer(t)
	if err := s.store.SetMerchantSource("xianyu"); err != nil {
		t.Fatalf("写入陈旧源标识: %v", err)
	}
	fakeHaoyouAPI(t, haoyouPage(), http.StatusOK)

	s2 := newTestServerFrom(t, s.store) // 模拟重启:同一个库,新 Server
	rr := httptest.NewRecorder()
	s2.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/merchant", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("重启后 GET /api/merchant = %d, 期望 200(不再读库里的源配置)", rr.Code)
	}
	if src := merchantRespSource(t, rr); src != merchantSrcHaoyou {
		t.Errorf("重启后 source = %q, 期望恒为 %q(库里陈旧值不得影响输出)", src, merchantSrcHaoyou)
	}
}

// merchantRespSource 从 /api/merchant 响应里取 source 字段。
func merchantRespSource(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var out struct {
		Source string `json:"source"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析 /api/merchant 响应: %v(前 200 字节: %q)", err, rr.Body.String()[:min(200, rr.Body.Len())])
	}
	return out.Source
}
