// Package cli 提供 kypanel 命令行管理工具。
// 用法：在服务器上以 root 身份执行 `ky` 进入交互式菜单。
// ky 是指向面板二进制 /opt/kypanel/panel 的软链接，panel 检测到
// 自身以 "ky" 名字运行时进入本菜单模式（也可用 `kypanel ky` 触发）。
// 复用 kypanel/internal/model 与 internal/service，无需新依赖。
package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"kypanel/internal/config"
	"kypanel/internal/model"
	"kypanel/internal/service"
)

const (
	InstallDir  = "/opt/kypanel"
	ConfigPath  = InstallDir + "/config.json"
	LogsDir     = InstallDir + "/logs"
	ServiceName = "kypanel"
	PanelBin    = InstallDir + "/panel"
	EnvFile     = InstallDir + "/panel.env"
)

// panelDBPath 返回面板数据库文件路径：
// 优先从 config.json 的 db.path 读取（与面板进程一致），否则兜底 InstallDir/panel.db。
func panelDBPath() string {
	if c, err := config.Load(ConfigPath); err == nil && c.DB.Path != "" {
		return c.DB.Path
	}
	return InstallDir + "/panel.db"
}

// cfg 全局面板配置（复用 config.Config，确保 save 时不丢字段）
var cfg *config.Config

const (
	CertFile = InstallDir + "/certs/panel.crt"
	KeyFile  = InstallDir + "/certs/panel.key"
)

var (
	usernameRe  = regexp.MustCompile(`^[a-zA-Z0-9_]{3,32}$`)
	portInRange = func(p int) bool { return p >= 8888 && p <= 65535 }
	scanner     = bufio.NewReader(os.Stdin)
)

// Run 进入交互式管理菜单（阻塞直到用户退出）
func Run() {
	if !isRoot() {
		fmt.Fprintln(os.Stderr, "[错误] 请以 root 身份运行 ky 命令")
		os.Exit(1)
	}
	for {
		showMenu()
		choice := readLine("请输入命令编号: ")
		switch strings.TrimSpace(choice) {
		case "1":
			startPanel()
		case "2":
			stopPanel()
		case "3":
			restartPanel()
		case "4":
			changePort()
		case "5":
			changeUsername()
		case "6":
			changePassword()
		case "7":
			showInfo()
		case "8":
			cleanDisk()
		case "9":
			configHTTPS()
		case "0", "q", "Q", "exit":
			fmt.Println("再见")
			return
		default:
			fmt.Println("无效选项，请重新输入")
		}
		fmt.Println()
	}
}

func showMenu() {
	fmt.Println("============== kypanel 命令行 ==============")
	fmt.Println("(1) 启动面板        (2) 停止面板       (3) 重启面板")
	fmt.Println("(4) 修改面板端口    (5) 修改面板用户名  (6) 修改面板密码")
	fmt.Println("(7) 查看面板信息    (8) 磁盘清理工具   (9) 配置 HTTPS")
	fmt.Println("(0) 退出")
	fmt.Println("=============================================")
}

// ---------- 输入辅助 ----------

func readLine(prompt string) string {
	fmt.Print(prompt)
	line, err := scanner.ReadString('\n')
	if err != nil {
		return ""
	}
	return strings.TrimSpace(line)
}

// readPassword 明文读密码（lp 是 root 工具）。
func readPassword(prompt string) string {
	return readLine(prompt)
}

// ---------- systemctl 封装 ----------

func systemctlAction(action string) error {
	cmd := exec.Command("systemctl", action, ServiceName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func isServiceActive() bool {
	cmd := exec.Command("systemctl", "is-active", "--quiet", ServiceName)
	_ = cmd.Run()
	return cmd.ProcessState.ExitCode() == 0
}

// ---------- 配置读写 ----------
// 复用 config.Load/Save 确保所有 JSON 字段（含 security_entrance / panel_name
// / panel_sub / auth / log / store 等）都正确保留与写入。

func loadConfig() error {
	c, err := config.Load(ConfigPath)
	if err != nil {
		return fmt.Errorf("读取配置失败: %w（请确认已安装面板: %s）", err, ConfigPath)
	}
	cfg = c
	return nil
}

func saveConfig() error {
	if cfg == nil {
		return fmt.Errorf("配置未加载，请先调用 loadConfig")
	}
	return cfg.Save(ConfigPath)
}

// ---------- 数据库 ----------

// openPanelDB 直接打开 panel.db，与面板进程共享（SQLite WAL 模式可并发读）
func openPanelDB() (*gorm.DB, error) {
	dbPath := panelDBPath()
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("数据库不存在: %s，请先启动一次面板完成初始化", dbPath)
	}
	db, err := gorm.Open(sqlite.Open(dbPath+"?_busy_timeout=5000&_journal_mode=WAL"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, err
	}
	return db, nil
}

// ---------- 1/2/3 启停 ----------

func startPanel() {
	if isServiceActive() {
		fmt.Println("面板已在运行中")
		return
	}
	if err := systemctlAction("start"); err != nil {
		fmt.Println("启动失败:", err)
		return
	}
	time.Sleep(1 * time.Second)
	if isServiceActive() {
		fmt.Println("面板已启动")
	} else {
		fmt.Println("面板启动失败，请查看日志: journalctl -u kypanel -n 30")
	}
}

func stopPanel() {
	if !isServiceActive() {
		fmt.Println("面板未在运行")
		return
	}
	if err := systemctlAction("stop"); err != nil {
		fmt.Println("停止失败:", err)
		return
	}
	fmt.Println("面板已停止")
}

func restartPanel() {
	if err := systemctlAction("restart"); err != nil {
		fmt.Println("重启失败:", err)
		return
	}
	time.Sleep(1 * time.Second)
	if isServiceActive() {
		fmt.Println("面板已重启")
	} else {
		fmt.Println("面板重启失败，请查看日志: journalctl -u kypanel -n 30")
	}
}

// ---------- 4 修改面板端口 ----------

func changePort() {
	if err := loadConfig(); err != nil {
		fmt.Println(err)
		return
	}
	oldPort := cfg.Server.Port
	fmt.Printf("当前端口: %d\n", oldPort)
	input := readLine("新端口 (8888-65535, 回车取消): ")
	if input == "" {
		fmt.Println("已取消")
		return
	}
	newPort, err := strconv.Atoi(input)
	if err != nil || !portInRange(newPort) {
		fmt.Println("端口无效（需 8888-65535 整数）")
		return
	}
	if newPort == oldPort {
		fmt.Println("与当前端口相同，未修改")
		return
	}
	// 检查端口是否被占用
	check := exec.Command("sh", "-c",
		fmt.Sprintf("ss -tln 2>/dev/null | grep -E '[:.]%d\\b' || netstat -tln 2>/dev/null | grep -E '[:.]%d\\b'", newPort, newPort))
	buf, _ := check.CombinedOutput()
	if len(strings.TrimSpace(string(buf))) > 0 {
		fmt.Printf("端口 %d 已被占用，请更换:\n%s\n", newPort, strings.TrimSpace(string(buf)))
		return
	}
	// 放行新端口
	if err := service.AllowPortWithSource(strconv.Itoa(newPort), "tcp", "面板管理端口", "panel"); err != nil {
		fmt.Printf("放行新端口失败（可能防火墙不存在）: %v\n", err)
	} else {
		fmt.Printf("已放行端口 %d (tcp)\n", newPort)
	}
	// 询问是否移除旧端口
	if oldPort > 0 {
		ans := strings.ToLower(readLine(fmt.Sprintf("是否移除旧端口 %d? [y/N]: ", oldPort)))
		if ans == "y" || ans == "yes" {
			if err := service.RemovePortWithSource(strconv.Itoa(oldPort), "tcp", "panel"); err != nil {
				fmt.Printf("移除旧端口失败: %v\n", err)
			} else {
				fmt.Printf("已移除端口 %d\n", oldPort)
			}
		}
	}
	// 写回配置并重启
	cfg.Server.Port = newPort
	if err := saveConfig(); err != nil {
		fmt.Println("保存配置失败:", err)
		return
	}
	fmt.Println("配置已更新，正在重启面板...")
	if err := systemctlAction("restart"); err != nil {
		fmt.Println("重启失败:", err)
		return
	}
	time.Sleep(1 * time.Second)
	fmt.Printf("完成。新访问地址: %s（请在云安全组也放行此端口）\n", buildAccessURL())
}

// ---------- 9 配置 HTTPS ----------

func configHTTPS() {
	if err := loadConfig(); err != nil {
		fmt.Println(err)
		return
	}
	for {
		status := "HTTP（未启用）"
		if bool(cfg.Server.HTTPS) {
			status = "HTTPS（已启用）"
		}
		fmt.Println("------------- HTTPS 配置 -------------")
		fmt.Printf("  当前状态: %s\n", status)
		if bool(cfg.Server.HTTPS) {
			fmt.Printf("  证书文件: %s\n", cfg.Server.CertFile)
			fmt.Printf("  私钥文件: %s\n", cfg.Server.KeyFile)
		}
		fmt.Println("  (1) 启用 HTTPS（自动生成自签名证书）")
		fmt.Println("  (2) 关闭 HTTPS（恢复 HTTP）")
		fmt.Println("  (0) 返回上级菜单")
		choice := strings.TrimSpace(readLine("请选择: "))
		switch choice {
		case "1":
			enableHTTPS()
		case "2":
			disableHTTPS()
		case "0", "":
			return
		default:
			fmt.Println("无效选项，请重新输入")
		}
		fmt.Println()
	}
}

// enableHTTPS 生成自签名证书并启用 HTTPS
func enableHTTPS() {
	if bool(cfg.Server.HTTPS) {
		fmt.Println("HTTPS 已启用，无需重复配置")
		return
	}
	cn := strings.TrimSpace(readLine("证书域名或服务器IP（回车自动检测）: "))
	if cn == "" {
		if ip := detectPublicIP(); ip != "" {
			cn = ip
		} else {
			cn = "localhost"
		}
	}
	fmt.Println("正在生成自签名证书（有效期 3650 天，需要 openssl）...")
	if err := genSelfSignedCert(cn); err != nil {
		fmt.Println("生成证书失败:", err)
		fmt.Println("请先安装 openssl：CentOS 用 yum install -y openssl，Ubuntu/Debian 用 apt install -y openssl")
		return
	}
	cfg.Server.HTTPS = true
	cfg.Server.CertFile = CertFile
	cfg.Server.KeyFile = KeyFile
	if err := saveConfig(); err != nil {
		fmt.Println("保存配置失败:", err)
		return
	}
	fmt.Println("配置已更新，正在重启面板...")
	if err := systemctlAction("restart"); err != nil {
		fmt.Println("重启失败:", err)
		return
	}
	time.Sleep(1 * time.Second)
	fmt.Printf("完成！请使用 https://%s:%d 访问面板\n", cn, cfg.Server.Port)
	fmt.Println("说明：自签名证书浏览器会提示不受信任，点「高级 → 继续访问」即可，这是正常现象。")
}

// disableHTTPS 关闭 HTTPS 恢复 HTTP
func disableHTTPS() {
	if !bool(cfg.Server.HTTPS) {
		fmt.Println("HTTPS 当前未启用")
		return
	}
	cfg.Server.HTTPS = false
	cfg.Server.CertFile = ""
	cfg.Server.KeyFile = ""
	if err := saveConfig(); err != nil {
		fmt.Println("保存配置失败:", err)
		return
	}
	fmt.Println("配置已更新，正在重启面板...")
	if err := systemctlAction("restart"); err != nil {
		fmt.Println("重启失败:", err)
		return
	}
	time.Sleep(1 * time.Second)
	fmt.Printf("完成！请使用 %s 访问面板\n", buildAccessURL())
}

// genSelfSignedCert 用 openssl 生成自签名证书（crt + key）
func genSelfSignedCert(cn string) error {
	if err := os.MkdirAll(InstallDir+"/certs", 0o755); err != nil {
		return err
	}
	// 清理 CN 中的非法字符
	cn = regexp.MustCompile(`[^\w.\-]`).ReplaceAllString(cn, "")
	if cn == "" {
		cn = "localhost"
	}
	cmd := exec.Command("openssl", "req", "-x509", "-newkey", "rsa:2048",
		"-keyout", KeyFile,
		"-out", CertFile,
		"-days", "3650", "-nodes",
		"-subj", "/C=CN/ST=Guangdong/L=Shenzhen/O=kypanel/CN="+cn)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ---------- 5 修改用户名 ----------

func changeUsername() {
	db, err := openPanelDB()
	if err != nil {
		fmt.Println(err)
		return
	}
	var admin model.Admin
	if err := db.Order("id ASC").First(&admin).Error; err != nil {
		fmt.Println("查询管理员失败:", err)
		return
	}
	fmt.Printf("当前用户名: %s\n", admin.Username)
	for {
		input := readLine("新用户名 (3-32 位字母/数字/下划线, 回车取消): ")
		if input == "" {
			fmt.Println("已取消")
			return
		}
		if !usernameRe.MatchString(input) {
			fmt.Println("格式不符，请重新输入")
			continue
		}
		var cnt int64
		if err := db.Model(&model.Admin{}).Where("username = ? AND id <> ?", input, admin.ID).Count(&cnt).Error; err != nil {
			fmt.Println("查询失败:", err)
			return
		}
		if cnt > 0 {
			fmt.Println("用户名已存在，请换一个")
			continue
		}
		// 用 Updates 只更新需要的字段，避免 GORM Save 触发全字段更新引发竞态/锁定问题；
		// 同时 +1 TokenVer 使旧 token 立即失效，强制用户用新用户名重新登录。
		res := db.Model(&model.Admin{}).Where("id = ?", admin.ID).
			Updates(map[string]interface{}{"username": input, "token_ver": admin.TokenVer + 1})
		if res.Error != nil {
			fmt.Println("修改失败:", res.Error)
			return
		}
		if res.RowsAffected == 0 {
			fmt.Printf("修改失败：未匹配到记录（ID=%d）\n", admin.ID)
			return
		}
		fmt.Printf("用户名已更新为: %s（请用新用户名重新登录）\n", input)
		// 重启面板以清空进程内 TokenVer 缓存（参见 changePassword 注释）
		fmt.Println("正在重启面板服务以应用新 TokenVer...")
		if err := systemctlAction("restart"); err != nil {
			fmt.Printf("重启失败（请手动执行 systemctl restart kypanel）: %v\n", err)
		} else {
			time.Sleep(1 * time.Second)
			fmt.Println("完成。新用户名已生效。")
		}
		return
	}
}

// ---------- 6 修改密码 ----------

func changePassword() {
	db, err := openPanelDB()
	if err != nil {
		fmt.Println(err)
		return
	}
	var admin model.Admin
	if err := db.Order("id ASC").First(&admin).Error; err != nil {
		fmt.Println("查询管理员失败:", err)
		return
	}
	fmt.Printf("正在重置账号 [%s] 的密码\n", admin.Username)
	for attempt := 1; attempt <= 3; attempt++ {
		p1 := readPassword("新密码 (6-32 位, 回车取消): ")
		if p1 == "" {
			fmt.Println("已取消")
			return
		}
		if len(p1) < 6 || len(p1) > 32 {
			fmt.Println("长度需 6-32 位")
			continue
		}
		p2 := readPassword("确认新密码: ")
		if p1 != p2 {
			fmt.Println("两次输入不一致")
			continue
		}
		// CLI 工具是 root 身份运行，直接重置密码（不要求旧密码）
		if err := admin.SetPassword(p1); err != nil {
			fmt.Println("加密失败:", err)
			return
		}
		// 用 Updates 只更新 password_hash + token_ver(+1)，避免 GORM Save 把其他字段
		// （TOTPSecret 等）覆盖/清空；同时 +1 TokenVer 让所有已签发 token 失效，
		// 强制用户用新密码重新登录。
		newTokenVer := admin.TokenVer + 1
		res := db.Model(&model.Admin{}).Where("id = ?", admin.ID).
			Updates(map[string]interface{}{
				"password_hash": admin.PasswordHash,
				"token_ver":     newTokenVer,
			})
		if res.Error != nil {
			fmt.Println("保存失败:", res.Error)
			return
		}
		if res.RowsAffected == 0 {
			fmt.Printf("保存失败：未匹配到记录（ID=%d）\n", admin.ID)
			return
		}
		fmt.Println("密码已重置（请用新密码重新登录）")
		// 重启面板服务以清空进程内的 TokenVer 缓存，避免内存里的旧 token_ver 与 DB 不一致
		// 导致用户登录后所有接口都返回 401「登录已失效」。
		fmt.Println("正在重启面板服务以应用新 TokenVer...")
		if err := systemctlAction("restart"); err != nil {
			fmt.Printf("重启失败（请手动执行 systemctl restart kypanel）: %v\n", err)
		} else {
			time.Sleep(1 * time.Second)
			fmt.Println("完成。新密码已生效，请用浏览器重新登录。")
		}
		return
	}
	fmt.Println("尝试次数过多，已取消")
}

// buildAccessURL 根据当前 cfg 拼接完整的访问 URL（含安全入口）。
// 示例：https://<server-ip>:<port>/<entrance>
func buildAccessURL() string {
	scheme := "http"
	if bool(cfg.Server.HTTPS) {
		scheme = "https"
	}
	url := fmt.Sprintf("%s://%s:%d", scheme, detectPublicIP(), cfg.Server.Port)
	if cfg.SecurityEntrance != "" {
		url += "/" + cfg.SecurityEntrance
	}
	return url
}

// ---------- 7 查看面板信息 ----------

func showInfo() {
	if err := loadConfig(); err != nil {
		fmt.Println(err)
		return
	}
	// 状态
	active := isServiceActive()
	status := "未运行"
	if active {
		status = "运行中"
	}
	// 管理员账号
	username := "(未知)"
	if db, err := openPanelDB(); err == nil {
		var admin model.Admin
		if err := db.Order("id ASC").First(&admin).Error; err == nil {
			username = admin.Username
		}
	}
	// 安装信息
	binExists := fileExists(PanelBin)
	cfgExists := fileExists(ConfigPath)

	// HTTPS 状态
	httpsText := "未启用"
	if bool(cfg.Server.HTTPS) {
		httpsText = "已启用"
	}
	// 安全入口
	entrance := cfg.SecurityEntrance
	if entrance == "" {
		entrance = "(未启用)"
	}
	// 面板名称/副标题
	panelName := cfg.PanelName
	if panelName == "" {
		panelName = "开猿运维"
	}
	panelSub := cfg.PanelSub
	if panelSub == "" {
		panelSub = "服务器管理面板"
	}
	fmt.Println("------------- 面板信息 -------------")
	fmt.Printf("  运行状态 : %s\n", status)
	fmt.Printf("  面板名称 : %s\n", panelName)
	fmt.Printf("  面板副标 : %s\n", panelSub)
	fmt.Printf("  面板端口 : %d\n", cfg.Server.Port)
	fmt.Printf("  HTTPS    : %s\n", httpsText)
	fmt.Printf("  安全入口 : %s\n", entrance)
	fmt.Printf("  访问地址 : %s\n", buildAccessURL())
	fmt.Printf("  管理员   : %s\n", username)
	fmt.Printf("  安装目录 : %s\n", InstallDir)
	fmt.Printf("  面板二进制: %s (%s)\n", PanelBin, yesNo(binExists))
	fmt.Printf("  配置文件 : %s (%s)\n", ConfigPath, yesNo(cfgExists))
	fmt.Println("------------- 磁盘使用 -------------")
	printDiskUsage(InstallDir)
	fmt.Println("-----------------------------------")
}

func detectPublicIP() string {
	for _, url := range []string{
		"https://api.ipify.org",
		"https://ifconfig.me/ip",
		"https://ip.sb",
	} {
		cmd := exec.Command("curl", "-fsSL", "--max-time", "4", url)
		buf, err := cmd.Output()
		if err == nil {
			if ip := strings.TrimSpace(string(buf)); ip != "" {
				return ip
			}
		}
	}
	// 退化：本地网卡 IP
	cmd := exec.Command("sh", "-c", "hostname -I 2>/dev/null | awk '{print $1}'")
	buf, err := cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(buf))
	}
	return ""
}

func printDiskUsage(dir string) {
	cmd := exec.Command("df", "-h", dir)
	buf, err := cmd.Output()
	if err != nil {
		fmt.Println("  (df 失败)")
		return
	}
	lines := strings.Split(strings.TrimSpace(string(buf)), "\n")
	if len(lines) >= 2 {
		fmt.Println("  " + lines[0])
		fmt.Println("  " + lines[1])
	}
	// 面板日志大小
	if size, err := dirSize(LogsDir); err == nil {
		fmt.Printf("  面板日志目录: %s (%.2f MB)\n", LogsDir, float64(size)/1024/1024)
	}
}

func dirSize(path string) (int64, error) {
	var total int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// ---------- 8 磁盘清理 ----------

func cleanDisk() {
	fmt.Println("请选择清理项:")
	fmt.Println("  (a) 清理面板日志 (默认保留 30 天)")
	fmt.Println("  (b) 清理 /tmp 下 kypanel 相关临时文件")
	fmt.Println("  (c) 清理面板 data 目录下的旧数据库备份")
	fmt.Println("  (d) 全部清理")
	fmt.Println("  (回车取消)")
	choice := strings.ToLower(readLine("选择: "))
	if choice == "" {
		fmt.Println("已取消")
		return
	}
	cleanLogs := choice == "a" || choice == "d"
	cleanTmp := choice == "b" || choice == "d"
	cleanBackups := choice == "c" || choice == "d"

	var freed int64
	if cleanLogs {
		freed += cleanOldLogs(30)
	}
	if cleanTmp {
		freed += cleanTmpFiles()
	}
	if cleanBackups {
		freed += cleanDBBackups()
	}
	fmt.Printf("清理完成，释放空间: %.2f MB\n", float64(freed)/1024/1024)
}

// cleanOldLogs 删除超过 maxDay 天的日志（保留当前日志文件）
func cleanOldLogs(maxDay int) int64 {
	var freed int64
	cutoff := time.Now().AddDate(0, 0, -maxDay)
	entries, err := os.ReadDir(LogsDir)
	if err != nil {
		fmt.Println("  读取日志目录失败:", err)
		return 0
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		// 跳过当前正在使用的日志（panel.log）
		if e.Name() == "panel.log" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			full := filepath.Join(LogsDir, e.Name())
			size := info.Size()
			if err := os.Remove(full); err == nil {
				freed += size
				fmt.Printf("  已删除: %s (%.1f KB)\n", full, float64(size)/1024)
			}
		}
	}
	return freed
}

// cleanTmpFiles 删除 /tmp 下 kypanel 开头的文件
func cleanTmpFiles() int64 {
	var freed int64
	entries, err := os.ReadDir("/tmp")
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "kypanel") || strings.HasPrefix(name, "panel.") {
			full := "/tmp/" + name
			info, err := e.Info()
			if err != nil {
				continue
			}
			if err := os.Remove(full); err == nil {
				freed += info.Size()
				fmt.Printf("  已删除: %s\n", full)
			}
		}
	}
	return freed
}

// cleanDBBackups 删除 data 目录下的旧备份（panel.db.bak.*）
func cleanDBBackups() int64 {
	var freed int64
	entries, err := os.ReadDir(InstallDir + "/data")
	if err != nil {
		return 0
	}
	cutoff := time.Now().AddDate(0, 0, -7)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasPrefix(e.Name(), "panel.db.bak.") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			full := filepath.Join(InstallDir, "data", e.Name())
			size := info.Size()
			if err := os.Remove(full); err == nil {
				freed += size
				fmt.Printf("  已删除: %s (%.1f KB)\n", full, float64(size)/1024)
			}
		}
	}
	return freed
}

// ---------- 通用工具 ----------

// isRoot 判断当前是否为 root（用 id -u，跨平台可编译）
func isRoot() bool {
	cmd := exec.Command("id", "-u")
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "0"
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func yesNo(b bool) string {
	if b {
		return "存在"
	}
	return "缺失"
}
