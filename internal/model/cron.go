package model

import "time"

// Cron 计划任务
type Cron struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	Name       string    `gorm:"size:64" json:"name"`
	Spec       string    `gorm:"size:64" json:"spec"`              // cron 表达式，如 */5 * * * *
	Command    string    `gorm:"size:2048" json:"command"`         // 要执行的 shell 命令
	Remark     string    `gorm:"size:256" json:"remark"`           // 备注
	Status     string    `gorm:"size:16;default:enabled" json:"status"` // enabled / disabled
	LastRun    *time.Time `json:"last_run"`                        // 最近一次执行时间
	LastResult string    `gorm:"size:512" json:"last_result"`      // 最近一次执行结果（exit 码 / 错误摘要）
	// 模板字段：保留模板上下文以便编辑
	Template   string    `gorm:"size:32" json:"template"`           // 任务模板 key
	SiteName   string    `gorm:"size:512" json:"site_name"`         // 备份网站 / 日志切割（多个站点用英文逗号分隔；"*" 代表全部）
	SiteRoot   string    `gorm:"size:1024" json:"site_root"`        // 站点根目录（多个站点用英文逗号分隔，与 SiteName 一一对应）
	Database   string    `gorm:"size:512" json:"database"`          // 备份数据库（多个库用英文逗号分隔；"*" 代表全部）
	Dir        string    `gorm:"size:512" json:"dir"`               // 备份目录 / 清理日志目录
	URL        string    `gorm:"size:512" json:"url"`               // 访问 URL
	Days       int       `json:"days"`                              // 日志保留天数
	Keep       int       `json:"keep"`                              // 增量备份保留份数
	Format     string    `gorm:"size:16" json:"format"`             // 压缩格式 tar.gz / zip
	// 备份目标（备份网站/数据库）：local=本地，remote=远程存储
	TargetType string    `gorm:"size:16;default:local" json:"target_type"`   // local / remote
	TargetName string    `gorm:"size:64" json:"target_name"`                 // local=local，remote=存储名称
	RemoteKeep int       `json:"remote_keep"`                                // 远程保留份数（= 0 表示用本地 keep）
	RunCount   int       `json:"run_count"`                                  // 累计执行次数
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}