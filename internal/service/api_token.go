package service

import (
	"errors"
	"net"
	"strings"
	"time"

	"kypanel/internal/model"
	"kypanel/internal/utils"
)

// ApiTokenType 令牌用途类型
const (
	ApiTokenTypeAPI = "api" // 通用开放 API
	ApiTokenTypeMCP = "mcp" // AI 助手 MCP
	ApiTokenTypeAll = "all" // 通用：既能访问 /api/* 也能访问 /mcp
)

// apiTokenScopeModules 令牌可选权限范围（模块名 -> 中文名）。
// 与 middleware.PermissionGuard 的模块划分保持一致。
// 空 / * 表示全部权限（兼容旧令牌与需要完整权限的场景）。
var apiTokenScopeModules = []struct{ Key, Label string }{
	{"site", "网站"}, {"database", "数据库"}, {"backup", "备份"}, {"ftp", "FTP"},
	{"file", "文件"}, {"container", "容器"}, {"appstore", "应用商店"}, {"cron", "计划任务"},
	{"log", "日志"}, {"monitor", "监控"}, {"process", "进程"}, {"firewall", "防火墙"},
	{"mcp", "MCP"}, {"settings", "设置"},
}

// ApiTokenScopeKeys 返回全部可选权限范围（供前端渲染选择项）
func ApiTokenScopeKeys() []string {
	out := make([]string, 0, len(apiTokenScopeModules))
	for _, m := range apiTokenScopeModules {
		out = append(out, m.Key)
	}
	return out
}

// ApiTokenScopeLabel 模块中文名
func ApiTokenScopeLabel(key string) string {
	for _, m := range apiTokenScopeModules {
		if m.Key == key {
			return m.Label
		}
	}
	return key
}

// CreateApiTokenReq 创建令牌请求
type CreateApiTokenReq struct {
	Name     string `json:"name" binding:"required,min=1,max=64"`
	Type     string `json:"type" binding:"required"` // api / mcp / all
	AllowIPs string `json:"allow_ips"`               // 多行 IP，一行一个，空=不限制
	// ExpireDays 过期天数：0 表示永不过期
	ExpireDays int `json:"expire_days"`
	// Scopes 权限范围：逗号分隔的模块名（如 "site,file,monitor"），空或 * = 全部权限
	Scopes string `json:"scopes"`
}

// ApiTokenView 令牌列表项（不返回明文/哈希）
type ApiTokenView struct {
	ID         uint       `json:"id"`
	Name       string     `json:"name"`
	Type       string     `json:"type"`
	Scopes     []string   `json:"scopes"`
	AllowIPs   []string   `json:"allow_ips"`
	ExpireAt   *time.Time `json:"expire_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// ListApiTokens 返回所有令牌（脱敏，不含哈希与明文）
func ListApiTokens() []ApiTokenView {
	var tokens []model.ApiToken
	if err := model.DB.Order("id DESC").Find(&tokens).Error; err != nil {
		return nil
	}
	out := make([]ApiTokenView, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, toApiTokenView(&t))
	}
	return out
}

func toApiTokenView(t *model.ApiToken) ApiTokenView {
	view := ApiTokenView{
		ID:         t.ID,
		Name:       t.Name,
		Type:       t.Type,
		AllowIPs:   splitAllowIPs(t.AllowIPs),
		ExpireAt:   t.ExpireAt,
		LastUsedAt: t.LastUsedAt,
		CreatedAt:  t.CreatedAt,
	}
	// scopes 为空或 * 表示全部权限，前端展示空数组即可
	if s := strings.TrimSpace(t.Scopes); s != "" && s != "*" {
		for _, k := range strings.Split(s, ",") {
			if k = strings.TrimSpace(k); k != "" {
				view.Scopes = append(view.Scopes, k)
			}
		}
	}
	return view
}

// CreateApiToken 创建令牌，返回明文（仅此一次可见）
func CreateApiToken(req CreateApiTokenReq) (string, *ApiTokenView, error) {
	if req.Type != ApiTokenTypeAPI && req.Type != ApiTokenTypeMCP && req.Type != ApiTokenTypeAll {
		return "", nil, errors.New("令牌类型必须是 api / mcp / all")
	}
	// 校验 IP 白名单每行是合法 IP（不支持网段/通配符，一行一个精确 IP）
	ips, err := normalizeAllowIPs(req.AllowIPs)
	if err != nil {
		return "", nil, err
	}
	// 校验并规范化权限范围（空 / * 表示全部权限）
	scopes, err := normalizeApiTokenScopes(req.Scopes)
	if err != nil {
		return "", nil, err
	}

	plain, err := utils.GenerateAPIKey()
	if err != nil {
		return "", nil, err
	}

	t := model.ApiToken{
		Name:      strings.TrimSpace(req.Name),
		TokenHash: utils.HashAPIKey(plain),
		Type:      req.Type,
		Scopes:    scopes,
		AllowIPs:  strings.Join(ips, "\n"),
	}
	if req.ExpireDays > 0 {
		exp := time.Now().AddDate(0, 0, req.ExpireDays)
		t.ExpireAt = &exp
	}
	if err := model.DB.Create(&t).Error; err != nil {
		return "", nil, err
	}
	view := toApiTokenView(&t)
	return plain, &view, nil
}

// DeleteApiToken 删除令牌
func DeleteApiToken(id uint) error {
	return model.DB.Delete(&model.ApiToken{}, id).Error
}

// normalizeAllowIPs 规范化 IP 白名单：逐行 trim、去空、校验是合法 IP。
// 返回规范化后的 IP 列表（去重，保持顺序）。
func normalizeAllowIPs(text string) ([]string, error) {
	lines := strings.Split(text, "\n")
	seen := map[string]bool{}
	var out []string
	for _, ln := range lines {
		ip := strings.TrimSpace(ln)
		if ip == "" {
			continue
		}
		if net.ParseIP(ip) == nil {
			return nil, errors.New("非法 IP 地址: " + ip + "（不支持网段与通配符，请每行填写一个精确 IP）")
		}
		if !seen[ip] {
			seen[ip] = true
			out = append(out, ip)
		}
	}
	return out, nil
}

// normalizeApiTokenScopes 校验并规范化权限范围：
// 空 / "*" / "全部" → 返回 ""（表示全部权限，兼容旧令牌）；
// 否则逐项 trim、去重，且必须是合法模块名。
func normalizeApiTokenScopes(scopes string) (string, error) {
	raw := strings.TrimSpace(scopes)
	if raw == "" || raw == "*" {
		return "", nil
	}
	valid := map[string]bool{}
	for _, m := range apiTokenScopeModules {
		valid[m.Key] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		k := strings.TrimSpace(part)
		if k == "" {
			continue
		}
		if !valid[k] {
			return "", errors.New("非法权限范围: " + k + "（可选：" + strings.Join(ApiTokenScopeKeys(), "、") + "）")
		}
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return strings.Join(out, ","), nil
}

func splitAllowIPs(text string) []string {
	if text == "" {
		return []string{}
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		if s := strings.TrimSpace(ln); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// VerifyApiToken 校验 API 令牌（用于开放 API / MCP 鉴权）。
// 参数：
//   - tokenType: 期望的令牌类型（api 或 mcp），用于路由分流
//   - clientIP: 请求来源 IP，用于白名单校验
// 返回 (adminID, scopes, ok)。ok=false 表示令牌无效/类型不符/白名单拒绝/已过期。
// scopes 为该令牌声明的权限范围（逗号分隔模块名），空表示全部权限。
//
// 支持 type=api / mcp / all 三种：
//   - all 类型对所有 tokenType 都通过（既能访问 /api/* 也能访问 /mcp）
func VerifyApiToken(tokenType, plain, clientIP string) (uint, string, bool) {
	if plain == "" {
		return 0, "", false
	}
	hash := utils.HashAPIKey(plain)
	// 先按精确类型查，找不到再查 type=all（通用令牌）
	var t model.ApiToken
	err := model.DB.Where("token_hash = ? AND type = ?", hash, tokenType).First(&t).Error
	if err != nil {
		if err := model.DB.Where("token_hash = ? AND type = ?", hash, ApiTokenTypeAll).First(&t).Error; err != nil {
			return 0, "", false
		}
	}
	// 过期检查
	if t.ExpireAt != nil && time.Now().After(*t.ExpireAt) {
		return 0, "", false
	}
	// IP 白名单检查（空=不限制）
	if t.AllowIPs != "" {
		if !ipAllowed(t.AllowIPs, clientIP) {
			return 0, "", false
		}
	}
	// 更新最近使用时间（异步，不阻塞鉴权主流程）
	now := time.Now()
	go func(id uint) {
		model.DB.Model(&model.ApiToken{}).Where("id = ?", id).Update("last_used_at", now)
	}(t.ID)

	// 令牌本身不绑定具体管理员（面板是单管理员/超管模型），返回 0 表示走默认超管上下文。
	// 鉴权中间件会据 admin_id=0 走超管（role_id=0）逻辑。
	return 0, strings.TrimSpace(t.Scopes), true
}

// ipAllowed 判断 clientIP 是否在白名单文本中（精确匹配，一行一个 IP）
func ipAllowed(allowText, clientIP string) bool {
	for _, ln := range strings.Split(allowText, "\n") {
		if ip := strings.TrimSpace(ln); ip != "" && ip == clientIP {
			return true
		}
	}
	return false
}
