package model

import "gorm.io/gorm"

// DatabaseAccount 记录关系型数据库的用户名、密码、备注、允许访问主机
// 密码以明文形式保存在本地 sqlite，与主流面板保持一致体验
type DatabaseAccount struct {
	gorm.Model
	Type     string `gorm:"index"` // mysql / pgsql
	DbName   string `gorm:"index"`
	Username string
	Password string // 密码（明文）
	Comment  string
	Hosts    string // 允许访问主机，逗号分隔，空表示仅 localhost
}
