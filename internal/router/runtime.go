package router

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// setupRuntimeRoutes 运行环境管理（PHP / Python / Node / Go）路由
func setupRuntimeRoutes(g *gin.RouterGroup) {
	// 环境基础信息（版本/路径/运行状态）
	g.GET("/env/info", func(c *gin.Context) {
		name := c.Query("name")
		if name == "" {
			utils.Fail(c, 400, "缺少 name 参数")
			return
		}
		info, err := service.RuntimeInfo(name)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, info)
	})

	// 服务启停（start / stop / restart）
	g.POST("/env/service", func(c *gin.Context) {
		var req struct {
			Name   string `json:"name" binding:"required"`
			Action string `json:"action" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		res, err := service.RuntimeService(req.Name, req.Action)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, res)
	})

	// PHP 扩展列表
	g.GET("/env/php/extensions", func(c *gin.Context) {
		list, err := service.PhpExtList(c.Query("version"))
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, list)
	})

	// PHP 扩展安装
	g.POST("/env/php/extensions/install", func(c *gin.Context) {
		var req struct {
			Version string `json:"version"`
			Ext     string `json:"ext" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.PhpExtInstall(req.Version, req.Ext); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, gin.H{"installed": req.Ext})
	})

	// PHP 扩展卸载
	g.POST("/env/php/extensions/uninstall", func(c *gin.Context) {
		var req struct {
			Version string `json:"version"`
			Ext     string `json:"ext" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.PhpExtUninstall(req.Version, req.Ext); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, gin.H{"uninstalled": req.Ext})
	})

	// PHP ini 读取
	g.GET("/env/php/ini", func(c *gin.Context) {
		data, err := service.PhpIniGet(c.Query("version"))
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, data)
	})

	// PHP ini 保存
	g.POST("/env/php/ini", func(c *gin.Context) {
		var req struct {
			Version string            `json:"version"`
			Updates map[string]string `json:"updates"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if len(req.Updates) == 0 {
			utils.Fail(c, 400, "缺少更新项")
			return
		}
		res, err := service.PhpIniSet(req.Version, req.Updates)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, res)
	})

	// PHP-FPM 配置读取
	g.GET("/env/php/fpm-config", func(c *gin.Context) {
		data, err := service.PhpFpmConfigGet(c.Query("version"))
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, data)
	})

	// PHP-FPM 配置保存
	g.POST("/env/php/fpm-config", func(c *gin.Context) {
		var req struct {
			Version string            `json:"version"`
			Updates map[string]string `json:"updates"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		res, err := service.PhpFpmConfigSet(req.Version, req.Updates)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, res)
	})

	// PHP 负载状态
	g.GET("/env/php/status", func(c *gin.Context) {
		data, err := service.PhpStatus(c.Query("version"))
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, data)
	})

	// PHP 日志（type: fpm / slow）
	g.GET("/env/php/log", func(c *gin.Context) {
		lines, _ := strconv.Atoi(c.Query("lines"))
		data, err := service.PhpLog(c.Query("version"), c.Query("type"), lines)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, data)
	})

	// PHP phpinfo 摘要
	g.GET("/env/php/phpinfo", func(c *gin.Context) {
		data, err := service.PhpInfoPage(c.Query("version"))
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, data)
	})

	// Python/Node/Go 已装包列表
	g.GET("/env/packages", func(c *gin.Context) {
		data, err := service.RuntimePkgs(c.Query("name"))
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, data)
	})

	// Python/Node/Go 全局配置读取
	g.GET("/env/global-config", func(c *gin.Context) {
		data, err := service.RuntimeGlobalConfig(c.Query("name"))
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, data)
	})

	// Python/Node/Go 全局配置保存
	g.POST("/env/global-config", func(c *gin.Context) {
		var req struct {
			Name    string            `json:"name" binding:"required"`
			Updates map[string]string `json:"updates"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if len(req.Updates) == 0 {
			utils.Fail(c, 400, "缺少更新项")
			return
		}
		_ = strings.TrimSpace(req.Name)
		res, err := service.RuntimeGlobalConfigSet(req.Name, req.Updates)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, res)
	})
}
