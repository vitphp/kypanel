# kypanel API 接口文档

kypanel 后端基于 Go + Gin，所有业务 API 统一前缀 `/api`。

## 基本信息

| 项目 | 值 |
|---|---|
| Base URL | `http://<host>:<panel-port>/api`（HTTPS 由面板端口或反代提供） |
| 数据格式 | JSON（`Content-Type: application/json`） |
| 健康检查 | `GET /api/ping` → `{"code":0,"msg":"pong"}` |

### 安全入口（Security Entrance）

面板安装时可启用「安全入口」（6 位随机串）。启用后，**登录 / 验证码**接口路径内嵌入口值：

- 未启用：`POST /api/auth/login`、`GET /api/auth/captcha`、`GET /api/auth/captcha-check`
- 已启用：`POST /api/<entrance>/auth/login`、`GET /api/<entrance>/auth/captcha`、`GET /api/<entrance>/auth/captcha-check`

其余 `/api/*` 业务接口路径不受安全入口影响（但前端页面需要入口前缀才能访问）。

### 通用响应格式

所有接口统一返回：

```json
{ "code": 0, "msg": "ok", "data": { ... } }
```

- `code = 0` 表示成功，非 0 表示失败（`msg` 为错误信息）
- 特殊接口（文件下载、验证码图片、WebSocket）返回原始内容

### 权限分层

| 鉴权类型 | 说明 |
|---|---|
| 无需认证 | `/api/ping`、登录/验证码（路径内嵌安全入口） |
| JWT（登录态） | `Authorization: Bearer <jwt>`，前端登录后自动携带 |
| API 令牌 | `Authorization: Bearer <36位随机串>`，在面板「设置 → API 令牌」创建 |
| 临时访问令牌 | `lp_temp_` 开头，用于临时登录链接免密进入面板 |

鉴权中间件 `ApiTokenOrAuth` 的 token 提取规则：优先 `Authorization: Bearer` 头；
仅 GET 请求允许 URL 参数 `?token=`（写操作强制 Header，防 CSRF）。

## 认证与账号

### 登录（无需认证）

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/{entrance}/auth/login` | 登录。请求：`{username, password, captcha?, totp_code?}`；成功返回 `{token, need_captcha, need_totp}`；失败时响应携带 `need_captcha`/`need_totp` 字段 |
| GET | `/api/{entrance}/auth/captcha` | 获取验证码图片（IP 密码错误 1 次后才允许获取），返回 PNG |
| GET | `/api/{entrance}/auth/captcha-check` | 查询当前 IP 是否需要验证码：`{need_captcha: bool}` |

### 账号（需登录）

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/auth/logout` | 退出登录（清除 cookie） |
| POST | `/api/auth/change-password` | 修改密码（改后旧 token 立即失效） |
| GET | `/api/auth/profile` | 当前账号信息（admin_id / username） |
| GET | `/api/auth/permissions` | 当前用户权限模块列表（前端隐藏菜单用） |
| GET | `/api/auth/totp/status` | 查询双因素认证（2FA/TOTP）状态 |
| POST | `/api/auth/totp/enable-begin` | 开始启用 2FA（生成密钥 + 二维码 URI） |
| POST | `/api/auth/totp/enable-confirm` | 确认启用 2FA（需验证码） |
| POST | `/api/auth/totp/disable` | 关闭 2FA（需当前验证码） |

## 概览 / 系统

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/system/info` | 首页系统概览（CPU/内存/磁盘/负载等） |
| GET | `/api/dashboard/summary` | 概览聚合数据 |
| GET | `/api/dashboard/layout` | 概览卡片布局 |
| PUT | `/api/dashboard/layout` | 保存概览卡片布局 |
| POST | `/api/system/exec` | 通用命令执行（仅超管，前端需确认） |
| POST | `/api/system/restart-panel` | 重启面板服务（仅超管） |
| POST | `/api/system/restart-server` | 重启服务器（仅超管，前端二次确认） |

## 网站

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/site/list` | 网站列表 |
| POST | `/api/site/create` | 创建网站 |
| POST | `/api/site/action` | 网站操作（启动/停止/重启） |
| POST | `/api/site/delete` | 删除网站 |
| GET | `/api/site/detail` | 网站详情 |
| POST | `/api/site/settings` | 修改网站设置 |
| POST | `/api/site/settings/ssl` | 修改网站 SSL 设置 |
| GET | `/api/site/config` | 获取网站配置（nginx 等） |
| POST | `/api/site/config` | 保存网站配置 |
| GET | `/api/site/php-fpms` | PHP-FPM 池列表 |
| GET | `/api/site/subdirs` | 子目录列表 |
| GET | `/api/site/redirect/dirs` | 可重定向目录列表 |
| GET | `/api/site/blockip` | 封禁 IP 列表 |
| POST | `/api/site/blockip/add` | 添加封禁 IP |
| POST | `/api/site/blockip/delete` | 解除封禁 IP |
| GET | `/api/site/dir-tree` | 目录树 |
| GET | `/api/site/logs` | 网站访问/错误日志 |
| GET | `/api/site/logs/analyze` | 日志分析 |
| POST | `/api/site/remark` | 设置网站备注 |
| GET | `/api/site/stat` | 网站访问统计 |
| GET | `/api/site/stat/status` | 访问统计服务状态 |

### 创建网站（POST /api/site/create）请求字段

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `name` | string | 是 | 站点名称（唯一，创建后可在列表点击名称修改） |
| `type` | string | 是 | `php` / `static` / `node` / `python` / `go` / `proxy` |
| `domain` | string | 是 | 主域名（支持域名 / IP / `IP:端口` 形式） |
| `domains` | string | 否 | 附加域名，逗号分隔 |
| `port` | int | 是 | 监听端口（1–65535，对外访问端口） |
| `root` | string | 否 | 网站目录（绝对路径）。留空时自动推断为 `/www/wwwroot/<完整域名>`（如 `vltphp.n.05v.cn` → `/www/wwwroot/vltphp.n.05v.cn`；域名未填则回退站点名）。`proxy` 类型同样适用：留空会创建目录，Let's Encrypt HTTP-01 验证文件写入该目录 |
| `runtime_version` | string | 视类型 | PHP / Node / Python / Go 的运行时版本（如 `PHP 8.2`、`Node 20`、`Python 3.12`、`Go 1.24`）；必须为已安装版本，未安装返回错误 |
| `php_fpm` | string | 否 | PHP-FPM 版本标识（PHP 类型；不填自动按 runtime_version 解析） |
| `start_command` | string | 否 | 进程型启动命令（如 `python app.py`、`npm run start`） |
| `env_vars` | string | 否 | 环境变量，`KEY=VALUE` 每行一个 |
| `proxy_port` | int | 否 | 进程型应用端口，nginx 反代到 `127.0.0.1:<proxy_port>` |
| `proxy_pass` | string | proxy 必填 | 反代目标（如 `http://127.0.0.1:8080`） |
| `create_db` | bool | 否 | PHP 类型是否同时创建数据库 |
| `db_name` / `db_user` / `db_password` | string | 否 | 数据库名/用户/密码，留空自动生成 |
| `create_ftp` | bool | 否 | PHP 类型是否同时创建 FTP 账号 |
| `ftp_username` / `ftp_password` | string | 否 | FTP 用户名/密码，留空自动生成 |

> 注意：`port` 为必填，不填会返回参数错误；创建前需先安装对应运行时环境（PHP/Node/Python/Go）与 Web 服务器（Nginx/Apache）。

### SSL 证书

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/site/ssl/certbot` | 获取 Certbot 状态/可用证书 |
| GET | `/api/site/ssl/certs` | 证书列表 |
| POST | `/api/site/ssl/cert/delete` | 删除证书 |
| GET | `/api/site/ssl/cert/download` | 下载证书 |
| POST | `/api/site/ssl/apply-cert` | 申请证书（手动上传） |
| POST | `/api/site/ssl/letsencrypt` | 通过 Let's Encrypt 申请 |
| POST | `/api/site/ssl/apply` | 签发证书 |
| POST | `/api/site/ssl/dns-start` | 开始 DNS 验证 |
| POST | `/api/site/ssl/dns-check` | 检查 DNS 验证结果 |
| POST | `/api/site/ssl/dns-complete` | 完成 DNS 验证签发 |
| POST | `/api/site/ssl/renew` | 续签证书 |

## 网站安全（WAF 单站点）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/site/security/config` | 获取站点安全配置 |
| POST | `/api/site/security/config` | 保存站点安全配置 |
| GET | `/api/site/security/ip` | IP 黑白名单 |
| POST | `/api/site/security/ip/add` | 添加 IP 规则 |
| POST | `/api/site/security/ip/delete` | 删除 IP 规则 |
| GET | `/api/site/security/ua` | UA 过滤规则 |
| POST | `/api/site/security/ua/add` | 添加 UA 规则 |
| POST | `/api/site/security/ua/delete` | 删除 UA 规则 |
| GET | `/api/site/security/referer` | 防盗链规则 |
| POST | `/api/site/security/referer/add` | 添加防盗链规则 |
| POST | `/api/site/security/referer/delete` | 删除防盗链规则 |
| GET | `/api/site/security/rule` | 安全规则列表 |
| POST | `/api/site/security/rule/add` | 添加安全规则 |
| POST | `/api/site/security/rule/update` | 更新安全规则 |
| POST | `/api/site/security/rule/delete` | 删除安全规则 |
| GET | `/api/site/security/logs` | 安全拦截日志 |
| POST | `/api/site/security/logs/clear` | 清空拦截日志 |
| GET | `/api/site/security/stats` | 安全拦截统计 |

## 全局防火墙（WAF）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/waf/overview` | WAF 总览 |
| POST | `/api/waf/setting` | 保存 WAF 设置 |
| GET | `/api/waf/rules` | 规则列表 |
| POST | `/api/waf/rule/update` | 更新规则 |
| POST | `/api/waf/rule/add` | 添加规则 |
| POST | `/api/waf/rule/delete` | 删除规则 |
| POST | `/api/waf/rules/reset` | 重置规则 |
| GET | `/api/waf/iprules` | IP 规则 |
| POST | `/api/waf/iprule/add` | 添加 IP 规则 |
| POST | `/api/waf/iprule/delete` | 删除 IP 规则 |
| GET | `/api/waf/cc` | CC 防护配置 |
| POST | `/api/waf/cc/save` | 保存 CC 防护配置 |
| GET | `/api/waf/logs` | 拦截日志 |
| POST | `/api/waf/logs/clear` | 清空拦截日志 |
| GET | `/api/waf/stats/top_ip` | 拦截 Top IP |
| GET | `/api/waf/stats/top_category` | 拦截 Top 攻击类型 |

## 数据库

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/database/service-status` | 数据库服务状态 |
| POST | `/api/database/service-action` | 数据库服务操作（启动/停止/重启） |
| GET | `/api/database/root-info` | 数据库 root 信息 |
| POST | `/api/database/root-password` | 修改 root 密码 |
| GET | `/api/database/types` | 支持的数据库类型 |
| GET | `/api/database/status` | 数据库状态 |
| GET | `/api/database/list` | 数据库列表 |
| POST | `/api/database/create` | 创建数据库 |
| POST | `/api/database/delete` | 删除数据库 |
| POST | `/api/database/comment` | 设置数据库备注 |
| GET | `/api/database/pma/status` | phpMyAdmin 可用状态 |
| POST | `/api/database/pma/token` | 获取 phpMyAdmin 访问 token |
| POST | `/api/database/password` | 修改数据库密码 |
| POST | `/api/database/perms` | 修改数据库权限 |
| GET | `/api/database/backup/list` | 数据库备份列表 |
| POST | `/api/database/backup/create` | 创建数据库备份 |
| POST | `/api/database/backup/restore` | 恢复数据库备份 |
| POST | `/api/database/backup/delete` | 删除数据库备份 |
| GET | `/api/database/backup/download` | 下载数据库备份 |
| POST | `/api/database/import` | 导入 SQL 文件 |

> 数据库引擎相关服务路由由 `registerDatabaseServiceRoutes` 注册（如 MySQL/MongoDB/Redis 各引擎的管理接口）。

## Docker

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/docker/status` | Docker 服务状态 |
| GET | `/api/docker/containers` | 容器列表 |
| POST | `/api/docker/containers/create` | 创建容器 |
| POST | `/api/docker/containers/:id/action` | 容器操作（启动/停止/重启/删除） |
| DELETE | `/api/docker/containers/:id` | 删除容器 |
| GET | `/api/docker/containers/:id/logs` | 容器日志 |
| GET | `/api/docker/images` | 镜像列表 |
| GET | `/api/docker/networks` | 网络列表 |
| POST | `/api/docker/networks/create` | 创建网络 |
| DELETE | `/api/docker/networks/:id` | 删除网络 |
| GET | `/api/docker/apps` | Docker 应用商店列表 |
| POST | `/api/docker/apps/install` | 安装 Docker 应用 |
| POST | `/api/docker/apps/uninstall` | 卸载 Docker 应用 |

## FTP

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/ftp/status` | FTP 服务状态 |
| GET | `/api/ftp/list` | FTP 账号列表 |
| POST | `/api/ftp/create` | 创建 FTP 账号 |
| POST | `/api/ftp/delete` | 删除 FTP 账号 |
| POST | `/api/ftp/toggle` | 启用/禁用 FTP 账号 |

## 文件管理

| 方法 | 路径 | 说明 |
|---|---|---|
| GET/POST | `/api/file/list` | 目录列表 |
| GET/POST | `/api/file/read` | 读取文件内容 |
| POST | `/api/file/write` | 写入文件 |
| POST | `/api/file/mkdir` | 创建目录 |
| POST | `/api/file/create` | 创建文件 |
| POST | `/api/file/rename` | 重命名/移动 |
| POST | `/api/file/delete` | 删除（进回收站） |
| POST | `/api/file/copy` | 复制 |
| POST | `/api/file/mv` | 移动 |
| POST | `/api/file/remote_download` | 远程下载 URL（异步任务） |
| GET | `/api/file/remote_download/tasks` | 远程下载任务列表/进度 |
| POST | `/api/file/upload` | 上传文件（字节级断点续传，流式） |
| GET | `/api/file/upload/offset` | 查询上传偏移（续传起点） |
| POST | `/api/file/upload/reset` | 上传前清理（覆盖） |
| GET | `/api/file/download` | 下载文件 |
| GET | `/api/file/preview` | 预览文件（图片/文本） |
| POST | `/api/file/preview-token` | 获取临时预览 token |
| GET | `/api/file/du` | 目录占用统计 |
| POST | `/api/file/chmod` | 修改权限 |
| GET | `/api/file/users` | 系统用户列表 |
| GET | `/api/file/recommend_owner` | 推荐属主 |
| GET | `/api/file/groups` | 系统用户组列表 |
| POST | `/api/file/zip` | 压缩 |
| POST | `/api/file/unzip` | 解压 |
| POST | `/api/file/search` | 文件搜索 |
| POST | `/api/file/syntax_check` | 语法检查 |
| GET | `/api/file/trash/list` | 回收站列表 |
| POST | `/api/file/trash/restore` | 从回收站恢复 |
| POST | `/api/file/trash/purge` | 彻底删除 |
| POST | `/api/file/trash/empty` | 清空回收站 |

## 运行环境 / 应用商店

### 运行环境（Runtime）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/env/info` | 运行环境信息 |
| POST | `/api/env/service` | 环境服务操作 |
| GET | `/api/env/php/extensions` | PHP 扩展列表 |
| POST | `/api/env/php/extensions/install` | 安装 PHP 扩展 |
| POST | `/api/env/php/extensions/uninstall` | 卸载 PHP 扩展 |
| GET | `/api/env/php/ini` | 读取 php.ini |
| POST | `/api/env/php/ini` | 保存 php.ini |
| GET | `/api/env/php/fpm-config` | 读取 FPM 配置 |
| POST | `/api/env/php/fpm-config` | 保存 FPM 配置 |
| GET | `/api/env/php/status` | PHP-FPM 状态 |
| GET | `/api/env/php/log` | PHP 错误日志 |
| GET | `/api/env/php/phpinfo` | phpinfo |
| GET | `/api/env/packages` | 系统软件包列表 |
| GET | `/api/env/global-config` | 全局配置 |
| POST | `/api/env/global-config` | 保存全局配置 |

### 应用商店

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/apps/list` | 应用列表 |
| GET | `/api/apps/categories` | 应用分类 |
| GET | `/api/apps/php-versions` | PHP 版本列表 |
| POST | `/api/apps/install` | 安装应用（PHP/Python/Go/Node 等） |
| GET | `/api/apps/env-status` | 环境安装状态 |
| POST | `/api/apps/uninstall` | 卸载应用 |
| POST | `/api/apps/service/action` | 服务操作 |
| GET | `/api/apps/service/status` | 服务状态 |
| GET | `/api/apps/log` | 安装日志 |
| GET | `/api/apps/tasks` | 安装任务列表 |
| POST | `/api/apps/tasks/cancel` | 取消安装任务 |

## 监控

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/monitor/config` | 监控配置 |
| POST | `/api/monitor/config` | 保存监控配置 |
| GET | `/api/monitor/history` | 历史监控数据 |
| GET | `/api/monitor/current` | 当前监控数据 |
| GET | `/api/monitor/size` | 监控数据占用 |
| POST | `/api/monitor/clear` | 清空监控数据 |
| GET | `/api/monitor/stats` | 监控统计 |

## 进程

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/process/list` | 进程列表 |
| POST | `/api/process/kill` | 结束进程 |

## 计划任务（Cron）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/cron/list` | 任务列表 |
| GET | `/api/cron/templates` | 任务模板 |
| POST | `/api/cron/preview` | 预览任务 |
| GET | `/api/cron/sites` | 网站列表（备份任务用） |
| GET | `/api/cron/databases` | 数据库列表（备份任务用） |
| POST | `/api/cron/save` | 保存任务（仅超管） |

## 备份中心

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/backup/list` | 备份列表 |
| POST | `/api/backup/create` | 创建备份 |
| POST | `/api/backup/delete` | 删除备份 |
| POST | `/api/backup/restore` | 恢复备份 |
| POST | `/api/backup/download-token` | 获取下载 token |
| GET | `/api/backup/storages` | 备份存储配置 |
| POST | `/api/backup/storages` | 保存备份存储配置 |
| POST | `/api/backup/storage-delete` | 删除存储配置 |
| POST | `/api/backup/upload` | 上传备份 |

## 防火墙（系统级）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/security/status` | 防火墙状态 |
| GET | `/api/security/rules` | 防火墙规则 |
| POST | `/api/security/rule/add` | 添加规则 |
| POST | `/api/security/rule/update` | 更新规则 |
| POST | `/api/security/rule/delete` | 删除规则 |
| POST | `/api/security/rule/remark` | 规则备注 |
| POST | `/api/security/rules/apply` | 应用规则 |
| GET | `/api/security/ip/lookup` | IP 归属查询 |
| POST | `/api/security/ip/reload` | 重载 IP 库 |

## 操作日志

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/oplog/list` | 操作日志列表 |
| POST | `/api/oplog/export-token` | 获取日志导出 token |
| POST | `/api/oplog/clear` | 清空操作日志 |

## 设置

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/settings/info` | 面板设置信息 |
| POST | `/api/settings/username` | 修改用户名 |
| POST | `/api/settings/port` | 修改面板端口 |
| POST | `/api/settings/domain` | 绑定域名 |
| POST | `/api/settings/security-entrance` | 修改安全入口 |
| GET | `/api/settings/security-entrance/generate` | 生成新安全入口 |
| POST | `/api/settings/panel-name` | 修改面板名称 |
| POST | `/api/settings/mysql-password` | 修改 MySQL root 密码 |
| GET | `/api/settings/litessl` | LiteSSL 配置 |
| POST | `/api/settings/litessl` | 保存 LiteSSL 配置 |
| GET | `/api/logs/system` | 系统日志 |
| POST | `/api/logs/system/clear` | 清空系统日志 |
| GET | `/api/logs/install-list` | 安装日志列表 |
| GET | `/api/settings/login-allowlist` | 登录 IP 白名单 |
| POST | `/api/settings/login-allowlist` | 保存登录白名单 |
| GET | `/api/settings/sessions` | 在线会话列表 |
| POST | `/api/settings/session/kick` | 踢下线指定会话 |
| POST | `/api/settings/session/kick-all` | 踢下线全部会话 |

## API 令牌

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/api-tokens` | 令牌列表 |
| POST | `/api/api-tokens` | 创建令牌（body 含 `type`: api/mcp，`scopes`） |
| POST | `/api/api-tokens/:id/delete` | 删除令牌 |

> 创建的令牌仅显示一次；`type=mcp` 的令牌用于 MCP 端点，`type=api` 用于普通 API。

## 用户与角色（仅超管）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/roles` | 角色列表 |
| POST | `/api/roles` | 保存角色 |
| POST | `/api/roles/delete` | 删除角色 |
| GET | `/api/roles/modules` | 权限模块列表 |
| GET | `/api/users` | 子账号列表 |
| POST | `/api/users` | 创建子账号 |
| POST | `/api/users/role` | 修改账号角色 |
| POST | `/api/users/password` | 重置账号密码 |
| POST | `/api/users/delete` | 删除账号 |

## 临时访问

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/temp-access` | 临时访问链接列表 |
| POST | `/api/temp-access` | 创建临时访问链接 |
| POST | `/api/temp-access/:id/delete` | 删除链接 |
| POST | `/api/temp-access/:id/toggle` | 启用/禁用链接 |
| GET | `/api/temp-access/use-logs` | 使用记录 |
| GET | `/api/temp-access/operations` | 关联操作日志 |
| GET | `/api/temp-access/link` | 获取访问链接 |

> 前端 `GET /temp-login?token=lp_temp_xxx` 为免密进入后台的唯一公开入口（token 无效返回 404）。

## 网站搬家（迁移）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/migrate/env` | 迁移环境检测 |
| POST | `/api/migrate/detect-panel` | 检测目标面板类型 |
| POST | `/api/migrate/export` | 导出（本面板） |
| POST | `/api/migrate/export/precheck` | 导出预检 |
| POST | `/api/migrate/export/bt` | 导出为第三方面板格式 |
| POST | `/api/migrate/export/bt/precheck` | 第三方面板导出预检 |
| POST | `/api/migrate/bt/precheck` | 第三方面板迁移预检 |
| GET | `/api/migrate/exports` | 导出任务列表 |
| DELETE | `/api/migrate/exports/:id` | 删除导出任务 |
| GET | `/api/migrate/download/:id` | 下载迁移包 |
| POST | `/api/migrate/import/fetch-sites` | 获取远程站点列表 |
| POST | `/api/migrate/import/plan` | 生成迁移计划 |
| POST | `/api/migrate/import/run` | 执行迁移 |
| GET | `/api/migrate/tasks/:id` | 迁移任务状态 |
| GET | `/api/migrate/remote/ping` | 远程连通性测试 |
| GET | `/api/migrate/remote/sites` | 远程站点列表 |
| GET | `/api/migrate/remote/env` | 远程环境信息 |
| POST | `/api/migrate/remote/pack` | 远程打包 |
| GET | `/api/migrate/remote/download` | 下载远程迁移包 |

## 更新

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/update/check` | 检查面板新版本（`?arch=amd64`） |
| POST | `/api/update/upgrade` | 执行升级 |

## 告警通知

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/alert/settings` | 告警设置 |
| POST | `/api/alert/settings` | 保存告警设置 |
| POST | `/api/alert/test-channel` | 测试通知渠道 |
| POST | `/api/alert/clear-logs` | 清空告警记录 |

## 特殊分组（无需 JWT/API Token）

### WebSocket 终端

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/terminal` | WebSocket 终端（URL 携带 `?token=`，由内部校验） |

### 下载 / 预览（一次性 token）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/backup/download` | 下载备份 |
| GET | `/api/cron/backup-download` | 下载计划任务备份 |
| GET | `/api/oplog/export` | 导出操作日志 |
| GET | `/api/file/raw` | 原始文件访问 |

> 这些接口无法携带 Bearer Header，统一使用 URL 一次性 token，由各 handler 内部校验短期 token，仅过 SecurityGuard（IP 黑白名单）。

### phpMyAdmin 反向代理

| 方法 | 路径 | 说明 |
|---|---|---|
| ANY | `/phpmyadmin/*any` | phpMyAdmin 反向代理（首次 query token 鉴权后种 cookie） |

### MCP

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/mcp` | MCP Streamable HTTP 端点，详见 [MCP 文档](mcp.md) |

## API 令牌创建示例

```bash
# 登录获取 JWT
curl -sS https://<host>:<port>/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"***"}' \
  -c cookies.txt

# 创建 type=api 令牌（需登录）
curl -sS https://<host>:<port>/api/api-tokens \
  -b cookies.txt \
  -H "Content-Type: application/json" \
  -d '{"name":"my-script","type":"api","scopes":["site","database"]}'
```

调用业务接口：

```bash
curl -sS https://<host>:<port>/api/site/list \
  -H "Authorization: Bearer <api-token>"
```
