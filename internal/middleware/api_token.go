package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// PanelTokenCookie 登录态 cookie 名：登录成功后写入，NoRoute 的安全入口校验用它判断「已登录」。
// 放在 middleware 包供 ApiTokenOrAuth（临时登录写 cookie）与 router（NoRoute 校验）共用。
const PanelTokenCookie = "lp_token"

// ApiTokenOrAuth 鉴权中间件：先校验 API 令牌（带 type 区分），失败再回退到登录 JWT。
// 用于「通用 API」路由组：JWT（前端登录态）和 API 令牌都能访问。
//
// tokenType: 期望的令牌类型（"api" 或 "mcp"），用于路由分流。
//
// 鉴权成功时把 admin_id 注入 context（API 令牌注入 0，对应超管上下文）；
// JWT 鉴权保持原有 admin_id。
func ApiTokenOrAuth(tokenType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		rawToken := extractToken(c)
		if rawToken == "" {
			utils.FailWithStatus(c, http.StatusUnauthorized, 401, "未登录或 Token 已过期")
			c.Abort()
			return
		}

		// 0) 先尝试临时访问 token（lp_temp_ 前缀，用于免密进入面板后台）
		if strings.HasPrefix(rawToken, service.TempAccessTokenPrefix) {
			if tempID, ok, reason := service.VerifyTempAccess(rawToken, c.ClientIP(), c.GetHeader("User-Agent")); ok {
				c.Set("admin_id", uint(0)) // 超管上下文
				c.Set("username", "temp-access")
				c.Set("auth_type", "temp")
				c.Set("temp_access_id", tempID) // 用于 OperationLog 关联临时访问 session
				// 写入登录态 cookie：与正常登录一致，临时登录后刷新页面时
				// NoRoute 靠 lp_token cookie 判断「已登录」放行，避免刷新 404。
				c.SetCookie(PanelTokenCookie, rawToken, service.TokenTTLHours()*3600, "/", "", CookieSecure(c), true)
				c.Next()
				return
			} else {
				utils.FailWithStatus(c, http.StatusUnauthorized, 401, reason)
				c.Abort()
				return
			}
		}

		// 1) 先尝试 API 令牌（36 位纯随机，无 `.` 分隔）
		if !strings.Contains(rawToken, ".") && len(rawToken) == 36 {
			if _, scopes, ok := service.VerifyApiToken(tokenType, rawToken, c.ClientIP()); ok {
				c.Set("admin_id", uint(0)) // 超管上下文
				c.Set("username", "api-token")
				c.Set("auth_type", "api_token")
				// 注入令牌权限范围：空 = 全部权限；非空时 PermissionGuard 按模块校验
				c.Set("token_scopes", scopes)
				c.Next()
				return
			}
		}

		// 2) 回退到 JWT 鉴权（沿用现有登录 token 逻辑）
		claims, err := utils.ParseToken(rawToken)
		if err != nil {
			utils.FailWithStatus(c, http.StatusUnauthorized, 401, "Token 无效或已过期")
			c.Abort()
			return
		}
		if claims.TokenVer != service.TokenVerOf(claims.AdminID) {
			utils.FailWithStatus(c, http.StatusUnauthorized, 401, "登录已失效，请重新登录")
			c.Abort()
			return
		}
		c.Set("admin_id", claims.AdminID)
		c.Set("username", claims.Username)
		c.Set("auth_type", "jwt")
		// 注入当前 token 指纹，让「在线会话」列表能识别当前登录账号（避免误踢自己）
		c.Set("token_fingerprint", tokenFingerprint(rawToken))
		c.Next()
	}
}

// ApiTokenOnlyAuth 仅接受 API 令牌（不接受 JWT）。用于「必须用 API 令牌」的对外接口，
// 确保外部脚本/AI 工具必须显式使用 API 令牌（即使有登录态也无法访问，便于权限隔离）。
func ApiTokenOnlyAuth(tokenType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		rawToken := extractToken(c)
		if rawToken == "" {
			utils.FailWithStatus(c, http.StatusUnauthorized, 401, "未提供 API 令牌")
			c.Abort()
			return
		}
		_, scopes, ok := service.VerifyApiToken(tokenType, rawToken, c.ClientIP())
		if !ok {
			utils.FailWithStatus(c, http.StatusUnauthorized, 401, "API 令牌无效或已被禁用")
			c.Abort()
			return
		}
		c.Set("admin_id", uint(0))
		c.Set("username", "api-token")
		c.Set("auth_type", "api_token")
		// 注入令牌权限范围：空 = 全部权限；非空时 PermissionGuard 按模块校验
		c.Set("token_scopes", scopes)
		c.Next()
	}
}

// extractToken 提取 Bearer token（与 Auth() 逻辑一致：仅 GET 允许 query token，写操作强制 Header）
func extractToken(c *gin.Context) string {
	token := c.Query("token")
	if token == "" {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			return ""
		}
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return ""
		}
		token = parts[1]
	}
	// 写操作不允许走 query token（与 Auth() 一致的 CSRF 防护）
	if c.Request.Method != http.MethodGet && c.Query("token") != "" {
		return ""
	}
	return token
}