package service

import (
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"regexp"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"kypanel/internal/model"
)

// mailboxNameRe 校验邮箱账号名（@ 前部分）：字母数字点下划线连字符，避免非法字符入地址。
var mailboxNameRe = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,64}$`)

// ListMailboxes 返回某域名下的全部邮箱账号（不含密码），按 id 倒序
func ListMailboxes(domainID uint) []model.MailboxView {
	var boxes []model.Mailbox
	model.DB.Where("domain_id = ?", domainID).Order("id desc").Find(&boxes)
	out := make([]model.MailboxView, 0, len(boxes))
	for _, b := range boxes {
		out = append(out, toMailboxView(b))
	}
	return out
}

func toMailboxView(b model.Mailbox) model.MailboxView {
	return model.MailboxView{
		ID:            b.ID,
		Domain:        b.Domain,
		Name:          b.Name,
		Address:       b.Address(),
		Enabled:       b.Enabled,
		QuotaMb:       b.QuotaMb,
		StorageUsed:   b.StorageUsed,
		ForwardTo:     b.ForwardTo,
		KeepCopy:      b.KeepCopy,
		AutoReplyOn:   b.AutoReplyOn,
		AutoReplyText: b.AutoReplyText,
		Remark:        b.Remark,
		CreatedAt:     b.CreatedAt,
	}
}

// CreateMailbox 新增一个邮箱账号
func CreateMailbox(domainID uint, name, password, remark string, quota int64) (*model.Mailbox, error) {
	// 校验域名存在
	dom, err := MailDomainByID(domainID)
	if err != nil {
		return nil, errors.New("域名不存在，请先添加域名")
	}
	name = strings.TrimSpace(name)
	// 容错：用户可能填了完整邮箱 address@domain 或带了 @，剥掉域名部分
	if at := strings.IndexByte(name, '@'); at >= 0 {
		name = name[:at]
	}
	if !mailboxNameRe.MatchString(name) {
		return nil, errors.New("邮箱名不合法（仅允许字母数字、点、下划线、连字符）")
	}
	if quota <= 0 {
		quota = dom.QuotaPerBox
		if quota <= 0 {
			quota = 1024
		}
	}
	// 唯一性：同域名下邮箱名不能重复
	var cnt int64
	model.DB.Model(&model.Mailbox{}).
		Where("domain_id = ? AND name = ?", domainID, name).Count(&cnt)
	if cnt > 0 {
		return nil, fmt.Errorf("邮箱 %s@%s 已存在", name, dom.Domain)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, errors.New("密码加密失败")
	}
	box := &model.Mailbox{
		DomainID:     domainID,
		Name:         name,
		Domain:       dom.Domain,
		PasswordHash: string(hash),
		Enabled:      true,
		QuotaMb:      quota,
		Remark:       strings.TrimSpace(remark),
	}
	if err := model.DB.Create(box).Error; err != nil {
		slog.Error("创建邮箱账号失败", "err", err)
		return nil, errors.New("创建失败")
	}
	return box, nil
}

// CreateMailboxesBatch 批量新增：lines 为多行文本，每行格式 "邮箱名 密码" 或 "邮箱名:密码"
// 返回创建成功与失败明细。
func CreateMailboxesBatch(domainID uint, lines string, quota int64) (created int, failed []string, err error) {
	trimmed := strings.TrimSpace(lines)
	if trimmed == "" {
		return 0, nil, errors.New("请填写要添加的账号（每行：邮箱名 密码）")
	}
	failed = []string{}
	for _, ln := range strings.Split(trimmed, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		// 兼容 "名 密码" 或 "名:密码"
		var name, pwd string
		if strings.Contains(ln, ":") {
			parts := strings.SplitN(ln, ":", 2)
			name, pwd = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		} else {
			fields := strings.Fields(ln)
			if len(fields) >= 2 {
				name, pwd = fields[0], fields[1]
			}
		}
		if name == "" || pwd == "" {
			failed = append(failed, ln+"（格式应为：邮箱名 密码）")
			continue
		}
		if _, err := CreateMailbox(domainID, name, pwd, "", quota); err != nil {
			failed = append(failed, name+"："+err.Error())
			continue
		}
		created++
	}
	return created, failed, nil
}

// RandomMailboxRule 随机生成邮箱的规则
type RandomMailboxRule struct {
	Prefix   string // 固定前缀，可为空
	Length   int    // 随机部分长度
	Digit    int    // 随机字符集：0=字母数字，1=纯数字，2=纯字母
	Count    int    // 生成数量
	Password string // 统一密码（空则随机生成每位相同长度? 简化为固定密码）
	Quota    int64
}

// GenerateRandomMailboxes 按规则随机生成账号并批量创建
func GenerateRandomMailboxes(domainID uint, rule RandomMailboxRule) (created int, accounts []map[string]string, failed []string, err error) {
	if rule.Count <= 0 || rule.Count > 200 {
		rule.Count = 20
	}
	if rule.Length <= 0 || rule.Length > 32 {
		rule.Length = 6
	}
	pwd := strings.TrimSpace(rule.Password)
	if pwd == "" {
		pwd = randomPassword(10)
	}
	accounts = []map[string]string{}
	failed = []string{}
	for i := 0; i < rule.Count; i++ {
		name := rule.Prefix + randomStr(rule.Length, rule.Digit)
		if _, err := CreateMailbox(domainID, name, pwd, "批量生成", rule.Quota); err != nil {
			failed = append(failed, name+"："+err.Error())
			continue
		}
		accounts = append(accounts, map[string]string{"name": name, "password": pwd})
		created++
	}
	return created, accounts, failed, nil
}

// DeleteMailbox 删除邮箱账号（连带清理邮件索引、maildir 文件与附件库）
func DeleteMailbox(id uint) error {
	var box model.Mailbox
	if err := model.DB.First(&box, id).Error; err != nil {
		return errors.New("账号不存在")
	}
	// 先清理数据（此时账号记录还在，便于定位目录）
	CleanupMailboxStorage(box)
	// 清理队列中该账号的待发邮件
	model.DB.Where("mailbox_id = ?", box.ID).Delete(&model.MailOutbox{})
	// 最后删账号
	if err := model.DB.Delete(&model.Mailbox{}, id).Error; err != nil {
		return err
	}
	return nil
}

// ToggleMailbox 启用/停用账号
func ToggleMailbox(id uint, enabled bool) error {
	return model.DB.Model(&model.Mailbox{}).Where("id = ?", id).
		Update("enabled", enabled).Error
}

// UpdateMailboxPassword 重置某账号密码
func UpdateMailboxPassword(id uint, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return model.DB.Model(&model.Mailbox{}).Where("id = ?", id).
		Update("password_hash", string(hash)).Error
}

// --- 随机工具 ---

// randomStr 按规则生成长度为 n 的随机字符串。mode: 0=字母数字 1=数字 2=字母
func randomStr(n, mode int) string {
	var chars string
	switch mode {
	case 1:
		chars = "0123456789"
	case 2:
		chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	default:
		chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

// --- 对接检测（MX 是否指向本机） ---

// MailDomainStatus 某域名的对接状态（供列表展示）
type MailDomainStatus struct {
	DomainID    uint     `json:"domain_id"`
	Ready       bool     `json:"ready"`        // 是否对接完成（MX 生效）
	MxHosts     []string `json:"mx_hosts"`     // 检测到的 MX
	LocalIps    []string `json:"local_ips"`    // 本机公网 IP
	NotReadyMsg string   `json:"not_ready_msg"` // 未就绪原因
}

// CheckMailDomainReady 检测某域名 MX 是否指向本机（对接完成判定）。
// 规则：该域名有 MX 记录，且 MX 指向的主机名解析出的任一 IP 命中本机公网 IP。
func CheckMailDomainReady(domainID uint) MailDomainStatus {
	st := MailDomainStatus{DomainID: domainID}
	dom, err := MailDomainByID(domainID)
	if err != nil {
		st.NotReadyMsg = "域名不存在"
		return st
	}
	local := detectMailServerPublicIP()
	if local == "" {
		local = detectMailServerLocalIP()
	}
	if local != "" {
		st.LocalIps = []string{local}
	}
	// 查 MX
	mxs, err := net.LookupMX(dom.Domain)
	if err != nil || len(mxs) == 0 {
		st.NotReadyMsg = "未检测到该域名的 MX 记录，无法收信，请先在 DNS 里配置（见「绑定/解析」引导）"
		return st
	}
	st.MxHosts = make([]string, 0, len(mxs))
	for _, m := range mxs {
		h := strings.TrimSuffix(m.Host, ".")
		st.MxHosts = append(st.MxHosts, h)
	}
	if local != "" {
		// 尝试解析 MX 目标主机 → 看 IP 是否含本机公网 IP
		for _, m := range mxs {
			host := strings.TrimSuffix(m.Host, ".")
			ips, err := net.LookupIP(host)
			if err != nil {
				continue
			}
			for _, ip := range ips {
				if ip.String() == local {
					st.Ready = true
					return st
				}
			}
		}
		// MX 存在但没指向本机
		st.NotReadyMsg = "检测到 MX 记录，但它没指向这台服务器（" + local + "），信仍会投到别处。请检查 A/MX 是否把 mail.域名 指向本机。"
		return st
	}
	// 拿不到本机 IP，无法确认
	st.NotReadyMsg = "无法确认本机公网 IP，暂时判定未对接"
	return st
}
