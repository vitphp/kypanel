package service

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"kypanel/internal/config"
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
		CaptchaTTL:      1800,
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

// SiteSecIpRuleView 单站 IP 规则视图（仅在列表时附加归属地，不入库）
type SiteSecIpRuleView struct {
	model.SiteSecIpRule
	Region *IpRegion `json:"region,omitempty"`
}

// ListSiteSecIpRules 单站 IP 规则列表（过滤已过期；返回总数便于前端展示）
func ListSiteSecIpRules(siteID uint) []SiteSecIpRuleView {
	var rules []model.SiteSecIpRule
	// 只返回未过期的：永久（ExpireAt IS NULL）或未到期（ExpireAt > NOW）
	now := time.Now()
	model.DB.Where("site_id = ? AND (expire_at IS NULL OR expire_at > ?)", siteID, now).
		Order("id desc").Find(&rules)
	out := make([]SiteSecIpRuleView, 0, len(rules))
	for _, r := range rules {
		v := SiteSecIpRuleView{SiteSecIpRule: r}
		// 仅对单 IP 类型批量查归属地（依赖内置 ip2region.xdb 离线库，IPv4 命中、IPv6 返空）
		if r.MatchType == "ip" {
			if reg, ok := SearchIp(r.Content); ok {
				v.Region = reg
			}
		}
		out = append(out, v)
	}
	return out
}

// AddSiteSecIpRule 新增单站 IP 规则
// AddSiteSecIpRule 新增单站 IP 规则（写入 DB 后立即重写 nginx 配置并热重载）
func AddSiteSecIpRule(siteID uint, action, matchType, content string, expireSeconds int, remark string) (uint, error) {
	id, err := addSiteSecIpRuleNoApply(siteID, action, matchType, content, expireSeconds, remark)
	if err != nil {
		return 0, err
	}
	if err := applySiteSecurity(siteID, true); err != nil {
		return id, err
	}
	return id, nil
}

// addSiteSecIpRuleNoApply 新增单站 IP 规则但不触发 nginx 重写（批量扫描时由调用方统一 apply/reload）。
func addSiteSecIpRuleNoApply(siteID uint, action, matchType, content string, expireSeconds int, remark string) (uint, error) {
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

// siteSecGlobalConfPath nginx http 级 map 配置文件（定义 $lp_ss_cc 变量）
const siteSecGlobalConfPath = "/etc/nginx/lp_sitesec_global.conf"

// geoCountryConfPath 国家 CIDR 映射文件（由 ip2region.xdb 离线生成，供 nginx map 实时判断 geo）
func geoCountryConfPath() string {
	return filepath.Join(config.Get().DataDir, "data", "geo_country.conf")
}

// genSiteSecGlobal 生成 nginx http 级单站安全全局配置（定义 $lp_ss_cc：请求时实时映射 IP→国家）
func genSiteSecGlobal() string {
	var sb strings.Builder
	sb.WriteString("# kypanel 单站安全全局配置（自动生成，请勿手动修改）\n")
	// 注意：map_hash_max_size / map_hash_bucket_size 必须在 http 级、且早于任何 map 块设置，
	// 由 ensureNginxHashInclude 统一写入 /etc/nginx/lp_hash.conf 并注入 nginx.conf（见 waf.go），
	// 此处 map 块内不再重复设置（否则 nginx 报 duplicate 或在块内被忽略）。
	sb.WriteString("map $remote_addr $lp_ss_cc {\n")
	sb.WriteString("    default \"--\";\n")
	sb.WriteString("    include " + geoCountryConfPath() + ";\n")
	sb.WriteString("}\n")
	return sb.String()
}

// ensureSiteSecGlobalInclude 确保 nginx.conf 的 http 块 include 了全局单站安全配置（幂等）
func ensureSiteSecGlobalInclude() error {
	data, err := os.ReadFile(nginxConfFile)
	if err != nil {
		return err
	}
	content := string(data)
	marker := "include " + siteSecGlobalConfPath + ";"
	if strings.Contains(content, marker) {
		return nil
	}
	anchor := "include /etc/nginx/conf.d/*.conf;"
	if strings.Contains(content, anchor) {
		content = strings.Replace(content, anchor, marker+"\n\t"+anchor, 1)
	} else {
		content = strings.Replace(content, "http {", "http {\n\t"+marker, 1)
	}
	return os.WriteFile(nginxConfFile, []byte(content), 0o644)
}

// geoCountryWantedCache 上一次生成 geo_country.conf 的缓存 key（"full"=已启用禁海外，
// "off"=未启用），用于避免每次规则变动都全量扫描 xdb。
var geoCountryWantedCache string

// anySiteBlockOverseas 是否存在任意站点开启了"禁海外"（block_china 列）。
// 只要有一个站点开启，就需要生成完整的"中国 vs 海外"地图文件。
func anySiteBlockOverseas() bool {
	var n int64
	model.DB.Model(&model.SiteSecurityConfig{}).Where("block_china = ?", true).Count(&n)
	return n > 0
}

// ensureGeoCountryConf 生成 nginx map 用的"中国 CIDR 映射"文件（geo_country.conf）。
// 仅当存在站点开启"禁海外"时才生成完整地图（所有 IP 归类为"中国"或"OVERSEAS"），
// 否则写占位（map 命中默认 "--"，请求时实时判断不拦截）。生成一次后缓存到磁盘，
// 正常运行期不重复全量生成（key 不变且文件已存在则跳过）。
func ensureGeoCountryConf() {
	p := geoCountryConfPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if !IpRegionEnabled() {
		// 离线库未加载：写占位（仅注释），保证 map include 不报错；禁海外降级不生效
		_ = os.WriteFile(p, []byte("# kypanel 国家 CIDR 映射（离线库未加载，占位）\n"), 0o644)
		geoCountryWantedCache = ""
		return
	}
	needFull := anySiteBlockOverseas()
	key := "full"
	if !needFull {
		key = "off"
	}
	if key == geoCountryWantedCache {
		// key 未变且文件已存在，跳过全量扫描（占位或完整地图都无需重写）
		if _, err := os.Stat(p); err == nil {
			return
		}
	}
	geoCountryWantedCache = key
	var conf string
	var err error
	if needFull {
		conf, err = GenerateGeoCountryConf()
		if err != nil {
			slog.Warn("生成国家 CIDR 映射失败", "err", err)
			return
		}
	} else {
		conf = "# kypanel 国家 CIDR 映射（禁海外未启用，占位）\n"
	}
	if err := os.WriteFile(p, []byte(conf), 0o644); err != nil {
		slog.Warn("写入国家 CIDR 映射失败", "err", err)
	}
}

// siteSecIpBlockList 单站 IP 封禁列表（排除过期）
func siteSecIpBlockList(siteID uint) []model.SiteSecIpRule {
	return siteSecIpRules(siteID, "block")
}

// siteSecIpAllowList 单站 IP 白名单列表
func siteSecIpAllowList(siteID uint) []model.SiteSecIpRule {
	return siteSecIpRules(siteID, "allow")
}

// siteSecIpRegexAllowList 仅返回可生成 $remote_addr 正则的 allow 规则（ip/cidr/range）。
// country/isp 类型由 geo block 实时处理，不进入 IP 正则白名单（否则 siteSecIpRegex 返回空串
// 生成出 "if ($remote_addr !~* \"\")" 的死规则误判）。
func siteSecIpRegexAllowList(siteID uint) []model.SiteSecIpRule {
	all := siteSecIpAllowList(siteID)
	out := make([]model.SiteSecIpRule, 0, len(all))
	for _, r := range all {
		if r.MatchType == "cidr" || r.MatchType == "range" || r.MatchType == "ip" {
			out = append(out, r)
		}
	}
	return out
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
	// 只对 ip/cidr/range 类型生成 nginx 正则。
	if cfg.IPWhitelistEnabled {
		if allows := siteSecIpRegexAllowList(cfg.SiteID); len(allows) > 0 {
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
		}
	}

	// 2.1 禁海外实时判断（请求时由 nginx map $lp_ss_cc 映射 IP→国家，仅放行中国大陆）
	genSiteSecGeoBlock(&sb, cfg, site.Name)

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

// genSiteSecGeoBlock 生成"禁海外"实时判断规则（依赖 nginx http 级 map $lp_ss_cc）。
// 请求时由 nginx 根据 $remote_addr 实时映射国家名：仅"中国"放行，其余（含 "--" 未知）一律拦截。
// 这是对任意访问量都安全的做法：判定发生在请求路径，不随流量增长而增加 DB/配置负担。
// cfg 用于观察模式判断；siteName 用于拼 access_log 文件名。
func genSiteSecGeoBlock(sb *strings.Builder, cfg *model.SiteSecurityConfig, siteName string) {
	if !cfg.BlockOverseas {
		return
	}
	if !IpRegionEnabled() {
		// 离线库未加载时无法实时判断，降级为注释避免误杀
		sb.WriteString("# 禁海外已开启，但离线 IP 库未加载，暂未生效（请到安全中心加载 ip2region.xdb）\n")
		return
	}
	sb.WriteString("# 禁海外（实时判断 $lp_ss_cc，仅拦截确定的 OVERSEAS；中国与未知归属 IP 均放行，避免离线库不准误杀）\n")
	if cfg.Mode == "observe" {
		sb.WriteString("if ($lp_ss_cc == \"OVERSEAS\") { access_log /var/log/nginx/" + siteName + ".waf.log combined; }\n")
	} else {
		sb.WriteString("if ($lp_ss_cc == \"OVERSEAS\") { return 403; }\n")
	}
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
		case "country", "isp":
			// 国家/ISP 类型的规则不能直接拼到 nginx 的 $remote_addr 正则里（IP 地址不是国家名），
			// 否则会生成 if ($remote_addr !~* "中国") 这种永远命中/永远不命中的死规则。
			// 真实生效方式：由 runSiteSecGeoScan 维护一份动态 IP 白名单（type=ip, action=allow）。
			continue
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
	// 确保 http 级全局 map（$lp_ss_cc）+ 国家 CIDR 映射就绪（幂等，生成一次后缓存到磁盘）
	if err := applySiteSecurityGlobal(); err != nil {
		slog.Warn("初始化单站安全全局配置失败", "err", err)
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

	// 拖拽验证码片段（与单站安全片段独立，启用即写入）
	if err := writeSiteCaptchaSnippet(&site); err != nil {
		return err
	}

	// 重写站点配置（确保 include 行注入）+ 热重载
	if err := writeSiteConf(&site); err != nil {
		return err
	}
	// use_global_rules 变化会影响全局 WAF 片段，重新生成该站点的 WAF 片段
	regenerateSiteWAFSnippet(&site)
	return webReload()
}

// applySiteSecurityGlobal 生成 http 级全局 map（定义 $lp_ss_cc）+ 国家 CIDR 映射文件，
// 并幂等注入 nginx.conf 的 http 块。供 InitSiteSecurity 与每次 applySiteSecurity 调用。
func applySiteSecurityGlobal() error {
	ensureGeoCountryConf()
	if err := os.WriteFile(siteSecGlobalConfPath, []byte(genSiteSecGlobal()), 0o644); err != nil {
		return err
	}
	// 确保 http 级哈希表尺寸配置（早于本 map 块，幂等）
	if err := ensureNginxHashInclude(); err != nil {
		slog.Warn("注入 http 级哈希表尺寸配置失败", "err", err)
	}
	return ensureSiteSecGlobalInclude()
}

// writeSiteSecSnippetFile 仅重写单站安全片段文件（不 reload），供后台扫描批量更新后统一 reload。
func writeSiteSecSnippetFile(siteID uint) error {
	var site model.Site
	if err := model.DB.First(&site, siteID).Error; err != nil {
		return err
	}
	cfg := GetSiteSecurityConfig(siteID)
	path := siteSecSnippetPath(site.Name)
	conf := genSiteSecuritySnippet(&cfg, &site)
	if !cfg.Enabled || strings.TrimSpace(conf) == "" {
		return os.WriteFile(path, []byte("# 单站安全未启用\n"), 0o644)
	}
	if err := os.WriteFile(path, []byte(conf), 0o644); err != nil {
		return err
	}
	// 同步维护拖拽验证码片段（验证码可独立于单站安全启用）
	return writeSiteCaptchaSnippet(&site)
}

// purgeLegacyCountryIpRules 清理旧版本中按"国家/运营商"配置的站点 IP 规则（match_type=country/isp）。
// 该能力已移除（由"禁海外"开关统一替代），历史规则不再生效，清理避免脏数据残留。
func purgeLegacyCountryIpRules() {
	res := model.DB.Where("match_type IN ?", []string{"country", "isp"}).
		Delete(&model.SiteSecIpRule{})
	if res.Error == nil && res.RowsAffected > 0 {
		slog.Info("清理历史国家/运营商 IP 规则（已改为禁海外开关）", "count", res.RowsAffected)
	}
}

// purgeLegacyGeoIpRules 清理旧版本由后台扫描自动添加的 IP 规则（remark 以 geo 开头）。
// 新版改用 nginx map 实时判断，不再把每个访问 IP 写进规则表，故历史规则需清理。
func purgeLegacyGeoIpRules() {
	res := model.DB.Where("match_type = ? AND remark LIKE ?", "ip", "geo%").
		Delete(&model.SiteSecIpRule{})
	if res.Error == nil && res.RowsAffected > 0 {
		slog.Info("清理历史自动 IP 规则（已改为实时判断）", "count", res.RowsAffected)
	}
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

// ---- 拖拽拼图验证码（边缘层闸门）----

// captchaSnippetPath 拖拽验证码 nginx 片段路径
func captchaSnippetPath(name string) string {
	return filepath.Join(siteSecConfDir, "lp_"+name+"_captcha.conf")
}

// genSiteCaptchaSnippet 生成「拖拽验证码闸门」nginx 片段（server 级 auth_request + 挑战页/API 反代）
func genSiteCaptchaSnippet(cfg *model.SiteSecurityConfig, site *model.Site) string {
	if !cfg.CaptchaEnabled || WebServerType() == webApache {
		return ""
	}
	port := config.Get().Server.Port
	if port <= 0 {
		port = 9999
	}
	host := fmt.Sprintf("127.0.0.1:%d", port)
	// 面板可能以 HTTPS 提供（config server.https=true）。本地反向代理必须匹配协议，
	// 否则面板会返回 400 "Client sent an HTTP request to an HTTPS server"，
	// 进而 auth_request 收到非 200/401/403 的状态，把正常请求变成 500。
	// 面板用自签名证书，故关闭上游证书校验。
	scheme := "http"
	sslOpts := ""
	if bool(config.Get().Server.HTTPS) {
		scheme = "https"
		sslOpts = "    proxy_ssl_verify off;\n"
	}
	var sb strings.Builder
	sb.WriteString("# kypanel 拖拽验证码闸门（自动生成，请勿手动修改）\n")
	fmt.Fprintf(&sb, "auth_request /_lp_captcha_check;\n")
	sb.WriteString("error_page 401 = /_lp_captcha_challenge;\n\n")
	// 内部校验：交给面板判断 cookie 是否有效（仅做 HMAC，无 DB 压力）
	sb.WriteString("location = /_lp_captcha_check {\n")
	sb.WriteString("    internal;\n")
	sb.WriteString("    auth_request off;\n")
	fmt.Fprintf(&sb, "    proxy_pass %s://%s/api/site/captcha/check?site=%d;\n", scheme, host, site.ID)
	sb.WriteString("    proxy_pass_request_body off;\n")
	sb.WriteString("    proxy_set_header Content-Length \"\";\n")
	fmt.Fprintf(&sb, "    proxy_set_header Host %s;\n", host)
	sb.WriteString("    proxy_set_header X-Real-IP $remote_addr;\n")
	sb.WriteString("    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n")
	sb.WriteString(sslOpts)
	sb.WriteString("}\n\n")
	// 挑战页（面板托管，同域反代，豁免校验）。必须用【普通精确 location = /_lp_captcha_challenge】，
	// 与 error_page 401 = /_lp_captcha_challenge 内部重定向对应。注意：nginx 不允许在命名 location
	// （location @xxx）里使用带 URI 的 proxy_pass（会报 "proxy_pass cannot have URI part in named
	// location"），因此这里必须用普通精确匹配而非命名 location。
	sb.WriteString("location = /_lp_captcha_challenge {\n")
	sb.WriteString("    internal;\n")
	sb.WriteString("    auth_request off;\n")
	fmt.Fprintf(&sb, "    proxy_pass %s://%s/captcha-challenge?site=%d;\n", scheme, host, site.ID)
	fmt.Fprintf(&sb, "    proxy_set_header Host %s;\n", host)
	sb.WriteString("    proxy_set_header X-Real-IP $remote_addr;\n")
	sb.WriteString(sslOpts)
	sb.WriteString("}\n\n")
	// 验证码 API（puzzle/verify），同域反代到面板，豁免校验
	sb.WriteString("location ^~ /_lp_captcha_api/ {\n")
	sb.WriteString("    auth_request off;\n")
	fmt.Fprintf(&sb, "    proxy_pass %s://%s/api/;\n", scheme, host)
	fmt.Fprintf(&sb, "    proxy_set_header Host %s;\n", host)
	sb.WriteString("    proxy_set_header X-Real-IP $remote_addr;\n")
	sb.WriteString("    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n")
	sb.WriteString(sslOpts)
	sb.WriteString("}\n")
	return sb.String()
}

// genApacheCaptchaBlock 生成 Apache 版「拖拽验证码闸门」。
// Apache 无 auth_request，改用标准模块 mod_lua 在 access 阶段【本地】复算 HMAC-SHA256 校验放行 cookie 的签名
// （脚本见 writeApacheCaptchaFiles），从根本上杜绝旧实现「只用正则匹配 cookie 格式、不验签 → 伪造 cookie 即绕过」。
// 与 nginx 版语义一致：未通过校验（lua 返回 401）的访客请求由 ErrorDocument 内部代理到面板挑战页
// （保持原 URL，避免 302 重定向在验证后 location.reload() 死循环）；site id 经 X-Lp-Site 头传给面板挑战页。
// 需 Apache 启用 mod_lua / mod_proxy / mod_proxy_http / mod_headers（ensureApacheModules 会自动 a2enmod）。
func genApacheCaptchaBlock(siteID uint) string {
	if WebServerType() != webApache {
		return ""
	}
	cfg := GetSiteSecurityConfig(siteID)
	if !cfg.CaptchaEnabled {
		return ""
	}
	var site model.Site
	if err := model.DB.First(&site, siteID).Error; err != nil {
		return ""
	}
	port := config.Get().Server.Port
	if port <= 0 {
		port = 9999
	}
	// 面板以 HTTPS 提供时，本地反代也须走 https（自签名证书，关闭对端校验），
	// 否则面板返回 400，验证码闸门会让所有请求 500。
	scheme := "http"
	sslProxyOpts := ""
	if bool(config.Get().Server.HTTPS) {
		scheme = "https"
		sslProxyOpts = "    SSLProxyEngine on\n    SSLProxyVerify none\n    SSLProxyCheckPeerCN off\n    SSLProxyCheckPeerName off\n"
	}
	panel := fmt.Sprintf("%s://127.0.0.1:%d", scheme, port)
	var sb strings.Builder
	sb.WriteString("    # kypanel 拖拽验证码闸门（mod_lua 本地验签，自动生成，请勿手动修改）\n")
	sb.WriteString("    <IfModule mod_lua.c>\n")
	sb.WriteString(sslProxyOpts)
	fmt.Fprintf(&sb, "    LuaHookAccessChecker %s authorize\n", apacheCaptchaLuaPath(site.Name))
	// 挑战页 / 验证码 API 同域反代到面板
	fmt.Fprintf(&sb, "    ProxyPass /captcha-challenge %s/captcha-challenge\n", panel)
	fmt.Fprintf(&sb, "    ProxyPassReverse /captcha-challenge %s/captcha-challenge\n", panel)
	fmt.Fprintf(&sb, "    ProxyPass /_lp_captcha_challenge %s/captcha-challenge\n", panel)
	fmt.Fprintf(&sb, "    ProxyPassReverse /_lp_captcha_challenge %s/captcha-challenge\n", panel)
	fmt.Fprintf(&sb, "    ProxyPass /_lp_captcha_api/ %s/api/\n", panel)
	fmt.Fprintf(&sb, "    ProxyPassReverse /_lp_captcha_api/ %s/api/\n", panel)
	fmt.Fprintf(&sb, "    ProxyPass /api/site/captcha/ %s/api/site/captcha/\n", panel)
	fmt.Fprintf(&sb, "    ProxyPassReverse /api/site/captcha/ %s/api/site/captcha/\n", panel)
	// ProxyPass 反代挑战页时不带 query，用请求头把本站 site id 传给面板（面板据此注入 __LP_SITE__）
	fmt.Fprintf(&sb, "    RequestHeader set X-Lp-Site \"%d\"\n", siteID)
	// lua 判定未通过 → 401 → 内部返回挑战页（保持原 URL，验证通过后 reload 即放行）
	sb.WriteString("    ErrorDocument 401 /_lp_captcha_challenge\n")
	sb.WriteString("    </IfModule>\n")
	return sb.String()
}

// CaptchaIncludeLine 返回在站点 server 块中引入验证码片段的 include 行（仅 nginx）
func CaptchaIncludeLine(siteID uint) string {
	if WebServerType() == webApache {
		return ""
	}
	cfg := GetSiteSecurityConfig(siteID)
	if !cfg.CaptchaEnabled {
		return ""
	}
	var site model.Site
	if err := model.DB.First(&site, siteID).Error; err != nil {
		return ""
	}
	path := captchaSnippetPath(site.Name)
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return "    include " + path + ";\n"
}

// ensureCaptchaInclude 确保最终写入的站点 conf 文本中含有拖拽验证码 include 行。
// 两种情况都要覆盖：
//  1. 普通站点：genSiteConf 已通过 CaptchaIncludeLine 注入（此处检测到已存在则跳过）；
//  2. 站点存在 config_override：genSiteConf 会直接返回 override 文本，CaptchaIncludeLine
//     永远不会被调用，导致验证码 include 缺失、auth_request 不生效、访客看不到挑战页。
//     此处在每个 server { 块开头注入 include 行（放在 server 级而非 location 嵌套内，
//     避免与 location 内的 auth_request off 冲突导致 "auth_request directive is duplicate"）。
func ensureCaptchaInclude(conf string, s *model.Site) string {
	if !GetSiteSecurityConfig(s.ID).CaptchaEnabled {
		return conf
	}
	path := captchaSnippetPath(s.Name)
	if _, err := os.Stat(path); err != nil {
		return conf
	}
	inc := "    include " + path + ";\n"
	if strings.Contains(conf, inc) {
		return conf
	}
	return strings.ReplaceAll(conf, "server {", "server {\n"+inc)
}

// writeSiteCaptchaSnippet 写出/清空该站点的验证码 nginx 片段
func writeSiteCaptchaSnippet(site *model.Site) error {
	path := captchaSnippetPath(site.Name)
	cfg := GetSiteSecurityConfig(site.ID)
	conf := genSiteCaptchaSnippet(&cfg, site)
	if !cfg.CaptchaEnabled || strings.TrimSpace(conf) == "" {
		return os.WriteFile(path, []byte("# 拖拽验证码未启用\n"), 0o644)
	}
	return os.WriteFile(path, []byte(conf), 0o644)
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

	// 6. 禁海外（仅 nginx 支持：依赖 http 级 map $lp_ss_cc 实时判断国家）
	// Apache 无等效的 IP→国家内建映射，故 Apache 站点暂不支持该能力，仅注释提示。
	if cfg.BlockOverseas {
		if IpRegionEnabled() {
			sb.WriteString("    # 禁海外（Apache 暂不支持，请改用 Nginx 部署以生效；Nginx 站点已自动拦截海外 IP）\n")
		} else {
			sb.WriteString("    # 禁海外已开启，但离线 IP 库未加载，Apache 站点暂未生效\n")
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
	// 清理旧版本自动添加的 IP 规则（现已改为 nginx map 实时判断，不再扫日志加规则）
	purgeLegacyGeoIpRules()
	// 清理已移除的"国家/运营商"站点 IP 规则（由禁海外开关替代）
	purgeLegacyCountryIpRules()
	if nginxInstalledBin() {
		// 生成 http 级全局 map + 国家 CIDR 映射，并注入 nginx.conf（幂等）
		if err := applySiteSecurityGlobal(); err != nil {
			slog.Warn("初始化单站安全全局配置失败", "err", err)
		}
		// 重写所有启用站点的安全片段（移除历史自动 IP 规则）+ 统一校验热重载
		var cfgs []model.SiteSecurityConfig
		model.DB.Where("enabled = ?", true).Find(&cfgs)
		for _, c := range cfgs {
			if err := writeSiteSecSnippetFile(c.SiteID); err != nil {
				slog.Warn("重写单站安全片段失败", "site", c.SiteID, "err", err)
			}
		}
		// 重写所有启用了拖拽验证码的站点片段（验证码可独立于单站安全启用），
		// 同时重写主站点 conf（让 CaptchaIncludeLine 把 include 行注入 server 块），
		// 否则重启后 snippet 文件已就绪但站点 server 块里没有 include，
		// auth_request / error_page / 挑战页 location 全部失效，访客看不到验证码页。
		var capCfgs []model.SiteSecurityConfig
		model.DB.Where("captcha_enabled = ?", true).Find(&capCfgs)
		for _, c := range capCfgs {
			var s model.Site
			if err := model.DB.First(&s, c.SiteID).Error; err != nil {
				continue
			}
			if err := writeSiteCaptchaSnippet(&s); err != nil {
				slog.Warn("重写拖拽验证码片段失败", "site", c.SiteID, "err", err)
			}
			if WebServerType() == webNginx {
				if err := writeSiteConf(&s); err != nil {
					slog.Warn("注入拖拽验证码 include 行失败", "site", c.SiteID, "err", err)
				}
			}
		}
		if err := nginxTest(); err != nil {
			slog.Warn("nginx 配置校验失败", "err", err)
		} else {
			_ = nginxReload()
		}
	}
	go siteSecMonitorLoop()
}

// siteSecMonitorLoop 单站安全监控主循环：每 10s 扫描一次启用了单站安全的站点
func siteSecMonitorLoop() {
	for {
		time.Sleep(10 * time.Second)
		runSiteSecScan()
	}
}

// runSiteSecScan 扫描所有启用了单站安全的站点：
// 1) 清理过期 IP 规则（避免表里堆积失效数据；同时刷新受影响站点的安全片段，
//    否则 DB 删了规则但 nginx 片段里仍残留旧的 geo/IP 白名单死规则，造成误判）
// 2) CC 超频自动封禁（单 IP 频率超阈值 → 拉黑，写入 DB 后统一 reload 一次）
//
// 注意：国家/ISP 规则不再在此扫描（旧版本会把每个访问 IP 写进规则表，大流量下会撑爆
// DB 与 nginx 配置）。现改用 nginx map $lp_ss_cc 在请求时实时判断，与访问量无关。
func runSiteSecScan() {
	var cfgs []model.SiteSecurityConfig
	model.DB.Where("enabled = ?", true).Find(&cfgs)
	if len(cfgs) == 0 {
		return
	}
	now := time.Now()
	// 全局清理：所有站点的过期 IP 规则一次性扫掉，并记录哪些站点受影响以便重写片段。
	// 不用 Delete 返回值判断，因为 GORM 的 Delete 不返回受影响行数；改为按 site_id 重新查询。
	if model.DB.Where("expire_at IS NOT NULL AND expire_at <= ?", now).
		Delete(&model.SiteSecIpRule{}).Error == nil {
		// 任何站点若当前已没有任何规则（或规则集合发生变化），其 snippet 与 DB 可能不一致。
		// 重新生成所有启用站点的 snippet 以保证 nginx 看到的规则与 DB 同步。
		for _, cfg := range cfgs {
			_ = writeSiteSecSnippetFile(cfg.SiteID)
		}
		// 统一 reload
		if err := nginxTest(); err == nil {
			_ = nginxReload()
		}
	}
	changed := false
	for _, cfg := range cfgs {
		var site model.Site
		if err := model.DB.First(&site, cfg.SiteID).Error; err != nil {
			continue
		}
		if cfg.CCEnabled {
			if added := runSiteSecCCScan(&cfg, &site); added {
				changed = true
				// 浏览量大/遭 CC 攻击时，若开启「自动开启验证码」则自动拉起拖拽验证码护盾
				if cfg.CaptchaAutoOnCC && !cfg.CaptchaEnabled {
					if err := model.DB.Model(&model.SiteSecurityConfig{}).
						Where("site_id = ?", site.ID).Update("captcha_enabled", true).Error; err == nil {
						if aerr := applySiteSecurity(site.ID, true); aerr != nil {
							slog.Warn("CC 触发自动开启拖拽验证码失败", "site", site.ID, "err", aerr)
						} else {
							slog.Info("CC 攻击触发，已自动开启拖拽验证码", "site", site.ID)
						}
					}
				}
			}
		}
	}
	// 扫描结束统一热重载一次（而非每条规则 reload 一次，避免大流量下 nginx 被打死）
	if changed && nginxInstalledBin() {
		if err := nginxTest(); err == nil {
			_ = nginxReload()
		}
	}
}

// siteSecAccessLogPath 站点访问日志路径（按 Web 服务器类型）
func siteSecAccessLogPath(name string) string {
	return siteAccessLogPath(name)
}

// runSiteSecCCScan 单站 CC 频率扫描 + 自动封禁。返回是否有新增封禁（用于决定是否需要 reload）。
// 使用 noApply 变体写入 DB，由 runSiteSecScan 统一 reload 一次。
func runSiteSecCCScan(cfg *model.SiteSecurityConfig, site *model.Site) bool {
	if cfg.CCMaxRequests <= 0 || cfg.CCWindowSec <= 0 {
		return false
	}
	counters := make(map[string]int)
	blocked := map[string]bool{}
	for _, r := range siteSecIpBlockList(cfg.SiteID) {
		blocked[r.Content] = true
	}

	path := siteSecAccessLogPath(site.Name)
	f, err := os.Open(path)
	if err != nil {
		return false
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

	// 自动规则上限保护：单站 IP 黑名单达到上限则停止新增，避免 nginx 配置无限膨胀
	const maxAutoRules = 3000
	curBlock := len(siteSecIpBlockList(cfg.SiteID))
	added := 0
	for ip, cnt := range counters {
		if cnt < cfg.CCMaxRequests {
			continue
		}
		if curBlock+added >= maxAutoRules {
			slog.Warn("单站自动 IP 规则已达上限，停止新增 CC 封禁", "site", cfg.SiteID, "limit", maxAutoRules)
			break
		}
		if _, err := addSiteSecIpRuleNoApply(cfg.SiteID, "block", "ip", ip, cfg.CCBlockSec,
			fmt.Sprintf("CC 自动封禁（%ds 内 %d 次请求）", cfg.CCWindowSec, cnt)); err == nil {
			recordSiteSecLog(cfg.SiteID, ip, "cc_attack", "CC 频率限制", "block", "AUTO", "/", "")
			added++
		}
	}
	return added > 0
}

// runSiteSecGeoScan 单站 geo 扫描：按国家/ISP 规则反查 IP 归属地。
// 同时支持封禁和放行两种语义：
//   - 命中任意 block 规则 → 加入 IP 黑名单（type=ip, action=block, 永久）
//   - 命中任意 allow 规则 → 加入 IP 白名单（type=ip, action=allow, 1h TTL）
//   - 存在 allow 规则但 IP 不命中（且未被 block）→ 加入 IP 黑名单（type=ip, action=block, 10min TTL）
//     （典型场景：用户配置"放行中国" + 启用 IP 白名单模式 → 非中国 IP 应被拒绝）
//
// 注意：所有判定基于 access log 已记录到的 IP，首次访问的 IP 会在 10s 扫描窗口内被识别。
// runSiteSecGeoScan 已废弃：国家/ISP 规则现由 nginx map $lp_ss_cc 在请求时实时判断，
// 不再扫描访问日志把每个访问 IP 写进规则表（避免大流量下规则表与 nginx 配置无限膨胀）。
// 保留空实现以兼容潜在调用点。
func runSiteSecGeoScan(cfg *model.SiteSecurityConfig, site *model.Site) {
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
