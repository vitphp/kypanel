package service

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"kypanel/internal/model"
)

// ============================ Apache 站点配置生成 ============================
//
// 与 nginx 的 genSiteConf 对等，生成 Apache VirtualHost 配置。
// PHP 通过 php-fpm + mod_proxy_fcgi 方式（SetHandler proxy:fcgi://）。

// genApacheConf 生成 apache 站点配置（含全部设置项；自定义配置非空时完全覆盖）
func genApacheConf(s *model.Site) string {
	if strings.TrimSpace(s.ConfigOverride) != "" {
		return s.ConfigOverride
	}

	names := strings.Join(siteServerNames(s), " ")

	var sb strings.Builder
	if s.SslEnabled {
		sb.WriteString("# kypanel site: " + s.Name + " (HTTP)\n")
		if s.SslForce {
			sb.WriteString(genApacheRedirectVHost(s, names))
		} else {
			sb.WriteString(genApacheVHost(s, 80, names, false))
		}
		sb.WriteString("\n# kypanel site: " + s.Name + " (HTTPS)\n")
		sb.WriteString(genApacheVHost(s, 443, names, true))
	} else {
		sb.WriteString(genApacheVHost(s, s.Port, names, false))
	}

	// 带端口的绑定（IP:端口 / 域名:端口）生成独立 VirtualHost
	for _, b := range sitePortBindings(s) {
		p, _ := strconv.Atoi(b.Port)
		sb.WriteString("\n# kypanel site: " + s.Name + " (" + b.Host + ":" + b.Port + ")\n")
		cp := *s
		cp.Domain = b.Host
		cp.Domains = ""
		sb.WriteString(genApacheVHost(&cp, p, b.Host, false))
	}
	return sb.String()
}

// genApacheRedirectVHost 80 端口强制跳转 443
func genApacheRedirectVHost(s *model.Site, names string) string {
	var sb strings.Builder
	sb.WriteString("<VirtualHost *:80>\n")
	fmt.Fprintf(&sb, "    ServerName %s\n", sitePrimaryName(s))
	sb.WriteString("    RewriteEngine On\n")
	sb.WriteString("    RewriteCond %{HTTPS} !=on\n")
	sb.WriteString("    RewriteRule ^(.*)$ https://%{HTTP_HOST}%{REQUEST_URI} [R=301,L]\n")
	sb.WriteString("</VirtualHost>\n")
	return sb.String()
}

// genApacheVHost 生成单个 VirtualHost
func genApacheVHost(s *model.Site, port int, names string, ssl bool) string {
	var sb strings.Builder
	if ssl {
		fmt.Fprintf(&sb, "<VirtualHost *:%d>\n", port)
	} else {
		fmt.Fprintf(&sb, "<VirtualHost *:%d>\n", port)
	}
	fmt.Fprintf(&sb, "    ServerName %s\n", sitePrimaryName(s))
	// 额外域名用 ServerAlias
	for _, alias := range siteServerNames(s)[1:] {
		fmt.Fprintf(&sb, "    ServerAlias %s\n", alias)
	}

	// 根目录
	root := s.Root
	if model.IsRootType(s.Type) {
		if rd := strings.TrimSpace(s.RuntimeDir); rd != "" {
			root = filepath.Join(root, strings.Trim(rd, "/"))
		}
	}
	fmt.Fprintf(&sb, "    DocumentRoot \"%s\"\n", root)

	if ssl {
		sb.WriteString("    SSLEngine on\n")
		if s.SslCertPath != "" {
			fmt.Fprintf(&sb, "    SSLCertificateFile \"%s\"\n", s.SslCertPath)
		}
		if s.SslKeyPath != "" {
			fmt.Fprintf(&sb, "    SSLCertificateKeyFile \"%s\"\n", s.SslKeyPath)
		}
	}

	sb.WriteString("\n    <Directory \"" + root + "\">\n")
	sb.WriteString("        Options FollowSymLinks -Indexes\n")
	sb.WriteString("        AllowOverride All\n")
	sb.WriteString("        Require all granted\n")
	sb.WriteString("    </Directory>\n\n")

	// 站点级 IP 黑名单（Require not ip）
	if ips := effectiveSiteBlockIPs(s.ID); len(ips) > 0 {
		sb.WriteString("    <Directory \"" + root + "\">\n")
		for _, ip := range ips {
			fmt.Fprintf(&sb, "        Require not ip %s\n", ip)
		}
		sb.WriteString("        Require all granted\n")
		sb.WriteString("    </Directory>\n\n")
	}

	// 敏感文件/目录保护
	if model.IsRootType(s.Type) {
		sb.WriteString("    <FilesMatch \"^\\.(git|svn|env|user\\.ini|htaccess)$|^(LICENSE|README\\.md)$\">\n")
		sb.WriteString("        Require all denied\n")
		sb.WriteString("    </FilesMatch>\n\n")
	}

	// 重定向规则（Apache RewriteRule）
	if len(s.Redirects) > 0 {
		for _, r := range s.Redirects {
			genApacheRedirectRule(&sb, r)
		}
	} else if u := strings.TrimSpace(s.RedirectURL); u != "" {
		code := s.RedirectCode
		if code != 301 && code != 302 && code != 307 && code != 308 {
			code = 301
		}
		sb.WriteString("    RewriteEngine On\n")
		if s.RedirectKeepPath {
			fmt.Fprintf(&sb, "    RewriteRule ^(.*)$ %s$1 [R=%d,L]\n", u, code)
		} else {
			fmt.Fprintf(&sb, "    RewriteRule ^(.*)$ %s [R=%d,L]\n", u, code)
		}
	}

	// 站点类型分支
	switch s.Type {
	case model.SiteTypeStatic:
		// 伪静态规则：写进 VirtualHost 的 RewriteRule（或在 .htaccess）
		if rw := strings.TrimSpace(s.Rewrite); rw != "" {
			sb.WriteString("    RewriteEngine On\n")
			sb.WriteString("    # 伪静态规则\n")
			sb.WriteString(apacheRewriteBlock(rw))
		}
		// 默认文档
		genApacheDirectoryIndex(&sb, s, root)

	case model.SiteTypePHP:
		// 伪静态
		if rw := strings.TrimSpace(s.Rewrite); rw != "" {
			sb.WriteString("    RewriteEngine On\n")
			sb.WriteString(apacheRewriteBlock(rw))
		}
		genApacheDirectoryIndex(&sb, s, root)
		// PHP 通过 php-fpm (mod_proxy_fcgi)
		fpm := strings.TrimSpace(s.PhpFpm)
		if fpm == "" {
			fpm = defaultPhpFpm()
		}
		// php-fpm socket 形如 unix:/run/php/php8.1-fpm.sock
		if strings.HasPrefix(fpm, "unix:") {
			sock := strings.TrimPrefix(fpm, "unix:")
			sb.WriteString("    <FilesMatch \\.php$>\n")
			sb.WriteString("        SetHandler \"proxy:unix:" + sock + "|fcgi://localhost\"\n")
			sb.WriteString("    </FilesMatch>\n")
		} else if strings.Contains(fpm, ":") {
			// tcp 形如 127.0.0.1:9000
			sb.WriteString("    <FilesMatch \\.php$>\n")
			sb.WriteString("        SetHandler \"proxy:fcgi://" + fpm + "\"\n")
			sb.WriteString("    </FilesMatch>\n")
		}

	default: // node / python / go / proxy 反向代理
		sb.WriteString("    ProxyPreserveHost On\n")
		fmt.Fprintf(&sb, "    ProxyPass / %s/\n", s.ProxyPass)
		fmt.Fprintf(&sb, "    ProxyPassReverse / %s/\n", s.ProxyPass)
	}

	// 防盗链
	if s.HotlinkEnabled && strings.TrimSpace(s.HotlinkReferers) != "" {
		sb.WriteString("    RewriteEngine On\n")
		sb.WriteString("    RewriteCond %{HTTP_REFERER} !^$\n")
		refers := strings.Split(s.HotlinkReferers, ",")
		conds := make([]string, 0, len(refers))
		for _, r := range refers {
			conds = append(conds, "!^https?://([^/]*\\.)?"+strings.TrimSpace(r))
		}
		sb.WriteString("    RewriteCond %{HTTP_REFERER} " + strings.Join(conds, " [NC] ") + " [NC]\n")
		sb.WriteString("    RewriteRule \\.(gif|jpg|jpeg|png|bmp|swf|webp|ico|css|js|woff2?|ttf|svg|mp4|mp3)$ - [F,NC]\n")
	}

	// 安全响应头
	if s.SecurityHeaders {
		sb.WriteString("    Header always set X-Frame-Options \"SAMEORIGIN\"\n")
		sb.WriteString("    Header always set X-Content-Type-Options \"nosniff\"\n")
		sb.WriteString("    Header always set X-XSS-Protection \"1; mode=block\"\n")
		sb.WriteString("    Header always set Referrer-Policy \"strict-origin-when-cross-origin\"\n")
	}
	// HSTS
	if ssl {
		sb.WriteString("    Header always set Strict-Transport-Security \"max-age=31536000; includeSubDomains\"\n")
	}

	// 日志
	fmt.Fprintf(&sb, "    CustomLog /var/log/apache2/%s.access.log combined\n", s.Name)
	fmt.Fprintf(&sb, "    ErrorLog /var/log/apache2/%s.error.log\n", s.Name)

	// 全局 WAF 站点片段（include 到 VirtualHost 内，支持单站跳过）
	if inc := apacheWAFIncludeLine(s.Name, s.ID); inc != "" {
		sb.WriteString(inc)
	}

	// 单站安全片段（apache 版：安全规则直接内联，或 include 片段文件）
	if inc := siteSecApacheBlock(s.ID); inc != "" {
		sb.WriteString(inc)
	}

	sb.WriteString("</VirtualHost>\n")
	return sb.String()
}

// apacheRewriteBlock 将伪静态规则包装成 Apache RewriteRule 块
func apacheRewriteBlock(rw string) string {
	// 用户伪静态规则通常是 nginx 风格的 rewrite/location，简单情况下直接原样输出并加注释。
	// 对于常见 SPA 场景（try_files $uri /index.html），转成 Apache RewriteRule。
	lines := strings.Split(rw, "\n")
	var sb strings.Builder
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			sb.WriteString("    " + ln + "\n")
			continue
		}
		// 检测 nginx rewrite 规则：rewrite ^(.*)$ /index.php$1 last;
		if strings.HasPrefix(ln, "rewrite ") {
			// 转 Apache RewriteRule
			sb.WriteString("    # " + ln + " (nginx 语法，已近似转换)\n")
			// 简单近似：rewrite ^xxx $yyy last -> RewriteRule ^xxx yyy [L]
			fields := strings.Fields(ln)
			if len(fields) >= 3 {
				src := fields[1]
				dst := fields[2]
				flag := "[L]"
				if strings.Contains(ln, "break") {
					flag = "[L]"
				}
				sb.WriteString("    RewriteRule " + src + " " + dst + " " + flag + "\n")
			}
			continue
		}
		if strings.Contains(ln, "try_files") {
			// try_files $uri $uri/ /index.php?$query_string -> 近似 RewriteCond/Rule
			sb.WriteString("    # " + ln + " (nginx try_files，近似转换)\n")
			sb.WriteString("    RewriteCond %{REQUEST_FILENAME} !-f\n")
			sb.WriteString("    RewriteCond %{REQUEST_FILENAME} !-d\n")
			sb.WriteString("    RewriteRule ^(.*)$ /index.php [L]\n")
			continue
		}
		// 其余原样（可能是 .htaccess 风格）
		sb.WriteString("    " + ln + "\n")
	}
	return sb.String()
}

// genApacheDirectoryIndex 生成 Apache 默认文档目录配置
func genApacheDirectoryIndex(sb *strings.Builder, s *model.Site, root string) {
	index := strings.TrimSpace(s.DefaultIndex)
	if index == "" {
		index = defaultIndexForType(s.Type)
	}
	sb.WriteString("    <IfModule dir_module>\n")
	fmt.Fprintf(sb, "        DirectoryIndex %s\n", index)
	sb.WriteString("    </IfModule>\n\n")
}

// genApacheRedirectRule 生成多条重定向规则之一（按域名 / 目录）
func genApacheRedirectRule(sb *strings.Builder, r model.SiteRedirect) {
	target := strings.TrimSpace(r.TargetURL)
	if target == "" {
		return
	}
	code := r.Code
	if code != 301 && code != 302 && code != 307 && code != 308 {
		code = 301
	}
	suffix := ""
	if r.KeepPath {
		suffix = "$1"
	}
	sb.WriteString("    RewriteEngine On\n")
	switch r.MatchType {
	case model.RedirectMatchDomain:
		for _, d := range strings.Split(r.Domains, ",") {
			d = strings.TrimSpace(d)
			if d == "" {
				continue
			}
			sb.WriteString("    RewriteCond %{HTTP_HOST} ^" + strings.ReplaceAll(d, ".", "\\.") + "$ [NC]\n")
		}
		fmt.Fprintf(sb, "    RewriteRule ^(.*)$ %s%s [R=%d,L]\n", target, suffix, code)
	case model.RedirectMatchDir:
		for _, p := range strings.Split(r.Paths, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			fmt.Fprintf(sb, "    RewriteRule ^%s(.*)$ %s$1 [R=%d,L]\n", p, target, code)
		}
	}
}

// sitePrimaryName 站点主域名（ServerName）
func sitePrimaryName(s *model.Site) string {
	if d := strings.TrimSpace(s.Domain); d != "" {
		return strings.Split(d, ",")[0]
	}
	names := siteServerNames(s)
	if len(names) > 0 {
		return names[0]
	}
	return "_"
}
