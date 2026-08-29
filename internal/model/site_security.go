package model

import "time"

// SiteSecurityConfig 单站安全配置（每个站点一行，字段全平铺便于快速读写）
type SiteSecurityConfig struct {
	ID     uint `gorm:"primaryKey" json:"id"`
	SiteID uint `gorm:"uniqueIndex" json:"site_id"`

	// 总开关
	Enabled bool `gorm:"default:false" json:"enabled"`

	// 是否沿用全局 WAF 规则（false = 覆盖全局，本规则独立）
	UseGlobalRules bool `gorm:"default:true" json:"use_global_rules"`

	// 防护模式
	Mode string `gorm:"size:16;default:block" json:"mode"` // block / observe

	// ===== 访问控制 =====
	IPWhitelistEnabled bool   `gorm:"default:false" json:"ip_whitelist_enabled"` // 白名单模式（仅放行列表内 IP）
	UAWhitelistEnabled bool   `gorm:"default:false" json:"ua_whitelist_enabled"` // UA 白名单模式
	BlockChina         bool   `gorm:"default:false" json:"block_china"`          // 屏蔽境外（仅放行中国大陆）
	RefererCheck       string `gorm:"size:16;default:off" json:"referer_check"`  // off / blacklist / whitelist

	// ===== CC 防护 =====
	CCEnabled      bool `gorm:"default:false" json:"cc_enabled"`
	CCMaxRequests  int  `gorm:"default:100" json:"cc_max_requests"` // 窗口内最大请求数
	CCWindowSec    int  `gorm:"default:10" json:"cc_window_sec"`
	CCBlockSec     int  `gorm:"default:3600" json:"cc_block_sec"`
	CCMaxConnPerIP int  `gorm:"default:0" json:"cc_max_conn_per_ip"` // 0 不限制

	// ===== HTTP 安全头 =====
	HSTS          bool `gorm:"default:false" json:"hsts"`
	XFrameOptions string `gorm:"size:16;default:DENY" json:"x_frame_options"` // DENY / SAMEORIGIN / off
	NoSniff       bool `gorm:"default:true" json:"no_sniff"`
	NoDirList     bool `gorm:"default:true" json:"no_dir_list"`

	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名
func (SiteSecurityConfig) TableName() string { return "site_security_configs" }

// SiteSecIpRule 单站 IP 规则（黑/白名单，支持 CIDR / 段 / 国家 / ISP）
type SiteSecIpRule struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	SiteID    uint       `gorm:"index" json:"site_id"`
	Action    string     `gorm:"size:8" json:"action"`      // allow / block
	MatchType string     `gorm:"size:16" json:"match_type"` // ip / cidr / range / country / isp
	Content   string     `gorm:"size:256" json:"content"`   // 具体值（IP/CIDR/国家名/ISP）
	ExpireAt  *time.Time `json:"expire_at"`
	Remark    string     `gorm:"size:256" json:"remark"`
	CreatedAt time.Time  `json:"created_at"`
}

// TableName 指定表名
func (SiteSecIpRule) TableName() string { return "site_sec_ip_rules" }

// SiteSecUaRule 单站 UA 规则（黑/白名单）
type SiteSecUaRule struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	SiteID    uint      `gorm:"index" json:"site_id"`
	Action    string    `gorm:"size:8" json:"action"` // allow / block
	Content   string    `gorm:"size:256" json:"content"`
	Remark    string    `gorm:"size:256" json:"remark"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName 指定表名
func (SiteSecUaRule) TableName() string { return "site_sec_ua_rules" }

// SiteSecRefererRule 单站 Referer 规则
type SiteSecRefererRule struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	SiteID    uint      `gorm:"index" json:"site_id"`
	Action    string    `gorm:"size:8" json:"action"` // allow / block
	Content   string    `gorm:"size:256" json:"content"`
	Remark    string    `gorm:"size:256" json:"remark"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName 指定表名
func (SiteSecRefererRule) TableName() string { return "site_sec_referer_rules" }

// SiteSecCustomRule 单站自定义规则
type SiteSecCustomRule struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	SiteID     uint      `gorm:"index" json:"site_id"`
	Name       string    `gorm:"size:128" json:"name"`
	Pattern    string    `gorm:"type:text" json:"pattern"` // 正则
	MatchField string    `gorm:"size:16;default:uri" json:"match_field"` // uri / args / ua / referer / header / body
	Action     string    `gorm:"size:8;default:block" json:"action"`     // block / log
	Enabled    bool      `gorm:"default:true" json:"enabled"`
	Remark     string    `gorm:"size:256" json:"remark"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// TableName 指定表名
func (SiteSecCustomRule) TableName() string { return "site_sec_custom_rules" }

// SiteSecLog 单站攻击日志
type SiteSecLog struct {
	ID       uint      `gorm:"primaryKey" json:"id"`
	SiteID   uint      `gorm:"index" json:"site_id"`
	Time     time.Time `gorm:"index" json:"time"`
	IP       string    `gorm:"size:64;index" json:"ip"`
	Region   string    `gorm:"size:128" json:"region"`
	Category string    `gorm:"size:32" json:"category"`
	RuleName string    `gorm:"size:128" json:"rule_name"`
	Action   string    `gorm:"size:8" json:"action"` // block / log
	Method   string    `gorm:"size:16" json:"method"`
	URI      string    `gorm:"size:1024" json:"uri"`
	UA       string    `gorm:"size:512" json:"ua"`
}

// TableName 指定表名
func (SiteSecLog) TableName() string { return "site_sec_logs" }
