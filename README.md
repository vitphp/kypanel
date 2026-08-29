# kypanel（开猿运维）

[![官网](https://img.shields.io/badge/官网-panel.apihot.cn-blue)](https://panel.apihot.cn/)
[![License](https://img.shields.io/badge/License-Apache--2.0-green.svg)](LICENSE)

**kypanel（开猿运维）** 是一款开源、免费、自托管的 Linux 服务器管理面板。提供网站管理、数据库、文件管理、安全中心、Web 终端、Docker、WAF 应用防护、监控告警等一站式能力，让服务器运维像使用图形界面一样轻松。

- 🏠 **官网**：https://panel.apihot.cn/
- 📖 **在线文档**：本仓库 `docs/` 目录（[API](docs/api.md) / [MCP](docs/mcp.md)）
- 🐳 **内置 AI 运维**：原生支持 MCP 协议，可直接接入 Claude Code / Codex / Cursor

## 快速开始

### 一键安装（官方脚本）

```bash
curl -fsSL https://panel.apihot.cn/sh/i.sh | bash
```

> 适用于 Ubuntu / Debian / CentOS / Rocky（x86_64 + ARM64）。
> 安装完成后终端会输出访问地址、随机端口、安全入口、管理员账号与随机密码（仅显示一次）。

指定端口安装：

```bash
curl -fsSL https://panel.apihot.cn/sh/i.sh | bash -s -- --port 9999
```

首次访问（安装脚本默认启用 HTTPS 自签名证书，浏览器提示不安全属正常，点「高级 → 继续访问」即可）：

```
https://<服务器IP>:<随机端口>/<安全入口>/
```

> **安全入口**：6 位随机串，相当于面板的「隐形门牌」，未知入口则访问返回 404，可有效防止密码爆破与扫描攻击。

## 功能特性

| 功能模块 | 说明 |
|---|---|
| 🌐 **网站管理** | 支持静态、PHP、Node.js、Python、Go、反向代理等站点类型；一键创建、SSL 证书申请（Let's Encrypt / DNS 验证 / 自动续签）、访问日志分析、站点访问统计、目录树管理、单站点安全防护 |
| 🗄️ **数据库管理** | MySQL / MariaDB / SQLServer / MongoDB / Redis 多引擎管理；建库建账号、账号授权、导入导出、定时备份与一键恢复、网页版 phpMyAdmin |
| 🐳 **Docker 管理** | 容器 / 镜像 / 网络 / 数据卷直观管理；容器日志实时查看、一键启停重启；内置 Docker 应用商店，常用应用一键部署 |
| 📁 **文件管理** | 在线文件浏览、上传下载（GB 级大文件断点续传）、远程下载 URL、压缩解压、在线编辑（内置代码高亮）、图片 / 视频 / 音乐在线预览、目录「终端」一键直达、删除进回收站可随时找回 |
| 🛡️ **安全中心** | 防火墙（firewalld / nftables / iptables）端口放行与 IP 拉黑、按城市 / 国家离线封锁（内置 ip2region 库）、IP 归属地查询、登录 IP 白名单、安全入口、双因素认证（2FA） |
| ⚔️ **WAF 应用防护** | 全局 + 单站点两级防护；内置 34 条攻击规则（10 大类）、自定义规则、IP 黑白名单、CC 防护（Nginx limit_req）、UA 过滤、防盗链、攻击日志与统计，Nginx / Apache 双支持 |
| 🖥️ **Web 终端** | 浏览器内直接打开 SSH 终端（WebSocket），文件管理任意目录一键进入对应终端 |
| 📈 **监控与进程** | CPU / 内存 / 磁盘 / 网络实时曲线与历史数据；进程列表与一键结束、服务器负载总览 |
| ⏰ **计划任务 & 备份** | Cron 定时任务可视化配置（访问 URL / 备份网站 / 备份数据库 / 执行命令）；集中备份中心，多存储可配 |
| 🔄 **网站搬家** | 支持同面板 / 跨面板（含宝塔）站点一键导出迁移，远程站点自动打包下载导入 |
| 🔑 **多用户与审计** | 子账号、角色权限（内置运维 / 只读角色，可自定义）、操作日志全量审计、在线会话管理与踢下线、登录失败自动验证码 |
| 🤖 **AI 原生运维** | 内置 MCP Server，Claude Code / Codex / Cursor 等 AI 工具可直连面板完成查询状态、部署网站、排查故障 |

## 项目特性

- 💎 **开箱即用**：单二进制部署（前后端内嵌），内置 SQLite 数据库，零外部依赖，支持 x86_64 与 ARM64
- 🔐 **安全可靠**：JWT 认证（HttpOnly Cookie）、密码 bcrypt 加密存储、操作审计、安全入口防爆破、防火墙默认拒绝（default-drop）、敏感文件权限 600 收紧、明文密码自动清除
- 🚀 **持续迭代**：活跃维护，支持在线升级（`--update` 保留数据）、系统服务自注册与开机自启、HTTPS 证书自动续签
- ⚙️ **内置 CLI**：`ky` 命令行菜单，忘记密码 / 改端口 / 磁盘清理 / 配置 HTTPS 不依赖面板

## 功能截图

| 概览 | 网站 |
|---|---|
| ![概览](docs/screenshots/overview.png) | ![网站](docs/screenshots/website.png) |

| 数据库 | Docker |
|---|---|
| ![数据库](docs/screenshots/database.png) | ![Docker](docs/screenshots/docker.png) |

| 应用商店 |
|---|
| ![应用商店](docs/screenshots/appstore.png) |

## 文档

| 文档 | 说明 |
|---|---|
| [API 接口文档](docs/api.md) | 全部 REST API 路由（约 200 个）、鉴权体系（JWT / API 令牌 / 临时令牌）、安全入口规则、请求示例 |
| [MCP 接口文档](docs/mcp.md) | MCP 协议说明、9 个内置工具参数表、Claude Code / Cursor 接入配置示例 |
| [技术架构](#技术架构) | 前后端技术栈、目录结构与部署形态 |

## 技术架构

| 层 | 技术 |
|---|---|
| 后端 | Go + Gin，单二进制（`go:embed` 内嵌前端） |
| 数据库 | SQLite（WAL 模式，零运维） |
| 前端 | Vue 3 + Vite + Element Plus + Pinia + Vue Router |
| 终端 | @xterm/xterm（WebSocket） |
| 鉴权 | JWT（HttpOnly Cookie）+ API 令牌（36 位随机）+ 临时访问令牌，三层体系 |
| 防火墙 | firewalld → nftables → iptables 自动适配 |
| IP 归属 | 纯 Go 实现 ip2region.xdb 离线解析，零第三方依赖 |
| 监控 | 后台采集协程 + 历史数据入库（SQLite） |

```
kypanel/
├── cmd/panel/           # 后端入口（含 CLI / 自动续签 / 应用导出等子命令）
├── internal/
│   ├── cli/             # ky 命令行管理菜单
│   ├── config/          # 配置加载与持久化
│   ├── logger/          # 日志（文件 + 控制台，按天滚动）
│   ├── model/           # GORM 数据模型（含 AutoMigrate）
│   ├── middleware/      # JWT / API 令牌 / 权限 / 安全守卫中间件
│   ├── router/          # 全部 API 路由（按模块拆分 27 个文件）
│   ├── service/         # 业务逻辑（站点/数据库/Docker/WAF/安全/迁移…）
│   └── utils/           # 通用工具（JWT/加密/响应封装）
├── web/                 # Vue 3 前端源码
├── webui/               # 前端构建产物（go:embed 目标目录）
├── docs/                # 文档（API / MCP / 截图）
├── scripts/             # 构建 / 安装 / 卸载 / 证书续签脚本
└── data/                # 运行时数据（SQLite、日志、IP 库，不提交）
```

## 安装

### 方式一：官方一键脚本（推荐）

```bash
curl -fsSL https://panel.apihot.cn/sh/i.sh | bash
```

### 方式二：指定端口 / 自定义资源源

```bash
# 指定端口
curl -fsSL https://panel.apihot.cn/sh/i.sh | bash -s -- --port 9999

# 自定义资源目录（二进制 + ip2region.xdb 与 i.sh 同目录）
curl -fsSL https://你的域名/sh/i.sh | bash -s -- --base-url https://你的域名/sh
```

### 方式三：离线 / 本地安装

把 `kypanel_amd64`（后端）与 `i.sh` 放在同一目录：

```bash
bash install.sh                          # 自动识别同目录二进制
bash install.sh --port 9999              # 指定端口
```

### 方式四：手动部署（前后端分离）

1. 上传 `bin/kypanel_amd64` 与 `bin/panel-web.tar.gz` 到服务器
2. 解压前端包到面板数据目录的 `web/` 文件夹
3. 启动：`/opt/kypanel/panel -config /opt/kypanel/config.json`

> 后端启动时磁盘前端优先（存在 `index.html` 时优先托管磁盘前端），
> 前端可单独替换、刷新即生效，无需重新编译后端。

### 环境变量（首次启动初始化）

| 变量 | 说明 |
|---|---|
| `PANEL_ADMIN_USER` | 初始管理员用户名（默认 `admin`） |
| `PANEL_ADMIN_PASS` | 初始密码（不设置则随机生成强密码并打印；创建后自动从磁盘清除明文） |
| `PANEL_DATA_DIR` | 数据目录覆盖（开发/测试用） |
| `PANEL_PORT` | 安装脚本指定端口 |

### 命令行参数

```bash
./panel -config /opt/kypanel/config.json   # 指定配置启动
./panel -version                            # 版本信息
./panel -renew-ssl                          # 自动续签 2 天内到期证书后退出（供计划任务每日调用）
./panel -export-apps apps.json              # 导出内置应用数据
```

## 命令行管理（ky）

**`ky`** 是随面板安装的服务器端命令行工具：安装脚本会自动创建软链接 `/usr/local/bin/ky → 面板二进制`。当面板 Web 界面不可用时（忘记密码、端口被改、服务起不来等），SSH 登录服务器直接用 `ky` 即可完成全部日常运维，无需打开浏览器。

### 进入方式（任选其一）

```bash
ky                 # 软链接，直接进入菜单
panel ky           # 显式传入 ky 参数
panel menu         # 或 menu 参数
```

> `ky` 是 root 工具：直接读写 `/opt/kypanel/config.json` 与 SQLite 数据库，不经过 Web API。

### 菜单功能

```
============== kypanel 命令行 ==============
(1) 启动面板        (2) 停止面板       (3) 重启面板
(4) 修改面板端口    (5) 修改面板用户名  (6) 修改面板密码
(7) 查看面板信息    (8) 磁盘清理工具   (9) 配置 HTTPS
(0) 退出
=============================================
```

| 编号 | 功能 | 说明 |
|---|---|---|
| 1/2/3 | 启停 / 重启面板 | 封装 `systemctl`，自动检测运行状态；启动失败会提示查看 `journalctl -u kypanel -n 30` |
| 4 | 修改面板端口 | 校验 8888-65535、检测端口占用；**自动放行新端口到防火墙**（可顺手移除旧端口），写配置后自动重启面板并打印新访问地址 |
| 5 | 修改面板用户名 | 校验 `[a-zA-Z0-9_]{3,32}` 并查重；`token_ver +1` 使所有已登录会话立即失效，强制重新登录 |
| 6 | 修改面板密码 | root 直接重置（**不要求旧密码**）；bcrypt 加密入库、`token_ver +1` 踢下线全部会话，自动重启生效 |
| 7 | 查看面板信息 | 运行状态 / 端口 / HTTPS / 安全入口 / 完整访问地址 / 管理员账号 / 安装目录 / 配置路径 / 磁盘使用与日志大小 |
| 8 | 磁盘清理工具 | 交互式扫描并清理面板相关无用文件，释放磁盘空间 |
| 9 | 配置 HTTPS | 一键启用（openssl 自动生成 3650 天自签名证书，域名/IP 可自动检测）或关闭 HTTPS，配置后自动重启生效 |

### 典型场景

| 场景 | 操作 |
|---|---|
| 忘记面板密码 | `ky` → 输入 `6` → 输入两遍新密码，自动重启生效 |
| 面板端口被占用 / 想换端口 | `ky` → 输入 `4` → 输入新端口（自动放行防火墙），完成后用打印的新地址访问 |
| 登录后所有接口报「登录已失效」 | `ky` → `5` 或 `6` 重设账号密码，`token_ver +1` 会强制刷新所有会话 |
| HTTPS 证书不受信任 | `ky` → `9` → `1` 重新生成自签名证书 |
| 面板服务异常起不来 | `ky` → `7` 查看配置与磁盘，`ky` → `1` 重试启动，配合 `journalctl -u kypanel -n 30` 看日志 |

## 升级 / 卸载

```bash
# 升级面板（保留数据与配置）
bash <(curl -fsSL https://panel.apihot.cn/sh/i.sh) -- --update

# 全新重装（删除全部数据）
bash <(curl -fsSL https://panel.apihot.cn/sh/i.sh) -- --reinstall

# 卸载面板（停止服务并删除全部文件）
bash <(curl -fsSL https://panel.apihot.cn/sh/i.sh) -- --uninstall
```

## 开发

### 构建前端

```bash
cd web
npm install
npm run build     # 产物输出到 webui/dist
```

### 构建后端（交叉编译）

Linux：

```bash
./scripts/build.sh amd64    # 或 arm64
```

Windows（PowerShell）：

```powershell
powershell -File scripts/cross-build.ps1
```

构建产物统一输出到 `bin/`：

| 产物 | 说明 |
|---|---|
| `bin/kypanel_amd64` | 后端二进制（交叉编译，内嵌前端，strip 后约 31MB） |
| `bin/panel-web.tar.gz` | 前端独立包（可单独替换部署） |
| `bin/ip2region.xdb` | IP 归属离线库（约 11MB，独立部署） |
| `bin/i.sh` | 服务器安装脚本 |

## 常见问题

| 问题 | 解决 |
|---|---|
| 浏览器提示证书不受信任 | 安装脚本默认启用 HTTPS 自签名证书，点「高级 → 继续访问」即可；可后续在面板设置或 `ky → (9) 配置 HTTPS` 更换 |
| 面板打不开 | 检查云服务器安全组是否放行面板端口（腾讯云 / 阿里云控制台 → 安全组） |
| 忘记面板密码 | SSH 登录服务器执行 `ky` → 选择 6「修改面板密码」 |
| 登录显示需要验证码 | 密码错误 1 次后触发验证码，属正常防爆破机制；输入正确验证码即可 |
| HTTPS 证书失效 | 执行 `bash <(curl -fsSL https://panel.apihot.cn/sh/i.sh) -- --fix-https` 一键修复 |
| 网站证书到期 | 面板内置每日自动续签（`-renew-ssl`），也可在网站 SSL 设置中手动续签 |

## AI 运维（MCP）

kypanel 内置 MCP Server（`POST /api/mcp`，Streamable HTTP），提供 9 个运维工具：

`system_info`、`process_list`、`service_status`、`website_list`、`website_create`、`database_list`、`app_list`、`file_list`、`exec_command`

```bash
# Claude Code 接入
claude mcp add kypanel \
  --transport http \
  --url https://<host>:<port>/api/mcp \
  --header "Authorization: Bearer <mcp-token>"
```

> 详细用法见 [MCP 接口文档](docs/mcp.md)。建议在面板「API 令牌」中为 AI 工具单独创建 `type=mcp` 令牌并设置 scopes。

## 许可

Apache-2.0

---

**kypanel** · 开源的 Linux 服务器管理面板 · [官网](https://panel.apihot.cn/) · 让服务器运维如此简单
