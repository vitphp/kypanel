package service

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"kypanel/internal/config"
	"kypanel/internal/model"
)

// ---------- 常量 ----------

const (
	wafGlobalConfPath    = "/etc/nginx/lp_waf_global.conf" // nginx http 级 include
	wafSiteConfDir       = "/etc/nginx/waf"                // 站点级 WAF 片段（nginx）
	apacheWafConfPath    = "/etc/apache2/conf-enabled/lp_waf.conf"
	apacheWafSiteConfDir = "/etc/apache2/waf" // 站点级 WAF 片段（apache）
)

// 防护模式
const (
	WAFModeBlock   = "block"   // 拦截模式
	WAFModeObserve = "observe" // 观察模式：仅记录不拦截
)

// 设置 key
const (
	wafSettingEnabled = "waf_enabled"
	wafSettingMode    = "waf_mode"
	wafSettingApache  = "waf_apache"
)

// 运行状态缓存
var (
	wafStatusMu sync.RWMutex
	wafEnabled  bool
	wafMode     string
	wafApache   bool
)

// ---------- 初始化 ----------

// InitWAF 初始化 WAF：加载设置、写入内置规则、生成全局配置、启动 CC 防护协程
func InitWAF() {
	loadWAFSetting()
	seedWAFBuiltinRules()
	_ = applyWAFConfig(false)
	go ccMonitorLoop()
}

func loadWAFSetting() {
	wafEnabled = getWAFSettingBool(wafSettingEnabled)
	wafMode = getWAFSetting(wafSettingMode)
	if wafMode == "" {
		wafMode = WAFModeBlock
	}
	wafApache = getWAFSettingBool(wafSettingApache)
}

func getWAFSetting(key string) string {
	s, ok := model.GetWAFSetting(key)
	if !ok {
		return ""
	}
	return s.Value
}

func getWAFSettingBool(key string) bool {
	return strings.TrimSpace(getWAFSetting(key)) == "true"
}

func setWAFSetting(key, value string) error {
	s, ok := model.GetWAFSetting(key)
	if !ok {
		s = &model.WAFSetting{Key: key}
	}
	s.Value = value
	return model.SaveWAFSetting(s)
}

// ---------- 内置规则库 ----------

type builtinWAFRule struct {
	category   string
	categoryCN string
	name       string
	pattern    string
	matchField string
	remark     string
}

// builtinWAFRules 内置攻击规则集（10 类）
var builtinWAFRules = []builtinWAFRule{
	// SQL 注入
	{"sql_inject", "SQL注入", "SQL 注入-联合查询", `(?i)(union[\s\S]+?select\s+[\S\s]+?from)`, "uri", "union select 联合查询注入"},
	{"sql_inject", "SQL注入", "SQL 注入-常见函数", `(?i)(sleep\s*\(\s*\d|updatexml\s*\(|extractvalue\s*\(|benchmark\s*\(|load_file\s*\()`, "uri", "mysql 高危函数注入"},
	{"sql_inject", "SQL注入", "SQL 注入-恒真/恒假", `(?i)(\b1\s*=\s*1\b|\b1\s*=\s*2\b|\b0\b\s*=\s*\b0\b)`, "uri", "经典 or 1=1 注入"},
	{"sql_inject", "SQL注入", "SQL 注入-注释绕过", `(?i)(--\s|/\*!|#.*\bor\b|/\*.*\*/)`, "uri", "注释符绕过检测"},
	{"sql_inject", "SQL注入", "SQL 注入-报错注入", `(?i)(\(select.+from\s+information_schema|group\s+by\s+\d+\s+with\s+rollup|floor\(rand\(0\)\*2\))`, "uri", "报错注入特征"},
	{"sql_inject", "SQL注入", "SQL 注入-分号堆叠", `(?i)(;\s*(select|insert|update|delete|drop|truncate|alter)\s+[\S])`, "uri", "堆叠注入"},
	// XSS
	{"xss", "XSS跨站", "XSS-脚本标签", `(?i)(<script[^>]*>|</script>)`, "uri", "script 标签"},
	{"xss", "XSS跨站", "XSS-事件处理", `(?i)(\bon\w+\s*=\s*['"]?javascript:|<img[^>]+onerror)`, "uri", "onerror/onclick 等事件属性"},
	{"xss", "XSS跨站", "XSS-协议注入", `(?i)(javascript\s*:|vbscript\s*:|data\s*:\s*text/html)`, "uri", "危险协议"},
	{"xss", "XSS跨站", "XSS-iframe/object", `(?i)(<iframe[^>]*>|<object[^>]*>|<embed[^>]*>)`, "uri", "iframe/object 注入"},
	{"xss", "XSS跨站", "XSS-document 操作", `(?i)(document\.(cookie|location|write)|window\.location\s*=|alert\s*\(|prompt\s*\(|confirm\s*\()`, "uri", "DOM 操作注入"},
	// 命令注入
	{"cmd_inject", "命令注入", "命令注入-管道符", `(?i)(\|\s*(cat|ls|id|whoami|rm|nc|wget|curl|bash|sh|python|perl|php)\b)`, "uri", "| cat 等管道执行"},
	{"cmd_inject", "命令注入", "命令注入-分号", `(?i)(;\s*(cat|ls|id|whoami|rm|nc|wget|curl|bash|sh|python|perl|php)\b)`, "uri", "分号拼接命令"},
	{"cmd_inject", "命令注入", "命令注入-$()", `(?i)(\$\((cat|ls|id|whoami|rm|nc|wget|curl|bash|sh|python|perl|php)\b)`, "uri", "$() 命令替换"},
	{"cmd_inject", "命令注入", "命令注入-反引号", "`(cat|ls|id|whoami|rm|nc|wget|curl|bash|sh|python|perl|php)", "uri", "反引号命令替换"},
	{"cmd_inject", "命令注入", "命令注入-系统命令", `(?i)(\b(wget|curl)\s+[\S]+\s+-o\b|\bnc\s+-e\b|\b/bin/(sh|bash)\b)`, "uri", "下载执行/反弹 shell"},
	// 路径穿越
	{"path_traversal", "路径穿越", "路径穿越-../", `(?i)(\.\.(/|%2f|%2F|%5c|\\|%252e%252e)+)`, "uri", "经典 ../ 穿越"},
	{"path_traversal", "路径穿越", "路径穿越-编码绕过", `(?i)(%2e%2e%2f|%252e%252e%252f|\.\.%5c)`, "uri", "URL 编码绕过"},
	{"path_traversal", "路径穿越", "路径穿越-绝对路径", `(?i)(file:///etc/passwd|/etc/passwd\b|C:\\windows\\win\.ini)`, "uri", "读取敏感系统文件"},
	// 文件包含
	{"file_include", "文件包含", "文件包含-本地", `(?i)(include\s*=\s*\.\.|php://(filter|input|zip))`, "uri", "php 流包装器包含"},
	{"file_include", "文件包含", "文件包含-远程", `(?i)((include|require|include_once|require_once)\s*=\s*https?://)`, "uri", "远程文件包含"},
	{"file_include", "文件包含", "文件包含-日志", `(?i)(/var/log/(apache2?|nginx|php-fpm)/)`, "uri", "日志注入包含"},
	// 代码执行
	{"code_exec", "代码执行", "代码执行-eval家族", `(?i)(\beval\s*\(|\bassert\s*\(|\bcreate_function\s*\(|\bcall_user_func\s*\()`, "uri", "PHP 危险函数"},
	{"code_exec", "代码执行", "代码执行-系统函数", `(?i)(\b(system|shell_exec|passthru|exec|popen|proc_open)\s*\()`, "uri", "PHP 系统命令执行函数"},
	{"code_exec", "代码执行", "代码执行-序列化", `(?i)(O:\d+:"|a:\d+:\{|s:\d+")`, "uri", "PHP 反序列化特征"},
	// 恶意扫描器
	{"scanner", "恶意扫描器", "扫描器-常见工具", `(?i)(sqlmap|nikto|nmap|nessus|acunetix|w3af|hydra|metasploit|burpsuite)`, "uri", "常见扫描工具特征"},
	{"scanner", "恶意扫描器", "扫描器-敏感路径探测", `(?i)(/(phpmyadmin|adminer|wp-admin|\.git/|\.svn/|\.env|\.DS_Store|backup|bak|dump\.sql))`, "uri", "敏感路径探测"},
	{"scanner", "恶意扫描器", "扫描器-漏洞探测", `(?i)(\?id=\d+\s+('|--)|\.jsp\s*$|\.action\s*$)`, "uri", "漏洞扫描请求特征"},
	// 敏感文件
	{"sensitive_file", "敏感文件", "敏感文件-备份", `(?i)(\.(bak|old|backup|swp|sql|tar|gz|zip|rar)\s*$)`, "uri", "备份文件泄露"},
	{"sensitive_file", "敏感文件", "敏感文件-配置", `(?i)(/(\.env|web\.config|\.htaccess|\.user\.ini|config\.php|database\.yml)\b)`, "uri", "配置文件泄露"},
	// WebShell
	{"webshell", "WebShell", "WebShell-一句话", `(?i)(eval\s*\(\s*\$_(POST|GET|REQUEST)|assert\s*\(\s*\$_(POST|GET|REQUEST)|@\s*eval\s*\(|system\s*\(\s*\$_(POST|GET|REQUEST))`, "uri", "经典一句话木马"},
	{"webshell", "WebShell", "WebShell-变形", `(?i)(\$_REQUEST\[[\w]+'?\]\s*\(|create_function\s*\(['"][^'"]*['"],\s*\$_)`, "uri", "变形木马"},
	// 恶意 UA
	{"bad_ua", "恶意UA", "恶意UA-扫描器", `(?i)(sqlmap|nikto|nmap|nessus|acunetix|w3af|masscan|zgrab|python-requests|scrapy)`, "ua", "扫描器/爬虫 UA"},
	{"bad_ua", "恶意UA", "恶意UA-工具", `(?i)(libwww-perl|Go-http-client|java/|okhttp|httpclient)`, "ua", "脚本工具 UA"},
}

// seedWAFBuiltinRules 内置规则写入 DB（已存在则跳过）
func seedWAFBuiltinRules() {
	count, _ := model.CountBuiltinWAFRules()
	if count > 0 {
		return
	}
	for _, r := range builtinWAFRules {
		_ = model.CreateWAFRule(&model.WAFRule{
			Category:   r.category,
			CategoryCN: r.categoryCN,
			Name:       r.name,
			Pattern:    r.pattern,
			MatchField: r.matchField,
			Action:     "block",
			Enabled:    true,
			Builtin:    true,
			Remark:     r.remark,
		})
	}
}

// ---------- 规则库 CRUD ----------

// ListWAFRules 规则列表
func ListWAFRules() []model.WAFRule {
	rules, _ := model.ListWAFRulesOrdered("id asc")
	return rules
}

type UpdateWAFRuleReq struct {
	Enabled *bool  `json:"enabled"`
	Action  string `json:"action"` // block / log
	Pattern string `json:"pattern"`
}

// UpdateWAFRule 更新规则（启停/动作/内容）
func UpdateWAFRule(id uint, req UpdateWAFRuleReq) error {
	rule, err := model.GetWAFRule(id)
	if err != nil {
		return errors.New("规则不存在")
	}
	if req.Enabled != nil {
		rule.Enabled = *req.Enabled
	}
	if req.Action != "" {
		rule.Action = req.Action
	}
	if req.Pattern != "" {
		if _, err := regexp.Compile(req.Pattern); err != nil {
			return errors.New("正则表达式不合法: " + err.Error())
		}
		rule.Pattern = req.Pattern
	}
	rule.UpdatedAt = time.Now()
	if err := model.SaveWAFRule(rule); err != nil {
		return err
	}
	return applyWAFConfig(true)
}

type CreateWAFRuleReq struct {
	Name       string `json:"name"`
	Pattern    string `json:"pattern" binding:"required"`
	MatchField string `json:"match_field"` // uri / args / ua / referer / header / body
	Action     string `json:"action"`      // block / log
	Priority   int    `json:"priority"`
	Remark     string `json:"remark"`
}

// CreateWAFRule 新增自定义规则
func CreateWAFRule(req CreateWAFRuleReq) error {
	if strings.TrimSpace(req.Pattern) == "" {
		return errors.New("规则内容不能为空")
	}
	if _, err := regexp.Compile(req.Pattern); err != nil {
		return errors.New("正则表达式不合法: " + err.Error())
	}
	if req.MatchField == "" {
		req.MatchField = "uri"
	}
	if req.Action == "" {
		req.Action = "block"
	}
	rule := model.WAFRule{
		Category:   "custom",
		CategoryCN: "自定义规则",
		Name:       req.Name,
		Pattern:    req.Pattern,
		MatchField: req.MatchField,
		Action:     req.Action,
		Enabled:    true,
		Builtin:    false,
		Priority:   req.Priority,
		Remark:     req.Remark,
	}
	if rule.Name == "" {
		rule.Name = "自定义规则"
	}
	// Select 强制写入 builtin=false 等零值字段（避免被 gorm default 值覆盖）
	if err := model.CreateWAFRuleWithFields(&rule); err != nil {
		return err
	}
	return applyWAFConfig(true)
}

// DeleteWAFRule 删除规则（内置规则不允许删除）
func DeleteWAFRule(id uint) error {
	rule, err := model.GetWAFRule(id)
	if err != nil {
		return errors.New("规则不存在")
	}
	if rule.Builtin {
		return errors.New("内置规则不允许删除，可停用")
	}
	if err := model.DeleteWAFRule(rule); err != nil {
		return err
	}
	return applyWAFConfig(true)
}

// ResetWAFRules 恢复内置规则（删除全部重新写入）
func ResetWAFRules() error {
	if err := model.DeleteBuiltinWAFRules(); err != nil {
		return err
	}
	seedWAFBuiltinRules()
	return applyWAFConfig(true)
}

// ---------- 黑白名单 ----------

// ListWAFIpRules 黑白名单列表
func ListWAFIpRules() []model.WAFIpRule {
	rules, _ := model.ListAllWAFIpRules()
	return rules
}

type WAFIpRuleReq struct {
	Type          string `json:"type" binding:"required,oneof=ip url ua"`
	Action        string `json:"action" binding:"required,oneof=allow block"`
	Content       string `json:"content" binding:"required"`
	ExpireSeconds int    `json:"expire_seconds"`
	Remark        string `json:"remark"`
}

// CreateWAFIpRule 新增黑白名单
func CreateWAFIpRule(req WAFIpRuleReq) error {
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return errors.New("内容不能为空")
	}
	if req.Type == "ip" {
		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if _, _, err := net.ParseCIDR(line); err != nil {
				if net.ParseIP(line) == nil {
					return errors.New("IP 格式不合法: " + line)
				}
			}
		}
	}
	if req.Type == "url" {
		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if _, ok := wafURLLocation(line); !ok {
				return errors.New("URL 必须以 / 开头，且不能包含空格、引号、花括号、反斜杠、分号等特殊字符: " + line)
			}
		}
	}
	var expireAt *time.Time
	if req.ExpireSeconds > 0 {
		t := time.Now().Add(time.Duration(req.ExpireSeconds) * time.Second)
		expireAt = &t
	}
	rule := model.WAFIpRule{
		Type:     req.Type,
		Action:   req.Action,
		Content:  content,
		ExpireAt: expireAt,
		Remark:   req.Remark,
	}
	if err := model.CreateWAFIpRule(&rule); err != nil {
		return err
	}
	return applyWAFConfig(true)
}

// DeleteWAFIpRule 删除黑白名单
func DeleteWAFIpRule(id uint) error {
	if err := model.DeleteWAFIpRule(id); err != nil {
		return err
	}
	return applyWAFConfig(true)
}

// ---------- CC 防护配置 ----------

// GetWAFCcConfig 读取 CC 配置（不存在时返回默认值）
func GetWAFCcConfig() model.WAFCcConfig {
	cfg, err := model.GetWAFCcConfig()
	if err != nil {
		return model.WAFCcConfig{
			Enabled:      false,
			MaxRequests:  100,
			WindowSec:    10,
			BlockSec:     3600,
			MaxConnPerIP: 50,
		}
	}
	return *cfg
}

type WAFCcConfigReq struct {
	Enabled       bool   `json:"enabled"`
	MaxRequests   int    `json:"max_requests"`
	WindowSec     int    `json:"window_sec"`
	BlockSec      int    `json:"block_sec"`
	MaxConnPerIP  int    `json:"max_conn_per_ip"`
	IncludeStatic bool   `json:"include_static"`
	Whitelist     string `json:"whitelist"`
}

// SaveWAFCcConfig 保存 CC 配置
func SaveWAFCcConfig(req WAFCcConfigReq) error {
	cfg := GetWAFCcConfig()
	cfg.Enabled = req.Enabled
	if req.MaxRequests > 0 {
		cfg.MaxRequests = req.MaxRequests
	}
	if req.WindowSec > 0 {
		cfg.WindowSec = req.WindowSec
	}
	if req.BlockSec > 0 {
		cfg.BlockSec = req.BlockSec
	}
	if req.MaxConnPerIP >= 0 {
		cfg.MaxConnPerIP = req.MaxConnPerIP
	}
	cfg.IncludeStatic = req.IncludeStatic
	cfg.Whitelist = req.Whitelist
	cfg.UpdatedAt = time.Now()
	return model.SaveWAFCcConfig(&cfg)
}

// ---------- 攻击日志 ----------

// ListWAFAttackLogs 攻击日志分页查询
func ListWAFAttackLogs(page, pageSize int, keyword string) ([]model.WAFAttackLog, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	q := model.DB.Model(&model.WAFAttackLog{})
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("ip LIKE ? OR uri LIKE ? OR rule_name LIKE ? OR site LIKE ?", like, like, like, like)
	}
	var total int64
	q.Count(&total)
	var logs []model.WAFAttackLog
	q.Order("time desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&logs)
	return logs, total, nil
}

// ClearWAFAttackLogs 清空攻击日志
func ClearWAFAttackLogs() error {
	return model.ClearWAFAttackLogs()
}

// recordWAFAttack 记录一条攻击日志
func recordWAFAttack(ip, site, category, categoryCN, ruleName, action, method, uri, ua string) {
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
	log := model.WAFAttackLog{
		Time:       time.Now(),
		IP:         ip,
		Region:     region,
		Site:       site,
		Category:   category,
		CategoryCN: categoryCN,
		RuleName:   ruleName,
		Action:     action,
		Method:     method,
		URI:        uri,
		UA:         ua,
	}
	_ = model.CreateWAFAttackLog(&log)
	// 日志最多保留 50000 条，超出删除最旧的
	count, _ := model.CountWAFAttackLogs(nil)
	if count > 50000 {
		first, err := model.FirstWAFAttackLogOrdered("time asc")
		if err == nil && first.ID > 0 {
			_ = model.DeleteWAFAttackLogsBefore(first.ID)
		}
	}
}

// ---------- 总览 ----------

// WAFOverview WAF 总览数据
type WAFOverview struct {
	Enabled          bool     `json:"enabled"`
	Mode             string   `json:"mode"`
	Apache           bool     `json:"apache"`
	NginxInstalled   bool     `json:"nginx_installed"`
	ApacheInstalled  bool     `json:"apache_installed"`
	RuleCount        int64    `json:"rule_count"`
	EnabledRuleCount int64    `json:"enabled_rule_count"`
	BlockRuleCount   int64    `json:"block_rule_count"`
	CustomRuleCount  int64    `json:"custom_rule_count"`
	IpBlackCount     int64    `json:"ip_black_count"`
	IpWhiteCount     int64    `json:"ip_white_count"`
	TodayBlock       int64    `json:"today_block"`
	TodayLog         int64    `json:"today_log"`
	TotalBlock       int64    `json:"total_block"`
	CurrentBans      []string `json:"current_bans"`
	CcEnabled        bool     `json:"cc_enabled"`
}

// GetWAFOverview 返回 WAF 总览数据
func GetWAFOverview() WAFOverview {
	o := WAFOverview{
		Enabled:         wafEnabled,
		Mode:            wafMode,
		Apache:          wafApache,
		NginxInstalled:  nginxInstalledBin(),
		ApacheInstalled: apacheInstalledBin(),
		CcEnabled:       GetWAFCcConfig().Enabled,
		CurrentBans:     []string{}, // 保证不为 null（前端直接 .length）
	}
	model.DB.Model(&model.WAFRule{}).Count(&o.RuleCount)
	model.DB.Model(&model.WAFRule{}).Where("enabled = ?", true).Count(&o.EnabledRuleCount)
	model.DB.Model(&model.WAFRule{}).Where("action = ? AND enabled = ?", "block", true).Count(&o.BlockRuleCount)
	model.DB.Model(&model.WAFRule{}).Where("builtin = ?", false).Count(&o.CustomRuleCount)
	model.DB.Model(&model.WAFIpRule{}).Where("type = ? AND action = ?", "ip", "block").Count(&o.IpBlackCount)
	model.DB.Model(&model.WAFIpRule{}).Where("type = ? AND action = ?", "ip", "allow").Count(&o.IpWhiteCount)

	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	model.DB.Model(&model.WAFAttackLog{}).Where("time >= ? AND action = ?", startOfDay, "block").Count(&o.TodayBlock)
	model.DB.Model(&model.WAFAttackLog{}).Where("time >= ?", startOfDay).Count(&o.TodayLog)
	model.DB.Model(&model.WAFAttackLog{}).Where("action = ?", "block").Count(&o.TotalBlock)

	var rules []model.WAFIpRule
	model.DB.Where("type = ? AND action = ?", "ip", "block").Find(&rules)
	for _, r := range rules {
		for _, line := range strings.Split(r.Content, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				o.CurrentBans = append(o.CurrentBans, line)
			}
		}
	}
	return o
}

// ---------- 开关与模式 ----------

// SaveWAFSetting 保存 WAF 开关/模式
func SaveWAFSetting(req WAFSettingReq) error {
	if req.Mode != "" && req.Mode != WAFModeBlock && req.Mode != WAFModeObserve {
		return errors.New("防护模式不合法")
	}
	if req.Enabled != nil {
		if err := setWAFSetting(wafSettingEnabled, strconv.FormatBool(*req.Enabled)); err != nil {
			return err
		}
	}
	if req.Mode != "" {
		if err := setWAFSetting(wafSettingMode, req.Mode); err != nil {
			return err
		}
	}
	if req.Apache != nil {
		if err := setWAFSetting(wafSettingApache, strconv.FormatBool(*req.Apache)); err != nil {
			return err
		}
	}
	loadWAFSetting()
	return applyWAFConfig(true)
}

type WAFSettingReq struct {
	Enabled *bool  `json:"enabled"`
	Mode    string `json:"mode"`
	Apache  *bool  `json:"apache"`
}

// ---------- Nginx / Apache 检测 ----------

func nginxInstalledBin() bool {
	if _, err := exec.LookPath("nginx"); err != nil {
		return false
	}
	return true
}

func apacheInstalledBin() bool {
	if _, err := exec.LookPath("apache2"); err != nil {
		if _, err2 := exec.LookPath("httpd"); err2 != nil {
			return false
		}
	}
	return true
}

// ---------- 配置生成 ----------

// enabledWAFRules 返回启用的规则列表（按匹配字段分组）
func enabledWAFRules() []model.WAFRule {
	rules, _ := model.ListEnabledWAFRules()
	return rules
}

// wafIpRuleContentList 按 type+action 查询黑白名单，并把每条规则的 Content 按行拆分返回
func wafIpRuleContentList(ruleType, action string) []string {
	rules, _ := model.ListWAFIpRules(ruleType, action)
	var out []string
	for _, r := range rules {
		for _, line := range strings.Split(r.Content, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				out = append(out, line)
			}
		}
	}
	return out
}

// wafIpBlockList 当前生效的 IP 封禁（不含过期）
func wafIpBlockList() []string {
	now := time.Now()
	rules, _ := model.ListWAFIpRules("ip", "block")
	var out []string
	for _, r := range rules {
		if r.ExpireAt != nil && r.ExpireAt.Before(now) {
			continue
		}
		for _, line := range strings.Split(r.Content, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				out = append(out, line)
			}
		}
	}
	return out
}

// wafIpAllowList 当前生效的 IP 白名单
func wafIpAllowList() []string {
	return wafIpRuleContentList("ip", "allow")
}

// wafUrlBlockList / wafUrlAllowList URL 黑白名单（用于生成 nginx location 规则）
func wafUrlBlockList() []string {
	return wafIpRuleContentList("url", "block")
}

func wafUrlAllowList() []string {
	return wafIpRuleContentList("url", "allow")
}

func wafUaBlockList() []string {
	return wafIpRuleContentList("ua", "block")
}

// wafCcWhitelist CC 白名单 IP
func wafCcWhitelist() []string {
	cfg := GetWAFCcConfig()
	var out []string
	for _, line := range strings.Split(cfg.Whitelist, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// purgeExpiredBans 清理过期封禁，返回是否有变更
func purgeExpiredBans() bool {
	rules, _ := model.ListExpiredWAFIpRules("ip", "block", time.Now())
	if len(rules) == 0 {
		return false
	}
	ids := make([]uint, 0, len(rules))
	for _, r := range rules {
		ids = append(ids, r.ID)
	}
	_ = model.DeleteWAFIpRulesByIDs(ids)
	return true
}

// applyWAFConfig 生成全局 WAF 配置（Nginx http 级 + Apache）并校验热重载。
// force=true 时无论是否有变更都重新生成；失败时返回错误但保留旧配置。
func applyWAFConfig(force bool) error {
	wafStatusMu.RLock()
	enabled := wafEnabled
	wafStatusMu.RUnlock()

	if !enabled {
		// 关闭 WAF：删除全局配置；站点配置中的 include 行保留，
		// 因此把每个站点的 WAF 片段文件写成空内容（include 空文件不报错）
		_ = os.Remove(wafGlobalConfPath)
		_ = removeNginxWAFInclude()
		_ = os.RemoveAll(wafSiteConfDir)
		_ = os.MkdirAll(wafSiteConfDir, 0o755)
		for _, item := range ListSites() {
			path := filepath.Join(wafSiteConfDir, "lp_"+item.Name+".conf")
			_ = os.WriteFile(path, []byte("# WAF disabled by kypanel\n"), 0o644)
		}
		// Apache 站点 WAF 片段目录同样清空（写占位）
		_ = os.RemoveAll(apacheWafSiteConfDir)
		_ = os.MkdirAll(apacheWafSiteConfDir, 0o755)
		for _, item := range ListSites() {
			path := filepath.Join(apacheWafSiteConfDir, "lp_"+item.Name+".conf")
			_ = os.WriteFile(path, []byte("# WAF disabled by kypanel\n"), 0o644)
		}
		removeApacheWAFConf()
		if nginxInstalledBin() {
			_ = nginxTest()
			_ = nginxReload()
		}
		if apacheInstalledBin() {
			_ = webReload()
		}
		return nil
	}

	if err := os.MkdirAll(wafSiteConfDir, 0o755); err != nil {
		return err
	}

	// 1. 生成 Nginx http 级全局配置（limit_req_zone + IP 封禁 map + CC 限速）
	globalConf := genNginxWAFGlobal()
	if err := os.WriteFile(wafGlobalConfPath, []byte(globalConf), 0o644); err != nil {
		return err
	}
	// 确保 nginx.conf http 块 include 了全局 WAF 配置（幂等）
	if err := ensureNginxWAFInclude(); err != nil {
		return err
	}

	// 2. 为每个站点生成 WAF 片段
	sites := ListSites()
	for _, item := range sites {
		conf := genSiteWAFSnippet(&item.Site)
		path := filepath.Join(wafSiteConfDir, "lp_"+item.Name+".conf")
		if strings.TrimSpace(conf) == "" {
			_ = os.Remove(path)
			continue
		}
		if err := os.WriteFile(path, []byte(conf), 0o644); err != nil {
			return err
		}
	}

	// 2.1 重写每个站点配置，注入 WAF include 行（use_global_rules=false 的站点自动跳过）
	for _, item := range sites {
		if nginxInstalledBin() {
			_ = writeSiteConf(&item.Site)
		}
	}

	// 3. Apache 规则
	if wafApache && apacheInstalledBin() {
		if err := writeApacheWAFConf(); err != nil {
			slog.Warn("写入 Apache WAF 配置失败", "err", err)
		}
	} else {
		removeApacheWAFConf()
	}

	// 4. 校验并重载（nginx 未安装时跳过，配置已写入磁盘，装好后自动生效）
	if !nginxInstalledBin() {
		return nil
	}
	if err := nginxTest(); err != nil {
		return err
	}
	if err := nginxReload(); err != nil {
		return err
	}
	return nil
}

// ApplyWAFConfig 重新生成并应用 WAF 配置（供路由调用）
func ApplyWAFConfig() error {
	return applyWAFConfig(true)
}

// ensureNginxWAFInclude 确保 nginx.conf 的 http 块 include 了全局 WAF 配置（幂等）
func ensureNginxWAFInclude() error {
	data, err := os.ReadFile(nginxConfFile)
	if err != nil {
		return err
	}
	content := string(data)
	marker := "include " + wafGlobalConfPath + ";"
	if strings.Contains(content, marker) {
		return nil // 已 include，无需处理
	}
	// 在 http 块的 conf.d include 前插入（找不到则追加到文件末尾 http 块外不行，必须 http 内）
	anchor := "include /etc/nginx/conf.d/*.conf;"
	if strings.Contains(content, anchor) {
		content = strings.Replace(content, anchor, marker+"\n\t"+anchor, 1)
	} else {
		// 兜底：在 "http {" 之后插入
		content = strings.Replace(content, "http {", "http {\n\t"+marker, 1)
	}
	return os.WriteFile(nginxConfFile, []byte(content), 0o644)
}

// removeNginxWAFInclude 从 nginx.conf 移除全局 WAF include 行（关闭 WAF 时调用）
func removeNginxWAFInclude() error {
	data, err := os.ReadFile(nginxConfFile)
	if err != nil {
		return err
	}
	content := string(data)
	marker := "include " + wafGlobalConfPath + ";"
	if !strings.Contains(content, marker) {
		return nil
	}
	content = strings.Replace(content, "\n\t"+marker, "", 1)
	content = strings.Replace(content, "\n"+marker, "", 1)
	content = strings.Replace(content, marker+"\n", "", 1)
	return os.WriteFile(nginxConfFile, []byte(content), 0o644)
}

// genNginxWAFGlobal 生成 nginx http 级 WAF 配置
func genNginxWAFGlobal() string {
	var sb strings.Builder
	sb.WriteString("# kypanel WAF 全局配置（自动生成，请勿手动修改）\n")

	// IP 封禁 map（map 指令需要在 http 级定义）
	blocks := wafIpBlockList()
	allows := wafIpAllowList()
	if len(blocks) > 0 || len(allows) > 0 {
		sb.WriteString("map $remote_addr $lp_waf_deny {\n")
		sb.WriteString("    default 0;\n")
		for _, allow := range allows {
			fmt.Fprintf(&sb, "    %s 0;\n", allow)
		}
		for _, block := range blocks {
			fmt.Fprintf(&sb, "    %s 1;\n", block)
		}
		sb.WriteString("}\n")
	} else {
		// 空 map 兜底，避免 include 时报错
		sb.WriteString("map $remote_addr $lp_waf_deny {\n")
		sb.WriteString("    default 0;\n")
		sb.WriteString("}\n")
	}

	// CC 防护 limit_req_zone（http 级）
	cfg := GetWAFCcConfig()
	if cfg.Enabled && cfg.MaxRequests > 0 && cfg.WindowSec > 0 {
		rate := float64(cfg.MaxRequests) / float64(cfg.WindowSec)
		fmt.Fprintf(&sb, "limit_req_zone $binary_remote_addr zone=lp_waf_cc:20m rate=%.2fr/s;\n", rate)
	}
	return sb.String()
}

// wafSiteSkipGlobal 判断某站点是否跳过全局 WAF（单站安全配置 UseGlobalRules=false）
func wafSiteSkipGlobal(siteID uint) bool {
	cfg := GetSiteSecurityConfig(siteID)
	return !cfg.UseGlobalRules
}

// genSiteWAFSnippet 生成单个站点的 WAF server 级片段
func genSiteWAFSnippet(s *model.Site) string {
	if !wafEnabled {
		return ""
	}
	// 站点设置了「不使用全局规则」时跳过全局 WAF
	if wafSiteSkipGlobal(s.ID) {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("# kypanel WAF 站点规则（自动生成）\n")

	// IP 封禁 / 白名单（server 级直接判断，观察模式同样拦截已明确封禁的 IP）
	sb.WriteString("if ($lp_waf_deny = 1) {\n")
	sb.WriteString("    return 403;\n")
	sb.WriteString("}\n")

	// CC 限速（server 级）
	cfg := GetWAFCcConfig()
	if cfg.Enabled && cfg.MaxRequests > 0 && cfg.WindowSec > 0 {
		fmt.Fprintf(&sb, "limit_req zone=lp_waf_cc burst=%d nodelay;\n", cfg.MaxRequests*2)
		fmt.Fprintf(&sb, "limit_req_status 429;\n")
	}

	// 攻击规则
	rules := enabledWAFRules()
	uriRules := make([]string, 0)
	uaRules := make([]string, 0)
	for _, r := range rules {
		switch r.MatchField {
		case "ua":
			uaRules = append(uaRules, r.Pattern)
		default: // uri / args / referer / header / body 统一匹配 $request_uri
			uriRules = append(uriRules, r.Pattern)
		}
	}

	// URI 规则合并为一个 if（提高性能）
	if len(uriRules) > 0 {
		joined := "(?i)(" + strings.Join(uriRules, "|") + ")"
		sb.WriteString("if ($request_uri ~* \"" + nginxEscape(joined) + "\") {\n")
		if wafMode == WAFModeObserve {
			sb.WriteString("    # 观察模式：仅记录\n")
			sb.WriteString("    access_log /var/log/nginx/lp_waf_observe.log combined;\n")
		} else {
			sb.WriteString("    return 403;\n")
		}
		sb.WriteString("}\n")
	}

	// UA 规则
	if len(uaRules) > 0 {
		joined := "(?i)(" + strings.Join(uaRules, "|") + ")"
		sb.WriteString("if ($http_user_agent ~* \"" + nginxEscape(joined) + "\") {\n")
		if wafMode == WAFModeObserve {
			sb.WriteString("    # 观察模式：仅记录\n")
		} else {
			sb.WriteString("    return 403;\n")
		}
		sb.WriteString("}\n")
	}

	// URL 黑白名单
	for _, url := range wafUrlBlockList() {
		if esc, ok := wafURLLocation(url); ok {
			fmt.Fprintf(&sb, "location = %s { return 403; }\n", esc)
		}
	}
	for _, url := range wafUrlAllowList() {
		if esc, ok := wafURLLocation(url); ok {
			fmt.Fprintf(&sb, "location = %s { return 200; }\n", esc)
		}
	}
	return sb.String()
}

// wafURLLocation 校验并转义 URL 黑白名单条目，返回可安全写入 nginx location 块的值。
// 合法 URL 必须以 / 开头，且不含会破坏 nginx 配置的字符（空白、引号、花括号、反斜杠、分号、# 等）。
// ok=false 表示条目非法，调用方应跳过该条（宁可规则不生效，不能破坏站点配置）。
func wafURLLocation(url string) (string, bool) {
	url = strings.TrimSpace(url)
	if url == "" || !strings.HasPrefix(url, "/") {
		return "", false
	}
	for i := 0; i < len(url); i++ {
		c := url[i]
		if c <= ' ' || c == '"' || c == '\'' || c == '{' || c == '}' || c == '\\' || c == ';' || c == '#' {
			return "", false
		}
	}
	return nginxEscape(url), true
}

// nginxEscape 转义 nginx if 匹配中的双引号与反斜杠
func nginxEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// genSiteConfWAFInclude 返回 server 块内 WAF include 片段
// 在 genSiteServerBlock 中被调用，追加到 server 块末尾。
func genSiteConfWAFInclude(name string) string {
	if !wafEnabled {
		return ""
	}
	return fmt.Sprintf("    include %s;\n", filepath.Join(wafSiteConfDir, "lp_"+name+".conf"))
}

// ---------- Apache ----------

// genApacheSiteWAFSnippet 生成单个 Apache 站点的 WAF 片段（RewriteRule/RewriteCond 拦截）
// 片段会被 include 到该站点的 <VirtualHost> 内。
func genApacheSiteWAFSnippet(s *model.Site) string {
	if !wafEnabled {
		return ""
	}
	// 站点设置了「不使用全局规则」时跳过全局 WAF
	if wafSiteSkipGlobal(s.ID) {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("# kypanel WAF Apache 站点规则（自动生成）\n")
	sb.WriteString("RewriteEngine On\n")

	// IP 封禁 / 白名单
	for _, ip := range wafIpBlockList() {
		if strings.Contains(ip, "/") {
			fmt.Fprintf(&sb, "RewriteCond %%{REMOTE_ADDR} ^%s$\n", ip)
		} else {
			fmt.Fprintf(&sb, "RewriteCond %%{REMOTE_ADDR} ^%s$\n", regexp.QuoteMeta(ip))
		}
		sb.WriteString("RewriteRule .* - [F,L]\n")
	}
	for _, ip := range wafIpAllowList() {
		fmt.Fprintf(&sb, "RewriteCond %%{REMOTE_ADDR} ^%s$\n", regexp.QuoteMeta(ip))
		sb.WriteString("RewriteRule .* - [L]\n")
	}

	// 攻击规则（URI / UA）
	rules := enabledWAFRules()
	uriPatterns := make([]string, 0)
	uaPatterns := make([]string, 0)
	for _, r := range rules {
		switch r.MatchField {
		case "ua":
			uaPatterns = append(uaPatterns, r.Pattern)
		default:
			uriPatterns = append(uriPatterns, r.Pattern)
		}
	}
	if len(uriPatterns) > 0 {
		if wafMode == WAFModeObserve {
			sb.WriteString("# 观察模式：URI 规则仅记录\n")
		} else {
			fmt.Fprintf(&sb, "RewriteCond %%{REQUEST_URI} \"(?i)(%s)\"\n", strings.Join(uriPatterns, "|"))
			sb.WriteString("RewriteRule .* - [F,L]\n")
		}
	}
	if len(uaPatterns) > 0 {
		if wafMode == WAFModeObserve {
			sb.WriteString("# 观察模式：UA 规则仅记录\n")
		} else {
			fmt.Fprintf(&sb, "RewriteCond %%{HTTP_USER_AGENT} \"(?i)(%s)\"\n", strings.Join(uaPatterns, "|"))
			sb.WriteString("RewriteRule .* - [F,L]\n")
		}
	}

	// URL 黑白名单
	for _, url := range wafUrlBlockList() {
		fmt.Fprintf(&sb, "RewriteRule ^%s$ - [F,L]\n", regexp.QuoteMeta(url))
	}
	return sb.String()
}

// writeApacheWAFConf 为每个 Apache 站点生成独立 WAF 片段（替代旧的一刀切全局 conf）
// 站点片段 include 到各自 <VirtualHost>，从而支持「单站跳过全局 WAF」。
func writeApacheWAFConf() error {
	if err := os.MkdirAll(apacheWafSiteConfDir, 0o755); err != nil {
		return err
	}
	for _, item := range ListSites() {
		conf := genApacheSiteWAFSnippet(&item.Site)
		path := filepath.Join(apacheWafSiteConfDir, "lp_"+item.Name+".conf")
		if strings.TrimSpace(conf) == "" {
			// 跳过全局 WAF 或 WAF 关闭：写空文件占位，避免 include 报错
			_ = os.WriteFile(path, []byte("# WAF skipped by kypanel\n"), 0o644)
			continue
		}
		if err := os.WriteFile(path, []byte(conf), 0o644); err != nil {
			return err
		}
	}
	// 删除旧的一刀切全局 conf（若存在）
	removeApacheWAFConf()
	// 校验并重载 Apache
	res, _ := ExecCommand("apache2ctl -t 2>/dev/null || httpd -t 2>/dev/null", 15*time.Second)
	if res != nil && res.ExitCode == 0 {
		_, _ = ExecCommand("apache2ctl graceful 2>/dev/null || httpd -k graceful 2>/dev/null", 15*time.Second)
	}
	return nil
}

func removeApacheWAFConf() {
	_ = os.Remove(apacheWafConfPath)
	_ = os.Remove("/etc/httpd/conf.d/lp_waf.conf")
}

// ---------- 站点配置注入 ----------

// InjectSiteWAFInclude 在 genSiteServerBlock 中调用，返回追加到 server 块的 WAF include 行
// 放到 website.go 的 genSiteServerBlock 末尾。

// wafSiteConfIncludeLine 在 writeSiteConf 中追加 include 到站点配置（server 块内）
func wafSiteConfIncludeLine(name string, siteID uint) string {
	if !wafEnabled {
		return ""
	}
	// 站点设置了「不使用全局规则」时跳过全局 WAF include
	if wafSiteSkipGlobal(siteID) {
		return ""
	}
	return "    include " + filepath.Join(wafSiteConfDir, "lp_"+name+".conf") + ";\n"
}

// apacheWAFIncludeLine 返回 Apache 站点 <VirtualHost> 内的全局 WAF 片段 include 行
func apacheWAFIncludeLine(name string, siteID uint) string {
	if !wafEnabled {
		return ""
	}
	if wafSiteSkipGlobal(siteID) {
		return ""
	}
	return "    Include " + filepath.Join(apacheWafSiteConfDir, "lp_"+name+".conf") + "\n"
}

// regenerateSiteWAFSnippet 重新生成单个站点的全局 WAF 片段（nginx/apache 都处理）
// 当单站安全配置的 use_global_rules 改变后调用，确保该站点的全局 WAF 片段同步。
func regenerateSiteWAFSnippet(s *model.Site) {
	if !wafEnabled {
		return
	}
	// nginx 片段
	if nginxInstalledBin() {
		_ = os.MkdirAll(wafSiteConfDir, 0o755)
		conf := genSiteWAFSnippet(s)
		path := filepath.Join(wafSiteConfDir, "lp_"+s.Name+".conf")
		if strings.TrimSpace(conf) == "" {
			_ = os.WriteFile(path, []byte("# WAF skipped by kypanel\n"), 0o644)
		} else {
			_ = os.WriteFile(path, []byte(conf), 0o644)
		}
	}
	// apache 片段
	if apacheInstalledBin() {
		_ = os.MkdirAll(apacheWafSiteConfDir, 0o755)
		conf := genApacheSiteWAFSnippet(s)
		path := filepath.Join(apacheWafSiteConfDir, "lp_"+s.Name+".conf")
		if strings.TrimSpace(conf) == "" {
			_ = os.WriteFile(path, []byte("# WAF skipped by kypanel\n"), 0o644)
		} else {
			_ = os.WriteFile(path, []byte(conf), 0o644)
		}
	}
}

// ---------- CC 防护后台协程 ----------

// ccMonitorLoop 定时扫描访问日志，统计单 IP 频率，超限自动封禁
func ccMonitorLoop() {
	for {
		time.Sleep(10 * time.Second)
		cfg := GetWAFCcConfig()
		if !wafEnabled || !cfg.Enabled {
			continue
		}
		if cfg.MaxRequests <= 0 || cfg.WindowSec <= 0 {
			continue
		}
		runCCScan(cfg)
	}
}

// accessLogPaths 返回全部站点访问日志路径
func accessLogPaths() []string {
	var sites []model.Site
	model.DB.Select("name").Find(&sites)
	paths := make([]string, 0, len(sites))
	for _, s := range sites {
		paths = append(paths, "/var/log/nginx/"+s.Name+".access.log")
	}
	return paths
}

// runCCScan 扫描访问日志并封禁超频 IP
func runCCScan(cfg model.WAFCcConfig) {
	type counter struct {
		count int
		first time.Time
	}
	counters := make(map[string]*counter)
	whitelist := map[string]bool{}
	for _, ip := range wafCcWhitelist() {
		whitelist[ip] = true
	}
	// 已封禁 IP（避免重复封禁）
	blocked := map[string]bool{}
	for _, ip := range wafIpBlockList() {
		blocked[ip] = true
	}

	now := time.Now()
	cutoff := now.Add(-time.Duration(cfg.WindowSec) * time.Second)

	for _, path := range accessLogPaths() {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufioNewScanner(f)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			ip := parseAccessLogIP(line)
			if ip == "" || whitelist[ip] || blocked[ip] {
				continue
			}
			c, ok := counters[ip]
			if !ok {
				counters[ip] = &counter{count: 1, first: now}
				continue
			}
			c.count++
			if c.first.Before(cutoff) {
				c.count = 1
				c.first = now
			}
		}
		f.Close()
	}

	// 封禁超频 IP
	changed := false
	for ip, c := range counters {
		if c.count >= cfg.MaxRequests {
			if err := CreateWAFIpRule(WAFIpRuleReq{
				Type:          "ip",
				Action:        "block",
				Content:       ip,
				ExpireSeconds: cfg.BlockSec,
				Remark:        fmt.Sprintf("CC 攻击自动封禁（%ds 内 %d 次请求）", cfg.WindowSec, c.count),
			}); err == nil {
				changed = true
				recordWAFAttack(ip, "", "cc_attack", "CC攻击", "CC 频率限制", "block", "AUTO", "/", "")
			}
		}
	}
	if changed || purgeExpiredBans() {
		_ = applyWAFConfig(false)
	}
}

// ---------- 辅助 ----------

// parseAccessLogIP 从 nginx access log 行提取客户端 IP（默认 combined 格式第一段）
func parseAccessLogIP(line string) string {
	if line == "" {
		return ""
	}
	// 提取第一个空格前的内容（可能是 IP 或 -）
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	ip := strings.Trim(fields[0], `[]`)
	// 若为 "IP:port" 形式则剥端口
	if i := strings.LastIndex(ip, ":"); i > 0 && strings.Count(ip, ":") == 1 {
		ip = ip[:i]
	}
	if net.ParseIP(ip) == nil {
		return ""
	}
	return ip
}

// wafRuleCategoryCN 返回规则类别中文名
func wafRuleCategoryCN(category string) string {
	for _, r := range builtinWAFRules {
		if r.category == category {
			return r.categoryCN
		}
	}
	return "自定义规则"
}

// WAFStatsTopIp 统计 Top 攻击 IP（供前端展示）
func WAFStatsTopIp(limit int) []map[string]interface{} {
	if limit <= 0 {
		limit = 10
	}
	var rows []map[string]interface{}
	model.DB.Model(&model.WAFAttackLog{}).
		Select("ip, COUNT(*) as cnt").
		Group("ip").
		Order("cnt desc").
		Limit(limit).
		Scan(&rows)
	return rows
}

// WAFStatsTopCategory 统计攻击类型分布
func WAFStatsTopCategory(limit int) []map[string]interface{} {
	if limit <= 0 {
		limit = 10
	}
	var rows []map[string]interface{}
	model.DB.Model(&model.WAFAttackLog{}).
		Select("category_cn, COUNT(*) as cnt").
		Group("category_cn").
		Order("cnt desc").
		Limit(limit).
		Scan(&rows)
	return rows
}

// bufioNewScanner 创建 bufio.Scanner
func bufioNewScanner(f *os.File) *bufio.Scanner {
	return bufio.NewScanner(f)
}

// configDir 获取面板数据目录（供 WAF 临时文件使用）
func wafDataDir() string {
	return config.Get().DataDir
}
