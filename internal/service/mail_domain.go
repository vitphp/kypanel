package service

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"regexp"
	"strings"

	"kypanel/internal/config"
	"kypanel/internal/model"
)

// mailDomainRe 校验域名格式：仅允许 字母数字 . 和 -，且必须含点（顶级域），
// 规范小写、去首尾点。示例：example.com / mail.example.org
var mailDomainRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

// ListMailDomains 返回全部邮箱域名
func ListMailDomains() []model.MailDomain {
	var list []model.MailDomain
	model.DB.Order("id desc").Find(&list)
	return list
}

// CreateMailDomain 添加一个邮箱域名（校验唯一、非法字符）
func CreateMailDomain(domain, remark string, quota int64) (*model.MailDomain, error) {
	d := strings.ToLower(strings.TrimSpace(domain))
	d = strings.Trim(d, ".")
	if !mailDomainRe.MatchString(d) {
		return nil, errors.New("域名格式无效（示例：example.com）")
	}
	if quota <= 0 {
		quota = 1024
	}
	// 唯一性
	var cnt int64
	model.DB.Model(&model.MailDomain{}).Where("domain = ?", d).Count(&cnt)
	if cnt > 0 {
		return nil, errors.New("该域名已存在")
	}
	rec := &model.MailDomain{
		Domain:    d,
		Enabled:   true,
		Remark:    strings.TrimSpace(remark),
		QuotaPerBox: quota,
	}
	if err := model.DB.Create(rec).Error; err != nil {
		slog.Error("创建邮箱域名失败", "err", err)
		return nil, errors.New("创建失败")
	}
	return rec, nil
}

// UpdateMailDomain 更新域名（启停 / 备注 / 配额 / DNS 配置状态勾选）
func UpdateMailDomain(id uint, patch map[string]interface{}) error {
	if len(patch) == 0 {
		return nil
	}
	return model.DB.Model(&model.MailDomain{}).Where("id = ?", id).Updates(patch).Error
}

// DeleteMailDomain 删除邮箱域名（P1 仅删域名记录；后续 P 会级联清理账号与邮件）
func DeleteMailDomain(id uint) error {
	res := model.DB.Delete(&model.MailDomain{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("域名不存在")
	}
	return nil
}

// MailDomainDNSServer 返回邮件服务器建议指向的主机/IP，供 DNS 引导使用。
// 优先顺序：
//  1. 面板配置 server.domain（用户显式配置的对外域名）
//  2. 自动检测本机公网 IP（不依赖外部手动填写，免去用户去找 IP 的步骤）
//  3. 回退到本机内网 IP（极端情况下仍给出可参考值，不再用占位符）
// 同时返回检测到的主机名，前端可展示"检测到你的服务器"信息，让用户不用手填。
func MailDomainDNSServer() string {
	if d := strings.TrimSpace(config.Get().Server.Domain); d != "" {
		return d
	}
	if ip := detectMailServerPublicIP(); ip != "" {
		return ip
	}
	return detectMailServerLocalIP()
}

// detectMailServerPublicIP 自动检测本机公网 IP（curl 多个 IP 解析服务，取最快可达的）。
// 复用现有成熟逻辑（与 cli/menu.go 的 detectPublicIP 同思路），不要求用户去查 IP。
func detectMailServerPublicIP() string {
	for _, u := range []string{
		"https://api.ipify.org",
		"https://ifconfig.me/ip",
		"https://ip.sb",
	} {
		cmd := exec.Command("curl", "-fsSL", "--max-time", "4", u)
		if buf, err := cmd.Output(); err == nil {
			if ip := strings.TrimSpace(string(buf)); ip != "" {
				return ip
			}
		}
	}
	return ""
}

// detectMailServerLocalIP 取本机内网 IP（公网检测失败时回退）。
func detectMailServerLocalIP() string {
	cmd := exec.Command("sh", "-c", "hostname -I 2>/dev/null | awk '{print $1}'")
	if buf, err := cmd.Output(); err == nil {
		return strings.TrimSpace(string(buf))
	}
	return ""
}

// detectMailServerHostname 取本机主机名（用于前端展示）。
func detectMailServerHostname() string {
	cmd := exec.Command("sh", "-c", "hostname 2>/dev/null")
	if buf, err := cmd.Output(); err == nil {
		return strings.TrimSpace(string(buf))
	}
	return ""
}

// BuildMailDnsGuide 生成某域名的 DNS 绑定引导信息（MX/SPF/DKIM/DMARC 该怎么解析）
func BuildMailDnsGuide(domain string) (*model.MailDnsGuide, error) {
	rec, err := getMailDomainByDomain(domain)
	if err != nil {
		return nil, err
	}
	srv := MailDomainDNSServer() // 自动检测，面板已填值，用户不需要手填
	// SPF 直接声明本服务器 IP（比 "mx ~all" 更精确：本面板是直接发信，不是 MX 中继）
	spf := "v=spf1 ip4:" + srv + " ~all"
	g := &model.MailDnsGuide{
		Domain:     rec.Domain,
		MailServer: srv,
		MxHost:     "@",
		MxValue:    fmt.Sprintf("mail.%s", rec.Domain),
		SpfValue:   spf,
		DkimHost:   "default._domainkey",
		DkimValue:  "v=DKIM1; k=rsa; p=<生成后自动填写>", // P4 签名就绪后自动替换为真实公钥
		DmarcHost:  "_dmarc",
		DmarcValue: "v=DMARC1; p=quarantine; rua=mailto:postmaster@" + rec.Domain,
	}
	g.Notes = []string{
		"1) A 记录：mail." + rec.Domain + " 解析到本服务器（主机 mail，类型 A，值 " + srv + "）",
		"2) MX 记录：主机 @，值 mail." + rec.Domain + "，优先级 10",
		"3) SPF 记录：TXT，主机 @，值 " + spf + "（声明本服务器 " + srv + " 是唯一代发方）",
		"4) DKIM 与 DMARC 可后续补（P4 签名功能上线后面板会自动填真实公钥）",
		"5) 生效通常需几分钟到数小时，可点页面上的「检测解析是否生效」自动查 MX",
	}
	return g, nil
}

// CheckMailDnsRecord 简易"自检"：尝试解析某域名的 MX 记录并看是否生效。
// 使用标准库 net 查询 MX。注意 DNS 传播有延迟，未解析到可能是还没生效（不代表配错）。
// 留到 P2 收信功能就绪后用于指导用户确认解析是否已生效。
func CheckMailDnsRecord(domain string) (mxRecords []string, err error) {
	hosts, err := net.LookupMX(domain)
	if err != nil {
		// DNS 查询失败/无 MX 记录：明确提示，避免用户误以为配好了
		return nil, fmt.Errorf("MX 记录暂未生效或不存在（DNS 可能仍在传播，请检查解析）: %w", err)
	}
	out := make([]string, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, strings.TrimSuffix(h.Host, "."))
	}
	return out, nil
}

// 内部：按域名取记录
func getMailDomainByDomain(domain string) (*model.MailDomain, error) {
	var rec model.MailDomain
	if err := model.DB.Where("domain = ?", strings.ToLower(strings.TrimSpace(domain))).First(&rec).Error; err != nil {
		return nil, errors.New("域名不存在，请先添加")
	}
	return &rec, nil
}

// MailDomainByID 按主键取域名记录
func MailDomainByID(id uint) (*model.MailDomain, error) {
	var rec model.MailDomain
	if err := model.DB.First(&rec, id).Error; err != nil {
		return nil, errors.New("域名不存在")
	}
	return &rec, nil
}

// MailDomainNameByID 按主键取域名名字符串
func MailDomainNameByID(id uint) (string, error) {
	rec, err := MailDomainByID(id)
	if err != nil {
		return "", err
	}
	return rec.Domain, nil
}

// MailDomainDnsGuideByID 按主键取域名并生成 DNS 引导
func MailDomainDnsGuideByID(id uint) (*model.MailDnsGuide, error) {
	rec, err := MailDomainByID(id)
	if err != nil {
		return nil, err
	}
	return BuildMailDnsGuide(rec.Domain)
}
