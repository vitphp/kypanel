package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func setup() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	srv := NewServer()
	r.Any("/api/mcp", srv.ServeHTTP)
	return r
}

func doJSON(r *gin.Engine, t *testing.T, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v, body=%s", err, w.Body.String())
	}
	return w, resp
}

// TestInitialize MCP initialize 握手：协商协议版本并声明能力
func TestInitialize(t *testing.T) {
	r := setup()
	w, resp := doJSON(r, t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"claude-code","version":"x"}}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", w.Code)
	}
	if resp["error"] != nil {
		t.Fatalf("initialize 返回错误: %v", resp["error"])
	}
	result := resp["result"].(map[string]any)
	if result["protocolVersion"] != "2025-06-18" {
		t.Errorf("protocolVersion = %v", result["protocolVersion"])
	}
	if result["serverInfo"] == nil {
		t.Errorf("缺少 serverInfo")
	}
}

// TestToolsList 工具列表：返回全部工具且含 inputSchema
func TestToolsList(t *testing.T) {
	r := setup()
	_, resp := doJSON(r, t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	result := resp["result"].(map[string]any)
	tools := result["tools"].([]any)
	if len(tools) < 8 {
		t.Fatalf("工具数量 = %d, 期望至少 8 个", len(tools))
	}
	names := map[string]bool{}
	for _, item := range tools {
		tool := item.(map[string]any)
		names[tool["name"].(string)] = true
		if tool["description"] == "" || tool["inputSchema"] == nil {
			t.Errorf("工具 %v 缺少 description 或 inputSchema", tool["name"])
		}
	}
	for _, want := range []string{"system_info", "process_list", "website_list", "website_create", "exec_command"} {
		if !names[want] {
			t.Errorf("缺少工具 %s", want)
		}
	}
}

// TestToolsCallUnknown 调用不存在的工具返回 isError=true
func TestToolsCallUnknown(t *testing.T) {
	r := setup()
	_, resp := doJSON(r, t, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"not_exist","arguments":{}}}`)
	result := resp["result"].(map[string]any)
	if result["isError"] != true {
		t.Errorf("未知工具应返回 isError=true, got %v", result)
	}
}

// TestUnknownMethod JSON-RPC 未知方法返回错误码
func TestUnknownMethod(t *testing.T) {
	r := setup()
	_, resp := doJSON(r, t, `{"jsonrpc":"2.0","id":4,"method":"resources/list"}`)
	if resp["error"] == nil {
		t.Fatalf("未知方法应返回 error")
	}
	err := resp["error"].(map[string]any)
	if int(err["code"].(float64)) != -32601 {
		t.Errorf("错误码 = %v, 期望 -32601", err["code"])
	}
}

// TestBadJSON 非法 JSON 返回 parse error
func TestBadJSON(t *testing.T) {
	r := setup()
	req := httptest.NewRequest(http.MethodPost, "/api/mcp", strings.NewReader(`{bad`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] == nil {
		t.Fatalf("非法 JSON 应返回 error")
	}
}

// TestMethodNotAllowed GET 不支持
func TestMethodNotAllowed(t *testing.T) {
	r := setup()
	req := httptest.NewRequest(http.MethodGet, "/api/mcp", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET 应返回 405, got %d", w.Code)
	}
}
