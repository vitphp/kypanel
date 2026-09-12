package service

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"
)

// 入站邮件安全：SPF 校验、DKIM 校验、连接限速、灰名单。
// 全部自研（不引第三方库），仅用标准库 net / crypto。

// ===== DNS TXT 缓存（SPF / DKIM 公钥查询共用）=====

type dnsTxtEntry struct {
	vals []string
	exp  time.Time
}

var dnsTxtCache sync.Map // domain -> dnsTxtEntry

// lookupTXT 查询域名 TXT 记录（带 5 分钟内存缓存，降低递归查询压力）。
func lookupTXT(domain string) []string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return nil
	}
	if v, ok := dnsTxtCache.Load(domain); ok {
		if e := v.(dnsTxtEntry); time.Now().Before(e.exp) {
			return e.vals
		}
	}
	vals, err := net.LookupTXT(domain)
	if err != nil {
		vals = nil
	}
	dnsTxtCache.Store(domain, dnsTxtEntry{vals: vals, exp: time.Now().Add(5 * time.Minute)})
	return vals
}

// ===== SPF =====

// spfResult SPF 校验结果（RFC 7208）
type spfResult string

const (
	spfPass      spfResult = "pass"
	spfFail      spfResult = "fail"      // -all，硬失败
	spfSoftFail  spfResult = "softfail"  // ~all，软失败
	spfNeutral   spfResult = "neutral"
	spfNone      spfResult = "none"      // 无 SPF 记录
	spfTempError spfResult = "temperror"
)

// CheckSPF 校验来源 IP 是否被发件域的 SPF 授权（ip 为连接来源 IP，helo 为 EHLO 名）。
func CheckSPF(ip net.IP, helo, mailFrom string) spfResult {
	domain := domainOf(mailFrom)
	if domain == "" {
		return spfNone
	}
	return spfEvaluate(ip, helo, domain, 0)
}

// spfEvaluate 递归求值（depth 限制 include 层级，防环）。
func spfEvaluate(ip net.IP, helo, domain string, depth int) spfResult {
	if depth > 10 {
		return spfTempError
	}
	var record string
	for _, t := range lookupTXT(domain) {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(t)), "v=spf1") {
			record = strings.TrimSpace(t)
			break
		}
	}
	if record == "" {
		return spfNone
	}
	terms := strings.Fields(record)
	if len(terms) > 0 && strings.HasPrefix(strings.ToLower(terms[0]), "v=spf1") {
		terms = terms[1:]
	}
	for _, term := range terms {
		if term == "" {
			continue
		}
		qual := byte('+')
		switch term[0] {
		case '+', '-', '~', '?':
			qual = term[0]
			term = term[1:]
		}
		if term == "" {
			continue
		}
		mech, arg := splitMechanism(term)
		matched := false
		switch mech {
		case "all":
			matched = true
		case "ip4":
			matched = spfMatchIP4(ip, arg)
		case "ip6":
			matched = spfMatchIP6(ip, arg)
		case "a":
			matched = spfMatchA(ip, spfArgDomain(arg, domain))
		case "mx":
			matched = spfMatchMX(ip, spfArgDomain(arg, domain))
		case "include":
			if arg == "" {
				continue
			}
			res := spfEvaluate(ip, helo, arg, depth+1)
			if res == spfPass {
				return spfPass
			}
			continue
		default:
			// ptr / exists / 未知机制：不参与匹配（保守处理为不命中）
			continue
		}
		if matched {
			switch qual {
			case '-':
				return spfFail
			case '~':
				return spfSoftFail
			case '?':
				return spfNeutral
			default:
				return spfPass
			}
		}
	}
	return spfNeutral
}

// splitMechanism 拆分 "ip4:1.2.3.4/24" → ("ip4", "1.2.3.4/24")
func splitMechanism(term string) (string, string) {
	if i := strings.IndexByte(term, ':'); i >= 0 {
		return strings.ToLower(term[:i]), term[i+1:]
	}
	// 形如 a/24、mx/24
	if i := strings.IndexByte(term, '/'); i >= 0 {
		return strings.ToLower(term[:i]), term[i:]
	}
	return strings.ToLower(term), ""
}

// spfArgDomain 解析机制参数里的域名；arg 为 "/24" 这类前缀长度或 ":domain" 变体时回退到默认域。
func spfArgDomain(arg, def string) string {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return def
	}
	if strings.HasPrefix(arg, "/") {
		return def
	}
	if i := strings.IndexByte(arg, '/'); i >= 0 {
		return arg[:i]
	}
	return arg
}

func spfMatchIP4(ip net.IP, arg string) bool {
	v4 := ip.To4()
	if v4 == nil || arg == "" {
		return false
	}
	if strings.Contains(arg, "/") {
		pfx, err := netip.ParsePrefix(arg)
		if err != nil || !pfx.Addr().Is4() {
			return false
		}
		a, ok := netip.AddrFromSlice(v4)
		return ok && pfx.Contains(a)
	}
	a, ok := netip.AddrFromSlice(v4)
	if !ok {
		return false
	}
	b, err := netip.ParseAddr(arg)
	return err == nil && a == b
}

func spfMatchIP6(ip net.IP, arg string) bool {
	if ip.To4() != nil || arg == "" {
		return false
	}
	if strings.Contains(arg, "/") {
		pfx, err := netip.ParsePrefix(arg)
		if err != nil {
			return false
		}
		a, ok := netip.AddrFromSlice(ip)
		return ok && pfx.Contains(a)
	}
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	b, err := netip.ParseAddr(arg)
	return err == nil && a == b
}

func spfMatchA(ip net.IP, domain string) bool {
	if domain == "" {
		return false
	}
	ips, err := net.LookupIP(domain)
	if err != nil {
		return false
	}
	for _, cand := range ips {
		if cand.Equal(ip) {
			return true
		}
	}
	return false
}

func spfMatchMX(ip net.IP, domain string) bool {
	if domain == "" {
		return false
	}
	mxs, err := net.LookupMX(domain)
	if err != nil {
		return false
	}
	for _, mx := range mxs {
		if spfMatchA(ip, strings.TrimSuffix(mx.Host, ".")) {
			return true
		}
	}
	return false
}

// ===== DKIM 校验 =====

// VerifyDKIM 校验报文的 DKIM-Signature。返回 pass / fail / none。
// 支持 c=relaxed/relaxed 与 relaxed/simple（覆盖主流发信方）。
func VerifyDKIM(raw []byte) string {
	headers, body := splitRawMessage(raw)
	var sigHeader string
	for _, h := range headers {
		if strings.EqualFold(h.name, "DKIM-Signature") {
			sigHeader = h.value
			break
		}
	}
	if sigHeader == "" {
		return "none"
	}
	tags := dkimParseTags(sigHeader)
	if tags["v"] != "1" || !strings.HasPrefix(strings.ToLower(tags["a"]), "rsa-sha256") {
		return "none"
	}
	canon := tags["c"]
	if canon == "" {
		canon = "simple/simple"
	}
	parts := strings.SplitN(canon, "/", 2)
	hc := strings.ToLower(parts[0])
	bc := "simple"
	if len(parts) == 2 {
		bc = strings.ToLower(parts[1])
	}
	// 仅实现 relaxed 头规范化；simple 头需保留原始折叠，此处保守跳过
	if hc != "relaxed" {
		return "none"
	}

	// 1) 正文哈希比对
	var bodyHash string
	switch bc {
	case "relaxed":
		bodyHash = dkimBodyHash(body)
	case "simple":
		sum := sha256.Sum256([]byte(dkimCanonicalBodySimple(body)))
		bodyHash = base64.StdEncoding.EncodeToString(sum[:])
	default:
		return "none"
	}
	if bodyHash != strings.TrimSpace(tags["bh"]) {
		return "fail"
	}

	// 2) 取 DNS 公钥
	domain := strings.TrimSpace(tags["d"])
	selector := strings.TrimSpace(tags["s"])
	if domain == "" || selector == "" {
		return "fail"
	}
	pub := dkimPubKeyLookup(selector, domain)
	if pub == nil {
		return "none"
	}

	// 3) 重建被签名的规范化数据
	names := strings.Split(strings.ToLower(tags["h"]), ":")
	canonHeaders := dkimCollectSignedHeaders(headers, names)
	if canonHeaders == "" {
		return "fail"
	}
	// 用去掉 b= 值的 DKIM-Signature 头（relaxed 规范化、无 CRLF）参与验签
	bVal := strings.TrimSpace(tags["b"])
	unsigned := strings.Replace(sigHeader, bVal, "", 1)
	data := canonHeaders + dkimRelaxedHeaderNoCRLF("DKIM-Signature", unsigned)
	sum := sha256.Sum256([]byte(data))

	sig, err := base64.StdEncoding.DecodeString(bVal)
	if err != nil {
		return "fail"
	}
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig); err != nil {
		return "fail"
	}
	return "pass"
}

// dkimCanonicalBodySimple simple 正文规范化：统一 CRLF、去末尾空行、空正文为单个 CRLF。
func dkimCanonicalBodySimple(body []byte) string {
	t := strings.ReplaceAll(string(body), "\r\n", "\n")
	t = strings.ReplaceAll(t, "\n", "\r\n")
	for strings.HasSuffix(t, "\r\n") {
		t = t[:len(t)-2]
	}
	if t == "" {
		return "\r\n"
	}
	return t + "\r\n"
}

// dkimCollectSignedHeaders 按 h= 列表（自下而上取同名头）收集并规范化被签名的头。
func dkimCollectSignedHeaders(headers []rawHeader, names []string) string {
	used := make([]bool, len(headers))
	var b strings.Builder
	for _, name := range names {
		name = strings.TrimSpace(strings.ToLower(name))
		if name == "" {
			continue
		}
		found := -1
		for i := len(headers) - 1; i >= 0; i-- {
			if !used[i] && strings.ToLower(headers[i].name) == name {
				found = i
				break
			}
		}
		if found < 0 {
			continue
		}
		used[found] = true
		b.WriteString(dkimCanonicalHeader(headers[found].name, headers[found].value))
	}
	return b.String()
}

// dkimPubKeyLookup 公钥查询钩子（默认走 DNS，测试时可替换）。
var dkimPubKeyLookup = func(selector, domain string) *rsa.PublicKey {
	return dkimLookupPublicKey(selector, domain)
}

// dkimLookupPublicKey 从 DNS TXT 取 DKIM 公钥（selector._domainkey.domain）。
func dkimLookupPublicKey(selector, domain string) *rsa.PublicKey {
	host := selector + "._domainkey." + domain
	var pValue string
	for _, t := range lookupTXT(host) {
		for _, kv := range strings.Split(t, ";") {
			kv = strings.TrimSpace(kv)
			if strings.HasPrefix(strings.ToLower(kv), "p=") {
				pValue += strings.TrimSpace(kv[2:])
			}
		}
		if pValue != "" {
			break
		}
	}
	if pValue == "" {
		return nil
	}
	der, err := base64.StdEncoding.DecodeString(pValue)
	if err != nil {
		return nil
	}
	if pub, err := x509.ParsePKCS1PublicKey(der); err == nil {
		return pub
	}
	if pubAny, err := x509.ParsePKIXPublicKey(der); err == nil {
		if pub, ok := pubAny.(*rsa.PublicKey); ok {
			return pub
		}
	}
	return nil
}

// dkimParseTags 解析 DKIM-Signature 的 tag=value（; 分隔）。
func dkimParseTags(s string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		i := strings.IndexByte(part, '=')
		if i <= 0 {
			continue
		}
		out[strings.ToLower(strings.TrimSpace(part[:i]))] = strings.TrimSpace(part[i+1:])
	}
	return out
}

// ===== 连接限速 =====

var (
	mailRateMu   sync.Mutex
	mailRateHits = map[string][]int64{}
)

// mailRateLimit 每来源 IP 的建连频率上限（次/分钟），可用 PANEL_MAIL_RATE_LIMIT 覆盖（0=关闭）。
func mailRateLimit() int {
	if v := strings.TrimSpace(os.Getenv("PANEL_MAIL_RATE_LIMIT")); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return 120
}

// AllowMailConnection 判断某 IP 是否允许建连（滑动 1 分钟窗口）。超限返回 false。
func AllowMailConnection(ip string) bool {
	limit := mailRateLimit()
	if limit <= 0 || ip == "" {
		return true
	}
	now := time.Now().Unix()
	cut := now - 60
	mailRateMu.Lock()
	defer mailRateMu.Unlock()
	list := mailRateHits[ip]
	kept := list[:0]
	for _, t := range list {
		if t >= cut {
			kept = append(kept, t)
		}
	}
	if len(kept) >= limit {
		mailRateHits[ip] = kept
		return false
	}
	mailRateHits[ip] = append(kept, now)
	return true
}

// StartMailRateJanitor 定期清理限速表，避免长期运行内存增长。
func StartMailRateJanitor() {
	go func() {
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for range t.C {
			cut := time.Now().Unix() - 120
			mailRateMu.Lock()
			for ip, list := range mailRateHits {
				kept := list[:0]
				for _, v := range list {
					if v >= cut {
						kept = append(kept, v)
					}
				}
				if len(kept) == 0 {
					delete(mailRateHits, ip)
				} else {
					mailRateHits[ip] = kept
				}
			}
			mailRateMu.Unlock()
		}
	}()
}

// ===== 灰名单（可选，默认关闭；PANEL_MAIL_GREYLIST=1 开启）=====

type greylistEntry struct {
	first time.Time
}

var (
	greylistMu    sync.Mutex
	greylistStore = map[string]greylistEntry{}
)

// mailGreylistEnabled 是否启用灰名单（默认关闭，避免正常邮件被延迟）。
func mailGreylistEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("PANEL_MAIL_GREYLIST")))
	return v == "1" || v == "true" || v == "on"
}

// GreylistDefer 首次出现的 (ip, from, rcpt) 组合延迟 60 秒，正牌 MTA 会重试，垃圾邮件通常放弃。
// 返回 true 表示本次应 451 延迟。
func GreylistDefer(ip, from, rcpt string) bool {
	if !mailGreylistEnabled() {
		return false
	}
	key := ip + "|" + strings.ToLower(from) + "|" + strings.ToLower(rcpt)
	now := time.Now()
	greylistMu.Lock()
	defer greylistMu.Unlock()
	e, ok := greylistStore[key]
	if !ok {
		greylistStore[key] = greylistEntry{first: now}
		return true
	}
	if now.Sub(e.first) < 60*time.Second {
		return true
	}
	delete(greylistStore, key)
	return false
}

// StartMailGreylistJanitor 清理过期灰名单条目。
func StartMailGreylistJanitor() {
	go func() {
		t := time.NewTicker(30 * time.Minute)
		defer t.Stop()
		for range t.C {
			cut := time.Now().Add(-1 * time.Hour)
			greylistMu.Lock()
			for k, e := range greylistStore {
				if e.first.Before(cut) {
					delete(greylistStore, k)
				}
			}
			greylistMu.Unlock()
		}
	}()
}
