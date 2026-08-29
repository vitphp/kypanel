package model

import "time"

// DockerApp Docker 应用商店安装记录
type DockerApp struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	Key         string     `gorm:"size:64;uniqueIndex" json:"key"`
	Name        string     `gorm:"size:64" json:"name"`
	Status      string     `gorm:"size:32;default:not_installed" json:"status"` // not_installed / installing / installed / failed / removed
	Port        int        `json:"port"`                                        // 对外端口
	Error       string     `gorm:"size:512" json:"error"`
	InstalledAt *time.Time `json:"installed_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// GetDockerApp 按 key 读取
func GetDockerApp(key string) (*DockerApp, error) {
	var rec DockerApp
	err := DB.Where("key = ?", key).First(&rec).Error
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// SaveDockerApp 保存
func SaveDockerApp(rec *DockerApp) error {
	return Upsert(rec.ID, rec)
}
