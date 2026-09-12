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
	"net/url"
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
	Type      string `json:"type" binding:"required,oneof=static php node python go java proxy"`
	Root      string `json:"root"` // 静态/PHP 根目录 或 python/node/go 项目路径
	ProxyPass string `json:"proxy_pass"`
	Remark    string `json:"remark"`
	// 伪静态（迁入时同步源面板的 rewrite 规则）
	Rewrite string `json:"rewrite"`

	// PHP / 运行环境公共
	RuntimeVersion string `json:"runtime_version"` // PHP 版本（如 PHP 8.2）/ Python 3.11 / Node 18 / Go 1.22

	// python/node/go 项目参数
	StartCommand string `json:"start_command"` // 启动命令，如 python app.py / npm run start
	// go 站点源码部署：上传后的临时文件路径 + 原始文件名（创建时解压/落盘到项目目录）
	SourceTmp  string `json:"source_tmp"`
	SourceName string `json:"source_name"`
	EnvVars    string `json:"env_vars"`   // 环境变量，KEY=VALUE 每行一个
	ProxyPort  int    `json:"proxy_port"` // 应用运行端口（nginx 反代目标）
	Framework  string `json:"framework"`  // python 框架：flask / django / generic
	// Java 站点（jar 形态）
	JvmArgs string `json:"jvm_args"` // JVM 参数，如 -Xmx512m -Duser.timezone=GMT+08
	JarFile string `json:"jar_file"` // 项目目录下已有的 jar 文件名（不上传时直接指定）
	// 依赖安装 / 构建命令（node：npm install && npm run build；python：pip install -r requirements.txt）
	InstallCommand string `json:"install_command"`

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
// 优先用 nginx -s reload 平滑重载；当 nginx 未运行（/run/nginx.pid 为空或丢失）时，
// reload 会报 "invalid PID number"，此时退化为启动 nginx（start 对已运行实例幂等），
// 确保配置变更后站点/地图能生效而不依赖一个可能为空/失效的 pid 文件。
func nginxReload() error {
	if res, err := ExecCommand("nginx -s reload", 30*time.Second); err == nil && res.ExitCode == 0 {
		return nil
	}
	if _, err := exec.LookPath("systemctl"); err == nil {
		if res, err := ExecCommand("systemctl reload nginx", 30*time.Second); err == nil && res.ExitCode == 0 {
			return nil
		}
		if res, err := ExecCommand("systemctl start nginx", 30*time.Second); err == nil && res.ExitCode == 0 {
			return nil
		}
	}
	res, err := ExecCommand("nginx", 30*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("nginx reload/启动失败: %s", strings.TrimSpace(res.Stderr+res.Stdout))
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

// siteServerNames 返回 server_name 列表（主域名 + 附加域名）。
// 带端口的绑定（如 zz-py2.n.05v.cn:18083）也将其 host 部分纳入 server_name，
// 避免「仅绑定带端口域名」时主 server 块 server_name 为空导致 nginx -t 失败；
// 其独立监听端口的 server 块由 sitePortBindings 另行生成（同一 host 不同端口不冲突）。
func siteServerNames(s *model.Site) []string {
	var names []string
	seen := map[string]bool{}
	for _, d := range siteAllBindings(s) {
		host, _, hasPort := splitHostPort(d)
		name := d
		if hasPort {
			name = host
		}
		if !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
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

// kypanel 安全兜底规则（针对本面板托管的站点栈——Go/PHP/Node/Java/Python 静态与动态站点——自研裁剪，
// 不照搬其它面板）。聚焦「凭证泄露 / 版本库 / 依赖清单 / 备份与临时文件」四类高风险面，
// 刻意不拦截 README/LICENSE/CHANGELOG 等文档文件，避免误伤文档型站点；仅匹配以「/」开头的文件名片段，避免误伤 /license 这类正常路由。
const (
	// 凭证与版本库：.env 系列、.git/.svn 等、私钥/证书、IDE 与构建配置
	nginxSensitiveSecretsRe = `/(\.env.*|\.git|\.gitignore|\.gitattributes|\.gitmodules|\.svn|\.hg|\.bzr|\.htaccess|\.htpasswd|\.user\.ini|\.DS_Store|Thumbs\.db|\.idea|\.vscode|\.claude|\.zed|\.project|\.classpath|\.settings|id_rsa|id_dsa|id_ecdsa|\.pem|\.key|\.crt|\.csr|\.pfx|\.p12|\.keystore|\.jks|\.kdbx|\.secret)$`
	// 依赖清单 / 备份 / 数据库与临时文件：composer.json、go.mod、*.sql、*.bak、*.log 等
	nginxSensitiveArtifactsRe = `/(composer\.json|composer\.lock|package(-lock)?\.json|yarn\.lock|pnpm-lock\.yaml|go\.mod|go\.sum|Cargo\.toml|Cargo\.lock|pom\.xml|build\.gradle|pyproject\.toml|requirements\.txt|phpunit\.xml|Gemfile(\.lock)?|application(-\w+)?\.(ya?ml|properties)|\.log|\.sql(\.gz)?|\.dump|\.db|\.sqlite|\.sqlite3|\.bak(up)?|\.old|\.tmp|\.temp|\.swp|\.swo|\.orig|\.save|\.\w+~)$`
	// 敏感目录：版本库、缓存、依赖、构建与运行时目录
	nginxSensitiveDirsRe = `/(\.git|\.svn|\.hg|\.bzr|\.vscode|\.idea|\.claude|\.zed|\.ssh|\.github|\.gitlab|\.npm|\.yarn|\.pnpm|\.cache|\.husky|\.turbo|\.next|\.nuxt|\.output|node_modules|vendor|runtime|__pycache__|\.pytest_cache|target|\.terraform|\.serverless|\.aws)/`
)

// genSiteServerBlock 生成单个 server 块；defaultSrv 为 true 时在 listen 加 default_server
// （默认站点：未绑定域名请求落到该站点）
func genSiteServerBlock(s *model.Site, port int, ssl bool, defaultSrv bool) string {
	var sb strings.Builder
	sb.WriteString("server {\n")
	if ssl {
		line := "    listen 443 ssl"
		if defaultSrv {
			line += " default_server"
		}
		sb.WriteString(line + ";\n")
		// 新写法：http2 作为独立 server 级指令（旧写法 listen ... http2 在 nginx 新版已废弃）
		sb.WriteString("    http2 on;\n")
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
	if strings.TrimSpace(root) == "" {
		// 兜底：反代类站点（node/python/go/proxy）若因历史数据未落 Root 字段，
		// 下面 .well-known 块会输出 "root ;" 空参数指令，导致 nginx -t 校验失败、
		// 进而拖垮该站点所有配置写入。回退到默认页目录保证配置始终合法。
		root = DefaultPagesDir()
	}
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
	}

	// Let's Encrypt 证书验证目录（HTTP-01 challenge 需要可达）
	// root 类型：跟随 server root（含运行目录 RuntimeDir）；
	// 反代类型（node/python/go/proxy）：指向站点根目录 s.Root，避免验证请求被反代到后端
	sb.WriteString("    location ^~ /.well-known/ {\n")
	fmt.Fprintf(&sb, "        root %s;\n", root)
	sb.WriteString("        allow all;\n")
	// 证书续期请求不能被验证码闸门拦截（auth_request 在 server 级，这里显式豁免）
	sb.WriteString("        auth_request off;\n")
	sb.WriteString("    }\n\n")

	// ===== kypanel 安全兜底：禁止访问敏感文件/目录（对所有站点类型生效，含反向代理站点） =====
	// 按本面板托管的站点栈裁剪，避免误伤正常资源；/.well-known/ 由上方 location ^~ 始终放行（证书验证需要）。
	sb.WriteString("    # 安全兜底-凭证与版本库(.env/.git/私钥/证书/IDE配置等)\n")
	fmt.Fprintf(&sb, "    location ~* %s {\n", nginxSensitiveSecretsRe)
	sb.WriteString("        return 404;\n")
	sb.WriteString("    }\n\n")
	sb.WriteString("    # 安全兜底-依赖清单/备份/数据库/临时文件\n")
	fmt.Fprintf(&sb, "    location ~* %s {\n", nginxSensitiveArtifactsRe)
	sb.WriteString("        return 404;\n")
	sb.WriteString("    }\n\n")
	sb.WriteString("    # 安全兜底-敏感目录(.git/node_modules/缓存/构建目录等)\n")
	fmt.Fprintf(&sb, "    location ~* %s {\n", nginxSensitiveDirsRe)
	sb.WriteString("        return 404;\n")
	sb.WriteString("    }\n\n")
	// 禁止在证书验证目录内放置可执行/敏感文件：用 location 替代 if，规避 nginx "if is evil" 的隐患。
	sb.WriteString("    location ~* /\\.well-known/.*\\.(php|jsp|py|pl|rb|cgi|sh|js|css|lua|ts|go|zip|tar\\.gz|rar|7z|sql|bak|env|ini|key|pem|crt)$ {\n")
	sb.WriteString("        return 403;\n")
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
		// 防跨站：将 PHP 文件操作限制在本站点根目录与 /tmp，避免一站被黑读写/污染其他站点
		fmt.Fprintf(&sb, "        fastcgi_param PHP_ADMIN_VALUE \"open_basedir=%s/:/tmp/\";\n", strings.TrimRight(s.Root, "/"))
		sb.WriteString("    }\n\n")
	default: // node / python / go / proxy：反向代理到本地端口或任意 URL
		sb.WriteString("    location / {\n")
		fmt.Fprintf(&sb, "        proxy_pass %s;\n", s.ProxyPass)
		sb.WriteString("        proxy_set_header Host $host;\n")
		sb.WriteString("        proxy_set_header X-Real-IP $remote_addr;\n")
		sb.WriteString("        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n")
		sb.WriteString("        proxy_set_header X-Forwarded-Proto $scheme;\n")
		// WebSocket：HTTP/1.1 + 升级头透传（$lp_connection_upgrade 由全局片段 lp-global.conf 定义 map）
		sb.WriteString("        proxy_http_version 1.1;\n")
		sb.WriteString("        proxy_set_header Upgrade $http_upgrade;\n")
		sb.WriteString("        proxy_set_header Connection $lp_connection_upgrade;\n")
		// 放宽上传体积：nginx 默认 1m，反代后端的上传接口会 413
		sb.WriteString("        client_max_body_size 100m;\n")
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
	// 拖拽验证码闸门片段（存在则 include，不存在为空）
	if inc := CaptchaIncludeLine(s.ID); inc != "" {
		sb.WriteString(inc)
	}
	// 邮箱门户接口反代片段（存在则 include，不存在为空）
	if inc := MailPortalIncludeLine(s.Name, s.ID); inc != "" {
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

// isRuntimeSite 是否为进程型站点（python/node/go/java，由 systemd 守护）。
// 判定依据是站点类型注册表：新增语言站点只需在 siteTypeSpecs 里加一项。
func isRuntimeSite(t string) bool {
	_, ok := siteSpec(t)
	return ok
}

// runtimeVersionOf / normalizeRuntimeVersion / findRuntimeBinDir 已下沉到站点类型注册表
// （见 site_types.go）：各类型的探测命令、版本正则、安装路径 glob 全部由 siteTypeSpecs 描述。

func siteServiceName(name string) string { return "lp-" + name }

func siteServicePath(name string) string {
	return "/etc/systemd/system/" + siteServiceName(name) + ".service"
}

func siteRunnerPath(name string) string { return "/www/.lp-run/" + name + ".sh" }

// siteRuntimePrelude 生成站点进程 / 命令运行所需的环境前置：
// 运行时 PATH（按所选版本注入）、类型对应环境变量（GOROOT/JAVA_HOME）、
// 应用端口与用户自定义环境变量。供启动脚本与依赖安装命令共用，保证两者环境一致。
func siteRuntimePrelude(s *model.Site) string {
	var sb strings.Builder
	// 按 RuntimeVersion 注入指定版本的运行时 PATH（版本统一归一化为主.次格式，路径 glob 模糊匹配）
	if s.RuntimeVersion != "" {
		if spec, ok := siteSpec(s.Type); ok {
			if bin := findRuntimeBinDir(s.Type, normalizeRuntimeVersion(s.RuntimeVersion)); bin != "" {
				fmt.Fprintf(&sb, "export PATH=\"%s:$PATH\"\n", bin)
				// 类型需要的基础环境变量（GOROOT/JAVA_HOME）= bin 的上一级
				if spec.EnvKey != "" {
					fmt.Fprintf(&sb, "export %s=\"%s\"\n", spec.EnvKey, filepath.Dir(bin))
				}
			}
		}
	}
	// 应用运行端口：多数 Node/Python 框架读取 PORT 环境变量；放在用户变量之前，允许被用户变量覆盖
	if isRuntimeSite(s.Type) && s.ProxyPort > 0 {
		fmt.Fprintf(&sb, "export PORT=%d\n", s.ProxyPort)
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
	return sb.String()
}

// effectiveStartCommand 返回站点实际执行的启动命令。
// 用户显式填写时一律优先；留空则由类型的 DefaultRun 自动拼装
// （Java：java <JVM参数> -jar <jar> --server.port=<端口>，免去手写整条命令）。
func effectiveStartCommand(s *model.Site) string {
	if cmd := strings.TrimSpace(s.StartCommand); cmd != "" {
		return cmd
	}
	if spec, ok := siteSpec(s.Type); ok && spec.DefaultRun != nil {
		return spec.DefaultRun(s)
	}
	return ""
}

// writeSiteService 为 python/node/go/java 站点生成 systemd 服务 + 启动脚本。
// autoStart=false 时只生成并 enable（不启动）：用于「启动命令尚未就绪/不可用」的场景，
// 避免空目录或错误命令导致 Restart=always 无限重启刷日志。
func writeSiteService(s *model.Site, autoStart bool) error {
	runnerDir := "/www/.lp-run"
	if err := os.MkdirAll(runnerDir, 0o755); err != nil {
		return err
	}
	var sb strings.Builder
	sb.WriteString("#!/bin/bash\n")
	sb.WriteString(siteRuntimePrelude(s))
	if strings.TrimSpace(s.Root) != "" {
		fmt.Fprintf(&sb, "cd %s\n", shellQuote(s.Root))
	}
	startCmd := effectiveStartCommand(s)
	if strings.TrimSpace(startCmd) == "" {
		sb.WriteString("echo 'start command not set, waiting for entry selection'\n")
		sb.WriteString("exit 0\n")
	} else {
		// 用 bash -c 执行：支持 && / 变量赋值 / 重定向等 shell 语法
		// （exec 是内建，无法直接解析这些语法，会导致复杂启动命令失败）
		fmt.Fprintf(&sb, "exec bash -c %s\n", shellQuote(startCmd))
	}
	if err := os.WriteFile(siteRunnerPath(s.Name), []byte(sb.String()), 0o755); err != nil {
		return err
	}

	// 注意：systemd 的 WorkingDirectory / ExecStart 不做 shell 分词，给值加引号会被
	// 当作路径的一部分（WorkingDirectory="/x" 会报 "path is not absolute"），因此必须写裸值。
	// 站点名受 siteNameRe 白名单约束（字母数字点下划线中划线），runner 文件名同样安全。
	wd := strings.TrimSpace(s.Root)
	if wd == "" {
		wd = "/"
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
# 面板点「停止」时 systemd 发 SIGTERM，进程以 143 退出；不声明为成功会让单元停在
# failed 状态（列表显示异常、再次启动前需要 reset-failed）。
SuccessExitStatus=143

[Install]
WantedBy=multi-user.target
`, s.Name, wd, siteRunnerPath(s.Name), s.Name, s.Name)
	if err := os.WriteFile(siteServicePath(s.Name), []byte(unit), 0o644); err != nil {
		return err
	}
	cmd := fmt.Sprintf("systemctl daemon-reload && systemctl enable %s", siteServiceName(s.Name))
	if autoStart {
		// 用 restart 兼容「首次创建」与「配置变更后重启」两种场景
		cmd += fmt.Sprintf(" && systemctl restart %s", siteServiceName(s.Name))
	}
	res, err := ExecCommand(cmd, 30*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(pickSystemctlError(res.Stderr))
	}
	return nil
}

// pickSystemctlError 过滤 systemctl 输出中的噪音（如 enable 生成的 symlink 提示），
// 只保留真正的失败原因，避免告警文案冗长。
func pickSystemctlError(stderr string) string {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	keep := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "Created symlink") || strings.HasPrefix(l, "Removed ") {
			continue
		}
		keep = append(keep, l)
	}
	msg := strings.Join(keep, " ")
	if len(msg) > 240 {
		msg = msg[:240] + "…"
	}
	if msg == "" {
		msg = "systemctl 返回非零退出码"
	}
	return msg
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

// ============================ nginx 全局公共片段 ============================
// 反代站点需要 WebSocket 升级映射。map 只能定义在 http 上下文，无法写在站点 server 块内
// （多站点重复定义同名 map 会报 duplicate map），因此统一落到 conf.d 下的全局片段。
// 注意：这里刻意不写 client_max_body_size——它是 http 上下文指令，若宿主机 nginx 已在 http 级
// 定义过会直接报 duplicate directive、拖垮整个 nginx 校验；上传体积改在反代站点的 location
// 内单独设置（location 上下文覆盖 http 级，不会冲突）。

// nginxGlobalSnippetPath 全局片段路径（00- 前缀保证先于站点配置加载）
const nginxGlobalSnippetPath = "/etc/nginx/conf.d/00-kypanel-global.conf"

const nginxGlobalSnippet = `# kypanel 全局公共配置（自动生成，请勿手动修改）
# WebSocket 升级映射：普通请求 Connection=close，升级请求 Connection=upgrade
map $http_upgrade $lp_connection_upgrade {
    default upgrade;
    ''      close;
}
`

// ensureNginxGlobalSnippet 幂等写入全局公共片段（内容变化时才落盘）。
// 写入后立即校验：若新片段与本机已有配置冲突（如同名 map），回滚为旧内容并返回错误，
// 避免残留一个坏文件导致后续所有站点配置写入全部失败。
func ensureNginxGlobalSnippet() error {
	old, readErr := os.ReadFile(nginxGlobalSnippetPath)
	if readErr == nil && string(old) == nginxGlobalSnippet {
		return nil
	}
	if err := os.MkdirAll(nginxConfDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(nginxGlobalSnippetPath, []byte(nginxGlobalSnippet), 0o644); err != nil {
		return err
	}
	if err := nginxTest(); err != nil {
		if readErr == nil {
			_ = os.WriteFile(nginxGlobalSnippetPath, old, 0o644)
		} else {
			_ = os.Remove(nginxGlobalSnippetPath)
		}
		return err
	}
	return nil
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
		sb.WriteString("    http2 on;\n")
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
	// nginx：确保全局片段（WebSocket 升级 map + 上传体积限制）已就位，
	// 否则反代站点配置引用的 $lp_connection_upgrade 未定义会导致 nginx -t 失败
	if ws := WebServerType(); ws != webApache {
		if err := ensureNginxGlobalSnippet(); err != nil {
			return err
		}
	}
	conf := genSiteConf(s)
	// 注入拖拽验证码 include 行（覆盖站点存在 config_override 时 genSiteConf 直接返回 override 的情况）
	conf = ensureCaptchaInclude(conf, s)
	regenerateSiteWAFSnippet(s)
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

// siteDirFromDomain 根据域名推断网站目录名，统一使用完整域名（保留 "."）。
// 例如 "vltphp.n.05v.cn" → "vltphp.n.05v.cn"，"127.0.0.1:8899" → "127.0.0.1"，
// "*.example.com" → "example.com"。域名可能含中文/特殊字符 → 仅保留 [a-z0-9-_.]，
// 若最终为空则回退到 fallback（站点名），再次为空则回退 "site"。
// 说明：完整域名作为目录名可避免同前缀不同域名（如 a.example.com / a.other.com）
// 的站点目录冲突；数据库名/FTP 用户名不能含 "."，仍用域名首段（见 domainPrefixOf）。
func siteDirFromDomain(domain, fallback string) string {
	name := strings.TrimSpace(fallback)
	if d := strings.TrimSpace(domain); d != "" {
		// 形如 host:port → 只取 host
		if i := strings.IndexAny(d, ":"); i >= 0 {
			d = d[:i]
		}
		// 通配符绑定：*.example.com → example.com
		d = strings.TrimPrefix(strings.TrimSpace(d), "*.")
		d = strings.ToLower(strings.TrimSpace(d))
		if d != "" {
			name = d
		}
	}
	// 仅保留 [a-z0-9-_.]，滤掉其他字符，保证作为目录名安全
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_' || r == '.':
			return r
		default:
			return -1
		}
	}, name)
	// 清理首尾点与连续点，避免出现 ".example" / "example..com" 这类不友好目录
	cleaned = strings.Trim(cleaned, ".")
	for strings.Contains(cleaned, "..") {
		cleaned = strings.ReplaceAll(cleaned, "..", ".")
	}
	if cleaned == "" {
		return "site"
	}
	return cleaned
}

// domainPrefixOf 提取域名首段（如 "vltphp.n.05v.cn" → "vltphp"），
// 用于数据库名 / FTP 用户名这类不允许含 "." 的标识符生成。
func domainPrefixOf(domain, fallback string) string {
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
	// 仅保留 [a-z0-9-_]
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

// normalizeProxyPass 校验并补全反向代理目标：缺少协议时自动补 http://。
// 避免用户填 "127.0.0.1:3000"（漏协议）导致 nginx -t 报 invalid URL prefix、创建失败。
func normalizeProxyPass(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", errors.New("反向代理类型需要填写代理目标，如 http://127.0.0.1:8080")
	}
	if !strings.Contains(p, "://") {
		p = "http://" + p
	}
	u, err := url.Parse(p)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("代理目标格式不合法，应为 http://主机:端口，如 http://127.0.0.1:8080")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("代理目标仅支持 http:// 或 https:// 协议")
	}
	return p, nil
}

// checkProxyPortConflict 校验进程型站点的应用端口是否与其它站点（监听端口/应用端口）冲突。
// 只比较「进程型站点」的 ProxyPort：静态/PHP/反向代理站点的 ProxyPort 无实际意义
// （历史数据可能残留表单默认值 3000），不应参与冲突判定。
func checkProxyPortConflict(selfName string, proxyPort int) error {
	sites, err := model.ListSites()
	if err != nil {
		return nil
	}
	for _, o := range sites {
		if o.Name == selfName {
			continue
		}
		if o.Port == proxyPort {
			return fmt.Errorf("应用运行端口 %d 已被站点 %s 的监听端口占用，请更换", proxyPort, o.Name)
		}
		if isRuntimeSite(o.Type) && o.ProxyPort == proxyPort {
			return fmt.Errorf("应用运行端口 %d 已被站点 %s 使用，请更换", proxyPort, o.Name)
		}
	}
	return nil
}

// defaultPythonStartCommand 按所选 Python 框架生成推荐启动命令。
// 仅 flask 能确定默认入口（app:app）；django 的 wsgi 模块名依项目而异，返回空由用户填写。
func defaultPythonStartCommand(framework string, port int) string {
	if framework == "flask" {
		return fmt.Sprintf("gunicorn -w 2 -b 127.0.0.1:%d app:app", port)
	}
	return ""
}

// appendWarning 拼接多条非致命告警。
func appendWarning(cur, add string) string {
	if add == "" {
		return cur
	}
	if cur == "" {
		return add
	}
	return cur + "；" + add
}

// startCmdBuiltins 启动命令首词命中这些 shell 内建/包装器时跳过存在性检查。
var startCmdBuiltins = map[string]bool{
	"source": true, ".": true, "export": true, "cd": true, "exec": true,
	"set": true, "unset": true, "bash": true, "sh": true, "env": true,
	"nohup": true, "time": true, "timeout": true,
}

// isShellName 判断字符串是否为合法的 shell 变量名（用于识别 KEY=VALUE 前缀）。
func isShellName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

// firstCommandWord 取命令行首个可执行词（跳过 KEY=VALUE 环境变量前缀与常见包装器）。
func firstCommandWord(cmd string) string {
	fields := strings.Fields(strings.TrimSpace(cmd))
	for _, f := range fields {
		if i := strings.Index(f, "="); i > 0 && !strings.ContainsAny(f[:i], `/\`) && isShellName(f[:i]) {
			continue // 环境变量前缀，如 PORT=3000
		}
		return f
	}
	return ""
}

// checkStartCommandAvailable 用站点运行环境检查启动命令首词是否存在，
// 提前发现"填了 gunicorn/npm 但运行环境里没装"的情况（避免 systemd 无限重启）。
// 返回非致命告警（调用方只记 warning，不阻断创建）。
func checkStartCommandAvailable(s *model.Site) error {
	// 用「实际执行」的命令校验：Java 站点启动命令可留空（自动拼装），必须取拼装后的结果，
	// 否则留空时会跳过校验、并在 autoStart 判定上被当成"命令未就绪"而不启动。
	first := firstCommandWord(effectiveStartCommand(s))
	if first == "" || startCmdBuiltins[first] {
		return nil
	}
	if strings.HasPrefix(first, "./") || strings.HasPrefix(first, "/") {
		p := first
		if strings.HasPrefix(first, "./") {
			p = filepath.Join(s.Root, strings.TrimPrefix(first, "./"))
		}
		info, err := os.Stat(p)
		if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
			return fmt.Errorf("启动命令 %q 指向的文件不存在或不可执行", first)
		}
		return nil
	}
	script := siteRuntimePrelude(s) + "command -v " + shellQuote(first)
	res, err := ExecCommand("bash -c "+shellQuote(script), 15*time.Second)
	if err != nil || res == nil || res.ExitCode != 0 {
		return fmt.Errorf("启动命令中的 %q 未在当前运行环境中找到，请先安装依赖或改用完整路径", first)
	}
	return nil
}

// runSiteInstallCommand 在项目目录下、以站点运行环境执行一次依赖安装/构建命令。
func runSiteInstallCommand(s *model.Site, cmd string) error {
	script := siteRuntimePrelude(s)
	if strings.TrimSpace(s.Root) != "" {
		script += "cd " + shellQuote(s.Root) + "\n"
	}
	script += cmd
	res, err := ExecCommand("bash -c "+shellQuote(script), 15*time.Minute)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errors.New(pickCommandError(res.Stderr, res.Stdout, res.ExitCode))
	}
	return nil
}

// pickCommandError 从命令输出中挑出可读的错误摘要：
// 过滤包管理器噪音（pip notice、npm warn、版本升级提示等），
// 优先取含错误关键字的行，最多 2 行、总长 200 字符，避免把整段原始输出塞进告警。
func pickCommandError(stderr, stdout string, exitCode int) string {
	raw := strings.TrimSpace(stderr)
	if raw == "" {
		raw = strings.TrimSpace(stdout)
	}
	cleaned := make([]string, 0, 8)
	for _, line := range strings.Split(raw, "\n") {
		l := strings.TrimSpace(line)
		if l == "" {
			continue
		}
		low := strings.ToLower(l)
		if strings.HasPrefix(low, "[notice]") ||
			strings.Contains(low, "a new release of") ||
			strings.Contains(low, "to update, run:") ||
			strings.HasPrefix(low, "npm warn") {
			continue
		}
		cleaned = append(cleaned, l)
	}
	picked := make([]string, 0, 2)
	for _, l := range cleaned {
		low := strings.ToLower(l)
		if strings.Contains(low, "error") || strings.Contains(low, "failed") ||
			strings.Contains(low, "cannot") || strings.Contains(low, "no such") ||
			strings.Contains(low, "not found") || strings.Contains(low, "fatal") {
			picked = append(picked, l)
			if len(picked) >= 2 {
				break
			}
		}
	}
	if len(picked) == 0 {
		// 无明确错误关键字：取末尾 2 行（通常是真正的失败原因）
		if len(cleaned) > 2 {
			cleaned = cleaned[len(cleaned)-2:]
		}
		picked = cleaned
	}
	msg := strings.Join(picked, " ")
	if len(msg) > 200 {
		msg = msg[:200] + "…"
	}
	if msg == "" {
		msg = "退出码 " + strconv.Itoa(exitCode)
	}
	return msg
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
	// 校验每个绑定（支持 域名:端口 / IP:端口），如 zz-py2.n.05v.cn:18083
	for _, raw := range strings.Split(domain, ",") {
		b := strings.TrimSpace(raw)
		if b == "" {
			return nil, errors.New("域名包含空项，请检查逗号分隔")
		}
		if !isValidBinding(b) {
			return nil, errors.New("域名格式不合法: " + b)
		}
	}
	mainDomain := strings.Split(domain, ",")[0]
	mainDomain = strings.TrimSpace(mainDomain)
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
		Rewrite:         req.Rewrite,
		RuntimeVersion:  strings.TrimSpace(req.RuntimeVersion),
		StartCommand:    strings.TrimSpace(req.StartCommand),
		EnvVars:         strings.TrimSpace(req.EnvVars),
		ProxyPort:       req.ProxyPort,
		Framework:       req.Framework,
		InstallCommand:  strings.TrimSpace(req.InstallCommand),
		JvmArgs:         strings.TrimSpace(req.JvmArgs),
		JarFile:         strings.TrimSpace(req.JarFile),
		Status:          model.SiteRunning,
		SecurityHeaders: true, // 默认开启安全响应头（防跨站/防嗅探）
	}
	// 项目端口仅进程型站点（node/python/go）有意义：其它类型一律清零，
	// 避免表单默认值（3000）被无意义落库，导致后续进程型站点被误判端口冲突
	if !isRuntimeSite(s.Type) {
		s.ProxyPort = 0
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
		// Java 站点的应用端口允许留空：自动分配逻辑见下方 Java 分支（需等 jar 落盘/解压后
		// 才能读其内嵌 server.port，故不能在此处提前校验）。其余进程型站点仍要求必填。
		if s.ProxyPort <= 0 && s.Type != model.SiteTypeJava {
			return nil, errors.New("请填写应用运行端口（1-65535）")
		}
		if s.ProxyPort > 65535 {
			return nil, errors.New("应用运行端口超出范围（1-65535）")
		}
		if s.ProxyPort > 0 && s.ProxyPort == s.Port {
			return nil, fmt.Errorf("应用运行端口不能与站点监听端口相同（%d），请更换", s.Port)
		}
		if s.ProxyPort > 0 {
			if err := checkProxyPortConflict(s.Name, s.ProxyPort); err != nil {
				return nil, err
			}
		}
		// 启动命令选填：Go 不填时按上传内容自动推断（见下方 Go 分支）；
		// Python 选择框架后可自动生成推荐命令；Java 有 jar 时按 jar + JVM 参数自动拼装；
		// 其余情况仍要求填写。
		if s.Type != model.SiteTypeGo && s.StartCommand == "" {
			if s.Type == model.SiteTypePython {
				s.StartCommand = defaultPythonStartCommand(s.Framework, s.ProxyPort)
			}
			if s.StartCommand == "" && s.Type != model.SiteTypeJava {
				return nil, errors.New("请填写启动命令，如 python app.py / npm run start")
			}
		}
		if err := os.MkdirAll(s.Root, 0o755); err != nil {
			return nil, errors.New("创建项目目录失败: " + err.Error())
		}
		_ = ChownToWebUser(s.Root, true)
		// Go 站点：部署上传的源码（压缩包解压/自动编译 / 单二进制落盘）
		if s.Type == model.SiteTypeGo && req.SourceTmp != "" {
			execs, err := DeploySiteSource(req.SourceTmp, req.SourceName, s.Root, req.RuntimeVersion, s.Name)
			if err != nil {
				return nil, err
			}
			CleanupUpload(req.SourceTmp)
			s.ExecFiles = execs
			// 启动命令未填写时自动推断：唯一可执行文件 → ./文件名
			if s.StartCommand == "" {
				if len(execs) == 1 {
					s.StartCommand = "./" + execs[0].Path
				} else if len(execs) == 0 {
					return nil, errors.New("未在源码中找到可执行文件，也无法编译出二进制；请上传已编译的程序或包含 go.mod 的 Go 源码")
				}
				// len(execs) > 1 时留空，交由前端弹窗选择入口
			}
		}
		// Java 站点：部署上传的 jar（或 .zip 内含 jar）到项目目录，确定要运行的 jar 文件名
		if s.Type == model.SiteTypeJava {
			if req.SourceTmp != "" {
				jarName, err := DeployJavaArtifact(req.SourceTmp, req.SourceName, s.Root)
				if err != nil {
					return nil, err
				}
				CleanupUpload(req.SourceTmp)
				s.JarFile = jarName
			}
			// 应用端口留空时自动确定（用户显式填写则直接采用）：
			//  1) 读已落盘 jar 内嵌的 server.port（兼容 jar 直传与 zip 解压两种上传方式）
			//  2) 否则自动分配空闲端口
			if s.ProxyPort <= 0 {
				declared := 0
				if s.JarFile != "" {
					if info := InspectJavaArtifact(filepath.Join(s.Root, s.JarFile)); info.Port > 0 {
						declared = info.Port
					}
				}
				if declared > 0 && isPortBindable(declared) {
					s.ProxyPort = declared
				} else {
					if declared > 0 {
						s.DeployWarning = appendWarning(s.DeployWarning,
							"项目内声明的端口 "+strconv.Itoa(declared)+" 已被占用，已改用自动分配的端口")
					}
					s.ProxyPort = AllocateSitePort()
				}
				if s.ProxyPort <= 0 {
					return nil, errors.New("自动分配应用端口失败，请手动填写应用端口")
				}
			}
			// 补校验：自动分配的端口可能与监听端口/其它站点冲突（显式填写已在校验阶段处理）
			if s.ProxyPort == s.Port {
				return nil, fmt.Errorf("应用运行端口不能与站点监听端口相同（%d），请更换", s.Port)
			}
			if err := checkProxyPortConflict(s.Name, s.ProxyPort); err != nil {
				return nil, err
			}
			// jar 仅在使用「自动拼装启动命令」时必需：已自定义启动命令的场景
			// （如从宝塔迁移过来的 Java 项目）允许只给命令、不给 jar。
			if s.JarFile == "" && s.StartCommand == "" {
				return nil, errors.New("请上传 jar 包，或填写项目目录中已有的 jar 文件名（也可直接自定义启动命令）")
			}
			// jar 必须真实存在，否则 systemd 会 Restart=always 无限重启刷日志
			if s.JarFile != "" && !jarFileExists(s.Root, s.JarFile) {
				return nil, errors.New("项目目录中未找到 " + s.JarFile + "，请重新上传")
			}
			// 非可执行包（清单里没有主类）不阻断创建，但明确告警，避免用户以为"没反应"
			if hint := JarMainHint(s.Root, s.JarFile); hint != "" {
				s.DeployWarning = appendWarning(s.DeployWarning, hint)
			}
			// 启动命令留空时由 effectiveStartCommand 按 jar + JVM 参数 + 端口自动拼装
		}
		s.ProxyPass = fmt.Sprintf("http://127.0.0.1:%d", s.ProxyPort)
		if s.RuntimeVersion == "" {
			s.RuntimeVersion = runtimeVersionOf(s.Type)
		}

	case model.IsRootType(s.Type):
		// 静态 / PHP：确定根目录并创建
		// 不填写 Root 时，按以下顺序自动推断：
		//   1) 完整域名（如 a.example.com → /www/wwwroot/a.example.com）
		//   2) 站点名（兜底）
		if req.Root == "" {
			s.Root = filepath.Join(webRootBase, siteDirFromDomain(req.Domain, s.Name))
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
			_ = ChownToWebUser(s.Root, true)
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
			_ = ChownToWebUser(s.Root, true)
		}

	default: // proxy 反向代理
		pp, err := normalizeProxyPass(req.ProxyPass)
		if err != nil {
			return nil, err
		}
		s.ProxyPass = pp
		// 反代站点同样需要站点根目录：Let's Encrypt 的 HTTP-01 验证目录
		// （/.well-known/）落在站点 root 下，Root 为空会让生成的 nginx 配置
		// 出现 "root ;" 空参数指令，nginx -t 校验失败导致站点根本创建不出来。
		if req.Root == "" {
			s.Root = filepath.Join(webRootBase, siteDirFromDomain(req.Domain, s.Name))
		} else {
			s.Root = strings.TrimRight(req.Root, "/")
		}
		if err := os.MkdirAll(s.Root, 0o755); err != nil {
			return nil, errors.New("创建站点目录失败: " + err.Error())
		}
		_ = ChownToWebUser(s.Root, true)
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

	// 进程型站点：先按需执行依赖安装/构建命令，再启动 systemd 守护。
	// 安装失败或服务启动失败不阻断创建（站点配置与项目目录已生成），改为记录告警返回前端，
	// 用户修正后可手动重启，避免"创建到一半整体回滚"、留下半成品。
	if isRuntimeSite(s.Type) {
		// 依赖安装命令仅对 node/python 生效（Go 为编译型语言，依赖由编译器处理）
		isNodePy := s.Type == model.SiteTypeNode || s.Type == model.SiteTypePython
		if cmd := strings.TrimSpace(s.InstallCommand); cmd != "" && isNodePy {
			if err := runSiteInstallCommand(s, cmd); err != nil {
				s.DeployWarning = appendWarning(s.DeployWarning, "依赖安装命令执行失败："+err.Error())
			}
		}
		// 启动命令为空（如 Go 多入口等待选择）或首词不可用时，只 enable 不启动，
		// 避免空目录 / 错误命令触发 Restart=always 无限重启；用户修正后手动启动即可。
		// 注意取 effectiveStartCommand：Java 站点命令由 jar 自动拼装，StartCommand 可能为空。
		autoStart := strings.TrimSpace(effectiveStartCommand(s)) != ""
		if err := checkStartCommandAvailable(s); err != nil {
			s.DeployWarning = appendWarning(s.DeployWarning, err.Error())
			autoStart = false
		}
		if err := writeSiteService(s, autoStart); err != nil {
			s.DeployWarning = appendWarning(s.DeployWarning, "进程服务启动失败："+err.Error())
			autoStart = false
		}
		if !autoStart {
			s.Status = model.SiteStopped
		}
	}

	if err := model.CreateSite(s); err != nil {
		return nil, err
	}
	// 域名中带端口的绑定（如 zz-py2.n.05v.cn:18083），自动放行对应防火墙端口，确保外网可访问。
	// 失败不阻断创建，仅记录告警（防火墙未安装/规则写入失败时由用户手动处理）。
	for _, b := range sitePortBindings(s) {
		if err := AllowPortWithSource(b.Port, "tcp", "站点域名端口放行 "+b.Host, "site:"+s.Name); err != nil {
			s.DeployWarning = appendWarning(s.DeployWarning, "端口 "+b.Port+" 防火墙自动放行失败："+err.Error())
		}
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
		// 进程型站点：重新生成 systemd 单元与启动脚本后再启动。
		// 这里用 writeSiteService 而不是裸 systemctl：既保证单元文件与当前配置一致，
		// 也能自愈历史遗留/损坏的单元文件（如 "bad unit file setting" 导致的无法启动）。
		if isRuntimeSite(s.Type) {
			s.Status = model.SiteRunning
			if err := writeSiteService(s, true); err != nil {
				return errors.New("启动进程服务失败: " + err.Error())
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
	// 5) 拖拽验证码片段
	_ = os.Remove(captchaSnippetPath(name))
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

// SetSiteStartCommand 设置进程型站点的启动命令并重启服务（Go 站点选择入口后调用）。
func SetSiteStartCommand(id uint, cmd string) error {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return errors.New("启动命令不能为空")
	}
	s, err := getSiteOrErr(id)
	if err != nil {
		return err
	}
	if !isRuntimeSite(s.Type) {
		return errors.New("仅进程型站点支持修改启动命令")
	}
	s.StartCommand = cmd
	if err := model.UpdateSiteField(s.ID, "start_command", cmd); err != nil {
		return err
	}
	// 选好入口即视为启用：创建时若因「启动命令未就绪」只 enable 未启动（status=stopped），
	// 这里恢复为 running，保证 rewriteSiteService 会真正拉起服务。
	if s.Status != model.SiteRunning {
		s.Status = model.SiteRunning
		_ = model.UpdateSiteField(s.ID, "status", model.SiteRunning)
	}
	return rewriteSiteService(s)
}
