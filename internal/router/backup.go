package router

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"

	"kypanel/internal/model"
	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// setupBackupRoutes 备份中心路由
func setupBackupRoutes(g *gin.RouterGroup) {
	// 备份任务列表（?type=site/panel）
	g.GET("/backup/list", func(c *gin.Context) {
		utils.Ok(c, service.ListBackupTasks(c.Query("type")))
	})

	// 创建备份
	g.POST("/backup/create", func(c *gin.Context) {
		var req struct {
			Type       string `json:"type" binding:"required"`
			Target     string `json:"target"`
			TargetType string `json:"target_type"`   // local / remote
			TargetName string `json:"target_name"`   // 远程存储名称
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		task, err := service.CreateBackup(req.Type, req.Target)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		// 若指定了远程存储且备份成功，尝试上传
		if req.TargetType == "remote" && req.TargetName != "" && task != nil && task.Status == "success" {
			if upErr := service.UploadBackupTaskToRemote(task.ID, req.TargetName); upErr != nil {
				// 上传失败不算备份失败（本地备份文件已生成），仅记录到 task.Error
				if task.Error == "" {
					task.Error = "上传到远程失败: " + upErr.Error()
				} else {
					task.Error += "; 上传到远程失败: " + upErr.Error()
				}
				model.DB.Save(task)
			}
		}
		recordOpForCtx(c, "backup.create", "创建备份: "+req.Type+"/"+req.Target, "success")
		utils.Ok(c, task)
	})

	// 删除备份
	g.POST("/backup/delete", func(c *gin.Context) {
		var req struct {
			ID uint `json:"id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.DeleteBackup(req.ID); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "backup.delete", "删除备份 #"+strconv.FormatUint(uint64(req.ID), 10), "success")
		utils.Ok(c, nil)
	})

	// 恢复备份
	g.POST("/backup/restore", func(c *gin.Context) {
		var req struct {
			ID uint `json:"id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.RestoreBackup(req.ID); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "backup.restore", "恢复备份 #"+strconv.FormatUint(uint64(req.ID), 10), "success")
		utils.Ok(c, nil)
	})

	// 申请一次性下载 token（需登录态；实际下载在 setupDownloadRoutes 的免 JWT 组）
	g.POST("/backup/download-token", func(c *gin.Context) {
		var req struct {
			ID uint `json:"id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		token := service.NewDownloadToken("backup", fmt.Sprintf("%d", req.ID))
		utils.Ok(c, gin.H{"token": token})
	})

	// ---- 远程存储配置 ----
	g.GET("/backup/storages", func(c *gin.Context) {
		utils.Ok(c, service.GetBackupStorages())
	})
	g.POST("/backup/storages", func(c *gin.Context) {
		// 支持三种模式：
		//   1) { storage: {...}, old_name: "" } → 单条追加（弹窗"添加"）
		//   2) { storage: {...}, old_name: "x" } → 按 old_name 替换（弹窗"编辑"）
		//   3) { list: [...] } → 全量替换（兼容旧"保存配置"按钮）
		var req struct {
			List    []model.BackupStorage `json:"list"`
			Storage *model.BackupStorage  `json:"storage"`
			OldName string                `json:"old_name"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		switch {
		case req.Storage != nil:
			if req.OldName != "" {
				if err := service.UpdateBackupStorage(req.OldName, *req.Storage); err != nil {
					utils.Fail(c, 500, err.Error())
					return
				}
			} else {
				if err := service.AppendBackupStorage(*req.Storage); err != nil {
					utils.Fail(c, 500, err.Error())
					return
				}
			}
		case req.List != nil:
			if err := service.SaveBackupStorages(req.List); err != nil {
				utils.Fail(c, 500, err.Error())
				return
			}
		default:
			utils.Fail(c, 400, "缺少 list 或 storage 字段")
			return
		}
		utils.Ok(c, nil)
	})

	// 删除单条远程存储
	g.POST("/backup/storage-delete", func(c *gin.Context) {
		var req struct {
			Name string `json:"name" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.DeleteBackupStorage(req.Name); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, nil)
	})

	// 上传备份到远程存储
	g.POST("/backup/upload", func(c *gin.Context) {
		var req struct {
			ID      uint   `json:"id" binding:"required"`
			Storage string `json:"storage" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.UploadBackupToStorage(req.ID, req.Storage); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "backup.upload", "上传备份 #"+strconv.FormatUint(uint64(req.ID), 10)+" 到 "+req.Storage, "success")
		utils.Ok(c, nil)
	})
}
