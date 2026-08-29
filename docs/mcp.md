# kypanel MCP 接口文档

kypanel 实现了 [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) Streamable HTTP Server，
供 **Claude Code / Codex / Cursor** 等支持 MCP 的 AI 工具远程连接面板，
通过工具调用完成服务器管理、网站部署、故障排查等运维操作。

## 基本信息

| 项目 | 值 |
|---|---|
| 协议 | MCP Streamable HTTP（JSON-RPC 2.0 over HTTP POST） |
| 端点 | `POST /api/mcp` |
| 鉴权 | `Authorization: Bearer <token>` |
| 支持的协议版本 | `2025-06-18` / `2024-11-05` |
| Server 名称 | `kypanel` |
| Server 版本 | `0.1.0` |

## 鉴权

MCP 端点使用 `ApiTokenOrAuth("mcp")` 中间件，接受两种凭证：

1. **JWT（前端登录态）**：浏览器登录后拿到的 token。
2. **MCP API 令牌**：在面板「设置 → API 令牌」中创建的 `type=mcp` 令牌。
   - 与普通 API 令牌（`type=api`）隔离，`type=api` 的令牌不能访问 MCP 端点，防止误用。
   - 令牌为 36 位纯随机字符串，创建后仅显示一次，请妥善保存。
   - 令牌权限范围（scopes）为空时拥有全部权限；设置了 scopes 时按模块校验权限。

> 建议：AI 工具使用独立的 `type=mcp` 令牌连接，权限可控、可随时吊销。

## 支持的 JSON-RPC 方法

| 方法 | 说明 |
|---|---|
| `initialize` | 协商协议版本，声明能力（tools.listChanged=false） |
| `ping` | 心跳检测，返回空结果 |
| `tools/list` | 获取全部工具列表（名称、描述、入参 JSON Schema） |
| `tools/call` | 调用指定工具 |
| `notifications/initialized` / `notifications/cancelled` / `notifications/tools/list_changed` | 通知类消息，无需响应（HTTP 202） |

所有调用同步完成，直接以 `application/json` 响应，无需 SSE 流。

### 请求示例（tools/call）

```bash
curl -sS https://<host>:<port>/api/mcp \
  -H "Authorization: Bearer <mcp-token>" \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
      "name": "service_status",
      "arguments": { "key": "nginx" }
    }
  }'
```

### 响应格式

成功：

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "content": [
      { "type": "text", "text": "{...}" }
    ]
  }
}
```

工具执行失败（`isError=true`，属工具错误而非 JSON-RPC 错误）：

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "content": [ { "type": "text", "text": "错误信息" } ],
    "isError": true
  }
}
```

协议级错误（非法 JSON、方法不存在等）返回 JSON-RPC `error` 对象。

## 工具列表

| 工具名 | 说明 |
|---|---|
| `system_info` | 获取服务器系统信息 |
| `process_list` | 查看进程列表（按 CPU 排序，可过滤） |
| `service_status` | 查询面板托管服务的运行状态 |
| `website_list` | 获取已创建网站列表 |
| `website_create` | 创建新网站（含数据库可选） |
| `database_list` | 获取数据库列表 |
| `app_list` | 获取应用商店已安装/可用的运行环境列表 |
| `file_list` | 列出指定目录下的文件和子目录 |
| `exec_command` | 在服务器上执行只读/诊断命令（带危险命令拦截） |

### system_info

获取服务器系统信息：主机名、操作系统、CPU 核数、内存/磁盘使用、系统负载等。

无参数。

### process_list

查看服务器进程列表（按 CPU 占用排序），用于排查高负载进程。

| 参数 | 类型 | 必填 | 默认 | 说明 |
|---|---|---|---|---|
| `keyword` | string | 否 | 空 | 进程名关键字，如 nginx / php / mysql |
| `limit` | integer | 否 | 30 | 返回条数（1-200） |

### service_status

查询面板托管服务的运行状态。

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `key` | string | 是 | 服务 key，如 nginx / mysql / mysql8 / php / php82 / php74（可用 `app_list` 查看实际 key） |

### website_list

获取面板已创建的网站列表：域名、类型、运行状态、端口、SSL 状态等。

无参数。

### website_create

创建新网站。`type` 支持 `static`(静态/PHP) / `node` / `python` / `go` / `proxy`(反向代理)。
PHP 站点可同时创建数据库。该操作会写入面板操作日志。

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `domain` | string | 是 | 主域名，如 example.com；多域名用逗号分隔 |
| `type` | string | 是 | 站点类型：static / node / python / go / proxy |
| `name` | string | 否 | 站点名称，留空自动用主域名 |
| `root` | string | 否 | 站点根目录（绝对路径），留空自动创建 |
| `proxy_pass` | string | 否 | 反向代理目标，`type=proxy` 时必填，如 http://127.0.0.1:8080 |
| `runtime_version` | string | 否 | 运行环境版本，如 PHP 8.2 / Node 18 / Python 3.11 / Go 1.22 |
| `start_command` | string | 否 | node/python/go 启动命令，如 python app.py |
| `env_vars` | string | 否 | 环境变量，KEY=VALUE 每行一个 |
| `proxy_port` | integer | 否 | node/python/go 应用运行端口（nginx 反代目标） |
| `framework` | string | 否 | python 框架：flask / django / generic |
| `create_db` | boolean | 否 | 是否同时创建数据库（PHP 站点） |
| `db_name` | string | 否 | 数据库名，留空自动生成 |

### database_list

获取面板管理的数据库列表：库名、类型、字符集、大小等。

无参数。

### app_list

获取应用商店已安装/可用的运行环境列表（PHP / Python / Go / Node.js 各版本及安装状态）。

无参数。

### file_list

列出服务器指定目录下的文件和子目录（含大小、修改时间、权限），用于排查部署问题。

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `path` | string | 是 | 目录绝对路径，如 /www/wwwroot/example.com |

### exec_command

在服务器上执行诊断命令（故障排查）。**仅允许只读/诊断类命令**（面板内置危险命令拦截），
执行记录写入操作日志。

| 参数 | 类型 | 必填 | 默认 | 说明 |
|---|---|---|---|---|
| `command` | string | 是 | - | 要执行的命令，如 `systemctl status nginx`、`tail -n 100 /www/wwwlogs/example.com.error.log`、`df -h`、`free -m` |
| `timeout` | integer | 否 | 10 | 超时秒数（1-60） |

返回：`exit_code`、`stdout`、`stderr`。

## 在 AI 工具中配置

### Claude Code

在 Claude Code 中配置 MCP server（`claude mcp add`）：

```bash
claude mcp add kypanel \
  --transport http \
  --url https://<host>:<port>/api/mcp \
  --header "Authorization: Bearer <mcp-token>"
```

### Cursor

在 Cursor 的 MCP Server 配置中新增：

- **Type**: sse（或 streamable http，视版本支持）
- **URL**: `https://<host>:<port>/api/mcp`
- **Header**: `Authorization: Bearer <mcp-token>`

## 安全说明

- MCP 调用等同于管理员操作，全部写入操作日志（action 前缀 `mcp.`）。
- `exec_command` 只放行只读/诊断命令，危险命令由面板拦截。
- 子账号需具备对应模块权限才能通过 MCP 调用（PermissionGuard 统一校验）。
- 建议在面板「API 令牌」中为 AI 工具单独创建 `type=mcp` 令牌，按需设置 scopes，不再使用时吊销。
