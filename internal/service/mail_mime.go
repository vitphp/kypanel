package service

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"strings"
	"time"
)

// MIME 报文构造（RFC 2822 / 2045）。
// 结构：multipart/mixed → multipart/related → multipart/alternative。

// MailAttachment 附件或内嵌图片
type MailAttachment struct {
	Filename    string
	ContentType string
	Data        []byte
	Inline      bool   // 内嵌图片（正文以 cid 引用）
	ContentID   string // 内嵌图的 Content-ID（不含尖括号）
}

// MailContent 待发送邮件内容
type MailContent struct {
	FromName string
	FromAddr string
	ToAddrs  []string
	Subject  string
	TextBody string
	HtmlBody string
	Attach   []MailAttachment
}

// buildMimeMessage 组装完整邮件报文（含头）。
func buildMimeMessage(c MailContent) []byte {
	var buf bytes.Buffer

	writeHeader(&buf, "From", formatAddress(c.FromName, c.FromAddr))
	writeHeader(&buf, "To", strings.Join(c.ToAddrs, ", "))
	writeHeader(&buf, "Subject", encodeHeader(c.Subject))
	writeHeader(&buf, "Date", time.Now().Format(time.RFC1123Z))
	writeHeader(&buf, "Message-ID", genMessageID(c.FromAddr))
	writeHeader(&buf, "MIME-Version", "1.0")

	var normalAttach, inlineAttach []MailAttachment
	for _, a := range c.Attach {
		if a.Inline {
			inlineAttach = append(inlineAttach, a)
		} else {
			normalAttach = append(normalAttach, a)
		}
	}

	bodyPart := buildAlternativePart(c)
	rootPart := bodyPart
	if len(inlineAttach) > 0 {
		rootPart = wrapMultipart("multipart/related", bodyPart, inlineAttach...)
	}
	if len(normalAttach) > 0 {
		buf.Write(wrapMultipart("multipart/mixed", rootPart, normalAttach...))
	} else {
		buf.Write(rootPart)
	}
	return buf.Bytes()
}

// buildAlternativePart 构造 multipart/alternative（纯文本 + HTML）。
func buildAlternativePart(c MailContent) []byte {
	text := c.TextBody
	html := c.HtmlBody
	if text == "" && html != "" {
		text = htmlToPlain(html)
	}
	if html == "" && text != "" {
		return singlePart("text/plain; charset=UTF-8", []byte(text))
	}
	if text == "" {
		text = " "
	}
	b := genBoundary()
	var buf bytes.Buffer
	buf.WriteString("Content-Type: multipart/alternative; boundary=\"" + b + "\"\r\n\r\n")
	buf.WriteString("--" + b + "\r\n")
	buf.Write(singlePart("text/plain; charset=UTF-8", []byte(text)))
	buf.WriteString("\r\n--" + b + "\r\n")
	buf.Write(singlePart("text/html; charset=UTF-8", []byte(html)))
	buf.WriteString("\r\n--" + b + "--\r\n")
	return buf.Bytes()
}

// wrapMultipart 生成 multipart/<subtype>，首段为 root，其后为附件/内嵌图。
func wrapMultipart(subtype string, root []byte, attach ...MailAttachment) []byte {
	b := genBoundary()
	var buf bytes.Buffer
	buf.WriteString("Content-Type: " + subtype + "; boundary=\"" + b + "\"\r\n\r\n")
	buf.WriteString("--" + b + "\r\n")
	buf.Write(root)
	buf.WriteString("\r\n")
	for _, a := range attach {
		buf.WriteString("--" + b + "\r\n")
		buf.Write(buildAttachPart(a))
		buf.WriteString("\r\n")
	}
	buf.WriteString("--" + b + "--\r\n")
	return buf.Bytes()
}

func buildAttachPart(a MailAttachment) []byte {
	var buf bytes.Buffer
	ct := a.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	disp := "attachment"
	if a.Inline {
		disp = "inline"
	}
	buf.WriteString("Content-Type: " + ct + "; name=\"" + encodeHeader(a.Filename) + "\"\r\n")
	buf.WriteString("Content-Transfer-Encoding: base64\r\n")
	buf.WriteString("Content-Disposition: " + disp + "; filename=\"" + encodeHeader(a.Filename) + "\"\r\n")
	if a.Inline && a.ContentID != "" {
		buf.WriteString("Content-ID: <" + a.ContentID + ">\r\n")
	}
	buf.WriteString("\r\n")
	writeBase64Lines(&buf, a.Data)
	return buf.Bytes()
}

func singlePart(contentType string, body []byte) []byte {
	var buf bytes.Buffer
	buf.WriteString("Content-Type: " + contentType + "\r\n")
	buf.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	writeBase64Lines(&buf, body)
	return buf.Bytes()
}

// writeBase64Lines base64 编码并按 76 列换行
func writeBase64Lines(buf *bytes.Buffer, data []byte) {
	enc := base64.StdEncoding.EncodeToString(data)
	for i := 0; i < len(enc); i += 76 {
		end := i + 76
		if end > len(enc) {
			end = len(enc)
		}
		buf.WriteString(enc[i:end] + "\r\n")
	}
}

func writeHeader(buf *bytes.Buffer, key, value string) {
	buf.WriteString(key + ": " + value + "\r\n")
}

func formatAddress(name, addr string) string {
	if name == "" {
		return addr
	}
	return encodeHeader(name) + " <" + addr + ">"
}

// encodeHeader 含非 ASCII 时做 RFC2047 编码
func encodeHeader(s string) string {
	if isASCII(s) {
		return s
	}
	return mime.QEncoding.Encode("UTF-8", s)
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func genBoundary() string {
	return "----=_kypanel_" + randHex(16)
}

func genMessageID(fromAddr string) string {
	domain := "kypanel"
	if at := strings.LastIndexByte(fromAddr, '@'); at >= 0 {
		domain = fromAddr[at+1:]
	}
	return "<" + randHex(16) + "." + fmt.Sprint(time.Now().UnixNano()) + "@" + domain + ">"
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// htmlToPlain HTML 转纯文本，用作纯文本回退
func htmlToPlain(html string) string {
	s := html
	repl := []struct{ from, to string }{
		{"<br>", "\n"}, {"<br/>", "\n"}, {"<br />", "\n"},
		{"</p>", "\n"}, {"</div>", "\n"}, {"</li>", "\n"}, {"</tr>", "\n"},
	}
	for _, r := range repl {
		s = strings.ReplaceAll(s, r.from, r.to)
		s = strings.ReplaceAll(s, strings.ToUpper(r.from), r.to)
	}
	var out strings.Builder
	inTag := false
	for _, ch := range s {
		switch {
		case ch == '<':
			inTag = true
		case ch == '>':
			inTag = false
		case !inTag:
			out.WriteRune(ch)
		}
	}
	t := out.String()
	t = strings.ReplaceAll(t, "&nbsp;", " ")
	t = strings.ReplaceAll(t, "&amp;", "&")
	t = strings.ReplaceAll(t, "&lt;", "<")
	t = strings.ReplaceAll(t, "&gt;", ">")
	t = strings.ReplaceAll(t, "&quot;", "\"")
	t = strings.ReplaceAll(t, "&#39;", "'")
	return strings.TrimSpace(t)
}
