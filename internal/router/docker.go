package router

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// setupDockerRoutes Docker 容器与容器应用商店路由
func setupDockerRoutes(g *gin.RouterGroup) {
	// Docker 状态
	g.GET("/docker/status", func(c *gin.Context) {
		utils.Ok(c, service.GetDockerInfo())
	})

	// 容器列表
	g.GET("/docker/containers", func(c *gin.Context) {
		all := c.DefaultQuery("all", "1") == "1"
		var (
			list []service.ContainerInfo
			err  error
		)
		if all {
			list, err = service.ListContainers()
		} else {
			list, err = service.ListRunningContainers()
		}
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, list)
	})

	// 创建容器
	g.POST("/docker/containers/create", func(c *gin.Context) {
		var req service.CreateContainerReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		if err := service.CreateContainer(req); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "docker.create", "创建容器: "+req.Name, "success")
		utils.Ok(c, nil)
	})

	// 容器启停
	g.POST("/docker/containers/:id/action", func(c *gin.Context) {
		var req struct {
			Action string `json:"action" binding:"required,oneof=start stop restart"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.ContainerAction(c.Param("id"), req.Action); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "docker.action", req.Action+" 容器: "+c.Param("id"), "success")
		utils.Ok(c, nil)
	})

	// 删除容器
	g.DELETE("/docker/containers/:id", func(c *gin.Context) {
		force := c.DefaultQuery("force", "0") == "1"
		if err := service.RemoveContainer(c.Param("id"), force); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "docker.remove", "删除容器: "+c.Param("id"), "success")
		utils.Ok(c, nil)
	})

	// 容器日志
	g.GET("/docker/containers/:id/logs", func(c *gin.Context) {
		lines, _ := strconv.Atoi(c.DefaultQuery("lines", "200"))
		logText, err := service.ContainerLogs(c.Param("id"), lines)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, gin.H{"log": logText})
	})

	// 镜像列表
	g.GET("/docker/images", func(c *gin.Context) {
		list, err := service.ListImages()
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, list)
	})

	// ====== Docker 网络管理 ======
	// 网络列表
	g.GET("/docker/networks", func(c *gin.Context) {
		list, err := service.ListNetworks()
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, list)
	})

	// 创建网络
	g.POST("/docker/networks/create", func(c *gin.Context) {
		var req service.CreateNetworkReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		if err := service.CreateNetwork(req); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "docker.network.create", "创建网络: "+req.Name, "success")
		utils.Ok(c, nil)
	})

	// 删除网络
	g.DELETE("/docker/networks/:id", func(c *gin.Context) {
		if err := service.RemoveNetwork(c.Param("id")); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "docker.network.rm", "删除网络: "+c.Param("id"), "success")
		utils.Ok(c, nil)
	})

	// ====== 容器应用商店 ======
	// 应用列表（含状态）
	g.GET("/docker/apps", func(c *gin.Context) {
		utils.Ok(c, service.ListDockerApps())
	})

	// 安装应用
	g.POST("/docker/apps/install", func(c *gin.Context) {
		var req service.InstallDockerAppReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		if err := service.InstallDockerApp(req); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "docker.app.install", "安装容器应用: "+req.Key, "success")
		utils.Ok(c, nil)
	})

	// 卸载应用
	g.POST("/docker/apps/uninstall", func(c *gin.Context) {
		var req struct {
			Key string `json:"key" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.UninstallDockerApp(req.Key); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "docker.app.uninstall", "卸载容器应用: "+req.Key, "success")
		utils.Ok(c, nil)
	})
}
