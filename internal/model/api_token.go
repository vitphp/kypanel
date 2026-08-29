package model

import "time"

// ApiToken 面板 API 访问令牌（开放 API / MCP 通用凭据）。
// 每个令牌可指定用途类型（api=通用 API，mcp=AI 助手 MCP）、来源 IP 白名单、过期时间。
// 明文 token 仅在创建时返回一次，数据库只存哈希（SHA256），泄露后无法反查明文。
type ApiToken struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:64;not null" json:"name"`     // 令牌名称，如「CI 脚本」「Cursor」
	TokenHash string    `gorm:"size:64;not null;uniqueIndex" json:"-"` // SHA256(明文) 前 64 hex
	Type      string    `gorm:"size:16;not null;index" json:"type"` // api / mcp / all
	Scopes    string    `gorm:"size:512" json:"scopes"`             // 权限范围：逗号分隔的模块名（site,file,...），空或 * = 全部权限
	AllowIPs  string    `gorm:"type:text" json:"allow_ips"`         // 多行 IP，一行一个，空=不限制
	ExpireAt  *time.Time `json:"expire_at"`                         // 过期时间，nil=永不过期
	LastUsedAt *time.Time `json:"last_used_at"`                     // 最近一次使用时间
	CreatedAt time.Time `json:"created_at"`
}
