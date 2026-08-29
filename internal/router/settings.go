package router

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"

	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// setupSettingsRoutes 设置与日志路由
func setupSettingsRoutes(g *gin.RouterGroup) {
	// 面板信息
	g.GET("/settings/info", func(c *gin.Context) {
		utils.Ok(c, service.GetPanelInfo())
	})

	// 修改管理员用户名
	g.POST("/settings/username", func(c *gin.Context) {
		var req struct {
			Username string `json:"username" binding:"required,min=1,max=64"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		adminID := c.GetUint("admin_id")
		if err := service.ChangeUsername(adminID, req.Username); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "settings.username", "修改管理员账号为 "+req.Username, "success")
		utils.Ok(c, nil)
	})

	// 保存面板端口
	g.POST("/settings/port", func(c *gin.Context) {
		var req service.SavePortReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		oldPort, err := service.SavePanelPort(req)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		// 端口实际改变时，异步重启 panel（800ms 后），让新端口生效；
		// 同步响应在重启前已返回给前端
		if oldPort != req.Port {
			service.RestartPanel()
		}
		service.RecordOp(c.GetUint("admin_id"), "settings.port", fmt.Sprintf("修改面板端口为 %d", req.Port), c.ClientIP(), "success")
		utils.Ok(c, nil)
	})

	// 保存面板绑定域名
	g.POST("/settings/domain", func(c *gin.Context) {
		var req service.SaveDomainReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.SavePanelDomain(req); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "settings.domain", "修改面板绑定域名: "+req.Domain, "success")
		utils.Ok(c, nil)
	})

	// 保存面板安全入口（登录页 URL 前缀；空值表示关闭入口）
	g.POST("/settings/security-entrance", func(c *gin.Context) {
		var req service.SaveEntranceReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		old, err := service.SavePanelSecurityEntrance(req)
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		// 立即同步内存中的 currentEntrance（SavePanelSecurityEntrance 内部已同步写 config.json），
		// 使后续登录/验证码请求、NoRoute 校验都走新入口，无需重启 panel。
		currentEntrance = req.Entrance
		recordOpForCtx(c, "settings.entrance", "修改安全入口（旧值已隐藏，新入口请立即记录）", "success")
		utils.Ok(c, gin.H{"entrance": req.Entrance, "old": old})
	})

	// 生成新的 6 位随机入口串（不保存；前端预览后由用户点击「保存」才落盘）
	g.GET("/settings/security-entrance/generate", func(c *gin.Context) {
		utils.Ok(c, gin.H{"entrance": service.GenerateSecurityEntrance()})
	})

	// 保存面板名称/副标题（登录页、顶栏、浏览器标签都基于此显示）
	g.POST("/settings/panel-name", func(c *gin.Context) {
		var req service.SavePanelNameReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.SavePanelName(req); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "settings.panel_name", "修改面板名称: "+req.Name, "success")
		utils.Ok(c, gin.H{"name": req.Name, "sub": req.Sub})
	})

	// 保存 MySQL root 密码
	g.POST("/settings/mysql-password", func(c *gin.Context) {
		var req struct {
			Password string `json:"password" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.SetMysqlRootPwd(req.Password); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "settings.mysql_pwd", "更新 MySQL root 密码配置", "success")
		utils.Ok(c, nil)
	})

	// LiteSSL EAB 凭据
	g.GET("/settings/litessl", func(c *gin.Context) {
		utils.Ok(c, service.GetLiteSSLSetting())
	})
	g.POST("/settings/litessl", func(c *gin.Context) {
		var req service.LiteSSLSetting
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.SetLiteSSLSetting(req); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "settings.litessl", "更新 LiteSSL EAB 凭据", "success")
		utils.Ok(c, nil)
	})

	// 系统日志（面板运行日志）：返回结构化日志行（时间/级别/IP/消息）
	g.GET("/logs/system", func(c *gin.Context) {
		lines := 200
		if l, err := strconv.Atoi(c.DefaultQuery("lines", "200")); err == nil && l > 0 && l <= 1000 {
			lines = l
		}
		list, err := service.ReadSystemLog(lines)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, gin.H{"lines": list, "count": len(list)})
	})

	// 清空系统日志文件
	g.POST("/logs/system/clear", func(c *gin.Context) {
		if err := service.ClearSystemLog(); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		service.RecordOp(c.GetUint("admin_id"), "system.clear_log", "清空系统日志", c.ClientIP(), "success")
		utils.Ok(c, nil)
	})

	// 应用安装日志列表
	g.GET("/logs/install-list", func(c *gin.Context) {
		utils.Ok(c, service.ListInstallLogs())
	})

	// ---- 安全加固：登录 IP 白名单 ----
	g.GET("/settings/login-allowlist", func(c *gin.Context) {
		utils.Ok(c, service.GetLoginAllowList())
	})
	g.POST("/settings/login-allowlist", func(c *gin.Context) {
		var req struct {
			List []string `json:"list"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.SetLoginAllowList(req.List); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "settings.login_allowlist", "更新登录 IP 白名单", "success")
		utils.Ok(c, nil)
	})

	// ---- 安全加固：会话管理 ----
	g.GET("/settings/sessions", func(c *gin.Context) {
		// 注入当前 token 指纹，让前端能标出"当前会话"避免误踢
		currentFP := c.GetString("token_fingerprint")
		utils.Ok(c, service.ListSessions(currentFP))
	})
	g.POST("/settings/session/kick", func(c *gin.Context) {
		var req struct {
			SessionID uint `json:"session_id"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.KickSession(req.SessionID); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "settings.session_kick", "踢下线会话", "success")
		utils.Ok(c, nil)
	})
	g.POST("/settings/session/kick-all", func(c *gin.Context) {
		var req struct {
			AdminID uint `json:"admin_id"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		// 关键：保留当前会话（按 current_token_fingerprint 排除），踢掉其他所有会话
		currentFP := c.GetString("token_fingerprint")
		affected := service.KickOtherSessions(req.AdminID, currentFP)
		recordOpForCtx(c, "settings.session_kick_all",
			fmt.Sprintf("踢下线其他会话（保留当前）共 %d 条", affected), "success")
		utils.Ok(c, nil)
	})
}
