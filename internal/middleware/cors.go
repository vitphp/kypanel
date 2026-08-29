package middleware

import (
	"net/url"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

// CORS 跨域中间件：允许同源请求、常用本地开发源和 PANEL_CORS_ORIGINS 显式配置的来源。
// HTTP/HTTPS 按 Origin 分别匹配，不再向任意网站返回 *。
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if origin != "" && corsOriginAllowed(c, origin) {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Header("Access-Control-Max-Age", "86400")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}

func corsOriginAllowed(c *gin.Context, origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}

	// 同一面板主机的 HTTP/HTTPS 来源均允许，兼容直接访问和反向代理场景。
	if strings.EqualFold(parsed.Host, c.Request.Host) {
		return true
	}

	// Vite 等本地开发前端的固定来源。
	switch strings.ToLower(origin) {
	case "http://localhost:5173", "https://localhost:5173",
		"http://127.0.0.1:5173", "https://127.0.0.1:5173":
		return true
	}

	// 其他跨域前端必须显式配置，多个来源用逗号分隔。
	for _, allowed := range strings.Split(os.Getenv("PANEL_CORS_ORIGINS"), ",") {
		if strings.EqualFold(strings.TrimSpace(allowed), origin) {
			return true
		}
	}
	return false
}

// CookieSecure 根据当前连接协议决定是否设置 Secure，兼容直连和可信 HTTPS 反向代理。
func CookieSecure(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	forwardedProto := strings.TrimSpace(strings.Split(c.GetHeader("X-Forwarded-Proto"), ",")[0])
	return strings.EqualFold(forwardedProto, "https")
}
