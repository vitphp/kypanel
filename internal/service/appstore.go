package service

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"kypanel/internal/config"
	"kypanel/internal/model"
)

// ============================================================================
// 应用商店模块结构说明（按职责分为四大块，便于定位与维护）：
//   1) 元数据与远程拉取   —— AppMeta/AppCategory 类型、内置应用注册、远程 app 拉取
//   2) 安装脚本生成       —— php/node/python/go 及 pma/vsftpd/mongodb/sqlserver 脚本
//   3) 安装/卸载任务执行   —— 串行队列、InstallApp/UninstallApp、ServiceAction
//   4) 运行时探测与环境   —— 版本探测、phpMyAdmin、环境状态、日志读取
// ============================================================================

// AppMeta 应用商店中的软件元数据
type AppMeta struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Category    string   `json:"category"` // server / database / cache / runtime
	Description string   `json:"description"`
	Icon        string   `json:"icon"`        // 前端图标名（Element Plus）
	Service     string   `json:"service"`     // systemd 服务名，php 用 "php-fpm" 表示需探测
	VersionCmd  string   `json:"version_cmd"` // 版本探测命令（输出到 stdout/stderr）
	AptPackages []string `json:"-"`
	YumPackages []string `json:"-"`
	// AptFallbackPackages 在 apt 安装 AptPackages 失败时（如包不存在）尝试安装的替代包
	AptFallbackPackages []string `json:"-"`
	// AptFallbackService 安装替代包后使用的 systemd 服务名（默认与 Service 相同）
	AptFallbackService string `json:"-"`
	// AptRemovePatterns apt 卸载时额外按通配模式清理的包名前缀（如 postgresql 装的是
	// postgresql-17 等子包，apt remove postgresql 会报"包未安装"导致假成功）。
	// 卸载时先用 dpkg -l 筛出实际安装的匹配包再 remove，避免通配无匹配时 apt 报错。
	AptRemovePatterns []string `json:"-"`
	InstallScript      string `json:"-"` // 自定义安装脚本（非 apt/yum 通用流程的应用，如 phpMyAdmin、多版本 PHP）
	UninstallScript    string `json:"-"` // 自定义卸载脚本
	// InstallMarkFile 安装标记文件：设置了该字段的应用没有真实的"可卸载二进制"
	// （如 site-migrate 网站搬家，探测命令 tar --version 恒真——tar 是系统自带工具，
	//  卸载脚本删不掉，卸载后探测仍命中导致状态回弹为「已安装」）。
	// 面板安装成功后 touch 该文件、卸载成功后删除；探测优先以标记文件是否存在为准。
	// 路径支持 {DataDir} 占位符（运行时替换为面板数据目录）。
	InstallMarkFile string `json:"-"`
	// Channels 多源下载渠道：面板安装时先测速选延迟最低的源，下载失败自动切换下一个。
	// 每项为下载地址模板，支持 ${VER}（版本）、${FULL}（完整版本号）、${ARCH}（架构）占位符。
	Channels []string `json:"-"`
	// OpenPorts 安装完成后自动放行的端口（含端口段），如 FTP: ["20","21","39000-40000"]
	OpenPorts []string `json:"-"`
	WebUrl    string   `json:"web_url,omitempty"` // 安装后的访问地址（如 phpMyAdmin）
	Remarks   string   `json:"remarks"`
	// SelectPhpVersion 安装前是否需要选择 PHP 版本（如 phpMyAdmin）
	SelectPhpVersion bool `json:"select_php_version,omitempty"`
	// SelectVersion 安装前是否可选择版本（通用版本选择，如 MySQL 8.0/5.7）
	SelectVersion bool `json:"select_version,omitempty"`
	// Versions 可选版本列表（供前端下拉选择）
	Versions []string `json:"versions,omitempty"`
	// VersionDefault 默认选中的版本
	VersionDefault string `json:"version_default,omitempty"`
	// SubCategory 二级分类 key（如 lang:php），用于顶级分类下的进一步筛选（运行时环境内的语言）
	SubCategory string `json:"sub_category,omitempty"`
	// SystemDefault 是否发行版自带（DB 无面板安装记录 + 系统里实际探测到已存在）。
	// 这类包被系统其它组件依赖、卸载无意义或会误伤，面板不允许卸载。
	// 由 listAppsUncached 按运行时探测结果动态填充（source=system 时为 true），
	// 前端据此隐藏「卸载」按钮并展示「系统自带」标签。
	SystemDefault bool `json:"system_default,omitempty"`
	// LocalOnly 本地专属应用（面板内置功能，官网商店无此应用）。
	// 远程商店拉取成功时默认以远程列表为准，标记此字段的应用始终显示，
	// 不依赖官网 seed 数据（如 site-migrate 网站搬家）。
	LocalOnly bool `json:"-"`
	// Hidden 不再提供安装入口的应用（如无版本运行时 nodejs/python3/golang）。
	// meta 定义保留——已通过面板安装过的用户仍能显示/管理/卸载（findApp 依赖 meta），
	// 但未安装的不再出现在「可安装」列表。由 listAppsUncached 过滤展示。
	Hidden bool `json:"hidden,omitempty"`
	// InstalledActions 应用安装后在卡片上显示的自定义操作按钮。
	// 通用机制：由后端配置，前端根据 action 类型执行对应交互（如弹窗、跳转）。
	InstalledActions []AppAction `json:"installed_actions,omitempty"`
}

// AppAction 应用自定义按钮。
type AppAction struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Type   string `json:"type"`   // primary / success / warning / danger / default / info
	Action string `json:"action"` // 前端识别的事件标识，如 open-migrate
}

// 分类常量
const (
	CatServer   = "server"   // 服务器软件
	CatDatabase = "database" // 数据库
	CatCache    = "cache"    // 缓存
	CatRuntime  = "runtime"  // 运行环境
	CatTool     = "tool"     // 系统工具
)

// 系统自带应用不再用硬编码 key 列表判定，改为「运行时动态探测」：
// DB 无面板安装记录 + 系统里实际探测到二进制/服务已存在 → 视为系统自带（source=system，不可卸载）。
// 这样重装系统后只有真正预装的组件（如 python3）会被标记为系统自带，
// 面板后续安装的应用（DB 有 installed 记录）都可正常卸载。
// 判定逻辑见 appstore_tasks.go 的 listAppsUncached / UninstallApp 与 appstore_probe.go 的 EnvStatus。

// 二级分类（运行时环境下的语言维度）
const (
	SubLangPHP    = "lang:php"
	SubLangNodejs = "lang:nodejs"
	SubLangPython = "lang:python"
	SubLangGolang = "lang:golang"
	SubLangJava   = "lang:java"
)

// AppSubCategory 应用二级分类
type AppSubCategory struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Icon  string `json:"icon"`
}

// AppCategory 应用分类（含展示信息）
type AppCategory struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Icon  string `json:"icon"`
	Desc  string `json:"desc"`
	// SubCategories 二级分类列表（仅顶级分类运行时环境使用）
	SubCategories []AppSubCategory `json:"sub_categories,omitempty"`
}

// 分类展示顺序与元信息
var appCategoryMeta = map[string]AppCategory{
	CatServer:   {Key: CatServer, Label: "服务器软件", Icon: "Cpu", Desc: "Nginx、Apache 等 Web 服务器"},
	CatDatabase: {Key: CatDatabase, Label: "数据库", Icon: "Coin", Desc: "MySQL、SQLServer、MongoDB、Redis、PostgreSQL、SQLite"},
	CatCache:    {Key: CatCache, Label: "缓存", Icon: "DataLine", Desc: "Redis、Memcached 等内存缓存"},
	CatRuntime:  {Key: CatRuntime, Label: "运行环境", Icon: "Files", SubCategories: runtimeSubCategories},
	CatTool:     {Key: CatTool, Label: "系统工具", Icon: "Box", Desc: "Docker 等系统级工具"},
}

// runtimeSubCategories 运行时环境下的语言二级分类
var runtimeSubCategories = []AppSubCategory{
	{Key: SubLangPHP, Label: "PHP", Icon: "Files"},
	{Key: SubLangNodejs, Label: "Node.js", Icon: "VideoPlay"},
	{Key: SubLangPython, Label: "Python", Icon: "TrendCharts"},
	{Key: SubLangGolang, Label: "Go", Icon: "Cpu"},
	{Key: SubLangJava, Label: "Java", Icon: "Coffee"},
}

// 应用商店内置应用
var appMetas = []AppMeta{
	{
		Key: "nginx", Name: "Nginx", Category: CatServer, Icon: "Cpu",
		Description: "高性能 Web 服务器，网站管理的核心依赖",
		Service:     "nginx", VersionCmd: "nginx -v",
		AptPackages: []string{"nginx"}, YumPackages: []string{"nginx"},
		OpenPorts: []string{"80", "443"},
		Remarks:   "安装后可在「网站管理」中创建站点",
	},
	{
		Key: "apache", Name: "Apache", Category: CatServer, Icon: "Guide",
		Description: "老牌开源 Web 服务器（httpd）",
		Service:     "apache2", VersionCmd: "apache2 -v || httpd -v",
		AptPackages: []string{"apache2"}, YumPackages: []string{"httpd"},
		OpenPorts: []string{"80", "443"},
		Remarks:   "Debian 系服务名为 apache2，RHEL 系为 httpd",
	},
	{
		Key: "mysql", Name: "MySQL", Category: CatDatabase, Icon: "Coin",
		Description: "MySQL 关系型数据库（推荐 8.0，可选 5.7）",
		Service:     "mysql", VersionCmd: "mysqld --version || mysql --version",
		AptPackages: []string{"mysql-server"}, YumPackages: []string{"mysql-server"},
		AptFallbackPackages: []string{"mariadb-server"},
		AptFallbackService:  "mariadb",
		SelectVersion:       true,
		Versions:            []string{"8.0", "5.7"},
		VersionDefault:      "8.0",
		Remarks:             "首次安装约需几分钟，可在「数据库」中建库建用户；默认不对外放行端口，需在「系统-防火墙」手动放行",
	},
	{
		Key: "mariadb", Name: "MariaDB", Category: CatDatabase, Icon: "Coin",
		Description: "MySQL 的社区分支，兼容 MySQL 协议",
		Service:     "mariadb", VersionCmd: "mariadbd --version || mysql --version",
		AptPackages: []string{"mariadb-server"}, YumPackages: []string{"mariadb-server"},
		SelectVersion:  true,
		Versions:       []string{"11.4", "10.6"},
		VersionDefault: "11.4",
		Remarks:        "与 MySQL 二选一安装即可；默认不对外放行端口，需在「系统-防火墙」手动放行",
	},
	{
		Key: "postgresql", Name: "PostgreSQL", Category: CatDatabase, Icon: "Coin",
		Description: "功能强大的开源关系型数据库",
		Service:     "postgresql", VersionCmd: "psql --version",
		AptPackages: []string{"postgresql"}, YumPackages: []string{"postgresql-server"},
		// Debian 上 apt install postgresql 实际装的是 postgresql-17 等子包（postgresql 是元包），
		// 卸载只 remove postgresql 会报"包未安装"导致假成功。按 postgresql* 前缀通配清理实际子包。
		AptRemovePatterns: []string{"postgresql*"},
		SelectVersion:     true,
		Versions:       []string{"16", "15", "14"},
		VersionDefault: "16",
		Remarks:        "安装后默认监听 5432，服务由 systemd 管理；默认不对外放行端口，需在「系统-防火墙」手动放行",
	},
	{
		Key: "redis", Name: "Redis", Category: CatDatabase, Icon: "DataLine",
		Description: "高性能内存缓存 / KV 存储，可作为 NoSQL 数据库使用",
		Service:     "redis", VersionCmd: "redis-server --version",
		AptPackages: []string{"redis-server"}, YumPackages: []string{"redis"},
		SelectVersion:  true,
		Versions:       []string{"7.2", "7.0"},
		VersionDefault: "7.2",
		Remarks:        "默认监听 127.0.0.1:6379，安装后可在「数据库」Redis 标签管理；默认不对外放行端口，需在「系统-防火墙」手动放行",
	},
	{
		Key: "mongodb", Name: "MongoDB", Category: CatDatabase, Icon: "DataAnalysis",
		Description: "面向文档的 NoSQL 数据库（社区版）",
		Service:     "mongod", VersionCmd: "mongod --version || mongosh --version",
		InstallScript:   mongodbInstallScript,
		UninstallScript: mongodbUninstallScript,
		Channels:        mongodbChannels,
		SelectVersion:   true,
		Versions:        []string{"7.0", "6.0"},
		VersionDefault:  "7.0",
		Remarks:         "使用官方仓库安装 mongodb-org；安装后可在「数据库」MongoDB 标签管理；默认不对外放行端口，需在「系统-防火墙」手动放行",
	},
	{
		Key: "sqlserver", Name: "SQLServer", Category: CatDatabase, Icon: "Coin",
		Description: "Microsoft SQL Server for Linux（含命令行工具 sqlcmd）",
		// sqlservr 不支持 --version（会把该参数当启动选项直接拉起引擎，导致探测卡死/超时），
		// 改为从包管理器读取引擎版本：dpkg-query 或 rpm 二选一。
		Service: "mssql-server", VersionCmd: "test -x /opt/mssql/bin/sqlservr && (dpkg-query -f '${Version}' -W mssql-server 2>/dev/null || rpm -q mssql-server --qf '%{VERSION}' 2>/dev/null)",
		InstallScript:   sqlserverInstallScript,
		UninstallScript: sqlserverUninstallScript,
		SelectVersion:   true,
		Versions:        []string{"2022", "2019"},
		VersionDefault:  "2022",
		Remarks:         "安装过程中需要设置 SA 密码；安装后请在「数据库」SQLServer 标签使用，并记录 SA 密码到设置；默认不对外放行端口，需在「系统-防火墙」手动放行",
	},
	{
		Key: "sqlite", Name: "SQLite", Category: CatDatabase, Icon: "Document",
		Description: "轻量级本地文件型关系数据库",
		Service:     "", VersionCmd: "sqlite3 --version",
		AptPackages: []string{"sqlite3"}, YumPackages: []string{"sqlite"},
		Remarks: "无需常驻服务，数据库文件保存在面板数据目录 sqlite 文件夹下",
	},
	{
		Key: "memcached", Name: "Memcached", Category: CatCache, Icon: "DataLine",
		Description: "轻量分布式内存对象缓存系统",
		// 去掉 `| head -n1` 管道（会吞掉 memcached 的退出码），改用 test -x 前置检查
		Service: "memcached", VersionCmd: "test -x /usr/bin/memcached && memcached -h 2>&1",
		AptPackages: []string{"memcached"}, YumPackages: []string{"memcached"},
		Remarks: "默认监听 127.0.0.1:11211；默认不对外放行端口，需在「系统-防火墙」手动放行",
	},
	{
		Key: "docker", Name: "Docker", Category: CatTool, Icon: "Box",
		Description: "容器引擎与容器编排工具",
		Service:     "docker", VersionCmd: "docker version --format '{{.Server.Version}}'",
		InstallScript: `#!/bin/bash
set -e
if command -v docker >/dev/null 2>&1; then
  echo "Docker 已安装"
  exit 0
fi
if ! command -v docker >/dev/null 2>&1; then
  echo "下载并执行 Docker 官方安装脚本（阿里云镜像） ..."
  # get.docker.com 支持 --mirror Aliyun，从 mirrors.aliyun.com/docker-ce 拉取，国内速度快
  curl -fsSL --retry 5 --retry-delay 3 --connect-timeout 20 https://get.docker.com | sh -s -- --mirror Aliyun || true
fi
if ! command -v docker >/dev/null 2>&1; then
  echo "Docker 官方脚本（阿里云镜像）安装失败，回退官方源重试"
  curl -fsSL --retry 5 --retry-delay 3 --connect-timeout 20 https://get.docker.com | sh || true
fi
if ! command -v docker >/dev/null 2>&1; then
  echo "Docker 官方脚本下载失败，回退发行版软件源安装 docker.io"
  if command -v apt-get >/dev/null 2>&1; then
    apt-get update -y
    DEBIAN_FRONTEND=noninteractive apt-get install -y docker.io
  elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
    PM=dnf; command -v dnf >/dev/null 2>&1 || PM=yum
    $PM install -y docker
  fi
fi
command -v docker >/dev/null 2>&1 || { echo "Docker 安装失败" >&2; exit 1; }
systemctl enable docker || true
systemctl start docker || true
echo "Docker 安装完成"
`,
		UninstallScript: `#!/bin/bash
systemctl stop docker 2>/dev/null || true
systemctl stop containerd 2>/dev/null || true
systemctl disable docker 2>/dev/null || true
if command -v apt-get >/dev/null 2>&1; then
  # Debian/Ubuntu 的 docker.io 是元包，apt remove docker.io 只删元包本身，
  # 不会自动删 docker-cli / docker-buildx / containerd 等依赖子包（残留导致面板误判已安装）。
  # 这里显式列出 Debian 包名，并 purge + autoremove 清理依赖残留。
  apt-get remove -y --purge docker-ce docker-ce-cli docker-cli docker-buildx docker-buildx-plugin docker-compose-plugin containerd.io containerd docker.io 2>/dev/null || true
  apt-get autoremove -y --purge 2>/dev/null || true
elif command -v yum >/dev/null 2>&1; then
  yum remove -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin 2>/dev/null || true
fi
`,
		Channels: dockerChannels,
		Remarks:  "Docker 官方一键安装脚本，安装后请刷新页面",
	},
	{
		// 无版本运行时：不再提供安装入口（只提供 Node/Python/Go 多版本），
		// 定义保留用于「已安装兜底」——历史通过面板装过的用户仍可显示/管理/卸载。
		Key: "nodejs", Name: "Node.js", Category: CatRuntime, SubCategory: SubLangNodejs, Icon: "VideoPlay",
		Description: "Node.js 运行时（含 npm），适合前端项目 / 服务端 JS",
		Service:     "", VersionCmd: "node -v && npm -v",
		AptPackages: []string{"nodejs", "npm"}, YumPackages: []string{"nodejs", "npm"},
		Hidden:        true,
		SelectVersion: true,
		Versions:      []string{"22", "20", "18"},
		Remarks:       "该应用已停止提供安装，请安装带版本号的 Node 多版本",
	},
	{
		Key: "python3", Name: "Python3", Category: CatRuntime, SubCategory: SubLangPython, Icon: "TrendCharts",
		Description: "Python 3 运行时（含 pip3），适合脚本与 Python 站点",
		// 不带 `|| true`：避免 python3 不存在时退出码被吞（恒 0）导致误判「已安装」
		Service: "", VersionCmd: "python3 -V",
		AptPackages:    []string{"python3", "python3-pip", "python3-venv"},
		YumPackages:    []string{"python3", "python3-pip"},
		Hidden:         true,
		SelectVersion:  true,
		Versions:       []string{"3.12", "3.11", "3.10"},
		Remarks:        "该应用已停止提供安装，请安装带版本号的 Python 多版本",
	},
	{
		Key: "golang", Name: "Golang", Category: CatRuntime, SubCategory: SubLangGolang, Icon: "Cpu",
		Description: "Go 语言工具链（go + gofmt），适合编译型后端服务",
		Service:     "", VersionCmd: "go version",
		AptPackages: []string{"golang-go"}, YumPackages: []string{"golang"},
		Hidden:        true,
		SelectVersion: true,
		Versions:      []string{"1.23", "1.22", "1.21"},
		Remarks:       "该应用已停止提供安装，请安装带版本号的 Go 多版本",
	},
	{
		Key: "java", Name: "Java (OpenJDK)", Category: CatRuntime, SubCategory: SubLangJava, Icon: "Coffee",
		Description: "Java 运行时环境（OpenJDK JRE+JDK）",
		// 不带 `| head` 管道：管道会吞掉 java 的退出码（head 恒成功），导致未安装时仍误判「已安装」
		Service: "", VersionCmd: "java -version",
		AptPackages: []string{"default-jre", "default-jdk"},
		YumPackages: []string{"java-17-openjdk", "java-17-openjdk-devel"},
		Remarks:     "适用于 Tomcat / Spring Boot 等 Java 应用",
	},
}

// ===== 远程应用（官网下发） =====
// 面板应用商店 = 本地内置应用 + 官网下发应用（合并展示，官网同名 key 覆盖本地）。
// 官网通过 https://panel.apihot.cn/api/apps 提供应用列表（含完整安装脚本/包名），
// 由官网后台增删改，面板每次请求 /api/apps/list 时拉取并缓存。

// remoteApp 官网 /api/apps 返回的应用结构（字段与官网 App 模型对齐）
type remoteApp struct {
	Key                 string `json:"key"`
	Name                string `json:"name"`
	Category            string `json:"category"`
	SubCategory         string `json:"sub_category"`
	Description         string `json:"description"`
	Icon                string `json:"icon"`
	Service             string `json:"service"`
	VersionCmd          string `json:"version_cmd"`
	AptPackages         string `json:"apt_packages"`
	YumPackages         string `json:"yum_packages"`
	AptFallbackPackages string `json:"apt_fallback_packages"`
	AptFallbackService  string `json:"apt_fallback_service"`
	InstallScript       string `json:"install_script"`
	UninstallScript     string `json:"uninstall_script"`
	OpenPorts           string `json:"open_ports"`
	WebURL              string `json:"web_url"`
	Remarks             string `json:"remarks"`
	SelectPhpVersion    bool   `json:"select_php_version"`
	SelectVersion       bool   `json:"select_version"`
	Versions            string `json:"versions"`
	VersionDefault      string `json:"version_default"`
	SystemDefault       bool   `json:"system_default"`
	Channels            string `json:"channels"` // 多源下载渠道（官网 App.Channels，每行一个 URL 模板）
	Sort                int    `json:"sort"`
}

// toAppMeta 把官网应用转成面板 AppMeta（逗号分隔字符串 → []string）
func (r *remoteApp) toAppMeta() AppMeta {
	return AppMeta{
		Key:                 r.Key,
		Name:                r.Name,
		Category:            r.Category,
		SubCategory:         r.SubCategory,
		Description:         r.Description,
		Icon:                r.Icon,
		Service:             r.Service,
		VersionCmd:          r.VersionCmd,
		AptPackages:         splitAppCSV(r.AptPackages),
		YumPackages:         splitAppCSV(r.YumPackages),
		AptFallbackPackages: splitAppCSV(r.AptFallbackPackages),
		AptFallbackService:  r.AptFallbackService,
		InstallScript:       r.InstallScript,
		UninstallScript:     r.UninstallScript,
		OpenPorts:           splitAppCSV(r.OpenPorts),
		WebUrl:              r.WebURL,
		Remarks:             r.Remarks,
		SelectPhpVersion:    r.SelectPhpVersion,
		SelectVersion:       r.SelectVersion,
		Versions:            splitAppCSV(r.Versions),
		VersionDefault:      r.VersionDefault,
		SystemDefault:       r.SystemDefault,
		Channels:            splitAppLines(r.Channels),
	}
}

// splitAppLines 把多行渠道文本切成非空行
func splitAppLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// splitAppCSV 把逗号分隔字符串切成非空切片
func splitAppCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

var (
	remoteAppsMu    sync.Mutex
	remoteAppsCache []AppMeta // 官网应用列表主缓存：成功拉取一次后永久缓存，不再请求官网
)

// fetchRemoteApps 返回官网应用列表。
// 面板缓存策略：成功拉取一次后永久缓存（进程内存），后续请求直接返回缓存、不请求官网；
// 需要刷新时由前端进入应用商店页调用 RefreshRemoteStore 强制重新拉取。
// 返回 (metas, ok)：ok=false 表示无可用远程列表（从未拉取成功且本次拉取失败），
// 此时调用方回退到本地内置应用兜底，不影响面板主流程。
func fetchRemoteApps() ([]AppMeta, bool) {
	remoteAppsMu.Lock()
	if remoteAppsCache != nil {
		cache := remoteAppsCache
		remoteAppsMu.Unlock()
		return cache, true
	}
	remoteAppsMu.Unlock()
	return fetchRemoteAppsNow()
}

// fetchRemoteAppsNow 实际请求官网 /api/apps（无缓存保护），成功后写入内存缓存。
func fetchRemoteAppsNow() ([]AppMeta, bool) {
	base := strings.TrimRight(config.Get().Store.BaseURL, "/")
	if base == "" {
		return nil, false
	}

	client := &http.Client{Timeout: 10 * time.Second}
	url := base + "/api/apps"
	resp, err := client.Get(url)
	if err != nil {
		slog.Warn("拉取远程应用列表失败", "err", err)
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		slog.Warn("远程应用接口返回异常状态", "status", resp.StatusCode)
		return nil, false
	}
	var body struct {
		Code int         `json:"code"`
		Data []remoteApp `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		slog.Warn("解析远程应用列表失败", "err", err)
		return nil, false
	}
	if body.Code != 0 {
		slog.Warn("远程应用接口返回错误", "code", body.Code)
		return nil, false
	}

	metas := make([]AppMeta, 0, len(body.Data))
	for _, a := range body.Data {
		if a.Key == "" || a.Name == "" || a.Category == "" {
			continue
		}
		metas = append(metas, a.toAppMeta())
	}
	// 拉取成功：替换主缓存（此后 fetchRemoteApps 直接走缓存，不再请求官网）
	remoteAppsMu.Lock()
	remoteAppsCache = metas
	remoteAppsMu.Unlock()
	return metas, true
}

// ===== 远程分类 / 分组（官网下发，驱动面板 /apps 页面的 Tab 渲染） =====

// remoteCategory 官网 /api/app-categories 返回的分类项
type remoteCategory struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Icon  string `json:"icon"`
	Color string `json:"color"`
	Desc  string `json:"desc"`
	Sort  int    `json:"sort"`
}

// remoteGroup 官网 /api/app-categories 返回的分组项
type remoteGroup struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Icon        string `json:"icon"`
	Color       string `json:"color"`
	CategoryKey string `json:"category_key"`
	Sort        int    `json:"sort"`
}

var (
	remoteCatsMu    sync.Mutex
	remoteCatsCache []AppCategory // 官网分类主缓存：成功拉取一次后永久缓存，不再请求官网
)

// fetchRemoteCategories 返回官网分类 + 分组。
// 面板缓存策略与应用列表一致：成功拉取一次后永久缓存（进程内存），后续请求直接返回缓存；
// 需要刷新时由前端进入应用商店页调用 RefreshRemoteStore 强制重新拉取。
// 无缓存且拉取失败时返回空（AppCategories 会回退到本地硬编码分类）。
func fetchRemoteCategories() ([]AppCategory, []AppSubCategory) {
	remoteCatsMu.Lock()
	if remoteCatsCache != nil {
		cache := remoteCatsCache
		remoteCatsMu.Unlock()
		return cache, nil
	}
	remoteCatsMu.Unlock()
	return fetchRemoteCategoriesNow()
}

// fetchRemoteCategoriesNow 实际请求官网 /api/app-categories（无缓存保护），成功后写入内存缓存。
func fetchRemoteCategoriesNow() ([]AppCategory, []AppSubCategory) {
	base := strings.TrimRight(config.Get().Store.BaseURL, "/")
	if base == "" {
		return nil, nil
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(base + "/api/app-categories")
	if err != nil {
		slog.Warn("拉取远程分类失败", "err", err)
		return nil, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		slog.Warn("远程分类接口返回异常状态", "status", resp.StatusCode)
		return nil, nil
	}
	var body struct {
		Code int `json:"code"`
		Data struct {
			Categories []remoteCategory `json:"categories"`
			Groups     []remoteGroup    `json:"groups"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		slog.Warn("解析远程分类失败", "err", err)
		return nil, nil
	}
	if body.Code != 0 {
		slog.Warn("远程分类接口返回错误", "code", body.Code)
		return nil, nil
	}

	// 分类 → AppCategory（按分类 key 分组，下面的分组挂在各自的 sub_categories）
	subIdx := map[string][]AppSubCategory{}
	groupList := []remoteGroup{}
	groupColors := map[string]string{}
	for _, g := range body.Data.Groups {
		subIdx[g.CategoryKey] = append(subIdx[g.CategoryKey], AppSubCategory{
			Key: g.Key, Label: g.Name, Icon: g.Icon,
		})
		groupList = append(groupList, g)
		if g.Color != "" {
			groupColors[g.Key] = g.Color
		}
	}
	// 排序：每个分类内的 sub 按所属分组的 Sort DESC（同 Sort 时按 Key 升序，幂等）
	sort.SliceStable(groupList, func(i, j int) bool {
		if groupList[i].Sort != groupList[j].Sort {
			return groupList[i].Sort > groupList[j].Sort
		}
		return groupList[i].Key < groupList[j].Key
	})
	for k := range subIdx {
		sort.SliceStable(subIdx[k], func(i, j int) bool {
			ki, kj := subIdx[k][i].Key, subIdx[k][j].Key
			gi, gj := groupColors[ki], groupColors[kj]
			if gi != gj {
				return gi > gj
			}
			return ki < kj
		})
	}

	cats := make([]AppCategory, 0, len(body.Data.Categories))
	for _, c := range body.Data.Categories {
		if c.Key == "" || c.Name == "" {
			continue
		}
		cats = append(cats, AppCategory{
			Key: c.Key, Label: c.Name, Icon: c.Icon,
			Desc: c.Desc, SubCategories: subIdx[c.Key],
		})
	}
	remoteCatsMu.Lock()
	remoteCatsCache = cats
	remoteCatsMu.Unlock()
	slog.Info("远程分类已更新", "categories", len(cats), "groups", len(body.Data.Groups))
	return cats, nil
}

// RefreshRemoteStore 强制重新拉取官网应用商店数据（应用列表 + 分类/分组），
// 由前端进入应用商店页时调用（对应「面板每次刷新页面时缓存一次」，
// 让官网后台的新增/修改/删除应用立即生效）。
// 任一拉取失败都保留旧缓存不清空（官网抖动不丢面板已有远程数据），并返回错误由调用方提示。
func RefreshRemoteStore() error {
	if _, appsOK := fetchRemoteAppsNow(); appsOK {
		fetchRemoteCategoriesNow()
		slog.Info("远程应用商店缓存已刷新")
		return nil
	}
	return errors.New("拉取官网应用商店列表失败，已保留上次缓存")
}

// allAppMetas 返回「本地 + 远程」合并后的应用列表。
// 远程列表走面板侧永久缓存（成功拉取一次后不再请求官网），
// 由前端进入应用商店页调用 RefreshRemoteStore 强制刷新。
// 远程成功时以远程列表为准（官网下架/删除的应用不再出现）；
// 远程失败时回退到本地内置应用兜底（保证面板 /apps 页面不空白）。
func allAppMetas() []AppMeta {
	remote, remoteOK := fetchRemoteApps()
	if !remoteOK {
		// 远程拉取失败：用本地内置应用兜底
		merged := make([]AppMeta, 0, len(appMetas))
		merged = append(merged, appMetas...)
		return merged
	}

	// 远程拉取成功：以远程列表为准（保留下架/删除在面板侧生效），
	// 但用本地内置应用回填“逻辑字段”——版本探测命令、安装/卸载脚本、开放端口、服务名等。
	// 这些字段在 AppMeta 上标记了 json:"-"（不对外序列化），官网 seed 数据中并不存在，
	// 若不以本地为准，远程返回的 meta 的 VersionCmd/InstallScript 等全部为空，
	// 导致 EnvStatus 版本探测失败（version 恒为空）、FTP 等应用在管理页显示“未安装”。
	localByKey := make(map[string]AppMeta, len(appMetas))
	for _, m := range appMetas {
		localByKey[m.Key] = m
	}
	merged := make([]AppMeta, 0, len(remote))
	// 远程列表按 key 去重：官网后台应用表若出现重复登记（同一应用两条记录），
	// 面板侧不能跟着显示两个一模一样的应用——已安装列表会出现重复项、无法区分。
	// 只保留第一条，其余同 key 项丢弃；seen 同时复用于后续 LocalOnly / DB 记录补回。
	seen := make(map[string]bool, len(remote)+len(appMetas))
	for _, r := range remote {
		if seen[r.Key] {
			continue
		}
		seen[r.Key] = true
		if local, ok := localByKey[r.Key]; ok {
			// 本地专属应用（LocalOnly）官网 seed 中即便存在也应以本地完整配置为准，
			// 否则远程数据会覆盖 InstalledActions、Description、Remarks 等前端展示字段。
			if local.LocalOnly {
				merged = append(merged, local)
				continue
			}
			// 版本探测命令、安装/卸载脚本、开放端口、服务名等是“逻辑字段”，
			// 标记了 json:"-"（不对外序列化），远程 seed 数据里要么为空、要么不可靠
			// （如 ftp 远程 version_cmd 为 "vsftpd -v 2>&1"，而 vsftpd 把 -v 当配置文件读，
			// 输出为空导致 EnvStatus 永远探测不到版本）。这些逻辑字段一律以本地内置为准。
			if local.VersionCmd != "" {
				r.VersionCmd = local.VersionCmd
			}
			if local.InstallScript != "" {
				r.InstallScript = local.InstallScript
			}
			if local.UninstallScript != "" {
				r.UninstallScript = local.UninstallScript
			}
			if len(local.OpenPorts) > 0 {
				r.OpenPorts = local.OpenPorts
			}
			if local.Service != "" {
				r.Service = local.Service
			}
			// apt 卸载逻辑字段同样以本地为准：官网 seed 数据里没有这些字段，
			// 若远程 postgresql 应用缺 AptRemovePatterns，卸载时只会 remove 元包
			// postgresql（实际未安装）导致"假成功/残留"。
			if len(local.AptRemovePatterns) > 0 {
				r.AptRemovePatterns = local.AptRemovePatterns
			}
			if len(local.AptFallbackPackages) > 0 {
				r.AptFallbackPackages = local.AptFallbackPackages
			}
			if local.AptFallbackService != "" {
				r.AptFallbackService = local.AptFallbackService
			}
		}
		merged = append(merged, r)
	}
	// 本地专属应用（LocalOnly）始终显示，不依赖官网商店列表。
	// 官网下架逻辑不受影响（仅 LocalOnly 应用会被补回，普通应用仍以远程为准）。
	for _, m := range appMetas {
		if m.LocalOnly && !seen[m.Key] {
			merged = append(merged, m)
		}
	}
	// 已安装应用兜底：官网下架/删除 meta 后，已通过面板安装（含安装中/卸载中/失败）
	// 的应用不能凭空消失——否则「已安装」列表消失、服务无法启停、应用无法卸载。
	// 用本地内置 meta 补回（这些 meta 定义里含卸载脚本/服务名等逻辑字段），
	// 展示层再按 Hidden + 是否已安装决定是否出现在「可安装」列表。
	if recs, err := model.ListActiveAppRecords(); err == nil {
		for _, rec := range recs {
			if seen[rec.Key] {
				continue
			}
			if local, ok := localByKey[rec.Key]; ok {
				merged = append(merged, local)
				seen[rec.Key] = true
			}
		}
	}
	return merged
}

// ExportAppMetasJSON 导出全部本地内置应用（含生成的安装脚本）为 JSON，
// 用于初始化官网应用商店数据。字段与官网 App 模型对齐（逗号分隔字符串）。
// 供一次性数据迁移使用（官网 seed 应用数据）。
func ExportAppMetasJSON() ([]byte, error) {
	type exportApp struct {
		Key                 string `json:"key"`
		Name                string `json:"name"`
		Category            string `json:"category"`
		SubCategory         string `json:"sub_category"`
		Description         string `json:"description"`
		Icon                string `json:"icon"`
		Service             string `json:"service"`
		VersionCmd          string `json:"version_cmd"`
		AptPackages         string `json:"apt_packages"`
		YumPackages         string `json:"yum_packages"`
		AptFallbackPackages string `json:"apt_fallback_packages"`
		AptFallbackService  string `json:"apt_fallback_service"`
		InstallScript       string `json:"install_script"`
		UninstallScript     string `json:"uninstall_script"`
		OpenPorts           string `json:"open_ports"`
		WebURL              string `json:"web_url"`
		Remarks             string `json:"remarks"`
		SelectPhpVersion    bool   `json:"select_php_version"`
		SelectVersion       bool   `json:"select_version"`
		Versions            string `json:"versions"`
		VersionDefault      string `json:"version_default"`
		SystemDefault       bool   `json:"system_default"`
		Sort                int    `json:"sort"`
	}
	out := make([]exportApp, 0, len(appMetas))
	for i, m := range appMetas {
		out = append(out, exportApp{
			Key:                 m.Key,
			Name:                m.Name,
			Category:            m.Category,
			SubCategory:         m.SubCategory,
			Description:         m.Description,
			Icon:                m.Icon,
			Service:             m.Service,
			VersionCmd:          m.VersionCmd,
			AptPackages:         strings.Join(m.AptPackages, ","),
			YumPackages:         strings.Join(m.YumPackages, ","),
			AptFallbackPackages: strings.Join(m.AptFallbackPackages, ","),
			AptFallbackService:  m.AptFallbackService,
			InstallScript:       m.InstallScript,
			UninstallScript:     m.UninstallScript,
			OpenPorts:           strings.Join(m.OpenPorts, ","),
			WebURL:              m.WebUrl,
			Remarks:             m.Remarks,
			SelectPhpVersion:    m.SelectPhpVersion,
			SelectVersion:       m.SelectVersion,
			Versions:            strings.Join(m.Versions, ","),
			VersionDefault:      m.VersionDefault,
			SystemDefault:       m.SystemDefault,
			Sort:                len(appMetas) - i, // 保持注册顺序：越靠前 sort 越大
		})
	}
	return json.MarshalIndent(out, "", "  ")
}
