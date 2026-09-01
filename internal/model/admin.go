package model

import (
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Admin 管理员账号
type Admin struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"size:64;uniqueIndex;not null" json:"username"`
	PasswordHash string    `gorm:"size:255;not null" json:"-"`
	TokenVer     int       `gorm:"default:0" json:"-"` // 令牌版本号：改密时 +1，使旧 token 立即失效
	TOTPSecret   string    `gorm:"size:64" json:"-"`   // 2FA/TOTP 密钥（Base32），空表示未启用
	RoleID       uint      `gorm:"default:0" json:"role_id"` // 角色 ID，0 表示超级管理员（不受权限限制）
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// SetPassword 设置密码（bcrypt 哈希）
func (a *Admin) SetPassword(plain string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	a.PasswordHash = string(hash)
	return nil
}

// CheckPassword 校验密码
func (a *Admin) CheckPassword(plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(a.PasswordHash), []byte(plain)) == nil
}

// HasAnyAdmin 判断是否存在任意管理员账号
func HasAnyAdmin() bool {
	var count int64
	DB.Model(&Admin{}).Count(&count)
	return count > 0
}

// EnsureDefaultAdmin 确保至少存在一个管理员。
// 仅当数据库中完全没有管理员时才以配置/环境变量中的默认账号创建（首次安装引导）。
// 注意：不能按 PANEL_ADMIN_USER 逐个查找后重建——panel.env 中会残留该环境变量
// （stripEnvPlainPassword 只清 PANEL_ADMIN_PASS），若按用户名找，被删除的默认账号
// 会在每次重启时被重新创建出来，导致用户管理里出现"删不掉的幽灵账号"。
func EnsureDefaultAdmin(username, password string) (*Admin, error) {
	var admin Admin
	err := DB.Order("id asc").First(&admin).Error
	if err == nil {
		return &admin, nil // 已存在任意管理员，不再创建默认账号
	}

	admin = Admin{Username: username}
	if err := admin.SetPassword(password); err != nil {
		return nil, err
	}
	if err := DB.Create(&admin).Error; err != nil {
		return nil, err
	}
	return &admin, nil
}
