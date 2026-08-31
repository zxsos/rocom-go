package server

// 临时验证 handleStatic 的三档缓存策略,验证后即删(不影响 golden 契约)。
import (
	"io/fs"
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
		{"/pets", "no-cache"}, // SPA fallback
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
