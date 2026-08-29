package model

import "time"

// 应用安装状态
const (
	AppNotInstalled = "not_installed"
	AppInstalling   = "installing"
	AppInstalled    = "installed"
	AppUninstalling = "uninstalling"
	AppFailed       = "failed"
)

// AppRecord 应用商店中某个应用的安装记录
type AppRecord struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	Key         string     `gorm:"size:32;uniqueIndex" json:"key"`
	Status      string     `gorm:"size:32;default:not_installed" json:"status"`
	Version     string     `gorm:"size:128" json:"version"`
	Error       string     `gorm:"size:512" json:"error"`
	ServiceName string     `gorm:"size:64" json:"service_name"` // 实际解析到的 systemd 服务名
	InstalledAt *time.Time `json:"installed_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// GetAppRecord 按 key 读取应用记录，不存在则返回 nil
func GetAppRecord(key string) (*AppRecord, error) {
	var rec AppRecord
	err := DB.Where("key = ?", key).First(&rec).Error
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// ListAppRecordsByStatus 按状态读取应用记录（用于启动时清理 ghost 任务）
func ListAppRecordsByStatus(status string) ([]AppRecord, error) {
	var recs []AppRecord
	err := DB.Where("status = ?", status).Find(&recs).Error
	return recs, err
}

// SaveAppRecord 保存应用记录
func SaveAppRecord(rec *AppRecord) error {
	return Upsert(rec.ID, rec)
}
