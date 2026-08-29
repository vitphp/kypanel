package service

import (
	"errors"
	"regexp"
	"strings"
)

var identRe = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// checkDBIdentifier 校验数据库/用户等标识符，防止 SQL 注入。
// 允许字母、数字、下划线（MySQL 等主流引擎的合法标识符字符集）。
func checkDBIdentifier(name, label string) error {
	if name == "" {
		return errors.New(label + "不能为空")
	}
	if !identRe.MatchString(name) {
		return errors.New(label + "只能包含字母、数字、下划线")
	}
	if len(name) > 64 {
		return errors.New(label + "过长")
	}
	return nil
}

// sqlEscapeString 转义单引号和反斜杠，防止字符串值注入（用于拼接 SQL 字面量）。
func sqlEscapeString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "''")
	return s
}

// CreateDatabaseReq 创建数据库请求（兼容所有类型）
type CreateDatabaseReq struct {
	Name     string `json:"name"`
	User     string `json:"user"`
	Password string `json:"password"`
	Charset  string `json:"charset"`
	Path     string `json:"path"`
}

// DatabaseInfo 数据库信息（兼容旧接口，保留 MySQL 场景使用）
type DatabaseInfo struct {
	Name      string `json:"name"`
	Size      string `json:"size"`
	Collation string `json:"collation"`
}

// MysqlAvailable 检测 mysql 客户端是否可用（兼容旧接口）
func MysqlAvailable() (bool, string) {
	return DatabaseAvailable(string(DBTypeMySQL))
}

// ListDatabases 列出所有 MySQL 数据库（兼容旧接口）
func ListDatabases() ([]DatabaseInfo, error) {
	rows, err := ListDatabasesByType(string(DBTypeMySQL))
	if err != nil {
		return nil, err
	}
	var list []DatabaseInfo
	for _, row := range rows {
		name, _ := row["name"].(string)
		list = append(list, DatabaseInfo{Name: name})
	}
	return list, nil
}

// CreateDatabase 创建 MySQL 数据库 + 用户并授权（兼容旧接口）
func CreateDatabase(req CreateDatabaseReq) error {
	return CreateDatabaseByType(string(DBTypeMySQL), req)
}

// DeleteDatabase 删除 MySQL 数据库及对应用户（兼容旧接口）
func DeleteDatabase(name string) error {
	return DeleteDatabaseByType(string(DBTypeMySQL), name)
}

func isSystemDatabase(name string) bool {
	switch name {
	case "information_schema", "mysql", "performance_schema", "sys":
		return true
	}
	return false
}
