package service

import (
	"errors"
	"strings"
	"time"
)

// resolveDBServiceName 解析数据库类型对应的 systemd 服务单元名。
// MySQL/Redis 会探测实际安装的是 mariadb 还是 mysql、redis-server 还是 redis。
func resolveDBServiceName(t DatabaseType) (string, error) {
	switch t {
	case DBTypeMySQL:
		for _, name := range []string{"mariadb", "mysqld", "mysql"} {
			if serviceUnitExists(name) {
				return name, nil
			}
		}
		return "", errors.New("未找到 MySQL/MariaDB 服务单元")
	case DBTypeRedis:
		for _, name := range []string{"redis-server", "redis"} {
			if serviceUnitExists(name) {
				return name, nil
			}
		}
		return "", errors.New("未找到 Redis 服务单元")
	case DBTypeMongoDB:
		return "mongod", nil
	case DBTypePgSQL:
		return "postgresql", nil
	case DBTypeSQLServer:
		return "mssql-server", nil
	}
	return "", errors.New("该数据库类型不支持服务控制")
}

// serviceUnitExists 判断某个 systemd 单元是否存在
func serviceUnitExists(name string) bool {
	res, err := ExecCommand("systemctl list-unit-files "+name+".service 2>&1", 3*time.Second)
	if err != nil || res == nil {
		return false
	}
	return !strings.Contains(res.Stdout, "0 unit files listed")
}

// DatabaseServiceStatus 查询数据库服务的运行状态
// 返回值："running" | "stopped" | "unknown" | "n/a"（SQLite）
func DatabaseServiceStatus(dbType string) (string, error) {
	t := DatabaseType(dbType)
	if t == "" {
		return "", errors.New("缺少 type 参数")
	}
	if t == DBTypeSQLite {
		return "n/a", nil
	}
	name, err := resolveDBServiceName(t)
	if err != nil {
		// 服务单元不存在 = 该数据库环境未安装，属于正常情况而非错误。
		// 返回 "unknown" + nil error，避免前端误弹"未找到 XX 服务单元"。
		return "unknown", nil
	}
	res, err := ExecCommand("systemctl is-active "+name, 5*time.Second)
	if err != nil || res == nil {
		return "unknown", nil
	}
	state := strings.TrimSpace(res.Stdout)
	if state == "active" {
		return "running", nil
	}
	return "stopped", nil
}

// DatabaseServiceAction 启动/停止/重启数据库服务
func DatabaseServiceAction(dbType, action string) error {
	t := DatabaseType(dbType)
	if t == "" {
		return errors.New("缺少 type 参数")
	}
	if t == DBTypeSQLite {
		return errors.New("SQLite 无服务可控制")
	}
	if action != "start" && action != "stop" && action != "restart" {
		return errors.New("非法 action: " + action)
	}
	name, err := resolveDBServiceName(t)
	if err != nil {
		return err
	}
	res, err := ExecCommand("systemctl "+action+" "+name, 60*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		stderr := strings.TrimSpace(res.Stderr)
		if stderr == "" {
			stderr = strings.TrimSpace(res.Stdout)
		}
		if stderr == "" {
			stderr = "服务 " + action + " 失败"
		}
		return errors.New(stderr)
	}
	return nil
}
