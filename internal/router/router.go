package router

import (
	"html"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"kypanel/internal/config"
	"kypanel/internal/middleware"
	"kypanel/internal/service"
	"kypanel/internal/utils"
	"kypanel/webui"
)

// panelTokenCookie 登录态 cookie 名：登录成功后写入，NoRoute 用它判断「已登录」跳过安全入口校验
const panelTokenCookie = middleware.PanelTokenCookie

// currentEntrance 当前生效的安全入口（安装时生成，启动时由 serveFrontend 注入）。
// 登录/验证码接口路径内嵌该入口值（/api/<entrance>/auth/login），攻击者不知道入口就无法调用。
var currentEntrance string

// Setup 初始化路由
func Setup(cfg *config.Config) *gin.Engine {
	// 在注册路由前记录安全入口，供登录/验证码接口路径内嵌入口值使用
	currentEntrance = cfg.SecurityEntrance
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), middleware.CORS())

	// 健康检查
	r.GET("/api/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "pong"})
	})

	// 域名绑定白名单（全局，排在最前）
	r.Use(middleware.DomainGuard())

	// 无需认证的路由
	setupAuthRoutes(r)

	// 需要认证的路由：通用 API 路由组同时支持登录 JWT 和 type=api 的 API 令牌。
	authGroup := r.Group("/api")
	authGroup.Use(middleware.ApiTokenOrAuth("api"))
	authGroup.Use(middleware.SecurityGuard())
	authGroup.Use(middleware.PermissionGuard())
	{
		setupAuthMemberRoutes(authGroup)
		setupUserRoutes(authGroup)
		setupSystemRoutes(authGroup)
		setupFileRoutes(authGroup)
		setupOplogRoutes(authGroup)
		setupProcessRoutes(authGroup)
		setupAppRoutes(authGroup)
		setupSiteRoutes(authGroup)
		setupSiteSecurityRoutes(authGroup)
		setupDockerRoutes(authGroup)
		setupMonitorRoutes(authGroup)
		setupAlertRoutes(authGroup)
		setupBackupRoutes(authGroup)
		setupCronRoutes(authGroup)
		setupDatabaseRoutes(authGroup)
		registerDatabaseServiceRoutes(authGroup)
		setupFtpRoutes(authGroup)
		setupSettingsRoutes(authGroup)
		setupUpdateRoutes(authGroup)
		setupSecurityRoutes(authGroup)
		setupWAFRoutes(authGroup)
		setupRuntimeRoutes(authGroup)
		setupApiTokenRoutes(authGroup)     // API 令牌 CRUD（/api/api-tokens）
		setupTempAccessRoutes(authGroup)   // 临时访问（临时登录链接）
		setupMigrateRoutes(authGroup)      // 网站搬家（迁移）
	}

	// MCP 路由：只接受 JWT 和 type=mcp 的 API 令牌（防止 type=api 令牌误打到 MCP 端点）
	// 挂 PermissionGuard：JWT 子账号需要 mcp 功能权限；令牌按声明的 scopes 校验
	mcpGroup := r.Group("/api")
	mcpGroup.Use(middleware.ApiTokenOrAuth("mcp"))
	mcpGroup.Use(middleware.SecurityGuard())
	mcpGroup.Use(middleware.PermissionGuard())
	setupMcpRoutes(mcpGroup)

	// WebSocket 终端：不经过 HTTP Auth 中间件（浏览器 WebSocket 无法自定义 Header），
	// 由 HandleTerminalWS 内部通过 query token 自行校验。
	termGroup := r.Group("/api")
	setupTerminalRoutes(termGroup)

	// 下载/预览路由组：不经过 JWT/API token 鉴权（这些接口无法携带 Bearer 头，
	// 只能 URL 带一次性/预览 token），由各 handler 内部校验短期 token。
	// 仅保留 SecurityGuard（IP 黑白名单等）作为最外层防线。
	downloadGroup := r.Group("/api")
	downloadGroup.Use(middleware.SecurityGuard())
	setupDownloadRoutes(downloadGroup)

	// phpMyAdmin 反向代理：走面板端口，通过 query token 首次鉴权后种下 cookie，
	// 页面内静态资源通过 cookie 鉴权，避免 401 空白页。
	pmaGroup := r.Group("/phpmyadmin")
	pmaGroup.Use(middleware.PMAAuth())
	pmaGroup.Any("/*any", handlePMAProxy)

	// 托管前端静态文件
	serveFrontend(r, cfg)

	return r
}

// serveFrontend 托管前端：优先使用磁盘 {DataDir}/web（可覆盖内置前端），
// 否则使用二进制内置（go:embed）前端，实现前后端单文件部署。
// 前后端分离部署：把前端构建产物（webui/dist）作为独立文件夹上传到 {DataDir}/web
// 即可，后端磁盘优先托管，替换文件夹免重编译、刷新即生效。
func serveFrontend(r *gin.Engine, cfg *config.Config) {
	distPath := filepath.Join(cfg.DataDir, "web")
	if _, err := os.Stat(filepath.Join(distPath, "index.html")); err == nil {
		serveDist(r, os.DirFS(distPath))
		return
	}

	// 二进制内置前端（go:embed webui/dist）
	sub, err := fs.Sub(webui.Dist, "dist")
	if err != nil {
		r.NoRoute(func(c *gin.Context) {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "not found"})
		})
		return
	}
	serveDist(r, sub)
}

// serveDist 以给定文件系统托管前端静态资源（SPA）。
// 安全入口统一通过包级 currentEntrance 读取（启动时由 Setup 从 cfg 赋值，设置页修改后实时更新），
// 避免「闭包旧值」与「实时值」不一致导致入口校验错乱。
func serveDist(r *gin.Engine, dist fs.FS) {
	if assets, err := fs.Sub(dist, "assets"); err == nil {
		r.StaticFS("/assets", http.FS(assets))
	}
	// 暴露 dist 根目录下的图标资源（favicon.ico / favicon.png / logo.png 等），
	// 允许前端 index.html 的 <link rel="icon"> 和组件里的 <img src="/logo.png"> 引用。
	// 传统 nginx 等静态服务器会一并服务这些文件，但 Gin 必须显式声明每个。
	knownIcons := []string{"favicon.ico", "favicon.png", "logo.png", "apple-touch-icon.png"}
	for _, name := range knownIcons {
		if _, err := dist.Open(name); err == nil {
			r.GET("/"+name, gin.WrapH(http.FileServer(http.FS(dist))))
		}
	}
	r.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path
		// API / 代理路径交给后端处理，不返回前端页面
		if strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/phpmyadmin") {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "not found"})
			return
		}
		// 临时登录链接：免密进入后台的唯一公开入口，不受安全入口前缀限制。
		// 无 token / 前缀不对 / token 无效一律 404（不暴露该功能存在）；
		// token 有效才渲染前端，由前端 TempLogin.vue 用 Bearer token 调 /system/info
		// 完成登录态写入并进入后台。
		// 注意：这里必须用只读校验 CheckTempAccessValid（不扣次数），
		// 否则 NoRoute 扣 1 次后，前端 /system/info 二次校验会因 MaxUses=1 次数耗尽而 401。
		bypassEntrance := false
		if p == "/temp-login" {
			t := c.Query("token")
			// token 有效，或已登录（cookie 有效，例如登录后刷新本页）都放行渲染前端；
			// 否则一律 404，不暴露该功能存在。
			tokenOK := t != "" && strings.HasPrefix(t, service.TempAccessTokenPrefix) &&
				service.CheckTempAccessValid(t)
			if !tokenOK && !isLoggedInByCookie(c) {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "not found"})
				return
			}
			// 跳过下方安全入口校验（未登录 + 不带 /<entrance> 前缀会被拦截），
			// 直接渲染前端，交由前端 TempLogin.vue 完成登录态写入。
			bypassEntrance = true
		}
		// 安全入口：仅「未登录」时强制要求 /<entrance> 前缀；已登录（cookie token 有效）直接放行，
		// 这样登录后刷新 /dashboard 等任意路径都不会 404。
		// 未登录且 URL 不带入口前缀（如 /、/login、/dashboard）时，直接 404，
		// 只有通过安全入口 /<entrance> 才能进入登录页，登录页路径对未登录用户完全隐藏。
		// 注意：这里必须用包级 currentEntrance（实时值），不能用闭包参数 entrance（启动时定死的旧值），
		// 否则「改入口后」NoRoute 校验与 HTML 注入的入口值不一致，导致旧入口放行 + 新入口 404 的错乱。
		if currentEntrance != "" && currentEntrance != "-" && !bypassEntrance && !isLoggedInByCookie(c) {
			prefix := "/" + currentEntrance
			if p != prefix && !strings.HasPrefix(p, prefix+"/") {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "not found"})
				return
			}
		}
		idx, err := fs.ReadFile(dist, "index.html")
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "not found"})
			return
		}
		// index.html 不缓存：前端每次构建 chunk 文件名带 hash，若浏览器缓存旧 HTML，
		// 会引用已删除的旧 chunk 导致 404/MIME 错误。assets/* 仍可强缓存（文件名带 hash）。
		c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
		// 把安全入口值注入到 HTML，登录后前端从 URL 中去掉入口。
		// 入口通过包级 currentEntrance 读取（设置页修改后实时生效），而非闭包变量。
		html := injectSecurityEntrance(idx, currentEntrance)
		// 注入面板名称/副标题（用于浏览器标签 title、登录页/临时登录页显示）
		cfg := config.Get()
		html = injectPanelName(html, cfg.PanelName, cfg.PanelSub)
		c.Data(http.StatusOK, "text/html; charset=utf-8", html)
	})
}

// isLoggedInByCookie 通过 HttpOnly cookie 里的 token 判断是否已登录。
// 用于安全入口校验：登录后刷新页面（普通 GET 无 Authorization header）时靠 cookie 放行。
func isLoggedInByCookie(c *gin.Context) bool {
	token, err := c.Cookie(panelTokenCookie)
	if err != nil || token == "" {
		return false
	}
	// 临时访问 token：会话存活（存在/启用/未过期）即视为已登录。
	// 不检查使用次数——登录时已消费 1 次，之后刷新页面靠 cookie 放行，
	// 不能因为次数耗尽而刷新 404。
	if strings.HasPrefix(token, service.TempAccessTokenPrefix) {
		return service.CheckTempAccessSessionAlive(token)
	}
	claims, err := utils.ParseToken(token)
	if err != nil {
		return false
	}
	// 与正式鉴权中间件一致：改密/删除账号后旧 token 不应再被当作登录态
	if claims.TokenVer != service.TokenVerOf(claims.AdminID) {
		return false
	}
	return true
}

// jsSafe 生成安全的 JS 字符串字面量：
// strconv.Quote 负责引号/反斜杠/控制字符转义，再额外转义 < > &，
// 防止 <script> 被 </script> 提前闭合导致存储型 XSS（面板名称/副标题为管理员自定义值）。
func jsSafe(s string) string {
	r := strconv.Quote(s)
	r = strings.ReplaceAll(r, "<", "\\x3c")
	r = strings.ReplaceAll(r, ">", "\\x3e")
	r = strings.ReplaceAll(r, "&", "\\x26")
	return r
}

// injectSecurityEntrance 把安全入口值注入到 index.html（在 <head> 里插入 <script>），
// 前端通过 window.__SECURITY_ENTRANCE__ 读取，登录后从 URL 中去掉入口。
// 不修改 html 文件本身，避免构建期耦合；运行时注入，仅当非空时插入。
func injectSecurityEntrance(html []byte, entrance string) []byte {
	if entrance == "" {
		return html
	}
	// strings.Replace 替换 <head>（n=-1 表示全部替换，但 <head> 在 HTML 中只出现一次，安全）
	scriptTag := "<script>window.__SECURITY_ENTRANCE__=" + jsSafe(entrance) + "</script>"
	replaced := strings.Replace(string(html), "<head>", "<head>"+scriptTag, 1)
	if replaced == string(html) {
		// 找不到 <head> 头，回退到开头插入
		replaced = scriptTag + string(html)
	}
	return []byte(replaced)
}

// injectPanelName 把面板名称/副标题注入到 index.html（在 <title> 和 <head> 里插入 <script>）。
// 登录页未登录时也能用，让用户看到自己的品牌名。
func injectPanelName(page []byte, name, sub string) []byte {
	if name == "" {
		name = "开猿运维"
	}
	if sub == "" {
		sub = "Linux管理面板"
	}
	// 1. 替换 <title>，让浏览器标签显示自定义名（HTML 上下文，需转义防注入）
	titleTag := "<title>" + html.EscapeString(name) + " - " + html.EscapeString(sub) + "</title>"
	replaced := strings.Replace(string(page), "<title>开猿 Linux 面板</title>", titleTag, 1)
	if replaced == string(page) {
		// 兼容默认 title 被改的情况：再尝试 <title>xxx</title> 通用替换
		if idx := strings.Index(replaced, "<title>"); idx >= 0 {
			end := strings.Index(replaced[idx:], "</title>")
			if end >= 0 {
				replaced = replaced[:idx] + titleTag + replaced[idx+end+len("</title>"):]
			}
		}
	}
	// 2. 在 <head> 注入 window.__PANEL_NAME__ / __PANEL_SUB__（登录页/临时登录页使用）
	scriptTag := "<script>window.__PANEL_NAME__=" + jsSafe(name) +
		";window.__PANEL_SUB__=" + jsSafe(sub) + "</script>"
	if strings.Contains(replaced, "<head>") {
		replaced = strings.Replace(replaced, "<head>", "<head>"+scriptTag, 1)
	} else {
		replaced = scriptTag + replaced
	}
	return []byte(replaced)
}
