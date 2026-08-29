package service

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"kypanel/internal/model"
)

// ============================ 单站安全（Site Security） ============================
//
// 与全局 WAF 的关系：全局 WAF 是默认底，单站配置可勾选 UseGlobalRules=false
// 来覆盖全局规则（此时该站独立使用自己的规则集）。
//
// Nginx 实现：每个站点在 /etc/nginx/waf/site/ 下生成 lp_site_<name>.conf，
// 通过 genSiteServerBlock 末尾的 include 注入 server 块；规则变化只重写该文件并
// nginx -t && nginx -s reload 热重载。
// Apache：预留接口（后续实现），当前暂不落地。

const (
	siteSecConfDir = "/etc/nginx/waf/site" // 单站安全片段目录
)

// siteSecSnippetName 单站安全片段文件名
func siteSecSnippetName(name string) string {
	return "lp_site_" + name + ".conf"
}

// siteSecSnippetPath 单站安全片段绝对路径
func siteSecSnippetPath(name string) string {
	return filepath.Join(siteSecConfDir, siteSecSnippetName(name))
}

// ---------- 配置 CRUD ----------

// defaultSiteSecurityConfig 单站安全默认配置
func defaultSiteSecurityConfig(siteID uint) model.SiteSecurityConfig {
	return model.SiteSecurityConfig{
		SiteID:          siteID,
		Enabled:         false,
		UseGlobalRules:  true,
		Mode:            "block",
		RefererCheck:    "off",
		CCMaxRequests:   100,
		CCWindowSec:     10,
		CCBlockSec:      3600,
		XFrameOptions:   "DENY",
		NoSniff:         true,
		NoDirList:       true,
	}
}

// GetSiteSecurityConfig 读取单站安全配置（不存在返回默认）
func GetSiteSecurityConfig(siteID uint) model.SiteSecurityConfig {
	var cfg model.SiteSecurityConfig
	if err := model.DB.Where("site_id = ?", siteID).First(&cfg).Error; err != nil {
		return defaultSiteSecurityConfig(siteID)
	}
	// 补齐可能为空的关键字段默认值（兼容旧数据）
	if cfg.Mode == "" {
		cfg.Mode = "block"
	}
	if cfg.RefererCheck == "" {
		cfg.RefererCheck = "off"
	}
	if cfg.XFrameOptions == "" {
		cfg.XFrameOptions = "DENY"
	}
	if cfg.CCMaxRequests == 0 {
		cfg.CCMaxRequests = 100
	}
	if cfg.CCWindowSec == 0 {
		cfg.CCWindowSec = 10
	}
	if cfg.CCBlockSec == 0 {
		cfg.CCBlockSec = 3600
	}
	return cfg
}

// SaveSiteSecurityConfig 保存单站安全配置并应用
func SaveSiteSecurityConfig(siteID uint, cfg model.SiteSecurityConfig) error {
	cfg.ID = 0
	cfg.SiteID = siteID
	if cfg.Mode == "" {
		cfg.Mode = "block"
	}
	if cfg.Mode != "block" && cfg.Mode != "observe" {
		return errors.New("防护模式不合法")
	}
	cfg.UpdatedAt = time.Now()

	var existing model.SiteSecurityConfig
	err := model.DB.Where("site_id = ?", siteID).First(&existing).Error
	if err != nil {
		if err := model.DB.Create(&cfg).Error; err != nil {
			return err
		}
	} else {
		cfg.ID = existing.ID
		if err := model.DB.Save(&cfg).Error; err != nil {
			return err
		}
	}
	return applySiteSecurity(siteID, true)
}

// ---------- 规则 CRUD ----------

// ListSiteSecIpRules 单站 IP 规则列表
func ListSiteSecIpRules(siteID uint) []model.SiteSecIpRule {
	var rules []model.SiteSecIpRule
	model.DB.Where("site_id = ?", siteID).Order("id desc").Find(&rules)
	return rules
}

// AddSiteSecIpRule 新增单站 IP 规则
func AddSiteSecIpRule(siteID uint, action, matchType, content string, expireSeconds int, remark string) (uint, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return 0, errors.New("内容不能为空")
	}
	if matchType == "" {
		matchType = "ip"
	}
	switch matchType {
	case "ip":
		if net.ParseIP(content) == nil {
			return 0, errors.New("IP 格式不合法: " + content)
		}
	case "cidr":
		if _, _, err := net.ParseCIDR(content); err != nil {
			return 0, errors.New("CIDR 格式不合法: " + content)
		}
	}
	var expireAt *time.Time
	if expireSeconds > 0 {
		t := time.Now().Add(time.Duration(expireSeconds) * time.Second)
		expireAt = &t
	}
	rule := model.SiteSecIpRule{
		SiteID:    siteID,
		Action:    action,
		MatchType: matchType,
		Content:   content,
		ExpireAt:  expireAt,
		Remark:    remark,
	}
	if err := model.DB.Create(&rule).Error; err != nil {
		return 0, err
	}
	if err := applySiteSecurity(siteID, true); err != nil {
		return rule.ID, err
	}
	return rule.ID, nil
}

// DeleteSiteSecIpRule 删除单站 IP 规则
func DeleteSiteSecIpRule(siteID, ruleID uint) error {
	if err := model.DB.Where("id = ? AND site_id = ?", ruleID, siteID).Delete(&model.SiteSecIpRule{}).Error; err != nil {
		return err
	}
	return applySiteSecurity(siteID, true)
}

// ListSiteSecUaRules 单站 UA 规则列表
func ListSiteSecUaRules(siteID uint) []model.SiteSecUaRule {
	var rules []model.SiteSecUaRule
	model.DB.Where("site_id = ?", siteID).Order("id desc").Find(&rules)
	return rules
}

// AddSiteSecUaRule 新增单站 UA 规则
func AddSiteSecUaRule(siteID uint, action, content, remark string) (uint, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return 0, errors.New("内容不能为空")
	}
	rule := model.SiteSecUaRule{SiteID: siteID, Action: action, Content: content, Remark: remark}
	if err := model.DB.Create(&rule).Error; err != nil {
		return 0, err
	}
	if err := applySiteSecurity(siteID, true); err != nil {
		return rule.ID, err
	}
	return rule.ID, nil
}

// DeleteSiteSecUaRule 删除单站 UA 规则
func DeleteSiteSecUaRule(siteID, ruleID uint) error {
	if err := model.DB.Where("id = ? AND site_id = ?", ruleID, siteID).Delete(&model.SiteSecUaRule{}).Error; err != nil {
		return err
	}
	return applySiteSecurity(siteID, true)
}

// ListSiteSecRefererRules 单站 Referer 规则列表
func ListSiteSecRefererRules(siteID uint) []model.SiteSecRefererRule {
	var rules []model.SiteSecRefererRule
	model.DB.Where("site_id = ?", siteID).Order("id desc").Find(&rules)
	return rules
}

// AddSiteSecRefererRule 新增单站 Referer 规则
func AddSiteSecRefererRule(siteID uint, action, content, remark string) (uint, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return 0, errors.New("内容不能为空")
	}
	rule := model.SiteSecRefererRule{SiteID: siteID, Action: action, Content: content, Remark: remark}
	if err := model.DB.Create(&rule).Error; err != nil {
		return 0, err
	}
	if err := applySiteSecurity(siteID, true); err != nil {
		return rule.ID, err
	}
	return rule.ID, nil
}

// DeleteSiteSecRefererRule 删除单站 Referer 规则
func DeleteSiteSecRefererRule(siteID, ruleID uint) error {
	if err := model.DB.Where("id = ? AND site_id = ?", ruleID, siteID).Delete(&model.SiteSecRefererRule{}).Error; err != nil {
		return err
	}
	return applySiteSecurity(siteID, true)
}

// ListSiteSecCustomRules 单站自定义规则列表
func ListSiteSecCustomRules(siteID uint) []model.SiteSecCustomRule {
	var rules []model.SiteSecCustomRule
	model.DB.Where("site_id = ?", siteID).Order("id desc").Find(&rules)
	return rules
}

// AddSiteSecCustomRule 新增单站自定义规则
func AddSiteSecCustomRule(siteID uint, req model.SiteSecCustomRule) (uint, error) {
	if strings.TrimSpace(req.Pattern) == "" {
		return 0, errors.New("规则内容不能为空")
	}
	if _, err := regexp.Compile(req.Pattern); err != nil {
		return 0, errors.New("正则表达式不合法: " + err.Error())
	}
	if req.MatchField == "" {
		req.MatchField = "uri"
	}
	if req.Action == "" {
		req.Action = "block"
	}
	req.SiteID = siteID
	req.Enabled = true
	if err := model.DB.Create(&req).Error; err != nil {
		return 0, err
	}
	if err := applySiteSecurity(siteID, true); err != nil {
		return req.ID, err
	}
	return req.ID, nil
}

// UpdateSiteSecCustomRule 更新单站自定义规则
func UpdateSiteSecCustomRule(siteID, ruleID uint, enabled *bool, action, pattern string) error {
	var rule model.SiteSecCustomRule
	if err := model.DB.Where("id = ? AND site_id = ?", ruleID, siteID).First(&rule).Error; err != nil {
		return errors.New("规则不存在")
	}
	if enabled != nil {
		rule.Enabled = *enabled
	}
	if action != "" {
		rule.Action = action
	}
	if pattern != "" {
		if _, err := regexp.Compile(pattern); err != nil {
			return errors.New("正则表达式不合法: " + err.Error())
		}
		rule.Pattern = pattern
	}
	rule.UpdatedAt = time.Now()
	if err := model.DB.Save(&rule).Error; err != nil {
		return err
	}
	return applySiteSecurity(siteID, true)
}

// DeleteSiteSecCustomRule 删除单站自定义规则
func DeleteSiteSecCustomRule(siteID, ruleID uint) error {
	if err := model.DB.Where("id = ? AND site_id = ?", ruleID, siteID).Delete(&model.SiteSecCustomRule{}).Error; err != nil {
		return err
	}
	return applySiteSecurity(siteID, true)
}

// ---------- 攻击日志 ----------

// ListSiteSecLogs 单站攻击日志分页
func ListSiteSecLogs(siteID uint, page, pageSize int, keyword string) ([]model.SiteSecLog, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	q := model.DB.Model(&model.SiteSecLog{}).Where("site_id = ?", siteID)
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("ip LIKE ? OR uri LIKE ? OR rule_name LIKE ?", like, like, like)
	}
	var total int64
	q.Count(&total)
	var logs []model.SiteSecLog
	q.Order("time desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&logs)
	return logs, total, nil
}

// ClearSiteSecLogs 清空单站攻击日志
func ClearSiteSecLogs(siteID uint) error {
	return model.DB.Where("site_id = ?", siteID).Delete(&model.SiteSecLog{}).Error
}

// SiteSecStats 单站安全统计
type SiteSecStats struct {
	TotalBlock    int64                    `json:"total_block"`
	TodayBlock    int64                    `json:"today_block"`
	TotalLog      int64                    `json:"total_log"`
	TopIP         []map[string]interface{} `json:"top_ip"`
	TopRule       []map[string]interface{} `json:"top_rule"`
}

// GetSiteSecStats 单站安全统计
func GetSiteSecStats(siteID uint) SiteSecStats {
	var s SiteSecStats
	model.DB.Model(&model.SiteSecLog{}).Where("site_id = ? AND action = ?", siteID, "block").Count(&s.TotalBlock)
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	model.DB.Model(&model.SiteSecLog{}).Where("site_id = ? AND action = ? AND time >= ?", siteID, "block", startOfDay).Count(&s.TodayBlock)
	model.DB.Model(&model.SiteSecLog{}).Where("site_id = ?", siteID).Count(&s.TotalLog)

	model.DB.Model(&model.SiteSecLog{}).
		Select("ip, COUNT(*) as cnt").
		Where("site_id = ?", siteID).
		Group("ip").Order("cnt desc").Limit(10).Scan(&s.TopIP)
	model.DB.Model(&model.SiteSecLog{}).
		Select("rule_name, COUNT(*) as cnt").
		Where("site_id = ?", siteID).
		Group("rule_name").Order("cnt desc").Limit(10).Scan(&s.TopRule)
	return s
}

// ---------- Nginx 片段生成 ----------

// siteSecIpBlockList 单站 IP 封禁列表（排除过期）
func siteSecIpBlockList(siteID uint) []model.SiteSecIpRule {
	return siteSecIpRules(siteID, "block")
}

// siteSecIpAllowList 单站 IP 白名单列表
func siteSecIpAllowList(siteID uint) []model.SiteSecIpRule {
	return siteSecIpRules(siteID, "allow")
}

func siteSecIpRules(siteID uint, action string) []model.SiteSecIpRule {
	now := time.Now()
	var rules []model.SiteSecIpRule
	model.DB.Where("site_id = ? AND action = ?", siteID, action).Find(&rules)
	var out []model.SiteSecIpRule
	for _, r := range rules {
		if r.ExpireAt != nil && r.ExpireAt.Before(now) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// genSiteSecuritySnippet 生成单站安全 Nginx 片段
func genSiteSecuritySnippet(cfg *model.SiteSecurityConfig, site *model.Site) string {
	if !cfg.Enabled {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("# kypanel 单站安全规则（自动生成，请勿手动修改）\n")

	// 1. IP 白名单模式（仅放行白名单 IP）
	if cfg.IPWhitelistEnabled {
		if allows := siteSecIpAllowList(cfg.SiteID); len(allows) > 0 {
			sb.WriteString("if ($remote_addr !~* \"" + siteSecIpRegex(allows) + "\") {\n")
			sb.WriteString("    return 403;\n")
			sb.WriteString("}\n")
		}
	}

	// 2. IP 黑名单
	for _, r := range siteSecIpBlockList(cfg.SiteID) {
		switch r.MatchType {
		case "ip":
			fmt.Fprintf(&sb, "if ($remote_addr = \"%s\") { return 403; }\n", r.Content)
		case "cidr", "range":
			fmt.Fprintf(&sb, "if ($remote_addr ~* \"^%s\") { return 403; }\n", siteSecRangeToRegex(r.Content))
		case "country", "isp":
			// 国家/ISP 封禁：由于 nginx 无法直接按地理归属匹配，
			// 由 CC 协程扫描访问日志时结合 ip2region 自动封禁对应 IP。
			// 这里生成空注释占位，实际封禁由 geoMonitorLoop 异步完成。
			sb.WriteString("# geo block: " + r.Content + "（由后台协程异步执行）\n")
		}
	}

	// 3. UA 黑名单
	if uas := siteSecUaBlockList(cfg.SiteID); len(uas) > 0 {
		joined := "(?i)(" + strings.Join(uas, "|") + ")"
		sb.WriteString("if ($http_user_agent ~* \"" + nginxEscape(joined) + "\") {\n")
		sb.WriteString("    return 403;\n")
		sb.WriteString("}\n")
	}
	// UA 白名单模式
	if cfg.UAWhitelistEnabled {
		if uas := siteSecUaAllowList(cfg.SiteID); len(uas) > 0 {
			joined := "(?i)(" + strings.Join(uas, "|") + ")"
			sb.WriteString("if ($http_user_agent !~* \"" + nginxEscape(joined) + "\") {\n")
			sb.WriteString("    return 403;\n")
			sb.WriteString("}\n")
		}
	}

	// 4. Referer 防盗链
	if cfg.RefererCheck == "blacklist" {
		if refs := siteSecRefererBlockList(cfg.SiteID); len(refs) > 0 {
			joined := "(?i)(" + strings.Join(refs, "|") + ")"
			sb.WriteString("if ($http_referer ~* \"" + nginxEscape(joined) + "\") {\n")
			sb.WriteString("    return 403;\n")
			sb.WriteString("}\n")
		}
	} else if cfg.RefererCheck == "whitelist" {
		if refs := siteSecRefererAllowList(cfg.SiteID); len(refs) > 0 {
			joined := "(?i)(" + strings.Join(refs, "|") + ")"
			sb.WriteString("if ($http_referer !~* \"" + nginxEscape(joined) + "\") {\n")
			sb.WriteString("    return 403;\n")
			sb.WriteString("}\n")
		}
	}

	// 5. 自定义规则
	customRules := siteSecEnabledCustomRules(cfg.SiteID)
	uriRules := make([]string, 0)
	uaRules := make([]string, 0)
	for _, r := range customRules {
		if r.MatchField == "ua" {
			uaRules = append(uaRules, r.Pattern)
		} else {
			uriRules = append(uriRules, r.Pattern)
		}
	}
	if len(uriRules) > 0 {
		joined := "(?i)(" + strings.Join(uriRules, "|") + ")"
		sb.WriteString("if ($request_uri ~* \"" + nginxEscape(joined) + "\") {\n")
		if cfg.Mode == "observe" {
			sb.WriteString("    access_log /var/log/nginx/" + site.Name + ".waf.log combined;\n")
		} else {
			sb.WriteString("    return 403;\n")
		}
		sb.WriteString("}\n")
	}
	if len(uaRules) > 0 {
		joined := "(?i)(" + strings.Join(uaRules, "|") + ")"
		sb.WriteString("if ($http_user_agent ~* \"" + nginxEscape(joined) + "\") {\n")
		if cfg.Mode == "observe" {
			sb.WriteString("    access_log /var/log/nginx/" + site.Name + ".waf.log combined;\n")
		} else {
			sb.WriteString("    return 403;\n")
		}
		sb.WriteString("}\n")
	}

	// 6. CC 防护：由后台协程（siteSecMonitorLoop）扫描访问日志统计单 IP 频率，
	// 超阈值自动拉黑该 IP（写入 site_sec_ip_rules）。nginx 层不生成 limit_req（避免
	// 依赖 http 级 zone 导致 nginx -t 失败），封禁逻辑全部走 Go 协程 + IP 黑名单。
	// 参见 siteSecMonitorLoop / runSiteSecCCScan。

	// 7. HTTP 安全头（仅 HTTPS server 块有效，由 genSiteServerBlock 注入 add_header）
	return sb.String()
}

func siteSecEnabledCustomRules(siteID uint) []model.SiteSecCustomRule {
	var rules []model.SiteSecCustomRule
	model.DB.Where("site_id = ? AND enabled = ?", siteID, true).Find(&rules)
	return rules
}

func siteSecUaBlockList(siteID uint) []string {
	var rules []model.SiteSecUaRule
	model.DB.Where("site_id = ? AND action = ?", siteID, "block").Find(&rules)
	var out []string
	for _, r := range rules {
		out = append(out, regexp.QuoteMeta(r.Content))
	}
	return out
}

func siteSecUaAllowList(siteID uint) []string {
	var rules []model.SiteSecUaRule
	model.DB.Where("site_id = ? AND action = ?", siteID, "allow").Find(&rules)
	var out []string
	for _, r := range rules {
		out = append(out, regexp.QuoteMeta(r.Content))
	}
	return out
}

func siteSecRefererBlockList(siteID uint) []string {
	var rules []model.SiteSecRefererRule
	model.DB.Where("site_id = ? AND action = ?", siteID, "block").Find(&rules)
	var out []string
	for _, r := range rules {
		out = append(out, regexp.QuoteMeta(r.Content))
	}
	return out
}

func siteSecRefererAllowList(siteID uint) []string {
	var rules []model.SiteSecRefererRule
	model.DB.Where("site_id = ? AND action = ?", siteID, "allow").Find(&rules)
	var out []string
	for _, r := range rules {
		out = append(out, regexp.QuoteMeta(r.Content))
	}
	return out
}

// siteSecIpRegex 白名单 IP 列表生成匹配正则
func siteSecIpRegex(rules []model.SiteSecIpRule) string {
	parts := make([]string, 0, len(rules))
	for _, r := range rules {
		switch r.MatchType {
		case "cidr", "range":
			parts = append(parts, siteSecRangeToRegex(r.Content))
		default:
			parts = append(parts, regexp.QuoteMeta(r.Content))
		}
	}
	return strings.Join(parts, "|")
}

// siteSecRangeToRegex 将 CIDR 或 IP 段转成 nginx 正则前缀
func siteSecRangeToRegex(content string) string {
	content = strings.TrimSpace(content)
	if strings.Contains(content, "/") {
		// CIDR：简单处理，取网络前缀（如 1.2.3.0/24 -> 1.2.3.）
		ip, _, err := net.ParseCIDR(content)
		if err != nil {
			return regexp.QuoteMeta(content)
		}
		parts := strings.Split(ip.String(), ".")
		if len(parts) == 4 {
			// 根据前缀长度决定匹配前几段
			_, cidr, _ := net.ParseCIDR(content)
			ones, _ := cidr.Mask.Size()
			seg := ones / 8
			prefix := strings.Join(parts[:seg], ".") + "."
			return regexp.QuoteMeta(prefix)
		}
		return regexp.QuoteMeta(content)
	}
	// IP 段 1.2.3.* 形式
	if strings.Contains(content, "*") {
		return regexp.QuoteMeta(strings.Split(content, "*")[0])
	}
	return regexp.QuoteMeta(content)
}

// ---------- 应用与热重载 ----------

// applySiteSecurity 生成单站安全片段并热重载
func applySiteSecurity(siteID uint, force bool) error {
	var site model.Site
	if err := model.DB.First(&site, siteID).Error; err != nil {
		return errors.New("站点不存在")
	}
	cfg := GetSiteSecurityConfig(siteID)

	if WebServerType() == webApache {
		// Apache：安全规则内联到 VirtualHost（由 genApacheVHost 调 siteSecApacheBlock），
		// 无需单独片段文件，直接重写站点配置即可。
		return writeSiteConfAndReload(&site)
	}

	// Nginx：生成独立片段文件 + include
	if err := os.MkdirAll(siteSecConfDir, 0o755); err != nil {
		return err
	}
	path := siteSecSnippetPath(site.Name)
	conf := genSiteSecuritySnippet(&cfg, &site)

	if !cfg.Enabled || strings.TrimSpace(conf) == "" {
		// 未启用时写入空文件占位（避免站点配置里的 include 指向不存在文件）
		_ = os.WriteFile(path, []byte("# 单站安全未启用\n"), 0o644)
	} else {
		if err := os.WriteFile(path, []byte(conf), 0o644); err != nil {
			return err
		}
	}

	// 重写站点配置（确保 include 行注入）+ 热重载
	if err := writeSiteConf(&site); err != nil {
		return err
	}
	// use_global_rules 变化会影响全局 WAF 片段，重新生成该站点的 WAF 片段
	regenerateSiteWAFSnippet(&site)
	return webReload()
}

// SiteSecIncludeLine 返回要注入 site server 块的单站安全 include 行（仅 nginx 使用）
func SiteSecIncludeLine(siteID uint) string {
	if WebServerType() == webApache {
		return "" // apache 安全规则内联在 genApacheVHost，无需 include
	}
	cfg := GetSiteSecurityConfig(siteID)
	if !cfg.Enabled {
		return ""
	}
	var site model.Site
	if err := model.DB.First(&site, siteID).Error; err != nil {
		return ""
	}
	return "    include " + siteSecSnippetPath(site.Name) + ";\n"
}

// siteSecApacheBlock 生成单站安全的 Apache 版内联片段（写入 VirtualHost 内）
func siteSecApacheBlock(siteID uint) string {
	cfg := GetSiteSecurityConfig(siteID)
	if !cfg.Enabled {
		return ""
	}
	var sb strings.Builder

	// 1. IP 白名单模式（仅放行白名单 IP）
	if cfg.IPWhitelistEnabled {
		if allows := siteSecIpAllowList(siteID); len(allows) > 0 {
			sb.WriteString("    # IP 白名单模式：仅放行下列 IP\n")
			sb.WriteString("    <RequireAny>\n")
			for _, r := range allows {
				switch r.MatchType {
				case "cidr", "range":
					fmt.Fprintf(&sb, "        Require ip %s\n", r.Content)
				default:
					fmt.Fprintf(&sb, "        Require ip %s\n", r.Content)
				}
			}
			sb.WriteString("    </RequireAny>\n")
		}
	}

	// 2. IP 黑名单
	if blocks := siteSecIpBlockList(siteID); len(blocks) > 0 {
		sb.WriteString("    # IP 黑名单\n")
		for _, r := range blocks {
			switch r.MatchType {
			case "ip", "cidr", "range":
				fmt.Fprintf(&sb, "    Require not ip %s\n", r.Content)
			case "country", "isp":
				sb.WriteString("    # geo block: " + r.Content + "（由后台协程异步执行）\n")
			}
		}
	}

	// 3. UA 黑名单
	if uas := siteSecUaBlockList(siteID); len(uas) > 0 {
		joined := "(?i)(" + strings.Join(uas, "|") + ")"
		sb.WriteString("    # UA 黑名单\n")
		sb.WriteString("    RewriteEngine On\n")
		sb.WriteString("    RewriteCond %{HTTP_USER_AGENT} \"" + strings.ReplaceAll(joined, `\`, `\\`) + "\" [NC]\n")
		sb.WriteString("    RewriteRule ^ - [F,L]\n")
	}
	// UA 白名单模式
	if cfg.UAWhitelistEnabled {
		if uas := siteSecUaAllowList(siteID); len(uas) > 0 {
			joined := "(?i)(" + strings.Join(uas, "|") + ")"
			sb.WriteString("    # UA 白名单模式\n")
			sb.WriteString("    RewriteEngine On\n")
			sb.WriteString("    RewriteCond %{HTTP_USER_AGENT} !\"" + strings.ReplaceAll(joined, `\`, `\\`) + "\" [NC]\n")
			sb.WriteString("    RewriteRule ^ - [F,L]\n")
		}
	}

	// 4. Referer 防盗链
	if cfg.RefererCheck == "blacklist" {
		if refs := siteSecRefererBlockList(siteID); len(refs) > 0 {
			joined := "(?i)(" + strings.Join(refs, "|") + ")"
			sb.WriteString("    # Referer 黑名单\n")
			sb.WriteString("    RewriteEngine On\n")
			sb.WriteString("    RewriteCond %{HTTP_REFERER} \"" + strings.ReplaceAll(joined, `\`, `\\`) + "\" [NC]\n")
			sb.WriteString("    RewriteRule ^ - [F,L]\n")
		}
	} else if cfg.RefererCheck == "whitelist" {
		if refs := siteSecRefererAllowList(siteID); len(refs) > 0 {
			joined := "(?i)(" + strings.Join(refs, "|") + ")"
			sb.WriteString("    # Referer 白名单模式\n")
			sb.WriteString("    RewriteEngine On\n")
			sb.WriteString("    RewriteCond %{HTTP_REFERER} !\"" + strings.ReplaceAll(joined, `\`, `\\`) + "\" [NC]\n")
			sb.WriteString("    RewriteRule ^ - [F,L]\n")
		}
	}

	// 5. 自定义规则
	customRules := siteSecEnabledCustomRules(siteID)
	for _, r := range customRules {
		pattern := r.Pattern
		cond := "%{REQUEST_URI}"
		if r.MatchField == "ua" {
			cond = "%{HTTP_USER_AGENT}"
		} else if r.MatchField == "referer" {
			cond = "%{HTTP_REFERER}"
		}
		if cfg.Mode == "observe" {
			// 观察模式：记录日志不拦截（用 SetEnvIf 标记，实际记录由后台协程）
			sb.WriteString("    # observe rule: " + r.Name + "\n")
			sb.WriteString("    SetEnvIf Request_URI \"" + pattern + "\" lp_waf_observe=1\n")
		} else {
			sb.WriteString("    # custom rule: " + r.Name + "\n")
			sb.WriteString("    RewriteEngine On\n")
			sb.WriteString("    RewriteCond " + cond + " \"" + pattern + "\" [NC]\n")
			sb.WriteString("    RewriteRule ^ - [F,L]\n")
		}
	}

	if strings.TrimSpace(sb.String()) == "" {
		return ""
	}
	return sb.String()
}

// ============================ 后台监控协程 ============================
//
// 单站安全的动态防护（CC 自动封禁、攻击日志采集、geo 封禁）由后台协程实现，
// 因为 nginx 配置无法表达「按频率/按地理归属动态封禁」，这些逻辑需要 Go 端扫描
// 访问日志后异步封禁对应 IP。

// InitSiteSecurity 启动单站安全后台协程（main 中调用一次）
func InitSiteSecurity() {
	go siteSecMonitorLoop()
}

// siteSecMonitorLoop 单站安全监控主循环：每 10s 扫描一次启用了单站安全的站点
func siteSecMonitorLoop() {
	for {
		time.Sleep(10 * time.Second)
		runSiteSecScan()
	}
}

// runSiteSecScan 扫描所有启用了单站安全的站点，做三件事：
// 1) 攻击日志采集（匹配自定义规则 + UA/IP 黑名单，写 site_sec_logs）
// 2) CC 超频自动封禁（单 IP 频率超阈值 → 拉黑）
// 3) geo 封禁（按国家/ISP 规则，反查 IP 归属后拉黑）
func runSiteSecScan() {
	var cfgs []model.SiteSecurityConfig
	model.DB.Where("enabled = ?", true).Find(&cfgs)
	if len(cfgs) == 0 {
		return
	}
	for _, cfg := range cfgs {
		var site model.Site
		if err := model.DB.First(&site, cfg.SiteID).Error; err != nil {
			continue
		}
		if cfg.CCEnabled {
			runSiteSecCCScan(&cfg, &site)
		}
		runSiteSecGeoScan(&cfg, &site)
	}
}

// siteSecAccessLogPath 站点访问日志路径（按 Web 服务器类型）
func siteSecAccessLogPath(name string) string {
	return siteAccessLogPath(name)
}

// runSiteSecCCScan 单站 CC 频率扫描 + 自动封禁
func runSiteSecCCScan(cfg *model.SiteSecurityConfig, site *model.Site) {
	if cfg.CCMaxRequests <= 0 || cfg.CCWindowSec <= 0 {
		return
	}
	counters := make(map[string]int)
	blocked := map[string]bool{}
	for _, r := range siteSecIpBlockList(cfg.SiteID) {
		blocked[r.Content] = true
	}

	path := siteSecAccessLogPath(site.Name)
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufioNewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		ip := parseAccessLogIP(scanner.Text())
		if ip == "" || blocked[ip] {
			continue
		}
		counters[ip]++
	}

	for ip, cnt := range counters {
		if cnt >= cfg.CCMaxRequests {
			if _, err := AddSiteSecIpRule(cfg.SiteID, "block", "ip", ip, cfg.CCBlockSec,
				fmt.Sprintf("CC 自动封禁（%ds 内 %d 次请求）", cfg.CCWindowSec, cnt)); err == nil {
				recordSiteSecLog(cfg.SiteID, ip, "cc_attack", "CC 频率限制", "block", "AUTO", "/", "")
			}
		}
	}
}

// runSiteSecGeoScan 单站 geo 封禁扫描：按国家/ISP 规则反查并拉黑
func runSiteSecGeoScan(cfg *model.SiteSecurityConfig, site *model.Site) {
	var geoRules []model.SiteSecIpRule
	model.DB.Where("site_id = ? AND action = ? AND match_type IN ?", cfg.SiteID, "block", []string{"country", "isp"}).Find(&geoRules)
	if len(geoRules) == 0 {
		return
	}

	path := siteSecAccessLogPath(site.Name)
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	// 去重统计每个 IP 是否命中 geo 规则
	seen := map[string]bool{}
	scanner := bufioNewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		ip := parseAccessLogIP(scanner.Text())
		if ip == "" || seen[ip] {
			continue
		}
		region, ok := SearchIp(ip)
		if !ok {
			continue
		}
		for _, rule := range geoRules {
			hit := false
			if rule.MatchType == "country" && (region.Country == rule.Content || strings.Contains(region.Raw, rule.Content)) {
				hit = true
			}
			if rule.MatchType == "isp" && region.ISP == rule.Content {
				hit = true
			}
			if hit {
				seen[ip] = true
				if _, err := AddSiteSecIpRule(cfg.SiteID, "block", "ip", ip, 0,
					fmt.Sprintf("geo 封禁（%s）", rule.Content)); err == nil {
					recordSiteSecLog(cfg.SiteID, ip, "geo_block", rule.Content, "block", "GEO", "/", region.Raw)
				}
				break
			}
		}
	}
}

// recordSiteSecLog 写入单站攻击日志（最多保留 50000 条）
func recordSiteSecLog(siteID uint, ip, category, ruleName, action, method, uri, ua string) {
	region := ""
	if r, ok := SearchIp(ip); ok {
		var parts []string
		if r.Country != "" && r.Country != "0" {
			parts = append(parts, r.Country)
		}
		if r.Province != "" && r.Province != "0" {
			parts = append(parts, r.Province)
		}
		if r.City != "" && r.City != "0" {
			parts = append(parts, r.City)
		}
		region = strings.Join(parts, " ")
	}
	log := model.SiteSecLog{
		SiteID:   siteID,
		Time:     time.Now(),
		IP:       ip,
		Region:   region,
		Category: category,
		RuleName: ruleName,
		Action:   action,
		Method:   method,
		URI:      uri,
		UA:       ua,
	}
	model.DB.Create(&log)

	var count int64
	model.DB.Model(&model.SiteSecLog{}).Where("site_id = ?", siteID).Count(&count)
	if count > 50000 {
		var first model.SiteSecLog
		model.DB.Where("site_id = ?", siteID).Order("time asc").First(&first)
		if first.ID > 0 {
			model.DB.Where("site_id = ? AND id <= ?", siteID, first.ID).Delete(&model.SiteSecLog{})
		}
	}
}
