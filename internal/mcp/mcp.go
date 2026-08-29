// Package mcp 实现 Model Context Protocol (MCP) Streamable HTTP Server，
// 供 Claude Code / Codex / Cursor 等支持 MCP 的 AI 工具远程连接面板，
// 通过 tools 调用面板能力完成服务器管理、网站部署、故障排查等运维操作。
// 鉴权由路由层 middleware.Auth() 统一完成（Bearer JWT），权限与操作日志由面板统一管理。
package mcp

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	protocolVersion2025 = "2025-06-18"
	protocolVersion2024 = "2024-11-05"
	serverName          = "kypanel"
	serverVersion       = "0.1.0"
)

// ToolContext 工具执行上下文（由路由层鉴权后注入）
type ToolContext struct {
	AdminID  uint
	Username string
	ClientIP string
}

// Tool 定义一个 MCP 工具
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	handler     func(ctx *ToolContext, args map[string]any) (any, error)
}

// Server MCP Streamable HTTP Server
type Server struct {
	tools map[string]*Tool
}

// NewServer 创建 MCP Server，注册全部工具
func NewServer() *Server {
	s := &Server{tools: make(map[string]*Tool)}
	for _, t := range allTools() {
		s.tools[t.Name] = t
	}
	return s
}

// ServeHTTP 处理 MCP Streamable HTTP 请求（JSON-RPC 2.0 over HTTP POST）
// 所有调用同步完成，直接以 application/json 响应（MCP 规范允许，客户端无需 SSE 流）。
func (s *Server) ServeHTTP(c *gin.Context) {
	// 仅支持 POST（Streamable HTTP 的 JSON-RPC 通道）
	if c.Request.Method != http.MethodPost {
		c.Status(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil || len(body) == 0 {
		s.writeError(c, nil, -32700, "parse error: empty request body")
		return
	}

	// 解析 JSON-RPC 消息（Streamable HTTP 单条消息，不支持 batch 数组）
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		s.writeError(c, nil, -32700, "parse error: invalid JSON")
		return
	}

	var method string
	_ = json.Unmarshal(raw["method"], &method)
	var id json.RawMessage = raw["id"] // 无 id 时为 notification

	if method == "" {
		s.writeError(c, id, -32600, "invalid request: missing method")
		return
	}

	tctx := &ToolContext{
		AdminID:  c.GetUint("admin_id"),
		Username: c.GetString("username"),
		ClientIP: c.ClientIP(),
	}

	switch method {
	case "initialize":
		s.writeResult(c, id, s.handleInitialize(raw))
	case "ping":
		s.writeResult(c, id, map[string]any{})
	case "tools/list":
		s.writeResult(c, id, s.handleToolsList())
	case "tools/call":
		s.writeResult(c, id, s.handleToolsCall(tctx, raw))
	case "notifications/initialized", "notifications/cancelled", "notifications/tools/list_changed":
		// 通知类消息无需响应
		c.Status(http.StatusAccepted)
	default:
		s.writeError(c, id, -32601, "Method not found: "+method)
	}
}

// handleInitialize 处理 initialize：协商协议版本，声明能力
func (s *Server) handleInitialize(raw map[string]json.RawMessage) map[string]any {
	proto := protocolVersion2025
	if p, ok := raw["params"]; ok {
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if json.Unmarshal(p, &params) == nil && params.ProtocolVersion != "" {
			proto = params.ProtocolVersion
		}
	}
	return map[string]any{
		"protocolVersion": proto,
		"capabilities": map[string]any{
			"tools": map[string]any{"listChanged": false},
		},
		"serverInfo": map[string]any{"name": serverName, "version": serverVersion},
	}
}

// handleToolsList 返回工具列表
func (s *Server) handleToolsList() map[string]any {
	list := make([]map[string]any, 0, len(s.tools))
	for _, t := range s.tools {
		list = append(list, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.InputSchema,
		})
	}
	return map[string]any{"tools": list}
}

// handleToolsCall 调用指定工具
func (s *Server) handleToolsCall(tctx *ToolContext, raw map[string]json.RawMessage) map[string]any {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if p, ok := raw["params"]; ok {
		_ = json.Unmarshal(p, &params)
	}
	if params.Name == "" {
		return toolError("缺少工具名称")
	}
	tool, ok := s.tools[params.Name]
	if !ok {
		return toolError("未知工具: " + params.Name)
	}
	if params.Arguments == nil {
		params.Arguments = map[string]any{}
	}
	return toolResult(tool.handler(tctx, params.Arguments))
}

// toolResult 将工具执行结果包装为 MCP tools/call result
func toolResult(data any, err error) map[string]any {
	if err != nil {
		return toolError(err.Error())
	}
	text, jerr := json.MarshalIndent(data, "", "  ")
	if jerr != nil {
		return toolError("序列化结果失败: " + jerr.Error())
	}
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(text)}},
	}
}

// toolError 工具执行失败（isError=true，工具错误不是 JSON-RPC error）
func toolError(msg string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": msg}},
		"isError": true,
	}
}

// writeResult 输出 JSON-RPC 成功响应
func (s *Server) writeResult(c *gin.Context, id json.RawMessage, result any) {
	c.JSON(http.StatusOK, gin.H{"jsonrpc": "2.0", "id": id, "result": result})
}

// writeError 输出 JSON-RPC 错误响应
func (s *Server) writeError(c *gin.Context, id json.RawMessage, code int, msg string) {
	c.JSON(http.StatusOK, gin.H{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   gin.H{"code": code, "message": msg},
	})
}
