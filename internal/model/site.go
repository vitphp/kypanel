package model

import "time"

// 站点类型与状态
const (
	SiteTypeStatic = "static" // 纯静态站
	SiteTypePHP    = "php"    // PHP 站点（Nginx + PHP-FPM）
	SiteTypeNode   = "node"   // Node.js 站点（反代本地端口）
	SiteTypePython = "python" // Python 站点（反代本地端口）
	SiteTypeGo     = "go"     // Go 站点（反代本地端口）
	SiteTypeProxy  = "proxy"  // 反向代理（任意 URL）

	SiteRunning = "running"
	SiteStopped = "stopped"
)

// SiteRuntimeTypes 前端可选择的站点类型列表
var SiteRuntimeTypes = []string{
	SiteTypeStatic,
	SiteTypePHP,
	SiteTypePython,
	SiteTypeGo,
	SiteTypeNode,
	SiteTypeProxy,
}

// IsRootType 是否为带根目录的站点类型（静态/PHP）
func IsRootType(t string) bool {
	return t == SiteTypeStatic || t == SiteTypePHP
}

// Site 网站记录（Nginx 站点）
type Site struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	Name           string    `gorm:"size:64;uniqueIndex" json:"name"` // 站点标识（用于配置文件名）
	Domain         string    `gorm:"size:255" json:"domain"`          // 主域名或 IP
	Domains        string    `gorm:"size:512" json:"domains"`         // 附加域名，逗号分隔
	Port           int       `json:"port"`                            // 监听端口
	Type           string    `gorm:"size:16" json:"type"`             // static / php / node / python / go / proxy
	Root           string    `gorm:"size:512" json:"root"`            // 静态站根目录 / 项目路径
	RuntimeDir     string    `gorm:"size:512" json:"runtime_dir"`     // 运行子目录（如 public），最终 root = Root + RuntimeDir
	DefaultIndex   string    `gorm:"size:128" json:"default_index"`   // 默认文档，如 index.php index.html
	RateLimitKbs   int       `json:"rate_limit_kbs"`                  // 流量限制 KB/s，0 表示不限
	Rewrite        string    `gorm:"size:4096" json:"rewrite"`        // 伪静态规则
	HotlinkEnabled bool      `json:"hotlink_enabled"`                 // 防盗链开关
	HotlinkReferers string   `gorm:"size:1024" json:"hotlink_referers"` // 防盗链白名单域名，逗号分隔
	CacheEnabled   bool      `json:"cache_enabled"`                    // 静态资源缓存（图片 30d / JS·CSS 12h）
	SecurityHeaders bool     `json:"security_headers"`                 // 安全响应头（防跨站/防嗅探/防 MIME 嗅探）
	RedirectURL    string    `gorm:"size:512" json:"redirect_url"`    // 重定向目标 URL（兼容旧单条）
	RedirectCode   int       `json:"redirect_code"`                   // 重定向状态码 301 / 302 / 307 / 308
	RedirectKeepPath bool    `json:"redirect_keep_path"`              // 重定向时保留路径与 query（$request_uri），否则仅跳根路径
	Redirects      []SiteRedirect `gorm:"-" json:"redirects"`         // 多条重定向规则（关联表，不映射列）
	SslEnabled     bool      `json:"ssl_enabled"`                     // HTTPS 开关
	SslForce       bool      `json:"ssl_force"`                       // 强制 HTTP 跳转 HTTPS
	SslCertPath    string    `gorm:"size:512" json:"-"`               // 证书文件路径（内容不随详情返回）
	SslKeyPath     string    `gorm:"size:512" json:"-"`               // 私钥文件路径（内容不随详情返回）
	ConfigOverride string    `gorm:"size:16384" json:"config_override"` // 自定义完整配置（非空时完全覆盖自动生成）
	PhpFpm         string    `gorm:"size:128" json:"php_fpm"`         // PHP fastcgi_pass 目标
	ProxyPass      string    `gorm:"size:512" json:"proxy_pass"`      // 反向代理目标
	RuntimeVersion string    `gorm:"size:64" json:"runtime_version"`  // 运行时版本（如 PHP 8.2 / Python 3.11 / Node 18）
	StartCommand   string    `gorm:"size:512" json:"start_command"`   // python/node/go 启动命令
	EnvVars        string    `gorm:"size:1024" json:"env_vars"`       // 环境变量，KEY=VALUE 每行一个
	ProxyPort      int       `json:"proxy_port"`                      // python/node/go 应用运行端口
	Framework      string    `gorm:"size:32" json:"framework"`        // python 框架：flask / django / generic
	Status         string    `gorm:"size:16" json:"status"`           // running / stopped
	Remark         string    `gorm:"size:255" json:"remark"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// 重定向匹配类型
const (
	RedirectMatchDomain = "domain" // 按域名匹配
	RedirectMatchDir    = "dir"    // 按目录匹配
)

// SiteRedirect 站点重定向规则（一个站点可有多条）
type SiteRedirect struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	SiteID   uint   `gorm:"index" json:"site_id"`
	MatchType string `gorm:"size:16" json:"match_type"`  // domain / dir
	Domains  string `gorm:"size:1024" json:"domains"`     // 匹配域名，逗号分隔（match_type=domain）
	Paths    string `gorm:"size:1024" json:"paths"`       // 匹配目录，逗号分隔（match_type=dir），如 /blog
	TargetURL string `gorm:"size:512" json:"target_url"`  // 目标 URL
	Code     int    `json:"code"`                          // 301 / 302 / 307 / 308
	KeepPath bool   `json:"keep_path"`                     // 保留路径与 query
	Sort     int    `json:"sort"`                          // 排序（越小越靠前）
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SSLCertRecord 已申请的 SSL 证书记录
// 支持一键申请（ACME）和手动上传的证书统一管理
type SSLCertRecord struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:128;uniqueIndex" json:"name"`      // 证书标识名（如域名或自定义名称）
	Brand     string    `gorm:"size:32" json:"brand"`                  // letsencrypt / litessl / custom
	Domains   string    `gorm:"size:512" json:"domains"`               // 域名列表，逗号分隔
	CertPath  string    `gorm:"size:512" json:"cert_path"`             // 证书 PEM 文件路径
	KeyPath   string    `gorm:"size:512" json:"key_path"`              // 私钥 PEM 文件路径
	Algorithm string    `gorm:"size:16" json:"algorithm"`              // rsa2048 / ecc256
	Email     string    `gorm:"size:128" json:"email"`                 // ACME 账户邮箱
	NotBefore time.Time `json:"not_before"`                            // 生效时间
	NotAfter  time.Time `json:"not_after"`                             // 过期时间
	Days      int       `json:"days"`                                  // 剩余天数
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
