package router

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"

	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// setupDownloadRoutes 注册「下载 / 预览」类的 GET 接口。
// 这些接口在 router.go 中挂在一个不经过 JWT/API token 鉴权的 downloadGroup 上
// （<img>/<video>/<a download> 等无法携带 Bearer 头），因此各自用短期 token 自行鉴权：
//   - 一次性下载 token：用一次即失效，绑定 scope+target
//   - 预览 token：短时效、可多次、绑定文件路径
//
// 注意：对应的「申请 token」POST 接口（如 /backup/download-token）仍保留在
// authGroup（需要登录态），只有这里的 GET 下载/预览接口免 JWT。
func setupDownloadRoutes(g *gin.RouterGroup) {
	// ===== 备份中心：下载备份 =====
	g.GET("/backup/download", func(c *gin.Context) {
		id, _ := strconv.Atoi(c.Query("id"))
		if id <= 0 {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if !service.ConsumeDownloadToken(c.Query("token"), "backup", fmt.Sprintf("%d", id)) {
			utils.Fail(c, 403, "下载链接无效或已过期")
			return
		}
		path, err := service.BackupFilePath(uint(id))
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		c.File(path)
	})

	// ===== 计划任务：下载备份文件 =====
	g.GET("/cron/backup-download", func(c *gin.Context) {
		id, _ := strconv.Atoi(c.Query("id"))
		name := c.Query("name")
		if id <= 0 || name == "" {
			c.String(400, "参数错误")
			return
		}
		if !service.ConsumeDownloadToken(c.Query("token"), "cron_backup", fmt.Sprintf("%d:%s", id, name)) {
			c.String(403, "下载链接无效或已过期")
			return
		}
		if err := service.ServeCronBackupDownload(uint(id), name, c.Writer); err != nil {
			c.String(404, "%v", err)
			return
		}
	})

	// ===== 操作日志：导出 CSV =====
	g.GET("/oplog/export", func(c *gin.Context) {
		if !service.ConsumeDownloadToken(c.Query("token"), "oplog", "all") {
			utils.Fail(c, 403, "下载链接无效或已过期")
			return
		}
		data, err := service.ExportOpsCSV()
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "oplog.export", "导出操作日志", "success")
		c.Header("Content-Type", "text/csv; charset=utf-8")
		c.Header("Content-Disposition", `attachment; filename="oplog.csv"`)
		c.Data(200, "text/csv; charset=utf-8", data)
	})

	// ===== 文件：图片/视频/音频预览 =====
	g.GET("/file/raw", func(c *gin.Context) {
		clean, err := service.SanitizePath(c.Query("path"))
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		if !service.VerifyPreviewToken(c.Query("token"), "file", clean) {
			utils.Fail(c, 403, "预览链接无效或已过期")
			return
		}
		c.File(clean)
	})
}
