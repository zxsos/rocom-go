package server

// 本文件守住两件只与「静态资源怎么发给浏览器」有关的事,都不进 golden 契约:
//
//  1. 三档缓存策略(assets/ 长缓存 immutable、fonts/ 短缓存、其余 no-cache)。
//     理由:文件名带 hash 才能安全长缓存,这条如果写反,用户会一直拿到旧前端。
//  2. **assets/ 缺件时必须有告警日志**(静态资源的 SPA fallback 是故障,不是正常路由)。
//     2026-09-04 线上黑屏:某个 chunk 不在 embed 里时返回 200 + text/html,
//     浏览器按 HTML 规范拒绝把它当 module script 执行,React 从未挂载、页面全黑,
//     而服务端日志一条异常都没有 —— 排查成本极高。告警是让它可见的唯一手段。
//
// 文件名动态取自真实产物,避免构建 hash 变化导致硬编码失效。
import (
	"bytes"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStaticCacheHeaders(t *testing.T) {
	sub, _ := fs.Sub(webFS, "web")
	// 动态取真实产物文件名,避免构建 hash 变化导致测试硬编码失效。
	assets, _ := fs.ReadDir(sub, "assets")
	if len(assets) == 0 {
		t.Fatal("no assets built")
	}
	fonts, _ := fs.ReadDir(sub, "fonts")
	if len(fonts) == 0 {
		t.Fatal("no fonts built")
	}
	routeMap, _ := fs.ReadDir(sub, "route-map/data")
	if len(routeMap) == 0 {
		t.Fatal("no route-map data built")
	}
	cases := []struct {
		path string
		want string
	}{
		{"/", "no-cache"},
		{"/pets", "no-cache"}, // SPA fallback(前端路由,正常)
		{"/logo.svg", "no-cache"},
		{"/assets/" + assets[0].Name(), "public, max-age=31536000, immutable"},
		{"/fonts/" + fonts[0].Name(), "public, max-age=86400"},
		{"/route-map/data/" + routeMap[0].Name(), "no-cache"}, // public 数据,无 hash
	}
	s := &Server{}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, c.path, nil)
		rec := httptest.NewRecorder()
		s.handleStatic(rec, req)
		if got := rec.Header().Get("Cache-Control"); got != c.want {
			t.Errorf("%s: Cache-Control = %q, want %q", c.path, got, c.want)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", c.path, rec.Code)
		}
	}
}

// TestStaticAssetFallbackWarns 静态资源缺件落到 SPA fallback 时必须记一条告警。
//
// 只数日志里有没有告警是不够的 —— 删掉告警逻辑后这条会红,但**反过来**若有人
// 给所有 fallback 都加了告警(含前端路由 /pets),它也会通过,那是噪音。
// 故同时断言:前端路由这种**正常** fallback 不告警。两条合起来才是「只在故障时叫」。
func TestStaticAssetFallbackWarns(t *testing.T) {
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)

	s := &Server{}
	// 缺件的静态资源:index.html 引用了但文件不存在(embed 漏编的典型形态)
	s.handleStatic(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/assets/does-not-exist-abc123.js", nil))
	got := buf.String()
	if !bytes.Contains([]byte(got), []byte("静态资源缺件")) {
		t.Errorf("assets/ 缺件未告警,线上会静默黑屏。实际输出:\n%s", got)
	}
	if !bytes.Contains([]byte(got), []byte("does-not-exist-abc123.js")) {
		t.Errorf("告警未带具体路径,排查时无从下手。实际输出:\n%s", got)
	}

	// 对照:前端路由走 fallback 是正常的,不能告警(否则每次刷新页面都刷日志)
	buf.Reset()
	s.handleStatic(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/pets", nil))
	if bytes.Contains(buf.Bytes(), []byte("静态资源缺件")) {
		t.Errorf("前端路由 /pets 被误报成缺件 —— 正常 SPA fallback 不该告警:\n%s", buf.String())
	}
}
