package mcp

import (
	"errors"
	"fmt"
	"time"

	"kypanel/internal/service"
)

// allTools 返回全部 MCP 工具定义
func allTools() []*Tool {
	return []*Tool{
		{
			Name:        "system_info",
			Description: "获取服务器系统信息：主机名、操作系统、CPU 核数、内存/磁盘使用、系统负载等",
			InputSchema: objectSchema(nil, nil),
			handler:     handleSystemInfo,
		},
		{
			Name:        "process_list",
			Description: "查看服务器进程列表（按 CPU 占用排序），可按关键字过滤，用于排查高负载进程",
			InputSchema: objectSchema(map[string]any{
				"keyword": stringSchema("进程名关键字，如 nginx / php / mysql，可空"),
				"limit":   intSchema("返回条数，默认 30"),
			}, nil),
			handler: handleProcessList,
		},
		{
			Name:        "service_status",
			Description: "查询面板托管服务的运行状态，key 支持 nginx / mysql / mysql8 / php / php82 / php74 等（可用 app_list 查看实际 key）",
			InputSchema: objectSchema(map[string]any{
				"key": stringSchema("服务 key，必填，如 nginx / mysql / php82"),
			}, []string{"key"}),
			handler: handleServiceStatus,
		},
		{
			Name:        "website_list",
			Description: "获取面板已创建的网站列表：域名、类型、运行状态、端口、SSL 状态等",
			InputSchema: objectSchema(nil, nil),
			handler:     handleWebsiteList,
		},
		{
			Name:        "website_create",
			Description: "创建新网站。type 支持 static(静态/PHP)/node/python/go/proxy(反向代理)。PHP 站点可同时创建数据库，需管理员确认执行",
			InputSchema: objectSchema(map[string]any{
				"domain":    stringSchema("主域名（必填），如 example.com，多域名用逗号分隔"),
				"name":      stringSchema("站点名称，留空自动用主域名"),
				"type":      stringSchema("站点类型：static / node / python / go / proxy，必填"),
				"root":      stringSchema("站点根目录（绝对路径），留空自动创建"),
				"proxy_pass": stringSchema("反向代理目标，type=proxy 时必填，如 http://127.0.0.1:8080"),
				"runtime_version": stringSchema("运行环境版本，如 PHP 8.2 / Node 18 / Python 3.11 / Go 1.22"),
				"start_command":   stringSchema("node/python/go 启动命令，如 python app.py"),
				"env_vars":        stringSchema("环境变量，KEY=VALUE 每行一个"),
				"proxy_port":      intSchema("node/python/go 应用运行端口，nginx 反代目标"),
				"framework":       stringSchema("python 框架：flask / django / generic"),
				"create_db":       boolSchema("是否同时创建数据库（PHP 站点）"),
				"db_name":         stringSchema("数据库名，留空自动生成"),
			}, []string{"domain", "type"}),
			handler: handleCreateSite,
		},
		{
			Name:        "database_list",
			Description: "获取面板管理的数据库列表：库名、类型、字符集、大小等",
			InputSchema: objectSchema(nil, nil),
			handler:     handleDatabaseList,
		},
		{
			Name:        "app_list",
			Description: "获取应用商店已安装/可用的运行环境列表（PHP / Python / Go / Node.js 各版本及安装状态）",
			InputSchema: objectSchema(nil, nil),
			handler:     handleAppList,
		},
		{
			Name:        "file_list",
			Description: "列出服务器指定目录下的文件和子目录（含大小、修改时间、权限），用于排查部署问题",
			InputSchema: objectSchema(map[string]any{
				"path": stringSchema("目录绝对路径（必填），如 /www/wwwroot/example.com"),
			}, []string{"path"}),
			handler: handleFileList,
		},
		{
			Name:        "exec_command",
			Description: "在服务器上执行诊断命令（故障排查），仅允许只读/诊断类命令（面板内置危险命令拦截），执行记录写入操作日志",
			InputSchema: objectSchema(map[string]any{
				"command": stringSchema("要执行的命令，如: systemctl status nginx / tail -n 100 /www/wwwlogs/example.com.error.log / df -h / free -m"),
				"timeout": intSchema("超时秒数，默认 10，最大 60"),
			}, []string{"command"}),
			handler: handleExecCommand,
		},
	}
}

// ---------- 工具处理器 ----------

func handleSystemInfo(ctx *ToolContext, args map[string]any) (any, error) {
	return service.GetSystemInfo()
}

func handleProcessList(ctx *ToolContext, args map[string]any) (any, error) {
	keyword := getString(args, "keyword")
	limit := getInt(args, "limit", 30)
	if limit < 1 || limit > 200 {
		limit = 30
	}
	return service.ListProcesses("cpu", keyword, limit)
}

func handleServiceStatus(ctx *ToolContext, args map[string]any) (any, error) {
	key := getString(args, "key")
	if key == "" {
		return nil, errors.New("参数 key 必填（服务名），如 nginx / mysql / php82")
	}
	status, err := service.ServiceStatus(key)
	if err != nil {
		return nil, fmt.Errorf("查询服务 %s 失败: %w", key, err)
	}
	return map[string]any{"key": key, "status": status}, nil
}

func handleWebsiteList(ctx *ToolContext, args map[string]any) (any, error) {
	return service.ListSites(), nil
}

func handleCreateSite(ctx *ToolContext, args map[string]any) (any, error) {
	req := service.CreateSiteReq{
		Name:           getString(args, "name"),
		Domain:         getString(args, "domain"),
		Type:           getString(args, "type"),
		Root:           getString(args, "root"),
		ProxyPass:      getString(args, "proxy_pass"),
		RuntimeVersion: getString(args, "runtime_version"),
		StartCommand:   getString(args, "start_command"),
		EnvVars:        getString(args, "env_vars"),
		ProxyPort:      getInt(args, "proxy_port", 0),
		Framework:      getString(args, "framework"),
		CreateDB:       getBool(args, "create_db"),
		DBName:         getString(args, "db_name"),
	}
	if req.Domain == "" || req.Type == "" {
		return nil, errors.New("domain 和 type 为必填参数")
	}
	site, err := service.CreateSite(req)
	if err != nil {
		return nil, err
	}
	recordOp(ctx, "mcp.website_create", fmt.Sprintf("创建网站 %s (type=%s)", req.Domain, req.Type), "success")
	return site, nil
}

func handleDatabaseList(ctx *ToolContext, args map[string]any) (any, error) {
	return service.ListDatabases()
}

func handleAppList(ctx *ToolContext, args map[string]any) (any, error) {
	return service.ListApps(), nil
}

func handleFileList(ctx *ToolContext, args map[string]any) (any, error) {
	path := getString(args, "path")
	if path == "" {
		return nil, errors.New("参数 path 必填（目录绝对路径）")
	}
	items, err := service.ListDir(path)
	if err != nil {
		return nil, err
	}
	return items, nil
}

func handleExecCommand(ctx *ToolContext, args map[string]any) (any, error) {
	cmd := getString(args, "command")
	if cmd == "" {
		return nil, errors.New("参数 command 必填")
	}
	timeout := getInt(args, "timeout", 10)
	if timeout < 1 || timeout > 60 {
		timeout = 10
	}
	res, err := service.ExecCommand(cmd, time.Duration(timeout)*time.Second)
	status := "success"
	if err != nil {
		status = "failed"
	} else if res.ExitCode != 0 {
		status = "failed"
	}
	recordOp(ctx, "mcp.exec_command", fmt.Sprintf("执行命令: %s", cmd), status)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"exit_code": res.ExitCode,
		"stdout":    res.Stdout,
		"stderr":    res.Stderr,
	}, nil
}

// ---------- 辅助 ----------

// recordOp 记录 MCP 操作到面板操作日志（权限统一由面板管理）
func recordOp(ctx *ToolContext, action, detail, status string) {
	service.RecordOp(ctx.AdminID, action, detail, ctx.ClientIP, status)
}

func objectSchema(props map[string]any, required []string) map[string]any {
	if props == nil {
		props = map[string]any{}
	}
	return map[string]any{"type": "object", "properties": props, "required": required}
}

func stringSchema(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func intSchema(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

func boolSchema(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

func getString(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

func getInt(args map[string]any, key string, def int) int {
	if v, ok := args[key].(float64); ok {
		return int(v)
	}
	return def
}

func getBool(args map[string]any, key string) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return false
}
