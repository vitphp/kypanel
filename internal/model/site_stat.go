package model

import "time"

// SiteStatVisit 单条访问日志（按行入库），用于网站访问统计。
// 设计上每条访问一次，每次解析一行 nginx access 日志。
type SiteStatVisit struct {
	ID         uint64    `gorm:"primaryKey" json:"id"`
	SiteID     uint      `gorm:"index:idx_site_visited,priority:1" json:"site_id"`
	SiteName   string    `gorm:"size:64;index" json:"site_name"` // 冗余便于在子查询聚合时减少 join
	IP         string    `gorm:"size:64;index" json:"ip"`
	Province   string    `gorm:"size:64;index" json:"province"`
	City       string    `gorm:"size:64;index" json:"city"`
	ISP        string    `gorm:"size:64" json:"isp"`
	BytesSent  int64     `gorm:"column:bytes_sent" json:"bytes"`
	Status     int       `gorm:"index" json:"status"`
	Method     string    `gorm:"size:8" json:"method"`
	Path       string    `gorm:"size:512" json:"path"`
	UA         string    `gorm:"size:512" json:"ua"`
	UAHash     int64     `gorm:"index" json:"ua_hash"` // 用于 UV/PV 去重（int64 避免 SQLite 不支持 uint64 高位）
	Referer    string    `gorm:"size:512" json:"referer"`
	VisitedAt  time.Time `gorm:"index:idx_site_visited,priority:2" json:"visited_at"`
}

// TableName 指定表名
func (SiteStatVisit) TableName() string {
	return "site_stat_visits"
}
