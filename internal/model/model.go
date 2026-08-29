package model

import (
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB 全局数据库实例
var DB *gorm.DB

// Init 初始化数据库
func Init(path string) error {
	// 启用 WAL + busy_timeout，缓解多 goroutine 并发写时的 "database is locked"
	db, err := gorm.Open(sqlite.Open(path+"?_busy_timeout=5000&_journal_mode=WAL"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return err
	}

	// 自动迁移
	if err := db.AutoMigrate(
		&Admin{},
		&OperationLog{},
		&AppRecord{},
		&Site{},
		&Cron{},
		&FtpUser{},
		&DockerApp{},
		&Setting{},
		&TrashItem{},
		&SiteStatVisit{},
		&DatabaseAccount{},
		&SSLCertRecord{},
		&SiteRedirect{},
		&WAFSetting{},
		&WAFRule{},
		&WAFIpRule{},
		&WAFCcConfig{},
		&WAFAttackLog{},
		&SiteBlockIP{},
		&SiteSecurityConfig{},
		&SiteSecIpRule{},
		&SiteSecUaRule{},
		&SiteSecRefererRule{},
		&SiteSecCustomRule{},
		&SiteSecLog{},
		&AlertLog{},
		&LoginSession{},
		&BackupTask{},
		&Role{},
		&LoginFailRecord{},
		&ApiToken{},
		&TempAccess{},
		&TempAccessUseLog{},
	); err != nil {
		return err
	}

	DB = db
	return nil
}

// Upsert 通用"存在则更新、不存在则创建"：以主键 ID 是否为 0 判断。
// 适用于各业务记录的保存（AppRecord / DockerApp / DatabaseAccount 等）。
func Upsert(id uint, rec interface{}) error {
	if id == 0 {
		return DB.Create(rec).Error
	}
	return DB.Save(rec).Error
}

// FirstByID 按主键查询记录到 dst；返回是否找到（false 表示记录不存在）。
// 统一「查询 + 不存在判断」样板，避免各处重复 First + err != nil。
func FirstByID(dst interface{}, id uint) bool {
	return DB.First(dst, id).Error == nil
}
