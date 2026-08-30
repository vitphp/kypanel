package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"kypanel/internal/cli"
	"kypanel/internal/config"
	"kypanel/internal/logger"
	"kypanel/internal/model"
	"kypanel/internal/router"
	"kypanel/internal/service"
	"kypanel/internal/utils"
	"kypanel/internal/version"
)

func main() {
	// 以 "ky" 身份运行（软链接 /usr/local/bin/ky -> 面板二进制）
	// 或显式传入 ky/menu 参数时，进入命令行管理菜单
	if filepath.Base(os.Args[0]) == "ky" ||
		(len(os.Args) > 1 && (os.Args[1] == "ky" || os.Args[1] == "menu")) {
		cli.Run()
		return
	}

	var (
		configPath  = flag.String("config", "", "配置文件路径")
		showVer     = flag.Bool("version", false, "显示版本号")
		exportApps  = flag.String("export-apps", "", "导出内置应用数据到指定 JSON 文件（用于官网应用商店 seed）")
		renewSSL    = flag.Bool("renew-ssl", false, "自动续签即将到期（默认 2 天内）的 ACME 证书后退出（供计划任务每日调用）")
		renewDays   = flag.Int("renew-days", 2, "配合 -renew-ssl 使用：剩余天数小于等于该值才续签，默认 2")
	)
	flag.Parse()

	if *showVer {
		fmt.Printf("kypanel %s (commit %s, built %s)\n", version.Version, version.Commit, version.Date)
		os.Exit(0)
	}

	// 导出内置应用数据（一次性迁移用），不启动服务
	if *exportApps != "" {
		data, err := service.ExportAppMetasJSON()
		if err != nil {
			fmt.Fprintln(os.Stderr, "导出失败:", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*exportApps, data, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "写入文件失败:", err)
			os.Exit(1)
		}
		fmt.Printf("已导出 %d 个应用到 %s\n", len(data), *exportApps)
		os.Exit(0)
	}

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "加载配置失败:", err)
		os.Exit(1)
	}

	// 未显式指定配置文件时，默认使用数据目录下的 config.json，
	// 保证 JWT 密钥等配置在重启后不丢失（否则重启会让所有已登录 Token 失效）
	if *configPath == "" {
		*configPath = filepath.Join(cfg.DataDir, "config.json")
		if loaded, loadErr := config.Load(*configPath); loadErr != nil {
			slog.Warn("回退默认配置失败", "err", loadErr, "path", *configPath)
		} else {
			cfg = loaded
		}
	}

	// 环境变量覆盖数据目录（开发/测试用）
	if dir := os.Getenv("PANEL_DATA_DIR"); dir != "" {
		cfg.DataDir = dir
		cfg.DB.Path = dir + "/panel.db"
		cfg.Log.File = dir + "/logs/panel.log"
	}

	// 初始化日志
	if _, err := logger.Setup(logger.Options{
		Level: cfg.Log.Level,
		File:  cfg.Log.File,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "初始化日志失败:", err)
		os.Exit(1)
	}

	// 确保目录存在
	if err := cfg.EnsureDirs(); err != nil {
		slog.Error("创建目录失败", "err", err)
		os.Exit(1)
	}

	// 升级自愈：若上次升级后新版本启动失败，先自动恢复旧二进制与数据备份再继续启动。
	// 必须在数据库初始化之前执行，保证恢复后的旧数据被正确加载。
	service.AutoRollbackUpgrade()

	// 初始化数据库
	if err := model.Init(cfg.DB.Path); err != nil {
		slog.Error("初始化数据库失败", "err", err)
		os.Exit(1)
	}

	// 初始化全局默认页面（index/404/停用/不存在页）+ default_server 兜底配置
	if err := service.InitDefaultPages(); err != nil {
		slog.Warn("初始化默认页面失败", "err", err)
	}
	// CLI 模式：自动续签 HTTPS 证书后退出（计划任务每天调用，防止证书到期网站打不开）
	if *renewSSL {
		days := *renewDays
		if days <= 0 {
			days = 2
		}
		result, err := service.RenewLetsEncryptDays(days)
		if err != nil {
			slog.Error("自动续签证书失败", "err", err)
			fmt.Fprintln(os.Stderr, "自动续签失败:", err)
			os.Exit(1)
		}
		fmt.Println(result)
		slog.Info("自动续签证书完成", "result", result)
		os.Exit(0)
	}

	// 清理 LoginSession 表的历史重复数据（一次性：升级后 AutoMigrate 会给 token_hash 加 uniqueIndex）
	if n, err := service.CleanSessionDuplicates(); err != nil {
		slog.Warn("清理 LoginSession 重复记录失败", "err", err)
	} else if n > 0 {
		slog.Info("已清理 LoginSession 重复记录", "count", n)
	}

	// 清理已下线（active=false）的旧会话记录，避免列表堆积
	if n := service.CleanInactiveSessions(50); n > 0 {
		slog.Info("已清理已下线会话记录", "count", n)
	}

	// 初始化 JWT 密钥：无密钥则生成并持久化到配置文件，重启后继续有效
	if cfg.Auth.JWTSecret == "" {
		cfg.Auth.JWTSecret = generateSecret()
		if err := cfg.Save(*configPath); err != nil {
			slog.Warn("保存配置失败（JWT 密钥仅本次有效）", "err", err)
		} else {
			slog.Info("已生成新的 JWT 密钥并保存", "path", *configPath)
		}
	}
	utils.InitJWT(cfg.Auth.JWTSecret)
	utils.InitCrypto(cfg.Auth.JWTSecret)

	// 初始化安全入口：6 位字母数字组合随机串，未配置则自动生成并保存
	// 用于登录页 URL 前缀（https://ip:port/<entrance>/login），隐藏面板真实入口
	if cfg.SecurityEntrance == "" {
		cfg.SecurityEntrance = generateSecurityEntrance()
		if err := cfg.Save(*configPath); err != nil {
			slog.Warn("保存配置失败（安全入口仅本次有效）", "err", err)
		} else {
			slog.Info("已生成安全入口并保存", "entrance", cfg.SecurityEntrance)
		}
	}
	// 醒目提示，方便首次部署后立即看到入口
	slog.Warn("面板安全入口", "url", fmt.Sprintf("http://%s:%d/%s/", localIP(), cfg.Server.Port, cfg.SecurityEntrance))

	// 初始化面板名称/副标题：未配置则用默认值（开猿运维 / Linux管理面板）
	if cfg.PanelName == "" {
		cfg.PanelName = "开猿运维"
		cfg.PanelSub = "Linux管理面板"
		_ = cfg.Save(*configPath) // 首次启动时持久化默认值
	}

	// 初始化默认管理员（首次启动）
	// 账号密码可用环境变量 PANEL_ADMIN_USER / PANEL_ADMIN_PASS 覆盖（安装脚本随机生成）。
	// 安全：未通过环境变量显式指定密码时，不再回退到公开弱密码「admin888」，
	// 而是随机生成强密码并打印到控制台（仅首次创建账号时生效），杜绝裸运行二进制被撞库。
	adminUser := os.Getenv("PANEL_ADMIN_USER")
	adminPass := os.Getenv("PANEL_ADMIN_PASS")
	if adminUser == "" {
		adminUser = "admin"
	}
	passwordGenerated := false
	if adminPass == "" {
		adminPass = generatePassword()
		passwordGenerated = true
	}
	if _, err := model.EnsureDefaultAdmin(adminUser, adminPass); err != nil {
		slog.Error("初始化默认管理员失败", "err", err)
		os.Exit(1)
	}
	if passwordGenerated {
		slog.Warn("已生成默认管理员随机密码（请立即登录并修改）", "username", adminUser, "password", adminPass)
	}
	slog.Info("默认管理员已就绪", "username", adminUser)

	// 清除 panel.env 中的明文密码（安全加固）：
	// 密码只应存数据库 bcrypt。安装脚本首次启动会临时通过环境变量传入初始密码，
	// 账号创建完成后这里主动把 panel.env 里的 PANEL_ADMIN_PASS 行删除，
	// 避免明文密码长期残留在磁盘上。
	stripEnvPlainPassword(cfg.DataDir)

	// 安全加固：收紧敏感文件权限为 600
	// config.json 含 JWT 密钥，panel.db 含设置凭据（历史明文 / 密文），
	// 防止同机其他用户读取。Windows 上跳过（权限模型不同）。
	secureFilePerm(*configPath)
	secureFilePerm(cfg.DB.Path)

	// 初始化内置角色（运维 / 只读），幂等
	service.EnsureBuiltinRoles()

	// 加载历史监控数据并启动采集
	if err := service.LoadMonitorHistory(); err != nil {
		slog.Warn("加载历史监控数据失败", "err", err)
	}
	service.StartMonitor()
	service.StartAlert()

	// 清理 ghost 状态：上一次服务异常退出残留的 installing/uninstalling 记录，
	// 重启时 in-memory 的 appTasks 已丢失，这些记录对应的任务实际已不在跑——重置回 not_installed，
	// 避免「应用商店卡片一直显示安装中 / 浮窗却找不到任务」脱钩
	if n := service.CleanupGhostInstalls(); n > 0 {
		slog.Warn("已重置 ghost 状态的应用记录", "count", n)
	}

	// 启动后台 ghost watcher：service 运行期间产生的卡死记录（apt 进程意外退出、
	// 子协程 panic 没走 defer 清理等）也会被周期性清理，每 30 秒扫一次
	service.StartGhostWatcher(context.Background())

	// 加载安全规则（端口/IP/国家/运营商）
	service.InitSecurityRules()
	service.ApplySecurityRules()

	// 确保 cron 任务 wrapper 脚本存在（系统 cron 调起任务时用它执行用户命令并写日志）
	service.EnsureCronWrapper()
	// 同步 crontab（用新格式覆盖旧"直接命令"行，确保系统 cron 调起时走 wrapper 记录日志）
	service.SyncCrontab()

	// 预加载离线 IP 库（用于安全中心按城市/国家拉黑）
	service.InitIpRegion()

	// 初始化 WAF（内置规则、全局配置、CC 防护协程）
	service.InitWAF()

	// 自愈 Web 服务器：启动时修复无效站点证书（自签兜底），配置通过且 nginx 未运行时自动拉起。
	// 防止某个站点的坏证书导致 nginx 全局校验失败、所有网站起不来（升级/重启后尤为关键）
	service.SelfHealWebServerOnBoot()

	// 初始化单站安全后台协程（CC 自动封禁、攻击日志采集、geo 封禁）
	service.InitSiteSecurity()

	// 启动站点访问日志实时入库（统计用）
	service.StartSiteStatImports()

	// 启动站点级 IP 拉黑过期清理协程（每小时跑一次）
	service.StartSiteBlockIPJanitor()

	// 启动 HTTP 服务
	r := router.Setup(cfg)
	addr := fmt.Sprintf(":%d", cfg.Server.Port)

	srv := &http.Server{
		Addr:    addr,
		Handler: r,
		// 大文件上传关键：ReadTimeout 覆盖整个请求体读取阶段（含 multipart 文件数据），
		// 若设 30s 会在 GB 级文件传完前掐断连接，导致 c.FormFile 拿不到文件（报"缺少上传文件"）。
		// 因此 ReadTimeout 置 0（不限制），只保留 ReadHeaderTimeout 防慢速攻击。
		ReadTimeout:       0,
		ReadHeaderTimeout: 15 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
	}

	slog.Info("kypanel 启动", "version", version.Version, "addr", addr, "https", cfg.Server.HTTPS)

	// HTTPS 支持：证书加载失败时自动降级为 HTTP，保证服务永远可用
	useTLS := bool(cfg.Server.HTTPS) && cfg.Server.CertFile != "" && cfg.Server.KeyFile != ""
	if useTLS {
		if _, err := tls.LoadX509KeyPair(cfg.Server.CertFile, cfg.Server.KeyFile); err != nil {
			slog.Error("HTTPS 证书无效，自动降级为 HTTP", "err", err)
			useTLS = false
		}
	}
	var listenErr error
	if useTLS {
		listenErr = srv.ListenAndServeTLS(cfg.Server.CertFile, cfg.Server.KeyFile)
	} else {
		listenErr = srv.ListenAndServe()
	}
	if listenErr != nil && listenErr != http.ErrServerClosed {
		slog.Error("服务启动失败", "err", listenErr)
		os.Exit(1)
	}
}

// secureFilePerm 将敏感文件权限收紧为 600（Linux/macOS），防止同机其他用户读取。
// 仅当当前权限的 group/other 位存在读/写/执行时才修改，避免无谓的系统调用。
func secureFilePerm(path string) {
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if info.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(path, 0o600); err != nil {
			slog.Warn("收紧敏感文件权限失败", "path", path, "err", err)
		} else {
			slog.Info("已收紧敏感文件权限为 600", "path", path)
		}
	}
}

// generateSecret 生成随机 JWT 密钥
func generateSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "kypanel-dev-secret"
	}
	return hex.EncodeToString(b)
}

// generatePassword 生成随机强密码（16 字符，大小写字母 + 数字）
// 用于裸运行（无 PANEL_ADMIN_PASS 环境变量）时兜底，避免弱密码 admin888 暴露。
func generatePassword() string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 16)
	randBytes := make([]byte, 16)
	if _, err := rand.Read(randBytes); err != nil {
		// crypto/rand 失败几乎不可能，兜底一个仍强于 admin888 的固定串
		return "Lp" + time.Now().Format("0102150405") + "Xz9"
	}
	for i := range b {
		b[i] = chars[int(randBytes[i])%len(chars)]
	}
	return string(b)
}

// stripEnvPlainPassword 删除 panel.env（DataDir/panel.env）里的 PANEL_ADMIN_PASS 行，
// 避免明文密码残留磁盘。密码仅存数据库 bcrypt，忘记时通过 `ky` 命令重置。
func stripEnvPlainPassword(dataDir string) {
	envFile := filepath.Join(dataDir, "panel.env")
	data, err := os.ReadFile(envFile)
	if err != nil {
		return // 文件不存在或不可读，忽略
	}
	lines := strings.Split(string(data), "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "PANEL_ADMIN_PASS=") {
			continue // 删除明文密码行
		}
		out = append(out, l)
	}
	newData := strings.Join(out, "\n")
	if newData != string(data) {
		_ = os.WriteFile(envFile, []byte(newData), 0o600)
		slog.Info("已清除 panel.env 中的明文密码")
	}
}

// generateSecurityEntrance 生成 6 位字母数字组合的安全入口串（用于登录页 URL 前缀）
// 使用大小写字母 + 数字，排除易混淆的字符（0OIl1）以提高可读性
func generateSecurityEntrance() string {
	const chars = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 6)
	randBytes := make([]byte, 6)
	if _, err := rand.Read(randBytes); err != nil {
		// crypto/rand 失败几乎不可能，兜底用时间戳生成 6 位
		s := fmt.Sprintf("%d", time.Now().UnixNano())
		return s[len(s)-6:]
	}
	for i := range b {
		b[i] = chars[int(randBytes[i])%len(chars)]
	}
	return string(b)
}

// localIP 获取本机第一个非回环 IPv4 地址（用于在启动日志里提示访问入口 URL）
func localIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil || ip.IsLoopback() {
			continue
		}
		ip4 := ip.To4()
		if ip4 == nil {
			continue
		}
		return ip4.String()
	}
	return "127.0.0.1"
}
