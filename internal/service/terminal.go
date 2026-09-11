package service

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"kypanel/internal/utils"
)

// HandleTerminalWS Web 终端 WebSocket 处理器：
// 浏览器输入 -> PTY -> /bin/bash；PTY 输出 -> WebSocket -> 浏览器。
// 支持 {"type":"resize","cols":N,"rows":N} 文本指令动态调整终端尺寸。
func HandleTerminalWS(c *gin.Context) {
	// 鉴权：优先 Authorization header，其次 query token（WebSocket 用）
	token := c.Query("token")
	if token == "" {
		if h := c.GetHeader("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimPrefix(h, "Bearer ")
		}
	}
	if token == "" {
		utils.FailWithStatus(c, http.StatusUnauthorized, 401, "未登录或 Token 已过期")
		return
	}
	claims, err := utils.ParseToken(token)
	if err != nil {
		utils.FailWithStatus(c, http.StatusUnauthorized, 401, "Token 无效或已过期")
		return
	}
	// Web 终端等同于 root shell，仅超级管理员可用
	if !IsSuperAdmin(claims.AdminID) {
		utils.FailWithStatus(c, http.StatusForbidden, 403, "仅超级管理员可使用终端")
		return
	}

	// Origin 同源校验，防跨站 WebSocket 劫持（CSWSH）
	if !wsSameOrigin(c.Request) {
		utils.FailWithStatus(c, http.StatusForbidden, 403, "Origin 校验失败")
		return
	}

	// 检查是否为 WebSocket 升级请求
	if !strings.EqualFold(c.GetHeader("Upgrade"), "websocket") {
		utils.FailWithStatus(c, http.StatusBadRequest, 400, "仅支持 WebSocket")
		return
	}

	// 劫持 HTTP 连接，自行完成 WebSocket 握手
	hj, ok := c.Writer.(http.Hijacker)
	if !ok {
		utils.FailWithStatus(c, http.StatusInternalServerError, 500, "连接不支持升级")
		return
	}
	conn, brw, err := hj.Hijack()
	if err != nil {
		slog.Error("劫持连接失败", "err", err)
		return
	}

	ws, err := wsUpgrade(conn, brw.Reader, c.Request)
	if err != nil {
		slog.Error("WebSocket 握手失败", "err", err)
		_ = conn.Close()
		return
	}
	defer ws.Close()

	// 启动 PTY 运行默认 shell；支持 cwd 参数进入指定目录
	term, err := startPty(120, 30, defaultShell(), c.Query("cwd"))
	if err != nil {
		_ = ws.WriteMessage(1, []byte("\r\n\x1b[31m[kypanel] 无法启动终端: "+err.Error()+"\x1b[0m\r\n"))
		return
	}
	defer term.Close()

	// PTY 输出 -> WebSocket（二进制消息）
	go func() {
		buf := make([]byte, 8192)
		for {
			n, rerr := term.Read(buf)
			if n > 0 {
				if werr := ws.WriteMessage(2, buf[:n]); werr != nil {
					return
				}
			}
			if rerr != nil {
				if rerr != io.EOF {
					slog.Debug("PTY 读取结束", "err", rerr)
				}
				return
			}
		}
	}()

	// WebSocket -> PTY（文本消息按 resize 指令解析，其余直接写入）
	for {
		mt, data, rerr := ws.ReadMessage()
		if rerr != nil {
			break
		}
		if mt == 1 { // 文本消息
			var op struct {
				Type string `json:"type"`
				Cols int    `json:"cols"`
				Rows int    `json:"rows"`
			}
			if json.Unmarshal(data, &op) == nil && op.Type == "resize" && op.Cols > 0 && op.Rows > 0 {
				if serr := term.Resize(op.Cols, op.Rows); serr != nil {
					slog.Debug("调整终端尺寸失败", "err", serr)
				}
				continue
			}
		}
		if _, werr := term.Write(data); werr != nil {
			break
		}
	}

	// 读循环退出后关闭 PTY，让输出协程随之退出
	_ = term.Close()
}
