package router

import (
	"github.com/gin-gonic/gin"

	"kypanel/internal/mcp"
)

// setupMcpRoutes 注册 MCP (Model Context Protocol) 路由：
//   - POST /api/mcp           MCP Streamable HTTP 端点（Claude Code / Codex / Cursor 连接入口）
//
// 鉴权由本分组自带中间件 ApiTokenOrAuth("mcp") 完成，AI 工具通过 Authorization: Bearer <token> 访问。
// 仅接受 JWT（前端登录态）或 type=mcp 的 API 令牌，不接受 type=api 的令牌（防止误用）。
func setupMcpRoutes(g *gin.RouterGroup) {
	mcpSrv := mcp.NewServer()
	g.Any("/mcp", mcpSrv.ServeHTTP)
}