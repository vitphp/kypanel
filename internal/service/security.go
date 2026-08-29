package service

import (
	"errors"
	"os/exec"
	"strings"
)

// ---------------------------------------------------------------------------
// 安全中心：防火墙状态查询、应用端口自动放行/回收。
// 端口/IP/国家/运营商规则的统一模型与防火墙下发见 security_rules.go。
// ---------------------------------------------------------------------------

// FirewallStatus 防火墙状态
type FirewallStatus struct {
	Backend      string `json:"backend"` // firewalld / nftables / iptables / none
	Enabled      bool   `json:"enabled"`
	DefaultDrop  bool   `json:"default_drop"` // 默认拒绝模式（只放行基础端口）
	BasePorts    []int  `json:"base_ports"`   // 默认开放的基础端口 22/80/443/面板端口
	IpRegionOK   bool   `json:"ip_region_ok"` // 离线库是否可用
	IpRegionSize int    `json:"ip_region_size"`
	IpRegionErr  string `json:"ip_region_err"`
}

// DetectFirewall 检测系统可用的防火墙后端
func DetectFirewall() (string, bool) {
	if cmdExists("firewall-cmd") {
		// 检查 firewalld 是否在运行
		if out, err := exec.Command("firewall-cmd", "--state").Output(); err == nil && strings.Contains(strings.ToLower(string(out)), "running") {
			return "firewalld", true
		}
	}
	if cmdExists("nft") {
		return "nftables", true
	}
	if cmdExists("iptables") {
		return "iptables", true
	}
	return "none", false
}

func cmdExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// GetFirewallStatus 获取防火墙与离线库状态
func GetFirewallStatus() FirewallStatus {
	backend, enabled := DetectFirewall()
	ok, size, errMsg := IpRegionStatus()
	return FirewallStatus{
		Backend:      backend,
		Enabled:      enabled,
		DefaultDrop:  firewallDefaultDrop(),
		BasePorts:    baseOpenPorts(),
		IpRegionOK:   ok,
		IpRegionSize: size,
		IpRegionErr:  errMsg,
	}
}

// ---------- 端口放行 ----------

// appAutoRemark 应用安装时自动放行端口的规则备注前缀，用于卸载时精准回收
const appAutoRemark = "app-auto"

// commonPortRemarks 常见端口 → 用途映射。放行端口未显式指定备注时，
// 自动按端口号填入用途说明，便于在防火墙列表里一眼识别（如 22=SSH、3389=RDP）。
var commonPortRemarks = map[string]string{
	"20":    "FTP 数据传输",
	"21":    "FTP 控制",
	"22":    "SSH",
	"23":    "Telnet",
	"25":    "SMTP 邮件发送",
	"53":    "DNS",
	"80":    "HTTP",
	"110":   "POP3 收件",
	"143":   "IMAP 收件",
	"443":   "HTTPS",
	"465":   "SMTPS 邮件发送",
	"587":   "SMTP 邮件提交",
	"993":   "IMAPS 收件",
	"995":   "POP3S 收件",
	"1433":  "SQL Server",
	"1521":  "Oracle",
	"2375":  "Docker API（明文）",
	"2376":  "Docker API（TLS）",
	"3306":  "MySQL",
	"3389":  "RDP 远程桌面",
	"5432":  "PostgreSQL",
	"6379":  "Redis",
	"8080":  "HTTP 备用端口",
	"8443":  "HTTPS 备用端口",
	"8888":  "HTTP 备用端口",
	"9090":  "Prometheus",
	"9200":  "Elasticsearch",
	"11211": "Memcached",
	"27017": "MongoDB",
}

// AllowPort 放行端口（兼容旧调用）：remark 为空时自动填端口默认用途。
// 支持 IPv4 与 IPv6，支持 tcp/udp（含端口段如 39000-40000）。
// 走统一的规则系统（security_rules.json 持久化 + ApplySecurityRules 统一下发）。
func AllowPort(port, proto string) error {
	return AllowPortWithRemark(port, proto, "")
}

// AllowPortWithRemark 放行端口，并带备注说明（remark 为空时自动按端口用途填充）。
// 各业务（应用安装 / 站点绑定 / 面板端口）统一走此接口，避免散落、便于统一备注。
func AllowPortWithRemark(port, proto, remark string) error {
	return allowPort(port, proto, remark, "")
}

// AllowPortWithSource 放行端口，带备注 + 自动来源标识（用于卸载/变更时精准回收）。
//   - source 形如 "app:mysql" / "site:example.com" / "panel" 等
func AllowPortWithSource(port, proto, remark, source string) error {
	return allowPort(port, proto, remark, source)
}

// allowPort 放行端口的统一实现
func allowPort(port, proto, remark, source string) error {
	port = strings.TrimSpace(port)
	if port == "" {
		return errors.New("端口不能为空")
	}
	if proto == "" {
		proto = "tcp"
	}
	switch proto {
	case "tcp", "udp", "tcpudp", "tcp/udp":
	default:
		return errors.New("协议必须是 tcp/udp/tcpudp")
	}
	// 幂等：已存在相同端口 + 协议 + app-auto 标记的放行规则时，直接返回，避免重复放行
	if appAutoPortExists(port, proto) {
		return nil
	}
	// 备注为空时交给 AddSecurityRule 自动按端口用途填充（22=SSH、3389=RDP 等）
	_, err := AddSecurityRule(AddSecurityRuleReq{
		Type:       RuleTypePort,
		Port:       port,
		Proto:      proto,
		Action:     RuleActionAllow,
		Direction:  RuleDirectionIn,
		Remark:     remark,
		AutoSource: source,
	})
	return err
}

// appAutoPortExists 检查是否已存在「同端口 + 同协议」的入站放行规则（不限 Remark 标记）
// 用于跨 system-base / app-auto 的去重：同一端口 + 同一协议只能有 1 条放行规则，
// 重复时直接跳过（避免系统基础端口和应用自动放行产生两条记录）。
// 注意：tcp 和 udp 视为不同协议，不会去重；tcpudp 与 tcp/udp 等价匹配。
func appAutoPortExists(port, proto string) bool {
	if proto == "tcp/udp" {
		proto = "tcpudp"
	}
	for _, r := range ListSecurityRules() {
		if r.Type != RuleTypePort || r.Action != RuleActionAllow {
			continue
		}
		if r.Port != port {
			continue
		}
		if r.Proto == proto || r.Proto == "tcpudp" || proto == "tcpudp" {
			return true
		}
	}
	return false
}

// RemovePort 移除端口放行（兼容旧调用）：按端口 + app-auto 标记删除自动放行规则
func RemovePort(port, proto string) error {
	return RemovePortWithSource(port, proto, "")
}

// RemovePortWithSource 移除端口放行，按 source 精准回收（source 非空时只删该来源的规则）。
// source 为空时回退到旧的 app-auto 备注匹配（兼容历史数据）。
func RemovePortWithSource(port, proto, source string) error {
	port = strings.TrimSpace(port)
	if proto == "" {
		proto = "tcp"
	}
	if proto == "tcp/udp" {
		proto = "tcpudp"
	}
	rules := ListSecurityRules()
	removed := 0
	for _, r := range rules {
		if r.Type != RuleTypePort || r.Action != RuleActionAllow {
			continue
		}
		// source 匹配：有 source 时按 AutoSource 精准回收；无 source 时回退 app-auto 备注
		if source != "" {
			if r.AutoSource != source {
				continue
			}
		} else {
			if r.Remark != appAutoRemark {
				continue
			}
		}
		if r.Port != port {
			continue
		}
		if proto != "" && r.Proto != proto {
			continue
		}
		if err := DeleteSecurityRule(r.ID); err == nil {
			removed++
		}
	}
	if removed == 0 {
		return errors.New("未找到对应的自动放行端口规则")
	}
	return nil
}
