package service

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// SMTP 客户端：连接对方 MX 发信（EHLO / MAIL FROM / RCPT TO / DATA / QUIT）。

type smtpClient struct {
	conn net.Conn
	r    *bufio.Reader
	w    *bufio.Writer
	host string
}

// dialSMTP 连接 host:port（port<=0 时用 25）。
func dialSMTP(host string, port int, timeout time.Duration) (*smtpClient, error) {
	if port <= 0 {
		port = 25
	}
	addr := net.JoinHostPort(host, fmt.Sprint(port))
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	c := &smtpClient{
		conn: conn,
		r:    bufio.NewReader(conn),
		w:    bufio.NewWriter(conn),
		host: host,
	}
	code, _, err := c.readReply()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if code != 220 {
		_ = conn.Close()
		return nil, fmt.Errorf("对方服务器未就绪: %d", code)
	}
	return c, nil
}

func (c *smtpClient) close() {
	_, _ = c.w.WriteString("QUIT\r\n")
	_ = c.w.Flush()
	_ = c.conn.Close()
}

// readReply 读一行回复，兼容多行续行（"-"）
func (c *smtpClient) readReply() (int, string, error) {
	line, err := c.r.ReadString('\n')
	if err != nil {
		return 0, "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) < 3 {
		return 0, line, errors.New("无效回复")
	}
	code := 0
	fmt.Sscanf(line[:3], "%d", &code)
	for len(line) >= 4 && line[3] == '-' {
		next, err := c.r.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimRight(next, "\r\n")
		if len(line) >= 3 {
			fmt.Sscanf(line[:3], "%d", &code)
		}
	}
	return code, line, nil
}

func (c *smtpClient) cmd(format string, args ...interface{}) (int, string, error) {
	msg := fmt.Sprintf(format, args...)
	if _, err := c.w.WriteString(msg + "\r\n"); err != nil {
		return 0, "", err
	}
	if err := c.w.Flush(); err != nil {
		return 0, "", err
	}
	return c.readReply()
}

// ehlo 发送 EHLO（失败回退 HELO）
func (c *smtpClient) ehlo(fromDomain string) error {
	code, _, err := c.cmd("EHLO %s", fromDomain)
	if err != nil {
		return err
	}
	if code != 250 {
		code, _, err = c.cmd("HELO %s", fromDomain)
		if err != nil {
			return err
		}
		if code != 250 {
			return fmt.Errorf("EHLO/HELO 被拒绝: %d", code)
		}
	}
	return nil
}

// SendMail 通过此连接发送一封邮件（from + 多个收件人 + 原始报文）。
func (c *smtpClient) SendMail(from string, rcpts []string, raw []byte) error {
	code, msg, err := c.cmd("MAIL FROM:<%s>", from)
	if err != nil {
		return err
	}
	if code != 250 {
		return fmt.Errorf("MAIL FROM 被拒绝: %d %s", code, msg)
	}
	for _, rc := range rcpts {
		code, msg, err = c.cmd("RCPT TO:<%s>", rc)
		if err != nil {
			return err
		}
		if code != 250 && code != 251 {
			return fmt.Errorf("RCPT TO <%s> 被拒绝: %d %s", rc, code, msg)
		}
	}
	code, msg, err = c.cmd("DATA")
	if err != nil {
		return err
	}
	if code != 354 {
		return fmt.Errorf("DATA 被拒绝: %d %s", code, msg)
	}
	// 写入报文，做 dot-stuffing（行首 . → ..）
	if err := c.writeData(raw); err != nil {
		return err
	}
	code, msg, err = c.readReply()
	if err != nil {
		return err
	}
	if code != 250 {
		return fmt.Errorf("邮件未被接受: %d %s", code, msg)
	}
	return nil
}

// writeData 写正文并做 dot-stuffing，最后发送结束标记 .
func (c *smtpClient) writeData(raw []byte) error {
	// 规范换行 + dot-stuffing
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	for _, ln := range lines {
		if strings.HasPrefix(ln, ".") {
			ln = "." + ln
		}
		if _, err := c.w.WriteString(ln + "\r\n"); err != nil {
			return err
		}
	}
	if _, err := c.w.WriteString(".\r\n"); err != nil {
		return err
	}
	return c.w.Flush()
}

// deliverExternalToHost 尝试把邮件直发到指定 MX 主机（单次，不重试）。
func deliverExternalToHost(mxHost string, from string, rcpts []string, raw []byte, timeout time.Duration) error {
	cl, err := dialSMTP(mxHost, 25, timeout)
	if err != nil {
		return err
	}
	defer cl.close()
	fromDomain := "kypanel.local"
	if at := strings.LastIndexByte(from, '@'); at >= 0 {
		fromDomain = from[at+1:]
	}
	if err := cl.ehlo(fromDomain); err != nil {
		return err
	}
	return cl.SendMail(from, rcpts, raw)
}
