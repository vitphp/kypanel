package model

import "time"

// FtpUser FTP 用户（基于系统用户 + vsftpd）
type FtpUser struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Username  string    `gorm:"size:64;uniqueIndex" json:"username"`
	HomeDir   string    `gorm:"size:256" json:"home_dir"`   // 家目录（网站根目录）
	Remark    string    `gorm:"size:256" json:"remark"`
	Status    string    `gorm:"size:16;default:enabled" json:"status"` // enabled / disabled
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
