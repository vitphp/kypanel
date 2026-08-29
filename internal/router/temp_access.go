package router

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"kypanel/internal/config"
	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// setupTempAccessRoutes 注册临时访问（临时登录链接）路由：
//   - GET    /api/temp-access            列表
//   - POST   /api/temp-access            创建（返回明文 token + 完整链接，仅一次可见）
//   - POST   /api/temp-access/:id/delete 删除
//   - POST   /api/temp-access/:id/toggle 启用/禁用
//   - GET    /api/temp-access/use-logs   使用日志（IP + 归属地）
// requireSuperAdmin 硬性校验超级管理员身份（不含角色 * 权限）。
// adminID==0 视为超管（API 令牌上下文，创建时已由管理员承担信任责任）。
// 校验失败时已写入 403 响应，调用方需立即 return。
func requireSuperAdmin(c *gin.Context) bool {
	if !service.IsSuperAdmin(c.GetUint("admin_id")) {
		utils.Fail(c, 403, "仅超级管理员可操作")
		return false
	}
	return true
}

func setupTempAccessRoutes(g *gin.RouterGroup) {
	// 列表
	g.GET("/temp-access", func(c *gin.Context) {
		if !requireSuperAdmin(c) {
			return
		}
		utils.Ok(c, service.ListTempAccess())
	})

	// 创建
	g.POST("/temp-access", func(c *gin.Context) {
		if !requireSuperAdmin(c) {
			return
		}
		var req service.CreateTempAccessReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		if req.MaxUses < 0 {
			utils.Fail(c, 400, "使用次数不能为负数")
			return
		}
		// 构建 base URL：优先用绑定域名，否则用请求 Host
		baseURL := buildPanelBaseURL(c)
		token, link, view, err := service.CreateTempAccess(req, baseURL)
		if err != nil {
			utils.Fail(c, 500, "创建失败: "+err.Error())
			return
		}
		service.RecordOp(c.GetUint("admin_id"), "temp_access.create",
			"创建临时访问: "+req.Name, c.ClientIP(), "success")
		utils.Ok(c, gin.H{
			"token": token, // 明文，仅返回一次
			"link":  link,  // 完整链接
			"info":  view,
		})
	})

	// 删除
	g.POST("/temp-access/:id/delete", func(c *gin.Context) {
		if !requireSuperAdmin(c) {
			return
		}
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil || id <= 0 {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.DeleteTempAccess(uint(id)); err != nil {
			utils.Fail(c, 500, "删除失败: "+err.Error())
			return
		}
		service.RecordOp(c.GetUint("admin_id"), "temp_access.delete",
			"删除临时访问 #"+strconv.Itoa(id), c.ClientIP(), "success")
		utils.Ok(c, nil)
	})

	// 启用/禁用
	g.POST("/temp-access/:id/toggle", func(c *gin.Context) {
		if !requireSuperAdmin(c) {
			return
		}
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil || id <= 0 {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.ToggleTempAccess(uint(id)); err != nil {
			utils.Fail(c, 500, "操作失败: "+err.Error())
			return
		}
		utils.Ok(c, nil)
	})

	// 使用日志（可选 temp_access_id 过滤）
	g.GET("/temp-access/use-logs", func(c *gin.Context) {
		if !requireSuperAdmin(c) {
			return
		}
		id, _ := strconv.Atoi(c.Query("temp_access_id"))
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
		utils.Ok(c, service.ListTempAccessUseLogs(uint(id), limit))
	})

	// 临时访问关联的操作日志（按 temp_access_id 过滤 Source=temp 的 OperationLog）
	g.GET("/temp-access/operations", func(c *gin.Context) {
		if !requireSuperAdmin(c) {
			return
		}
		id, err := strconv.Atoi(c.Query("temp_access_id"))
		if err != nil || id <= 0 {
			utils.Fail(c, 400, "参数错误：缺少 temp_access_id")
			return
		}
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
		if page < 1 {
			page = 1
		}
		if pageSize < 1 || pageSize > 200 {
			pageSize = 50
		}
		logs, total, err := service.ListOpsByTempAccess(uint(id), page, pageSize)
		if err != nil {
			utils.Fail(c, 500, "查询失败: "+err.Error())
			return
		}
		utils.Ok(c, gin.H{"list": logs, "total": total})
	})

	// 重新获取完整链接（复制链接按钮用）：按 id 解密 token 拼接
	g.GET("/temp-access/link", func(c *gin.Context) {
		if !requireSuperAdmin(c) {
			return
		}
		id, err := strconv.Atoi(c.Query("id"))
		if err != nil || id <= 0 {
			utils.Fail(c, 400, "参数错误")
			return
		}
		link, err := service.GetTempAccessLink(uint(id), buildPanelBaseURL(c))
		if err != nil {
			utils.Fail(c, 404, err.Error())
			return
		}
		utils.Ok(c, gin.H{"link": link})
	})
}

// buildPanelBaseURL 构建面板访问地址（协议 + host）。
// 优先使用配置的绑定域名，否则回退到请求的 Host。
func buildPanelBaseURL(c *gin.Context) string {
	cfg := config.Get()
	scheme := "http"
	if bool(cfg.Server.HTTPS) {
		scheme = "https"
	}
	host := c.Request.Host
	if cfg.Server.Domain != "" {
		host = cfg.Server.Domain
	}
	return scheme + "://" + host
}
