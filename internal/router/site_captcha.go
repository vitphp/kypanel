package router

import (
	"net/http"
	"strconv"
	"strings"

	"kypanel/internal/service"
	"kypanel/internal/utils"

	"github.com/gin-gonic/gin"
)

// setupSiteCaptchaRoutes 注册拖拽验证码的公开接口（无需登录）。
// 这些接口由站点 nginx 经 auth_request / 反代 同域暴露给访客，必须放在 serveFrontend 之前注册，
// 避免被 SPA 的 NoRoute 兜底拦截。同时在 middleware.DomainGuard 中已对路径豁免域名白名单。
func setupSiteCaptchaRoutes(r *gin.Engine) {
	// 挑战页（面板托管，返回自包含 HTML，由 nginx error_page 401 触发）
	r.GET("/captcha-challenge", func(c *gin.Context) {
		// 从自身 query 读取 site 并注入 HTML（覆盖 __LP_SITE__ 占位符）。
		// 关键：error_page 401 内部重定向时浏览器地址栏仍是原路径，location.search 无法取到
		// ?site=xxx，必须从面板这一侧的 query 取，否则挑战页 JS 会拿到 site=0 → 加载失败。
		// Apache 版闸门经 ProxyPass 反代挑战页时不带 query，改用 X-Lp-Site 头传递 site，故此处回退读取该头。
		siteStr := c.Query("site")
		if siteStr == "" {
			siteStr = c.GetHeader("X-Lp-Site")
		}
		// 安全：site 会被原样注入挑战页 HTML，必须只允许数字，杜绝反射型 XSS（?site=<script>）。
		siteStr = sanitizeSiteParam(siteStr)
		// ReplaceAll：模板里 __LP_SITE__ 出现在两处（注释 + 实际 var），必须全部替换，
		// 否则用 Replace(...,1) 只会替换注释里的那个，var 仍是占位符 → 验证码加载失败。
		html := strings.ReplaceAll(service.SiteCaptchaChallengeHTML, "__LP_SITE__", siteStr)
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
	})

	// 获取拼图挑战
	r.GET("/api/site/captcha/puzzle", func(c *gin.Context) {
		siteID, err := strconv.ParseUint(c.Query("site"), 10, 32)
		if err != nil || siteID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		ch, e := service.GenerateSiteCaptcha(uint(siteID))
		if e != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "生成验证码失败"})
			return
		}
		// 直接返回 challenge 字段（不包 utils.Ok 的 {code,msg,data} 包装）：
		// 挑战页是站点侧手写 JS 通过 fetch+JSON 直接读 d.token/d.bg/d.piece，
		// 若包一层 data 就读不到，表现为"验证码加载失败"。
		c.JSON(http.StatusOK, ch)
	})

	// 校验拖拽结果，通过后下发放行 cookie（HttpOnly）
	r.POST("/api/site/captcha/verify", func(c *gin.Context) {
		var req struct {
			Token string `json:"token"`
			X     int    `json:"x"`
		}
		if e := c.ShouldBindJSON(&req); e != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if req.Token == "" {
			utils.Fail(c, 400, "缺少令牌")
			return
		}
		// site 优先取 URL 参数；前端经 nginx 反代调用时不带 site 参数，
		// 故回退从 token 中解析 siteID（token 已内含 siteID，可校验一致性）。
		siteID, _ := strconv.ParseUint(c.Query("site"), 10, 32)
		if siteID == 0 {
			siteID = uint64(service.SiteIDFromCaptchaToken(req.Token))
		}
		if siteID == 0 {
			utils.Fail(c, 400, "令牌无效")
			return
		}
		if ok, _ := service.VerifySiteCaptcha(uint(siteID), req.Token, req.X); !ok {
			utils.Fail(c, 400, "验证失败")
			return
		}
		val, ttl := service.IssueSiteCaptchaCookie(uint(siteID), service.GetSiteCaptchaTTL(uint(siteID)))
		c.SetCookie("lp_captcha", val, ttl, "/", "", false, true)
		utils.Ok(c, nil)
	})

	// 由 nginx auth_request 调用：校验放行 cookie 是否有效（仅做 HMAC，无 DB 压力）
	r.GET("/api/site/captcha/check", func(c *gin.Context) {
		siteID, err := strconv.ParseUint(c.Query("site"), 10, 32)
		if err != nil || siteID == 0 {
			c.Status(http.StatusUnauthorized)
			c.Writer.Write([]byte("denied"))
			return
		}
		cookie, _ := c.Cookie("lp_captcha")
		if service.CheckSiteCaptchaCookie(uint(siteID), cookie) {
			c.Status(http.StatusOK)
			c.Writer.Write([]byte("ok"))
			return
		}
		c.Status(http.StatusUnauthorized)
		c.Writer.Write([]byte("denied"))
	})
}

// sanitizeSiteParam 只保留数字，空/非法一律回退 "0"，防止 site 参数注入挑战页 HTML。
func sanitizeSiteParam(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	if out := b.String(); out != "" {
		return out
	}
	return "0"
}
