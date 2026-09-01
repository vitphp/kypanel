package service

import (
	"errors"
	"strings"
	"time"

	"kypanel/internal/model"
)

// ============================ 角色与权限（RBAC） ============================

// EnsureBuiltinRoles 初始化内置角色（幂等）
func EnsureBuiltinRoles() {
	if model.DB == nil {
		return
	}
	builtins := []model.Role{
		{Name: "运维", Permissions: "dashboard,site,database,backup,ftp,file,container,appstore,cron,log,monitor,process,firewall,mcp", Remark: "内置：可管理除设置/用户外的所有功能", Builtin: true},
		{Name: "只读", Permissions: "dashboard,site,database,backup,file,container,appstore,cron,log,monitor,firewall,mcp", Remark: "内置：只读，不可修改任何配置", Builtin: true},
	}
	for _, r := range builtins {
		var existing model.Role
		if err := model.DB.Where("name = ?", r.Name).First(&existing).Error; err != nil {
			_ = model.DB.Create(&r).Error
		}
	}
}

// ListRoles 列出所有角色
func ListRoles() []model.Role {
	var roles []model.Role
	model.DB.Order("id asc").Find(&roles)
	return roles
}

// SaveRole 创建或更新角色
func SaveRole(r *model.Role) error {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		return errors.New("角色名不能为空")
	}
	// 规范化权限（去空白，* 时忽略其它）
	if strings.Contains(r.Permissions, "*") {
		r.Permissions = "*"
	} else {
		r.Permissions = strings.TrimSpace(r.Permissions)
	}
	if r.ID == 0 {
		return model.DB.Create(r).Error
	}
	var existing model.Role
	if err := model.DB.First(&existing, r.ID).Error; err != nil {
		return errors.New("角色不存在")
	}
	// 内置角色不允许改权限（但可改备注）
	if existing.Builtin {
		r.Permissions = existing.Permissions
		r.Name = existing.Name
	}
	return model.DB.Save(r).Error
}

// DeleteRole 删除角色（内置角色不可删；有关联用户的角色不可删）
func DeleteRole(id uint) error {
	var r model.Role
	if err := model.DB.First(&r, id).Error; err != nil {
		return errors.New("角色不存在")
	}
	if r.Builtin {
		return errors.New("内置角色不可删除")
	}
	var count int64
	model.DB.Model(&model.Admin{}).Where("role_id = ?", id).Count(&count)
	if count > 0 {
		return errors.New("该角色下仍有用户，请先调整用户角色")
	}
	return model.DB.Delete(&r).Error
}

// ============================ 用户管理 ============================

// AdminItem 用户列表项（不含密码哈希）
type AdminItem struct {
	ID        uint      `json:"id"`
	Username  string    `json:"username"`
	RoleID    uint      `json:"role_id"`
	RoleName  string    `json:"role_name"`
	TOTP      bool      `json:"totp_enabled"`
	CreatedAt time.Time `json:"created_at"`
	IsFounder bool      `json:"is_founder"` // 是否为创始用户（安装面板时创建的第一个账号）
	CanDelete bool      `json:"can_delete"` // 当前操作者是否有权删除该用户
}

// founderAdmin 返回创始用户：id 最小的账号，即安装面板时创建的第一个用户。
// 创始用户是特权账号，不可被任何人删除（包括其本人）。
func founderAdmin() *model.Admin {
	var founder model.Admin
	if err := model.DB.Order("id asc").First(&founder).Error; err != nil {
		return nil
	}
	return &founder
}

// ListAdmins 列出所有管理员（含角色名、创建时间、是否为创始用户及当前操作者可否删除）
func ListAdmins(operatorID uint) []AdminItem {
	var admins []model.Admin
	model.DB.Order("id asc").Find(&admins)
	roles := map[uint]string{}
	for _, r := range ListRoles() {
		roles[r.ID] = r.Name
	}
	founder := founderAdmin()
	var founderID uint
	if founder != nil {
		founderID = founder.ID
	}
	var out []AdminItem
	for _, a := range admins {
		item := AdminItem{
			ID:        a.ID,
			Username:  a.Username,
			RoleID:    a.RoleID,
			RoleName:  "超级管理员",
			TOTP:      a.TOTPSecret != "",
			CreatedAt: a.CreatedAt,
			IsFounder: founder != nil && a.ID == founderID,
		}
		if a.RoleID != 0 {
			item.RoleName = roles[a.RoleID]
		}
		// 允许删除：不能删自己，也不能删创始用户；
		// 其余用户均可删（创始用户可删除任何其他用户，包括其他超级管理员）
		item.CanDelete = a.ID != operatorID && !item.IsFounder
		out = append(out, item)
	}
	return out
}

// CreateAdmin 创建子账号
func CreateAdmin(username, password string, roleID uint) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return errors.New("账号不能为空")
	}
	if len(password) < 6 {
		return errors.New("密码至少 6 位")
	}
	if roleID == 0 {
		// 防止通过 API 绕过前端直接创建出 role_id=0（视为超管）的子账号
		return errors.New("请选择角色")
	}
	var count int64
	model.DB.Model(&model.Admin{}).Where("username = ?", username).Count(&count)
	if count > 0 {
		return errors.New("账号已存在")
	}
	admin := model.Admin{Username: username, RoleID: roleID}
	if err := admin.SetPassword(password); err != nil {
		return err
	}
	return model.DB.Create(&admin).Error
}

// UpdateAdminRole 修改用户角色
// operatorID 为当前操作者 ID。保护规则：
//   - 不能修改自己（防止把自己降级后锁死）；
//   - 创始用户（安装时创建的第一个账号）不可修改角色（与不可删除保持一致）。
// 其余用户（包括其他超级管理员）均可调整角色。
func UpdateAdminRole(operatorID, adminID, roleID uint) error {
	var admin model.Admin
	if err := model.DB.First(&admin, adminID).Error; err != nil {
		return errors.New("用户不存在")
	}
	if adminID == operatorID {
		return errors.New("不能修改当前登录账号的角色")
	}
	founder := founderAdmin()
	if founder != nil && adminID == founder.ID {
		return errors.New("创始用户不可修改角色")
	}
	admin.RoleID = roleID
	return model.DB.Save(&admin).Error
}

// ResetAdminPassword 重置子账号密码
func ResetAdminPassword(adminID uint, password string) error {
	if len(password) < 6 {
		return errors.New("密码至少 6 位")
	}
	var admin model.Admin
	if err := model.DB.First(&admin, adminID).Error; err != nil {
		return errors.New("用户不存在")
	}
	if err := admin.SetPassword(password); err != nil {
		return err
	}
	admin.TokenVer++ // 使该用户旧 token 失效
	if err := model.DB.Save(&admin).Error; err != nil {
		return err
	}
	InvalidateTokenVer(adminID)
	return nil
}

// DeleteAdmin 删除用户。
// 规则：
//   - 不能删除自己（包括创始用户）；
//   - 创始用户（安装面板时创建的第一个账号）不可被任何人删除；
//   - 其余用户可删除：创始用户可删除任何其他用户（包括超级管理员），
//     其他超级管理员可删除创始用户以外的任何用户。
func DeleteAdmin(operatorID, adminID uint) error {
	if adminID == operatorID {
		return errors.New("不能删除当前登录账号")
	}
	var admin model.Admin
	if err := model.DB.First(&admin, adminID).Error; err != nil {
		return errors.New("用户不存在")
	}
	founder := founderAdmin()
	if founder != nil && adminID == founder.ID {
		return errors.New("创始用户不可删除")
	}
	return model.DB.Delete(&admin).Error
}

// ============================ 权限查询 ============================

// AdminPermissions 返回指定管理员的权限模块列表（nil 表示全部权限=超管）
func AdminPermissions(adminID uint) []string {
	var admin model.Admin
	if err := model.DB.First(&admin, adminID).Error; err != nil {
		return nil
	}
	if admin.RoleID == 0 {
		return nil // 超管：全部权限
	}
	var role model.Role
	if err := model.DB.First(&role, admin.RoleID).Error; err != nil {
		return nil
	}
	if role.Permissions == "*" {
		return nil
	}
	return strings.Split(role.Permissions, ",")
}

// AdminHasPermission 判断管理员是否有某模块权限
func AdminHasPermission(adminID uint, module string) bool {
	perms := AdminPermissions(adminID)
	if perms == nil {
		return true // 超管或 * 角色
	}
	for _, p := range perms {
		if p == module {
			return true
		}
	}
	return false
}

// IsSuperAdmin 判断管理员是否为超级管理员（role_id == 0）。
// 用于命令执行、终端、重启等高危操作的硬性权限校验（不受角色 * 权限影响）。
//
// adminID == 0 也视为超管：对应「API/MCP 令牌」调用上下文（令牌不绑定具体管理员，
// 创建时已由管理员承担信任责任，默认拥有全部面板权限）。
func IsSuperAdmin(adminID uint) bool {
	if adminID == 0 {
		return true
	}
	var admin model.Admin
	if err := model.DB.First(&admin, adminID).Error; err != nil {
		return false
	}
	return admin.RoleID == 0
}
