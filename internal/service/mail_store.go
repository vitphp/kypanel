package service

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	"kypanel/internal/config"
	"kypanel/internal/model"
)

// 邮件存储：maildir 布局 <DataDir>/mail/<domain>/<user>/{cur,new,tmp}
// 正文每封一个文件（写 tmp → rename 入 new），SQLite 存索引元数据。

func mailDataRoot() string {
	return filepath.Join(config.Get().DataDir, "mail")
}

// mailboxDir 返回某账号的 maildir 目录
func mailboxDir(domain, user string) string {
	return filepath.Join(mailDataRoot(), domain, user)
}

// ensureMailboxDirs 确保账号的 new/cur/tmp 三目录存在
func ensureMailboxDirs(domain, user string) error {
	for _, sub := range []string{"new", "cur", "tmp"} {
		if err := os.MkdirAll(filepath.Join(mailboxDir(domain, user), sub), 0o755); err != nil {
			return err
		}
	}
	return nil
}

// DeliverMessage 把一封原始邮件投递到某账号的 maildir 并入库索引。
// 原子落盘：先写 tmp，再 rename 到 new。domain/user 均已小写规范化。
func DeliverMessage(domain, user string, raw []byte) (*model.MailMessage, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	user = strings.ToLower(strings.TrimSpace(user))
	if domain == "" || user == "" {
		return nil, errors.New("投递地址不完整")
	}
	box, err := findMailboxByAddress(domain, user)
	if err != nil || box == nil {
		return nil, errors.New("收件账号不存在")
	}
	if !box.Enabled {
		return nil, errors.New("收件账号已停用")
	}
	if err := ensureMailboxDirs(domain, user); err != nil {
		return nil, err
	}
	// 生成唯一文件名：纳秒时间戳 + 短随机，保证并发唯一
	fname := fmt.Sprintf("%d.%s", time.Now().UnixNano(), randomPassword(6))
	tmpPath := filepath.Join(mailboxDir(domain, user), "tmp", fname)
	if err := os.WriteFile(tmpPath, raw, 0o600); err != nil {
		return nil, err
	}
	newPath := filepath.Join(mailboxDir(domain, user), "new", fname)
	if err := os.Rename(tmpPath, newPath); err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}

	meta := parseMessageMeta(raw, box, fname)
	if err := model.DB.Create(meta).Error; err != nil {
		slog.Error("邮件元数据入库失败", "err", err, "addr", meta.Address)
		// 入库失败也保留文件（下次扫描可恢复），返回成功但记录错误
		slog.Warn("邮件已落盘但索引入库失败，文件保留于 maildir", "domain", domain, "user", user, "file", fname)
		return meta, nil
	}
	return meta, nil
}

// findMailboxByAddress 按 domain + user 查账号
func findMailboxByAddress(domain, user string) (*model.Mailbox, error) {
	var box model.Mailbox
	if err := model.DB.Where("domain = ? AND name = ?", domain, user).First(&box).Error; err != nil {
		return nil, err
	}
	return &box, nil
}

// parseMessageMeta 解析原始邮件头部，填充索引元数据
func parseMessageMeta(raw []byte, box *model.Mailbox, fname string) *model.MailMessage {
	m := &model.MailMessage{
		MailboxID: box.ID,
		Domain:    box.Domain,
		Mailbox:   box.Name,
		Address:   box.Address(),
		Filename:  fname,
		Date:      time.Now().Unix(),
		Folder:    "inbox",
		RawSize:   int64(len(raw)),
		Seen:      false,
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return m // 解析失败也保留原始信，只是索引字段为空
	}
	h := msg.Header
	if s := h.Get("Subject"); s != "" {
		m.Subject = decodeHeader(s)
	}
	if f := h.Get("From"); f != "" {
		addr, name := parseAddressHeader(f)
		m.FromAddr = addr
		m.FromName = decodeHeader(name)
	}
	if mid := h.Get("Message-Id"); mid != "" {
		m.MessageID = strings.TrimSpace(mid)
	}
	if d := h.Get("Date"); d != "" {
		if t, err := mail.ParseDate(d); err == nil {
			m.Date = t.Unix()
		}
	}
	m.HasAttach = hasRealAttachment(raw, 0)
	return m
}

// hasRealAttachment 递归判断邮件是否含真实附件（Content-Disposition: attachment）。
func hasRealAttachment(raw []byte, depth int) bool {
	if depth > 6 {
		return false
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return false
	}
	ct := msg.Header.Get("Content-Type")
	ctype, params, _ := mime.ParseMediaType(ct)
	if strings.HasPrefix(ctype, "multipart/") {
		b := params["boundary"]
		if b == "" {
			return false
		}
		for _, p := range splitMultipart(raw, b) {
			if hasRealAttachment(p, depth+1) {
				return true
			}
		}
		return false
	}
	disp := strings.ToLower(msg.Header.Get("Content-Disposition"))
	return strings.Contains(disp, "attachment")
}

// parseAddressHeader 解析 From/To 头 → (邮箱, 显示名)
func parseAddressHeader(s string) (addr, name string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	if list, err := mail.ParseAddressList(s); err == nil && len(list) > 0 {
		return list[0].Address, list[0].Name
	}
	if lt := strings.IndexByte(s, '<'); lt >= 0 {
		if rt := strings.IndexByte(s[lt:], '>'); rt > 0 {
			return strings.TrimSpace(s[lt+1 : lt+rt]), strings.TrimSpace(s[:lt])
		}
	}
	return s, ""
}

// decodeHeader 解码 RFC2047 编码的 header
func decodeHeader(s string) string {
	if !strings.Contains(s, "=?") {
		return s
	}
	dec := &mime.WordDecoder{}
	out, err := dec.DecodeHeader(s)
	if err != nil {
		return s
	}
	return out
}

// listMailboxMessages 按账号列出消息（folder 默认 inbox）
func listMailboxMessages(mailboxID uint, folder string) ([]model.MailMessageView, error) {
	if folder == "" {
		folder = "inbox"
	}
	var recs []model.MailMessage
	q := model.DB.Where("mailbox_id = ? AND folder = ?", mailboxID, folder).Order("date desc").Find(&recs)
	if q.Error != nil {
		return nil, q.Error
	}
	out := make([]model.MailMessageView, 0, len(recs))
	for _, r := range recs {
		out = append(out, model.MailMessageView{
			ID: r.ID, Address: r.Address, FromAddr: r.FromAddr, FromName: r.FromName,
			ToAddrs: r.ToAddrs, Subject: r.Subject, Date: r.Date, Seen: r.Seen,
			HasAttach: r.HasAttach, Folder: r.Folder, RawSize: r.RawSize,
		})
	}
	return out, nil
}

// readMailboxMessage 读某封信：正文 + 标记已读
func readMailboxMessage(mailboxID, msgID uint, markSeen bool) (*model.MailMessageView, error) {
	var rec model.MailMessage
	if err := model.DB.Where("id = ? AND mailbox_id = ?", msgID, mailboxID).First(&rec).Error; err != nil {
		return nil, errors.New("邮件不存在")
	}
	var box model.Mailbox
	if err := model.DB.First(&box, rec.MailboxID).Error; err != nil {
		return nil, errors.New("账号不存在")
	}
	base := mailboxDir(box.Domain, box.Name)
	data, err := os.ReadFile(filepath.Join(base, "cur", rec.Filename))
	if err != nil {
		data, err = os.ReadFile(filepath.Join(base, "new", rec.Filename))
		if err != nil {
			return nil, errors.New("邮件文件缺失")
		}
	}
	textBody, htmlBody := extractBodies(data)

	if markSeen && !rec.Seen {
		model.DB.Model(&model.MailMessage{}).Where("id = ?", rec.ID).Update("seen", true)
		rec.Seen = true
		moveMaildirToCur(box.Domain, box.Name, rec.Filename)
	}

	// 附件列表
	atts := extractAttachments(data)
	attViews := make([]model.MailAttachmentView, 0, len(atts))
	for i, a := range atts {
		attViews = append(attViews, model.MailAttachmentView{
			Index: i, Filename: a.Filename, ContentType: a.ContentType, Size: int64(len(a.Data)),
		})
	}

	return &model.MailMessageView{
		ID: rec.ID, Address: rec.Address, FromAddr: rec.FromAddr, FromName: rec.FromName,
		ToAddrs: rec.ToAddrs, Subject: rec.Subject, Date: rec.Date, Seen: rec.Seen,
		HasAttach: rec.HasAttach, Folder: rec.Folder, RawSize: rec.RawSize,
		TextBody: textBody, HtmlBody: htmlBody, Attachments: attViews,
	}, nil
}

// attachmentData 附件原始数据
type attachmentData struct {
	Filename    string
	ContentType string
	Data        []byte
}

// extractAttachments 递归提取邮件中的附件（Content-Disposition: attachment）。
func extractAttachments(raw []byte) []attachmentData {
	var out []attachmentData
	collectAttachments(raw, 0, &out)
	return out
}

func collectAttachments(raw []byte, depth int, out *[]attachmentData) {
	if depth > 6 {
		return
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return
	}
	ct := msg.Header.Get("Content-Type")
	ctype, params, _ := mime.ParseMediaType(ct)
	if strings.HasPrefix(ctype, "multipart/") {
		b := params["boundary"]
		if b == "" {
			return
		}
		for _, p := range splitMultipart(raw, b) {
			collectAttachments(p, depth+1, out)
		}
		return
	}
	disp := strings.ToLower(msg.Header.Get("Content-Disposition"))
	if !strings.Contains(disp, "attachment") {
		return
	}
	data, _ := io.ReadAll(msg.Body)
	if strings.EqualFold(strings.TrimSpace(msg.Header.Get("Content-Transfer-Encoding")), "base64") {
		if dec, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(string(data), "\n", "")); err == nil {
			data = dec
		}
	}
	fname := attachmentFilename(msg.Header.Get("Content-Disposition"), msg.Header.Get("Content-Type"))
	if fname == "" {
		fname = "attachment"
	}
	*out = append(*out, attachmentData{Filename: fname, ContentType: ctype, Data: data})
}

// attachmentFilename 从 Content-Disposition / Content-Type 中取文件名（含 RFC2047 解码）。
func attachmentFilename(disp, ct string) string {
	_, dparams, _ := mime.ParseMediaType(disp)
	if fn := dparams["filename"]; fn != "" {
		return decodeHeader(fn)
	}
	_, cparams, _ := mime.ParseMediaType(ct)
	if fn := cparams["name"]; fn != "" {
		return decodeHeader(fn)
	}
	return ""
}

// GetMailAttachmentAPI 取某封邮件第 index 个附件的原始数据（供下载）。
func GetMailAttachmentAPI(mailboxID, msgID uint, index int) (filename, contentType string, data []byte, err error) {
	var rec model.MailMessage
	if err := model.DB.Where("id = ? AND mailbox_id = ?", msgID, mailboxID).First(&rec).Error; err != nil {
		return "", "", nil, errors.New("邮件不存在")
	}
	var box model.Mailbox
	if err := model.DB.First(&box, rec.MailboxID).Error; err != nil {
		return "", "", nil, errors.New("账号不存在")
	}
	base := mailboxDir(box.Domain, box.Name)
	raw, e := os.ReadFile(filepath.Join(base, "cur", rec.Filename))
	if e != nil {
		raw, e = os.ReadFile(filepath.Join(base, "new", rec.Filename))
		if e != nil {
			return "", "", nil, errors.New("邮件文件缺失")
		}
	}
	atts := extractAttachments(raw)
	if index < 0 || index >= len(atts) {
		return "", "", nil, errors.New("附件不存在")
	}
	a := atts[index]
	return a.Filename, a.ContentType, a.Data, nil
}

// deleteMailboxMessage 删除某封信（元数据 + maildir 文件）
func deleteMailboxMessage(mailboxID, msgID uint) error {
	var rec model.MailMessage
	if err := model.DB.Where("id = ? AND mailbox_id = ?", msgID, mailboxID).First(&rec).Error; err != nil {
		return errors.New("邮件不存在")
	}
	var box model.Mailbox
	if err := model.DB.First(&box, rec.MailboxID).Error; err == nil {
		base := mailboxDir(box.Domain, box.Name)
		for _, sub := range []string{"cur", "new", "tmp"} {
			_ = os.Remove(filepath.Join(base, sub, rec.Filename))
		}
	}
	return model.DB.Delete(&model.MailMessage{}, rec.ID).Error
}

// setMailboxMessageSeen 批量标记已读/未读
func setMailboxMessageSeen(mailboxID uint, ids []uint, seen bool) error {
	if len(ids) == 0 {
		return nil
	}
	var recs []model.MailMessage
	model.DB.Where("mailbox_id = ? AND id IN ?", mailboxID, ids).Find(&recs)
	for _, r := range recs {
		model.DB.Model(&model.MailMessage{}).Where("id = ?", r.ID).Update("seen", seen)
		var box model.Mailbox
		if err := model.DB.First(&box, r.MailboxID).Error; err != nil {
			continue
		}
		if seen {
			moveMaildirToCur(box.Domain, box.Name, r.Filename)
		}
	}
	return nil
}

// moveMaildirToCur 若文件在 new 则移到 cur
func moveMaildirToCur(domain, user, fname string) {
	base := mailboxDir(domain, user)
	if _, err := os.Stat(filepath.Join(base, "new", fname)); err == nil {
		_ = os.Rename(filepath.Join(base, "new", fname), filepath.Join(base, "cur", fname))
	}
}

// 供 router 调用的导出封装

func ListMailboxMessagesAPI(mailboxID uint, folder string) ([]model.MailMessageView, error) {
	return listMailboxMessages(mailboxID, folder)
}

func ReadMailboxMessageAPI(mailboxID, msgID uint) (*model.MailMessageView, error) {
	return readMailboxMessage(mailboxID, msgID, true)
}

func DeleteMailboxMessageAPI(mailboxID, msgID uint) error {
	return deleteMailboxMessage(mailboxID, msgID)
}

func SetMailboxMessagesSeenAPI(mailboxID uint, ids []uint, seen bool) error {
	return setMailboxMessageSeen(mailboxID, ids, seen)
}

// CountMailboxUnseenAPI 某账号收件箱未读数
func CountMailboxUnseenAPI(mailboxID uint) int64 {
	var n int64
	model.DB.Model(&model.MailMessage{}).
		Where("mailbox_id = ? AND folder = ? AND seen = ?", mailboxID, "inbox", false).Count(&n)
	return n
}

// CountSystemUnseenAPI 导出：全部账号未读数（用于左侧菜单红点）
func CountSystemUnseenAPI() int64 {
	var n int64
	model.DB.Model(&model.MailMessage{}).Where("seen = ?", false).Count(&n)
	return n
}

// MarkMailboxAllSeenAPI 把某账号收件箱全部未读标为已读
func MarkMailboxAllSeenAPI(mailboxID uint) error {
	var ids []uint
	model.DB.Model(&model.MailMessage{}).
		Where("mailbox_id = ? AND folder = ? AND seen = ?", mailboxID, "inbox", false).
		Pluck("id", &ids)
	if len(ids) == 0 {
		return nil
	}
	return setMailboxMessageSeen(mailboxID, ids, true)
}

// extractBodies 提取 text/plain 与 text/html，并把 HTML 中的 cid: 内嵌图
// 替换为 data: URL（浏览器不认 cid: 协议，需转换后才能显示）。
func extractBodies(raw []byte) (text, html string) {
	col := &mimeCollector{inline: map[string]string{}}
	col.walk(raw, 0)
	text = col.text
	html = col.html
	if html != "" && len(col.inline) > 0 {
		html = replaceCidRefs(html, col.inline)
	}
	return text, html
}

// mimeCollector 遍历 MIME 树时收集文本正文与内嵌图片
type mimeCollector struct {
	text   string
	html   string
	inline map[string]string // cid（小写、去尖括号）-> data URL
}

// walk 递归解析一段 MIME 实体，收集文本/HTML/内嵌图。
func (c *mimeCollector) walk(raw []byte, depth int) {
	if depth > 6 {
		return
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return
	}
	ct := msg.Header.Get("Content-Type")
	ctype, params, _ := mime.ParseMediaType(ct)
	if strings.HasPrefix(ctype, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return
		}
		for _, p := range splitMultipart(raw, boundary) {
			c.walk(p, depth+1)
		}
		return
	}
	// 叶子 part
	if strings.HasPrefix(ctype, "text/plain") && c.text == "" {
		c.text = decodeBody(msg.Body, msg.Header.Get("Content-Transfer-Encoding"), params["charset"])
		return
	}
	if strings.HasPrefix(ctype, "text/html") && c.html == "" {
		c.html = decodeBody(msg.Body, msg.Header.Get("Content-Transfer-Encoding"), params["charset"])
		return
	}
	// 内嵌图片：有 Content-ID
	if strings.HasPrefix(ctype, "image/") {
		cid := strings.Trim(strings.TrimSpace(msg.Header.Get("Content-Id")), "<>")
		if cid == "" {
			return
		}
		data, _ := io.ReadAll(msg.Body)
		if strings.EqualFold(strings.TrimSpace(msg.Header.Get("Content-Transfer-Encoding")), "base64") {
			if dec, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(string(data), "\n", "")); err == nil {
				data = dec
			}
		}
		c.inline[strings.ToLower(cid)] = "data:" + ctype + ";base64," + base64.StdEncoding.EncodeToString(data)
	}
}

// replaceCidRefs 把 HTML 中的 cid:xxx 引用替换为对应的 data URL。
func replaceCidRefs(html string, inline map[string]string) string {
	for cid, dataURL := range inline {
		// 大小写不敏感替换 cid:xxx（常见形式：src="cid:xxx"）
		re := regexp.MustCompile(`(?i)cid:` + regexp.QuoteMeta(cid))
		html = re.ReplaceAllString(html, dataURL)
	}
	return html
}

// splitMultipart 按 boundary 切分 multipart part。
func splitMultipart(raw []byte, boundary string) [][]byte {
	marker := []byte("--" + boundary)
	var parts [][]byte
	pos := 0
	for {
		// 找本 part 的起始边界
		start := bytes.Index(raw[pos:], marker)
		if start < 0 {
			break
		}
		start += pos
		// 收尾边界(后面是 -- 或空白终止)则结束
		rest := raw[start+len(marker):]
		if len(rest) > 0 && rest[0] == '-' {
			break
		}
		// 跳过边界行到行尾
		lineEnd := bytes.IndexByte(rest, '\n')
		if lineEnd < 0 {
			break
		}
		contentStart := start + len(marker) + lineEnd + 1
		// 找下一个边界
		next := bytes.Index(raw[contentStart:], []byte("\n"+string(marker)))
		if next < 0 {
			// 最后一个 part：内容到文件尾部（去掉末尾的 \r\n）
			tail := raw[contentStart:]
			parts = append(parts, bytes.TrimRight(tail, "\r\n"))
			break
		}
		part := raw[contentStart : contentStart+next]
		// 去掉 part 末尾紧跟边界前的换行
		part = bytes.TrimRight(part, "\r\n")
		if len(part) > 0 {
			parts = append(parts, part)
		}
		pos = contentStart + next + 1
	}
	return parts
}

// decodeBody 按传输编码与字符集解码正文。
func decodeBody(r io.Reader, enc, charset string) string {
	buf, _ := io.ReadAll(r)
	var out []byte
	switch strings.ToLower(strings.TrimSpace(enc)) {
	case "base64":
		dec, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(string(buf), "\n", ""))
		if err == nil {
			out = dec
		} else {
			out = buf
		}
	case "quoted-printable":
		out = qpDecode(buf)
	default:
		out = buf
	}
	return charsetDecode(out, charset)
}

func qpDecode(data []byte) []byte {
	var out []byte
	for i := 0; i < len(data); i++ {
		c := data[i]
		if c == '=' && i+2 < len(data) {
			if isHex(data[i+1]) && isHex(data[i+2]) {
				out = append(out, hexVal(data[i+1])<<4|hexVal(data[i+2]))
				i += 2
				continue
			}
		}
		if c == '\r' || c == '\n' {
			continue // 忽略行尾软换行（quoted-printable 的 =\r\n 已在上面处理）
		}
		out = append(out, c)
	}
	return out
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func hexVal(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	default:
		return c - 'A' + 10
	}
}

// charsetDecode 处理非 utf-8 字符集（gbk/gb2312 常见）
func charsetDecode(data []byte, charset string) string {
	cs := strings.ToLower(strings.TrimSpace(charset))
	if cs == "" || strings.Contains(cs, "utf-8") || strings.Contains(cs, "us-ascii") || utf8.Valid(data) {
		return string(data)
	}
	var enc encoding.Encoding
	switch {
	case strings.Contains(cs, "gbk"), strings.Contains(cs, "gb2312"), strings.Contains(cs, "gb18030"):
		enc = simplifiedchinese.GBK
	default:
		return string(data)
	}
	out, _, err := transform.String(enc.NewDecoder(), string(data))
	if err != nil {
		return string(data)
	}
	return out
}
