package router

import (
	"net/http"
	"runtime"

	"github.com/gin-gonic/gin"

	"kypanel/internal/service"
	"kypanel/internal/utils"
	"kypanel/internal/version"
)

// setupUpdateRoutes 注册面板在线更新路由（需要 settings 权限）
func setupUpdateRoutes(r *gin.RouterGroup) {
	g := r.Group("/update")
	{
		g.GET("/check", handleUpdateCheck)
		g.POST("/upgrade", handleUpdateUpgrade)
	}
}

// handleUpdateCheck 检查面板是否有新版本
func handleUpdateCheck(c *gin.Context) {
	arch := c.Query("arch")
	if arch == "" {
		arch = runtime.GOARCH
	}
	// force=1 时跳过缓存，用于前端手动点击「检查更新」
	force := c.Query("force") == "1"
	info, err := service.CheckUpdate(c.Request.Context(), arch, force)
	if err != nil {
		utils.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	utils.Ok(c, gin.H{
		"current_version": version.Version,
		"has_update":      info.HasUpdate,
		"version":         info.Version,
		"min_version":     info.MinVersion,
		"changelog":       info.Changelog,
		"download_url":    info.DownloadURL,
		"force":           info.Force,
	})
}

// handleUpdateUpgrade 执行面板升级
func handleUpdateUpgrade(c *gin.Context) {
	var req struct {
		DownloadURL string `json:"download_url" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}

	if err := service.UpgradePanel(req.DownloadURL); err != nil {
		utils.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	utils.Ok(c, gin.H{"msg": "升级已启动，面板将在几秒后自动重启"})
}
