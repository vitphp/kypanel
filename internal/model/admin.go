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

// EnsureDefaultAdmin 确保至少存在一个管理员。
// 若不存在则以配置中的默认账号创建（首次启动引导）。
func EnsureDefaultAdmin(username, password string) (*Admin, error) {
	var admin Admin
	err := DB.Where("username = ?", username).First(&admin).Error
	if err == nil {
		return &admin, nil
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
