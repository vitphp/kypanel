package service

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"kypanel/internal/config"
	"kypanel/internal/model"
)

// SMTP 收信服务器：TCP（可选 STARTTLS / 隐式 TLS）命令状态机，
// 收到 DATA 后做 SPF/DKIM 校验与收件人校验，再投递到 maildir（支持别名）。

var mailSmtpMu sync.Mutex

const mailSmtpMaxMessage = 25 * 1024 * 1024 // 单封最大 25MB

// mailPlainPorts 明文（支持 STARTTLS）监听端口：25 收信、587 提交、2525 备用。
func mailPlainPorts() []int {
	return mailPortsFromEnv("PANEL_MAIL_SMTP_PORTS", []int{25, 587, 2525})
}

// mailImplicitTLSPorts 隐式 TLS（SMTPS）监听端口：465。
func mailImplicitTLSPorts() []int {
	return mailPortsFromEnv("PANEL_MAIL_SMTPS_PORTS", []int{465})
}

// mailPortsFromEnv 读取端口列表环境变量（逗号分隔），非法则用默认。
func mailPortsFromEnv(env string, def []int) []int {
	if p := strings.TrimSpace(os.Getenv(env)); p != "" {
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
	return def
}

// MailSmtpStatus 邮件服务运行状态（供面板展示）。
type MailSmtpStatus struct {
	PlainPorts    []int  `json:"plain_ports"`    // 明文端口（EHLO 宣告 STARTTLS）
	SMTPSPorts    []int  `json:"smtps_ports"`    // 隐式 TLS 端口
	SubmissionReq []int  `json:"submission_ports"` // 要求认证的端口（587/465）
	TLSReady      bool   `json:"tls_ready"`      // 是否具备 TLS 证书
	TLSSource     string `json:"tls_source"`     // 证书来源：panel / self-signed
}

// GetMailSmtpStatus 返回当前 SMTP 服务状态。
func GetMailSmtpStatus() MailSmtpStatus {
	plain := mailPlainPorts()
	smtps := mailImplicitTLSPorts()
	st := MailSmtpStatus{PlainPorts: plain, SMTPSPorts: smtps}
	// 只把配置里"确实按提交端口语义启动"的端口标出来（587/465）
	for _, p := range append(append([]int{}, plain...), smtps...) {
		if isSubmissionPort(p) {
			st.SubmissionReq = append(st.SubmissionReq, p)
		}
	}
	if st.SubmissionReq == nil {
		st.SubmissionReq = []int{}
	}
	if c := mailTLSCertificate(); c != nil {
		st.TLSReady = true
		st.TLSSource = mailTLSSourceName()
	}
	return st
}

// StartMailSmtpServer 启动 SMTP 收信服务（明文端口 + 隐式 TLS 端口）。
func StartMailSmtpServer() {
	mailSmtpMu.Lock()
	plain := mailPlainPorts()
	smtps := mailImplicitTLSPorts()
	mailSmtpMu.Unlock()
	for _, port := range plain {
		go listenMailSmtp(port, false)
	}
	for _, port := range smtps {
		go listenMailSmtp(port, true)
	}
	StartMailRateJanitor()
	StartMailGreylistJanitor()
}

// isSubmissionPort 判断是否要求认证的提交端口。
func isSubmissionPort(port int) bool {
	return port == 587 || port == 465
}

func listenMailSmtp(port int, implicitTLS bool) {
	addr := fmt.Sprintf(":%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Warn("SMTP 端口监听失败", "addr", addr, "err", err)
		return
	}
	mode := "明文/STARTTLS"
	if implicitTLS {
		mode = "隐式TLS"
	}
	slog.Info("SMTP 收信服务已启动", "addr", addr, "mode", mode)
	for {
		conn, err := ln.Accept()
		if err != nil {
			if isClosedNetErr(err) {
				return
			}
			continue
		}
		go handleSmtpConn(conn, implicitTLS, isSubmissionPort(port))
	}
}

// isClosedNetErr 判断监听套接字是否已被关闭。
func isClosedNetErr(err error) bool {
	return errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "use of closed network connection")
}

// smtpSession 单连接会话状态
type smtpSession struct {
	conn        net.Conn
	r           *bufio.Reader
	w           *bufio.Writer
	helo        string
	from        string
	rcpts       []string // 完整收件地址
	authed      bool     // 是否已通过 AUTH
	authAs      string   // 认证的账号（完整地址）
	tlsActive   bool     // 当前连接是否已加密
	remoteIP    string   // 来源 IP（限速/SPF）
	requireAuth bool     // 提交端口要求认证
}

// handleSmtpConn 处理一条 SMTP 连接（隐式 TLS / 限速 / 超时保护）。
func handleSmtpConn(conn net.Conn, implicitTLS, requireAuth bool) {
	defer conn.Close()
	remoteIP := remoteHost(conn)
	if !AllowMailConnection(remoteIP) {
		_, _ = conn.Write([]byte("421 请求过于频繁，请稍后再试\r\n"))
		slog.Warn("SMTP 连接被限速拒绝", "ip", remoteIP)
		return
	}
	if implicitTLS {
		cert := mailTLSCertificate()
		if cert == nil {
			_, _ = conn.Write([]byte("421 TLS 不可用\r\n"))
			return
		}
		tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{*cert}})
		_ = tlsConn.SetDeadline(time.Now().Add(30 * time.Second))
		if err := tlsConn.Handshake(); err != nil {
			slog.Warn("SMTP 隐式 TLS 握手失败", "ip", remoteIP, "err", err)
			return
		}
		conn = tlsConn
	}
	_ = conn.SetDeadline(time.Now().Add(120 * time.Second))
	s := &smtpSession{
		conn:        conn,
		r:           bufio.NewReader(conn),
		w:           bufio.NewWriter(conn),
		remoteIP:    remoteIP,
		tlsActive:   implicitTLS,
		requireAuth: requireAuth,
	}
	s.reply(220, "kypanel SMTP ready")
	for {
		line, err := s.readLine()
		if err != nil {
			return
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
		case "STARTTLS":
			if err := s.handleStartTLS(); err != nil {
				return
			}
		case "MAIL":
			if err := s.handleMail(arg); err != nil {
				s.reply(501, err.Error())
			} else {
				s.reply(250, "OK")
			}
		case "RCPT":
			if code, msg := s.handleRcpt(arg); code != 0 {
				s.reply(code, msg)
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
			s.reply(250, "Supported: EHLO HELO STARTTLS MAIL RCPT DATA AUTH RSET NOOP QUIT")
		default:
			s.reply(500, "Command unrecognized")
		}
	}
}

// remoteHost 取连接来源 IP（去掉端口）。
func remoteHost(conn net.Conn) string {
	if addr, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
		return addr.IP.String()
	}
	host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return conn.RemoteAddr().String()
	}
	return host
}

func (s *smtpSession) reply(code int, msg string) {
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

// ehloResponse EHLO 能力回应（未加密且证书可用时宣告 STARTTLS）。
func (s *smtpSession) ehloResponse() {
	lines := []string{
		"kypanel",
		"SIZE " + strconv.Itoa(mailSmtpMaxMessage),
		"8BITMIME",
		"ENHANCEDSTATUSCODES",
		"PIPELINING",
	}
	if !s.tlsActive && mailTLSCertificate() != nil {
		lines = append(lines, "STARTTLS")
	}
	lines = append(lines, "AUTH PLAIN LOGIN")
	multi := make([]string, 0, len(lines))
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

// handleStartTLS 升级为 TLS 连接（升级成功后重置会话状态）。
func (s *smtpSession) handleStartTLS() error {
	if s.tlsActive {
		s.reply(503, "TLS 已启用")
		return nil
	}
	cert := mailTLSCertificate()
	if cert == nil {
		s.reply(454, "TLS 暂不可用")
		return nil
	}
	s.reply(220, "Ready to start TLS")
	tlsConn := tls.Server(s.conn, &tls.Config{Certificates: []tls.Certificate{*cert}})
	_ = tlsConn.SetDeadline(time.Now().Add(30 * time.Second))
	if err := tlsConn.Handshake(); err != nil {
		slog.Warn("SMTP STARTTLS 握手失败", "ip", s.remoteIP, "err", err)
		return err
	}
	_ = tlsConn.SetDeadline(time.Now().Add(120 * time.Second))
	s.conn = tlsConn
	s.r = bufio.NewReader(tlsConn)
	s.w = bufio.NewWriter(tlsConn)
	s.tlsActive = true
	s.authed = false
	s.authAs = ""
	s.helo = ""
	s.resetEnvelope()
	return nil
}

func (s *smtpSession) resetEnvelope() {
	s.from = ""
	s.rcpts = nil
}

// extractBracketAddress 从 "FROM:<a@b> SIZE=..." 或 "<a@b>" 中取出邮箱。
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

// handleMail 处理 MAIL FROM（含防伪造与提交端口认证要求）。
func (s *smtpSession) handleMail(arg string) error {
	// 退信使用空地址 MAIL FROM:<>
	if strings.Contains(arg, "<>") {
		if s.requireAuth && !s.authed {
			return errors.New("该端口需认证后发信")
		}
		s.from = ""
		s.rcpts = nil
		return nil
	}
	addr, err := extractBracketAddress(arg)
	if err != nil {
		return err
	}
	addr = strings.ToLower(addr)
	// 提交端口（587/465）必须认证
	if s.requireAuth && !s.authed {
		return errors.New("该端口需认证后发信")
	}
	// 防伪造：声明为本站邮箱域名的发件人必须已认证，否则拒绝（避免冒充内部用户）
	if fromDomain := domainOf(addr); fromDomain != "" && isLocalMailDomain(fromDomain) && !s.authed {
		RecordMailLog("in", fromDomain, 0, addr, strings.Join(s.rcpts, ", "), "",
			0, "reject", "伪造本站域名发件人（未认证）", s.remoteIP)
		return errors.New("禁止伪造本站域名发件人（请认证后再发信）")
	}
	s.from = addr
	s.rcpts = nil
	return nil
}

// handleRcpt 处理 RCPT TO：校验可投递收件人 + 灰名单延迟。
// 返回 (code,msg)：code==0 表示接受（由调用方回 250）。
func (s *smtpSession) handleRcpt(arg string) (int, string) {
	addr, err := extractBracketAddress(arg)
	if err != nil {
		return 501, err.Error()
	}
	addr = strings.ToLower(addr)
	domain, user := splitLocalAddress(addr)
	if domain == "" || user == "" {
		return 501, "invalid recipient"
	}
	// 只接受本站域名（防开放中继）
	if !isLocalMailDomain(domain) {
		return 550, "relay denied（本服务器不转发外域收件人）"
	}
	if !mailboxExists(domain, user) {
		return 550, "user unknown (" + addr + ")"
	}
	if !s.authed && GreylistDefer(s.remoteIP, s.from, addr) {
		return 451, "灰名单：请稍后重试（首次来信延迟校验）"
	}
	s.rcpts = append(s.rcpts, addr)
	return 0, ""
}

// splitLocalAddress 拆分 name@domain；非本站返回空
func splitLocalAddress(addr string) (domain, user string) {
	at := strings.LastIndexByte(addr, '@')
	if at <= 0 || at == len(addr)-1 {
		return "", ""
	}
	return strings.ToLower(addr[at+1:]), strings.ToLower(addr[:at])
}

// handleData 读取正文到 "."，做安全检查后投递给所有收件人。
func (s *smtpSession) handleData() error {
	if len(s.rcpts) == 0 {
		s.reply(503, "Need RCPT first")
		return nil
	}
	s.reply(354, "End data with <CR><LF>.<CR><LF>")
	var buf bytes.Buffer
	var total int64
	for {
		line, err := s.readLine()
		if err != nil {
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
		total += int64(len(line) + 2)
		if total > mailSmtpMaxMessage {
			s.reply(552, "Message size exceeds limit")
			return nil
		}
	}
	raw := buf.Bytes()
	// 未经认证的外部来信：SPF + DKIM 校验，并注入 Authentication-Results 头
	if !s.authed {
		spf := CheckSPF(net.ParseIP(s.remoteIP), s.helo, s.from)
		if spf == spfFail {
			RecordMailLog("in", domainOf(s.from), 0, s.from, strings.Join(s.rcpts, ", "),
				"", total, "reject", "SPF 校验失败（-all）", s.remoteIP)
			slog.Warn("SPF 硬失败，已拒收", "from", s.from, "ip", s.remoteIP)
			s.reply(550, "SPF check failed")
			return nil
		}
		dkim := VerifyDKIM(raw)
		raw = injectAuthResults(raw, s.remoteIP, string(spf), dkim, s.from)
	}

	delivered := 0
	var firstErr error
	for _, rcpt := range s.rcpts {
		domain, user := splitLocalAddress(rcpt)
		if err := DeliverInbound(s.from, domain, user, raw); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			slog.Warn("SMTP 投递失败", "rcpt", rcpt, "err", err)
			RecordMailLog("in", domain, 0, s.from, rcpt, "", total, "failed", err.Error(), s.remoteIP)
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

// injectAuthResults 在报文头部注入 Authentication-Results（供 WebMail / 下游判断）。
func injectAuthResults(raw []byte, ip, spf, dkim, mailFrom string) []byte {
	if spf == "" && dkim == "" {
		return raw
	}
	host := "kypanel"
	if h := detectMailServerHostname(); h != "" {
		host = h
	}
	var sb strings.Builder
	sb.WriteString("Authentication-Results: " + host + ";\r\n")
	if spf != "" {
		sb.WriteString("  spf=" + spf)
		if mailFrom != "" {
			sb.WriteString(" smtp.mailfrom=" + mailFrom)
		}
		sb.WriteString(";\r\n")
	}
	if dkim != "" {
		sb.WriteString("  dkim=" + dkim + ";\r\n")
	}
	sb.WriteString("  x-source-ip=" + ip + "\r\n")
	return append([]byte(sb.String()), raw...)
}

// ===== TLS 证书（优先面板证书，否则自签并缓存）=====

var (
	mailTLSCertOnce sync.Once
	mailTLSCertVal  *tls.Certificate
	mailTLSSource   string
)

func mailTLSSourceName() string {
	_ = mailTLSCertificate()
	return mailTLSSource
}

// mailTLSCertificate 返回 SMTP 用 TLS 证书（首次调用时加载或生成）。
func mailTLSCertificate() *tls.Certificate {
	mailTLSCertOnce.Do(func() {
		cfg := config.Get()
		if cfg.Server.CertFile != "" && cfg.Server.KeyFile != "" {
			if c, err := tls.LoadX509KeyPair(cfg.Server.CertFile, cfg.Server.KeyFile); err == nil {
				mailTLSCertVal = &c
				mailTLSSource = "panel"
				return
			}
		}
		dir := filepath.Join(cfg.DataDir, "mail", "tls")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			slog.Warn("创建 SMTP 证书目录失败", "err", err)
			return
		}
		certPath := filepath.Join(dir, "cert.pem")
		keyPath := filepath.Join(dir, "key.pem")
		if c, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
			mailTLSCertVal = &c
			mailTLSSource = "self-signed"
			return
		}
		c, err := generateSelfSignedMailCert(certPath, keyPath)
		if err != nil {
			slog.Warn("生成 SMTP 自签证书失败", "err", err)
			return
		}
		mailTLSCertVal = &c
		mailTLSSource = "self-signed"
	})
	return mailTLSCertVal
}

// generateSelfSignedMailCert 生成自签证书并落盘（10 年有效）。
func generateSelfSignedMailCert(certPath, keyPath string) (tls.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}
	host := detectMailServerHostname()
	if host == "" {
		host = "kypanel.local"
	}
	dnsNames := []string{host, "localhost", "mail.local"}
	if d := strings.TrimSpace(config.Get().Server.Domain); d != "" {
		dnsNames = append(dnsNames, d, "mail."+d)
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: host, Organization: []string{"kypanel SMTP"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(certPEM, keyPEM)
}
