# kypanel

Linux 服务器管理面板。

- **后端**：Go (Gin) — 单二进制，SQLite 存储
- **前端**：Vue 3 + Vite，构建产物输出到 `webui/dist`（支持 `go:embed` 内嵌或前后端分离部署）
- **目标系统**：Linux（Ubuntu / Debian / CentOS / Rocky，x86_64 + ARM64）

## 构建

### 1. 构建前端

```bash
cd web
npm install
npm run build   # 产物输出到 webui/dist
```

### 2. 构建后端（交叉编译）

Linux：

```bash
./scripts/build.sh amd64   # 或 arm64
```

Windows（PowerShell）：

```powershell
powershell -File scripts/cross-build.ps1
```

构建产物统一输出到 `bin/`：

| 产物 | 说明 |
|---|---|
| `bin/kypanel_amd64` | 后端二进制（交叉编译，内嵌前端） |
| `bin/panel-web.tar.gz` | 前端独立包（可单独替换部署） |
| `bin/ip2region.xdb` | IP 归属离线库（约 11MB，独立部署） |
| `bin/i.sh` | 服务器安装脚本 |

## 部署

前后端分离部署：上传后端二进制 + 前端包到服务器，前端解压到数据目录的 `web/` 文件夹。

- 后端启动时磁盘前端优先（存在 `index.html` 时优先托管磁盘前端）
- 前端可单独替换、刷新即生效，无需重新编译后端

## 目录结构

```
kypanel/
├── cmd/panel/           # 后端入口
├── internal/            # Go 后端（config/logger/model/service/router/middleware/utils）
├── web/                 # Vue 3 前端源码
├── webui/               # 前端构建产物（go:embed 目标目录）
├── scripts/             # 构建/安装/运维脚本
└── data/                # 运行时数据（SQLite、日志、IP 库，不提交）
```
