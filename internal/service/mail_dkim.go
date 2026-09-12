package service

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"kypanel/internal/model"
	"kypanel/internal/utils"
)

// DKIM（RFC 6376）自研实现：relaxed/relaxed 规范化 + rsa-sha256 签名/校验。
// 私钥以 AES-GCM 加密后存 mail_domains.dkim_private_key。

const dkimDefaultSelector = "default"

// dkimSignedHeaders 出站邮件参与签名的头（按此顺序写入 h=）。
// 覆盖 From/To/Subject/Date/Message-ID/MIME-Version/Content-Type：
// 这些头由 buildMimeMessage 一定写入，避免出现空头导致签名与校验不一致。
var dkimSignedHeaders = []string{
	"from", "to", "subject", "date", "message-id", "mime-version", "content-type",
}

// EnsureMailDkim 确保某域名已有 DKIM 密钥；没有则生成 RSA-2048 并落库。
// 返回可用于 DNS TXT 的公钥记录值（v=DKIM1; k=rsa; p=...）与选择器。
func EnsureMailDkim(domainID uint) (selector, txt string, err error) {
	var dom model.MailDomain
	if err := model.DB.First(&dom, domainID).Error; err != nil {
		return "", "", errors.New("域名不存在")
	}
	if sel := strings.TrimSpace(dom.DkimSelector); sel != "" {
		selector = sel
	} else {
		selector = dkimDefaultSelector
	}
	// 已有私钥：直接导出公钥
	if key := dkimLoadPrivateKey(dom); key != nil {
		return selector, dkimPublicTXT(key), nil
	}
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", errors.New("生成 DKIM 密钥失败: " + err.Error())
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	stored := string(keyPEM)
	if enc, e := utils.EncryptString(stored); e == nil {
		stored = enc
	}
	patch := map[string]interface{}{
		"dkim_private_key": stored,
		"dkim_selector":    selector,
		"dkim_configured":  true,
	}
	if err := model.DB.Model(&model.MailDomain{}).Where("id = ?", domainID).Updates(patch).Error; err != nil {
		return "", "", errors.New("保存 DKIM 密钥失败")
	}
	dom.DkimPrivateKey = stored
	dom.DkimSelector = selector
	return selector, dkimPublicTXT(priv), nil
}

// MailDkimInfo 读取某域名当前的 DKIM 公钥信息（未生成时返回空值）。
func MailDkimInfo(domainID uint) (selector string, generated bool, txt string) {
	var dom model.MailDomain
	if err := model.DB.First(&dom, domainID).Error; err != nil {
		return "", false, ""
	}
	selector = strings.TrimSpace(dom.DkimSelector)
	if selector == "" {
		selector = dkimDefaultSelector
	}
	key := dkimLoadPrivateKey(dom)
	if key == nil {
		return selector, false, ""
	}
	return selector, true, dkimPublicTXT(key)
}

// dkimLoadPrivateKey 解密并解析域名私钥；无密钥或解析失败返回 nil。
func dkimLoadPrivateKey(dom model.MailDomain) *rsa.PrivateKey {
	raw := strings.TrimSpace(dom.DkimPrivateKey)
	if raw == "" {
		return nil
	}
	if dec, err := utils.DecryptString(raw); err == nil {
		raw = dec
	}
	block, _ := pem.Decode([]byte(raw))
	if block == nil {
		return nil
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		if k2, err2 := x509.ParsePKCS8PrivateKey(block.Bytes); err2 == nil {
			if rk, ok := k2.(*rsa.PrivateKey); ok {
				return rk
			}
		}
		return nil
	}
	return key
}

// dkimPublicTXT 由私钥导出 DNS TXT 记录值。
func dkimPublicTXT(priv *rsa.PrivateKey) string {
	der := x509.MarshalPKCS1PublicKey(&priv.PublicKey)
	return "v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(der)
}

// DkimDnsHost 返回该域名 DKIM 记录的主机名（selector._domainkey.domain）。
func DkimDnsHost(domain, selector string) string {
	if selector == "" {
		selector = dkimDefaultSelector
	}
	return selector + "._domainkey." + domain
}

// SignOutboundMail 为出站报文补 DKIM-Signature（域名未配置密钥时原样返回）。
// 仅对"发件域属于本站邮箱域名"的邮件签名。
func SignOutboundMail(raw []byte, fromAddr string) []byte {
	domain := domainOf(fromAddr)
	if domain == "" {
		return raw
	}
	var dom model.MailDomain
	if err := model.DB.Where("domain = ?", domain).First(&dom).Error; err != nil {
		return raw
	}
	selector := strings.TrimSpace(dom.DkimSelector)
	if selector == "" {
		selector = dkimDefaultSelector
	}
	priv := dkimLoadPrivateKey(dom)
	if priv == nil {
		return raw
	}
	signed, err := dkimSign(raw, dom.Domain, selector, priv)
	if err != nil {
		slog.Warn("DKIM 签名失败", "domain", dom.Domain, "err", err)
		return raw
	}
	return signed
}

// dkimSign 计算并插入 DKIM-Signature 头。
func dkimSign(raw []byte, domain, selector string, priv *rsa.PrivateKey) ([]byte, error) {
	headers, body := splitRawMessage(raw)
	if len(headers) == 0 {
		return nil, errors.New("报文无头部")
	}
	// 1) 正文哈希（relaxed）
	bodyHash := dkimBodyHash(body)

	// 2) 选出参与签名的头（按定义顺序），缺失的跳过。
	// 同名头取最后一次出现（RFC 6376 规定重复头自下而上选取），与校验端一致。
	index := map[string]string{} // name -> 原始值
	for _, h := range headers {
		index[strings.ToLower(h.name)] = h.value
	}
	var hList []string
	var canonHeaders strings.Builder
	for _, name := range dkimSignedHeaders {
		v, ok := index[name]
		if !ok {
			continue
		}
		hList = append(hList, name)
		canonHeaders.WriteString(dkimCanonicalHeader(name, v))
	}
	if len(hList) == 0 {
		return nil, errors.New("无可签名头")
	}

	// 3) 构造 DKIM-Signature（b= 留空），对"规范化后的它 + 上面的头"签名。
	// 参与签名的 DKIM-Signature 头按 relaxed 规范化（名称小写、值折叠空白），且不带结尾 CRLF。
	value := fmt.Sprintf("v=1; a=rsa-sha256; c=relaxed/relaxed; d=%s; s=%s; "+
		"h=%s; bh=%s; b=", domain, selector, strings.Join(hList, ":"), bodyHash)
	signature, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256,
		sha256Sum(canonHeaders.String()+dkimRelaxedHeaderNoCRLF("DKIM-Signature", value)))
	if err != nil {
		return nil, err
	}
	final := "DKIM-Signature: " + value + base64.StdEncoding.EncodeToString(signature) + "\r\n"

	// 4) 插入到报文最前面
	var out []byte
	out = append(out, []byte(final)...)
	out = append(out, raw...)
	return out, nil
}

// dkimBodyHash 计算 relaxed 正文哈希（base64）。
func dkimBodyHash(body []byte) string {
	sum := sha256.Sum256([]byte(dkimCanonicalBody(body)))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// dkimCanonicalBody relaxed 正文规范化：
// 去掉行尾空白、把行内连续空白压成单个空格、去掉末尾空行、结尾补 CRLF。
func dkimCanonicalBody(body []byte) string {
	text := strings.ReplaceAll(string(body), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	var out []string
	for _, ln := range lines {
		ln = strings.TrimRight(ln, " \t")
		ln = collapseWSP(ln)
		out = append(out, ln)
	}
	// 去掉末尾空行
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\r\n") + "\r\n"
}

// collapseWSP 把连续空白（空格/制表）压成单个空格。
func collapseWSP(s string) string {
	var b strings.Builder
	prevWS := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '\t' {
			if prevWS {
				continue
			}
			prevWS = true
			b.WriteByte(' ')
			continue
		}
		prevWS = false
		b.WriteByte(c)
	}
	return b.String()
}

// dkimCanonicalHeader relaxed 头规范化：name 小写、值去首尾空白并折叠内部空白。
func dkimCanonicalHeader(name, value string) string {
	return strings.ToLower(name) + ":" + dkimRelaxedHeaderValue(value) + "\r\n"
}

// dkimRelaxedHeaderNoCRLF 同 dkimCanonicalHeader，但不带结尾 CRLF（用于签名中的 DKIM-Signature 头）。
func dkimRelaxedHeaderNoCRLF(name, value string) string {
	return strings.ToLower(name) + ":" + dkimRelaxedHeaderValue(value)
}

// sha256Sum 计算 SHA-256 摘要。
func sha256Sum(s string) []byte {
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}

// dkimRelaxedHeaderValue 对头值做 relaxed 规范化（去首尾空白 + 折叠内部空白）。
func dkimRelaxedHeaderValue(v string) string {
	// 先展开折叠（CRLF + WSP → 单空格）
	v = strings.ReplaceAll(v, "\r\n", " ")
	v = strings.ReplaceAll(v, "\n", " ")
	v = strings.ReplaceAll(v, "\t", " ")
	return strings.TrimSpace(collapseWSP(v))
}

// rawHeader 原始头字段
type rawHeader struct {
	name  string
	value string
}

// splitRawMessage 拆分报文为头字段列表与正文（兼容 CRLF / LF）。
// 值保留原始内容（含折叠换行），交给规范化函数处理。
// 注意：必须整体跳过空行分隔符（CRLFCRLF 或 LFLF），否则正文会多出一个换行，
// 导致 DKIM 正文哈希与真实收件方计算值不一致、签名校验失败。
func splitRawMessage(raw []byte) ([]rawHeader, []byte) {
	text := string(raw)
	if i := strings.Index(text, "\r\n\r\n"); i >= 0 {
		return parseRawHeaders(text[:i]), []byte(text[i+4:])
	}
	if i := strings.Index(text, "\n\n"); i >= 0 {
		return parseRawHeaders(text[:i]), []byte(text[i+2:])
	}
	// 无正文分隔：整个都是头
	return parseRawHeaders(text), nil
}

// parseRawHeaders 解析头块，处理折叠行（以空白开头的续行）。
func parseRawHeaders(headerPart string) []rawHeader {
	headerPart = strings.ReplaceAll(headerPart, "\r\n", "\n")
	lines := strings.Split(headerPart, "\n")
	var out []rawHeader
	var cur *rawHeader
	for _, ln := range lines {
		if len(ln) > 0 && (ln[0] == ' ' || ln[0] == '\t') && cur != nil {
			cur.value += "\n" + ln
			continue
		}
		colon := strings.IndexByte(ln, ':')
		if colon <= 0 {
			continue
		}
		if cur != nil {
			out = append(out, *cur)
		}
		cur = &rawHeader{name: strings.TrimSpace(ln[:colon]), value: strings.TrimSpace(ln[colon+1:])}
	}
	if cur != nil {
		out = append(out, *cur)
	}
	return out
}
