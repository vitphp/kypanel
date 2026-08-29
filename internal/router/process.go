package router

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// setupProcessRoutes 进程管理路由
func setupProcessRoutes(g *gin.RouterGroup) {
	// 进程列表
	g.GET("/process/list", func(c *gin.Context) {
		sortKey := c.DefaultQuery("sort", "cpu")
		keyword := c.Query("q")
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
		if limit <= 0 || limit > 1000 {
			limit = 200
		}

		procs, err := service.ListProcesses(sortKey, keyword, limit)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, procs)
	})

	// 结束进程
	g.POST("/process/kill", func(c *gin.Context) {
		var req struct {
			PID int `json:"pid" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.KillProcess(req.PID); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "process.kill", "PID: "+strconv.Itoa(req.PID), "success")
		utils.Ok(c, nil)
	})
}
