package model

import "time"

// OperationLog 操作日志
type OperationLog struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	AdminID   uint      `gorm:"index" json:"admin_id"`
	Action    string    `gorm:"size:64" json:"action"`   // 操作类型，如 login / file.delete / api.site.create
	Detail    string    `gorm:"size:512" json:"detail"`  // 操作描述
	RawCmd    string    `gorm:"size:1024" json:"raw_cmd"` // 原始命令/输入（明文追溯用，可空）
	IP        string    `gorm:"size:64" json:"ip"`       // 来源 IP
	Status    string    `gorm:"size:16" json:"status"`   // success / fail
	Source    string    `gorm:"size:16;index" json:"source"` // 调用来源：login(前端 JWT) / api(api_token) / mcp(mcp_token) / temp(临时访问)
	TempAccessID uint   `gorm:"index" json:"temp_access_id"` // 临时访问 ID（仅 Source=temp 时有值）
	CreatedAt time.Time `json:"created_at"`
}
