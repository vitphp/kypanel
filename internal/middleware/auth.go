package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// tokenFingerprint 计算 token 指纹（sha256 前 8 字节 hex，与 service/session.go 一致）。
// 作为会话去重 key，相同 token 在 LoginSession 表里只对应一条记录。
func tokenFingerprint(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:8])
}

// Auth JWT 鉴权中间件
// 优先读取 Authorization: Bearer <token>。
// query 参数 token 仅用于「读取类 GET 请求」兜底（<img>/<video>/<a download> 等无法携带
// Header 的场景，如文件预览/下载）；写操作（POST/PUT/DELETE/PATCH）强制要求 Bearer 头，
// 避免攻击者构造带 token 的 GET 链接诱导用户触发写操作（CSRF）。
func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.Query("token")

		// 写操作不允许走 query token（CSRF 防护），必须 Bearer 头
		if c.Request.Method != http.MethodGet && token != "" {
			utils.FailWithStatus(c, http.StatusUnauthorized, 401, "写操作需通过 Authorization 头鉴权")
			c.Abort()
			return
		}

		if token == "" {
			authHeader := c.GetHeader("Authorization")
			if authHeader == "" {
				utils.FailWithStatus(c, http.StatusUnauthorized, 401, "未登录或 Token 已过期")
				c.Abort()
				return
			}
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				utils.FailWithStatus(c, http.StatusUnauthorized, 401, "Authorization 格式错误")
				c.Abort()
				return
			}
			token = parts[1]
		}

		claims, err := utils.ParseToken(token)
		if err != nil {
			utils.FailWithStatus(c, http.StatusUnauthorized, 401, "Token 无效或已过期")
			c.Abort()
			return
		}

		// 令牌版本校验：改密后 TokenVer 递增，旧 token 的 ver 不再匹配，立即失效
		if claims.TokenVer != service.TokenVerOf(claims.AdminID) {
			utils.FailWithStatus(c, http.StatusUnauthorized, 401, "登录已失效，请重新登录")
			c.Abort()
			return
		}

		// 将用户信息注入上下文
		c.Set("admin_id", claims.AdminID)
		c.Set("username", claims.Username)
		// 注入当前 token 指纹，用于"在线会话"列表标记 is_current（识别自己的会话）
		c.Set("token_fingerprint", tokenFingerprint(token))
		// 同步检查该 token 对应的会话是否被踢（active=false → 拒绝访问）
		// 这样"踢下线"真的立即生效，不需要再 bumpTokenVer 让全站 token 失效
		if !service.IsSessionActive(tokenFingerprint(token)) {
			utils.FailWithStatus(c, http.StatusUnauthorized, 401, "该会话已被踢下线，请重新登录")
			c.Abort()
			return
		}
		// 异步更新会话 last_seen（节流：每 30s 最多写一次 DB，避免高频请求冲击）
		// token 指纹（sha256 前 16 字节 hex）作为会话去重 key
		go service.TouchSession(tokenFingerprint(token))
		c.Next()
	}
}

// PMAAuth phpMyAdmin 专用鉴权：
// 首次打开 /phpmyadmin/?token=<面板JWT> 时用 query token 鉴权并种下 pma_token cookie；
// 页面内 AJAX/静态资源请求自动携带 cookie 通过鉴权。
// 注意：phpMyAdmin 内部链接的 token 是其自身 CSRF token（不是 JWT），需要过滤掉，
// 否则会被当作面板 token 解析失败而报 401。
func PMAAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 优先读取 cookie 中的面板 token
		token, _ := c.Cookie("pma_token")
		if token == "" {
			// 2. 首次从面板打开时 URL 可能带 query token
			token = c.Query("token")
			// phpMyAdmin 内部请求也带 token 参数，但是 CSRF token（32 位十六进制），不是 JWT，忽略
			if token != "" && strings.Count(token, ".") < 2 {
				token = ""
			}
		}
		if token == "" {
			authHeader := c.GetHeader("Authorization")
			if authHeader != "" {
				parts := strings.SplitN(authHeader, " ", 2)
				if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
					token = parts[1]
				}
			}
		}
		if token == "" {
			utils.FailWithStatus(c, http.StatusUnauthorized, 401, "未登录或 Token 已过期")
			c.Abort()
			return
		}

		claims, err := utils.ParseToken(token)
		if err != nil {
			utils.FailWithStatus(c, http.StatusUnauthorized, 401, "Token 无效或已过期")
			c.Abort()
			return
		}

		// 令牌版本校验：改密后 TokenVer 递增，旧 token 立即失效
		if claims.TokenVer != service.TokenVerOf(claims.AdminID) {
			utils.FailWithStatus(c, http.StatusUnauthorized, 401, "登录已失效，请重新登录")
			c.Abort()
			return
		}

		c.Set("admin_id", claims.AdminID)
		c.Set("username", claims.Username)

		// 只要有有效 token（无论是 query 还是 cookie 来的）都刷新 cookie，避免使用过程中过期
		c.SetCookie("pma_token", token, 3600*24, "/phpmyadmin", "", CookieSecure(c), true)

		c.Next()
	}
}
