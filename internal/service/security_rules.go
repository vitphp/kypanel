package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/bits"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"kypanel/internal/config"
)

const securityCmdTimeout = 15 * time.Second

// 规则类型
const (
	RuleTypePort    = "port"
	RuleTypeIP      = "ip"
	RuleTypeCountry = "country"
	RuleTypeISP     = "isp"
)

// 动作
const (
	RuleActionAllow = "allow"
	RuleActionBlock = "block"
)

// 方向
const (
	RuleDirectionIn   = "in"
	RuleDirectionOut  = "out"
	RuleDirectionBoth = "both"
)

// SecurityRule 单条安全规则
type SecurityRule struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`      // port / ip / country / isp
	Action     string   `json:"action"`    // allow / block
	Direction  string   `json:"direction"` // in / out / both
	Proto      string   `json:"proto"`     // port 类型: tcp / udp / tcpudp
	Port       string   `json:"port"`      // port 类型原始输入
	Ports      []string `json:"ports"`     // port 类型解析后的端口项（单端口或 a-b）
	SourceIP   string   `json:"source_ip"` // port 类型来源，空=所有 IP，可填 IP/CIDR/范围
	Content    []string `json:"content"`   // ip/country/isp 类型目标列表
	Remark     string   `json:"remark"`
	AutoSource string   `json:"auto_source,omitempty"` // 自动放行来源标识（空=手动），用于卸载/变更时精准回收
	CreatedAt  int64    `json:"created_at"`
}

// SecurityRulesResponse 列表响应
type SecurityRulesResponse struct {
	Rules []SecurityRule `json:"rules"`
}

// AddSecurityRuleReq 新增规则请求
type AddSecurityRuleReq struct {
	Type       string `json:"type" binding:"required,oneof=port ip country isp"`
	Port       string `json:"port"`      // port 类型
	Proto      string `json:"proto"`     // port 类型: tcp/udp/tcpudp
	SourceIP   string `json:"source_ip"` // port 类型来源
	Content    string `json:"content"`   // ip/country/isp 类型多行内容
	Action     string `json:"action" binding:"required,oneof=allow block"`
	Direction  string `json:"direction" binding:"omitempty,oneof=in out both"`
	Remark     string `json:"remark"`
	AutoSource string `json:"auto_source,omitempty"` // 自动放行来源标识（空=手动）
}

var (
	securityRules     []SecurityRule
	securityRulesMu   sync.RWMutex
	securityRulesFile string
)

// systemBaseRemark 系统默认基础端口规则备注（22/80/443/面板端口，用于展示与保护）
const systemBaseRemark = "system-base"

// InitSecurityRules 初始化规则文件路径并加载；写入系统默认基础端口规则（确保一定存在）
func InitSecurityRules() {
	securityRulesFile = filepath.Join(config.Get().DataDir, "security_rules.json")
	_ = LoadSecurityRules()

	// 始终确保 system-base 基础端口规则存在（22/80/443/面板端口）。
	// 修复：之前只在文件为空时初始化，导致 system-base 规则被用户误删/逻辑清理掉后无法自动恢复。
	ensureSystemBaseRules()

	// 历史数据迁移：所有"放行端口"且备注为空的规则，按端口自动填用途描述（22=SSH、3389=RDP 等）。
	// 一次性迁移，运行后无副作用；启动时填好用户在前端立即能看到，不用逐个手动改。
	backfillCommonPortRemarks()
}

// backfillCommonPortRemarks 一次性迁移：把所有"放行端口"规则的备注按端口默认用途填上。
// 包括空备注、以及旧数据里的 "system-base"/"app-auto" 占位符（这些是内部标签，UI 显示意义不大），
// 用户已编辑过的中文备注（如 "内部服务"）保留不动。
func backfillCommonPortRemarks() {
	securityRulesMu.Lock()
	defer securityRulesMu.Unlock()
	changed := false
	for i, r := range securityRules {
		if r.Type != RuleTypePort || r.Action != RuleActionAllow {
			continue
		}
		// 跳过用户自定义的备注：非空且不是占位符（system-base / app-auto）就保留
		cur := strings.TrimSpace(r.Remark)
		if cur != "" && cur != systemBaseRemark && cur != appAutoRemark {
			continue
		}
		if v, ok := commonPortRemarks[r.Port]; ok {
			securityRules[i].Remark = v
			changed = true
		}
	}
	if changed {
		_ = SaveSecurityRules(securityRules)
	}
}

// dedupeSystemAppPortRules 清理「同端口 + 同协议」的重复放行规则（仅限 system-base / app-auto
// 自动产生的，不动用户手动加的规则）。只保留最早一条，删除后续重复。
// 同端口不同协议（tcp vs udp）视为不同，不去重。
// 注意：调用方必须已持有 securityRulesMu。
// 返回是否发生了删除（用于外层决定是否触发持久化）。
func dedupeSystemAppPortRules() bool {
	type key struct {
		port  string
		proto string
	}
	seen := map[key]bool{}
	// 从后往前遍历，用 idx 收集要删除的下标，最后一次性从切片移除
	var toDelete []int
	for i, r := range securityRules {
		if r.Type != RuleTypePort || r.Action != RuleActionAllow {
			continue
		}
		if r.Remark != systemBaseRemark && r.Remark != appAutoRemark {
			continue
		}
		k := key{port: r.Port, proto: r.Proto}
		if seen[k] {
			toDelete = append(toDelete, i)
		} else {
			seen[k] = true
		}
	}
	// 从后往前删除（避免下标错乱）
	for i := len(toDelete) - 1; i >= 0; i-- {
		idx := toDelete[i]
		securityRules = append(securityRules[:idx], securityRules[idx+1:]...)
	}
	return len(toDelete) > 0
}

// dedupeSystemAppPortRulesLocked 已是 _Locked 别名（函数本身不加锁，调用方必须已持有 securityRulesMu）
// 保留此别名是为了语义清晰：调用方一看就知道必须在持锁区调用。
func dedupeSystemAppPortRulesLocked() bool {
	return dedupeSystemAppPortRules()
}

// ensureSystemBaseRules 确保系统默认基础端口规则（22/80/443/面板端口）都存在，标记 system-base。
// 这些端口始终放行（与 applyRulesNftables 的 baseOpenPorts 一一致），并在前端端口列表展示。
// 启动时调用：缺失的端口会被补齐，已存在的不会重复。
func ensureSystemBaseRules() {
	securityRulesMu.Lock()
	defer securityRulesMu.Unlock()

	// 启动时清理历史遗留的「同端口 + 同协议」重复放行规则
	changed := dedupeSystemAppPortRules()

	// 已有的「同端口放行规则」集合（不限 Remark 标记）：跨 system-base / app-auto 去重，
	// 同端口只允许 1 条放行规则，避免基础端口和应用自动放行产生重复
	existing := map[string]bool{}
	for _, r := range securityRules {
		if r.Type == RuleTypePort && r.Action == RuleActionAllow {
			existing[r.Port] = true
		}
	}

	ports := baseOpenPorts()
	now := time.Now().Unix()
	for _, p := range ports {
		key := strconv.Itoa(p)
		if existing[key] {
			continue
		}
		// 备注优先用端口默认用途（如 "SSH"、"HTTP"），查不到才用 systemBaseRemark
		remark := commonPortRemarks[key]
		if remark == "" {
			remark = systemBaseRemark
		}
		securityRules = append(securityRules, SecurityRule{
			ID:        generateSecurityRuleID(),
			Type:      RuleTypePort,
			Action:    RuleActionAllow,
			Direction: RuleDirectionIn,
			Proto:     "tcp",
			Port:      key,
			Ports:     []string{key},
			Remark:    remark,
			CreatedAt: now,
		})
		changed = true
	}
	if changed {
		_ = SaveSecurityRules(securityRules)
	}
}

// securityRulesPath 返回规则文件路径
func securityRulesPath() string {
	if securityRulesFile == "" {
		return filepath.Join(config.Get().DataDir, "security_rules.json")
	}
	return securityRulesFile
}

// LoadSecurityRules 从文件加载规则
func LoadSecurityRules() error {
	path := securityRulesPath()
	securityRulesMu.Lock()
	defer securityRulesMu.Unlock()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			securityRules = []SecurityRule{}
			return nil
		}
		return err
	}
	var rules []SecurityRule
	if err := json.Unmarshal(data, &rules); err != nil {
		securityRules = []SecurityRule{}
		return err
	}
	securityRules = rules
	return nil
}

// SaveSecurityRules 保存规则到文件
func SaveSecurityRules(rules []SecurityRule) error {
	path := securityRulesPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// ListSecurityRules 返回全部规则
func ListSecurityRules() []SecurityRule {
	securityRulesMu.RLock()
	defer securityRulesMu.RUnlock()
	out := make([]SecurityRule, len(securityRules))
	copy(out, securityRules)
	// 保证切片字段不为 null（Go nil slice → JSON null，前端遍历会崩溃）
	for i := range out {
		if out[i].Content == nil {
			out[i].Content = []string{}
		}
		if out[i].Ports == nil {
			out[i].Ports = []string{}
		}
	}
	return out
}

// GetSecurityRules 别名，供中间件使用
func GetSecurityRules() []SecurityRule {
	return ListSecurityRules()
}

// ListSecurityRulesByType 按类型返回规则，geo 表示 country+isp
func ListSecurityRulesByType(typ string) []SecurityRule {
	rules := ListSecurityRules()
	if typ == "" || typ == "all" {
		return rules
	}
	var out []SecurityRule
	for _, r := range rules {
		if typ == "geo" && (r.Type == RuleTypeCountry || r.Type == RuleTypeISP) {
			out = append(out, r)
			continue
		}
		if r.Type == typ {
			out = append(out, r)
		}
	}
	return out
}

// AddSecurityRule 新增规则
func AddSecurityRule(req AddSecurityRuleReq) (SecurityRule, error) {
	dir := req.Direction
	if dir == "" {
		dir = RuleDirectionIn
	}
	// 放行端口且未指定备注时，自动按端口用途填充（22=SSH、3389=RDP 等）
	if req.Type == RuleTypePort && req.Action == RuleActionAllow && strings.TrimSpace(req.Remark) == "" {
		if v, ok := commonPortRemarks[strings.TrimSpace(req.Port)]; ok {
			req.Remark = v
		}
	}

	var rule SecurityRule

	switch req.Type {
	case RuleTypePort:
		ports, err := parsePortLine(req.Port)
		if err != nil {
			return SecurityRule{}, err
		}
		proto := req.Proto
		if proto == "" {
			proto = "tcp"
		}
		if proto == "tcp/udp" {
			proto = "tcpudp"
		}
		switch proto {
		case "tcp", "udp", "tcpudp":
		default:
			return SecurityRule{}, errors.New("协议必须是 tcp/udp/tcpudp")
		}
		src := strings.TrimSpace(req.SourceIP)
		if src != "" {
			if _, err := parseIPLine(src); err != nil {
				return SecurityRule{}, fmt.Errorf("来源 IP 解析失败: %v", err)
			}
		}
		rule = SecurityRule{
			ID:         generateSecurityRuleID(),
			Type:       RuleTypePort,
			Action:     req.Action,
			Direction:  dir,
			Proto:      proto,
			Port:       strings.TrimSpace(req.Port),
			Ports:      ports,
			SourceIP:   src,
			Remark:     strings.TrimSpace(req.Remark),
			AutoSource: req.AutoSource,
			CreatedAt:  time.Now().Unix(),
		}
	default: // ip / country / isp
		lines := splitLines(req.Content)
		if len(lines) == 0 {
			return SecurityRule{}, errors.New("内容不能为空")
		}
		var parsed []string
		for _, line := range lines {
			if req.Type == RuleTypeIP {
				items, err := parseIPLine(line)
				if err != nil {
					return SecurityRule{}, fmt.Errorf("解析失败 '%s': %v", line, err)
				}
				parsed = append(parsed, items...)
			} else {
				parsed = append(parsed, line)
			}
		}
		if len(parsed) == 0 {
			return SecurityRule{}, errors.New("没有可添加的有效内容")
		}
		rule = SecurityRule{
			ID:         generateSecurityRuleID(),
			Type:       req.Type,
			Action:     req.Action,
			Direction:  dir,
			Content:    uniqueStrings(parsed),
			Remark:     strings.TrimSpace(req.Remark),
			AutoSource: req.AutoSource,
			CreatedAt:  time.Now().Unix(),
		}
	}

	securityRulesMu.Lock()
	// 写入前先 dedupe 一次，防止历史数据残留的同端口重复
	dedupeSystemAppPortRulesLocked()
	securityRules = append(securityRules, rule)
	if err := SaveSecurityRules(securityRules); err != nil {
		securityRules = securityRules[:len(securityRules)-1]
		securityRulesMu.Unlock()
		return SecurityRule{}, err
	}
	securityRulesMu.Unlock()

	_ = ApplySecurityRulesWithTimeout(5 * time.Second)
	return rule, nil
}

// UpdateSecurityRule 修改规则（按 id 找到现有规则，整体替换为新参数）。
// 不允许修改 system-base 规则的核心字段（端口/协议/方向/动作），避免破坏基础放行。
// 备注字段（Remark）独立更新即可，无须走整体替换。
func UpdateSecurityRule(id string, req AddSecurityRuleReq) (SecurityRule, error) {
	if id == "" {
		return SecurityRule{}, errors.New("规则 id 不能为空")
	}
	securityRulesMu.Lock()
	defer securityRulesMu.Unlock()
	idx := -1
	for i, r := range securityRules {
		if r.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return SecurityRule{}, errors.New("规则不存在")
	}
	old := securityRules[idx]
	// 允许编辑所有端口规则（包括 system-base 基础端口）—— 用户可以修改 80、443 等的备注/备注以外的字段，
	// 启动时 ensureSystemBaseRules 会确保基础端口存在，所以即使被改名/改了协议，
	// 下次启动会按 baseOpenPorts 重新建回来。
	_ = systemBaseRemark // 保留常量引用（导出给其他模块用）
	dir := req.Direction
	if dir == "" {
		dir = old.Direction
		if dir == "" {
			dir = RuleDirectionIn
		}
	}
	action := req.Action
	if action == "" {
		action = old.Action
	}

	var newRule SecurityRule
	switch req.Type {
	case RuleTypePort:
		ports, err := parsePortLine(req.Port)
		if err != nil {
			return SecurityRule{}, err
		}
		proto := req.Proto
		if proto == "" {
			proto = "tcp"
		}
		if proto == "tcp/udp" {
			proto = "tcpudp"
		}
		switch proto {
		case "tcp", "udp", "tcpudp":
		default:
			return SecurityRule{}, errors.New("协议必须是 tcp/udp/tcpudp")
		}
		src := strings.TrimSpace(req.SourceIP)
		if src != "" {
			if _, err := parseIPLine(src); err != nil {
				return SecurityRule{}, fmt.Errorf("来源 IP 解析失败: %v", err)
			}
		}
		newRule = SecurityRule{
			ID:         old.ID,
			Type:       RuleTypePort,
			Action:     action,
			Direction:  dir,
			Proto:      proto,
			Port:       strings.TrimSpace(req.Port),
			Ports:      ports,
			SourceIP:   src,
			Remark:     old.Remark, // Remark 不通过整体更新修改
			AutoSource: old.AutoSource,
			CreatedAt:  old.CreatedAt,
		}
	default: // ip / country / isp
		lines := splitLines(req.Content)
		if len(lines) == 0 {
			return SecurityRule{}, errors.New("内容不能为空")
		}
		var parsed []string
		for _, line := range lines {
			if req.Type == RuleTypeIP {
				items, err := parseIPLine(line)
				if err != nil {
					return SecurityRule{}, fmt.Errorf("解析失败 '%s': %v", line, err)
				}
				parsed = append(parsed, items...)
			} else {
				parsed = append(parsed, line)
			}
		}
		if len(parsed) == 0 {
			return SecurityRule{}, errors.New("没有可添加的有效内容")
		}
		newRule = SecurityRule{
			ID:         old.ID,
			Type:       req.Type,
			Action:     action,
			Direction:  dir,
			Content:    uniqueStrings(parsed),
			Remark:     old.Remark,
			AutoSource: old.AutoSource,
			CreatedAt:  old.CreatedAt,
		}
	}

	// 写入前先 dedupe 一次（已持锁），防止历史重复残留
	dedupeSystemAppPortRulesLocked()
	// 写入并下发
	securityRules[idx] = newRule
	if err := SaveSecurityRules(securityRules); err != nil {
		securityRules[idx] = old
		return SecurityRule{}, err
	}
	_ = ApplySecurityRulesWithTimeout(5 * time.Second)
	return newRule, nil
}

// UpdateSecurityRuleRemark 修改规则备注
func UpdateSecurityRuleRemark(id, remark string) error {
	securityRulesMu.Lock()
	defer securityRulesMu.Unlock()
	var r SecurityRule
	if err := findRule(id, &r); err != nil {
		return err
	}
	r.Remark = strings.TrimSpace(remark)
	// 备注只是 UI 展示用，不影响防火墙规则本身，无需重新下发（apply 5 秒超时太慢）
	return SaveSecurityRules(securityRules)
}

func findRule(id string, r *SecurityRule) error {
	idx := -1
	for i, x := range securityRules {
		if x.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return errors.New("规则不存在")
	}
	*r = securityRules[idx]
	return nil
}

// DeleteSecurityRule 删除规则
func DeleteSecurityRule(id string) error {
	securityRulesMu.Lock()
	idx := -1
	for i, r := range securityRules {
		if r.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		securityRulesMu.Unlock()
		return errors.New("规则不存在")
	}
	// 系统默认基础端口规则（22/80/443/面板端口）不允许删除，防止用户误删导致无法访问面板
	if securityRules[idx].Remark == systemBaseRemark {
		securityRulesMu.Unlock()
		return errors.New("系统默认端口规则不可删除")
	}
	old := securityRules[idx]
	securityRules = append(securityRules[:idx], securityRules[idx+1:]...)
	if err := SaveSecurityRules(securityRules); err != nil {
		securityRules = append(securityRules[:idx], append([]SecurityRule{old}, securityRules[idx:]...)...)
		securityRulesMu.Unlock()
		return err
	}
	// 关键：先释放写锁，再下发防火墙（ApplySecurityRules 内部会读 ListSecurityRules 拿读锁，
	// 若持有写锁调用会死锁，导致后续所有请求阻塞）
	securityRulesMu.Unlock()

	_ = ApplySecurityRulesWithTimeout(5 * time.Second)
	return nil
}

// ApplySecurityRulesWithTimeout 将规则下发到防火墙，外层带超时保护。
// 默认 30 秒：超过时间即使未完成也返回 nil（规则已保存到文件，下次重启会重试）。
// 防止 nft/iptables 命令在某些异常情况下卡死，导致删除/添加规则接口 hang 住。
func ApplySecurityRulesWithTimeout(timeout time.Duration) error {
	done := make(chan error, 1)
	go func() {
		done <- ApplySecurityRules()
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		slog.Warn("ApplySecurityRules 超时，规则文件已保存，下次启动重试下发", "timeout", timeout)
		return nil
	}
}

// ApplySecurityRules 将 port/ip 规则下发到防火墙
func ApplySecurityRules() error {
	rules := ListSecurityRules()
	// system-base 基础端口规则仅用于前端展示，实际放行由 apply 函数里的 baseOpenPorts 硬编码保证（防误删锁死）
	filtered := make([]SecurityRule, 0, len(rules))
	for _, r := range rules {
		if r.Remark == systemBaseRemark {
			continue
		}
		filtered = append(filtered, r)
	}
	backend := chooseSecurityBackend()
	switch backend {
	case "firewalld":
		return applyRulesFirewalld(filtered)
	case "ufw":
		return applyRulesUfw(filtered)
	case "nftables":
		return applyRulesNftables(filtered)
	case "iptables":
		return applyRulesIptables(filtered)
	default:
		// 无防火墙时仅返回 nil，规则仍由面板中间件保护
		return nil
	}
}

// applyRulesFirewalld 使用 firewalld 应用端口与 IP 规则（CentOS/Rocky）。
// firewalld 本身即白名单（未放行即拒绝），基础端口放行即可实现默认拒绝。
func applyRulesFirewalld(rules []SecurityRule) error {
	// 基础端口放行（22/80/443/面板端口），IPv4+IPv6 由 firewalld 自动覆盖
	for _, p := range baseOpenPorts() {
		_, _ = ExecCommand(fmt.Sprintf("firewall-cmd --permanent --add-port=%d/tcp", p), securityCmdTimeout)
	}
	// 规则放行/拉黑
	allowIn, blockIn, _, _ := classifyRules(rules)
	for _, r := range allowIn {
		if r.dport != "" {
			_, _ = ExecCommand(fmt.Sprintf("firewall-cmd --permanent --add-port=%s/%s", r.dport, r.proto), securityCmdTimeout)
		} else if r.cidr != "" {
			_, _ = ExecCommand(fmt.Sprintf("firewall-cmd --permanent --add-source=%s", r.cidr), securityCmdTimeout)
		}
	}
	for _, r := range blockIn {
		if r.cidr != "" {
			_, _ = ExecCommand(fmt.Sprintf("firewall-cmd --permanent --zone=drop --add-source=%s", r.cidr), securityCmdTimeout)
		}
	}
	_, _ = ExecCommand("firewall-cmd --reload", securityCmdTimeout)
	return nil
}

// applyRulesUfw 使用 ufw 应用端口与 IP 规则（Ubuntu）。
// ufw 本身即白名单，基础端口放行即可实现默认拒绝。
func applyRulesUfw(rules []SecurityRule) error {
	// 基础端口放行
	for _, p := range baseOpenPorts() {
		_, _ = ExecCommand(fmt.Sprintf("ufw allow %d/tcp", p), securityCmdTimeout)
	}
	allowIn, blockIn, _, _ := classifyRules(rules)
	for _, r := range allowIn {
		if r.dport != "" {
			_, _ = ExecCommand(fmt.Sprintf("ufw allow %s/%s", r.dport, r.proto), securityCmdTimeout)
		} else if r.cidr != "" {
			_, _ = ExecCommand(fmt.Sprintf("ufw allow from %s", r.cidr), securityCmdTimeout)
		}
	}
	for _, r := range blockIn {
		if r.cidr != "" {
			_, _ = ExecCommand(fmt.Sprintf("ufw deny from %s", r.cidr), securityCmdTimeout)
		}
	}
	return nil
}

// chooseSecurityBackend 选择防火墙后端，按发行版默认优先级：firewalld → ufw → nftables → iptables
func chooseSecurityBackend() string {
	// firewalld（CentOS/Rocky）：firewall-cmd 存在且 firewalld 正在运行
	if cmdExists("firewall-cmd") {
		if out, err := exec.Command("firewall-cmd", "--state").Output(); err == nil && strings.Contains(strings.ToLower(string(out)), "running") {
			return "firewalld"
		}
	}
	// ufw（Ubuntu）：ufw 存在且 active
	if cmdExists("ufw") {
		if out, err := exec.Command("ufw", "status").Output(); err == nil && strings.Contains(strings.ToLower(string(out)), "active") {
			return "ufw"
		}
	}
	// nftables（Debian/裸 nft）
	if cmdExists("nft") {
		return "nftables"
	}
	// iptables（老系统）
	if cmdExists("iptables") {
		return "iptables"
	}
	return ""
}

// ---------- 解析工具 ----------

// splitLines 多行文本拆成非空行
func splitLines(s string) []string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	var out []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// parseIPLine 解析单行 IP/CIDR/范围
func parseIPLine(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	// 范围格式：1.2.3.4-1.2.3.8 或 1.2.3.xx-1.2.3.yy（xx 仅最后一段）
	if idx := strings.Index(s, "-"); idx > 0 {
		startStr := strings.TrimSpace(s[:idx])
		endStr := strings.TrimSpace(s[idx+1:])
		start, end, err := expandWildcardRange(startStr, endStr)
		if err != nil {
			return nil, err
		}
		if start > end {
			return nil, errors.New("起始 IP 不能大于结束 IP")
		}
		return rangeToCIDRs(start, end), nil
	}
	// CIDR
	if strings.Contains(s, "/") {
		_, ipNet, err := net.ParseCIDR(s)
		if err != nil {
			return nil, err
		}
		return []string{ipNet.String()}, nil
	}
	// 单个 IP
	ip := net.ParseIP(s)
	if ip == nil {
		return nil, errors.New("不是有效的 IP、网段或范围")
	}
	if ip4 := ip.To4(); ip4 != nil {
		return []string{fmt.Sprintf("%s/32", ip4.String())}, nil
	}
	return []string{fmt.Sprintf("%s/128", ip.String())}, nil
}

// expandWildcardRange 解析含 xx 通配的 IP 范围
func expandWildcardRange(startStr, endStr string) (uint32, uint32, error) {
	start, err := parseMaybeWildcardIP(startStr)
	if err != nil {
		return 0, 0, err
	}
	end, err := parseMaybeWildcardIP(endStr)
	if err != nil {
		return 0, 0, err
	}
	return start, end, nil
}

// parseMaybeWildcardIP 解析可能含 xx 的 IPv4 字符串
func parseMaybeWildcardIP(s string) (uint32, error) {
	parts := strings.Split(strings.TrimSpace(s), ".")
	if len(parts) != 4 {
		return 0, errors.New("IP 格式错误")
	}
	var nums [4]int
	for i, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "x" || p == "xx" || p == "*" {
			if i != 3 {
				return 0, errors.New("通配符仅支持最后一段")
			}
			nums[i] = -1
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 {
			return 0, errors.New("IP 段无效")
		}
		nums[i] = n
	}
	if nums[3] < 0 {
		return 0, errors.New("通配符段缺失结束值")
	}
	return uint32(nums[0])<<24 | uint32(nums[1])<<16 | uint32(nums[2])<<8 | uint32(nums[3]), nil
}

// rangeToCIDRs 把一段 IPv4 范围转成 CIDR 列表
func rangeToCIDRs(start, end uint32) []string {
	var cidrs []string
	for start <= end {
		size := uint32(1) << bits.TrailingZeros32(start)
		for size > end-start+1 {
			size >>= 1
		}
		mask := 32 - bits.TrailingZeros32(size)
		cidrs = append(cidrs, fmt.Sprintf("%s/%d", uint32ToIPv4(start), mask))
		start += size
	}
	return cidrs
}

// uint32ToIPv4 32 位整数转 IPv4 字符串
func uint32ToIPv4(v uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d", byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// parsePortLine 解析端口输入：80 / 80,443 / 8000-8100 / 80,8000-8100
func parsePortLine(s string) ([]string, error) {
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			ps := strings.SplitN(part, "-", 2)
			start, err1 := strconv.Atoi(strings.TrimSpace(ps[0]))
			end, err2 := strconv.Atoi(strings.TrimSpace(ps[1]))
			if err1 != nil || err2 != nil || start < 1 || start > 65535 || end < 1 || end > 65535 || start > end {
				return nil, fmt.Errorf("无效端口范围: %s", part)
			}
			out = append(out, fmt.Sprintf("%d-%d", start, end))
		} else {
			n, err := strconv.Atoi(part)
			if err != nil || n < 1 || n > 65535 {
				return nil, fmt.Errorf("无效端口: %s", part)
			}
			out = append(out, strconv.Itoa(n))
		}
	}
	if len(out) == 0 {
		return nil, errors.New("端口不能为空")
	}
	return uniqueStrings(out), nil
}

// uniqueStrings 去重并保持顺序
func uniqueStrings(ss []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, s := range ss {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// generateSecurityRuleID 生成短 ID
// ruleIDCounter 原子计数器，用于在同一纳秒内生成多个 ID 时保证唯一
var ruleIDCounter uint64

func generateSecurityRuleID() string {
	// 纳秒时间戳 + 原子递增后缀，保证同一时刻批量生成（如基础端口规则）ID 不重复
	n := time.Now().UnixNano()
	c := atomic.AddUint64(&ruleIDCounter, 1)
	return strconv.FormatInt(n, 10) + "-" + strconv.FormatUint(c, 10)
}

// ---------- 防火墙应用 ----------

// firewallDefaultDrop 是否启用默认拒绝：只放行基础端口 + 规则显式放行的内容
func firewallDefaultDrop() bool {
	return bool(config.Get().Server.FirewallDefaultDrop)
}

// baseOpenPorts 默认开放的基础端口：22(SSH)/80(HTTP)/443(HTTPS)/面板端口
func baseOpenPorts() []int {
	ports := []int{22, 80, 443}
	p := config.Get().Server.Port
	if p > 0 && p < 65536 {
		ports = append(ports, p)
	}
	return ports
}

// cmdRuleOK 检查防火墙命令（nft/iptables）是否执行成功（ExitCode == 0）
func cmdRuleOK(cmd string) bool {
	res, err := ExecCommand(cmd, securityCmdTimeout)
	return err == nil && res != nil && res.ExitCode == 0
}

// nftPortSet 生成 nft 端口集合，如 "22, 80, 443, 9999"
func nftPortSet(ports []int) string {
	ps := make([]string, len(ports))
	for i, p := range ports {
		ps[i] = strconv.Itoa(p)
	}
	return strings.Join(ps, ", ")
}

// nftTable 统一的 nftables 表名（inet 双栈，IPv4+IPv6 一条规则同时生效）
const nftTable = "kypanel"

// applyRulesNftables 使用 nftables（inet 双栈）应用端口与 IP 规则
// 默认拒绝模式下 sec-in 链 policy=drop，仅放行回环、已建立连接与基础端口（IPv4+IPv6）。
// 注意：inet 家族下 `tcp dport X` 同时匹配 IPv4/IPv6，无需 `ip protocol`/`ip6 nexthdr` 前缀
// （nft v0.9.x 及 v1.x 的 `inet` 家族统一语法，避免 v1.1.3 报 "No symbol type information"）。
func applyRulesNftables(rules []SecurityRule) error {
	drop := firewallDefaultDrop()
	// 重建表清空旧规则（兼容旧版 ip / ip6 单栈表，以及历史 inet filter kypanel 三段表名）
	_, _ = ExecCommand("nft delete table ip kypanel 2>/dev/null || true", securityCmdTimeout)
	_, _ = ExecCommand("nft delete table ip6 kypanel 2>/dev/null || true", securityCmdTimeout)
	_, _ = ExecCommand("nft delete table inet kypanel 2>/dev/null || true", securityCmdTimeout)
	if _, err := ExecCommand("nft add table inet kypanel", securityCmdTimeout); err != nil {
		return err
	}
	// 先以 policy accept 建链，基础放行规则全部成功后，再切为 drop，避免锁死 SSH
	if _, err := ExecCommand(`nft add chain inet kypanel sec-in { type filter hook input priority 0 \; policy accept \; }`, securityCmdTimeout); err != nil {
		return err
	}
	_, _ = ExecCommand(`nft add chain inet kypanel sec-out { type filter hook output priority 0 \; policy accept \; }`, securityCmdTimeout)

	if drop {
		baseOK := cmdRuleOK("nft add rule inet kypanel sec-in iifname lo accept")
		if baseOK {
			baseOK = cmdRuleOK("nft add rule inet kypanel sec-in ct state established,related accept")
		}
		if baseOK {
			// inet 家族：tcp dport {...} 一条规则同时覆盖 IPv4+IPv6
			baseOK = cmdRuleOK(fmt.Sprintf("nft add rule inet kypanel sec-in tcp dport {%s} accept", nftPortSet(baseOpenPorts())))
		}
		if baseOK {
			// 基础放行已就绪，才将入站链默认策略切换为 drop
			_, _ = ExecCommand(`nft add chain inet kypanel sec-in { policy drop \; }`, securityCmdTimeout)
		}
	}

	allowIn, blockIn, allowOut, blockOut := classifyRules(rules)

	// 入站 allow
	for _, r := range allowIn {
		addNftRule("sec-in", r, "accept", "saddr")
	}
	// 入站 block
	for _, r := range blockIn {
		addNftRule("sec-in", r, "drop", "saddr")
	}
	// 出站 allow
	for _, r := range allowOut {
		addNftRule("sec-out", r, "accept", "daddr")
	}
	// 出站 block
	for _, r := range blockOut {
		addNftRule("sec-out", r, "drop", "daddr")
	}
	return nil
}

// firewallRule 一条可直接下发的防火墙条目
type firewallRule struct {
	src   string // 可选来源 IP/CIDR
	proto string // tcp/udp
	dport string // 端口项（单端口或 a-b），空表示 IP 规则
	cidr  string // IP 规则的目标网段
	out   bool   // 出站
}

// classifyRules 将规则拆成防火墙条目（按方向/动作）
func classifyRules(rules []SecurityRule) (allowIn, blockIn, allowOut, blockOut []firewallRule) {
	for _, r := range rules {
		if r.Type == RuleTypePort {
			protos := []string{r.Proto}
			if r.Proto == "tcpudp" {
				protos = []string{"tcp", "udp"}
			}
			for _, p := range protos {
				for _, port := range r.Ports {
					for _, src := range portRuleSources(r.SourceIP) {
						fr := firewallRule{src: src, proto: p, dport: port}
						if r.Direction == RuleDirectionOut || r.Direction == RuleDirectionBoth {
							fr.out = true
							if r.Action == RuleActionAllow {
								allowOut = append(allowOut, fr)
							} else {
								blockOut = append(blockOut, fr)
							}
						}
						if r.Direction == RuleDirectionIn || r.Direction == RuleDirectionBoth {
							fr.out = false
							if r.Action == RuleActionAllow {
								allowIn = append(allowIn, fr)
							} else {
								blockIn = append(blockIn, fr)
							}
						}
					}
				}
			}
			continue
		}
		if r.Type == RuleTypeIP {
			for _, cidr := range r.Content {
				fr := firewallRule{cidr: cidr}
				if r.Direction == RuleDirectionOut || r.Direction == RuleDirectionBoth {
					fr.out = true
					if r.Action == RuleActionAllow {
						allowOut = append(allowOut, fr)
					} else {
						blockOut = append(blockOut, fr)
					}
				}
				if r.Direction == RuleDirectionIn || r.Direction == RuleDirectionBoth {
					fr.out = false
					if r.Action == RuleActionAllow {
						allowIn = append(allowIn, fr)
					} else {
						blockIn = append(blockIn, fr)
					}
				}
			}
		}
	}
	return
}

// portRuleSources 来源 IP 为空返回 [""]，否则解析为 CIDR 列表
func portRuleSources(src string) []string {
	src = strings.TrimSpace(src)
	if src == "" {
		return []string{""}
	}
	items, err := parseIPLine(src)
	if err != nil {
		return []string{src}
	}
	return items
}

// addNftRule 添加 nft 规则（inet 双栈：无来源时一条规则同时覆盖 IPv4/IPv6）。
// dir 为 "saddr"（入站）或 "daddr"（出站），统一入站/出站的生成逻辑。
func addNftRule(chain string, fr firewallRule, verdict string, dir string) {
	// 带网段的规则：按网段 IP 版本生成 saddr/daddr
	if fr.cidr != "" {
		if strings.Contains(fr.cidr, ":") {
			_, _ = ExecCommand(fmt.Sprintf("nft add rule inet kypanel %s ip6 %s %s %s", chain, dir, fr.cidr, verdict), securityCmdTimeout)
		} else {
			_, _ = ExecCommand(fmt.Sprintf("nft add rule inet kypanel %s ip %s %s %s", chain, dir, fr.cidr, verdict), securityCmdTimeout)
		}
		return
	}
	proto := fr.proto
	if proto == "" {
		proto = "tcp"
	}
	if fr.src != "" {
		// 指定了来源：按来源的 IP 版本生成对应规则
		if strings.Contains(fr.src, ":") {
			_, _ = ExecCommand(fmt.Sprintf("nft add rule inet kypanel %s ip6 %s %s %s dport %s %s", chain, dir, fr.src, proto, fr.dport, verdict), securityCmdTimeout)
		} else {
			_, _ = ExecCommand(fmt.Sprintf("nft add rule inet kypanel %s ip %s %s %s dport %s %s", chain, dir, fr.src, proto, fr.dport, verdict), securityCmdTimeout)
		}
		return
	}
	// 无来源：inet 家族一条规则同时覆盖 IPv4+IPv6
	_, _ = ExecCommand(fmt.Sprintf("nft add rule inet kypanel %s %s dport %s %s", chain, proto, fr.dport, verdict), securityCmdTimeout)
}

// applyRulesIptables 使用 iptables/ip6tables 应用端口与 IP 规则（IPv4+IPv6）
// 默认拒绝模式下先放行回环/已建立连接/基础端口，全部成功后才会设置 INPUT 默认策略 DROP，
// 避免基础规则未就绪时锁死 SSH/面板。
func applyRulesIptables(rules []SecurityRule) error {
	drop := firewallDefaultDrop()
	fams := []string{"iptables"}
	if cmdExists("ip6tables") {
		fams = append(fams, "ip6tables")
	}
	chains := map[bool]string{false: "KYPANEL-SEC-IN", true: "KYPANEL-SEC-OUT"}
	// 清理旧链并移除旧跳转（v4+v6）
	for _, fam := range fams {
		for _, ch := range chains {
			_, _ = ExecCommand(fmt.Sprintf("%s -F %s 2>/dev/null; %s -X %s 2>/dev/null", fam, ch, fam, ch), securityCmdTimeout)
		}
		for _, ch := range chains {
			_, _ = ExecCommand(fmt.Sprintf("%s -N %s 2>/dev/null || true", fam, ch), securityCmdTimeout)
		}
		_, _ = ExecCommand(fmt.Sprintf("%s -D INPUT -j KYPANEL-SEC-IN 2>/dev/null || true", fam), securityCmdTimeout)
		_, _ = ExecCommand(fmt.Sprintf("%s -D OUTPUT -j KYPANEL-SEC-OUT 2>/dev/null || true", fam), securityCmdTimeout)
	}

	if drop {
		// 基础放行必须全部成功，才允许切默认策略 DROP
		ready := true
		for _, fam := range fams {
			if !iptablesRuleOK(fam, "-A KYPANEL-SEC-IN -i lo -j ACCEPT") {
				ready = false
				continue
			}
			if !iptablesRuleOK(fam, "-A KYPANEL-SEC-IN -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT") {
				ready = false
				continue
			}
			for _, p := range baseOpenPorts() {
				if !iptablesRuleOK(fam, fmt.Sprintf("-A KYPANEL-SEC-IN -p tcp --dport %d -j ACCEPT", p)) {
					ready = false
				}
			}
		}
		if ready {
			for _, fam := range fams {
				_, _ = ExecCommand(fmt.Sprintf("%s -P INPUT DROP", fam), securityCmdTimeout)
			}
		}
	}

	allowIn, blockIn, allowOut, blockOut := classifyRules(rules)
	for _, fam := range fams {
		addIptables(allowIn, false, "ACCEPT", fam)
		addIptables(blockIn, false, "DROP", fam)
		addIptables(allowOut, true, "ACCEPT", fam)
		addIptables(blockOut, true, "DROP", fam)
		_, _ = ExecCommand(fmt.Sprintf("%s -I INPUT 1 -j KYPANEL-SEC-IN 2>/dev/null || true", fam), securityCmdTimeout)
		_, _ = ExecCommand(fmt.Sprintf("%s -I OUTPUT 1 -j KYPANEL-SEC-OUT 2>/dev/null || true", fam), securityCmdTimeout)
	}
	return nil
}

// iptablesRuleOK 检查 iptables 命令是否成功（fam 为 iptables/ip6tables）
func iptablesRuleOK(fam, args string) bool {
	return cmdRuleOK(fmt.Sprintf("%s %s", fam, args))
}

// addIptables 批量添加 iptables/ip6tables 规则（按条目的 IP 版本过滤）
func addIptables(rs []firewallRule, out bool, verdict, fam string) {
	chain := "KYPANEL-SEC-IN"
	dirFlag := "-s"
	if out {
		chain = "KYPANEL-SEC-OUT"
		dirFlag = "-d"
	}
	isV6 := fam == "ip6tables"
	for _, fr := range rs {
		family := "" // "" 双栈 / v4 / v6
		if fr.cidr != "" {
			if strings.Contains(fr.cidr, ":") {
				family = "v6"
			} else {
				family = "v4"
			}
		} else if fr.src != "" {
			if strings.Contains(fr.src, ":") {
				family = "v6"
			} else {
				family = "v4"
			}
		}
		if isV6 && family == "v4" {
			continue
		}
		if !isV6 && family == "v6" {
			continue
		}
		args := fmt.Sprintf("%s -A %s", fam, chain)
		if fr.cidr != "" {
			args += fmt.Sprintf(" %s %s", dirFlag, fr.cidr)
			args += " -j " + verdict
			_, _ = ExecCommand(args, securityCmdTimeout)
			continue
		}
		proto := fr.proto
		if proto == "" {
			proto = "tcp"
		}
		if fr.src != "" {
			args += fmt.Sprintf(" %s %s", dirFlag, fr.src)
		}
		args += fmt.Sprintf(" -p %s --dport %s -j %s", proto, strings.ReplaceAll(fr.dport, "-", ":"), verdict)
		_, _ = ExecCommand(args, securityCmdTimeout)
	}
}

// ---------- 中间件匹配 ----------

// HasAllowSecurityRule 是否存在 IP/国家/运营商 allow 规则（白名单模式）
// 端口 allow 规则不触发白名单，避免误锁
func HasAllowSecurityRule() bool {
	for _, r := range ListSecurityRules() {
		if r.Action == RuleActionAllow && r.Type != RuleTypePort {
			return true
		}
	}
	return false
}

// MatchSecurityAny 判断 IP 是否命中规则（IP 规则 + 国家/运营商规则）
func MatchSecurityAny(ipStr, action string) bool {
	if MatchSecurityRule(ipStr, action) {
		return true
	}
	return MatchSecurityGeoRule(ipStr, action)
}

// MatchSecurityRule 判断某个 IP 是否命中 IP 规则
func MatchSecurityRule(ipStr, action string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, r := range ListSecurityRules() {
		if r.Type != RuleTypeIP || r.Action != action {
			continue
		}
		for _, item := range r.Content {
			if ipMatchesCIDR(ip, item) {
				return true
			}
		}
	}
	return false
}

// MatchSecurityGeoRule 根据国家/运营商匹配
func MatchSecurityGeoRule(ipStr, action string) bool {
	if !IpRegionEnabled() {
		return false
	}
	region, ok := SearchIp(ipStr)
	if !ok || region == nil {
		return false
	}
	for _, r := range ListSecurityRules() {
		if r.Action != action || (r.Type != RuleTypeCountry && r.Type != RuleTypeISP) {
			continue
		}
		for _, item := range r.Content {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if r.Type == RuleTypeCountry {
				if strings.Contains(region.Country, item) || strings.Contains(item, region.Country) {
					return true
				}
			} else {
				if strings.Contains(region.ISP, item) || strings.Contains(item, region.ISP) {
					return true
				}
			}
		}
	}
	return false
}

// ipMatchesCIDR 判断 IP 是否匹配 CIDR
func ipMatchesCIDR(ip net.IP, cidr string) bool {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	return ipNet.Contains(ip)
}
