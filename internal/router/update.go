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
		g.GET("/progress", handleUpdateProgress)
		g.POST("/upgrade", handleUpdateUpgrade)
	}
}

// handleUpdateProgress 返回升级进度，供前端弹窗轮询展示。
// 升级完成后面板会重启：重启后的新进程若发现升级完成标记，则返回 phase=done 并清除标记。
func handleUpdateProgress(c *gin.Context) {
	st := service.UpgradeProgress()
	if !st.Running && st.Phase == "idle" {
		if v, ok := service.ConsumeUpgradeDone(); ok {
			utils.Ok(c, gin.H{
				"running": false,
				"phase":   "done",
				"message": "更新完成",
				"version": v,
			})
			return
		}
	}
	utils.Ok(c, gin.H{
		"running":    st.Running,
		"phase":      st.Phase,
		"message":    st.Message,
		"file":       st.File,
		"downloaded": st.Downloaded,
		"total":      st.Total,
		"error":      st.Error,
	})
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
		"web_url":         info.WebURL,
		"xdb_url":         info.XdbURL,
		"force":           info.Force,
	})
}

// handleUpdateUpgrade 执行面板升级。
// 安全关键：客户端自报的 download_url / web_url / sha256 一律【不被信任】——
// 它们会被服务端权威信息覆盖（见 service.AuthoritativeUpgrade），
// 杜绝攻击者利用该接口让面板下载并执行任意文件。入参仅校验 JSON 合法性，不再承载下载源。
func handleUpdateUpgrade(c *gin.Context) {
	var req service.UpdateInfo
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}

	if err := service.AuthoritativeUpgrade(req, runtime.GOARCH); err != nil {
		utils.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	utils.Ok(c, gin.H{"msg": "升级已启动，面板将在几秒后自动重启"})
}
