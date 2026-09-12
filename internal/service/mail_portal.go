package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"kypanel/internal/config"
	"kypanel/internal/model"
)

// ============================================================================
// 邮箱门户网站（官网 + webmail）
//
// 每个邮箱域名可开启一个门户站点：
//   - 默认域名 mail.<domain>，默认关闭，可开启/关闭
//   - 可配置 HTTPS、绑定其他域名
//   - 可设置官网标题 / 网站名称 / Logo / 底部版权
//   - 可开放注册（供他人自助注册邮箱）或关闭注册
// ============================================================================

// MailPortalView 门户配置视图（返回给前端）
type MailPortalView struct {
	DomainID     uint   `json:"domain_id"`
	Domain       string `json:"domain"`        // 邮箱域名 example.com
	Enabled      bool   `json:"enabled"`       // 门户是否开启
	PortalDomain string `json:"portal_domain"` // 门户域名 mail.example.com
	ExtraDomains string `json:"extra_domains"` // 附加绑定域名（逗号分隔）
	SSL          bool   `json:"ssl"`           // 是否启用 HTTPS
	Title        string `json:"title"`         // 官网标题
	Name         string `json:"name"`          // 网站名称
	Logo         string `json:"logo"`          // Logo 地址
	Footer       string `json:"footer"`        // 底部版权
	Register     bool   `json:"register"`      // 是否开放注册
	SiteID       uint   `json:"site_id"`       // 关联站点 ID
	SiteName     string `json:"site_name"`     // 关联站点名
	SiteRoot     string `json:"site_root"`     // 关联站点根目录
	URL          string `json:"url"`           // 访问地址

	// 证书相关（供前端展示/选择申请域名）
	CertBrand   string   `json:"cert_brand"`   // 证书品牌
	CertAlgo    string   `json:"cert_algo"`    // 证书算法
	CertEmail   string   `json:"cert_email"`   // 通知邮箱
	CertDomains []string `json:"cert_domains"` // 上次申请证书覆盖的域名
	CertOptions []string `json:"cert_options"` // 可申请证书的全部域名（门户域名 + 附加域名）
	CertExists  bool     `json:"cert_exists"`  // 是否已有证书文件
	CertDays    int      `json:"cert_days"`    // 现有证书剩余天数（0 表示无/未知）
}

// SaveMailPortalReq 保存门户配置请求
type SaveMailPortalReq struct {
	Enabled      bool     `json:"enabled"`
	PortalDomain string   `json:"portal_domain"`
	ExtraDomains string   `json:"extra_domains"`
	SSL          bool     `json:"ssl"`
	Title        string   `json:"title"`
	Name         string   `json:"name"`
	Logo         string   `json:"logo"`
	Footer       string   `json:"footer"`
	Register     bool     `json:"register"`
	CertBrand    string   `json:"cert_brand"`    // 证书品牌
	CertAlgo     string   `json:"cert_algo"`     // 证书算法
	CertEmail    string   `json:"cert_email"`    // 通知邮箱
	CertDomains  []string `json:"cert_domains"`  // 勾选要签发到证书里的域名
	ForceApply   bool     `json:"force_apply"`   // 强制重新申请（即使已有证书）
}

// MailPortalCertReq 门户证书申请请求
type MailPortalCertReq struct {
	Brand     string   `json:"brand"`
	Algorithm string   `json:"algorithm"`
	Email     string   `json:"email"`
	Domains   []string `json:"domains"`
}

// mailPortalSnippetPath 门户反代 nginx 片段路径
func mailPortalSnippetPath(name string) string {
	return filepath.Join(nginxConfDir, "lp_"+name+"_mailportal.inc")
}

// GetMailPortal 读取某邮箱域名的门户配置
func GetMailPortal(domainID uint) (*MailPortalView, error) {
	dom, err := MailDomainByID(domainID)
	if err != nil {
		return nil, err
	}
	return mailPortalView(dom), nil
}

// mailPortalView 组装门户视图（含默认值兜底）
func mailPortalView(dom *model.MailDomain) *MailPortalView {
	pd := strings.TrimSpace(dom.PortalDomain)
	if pd == "" {
		pd = "mail." + dom.Domain
	}
	name := strings.TrimSpace(dom.PortalName)
	if name == "" {
		name = dom.Domain
	}
	title := strings.TrimSpace(dom.PortalTitle)
	if title == "" {
		title = name
	}
	footer := strings.TrimSpace(dom.PortalFooter)
	if footer == "" {
		footer = fmt.Sprintf("© %d %s", time.Now().Year(), name)
	}
	v := &MailPortalView{
		DomainID:     dom.ID,
		Domain:       dom.Domain,
		Enabled:      dom.PortalEnabled,
		PortalDomain: pd,
		ExtraDomains: dom.PortalExtraDomains,
		SSL:          dom.PortalSSL,
		Title:        title,
		Name:         name,
		Logo:         dom.PortalLogo,
		Footer:       footer,
		Register:     dom.PortalRegister,
		SiteID:       dom.PortalSiteID,
	}
	// 证书配置：可选域名为门户域名 + 附加域名；已选域名缺省为全部
	v.CertOptions = mailPortalCertOptions(pd, dom.PortalExtraDomains)
	v.CertDomains = splitMailPortalDomains(dom.PortalCertDomains)
	if len(v.CertDomains) == 0 {
		v.CertDomains = append([]string{}, v.CertOptions...)
	}
	v.CertBrand = normalizeCertBrand(dom.PortalCertBrand)
	v.CertAlgo = normalizeCertAlgo(dom.PortalCertAlgo)
	v.CertEmail = strings.TrimSpace(dom.PortalCertEmail)
	if dom.PortalSiteID > 0 {
		if s, err := getSiteOrErr(dom.PortalSiteID); err == nil {
			v.SiteName = s.Name
			v.SiteRoot = s.Root
			v.SSL = s.SslEnabled
			// 读取现有证书剩余天数（有证书文件才算存在）
			certPath, _ := siteSSLPath(s.Name)
			if days, err := certExpiryDays(certPath); err == nil {
				v.CertExists = true
				v.CertDays = days
			}
		}
	}
	scheme := "http"
	if v.SSL {
		scheme = "https"
	}
	v.URL = scheme + "://" + pd
	return v
}

// mailPortalCertOptions 门户可申请证书的域名集合（门户域名 + 附加域名，去重、保持顺序）
func mailPortalCertOptions(portalDomain, extraDomains string) []string {
	out := []string{}
	seen := map[string]bool{}
	add := func(d string) {
		d = strings.ToLower(strings.TrimSpace(strings.Trim(d, ".")))
		if d == "" || seen[d] {
			return
		}
		seen[d] = true
		out = append(out, d)
	}
	add(portalDomain)
	for _, d := range splitMailPortalDomains(extraDomains) {
		add(d)
	}
	return out
}

// normalizeCertBrand 归一化证书品牌（默认 Let's Encrypt）
func normalizeCertBrand(brand string) string {
	if strings.ToLower(strings.TrimSpace(brand)) == "litessl" {
		return "litessl"
	}
	return "letsencrypt"
}

// normalizeCertAlgo 归一化证书算法（默认 RSA 2048）
func normalizeCertAlgo(algo string) string {
	if strings.ToLower(strings.TrimSpace(algo)) == "ecc256" {
		return "ecc256"
	}
	return "rsa2048"
}

// pickPortalCertDomains 从用户勾选的域名里筛出合法的门户域名（越界/重复的忽略），
// 结果为空时回退为全部可申请域名。
func pickPortalCertDomains(options []string, picked []string) []string {
	allowed := map[string]bool{}
	for _, d := range options {
		allowed[d] = true
	}
	out := []string{}
	seen := map[string]bool{}
	for _, d := range picked {
		d = strings.ToLower(strings.TrimSpace(strings.Trim(d, ".")))
		if d == "" || seen[d] || !allowed[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	if len(out) == 0 {
		out = append(out, options...)
	}
	return out
}

// splitMailPortalDomains 拆分逗号/空格分隔的域名列表，去重小写
func splitMailPortalDomains(raw string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t' || r == ';'
	}) {
		d := strings.ToLower(strings.TrimSpace(part))
		d = strings.Trim(d, ".")
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

// SaveMailPortal 保存门户配置：按需创建/更新站点、生成门户页面、同步 HTTPS 与域名。
func SaveMailPortal(domainID uint, req SaveMailPortalReq) (*MailPortalView, error) {
	dom, err := MailDomainByID(domainID)
	if err != nil {
		return nil, err
	}
	pd := strings.ToLower(strings.TrimSpace(req.PortalDomain))
	if pd == "" {
		pd = "mail." + dom.Domain
	}
	pd = strings.Trim(pd, ".")
	if !mailDomainRe.MatchString(pd) {
		return nil, errors.New("门户域名格式无效（示例：mail.example.com）")
	}
	extras := splitMailPortalDomains(req.ExtraDomains)
	for _, d := range extras {
		if !mailDomainRe.MatchString(d) {
			return nil, errors.New("附加域名格式无效: " + d)
		}
		if d == pd {
			return nil, errors.New("附加域名不能与门户域名相同")
		}
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = dom.Domain
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = name
	}
	footer := strings.TrimSpace(req.Footer)
	logo := strings.TrimSpace(req.Logo)

	// 证书申请配置：可选域名 = 门户域名 + 附加域名；勾选为空时默认全部
	certOptions := mailPortalCertOptions(pd, strings.Join(extras, ","))
	certDomains := pickPortalCertDomains(certOptions, req.CertDomains)
	brand := normalizeCertBrand(req.CertBrand)
	algo := normalizeCertAlgo(req.CertAlgo)
	certEmail := strings.TrimSpace(req.CertEmail)

	applyPortalFields(dom, pd, extras, req.SSL, title, name, logo, footer, req.Register)
	dom.PortalCertBrand = brand
	dom.PortalCertAlgo = algo
	dom.PortalCertEmail = certEmail
	dom.PortalCertDomains = strings.Join(certDomains, ",")

	// ---- 关闭门户 ----
	if !req.Enabled {
		if dom.PortalSiteID > 0 {
			if s, e := getSiteOrErr(dom.PortalSiteID); e == nil {
				_ = SiteAction(SiteActionReq{ID: s.ID, Action: "stop"})
				_ = os.Remove(mailPortalSnippetPath(s.Name))
			}
		}
		dom.PortalEnabled = false
		if err := model.DB.Save(dom).Error; err != nil {
			return nil, err
		}
		return mailPortalView(dom), nil
	}

	// ---- 开启门户：创建或复用站点 ----
	site, err := ensureMailPortalSite(dom, pd, extras, name)
	if err != nil {
		return nil, err
	}

	dom.PortalEnabled = true
	dom.PortalSiteID = site.ID
	if err := model.DB.Save(dom).Error; err != nil {
		return nil, err
	}

	// 生成门户页面（官网 index.html + webmail.html）
	if err := writeMailPortalFiles(dom, site, pd, extras, title, name, logo, footer, req.Register); err != nil {
		return nil, err
	}

	// 写入反代片段并重建站点配置
	if err := writeMailPortalSnippet(site); err != nil {
		return nil, err
	}
	if err := writeSiteConfAndReload(site); err != nil {
		return nil, err
	}

	// HTTPS：开启时若无证书（或用户勾选强制重新申请）则自动为所选域名申请免费证书。
	// 证书由面板内置 ACME 客户端签发（文件验证），成功后自动启用 HTTPS。
	if req.SSL {
		certPath, _ := siteSSLPath(site.Name)
		needApply := req.ForceApply
		if _, e := os.Stat(certPath); e != nil {
			needApply = true
		}
		if needApply {
			if err := issueACME(*site, brand, algo, certDomains, certEmail); err != nil {
				return nil, errors.New("证书申请失败：" + err.Error() +
					"（请确认域名已解析到本服务器且 80 端口可访问）")
			}
			dom.PortalCertDomains = strings.Join(certDomains, ",")
			if s2, e := getSiteOrErr(site.ID); e == nil {
				site = s2
			}
		}
		if !site.SslEnabled {
			if err := SaveSiteSSL(SiteSSLReq{ID: site.ID, Enabled: true}); err != nil {
				return nil, err
			}
			if s2, e := getSiteOrErr(site.ID); e == nil {
				site = s2
			}
		}
	} else if site.SslEnabled {
		if err := SaveSiteSSL(SiteSSLReq{ID: site.ID, Enabled: false}); err != nil {
			return nil, err
		}
		if s2, e := getSiteOrErr(site.ID); e == nil {
			site = s2
		}
	}

	dom.PortalEnabled = true
	dom.PortalSiteID = site.ID
	dom.PortalSSL = site.SslEnabled
	if err := model.DB.Save(dom).Error; err != nil {
		return nil, err
	}
	return mailPortalView(dom), nil
}

// applyPortalFields 把门户设置写入域名记录（不落库）
func applyPortalFields(dom *model.MailDomain, pd string, extras []string, ssl bool, title, name, logo, footer string, register bool) {
	dom.PortalDomain = pd
	dom.PortalExtraDomains = strings.Join(extras, ",")
	dom.PortalSSL = ssl
	dom.PortalTitle = title
	dom.PortalName = name
	dom.PortalLogo = logo
	dom.PortalFooter = footer
	dom.PortalRegister = register
}

// ensureMailPortalSite 确保门户站点存在（不存在则创建），并同步域名/备注。
func ensureMailPortalSite(dom *model.MailDomain, pd string, extras []string, name string) (*model.Site, error) {
	var site *model.Site
	if dom.PortalSiteID > 0 {
		if s, err := getSiteOrErr(dom.PortalSiteID); err == nil {
			site = s
		}
	}
	if site == nil {
		created, err := CreateSite(CreateSiteReq{
			Name:   pd,
			Domain: pd,
			Port:   80,
			Type:   model.SiteTypeStatic,
			Remark: "邮箱门户（域名 " + dom.Domain + "）",
		})
		if err != nil {
			return nil, err
		}
		site = created
	}
	site.Domain = pd
	site.Domains = strings.Join(extras, ",")
	site.Remark = "邮箱门户（域名 " + dom.Domain + "）"
	site.Status = model.SiteRunning
	if err := model.DB.Save(site).Error; err != nil {
		return nil, err
	}
	return site, nil
}

// writeMailPortalSnippet 生成门户反代 nginx 片段（把 /api/mail-portal/ 反代回面板）。
func writeMailPortalSnippet(site *model.Site) error {
	if WebServerType() == webApache {
		// Apache 暂不支持门户反代片段（门户页面仍可访问，但 webmail 接口不可用）
		return nil
	}
	port := config.Get().Server.Port
	if port <= 0 {
		port = 9999
	}
	host := fmt.Sprintf("127.0.0.1:%d", port)
	scheme := "http"
	sslOpts := ""
	if bool(config.Get().Server.HTTPS) {
		scheme = "https"
		sslOpts = "    proxy_ssl_verify off;\n"
	}
	var sb strings.Builder
	sb.WriteString("# kypanel 邮箱门户接口反代（自动生成，请勿手动修改）\n")
	sb.WriteString("location ^~ /api/mail-portal/ {\n")
	fmt.Fprintf(&sb, "    proxy_pass %s://%s/api/mail-portal/;\n", scheme, host)
	sb.WriteString("    proxy_set_header Host $host;\n")
	sb.WriteString("    proxy_set_header X-Forwarded-Host $host;\n")
	sb.WriteString("    proxy_set_header X-Real-IP $remote_addr;\n")
	sb.WriteString("    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n")
	sb.WriteString("    proxy_set_header X-Forwarded-Proto $scheme;\n")
	sb.WriteString("    client_max_body_size 30m;\n")
	sb.WriteString(sslOpts)
	sb.WriteString("}\n")
	sb.WriteString("# kypanel 邮箱门户附件目录保护（自动生成，请勿手动修改）\n")
	sb.WriteString("location ^~ /files/ {\n")
	sb.WriteString("    return 404;\n")
	sb.WriteString("}\n")
	return os.WriteFile(mailPortalSnippetPath(site.Name), []byte(sb.String()), 0o644)
}

// MailPortalIncludeLine 返回在站点 server 块中引入门户反代片段的 include 行（仅 nginx）。
func MailPortalIncludeLine(name string, siteID uint) string {
	if WebServerType() == webApache {
		return ""
	}
	if !isMailPortalSite(siteID) {
		return ""
	}
	path := mailPortalSnippetPath(name)
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return "    include " + path + ";\n"
}

// isMailPortalSite 判断某站点是否为已开启的邮箱门户站点。
func isMailPortalSite(siteID uint) bool {
	if siteID == 0 || model.DB == nil {
		return false
	}
	var cnt int64
	model.DB.Model(&model.MailDomain{}).
		Where("portal_site_id = ? AND portal_enabled = ?", siteID, true).Count(&cnt)
	return cnt > 0
}

// RegenerateAllMailPortals 按当前模板重新生成所有已开启门户的静态页面
// （index.html / webmail.html），返回成功重建的域名数量。
func RegenerateAllMailPortals() (int, error) {
	var doms []model.MailDomain
	if err := model.DB.Where("portal_enabled = ? AND portal_site_id > 0", true).Find(&doms).Error; err != nil {
		return 0, err
	}
	n := 0
	for i := range doms {
		dom := doms[i]
		site, err := getSiteOrErr(dom.PortalSiteID)
		if err != nil {
			continue
		}
		pd := strings.TrimSpace(dom.PortalDomain)
		if pd == "" {
			pd = "mail." + dom.Domain
		}
		extras := splitMailPortalDomains(dom.PortalExtraDomains)
		name := strings.TrimSpace(dom.PortalName)
		if name == "" {
			name = dom.Domain
		}
		title := strings.TrimSpace(dom.PortalTitle)
		if title == "" {
			title = name
		}
		err = writeMailPortalFiles(&dom, site, pd, extras, title, name,
			strings.TrimSpace(dom.PortalLogo), strings.TrimSpace(dom.PortalFooter), dom.PortalRegister)
		if err != nil {
			continue
		}
		n++
	}
	return n, nil
}

// DeleteMailPortalForDomain 删除域名时清理其门户站点（站点 + 配置 + 片段）。
func DeleteMailPortalForDomain(dom *model.MailDomain) {
	if dom == nil || dom.PortalSiteID == 0 {
		return
	}
	s, err := getSiteOrErr(dom.PortalSiteID)
	if err != nil {
		return
	}
	_, _ = DeleteSite(DeleteSiteReq{ID: s.ID, DelRoot: true})
	cleanupMailPortalLogo(dom.ID)
}

// ============================================================================
// 门户证书申请
// ============================================================================

// ApplyMailPortalCert 为已开启的门户站点申请（或重新申请）免费证书并启用 HTTPS。
// 支持一次为多个域名（门户域名 + 附加域名）签发同一张证书。
func ApplyMailPortalCert(domainID uint, req MailPortalCertReq) (*MailPortalView, error) {
	dom, err := MailDomainByID(domainID)
	if err != nil {
		return nil, err
	}
	if !dom.PortalEnabled || dom.PortalSiteID == 0 {
		return nil, errors.New("请先开启门户网站并保存，再申请证书")
	}
	site, err := getSiteOrErr(dom.PortalSiteID)
	if err != nil {
		return nil, errors.New("门户站点不存在，请重新保存门户配置")
	}
	options := mailPortalCertOptions(dom.PortalDomain, dom.PortalExtraDomains)
	domains := pickPortalCertDomains(options, req.Domains)
	brand := normalizeCertBrand(req.Brand)
	algo := normalizeCertAlgo(req.Algorithm)
	email := strings.TrimSpace(req.Email)
	if email == "" {
		email = strings.TrimSpace(dom.PortalCertEmail)
	}
	if err := issueACME(*site, brand, algo, domains, email); err != nil {
		return nil, errors.New("证书申请失败：" + err.Error() +
			"（请确认域名已解析到本服务器且 80 端口可访问）")
	}
	dom.PortalCertBrand = brand
	dom.PortalCertAlgo = algo
	dom.PortalCertEmail = email
	dom.PortalCertDomains = strings.Join(domains, ",")
	dom.PortalSSL = true
	if err := model.DB.Save(dom).Error; err != nil {
		return nil, err
	}
	return mailPortalView(dom), nil
}

// ============================================================================
// 门户 Logo 上传
// ============================================================================

// mailPortalLogoDir 门户 Logo 存储目录
func mailPortalLogoDir() string {
	return filepath.Join(config.Get().DataDir, "mailportal", "logo")
}

// mailPortalLogoAllowedExt 允许的 Logo 图片扩展名
var mailPortalLogoAllowedExt = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".webp": true, ".svg": true, ".ico": true, ".bmp": true,
}

// SaveMailPortalLogo 保存门户 Logo 文件，返回可直接引用的相对地址
// （如 /api/mail-portal/logo/3?v=1699999999，经门户反代片段由面板提供）。
func SaveMailPortalLogo(domainID uint, filename string, data []byte) (string, error) {
	if domainID == 0 {
		return "", errors.New("参数错误")
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if !mailPortalLogoAllowedExt[ext] {
		return "", errors.New("仅支持 png / jpg / jpeg / gif / webp / svg / ico / bmp 图片")
	}
	if len(data) == 0 {
		return "", errors.New("文件内容为空")
	}
	if len(data) > 2*1024*1024 {
		return "", errors.New("Logo 文件不能超过 2MB")
	}
	dir := mailPortalLogoDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// 清理旧 Logo
	cleanupMailPortalLogo(domainID)
	name := strconv.FormatUint(uint64(domainID), 10) + ext
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("/api/mail-portal/logo/%d?v=%d", domainID, time.Now().Unix()), nil
}

// cleanupMailPortalLogo 删除某域名已上传的 Logo 文件
func cleanupMailPortalLogo(domainID uint) {
	dir := mailPortalLogoDir()
	prefix := strconv.FormatUint(uint64(domainID), 10) + "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// MailPortalLogoFile 读取某域名的 Logo 文件内容（公开接口用），返回 Content-Type 与内容。
func MailPortalLogoFile(domainID uint) (string, []byte, error) {
	if domainID == 0 {
		return "", nil, errors.New("Logo 不存在")
	}
	dir := mailPortalLogoDir()
	prefix := strconv.FormatUint(uint64(domainID), 10) + "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", nil, errors.New("Logo 不存在")
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		ctype := "image/png"
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".jpg", ".jpeg":
			ctype = "image/jpeg"
		case ".gif":
			ctype = "image/gif"
		case ".webp":
			ctype = "image/webp"
		case ".svg":
			ctype = "image/svg+xml"
		case ".ico":
			ctype = "image/x-icon"
		case ".bmp":
			ctype = "image/bmp"
		}
		return ctype, data, nil
	}
	return "", nil, errors.New("Logo 不存在")
}

// ============================================================================
// 门户公开接口（无需登录，按访问 Host 定位邮箱域名）
// ============================================================================

// MailPortalDomainByHost 根据访问门户的 Host 找到对应邮箱域名。
func MailPortalDomainByHost(host string) (*model.MailDomain, error) {
	h := strings.ToLower(strings.TrimSpace(host))
	if i := strings.IndexByte(h, ':'); i >= 0 {
		h = h[:i]
	}
	h = strings.TrimSpace(h)
	if h == "" {
		return nil, errors.New("无效的访问域名")
	}
	var doms []model.MailDomain
	model.DB.Where("portal_enabled = ?", true).Find(&doms)
	for i := range doms {
		pd := strings.ToLower(strings.TrimSpace(doms[i].PortalDomain))
		if pd == "" {
			pd = "mail." + doms[i].Domain
		}
		if pd == h {
			return &doms[i], nil
		}
		for _, d := range strings.Split(doms[i].PortalExtraDomains, ",") {
			if strings.ToLower(strings.TrimSpace(d)) == h {
				return &doms[i], nil
			}
		}
	}
	return nil, errors.New("该域名未开启邮箱门户")
}

// MailPortalRegister 门户自助注册：在门户对应域名下创建邮箱账号。
func MailPortalRegister(host, name, password string) (*model.Mailbox, error) {
	dom, err := MailPortalDomainByHost(host)
	if err != nil {
		return nil, err
	}
	if !dom.Enabled {
		return nil, errors.New("该域名邮箱已停用")
	}
	if !dom.PortalRegister {
		return nil, errors.New("该站点未开放注册")
	}
	if strings.TrimSpace(password) == "" {
		return nil, errors.New("请设置密码")
	}
	if dom.MaxAccounts > 0 {
		var cnt int64
		model.DB.Model(&model.Mailbox{}).Where("domain_id = ?", dom.ID).Count(&cnt)
		if cnt >= dom.MaxAccounts {
			return nil, errors.New("该域名邮箱账号数已达上限，无法注册")
		}
	}
	return CreateMailbox(dom.ID, name, password, "门户注册", 0)
}

// MailPortalLogin 门户登录：校验邮箱账号密码，返回会话 token。
func MailPortalLogin(host, name, password string) (string, *model.Mailbox, error) {
	dom, err := MailPortalDomainByHost(host)
	if err != nil {
		return "", nil, err
	}
	if !dom.Enabled {
		return "", nil, errors.New("该域名邮箱已停用")
	}
	name = strings.TrimSpace(name)
	if at := strings.IndexByte(name, '@'); at >= 0 {
		name = name[:at]
	}
	if name == "" {
		return "", nil, errors.New("请输入邮箱账号")
	}
	var box model.Mailbox
	if err := model.DB.Where("domain_id = ? AND name = ?", dom.ID, name).First(&box).Error; err != nil {
		return "", nil, errors.New("账号或密码错误")
	}
	if !box.Enabled {
		return "", nil, errors.New("该账号已被停用")
	}
	if bcrypt.CompareHashAndPassword([]byte(box.PasswordHash), []byte(password)) != nil {
		return "", nil, errors.New("账号或密码错误")
	}
	token, err := issueMailboxToken(box.ID)
	if err != nil {
		return "", nil, err
	}
	return token, &box, nil
}

// ---- 邮箱会话 token（HMAC 签名，独立于面板管理员的 JWT）----

const mailboxTokenTTL = 7 * 24 * time.Hour

// mailboxTokenSecret 会话签名密钥（复用面板 JWT 密钥）
func mailboxTokenSecret() []byte {
	s := config.Get().Auth.JWTSecret
	if s == "" {
		s = "kypanel-mailbox-secret"
	}
	return []byte(s)
}

// issueMailboxToken 生成邮箱会话 token：mb.<base64(payload)>.<hmac>
func issueMailboxToken(mailboxID uint) (string, error) {
	exp := time.Now().Add(mailboxTokenTTL).Unix()
	payload := strconv.FormatUint(uint64(mailboxID), 10) + "." + strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, mailboxTokenSecret())
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return "mb." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + sig, nil
}

// ParseMailboxToken 校验并解析邮箱会话 token，返回 mailboxID。
func ParseMailboxToken(token string) (uint, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "mb" {
		return 0, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, false
	}
	fields := strings.Split(string(raw), ".")
	if len(fields) != 2 {
		return 0, false
	}
	id, err1 := strconv.ParseUint(fields[0], 10, 32)
	exp, err2 := strconv.ParseInt(fields[1], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	mac := hmac.New(sha256.New, mailboxTokenSecret())
	mac.Write(raw)
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(parts[2])) {
		return 0, false
	}
	if time.Now().Unix() > exp {
		return 0, false
	}
	return uint(id), true
}

// MailboxByPortalToken 按会话 token 取邮箱账号（校验有效性）。
func MailboxByPortalToken(token string) (*model.Mailbox, error) {
	id, ok := ParseMailboxToken(token)
	if !ok {
		return nil, errors.New("登录已过期，请重新登录")
	}
	var box model.Mailbox
	if err := model.DB.First(&box, id).Error; err != nil {
		return nil, errors.New("账号不存在")
	}
	if !box.Enabled {
		return nil, errors.New("账号已被停用")
	}
	return &box, nil
}
