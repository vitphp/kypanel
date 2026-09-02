package router

import (
	"github.com/gin-gonic/gin"

	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// setupSystemRoutes 系统信息路由
func setupSystemRoutes(g *gin.RouterGroup) {
	// 首页系统概览
	g.GET("/system/info", func(c *gin.Context) {
		info, err := service.GetSystemInfo()
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, info)
	})

	// 概览页聚合数据（网络速率 / 进程 TOP / 服务状态）
	g.GET("/dashboard/summary", func(c *gin.Context) {
		utils.Ok(c, service.GetDashboardSummary())
	})

	// 概览页卡片布局
	g.GET("/dashboard/layout", func(c *gin.Context) {
		utils.Ok(c, gin.H{"layout": service.GetDashboardLayout()})
	})
	g.PUT("/dashboard/layout", func(c *gin.Context) {
		var req struct {
			Layout []string `json:"layout" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.SaveDashboardLayout(req.Layout); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		utils.Ok(c, gin.H{"message": "保存成功"})
	})

	// 通用命令执行（仅超级管理员：命令执行等同于 root shell，必须超管权限）
	g.POST("/system/exec", func(c *gin.Context) {
		if !service.IsSuperAdmin(c.GetUint("admin_id")) {
			utils.Fail(c, 403, "仅超级管理员可执行命令")
			return
		}
		var req struct {
			Cmd string `json:"cmd" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		res, err := service.ExecCommand(req.Cmd, service.DefaultExecTimeout())
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "system.exec", "", "success", req.Cmd)
		utils.Ok(c, res)
	})

	// 重启面板服务（仅 KyPanel 自身进程）：通过 systemd 触发，进程在调用返回前就会
	// 收到 SIGTERM 重启，期间 /api/ping 会短时 502/超时，前端用 ping 探测来恢复刷新。
	g.POST("/system/restart-panel", func(c *gin.Context) {
		if !service.IsSuperAdmin(c.GetUint("admin_id")) {
			utils.Fail(c, 403, "仅超级管理员可重启面板")
			return
		}
		// 同步记录操作日志（异步重启可能写不进——response 提前回，gin 通道已关）
		recordOpForCtx(c, "system.restart_panel", "重启面板服务", "success")
		// 延迟 800ms 再执行，让当前 response 顺利返回
		service.RestartPanel()
		utils.Ok(c, gin.H{"message": "面板重启指令已发出"})
	})

	// 重启服务器：reboot，必须前端二次确认后才到这里
	g.POST("/system/restart-server", func(c *gin.Context) {
		if !service.IsSuperAdmin(c.GetUint("admin_id")) {
			utils.Fail(c, 403, "仅超级管理员可重启服务器")
			return
		}
		recordOpForCtx(c, "system.restart_server", "重启服务器", "success")
		service.RestartServer()
		utils.Ok(c, gin.H{"message": "服务器重启指令已发出"})
	})
}
