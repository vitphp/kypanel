package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"kypanel/internal/config"
	"kypanel/internal/model"
)

// ============================================================================
// 【4】运行时探测与环境状态
// ============================================================================

func findApp(key string) (AppMeta, bool) {
	for _, m := range allAppMetas() {
		if m.Key == key {
			return m, true
		}
	}
	return AppMeta{}, false
}

// resolveInstallMark 解析安装标记文件路径：支持 {DataDir} 占位符。
func resolveInstallMark(mark string) string {
	if strings.Contains(mark, "{DataDir}") {
		mark = strings.ReplaceAll(mark, "{DataDir}", config.Get().DataDir)
	}
	return mark
}

// detectPkgManager 检测系统的包管理器（返回 apt / dnf / yum）
func detectPkgManager() string {
	if _, err := exec.LookPath("apt-get"); err == nil {
		return "apt"
	}
	if _, err := exec.LookPath("dnf"); err == nil {
		return "dnf"
	}
	if _, err := exec.LookPath("yum"); err == nil {
		return "yum"
	}
	return ""
}

// probeVersion 探测应用版本
func probeVersion(versionCmd string) (string, error) {
	if versionCmd == "" {
		return "", errors.New("无版本命令")
	}
	res, err := ExecCommand(versionCmd, 2*time.Second)
	if err != nil {
		return "", err
	}
	// ExecCommand 对非零退出码不返回 error，需显式检查 ExitCode。
	// 修复 node/go/java 未安装时（命令 not found，exit 127）被误判为「有版本输出」的问题。
	if res.ExitCode != 0 {
		return "", errors.New("版本探测命令执行失败")
	}
	out := strings.TrimSpace(res.Stdout + res.Stderr)
	lines := strings.Split(out, "\n")
	ver := strings.TrimSpace(lines[0])
	if ver == "" {
		return "", errors.New("无版本输出")
	}
	return ver, nil
}

// isMetaInstalled 与 EnvStatus / ListApps 共用的「二进制 + 服务」探测：
// 用于 UninstallApp 在 record 缺失时判定「系统自带」——系统自带的 PHP 等发行版预装环境，
// EnvStatus 已识别为 installed，但 DB 里没 record 时，视为系统自带（不可卸载）。
// 也用于 StartApp/StopApp 在 record 缺失时允许对系统自带服务进行操作。
func isMetaInstalled(meta AppMeta) bool {
	// 标记文件应用（如 site-migrate 网站搬家）：无真实可卸载二进制，
	// 探测命令（tar --version 等）恒真，卸载后状态必然回弹。
	// 以面板维护的标记文件为准：安装成功 touch、卸载成功删除。
	if meta.InstallMarkFile != "" {
		p := resolveInstallMark(meta.InstallMarkFile)
		_, err := os.Stat(p)
		return err == nil
	}
	// node/python/go 多版本：必须用固定路径探测（与 EnvStatus 一致）。
	// 它们的 VersionCmd 以 `[ -s ... ] && ...` 开头，第一个词 `[` 在 PATH 中恒存在，
	// 直接 LookPath 会误判所有多版本「已安装」，导致从未装过的版本在商店显示 installed、
	// 点卸载却因 nvm/pyenv 不存在而卡死。
	if isMulti, installed := multiVersionInstalled(meta); isMulti {
		return installed
	}
	// Docker 特殊处理：必须 systemd unit 存在 + 服务可查询（不是 not-found）才算已安装。
	// 单纯用 LookPathBin("docker") 会把只装了 docker 客户端或残留 unit 的机器误判为「已安装」，
	// 而容器页 /docker/status 实际连不上 daemon，结果两边状态不一致。
	if meta.Key == "docker" {
		if !systemdUnitExists("docker") && !systemdUnitExists("docker.socket") {
			return false
		}
		if res, err := ExecCommand("systemctl is-active docker 2>/dev/null", 3*time.Second); err == nil {
			state := strings.TrimSpace(res.Stdout)
			if state != "" && state != "not-found" && state != "inactive" && state != "failed" {
				return true
			}
			// unit 存在但 inactive/failed：如果 docker 二进制 + dockerd 二进制都在，仍然认为「已安装但未运行」
			if _, err := LookPathBin("dockerd"); err == nil {
				return true
			}
			return false
		}
		return false
	}
	if meta.Service != "" {
		if _, err := LookPathBin(meta.Service); err == nil {
			return true
		}
		if systemdUnitExists(meta.Service) {
			return true
		}
	}
	if meta.VersionCmd != "" {
		// 「能用算已安装」：真实探测版本命令能否跑通，而非仅查二进制是否在 PATH。
		// 修复 phpmyadmin（grep 恒真）等 VersionCmd 以辅助命令开头导致误判「已安装」的问题。
		if v, err := probeVersion(meta.VersionCmd); err == nil && v != "" {
			return true
		}
	}
	return false
}

// nvmNodeDirs 返回 nvm 版本目录的候选根。
// 面板进程在 systemd 下可能没有 HOME 环境变量，此时 $HOME/.nvm 实际落在 /.nvm；
// 因此同时探测 $HOME/.nvm、/root/.nvm、/.nvm 三处。
func nvmNodeDirs() []string {
	dirs := make([]string, 0, 3)
	if h := os.Getenv("HOME"); h != "" {
		dirs = append(dirs, filepath.Join(h, ".nvm"))
	}
	dirs = append(dirs, "/root/.nvm", "/.nvm")
	return dirs
}

// pyenvDirs 同 nvmNodeDirs，用于 pyenv 版本目录。
func pyenvDirs() []string {
	dirs := make([]string, 0, 3)
	if h := os.Getenv("HOME"); h != "" {
		dirs = append(dirs, filepath.Join(h, ".pyenv"))
	}
	dirs = append(dirs, "/root/.pyenv", "/.pyenv")
	return dirs
}

// multiVersionInstalled 对 node/python/go 多版本按固定路径探测，
// 返回 (是否为多版本条目, 是否已安装)。
func multiVersionInstalled(meta AppMeta) (isMulti bool, installed bool) {
	for _, nv := range nodeVersions {
		if meta.Key != nv.Key {
			continue
		}
		// nvm 安装的版本：versions/node/v<ver>.*/bin/node（探测全部候选位置）
		for _, base := range nvmNodeDirs() {
			p := filepath.Join(base, "versions", "node", "v"+nv.Ver+".*", "bin", "node")
			if ms, _ := filepath.Glob(p); len(ms) > 0 {
				return true, true
			}
		}
		if _, err := os.Stat("/usr/local/node" + nv.Ver + "/bin/node"); err == nil {
			return true, true
		}
		return true, false
	}
	for _, pv := range pythonVersions {
		if meta.Key != pv.Key {
			continue
		}
		// pyenv 安装的版本：versions/<full>/bin/python<major>（pyenv 目录按完整补丁版本号，
		// 如 Python 3.13 实际目录为 versions/3.13.15，探测全部候选位置）
		for _, base := range pyenvDirs() {
			p := filepath.Join(base, "versions", pv.Full, "bin", "python"+pv.Major)
			if _, err := os.Stat(p); err == nil {
				return true, true
			}
		}
		if _, err := os.Stat("/usr/local/python" + pv.Ver + "/bin/python" + pv.Major); err == nil {
			return true, true
		}
		// 注意：不识别系统自带 / apt 安装的 python（如 /usr/bin/python3.13）。
		// 多版本 Python 是面板通过 pyenv 自管的应用，若把系统自带的算作「已安装」，
		// 用户卸载后（pyenv uninstall 对系统版本无效）状态会立刻被探测回弹为「已安装」。
		return true, false
	}
	for _, gv := range goVersions {
		if meta.Key != gv.Key {
			continue
		}
		if _, err := os.Stat("/usr/local/go" + gv.Ver + "/bin/go"); err == nil {
			return true, true
		}
		return true, false
	}
	return false, false
}

// resolveServiceName 解析实际 systemd 服务名（php-fpm 等带版本号的服务）
func resolveServiceName(meta AppMeta) string {
	if meta.Service == "" {
		return ""
	}
	if meta.Service != "php-fpm" {
		return meta.Service
	}
	// 多版本 PHP：优先精确匹配对应版本的服务（sury: php8.2-fpm / remi: php82-php-fpm）
	if strings.HasPrefix(meta.Key, "php") && len(meta.Key) > 3 {
		if digits := strings.TrimPrefix(meta.Key, "php"); len(digits) == 2 && isDigitStr(digits) {
			candidates := []string{
				"php" + digits[0:1] + "." + digits[1:2] + "-fpm",
				"php" + digits + "-php-fpm",
			}
			for _, c := range candidates {
				if systemdUnitExists(c) {
					return c
				}
			}
		}
	}
	// 兜底：探测任意已安装的 php*-fpm 服务
	res, err := ExecCommand("systemctl list-unit-files --type=service --no-legend | grep -oE 'php[0-9.]*-fpm' | head -n1", 15*time.Second)
	if err != nil {
		return "php-fpm"
	}
	name := strings.TrimSpace(res.Stdout)
	if name == "" {
		return "php-fpm"
	}
	return name
}

// systemdUnitExists 判断 systemd 服务单元是否存在
// 注意：不能依赖 `systemctl list-unit-files`——在部分虚拟化/容器环境（如腾讯云轻量服务器）中
// systemctl 的 dbus 请求会永久挂起；即使 ExecCommand 超时，也只能杀掉 /bin/sh 壳，
// systemctl 子进程会变成孤儿堆积，最终拖垮应用商店接口。
// 这里改为「unit 文件存在性 + 短超时 is-enabled」，任一环节都不会挂起。
func systemdUnitExists(name string) bool {
	if name == "" {
		return false
	}
	// 1) 纯文件系统检查：unit 文件存在即认为服务存在（常规 systemd 环境都适用）
	for _, dir := range []string{"/etc/systemd/system", "/usr/lib/systemd/system", "/run/systemd/system"} {
		if _, err := os.Stat(filepath.Join(dir, name+".service")); err == nil {
			return true
		}
	}
	// 2) systemd 运行时目录不存在（容器/精简环境），systemctl 只会挂起，直接判定不存在
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return false
	}
	// 3) 兜底：短超时 is-enabled（不带管道；超时后进程组被整体清理，不留孤儿）
	res, err := ExecCommand("systemctl is-enabled --quiet "+name+".service", 3*time.Second)
	return err == nil && res.ExitCode == 0
}

// isDigitStr 判断字符串是否全部为数字
func isDigitStr(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

// ListPhpVersions 返回已安装的 PHP 版本列表（降序，如 ["8.4","8.2",...]）
func ListPhpVersions() []string {
	opts := ListPhpFpms()
	if len(opts) == 0 {
		return []string{}
	}
	sort.SliceStable(opts, func(i, j int) bool {
		return phpVerNum(opts[i].Label) > phpVerNum(opts[j].Label)
	})
	vers := make([]string, 0, len(opts))
	for _, o := range opts {
		if v := phpMinorVersion(o.Label); v != "" {
			vers = append(vers, v)
		}
	}
	return vers
}

// selectPhpFpm 选择 PHP 版本；ver 为空时默认取最高版本，返回版本号和 FPM socket
func selectPhpFpm(ver string) (string, string, error) {
	opts := ListPhpFpms()
	if len(opts) == 0 {
		return "", "", errors.New("未检测到已安装的 PHP-FPM，请先在应用商店安装任意 PHP 版本")
	}
	if ver != "" {
		socket := resolvePhpFpm("PHP " + ver)
		if socket == "" {
			return "", "", errors.New("未找到 PHP " + ver + " 的 FPM 运行入口")
		}
		return ver, socket, nil
	}
	sort.SliceStable(opts, func(i, j int) bool {
		return phpVerNum(opts[i].Label) > phpVerNum(opts[j].Label)
	})
	label := opts[0].Label
	return phpMinorVersion(label), opts[0].Value, nil
}

// phpVerNum 将 "PHP 8.2" 转为可比较的整数（主*100+次）
func phpVerNum(s string) int {
	v := phpMinorVersion(s)
	if v == "" {
		return 0
	}
	parts := strings.SplitN(v, ".", 2)
	major, _ := strconv.Atoi(parts[0])
	minor := 0
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	return major*100 + minor
}

// PmaInstalled phpMyAdmin 是否已安装
func PmaInstalled() bool {
	if _, err := os.Stat(filepath.Join(pmaDir, "config.inc.php")); err != nil {
		return false
	}
	rec, err := model.GetAppRecord("phpmyadmin")
	return err == nil && rec.Status == model.AppInstalled
}

// PmaStatusResp phpMyAdmin 状态
type PmaStatusResp struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Path      string `json:"path"` // 通过面板端口访问的相对路径
}

// PmaStatus 返回 phpMyAdmin 安装状态与访问入口
func PmaStatus() PmaStatusResp {
	resp := PmaStatusResp{Installed: PmaInstalled(), Path: "/phpmyadmin/"}
	if resp.Installed {
		if meta, ok := findApp("phpmyadmin"); ok {
			if v, err := probeVersion(meta.VersionCmd); err == nil {
				resp.Version = v
			}
		}
	}
	return resp
}

// EnvStatusItem 单个环境安装状态

type EnvStatusItem struct {
	Key            string   `json:"key"`
	Name           string   `json:"name"`
	Installed      bool     `json:"installed"`
	Version        string   `json:"version"`
	SelectVersion  bool     `json:"select_version"`
	Versions       []string `json:"versions"`
	VersionDefault string   `json:"version_default"`
	Remarks        string   `json:"remarks"`
	Source         string   `json:"source"` // panel(面板安装) / system(系统自带) / ""(未安装)
}

// dbTypeToAppKey 把数据库类型映射到应用商店 key
func dbTypeToAppKey(t DatabaseType) string {
	switch t {
	case DBTypeMySQL:
		return "mysql"
	case DBTypeSQLServer:
		return "sqlserver"
	case DBTypeMongoDB:
		return "mongodb"
	case DBTypeRedis:
		return "redis"
	case DBTypePgSQL:
		return "postgresql"
	case DBTypeSQLite:
		return "sqlite"
	}
	return string(t)
}

// envStatus 文件缓存：EnvStatus 探测几十个环境，spawn 大量子进程，非常耗时。
// 缓存落盘到 {DataDir}/env_status.json，面板重启后依然有效（无需每次启动都慢一次）。
// 以下情况会重新探测：没有缓存文件、缓存过期（TTL）、安装/卸载软件后主动失效、手动刷新。
var envStatusMu sync.Mutex

// envStatusCacheTTL 环境状态缓存有效期。探测较慢，TTL 内直接读缓存避免页面频繁触发全量探测；
// 超过 TTL 自动重探，保证系统里手动安装/卸载（绕过面板）的环境状态不至于长期失真。
const envStatusCacheTTL = 5 * time.Minute

// envStatusFile 缓存文件路径
func envStatusFile() string {
	return filepath.Join(config.Get().DataDir, "env_status.json")
}

// EnvStatus 返回各环境（数据库 / FTP / Docker / Nginx / 运行时 / 多版本 PHP/Node）安装状态与版本信息。
// TTL 内直接读缓存；无缓存 / 缓存过期 / 缓存损坏时重新探测并落盘。
func EnvStatus() map[string]EnvStatusItem {
	envStatusMu.Lock()
	defer envStatusMu.Unlock()

	// 1) 尝试读缓存文件（TTL 内有效，避免高频探测）
	if info, err := os.Stat(envStatusFile()); err == nil && time.Since(info.ModTime()) < envStatusCacheTTL {
		if data, err := os.ReadFile(envStatusFile()); err == nil {
			var cache map[string]EnvStatusItem
			if json.Unmarshal(data, &cache) == nil && cache != nil {
				return cache
			}
		}
	}

	// 2) 无缓存 / 缓存过期 / 缓存损坏：重新探测并落盘
	res := envStatusUncached()
	envStatusSave(res)
	return res
}

// RefreshEnvStatus 强制重新探测环境状态（前端「重新检测环境」按钮）。
// 与 EnvStatus 不同：无论缓存是否新鲜都重新探测，保证用户手动操作后立即看到最新状态。
func RefreshEnvStatus() map[string]EnvStatusItem {
	envStatusMu.Lock()
	defer envStatusMu.Unlock()
	res := envStatusUncached()
	envStatusSave(res)
	return res
}

// envStatusSave 将探测结果写入缓存文件（失败静默，不影响主流程）
func envStatusSave(res map[string]EnvStatusItem) {
	data, err := json.Marshal(res)
	if err != nil {
		return
	}
	_ = os.WriteFile(envStatusFile(), data, 0o644)
}

// InvalidateEnvStatusCache 失效环境状态缓存（安装/卸载软件成功后调用）。
// 下次 EnvStatus 会重新探测并更新缓存文件。
func InvalidateEnvStatusCache() {
	envStatusMu.Lock()
	defer envStatusMu.Unlock()
	_ = os.Remove(envStatusFile())
}

// envStatusUncached 实际执行环境探测（无缓存）
func envStatusUncached() map[string]EnvStatusItem {
	res := make(map[string]EnvStatusItem)

	// 数据库环境
	for _, t := range DBTypeList() {
		available, msg := DatabaseAvailable(string(t))
		appKey := dbTypeToAppKey(t)
		meta, _ := findApp(appKey)
		item := EnvStatusItem{
			Key:            appKey,
			Name:           engineLabel(t),
			Installed:      available,
			SelectVersion:  meta.SelectVersion,
			Versions:       meta.Versions,
			VersionDefault: meta.VersionDefault,
			Remarks:        meta.Remarks,
		}
		if available {
			if v, err := probeVersion(meta.VersionCmd); err == nil && v != "" {
				item.Version = v
				// MySQL 实装 MariaDB 时标注真实引擎：Debian 上「MySQL」应用实际安装的
				// 是 mariadb-server（AptFallbackPackages），版本输出形如
				// "mysqld  Ver 15.1 Distrib 10.11.6-MariaDB"。面板各处仍按 mysql 管理，
				// 但展示层标明真实引擎，避免用户误以为装的是 MySQL 官方版。
				if t == DBTypeMySQL && strings.Contains(strings.ToLower(v), "mariadb") {
					item.Name = "MySQL (MariaDB)"
				}
			}
		} else {
			item.Remarks = msg
		}
		res[string(t)] = item
	}

	// FTP 环境
	if meta, ok := findApp("ftp"); ok {
		available := false
		// 注意：vsftpd 二进制装在 /usr/sbin/vsftpd，而 systemd 服务的 PATH 通常不含
		// /usr/sbin，用 LookPathBin（依赖 PATH）会找不到而误判为「未安装」。
		// 改用绝对路径直接检测，Debian/Ubuntu/RHEL 系 vsftpd 均在此路径。
		if _, err := os.Stat("/usr/sbin/vsftpd"); err == nil {
			available = true
		}
		item := EnvStatusItem{
			Key:            "ftp",
			Name:           "FTP 服务",
			Installed:      available,
			SelectVersion:  meta.SelectVersion,
			Versions:       meta.Versions,
			VersionDefault: meta.VersionDefault,
			Remarks:        meta.Remarks,
		}
		if available {
			if v, err := probeVersion(meta.VersionCmd); err == nil && v != "" {
				item.Version = v
			}
		} else {
			item.Remarks = "未检测到 vsftpd，请先安装 FTP 服务"
		}
		res["ftp"] = item
	}

	// Docker 环境
	if meta, ok := findApp("docker"); ok {
		available := false
		if _, err := LookPathBin("docker"); err == nil {
			available = true
		}
		item := EnvStatusItem{
			Key:            meta.Key,
			Name:           meta.Name,
			Installed:      available,
			SelectVersion:  meta.SelectVersion,
			Versions:       meta.Versions,
			VersionDefault: meta.VersionDefault,
			Remarks:        meta.Remarks,
		}
		if available {
			if v, err := probeVersion(meta.VersionCmd); err == nil && v != "" {
				item.Version = v
			}
		} else {
			item.Remarks = "未检测到 Docker，请先安装容器环境"
		}
		res["docker"] = item
	}

	// Nginx 环境（网站管理依赖）
	if meta, ok := findApp("nginx"); ok {
		available := false
		if _, err := LookPathBin("nginx"); err == nil {
			available = true
		}
		item := EnvStatusItem{
			Key:            "nginx",
			Name:           "Nginx",
			Installed:      available,
			SelectVersion:  meta.SelectVersion,
			Versions:       meta.Versions,
			VersionDefault: meta.VersionDefault,
			Remarks:        meta.Remarks,
		}
		if available {
			if v, err := probeVersion(meta.VersionCmd); err == nil && v != "" {
				item.Version = v
			}
		} else {
			item.Remarks = "未检测到 Nginx，请先安装 Web 服务器"
		}
		res["nginx"] = item
	}

	// Apache 环境（网站管理依赖，与 Nginx 互斥）
	if meta, ok := findApp("apache"); ok {
		available := false
		if _, err := LookPathBin("apache2"); err == nil {
			available = true
		}
		if _, err := LookPathBin("httpd"); err == nil {
			available = true
		}
		item := EnvStatusItem{
			Key:            "apache",
			Name:           "Apache",
			Installed:      available,
			SelectVersion:  meta.SelectVersion,
			Versions:       meta.Versions,
			VersionDefault: meta.VersionDefault,
			Remarks:        meta.Remarks,
		}
		if available {
			if v, err := probeVersion(meta.VersionCmd); err == nil && v != "" {
				item.Version = v
			}
		} else {
			item.Remarks = "未检测到 Apache，请先安装 Web 服务器"
		}
		res["apache"] = item
	}

	// 运行时环境（PHP / Node.js / Python / Go / Java）
	for _, key := range []string{"nodejs", "python3", "golang", "java"} {
		if meta, ok := findApp(key); ok {
			available := false
			if meta.Service != "" {
				if _, err := LookPathBin(meta.Service); err == nil {
					available = true
				}
			} else {
				// 没有 systemd 服务名时，真实探测版本命令能否跑通（「能用算已安装」）
				if meta.VersionCmd != "" {
					if v, err := probeVersion(meta.VersionCmd); err == nil && v != "" {
						available = true
					}
				}
			}
			// PHP 特殊处理：默认版应用定义已删除（2026-08-24），多版本由各自的 php74/php80 等 meta 独立探测
			item := EnvStatusItem{
				Key:            meta.Key,
				Name:           meta.Name,
				Installed:      available,
				SelectVersion:  meta.SelectVersion,
				Versions:       meta.Versions,
				VersionDefault: meta.VersionDefault,
				Remarks:        meta.Remarks,
			}
			if available {
				if v, err := probeVersion(meta.VersionCmd); err == nil && v != "" {
					item.Version = v
				}
			} else {
				item.Remarks = "未检测到 " + meta.Name + "，请先安装运行环境"
			}
			res[key] = item
		}
	}

	// 多版本 PHP（php7.4 ~ php8.4）
	for _, pv := range phpVersions {
		key := pv.Key
		if meta, ok := findApp(key); ok {
			available := false
			// 探测 phpX.Y 或 phpXX 二进制
			bin := "php" + pv.Ver
			if _, err := LookPathBin(bin); err == nil {
				available = true
			} else {
				// Remi 路径
				remiBin := "/opt/remi/php" + pv.Remi + "/root/usr/bin/php"
				if _, err := os.Stat(remiBin); err == nil {
					available = true
				}
			}
			item := EnvStatusItem{
				Key:            meta.Key,
				Name:           meta.Name,
				Installed:      available,
				SelectVersion:  meta.SelectVersion,
				Versions:       meta.Versions,
				VersionDefault: meta.VersionDefault,
				Remarks:        meta.Remarks,
			}
			if available {
				if v, err := probeVersion(meta.VersionCmd); err == nil && v != "" {
					item.Version = v
				}
			} else {
				item.Remarks = "未检测到 " + meta.Name + "，请先安装"
			}
			res[key] = item
		}
	}

	// 多版本 Node.js（node18 ~ node22）
	for _, nv := range nodeVersions {
		key := nv.Key
		if meta, ok := findApp(key); ok {
			available := false
			// 探测 nvm 安装的版本（面板进程可能无 HOME，探测全部候选位置）
			for _, base := range nvmNodeDirs() {
				nodeBin := filepath.Join(base, "versions", "node", "v"+nv.Ver+"."+"*", "bin", "node")
				matches, _ := filepath.Glob(nodeBin)
				if len(matches) > 0 {
					available = true
					break
				}
			}
			// 探测 /usr/local/node<ver> 路径
			if !available {
				if _, err := os.Stat("/usr/local/node" + nv.Ver + "/bin/node"); err == nil {
					available = true
				}
			}
			item := EnvStatusItem{
				Key:            meta.Key,
				Name:           meta.Name,
				Installed:      available,
				SelectVersion:  meta.SelectVersion,
				Versions:       meta.Versions,
				VersionDefault: meta.VersionDefault,
				Remarks:        meta.Remarks,
			}
			if available {
				if v, err := probeVersion(meta.VersionCmd); err == nil && v != "" {
					item.Version = v
				}
			} else {
				item.Remarks = "未检测到 " + meta.Name + "，请先安装"
			}
			res[key] = item
		}
	}

	// 多版本 Python（python3.10 ~ python3.12）
	for _, pv := range pythonVersions {
		key := pv.Key
		if meta, ok := findApp(key); ok {
			available := false
			// 探测 pyenv 安装的版本（pyenv 目录按完整补丁版本号，如 3.13.15；探测全部候选位置）
			for _, base := range pyenvDirs() {
				pyenvBin := filepath.Join(base, "versions", pv.Full, "bin", "python"+pv.Major)
				if _, err := os.Stat(pyenvBin); err == nil {
					available = true
					break
				}
			}
			// 探测 /usr/local/python<ver> 路径
			if !available {
				if _, err := os.Stat("/usr/local/python" + pv.Ver + "/bin/python" + pv.Major); err == nil {
					available = true
				}
			}
			// 不识别系统自带 / apt 安装的 python（见 multiVersionInstalled 注释）：
			// 多版本 Python 是面板通过 pyenv 自管的应用，避免卸载后状态被系统二进制回弹
			item := EnvStatusItem{
				Key:            meta.Key,
				Name:           meta.Name,
				Installed:      available,
				SelectVersion:  meta.SelectVersion,
				Versions:       meta.Versions,
				VersionDefault: meta.VersionDefault,
				Remarks:        meta.Remarks,
			}
			if available {
				if v, err := probeVersion(meta.VersionCmd); err == nil && v != "" {
					item.Version = v
				}
			} else {
				item.Remarks = "未检测到 " + meta.Name + "，请先安装"
			}
			res[key] = item
		}
	}

	// 多版本 Go（go1.21 ~ go1.23）
	for _, gv := range goVersions {
		key := gv.Key
		if meta, ok := findApp(key); ok {
			available := false
			// 探测 /usr/local/go<ver>/bin/go
			if _, err := os.Stat("/usr/local/go" + gv.Ver + "/bin/go"); err == nil {
				available = true
			}
			item := EnvStatusItem{
				Key:            meta.Key,
				Name:           meta.Name,
				Installed:      available,
				SelectVersion:  meta.SelectVersion,
				Versions:       meta.Versions,
				VersionDefault: meta.VersionDefault,
				Remarks:        meta.Remarks,
			}
			if available {
				if v, err := probeVersion(meta.VersionCmd); err == nil && v != "" {
					item.Version = v
				}
			} else {
				item.Remarks = "未检测到 " + meta.Name + "，请先安装"
			}
			res[key] = item
		}
	}

	// 统一补充安装来源：DB 有 installed 记录 → panel（面板安装）；否则 → system（系统自带）
	for k, item := range res {
		if !item.Installed {
			continue
		}
		if rec, err := model.GetAppRecord(item.Key); err == nil && rec.Status == model.AppInstalled {
			item.Source = "panel"
		} else {
			item.Source = "system"
		}
		res[k] = item
	}

	return res
}

var pmaConfigOnce sync.Once

const pmaConfigTemplate = `<?php
$cfg['blowfish_secret'] = '%s';
$i = 0;
$i++;
$cfg['Servers'][$i]['auth_type'] = 'config';
$cfg['Servers'][$i]['host'] = 'localhost';
$cfg['Servers'][$i]['user'] = '%s';
$cfg['Servers'][$i]['password'] = '%s';
$cfg['Servers'][$i]['compress'] = false;
$cfg['Servers'][$i]['AllowNoPassword'] = false;
$cfg['Servers'][$i]['AllowRoot'] = true;
$cfg['Servers'][$i]['hide_db'] = '^(information_schema|mysql|performance_schema|sys)$';
$cfg['DefaultLang'] = 'zh_CN';
$cfg['PmaAbsoluteUri'] = '%s';
$cfg['TitleDefault'] = '数据库管理 - phpMyAdmin';
$cfg['TitleServer'] = '数据库管理 - phpMyAdmin';
$cfg['TitleDatabase'] = '@DATABASE@ - phpMyAdmin';
$cfg['TitleTable'] = '@TABLE@ - phpMyAdmin';
`

// EnsurePmaConfig 修复已安装 phpMyAdmin 的配置：
// 1. 若当前不是免密登录模式（auth_type=config），自动重建为免密登录（面板「管理」直接进入）；
// 2. 用完整模板重写配置，清理之前 ensure 语法错误（如 $cfg[Servers']...）导致的「读取配置文件失败」。
func EnsurePmaConfig() {
	pmaConfigOnce.Do(func() {
		cfgPath := filepath.Join(pmaDir, "config.inc.php")
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			return
		}
		content := string(data)

		// 非免密模式（cookie/登录页）→ 自动切换为免密登录
		if !regexp.MustCompile(`(?m)auth_type\s*=\s*'config'`).MatchString(content) {
			if cerr := configurePhpMyAdmin(); cerr == nil {
				if d2, err2 := os.ReadFile(cfgPath); err2 == nil {
					content = string(d2)
				}
			}
		}

		// 读取现有账号/密钥，避免重新创建账号导致已有账号被改
		blowfish := extractPmaConfigValue(content, `$cfg['blowfish_secret']`)
		user := extractPmaConfigValue(content, `$cfg['Servers'][$i]['user']`)
		password := extractPmaConfigValue(content, `$cfg['Servers'][$i]['password']`)

		if user == "" || password == "" {
			if err := configurePhpMyAdmin(); err != nil {
				return
			}
			if d2, err2 := os.ReadFile(cfgPath); err2 == nil {
				content = string(d2)
				user = extractPmaConfigValue(content, `$cfg['Servers'][$i]['user']`)
				password = extractPmaConfigValue(content, `$cfg['Servers'][$i]['password']`)
			}
		}
		if user == "" || password == "" {
			return
		}
		if blowfish == "" {
			blowfish = randomHex(24)
		}

		pmaAbsURI := "/phpmyadmin/"
		newContent := fmt.Sprintf(pmaConfigTemplate, blowfish, user, password, pmaAbsURI)
		_ = os.WriteFile(cfgPath, []byte(newContent), 0644)
	})
}

// extractPmaConfigValue 从 phpMyAdmin 配置文本中提取指定 key 的字符串值
func extractPmaConfigValue(content, key string) string {
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `\s*=\s*['"]([^'"]+)['"]\s*;`)
	if m := re.FindStringSubmatch(content); m != nil {
		return m[1]
	}
	return ""
}

// configurePhpMyAdmin 配置免密登录：创建专用 MySQL 管理账号并写入 phpMyAdmin 配置
func configurePhpMyAdmin() error {
	cfgPath := filepath.Join(pmaDir, "config.inc.php")
	user := "lp_pma_" + randomHex(6)
	pass := randomHex(16)
	sql := fmt.Sprintf(
		"DROP USER IF EXISTS '%s'@'localhost'; CREATE USER '%s'@'localhost' IDENTIFIED BY '%s'; GRANT ALL PRIVILEGES ON *.* TO '%s'@'localhost' WITH GRANT OPTION; FLUSH PRIVILEGES;",
		user, user, pass, user)
	cmd := "mysql " + mysqlBaseArgs() + " -e " + shellQuote(sql)
	res, err := ExecCommand(cmd, 60*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("创建数据库管理账号失败: %s", strings.TrimSpace(res.Stderr))
	}
	cfg := fmt.Sprintf(`<?php
$cfg['blowfish_secret'] = '%s';
$i = 0;
$i++;
$cfg['Servers'][$i]['auth_type'] = 'config';
$cfg['Servers'][$i]['host'] = 'localhost';
$cfg['Servers'][$i]['user'] = '%s';
$cfg['Servers'][$i]['password'] = '%s';
$cfg['Servers'][$i]['compress'] = false;
$cfg['Servers'][$i]['AllowNoPassword'] = false;
$cfg['Servers'][$i]['AllowRoot'] = true;
$cfg['DefaultLang'] = 'zh_CN';
$cfg['TitleDefault'] = '数据库管理 - phpMyAdmin';
$cfg['TitleServer'] = '数据库管理 - phpMyAdmin';
$cfg['TitleDatabase'] = '@DATABASE@ - phpMyAdmin';
$cfg['TitleTable'] = '@TABLE@ - phpMyAdmin';
`, randomHex(24), user, pass)
	return os.WriteFile(cfgPath, []byte(cfg), 0o644)
}

// randomHex 生成 n 字节随机十六进制字符串
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// runCmdWithLog 执行长命令并把输出实时追加到日志文件
// runCmdWithLog 无取消上下文版本（供非任务流程使用）
func runCmdWithLog(cmdStr string, logPath string, timeout time.Duration) error {
	return runCmdWithLogCtx(context.Background(), cmdStr, logPath, timeout)
}

// stallTimeout 命令无输出超时：超过该时长无任何 stdout/stderr 输出视为卡死
// （典型场景：GitHub 不通时 git clone / curl 无限挂起），自动终止并报错，
// 让安装/卸载任务及时收尾，而不是无限等待直到 40 分钟总超时。
const stallTimeout = 15 * time.Minute

// stallWriter 写日志的同时记录最后写入时间，供无输出超时检测使用。
type stallWriter struct {
	w  io.Writer
	mu *sync.Mutex
	at *time.Time
}

func (s *stallWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	*s.at = time.Now()
	s.mu.Unlock()
	return s.w.Write(p)
}

// runCmdWithLogCtx 执行命令并将输出追加到日志文件；当 ctx 被取消时杀掉整个进程组。
// 任务取消（停止安装/卸载）依赖此函数：所有安装/卸载命令都必须带 ctx 调用。
// 自我处理：命令超过 stallTimeout（15 分钟）无任何输出时判定为卡死，
// 自动杀掉进程树并返回明确错误（可在面板日志中看到原因），避免安装任务无限挂起。
func runCmdWithLogCtx(ctx context.Context, cmdStr string, logPath string, timeout time.Duration) error {
	ctx2, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx2, "/bin/sh", "-c", cmdStr)
	setupCmdProcAttr(cmd)
	// 父 context 被取消（用户点了"停止"）时，杀掉整棵进程树（apt-get / curl 等）
	stopKill := context.AfterFunc(ctx, func() { killCmdTree(cmd) })
	defer stopKill()

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	// 无输出超时检测：任何 stdout/stderr 输出都会刷新最后活动时间
	var lastOutMu sync.Mutex
	lastOut := time.Now()
	w := &stallWriter{w: f, mu: &lastOutMu, at: &lastOut}
	cmd.Stdout = w
	cmd.Stderr = w

	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()

	stallTick := time.NewTicker(30 * time.Second)
	defer stallTick.Stop()
	for {
		select {
		case err := <-done:
			if err != nil {
				if ctx.Err() == context.Canceled {
					return errors.New("操作已取消")
				}
				if ctx2.Err() == context.DeadlineExceeded {
					return errors.New("命令执行超时（" + timeout.String() + "）")
				}
				return err
			}
			return nil
		case <-stallTick.C:
			lastOutMu.Lock()
			idle := time.Since(lastOut)
			lastOutMu.Unlock()
			if idle > stallTimeout {
				killCmdTree(cmd)
				<-done
				return errors.New("命令长时间无输出（超过 " + stallTimeout.String() + "），疑似网络或下载源不可达，已自动终止；请检查网络与镜像源后重试")
			}
		}
	}
}

// appendToFile 追加文本到文件
func appendToFile(path, content string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(content)
}

// ReadAppLog 读取应用日志（可指定行数）
func ReadAppLog(key string, lines int) (string, error) {
	// key 会拼进日志文件路径，白名单校验防止路径穿越读取任意 .log 文件
	if key == "" || !appTokenRe.MatchString(key) {
		return "", errors.New("日志标识无效")
	}
	path := AppLogPath(key)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	all := strings.Split(string(data), "\n")
	if lines > 0 && len(all) > lines {
		all = all[len(all)-lines:]
	}
	return strings.Join(all, "\n"), nil
}

// appCategoryOrder 分类展示顺序（本地兜底时使用，远程拉取到的分类按官网排序）
var appCategoryOrder = []string{CatServer, CatDatabase, CatCache, CatRuntime, CatTool}

// AppCategories 返回分类列表（含展示信息）。
// 优先从官网 /api/app-categories 拉取（动态驱动面板 /apps 页面 Tab）；
// 拉取失败/为空/未配置 base_url 时，回退到本地硬编码分类（兜底不变）。
func AppCategories() []AppCategory {
	if remote, _ := fetchRemoteCategories(); len(remote) > 0 {
		return remote
	}
	cats := make([]AppCategory, 0, len(appCategoryOrder))
	for _, k := range appCategoryOrder {
		if c, ok := appCategoryMeta[k]; ok {
			cats = append(cats, c)
		}
	}
	return cats
}
