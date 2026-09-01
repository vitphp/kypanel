package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Bool 兼容 JSON 中的 true/false 与 0/1（旧的安装脚本曾写入数字）
type Bool bool

func (b *Bool) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case "true", "1":
		*b = true
	case "false", "0":
		*b = false
	default:
		return fmt.Errorf("invalid bool value: %s", data)
	}
	return nil
}

func (b Bool) MarshalJSON() ([]byte, error) {
	if b {
		return []byte("true"), nil
	}
	return []byte("false"), nil
}

// Config 面板全局配置
type Config struct {
	// Server HTTP 服务
	Server struct {
		Port                int      `json:"port"`                  // 监听端口
		HTTPS               Bool     `json:"https"`                 // 是否启用 HTTPS
		CertFile            string   `json:"cert_file"`             // HTTPS 证书
		KeyFile             string   `json:"key_file"`              // HTTPS 私钥
		Domain              string   `json:"domain"`                // 绑定的域名（可选）
		TrustedIPs          []string `json:"trusted_ips"`           // 可信代理 IP
		FirewallDefaultDrop Bool     `json:"firewall_default_drop"` // 防火墙默认拒绝：只放行 22/80/443/面板端口（IPv4+IPv6）
	} `json:"server"`

	// DB 面板自身数据库
	DB struct {
		Path string `json:"path"` // SQLite 文件路径
	} `json:"db"`

	// Auth 认证
	Auth struct {
		JWTSecret string `json:"jwt_secret"` // JWT 签名密钥
		TokenHour int    `json:"token_hour"` // Token 有效期（小时）
	} `json:"auth"`

	// SecurityEntrance 安全入口：登录页 URL 前缀（如 https://host:port/abc123/login）
	// 空表示未启用安全入口。安装时自动生成 6 位字母数字组合随机串。
	SecurityEntrance string `json:"security_entrance"`

	// PanelName 面板显示名称（自定义品牌名），默认「开猿 Linux 面板」
	PanelName string `json:"panel_name"`
	// PanelSub 面板副标题（顶栏/登录页副标题），默认「服务器管理面板」
	PanelSub string `json:"panel_sub"`

	// Log 日志
	Log struct {
		Level  string `json:"level"`
		File   string `json:"file"`
		MaxDay int    `json:"max_day"`
	} `json:"log"`

	// Store 应用商店远程源（官网下发应用）
	Store struct {
		BaseURL string `json:"base_url"` // 官网 API 基址，如 https://panel.apihot.cn，空则禁用远程拉取
		// ReportErrors 应用安装/卸载失败时是否自动上报错误到官网（便于官方收集问题、针对性修复）
		ReportErrors Bool `json:"report_errors"`
	} `json:"store"`

	// DataDir 面板数据目录
	DataDir string `json:"data_dir"`
}

const (
	defaultPort    = 9999
	defaultDataDir = "/opt/kypanel"
)

var instance *Config

// Default 返回默认配置
func Default() *Config {
	c := &Config{}
	c.Server.Port = defaultPort
	c.Server.HTTPS = false
	c.DataDir = defaultDataDir
	c.DB.Path = filepath.Join(defaultDataDir, "panel.db")
	c.Auth.TokenHour = 24
	c.Log.Level = "info"
	c.Log.File = filepath.Join(defaultDataDir, "logs", "panel.log")
	c.Log.MaxDay = 30
	c.Store.BaseURL = "https://panel.apihot.cn"
	c.Store.ReportErrors = true // 默认开启错误上报，收集问题便于升级修复；可在 config.json 关闭
	return c
}

// Load 从文件加载配置，文件不存在则使用默认值
// 注意：每次调用都会重新读取文件（不使用 sync.Once），
// 以便 main 中先加载默认配置、再回填默认配置文件路径后二次加载。
func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		instance = cfg
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			instance = cfg // 配置不存在则用默认
			return cfg, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	instance = cfg
	return cfg, nil
}

// Get 返回全局配置实例
func Get() *Config {
	if instance == nil {
		instance = Default()
	}
	return instance
}

// Save 保存配置到文件
func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// EnsureDirs 确保面板运行所需的目录存在
func (c *Config) EnsureDirs() error {
	dirs := []string{
		c.DataDir,
		filepath.Dir(c.DB.Path),
		filepath.Dir(c.Log.File),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}
