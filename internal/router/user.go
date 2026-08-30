package router

import (
	"github.com/gin-gonic/gin"

	"kypanel/internal/model"
	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// setupUserRoutes 用户与角色管理路由（仅超级管理员可访问）
func setupUserRoutes(g *gin.RouterGroup) {
	// 仅超管可访问的权限组
	admin := g.Group("")
	admin.Use(func(c *gin.Context) {
		// 超级管理员 role_id=0
		if service.AdminHasPermission(c.GetUint("admin_id"), "settings") {
			c.Next()
			return
		}
		utils.FailWithStatus(c, 403, 403, "仅超级管理员可访问用户管理")
		c.Abort()
	})

	// ---- 角色 ----
	admin.GET("/roles", func(c *gin.Context) {
		utils.Ok(c, service.ListRoles())
	})
	admin.POST("/roles", func(c *gin.Context) {
		var role model.Role
		if err := c.ShouldBindJSON(&role); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.SaveRole(&role); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "user.role_save", "保存角色: "+role.Name, "success")
		utils.Ok(c, role)
	})
	admin.POST("/roles/delete", func(c *gin.Context) {
		var req struct {
			ID uint `json:"id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.DeleteRole(req.ID); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, nil)
	})

	// ---- 用户 ----
	admin.GET("/users", func(c *gin.Context) {
		utils.Ok(c, service.ListAdmins(c.GetUint("admin_id")))
	})
	admin.POST("/users", func(c *gin.Context) {
		var req struct {
			Username string `json:"username" binding:"required"`
			Password string `json:"password" binding:"required"`
			RoleID   uint   `json:"role_id"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.CreateAdmin(req.Username, req.Password, req.RoleID); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "user.create", "创建子账号: "+req.Username, "success")
		utils.Ok(c, nil)
	})
	admin.POST("/users/role", func(c *gin.Context) {
		var req struct {
			ID     uint `json:"id" binding:"required"`
			RoleID uint `json:"role_id"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.UpdateAdminRole(req.ID, req.RoleID); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, nil)
	})
	admin.POST("/users/password", func(c *gin.Context) {
		var req struct {
			ID       uint   `json:"id" binding:"required"`
			Password string `json:"password" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.ResetAdminPassword(req.ID, req.Password); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, nil)
	})
	admin.POST("/users/delete", func(c *gin.Context) {
		var req struct {
			ID uint `json:"id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.DeleteAdmin(c.GetUint("admin_id"), req.ID); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, nil)
	})

	// 权限模块列表（供角色勾选用）
	admin.GET("/roles/modules", func(c *gin.Context) {
		utils.Ok(c, model.PermissionModules)
	})

	// 当前登录用户权限（供前端隐藏菜单）
	g.GET("/auth/permissions", func(c *gin.Context) {
		perms := service.AdminPermissions(c.GetUint("admin_id"))
		utils.Ok(c, gin.H{"permissions": perms})
	})
}
