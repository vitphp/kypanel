package model

import "time"

// BackupTask 备份任务记录（本地 + 远程存储）
type BackupTask struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Type      string    `gorm:"size:16;index" json:"type"` // site / panel / database
	Target    string    `gorm:"size:128;index" json:"target"` // 备份对象：网站名 / "panel" / 数据库名
	FileName  string    `gorm:"size:255" json:"file_name"` // 备份文件名
	Size      int64     `json:"size"`                      // 文件大小（字节）
	Storage   string    `gorm:"size:16" json:"storage"`    // local / s3 / oss / ftp
	Status    string    `gorm:"size:16" json:"status"`     // success / failed / running
	Error     string    `gorm:"size:512" json:"error"`     // 失败原因
	CreatedAt time.Time `json:"created_at"`
}

// BackupStorage 远程存储配置
type BackupStorage struct {
	Type     string `json:"type"` // s3 / oss / ftp
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	Endpoint string `json:"endpoint"` // S3/OSS endpoint 或 FTP 主机
	Bucket   string `json:"bucket"`   // S3/OSS bucket
	Region   string `json:"region"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Host     string `json:"host"` // FTP 主机
	Port     int    `json:"port"` // FTP 端口
	User     string `json:"user"`
	Pass     string `json:"pass"`
	Path     string `json:"path"` // 远程目录前缀
}
