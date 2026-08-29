package service

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// 通用短期 token 管理器：
// 用于下载、预览、导出等"无法携带 Bearer 头、只能把凭证放 URL"的场景，
// 统一封装「创建 + 校验 + 过期 + 一次性消费」，避免各处散落重复逻辑。

// shortToken 一条 token 记录
type shortToken struct {
	Scope     string    // 用途标识（backup / oplog / file / cron_backup ...）
	Target    string    // 绑定目标（任务ID、文件路径等），防止 A 的 token 用于 B
	ExpiresAt time.Time // 过期时间
	Once      bool      // 是否一次性（true=用一次即失效）
}

var shortTokens sync.Map

// ShortTokenOptions 创建短期 token 的选项
type ShortTokenOptions struct {
	Scope  string        // 用途
	Target string        // 绑定目标
	TTL    time.Duration // 有效期（<=0 默认 5 分钟）
	Once   bool          // 是否一次性
}

// NewShortToken 创建短期 token，返回 token 字符串。
// 创建与校验统一走本文件，调用方只需记住 scope/target 约定。
func NewShortToken(opts ShortTokenOptions) string {
	if opts.TTL <= 0 {
		opts.TTL = 5 * time.Minute
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		// 极端失败退化为时间戳，仍保证可用
		return hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000000")))
	}
	token := hex.EncodeToString(b)
	shortTokens.Store(token, shortToken{
		Scope:     opts.Scope,
		Target:    opts.Target,
		ExpiresAt: time.Now().Add(opts.TTL),
		Once:      opts.Once,
	})
	return token
}

// VerifyShortToken 校验短期 token。
// - 一次性 token（Once=true）：校验通过后立即删除（用一次即失效）。
// - 多次 token（Once=false）：校验通过不删除，直到过期。
// 只有 scope 与 target 都匹配且未过期才返回 true。
func VerifyShortToken(token, scope, target string) bool {
	if token == "" {
		return false
	}
	v, ok := shortTokens.Load(token)
	if !ok {
		return false
	}
	entry := v.(shortToken)
	if time.Now().After(entry.ExpiresAt) {
		shortTokens.Delete(token) // 过期即清理
		return false
	}
	if entry.Scope != scope || entry.Target != target {
		return false
	}
	if entry.Once {
		shortTokens.Delete(token)
	}
	return true
}

// ============ 便捷包装（保持旧 API 兼容） ============

// NewDownloadToken 生成一次性下载 token（绑定 scope+target），5 分钟有效。
func NewDownloadToken(scope, target string) string {
	return NewShortToken(ShortTokenOptions{Scope: scope, Target: target, TTL: 5 * time.Minute, Once: true})
}

// ConsumeDownloadToken 校验并消费一次性 token。
func ConsumeDownloadToken(token, scope, target string) bool {
	return VerifyShortToken(token, scope, target)
}

// NewPreviewToken 生成短时效（10 分钟）可多次使用的预览 token（绑定 scope+target）。
func NewPreviewToken(scope, target string) string {
	return NewShortToken(ShortTokenOptions{Scope: scope, Target: target, TTL: 10 * time.Minute, Once: false})
}

// VerifyPreviewToken 校验预览 token（不消费，可多次使用）。
func VerifyPreviewToken(token, scope, target string) bool {
	return VerifyShortToken(token, scope, target)
}
