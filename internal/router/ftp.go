package router

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// setupFtpRoutes FTP 管理路由
func setupFtpRoutes(g *gin.RouterGroup) {
	// vsftpd 可用性
	g.GET("/ftp/status", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		ok, msg := service.FtpAvailable()
		utils.Ok(c, gin.H{"available": ok, "message": msg, "version": service.FtpVersion()})
	})

	// 用户列表
	g.GET("/ftp/list", func(c *gin.Context) {
		utils.Ok(c, service.ListFtpUsers())
	})

	// 创建用户
	g.POST("/ftp/create", func(c *gin.Context) {
		var req service.CreateFtpUserReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		if err := service.CreateFtpUser(req); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "ftp.create", "创建 FTP 用户: "+req.Username, "success")
		utils.Ok(c, nil)
	})

	// 删除用户
	g.POST("/ftp/delete", func(c *gin.Context) {
		var req struct {
			ID uint `json:"id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.DeleteFtpUser(req.ID); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "ftp.delete", "#"+strconv.FormatUint(uint64(req.ID), 10), "success")
		utils.Ok(c, nil)
	})

	// 启停用户
	g.POST("/ftp/toggle", func(c *gin.Context) {
		var req struct {
			ID     uint `json:"id" binding:"required"`
			Enable bool `json:"enable"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.ToggleFtpUser(req.ID, req.Enable); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, nil)
	})
}
