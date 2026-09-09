package service

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
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
// 优先取面板配置的 server 域名；否则返回服务器 IP 提示用户替换。
func MailDomainDNSServer() string {
	// 可尝试读取 config 中的 server.domain / 证书域名；此处给通用占位，前端会提示替换为实际 IP/域名
	if d := strings.TrimSpace(config.Get().Server.Domain); d != "" {
		return d
	}
	return "{你的服务器IP或域名}"
}

// BuildMailDnsGuide 生成某域名的 DNS 绑定引导信息（MX/SPF/DKIM/DMARC 该怎么解析）
func BuildMailDnsGuide(domain string) (*model.MailDnsGuide, error) {
	rec, err := getMailDomainByDomain(domain)
	if err != nil {
		return nil, err
	}
	srv := MailDomainDNSServer()
	g := &model.MailDnsGuide{
		Domain:     rec.Domain,
		MailServer: srv,
		MxHost:     "@", // 根记录
		MxValue:    fmt.Sprintf("mail.%s", rec.Domain), // 先让用户建一条 A 指向服务器，再建 MX 指向该 A
		SpfValue:   "v=spf1 mx ~all",
		DkimHost:   "default._domainkey",
		// DKIM 公钥需 P4 生成后填入；P1 提示占位
		DkimValue:  "v=DKIM1; k=rsa; p=<生成后自动填写>",
		DmarcHost:  "_dmarc",
		DmarcValue: "v=DMARC1; p=quarantine; rua=mailto:postmaster@" + rec.Domain,
	}
	// 中文说明
	g.Notes = []string{
		"1) 先把 A 记录：mail." + rec.Domain + " 解析到本服务器（值：主机记录填 mail，记录类型 A，目标填 " + srv + "）",
		"2) MX 记录：主机记录 @，值 mail." + rec.Domain + "，优先级 10",
		"3) SPF 记录：TXT，主机记录 @，值 " + g.SpfValue + "，用于声明只有本服务器能代发该域邮件",
		"4) DKIM 与 DMARC（做邮件防伪/进收件箱用，P4 签名功能上线后需补配）",
		"   DKIM：TXT，主机记录 " + g.DkimHost + "，" + g.DkimValue,
		"   DMARC：TXT，主机记录 _dmarc，值 " + g.DmarcValue,
		"5) 配置生效通常需几分钟到数小时，可在域名服务商处验证。",
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
