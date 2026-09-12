package service

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gorm.io/gorm"

	"kypanel/internal/model"
)

// 邮箱配额：以「已用字节数」（Mailbox.StorageUsed）为准，收信前校验上限。
// 默认单账号 1024MB；域未设 QuotaPerBox 时用默认值。

const mailDefaultQuotaMB = 1024

// mailboxQuotaBytes 账号容量上限（字节）。QuotaMb<=0 视为默认 1024MB。
func mailboxQuotaBytes(box *model.Mailbox) int64 {
	mb := box.QuotaMb
	if mb <= 0 {
		mb = mailDefaultQuotaMB
	}
	return mb * 1024 * 1024
}

// CheckMailboxCanReceive 校验收信后是否超出容量。incoming 为即将写入的字节数。
func CheckMailboxCanReceive(box *model.Mailbox, incoming int64) error {
	limit := mailboxQuotaBytes(box)
	if box.StorageUsed+incoming > limit {
		return fmt.Errorf("邮箱容量已满（已用 %.1fMB / 上限 %dMB）",
			float64(box.StorageUsed)/(1024*1024), limit/(1024*1024))
	}
	return nil
}

// AddMailboxStorage 累加/扣减账号已用容量（delta 可为负），并夹紧到 >=0。
func AddMailboxStorage(boxID uint, delta int64) {
	if boxID == 0 || delta == 0 {
		return
	}
	// 用 SQL 表达式原子更新，避免并发覆盖
	if delta > 0 {
		model.DB.Model(&model.Mailbox{}).Where("id = ?", boxID).
			Update("storage_used", gorm.Expr("storage_used + ?", delta))
		return
	}
	model.DB.Model(&model.Mailbox{}).Where("id = ?", boxID).
		Update("storage_used", gorm.Expr("max(storage_used + ?, 0)", delta))
}

// removeMailboxFiles 删除账号的 maildir 与门户附件库目录（best-effort）。
func removeMailboxFiles(domain, name string) {
	_ = os.RemoveAll(mailboxDir(domain, name))
	var box model.Mailbox
	if err := model.DB.Where("domain = ? AND name = ?", domain, name).First(&box).Error; err == nil {
		if dir := mailPortalFilesDirNoCreate(&box); dir != "" {
			_ = os.RemoveAll(dir)
		}
	}
}

// mailPortalFilesDirNoCreate 计算门户附件库目录但不创建（清理时用，避免"删了又被建回来"）。
func mailPortalFilesDirNoCreate(box *model.Mailbox) string {
	if box == nil {
		return ""
	}
	var dom model.MailDomain
	if err := model.DB.First(&dom, box.DomainID).Error; err != nil || dom.PortalSiteID == 0 {
		return ""
	}
	s, err := getSiteOrErr(dom.PortalSiteID)
	if err != nil || s == nil {
		return ""
	}
	return filepath.Join(s.Root, "files", sanitizeMailboxDirName(box.Name))
}

// removeDomainMailDir 删除整域 maildir 目录。
func removeDomainMailDir(domain string) {
	_ = os.RemoveAll(filepath.Join(mailDataRoot(), strings.ToLower(strings.TrimSpace(domain))))
}

// RecalcAllMailboxStorage 按索引表重算所有账号已用容量（启动时调用一次，修正历史数据）。
func RecalcAllMailboxStorage() {
	var boxes []model.Mailbox
	model.DB.Find(&boxes)
	for i := range boxes {
		used := mailboxStorageFromIndex(boxes[i].ID)
		if used != boxes[i].StorageUsed {
			model.DB.Model(&model.Mailbox{}).Where("id = ?", boxes[i].ID).
				Update("storage_used", used)
		}
	}
}

// RecalcMailboxStorage 重算单个账号已用容量。
func RecalcMailboxStorage(boxID uint) int64 {
	used := mailboxStorageFromIndex(boxID)
	model.DB.Model(&model.Mailbox{}).Where("id = ?", boxID).Update("storage_used", used)
	return used
}

// mailboxStorageFromIndex 汇总某账号所有邮件的 raw_size。
func mailboxStorageFromIndex(mailboxID uint) int64 {
	var total int64
	model.DB.Model(&model.MailMessage{}).Where("mailbox_id = ?", mailboxID).
		Select("COALESCE(SUM(raw_size), 0)").Scan(&total)
	return total
}

// MailboxUsage 账号用量信息（供前端展示）
type MailboxUsage struct {
	MailboxID uint    `json:"mailbox_id"`
	Address   string  `json:"address"`
	Used      int64   `json:"used"`
	Quota     int64   `json:"quota"`
	Percent   float64 `json:"percent"`
	Messages  int64   `json:"messages"`
}

// GetMailboxUsage 读取账号用量。
func GetMailboxUsage(boxID uint) (*MailboxUsage, error) {
	var box model.Mailbox
	if err := model.DB.First(&box, boxID).Error; err != nil {
		return nil, errors.New("账号不存在")
	}
	quota := mailboxQuotaBytes(&box)
	pct := 0.0
	if quota > 0 {
		pct = float64(box.StorageUsed) / float64(quota) * 100
	}
	var n int64
	model.DB.Model(&model.MailMessage{}).Where("mailbox_id = ?", boxID).Count(&n)
	return &MailboxUsage{
		MailboxID: box.ID,
		Address:   box.Address(),
		Used:      box.StorageUsed,
		Quota:     quota,
		Percent:   pct,
		Messages:  n,
	}, nil
}

// CleanupMailboxStorage 删除账号时回收其邮件索引与磁盘目录（第 7 项：删账号清数据）。
func CleanupMailboxStorage(box model.Mailbox) {
	// 1) 删除索引
	model.DB.Where("mailbox_id = ?", box.ID).Delete(&model.MailMessage{})
	// 2) 删除附件库与 maildir
	removeMailboxFiles(box.Domain, box.Name)
	slog.Info("已清理邮箱账号数据", "address", box.Address())
}

// CleanupDomainStorage 删除域名时回收其下所有账号数据与整域 maildir。
func CleanupDomainStorage(domain string) {
	var boxes []model.Mailbox
	model.DB.Where("domain = ?", domain).Find(&boxes)
	for _, b := range boxes {
		model.DB.Where("mailbox_id = ?", b.ID).Delete(&model.MailMessage{})
		removeMailboxFiles(b.Domain, b.Name)
	}
	model.DB.Where("domain = ?", domain).Delete(&model.Mailbox{})
	model.DB.Where("domain = ?", domain).Delete(&model.MailAlias{})
	model.DB.Where("domain = ?", domain).Delete(&model.MailApiKey{})
	removeDomainMailDir(domain)
	slog.Info("已清理邮箱域名数据", "domain", domain)
}
