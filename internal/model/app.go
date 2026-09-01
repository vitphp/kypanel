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

// ListActiveAppRecords 读取所有非「未安装」状态的应用记录
// （已安装 / 安装中 / 卸载中 / 失败）。用于已安装应用兜底：
// 官网下架/删除 meta 后，这些应用不能凭空消失，需用本地内置 meta 补回。
func ListActiveAppRecords() ([]AppRecord, error) {
	var recs []AppRecord
	err := DB.Where("status != ?", AppNotInstalled).Find(&recs).Error
	return recs, err
}

// SaveAppRecord 保存应用记录
func SaveAppRecord(rec *AppRecord) error {
	return Upsert(rec.ID, rec)
}

// AppChannel 应用下载渠道（多渠道加速：安装时测速选最快源，下载失败自动切换）
type AppChannel struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Key       string    `gorm:"size:64;index" json:"key"` // 应用 key 前缀（go / node / python / node-mirror / python-mirror）
	Name      string    `gorm:"size:64" json:"name"`      // 渠道名（如 阿里云、华为云、官方）
	URL       string    `gorm:"size:1024" json:"url"`     // 下载地址模板，支持 ${FULL}（完整版本号）、${ARCH}（架构）占位符
	Order     int       `json:"order"`                    // 默认优先级（越小越靠前）
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ListAppChannelsByKeyPrefix 按应用 key 前缀读取渠道（order 升序）
func ListAppChannelsByKeyPrefix(prefix string) ([]AppChannel, error) {
	var chs []AppChannel
	err := DB.Where("key LIKE ?", prefix+"%").Order("`order` asc, id asc").Find(&chs).Error
	return chs, err
}

// CountAppChannels 渠道总数
func CountAppChannels() (int64, error) {
	var n int64
	err := DB.Model(&AppChannel{}).Count(&n).Error
	return n, err
}

// UpsertAppChannels 批量写入渠道
func UpsertAppChannels(chs []AppChannel) error {
	return DB.Save(&chs).Error
}
