package model

import "time"

// LoginSession 登录会话记录（用于在线会话查看 / 踢下线）。
// (AdminID, IP) 加唯一索引：同一用户同 IP 只保留一条会话（同一设备/浏览器多次登录合并）。
// 忽略 UserAgent（UA 字符串因浏览器版本/窗口差异会有噪声，不适合作为去重 key）。
// TokenHash 单独唯一索引，防止同 token 误重复。
type LoginSession struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	AdminID   uint      `gorm:"uniqueIndex:idx_admin_ip" json:"admin_id"`
	Username  string    `gorm:"size:64" json:"username"`
	TokenHash string    `gorm:"size:64;uniqueIndex" json:"-"` // token 指纹：同 token upsert 防御性去重
	IP        string    `gorm:"size:64;uniqueIndex:idx_admin_ip" json:"ip"`
	UserAgent string    `gorm:"size:512" json:"user_agent"`    // 仅展示用，不参与去重
	Active    bool      `gorm:"default:true;index" json:"active"` // 是否仍活跃（登出/踢下线后置 false）
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
}
