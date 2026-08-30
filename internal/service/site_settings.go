package service

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"kypanel/internal/model"
)

const sslDir = "/etc/nginx/ssl"

// siteSSLPath 证书文件路径（按当前 Web 服务器类型返回对应目录）
func siteSSLPath(name string) (cert, key string) {
	dir := sslDir
	if WebServerType() == webApache {
		dir = apacheSslDir
	}
	return filepath.Join(dir, "lp_"+name+".pem"), filepath.Join(dir, "lp_"+name+".key")
}

var siteDomainRe = regexp.MustCompile(`^(\*\.)?[a-zA-Z0-9]([a-zA-Z0-9\-\.]*[a-zA-Z0-9])?$`)

// siteBindingRe 站点绑定正则：支持 域名 / IP / 域名:端口 / IP:端口
// 形如 example.com、*.example.com、127.0.0.1、127.0.0.1:8899、example.com:8443
var siteBindingRe = regexp.MustCompile(`^(\*\.)?[a-zA-Z0-9]([a-zA-Z0-9\-\.]*[a-zA-Z0-9])?(:\d{1,5})?$`)

// splitHostPort 解析「域名/IP[:端口]」，返回 host、port（无端口返回空串）、hasPort
func splitHostPort(binding string) (host, port string, hasPort bool) {
	binding = strings.TrimSpace(binding)
	// 优先用最后一个冒号切分（IPv6 暂不支持，仅 IPv4 + 域名）
	if idx := strings.LastIndex(binding, ":"); idx > 0 && idx < len(binding)-1 {
		p := binding[idx+1:]
		if n, err := strconv.Atoi(p); err == nil && n >= 1 && n <= 65535 {
			return binding[:idx], p, true
		}
	}
	return binding, "", false
}

// isValidBinding 校验单个绑定是否合法（域名/IP，可带端口）
func isValidBinding(binding string) bool {
	binding = strings.TrimSpace(binding)
	if binding == "" {
		return false
	}
	host, port, hasPort := splitHostPort(binding)
	if hasPort {
		n, _ := strconv.Atoi(port)
		if n < 1 || n > 65535 {
			return false
		}
	}
	// host 部分必须是合法域名或合法 IP
	if siteDomainRe.MatchString(host) {
		return true
	}
	if net.ParseIP(host) != nil {
		return true
	}
	return false
}

// SiteDetail 网站详情（含设置所需全部信息）
type SiteDetail struct {
	model.Site
	HasSslCert bool   `json:"has_ssl_cert"`
	WebServer  string `json:"web_server"`
}

// GetSiteDetail 返回站点详情
func GetSiteDetail(id uint) (*SiteDetail, error) {
	var s model.Site
	if err := model.DB.First(&s, id).Error; err != nil {
		return nil, errors.New("站点不存在")
	}
	s.Remark = fixRemarkMojibake(s.Remark)
	// PHP / 静态站点若默认文档为空，展示系统默认值（不写入 DB）
	if strings.TrimSpace(s.DefaultIndex) == "" && (s.Type == model.SiteTypePHP || s.Type == model.SiteTypeStatic) {
		s.DefaultIndex = defaultIndexForType(s.Type)
	}
	cert, _ := siteSSLPath(s.Name)
	_, certErr := os.Stat(cert)
	s.Redirects = ListSiteRedirects(s.ID)
	return &SiteDetail{Site: s, HasSslCert: certErr == nil, WebServer: detectWebServer()}, nil
}

func detectWebServer() string {
	return WebServerType()
}

// SiteSettingsReq 网站设置保存请求
// 前端每个 Tab 独立保存，仅提交该 Tab 的字段；后端按 Tab 只更新对应字段组。
type SiteSettingsReq struct {
	ID               uint               `json:"id" binding:"required"`
	Tab              string             `json:"tab"` // base / redirect / rewrite / hotlink
	Name             string             `json:"name"`
	Remark           string             `json:"remark"`
	Domain           string             `json:"domain"`
	Domains          string             `json:"domains"` // 附加域名，逗号分隔
	Root             string             `json:"root"`
	RuntimeDir       string             `json:"runtime_dir"`
	DefaultIndex     string             `json:"default_index"`
	RateLimitKbs     int                `json:"rate_limit_kbs"`
	Rewrite          string             `json:"rewrite"`
	HotlinkEnabled   bool               `json:"hotlink_enabled"`
	HotlinkReferers  string             `json:"hotlink_referers"`
	CacheEnabled     bool               `json:"cache_enabled"`
	SecurityHeaders  bool               `json:"security_headers"`
	RedirectURL      string             `json:"redirect_url"` // 兼容旧单条，新逻辑见 Redirects
	RedirectCode     int                `json:"redirect_code"`
	RedirectKeepPath bool               `json:"redirect_keep_path"`
	Redirects        []RedirectRuleItem `json:"redirects"` // 多条重定向规则
	PhpFpm           string             `json:"php_fpm"`
	ProxyPass        string             `json:"proxy_pass"`
	StartCommand     string             `json:"start_command"`
	EnvVars          string             `json:"env_vars"`
	ProxyPort        int                `json:"proxy_port"`
	RuntimeVersion   string             `json:"runtime_version"`
}

// RedirectRuleItem 单条重定向规则
type RedirectRuleItem struct {
	ID        uint     `json:"id"`
	MatchType string   `json:"match_type"` // domain / dir
	Domains   []string `json:"domains"`    // 匹配域名（可多选）
	Paths     []string `json:"paths"`      // 匹配目录（可多选），如 ["/blog", "/docs"]
	TargetURL string   `json:"target_url"`
	Code      int      `json:"code"`
	KeepPath  bool     `json:"keep_path"`
}

// validateSiteName 校验站点新名称
func validateSiteName(name string, excludeID uint) error {
	if !siteNameRe.MatchString(name) {
		return errors.New("站点名称仅支持字母、数字、点、下划线、中划线，最长 64 字符")
	}
	var count int64
	model.DB.Model(&model.Site{}).Where("name = ? AND id <> ?", name, excludeID).Count(&count)
	if count > 0 {
		return errors.New("站点名称已存在")
	}
	return nil
}

// validateDomains 校验域名/绑定格式与占用冲突
func validateDomains(domain, domains string, excludeID uint) error {
	all := append([]string{domain}, strings.Split(domains, ",")...)
	seen := map[string]bool{}
	for _, d := range all {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if !isValidBinding(d) {
			return errors.New("域名/IP 格式不合法: " + d)
		}
		if seen[d] {
			return errors.New("绑定重复: " + d)
		}
		seen[d] = true

		// 占用冲突检测（排除自身）—— 比较 host 部分（不含端口）
		host, _, _ := splitHostPort(d)
		var sites []model.Site
		model.DB.Where("id <> ?", excludeID).Find(&sites)
		for i := range sites {
			for _, n := range siteServerNames(&sites[i]) {
				otherHost, _, _ := splitHostPort(n)
				if otherHost == host {
					return errors.New("域名 " + d + " 已被网站「" + sites[i].Name + "」使用")
				}
			}
		}
	}
	return nil
}

// SaveSiteSettings 保存网站设置（按 Tab 只更新对应字段组，重新生成 nginx 配置并生效）
func SaveSiteSettings(req SiteSettingsReq) error {
	var s model.Site
	if err := model.DB.First(&s, req.ID).Error; err != nil {
		return errors.New("站点不存在")
	}

	switch req.Tab {
	case "domains":
		// 仅修改域名绑定（不校验根目录等 base 必填项，避免拖动排序等场景误报）
		s.Domain = strings.TrimSpace(req.Domain)
		s.Domains = strings.TrimSpace(strings.Join(splitCSV(req.Domains), ","))
		return applySiteSettings(&s, req.ID)
	case "redirect":
		return saveRedirectTab(&s, req)
	case "rewrite":
		s.Rewrite = req.Rewrite
		return applySiteSettings(&s, req.ID)
	case "hotlink":
		s.HotlinkEnabled = req.HotlinkEnabled
		s.HotlinkReferers = strings.TrimSpace(strings.Join(splitCSV(req.HotlinkReferers), ","))
		s.CacheEnabled = req.CacheEnabled
		s.SecurityHeaders = req.SecurityHeaders
		return applySiteSettings(&s, req.ID)
	default: // base（含旧版未传 tab 的全量请求，走 base 兼容）
		return saveBaseTab(&s, req)
	}
}

// applySiteSettings 公共收尾：重写 nginx 配置、重载、同步进程服务、落库
func applySiteSettings(s *model.Site, id uint) error {
	// 带端口的绑定（IP:端口 / 域名:端口）自动放行对应 TCP 端口
	allowSiteBindingPorts(s)
	s.ConfigOverride = ""
	if err := writeSiteConfAndReload(s); err != nil {
		return err
	}
	if isRuntimeSite(s.Type) {
		if err := rewriteSiteService(s); err != nil {
			return errors.New("更新进程服务失败: " + err.Error())
		}
	}
	return model.DB.Save(s).Error
}

// allowSiteBindingPorts 放行站点带端口绑定的 TCP 端口（幂等，失败不阻断主流程）
// 备注带上站点域名，来源标识为 site:<域名>，便于识别和后续回收
func allowSiteBindingPorts(s *model.Site) {
	siteRemark := "站点 " + s.Name + " 放行"
	for _, b := range sitePortBindings(s) {
		_ = AllowPortWithSource(b.Port, "tcp", siteRemark, "site:"+s.Name)
	}
}

// saveBaseTab 基础设置 Tab：名称/域名/目录/运行目录/PHP 版本/反代目标等
func saveBaseTab(s *model.Site, req SiteSettingsReq) error {
	// 站点名称变更（需要同步配置文件、证书、服务名等）
	newName := strings.TrimSpace(req.Name)
	if newName == "" {
		newName = s.Name
	}
	if newName != s.Name {
		if err := validateSiteName(newName, s.ID); err != nil {
			return err
		}
		oldName := s.Name
		newSite := *s
		newSite.Name = newName

		// 重命名面板托管的证书文件
		oldCert, oldKey := siteSSLPath(oldName)
		newCert, newKey := siteSSLPath(newName)
		if s.SslCertPath == "" || s.SslCertPath == oldCert {
			if _, err := os.Stat(oldCert); err == nil {
				if err := os.Rename(oldCert, newCert); err != nil {
					return errors.New("重命名证书文件失败: " + err.Error())
				}
				newSite.SslCertPath = newCert
			}
		}
		if s.SslKeyPath == "" || s.SslKeyPath == oldKey {
			if _, err := os.Stat(oldKey); err == nil {
				if err := os.Rename(oldKey, newKey); err != nil {
					return errors.New("重命名私钥文件失败: " + err.Error())
				}
				newSite.SslKeyPath = newKey
			}
		}

		// 重命名访问日志（可选，失败不阻断）
		oldLog := "/var/log/nginx/" + oldName + ".access.log"
		newLog := "/var/log/nginx/" + newName + ".access.log"
		if _, err := os.Stat(oldLog); err == nil {
			_ = os.Rename(oldLog, newLog)
		}

		// 删除旧 Web 服务器配置，避免重名 server/vhost 冲突
		_ = removeSiteConfFile(oldName)

		// 进程型站点：清理旧 systemd 服务，后续会按新名称重新生成
		if isRuntimeSite(s.Type) {
			removeSiteService(oldName)
		}

		// 同步计划任务里的站点名
		model.DB.Model(&model.Cron{}).Where("site_name = ?", oldName).Update("site_name", newName)

		*s = newSite
	}

	s.Remark = strings.TrimSpace(req.Remark)
	s.Domain = strings.TrimSpace(req.Domain)
	s.Domains = strings.TrimSpace(strings.Join(splitCSV(req.Domains), ","))
	s.DefaultIndex = strings.TrimSpace(req.DefaultIndex)
	s.RateLimitKbs = req.RateLimitKbs
	if s.RateLimitKbs < 0 {
		s.RateLimitKbs = 0
	}
	s.PhpFpm = strings.TrimSpace(req.PhpFpm)
	if strings.TrimSpace(req.RuntimeVersion) != "" {
		s.RuntimeVersion = strings.TrimSpace(req.RuntimeVersion)
	}

	// 域名校验
	if err := validateDomains(s.Domain, s.Domains, s.ID); err != nil {
		return err
	}

	// 目录校验（静态 / PHP）
	if model.IsRootType(s.Type) {
		root := strings.TrimRight(strings.TrimSpace(req.Root), "/")
		if root == "" {
			return errors.New("网站目录不能为空")
		}
		if !strings.HasPrefix(root, "/") {
			return errors.New("网站目录必须是绝对路径")
		}
		s.Root = root
		if err := os.MkdirAll(s.Root, 0o755); err != nil {
			return errors.New("创建目录失败: " + err.Error())
		}
		rd := strings.TrimSpace(req.RuntimeDir)
		rd = strings.Trim(rd, "/")
		if rd != "" {
			if strings.Contains(rd, "..") {
				return errors.New("运行目录不能包含 ..")
			}
			fullRuntime := filepath.Join(s.Root, rd)
			if err := os.MkdirAll(fullRuntime, 0o755); err != nil {
				return errors.New("创建运行目录失败: " + err.Error())
			}
		}
		s.RuntimeDir = rd
	}

	// 反向代理目标
	switch {
	case s.Type == model.SiteTypeProxy:
		pp := strings.TrimSpace(req.ProxyPass)
		if pp == "" {
			return errors.New("反向代理目标不能为空")
		}
		if !strings.HasPrefix(pp, "http://") && !strings.HasPrefix(pp, "https://") {
			return errors.New("代理目标必须以 http:// 或 https:// 开头")
		}
		s.ProxyPass = strings.TrimRight(pp, "/")
	case isRuntimeSite(s.Type):
		s.StartCommand = strings.TrimSpace(req.StartCommand)
		s.EnvVars = req.EnvVars
		if req.ProxyPort > 0 && req.ProxyPort <= 65535 {
			s.ProxyPort = req.ProxyPort
		}
		if s.StartCommand == "" {
			return errors.New("启动命令不能为空")
		}
		s.ProxyPass = fmt.Sprintf("http://127.0.0.1:%d", s.ProxyPort)
	}

	return applySiteSettings(s, req.ID)
}

// saveRedirectTab 重定向 Tab：多条规则整表替换
func saveRedirectTab(s *model.Site, req SiteSettingsReq) error {
	// 兼容旧单条字段
	s.RedirectURL = strings.TrimSpace(req.RedirectURL)
	if s.RedirectURL != "" && !strings.HasPrefix(s.RedirectURL, "http://") && !strings.HasPrefix(s.RedirectURL, "https://") {
		return errors.New("重定向目标必须以 http:// 或 https:// 开头")
	}
	switch req.RedirectCode {
	case 301, 302, 307, 308:
		s.RedirectCode = req.RedirectCode
	default:
		s.RedirectCode = 301
	}
	s.RedirectKeepPath = req.RedirectKeepPath

	rules, err := normalizeRedirectRules(req.Redirects)
	if err != nil {
		return err
	}
	// 先持久化规则（整表替换），genSiteConf 依赖 s.Redirects
	if err := replaceSiteRedirects(s.ID, rules); err != nil {
		return err
	}
	s.Redirects = rules

	return applySiteSettings(s, req.ID)
}

// normalizeRedirectRules 校验并规范化重定向规则（返回可直接写入 DB 的模型切片）
func normalizeRedirectRules(items []RedirectRuleItem) ([]model.SiteRedirect, error) {
	rules := make([]model.SiteRedirect, 0, len(items))
	for i, it := range items {
		matchType := strings.TrimSpace(it.MatchType)
		if matchType != model.RedirectMatchDomain && matchType != model.RedirectMatchDir {
			return nil, fmt.Errorf("第 %d 条规则匹配方式无效", i+1)
		}
		target := strings.TrimSpace(it.TargetURL)
		if target == "" {
			return nil, fmt.Errorf("第 %d 条规则未填写目标 URL", i+1)
		}
		if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
			return nil, fmt.Errorf("第 %d 条规则目标 URL 必须以 http:// 或 https:// 开头", i+1)
		}
		code := it.Code
		if code != 301 && code != 302 && code != 307 && code != 308 {
			code = 301
		}

		var domains, paths string
		if matchType == model.RedirectMatchDomain {
			var ds []string
			for _, d := range it.Domains {
				d = strings.TrimSpace(d)
				if d != "" {
					ds = append(ds, d)
				}
			}
			if len(ds) == 0 {
				return nil, fmt.Errorf("第 %d 条规则未选择匹配域名", i+1)
			}
			domains = strings.Join(ds, ",")
		} else {
			var ps []string
			for _, p := range it.Paths {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				if !strings.HasPrefix(p, "/") {
					p = "/" + p
				}
				ps = append(ps, strings.TrimRight(p, "/"))
			}
			if len(ps) == 0 {
				return nil, fmt.Errorf("第 %d 条规则未选择匹配目录", i+1)
			}
			paths = strings.Join(ps, ",")
		}

		rules = append(rules, model.SiteRedirect{
			SiteID:    0, // 由 replaceSiteRedirects 统一设置
			MatchType: matchType,
			Domains:   domains,
			Paths:     paths,
			TargetURL: target,
			Code:      code,
			KeepPath:  it.KeepPath,
			Sort:      i,
		})
	}
	return rules, nil
}

// replaceSiteRedirects 整表替换站点的重定向规则
func replaceSiteRedirects(siteID uint, rules []model.SiteRedirect) error {
	if err := model.DB.Where("site_id = ?", siteID).Delete(&model.SiteRedirect{}).Error; err != nil {
		return errors.New("清理旧重定向规则失败: " + err.Error())
	}
	for i := range rules {
		rules[i].SiteID = siteID
		if err := model.DB.Create(&rules[i]).Error; err != nil {
			return errors.New("保存重定向规则失败: " + err.Error())
		}
	}
	return nil
}

// ListSiteRedirects 读取站点的重定向规则
func ListSiteRedirects(siteID uint) []model.SiteRedirect {
	var rules []model.SiteRedirect
	model.DB.Where("site_id = ?", siteID).Order("sort asc, id asc").Find(&rules)
	return rules
}

// ListSiteDirs 返回站点根目录下的文件夹列表（相对路径，用于重定向「按目录」多选）
func ListSiteDirs(id uint) ([]map[string]string, error) {
	var s model.Site
	if err := model.DB.First(&s, id).Error; err != nil {
		return nil, err
	}
	root := strings.TrimSpace(s.Root)
	if root == "" {
		return nil, errors.New("站点未配置根目录")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, errors.New("读取目录失败: " + err.Error())
	}
	dirs := make([]map[string]string, 0)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		dirs = append(dirs, map[string]string{
			"name":  name,
			"value": "/" + name,
		})
	}
	return dirs, nil
}

// ListSiteSubdirs 返回站点根目录下的一级子目录（作为运行目录候选）
func ListSiteSubdirs(id uint) ([]map[string]string, error) {
	var s model.Site
	if err := model.DB.First(&s, id).Error; err != nil {
		return nil, err
	}
	if !model.IsRootType(s.Type) {
		return nil, errors.New("仅静态/PHP站点支持运行目录")
	}
	root := strings.TrimSpace(s.Root)
	opts := []map[string]string{{"label": "使用网站目录", "value": ""}}
	if root == "" {
		return opts, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, errors.New("读取目录失败: " + err.Error())
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		opts = append(opts, map[string]string{"label": name, "value": name})
	}
	return opts, nil
}

// DirTree 返回目录树浏览数据（用于弹窗选择目录）
func DirTree(path string) (map[string]interface{}, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "/"
	}
	if !filepath.IsAbs(path) {
		return nil, errors.New("路径必须是绝对路径")
	}
	clean := filepath.Clean(path)
	if strings.Contains(clean, "..") {
		return nil, errors.New("路径不合法")
	}
	info, err := os.Stat(clean)
	if err != nil {
		return nil, errors.New("读取目录失败: " + err.Error())
	}
	if !info.IsDir() {
		return nil, errors.New("目标不是目录")
	}

	entries, err := os.ReadDir(clean)
	if err != nil {
		return nil, errors.New("读取目录失败: " + err.Error())
	}

	var dirs []map[string]interface{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		dirs = append(dirs, map[string]interface{}{
			"name": name,
			"path": filepath.Join(clean, name),
		})
	}

	parent := filepath.Dir(clean)
	if parent == clean {
		parent = ""
	}

	return map[string]interface{}{
		"current": clean,
		"parent":  parent,
		"dirs":    dirs,
	}, nil
}

// rewriteSiteService 重写启动脚本并重启 systemd 服务（start/env/root 变更后调用）
func rewriteSiteService(s *model.Site) error {
	if s.Status != model.SiteRunning {
		return nil
	}
	// 复用创建逻辑重新生成 runner，然后重启服务
	if err := writeSiteService(s); err != nil {
		return err
	}
	res, err := ExecCommand(fmt.Sprintf("systemctl daemon-reload && systemctl restart %s", siteServiceName(s.Name)), 30*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(strings.TrimSpace(res.Stderr))
	}
	return nil
}

// splitCSV 拆分逗号分隔字段并过滤空项
func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// SiteSSLReq HTTPS 设置请求
type SiteSSLReq struct {
	ID      uint   `json:"id" binding:"required"`
	Enabled bool   `json:"enabled"`
	Force   bool   `json:"force"`
	Cert    string `json:"cert"` // PEM 证书内容，非空时覆盖
	Key     string `json:"key"`  // PEM 私钥内容，非空时覆盖
}

// SSLCertItem 已申请证书项
type SSLCertItem struct {
	ID        uint      `json:"id"`
	Name      string    `json:"name"`
	Brand     string    `json:"brand"`
	Domains   []string  `json:"domains"`
	Days      int       `json:"days"`
	CertPath  string    `json:"cert_path"`
	KeyPath   string    `json:"key_path"`
	Algorithm string    `json:"algorithm"`
	NotAfter  time.Time `json:"not_after"`
}

// ListSSLCerts 列出面板管理的所有 SSL 证书（含 ACME 申请和手动上传）
func ListSSLCerts() []SSLCertItem {
	var records []model.SSLCertRecord
	if err := model.DB.Order("updated_at desc").Find(&records).Error; err != nil {
		return nil
	}
	var items []SSLCertItem
	for _, r := range records {
		// 实时读取证书文件刷新剩余天数
		days, domains, _ := parseSSLCertInfo(r.CertPath)
		if days == 0 {
			days = r.Days
		}
		items = append(items, SSLCertItem{
			ID:        r.ID,
			Name:      r.Name,
			Brand:     r.Brand,
			Domains:   domains,
			Days:      days,
			CertPath:  r.CertPath,
			KeyPath:   r.KeyPath,
			Algorithm: r.Algorithm,
			NotAfter:  r.NotAfter,
		})
	}
	return items
}

func parseSSLCertInfo(certPath string) (int, []string, error) {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return 0, nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return 0, nil, errors.New("证书格式错误")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return 0, nil, err
	}
	domains := []string{cert.Subject.CommonName}
	seen := map[string]bool{cert.Subject.CommonName: true}
	for _, d := range cert.DNSNames {
		if !seen[d] {
			domains = append(domains, d)
			seen[d] = true
		}
	}
	days := int(time.Until(cert.NotAfter).Hours() / 24)
	return days, domains, nil
}

// ApplyCertReq 使用已有证书部署到站点
type ApplyCertReq struct {
	ID       uint   `json:"id" binding:"required"`
	CertName string `json:"cert_name" binding:"required"`
}

// ApplySSLCertToSiteResult 部署已有证书的返回（包含证书/私钥内容供前端展示）
type ApplySSLCertToSiteResult struct {
	Cert string `json:"cert"`
	Key  string `json:"key"`
}

// ApplySSLCertToSite 将已有证书复制到站点并启用 HTTPS，部署后返回 cert/key 内容
func ApplySSLCertToSite(req ApplyCertReq) (*ApplySSLCertToSiteResult, error) {
	var s model.Site
	if err := model.DB.First(&s, req.ID).Error; err != nil {
		return nil, errors.New("站点不存在")
	}
	var record model.SSLCertRecord
	if err := model.DB.Where("name = ?", req.CertName).First(&record).Error; err != nil {
		return nil, errors.New("证书记录不存在")
	}
	if err := os.MkdirAll(sslDir, 0o700); err != nil {
		return nil, errors.New("创建证书目录失败: " + err.Error())
	}
	certPath, keyPath := siteSSLPath(s.Name)
	certData, err := os.ReadFile(record.CertPath)
	if err != nil {
		return nil, errors.New("读取证书失败: " + err.Error())
	}
	keyData, err := os.ReadFile(record.KeyPath)
	if err != nil {
		return nil, errors.New("读取私钥失败: " + err.Error())
	}
	if err := os.WriteFile(certPath, certData, 0o644); err != nil {
		return nil, errors.New("写入证书失败: " + err.Error())
	}
	if err := os.WriteFile(keyPath, keyData, 0o600); err != nil {
		return nil, errors.New("写入私钥失败: " + err.Error())
	}
	s.SslEnabled = true
	s.SslCertPath = certPath
	s.SslKeyPath = keyPath
	s.SslForce = false
	s.ConfigOverride = ""
	if err := writeSiteConfAndReload(&s); err != nil {
		return nil, err
	}
	if err := model.DB.Save(&s).Error; err != nil {
		return nil, err
	}
	return &ApplySSLCertToSiteResult{Cert: string(certData), Key: string(keyData)}, nil
}

// DeleteSSLCertReq 删除证书记录请求
type DeleteSSLCertReq struct {
	ID uint `json:"id" binding:"required"`
}

// DeleteSSLCert 删除证书记录及对应文件
func DeleteSSLCert(req DeleteSSLCertReq) error {
	var record model.SSLCertRecord
	if err := model.DB.First(&record, req.ID).Error; err != nil {
		return errors.New("证书记录不存在")
	}
	// 删除文件（忽略错误，文件可能已被手动清理）
	_ = os.Remove(record.CertPath)
	_ = os.Remove(record.KeyPath)
	return model.DB.Delete(&record).Error
}

// DownloadSSLCert 返回证书和私钥内容用于下载
func DownloadSSLCert(id uint) (cert string, key string, err error) {
	var record model.SSLCertRecord
	if err := model.DB.First(&record, id).Error; err != nil {
		return "", "", errors.New("证书记录不存在")
	}
	certData, err := os.ReadFile(record.CertPath)
	if err != nil {
		return "", "", errors.New("读取证书失败: " + err.Error())
	}
	keyData, err := os.ReadFile(record.KeyPath)
	if err != nil {
		return "", "", errors.New("读取私钥失败: " + err.Error())
	}
	return string(certData), string(keyData), nil
}

// DownloadSiteSSL 根据站点 ID 读取当前站点已部署的证书和私钥内容
func DownloadSiteSSL(siteID uint) (cert string, key string, err error) {
	var s model.Site
	if err := model.DB.First(&s, siteID).Error; err != nil {
		return "", "", errors.New("站点不存在")
	}
	certPath, keyPath := s.SslCertPath, s.SslKeyPath
	// 老站点/未记录路径时，回退到按站点名生成的默认证书路径
	if certPath == "" || keyPath == "" {
		certPath, keyPath = siteSSLPath(s.Name)
	}
	if certPath == "" || keyPath == "" {
		return "", "", errors.New("站点未配置 SSL 证书")
	}
	certData, err := os.ReadFile(certPath)
	if err != nil {
		return "", "", errors.New("读取证书失败: " + err.Error())
	}
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return "", "", errors.New("读取私钥失败: " + err.Error())
	}
	return string(certData), string(keyData), nil
}

// validCertPEM 校验字符串是否为可解析的 PEM 证书
func validCertPEM(content string) bool {
	block, _ := pem.Decode([]byte(content))
	if block == nil || block.Type != "CERTIFICATE" {
		return false
	}
	_, err := x509.ParseCertificate(block.Bytes)
	return err == nil
}

// validKeyPEM 校验字符串是否为可解析的 PEM 私钥（PKCS1 / SEC1 / PKCS8）
func validKeyPEM(content string) bool {
	block, _ := pem.Decode([]byte(content))
	if block == nil {
		return false
	}
	if _, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return true
	}
	if _, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return true
	}
	if _, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		return true
	}
	return false
}

// certKeyMatch 校验证书与私钥是否属于同一对（公钥一致）
func certKeyMatch(certPEM, keyPEM string) bool {
	certBlock, _ := pem.Decode([]byte(certPEM))
	keyBlock, _ := pem.Decode([]byte(keyPEM))
	if certBlock == nil || keyBlock == nil {
		return false
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return false
	}
	var pub any
	if k, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes); err == nil {
		pub = &k.PublicKey
	} else if k, err := x509.ParseECPrivateKey(keyBlock.Bytes); err == nil {
		pub = &k.PublicKey
	} else if k, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes); err == nil {
		if signer, ok := k.(crypto.Signer); ok {
			pub = signer.Public()
		} else {
			return false
		}
	} else {
		return false
	}
	return pubKeysEqual(cert.PublicKey, pub)
}

func pubKeysEqual(a, b any) bool {
	switch ka := a.(type) {
	case *rsa.PublicKey:
		kb, ok := b.(*rsa.PublicKey)
		return ok && ka.N.Cmp(kb.N) == 0 && ka.E == kb.E
	case *ecdsa.PublicKey:
		kb, ok := b.(*ecdsa.PublicKey)
		return ok && ka.Curve == kb.Curve && ka.X.Cmp(kb.X) == 0 && ka.Y.Cmp(kb.Y) == 0
	}
	return false
}

// SaveSiteSSL 保存 HTTPS 设置（写入证书文件并重建配置）
func SaveSiteSSL(req SiteSSLReq) error {
	var s model.Site
	if err := model.DB.First(&s, req.ID).Error; err != nil {
		return errors.New("站点不存在")
	}

	if err := os.MkdirAll(sslDir, 0o700); err != nil {
		return errors.New("创建证书目录失败: " + err.Error())
	}
	certPath, keyPath := siteSSLPath(s.Name)

	cert := strings.TrimSpace(req.Cert)
	key := strings.TrimSpace(req.Key)
	if req.Enabled {
		// 校验：开启时必须已有证书文件或本次提供
		_, certErr := os.Stat(certPath)
		if certErr != nil && cert == "" {
			return errors.New("开启 HTTPS 需要提供证书（PEM 格式）")
		}
		// 防误填污染：证书/私钥内容非空时必须为合法 PEM，且相互匹配。
		// 若不对内容做校验，占位符（如 1 个字节的 "1"）会被原样写入证书文件，
		// 导致 nginx 全局配置校验失败、所有网站一起起不来。
		if cert != "" && !validCertPEM(cert) {
			return errors.New("证书内容无效：请输入完整 PEM 格式（以 -----BEGIN CERTIFICATE----- 开头）")
		}
		if key != "" && !validKeyPEM(key) {
			return errors.New("私钥内容无效：请输入完整 PEM 格式（以 -----BEGIN PRIVATE KEY----- 开头）")
		}
		if cert != "" && key != "" && !certKeyMatch(cert, key) {
			return errors.New("证书与私钥不匹配：请确认它们对应同一张证书")
		}
		if cert != "" && key == "" {
			// 只填了证书：与现有私钥文件匹配校验
			if keyData, err := os.ReadFile(keyPath); err == nil && validKeyPEM(string(keyData)) && !certKeyMatch(cert, string(keyData)) {
				return errors.New("新证书与现有私钥不匹配：请同时提供对应的私钥")
			}
		}
		if cert == "" && key != "" {
			// 只填了私钥：与现有证书文件匹配校验
			if certData, err := os.ReadFile(certPath); err == nil && validCertPEM(string(certData)) && !certKeyMatch(string(certData), key) {
				return errors.New("新私钥与现有证书不匹配：请同时提供对应的证书")
			}
		}
		if cert != "" {
			if err := os.WriteFile(certPath, []byte(cert), 0o644); err != nil {
				return errors.New("写入证书失败: " + err.Error())
			}
		}
		if key != "" {
			if err := os.WriteFile(keyPath, []byte(key), 0o600); err != nil {
				return errors.New("写入私钥失败: " + err.Error())
			}
		}
		s.SslCertPath = certPath
		s.SslKeyPath = keyPath
	} else {
		s.SslCertPath = ""
		s.SslKeyPath = ""
		// 保留文件，便于再次开启
	}
	s.SslEnabled = req.Enabled
	s.SslForce = req.Enabled && req.Force

	// 手动上传的证书也写入 SSLCertRecord 统一管理
	if req.Enabled && cert != "" {
		if _, _, err := parseSSLCertInfo(certPath); err == nil {
			days, domains, _ := parseSSLCertInfo(certPath)
			record := model.SSLCertRecord{
				Name:      s.Name,
				Brand:     "custom",
				Domains:   strings.Join(domains, ","),
				CertPath:  certPath,
				KeyPath:   keyPath,
				Algorithm: "",
				Days:      days,
			}
			var existing model.SSLCertRecord
			if err := model.DB.Where("name = ?", s.Name).First(&existing).Error; err == nil {
				record.ID = existing.ID
				_ = model.DB.Save(&record).Error
			} else {
				_ = model.DB.Create(&record).Error
			}
		}
	}

	// 重建配置（自动管理模式）
	s.ConfigOverride = ""
	if err := writeSiteConfAndReload(&s); err != nil {
		return err
	}
	return model.DB.Save(&s).Error
}

// GetSiteConfig 读取站点当前 nginx 配置（优先读文件，实时反映）
func GetSiteConfig(id uint) (string, error) {
	var s model.Site
	if err := model.DB.First(&s, id).Error; err != nil {
		return "", errors.New("站点不存在")
	}
	data, err := os.ReadFile(siteConfPathFor(s.Name, WebServerType()))
	if err != nil {
		return "", errors.New("读取配置失败: " + err.Error())
	}
	return string(data), nil
}

// SaveSiteConfig 保存自定义 nginx 配置（完整覆盖，校验后生效）
func SaveSiteConfig(id uint, content string) error {
	var s model.Site
	if err := model.DB.First(&s, id).Error; err != nil {
		return errors.New("站点不存在")
	}
	if strings.TrimSpace(content) == "" {
		return errors.New("配置内容不能为空")
	}
	s.ConfigOverride = content
	if err := writeSiteConfAndReload(&s); err != nil {
		return err
	}
	return model.DB.Model(&s).Update("config_override", content).Error
}

// RuntimeOption 运行环境选项（PHP-FPM 切换）
type RuntimeOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// ListPhpFpms 检测已安装的 PHP-FPM 实例（支持多版本切换）
// 兼容 apt(sury) / RHEL(remi) / 自定义编译 多种安装方式
// 只返回能识别出具体 PHP 版本的选项
func ListPhpFpms() []RuntimeOption {
	opts := []RuntimeOption{}
	seen := map[string]bool{}

	// 1. 扫描常见 unix socket 路径
	sockPatterns := []string{
		"/run/php/php*-fpm.sock",
		"/run/php-fpm/*.sock",
		"/var/run/php/php*-fpm.sock",
		"/var/run/php-fpm/*.sock",
		"/tmp/php*-fpm.sock",
	}
	for _, pattern := range sockPatterns {
		res, err := ExecCommand("ls "+pattern+" 2>/dev/null", 10*time.Second)
		if err != nil || res.ExitCode != 0 {
			continue
		}
		for _, line := range strings.Split(res.Stdout, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			ver := strings.TrimPrefix(filepath.Base(line), "php")
			ver = strings.TrimSuffix(ver, "-fpm.sock")
			ver = strings.TrimSuffix(ver, ".sock")
			if len(ver) == 2 && isDigitStr(ver) { // remi php82
				ver = ver[0:1] + "." + ver[1:]
			}
			if ver == "" {
				continue
			}
			label := "PHP " + ver
			v := "unix:" + line
			if strings.HasPrefix(v, "unix:/var/run/") {
				v = "unix:/run/" + strings.TrimPrefix(v, "unix:/var/run/")
			}
			if !seen[v] {
				opts = append(opts, RuntimeOption{Value: v, Label: label})
				seen[v] = true
			}
		}
	}

	// 2. 读取 php-fpm pool 配置文件中的 listen 配置（即使 fpm 未运行也能发现）
	poolPatterns := []string{
		"/etc/php/*/fpm/pool.d/*.conf",
		"/etc/opt/remi/php*/php-fpm.d/*.conf",
		"/opt/remi/php*/root/etc/php-fpm.d/*.conf",
		"/usr/local/php*/etc/php-fpm.d/*.conf",
		"/usr/local/php*/etc/php-fpm.conf",
	}
	for _, pattern := range poolPatterns {
		res, err := ExecCommand("ls "+pattern+" 2>/dev/null", 10*time.Second)
		if err != nil || res.ExitCode != 0 {
			continue
		}
		for _, line := range strings.Split(res.Stdout, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			ver := extractPhpVerFromPath(line)
			if ver == "" {
				continue
			}
			lres, lerr := ExecCommand("grep -E '^\\s*listen\\s*=' "+line+" | tail -n1", 5*time.Second)
			if lerr != nil || lres.ExitCode != 0 {
				continue
			}
			parts := strings.SplitN(lres.Stdout, "=", 2)
			if len(parts) != 2 {
				continue
			}
			listen := strings.TrimSpace(parts[1])
			if listen == "" || listen == "127.0.0.1:9000" {
				continue
			}
			// unix socket 统一加 unix: 前缀；/var/run 通常为 /run 的符号链接，统一归一化去重
			if !strings.Contains(listen, ":") && strings.HasSuffix(listen, ".sock") {
				listen = "unix:" + listen
			}
			if strings.HasPrefix(listen, "unix:/var/run/") {
				listen = "unix:/run/" + strings.TrimPrefix(listen, "unix:/var/run/")
			}
			if seen[listen] {
				continue
			}
			seen[listen] = true
			opts = append(opts, RuntimeOption{Value: listen, Label: "PHP " + ver})
		}
	}

	// 3. 通过 php-fpm 二进制发现已安装版本（补充没有配置文件或没有运行的情况）
	binPatterns := []string{
		"/usr/sbin/php-fpm*",
		"/usr/local/php*/sbin/php-fpm",
		"/opt/php*/sbin/php-fpm",
	}
	for _, pattern := range binPatterns {
		res, err := ExecCommand("ls "+pattern+" 2>/dev/null", 10*time.Second)
		if err != nil || res.ExitCode != 0 {
			continue
		}
		for _, line := range strings.Split(res.Stdout, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.Contains(line, "debug") || strings.Contains(line, "phar") {
				continue
			}
			vres, verr := ExecCommand(line+" -v 2>/dev/null | head -n1", 5*time.Second)
			if verr != nil || vres.ExitCode != 0 {
				continue
			}
			m := regexp.MustCompile(`PHP\s+(\d+\.\d+)`).FindStringSubmatch(vres.Stdout)
			if len(m) < 2 {
				continue
			}
			ver := m[1]
			// 若已存在该版本则跳过
			exists := false
			for _, o := range opts {
				if strings.Contains(o.Label, ver) {
					exists = true
					break
				}
			}
			if exists {
				continue
			}
			// 尝试根据版本号猜 sock 路径
			if sock := guessPhpSock(ver); sock != "" {
				v := "unix:" + sock
				seen[v] = true
				opts = append(opts, RuntimeOption{Value: v, Label: "PHP " + ver})
			}
		}
	}

	// 按版本号排序（7.4 < 8.0 < 8.1 ...）；opts 可能为空（无 PHP 环境），直接返回
	if len(opts) <= 1 {
		return opts
	}
	sort.SliceStable(opts[1:], func(i, j int) bool {
		a, b := phpNumValue(opts[1:][i].Label), phpNumValue(opts[1:][j].Label)
		return a < b
	})
	return opts
}

// extractPhpVerFromPath 从 fpm 配置文件路径提取版本号
func extractPhpVerFromPath(p string) string {
	// apt(sury): /etc/php/7.4/fpm/pool.d/www.conf
	if m := regexp.MustCompile(`/php/(\d+\.\d+)/`).FindStringSubmatch(p); len(m) >= 2 {
		return m[1]
	}
	// remi: /etc/opt/remi/php74/php-fpm.d/www.conf 或 /opt/remi/php74/root/...
	if m := regexp.MustCompile(`/php(\d\d)/`).FindStringSubmatch(p); len(m) >= 2 {
		v := m[1]
		return v[0:1] + "." + v[1:]
	}
	return ""
}

// guessPhpSock 根据版本号猜测常见 sock 路径
func guessPhpSock(ver string) string {
	candidates := []string{
		"/run/php/php" + ver + "-fpm.sock",
		"/var/run/php/php" + ver + "-fpm.sock",
		"/run/php-fpm/php" + strings.Replace(ver, ".", "", -1) + ".sock",
		"/var/run/php-fpm/php" + strings.Replace(ver, ".", "", -1) + ".sock",
		"/tmp/php" + ver + "-fpm.sock",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// defaultPhpFpm 返回默认 PHP-FPM 目标：多版本共存时返回第一个检测到的 socket
func defaultPhpFpm() string {
	for _, o := range ListPhpFpms() {
		return o.Value
	}
	return ""
}

// phpNumValue 将 "PHP 8.2" 提取为可比较的数值（如 802）
func phpNumValue(label string) int {
	m := regexp.MustCompile(`(\d+)\.(\d+)`).FindStringSubmatch(label)
	if len(m) >= 3 {
		return atoi(m[1])*100 + atoi(m[2])
	}
	return 0
}

// atoi 简易字符串转整数（忽略错误）
func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// resolvePhpFpm 根据版本描述（如 "PHP 8.2.18" / "8.2"）匹配已安装的 PHP-FPM 目标，未匹配返回空串
func resolvePhpFpm(version string) string {
	ver := phpMinorVersion(version)
	if ver == "" {
		return ""
	}
	for _, o := range ListPhpFpms() {
		if strings.Contains(o.Label, ver) {
			return o.Value
		}
	}
	return ""
}

// phpMinorVersion 从版本描述中提取主.次版本号（如 "8.2"）
func phpMinorVersion(s string) string {
	m := regexp.MustCompile(`(\d+)\.(\d+)`).FindStringSubmatch(s)
	if len(m) >= 3 {
		return m[1] + "." + m[2]
	}
	return ""
}

// phpVersionNum 将版本描述转换为可排序的数值（如 "PHP 8.4" -> 80400）
func phpVersionNum(s string) int {
	m := regexp.MustCompile(`(\d+)\.(\d+)(?:\.(\d+))?`).FindStringSubmatch(s)
	if len(m) < 3 {
		return 0
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch := 0
	if len(m) >= 4 && m[3] != "" {
		patch, _ = strconv.Atoi(m[3])
	}
	return major*10000 + minor*100 + patch
}
