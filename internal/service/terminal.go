package service

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"kypanel/internal/utils"
)

// wsUpgrader WebSocket 升级器。
// 浏览器 WebSocket 无法自定义 Header，鉴权通过 query token 完成。
// CheckOrigin 校验 Origin 与请求 Host 同源，防止跨站 WebSocket 劫持（CSWSH）：
// 恶意站点在浏览器里携带已泄露的 token 发起 WebSocket 连接时，其 Origin 会是
// 恶意站点的源（与面板 Host 不同源），此处直接拒绝。
var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		// 无 Origin 头：非浏览器客户端（curl/wscat/CLI 等）直接放行，
		// 它们无法被跨站诱导，安全风险低。
		if origin == "" {
			return true
		}
		// 解析 Origin 的 host[:port]，与请求 Host 比对，必须同源。
		originHost := origin
		if idx := strings.Index(origin, "://"); idx >= 0 {
			originHost = origin[idx+3:]
		}
		// 去掉路径部分
		if idx := strings.Index(originHost, "/"); idx >= 0 {
			originHost = originHost[:idx]
		}
		return strings.EqualFold(originHost, r.Host)
	},
}

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

	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		slog.Error("升级 WebSocket 失败", "err", err)
		return
	}
	defer conn.Close()

	// 启动 PTY 运行默认 shell；支持 cwd 参数进入指定目录
	term, err := startPty(120, 30, defaultShell(), c.Query("cwd"))
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[31m[kypanel] 无法启动终端: "+err.Error()+"\x1b[0m\r\n"))
		return
	}
	defer term.Close()

	// PTY 输出 -> WebSocket（二进制消息）
	go func() {
		buf := make([]byte, 8192)
		for {
			n, rerr := term.Read(buf)
			if n > 0 {
				if werr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
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
		mt, data, rerr := conn.ReadMessage()
		if rerr != nil {
			break
		}
		if mt == websocket.TextMessage {
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
