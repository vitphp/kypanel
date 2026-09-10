package service

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"kypanel/internal/model"
)

// SMTP 收信服务器：TCP 命令状态机，收到 DATA 后校验收件人并投递到 maildir。

var mailSmtpMu sync.Mutex

const mailSmtpMaxMessage = 25 * 1024 * 1024 // 单封最大 25MB

// MailSmtpConfiguredPorts 返回监听的 SMTP 端口，可用 PANEL_MAIL_SMTP_PORTS 覆盖。
func MailSmtpConfiguredPorts() []int {
	if p := os.Getenv("PANEL_MAIL_SMTP_PORTS"); p != "" {
		var out []int
		for _, s := range strings.Split(p, ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n > 0 && n < 65536 {
				out = append(out, n)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return []int{25, 2525}
}

// StartMailSmtpServer 启动 SMTP 收信服务。
func StartMailSmtpServer() {
	mailSmtpMu.Lock()
	ports := MailSmtpConfiguredPorts()
	mailSmtpMu.Unlock()
	for _, port := range ports {
		go listenMailSmtp(port)
	}
}

func listenMailSmtp(port int) {
	addr := fmt.Sprintf(":%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Warn("SMTP 收信端口监听失败", "addr", addr, "err", err)
		return
	}
	slog.Info("SMTP 收信服务已启动", "addr", addr)
	for {
		conn, err := ln.Accept()
		if err != nil {
			if isClosedNetErr(err) {
				return
			}
			continue
		}
		go handleSmtpConn(conn)
	}
}

func isClosedNetErr(err error) bool {
	return errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "use of closed network connection")
}

// smtpSession 单连接会话状态
type smtpSession struct {
	conn   net.Conn
	r      *bufio.Reader
	w      *bufio.Writer
	helo   string
	from   string
	rcpts  []string // 完整收件地址
	authed bool     // 是否已通过 AUTH
	authAs string   // 认证的账号（完整地址）
}

// handleSmtpConn 处理一条 SMTP 连接（超时保护 + 收尾关闭）
func handleSmtpConn(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(60 * time.Second))
	s := &smtpSession{
		conn: conn,
		r:    bufio.NewReader(conn),
		w:    bufio.NewWriter(conn),
	}
	s.reply(220, "kypanel SMTP ready")
	for {
		line, err := s.readLine()
		if err != nil {
			return
		}
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		cmd, arg := parseSmtpLine(line)
		verb := strings.ToUpper(cmd)
		switch verb {
		case "EHLO", "HELO":
			s.helo = arg
			s.resetEnvelope()
			if verb == "EHLO" {
				s.ehloResponse()
			} else {
				s.reply(250, "Hello "+s.helo)
			}
		case "MAIL":
			if err := s.handleMail(arg); err != nil {
				s.reply(501, err.Error())
			} else {
				s.reply(250, "OK")
			}
		case "RCPT":
			if err := s.handleRcpt(arg); err != nil {
				s.reply(550, err.Error())
			} else {
				s.reply(250, "OK")
			}
		case "DATA":
			if err := s.handleData(); err != nil {
				return
			}
		case "AUTH":
			_ = s.handleAuth(arg)
		case "RSET":
			s.resetEnvelope()
			s.reply(250, "OK")
		case "NOOP":
			s.reply(250, "OK")
		case "QUIT":
			s.reply(221, "Bye")
			return
		case "VRFY", "EXPN":
			s.reply(252, "Cannot VRFY user")
		case "HELP":
			s.reply(250, "Supported: EHLO HELO MAIL RCPT DATA RSET NOOP QUIT")
		default:
			s.reply(500, "Command unrecognized")
		}
	}
}

func (s *smtpSession) reply(code int, msg string) {
	// 多行用 "-" 连接（此处不处理，仅单行）
	msg = strings.ReplaceAll(msg, "\n", " ")
	_, _ = s.w.WriteString(fmt.Sprintf("%d %s\r\n", code, msg))
	_ = s.w.Flush()
}

func (s *smtpSession) readLine() (string, error) {
	line, err := s.r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func parseSmtpLine(line string) (cmd, arg string) {
	line = strings.TrimSpace(line)
	sp := strings.IndexByte(line, ' ')
	if sp < 0 {
		return line, ""
	}
	return line[:sp], strings.TrimSpace(line[sp+1:])
}

// ehloResponse EHLO 能力回应
func (s *smtpSession) ehloResponse() {
	lines := []string{
		"kypanel",
		"SIZE " + strconv.Itoa(mailSmtpMaxMessage),
		"8BITMIME",
		"ENHANCEDSTATUSCODES",
		"PIPELINING",
		"AUTH PLAIN LOGIN",
	}
	multi := make([]string, 0, len(lines)+1)
	for i, l := range lines {
		if i < len(lines)-1 {
			multi = append(multi, fmt.Sprintf("250-%s", l))
		} else {
			multi = append(multi, "250 "+l)
		}
	}
	_, _ = s.w.WriteString(strings.Join(multi, "\r\n") + "\r\n")
	_ = s.w.Flush()
}

func (s *smtpSession) resetEnvelope() {
	s.from = ""
	s.rcpts = nil
}

// extractBracketAddress 从 "FROM:<a@b> SIZE=..." 或 "<a@b>" 中取出邮箱
func extractBracketAddress(arg string) (string, error) {
	lt := strings.IndexByte(arg, '<')
	rt := strings.IndexByte(arg, '>')
	if lt < 0 || rt < 0 || rt < lt {
		return "", errors.New("bad syntax")
	}
	addr := strings.TrimSpace(arg[lt+1 : rt])
	if addr == "" {
		return "", errors.New("empty address")
	}
	if strings.EqualFold(addr, "postmaster") {
		addr = "postmaster"
	}
	return addr, nil
}

// handleAuth 处理 AUTH PLAIN / LOGIN
func (s *smtpSession) handleAuth(arg string) error {
	parts := strings.SplitN(strings.TrimSpace(arg), " ", 2)
	mech := strings.ToUpper(parts[0])
	switch mech {
	case "PLAIN":
		var payload string
		if len(parts) > 1 && parts[1] != "" {
			payload = parts[1]
		} else {
			s.reply(334, "")
			line, err := s.readLine()
			if err != nil {
				return err
			}
			payload = line
		}
		dec, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload))
		if err != nil {
			s.reply(501, "无效的 AUTH 数据")
			return err
		}
		// PLAIN: authzid \0 authcid \0 passwd
		fields := strings.Split(string(dec), "\x00")
		if len(fields) < 3 {
			s.reply(501, "无效的 AUTH 数据")
			return errors.New("bad plain")
		}
		return s.doAuth(fields[1], fields[2])
	case "LOGIN":
		s.reply(334, base64.StdEncoding.EncodeToString([]byte("Username:")))
		userLine, err := s.readLine()
		if err != nil {
			return err
		}
		ub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(userLine))
		if err != nil {
			s.reply(501, "无效用户名")
			return err
		}
		s.reply(334, base64.StdEncoding.EncodeToString([]byte("Password:")))
		passLine, err := s.readLine()
		if err != nil {
			return err
		}
		pb, err := base64.StdEncoding.DecodeString(strings.TrimSpace(passLine))
		if err != nil {
			s.reply(501, "无效密码")
			return err
		}
		return s.doAuth(string(ub), string(pb))
	default:
		s.reply(504, "不支持的认证方式")
		return errors.New("unsupported mech")
	}
}

// doAuth 用 bcrypt 校验账号密码
func (s *smtpSession) doAuth(user, pass string) error {
	user = strings.TrimSpace(user)
	if !strings.Contains(user, "@") {
		s.reply(535, "认证失败：用户名需为完整邮箱地址")
		return errors.New("bad user")
	}
	domain, name := splitLocalAddress(strings.ToLower(user))
	var box model.Mailbox
	if err := model.DB.Where("domain = ? AND name = ? AND enabled = ?", domain, name, true).First(&box).Error; err != nil {
		s.reply(535, "认证失败")
		return errors.New("no such user")
	}
	if bcrypt.CompareHashAndPassword([]byte(box.PasswordHash), []byte(pass)) != nil {
		s.reply(535, "认证失败：密码错误")
		return errors.New("bad password")
	}
	s.authed = true
	s.authAs = box.Address()
	s.reply(235, "认证成功")
	return nil
}

func (s *smtpSession) handleMail(arg string) error {
	addr, err := extractBracketAddress(arg)
	if err != nil {
		return err
	}
	s.from = strings.ToLower(addr)
	s.rcpts = nil
	return nil
}

func (s *smtpSession) handleRcpt(arg string) error {
	addr, err := extractBracketAddress(arg)
	if err != nil {
		return err
	}
	addr = strings.ToLower(addr)
	// 校验收件人是本站域名账号，否则拒收（防开放中继 & 无法投递）
	domain, user := splitLocalAddress(addr)
	if domain == "" || user == "" {
		return errors.New("invalid recipient")
	}
	if !mailboxExists(domain, user) {
		return errors.New("user unknown (" + addr + ")")
	}
	s.rcpts = append(s.rcpts, addr)
	return nil
}

// splitLocalAddress 拆分 name@domain；非本站返回空
func splitLocalAddress(addr string) (domain, user string) {
	at := strings.LastIndexByte(addr, '@')
	if at <= 0 || at == len(addr)-1 {
		return "", ""
	}
	return strings.ToLower(addr[at+1:]), strings.ToLower(addr[:at])
}

// mailboxExists 校验某域 + 用户是否为本站有效账号
func mailboxExists(domain, user string) bool {
	var cnt int64
	model.DB.Model(&model.Mailbox{}).
		Where("domain = ? AND name = ? AND enabled = ?", domain, user, true).Count(&cnt)
	return cnt > 0
}

// handleData 读取正文到 "."，投递给所有收件人
func (s *smtpSession) handleData() error {
	if len(s.rcpts) == 0 {
		s.reply(503, "Need RCPT first")
		return nil
	}
	s.reply(354, "End data with <CR><LF>.<CR><LF>")
	var buf bytes.Buffer
	var total int
	for {
		line, err := s.readLine()
		if err != nil {
			if err == io.EOF {
				return err
			}
			return err
		}
		if line == "." {
			break
		}
		// dot-unstuffing：行首 ".." → "."
		if strings.HasPrefix(line, "..") {
			line = line[1:]
		}
		buf.WriteString(line + "\r\n")
		total += len(line) + 2
		if total > mailSmtpMaxMessage {
			s.reply(552, "Message size exceeds limit")
			return nil
		}
	}
	raw := buf.Bytes()
	// 给纯收件但缺 From 的补默认
	if s.from == "" {
		s.from = "postmaster@localhost"
	}
	// 投递给每个收件人
	delivered := 0
	var firstErr error
	for _, rcpt := range s.rcpts {
		domain, user := splitLocalAddress(rcpt)
		if _, err := DeliverMessage(domain, user, raw); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			slog.Warn("SMTP 投递失败", "rcpt", rcpt, "err", err)
			continue
		}
		delivered++
	}
	s.resetEnvelope()
	if delivered > 0 {
		s.reply(250, "OK: queued as "+randomPassword(10))
		return nil
	}
	if firstErr == nil {
		firstErr = errors.New("no valid recipient")
	}
	s.reply(550, firstErr.Error())
	return nil
}
