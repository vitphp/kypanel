package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"kypanel/internal/service"
)

// apiModuleMap 接口路径前缀 → 权限模块 key。
// 未列出的接口（如 /api/auth/* /api/settings/info 等基础接口）不做权限拦截。
// 注意：Auth 中间件已注入 admin_id，本中间件需在其之后挂载。
var apiModuleMap = []struct {
	Prefix string
	Module string
}{
	{"/api/site", "site"},
	{"/api/database", "database"},
	{"/api/backup", "backup"},
	{"/api/ftp", "ftp"},
	{"/api/file", "file"},
	{"/api/container", "container"},
	{"/api/docker", "container"},
	{"/api/apps", "appstore"},
	{"/api/cron", "cron"},
	{"/api/logs", "log"},
	{"/api/monitor", "monitor"},
	{"/api/alert", "monitor"},
	{"/api/process", "process"},
	{"/api/security", "firewall"},
	{"/api/waf", "firewall"},
	{"/api/mcp", "mcp"},
	{"/api/migrate", "site"}, // 网站搬家（含源面板对外接口，API 令牌需开通「网站」权限）
	// 设置类接口归入 settings 权限，但账号自身相关的放行
	{"/api/settings/port", "settings"},
	{"/api/settings/domain", "settings"},
	{"/api/settings/username", "settings"},
	{"/api/settings/mysql-password", "settings"},
	{"/api/settings/login-allowlist", "settings"},
	{"/api/settings/session", "settings"},
	{"/api/settings/litessl", "settings"},
	{"/api/settings/sessions", "settings"},
	// 面板在线更新属于系统级设置
	{"/api/update", "settings"},
	// 高敏管理接口：临时访问链接与 API 令牌是「绕过登录的全权限钥匙」，
	// 归入 settings 权限模块，禁止未授权子账号使用（handler 内另有超管硬校验）。
	{"/api/temp-access", "settings"},
	{"/api/api-tokens", "settings"},
}

// PermissionGuard 权限中间件：按角色校验接口访问权限。
// 超级管理员（role_id=0）与拥有 * 权限的角色不受限制。
// API/MCP 令牌若声明了权限范围（token_scopes），则按范围校验；
// 空 / * 表示全部权限（兼容旧令牌与完整权限场景）。
func PermissionGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		adminID := c.GetUint("admin_id")
		path := c.Request.URL.Path

		module := moduleForPath(path)
		if module == "" {
			c.Next()
			return
		}
		// API/MCP 令牌：按声明的 scopes 校验（仅 api_token 鉴权会注入该值）
		if scopes, ok := c.Get("token_scopes"); ok {
			scopesStr, _ := scopes.(string)
			if scopesStr != "" && scopesStr != "*" && !scopeAllows(scopesStr, module) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权限访问该功能"})
				return
			}
			c.Next()
			return
		}
		if !service.AdminHasPermission(adminID, module) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权限访问该功能"})
			return
		}
		c.Next()
	}
}

// scopeAllows 判断令牌权限范围（逗号分隔模块名）是否包含指定模块
func scopeAllows(scopes, module string) bool {
	for _, s := range strings.Split(scopes, ",") {
		if strings.TrimSpace(s) == module {
			return true
		}
	}
	return false
}

// moduleForPath 根据路径返回权限模块，无匹配返回空
func moduleForPath(path string) string {
	for _, m := range apiModuleMap {
		if strings.HasPrefix(path, m.Prefix) {
			return m.Module
		}
	}
	return ""
}
