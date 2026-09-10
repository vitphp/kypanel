package service

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"kypanel/internal/model"
)

// 发信编排：本站收件人直接投递 maildir，外部收件人查 MX 直发，
// 并在发件人的「已发送」保存副本。

// SendMailRequest 发信请求
type SendMailRequest struct {
	MailboxID uint
	To        []string
	Cc        []string
	Subject   string
	TextBody  string
	HtmlBody  string
	Attach    []MailAttachment
}

// SendMailResult 发信结果
type SendMailResult struct {
	MessageID string   `json:"message_id"`
	Local     []string `json:"local"`
	Remote    []string `json:"remote"`
	Failed    []string `json:"failed"`
}

func SendMail(req SendMailRequest) (*SendMailResult, error) {
	if req.MailboxID == 0 {
		return nil, errors.New("缺少发件账号")
	}
	var box model.Mailbox
	if err := model.DB.First(&box, req.MailboxID).Error; err != nil {
		return nil, errors.New("发件账号不存在")
	}
	if !box.Enabled {
		return nil, errors.New("发件账号已停用")
	}
	rcpts := normalizeAddresses(append(append([]string{}, req.To...), req.Cc...))
	if len(rcpts) == 0 {
		return nil, errors.New("请填写收件人")
	}

	from := box.Address()
	content := MailContent{
		FromName: box.Name,
		FromAddr: from,
		ToAddrs:  rcpts,
		Subject:  req.Subject,
		TextBody: req.TextBody,
		HtmlBody: req.HtmlBody,
		Attach:   req.Attach,
	}
	raw := buildMimeMessage(content)
	messageID := extractMessageID(raw)

	result := &SendMailResult{MessageID: messageID}

	localByUser := map[string][]string{}
	var external []string
	for _, rc := range rcpts {
		domain, user := splitLocalAddress(rc)
		if domain == "" {
			result.Failed = append(result.Failed, rc+"（地址无效）")
			continue
		}
		if isLocalMailDomain(domain) {
			localByUser[domain] = append(localByUser[domain], user)
		} else {
			external = append(external, rc)
		}
	}

	for domain, users := range localByUser {
		for _, user := range users {
			if _, err := DeliverMessage(domain, user, raw); err != nil {
				result.Failed = append(result.Failed, user+"@"+domain+"（"+err.Error()+"）")
				continue
			}
			result.Local = append(result.Local, user+"@"+domain)
		}
	}

	// 外部投递：按目标域分组，逐域查 MX 直发
	if len(external) > 0 {
		byDomain := map[string][]string{}
		for _, rc := range external {
			byDomain[domainOf(rc)] = append(byDomain[domainOf(rc)], rc)
		}
		for d, addrs := range byDomain {
			if err := deliverExternalDomain(d, from, addrs, raw); err != nil {
				for _, a := range addrs {
					result.Failed = append(result.Failed, a+"（"+err.Error()+"）")
				}
				continue
			}
			result.Remote = append(result.Remote, addrs...)
		}
	}

	if err := saveSentCopy(box, rcpts, raw, messageID, len(req.Attach) > 0); err != nil {
		slog.Warn("保存已发送副本失败", "err", err)
	}

	if len(result.Local) == 0 && len(result.Remote) == 0 {
		if len(result.Failed) == 0 {
			return nil, errors.New("没有可投递的收件人")
		}
		return result, fmt.Errorf("发送失败：%s", strings.Join(result.Failed, "；"))
	}
	return result, nil
}

// deliverExternalDomain 把邮件投递到某外部域名的 MX（逐个 MX 尝试）。
func deliverExternalDomain(domain, from string, rcpts []string, raw []byte) error {
	mxs, err := net.LookupMX(domain)
	if err != nil || len(mxs) == 0 {
		return fmt.Errorf("无法解析收件方邮件服务器(MX)：%v", err)
	}
	sort.Slice(mxs, func(i, j int) bool { return mxs[i].Pref < mxs[j].Pref })
	var lastErr error
	for _, mx := range mxs {
		host := strings.TrimSuffix(mx.Host, ".")
		err := deliverExternalToHost(host, from, rcpts, raw, 15*time.Second)
		if err == nil {
			return nil
		}
		lastErr = err
		slog.Warn("外发尝试失败", "mx", host, "err", err)
	}
	if lastErr == nil {
		lastErr = errors.New("所有 MX 均不可达")
	}
	// 典型被云厂商封 25 出站的报错
	if strings.Contains(lastErr.Error(), "connect") || strings.Contains(lastErr.Error(), "timeout") || strings.Contains(lastErr.Error(), "i/o timeout") {
		return fmt.Errorf("无法连接对方邮件服务器 25 端口（可能本机 25 出站被封，请申请解封）：%v", lastErr)
	}
	return lastErr
}

// saveSentCopy 把发出去的邮件存到发件账号的 sent 目录 + 索引。
func saveSentCopy(box model.Mailbox, rcpts []string, raw []byte, messageID string, hasAttach bool) error {
	domain := box.Domain
	user := box.Name
	if err := ensureMailboxDirs(domain, user); err != nil {
		return err
	}
	fname := fmt.Sprintf("%d.%s", time.Now().UnixNano(), randHex(4))
	tmp := filepath.Join(mailboxDir(domain, user), "tmp", fname)
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	cur := filepath.Join(mailboxDir(domain, user), "cur", fname)
	if err := os.Rename(tmp, cur); err != nil {
		return err
	}
	rec := &model.MailMessage{
		MailboxID: box.ID,
		Domain:    domain,
		Mailbox:   user,
		Address:   box.Address(),
		Filename:  fname,
		FromAddr:  box.Address(),
		FromName:  box.Name,
		ToAddrs:   strings.Join(rcpts, ", "),
		Subject:   parseSubject(raw),
		Date:      time.Now().Unix(),
		Seen:      true,
		HasAttach: hasAttach,
		Folder:    "sent",
		RawSize:   int64(len(raw)),
		MessageID: messageID,
	}
	return model.DB.Create(rec).Error
}

// parseSubject 从原始报文中解析主题（用于已发送列表展示）。
func parseSubject(raw []byte) string {
	meta := parseMessageMeta(raw, &model.Mailbox{}, "")
	return meta.Subject
}

// extractMessageID 从报文中取 Message-ID 头。
func extractMessageID(raw []byte) string {
	for _, ln := range strings.Split(string(raw), "\n") {
		ln = strings.TrimRight(ln, "\r")
		if ln == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(ln), "message-id:") {
			return strings.TrimSpace(ln[len("message-id:"):])
		}
	}
	return ""
}

// isLocalMailDomain 判断域名是否为本站邮箱域名。
func isLocalMailDomain(domain string) bool {
	var cnt int64
	model.DB.Model(&model.MailDomain{}).Where("domain = ?", domain).Count(&cnt)
	return cnt > 0
}

// normalizeAddresses 去重、去空、小写域名（保留本地部分大小写）。
func normalizeAddresses(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, a := range in {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if !strings.Contains(a, "@") {
			continue
		}
		key := strings.ToLower(a)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, a)
	}
	return out
}

// domainOf 取地址的域名部分（小写）。
func domainOf(addr string) string {
	at := strings.LastIndexByte(addr, '@')
	if at < 0 {
		return ""
	}
	return strings.ToLower(addr[at+1:])
}

// ===== 草稿 =====

// SaveDraftRequest 存草稿请求
type SaveDraftRequest struct {
	MailboxID uint
	DraftID   uint // >0 表示覆盖已有草稿
	To        []string
	Subject   string
	TextBody  string
	HtmlBody  string
	Attach    []MailAttachment
}

// SaveDraft 保存草稿（内容按 MIME 存入 drafts 目录，可再次打开编辑）。
func SaveDraft(req SaveDraftRequest) (*model.MailMessage, error) {
	if req.MailboxID == 0 {
		return nil, errors.New("缺少账号")
	}
	var box model.Mailbox
	if err := model.DB.First(&box, req.MailboxID).Error; err != nil {
		return nil, errors.New("账号不存在")
	}
	content := MailContent{
		FromName: box.Name,
		FromAddr: box.Address(),
		ToAddrs:  req.To,
		Subject:  req.Subject,
		TextBody: req.TextBody,
		HtmlBody: req.HtmlBody,
		Attach:   req.Attach,
	}
	raw := buildMimeMessage(content)
	if err := ensureMailboxDirs(box.Domain, box.Name); err != nil {
		return nil, err
	}
	fname := fmt.Sprintf("%d.%s", time.Now().UnixNano(), randHex(4))
	tmp := filepath.Join(mailboxDir(box.Domain, box.Name), "tmp", fname)
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return nil, err
	}
	cur := filepath.Join(mailboxDir(box.Domain, box.Name), "cur", fname)
	if err := os.Rename(tmp, cur); err != nil {
		return nil, err
	}

	hasAttach := false
	for _, a := range req.Attach {
		if !a.Inline {
			hasAttach = true
		}
	}
	rec := &model.MailMessage{
		MailboxID: box.ID,
		Domain:    box.Domain,
		Mailbox:   box.Name,
		Address:   box.Address(),
		Filename:  fname,
		FromAddr:  box.Address(),
		FromName:  box.Name,
		ToAddrs:   strings.Join(req.To, ", "),
		Subject:   req.Subject,
		Date:      time.Now().Unix(),
		Seen:      true,
		HasAttach: hasAttach,
		Folder:    "drafts",
		RawSize:   int64(len(raw)),
	}
	// 覆盖已有草稿：删除旧的
	if req.DraftID > 0 {
		_ = deleteMailboxMessage(req.MailboxID, req.DraftID)
	}
	if err := model.DB.Create(rec).Error; err != nil {
		return nil, err
	}
	return rec, nil
}
