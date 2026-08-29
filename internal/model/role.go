package model

import "time"

// Role 角色（自定义角色，权限为菜单模块 key 列表）
type Role struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"size:64;uniqueIndex;not null" json:"name"`
	Remark      string    `gorm:"size:255" json:"remark"`
	Permissions string    `gorm:"size:1024" json:"permissions"` // 逗号分隔的模块 key，* 表示全部
	Builtin     bool      `gorm:"default:false" json:"builtin"` // 内置角色不可删除
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// PermissionModules 权限模块定义：key -> 展示名（与前端菜单/后端接口前缀对应）
// 顺序即前端权限勾选页展示顺序
var PermissionModules = []struct {
	Key   string
	Label string
}{
	{"dashboard", "概览"},
	{"site", "网站"},
	{"database", "数据库"},
	{"backup", "备份中心"},
	{"ftp", "FTP"},
	{"file", "文件管理"},
	{"container", "容器"},
	{"appstore", "应用商店"},
	{"cron", "计划任务"},
	{"log", "日志"},
	{"monitor", "监控"},
	{"process", "进程管理"},
	{"firewall", "防火墙"},
	{"settings", "设置"},
	{"mcp", "AI 助手"},
}

// PermissionModuleKeys 返回所有权限模块 key
func PermissionModuleKeys() []string {
	keys := make([]string, 0, len(PermissionModules))
	for _, m := range PermissionModules {
		keys = append(keys, m.Key)
	}
	return keys
}

// HasPermission 判断角色权限列表是否包含某模块（* 表示全部）
func HasPermission(permStr, module string) bool {
	if permStr == "*" {
		return true
	}
	if permStr == "" {
		return false
	}
	perms := splitComma(permStr)
	for _, p := range perms {
		if p == "*" || p == module {
			return true
		}
	}
	return false
}

func splitComma(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	return out
}
