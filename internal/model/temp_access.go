package model

import "time"

// TempAccess 临时访问（临时登录链接）。
// 管理员在设置页创建临时链接，访客打开链接即可免密码进入面板后台。
// 可限制使用次数与有效期；每次使用都记录来源 IP 与归属地。
type TempAccess struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	Token     string     `gorm:"size:512;not null" json:"-"`   // AES-GCM 密文（enc:v1: 前缀），不对外返回
	TokenHash string     `gorm:"size:64;uniqueIndex" json:"-"` // token 的 SHA-256 指纹，用于索引查询
	Name      string     `gorm:"size:64" json:"name"`          // 备注名，如「给同事临时看」
	MaxUses   int        `gorm:"default:0" json:"max_uses"`    // 最大使用次数，0=不限
	UsedCount int        `gorm:"default:0" json:"used_count"`  // 已使用次数
	ExpireAt  *time.Time `json:"expire_at"`                    // 过期时间，nil=永不过期
	LastIP    string     `gorm:"size:64" json:"last_ip"`       // 最后使用 IP
	LastUsedAt *time.Time `json:"last_used_at"`                // 最后使用时间
	Status    int        `gorm:"default:1" json:"status"`      // 1=启用 0=禁用
	CreatedAt time.Time  `json:"created_at"`
}

// TempAccessUseLog 临时访问使用日志（每次使用记一条，含 IP 归属地）
type TempAccessUseLog struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	TempAccessID uint     `gorm:"index" json:"temp_access_id"`
	IP          string    `gorm:"size:64" json:"ip"`
	Region      string    `gorm:"size:128" json:"region"` // 归属地（国家|省|市|ISP）
	UserAgent   string    `gorm:"size:512" json:"user_agent"`
	CreatedAt   time.Time `json:"created_at"`
}