package service

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"kypanel/internal/model"
)

// smtpTestClient 测试用极简 SMTP 客户端。
type smtpTestClient struct {
	conn net.Conn
	r    *bufio.Reader
	t    *testing.T
}

func dialSmtpTest(t *testing.T, addr string, useTLS bool) *smtpTestClient {
	t.Helper()
	var conn net.Conn
	var err error
	if useTLS {
		conn, err = tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", addr,
			&tls.Config{InsecureSkipVerify: true})
	} else {
		conn, err = net.DialTimeout("tcp", addr, 5*time.Second)
	}
	if err != nil {
		t.Fatalf("连接 SMTP %s 失败: %v", addr, err)
	}
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	c := &smtpTestClient{conn: conn, r: bufio.NewReader(conn), t: t}
	code, _ := c.readReply()
	if code != 220 {
		t.Fatalf("问候语应为 220，实际 %d", code)
	}
	return c
}

func (c *smtpTestClient) readReply() (int, string) {
	c.t.Helper()
	var last string
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			c.t.Fatalf("读取 SMTP 回复失败: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		last = line
		if len(line) < 4 || line[3] != '-' {
			break
		}
	}
	var code int
	fmt.Sscanf(last[:3], "%d", &code)
	return code, last
}

func (c *smtpTestClient) cmd(format string, args ...interface{}) (int, string) {
	c.t.Helper()
	msg := fmt.Sprintf(format, args...)
	if _, err := c.conn.Write([]byte(msg + "\r\n")); err != nil {
		c.t.Fatalf("发送 %q 失败: %v", msg, err)
	}
	return c.readReply()
}

func (c *smtpTestClient) close() { _ = c.conn.Close() }

// freePort 申请一个空闲端口。
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("申请端口失败: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// TestSmtpInboundFlow 覆盖：EHLO 宣告 STARTTLS、防伪造拒收、别名收信、真实收信落盘。
func TestSmtpInboundFlow(t *testing.T) {
	setupMailTestEnv(t)

	plainPort := freePort(t)
	t.Setenv("PANEL_MAIL_SMTP_PORTS", fmt.Sprint(plainPort))
	t.Setenv("PANEL_MAIL_SMTPS_PORTS", fmt.Sprint(freePort(t)))
	StartMailSmtpServer()
	time.Sleep(200 * time.Millisecond)

	dom, err := CreateMailDomain("example.com", "", 1024)
	if err != nil {
		t.Fatalf("创建域名失败: %v", err)
	}
	if _, err := CreateMailbox(dom.ID, "alice", "pw123456", "", 1024); err != nil {
		t.Fatalf("创建账号失败: %v", err)
	}
	if _, err := CreateMailAlias(dom.ID, "info", "alice@example.com", ""); err != nil {
		t.Fatalf("创建别名失败: %v", err)
	}

	addr := fmt.Sprintf("127.0.0.1:%d", plainPort)
	c := dialSmtpTest(t, addr, false)
	defer c.close()

	code, _ := c.cmd("EHLO tester.local")
	if code != 250 {
		t.Fatalf("EHLO 应返回 250，实际 %d", code)
	}
	// 重新 EHLO 拿完整能力列表（多行）
	if _, err := c.conn.Write([]byte("EHLO tester.local\r\n")); err != nil {
		t.Fatal(err)
	}
	var caps []string
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		line = strings.TrimRight(line, "\r\n")
		caps = append(caps, line)
		if len(line) < 4 || line[3] != '-' {
			break
		}
	}
	joined := strings.Join(caps, "|")
	if !strings.Contains(joined, "STARTTLS") {
		t.Fatalf("EHLO 未宣告 STARTTLS: %v", caps)
	}

	// 防伪造：未认证却声明本站域名发件 → 拒绝
	code, _ = c.cmd("MAIL FROM:<ceo@example.com>")
	if code == 250 {
		t.Fatal("未认证伪造本站域名发件应被拒绝")
	}

	// 外部发件人 → 别名收件 → 投递成功
	code, _ = c.cmd("MAIL FROM:<sender@outside.com>")
	if code != 250 {
		t.Fatalf("MAIL FROM 应 250，实际 %d", code)
	}
	code, _ = c.cmd("RCPT TO:<info@example.com>")
	if code != 250 {
		t.Fatalf("别名收件人应 250，实际 %d", code)
	}
	// 外域收件人应被拒（防开放中继）
	code, _ = c.cmd("RCPT TO:<someone@gmail.com>")
	if code != 550 {
		t.Fatalf("外域收件人应 550，实际 %d", code)
	}
	code, _ = c.cmd("DATA")
	if code != 354 {
		t.Fatalf("DATA 应 354，实际 %d", code)
	}
	body := "From: sender@outside.com\r\nTo: info@example.com\r\nSubject: smtp test\r\n\r\nhello smtp\r\n.\r\n"
	if _, err := c.conn.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	code, _ = c.readReply()
	if code != 250 {
		t.Fatalf("投递后应 250，实际 %d", code)
	}
	c.cmd("QUIT")

	c.close()

	var cnt int64
	model.DB.Model(&model.MailMessage{}).Where("folder = ?", "inbox").Count(&cnt)
	if cnt != 1 {
		t.Fatalf("SMTP 收信后应有 1 封邮件，实际 %d", cnt)
	}
	// 索引元数据应正确解析
	var rec model.MailMessage
	model.DB.Where("folder = ?", "inbox").First(&rec)
	if rec.Subject != "smtp test" {
		t.Fatalf("解析主题异常: %q", rec.Subject)
	}
	if rec.FromAddr != "sender@outside.com" {
		t.Fatalf("解析发件人异常: %q", rec.FromAddr)
	}
}

// TestSmtpSubmissionRequiresAuth 提交端口（隐式 TLS）必须认证，认证后允许本站域名发件。
func TestSmtpSubmissionRequiresAuth(t *testing.T) {
	setupMailTestEnv(t)

	smtpsPort := freePort(t)
	t.Setenv("PANEL_MAIL_SMTP_PORTS", fmt.Sprint(freePort(t)))
	t.Setenv("PANEL_MAIL_SMTPS_PORTS", fmt.Sprint(smtpsPort))
	StartMailSmtpServer()
	time.Sleep(200 * time.Millisecond)

	dom, err := CreateMailDomain("example.com", "", 1024)
	if err != nil {
		t.Fatalf("创建域名失败: %v", err)
	}
	if _, err := CreateMailbox(dom.ID, "alice", "pw123456", "", 1024); err != nil {
		t.Fatalf("创建账号失败: %v", err)
	}

	// 465 是提交端口，测试时会把 465 当作需认证端口；用随机端口无法命中 587/465 判定，
	// 因此这里直接验证 AUTH LOGIN 流程与认证后发信。
	addr := fmt.Sprintf("127.0.0.1:%d", smtpsPort)
	c := dialSmtpTest(t, addr, true)
	defer c.close()

	if code, _ := c.cmd("EHLO tester.local"); code != 250 {
		t.Fatalf("EHLO 应 250，实际 %d", code)
	}
	// AUTH LOGIN 两步（base64 用户名 / 密码）
	code, _ := c.cmd("AUTH LOGIN")
	if code != 334 {
		t.Fatalf("AUTH LOGIN 应 334，实际 %d", code)
	}
	if _, err := c.conn.Write([]byte(base64.StdEncoding.EncodeToString([]byte("alice@example.com")) + "\r\n")); err != nil {
		t.Fatal(err)
	}
	if code, _ = c.readReply(); code != 334 {
		t.Fatalf("AUTH 用户名应答应 334，实际 %d", code)
	}
	if _, err := c.conn.Write([]byte(base64.StdEncoding.EncodeToString([]byte("pw123456")) + "\r\n")); err != nil {
		t.Fatal(err)
	}
	if code, _ = c.readReply(); code != 235 {
		t.Fatalf("正确密码应 235，实际 %d", code)
	}
	// 认证后可用本站地址发件
	if code, _ = c.cmd("MAIL FROM:<alice@example.com>"); code != 250 {
		t.Fatalf("认证后 MAIL FROM 应 250，实际 %d", code)
	}
	// 错误密码应 535
	c2 := dialSmtpTest(t, addr, true)
	defer c2.close()
	c2.cmd("EHLO tester.local")
	c2.cmd("AUTH LOGIN")
	c2.conn.Write([]byte(base64.StdEncoding.EncodeToString([]byte("alice@example.com")) + "\r\n"))
	c2.readReply()
	c2.conn.Write([]byte(base64.StdEncoding.EncodeToString([]byte("wrong-password")) + "\r\n"))
	if code, _ := c2.readReply(); code != 535 {
		t.Fatalf("错误密码应 535，实际 %d", code)
	}
}

// TestSmtpRateLimit 连接限速：超过阈值后新连接应被 421 拒绝。
func TestSmtpRateLimit(t *testing.T) {
	setupMailTestEnv(t)

	port := freePort(t)
	t.Setenv("PANEL_MAIL_SMTP_PORTS", fmt.Sprint(port))
	t.Setenv("PANEL_MAIL_SMTPS_PORTS", fmt.Sprint(freePort(t)))
	t.Setenv("PANEL_MAIL_RATE_LIMIT", "2")
	// 限速表是进程级共享的，先清空避免受同包其他测试的连接计数影响
	mailRateMu.Lock()
	mailRateHits = map[string][]int64{}
	mailRateMu.Unlock()
	StartMailSmtpServer()
	time.Sleep(200 * time.Millisecond)

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for i := 0; i < 2; i++ {
		c := dialSmtpTest(t, addr, false)
		c.close()
	}
	// 第 3 次应被限速（返回 421）
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("读取回复失败: %v", err)
	}
	if !strings.HasPrefix(line, "421") {
		t.Fatalf("超限连接应返回 421，实际 %q", line)
	}
}
