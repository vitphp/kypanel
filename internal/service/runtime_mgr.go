package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ==================== 版本解析与路径探测 ====================

// runtimeLangVer 从 app 名称（如 "PHP 7.4" / "Node 20" / "Python 3.10" / "Go 1.21"）解析语言与版本号
func runtimeLangVer(name string) (lang, ver string) {
	n := strings.TrimSpace(name)
	lower := strings.ToLower(n)
	switch {
	case strings.HasPrefix(lower, "php"):
		lang = "php"
		ver = strings.TrimSpace(n[len("php"):])
	case strings.HasPrefix(lower, "python"):
		lang = "python"
		ver = strings.TrimSpace(n[len("python"):])
	case strings.HasPrefix(lower, "node"):
		lang = "node"
		ver = strings.TrimSpace(n[len("node"):])
	case strings.HasPrefix(lower, "go"):
		lang = "go"
		ver = strings.TrimSpace(n[len("go"):])
	default:
		lang = lower
	}
	return
}

// phpEnv PHP 各版本环境路径（兼容 Debian sury 与 RHEL remi 布局）
type phpEnv struct {
	Version     string // 如 7.4
	Bin         string // php 可执行文件
	Service     string // systemd 服务名
	IniFpm      string // fpm 用的 php.ini
	IniCli      string // cli 用的 php.ini
	FpmConf     string // php-fpm.conf
	PoolConf    string // pool.d/www.conf
	ConfDir     string // fpm/conf.d（扩展启用目录）
	ModsDir     string // mods-available（扩展可用目录）
	ErrorLog    string // fpm 错误日志
	SlowLog     string // 慢日志（从 pool 配置读取）
	Remi        bool
}

// phpEnvFor 探测指定版本的 PHP 环境；ver 为空时用默认 php 命令探测实际版本
func phpEnvFor(ver string) *phpEnv {
	if ver == "" {
		out := execOut("php -r 'echo PHP_VERSION;' 2>/dev/null")
		if out != "" {
			if m := regexp.MustCompile(`^(\d+\.\d+)`).FindStringSubmatch(out); len(m) >= 2 {
				ver = m[1]
			}
		}
	}
	if ver == "" {
		return nil
	}

	// RHEL/remi 布局优先探测
	remiNum := strings.Replace(ver, ".", "", -1)
	remiDirs := []string{
		"/etc/opt/remi/php" + remiNum,
		"/opt/remi/php" + remiNum + "/root/etc",
	}
	for _, d := range remiDirs {
		if _, err := os.Stat(d); err == nil {
			e := &phpEnv{
				Version:  ver,
				Bin:      "/opt/remi/php" + remiNum + "/root/usr/bin/php",
				Service:  "php" + remiNum + "-php-fpm",
				IniFpm:   d + "/php.ini",
				IniCli:   d + "/php.ini",
				FpmConf:  d + "/php-fpm.conf",
				PoolConf: d + "/php-fpm.d/www.conf",
				ConfDir:  d + "/php.d",
				ModsDir:  d + "/php.d",
				ErrorLog: "/var/opt/remi/php" + remiNum + "/log/php-fpm/error.log",
				Remi:     true,
			}
			return e
		}
	}

	// Debian sury 布局
	debianDir := "/etc/php/" + ver
	if _, err := os.Stat(debianDir); err == nil {
		e := &phpEnv{
			Version:  ver,
			Bin:      "/usr/bin/php" + ver,
			Service:  "php" + ver + "-fpm",
			IniFpm:   debianDir + "/fpm/php.ini",
			IniCli:   debianDir + "/cli/php.ini",
			FpmConf:  debianDir + "/fpm/php-fpm.conf",
			PoolConf: debianDir + "/fpm/pool.d/www.conf",
			ConfDir:  debianDir + "/fpm/conf.d",
			ModsDir:  debianDir + "/mods-available",
			ErrorLog: "/var/log/php" + ver + "-fpm.log",
		}
		// 从 pool 配置探测 slowlog
		if b, err := os.ReadFile(e.PoolConf); err == nil {
			if m := regexp.MustCompile(`^\s*slowlog\s*=\s*(\S+)`).FindStringSubmatch(string(b)); len(m) >= 2 {
				e.SlowLog = m[1]
			}
		}
		return e
	}

	// 兜底：常见自定义编译路径
	for _, base := range []string{"/usr/local/php" + remiNum, "/opt/php" + remiNum} {
		if _, err := os.Stat(base + "/etc/php-fpm.d/www.conf"); err == nil {
			return &phpEnv{
				Version:  ver,
				Bin:      base + "/bin/php",
				Service:  "php-fpm",
				IniFpm:   base + "/etc/php.ini",
				IniCli:   base + "/etc/php.ini",
				FpmConf:  base + "/etc/php-fpm.conf",
				PoolConf: base + "/etc/php-fpm.d/www.conf",
				ConfDir:  base + "/etc/php.d",
				ModsDir:  base + "/etc/php.d",
				ErrorLog: "/var/log/php-fpm.log",
			}
		}
	}
	return nil
}

// execOut 执行命令返回 stdout（忽略错误）
func execOut(cmdStr string) string {
	res, err := ExecCommand(cmdStr, 15*time.Second)
	if err != nil || res == nil {
		return ""
	}
	return res.Stdout
}

// execOutE 执行命令返回 stdout + err
func execOutE(cmdStr string, timeout time.Duration) (string, error) {
	res, err := ExecCommand(cmdStr, timeout)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout + "\n" + res.Stderr), nil
}

// ==================== 通用 RuntimeInfo ====================

// RuntimeInfo 返回某语言环境的版本 / 路径 / 运行状态信息
func RuntimeInfo(name string) (map[string]interface{}, error) {
	lang, ver := runtimeLangVer(name)
	info := map[string]interface{}{
		"lang": lang, "version": ver, "name": name,
		"bin": "", "path": "", "status": "unknown", "detail": "",
	}
	switch lang {
	case "php":
		e := phpEnvFor(ver)
		if e == nil {
			info["status"] = "not_found"
			return info, nil
		}
		info["bin"] = e.Bin
		info["path"] = e.IniCli
		info["ini_fpm"] = e.IniFpm
		info["ini_cli"] = e.IniCli
		info["fpm_conf"] = e.FpmConf
		info["pool_conf"] = e.PoolConf
		info["service"] = e.Service
		info["conf_dir"] = e.ConfDir
		info["error_log"] = e.ErrorLog
		info["slow_log"] = e.SlowLog
		info["status"], info["detail"] = phpRunning(e)
		info["version"] = e.Version
		if v := execOut(e.Bin + " -v 2>/dev/null | head -n1"); v != "" {
			info["full_version"] = strings.TrimSpace(v)
		}
	case "python":
		bin := pythonBin(ver)
		info["bin"] = bin
		info["path"] = bin
		info["service"] = "" // Python 解释器无独立 systemd 服务，随站点/脚本进程运行
		if v := execOut(bin + " -V 2>&1"); v != "" {
			info["full_version"] = strings.TrimSpace(v)
			info["ready"] = true
		}
		info["status"], info["processes"] = langProcessStatus("python", bin)
	case "node":
		bin := nodeBin(ver)
		info["bin"] = bin
		info["path"] = bin
		info["service"] = "" // Node 运行时无独立 systemd 服务，进程由 PM2/systemd 管理
		info["npm"] = nodeNpm(ver)
		if v := execOut(bin + " -v 2>&1"); v != "" {
			info["full_version"] = strings.TrimSpace(v)
			info["ready"] = true
		}
		info["status"], info["processes"] = langProcessStatus("node", bin)
	case "go":
		bin := goBin(ver)
		info["bin"] = bin
		info["path"] = bin
		info["service"] = "" // Go 编译器无独立 systemd 服务，编译产物自行以 systemd/进程管理
		info["goroot"] = strings.TrimSpace(execOut(bin + " env GOROOT 2>/dev/null"))
		info["gopath"] = strings.TrimSpace(execOut(bin + " env GOPATH 2>/dev/null"))
		if v := execOut(bin + " version 2>&1"); v != "" {
			info["full_version"] = strings.TrimSpace(v)
			info["ready"] = true
		}
		info["status"], info["processes"] = langProcessStatus("go", bin)
	}
	return info, nil
}

// phpRunning 检测 PHP-FPM 是否在运行
func phpRunning(e *phpEnv) (status, detail string) {
	out := execOut("ps aux 2>/dev/null | grep -E 'php-fpm: (master|process)' | grep -v grep | head -n1")
	if strings.Contains(out, "php-fpm: master") {
		// 统计 worker 数
		n := execOut("ps aux 2>/dev/null | grep 'php-fpm: pool' | grep -v grep | wc -l")
		if n == "" {
			n = "0"
		}
		return "running", "运行中（" + strings.TrimSpace(n) + " 个 worker 进程）"
	}
	if _, err := os.Stat("/run/php/" + e.Version + "-fpm.sock"); err == nil {
		return "running", "运行中（socket 存在）"
	}
	return "stopped", "未运行"
}

// RuntimeService 服务启停（目前 PHP-FPM 有 systemd 服务，其余语言随站点运行）
func RuntimeService(name, action string) (map[string]interface{}, error) {
	lang, ver := runtimeLangVer(name)
	if lang == "php" {
		e := phpEnvFor(ver)
		if e == nil {
			return nil, fmt.Errorf("未检测到 PHP %s 环境", ver)
		}
		switch action {
		case "start", "stop", "restart":
			_, err := execOutE(fmt.Sprintf("systemctl %s %s 2>&1", action, e.Service), 30*time.Second)
			if err != nil {
				// 兜底直接操作进程
				pkill := "start"
				switch action {
				case "stop":
					pkill = "php-fpm"
					execOut("pkill -TERM " + pkill + " 2>/dev/null || true")
				case "restart":
					execOut("pkill -TERM php-fpm 2>/dev/null || true; sleep 1")
					_, err2 := execOutE(e.Bin+"-fpm -y "+e.FpmConf+" 2>&1", 10*time.Second)
					if err2 == nil {
						return map[string]interface{}{"ok": true, "msg": "PHP-FPM 已重启（手动模式）"}, nil
					}
				case "start":
					_, err2 := execOutE(e.Bin+"-fpm -y "+e.FpmConf+" 2>&1", 10*time.Second)
					if err2 == nil {
						return map[string]interface{}{"ok": true, "msg": "PHP-FPM 已启动（手动模式）"}, nil
					}
				}
				return nil, err
			}
		}
		time.Sleep(800 * time.Millisecond)
		st, det := phpRunning(e)
		return map[string]interface{}{"ok": true, "status": st, "detail": det}, nil
	}
	// Python/Node/Go 无独立 systemd 服务：
	//   - start: 解释器无需启动，返回说明让前端用「重新检测」代替
	//   - stop:  终止所有该语言运行进程（pkill -TERM → -KILL）
	//   - restart: 停止 + 重新检测（解释器本身不需要启动）
	switch action {
	case "stop":
		pattern := langProcessPattern(lang)
		if pattern == "" {
			return nil, fmt.Errorf("不支持的运行时语言: %s", lang)
		}
		out, _ := execOutE(fmt.Sprintf("pkill -TERM -f '%s' 2>&1; sleep 1; pkill -KILL -f '%s' 2>/dev/null || true", pattern, pattern), 15*time.Second)
		return map[string]interface{}{"ok": true, "msg": "已发送终止信号", "detail": out}, nil
	case "restart":
		pattern := langProcessPattern(lang)
		if pattern != "" {
			execOut(fmt.Sprintf("pkill -TERM -f '%s' 2>/dev/null || true", pattern))
			time.Sleep(1 * time.Second)
			execOut(fmt.Sprintf("pkill -KILL -f '%s' 2>/dev/null || true", pattern))
		}
		return map[string]interface{}{"ok": true, "msg": "已重置运行时进程"}, nil
	case "start":
		return map[string]interface{}{"ok": true, "msg": lang + " 解释器无需启动，可点击「重新检测」刷新状态"}, nil
	}
	return map[string]interface{}{"ok": true, "msg": lang + " 无独立系统服务，随站点进程运行"}, nil
}

// langProcessPattern 返回语言对应的进程匹配关键字（与 langProcessStatus 保持一致）
func langProcessPattern(lang string) string {
	switch lang {
	case "python":
		return "python"
	case "node":
		return "node "
	case "go":
		return "go run |/go/bin/|go-build"
	}
	return ""
}

// ==================== PHP 扩展 ====================

// phpExtCatalog 可安装的 PHP 扩展目录（常见扩展，apt 包名 = php<ver>-<name>）
var phpExtCatalog = []string{
	"bcmath", "bz2", "calendar", "curl", "dba", "exif", "gd", "gettext", "gmp",
	"igbinary", "imagick", "imap", "intl", "ldap", "mbstring", "memcached", "mongodb",
	"msgpack", "mysql", "opcache", "pcntl", "pgsql", "redis", "soap", "sockets",
	"sqlite3", "ssh2", "swoole", "xml", "xmlrpc", "xsl", "yaml", "zip",
}

// PhpExtList 返回 PHP 已安装扩展与可安装扩展
func PhpExtList(version string) (map[string]interface{}, error) {
	e := phpEnvFor(version)
	if e == nil {
		return nil, fmt.Errorf("未检测到 PHP 环境")
	}
	installed := map[string]bool{}
	// 1. conf.d / mods-available 目录的 .ini 文件名
	for _, dir := range []string{e.ConfDir, e.ModsDir} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, en := range entries {
			name := strings.TrimSuffix(en.Name(), ".ini")
			if name != "" {
				installed[name] = true
			}
		}
	}
	// 2. php -m 模块列表（内置扩展）
	if out := execOut(e.Bin + " -m 2>/dev/null"); out != "" {
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || line == "[PHP Modules]" || line == "[Zend Modules]" ||
				strings.HasPrefix(line, "[") || strings.Contains(line, " ") {
				continue
			}
			installed[strings.ToLower(line)] = true
		}
	}
	installedList := make([]string, 0, len(installed))
	for k := range installed {
		installedList = append(installedList, k)
	}
	sort.Strings(installedList)

	available := make([]string, 0)
	for _, ext := range phpExtCatalog {
		if !installed[ext] {
			available = append(available, ext)
		}
	}
	sort.Strings(available)
	return map[string]interface{}{
		"installed": installedList,
		"available": available,
	}, nil
}

// phpExtAptPkg 计算扩展的 apt 包名（remi 特殊处理）
func phpExtAptPkg(e *phpEnv, ext string) string {
	if e.Remi {
		return "php" + strings.Replace(e.Version, ".", "", -1) + "-php-" + ext
	}
	return "php" + e.Version + "-" + ext
}

// PhpExtInstall 安装 PHP 扩展（apt/dnf 优先）
func PhpExtInstall(version, ext string) error {
	e := phpEnvFor(version)
	if e == nil {
		return fmt.Errorf("未检测到 PHP 环境")
	}
	pkg := phpExtAptPkg(e, ext)
	pm := "apt-get"
	if _, err := os.Stat("/usr/bin/dnf"); err == nil {
		pm = "dnf"
	} else if _, err := os.Stat("/usr/bin/yum"); err == nil {
		pm = "yum"
	}
	var cmd string
	if pm == "apt-get" {
		cmd = fmt.Sprintf("export DEBIAN_FRONTEND=noninteractive; apt-get update -y >/dev/null 2>&1; apt-get install -y %s 2>&1 || { echo APT_FAIL; pecl install %s 2>&1 | tail -n5; }", pkg, ext)
	} else {
		cmd = fmt.Sprintf("%s install -y %s 2>&1 || { echo RPM_FAIL; pecl install %s 2>&1 | tail -n5; }", pm, pkg, ext)
	}
	res, err := execOutE(cmd, 5*time.Minute)
	if err != nil {
		return err
	}
	if strings.Contains(res, "APT_FAIL") || strings.Contains(res, "RPM_FAIL") {
		return fmt.Errorf("扩展 %s 安装失败：%s", ext, tailLines(res, 3))
	}
	// 尝试启用扩展
	execOut(fmt.Sprintf("phpenmod -v %s %s 2>/dev/null || true", e.Version, ext))
	execOut(fmt.Sprintf("systemctl restart %s 2>/dev/null || true", e.Service))
	return nil
}

// PhpExtUninstall 卸载 PHP 扩展
func PhpExtUninstall(version, ext string) error {
	e := phpEnvFor(version)
	if e == nil {
		return fmt.Errorf("未检测到 PHP 环境")
	}
	pkg := phpExtAptPkg(e, ext)
	pm := "apt-get"
	if _, err := os.Stat("/usr/bin/dnf"); err == nil {
		pm = "dnf"
	} else if _, err := os.Stat("/usr/bin/yum"); err == nil {
		pm = "yum"
	}
	if pm == "apt-get" {
		_, err := execOutE(fmt.Sprintf("export DEBIAN_FRONTEND=noninteractive; apt-get remove -y --purge %s 2>&1 || true; apt-get autoremove -y >/dev/null 2>&1 || true", pkg), 3*time.Minute)
		if err != nil {
			return err
		}
	} else {
		execOut(fmt.Sprintf("%s remove -y %s 2>/dev/null || true", pm, pkg))
	}
	execOut(fmt.Sprintf("phpdismod -v %s %s 2>/dev/null || true", e.Version, ext))
	execOut(fmt.Sprintf("systemctl restart %s 2>/dev/null || true", e.Service))
	return nil
}

// ==================== PHP ini 读写 ====================

// phpIniSections 配置修改页各分组及键
var phpIniSections = []map[string]string{
	{"key": "基础配置", "items": "short_open_tag|memory_limit|error_reporting|display_errors|expose_php|date.timezone|realpath_cache_size"},
	{"key": "上传限制", "items": "upload_max_filesize|post_max_size|max_file_uploads"},
	{"key": "超时限制", "items": "max_execution_time|max_input_time|default_socket_timeout"},
	{"key": "Session", "items": "session.save_handler|session.save_path|session.cookie_lifetime|session.gc_maxlifetime|session.use_cookies"},
	{"key": "禁用函数", "items": "disable_functions"},
}

// PhpIniGet 读取 PHP-FPM php.ini 的关键配置
func PhpIniGet(version string) (map[string]interface{}, error) {
	e := phpEnvFor(version)
	if e == nil {
		return nil, fmt.Errorf("未检测到 PHP 环境")
	}
	content, err := os.ReadFile(e.IniFpm)
	if err != nil {
		content, err = os.ReadFile(e.IniCli)
		if err != nil {
			return nil, fmt.Errorf("无法读取 php.ini：%v", err)
		}
	}
	cfg := readIniValues(string(content))
	cfg["_file"] = e.IniFpm
	return map[string]interface{}{
		"config":   cfg,
		"sections": phpIniSections,
	}, nil
}

// readIniValues 解析 php.ini 关键项（保留注释项）
func readIniValues(content string) map[string]string {
	out := map[string]string{}
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, ";") {
			continue
		}
		idx := strings.Index(trimmed, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:idx])
		val := strings.TrimSpace(trimmed[idx+1:])
		if _, ok := out[key]; !ok {
			out[key] = val
		}
	}
	return out
}

// PhpIniSet 保存 php.ini 配置（更新多个键，成功返回受影响的键）
func PhpIniSet(version string, updates map[string]string) (map[string]interface{}, error) {
	e := phpEnvFor(version)
	if e == nil {
		return nil, fmt.Errorf("未检测到 PHP 环境")
	}
	path := e.IniFpm
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("无法读取 php.ini：%v", err)
	}
	changed, err := setIniValues(string(content), updates)
	if err != nil {
		return nil, err
	}
	// 备份后写回
	if err := backupFile(path); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(changed), 0o644); err != nil {
		return nil, fmt.Errorf("写入 php.ini 失败：%v", err)
	}
	execOut(fmt.Sprintf("systemctl restart %s 2>/dev/null || true", e.Service))
	return map[string]interface{}{"updated": keysOf(updates)}, nil
}

// setIniValues 批量替换/新增 php.ini 键值
func setIniValues(content string, updates map[string]string) (string, error) {
	lines := strings.Split(content, "\n")
	matched := map[string]bool{}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ";") {
			continue // 注释行不处理（避免破坏示例注释）
		}
		idx := strings.Index(trimmed, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:idx])
		if newVal, ok := updates[key]; ok {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = indent + key + " = " + newVal
			matched[key] = true
		}
	}
	// 追加未匹配的键（去重）
	appended := make([]string, 0)
	for k, v := range updates {
		if matched[k] {
			continue
		}
		// 若已有注释版本，在注释行后插入；否则追加到末尾
		inserted := false
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, ";") && strings.Contains(trimmed, k+" ") {
				lines[i] = k + " = " + v
				matched[k] = true
				inserted = true
				break
			}
		}
		if !inserted {
			appended = append(appended, k+" = "+v)
		}
	}
	if len(appended) > 0 {
		content = strings.Join(lines, "\n")
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += strings.Join(appended, "\n") + "\n"
	} else {
		content = strings.Join(lines, "\n")
	}
	return content, nil
}

// backupFile 写配置前备份
func backupFile(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	_ = os.WriteFile(path+".bak", b, 0o644)
	return nil
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ==================== PHP-FPM 配置 ====================

// phpFpmKeys FPM pool 可调配置
var phpFpmKeys = []string{
	"listen", "listen.allowed_clients", "user", "group", "pm",
	"pm.max_children", "pm.start_servers", "pm.min_spare_servers", "pm.max_spare_servers",
	"pm.max_requests", "pm.status_path", "request_terminate_timeout", "slowlog", "catch_workers_output",
}

// PhpFpmConfigGet 读取 pool 配置
func PhpFpmConfigGet(version string) (map[string]interface{}, error) {
	e := phpEnvFor(version)
	if e == nil {
		return nil, fmt.Errorf("未检测到 PHP 环境")
	}
	var path string
	for _, p := range []string{e.PoolConf, e.FpmConf} {
		if _, err := os.Stat(p); err == nil {
			path = p
			break
		}
	}
	if path == "" {
		return nil, fmt.Errorf("未找到 FPM 配置文件")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := readIniValues(string(b))
	return map[string]interface{}{"config": cfg, "path": path, "keys": phpFpmKeys}, nil
}

// PhpFpmConfigSet 保存 pool 配置
func PhpFpmConfigSet(version string, updates map[string]string) (map[string]interface{}, error) {
	e := phpEnvFor(version)
	if e == nil {
		return nil, fmt.Errorf("未检测到 PHP 环境")
	}
	var path string
	for _, p := range []string{e.PoolConf, e.FpmConf} {
		if _, err := os.Stat(p); err == nil {
			path = p
			break
		}
	}
	if path == "" {
		return nil, fmt.Errorf("未找到 FPM 配置文件")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	changed, err := setIniValues(string(b), updates)
	if err != nil {
		return nil, err
	}
	if err := backupFile(path); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(changed), 0o644); err != nil {
		return nil, fmt.Errorf("写入 FPM 配置失败：%v", err)
	}
	execOut(fmt.Sprintf("systemctl restart %s 2>/dev/null || true", e.Service))
	return map[string]interface{}{"updated": keysOf(updates)}, nil
}

// ==================== PHP 负载状态 ====================

// PhpStatus 获取 PHP-FPM 负载状态（通过 pm.status_path 或 ps 统计）
func PhpStatus(version string) (map[string]interface{}, error) {
	e := phpEnvFor(version)
	if e == nil {
		return nil, fmt.Errorf("未检测到 PHP 环境")
	}
	out := map[string]interface{}{}
	// 尝试 status_path
	statusPath := ""
	if b, err := os.ReadFile(e.PoolConf); err == nil {
		if m := regexp.MustCompile(`^\s*pm\.status_path\s*=\s*(\S+)`).FindStringSubmatch(string(b)); len(m) >= 2 {
			statusPath = m[1]
		}
	}
	if statusPath != "" {
		sock := "/run/php/" + e.Version + "-fpm.sock"
		if e.Remi {
			sock = "/run/php-fpm/www.sock"
		}
		if _, err := os.Stat(sock); err == nil {
			raw := execOut(fmt.Sprintf("curl -s --unix-socket %s http://localhost%s 2>/dev/null", sock, statusPath))
			if raw != "" {
				status := parseFpmStatus(raw)
				out["status_page"] = true
				for k, v := range status {
					out[k] = v
				}
				return out, nil
			}
		}
	}
	// 回退：ps 统计
	st, det := phpRunning(e)
	out["status_page"] = false
	out["service_status"] = st
	out["service_detail"] = det
	masterPid := strings.TrimSpace(execOut("ps aux 2>/dev/null | grep 'php-fpm: master' | grep -v grep | awk '{print $2}' | head -n1"))
	workers := strings.TrimSpace(execOut("ps aux 2>/dev/null | grep 'php-fpm: pool' | grep -v grep | wc -l"))
	mem := strings.TrimSpace(execOut("ps aux 2>/dev/null | grep 'php-fpm: pool' | grep -v grep | awk '{s+=$6} END {print s}'"))
	out["master_pid"] = masterPid
	out["worker_count"] = workers
	if mem != "" {
		if mb, err := strconv.ParseFloat(mem, 64); err == nil {
			out["memory_mb"] = fmt.Sprintf("%.1f", mb/1024)
		}
	}
	if b, err := os.ReadFile(e.PoolConf); err == nil {
		if m := regexp.MustCompile(`^\s*pm\.max_children\s*=\s*(\d+)`).FindStringSubmatch(string(b)); len(m) >= 2 {
			out["max_children"] = m[1]
		}
	}
	out["hint"] = "未配置 pm.status_path，显示进程统计。可在「FPM 配置」设置 pm.status_path=/php_status 获取完整状态"
	return out, nil
}

// parseFpmStatus 解析 PHP-FPM status 页面 key: value 行
func parseFpmStatus(raw string) map[string]interface{} {
	out := map[string]interface{}{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, ":") {
			continue
		}
		idx := strings.Index(line, ":")
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		switch key {
		case "pool", "process manager", "start time", "start since", "accepted conn",
			"listen queue", "max listen queue", "listen queue len", "idle processes",
			"active processes", "total processes", "max active processes",
			"max children reached", "slow requests":
			out[strings.ReplaceAll(key, " ", "_")] = val
		}
	}
	return out
}

// ==================== PHP 日志 ====================

// PhpLog 读取 PHP-FPM 日志（type: fpm=错误日志 / slow=慢日志）
func PhpLog(version, logType string, lines int) (map[string]interface{}, error) {
	e := phpEnvFor(version)
	if e == nil {
		return nil, fmt.Errorf("未检测到 PHP 环境")
	}
	if lines <= 0 {
		lines = 200
	}
	path := e.ErrorLog
	if logType == "slow" {
		path = e.SlowLog
		if path == "" {
			return map[string]interface{}{
				"type": "slow", "path": "", "log": "",
				"hint": "未配置 slowlog，可在「FPM 配置」中设置 slowlog=/var/log/php" + e.Version + "-fpm.log.slow 并设置 request_slowlog_timeout",
			}, nil
		}
	}
	if _, err := os.Stat(path); err != nil {
		return map[string]interface{}{"type": logType, "path": path, "log": "", "hint": "日志文件不存在"}, nil
	}
	raw := execOut(fmt.Sprintf("tail -n %d %s 2>/dev/null", lines, shellQuote(path)))
	return map[string]interface{}{"type": logType, "path": path, "log": raw}, nil
}

func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

// ==================== PHP phpinfo 摘要 ====================

// PhpInfoPage 返回 phpinfo 摘要（版本/系统/SAPI/关键模块/核心参数）
func PhpInfoPage(version string) (map[string]interface{}, error) {
	e := phpEnvFor(version)
	if e == nil {
		return nil, fmt.Errorf("未检测到 PHP 环境")
	}
	out := map[string]interface{}{}
	out["version"] = e.Version
	// php -m 模块
	modRaw := execOut(e.Bin + " -m 2>/dev/null")
	mods := make([]string, 0)
	for _, line := range strings.Split(modRaw, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			continue
		}
		if line != "" && !strings.Contains(line, " ") {
			mods = append(mods, line)
		}
	}
	sort.Strings(mods)
	out["modules"] = mods
	// php -i 关键项
	infoRaw := execOut(e.Bin + " -i 2>/dev/null")
	info := map[string]string{}
	for _, line := range strings.Split(infoRaw, "\n") {
		for _, key := range []string{"PHP Version", "System", "Server API", "Thread Safety", "PHP API", "PHP Extension", "Zend Extension", "Loaded Configuration File", "Scan this dir for additional .ini files"} {
			if strings.HasPrefix(line, key+" =>") || strings.HasPrefix(line, key+": ") {
				val := strings.TrimPrefix(strings.TrimPrefix(line, key+" =>"), key+": ")
				info[key] = strings.TrimSpace(val)
				break
			}
		}
	}
	out["info"] = info
	// OPcache 状态
	opc := execOut(e.Bin + " -r 'var_export([\\\"enabled\\\"=>function_exists(\\\"opcache_get_status\\\")?(bool)@opcache_get_status(false)[\\\"opcache_enabled\\\"]:false]);' 2>/dev/null")
	out["opcache"] = strings.TrimSpace(opc)
	// 关键 ini（复用 PhpIniGet）
	if cfg, err := PhpIniGet(version); err == nil {
		if m, ok := cfg["config"].(map[string]string); ok {
			short := map[string]string{}
			for _, k := range []string{"memory_limit", "upload_max_filesize", "post_max_size", "max_execution_time", "date.timezone", "display_errors"} {
				if v, ok := m[k]; ok {
					short[k] = v
				}
			}
			out["ini"] = short
		}
	}
	return out, nil
}

// ==================== Python / Node / Go ====================

// langProcessStatus 检测某语言的运行进程（返回 running/stopped 状态与进程摘要列表）
// Python/Node/Go 无独立常驻服务，这里以「是否有该语言的解释器/工具链相关进程在跑」作为状态，
// 供「服务」页展示真实运行情况（而非伪造的 systemd 启停）。
func langProcessStatus(lang, bin string) (string, []map[string]string) {
	// 进程匹配关键字：避免匹配到面板自身（如 node 是前端构建、go 是面板编译等），
	// 用进程命令行特征过滤，只保留「长期运行」的服务类进程。
	pattern := ""
	switch lang {
	case "python":
		pattern = "python"
	case "node":
		pattern = "node "
	case "go":
		pattern = "go run |/go/bin/|go-build"
	default:
		return "unknown", nil
	}
	out := execOut(fmt.Sprintf("ps -eo pid,etime,cmd 2>/dev/null | grep -E '%s' | grep -v grep | grep -v 'kypanel' | head -n 10", pattern))
	lines := strings.Split(strings.TrimSpace(out), "\n")
	procs := make([]map[string]string, 0)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid := fields[0]
		etime := fields[1]
		cmd := strings.Join(fields[2:], " ")
		// 截断过长的命令
		if len(cmd) > 80 {
			cmd = cmd[:80] + "..."
		}
		procs = append(procs, map[string]string{"pid": pid, "etime": etime, "cmd": cmd})
	}
	if len(procs) > 0 {
		return "running", procs
	}
	return "stopped", procs
}

// pythonBin 定位 Python 版本可执行文件
func pythonBin(ver string) string {
	if ver != "" {
		// pyenv 安装路径
		for _, c := range []string{
			"/usr/local/python" + ver + "/bin/python" + strings.Split(ver, ".")[0],
			"/usr/local/python" + ver + "/bin/python" + ver,
			filepath.Join(homeDir(), ".pyenv/versions", ver, "/bin/python" + strings.Split(ver, ".")[0]),
		} {
			if _, err := os.Stat(c); err == nil {
				return c
			}
		}
		if out := execOut("python" + ver + " -V 2>&1"); strings.Contains(out, "Python") {
			return "python" + ver
		}
	}
	if out := execOut("python3 -V 2>&1"); strings.Contains(out, "Python") {
		return "python3"
	}
	return "python3"
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

// pythonPip 定位 Python 版本对应的 pip
func pythonPip(ver string) string {
	bin := pythonBin(ver)
	dir := filepath.Dir(bin)
	for _, c := range []string{dir + "/pip", dir + "/pip3"} {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return "pip3"
}

func nodeBin(ver string) string {
	if ver != "" {
		for _, c := range []string{
			"/usr/local/node" + ver + "/bin/node",
			filepath.Join(homeDir(), ".nvm/versions/node/v"+ver+"/bin/node"),
		} {
			if _, err := os.Stat(c); err == nil {
				return c
			}
		}
		if out := execOut("node -v 2>&1"); out != "" && strings.Contains(out, "v") {
			return "node"
		}
	}
	return "node"
}

func nodeNpm(ver string) string {
	dir := filepath.Dir(nodeBin(ver))
	return dir + "/npm"
}

func goBin(ver string) string {
	if ver != "" {
		for _, c := range []string{"/usr/local/go" + ver + "/bin/go"} {
			if _, err := os.Stat(c); err == nil {
				return c
			}
		}
	}
	if out := execOut("go version 2>&1"); strings.HasPrefix(out, "go version") {
		return "go"
	}
	return "go"
}

// RuntimePkgs 返回 Python/Node 已安装包列表
func RuntimePkgs(name string) (map[string]interface{}, error) {
	lang, ver := runtimeLangVer(name)
	switch lang {
	case "python":
		raw := execOut(pythonPip(ver) + " list --format=json 2>/dev/null")
		pkgs := make([]map[string]string, 0)
		if raw != "" {
			var arr []struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			}
			if json.Unmarshal([]byte(raw), &arr) == nil {
				for _, p := range arr {
					pkgs = append(pkgs, map[string]string{"name": p.Name, "version": p.Version})
				}
			}
		}
		return map[string]interface{}{"lang": "python", "packages": pkgs}, nil
	case "node":
		raw := execOut(nodeNpm(ver) + " ls -g --depth=0 --json 2>/dev/null")
		pkgs := make([]map[string]string, 0)
		if raw != "" {
			var data struct {
				Dependencies map[string]struct {
					Version string `json:"version"`
				} `json:"dependencies"`
			}
			if json.Unmarshal([]byte(raw), &data) == nil {
				for name, d := range data.Dependencies {
					pkgs = append(pkgs, map[string]string{"name": name, "version": d.Version})
				}
			}
		}
		sort.Slice(pkgs, func(i, j int) bool { return pkgs[i]["name"] < pkgs[j]["name"] })
		return map[string]interface{}{"lang": "node", "packages": pkgs}, nil
	case "go":
		return map[string]interface{}{
			"lang":     "go",
			"packages": []map[string]string{},
			"note":     "Go 为编译型语言，不维护全局包列表；模块依赖以项目 go.mod 为准",
		}, nil
	}
	return nil, fmt.Errorf("不支持的运行环境类型")
}

// RuntimeGlobalConfig 读取 Python/Node/Go 全局配置
func RuntimeGlobalConfig(name string) (map[string]interface{}, error) {
	lang, ver := runtimeLangVer(name)
	cfg := map[string]string{}
	switch lang {
	case "python":
		// 默认展示阿里云 pypi 镜像（国内速度快）；若服务器已配置 pip.conf 则以其实际值为准
		cfg["pip_index_url"] = "https://mirrors.aliyun.com/pypi/simple"
		for _, conf := range []string{
			filepath.Join(homeDir(), ".pip/pip.conf"),
			filepath.Join(homeDir(), ".config/pip/pip.conf"),
			"/etc/pip.conf",
		} {
			if b, err := os.ReadFile(conf); err == nil {
				if m := regexp.MustCompile(`(?m)^\s*index-url\s*=\s*(\S+)`).FindStringSubmatch(string(b)); len(m) >= 2 {
					cfg["pip_index_url"] = m[1]
				}
				break
			}
		}
		cfg["pythonpath"] = strings.TrimSpace(execOut("echo $PYTHONPATH 2>/dev/null"))
		return map[string]interface{}{"lang": "python", "config": cfg}, nil
	case "node":
		cfg["npm_registry"] = strings.TrimSpace(execOut("npm config get registry 2>/dev/null"))
		if cfg["npm_registry"] == "" {
			// 默认展示阿里云 npmmirror（npmmirror.com 为阿里巴巴出品的 npm 镜像）；若服务器已配置 .npmrc 则以其实际值为准
			cfg["npm_registry"] = "https://registry.npmmirror.com"
		}
		cfg["node_options"] = strings.TrimSpace(execOut("echo $NODE_OPTIONS 2>/dev/null"))
		return map[string]interface{}{"lang": "node", "config": cfg}, nil
	case "go":
		bin := goBin(ver)
		cfg["goproxy"] = strings.TrimSpace(execOut(bin + " env GOPROXY 2>/dev/null"))
		cfg["gosumdb"] = strings.TrimSpace(execOut(bin + " env GOSUMDB 2>/dev/null"))
		cfg["gopath"] = strings.TrimSpace(execOut(bin + " env GOPATH 2>/dev/null"))
		cfg["gomodcache"] = strings.TrimSpace(execOut(bin + " env GOMODCACHE 2>/dev/null"))
		cfg["gobin"] = strings.TrimSpace(execOut(bin + " env GOBIN 2>/dev/null"))
		return map[string]interface{}{"lang": "go", "config": cfg}, nil
	}
	return nil, fmt.Errorf("不支持的运行环境类型")
}

// RuntimeGlobalConfigSet 保存 Python/Node/Go 全局配置
func RuntimeGlobalConfigSet(name string, updates map[string]string) (map[string]interface{}, error) {
	lang, ver := runtimeLangVer(name)
	switch lang {
	case "python":
		path := filepath.Join(homeDir(), ".pip/pip.conf")
		if _, err := os.Stat(path); err != nil {
			_ = os.MkdirAll(filepath.Dir(path), 0o755)
			_ = os.WriteFile(path, []byte("[global]\n"), 0o644)
		}
		b, _ := os.ReadFile(path)
		content := string(b)
		if idxUrl, ok := updates["pip_index_url"]; ok && idxUrl != "" {
			content = upsertConfValue(content, "index-url", idxUrl)
		}
		if py, ok := updates["pythonpath"]; ok {
			content = upsertConfValue(content, "python-path", py)
			execOut(fmt.Sprintf("export PYTHONPATH='%s' 2>/dev/null || true", strings.ReplaceAll(py, "'", "")))
		}
		if err := backupFile(path); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return nil, fmt.Errorf("写入 pip.conf 失败：%v", err)
		}
		return map[string]interface{}{"ok": true, "path": path}, nil
	case "node":
		path := filepath.Join(homeDir(), ".npmrc")
		b, _ := os.ReadFile(path)
		content := string(b)
		if reg, ok := updates["npm_registry"]; ok && reg != "" {
			content = upsertConfValue(content, "registry", reg)
		}
		if no, ok := updates["node_options"]; ok {
			content = upsertConfValue(content, "node-options", no)
		}
		if err := backupFile(path); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return nil, fmt.Errorf("写入 .npmrc 失败：%v", err)
		}
		return map[string]interface{}{"ok": true, "path": path}, nil
	case "go":
		bin := goBin(ver)
		for k, v := range updates {
			goEnvKey := strings.ToUpper(k)
			if _, err := execOutE(fmt.Sprintf("%s env -w %s=%s 2>&1", bin, goEnvKey, shellQuote(v)), 10*time.Second); err != nil {
				return nil, fmt.Errorf("设置 %s 失败：%v", k, err)
			}
		}
		return map[string]interface{}{"ok": true}, nil
	}
	return nil, fmt.Errorf("不支持的运行环境类型")
}

// upsertConfValue 在 ini/conf 文本中更新 key = value
func upsertConfValue(content, key, val string) string {
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `\s*=.*$`)
	if re.MatchString(content) {
		return re.ReplaceAllString(content, key+" = "+val)
	}
	if content == "" {
		return key + " = " + val + "\n"
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return content + key + " = " + val + "\n"
}
