package service

import (
	"crypto/rsa"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kypanel/internal/config"
	"kypanel/internal/model"
	"kypanel/internal/utils"
)

// setupMailTestEnv 初始化临时数据库与数据目录，返回清理函数。
func setupMailTestEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "panel.db")
	if err := model.Init(dbPath); err != nil {
		t.Fatalf("初始化数据库失败: %v", err)
	}
	cfg := config.Get()
	cfg.DataDir = dir
	utils.InitCrypto("test-secret-key")
	t.Cleanup(func() {
		if sqlDB, err := model.DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return dir
}

// rawTestMail 构造一封通用测试邮件。
func rawTestMail(subject, body string) []byte {
	return buildMimeMessage(MailContent{
		FromName: "Outside Sender",
		FromAddr: "sender@outside.com",
		ToAddrs:  []string{"alice@example.com"},
		Subject:  subject,
		TextBody: body,
	})
}

// TestMailInboundAliasQuota 覆盖收信落盘、域名开关、别名转发与容量上限。
func TestMailInboundAliasQuota(t *testing.T) {
	setupMailTestEnv(t)

	dom, err := CreateMailDomain("example.com", "", 1024)
	if err != nil {
		t.Fatalf("创建域名失败: %v", err)
	}
	if _, err := CreateMailbox(dom.ID, "alice", "pw123456", "", 1); err != nil {
		t.Fatalf("创建账号失败: %v", err)
	}
	if _, err := CreateMailAlias(dom.ID, "info", "alice@example.com", ""); err != nil {
		t.Fatalf("创建别名失败: %v", err)
	}

	// 1) 直接投递给真实账号
	if err := DeliverInbound("sender@outside.com", "example.com", "alice", rawTestMail("hi", "hello")); err != nil {
		t.Fatalf("投递失败: %v", err)
	}
	var got int64
	model.DB.Model(&model.MailMessage{}).Count(&got)
	if got != 1 {
		t.Fatalf("邮件数 = %d, want 1", got)
	}
	var box model.Mailbox
	model.DB.Where("name = ?", "alice").First(&box)
	if box.StorageUsed <= 0 {
		t.Fatalf("收信后 storage_used 应大于 0，实际 %d", box.StorageUsed)
	}

	// 2) 投递给别名 → 应落到 alice
	if err := DeliverInbound("sender@outside.com", "example.com", "info", rawTestMail("via alias", "hello")); err != nil {
		t.Fatalf("别名投递失败: %v", err)
	}
	model.DB.Model(&model.MailMessage{}).Count(&got)
	if got != 2 {
		t.Fatalf("别名投递后邮件数 = %d, want 2", got)
	}

	// 3) 未知名收件人应失败
	if err := DeliverInbound("sender@outside.com", "example.com", "nobody", rawTestMail("x", "y")); err == nil {
		t.Fatal("投递给不存在账号应失败")
	}

	// 4) 域名停用后不再收信
	if err := UpdateMailDomain(dom.ID, map[string]interface{}{"enabled": false}); err != nil {
		t.Fatalf("停用域名失败: %v", err)
	}
	if err := DeliverInbound("sender@outside.com", "example.com", "alice", rawTestMail("x", "y")); err == nil {
		t.Fatal("域名停用后应拒收")
	}
	_ = UpdateMailDomain(dom.ID, map[string]interface{}{"enabled": true})

	// 5) 容量上限：账号定义 1MB，投一封 1.2MB 的信应被拒
	big := buildMimeMessage(MailContent{
		FromName: "Big", FromAddr: "sender@outside.com",
		ToAddrs: []string{"alice@example.com"}, Subject: "big",
		TextBody: strings.Repeat("x", 1200*1024),
	})
	if err := DeliverInbound("sender@outside.com", "example.com", "alice", big); err == nil {
		t.Fatal("超出容量上限应被拒收")
	}
}

// TestMailSendDkimAndOutbox 覆盖发信：站内直投 + 外域入队列 + DKIM 签名可被校验。
func TestMailSendDkimAndOutbox(t *testing.T) {
	setupMailTestEnv(t)

	dom, _ := CreateMailDomain("example.com", "", 1024)
	// 生成 DKIM 密钥
	selector, txt, err := EnsureMailDkim(dom.ID)
	if err != nil {
		t.Fatalf("生成 DKIM 失败: %v", err)
	}
	if !strings.Contains(txt, "v=DKIM1") || !strings.Contains(txt, "p=") {
		t.Fatalf("DKIM 公钥格式不正确: %s", txt)
	}
	if selector == "" {
		t.Fatal("selector 不应为空")
	}
	box, err := CreateMailbox(dom.ID, "alice", "pw123456", "", 1024)
	if err != nil {
		t.Fatalf("创建账号失败: %v", err)
	}
	if _, err := CreateMailbox(dom.ID, "bob", "pw123456", "", 1024); err != nil {
		t.Fatalf("创建账号失败: %v", err)
	}

	res, err := SendMail(SendMailRequest{
		MailboxID: box.ID,
		To:        []string{"bob@example.com", "friend@gmail.com"},
		Subject:   "Hello DKIM",
		TextBody:  "body",
	})
	if err != nil {
		t.Fatalf("发信失败: %v", err)
	}
	if len(res.Local) != 1 || res.Local[0] != "bob@example.com" {
		t.Fatalf("站内投递结果异常: %+v", res.Local)
	}
	if len(res.Queued) != 1 || res.Queued[0] != "friend@gmail.com" {
		t.Fatalf("外域应进入队列: %+v", res.Queued)
	}

	// 队列落库 + 报文落盘
	var item model.MailOutbox
	if err := model.DB.Where("status = ?", "pending").First(&item).Error; err != nil {
		t.Fatalf("外发队列未写入: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(mailOutboxDir(), item.Filename))
	if err != nil {
		t.Fatalf("队列报文文件缺失: %v", err)
	}

	// 用该域名的真实私钥校验签名（注入公钥查询钩子，避免依赖 DNS）
	var domRec model.MailDomain
	model.DB.First(&domRec, dom.ID)
	priv := dkimLoadPrivateKey(domRec)
	if priv == nil {
		t.Fatal("未能加载域名私钥")
	}
	origHook := dkimPubKeyLookup
	dkimPubKeyLookup = func(s, d string) *rsa.PublicKey {
		if d == "example.com" {
			return &priv.PublicKey
		}
		return nil
	}
	defer func() { dkimPubKeyLookup = origHook }()

	if got := VerifyDKIM(raw); got != "pass" {
		t.Fatalf("队列报文的 DKIM 签名校验应为 pass，实际 %s", got)
	}

	// 已发送副本也应写入（folder=sent）
	var sent int64
	model.DB.Model(&model.MailMessage{}).Where("mailbox_id = ? AND folder = ?", box.ID, "sent").Count(&sent)
	if sent != 1 {
		t.Fatalf("已发送副本数 = %d, want 1", sent)
	}
}

// TestMailAutoReply 自动回复应异步发出并进入外发队列（发件人为外域）。
func TestMailAutoReply(t *testing.T) {
	setupMailTestEnv(t)

	dom, _ := CreateMailDomain("example.com", "", 1024)
	box, _ := CreateMailbox(dom.ID, "alice", "pw123456", "", 1024)
	if err := UpdateMailboxSettings(box.ID, MailboxSettingsReq{
		AutoReplyOn:   boolPtr(true),
		AutoReplyText: strPtr("我暂时不在，稍后回复。"),
	}); err != nil {
		t.Fatalf("设置自动回复失败: %v", err)
	}

	if err := DeliverInbound("sender@outside.com", "example.com", "alice", rawTestMail("hi", "hello")); err != nil {
		t.Fatalf("投递失败: %v", err)
	}

	// 自动回复是异步的，轮询等待队列出现回复邮件
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var cnt int64
		model.DB.Model(&model.MailOutbox{}).
			Where("from_addr = ?", box.Address()).Count(&cnt)
		if cnt > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("自动回复未进入外发队列")
}

// TestMailDeleteCleanup 删除账号应清理索引、maildir 与容量。
func TestMailDeleteCleanup(t *testing.T) {
	setupMailTestEnv(t)

	dom, _ := CreateMailDomain("example.com", "", 1024)
	box, _ := CreateMailbox(dom.ID, "alice", "pw123456", "", 1024)
	if err := DeliverInbound("sender@outside.com", "example.com", "alice", rawTestMail("hi", "hello")); err != nil {
		t.Fatalf("投递失败: %v", err)
	}
	dir := mailboxDir("example.com", "alice")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("maildir 未创建: %v", err)
	}

	if err := DeleteMailbox(box.ID); err != nil {
		t.Fatalf("删除账号失败: %v", err)
	}
	var cnt int64
	model.DB.Model(&model.MailMessage{}).Where("mailbox_id = ?", box.ID).Count(&cnt)
	if cnt != 0 {
		t.Fatalf("删除账号后仍有 %d 封邮件索引", cnt)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("删除账号后 maildir 应被清理, err=%v", err)
	}

	// 删除域名应清理整域数据
	if err := DeleteMailDomain(dom.ID); err != nil {
		t.Fatalf("删除域名失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(mailDataRoot(), "example.com")); !os.IsNotExist(err) {
		t.Fatalf("删除域名后 maildir 应被清理, err=%v", err)
	}
}

// TestMailApiKeyAuth 对外 API 令牌鉴权与跨域隔离。
func TestMailApiKeyAuth(t *testing.T) {
	setupMailTestEnv(t)

	dom, _ := CreateMailDomain("example.com", "", 1024)
	other, _ := CreateMailDomain("other.com", "", 1024)
	if _, err := CreateMailbox(dom.ID, "alice", "pw123456", "", 1024); err != nil {
		t.Fatalf("创建账号失败: %v", err)
	}

	plain, view, err := CreateMailApiKey(dom.ID, "网站通知")
	if err != nil {
		t.Fatalf("创建令牌失败: %v", err)
	}
	if !strings.HasPrefix(plain, "mk_") || view.KeyPrefix == "" {
		t.Fatalf("令牌格式异常: %s", plain)
	}
	key, err := AuthenticateMailApiKey(plain)
	if err != nil {
		t.Fatalf("令牌校验失败: %v", err)
	}
	if key.Domain != "example.com" {
		t.Fatalf("令牌绑定域名 = %s", key.Domain)
	}
	// 跨域发件应被拒绝
	if _, err := MailApiResolveMailbox(key, "bob@other.com"); err == nil {
		t.Fatal("跨域发件应被拒绝")
	}
	if _, err := MailApiResolveMailbox(key, "alice@example.com"); err != nil {
		t.Fatalf("本域发件应通过: %v", err)
	}
	// 停用后不可用
	if err := SetMailApiKeyEnabled(view.ID, false); err != nil {
		t.Fatalf("停用令牌失败: %v", err)
	}
	if _, err := AuthenticateMailApiKey(plain); err == nil {
		t.Fatal("停用后的令牌不应通过")
	}
	_ = other
}

// TestMailLogStats 邮件日志统计。
func TestMailLogStats(t *testing.T) {
	setupMailTestEnv(t)
	dom, _ := CreateMailDomain("example.com", "", 1024)
	if _, err := CreateMailbox(dom.ID, "alice", "pw123456", "", 1024); err != nil {
		t.Fatalf("创建账号失败: %v", err)
	}
	if err := DeliverInbound("sender@outside.com", "example.com", "alice", rawTestMail("hi", "hello")); err != nil {
		t.Fatalf("投递失败: %v", err)
	}
	logs, err := ListMailLogs(MailLogFilter{Direction: "in"})
	if err != nil {
		t.Fatalf("查询日志失败: %v", err)
	}
	if len(logs) == 0 {
		t.Fatal("收信应写入邮件日志")
	}
	st := MailLogSummary("example.com")
	if st.InOK < 1 || st.TodayIn < 1 {
		t.Fatalf("日志统计异常: %+v", st)
	}
}

func boolPtr(b bool) *bool    { return &b }
func strPtr(s string) *string { return &s }
