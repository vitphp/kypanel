package model

import "time"

// MailDomain 一个邮箱域名租户。每域名独立邮箱命名空间，互不可见。
// 用户在此域名下拥有 name@domain 的邮箱。
type MailDomain struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Domain    string    `gorm:"size:255;uniqueIndex;not null" json:"domain"` // 如 example.com（小写，不带 @/*）
	Enabled   bool      `gorm:"default:true" json:"enabled"`                 // 是否启用该域名邮箱（收信开关）
	Remark    string    `gorm:"size:255" json:"remark"`                      // 备注

	// 容量/配额（单位 MB，0 = 使用面板默认）
	MaxAccounts  int64 `gorm:"default:0" json:"max_accounts"` // 0 = 不限账号数
	QuotaPerBox  int64 `gorm:"default:1024" json:"quota_per_box"` // 单账号容量上限 MB

	// DNS / 防伪配置状态（供前端"绑定/解析引导"展示，是否已配由用户勾选/自检）
	MxConfigured    bool `gorm:"default:false" json:"mx_configured"`
	SpfConfigured   bool `gorm:"default:false" json:"spf_configured"`
	DkimConfigured  bool `gorm:"default:false" json:"dkim_configured"`
	DmarcConfigured bool `gorm:"default:false" json:"dmarc_configured"`

	// DKIM 私钥（加密/明文存储由 service 决定；P4 启用签名时写入）
	DkimPrivateKey string `gorm:"size:4096" json:"-"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MailDomainView 返回给前端的管理视图（不含私钥）
type MailDomainView struct {
	ID              uint      `json:"id"`
	Domain          string    `json:"domain"`
	Enabled         bool      `json:"enabled"`
	Remark          string    `json:"remark"`
	MaxAccounts     int64     `json:"max_accounts"`
	QuotaPerBox     int64     `json:"quota_per_box"`
	MxConfigured    bool      `json:"mx_configured"`
	SpfConfigured   bool      `json:"spf_configured"`
	DkimConfigured  bool      `json:"dkim_configured"`
	DmarcConfigured bool      `json:"dmarc_configured"`
	CreatedAt       time.Time `json:"created_at"`
}

// TableName 指定表名
func (MailDomain) TableName() string { return "mail_domains" }

// MailDnsGuide 某域名的 DNS 绑定引导信息（返回给前端展示"怎么解析"）
type MailDnsGuide struct {
	Domain    string `json:"domain"`
	MailServer string `json:"mail_server"` // 指向的 A/主机记录目标
	// MX
	MxHost  string `json:"mx_host"`
	MxValue string `json:"mx_value"`
	// SPF
	SpfValue string `json:"spf_value"`
	// DKIM（P4 签名启用后有真实公钥；P1 给出占位引导）
	DkimHost  string `json:"dkim_host"`
	DkimValue string `json:"dkim_value"`
	// DMARC
	DmarcHost  string `json:"dmarc_host"`
	DmarcValue string `json:"dmarc_value"`
	// 说明文字
	Notes []string `json:"notes"`
}

// Mailbox 一个邮箱账号。地址 = Name + "@" + 所属 Domain。
// 例如 Domain=example.com 下，Name=admin → 邮箱 admin@example.com。
type Mailbox struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	DomainID     uint      `gorm:"not null;index" json:"domain_id"`
	Name         string    `gorm:"size:64;not null" json:"name"` // @ 前的部分（不含 @）
	Domain       string    `gorm:"size:255" json:"domain"`       // 冗余存域名字符串，方便展示
	PasswordHash string    `gorm:"size:255" json:"-"`
	Enabled      bool      `gorm:"default:true" json:"enabled"`
	QuotaMb      int64     `gorm:"default:1024" json:"quota_mb"` // 该账号容量上限 MB
	Remark       string    `gorm:"size:255" json:"remark"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Address 返回完整邮箱地址
func (m *Mailbox) Address() string { return m.Name + "@" + m.Domain }

// TableName 指定表名
func (Mailbox) TableName() string { return "mail_mailboxes" }

// MailboxView 返回给前端的展示视图（不含密码）
type MailboxView struct {
	ID        uint      `json:"id"`
	Domain    string    `json:"domain"`
	Name      string    `json:"name"`
	Address   string    `json:"address"`
	Enabled   bool      `json:"enabled"`
	QuotaMb   int64     `json:"quota_mb"`
	Remark    string    `json:"remark"`
	CreatedAt time.Time `json:"created_at"`
}
