package service

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"kypanel/internal/config"
	"kypanel/internal/model"
	"kypanel/internal/utils"
	"kypanel/internal/version"
)

// PanelInfo 面板设置信息
type PanelInfo struct {
	Port            int    `json:"port"`
	HTTPS           bool   `json:"https"`
	Domain          string `json:"domain"`
	SecurityEntrance string `json:"security_entrance"` // 安全入口（登录页 URL 前缀）
	PanelName       string `json:"panel_name"`         // 面板显示名称
	PanelSub        string `json:"panel_sub"`          // 面板副标题
	PanelVersion    string `json:"panel_version"`      // 面板当前版本
}

// encryptSetting 加密后落库（敏感凭据不以明文存储）。
// 加密失败（crypto 未初始化等极端情况）时降级返回原值，避免功能不可用。
func encryptSetting(v string) string {
	s, err := utils.EncryptString(v)
	if err != nil {
		return v
	}
	return s
}

// decryptSetting 读取设置时解密。
// 非 enc:v1: 前缀的原样返回（兼容历史明文数据）；解密失败返回原值，保证读取不中断。
func decryptSetting(v string) string {
	s, err := utils.DecryptString(v)
	if err != nil {
		return v
	}
	return s
}

// GetMCPToken 读取 MCP 访问令牌（AI 工具远程调用面板凭据）
func GetMCPToken() string {
	return decryptSetting(model.GetSetting("mcp_token"))
}

// GenerateMCPToken 生成长期 MCP 访问令牌（一年有效）并保存，返回明文
func GenerateMCPToken(adminID uint, username string) (string, error) {
	var admin model.Admin
	if err := model.DB.First(&admin, adminID).Error; err != nil {
		return "", err
	}
	token, err := utils.GenerateToken(adminID, username, 365*24, admin.TokenVer)
	if err != nil {
		return "", err
	}
	if err := model.SetSetting("mcp_token", encryptSetting(token)); err != nil {
		return "", err
	}
	return token, nil
}

// GeneratePMAToken 生成 phpMyAdmin 访问令牌（7 天有效），返回明文
func GeneratePMAToken(adminID uint, username string) (string, error) {
	var admin model.Admin
	if err := model.DB.First(&admin, adminID).Error; err != nil {
		return "", err
	}
	return utils.GenerateToken(adminID, username, 24*7, admin.TokenVer)
}

// GetPanelInfo 返回面板设置信息
func GetPanelInfo() PanelInfo {
	cfg := config.Get()
	return PanelInfo{
		Port:            cfg.Server.Port,
		HTTPS:           bool(cfg.Server.HTTPS),
		Domain:          cfg.Server.Domain,
		SecurityEntrance: cfg.SecurityEntrance,
		PanelName:       cfg.PanelName,
		PanelSub:        cfg.PanelSub,
		PanelVersion:    version.Version,
	}
}

// SavePortReq 保存端口请求
type SavePortReq struct {
	Port int `json:"port" binding:"required,min=1,max=65535"`
}

// SavePanelPort 保存面板端口到配置文件
// 返回：旧端口（用于判断是否需要重启）
func SavePanelPort(req SavePortReq) (oldPort int, err error) {
	cfg := config.Get()
	oldPort = cfg.Server.Port
	if oldPort == req.Port {
		return oldPort, nil // 端口没变，不操作防火墙、不重启
	}
	cfg.Server.Port = req.Port
	if err = cfg.Save(configFilePath()); err != nil {
		return 0, err
	}
	// 立即在防火墙里放行新端口（baseOpenPorts 在下次重载时会自动包含新端口，
	// 这里显式调一次是多一层保护，且让用户能在「防火墙」里看到新端口的规则）
	if err = AllowPortWithSource(strconv.Itoa(req.Port), "tcp", "面板管理端口", "panel"); err != nil {
		// 放行失败不阻塞主流程（可能未启用防火墙），但要记录
		fmt.Printf("[settings] 防火墙放行新端口 %d 失败: %v\n", req.Port, err)
	}
	return oldPort, nil
}

// SaveDomainReq 保存域名请求
type SaveDomainReq struct {
	Domain string `json:"domain"`
}

// SavePanelDomain 保存面板绑定域名到配置文件
func SavePanelDomain(req SaveDomainReq) error {
	cfg := config.Get()
	cfg.Server.Domain = req.Domain
	return cfg.Save(configFilePath())
}

// SecurityEntranceCharset 安全入口可用的字符集（排除易混淆的 0/O/I/l/1）
const SecurityEntranceCharset = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// GenerateSecurityEntrance 生成 6 位随机入口串（crypto/rand 安全源）
func GenerateSecurityEntrance() string {
	const n = 6
	buf := make([]byte, n)
	randBytes := make([]byte, n)
	if _, err := rand.Read(randBytes); err != nil {
		// 兜底用时间戳（极端情况）
		s := fmt.Sprintf("%d", time.Now().UnixNano())
		if len(s) >= n {
			return s[len(s)-n:]
		}
		return s
	}
	for i := range buf {
		buf[i] = SecurityEntranceCharset[int(randBytes[i])%len(SecurityEntranceCharset)]
	}
	return string(buf)
}

// SaveEntranceReq 保存安全入口请求
type SaveEntranceReq struct {
	Entrance string `json:"entrance"` // 6 位字母数字组合（不区分大小写），空字符串表示关闭安全入口
}

// SavePanelNameReq 保存面板名称请求
type SavePanelNameReq struct {
	Name string `json:"name"` // 面板名称（1-32 字符）
	Sub  string `json:"sub"`  // 副标题（0-32 字符，可空）
}

// SavePanelName 保存面板显示名称/副标题到 config.json。
// 校验：name 非空且 ≤32 字符，sub ≤32 字符。
func SavePanelName(req SavePanelNameReq) error {
	name := strings.TrimSpace(req.Name)
	sub := strings.TrimSpace(req.Sub)
	if name == "" || len(name) > 32 {
		return errors.New("面板名称不能为空且不能超过 32 字符")
	}
	if len(sub) > 32 {
		return errors.New("副标题不能超过 32 字符")
	}
	cfg := config.Get()
	cfg.PanelName = name
	cfg.PanelSub = sub
	return cfg.Save(configFilePath())
}

// SavePanelSecurityEntrance 保存面板安全入口到配置文件。
// 校验：长度 1-10 位、字符必须是字母或数字（不限制大小写，不排除易混字符）。
// 空值表示关闭入口。
// 同时返回旧值，便于上层判断是否需要更新内存中的 currentEntrance。
func SavePanelSecurityEntrance(req SaveEntranceReq) (oldEntrance string, err error) {
	cfg := config.Get()
	oldEntrance = cfg.SecurityEntrance
	entrance := req.Entrance
	if entrance == "" {
		cfg.SecurityEntrance = ""
	} else {
		if l := len(entrance); l < 1 || l > 10 {
			return oldEntrance, errors.New("安全入口长度必须在 1-10 位之间")
		}
		for i := 0; i < len(entrance); i++ {
			c := entrance[i]
			if c >= '0' && c <= '9' {
				continue
			}
			if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
				continue
			}
			return oldEntrance, errors.New("安全入口只能包含字母和数字")
		}
		cfg.SecurityEntrance = entrance
	}
	if err = cfg.Save(configFilePath()); err != nil {
		return oldEntrance, err
	}
	return oldEntrance, nil
}

func configFilePath() string {
	path := config.Get().DataDir
	if path == "" {
		path = "/opt/kypanel"
	}
	return filepath.Join(path, "config.json")
}

// GetMysqlRootPwd 读取面板已保存的 MySQL root 密码
func GetMysqlRootPwd() string {
	return decryptSetting(model.GetSetting("mysql_root_pw"))
}

// SetMysqlRootPwd 实际修改 MySQL root@localhost 密码，并把新密码保存到面板设置。
// 用现有 root 认证（auth_socket / 旧密码）执行 ALTER，密码至少 6 位。
func SetMysqlRootPwd(pwd string) error {
	if len(pwd) < 6 {
		return errors.New("密码长度不能少于 6 位")
	}
	// 用当前 root 凭据（auth_socket 或 -p<old>）登录后 ALTER root@localhost 密码
	base := mysqlBaseArgs() // 当前 root 认证参数
	sqlStmt := fmt.Sprintf(
		"ALTER USER 'root'@'localhost' IDENTIFIED BY '%s'; FLUSH PRIVILEGES;",
		strings.ReplaceAll(pwd, "'", "''"),
	)
	res, err := ExecCommand("mysql "+base+" -e "+shellQuote(sqlStmt), 15*time.Second)
	if err != nil {
		return fmt.Errorf("执行 ALTER 失败: %w", err)
	}
	if res.ExitCode != 0 {
		return errors.New("修改 MySQL root 密码失败: " + strings.TrimSpace(res.Stderr))
	}
	return model.SetSetting("mysql_root_pw", encryptSetting(pwd))
}

// ResetMysqlRootPwdAfterInstall 安装 MySQL/MariaDB 后自动重置 root@localhost 密码为随机串。
// 用 auth_socket（刚安装完成时 root 默认走 socket 认证）连接，ALTER 成新密码。
// 失败仅返回错误，由调用方决定是否影响安装结果。
func ResetMysqlRootPwdAfterInstall() (string, error) {
	newPwd, err := generateRandomPassword(16)
	if err != nil {
		return "", fmt.Errorf("生成随机密码失败: %w", err)
	}
	// 刚装完时面板还未存密码，mysqlBaseArgs 返回无 -p 形式（auth_socket）
	if err := SetMysqlRootPwd(newPwd); err != nil {
		return "", err
	}
	return newPwd, nil
}

// generateRandomPassword 用 crypto/rand 生成 n 位大小写+数字混合的密码
func generateRandomPassword(n int) (string, error) {
	if n < 6 {
		n = 6
	}
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		out[i] = charset[idx.Int64()]
	}
	return string(out), nil
}

// LiteSSLSetting LiteSSL ACME EAB 凭据
type LiteSSLSetting struct {
	EabKid  string `json:"eab_kid"`
	EabHmac string `json:"eab_hmac"`
}

// GetLiteSSLSetting 读取 LiteSSL EAB 凭据
func GetLiteSSLSetting() LiteSSLSetting {
	return LiteSSLSetting{
		EabKid:  decryptSetting(model.GetSetting("litessl_eab_kid")),
		EabHmac: decryptSetting(model.GetSetting("litessl_eab_hmac")),
	}
}

// SetLiteSSLSetting 保存 LiteSSL EAB 凭据
func SetLiteSSLSetting(s LiteSSLSetting) error {
	if err := model.SetSetting("litessl_eab_kid", encryptSetting(s.EabKid)); err != nil {
		return err
	}
	return model.SetSetting("litessl_eab_hmac", encryptSetting(s.EabHmac))
}

// SystemLogLine slog 解析后的单行日志
type SystemLogLine struct {
	Time    string `json:"time"`    // 2026-08-25 15:21:48
	Level   string `json:"level"`   // INFO/WARN/ERROR/DEBUG
	Message string `json:"message"` // 完整消息（已去掉 time/level 前缀）
	IP      string `json:"ip"`      // 从消息中提取的来源 IP（可能为空）
	Raw     string `json:"raw"`     // 原始行（前端 fallback 用）
}

// ClearSystemLog 清空系统日志文件（截断文件到 0 字节，保留文件本身以便 slog 继续写入）
func ClearSystemLog() error {
	path := config.Get().Log.File
	if path == "" {
		return nil
	}
	return os.Truncate(path, 0)
}

// ReadSystemLog 读取系统日志尾部，返回结构化日志行（按时间倒序：最新在前）
func ReadSystemLog(lines int) ([]SystemLogLine, error) {
	if lines <= 0 || lines > 1000 {
		lines = 200
	}
	path := config.Get().Log.File
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []SystemLogLine{}, nil
		}
		return nil, err
	}
	all := strings.Split(string(data), "\n")
	// 去掉末尾空行
	for len(all) > 0 && strings.TrimSpace(all[len(all)-1]) == "" {
		all = all[:len(all)-1]
	}
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	// 解析并倒序（最新在前）
	parsed := make([]SystemLogLine, 0, len(all))
	for i := len(all) - 1; i >= 0; i-- {
		parsed = append(parsed, parseSystemLogLine(all[i]))
	}
	return parsed, nil
}

// parseSystemLogLine 解析单行 slog 格式：
// time="2026-08-25 15:21:48" level=INFO msg="..."
func parseSystemLogLine(line string) SystemLogLine {
	res := SystemLogLine{Raw: line, Message: line, Level: "INFO"}
	// 1) time="..."
	if m := timeRE.FindStringSubmatch(line); len(m) >= 2 {
		res.Time = m[1]
	}
	// 2) level=...
	if m := levelRE.FindStringSubmatch(line); len(m) >= 2 {
		res.Level = strings.ToUpper(m[1])
	}
	// 3) msg="..."（可能含双引号转义）
	if m := msgRE.FindStringSubmatch(line); len(m) >= 2 {
		res.Message = strings.ReplaceAll(m[1], `\"`, `"`)
	} else if res.Message == line {
		// 没匹配到 msg="..."（非 slog 格式，比如启动横幅、调试输出），保持原行
	}
	// 4) 从 message 中提取 IP（http: TLS handshake error from 1.2.3.4:xxx / from 1.2.3.4）
	if m := ipRE.FindStringSubmatch(res.Message); len(m) >= 2 {
		res.IP = m[1]
	}
	return res
}

// slog 字段解析正则
var (
	timeRE  = regexp.MustCompile(`time="([^"]*)"`)
	levelRE = regexp.MustCompile(`level=(INFO|WARN|ERROR|DEBUG)`)
	msgRE   = regexp.MustCompile(`msg="((?:[^"\\]|\\.)*)"`)
	ipRE    = regexp.MustCompile(`(?:from|client|remote) ((?:\d{1,3}\.){3}\d{1,3})(?::\d+)?`)
)

// ListInstallLogs 返回应用安装日志文件列表
func ListInstallLogs() []string {
	dir := filepath.Join(config.Get().DataDir, "logs", "apps")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
			names = append(names, strings.TrimSuffix(e.Name(), ".log"))
		}
	}
	return names
}
