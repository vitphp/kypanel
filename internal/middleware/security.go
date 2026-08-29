package middleware

import (
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"kypanel/internal/config"
	"kypanel/internal/service"
)

// SecurityGuard 安全规则中间件：白名单(allow)优先，其次黑名单(block)
// 挂载在需要认证的路由组上，登录接口不受影响（防止管理员误锁）
func SecurityGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if isLocalIP(ip) {
			c.Next()
			return
		}
		// 存在 allow 规则时为白名单模式：只有命中的 IP 放行
		if service.HasAllowSecurityRule() {
			if service.MatchSecurityAny(ip, service.RuleActionAllow) {
				c.Next()
				return
			}
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 403, "msg": "访问被安全策略拒绝（当前 IP 不在允许列表）"})
			return
		}
		if service.MatchSecurityAny(ip, service.RuleActionBlock) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 403, "msg": "访问被安全策略拦截"})
			return
		}
		c.Next()
	}
}

// DomainGuard 域名绑定中间件：若配置了绑定域名，仅允许通过该域名访问
// 登录接口与健康检查始终放行，避免管理员误锁
func DomainGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		domain := config.Get().Server.Domain
		if domain == "" {
			c.Next()
			return
		}
		path := c.Request.URL.Path
		// 放行登录和健康检查，避免锁死后无法恢复
		if path == "/api/ping" || path == "/api/auth/login" {
			c.Next()
			return
		}
		host := c.Request.Host
		if idx := strings.LastIndex(host, ":"); idx != -1 {
			host = host[:idx]
		}
		if host == domain || host == "localhost" || isLocalIP(host) {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 403, "msg": "访问被拒绝：请通过绑定的域名访问面板"})
	}
}

// isLocalIP 本机回环地址始终放行，避免误锁
func isLocalIP(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	return parsed.IsLoopback()
}
