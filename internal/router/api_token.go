package router

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// setupApiTokenRoutes 注册 API 访问令牌的 CRUD 路由：
//   - GET    /api/api-tokens              令牌列表（脱敏）
//   - POST   /api/api-tokens              创建令牌（返回明文，仅一次可见）
//   - DELETE /api/api-tokens/:id/delete   删除令牌
//
// 前端 MCP 标签页从 /api/api-tokens?type=mcp 拉取 MCP 类型令牌的连接信息（地址 + 最近一个 token 的尾位）。
func setupApiTokenRoutes(g *gin.RouterGroup) {
	// 列表（可按 type 过滤；type=api 时包含 api 和 all；type=mcp 时包含 mcp 和 all）
	g.GET("/api-tokens", func(c *gin.Context) {
		if !requireSuperAdmin(c) {
			return
		}
		list := service.ListApiTokens()
		t := c.Query("type")
		switch t {
		case service.ApiTokenTypeAPI:
			out := make([]service.ApiTokenView, 0, len(list))
			for _, x := range list {
				if x.Type == service.ApiTokenTypeAPI || x.Type == service.ApiTokenTypeAll {
					out = append(out, x)
				}
			}
			utils.Ok(c, out)
			return
		case service.ApiTokenTypeMCP:
			out := make([]service.ApiTokenView, 0, len(list))
			for _, x := range list {
				if x.Type == service.ApiTokenTypeMCP || x.Type == service.ApiTokenTypeAll {
					out = append(out, x)
				}
			}
			utils.Ok(c, out)
			return
		}
		utils.Ok(c, list)
	})

	// 创建（返回明文）
	g.POST("/api-tokens", func(c *gin.Context) {
		if !requireSuperAdmin(c) {
			return
		}
		var req service.CreateApiTokenReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		plain, view, err := service.CreateApiToken(req)
		if err != nil {
			utils.Fail(c, 400, err.Error())
			return
		}
		recordOpForCtx(c, "api_token.create", "创建 "+req.Type+" 令牌: "+req.Name, "success")
		utils.Ok(c, gin.H{
			"token": plain, // 明文，仅返回一次
			"info":  view,
		})
	})

	// 删除
	g.POST("/api-tokens/:id/delete", func(c *gin.Context) {
		if !requireSuperAdmin(c) {
			return
		}
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil || id <= 0 {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.DeleteApiToken(uint(id)); err != nil {
			utils.Fail(c, 500, "删除失败: "+err.Error())
			return
		}
		recordOpForCtx(c, "api_token.delete", "删除 API 令牌 #"+strconv.Itoa(id), "success")
		utils.Ok(c, nil)
	})
}