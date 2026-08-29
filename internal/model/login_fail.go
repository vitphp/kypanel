package model

import "time"

// LoginFailRecord 登录失败记录（持久化限流）。
// 以 IP + Username 为维度记录连续失败次数和锁定时间，
// 面板服务重启后依然有效，防止攻击者通过重启/多进程绕过内存限流。
type LoginFailRecord struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	IP        string    `gorm:"size:64;index:idx_login_fail_ip_user,unique" json:"ip"`
	Username  string    `gorm:"size:64;index:idx_login_fail_ip_user,unique" json:"username"`
	Count     int       `json:"count"`      // 连续失败次数
	LockedAt  time.Time `json:"locked_at"`  // 锁定起始时间（零值表示未锁定）
	UpdatedAt time.Time `json:"updated_at"`
}
