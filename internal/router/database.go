package router

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"kypanel/internal/service"
	"kypanel/internal/utils"
)

// 数据库服务控制、root 密码管理相关路由
func registerDatabaseServiceRoutes(g *gin.RouterGroup) {
	// 查询数据库服务状态
	g.GET("/database/service-status", func(c *gin.Context) {
		dbType := c.DefaultQuery("type", "")
		if dbType == "" {
			utils.Fail(c, 400, "缺少 type 参数")
			return
		}
		status, err := service.DatabaseServiceStatus(dbType)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, gin.H{"status": status, "type": dbType})
	})

	// 启动 / 停止 / 重启数据库服务
	g.POST("/database/service-action", func(c *gin.Context) {
		dbType := c.DefaultQuery("type", "")
		var req struct {
			Action string `json:"action" binding:"required,oneof=start stop restart"`
		}
		if dbType == "" {
			utils.Fail(c, 400, "缺少 type 参数")
			return
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		if err := service.DatabaseServiceAction(dbType, req.Action); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		service.RecordOp(c.GetUint("admin_id"), "database.service", fmt.Sprintf("数据库服务[%s] %s", dbType, req.Action), c.ClientIP(), "success")
		utils.Ok(c, gin.H{"action": req.Action})
	})

	// root 账户信息（仅 MySQL 完整实现，其他类型显示"暂未支持"）
	g.GET("/database/root-info", func(c *gin.Context) {
		dbType := c.DefaultQuery("type", "")
		if dbType == "" {
			utils.Fail(c, 400, "缺少 type 参数")
			return
		}
		switch service.DatabaseType(dbType) {
		case service.DBTypeMySQL:
			isAdmin := service.IsSuperAdmin(c.GetUint("admin_id"))
			pw := service.GetMysqlRootPwd()
			if !isAdmin {
				pw = "" // 仅超管可见明文密码
			}
			utils.Ok(c, gin.H{
				"type":           dbType,
				"user":           "root",
				"host":           "localhost",
				"password":       pw,
				"masked":         !isAdmin,
				"support_change": true,
				"hint":           "修改后请在所有使用此密码的应用/站点中同步更新。",
			})
		case service.DBTypeSQLServer:
			utils.Ok(c, gin.H{
				"type":           dbType,
				"user":           "sa",
				"host":           "localhost",
				"password":       "",
				"support_change": false,
				"hint":           "SQLServer root 密码管理暂未实现，可在「应用商店 > SQLServer > 设置」中维护。",
			})
		case service.DBTypePgSQL:
			utils.Ok(c, gin.H{
				"type":           dbType,
				"user":           "postgres",
				"host":           "localhost",
				"password":       "",
				"support_change": false,
				"hint":           "PostgreSQL root 密码管理暂未实现，可在服务器上执行 sudo -u postgres psql -c \"ALTER USER postgres PASSWORD 'newpass';\" 修改。",
			})
		case service.DBTypeRedis:
			utils.Ok(c, gin.H{
				"type":           dbType,
				"user":           "(无默认账号)",
				"host":           "localhost:6379",
				"password":       "",
				"support_change": false,
				"hint":           "Redis 默认无密码，密码管理请修改 /etc/redis/redis.conf 的 requirepass 并重启服务。",
			})
		case service.DBTypeMongoDB:
			utils.Ok(c, gin.H{
				"type":           dbType,
				"user":           "admin",
				"host":           "localhost:27017",
				"password":       "",
				"support_change": false,
				"hint":           "MongoDB root 密码管理暂未实现。",
			})
		default:
			utils.Fail(c, 400, "该数据库类型暂不支持 root 账户管理")
		}
	})

	// 修改 root 密码（仅 MySQL 完整实现）
	g.POST("/database/root-password", func(c *gin.Context) {
		dbType := c.DefaultQuery("type", "")
		if dbType == "" {
			utils.Fail(c, 400, "缺少 type 参数")
			return
		}
		var req struct {
			Password string `json:"password" binding:"required,min=6"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "密码至少 6 位")
			return
		}
		if service.DatabaseType(dbType) != service.DBTypeMySQL {
			utils.Fail(c, 400, "该数据库类型暂不支持通过面板修改 root 密码")
			return
		}
		if err := service.SetMysqlRootPwd(req.Password); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "database.root_password", "修改 MySQL root 密码", "success")
		utils.Ok(c, gin.H{"password": req.Password})
	})
}


// setupDatabaseRoutes 数据库管理路由（支持 MySQL/SQLServer/MongoDB/Redis/PgSQL/SQLite）
func setupDatabaseRoutes(g *gin.RouterGroup) {
	// 数据库类型列表
	g.GET("/database/types", func(c *gin.Context) {
		utils.Ok(c, service.ListDatabaseTypes())
	})

	// 某类数据库可用性
	g.GET("/database/status", func(c *gin.Context) {
		dbType := c.DefaultQuery("type", string(service.DBTypeMySQL))
		ok, msg := service.DatabaseAvailable(dbType)
		utils.Ok(c, gin.H{"available": ok, "message": msg})
	})

	// 数据库列表
	g.GET("/database/list", func(c *gin.Context) {
		dbType := c.DefaultQuery("type", string(service.DBTypeMySQL))
		list, err := service.ListDatabasesByType(dbType)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		// 仅超管可见明文密码，子账号一律掩码
		if !service.IsSuperAdmin(c.GetUint("admin_id")) {
			for i := range list {
				if p, ok := list[i]["password"].(string); ok && p != "" {
					list[i]["password"] = service.MaskSecret(p)
				}
			}
		}
		utils.Ok(c, list)
	})

	// 创建数据库
	g.POST("/database/create", func(c *gin.Context) {
		dbType := c.DefaultQuery("type", string(service.DBTypeMySQL))
		var req service.CreateDatabaseReq
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误: "+err.Error())
			return
		}
		if err := service.CreateDatabaseByType(dbType, req); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "database.create", "创建数据库["+dbType+"]: "+req.Name, "success")
		utils.Ok(c, nil)
	})

	// 删除数据库
	g.POST("/database/delete", func(c *gin.Context) {
		dbType := c.DefaultQuery("type", string(service.DBTypeMySQL))
		var req struct {
			Name string `json:"name" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.DeleteDatabaseByType(dbType, req.Name); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "database.delete", "删除数据库["+dbType+"]: "+req.Name, "success")
		utils.Ok(c, nil)
	})

	// 修改数据库备注
	g.POST("/database/comment", func(c *gin.Context) {
		dbType := c.DefaultQuery("type", string(service.DBTypeMySQL))
		var req struct {
			Name    string `json:"name" binding:"required"`
			Comment string `json:"comment"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if err := service.UpdateDatabaseComment(dbType, req.Name, req.Comment); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "database.comment", "修改数据库备注["+dbType+"]: "+req.Name, "success")
		utils.Ok(c, nil)
	})

		// phpMyAdmin 安装状态
	g.GET("/database/pma/status", func(c *gin.Context) {
		utils.Ok(c, service.PmaStatus())
	})

	// 生成最新的 phpMyAdmin 访问 token（每次点击「管理」重新签发，防止旧 token 过期）
	g.POST("/database/pma/token", func(c *gin.Context) {
		adminID := c.GetUint("admin_id")
		username := c.GetString("username")
		token, err := service.GeneratePMAToken(adminID, username)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, gin.H{"token": token})
	})

	// 修改数据库密码（目前仅支持 MySQL）
	g.POST("/database/password", func(c *gin.Context) {
		dbType := c.DefaultQuery("type", string(service.DBTypeMySQL))
		var req struct {
			Name     string `json:"name" binding:"required"`
			Password string `json:"password" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if dbType != string(service.DBTypeMySQL) {
			utils.Fail(c, 400, "该数据库类型暂不支持修改密码")
			return
		}
		if err := service.ChangeMysqlPassword(req.Name, req.Password); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "database.password", "修改数据库密码["+dbType+"]: "+req.Name, "success")
		utils.Ok(c, nil)
	})

	// 设置数据库访问权限（目前仅支持 MySQL）
	g.POST("/database/perms", func(c *gin.Context) {
		dbType := c.DefaultQuery("type", string(service.DBTypeMySQL))
		var req struct {
			Name  string   `json:"name" binding:"required"`
			Hosts []string `json:"hosts"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			utils.Fail(c, 400, "参数错误")
			return
		}
		if dbType != string(service.DBTypeMySQL) {
			utils.Fail(c, 400, "该数据库类型暂不支持设置访问权限")
			return
		}
		if err := service.SetMysqlHosts(req.Name, req.Hosts); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "database.perms", "设置数据库权限["+dbType+"]: "+req.Name, "success")
		utils.Ok(c, nil)
	})

	// 列出数据库备份
	g.GET("/database/backup/list", func(c *gin.Context) {
		dbType := c.DefaultQuery("type", string(service.DBTypeMySQL))
		name := c.Query("name")
		if name == "" {
			utils.Fail(c, 400, "缺少数据库名")
			return
		}
		list, err := service.DatabaseBackupList(dbType, name)
		if err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		utils.Ok(c, list)
	})

	// 创建数据库备份
	g.POST("/database/backup/create", func(c *gin.Context) {
		dbType := c.DefaultQuery("type", string(service.DBTypeMySQL))
		name := c.Query("name")
		if name == "" {
			utils.Fail(c, 400, "缺少数据库名")
			return
		}
		if err := service.DatabaseBackupCreate(dbType, name); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "database.backup.create", "创建数据库备份["+dbType+"]: "+name, "success")
		utils.Ok(c, nil)
	})

	// 恢复数据库备份
	g.POST("/database/backup/restore", func(c *gin.Context) {
		dbType := c.DefaultQuery("type", string(service.DBTypeMySQL))
		name := c.Query("name")
		file := c.Query("file")
		if name == "" || file == "" {
			utils.Fail(c, 400, "缺少参数")
			return
		}
		if err := service.DatabaseBackupRestore(dbType, name, file); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "database.backup.restore", "恢复数据库备份["+dbType+"]: "+name+"/"+file, "success")
		utils.Ok(c, nil)
	})

	// 删除数据库备份
	g.POST("/database/backup/delete", func(c *gin.Context) {
		dbType := c.DefaultQuery("type", string(service.DBTypeMySQL))
		name := c.Query("name")
		file := c.Query("file")
		if name == "" || file == "" {
			utils.Fail(c, 400, "缺少参数")
			return
		}
		if err := service.DatabaseBackupDelete(dbType, name, file); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "database.backup.delete", "删除数据库备份["+dbType+"]: "+name+"/"+file, "success")
		utils.Ok(c, nil)
	})

	// 下载数据库备份
	g.GET("/database/backup/download", func(c *gin.Context) {
		dbType := c.DefaultQuery("type", string(service.DBTypeMySQL))
		name := c.Query("name")
		file := c.Query("file")
		if name == "" || file == "" {
			utils.Fail(c, 400, "缺少参数")
			return
		}
		path, err := service.DatabaseBackupPath(dbType, name, file)
		if err != nil {
			utils.Fail(c, 404, err.Error())
			return
		}
		c.FileAttachment(path, file)
	})

	// 上传并导入数据库
	g.POST("/database/import", func(c *gin.Context) {
		dbType := c.DefaultQuery("type", string(service.DBTypeMySQL))
		name := c.PostForm("name")
		if name == "" {
			utils.Fail(c, 400, "缺少数据库名")
			return
		}
		file, err := c.FormFile("file")
		if err != nil {
			utils.Fail(c, 400, "请选择导入文件")
			return
		}
		fname := strings.ToLower(file.Filename)
		allowed := strings.HasSuffix(fname, ".sql") ||
			strings.HasSuffix(fname, ".sql.gz") ||
			strings.HasSuffix(fname, ".db") ||
			strings.HasSuffix(fname, ".sqlite")
		if !allowed {
			utils.Fail(c, 400, "仅支持 .sql、.sql.gz、.db、.sqlite 文件")
			return
		}
		dest := filepath.Join(os.TempDir(), fmt.Sprintf("lp_import_%d_%s", time.Now().Unix(), file.Filename))
		if err := c.SaveUploadedFile(file, dest); err != nil {
			utils.Fail(c, 500, "保存文件失败: "+err.Error())
			return
		}
		defer os.Remove(dest)
		if err := service.DatabaseImport(dbType, name, dest); err != nil {
			utils.Fail(c, 500, err.Error())
			return
		}
		recordOpForCtx(c, "database.import", "导入数据库["+dbType+"]: "+name+"/"+file.Filename, "success")
		utils.Ok(c, nil)
	})
}
