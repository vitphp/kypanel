package service

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"kypanel/internal/model"
)

// ============================ Web 服务器抽象层 ============================
//
// 面板同一时间只装一种 Web 服务器（nginx 与 apache 互斥），本层负责：
//  1. 探测当前安装的是 nginx 还是 apache（优先读 env_status.json，兜底探测二进制）
//  2. 统一配置目录 / 配置文件路径 / 校验 / 重载
//  3. 让建站、SSL、单站安全等逻辑按 Web 服务器类型自动走对应分支

const (
	webNginx  = "nginx"
	webApache = "apache"

	apacheSitesAvailable = "/etc/apache2/sites-available"
	apacheSitesEnabled   = "/etc/apache2/sites-enabled"
	apacheConfEnabled    = "/etc/apache2/conf-enabled"
	apacheSslDir         = "/etc/apache2/ssl"
)

// WebServerType 返回当前安装的 Web 服务器类型（"nginx" / "apache"）
// 直接探测二进制（即时权威，不依赖可能过期的 env_status.json 缓存）
func WebServerType() string {
	if _, err := exec.LookPath("nginx"); err == nil {
		return webNginx
	}
	if _, err := exec.LookPath("apache2"); err == nil {
		return webApache
	}
	if _, err := exec.LookPath("httpd"); err == nil {
		return webApache
	}
	// 兜底读环境状态文件
	if st := EnvStatus(); st != nil {
		if a, ok := st["apache"]; ok && a.Installed {
			return webApache
		}
	}
	return webNginx
}

// webServerAvailable 校验 Web 服务器已安装（返回具体错误提示）
func webServerAvailable() error {
	switch WebServerType() {
	case webApache:
		if _, err := exec.LookPath("apache2"); err == nil {
			return nil
		}
		if _, err := exec.LookPath("httpd"); err == nil {
			return nil
		}
		return errors.New("Apache 未安装，请先在「应用商店」安装 Apache")
	default:
		if _, err := exec.LookPath("nginx"); err != nil {
			return errors.New("Nginx 未安装，请先在「应用商店」安装 Nginx")
		}
		return nil
	}
}

// webConfigTest 校验 Web 服务器配置（nginx -t / apachectl configtest）
// 校验失败时先自愈（修复无效站点证书），再重试一次，避免单个站点的坏证书
// 导致 nginx 全局配置校验失败、拖垮所有网站。
func webConfigTest() error {
	if WebServerType() == webApache {
		if err := apacheConfigTest(); err == nil {
			return nil
		}
		_ = selfHealAllSiteCerts()
		return apacheConfigTest()
	}
	if err := nginxTest(); err == nil {
		return nil
	}
	_ = selfHealAllSiteCerts()
	return nginxTest()
}

// webReload 重载 Web 服务器
func webReload() error {
	if WebServerType() == webApache {
		return apacheReload()
	}
	return nginxReload()
}

// apacheConfigTest 校验 apache 配置
func apacheConfigTest() error {
	cmd := "apachectl configtest"
	if _, err := exec.LookPath("apache2ctl"); err == nil {
		cmd = "apache2ctl configtest"
	}
	res, err := ExecCommand(cmd, 30*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("Apache 配置校验失败: %s", strings.TrimSpace(res.Stderr+res.Stdout))
	}
	return nil
}

// apacheReload 重载 apache
func apacheReload() error {
	cmd := "apachectl graceful"
	if _, err := exec.LookPath("apache2ctl"); err == nil {
		cmd = "apache2ctl graceful"
	}
	_, err := ExecCommand(cmd, 30*time.Second)
	return err
}

// siteConfPathFor 返回站点配置文件路径（按 Web 服务器类型）
func siteConfPathFor(name, wsType string) string {
	if wsType == webApache {
		return filepath.Join(apacheSitesAvailable, "lp_"+name+".conf")
	}
	return filepath.Join(nginxConfDir, "lp_"+name+".conf")
}

// removeSiteConfFile 删除站点配置文件（nginx：删 conf；apache：a2dissite + 删 sites-available）
func removeSiteConfFile(name string) error {
	ws := WebServerType()
	if ws == webApache {
		_ = apacheDisableSite(name)
		return os.Remove(filepath.Join(apacheSitesAvailable, "lp_"+name+".conf"))
	}
	return os.Remove(siteConfPath(name))
}

// siteActiveCheckCmd 返回「检查站点是否在运行」的命令（按 Web 服务器类型）
func siteActiveCheckCmd(name string) string {
	if WebServerType() == webApache {
		// 检查 sites-enabled 里是否有该站点软链
		return fmt.Sprintf("test -e /etc/apache2/sites-enabled/lp_%s.conf && echo 1 || echo 0", name)
	}
	return fmt.Sprintf("nginx -T 2>/dev/null | grep -c 'lp_%s\\.conf'", name)
}

// siteAccessLogPath 站点访问日志路径（按 Web 服务器类型）
func siteAccessLogPath(name string) string {
	if WebServerType() == webApache {
		return filepath.Join("/var/log/apache2", name+".access.log")
	}
	return filepath.Join("/var/log/nginx", name+".access.log")
}

// siteSSLPathFor 证书文件路径（按 Web 服务器类型）
func siteSSLPathFor(name, wsType string) (cert, key string) {
	if wsType == webApache {
		return filepath.Join(apacheSslDir, "lp_"+name+".pem"), filepath.Join(apacheSslDir, "lp_"+name+".key")
	}
	return filepath.Join(sslDir, "lp_"+name+".pem"), filepath.Join(sslDir, "lp_"+name+".key")
}

// apacheEnabled 检查 apache 站点是否已 a2ensite（软链存在）
func apacheEnabled(name string) bool {
	link := filepath.Join(apacheSitesEnabled, "lp_"+name+".conf")
	_, err := os.Lstat(link)
	return err == nil
}

// apacheEnableSite a2ensite 启用站点（软链）
func apacheEnableSite(name string) error {
	if apacheEnabled(name) {
		return nil
	}
	src := filepath.Join(apacheSitesAvailable, "lp_"+name+".conf")
	dst := filepath.Join(apacheSitesEnabled, "lp_"+name+".conf")
	return os.Symlink(src, dst)
}

// apacheDisableSite a2dissite 停用站点（移除软链）
func apacheDisableSite(name string) error {
	link := filepath.Join(apacheSitesEnabled, "lp_"+name+".conf")
	if _, err := os.Lstat(link); err == nil {
		return os.Remove(link)
	}
	return nil
}

// apacheModuleEnabled 检查 apache 模块是否已启用（conf-enabled 软链）
func apacheModuleEnabled(mod string) bool {
	for _, p := range []string{
		filepath.Join("/etc/apache2/mods-enabled", mod+".load"),
		filepath.Join("/etc/apache2/mods-enabled", mod+".conf"),
	} {
		if _, err := os.Lstat(p); err == nil {
			return true
		}
	}
	return false
}

// apacheEnableModule 启用 apache 模块（a2enmod）
func apacheEnableModule(mod string) error {
	if apacheModuleEnabled(mod) {
		return nil
	}
	_, err := ExecCommand("a2enmod "+mod, 30*time.Second)
	return err
}

// ensureApacheModules 根据站点类型自动启用所需 apache 模块
func ensureApacheModules(s *model.Site) error {
	mods := []string{"rewrite", "headers", "dir", "mime", "alias"}
	switch s.Type {
	case model.SiteTypePHP:
		// php-fpm 方式：需要 proxy + proxy_fcgi
		mods = append(mods, "proxy", "proxy_fcgi")
	default: // node / python / go / proxy 反向代理
		if s.ProxyPass != "" {
			mods = append(mods, "proxy", "proxy_http")
		}
	}
	if s.SslEnabled {
		mods = append(mods, "ssl")
	}
	for _, m := range mods {
		if err := apacheEnableModule(m); err != nil {
			return fmt.Errorf("启用 apache 模块 %s 失败: %w", m, err)
		}
	}
	return nil
}
