package router

import (
	"github.com/gin-gonic/gin"

	"kypanel/internal/service"
)

// setupTerminalRoutes 注册 Web 终端 WebSocket 路由
func setupTerminalRoutes(rg *gin.RouterGroup) {
	rg.GET("/terminal", service.HandleTerminalWS)
}
