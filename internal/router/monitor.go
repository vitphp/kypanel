package router

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// setupMonitorRoutes 监控路由
func setupMonitorRoutes(g *gin.RouterGroup) {
	// 监控配置
	g.GET("/monitor/config", func(c *gin.Context) {
		utils.Ok(c, service.GetMonitorConfig())
	})

	// 更新监控配置
	g.POST("/monitor/config", func(c *gin.Context) {
		var req service.MonitorConfig
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.UpdateMonitorConfig(req); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, nil)
	})

	// 历史监控数据（支持时间范围）
	g.GET("/monitor/history", func(c *gin.Context) {
		rangeType := c.Query("range")
		var start, end int64
		if v, err := strconv.ParseInt(c.Query("start"), 10, 64); err == nil {
			start = v
		}
		if v, err := strconv.ParseInt(c.Query("end"), 10, 64); err == nil {
			end = v
		}
		utils.Ok(c, service.GetMonitorHistoryByRange(rangeType, start, end))
	})

	// 当前监控值
	g.GET("/monitor/current", func(c *gin.Context) {
		utils.Ok(c, service.GetMonitorCurrent())
	})

	// 监控数据文件大小
	g.GET("/monitor/size", func(c *gin.Context) {
		utils.Ok(c, map[string]int64{"bytes": service.MonitorLogSize()})
	})

	// 清空监控历史
	g.POST("/monitor/clear", func(c *gin.Context) {
		if err := service.ClearMonitorHistory(); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, nil)
	})

	// 监控统计（平均值）
	g.GET("/monitor/stats", func(c *gin.Context) {
		utils.Ok(c, service.MonitorStats())
	})
}
