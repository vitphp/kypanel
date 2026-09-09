package middleware

import (
	"compress/gzip"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// SecurityHeaders 为面板所有响应追加安全防护头（对标成熟面板的基础安全标配）：
//   - X-Frame-Options:          禁止面板页被 iframe 嵌入（防点击劫持 / 钓鱼内嵌）
//   - X-Content-Type-Options:    禁止浏览器 MIME 嗅探（防类型混淆）
//   - Referrer-Policy:           限制跳转外站时泄露 URL
//   - Permissions-Policy:        禁用面板用不到的浏览器敏感能力
//
// HSTS 仅在确实走 HTTPS 时注入（HTTP 降级下发 HSTS 无意义）。同时给会话 cookie
// 设 SameSite=Lax 作 CSRF 纵深：阻止跨站请求自动携带 cookie。
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Frame-Options", "SAMEORIGIN")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if reqIsHTTPS(c) {
			c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		c.SetSameSite(http.SameSiteLaxMode)
		c.Next()
	}
}

// reqIsHTTPS 判断当前请求是否实际走 HTTPS（直连 TLS 或可信代理透传 X-Forwarded-Proto）。
func reqIsHTTPS(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	p := strings.TrimSpace(strings.Split(c.GetHeader("X-Forwarded-Proto"), ",")[0])
	return strings.EqualFold(p, "https")
}

// gzipResponseWriter 极简 gzip 响应封装（标准库实现，避免引入第三方依赖）。
type gzipResponseWriter struct {
	gin.ResponseWriter
	gz *gzip.Writer
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) { return w.gz.Write(b) }
func (w *gzipResponseWriter) WriteString(s string) (int, error) {
	return w.gz.Write([]byte(s))
}

// CompressAssets 仅对前端静态资源（/assets/* 的 JS/CSS）启用 gzip，显著减小传输体积
// （主 JS 包 gzip 后缩小约 70%），加快面板页面加载。API / 文件下载 / 流式一律不压，
// 避免包装 ResponseWriter 破坏 Range / 大文件语义，也省去实时接口的 CPU 空耗。
func CompressAssets() gin.HandlerFunc {
	return func(c *gin.Context) {
		p := c.Request.URL.Path
		if !strings.HasPrefix(p, "/assets/") ||
			!strings.Contains(c.GetHeader("Accept-Encoding"), "gzip") ||
			c.Writer.Header().Get("Content-Encoding") != "" {
			c.Next()
			return
		}
		c.Writer.Header().Set("Content-Encoding", "gzip")
		c.Writer.Header().Del("Content-Length")
		c.Writer.Header().Add("Vary", "Accept-Encoding")
		gz := gzip.NewWriter(c.Writer)
		defer gz.Close()
		c.Writer = &gzipResponseWriter{ResponseWriter: c.Writer, gz: gz}
		c.Next()
	}
}

// StaticAssetsCache 给 /assets/* 前端产物设强缓存。这些文件名自带内容 hash（web 打包产物），
// 版本更新后 URL 必变，故可放心长缓存，避免每次重复下载未变化的 chunk。
func StaticAssetsCache() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/assets/") {
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
		}
		c.Next()
	}
}
