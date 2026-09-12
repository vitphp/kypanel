package service

import (
	"crypto/rand"
	"crypto/rsa"
	"net"
	"strings"
	"testing"
)

// 构造一封 ASCII 报文（便于精确篡改做负例）用于 DKIM 往返测试。
func buildTestMail() []byte {
	return buildMimeMessage(MailContent{
		FromName: "Boss",
		FromAddr: "boss@example.com",
		ToAddrs:  []string{"user@other.com"},
		Subject:  "Hello DKIM",
		TextBody: "this is the body",
	})
}

// TestDkimSignVerifyRoundTrip 校验「签名 → 校验」闭环，以及篡改后必须失败。
func TestDkimSignVerifyRoundTrip(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	raw := buildTestMail()
	signed, err := dkimSign(raw, "example.com", "default", priv)
	if err != nil {
		t.Fatalf("dkimSign: %v", err)
	}
	if !strings.HasPrefix(string(signed), "DKIM-Signature: v=1;") {
		t.Fatalf("签名头未插入: %s", string(signed[:60]))
	}
	if !strings.Contains(string(signed), "a=rsa-sha256") || !strings.Contains(string(signed), "c=relaxed/relaxed") {
		t.Fatal("签名参数不正确")
	}

	origHook := dkimPubKeyLookup
	dkimPubKeyLookup = func(selector, domain string) *rsa.PublicKey {
		if selector == "default" && domain == "example.com" {
			return &priv.PublicKey
		}
		return nil
	}
	defer func() { dkimPubKeyLookup = origHook }()

	if got := VerifyDKIM(signed); got != "pass" {
		t.Fatalf("合法签名应为 pass，实际 %s", got)
	}

	// 篡改被签名的 Subject 头 → 必须 fail
	tampered := strings.Replace(string(signed), "Subject: Hello DKIM", "Subject: Hello HACK", 1)
	if got := VerifyDKIM([]byte(tampered)); got != "fail" {
		t.Fatalf("篡改后应为 fail，实际 %s", got)
	}

	// 无签名头 → none
	if got := VerifyDKIM(raw); got != "none" {
		t.Fatalf("无签名应为 none，实际 %s", got)
	}
}

// TestDkimCanonicalBody relaxed 正文规范化：行尾空白去除、内部空白折叠、末尾空行剔除。
func TestDkimCanonicalBody(t *testing.T) {
	got := dkimCanonicalBody([]byte("a  b  \r\n\r\n\r\n"))
	want := "a b\r\n"
	if got != want {
		t.Fatalf("canonical body = %q, want %q", got, want)
	}
	// 空正文 → 空串（RFC: 空 body 的 relaxed 规范化为空）
	if got := dkimCanonicalBody(nil); got != "" {
		t.Fatalf("空正文应为空串，实际 %q", got)
	}
}

// TestDkimRelaxedHeaderValue 头值折叠展开。
func TestDkimRelaxedHeaderValue(t *testing.T) {
	got := dkimRelaxedHeaderValue("Hello\r\n  World  ")
	if got != "Hello World" {
		t.Fatalf("relaxed header = %q", got)
	}
}

// TestSpfMatchIP4 SPF ip4 机制匹配（含 CIDR 与精确地址）。
func TestSpfMatchIP4(t *testing.T) {
	ip := net.ParseIP("1.2.3.4")
	if !spfMatchIP4(ip, "1.2.3.0/24") {
		t.Fatal("1.2.3.4 应命中 1.2.3.0/24")
	}
	if spfMatchIP4(ip, "1.2.4.0/24") {
		t.Fatal("1.2.3.4 不应命中 1.2.4.0/24")
	}
	if !spfMatchIP4(ip, "1.2.3.4") {
		t.Fatal("精确地址应命中")
	}
	if spfMatchIP4(ip, "9.9.9.9") {
		t.Fatal("不同地址不应命中")
	}
	// IPv4 地址不应命中 ip6 机制
	if spfMatchIP6(ip, "::1") {
		t.Fatal("IPv4 地址不应命中 ip6")
	}
}

// TestSplitRawMessage 头/正文拆分与折叠行解析。
func TestSplitRawMessage(t *testing.T) {
	raw := []byte("From: a@b.com\r\nSubject: Hello\r\n World\r\n\r\nbody line\r\n")
	headers, body := splitRawMessage(raw)
	if len(headers) != 2 {
		t.Fatalf("头数量 = %d, want 2", len(headers))
	}
	if headers[1].name != "Subject" || !strings.Contains(headers[1].value, "World") {
		t.Fatalf("折叠头解析错误: %+v", headers[1])
	}
	if string(body) != "body line\r\n" {
		t.Fatalf("正文 = %q", string(body))
	}
}

// TestOutboxBackoff 退避时间递增且封顶。
func TestOutboxBackoff(t *testing.T) {
	if outboxBackoff(1) >= outboxBackoff(2) {
		t.Fatal("退避应递增")
	}
	if outboxBackoff(100) != outboxBackoff(6) {
		t.Fatal("退避应封顶在最后一步")
	}
}

// TestAliasTargetValidation 别名目标地址校验。
func TestAliasTargetValidation(t *testing.T) {
	if _, err := validateAliasTargets("a@b.com, c@d.com"); err != nil {
		t.Fatalf("合法地址被判非法: %v", err)
	}
	if _, err := validateAliasTargets("not-an-email"); err == nil {
		t.Fatal("非法地址应报错")
	}
	if _, err := validateAliasTargets("a@localhost"); err == nil {
		t.Fatal("无顶级域的地址应报错")
	}
}
