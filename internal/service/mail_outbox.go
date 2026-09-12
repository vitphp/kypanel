package service

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kypanel/internal/model"
)

// 外发队列：外域邮件先落盘入队，由后台 worker 直发 MX。
// 失败按退避策略重试，超过 MaxRetry 生成退信并标记 failed。
// 好处：面板/门户发信接口不再被对方 MX 的超时阻塞。

// outboxMaxRetry 默认最大重试次数
const outboxMaxRetry = 6

// mailOutboxDir 队列报文目录
func mailOutboxDir() string {
	return filepath.Join(mailDataRoot(), "outbox")
}

// EnqueueOutbox 把一封待外发邮件写入队列（文件 + 数据库记录）。
// fromAddr 为信封发件人；mailboxID 为发件账号（0 表示别名/系统转发，无退信目标）。
func EnqueueOutbox(fromAddr string, domainID, mailboxID uint, rcpts []string, raw []byte, messageID string) error {
	if len(rcpts) == 0 {
		return fmt.Errorf("无收件人")
	}
	if fromAddr == "" {
		fromAddr = "postmaster@localhost"
	}
	if err := os.MkdirAll(mailOutboxDir(), 0o700); err != nil {
		return err
	}
	fname := fmt.Sprintf("%d.%s", time.Now().UnixNano(), randHex(4))
	if err := os.WriteFile(filepath.Join(mailOutboxDir(), fname), raw, 0o600); err != nil {
		return err
	}
	rec := &model.MailOutbox{
		MailboxID: mailboxID,
		DomainID:  domainID,
		FromAddr:  fromAddr,
		ToAddrs:   strings.Join(rcpts, ", "),
		Subject:   parseSubject(raw),
		Filename:  fname,
		MessageID: messageID,
		MaxRetry:  outboxMaxRetry,
		NextTry:   time.Now().Unix(), // 立即尝试
		Status:    "pending",
	}
	if err := model.DB.Create(rec).Error; err != nil {
		_ = os.Remove(filepath.Join(mailOutboxDir(), fname))
		return err
	}
	return nil
}

// StartMailOutboxWorker 启动外发队列后台协程（每 20 秒扫一次到期任务）。
func StartMailOutboxWorker() {
	// 上次异常退出可能残留 in-flight 状态，这里不需要：状态只有 pending/sent/failed
	go func() {
		// 启动后先把上次残留的 pending 立刻推进（避免等一个 tick）
		processDueOutbox()
		t := time.NewTicker(20 * time.Second)
		defer t.Stop()
		for range t.C {
			processDueOutbox()
		}
	}()
}

// processDueOutbox 处理所有到期任务（单轮最多 20 封，避免长时间占用）。
func processDueOutbox() {
	var items []model.MailOutbox
	now := time.Now().Unix()
	model.DB.Where("status = ? AND next_try <= ?", "pending", now).
		Order("id asc").Limit(20).Find(&items)
	for i := range items {
		processOutboxItem(&items[i])
	}
}

// processOutboxItem 尝试投递单封队列邮件。
func processOutboxItem(item *model.MailOutbox) {
	raw, err := os.ReadFile(filepath.Join(mailOutboxDir(), item.Filename))
	if err != nil {
		// 报文文件丢失：无法重试，直接标记失败
		markOutboxFailed(item, "队列报文文件丢失")
		return
	}
	rcpts := splitAddrList(item.ToAddrs)
	if len(rcpts) == 0 {
		markOutboxFailed(item, "收件人为空")
		return
	}
	byDomain := map[string][]string{}
	for _, rc := range rcpts {
		d := domainOf(rc)
		if d == "" {
			continue
		}
		byDomain[d] = append(byDomain[d], rc)
	}
	var lastErr error
	allOK := true
	for d, addrs := range byDomain {
		if err := deliverExternalDomain(d, item.FromAddr, addrs, raw); err != nil {
			allOK = false
			lastErr = err
			slog.Warn("外发重试失败", "id", item.ID, "domain", d, "err", err)
		}
	}
	if allOK {
		item.Status = "sent"
		model.DB.Model(&model.MailOutbox{}).Where("id = ?", item.ID).Updates(map[string]interface{}{
			"status": "sent", "last_error": "", "next_try": 0,
		})
		RecordMailLog("out", domainOf(item.FromAddr), item.MailboxID, item.FromAddr, item.ToAddrs,
			item.Subject, 0, "ok", "队列投递成功", "")
		_ = os.Remove(filepath.Join(mailOutboxDir(), item.Filename))
		return
	}

	retry := item.Retry + 1
	msg := "投递失败"
	if lastErr != nil {
		msg = lastErr.Error()
	}
	if retry >= item.MaxRetry {
		markOutboxFailed(item, fmt.Sprintf("重试 %d 次仍失败：%s", retry, msg))
		return
	}
	next := time.Now().Add(outboxBackoff(retry)).Unix()
	model.DB.Model(&model.MailOutbox{}).Where("id = ?", item.ID).Updates(map[string]interface{}{
		"retry": retry, "last_error": truncateMailField(msg, 1024), "next_try": next,
	})
	slog.Info("邮件已重新入队等待重试", "id", item.ID, "retry", retry, "next_try", next)
}

// markOutboxFailed 标记队列任务失败并给原发件人发退信。
func markOutboxFailed(item *model.MailOutbox, reason string) {
	model.DB.Model(&model.MailOutbox{}).Where("id = ?", item.ID).Updates(map[string]interface{}{
		"status": "failed", "next_try": 0, "last_error": truncateMailField(reason, 1024),
	})
	RecordMailLog("bounce", domainOf(item.FromAddr), item.MailboxID, item.FromAddr, item.ToAddrs,
		item.Subject, 0, "failed", reason, "")
	// 发退信给原发件人（仅当有发件账号且账号仍存在）
	if item.MailboxID > 0 {
		var box model.Mailbox
		if err := model.DB.First(&box, item.MailboxID).Error; err == nil {
			sendBounceMail(box, item, reason)
		}
	}
	_ = os.Remove(filepath.Join(mailOutboxDir(), item.Filename))
}

// sendBounceMail 生成退信（本地投递给原发件账号）。
func sendBounceMail(box model.Mailbox, item *model.MailOutbox, reason string) {
	content := MailContent{
		FromName: "Mail Delivery System",
		FromAddr: "MAILER-DAEMON@" + box.Domain,
		ToAddrs:  []string{box.Address()},
		Subject:  "【退信】邮件无法投递: " + item.Subject,
		TextBody: fmt.Sprintf(
			"很抱歉，您发送的邮件无法投递。\r\n\r\n"+
				"收件人：%s\r\n主题：%s\r\n失败原因：%s\r\n\r\n"+
				"该邮件已在服务器上重试 %d 次仍未成功，已放弃投递。\r\n"+
				"请检查收件地址是否正确，或稍后重新发送。\r\n",
			item.ToAddrs, item.Subject, reason, item.Retry),
	}
	raw := buildMimeMessage(content)
	if _, err := DeliverMessage(box.Domain, box.Name, raw); err != nil {
		slog.Warn("发送退信失败", "err", err, "to", box.Address())
	}
}

// outboxBackoff 重试退避：1m → 5m → 15m → 30m → 1h → 2h。
func outboxBackoff(retry int) time.Duration {
	steps := []time.Duration{
		1 * time.Minute, 5 * time.Minute, 15 * time.Minute,
		30 * time.Minute, time.Hour, 2 * time.Hour,
	}
	if retry <= 0 {
		return steps[0]
	}
	if retry > len(steps) {
		return steps[len(steps)-1]
	}
	return steps[retry-1]
}

// splitAddrList 拆分逗号分隔的地址列表。
func splitAddrList(s string) []string {
	var out []string
	for _, a := range strings.Split(s, ",") {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}

// ===== 管理接口用 =====

// MailOutboxFilter 队列查询条件
type MailOutboxFilter struct {
	Status string
	Limit  int
}

// ListMailOutbox 查询外发队列。
func ListMailOutbox(f MailOutboxFilter) ([]model.MailOutbox, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 200
	}
	q := model.DB.Model(&model.MailOutbox{})
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	var out []model.MailOutbox
	if err := q.Order("id desc").Limit(f.Limit).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// RetryOutboxNow 手动把某队列任务置为立即重试。
func RetryOutboxNow(id uint) error {
	res := model.DB.Model(&model.MailOutbox{}).Where("id = ? AND status = ?", id, "pending").
		Updates(map[string]interface{}{"next_try": time.Now().Unix(), "retry": 0})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("任务不存在或已结束")
	}
	go processDueOutbox()
	return nil
}

// DeleteOutbox 删除队列任务（含报文文件）。
func DeleteOutbox(id uint) error {
	var item model.MailOutbox
	if err := model.DB.First(&item, id).Error; err != nil {
		return fmt.Errorf("任务不存在")
	}
	_ = os.Remove(filepath.Join(mailOutboxDir(), item.Filename))
	return model.DB.Delete(&model.MailOutbox{}, id).Error
}

// CountOutboxPending 待发队列数量（供概览展示）。
func CountOutboxPending() int64 {
	var n int64
	model.DB.Model(&model.MailOutbox{}).Where("status = ?", "pending").Count(&n)
	return n
}
