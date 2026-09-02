package router

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// recordOpForCtx 从 gin.Context 提取操作元信息（来源/账号/IP/临时访问 ID），
// 委托 service.RecordOpWithSource 记录操作日志。
// 这是 router 层对 gin 的适配器，让 service 层不依赖 gin。
func recordOpForCtx(c *gin.Context, action, detail, status string, rawCmd ...string) {
	service.RecordOpWithSource(
		c.GetString("auth_type"),
		c.GetUint("admin_id"),
		c.GetUint("temp_access_id"),
		c.ClientIP(),
		action, detail, status, rawCmd...,
	)
}

// setupOplogRoutes 操作日志路由
func setupOplogRoutes(g *gin.RouterGroup) {
	g.GET("/oplog/list", func(c *gin.Context) {
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
		source := c.Query("source") // login / api / mcp
		if page < 1 {
			page = 1
		}
		if pageSize < 1 || pageSize > 100 {
			pageSize = 20
		}
		list, total, err := service.ListOps(page, pageSize, source)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, gin.H{"list": list, "total": total})
	})

	// 申请一次性下载 token（需登录态；实际导出在 setupDownloadRoutes 的免 JWT 组）
	g.POST("/oplog/export-token", func(c *gin.Context) {
		token := service.NewDownloadToken("oplog", "all")
		utils.Ok(c, gin.H{"token": token})
	})

	// 清空操作日志（可按 source 选择性清空）
	g.POST("/oplog/clear", func(c *gin.Context) {
		source := c.Query("source") // login / api / mcp；空=清空全部
		if source == "" {
			// 旧接口不传 source 时清空全部（保持向后兼容）
			if err := service.ClearOps(); err != nil {
				utils.Fail(c, 500, err.Error())
				return
			}
			recordOpForCtx(c, "oplog.clear", "清空操作日志（全部）", "success")
			utils.Ok(c, nil)
			return
		}
		if err := service.ClearOpsBySource(source); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "oplog.clear", "清空操作日志（"+source+"）", "success")
		utils.Ok(c, nil)
	})
}
