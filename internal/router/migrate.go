package router

// 网站搬家（迁移）路由
// 本组路由挂载在 authGroup 下，自动获得 JWT / API 令牌（Bearer 或 GET query token）鉴权。
// /api/migrate/remote/* 为「源面板对外接口」，供目标面板迁入时调用。

import (
	"github.com/gin-gonic/gin"

	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// setupMigrateRoutes 网站搬家路由
func setupMigrateRoutes(g *gin.RouterGroup) {
	// ---- 本机环境 ----
	g.GET("/migrate/env", func(c *gin.Context) {
		utils.Ok(c, service.MigrateEnvStatus())
	})

	// ---- 自动探测面板类型（kypanel / bt）----
	g.POST("/migrate/detect-panel", func(c *gin.Context) {
		var req struct {
			URL   string `json:"url"`
			Token string `json:"token"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			// 透传具体原因，便于排查（笼统的「参数错误」无法定位问题）
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		res, err := service.DetectPanel(req.URL, req.Token)
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		utils.Ok(c, res)
	})

	// ---- 本机打包（迁出源端）----
	g.POST("/migrate/export", func(c *gin.Context) {
		var req struct {
			Sites     []string `json:"sites"`
			Databases []string `json:"databases"`
			FTPs      []string `json:"ftps"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		exp, err := service.ExportMigration(req.Sites, req.Databases, req.FTPs)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, exp)
	})

	// ---- 迁出前环境预检（对比目标面板环境）----
	g.POST("/migrate/export/precheck", func(c *gin.Context) {
		var req service.ExportPrecheckRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		res, err := service.ExportPrecheck(req)
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		utils.Ok(c, res)
	})

	// ---- 迁出到对端面板（异步任务）----
	g.POST("/migrate/export/bt", func(c *gin.Context) {
		var req service.BTExportRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		id, err := service.StartExportToBT(req)
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		utils.Ok(c, gin.H{"id": id})
	})

	// ---- 迁出到对端面板：迁移前冲突检测（网站/数据库/FTP 同名检测）----
	g.POST("/migrate/export/bt/precheck", func(c *gin.Context) {
		var req service.BTPrecheckRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		res, err := service.BTPrecheck(req)
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		utils.Ok(c, res)
	})

	// ---- 对端面板环境预检 ----
	g.POST("/migrate/bt/precheck", func(c *gin.Context) {
		var req struct {
			BTURL     string   `json:"bt_url"`
			BTSK      string   `json:"bt_sk"`
			Sites     []string `json:"sites"`
			Databases []string `json:"databases"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		res, err := service.BTEnvCompare(req.BTURL, req.BTSK, req.Sites, req.Databases)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, res)
	})

	// ---- 迁移包列表 / 删除 / 下载 ----
	g.GET("/migrate/exports", func(c *gin.Context) {
		utils.Ok(c, service.ListMigrationExports())
	})
	g.DELETE("/migrate/exports/:id", func(c *gin.Context) {
		if err := service.DeleteMigrationExport(c.Param("id")); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, nil)
	})
	g.GET("/migrate/download/:id", func(c *gin.Context) {
		id := c.Param("id")
		p, err := service.MigrationPackagePath(id)
		if err != nil {
			utils.Fail(c, 404, err.Error())
			return
		}
		c.FileAttachment(p, id+".tar.gz")
	})

	// ---- 迁入：获取源面板网站列表 ----
	g.POST("/migrate/import/fetch-sites", func(c *gin.Context) {
		var req service.ImportPlanRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		list, err := service.FetchRemoteSites(req)
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		utils.Ok(c, list)
	})

	// ---- 迁入：环境对比计划 ----
	g.POST("/migrate/import/plan", func(c *gin.Context) {
		var req service.ImportPlanRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		res, err := service.BuildImportPlan(req)
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		utils.Ok(c, res)
	})

	// ---- 迁入：开始迁移（异步任务）----
	g.POST("/migrate/import/run", func(c *gin.Context) {
		var req service.ImportRunRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		id, err := service.StartImport(req)
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		utils.Ok(c, gin.H{"id": id})
	})

	// ---- 任务列表：重新打开搬家工具时用它恢复「进行中」的任务进度 ----
	g.GET("/migrate/tasks", func(c *gin.Context) {
		utils.Ok(c, service.ListImportTasks())
	})

	// ---- 任务状态（导入/对端面板迁出通用）----
	g.GET("/migrate/tasks/:id", func(c *gin.Context) {
		t, err := service.ImportTaskStatus(c.Param("id"))
		if err != nil {
			utils.Fail(c, 404, err.Error())
			return
		}
		utils.Ok(c, t)
	})

	// ---- 取消进行中的迁移（下载/导入中途立即中断）----
	g.POST("/migrate/tasks/:id/cancel", func(c *gin.Context) {
		if err := service.CancelImportTask(c.Param("id")); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		utils.Ok(c, nil)
	})

	// ---- 源面板对外接口（供目标面板迁入时调用）----
	setupMigrateRemoteRoutes(g)
}

func setupMigrateRemoteRoutes(g *gin.RouterGroup) {
	// 面板信息探测
	g.GET("/migrate/remote/ping", func(c *gin.Context) {
		utils.Ok(c, gin.H{"panel_type": "kypanel", "version": service.PanelVersion()})
	})
	// 网站列表（含关联数据库/FTP）
	g.GET("/migrate/remote/sites", func(c *gin.Context) {
		utils.Ok(c, service.RemoteSiteList())
	})
	// 环境状态
	g.GET("/migrate/remote/env", func(c *gin.Context) {
		utils.Ok(c, service.MigrateEnvStatus())
	})
	// 打包
	g.POST("/migrate/remote/pack", func(c *gin.Context) {
		var req struct {
			Sites     []string `json:"sites"`
			Databases []string `json:"databases"`
			FTPs      []string `json:"ftps"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		id, err := service.RemotePack(req.Sites, req.Databases, req.FTPs)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, gin.H{"id": id})
	})
	// 下载迁移包（GET，支持 ?token=）
	g.GET("/migrate/remote/download", func(c *gin.Context) {
		id := c.Query("id")
		if id == "" {
			utils.Fail(c, 400, "缺少参数 id")
			return
		}
		p, err := service.MigrationPackagePath(id)
		if err != nil {
			utils.Fail(c, 404, err.Error())
			return
		}
		c.FileAttachment(p, id+".tar.gz")
	})
}
