package model

import "time"

// MailDomain 一个邮箱域名租户。每域名独立邮箱命名空间，互不可见。
// 用户在此域名下拥有 name@domain 的邮箱。
type MailDomain struct {
	ID      uint   `gorm:"primaryKey" json:"id"`
	Domain  string `gorm:"size:255;uniqueIndex;not null" json:"domain"` // 如 example.com（小写，不带 @/*）
	Enabled bool   `gorm:"default:true" json:"enabled"`                 // 是否启用该域名邮箱（收信开关）
	Remark  string `gorm:"size:255" json:"remark"`                      // 备注

	// 容量/配额（单位 MB，0 = 使用面板默认）
	MaxAccounts int64 `gorm:"default:0" json:"max_accounts"`     // 0 = 不限账号数
	QuotaPerBox int64 `gorm:"default:1024" json:"quota_per_box"` // 单账号容量上限 MB

	// DNS / 防伪配置状态（供前端"绑定/解析引导"展示，是否已配由用户勾选/自检）
	MxConfigured    bool `gorm:"default:false" json:"mx_configured"`
	SpfConfigured   bool `gorm:"default:false" json:"spf_configured"`
	DkimConfigured  bool `gorm:"default:false" json:"dkim_configured"`
	DmarcConfigured bool `gorm:"default:false" json:"dmarc_configured"`

	// ===== 邮箱门户网站（官网 + webmail）=====
	// 每个邮箱域名可开启一个门户站点：默认域名 mail.<domain>，默认关闭。
	// 开启后该站点对外可访问，作为官网并可开放注册供他人使用邮箱。
	PortalEnabled      bool   `gorm:"default:false" json:"portal_enabled"`        // 门户是否开启
	PortalDomain       string `gorm:"size:255" json:"portal_domain"`              // 门户域名（默认 mail.<domain>）
	PortalExtraDomains string `gorm:"size:1024" json:"portal_extra_domains"`      // 附加绑定域名（逗号分隔）
	PortalSSL          bool   `gorm:"default:false" json:"portal_ssl"`            // 是否启用 HTTPS
	PortalTitle        string `gorm:"size:255" json:"portal_title"`               // 官网标题（浏览器标题）
	PortalName         string `gorm:"size:255" json:"portal_name"`                // 网站名称
	PortalLogo         string `gorm:"size:1024" json:"portal_logo"`               // Logo 图片地址
	PortalFooter       string `gorm:"size:512" json:"portal_footer"`              // 底部版权
	PortalRegister     bool   `gorm:"default:false" json:"portal_register"`       // 是否开放注册
	PortalSiteID       uint   `gorm:"default:0" json:"portal_site_id"`            // 关联的站点 ID（0=未创建）

	// 门户 HTTPS 证书申请配置（开启 HTTPS 时按此自动申请免费证书）
	PortalCertBrand   string `gorm:"size:32" json:"portal_cert_brand"`     // 证书品牌：letsencrypt | litessl
	PortalCertAlgo    string `gorm:"size:32" json:"portal_cert_algo"`      // 证书算法：rsa2048 | ecc256
	PortalCertEmail   string `gorm:"size:255" json:"portal_cert_email"`    // 证书到期通知邮箱（可选）
	PortalCertDomains string `gorm:"size:1024" json:"portal_cert_domains"` // 证书覆盖的域名（逗号分隔）

	// DKIM 私钥
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
	Domain     string `json:"domain"`
	MailServer string `json:"mail_server"` // 指向的 A/主机记录目标
	// MX
	MxHost  string `json:"mx_host"`
	MxValue string `json:"mx_value"`
	// SPF
	SpfValue string `json:"spf_value"`
	// DKIM
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

// MailMessage 一封邮件的索引元数据（正文存 maildir 文件，这里存便于收件箱列表/已读/搜索）。
type MailMessage struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	MailboxID uint   `gorm:"not null;index" json:"mailbox_id"` // 所属账号 id
	Domain    string `gorm:"size:255;index" json:"domain"`
	Mailbox   string `gorm:"size:64" json:"mailbox"`    // @ 前的账号名
	Address   string `gorm:"size:255" json:"address"`   // 完整收件地址 name@domain
	Filename  string `gorm:"size:255" json:"-"`         // maildir 文件名（不含路径）
	FromAddr  string `gorm:"size:255" json:"from_addr"` // 解析出的发件人
	FromName  string `gorm:"size:255" json:"from_name"` // 发件人显示名（已解码）
	ToAddrs   string `gorm:"size:1024" json:"to_addrs"` // 收件人（逗号分隔，发件/草稿用）
	Subject   string `gorm:"size:512" json:"subject"`   // 主题（已解码）
	Date      int64  `json:"date"`                      // 投递时间 unix 秒
	Seen      bool   `gorm:"default:false;index" json:"seen"`
	HasAttach bool   `gorm:"default:false" json:"has_attach"`     // 是否含附件
	Folder    string `gorm:"size:32;default:inbox" json:"folder"` // inbox / sent / drafts ...
	RawSize   int64  `json:"raw_size"`
	MessageID string `gorm:"size:255" json:"message_id"`
}

// TableName 指定表名
func (MailMessage) TableName() string { return "mail_messages" }

// MailMessageView 收件箱列表/详情视图
type MailMessageView struct {
	ID          uint                 `json:"id"`
	Address     string               `json:"address"`
	FromAddr    string               `json:"from_addr"`
	FromName    string               `json:"from_name"`
	ToAddrs     string               `json:"to_addrs"`
	Subject     string               `json:"subject"`
	Date        int64                `json:"date"`
	Seen        bool                 `json:"seen"`
	HasAttach   bool                 `json:"has_attach"`
	Folder      string               `json:"folder"`
	RawSize     int64                `json:"raw_size"`
	TextBody    string               `json:"text_body,omitempty"`
	HtmlBody    string               `json:"html_body,omitempty"`
	Attachments []MailAttachmentView `json:"attachments,omitempty"` // 仅详情接口填充
}

// MailAttachmentView 附件展示信息（不含内容）
type MailAttachmentView struct {
	Index       int    `json:"index"` // 在附件列表中的序号（下载用）
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}
