package service

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"kypanel/internal/model"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

const (
	nginxConfDir  = "/etc/nginx/conf.d"
	nginxConfFile = "/etc/nginx/nginx.conf"
	webRootBase   = "/www/wwwroot" // 站点根目录基础路径
)

// 默认文档配置（PHP / 静态站点自动填充）
const (
	phpDefaultIndex    = "index.php index.html index.htm default.php default.htm default.html"
	staticDefaultIndex = "index.html index.htm default.html default.htm"
)

// defaultIndexForType 返回站点类型的默认文档配置
func defaultIndexForType(siteType string) string {
	switch siteType {
	case "php":
		return phpDefaultIndex
	case "static":
		return staticDefaultIndex
	default:
		return ""
	}
}

// 常见 GBK 乱码产生的生僻字（UTF-8 字节被按 GBK 解码时会出现）
var mojibakeMarks = []rune{'娴', '嬭', '瘯', '绔', '欑', '偣', '鏀', '瑰', '緢', '鎴', '愬', '犲', 'ソ', '腑', '鐐'}

// fixRemarkMojibake 尝试修复备注中 UTF-8 被误按 GBK 解码产生的乱码。
// 原理：把字符串按 UTF-8 编码成字节，再用 GBK 解码回 UTF-8；
// 仅当原文包含乱码特征字、解码成功且结果更合理时才返回修复值。
func fixRemarkMojibake(s string) string {
	if s == "" {
		return ""
	}
	b := []byte(s)
	if !utf8.Valid(b) {
		return s
	}
	// 统计原文是否包含典型乱码生僻字
	hasMark := false
	for _, r := range s {
		for _, m := range mojibakeMarks {
			if r == m {
				hasMark = true
				break
			}
		}
		if hasMark {
			break
		}
	}
	out, _, err := transform.Bytes(simplifiedchinese.GBK.NewDecoder(), b)
	if err != nil {
		return s
	}
	fixed := string(out)
	if !utf8.ValidString(fixed) || fixed == s {
		return s
	}
	// 只有在原文确实包含乱码特征字时才使用修复值，避免误伤正常中文
	if hasMark {
		return fixed
	}
	return s
}

// CreateSiteReq 创建网站请求（按站点类型动态填参）
type CreateSiteReq struct {
	Name      string `json:"name"` // 可选，留空时自动用主域名作为站点名称
	Domain    string `json:"domain" binding:"required"`
	Port      int    `json:"port"`
	Type      string `json:"type" binding:"required,oneof=static php node python go proxy"`
	Root      string `json:"root"` // 静态/PHP 根目录 或 python/node/go 项目路径
	ProxyPass string `json:"proxy_pass"`
	Remark    string `json:"remark"`

	// PHP / 运行环境公共
	RuntimeVersion string `json:"runtime_version"` // PHP 版本（如 PHP 8.2）/ Python 3.11 / Node 18 / Go 1.22

	// python/node/go 项目参数
	StartCommand string `json:"start_command"` // 启动命令，如 python app.py / npm run start
	EnvVars      string `json:"env_vars"`      // 环境变量，KEY=VALUE 每行一个
	ProxyPort    int    `json:"proxy_port"`    // 应用运行端口（nginx 反代目标）
	Framework    string `json:"framework"`     // python 框架：flask / django / generic

	// PHP 可选：创建数据库
	CreateDB   bool   `json:"create_db"`
	DBName     string `json:"db_name"`
	DBUser     string `json:"db_user"`
	DBPassword string `json:"db_password"`

	// PHP 可选：创建 FTP
	CreateFTP   bool   `json:"create_ftp"`
	FTPUsername string `json:"ftp_username"`
	FTPPassword string `json:"ftp_password"`
}

// SiteItem 网站列表项
type SiteItem struct {
	model.Site
	Active    string `json:"active"`     // 服务运行状态
	SSLStatus string `json:"ssl_status"` // SSL 状态描述
	SSLDays   int    `json:"ssl_days"`   // 剩余天数，-1 表示未部署
	IsDefault bool   `json:"is_default"` // 是否为默认站点（未绑定域名访问该站点）
}

// 站点名称允许字母、数字、点（域名）、下划线、中划线
var siteNameRe = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,64}$`)

// nginxAvailable 检测 nginx 是否已安装
func nginxAvailable() error {
	if _, err := exec.LookPath("nginx"); err != nil {
		return errors.New("Nginx 未安装，请先在「应用商店」安装 Nginx")
	}
	return nil
}

// nginxTest 校验 nginx 配置
func nginxTest() error {
	res, err := ExecCommand("nginx -t", 30*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("nginx 配置校验失败: %s", strings.TrimSpace(res.Stderr+res.Stdout))
	}
	return nil
}

// nginxReload 重载 nginx
func nginxReload() error {
	res, err := ExecCommand("nginx -s reload", 30*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("nginx reload 失败: %s", strings.TrimSpace(res.Stderr+res.Stdout))
	}
	return nil
}

// ============================ 站点证书自愈 ============================
//
// 历史故障：某个站点的证书文件若缺失/损坏/被写成占位符（如 1 字节内容），
// nginx -t 会全局失败，导致 nginx 起不来、所有网站一起挂掉，面板升级/重启后也起不来。
// 自愈策略：任何配置校验前自动检测并修复无效证书（生成自签证书兜底），
// 确保单个站点的坏证书不再拖垮整个 Web 服务器。

// validCertKeyPair 校验证书与私钥文件是否构成有效的 PEM 证书对
func validCertKeyPair(certPath, keyPath string) bool {
	if certPath == "" || keyPath == "" {
		return false
	}
	certData, err := os.ReadFile(certPath)
	if err != nil || len(bytes.TrimSpace(certData)) == 0 {
		return false
	}
	certBlock, _ := pem.Decode(certData)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		return false
	}
	if _, err := x509.ParseCertificate(certBlock.Bytes); err != nil {
		return false
	}
	keyData, err := os.ReadFile(keyPath)
	if err != nil || len(bytes.TrimSpace(keyData)) == 0 {
		return false
	}
	keyBlock, _ := pem.Decode(keyData)
	if keyBlock == nil {
		return false
	}
	if _, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes); err == nil {
		return true
	}
	if _, err := x509.ParseECPrivateKey(keyBlock.Bytes); err == nil {
		return true
	}
	if _, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes); err == nil {
		return true
	}
	return false
}

// writeSelfSignedCert 为站点生成自签证书（ECDSA P-256），覆盖写入证书与私钥文件
func writeSelfSignedCert(certPath, keyPath string, names []string) error {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return errors.New("生成自签密钥失败: " + err.Error())
	}
	if err := os.MkdirAll(filepath.Dir(certPath), 0o755); err != nil {
		return err
	}
	cn := "kypanel"
	if len(names) > 0 {
		cn = names[0]
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(1, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              names,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return errors.New("生成自签证书失败: " + err.Error())
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return err
	}
	return nil
}

// ensureSiteSSLCert 保证启用 HTTPS 的站点证书文件有效；无效时自动生成自签证书并落库路径。
// 调用方持有 s 的内存指针，路径修复同样写回 s，保证后续生成配置时引用到有效路径。
func ensureSiteSSLCert(s *model.Site) error {
	if s == nil || !s.SslEnabled {
		return nil
	}
	certPath, keyPath := s.SslCertPath, s.SslKeyPath
	if certPath == "" || keyPath == "" {
		cp, kp := siteSSLPath(s.Name)
		if certPath == "" {
			certPath = cp
		}
		if keyPath == "" {
			keyPath = kp
		}
	}
	if validCertKeyPair(certPath, keyPath) {
		if s.SslCertPath != certPath || s.SslKeyPath != keyPath {
			s.SslCertPath, s.SslKeyPath = certPath, keyPath
			_ = model.DB.Model(s).Updates(map[string]any{"ssl_cert_path": certPath, "ssl_key_path": keyPath}).Error
		}
		return nil
	}
	if err := writeSelfSignedCert(certPath, keyPath, siteServerNames(s)); err != nil {
		return err
	}
	s.SslCertPath, s.SslKeyPath = certPath, keyPath
	return model.DB.Model(s).Updates(map[string]any{"ssl_cert_path": certPath, "ssl_key_path": keyPath}).Error
}

// selfHealAllSiteCerts 遍历所有启用 HTTPS 的站点，修复无效证书（自签兜底）
func selfHealAllSiteCerts() error {
	sites, err := model.ListSites()
	if err != nil {
		return err
	}
	for i := range sites {
		if err := ensureSiteSSLCert(&sites[i]); err != nil {
			return err
		}
	}
	return nil
}

// ensureNginxRunning 若 nginx 未运行则尝试拉起（systemd 优先，无 systemd 时直接启动）
func ensureNginxRunning() error {
	res, err := ExecCommand("pgrep -x nginx >/dev/null 2>&1 && echo 1 || echo 0", 10*time.Second)
	if err == nil && strings.Contains(res.Stdout, "1") {
		return nil
	}
	cmd := "systemctl start nginx"
	if _, err := exec.LookPath("systemctl"); err != nil {
		cmd = "nginx"
	}
	_, err = ExecCommand(cmd, 30*time.Second)
	return err
}

// SelfHealWebServerOnBoot 面板启动时自愈 Web 服务器：
//  1. 修复所有启用 HTTPS 站点的无效证书（防止坏证书导致 nginx 全局校验失败、网站全挂）
//  2. 配置校验通过且 nginx 未运行时自动拉起，确保面板升级/重启后网站自动恢复
func SelfHealWebServerOnBoot() {
	if WebServerType() != webNginx && WebServerType() != webApache {
		return
	}
	if err := selfHealAllSiteCerts(); err != nil {
		return
	}
	if WebServerType() == webNginx {
		if err := webConfigTest(); err == nil {
			_ = ensureNginxRunning()
		}
	}
}

// siteConfPath 返回站点配置文件路径
func siteConfPath(name string) string {
	return filepath.Join(nginxConfDir, "lp_"+name+".conf")
}

// siteServerNames 返回 server_name 列表（主域名 + 附加域名，排除带端口的绑定）
func siteServerNames(s *model.Site) []string {
	var names []string
	for _, d := range siteAllBindings(s) {
		if _, _, hasPort := splitHostPort(d); hasPort {
			continue // 带端口的绑定单独生成 server 块，不进 server_name
		}
		names = append(names, d)
	}
	return names
}

// siteAllBindings 返回站点全部绑定（主域名 + 附加域名，含带端口），去重
func siteAllBindings(s *model.Site) []string {
	var names []string
	seen := map[string]bool{}
	if d := strings.TrimSpace(s.Domain); d != "" {
		names = append(names, d)
		seen[d] = true
	}
	for _, d := range strings.Split(s.Domains, ",") {
		d = strings.TrimSpace(d)
		if d != "" && !seen[d] {
			names = append(names, d)
			seen[d] = true
		}
	}
	return names
}

// sitePortBindings 返回带端口的绑定列表（如 127.0.0.1:8899、example.com:8443）
func sitePortBindings(s *model.Site) []struct {
	Host string
	Port string
} {
	var out []struct {
		Host string
		Port string
	}
	for _, d := range siteAllBindings(s) {
		if host, port, hasPort := splitHostPort(d); hasPort {
			out = append(out, struct {
				Host string
				Port string
			}{host, port})
		}
	}
	return out
}

// genSiteConf 生成站点配置（含全部设置项；自定义配置非空时完全覆盖）
// 根据当前 Web 服务器类型自动生成 nginx 或 apache 配置
func genSiteConf(s *model.Site) string {
	if strings.TrimSpace(s.ConfigOverride) != "" {
		return s.ConfigOverride
	}
	if WebServerType() == webApache {
		return genApacheConf(s)
	}

	var sb strings.Builder
	if s.SslEnabled {
		// HTTPS：80 块（可选强制跳转）+ 443 SSL 块
		sb.WriteString("# kypanel site: " + s.Name + " (HTTP)\n")
		if s.SslForce {
			sb.WriteString("server {\n")
			fmt.Fprintf(&sb, "    listen %d", 80)
			if isDefaultSite(s) {
				sb.WriteString(" default_server")
			}
			sb.WriteString(";\n")
			fmt.Fprintf(&sb, "    server_name %s;\n", strings.Join(siteServerNames(s), " "))
			sb.WriteString("    return 301 https://$host$request_uri;\n")
			sb.WriteString("}\n\n")
		} else {
			sb.WriteString(genSiteServerBlock(s, 80, false, isDefaultSite(s)))
		}
		sb.WriteString("# kypanel site: " + s.Name + " (HTTPS)\n")
		sb.WriteString(genSiteServerBlock(s, 443, true, isDefaultSite(s)))
	} else {
		sb.WriteString(genSiteServerBlock(s, s.Port, false, isDefaultSite(s)))
	}

	// 带端口的绑定（IP:端口 / 域名:端口）生成独立 server 块，仅监听该端口
	for _, b := range sitePortBindings(s) {
		sb.WriteString("\n# kypanel site: " + s.Name + " (" + b.Host + ":" + b.Port + ")\n")
		sb.WriteString(genPortServerBlock(s, b.Host, b.Port))
	}
	return sb.String()
}

// genPortServerBlock 生成「IP:端口 / 域名:端口」的独立 server 块
// 复用完整站点逻辑，但 server_name 只绑定该 host，监听指定端口（明文 HTTP）。
func genPortServerBlock(s *model.Site, host, port string) string {
	p, _ := strconv.Atoi(port)
	// 构造临时副本，仅绑定该 host（避免把其他域名也塞进 server_name）
	cp := *s
	cp.Domain = host
	cp.Domains = ""
	return genSiteServerBlock(&cp, p, false, false)
}

// isDefaultSite 站点是否为默认站点（未绑定域名请求落到该站点）
func isDefaultSite(s *model.Site) bool {
	return s.ID != 0 && DefaultSiteID() == s.ID
}

// genSiteServerBlock 生成单个 server 块；defaultSrv 为 true 时在 listen 加 default_server
// （默认站点：未绑定域名请求落到该站点）
func genSiteServerBlock(s *model.Site, port int, ssl bool, defaultSrv bool) string {
	var sb strings.Builder
	sb.WriteString("server {\n")
	if ssl {
		line := "    listen 443 ssl http2"
		if defaultSrv {
			line += " default_server"
		}
		sb.WriteString(line + ";\n")
		if s.SslCertPath != "" {
			fmt.Fprintf(&sb, "    ssl_certificate %s;\n", s.SslCertPath)
		}
		if s.SslKeyPath != "" {
			fmt.Fprintf(&sb, "    ssl_certificate_key %s;\n", s.SslKeyPath)
		}
		sb.WriteString("    ssl_protocols TLSv1.2 TLSv1.3;\n")
		sb.WriteString("    ssl_ciphers HIGH:!aNULL:!MD5;\n")
		sb.WriteString("    ssl_session_cache shared:SSL:10m;\n")
		sb.WriteString("    ssl_session_timeout 10m;\n\n")
	} else {
		line := fmt.Sprintf("    listen %d", port)
		if defaultSrv {
			line += " default_server"
		}
		sb.WriteString(line + ";\n")
	}
	fmt.Fprintf(&sb, "    server_name %s;\n\n", strings.Join(siteServerNames(s), " "))

	// 站点级 IP 黑名单（deny 指令在 server 块头部，allow all 在尾部由现有 root/location 块保证 fall-through）
	if blockIP := siteBlockIPDirectives(s.ID); blockIP != "" {
		sb.WriteString(blockIP)
		sb.WriteString("\n")
	}

	// 重定向规则（先于 location 处理）
	// 优先使用多条规则 s.Redirects；为空则回退旧的单条 s.RedirectURL
	if len(s.Redirects) > 0 {
		for _, r := range s.Redirects {
			genRedirectRule(&sb, r)
		}
	} else if u := strings.TrimSpace(s.RedirectURL); u != "" {
		code := s.RedirectCode
		if code != 301 && code != 302 && code != 307 && code != 308 {
			code = 301
		}
		if s.RedirectKeepPath {
			fmt.Fprintf(&sb, "    return %d %s$request_uri;\n\n", code, u)
		} else {
			fmt.Fprintf(&sb, "    return %d %s;\n\n", code, u)
		}
	}

	// 根目录 + 默认文档（静态 / PHP）
	root := s.Root
	if model.IsRootType(s.Type) {
		if rd := strings.TrimSpace(s.RuntimeDir); rd != "" {
			root = filepath.Join(root, strings.Trim(rd, "/"))
		}
		fmt.Fprintf(&sb, "    root %s;\n", root)
		index := strings.TrimSpace(s.DefaultIndex)
		if index == "" {
			index = defaultIndexForType(s.Type)
		}
		fmt.Fprintf(&sb, "    index %s;\n", index)
		fmt.Fprintf(&sb, "    error_page 404 /404.html;\n\n")

		// 敏感文件/目录保护（.env、.git、.htaccess 等禁止访问）
		sb.WriteString("    location ~ ^/(\\.user\\.ini|\\.htaccess|\\.git|\\.env|\\.svn|\\.project|LICENSE|README\\.md) {\n")
		sb.WriteString("        return 404;\n")
		sb.WriteString("    }\n\n")
	}

	// Let's Encrypt 证书验证目录（HTTP-01 challenge 需要可达）
	// root 类型：跟随 server root（含运行目录 RuntimeDir）；
	// 反代类型（node/python/go/proxy）：指向站点根目录 s.Root，避免验证请求被反代到后端
	sb.WriteString("    location ^~ /.well-known/ {\n")
	fmt.Fprintf(&sb, "        root %s;\n", root)
	sb.WriteString("        allow all;\n")
	sb.WriteString("    }\n\n")

	switch s.Type {
	case model.SiteTypeStatic:
		sb.WriteString("    location / {\n")
		// 配置了伪静态规则时由规则接管（如框架路由/SPA history），
		// 不再追加默认 try_files，避免规则被 try_files 短路而失效。
		if rw := strings.TrimSpace(s.Rewrite); rw != "" {
			sb.WriteString(rw + "\n")
		} else {
			sb.WriteString("        try_files $uri $uri/ =404;\n")
		}
		sb.WriteString("    }\n\n")
	case model.SiteTypePHP:
		sb.WriteString("    location / {\n")
		// 伪静态规则存在时优先由规则处理，规则内自带 try_files/rewrite
		if rw := strings.TrimSpace(s.Rewrite); rw != "" {
			sb.WriteString(rw + "\n")
		} else {
			sb.WriteString("        try_files $uri $uri/ =404;\n")
		}
		sb.WriteString("    }\n\n")
		sb.WriteString("    location ~ \\.php$ {\n")
		// PHP 文件不存在时不要让 FPM 返回 "File not found."，
		// 而是让 Nginx 404，这样 index 指令才能继续尝试 index.html 等后续默认文档。
		sb.WriteString("        try_files $uri =404;\n")
		fpm := strings.TrimSpace(s.PhpFpm)
		if fpm == "" {
			fpm = defaultPhpFpm()
		}
		fmt.Fprintf(&sb, "        fastcgi_pass %s;\n", fpm)
		sb.WriteString("        fastcgi_index index.php;\n")
		sb.WriteString("        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;\n")
		sb.WriteString("        include fastcgi_params;\n")
		sb.WriteString("    }\n\n")
	default: // node / python / go / proxy：反向代理到本地端口或任意 URL
		sb.WriteString("    location / {\n")
		fmt.Fprintf(&sb, "        proxy_pass %s;\n", s.ProxyPass)
		sb.WriteString("        proxy_set_header Host $host;\n")
		sb.WriteString("        proxy_set_header X-Real-IP $remote_addr;\n")
		sb.WriteString("        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n")
		sb.WriteString("        proxy_set_header X-Forwarded-Proto $scheme;\n")
		sb.WriteString("        proxy_connect_timeout 60s;\n")
		sb.WriteString("        proxy_read_timeout 300s;\n")
		sb.WriteString("    }\n\n")
	}

	// 防盗链（仅对静态资源生效）
	if s.HotlinkEnabled && strings.TrimSpace(s.HotlinkReferers) != "" {
		sb.WriteString("    location ~ \\.(gif|jpg|jpeg|png|bmp|swf|webp|ico|css|js|woff2?|ttf|svg|mp4|mp3)$ {\n")
		sb.WriteString("        valid_referers none blocked " + strings.ReplaceAll(s.HotlinkReferers, ",", " ") + ";\n")
		sb.WriteString("        if ($invalid_referer) {\n")
		sb.WriteString("            return 403;\n")
		sb.WriteString("        }\n")
		sb.WriteString("    }\n\n")
	}

	// 静态资源缓存（仅静态 / PHP 站点，图片 30 天、JS/CSS 12 小时）
	if s.CacheEnabled && model.IsRootType(s.Type) {
		sb.WriteString("    location ~ \\.(gif|jpg|jpeg|png|bmp|swf|webp|ico|svg|woff2?|ttf|eot)$ {\n")
		sb.WriteString("        expires 30d;\n")
		sb.WriteString("        add_header Cache-Control \"public, max-age=2592000\";\n")
		sb.WriteString("        access_log off;\n")
		sb.WriteString("    }\n\n")
		sb.WriteString("    location ~ \\.(js|css)$ {\n")
		sb.WriteString("        expires 12h;\n")
		sb.WriteString("        add_header Cache-Control \"public, max-age=43200\";\n")
		sb.WriteString("        access_log off;\n")
		sb.WriteString("    }\n\n")
	}

	// 流量限制
	if s.RateLimitKbs > 0 {
		fmt.Fprintf(&sb, "    limit_rate %dk;\n", s.RateLimitKbs)
	}

	// 安全响应头（防跨站 / 防 MIME 嗅探 / 防点击劫持）
	if s.SecurityHeaders {
		sb.WriteString("    add_header X-Frame-Options \"SAMEORIGIN\" always;\n")
		sb.WriteString("    add_header X-Content-Type-Options \"nosniff\" always;\n")
		sb.WriteString("    add_header X-XSS-Protection \"1; mode=block\" always;\n")
		sb.WriteString("    add_header Referrer-Policy \"strict-origin-when-cross-origin\" always;\n")
	}

	// HSTS：HTTPS 站点强制浏览器走 HTTPS，防止 SSL 降级攻击
	if ssl {
		sb.WriteString("    add_header Strict-Transport-Security \"max-age=31536000; includeSubDomains\" always;\n")
	}

	fmt.Fprintf(&sb, "    access_log /var/log/nginx/%s.access.log;\n", s.Name)
	fmt.Fprintf(&sb, "    error_log  /var/log/nginx/%s.error.log;\n", s.Name)
	// WAF 规则片段（存在则 include，不存在为空）
	if inc := wafSiteConfIncludeLine(s.Name, s.ID); inc != "" {
		sb.WriteString(inc)
	}
	// 单站安全片段（存在则 include，不存在为空）
	if inc := SiteSecIncludeLine(s.ID); inc != "" {
		sb.WriteString(inc)
	}
	sb.WriteString("}\n")
	return sb.String()
}

// genRedirectRule 生成单条重定向规则到 server 块
// 按域名：if ($host = xxx) { return ...; }（可多域名）
// 按目录：location ^~ /dir/ { return ...; }（可多目录）
func genRedirectRule(sb *strings.Builder, r model.SiteRedirect) {
	target := strings.TrimSpace(r.TargetURL)
	if target == "" {
		return
	}
	code := r.Code
	if code != 301 && code != 302 && code != 307 && code != 308 {
		code = 301
	}
	returnExpr := func() string {
		if r.KeepPath {
			return fmt.Sprintf("return %d %s$request_uri;", code, target)
		}
		return fmt.Sprintf("return %d %s;", code, target)
	}

	switch r.MatchType {
	case model.RedirectMatchDomain:
		for _, d := range splitCSV(r.Domains) {
			fmt.Fprintf(sb, "    if ($host = %s) {\n", d)
			fmt.Fprintf(sb, "        %s\n", returnExpr())
			sb.WriteString("    }\n")
		}
		sb.WriteString("\n")
	case model.RedirectMatchDir:
		for _, p := range splitCSV(r.Paths) {
			p = strings.TrimRight(p, "/")
			if p == "" {
				continue
			}
			fmt.Fprintf(sb, "    location ^~ %s {\n", p)
			fmt.Fprintf(sb, "        %s\n", returnExpr())
			sb.WriteString("    }\n")
		}
		sb.WriteString("\n")
	}
}

// defaultSiteIndex 默认首页（index.html）
const defaultSiteIndex = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>网站创建成功 - %s</title>
<style>
body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;
background:linear-gradient(135deg,#667eea,#764ba2);font-family:system-ui,sans-serif;color:#fff}
.card{text-align:center;padding:24px}
h1{font-size:42px;margin-bottom:12px}
p{opacity:.85;font-size:16px}
</style>
</head>
<body>
<div class="card">
<h1>网站创建成功</h1>
<p>站点 %s 已创建，本页面由 kypanel 自动生成。</p>
</div>
</body>
</html>
`

// default404Page 默认 404 页面（404.html）
const default404Page = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>页面不存在 - %s</title>
<style>
body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;
background:linear-gradient(135deg,#2d3748,#4a5568);font-family:system-ui,sans-serif;color:#fff}
.card{text-align:center;padding:24px}
.code{font-size:72px;font-weight:700;margin-bottom:8px;opacity:.9}
h1{font-size:28px;margin:0 0 12px}
p{opacity:.85;font-size:16px}
</style>
</head>
<body>
<div class="card">
<div class="code">404</div>
<h1>页面不存在</h1>
<p>您访问的页面不存在或已被移动，请检查地址后重试。</p>
</div>
</body>
</html>
`

// writeDefaultPages 生成默认首页与 404 页面（目标文件已存在时不覆盖）。
// 优先复制全局默认页面模板（default_pages，仅影响新建站点），读取失败时回退内置模板。
func writeDefaultPages(root, name string) {
	writeDefaultPageIfMissing(root, "index.html", DefaultPageIndex, func() string {
		return fmt.Sprintf(defaultSiteIndex, name, name)
	})
	writeDefaultPageIfMissing(root, "404.html", DefaultPage404, func() string {
		return fmt.Sprintf(default404Page, name)
	})
}

// writeDefaultPageIfMissing 目标文件不存在时写入：优先全局默认页面内容，其次内置模板
func writeDefaultPageIfMissing(root, fileName, kind string, fallback func() string) {
	dst := filepath.Join(root, fileName)
	if _, err := os.Stat(dst); err == nil {
		return
	}
	content := fallback()
	if b, err := os.ReadFile(defaultPagePath(kind)); err == nil && len(b) > 0 {
		content = string(b)
	}
	_ = os.WriteFile(dst, []byte(content), 0o755)
}

// isRuntimeSite 是否为进程型站点（python/node/go，由 systemd 守护）
func isRuntimeSite(t string) bool {
	return t == model.SiteTypeNode || t == model.SiteTypePython || t == model.SiteTypeGo
}

// ensureRuntime 校验运行环境已安装（支持多版本）
// runtimeVersion 格式：PHP 8.2 / Python 3.12 / Node 20 / Go 1.23
func ensureRuntime(t string, runtimeVersion string) error {
	switch t {
	case model.SiteTypePHP:
		bin := "php"
		if runtimeVersion != "" {
			// 尝试 php8.2 等版本特定二进制（兼容 "PHP 8.2.33" 完整版本号，归约为主.次版本）
			v := phpMinorVersion(runtimeVersion)
			if v == "" {
				v = strings.TrimPrefix(runtimeVersion, "PHP ")
			}
			if v != "" && v != runtimeVersion {
				bin = "php" + v
			}
		}
		if _, err := exec.LookPath(bin); err != nil {
			return errors.New("未检测到 " + bin + "，请先在「应用商店」安装对应 PHP 版本")
		}
	case model.SiteTypePython:
		if runtimeVersion != "" {
			v := strings.TrimPrefix(runtimeVersion, "Python ")
			// 尝试 pyenv 路径
			pyenvBin := filepath.Join(os.Getenv("HOME"), ".pyenv", "versions", v, "bin", "python"+v)
			if _, err := os.Stat(pyenvBin); err == nil {
				return nil
			}
			// 尝试 /usr/local/python<ver>
			localBin := "/usr/local/python" + v + "/bin/python" + v
			if _, err := os.Stat(localBin); err == nil {
				return nil
			}
		}
		if _, err := exec.LookPath("python3"); err != nil {
			return errors.New("未检测到 python3，请先在「应用商店」安装 Python 环境")
		}
	case model.SiteTypeNode:
		if runtimeVersion != "" {
			v := strings.TrimPrefix(runtimeVersion, "Node ")
			// 尝试 nvm 路径
			nvmPattern := filepath.Join(os.Getenv("HOME"), ".nvm", "versions", "node", "v"+v+".*", "bin", "node")
			matches, _ := filepath.Glob(nvmPattern)
			if len(matches) > 0 {
				return nil
			}
			// 尝试 /usr/local/node<ver>
			localBin := "/usr/local/node" + v + "/bin/node"
			if _, err := os.Stat(localBin); err == nil {
				return nil
			}
		}
		if _, err := exec.LookPath("node"); err != nil {
			return errors.New("未检测到 node，请先在「应用商店」安装 Node.js 环境")
		}
	case model.SiteTypeGo:
		if runtimeVersion != "" {
			v := strings.TrimPrefix(runtimeVersion, "Go ")
			// 尝试 /usr/local/go<ver>
			localBin := "/usr/local/go" + v + "/bin/go"
			if _, err := os.Stat(localBin); err == nil {
				return nil
			}
		}
		if _, err := exec.LookPath("go"); err != nil {
			return errors.New("未检测到 go，请先在「应用商店」安装 Golang 环境")
		}
	}
	return nil
}

// runtimeVersionOf 检测本机已安装的运行环境版本
func runtimeVersionOf(t string) string {
	switch t {
	case model.SiteTypePHP:
		res, err := ExecCommand("php -v", 15*time.Second)
		if err == nil && res.ExitCode == 0 {
			fields := strings.Fields(res.Stdout)
			if len(fields) >= 2 && strings.HasPrefix(fields[0], "PHP") {
				return fields[0] + " " + fields[1]
			}
		}
	case model.SiteTypePython:
		res, err := ExecCommand("python3 --version", 15*time.Second)
		if err == nil && res.ExitCode == 0 {
			return strings.TrimSpace(res.Stdout)
		}
	case model.SiteTypeNode:
		res, err := ExecCommand("node -v", 15*time.Second)
		if err == nil && res.ExitCode == 0 {
			return "Node " + strings.TrimSpace(res.Stdout)
		}
	case model.SiteTypeGo:
		res, err := ExecCommand("go version", 15*time.Second)
		if err == nil && res.ExitCode == 0 {
			fields := strings.Fields(res.Stdout)
			if len(fields) >= 3 {
				return fields[2]
			}
		}
	}
	return ""
}

func siteServiceName(name string) string { return "lp-" + name }

func siteServicePath(name string) string {
	return "/etc/systemd/system/" + siteServiceName(name) + ".service"
}

func siteRunnerPath(name string) string { return "/www/.lp-run/" + name + ".sh" }

// writeSiteService 为 python/node/go 站点生成 systemd 服务 + 启动脚本并启动
func writeSiteService(s *model.Site) error {
	runnerDir := "/www/.lp-run"
	if err := os.MkdirAll(runnerDir, 0o755); err != nil {
		return err
	}
	var sb strings.Builder
	sb.WriteString("#!/bin/bash\n")
	// 根据 RuntimeVersion 设置对应版本的环境变量
	if s.RuntimeVersion != "" {
		switch s.Type {
		case model.SiteTypeNode:
			v := strings.TrimPrefix(s.RuntimeVersion, "Node ")
			// nvm 路径
			nvmPattern := filepath.Join(os.Getenv("HOME"), ".nvm", "versions", "node", "v"+v+".*", "bin")
			matches, _ := filepath.Glob(nvmPattern)
			if len(matches) > 0 {
				fmt.Fprintf(&sb, "export PATH=\"%s:$PATH\"\n", matches[0])
			} else if _, err := os.Stat("/usr/local/node" + v + "/bin/node"); err == nil {
				fmt.Fprintf(&sb, "export PATH=\"/usr/local/node%s/bin:$PATH\"\n", v)
			}
		case model.SiteTypePython:
			v := strings.TrimPrefix(s.RuntimeVersion, "Python ")
			pyenvBin := filepath.Join(os.Getenv("HOME"), ".pyenv", "versions", v, "bin")
			if _, err := os.Stat(pyenvBin); err == nil {
				fmt.Fprintf(&sb, "export PATH=\"%s:$PATH\"\n", pyenvBin)
			} else if _, err := os.Stat("/usr/local/python" + v); err == nil {
				fmt.Fprintf(&sb, "export PATH=\"/usr/local/python%s/bin:$PATH\"\n", v)
			}
		case model.SiteTypeGo:
			v := strings.TrimPrefix(s.RuntimeVersion, "Go ")
			if _, err := os.Stat("/usr/local/go" + v); err == nil {
				fmt.Fprintf(&sb, "export PATH=\"/usr/local/go%s/bin:$PATH\"\n", v)
				fmt.Fprintf(&sb, "export GOROOT=\"/usr/local/go%s\"\n", v)
			}
		}
	}
	for _, line := range strings.Split(s.EnvVars, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "=") {
			fmt.Fprintf(&sb, "export %s\n", line)
		}
	}
	if s.Root != "" {
		fmt.Fprintf(&sb, "cd %s\n", s.Root)
	}
	fmt.Fprintf(&sb, "exec %s\n", s.StartCommand)
	if err := os.WriteFile(siteRunnerPath(s.Name), []byte(sb.String()), 0o755); err != nil {
		return err
	}

	unit := fmt.Sprintf(`[Unit]
Description=kypanel site %s
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=%s
ExecStart=/bin/bash %s
Restart=always
RestartSec=3
StandardOutput=append:/var/log/nginx/%s.service.log
StandardError=append:/var/log/nginx/%s.service.log

[Install]
WantedBy=multi-user.target
`, s.Name, s.Root, siteRunnerPath(s.Name), s.Name, s.Name)
	if err := os.WriteFile(siteServicePath(s.Name), []byte(unit), 0o644); err != nil {
		return err
	}
	res, err := ExecCommand(fmt.Sprintf("systemctl daemon-reload && systemctl enable --now %s", siteServiceName(s.Name)), 30*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(strings.TrimSpace(res.Stderr))
	}
	return nil
}

// removeSiteService 停止并删除 python/node/go 站点的 systemd 服务
func removeSiteService(name string) {
	_ = os.Remove(siteRunnerPath(name))
	_, _ = ExecCommand(fmt.Sprintf("systemctl disable --now %s 2>/dev/null; rm -f %s; systemctl daemon-reload", siteServiceName(name), siteServicePath(name)), 30*time.Second)
}

// siteServiceActive 查询 python/node/go 站点进程服务是否运行
func siteServiceActive(name string) bool {
	res, err := ExecCommand(fmt.Sprintf("systemctl is-active %s", siteServiceName(name)), 10*time.Second)
	return err == nil && res != nil && res.ExitCode == 0 && strings.TrimSpace(res.Stdout) == "active"
}

// writeSiteConf 写入站点配置并校验 Web 服务器，成功返回 nil
// genStoppedSiteConf 生成站点「停止」后的占位配置（nginx）：
// 保留 server_name 接管该域名请求，统一返回 503 并显示全局停用页 stop.html，
// 让访问者明确知道「网站已停止」而非「网站不存在」。停止只是逻辑停用，
// server 块仍占用域名，避免域名掉到 default_server 显示无关内容。
func genStoppedSiteConf(s *model.Site) string {
	names := strings.Join(siteServerNames(s), " ")
	dir := DefaultPagesDir()
	var sb strings.Builder
	sb.WriteString("# kypanel site: " + s.Name + " (STOPPED)\n")
	// HTTP 停用块
	sb.WriteString("server {\n")
	fmt.Fprintf(&sb, "    listen %d;\n", s.Port)
	fmt.Fprintf(&sb, "    server_name %s;\n", names)
	fmt.Fprintf(&sb, "    root %s;\n", dir)
	// stop.html 精确匹配 + internal：error_page 内部跳转专用，防止与 location /
	// 的 return 503 形成无限重定向循环（否则浏览器拿到的是 nginx 默认错误页而非 stop.html）
	sb.WriteString("    location = /stop.html { internal; }\n")
	sb.WriteString("    location / {\n")
	sb.WriteString("        return 503;\n")
	sb.WriteString("    }\n")
	sb.WriteString("    error_page 503 /stop.html;\n")
	sb.WriteString("}\n")
	// HTTPS 停用块（站点已部署证书时保留 443 接管，避免 https 请求落到其他站点）
	if s.SslEnabled {
		sb.WriteString("server {\n")
		sb.WriteString("    listen 443 ssl;\n")
		fmt.Fprintf(&sb, "    server_name %s;\n", names)
		if s.SslCertPath != "" {
			fmt.Fprintf(&sb, "    ssl_certificate %s;\n", s.SslCertPath)
		}
		if s.SslKeyPath != "" {
			fmt.Fprintf(&sb, "    ssl_certificate_key %s;\n", s.SslKeyPath)
		}
		fmt.Fprintf(&sb, "    root %s;\n", dir)
		sb.WriteString("    location = /stop.html { internal; }\n")
		sb.WriteString("    location / {\n")
		sb.WriteString("        return 503;\n")
		sb.WriteString("    }\n")
		sb.WriteString("    error_page 503 /stop.html;\n")
		sb.WriteString("}\n")
	}
	return sb.String()
}

// writeStoppedSiteConf 写入站点停用配置（nginx），配置校验失败自动回滚
func writeStoppedSiteConf(s *model.Site) error {
	conf := genStoppedSiteConf(s)
	path := siteConfPath(s.Name)
	if err := os.MkdirAll(nginxConfDir, 0o755); err != nil {
		return err
	}
	var backup []byte
	if old, err := os.ReadFile(path); err == nil {
		backup = old
	}
	if err := os.WriteFile(path, []byte(conf), 0o644); err != nil {
		return err
	}
	if err := webConfigTest(); err != nil {
		if backup != nil {
			_ = os.WriteFile(path, backup, 0o644)
		} else {
			_ = os.Remove(path)
		}
		return err
	}
	return nil
}

func writeSiteConf(s *model.Site) error {
	// 自愈：启用 HTTPS 的站点若证书文件缺失/无效（如被误写成占位符），
	// 自动生成自签证书兜底，避免 nginx 全局配置校验失败拖垮所有网站
	if s.SslEnabled {
		_ = ensureSiteSSLCert(s)
	}
	conf := genSiteConf(s)
	ws := WebServerType()

	var dir, path string
	if ws == webApache {
		dir = apacheSitesAvailable
		path = siteConfPathFor(s.Name, ws)
	} else {
		dir = nginxConfDir
		path = siteConfPath(s.Name)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// 备份现有配置以便回滚
	var backup []byte
	if old, err := os.ReadFile(path); err == nil {
		backup = old
	}
	if err := os.WriteFile(path, []byte(conf), 0o644); err != nil {
		return err
	}

	// apache：确保所需模块已启用 + a2ensite 启用站点
	if ws == webApache {
		if err := ensureApacheModules(s); err != nil {
			return err
		}
		if err := apacheEnableSite(s.Name); err != nil {
			return err
		}
	}

	if err := webConfigTest(); err != nil {
		// 回滚
		if backup != nil {
			_ = os.WriteFile(path, backup, 0o644)
		} else {
			_ = os.Remove(path)
		}
		if ws == webApache {
			_ = apacheDisableSite(s.Name)
		}
		return err
	}
	return nil
}

// writeSiteConfAndReload 写入站点配置并热加载 Web 服务器。
// 封装「写配置（含测试回滚）→ reload」两步曲，供创建/修改/删除站点、SSL、安全规则等场景复用。
func writeSiteConfAndReload(s *model.Site) error {
	if err := writeSiteConf(s); err != nil {
		return err
	}
	return webReload()
}

// sanitizeDomainPrefix 提取域名（如 "vltphp.n.05v.cn" → "vltphp"）作为网站目录前缀。
// domain 可能形如 "host:port"（剥端口），可能含中文/特殊字符 → 仅保留 [a-z0-9-_]，
// 若最终为空则回退到 fallback（站点名），再次为空则回退 "site"。
func sanitizeDomainPrefix(domain, fallback string) string {
	name := strings.TrimSpace(fallback)
	if d := strings.TrimSpace(domain); d != "" {
		// 形如 host:port → 只取 host
		if i := strings.IndexAny(d, ":"); i >= 0 {
			d = d[:i]
		}
		// 取第一个 "." 前的部分
		if i := strings.Index(d, "."); i >= 0 {
			d = d[:i]
		}
		d = strings.ToLower(strings.TrimSpace(d))
		if d != "" {
			name = d
		}
	}
	// 仅保留 [a-z0-9-_]，滤掉其他字符，保证作为目录名安全
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_':
			return r
		default:
			return -1
		}
	}, name)
	if cleaned == "" {
		return "site"
	}
	return cleaned
}

// CreateSite 创建网站（按类型动态校验参数）
func CreateSite(req CreateSiteReq) (*model.Site, error) {
	if err := webServerAvailable(); err != nil {
		return nil, err
	}
	// 域名必填，并作为站点名称（未显式填写名称时）
	domain := strings.TrimSpace(req.Domain)
	if domain == "" {
		return nil, errors.New("请填写域名（必填）")
	}
	mainDomain := strings.Split(domain, ",")[0]
	mainDomain = strings.TrimSpace(mainDomain)
	if !siteDomainRe.MatchString(mainDomain) {
		return nil, errors.New("域名格式不合法: " + mainDomain)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		// 通配符域名用于文件命名时去掉 *. 前缀
		name = strings.TrimPrefix(mainDomain, "*.")
	}
	if !siteNameRe.MatchString(name) {
		return nil, errors.New("站点名称仅支持字母、数字、点、下划线、中划线，最长 64 字符")
	}
	if req.Port <= 0 || req.Port > 65535 {
		return nil, errors.New("端口必须在 1-65535 之间")
	}
	// 数据库 / FTP 启用时检测本机是否已安装，否则拒绝并提示先去应用商店安装
	if req.CreateDB {
		if ok, _ := MysqlAvailable(); !ok {
			return nil, errors.New("请先在「应用商店」安装数据库服务（MySQL/MariaDB）后再启用创建数据库")
		}
	}
	if req.CreateFTP {
		if ok, _ := FtpAvailable(); !ok {
			return nil, errors.New("请先在「应用商店」安装 FTP 服务（vsftpd）后再启用创建 FTP")
		}
	}
	if _, ok := model.GetSiteByName(name); ok {
		return nil, errors.New("该域名已被其他网站使用")
	}

	s := &model.Site{
		Name:            name,
		Domain:          strings.TrimSpace(req.Domain),
		Port:            req.Port,
		Type:            req.Type,
		Remark:          req.Remark,
		RuntimeVersion:  strings.TrimSpace(req.RuntimeVersion),
		StartCommand:    strings.TrimSpace(req.StartCommand),
		EnvVars:         strings.TrimSpace(req.EnvVars),
		ProxyPort:       req.ProxyPort,
		Framework:       req.Framework,
		Status:          model.SiteRunning,
		SecurityHeaders: true, // 默认开启安全响应头（防跨站/防嗅探）
	}

	switch {
	case isRuntimeSite(s.Type):
		// python / node / go 项目：校验运行时 + 项目路径 + 启动命令 + 端口
		if err := ensureRuntime(s.Type, req.RuntimeVersion); err != nil {
			return nil, err
		}
		if req.Root == "" {
			return nil, errors.New("请填写项目路径")
		}
		s.Root = strings.TrimRight(req.Root, "/")
		if s.ProxyPort <= 0 || s.ProxyPort > 65535 {
			return nil, errors.New("请填写应用运行端口（1-65535）")
		}
		if s.StartCommand == "" {
			return nil, errors.New("请填写启动命令，如 python app.py / npm run start")
		}
		if err := os.MkdirAll(s.Root, 0o755); err != nil {
			return nil, errors.New("创建项目目录失败: " + err.Error())
		}
		_ = ChownToWebUser(s.Root, true)
		s.ProxyPass = fmt.Sprintf("http://127.0.0.1:%d", s.ProxyPort)
		if s.RuntimeVersion == "" {
			s.RuntimeVersion = runtimeVersionOf(s.Type)
		}

	case model.IsRootType(s.Type):
		// 静态 / PHP：确定根目录并创建
		// 不填写 Root 时，按以下顺序自动推断：
		//   1) 域名第一个 "." 前的内容（用户意图）
		//   2) 站点名（兜底）
		if req.Root == "" {
			s.Root = filepath.Join(webRootBase, sanitizeDomainPrefix(req.Domain, s.Name))
		} else {
			s.Root = strings.TrimRight(req.Root, "/")
		}
		if s.Type == model.SiteTypePHP {
			// 先校验 PHP 运行环境再创建目录，避免校验失败残留孤儿目录
			if err := ensureRuntime(model.SiteTypePHP, req.RuntimeVersion); err != nil {
				return nil, err
			}
		}
		if err := os.MkdirAll(s.Root, 0o755); err != nil {
			return nil, errors.New("创建站点目录失败: " + err.Error())
		}
		_ = ChownToWebUser(s.Root, true)
		if s.Type == model.SiteTypePHP {
			if s.RuntimeVersion == "" {
				s.RuntimeVersion = runtimeVersionOf(model.SiteTypePHP)
			}
			// 按所选版本关联 PHP-FPM（多版本共存时每个站点可指定不同版本）
			if s.PhpFpm == "" {
				s.PhpFpm = resolvePhpFpm(s.RuntimeVersion)
			}
			// 生成默认 index.html 与 404.html（不生成假 index.php）
			writeDefaultPages(s.Root, s.Name)
			// 可选：创建数据库
			if req.CreateDB {
				dbName := req.DBName
				if dbName == "" {
					dbName = s.Name
				}
				dbUser := req.DBUser
				if dbUser == "" {
					dbUser = s.Name
				}
				if err := CreateDatabase(CreateDatabaseReq{Name: dbName, User: dbUser, Password: req.DBPassword}); err != nil {
					return nil, errors.New("创建数据库失败: " + err.Error())
				}
			}
			// 可选：创建 FTP
			if req.CreateFTP {
				ftpUser := req.FTPUsername
				if ftpUser == "" {
					ftpUser = s.Name
				}
				if err := CreateFtpUser(CreateFtpUserReq{Username: ftpUser, Password: req.FTPPassword, HomeDir: s.Root, Remark: "站点 " + s.Name}); err != nil {
					return nil, errors.New("创建 FTP 失败: " + err.Error())
				}
			}
		} else {
			// 静态站点：生成默认 index.html 与 404.html
			writeDefaultPages(s.Root, s.Name)
		}

	default: // proxy 反向代理
		if strings.TrimSpace(req.ProxyPass) == "" {
			return nil, errors.New("反向代理类型需要填写代理目标，如 http://127.0.0.1:8080")
		}
		s.ProxyPass = strings.TrimRight(strings.TrimSpace(req.ProxyPass), "/")
	}

	// PHP / 静态站点：未指定默认文档时自动填充系统默认值
	if model.IsRootType(s.Type) && strings.TrimSpace(s.DefaultIndex) == "" {
		s.DefaultIndex = defaultIndexForType(s.Type)
	}

	if err := writeSiteConfAndReload(s); err != nil {
		return nil, err
	}
	// 更新 default_server 兜底（无默认站点时写入不存在页兜底块）
	_ = applyDefaultServerConf()

	// 进程型站点：启动 systemd 守护
	if isRuntimeSite(s.Type) {
		if err := writeSiteService(s); err != nil {
			_ = os.Remove(siteConfPathFor(s.Name, WebServerType()))
			_ = webReload()
			return nil, errors.New("启动进程服务失败: " + err.Error())
		}
	}

	if err := model.CreateSite(s); err != nil {
		return nil, err
	}
	// 启动该站点访问日志 importer
	StartSiteStatImport(s.ID)
	return s, nil
}

// ListSites 网站列表
func ListSites() []SiteItem {
	sites, _ := model.ListSites()
	items := make([]SiteItem, 0, len(sites))
	for _, s := range sites {
		s.Remark = fixRemarkMojibake(s.Remark)
		item := SiteItem{Site: s, Active: "unknown", SSLStatus: "未部署", SSLDays: -1, IsDefault: DefaultSiteID() == s.ID}
		if s.SslEnabled {
			certPath := s.SslCertPath
			if certPath == "" {
				certPath, _ = siteSSLPath(s.Name)
			}
			if days, err := certExpiryDays(certPath); err == nil {
				item.SSLDays = days
				if days < 0 {
					item.SSLStatus = "已过期"
				} else {
					item.SSLStatus = "剩余" + strconv.Itoa(days) + "天"
				}
			} else {
				item.SSLStatus = "已部署"
			}
		}
		if s.Status == model.SiteRunning {
			if isRuntimeSite(s.Type) {
				// 进程型站点：查 systemd 服务状态
				if siteServiceActive(s.Name) {
					item.Active = "running"
				} else {
					item.Active = "stopped"
				}
			} else if res, err := ExecCommand(siteActiveCheckCmd(s.Name), 15*time.Second); err == nil {
				if strings.TrimSpace(res.Stdout) != "0" && strings.TrimSpace(res.Stdout) != "" {
					item.Active = "running"
				} else {
					item.Active = "stopped"
				}
			}
		} else {
			item.Active = "stopped"
		}
		items = append(items, item)
	}
	return items
}

// certExpiryDays 读取证书剩余天数，已过期返回负数
func certExpiryDays(certPath string) (int, error) {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return 0, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return 0, errors.New("证书文件格式错误")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return 0, err
	}
	days := int(time.Until(cert.NotAfter).Hours() / 24)
	return days, nil
}

// SiteActionReq 站点操作请求
type SiteActionReq struct {
	ID     uint   `json:"id" binding:"required"`
	Action string `json:"action" binding:"required,oneof=start stop restart"`
}

// getSiteOrErr 按 ID 查询站点，不存在则返回统一错误。
// 供所有"先查站点再操作"的入口复用，避免重复 First + 站点不存在判断。
func getSiteOrErr(id uint) (*model.Site, error) {
	s, ok := model.GetSite(id)
	if !ok {
		return nil, errors.New("站点不存在")
	}
	return s, nil
}

// SiteAction 启停站点
func SiteAction(req SiteActionReq) error {
	s, err := getSiteOrErr(req.ID)
	if err != nil {
		return err
	}

	switch req.Action {
	case "start", "restart":
		// 进程型站点：先拉起 systemd 服务
		if isRuntimeSite(s.Type) {
			action := "start"
			if req.Action == "restart" {
				action = "restart"
			}
			res, err := ExecCommand(fmt.Sprintf("systemctl %s %s", action, siteServiceName(s.Name)), 30*time.Second)
			if err != nil {
				return errors.New("启动进程服务失败: " + err.Error())
			}
			if res.ExitCode != 0 {
				return errors.New("启动进程服务失败: " + strings.TrimSpace(res.Stderr))
			}
		}
		if err := writeSiteConfAndReload(s); err != nil {
			return err
		}
		s.Status = model.SiteRunning
	case "stop":
		if isRuntimeSite(s.Type) {
			res, err := ExecCommand(fmt.Sprintf("systemctl stop %s", siteServiceName(s.Name)), 30*time.Second)
			if err != nil {
				return errors.New("停止进程服务失败: " + err.Error())
			}
			if res.ExitCode != 0 {
				return errors.New("停止进程服务失败: " + strings.TrimSpace(res.Stderr))
			}
		}
		if WebServerType() == webApache {
			// Apache：删除站点配置（不适用停用页机制）
			if err := removeSiteConfFile(s.Name); err != nil {
				return err
			}
		} else {
			// Nginx：写停用占位配置，访问域名显示「网站已停止」页面
			if err := writeStoppedSiteConf(s); err != nil {
				return err
			}
			// 默认站点被停止时自动取消默认标记（未绑定域名恢复走不存在页兜底）
			if DefaultSiteID() == s.ID {
				_ = model.SetSetting(settingDefaultSite, "")
			}
		}
		if err := applyDefaultServerConf(); err != nil {
			return err
		}
		s.Status = model.SiteStopped
	}
	return model.SaveSite(s)
}

// DeleteSiteReq 删除网站请求
type DeleteSiteReq struct {
	ID      uint `json:"id" binding:"required"`
	Force   bool `json:"force"`    // 兼容旧版前端：是否同时删除站点根目录
	DelRoot bool `json:"del_root"` // 是否同时删除站点目录/项目目录
	DelDB   bool `json:"del_db"`   // 是否同时删除关联数据库
	DelFtp  bool `json:"del_ftp"`  // 是否同时删除关联 FTP 用户
}

// DeleteSiteResult 删除网站结果（附加项删除失败不阻断主流程，仅在 warnings 中提示）
type DeleteSiteResult struct {
	Warnings []string `json:"warnings"`
}

// cleanupSiteFiles 删除站点关联文件（日志、SSL 证书、WAF/单站安全片段等）。
// 日志文件按站点名命名（如 <name>.access.log），删除站点时必须清理，
// 否则重建同名站点时 nginx 会继续追加写入旧日志。
func cleanupSiteFiles(name string) {
	// 1) 日志：nginx / apache 目录均清理（访问、错误、进程服务、WAF 日志及旋转备份）
	for _, dir := range []string{"/var/log/nginx", "/var/log/apache2"} {
		prefix := filepath.Join(dir, name)
		for _, suffix := range []string{".access.log", ".error.log", ".service.log", ".waf.log"} {
			_ = os.Remove(prefix + suffix)
		}
		if matches, err := filepath.Glob(prefix + ".*.log*"); err == nil {
			for _, m := range matches {
				_ = os.Remove(m)
			}
		}
	}
	// 2) SSL 证书（按当前 Web 服务器类型自动定位目录）
	if cert, key := siteSSLPath(name); cert != "" {
		_ = os.Remove(cert)
		_ = os.Remove(key)
	}
	// 3) WAF 站点片段（nginx / apache）
	_ = os.Remove(filepath.Join(wafSiteConfDir, "lp_"+name+".conf"))
	_ = os.Remove(filepath.Join(apacheWafSiteConfDir, "lp_"+name+".conf"))
	// 4) 单站安全片段
	_ = os.Remove(siteSecSnippetPath(name))
}

// DeleteSite 删除网站
func DeleteSite(req DeleteSiteReq) (DeleteSiteResult, error) {
	var result DeleteSiteResult
	s, err := getSiteOrErr(req.ID)
	if err != nil {
		return result, err
	}

	// 进程型站点：停止并删除 systemd 服务
	if isRuntimeSite(s.Type) {
		removeSiteService(s.Name)
	}

	// 停止站点（删除配置）
	_ = removeSiteConfFile(s.Name)
	_ = webReload()

	// 可选：删除关联数据库（建站时默认库名 = 站点名）
	if req.DelDB {
		if err := DeleteDatabase(s.Name); err != nil {
			result.Warnings = append(result.Warnings, "数据库删除失败: "+err.Error())
		}
	}

	// 可选：删除关联 FTP 用户（默认用户名 = 站点名，或备注为「站点 xxx」）
	if req.DelFtp {
		var ftps []model.FtpUser
		model.DB.Where("username = ? OR remark = ?", s.Name, "站点 "+s.Name).Find(&ftps)
		for _, f := range ftps {
			if err := DeleteFtpUser(f.ID); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("FTP 用户 %s 删除失败: %s", f.Username, err.Error()))
			}
		}
	}

	// 可选删除根目录/项目目录
	if (req.Force || req.DelRoot) && s.Root != "" && s.Root != "/" {
		if err := os.RemoveAll(s.Root); err != nil {
			return result, errors.New("删除站点目录失败: " + err.Error())
		}
	}
	// 停止该站点 importer，并清理历史访问数据
	StopSiteStatImport(s.ID)
	_ = model.DB.Where("site_id = ?", s.ID).Delete(&model.SiteStatVisit{}).Error

	// 清理站点关联文件（日志、SSL 证书、WAF/单站安全片段等）。
	// 日志按站点名命名，不清理的话重建同名站点会继续追加旧日志。
	cleanupSiteFiles(s.Name)
	// 清理站点关联数据（重定向 / 单站安全规则 / 站点级 IP 黑名单）
	_ = model.DeleteSiteRedirects(s.ID)
	_ = model.DB.Where("site_id = ?", s.ID).Delete(&model.SiteSecurityConfig{}).Error
	_ = model.DB.Where("site_id = ?", s.ID).Delete(&model.SiteSecIpRule{}).Error
	_ = model.DB.Where("site_id = ?", s.ID).Delete(&model.SiteSecUaRule{}).Error
	_ = model.DB.Where("site_id = ?", s.ID).Delete(&model.SiteSecRefererRule{}).Error
	_ = model.DB.Where("site_id = ?", s.ID).Delete(&model.SiteSecCustomRule{}).Error
	_ = model.DB.Where("site_id = ?", s.ID).Delete(&model.SiteBlockIP{}).Error
	// 删除的是默认站点时清除默认标记，并刷新 default_server 兜底
	if DefaultSiteID() == s.ID {
		_ = model.SetSetting(settingDefaultSite, "")
	}
	_ = applyDefaultServerConf()
	return result, model.DeleteSite(s)
}

// UpdateSiteRemarkReq 更新站点备注请求
type UpdateSiteRemarkReq struct {
	ID     uint   `json:"id" binding:"required"`
	Remark string `json:"remark"`
}

// UpdateSiteRemark 更新站点备注（仅修改 remark，不重载 nginx）
func UpdateSiteRemark(req UpdateSiteRemarkReq) error {
	if req.ID == 0 {
		return errors.New("站点 ID 不能为空")
	}
	s, err := getSiteOrErr(req.ID)
	if err != nil {
		return err
	}
	s.Remark = strings.TrimSpace(req.Remark)
	return model.UpdateSiteField(s.ID, "remark", s.Remark)
}
