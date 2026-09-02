package router

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"kypanel/internal/model"
	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// setupCronRoutes 计划任务路由
func setupCronRoutes(g *gin.RouterGroup) {
	// 写操作（创建/删除/启停/执行）需要超级管理员：计划任务以 root 执行任意命令，
	// 等同于 shell 权限，必须硬性限定超管，避免子账号通过 cron 提权。
	superAdminOnly := func(c *gin.Context) {
		if !service.IsSuperAdmin(c.GetUint("admin_id")) {
			utils.Fail(c, 403, "仅超级管理员可管理计划任务")
			c.Abort()
			return
		}
		c.Next()
	}

	// 任务列表
	g.GET("/cron/list", func(c *gin.Context) {
		utils.Ok(c, service.ListCrons())
	})

	// 模板列表（前端向导用）
	g.GET("/cron/templates", func(c *gin.Context) {
		utils.Ok(c, service.GetCronTemplates())
	})

	// 模板预览：根据用户填的字段生成实际 shell 命令（不写入数据库）
	g.POST("/cron/preview", func(c *gin.Context) {
		var req service.CronTemplateReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		_, cmd, err := service.GenerateCronFromTemplate(req)
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		utils.Ok(c, map[string]any{"command": cmd})
	})

	// 数据源：可选站点（备份网站/日志切割）
	g.GET("/cron/sites", func(c *gin.Context) {
		items := service.ListSites()
		out := make([]map[string]any, 0, len(items))
		for _, s := range items {
			out = append(out, map[string]any{
				"name": s.Name,
				"root": s.Root,
				"type": s.Type,
			})
		}
		utils.Ok(c, out)
	})

	// 数据源：可选数据库（备份数据库）
	// 未安装 MySQL/MariaDB 时不报错，返回 available=false，前端据此显示禁用提示
	g.GET("/cron/databases", func(c *gin.Context) {
		ok, _ := service.MysqlAvailable()
		if !ok {
			utils.Ok(c, map[string]any{"available": false, "databases": []map[string]any{}})
			return
		}
		dbs, err := service.ListDatabases()
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		out := make([]map[string]any, 0, len(dbs))
		for _, d := range dbs {
			out = append(out, map[string]any{"name": d.Name})
		}
		utils.Ok(c, map[string]any{"available": true, "databases": out})
	})

	// 创建/更新任务
	g.POST("/cron/save", superAdminOnly, func(c *gin.Context) {
		var req service.CreateCronReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		if err := service.CreateCron(req); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		action := "创建计划任务"
		if req.ID > 0 {
			action = "更新计划任务"
		}
		recordOpForCtx(c, "cron.save", action+": "+req.Name, "success")
		utils.Ok(c, nil)
	})

	// 删除任务
	g.POST("/cron/delete", superAdminOnly, func(c *gin.Context) {
		var req struct {
			ID uint `json:"id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.DeleteCron(req.ID); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "cron.delete", "删除计划任务 #"+strconv.FormatUint(uint64(req.ID), 10), "success")
		utils.Ok(c, nil)
	})

	// 启停任务
	g.POST("/cron/toggle", superAdminOnly, func(c *gin.Context) {
		var req struct {
			ID     uint `json:"id" binding:"required"`
			Enable bool `json:"enable"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.ToggleCron(req.ID, req.Enable); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, nil)
	})

	// 立即执行
	g.POST("/cron/run", superAdminOnly, func(c *gin.Context) {
		var req struct {
			ID uint `json:"id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.RunCronNow(req.ID); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "cron.run", "立即执行计划任务 #"+strconv.FormatUint(uint64(req.ID), 10), "success")
		utils.Ok(c, nil)
	})

	// 查看任务执行日志
	g.GET("/cron/log", func(c *gin.Context) {
		id, _ := strconv.Atoi(c.Query("id"))
		if id <= 0 {
			utils.Fail(c, 400, "参数错误")
			return
		}
		log, err := service.CronLog(uint(id))
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, gin.H{"log": log})
	})

	// 备份任务：列出该任务生成的备份文件（按修改时间倒序）
	g.GET("/cron/backup-files", func(c *gin.Context) {
		id, _ := strconv.Atoi(c.Query("id"))
		if id <= 0 {
			utils.Fail(c, 400, "参数错误")
			return
		}
		files, err := service.ListCronBackupFiles(uint(id))
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		dir, _ := service.CronBackupDir(uint(id))
		utils.Ok(c, gin.H{"files": files, "dir": dir})
	})

	// 备份任务：删除某个备份文件
	g.POST("/cron/backup-delete", func(c *gin.Context) {
		var req struct {
			ID   uint   `json:"id" binding:"required"`
			Name string `json:"name" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.DeleteCronBackupFile(req.ID, req.Name); err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "cron.backup_delete", "删除计划任务备份 任务#"+strconv.FormatUint(uint64(req.ID), 10)+" 文件："+req.Name, "success")
		utils.Ok(c, nil)
	})

	// 备份任务：申请一次性下载 token（需登录态；实际下载在 setupDownloadRoutes 的免 JWT 组）
	g.POST("/cron/backup-download-token", func(c *gin.Context) {
		var req struct {
			ID   uint   `json:"id" binding:"required"`
			Name string `json:"name" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		token := service.NewDownloadToken("cron_backup", fmt.Sprintf("%d:%s", req.ID, req.Name))
		utils.Ok(c, gin.H{"token": token})
	})

	// 由 wrapper 脚本在用户命令执行完毕后回调：用于系统 cron 调度执行的执行计数 + 结果同步。
	// 内部端点，仅允许来自本机（wrapper 脚本通过 127.0.0.1 调用），无需登录 token，
	// 但必须带 X-Kypanel-Internal: 1 头作为隐式鉴权。
	g.POST("/cron/run-complete", func(c *gin.Context) {
		if c.GetHeader("X-Kypanel-Internal") != "1" {
			utils.Fail(c, 403, "forbidden")
			return
		}
		var req struct {
			ID       uint `json:"id" binding:"required"`
			ExitCode int  `json:"exit_code"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		now := time.Now()
		result := "执行成功"
		if req.ExitCode != 0 {
			result = fmt.Sprintf("退出码 %d", req.ExitCode)
		}
		if err := model.DB.Model(&model.Cron{}).Where("id = ?", req.ID).
			Updates(map[string]any{
				"last_run":    &now,
				"last_result": result,
				"run_count":   gorm.Expr("COALESCE(run_count, 0) + 1"),
			}).Error; err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, nil)
	})

	// 由 wrapper 脚本在用户命令成功执行后回调：把该 cron 任务的最新备份上传到其配置的远程存储。
	// 内部端点，仅允许本机 + X-Kypanel-Internal 头。任务未配置远程目标时直接返回成功（无操作）。
	g.POST("/cron/backup-upload", func(c *gin.Context) {
		if c.GetHeader("X-Kypanel-Internal") != "1" {
			utils.Fail(c, 403, "forbidden")
			return
		}
		var req struct {
			ID uint `json:"id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.UploadCronBackupToRemote(req.ID); err != nil {
			// 上传失败记录日志但不中断 cron 主流程（wrapper 已用 || true 忽略错误）
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, nil)
	})
}
