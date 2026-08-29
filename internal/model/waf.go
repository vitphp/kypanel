package model

import "time"

// WAFSetting WAF 全局配置
type WAFSetting struct {
	Key   string `gorm:"primaryKey;size:64" json:"key"`
	Value string `gorm:"size:2048" json:"value"`
}

// WAFRule 攻击防护规则（内置 + 自定义共用一张表）
type WAFRule struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	Category   string    `gorm:"size:32;index" json:"category"` // sql_inject / xss / cmd_inject / path_traversal / file_include / code_exec / scanner / sensitive_file / webshell / bad_ua / custom
	CategoryCN string    `gorm:"size:64" json:"category_cn"`
	Name       string    `gorm:"size:128" json:"name"`
	Pattern    string    `gorm:"type:text" json:"pattern"`     // 正则表达式（Go 正则兼容，Nginx 生成时转 PCRE）
	MatchField string    `gorm:"size:16" json:"match_field"`   // uri / args / ua / referer / header / body
	Action     string    `gorm:"size:16" json:"action"`        // block / log
	Enabled    bool      `gorm:"default:true" json:"enabled"`  // 是否启用
	Builtin    bool      `gorm:"default:false" json:"builtin"` // 是否内置规则
	Priority   int       `gorm:"default:0" json:"priority"`    // 自定义规则优先级，越大越靠前
	Remark     string    `gorm:"size:256" json:"remark"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// WAFIpRule 黑白名单（IP / URL / UA）
type WAFIpRule struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	Type      string     `gorm:"size:8;index" json:"type"`   // ip / url / ua
	Action    string     `gorm:"size:8" json:"action"`       // allow / block
	Content   string     `gorm:"size:512" json:"content"`    // IP、URL 或 UA 内容，一行一个用换行分隔
	ExpireAt  *time.Time `json:"expire_at"`                  // 临时封禁到期时间，nil 表示永久
	Remark    string     `gorm:"size:256" json:"remark"`
	CreatedAt time.Time  `json:"created_at"`
}

// WAFCcConfig CC 防护配置
type WAFCcConfig struct {
	ID            uint   `gorm:"primaryKey" json:"id"`
	Enabled       bool   `gorm:"default:false" json:"enabled"`
	MaxRequests   int    `gorm:"default:100" json:"max_requests"`     // 窗口内最大请求数
	WindowSec     int    `gorm:"default:10" json:"window_sec"`        // 统计窗口（秒）
	BlockSec      int    `gorm:"default:3600" json:"block_sec"`       // 封禁时长（秒）
	MaxConnPerIP  int    `gorm:"default:50" json:"max_conn_per_ip"`   // 单 IP 最大并发连接数（0 不限制）
	IncludeStatic bool   `gorm:"default:false" json:"include_static"` // 是否统计静态资源请求
	Whitelist     string `gorm:"type:text" json:"whitelist"`          // CC 白名单 IP，一行一个
	UpdatedAt     time.Time `json:"updated_at"`
}

// SiteBlockIP 站点级 IP 黑名单（仅对该站点生效，独立于全局 WAF 黑白名单）
type SiteBlockIP struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	SiteID    uint       `gorm:"index:idx_siteip_site,priority:1" json:"site_id"`
	IP        string     `gorm:"size:64;index" json:"ip"`
	ExpireAt  *time.Time `json:"expire_at"`           // 临时拉黑到期时间，nil 表示永久
	Remark    string     `gorm:"size:256" json:"remark"`
	CreatedAt time.Time  `json:"created_at"`
}

// TableName 指定表名
func (SiteBlockIP) TableName() string { return "site_block_ips" }

// WAFAttackLog 攻击拦截日志
type WAFAttackLog struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	Time       time.Time `gorm:"index" json:"time"`
	IP         string    `gorm:"size:64;index" json:"ip"`
	Region     string    `gorm:"size:128" json:"region"` // 归属地
	Site       string    `gorm:"size:128" json:"site"`   // 命中站点域名
	Category   string    `gorm:"size:32" json:"category"`
	CategoryCN string    `gorm:"size:64" json:"category_cn"`
	RuleName   string    `gorm:"size:128" json:"rule_name"`
	Action     string    `gorm:"size:8" json:"action"` // block / log
	Method     string    `gorm:"size:16" json:"method"`
	URI        string    `gorm:"size:1024" json:"uri"`
	UA         string    `gorm:"size:512" json:"ua"`
}
