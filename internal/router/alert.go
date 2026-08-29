package router

import (
	"github.com/gin-gonic/gin"

	"kypanel/internal/model"
	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// setupAlertRoutes 告警通知路由
func setupAlertRoutes(g *gin.RouterGroup) {
	// 获取告警配置 + 最近告警记录
	g.GET("/alert/settings", func(c *gin.Context) {
		utils.Ok(c, service.GetAlertSettings())
	})

	// 更新告警配置
	g.POST("/alert/settings", func(c *gin.Context) {
		var req model.AlertConfig
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.UpdateAlertSettings(req); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, nil)
	})

	// 测试通知渠道
	g.POST("/alert/test-channel", func(c *gin.Context) {
		var ch model.AlertChannel
		if err := c.ShouldBindJSON(&ch); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.TestAlertChannel(ch); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, nil)
	})

	// 清空告警记录
	g.POST("/alert/clear-logs", func(c *gin.Context) {
		if err := service.ClearAlertLogs(); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, nil)
	})
}
