package service

import (
	"log/slog"
	"strings"
	"time"

	"kypanel/internal/model"
)

// 收发信日志（审计）：收信投递、发信、退信、拒收都会落一条记录。

// RecordMailLog 写一条邮件日志（失败仅记录 warn，不影响主流程）。
func RecordMailLog(direction, domain string, mailboxID uint,
	fromAddr, toAddrs, subject string, size int64, status, detail, ip string) {
	rec := &model.MailLog{
		Direction: direction,
		Domain:    domain,
		MailboxID: mailboxID,
		FromAddr:  truncateMailField(fromAddr, 255),
		ToAddrs:   truncateMailField(toAddrs, 1024),
		Subject:   truncateMailField(subject, 512),
		Size:      size,
		Status:    status,
		Detail:    truncateMailField(detail, 1024),
		IP:        ip,
	}
	if err := model.DB.Create(rec).Error; err != nil {
		slog.Warn("写入邮件日志失败", "err", err)
	}
}

// truncateMailField 压缩换行并按上限截断（日志字段避免超长）。
func truncateMailField(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// MailLogFilter 日志查询条件
type MailLogFilter struct {
	Direction string
	Domain    string
	MailboxID uint
	Status    string
	Keyword   string
	Limit     int
}

// ListMailLogs 查询邮件日志（倒序，最多 500 条）。
func ListMailLogs(f MailLogFilter) ([]model.MailLog, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 200
	}
	q := model.DB.Model(&model.MailLog{})
	if f.Direction != "" {
		q = q.Where("direction = ?", f.Direction)
	}
	if f.Domain != "" {
		q = q.Where("domain = ?", f.Domain)
	}
	if f.MailboxID > 0 {
		q = q.Where("mailbox_id = ?", f.MailboxID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("from_addr LIKE ? OR to_addrs LIKE ? OR subject LIKE ?", like, like, like)
	}
	var out []model.MailLog
	if err := q.Order("id desc").Limit(f.Limit).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// MailLogStats 邮件日志统计（供概览卡片展示）
type MailLogStats struct {
	Total    int64 `json:"total"`
	InOK     int64 `json:"in_ok"`
	OutOK    int64 `json:"out_ok"`
	Failed   int64 `json:"failed"`
	TodayIn  int64 `json:"today_in"`
	TodayOut int64 `json:"today_out"`
}

// MailLogSummary 汇总统计（domain 为空表示全部域名）。
func MailLogSummary(domain string) MailLogStats {
	var st MailLogStats
	count := func(direction, status string) int64 {
		q := model.DB.Model(&model.MailLog{})
		if domain != "" {
			q = q.Where("domain = ?", domain)
		}
		if direction != "" {
			q = q.Where("direction = ?", direction)
		}
		if status != "" {
			q = q.Where("status = ?", status)
		}
		var n int64
		q.Count(&n)
		return n
	}
	st.Total = count("", "")
	st.InOK = count("in", "ok")
	st.OutOK = count("out", "ok")
	st.Failed = count("", "failed")

	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	countSince := func(direction string) int64 {
		q := model.DB.Model(&model.MailLog{}).Where("direction = ? AND created_at >= ?", direction, dayStart)
		if domain != "" {
			q = q.Where("domain = ?", domain)
		}
		var n int64
		q.Count(&n)
		return n
	}
	st.TodayIn = countSince("in")
	st.TodayOut = countSince("out")
	return st
}

// CleanupMailLogs 清理超过保留天数的邮件日志（默认 90 天）。
func CleanupMailLogs(keepDays int) int64 {
	if keepDays <= 0 {
		keepDays = 90
	}
	cut := time.Now().AddDate(0, 0, -keepDays)
	res := model.DB.Where("created_at < ?", cut).Delete(&model.MailLog{})
	return res.RowsAffected
}

// StartMailLogJanitor 每日清理过期邮件日志。
func StartMailLogJanitor() {
	go func() {
		t := time.NewTicker(24 * time.Hour)
		defer t.Stop()
		for range t.C {
			if n := CleanupMailLogs(90); n > 0 {
				slog.Info("已清理过期邮件日志", "count", n)
			}
		}
	}()
}
