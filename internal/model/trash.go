package model

import "time"

// TrashItem 文件回收站条目
type TrashItem struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	TrashDir   string    `gorm:"size:128" json:"trash_dir"`    // 回收站内目录名（uuid）
	Type       string    `gorm:"size:16" json:"type"`          // file / dir
	Name       string    `gorm:"size:255" json:"name"`         // 原始文件名
	OriginPath string    `gorm:"size:1024" json:"origin_path"` // 原始完整路径
	TrashPath  string    `gorm:"size:1024" json:"trash_path"`  // 回收站内实际路径
	Size       int64     `json:"size"`                         // 原始大小
	DeletedAt  time.Time `json:"deleted_at"`
}
