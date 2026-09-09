package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestCompressAssetsStaticFS 验证 CompressAssets 对 http.Dir 静态托管的真实文件：
// 响应带 gzip + 缓存头，且浏览器端解压后内容与源文件逐字节一致——确保不损坏前端产物。
func TestCompressAssetsStaticFS(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dir := t.TempDir()
	raw := bytes.Repeat([]byte("const app = { name: '面板压缩测试'; version: 1 }; "), 500)
	if err := os.WriteFile(filepath.Join(dir, "app-abc123.js"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	r.Use(StaticAssetsCache(), CompressAssets())
	r.StaticFS("/assets", http.Dir(dir))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/app-abc123.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String()[:200])
	}
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("未启用 gzip: %q", w.Header().Get("Content-Encoding"))
	}
	if w.Header().Get("Cache-Control") == "" {
		t.Fatal("缺少 Cache-Control")
	}
	gzr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatalf("gzip 解压失败: %v", err)
	}
	got, _ := io.ReadAll(gzr)
	if !bytes.Equal(got, raw) {
		t.Fatalf("gzip 后内容不一致: got %d bytes, want %d", len(got), len(raw))
	}
}

// TestNoCompressOnAPI 确保非 /assets 路径（模拟 API）不被压缩，避免影响业务响应。
func TestNoCompressOnAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CompressAssets())
	r.GET("/api/foo", func(c *gin.Context) {
		c.String(http.StatusOK, `{"a":1}`)
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/foo", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)
	if w.Header().Get("Content-Encoding") == "gzip" {
		t.Fatal("API 路径不应被压缩")
	}
}
