package router

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"kypanel/internal/middleware"
	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// setupAuthRoutes 认证相关路由（无需 JWT）。
// 登录 / 验证码接口的路径内嵌安全入口值：/api/<entrance>/auth/login、/api/<entrance>/auth/captcha。
// 攻击者不知道安全入口（6 位随机串）就无法拼出正确的 API 路径，直接返回 404，
// 从根本上阻止绕过安全入口扫描接口进行密码爆破。
func setupAuthRoutes(r *gin.Engine) {
	api := r.Group("/api")

	// 登录处理器（提取出来，供入口路径与兼容路径复用）
	loginHandler := func(c *gin.Context) {
		var req service.LoginReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		res := service.Login(req, c.ClientIP(), c.GetHeader("User-Agent"))
		if res.Err != nil {
			// 失败时携带 need_captcha / need_totp 字段，前端按需显示对应输入框
			c.JSON(http.StatusOK, gin.H{
				"code":         401,
				"msg":          res.Err.Error(),
				"need_captcha": res.NeedCaptcha,
				"need_totp":    res.NeedTotp,
			})
			return
		}
		// 登录成功：把 token 写入 HttpOnly cookie，供 NoRoute 安全入口校验判断「已登录」
		c.SetCookie(panelTokenCookie, res.Resp.Token, service.TokenTTLHours()*3600, "/", "", middleware.CookieSecure(c), true)
		utils.Ok(c, res.Resp)
	}

	captchaHandler := func(c *gin.Context) {
		ip := c.ClientIP()
		// 该 IP 密码错误 1 次后才允许获取验证码（防止首次访问就拉一堆码消耗）
		if !service.NeedCaptchaForLogin(ip) {
			c.JSON(http.StatusOK, gin.H{"code": 403, "msg": "暂无需验证码"})
			return
		}
		code, err := service.GenerateCaptchaCode()
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "生成验证码失败"})
			return
		}
		service.SaveCaptcha(ip, code)
		png, err := service.DrawCaptchaPNG(code)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "渲染验证码失败"})
			return
		}
		c.Data(http.StatusOK, "image/png", png)
	}

	// 是否需要验证码检查（仅查询状态，不生成验证码；前端进入登录页时调用决定是否显示输入框）
	captchaCheckHandler := func(c *gin.Context) {
		ip := c.ClientIP()
		c.JSON(http.StatusOK, gin.H{
			"code":         0,
			"need_captcha": service.NeedCaptchaForLogin(ip),
		})
	}

	if currentEntrance != "" && currentEntrance != "-" {
		// 启用安全入口：登录/验证码接口路径内嵌入口值
		entrance := "/" + currentEntrance
		api.POST(entrance+"/auth/login", loginHandler)
		api.GET(entrance+"/auth/captcha", captchaHandler)
		api.GET(entrance+"/auth/captcha-check", captchaCheckHandler)
	} else {
		// 未启用安全入口：保持原始路径（向后兼容）
		api.POST("/auth/login", loginHandler)
		api.GET("/auth/captcha", captchaHandler)
		api.GET("/auth/captcha-check", captchaCheckHandler)
	}
}

// setupAuthMemberRoutes 登录后才能访问的账号相关路由
func setupAuthMemberRoutes(g *gin.RouterGroup) {
	// 退出登录：清除登录态 cookie（前端同时清 localStorage）
	g.POST("/auth/logout", func(c *gin.Context) {
		c.SetCookie(panelTokenCookie, "", -1, "/", "", middleware.CookieSecure(c), true)
		utils.Ok(c, nil)
	})

	g.POST("/auth/change-password", func(c *gin.Context) {
		var req service.ChangePasswordReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		adminID := c.GetUint("admin_id")
		if err := service.ChangePassword(adminID, req); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		// 改密后清除该管理员的 token 版本缓存，使旧 token 立即失效
		service.InvalidateTokenVer(adminID)
		utils.Ok(c, nil)
	})

	g.GET("/auth/profile", func(c *gin.Context) {
		utils.Ok(c, gin.H{
			"admin_id": c.GetUint("admin_id"),
			"username": c.GetString("username"),
		})
	})

	// ---- 2FA / TOTP ----
	// 查询 2FA 状态
	g.GET("/auth/totp/status", func(c *gin.Context) {
		utils.Ok(c, service.TOTPStatus(c.GetUint("admin_id")))
	})
	// 开始启用：生成密钥 + 二维码 URI（暂不保存）
	g.POST("/auth/totp/enable-begin", func(c *gin.Context) {
		adminID := c.GetUint("admin_id")
		username := c.GetString("username")
		utils.Ok(c, service.TOTPEnableBegin(adminID, username))
	})
	// 确认启用：校验验证码后保存
	g.POST("/auth/totp/enable-confirm", func(c *gin.Context) {
		var req struct {
			Secret string `json:"secret" binding:"required"`
			Code   string `json:"code" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.TOTPEnableConfirm(c.GetUint("admin_id"), req.Secret, req.Code); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "auth.totp_enable", "启用双因素认证", "success")
		utils.Ok(c, nil)
	})
	// 关闭 2FA（需校验当前验证码）
	g.POST("/auth/totp/disable", func(c *gin.Context) {
		var req struct {
			Code string `json:"code" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.TOTPDisable(c.GetUint("admin_id"), req.Code); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "auth.totp_disable", "关闭双因素认证", "success")
		utils.Ok(c, nil)
	})
}
