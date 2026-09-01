package router

import (
	"github.com/gin-gonic/gin"

	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// setupAppRoutes 应用商店路由
func setupAppRoutes(g *gin.RouterGroup) {
	// 应用列表（含安装状态）
	g.GET("/apps/list", func(c *gin.Context) {
		utils.Ok(c, service.ListApps())
	})

	// 应用分类
	g.GET("/apps/categories", func(c *gin.Context) {
		utils.Ok(c, service.AppCategories())
	})

	// 刷新远程应用商店缓存（应用 + 分类），前端进入应用商店页时调用；
	// 官网后台变更应用后，刷新面板页面即可生效。
	g.POST("/apps/refresh", func(c *gin.Context) {
		if err := service.RefreshRemoteStore(); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, nil)
	})

	// 已安装的 PHP 版本列表（供应用选择 PHP 版本）
	g.GET("/apps/php-versions", func(c *gin.Context) {
		utils.Ok(c, service.ListPhpVersions())
	})

	// 安装应用
	g.POST("/apps/install", func(c *gin.Context) {
		var req struct {
			Key        string `json:"key" binding:"required"`
			PhpVersion string `json:"php_version"`
			Version    string `json:"version"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.InstallApp(req.Key, req.PhpVersion, req.Version); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "app.install", "开始安装应用: "+req.Key, "success")
		utils.Ok(c, nil)
	})

	// 环境安装状态（数据库 / FTP / Docker / Nginx / 运行时 / 多版本 PHP/Node）
	g.GET("/apps/env-status", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		utils.Ok(c, service.EnvStatus())
	})

	// 强制重新探测环境状态（前端「重新检测环境」按钮）：
	// env-status 默认有 TTL 缓存，用户手动安装/卸载后点此按钮可立即刷新，
	// 无需等待 TTL 过期或重启面板。
	g.POST("/apps/env-status/refresh", func(c *gin.Context) {
		utils.Ok(c, service.RefreshEnvStatus())
	})

	// 卸载应用
	g.POST("/apps/uninstall", func(c *gin.Context) {
		var req struct {
			Key string `json:"key" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.UninstallApp(req.Key); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "app.uninstall", "开始卸载应用: "+req.Key, "success")
		utils.Ok(c, nil)
	})

	// 服务启停
	g.POST("/apps/service/action", func(c *gin.Context) {
		var req service.ServiceActionReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.ServiceAction(req); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "app.service", req.Action+" 服务: "+req.Key, "success")
		utils.Ok(c, nil)
	})

	// 服务运行状态
	g.GET("/apps/service/status", func(c *gin.Context) {
		key := c.Query("key")
		status, err := service.ServiceStatus(key)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, gin.H{"key": key, "status": status})
	})

	// 安装/卸载日志
	g.GET("/apps/log", func(c *gin.Context) {
		key := c.Query("key")
		lines := 200
		logText, err := service.ReadAppLog(key, lines)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, gin.H{"key": key, "log": logText})
	})

	// 当前正在执行的安装/卸载任务队列（用于面板右下角浮窗提示）
	g.GET("/apps/tasks", func(c *gin.Context) {
		utils.Ok(c, service.ListAppTasks())
	})

	// 停止正在执行的安装/卸载任务
	g.POST("/apps/tasks/cancel", func(c *gin.Context) {
		var req struct {
			Key string `json:"key" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.CancelAppTask(req.Key); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "app.cancel", "停止任务: "+req.Key, "success")
		utils.Ok(c, nil)
	})
}
