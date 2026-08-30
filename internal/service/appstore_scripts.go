package service

import (
	"strings"
)

// phpVersions 可安装的 PHP 多版本（Key 无点号，Remi 为 RPM 包名中的版本号）
// 注：PHP 5.3 / 5.4 / 5.5 官方仓库（sury / remi）早已下架，无法通过包管理器安装，故从 5.6 起提供
var phpVersions = []struct {
	Key  string
	Ver  string // 如 7.4 / 8.2（sury 包名）
	Remi string // 如 74 / 82（remi 包名）
}{
	{Key: "php56", Ver: "5.6", Remi: "56"},
	{Key: "php70", Ver: "7.0", Remi: "70"},
	{Key: "php71", Ver: "7.1", Remi: "71"},
	{Key: "php72", Ver: "7.2", Remi: "72"},
	{Key: "php73", Ver: "7.3", Remi: "73"},
	{Key: "php74", Ver: "7.4", Remi: "74"},
	{Key: "php80", Ver: "8.0", Remi: "80"},
	{Key: "php81", Ver: "8.1", Remi: "81"},
	{Key: "php82", Ver: "8.2", Remi: "82"},
	{Key: "php83", Ver: "8.3", Remi: "83"},
	{Key: "php84", Ver: "8.4", Remi: "84"},
}

// nodeVersions 可安装的 Node.js 多版本（通过 nvm，历史版本均可用）
var nodeVersions = []struct {
	Key string
	Ver string
}{
	{Key: "node24", Ver: "24"},
	{Key: "node22", Ver: "22"},
	{Key: "node20", Ver: "20"},
	{Key: "node18", Ver: "18"},
	{Key: "node16", Ver: "16"},
	{Key: "node14", Ver: "14"},
}

// pythonVersions 可安装的 Python 多版本（通过 pyenv + 预下载源码编译）
// Full 为完整补丁版本号（镜像文件名必须带补丁号，如 Python-3.13.15.tgz），
// 阿里云 python-release 为扁平结构（source/Python-X.Y.Z.tgz），
// 清华/官方为带版本子目录结构（X.Y.Z/Python-X.Y.Z.tgz）。
var pythonVersions = []struct {
	Key   string
	Ver   string
	Full  string // 完整补丁版本号，用于镜像文件名与 pyenv install
	Major string // 主版本号，用于二进制名（如 python3.12）
}{
	{Key: "python313", Ver: "3.13", Full: "3.13.15", Major: "3.13"},
	{Key: "python312", Ver: "3.12", Full: "3.12.14", Major: "3.12"},
	{Key: "python311", Ver: "3.11", Full: "3.11.16", Major: "3.11"},
	{Key: "python310", Ver: "3.10", Full: "3.10.21", Major: "3.10"},
	{Key: "python39", Ver: "3.9", Full: "3.9.25", Major: "3.9"},
	{Key: "python38", Ver: "3.8", Full: "3.8.20", Major: "3.8"},
}

// goVersions 可安装的 Go 多版本（官方 tarball，历史版本均可用）
// Ver 为主版本号（用于安装目录 /usr/local/go<ver> 与软链 go<verNoDot>），
// Full 为官方 tarball 的完整补丁版本号——go.dev/dl 的文件名必须带补丁号，
// 如 go1.25.14.linux-amd64.tar.gz；不带补丁号的 go1.25.linux-amd64.tar.gz 会 404。
var goVersions = []struct {
	Key  string
	Ver  string
	Full string
}{
	{Key: "go125", Ver: "1.25", Full: "1.25.14"},
	{Key: "go124", Ver: "1.24", Full: "1.24.13"},
	{Key: "go123", Ver: "1.23", Full: "1.23.12"},
	{Key: "go122", Ver: "1.22", Full: "1.22.12"},
	{Key: "go121", Ver: "1.21", Full: "1.21.13"},
	{Key: "go120", Ver: "1.20", Full: "1.20.14"},
	{Key: "go119", Ver: "1.19", Full: "1.19.13"},
}

// 常用 PHP 扩展（apt 包名后缀 / remi 包名后缀）
var phpExtApt = []string{"mysql", "curl", "gd", "mbstring", "xml", "zip", "bcmath", "redis"}
var phpExtRemi = []string{"mysqlnd", "curl", "gd", "mbstring", "xml", "zip", "bcmath", "redis"}

// phpMyAdmin 安装位置与本地运行入口（Unix Socket，不对外开放端口）
const (
	pmaDir    = "/www/server/phpmyadmin"
	pmaSocket = "/run/lp_pma.sock"
)

func init() {
	// PHP 多版本（php56 ~ php84，支持 apt(sury) / dnf|yum(remi) 一键安装）
	for _, pv := range phpVersions {
		v, r := pv.Ver, pv.Remi
		appMetas = append(appMetas, AppMeta{
			Key: pv.Key, Name: "PHP " + v, Category: CatRuntime, SubCategory: SubLangPHP, Icon: "Files",
			Description:     "PHP " + v + " 运行时与 FPM（可与其他 PHP 版本共存），网站可分别指定版本",
			Service:         "php-fpm",
			VersionCmd:      `php` + v + ` -v 2>/dev/null || php` + r + ` -v 2>/dev/null || /opt/remi/php` + r + `/root/usr/bin/php -v 2>/dev/null || true`,
			InstallScript:   phpInstallScript(v, r),
			UninstallScript: phpUninstallScript(v, r),
			Remarks:         "安装后可在「网站管理」中选择该版本；支持与其它 PHP 版本共存",
		})
	}
	// Node.js 多版本（通过 nvm 安装，各版本独立）
	for _, nv := range nodeVersions {
		v := nv.Ver
		appMetas = append(appMetas, AppMeta{
			Key: nv.Key, Name: "Node " + v, Category: CatRuntime, SubCategory: SubLangNodejs, Icon: "VideoPlay",
			Description:     "Node.js " + v + " 运行时（含 npm），通过 nvm 安装，可与其他 Node 版本共存",
			Service:         "",
			VersionCmd:      `[ -s "$HOME/.nvm/nvm.sh" ] && . "$HOME/.nvm/nvm.sh" && node -v 2>/dev/null || /usr/local/node` + v + `/bin/node -v 2>/dev/null || true`,
			InstallScript:   nodeInstallScript(v),
			UninstallScript: nodeUninstallScript(v),
			Remarks:         "安装后可在「网站管理」中选择该版本；支持与其它 Node 版本共存",
		})
	}
	// Python 多版本（通过 pyenv 安装，各版本独立）
	for _, pv := range pythonVersions {
		v := pv.Ver
		appMetas = append(appMetas, AppMeta{
			Key: pv.Key, Name: "Python " + v, Category: CatRuntime, SubCategory: SubLangPython, Icon: "TrendCharts",
			Description:     "Python " + v + " 运行时（含 pip），通过 pyenv 安装，可与其他 Python 版本共存",
			Service:         "",
			VersionCmd:      `[ -s "$HOME/.pyenv/bin/pyenv" ] && export PATH="$HOME/.pyenv/bin:$PATH" && eval "$(pyenv init -)" && pyenv versions --bare 2>/dev/null | grep -w "` + pv.Full + `" || /usr/local/python` + v + `/bin/python` + pv.Major + ` -V 2>/dev/null || true`,
			InstallScript:   pythonInstallScript(v, pv.Full),
			UninstallScript: pythonUninstallScript(pv.Full),
			Remarks:         "安装后可在「网站管理」中选择该版本；支持与其它 Python 版本共存",
		})
	}
	// Go 多版本（通过官方 tarball 安装，各版本独立）
	for _, gv := range goVersions {
		v := gv.Ver
		appMetas = append(appMetas, AppMeta{
			Key: gv.Key, Name: "Go " + v, Category: CatRuntime, SubCategory: SubLangGolang, Icon: "Cpu",
			Description:     "Go " + v + " 语言工具链（go + gofmt），通过官方包安装，可与其他 Go 版本共存",
			Service:         "",
			VersionCmd:      `/usr/local/go` + v + `/bin/go version 2>/dev/null || true`,
			InstallScript:   goInstallScript(v, gv.Full),
			UninstallScript: goUninstallScript(v),
			Remarks:         "安装后可在「网站管理」中选择该版本；支持与其它 Go 版本共存",
		})
	}
	// FTP 服务器（vsftpd），配套「FTP 管理」账号
	appMetas = append(appMetas, AppMeta{
		Key: "ftp", Name: "FTP (vsftpd)", Category: CatServer, Icon: "Upload",
		Description: "vsftpd 文件传输服务器，配合 FTP 账号管理上传/下载",
		// test -x 前置检查，去掉 `|| true` 避免吞退出码导致误判「已安装」。
		// 版本探测不能用 `vsftpd -v`：Debian/Ubuntu 的 vsftpd -v 输出为空（exit 0），
		// probeVersion 拿到空串会误判「安装后未能检测到版本」导致应用商店显示安装失败。
		// 改为包管理器查询（apt 系 dpkg-query / yum 系 rpm），均失败再回退 vsftpd -v。
		Service: "vsftpd",
		// 注意：ExecCommand 使用 /bin/sh -c 执行（Debian/Ubuntu 上为 dash）。
		// 1) 必须使用 POSIX sh 兼容语法，不能用 bash 的 { ...; } 组命令，改用 ( ... ) 子 shell；
		// 2) dpkg 字段标记必须用双引号包裹 "\${Version}"（而非单引号）：单引号内 \$ 不会被 /bin/sh
		//    当变量展开，会原样传给 dpkg 变成字面 "\${Version}"，dpkg 输出会变成 "$3.0.5-0.2" 前缀；
		//    双引号内 \$ 被 sh 转义为字面 $，dpkg 收到的就是正确格式 ${Version}，输出干净版本号。
		// 3) rpm 字段 %{VERSION} 不含 $，单引号即可（rpm 自行解析 %{}）。
		VersionCmd:      `test -x /usr/sbin/vsftpd && (dpkg-query -W -f="\${Version}" vsftpd 2>/dev/null || rpm -q --qf '%{VERSION}' vsftpd 2>/dev/null || vsftpd -v 2>&1)`,
		InstallScript:   vsftpdInstallScript,
		UninstallScript: vsftpdUninstallScript,
		// FTP 需要放行 20/21 控制+数据端口，以及 39000-40000 被动模式端口段
		OpenPorts: []string{"20", "21", "39000-40000"},
		Remarks:   "安装后可在「FTP 管理」中创建账号并绑定目录（20/21 与被动端口 39000-40000 已自动放行）",
	})
	// phpMyAdmin（网页版数据库管理，需先安装 MySQL/MariaDB 与 PHP 版本）
	appMetas = append(appMetas, AppMeta{
		Key: "phpmyadmin", Name: "phpMyAdmin", Category: CatDatabase, Icon: "Coin",
		Description: "网页版 MySQL/MariaDB 数据库管理工具（中文界面），通过面板入口免密登录",
		Service:     "",
		// 不能用就隐藏：目录不存在时 grep 直接失败（不再 || true），probeVersion 判「未安装」
		VersionCmd:       `test -f ` + pmaDir + `/libraries/classes/Version.php && grep -oP "VERSION\s*=\s*'[^']+'" ` + pmaDir + `/libraries/classes/Version.php 2>/dev/null | head -n1`,
		InstallScript:    pmaInstallScript,
		UninstallScript:  pmaUninstallScript,
		SelectPhpVersion: true,
		Remarks:          "需先安装 MySQL/MariaDB 与 PHP 版本；安装后从「数据库」- MySQL 的「管理」进入，无需密码",
	})
	// 网站搬家（迁移工具：kypanel ↔ kypanel、kypanel → 对端面板）。
	// 安装本身只需系统自带的 tar/gzip/curl（一般已存在），真正的功能入口是
	// 安装完成后应用详情里的「网站迁移」按钮，由 InstalledActions 通用机制渲染。
	appMetas = append(appMetas, AppMeta{
		Key: "site-migrate", Name: "网站搬家", Category: CatTool, Icon: "Switch", LocalOnly: true,
		Description: "把网站/数据库/FTP 从一台面板迁移到另一台，支持 kypanel 互通与迁出到对端面板",
		Service:     "",
		// 用 tar 版本作为探测依据（迁移依赖 tar/gzip，两者系统自带）
		VersionCmd: `tar --version 2>/dev/null | head -n1`,
		InstallScript: `#!/bin/bash
set -e
echo "检查打包依赖工具 ..."
for t in tar gzip curl; do
  if ! command -v $t >/dev/null 2>&1; then
    echo "安装缺失工具 $t ..."
    if command -v apt-get >/dev/null 2>&1; then
      DEBIAN_FRONTEND=noninteractive apt-get install -y $t >/dev/null 2>&1 || true
    elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
      PM=dnf; command -v dnf >/dev/null 2>&1 || PM=yum
      $PM install -y $t >/dev/null 2>&1 || true
    fi
  fi
done
echo "依赖检查完成，网站搬家工具已就绪"`,
		UninstallScript: `#!/bin/bash
echo "网站搬家工具已卸载（历史迁移包保留在面板数据目录，需要可手动清理）"`,
		Remarks: "安装后在应用详情中点击「网站迁移」进行迁出/迁入；被迁入的源面板需创建 API 令牌并开通「网站」权限",
		InstalledActions: []AppAction{
			{Key: "migrate", Label: "网站迁移", Type: "primary", Action: "open-migrate"},
		},
	})
}

// ============================================================================
// 【2】安装脚本生成
// ============================================================================

// phpInstallScript 生成指定 PHP 版本的一键安装脚本
// apt 系使用 sury 仓库（php8.2-fpm 等，socket 自动互不冲突）；RHEL 系使用 remi 仓库（php82-php-fpm）
func phpInstallScript(ver, remi string) string {
	aptExts := ""
	for _, e := range phpExtApt {
		aptExts += " php" + ver + "-" + e
	}
	remiExts := ""
	for _, e := range phpExtRemi {
		remiExts += " php" + remi + "-php-" + e
	}
	tpl := `#!/bin/bash
set -e
if command -v apt-get >/dev/null 2>&1; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -y
    apt-get install -y curl lsb-release ca-certificates gnupg2 >/dev/null 2>&1 || apt-get install -y curl lsb-release ca-certificates >/dev/null 2>&1
    code=$(lsb_release -sc 2>/dev/null)
    if [ -n "$code" ] && [ -d /etc/apt/sources.list.d ]; then
        curl -fsSL https://packages.sury.org/php/apt.gpg -o /usr/share/keyrings/sury-php.gpg || true
        if [ -s /usr/share/keyrings/sury-php.gpg ]; then
            echo "deb [signed-by=/usr/share/keyrings/sury-php.gpg] https://packages.sury.org/php/ $code main" > /etc/apt/sources.list.d/php-sury.list
            apt-get update -y
        fi
    fi
    apt-get install -y php{{VER}}-fpm php{{VER}}-cli{{APT_EXT}}
    systemctl enable php{{VER}}-fpm 2>/dev/null || true
    systemctl restart php{{VER}}-fpm 2>/dev/null || systemctl start php{{VER}}-fpm 2>/dev/null || true
elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
    PM=dnf
    command -v dnf >/dev/null 2>&1 || PM=yum
    rpm -q remi-release >/dev/null 2>&1 || $PM install -y https://rpms.remirepo.net/enterprise/remi-release-$(rpm -E %rhel).rpm
    $PM module reset php -y || true
    $PM install -y php{{REMI}}-php-fpm php{{REMI}}-php-cli{{REMI_EXT}}
    systemctl enable php{{REMI}}-php-fpm 2>/dev/null || true
    systemctl restart php{{REMI}}-php-fpm 2>/dev/null || systemctl start php{{REMI}}-php-fpm 2>/dev/null || true
else
    echo "不支持的系统包管理器，请手动安装 PHP {{VER}}" >&2
    exit 1
fi
echo "PHP {{VER}} 安装完成"`
	tpl = strings.ReplaceAll(tpl, "{{VER}}", ver)
	tpl = strings.ReplaceAll(tpl, "{{REMI}}", remi)
	tpl = strings.ReplaceAll(tpl, "{{APT_EXT}}", aptExts)
	return strings.ReplaceAll(tpl, "{{REMI_EXT}}", remiExts)
}

// phpUninstallScript 生成指定 PHP 版本的卸载脚本
func phpUninstallScript(ver, remi string) string {
	aptExts := ""
	for _, e := range phpExtApt {
		aptExts += " php" + ver + "-" + e
	}
	remiExts := ""
	for _, e := range phpExtRemi {
		remiExts += " php" + remi + "-php-" + e
	}
	tpl := `#!/bin/bash
set -e
if command -v apt-get >/dev/null 2>&1; then
    DEBIAN_FRONTEND=noninteractive apt-get remove -y --purge php{{VER}}-fpm php{{VER}}-cli{{APT_EXT}}
    apt-get autoremove -y
    systemctl stop php{{VER}}-fpm 2>/dev/null || true
    systemctl disable php{{VER}}-fpm 2>/dev/null || true
    rm -f /etc/apt/sources.list.d/php-sury.list
    rm -f /usr/share/keyrings/sury-php.gpg
elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
    PM=dnf
    command -v dnf >/dev/null 2>&1 || PM=yum
    $PM remove -y php{{REMI}}-php-fpm php{{REMI}}-php-cli{{REMI_EXT}} || true
    systemctl stop php{{REMI}}-php-fpm 2>/dev/null || true
    systemctl disable php{{REMI}}-php-fpm 2>/dev/null || true
else
    echo "不支持的系统包管理器" >&2
    exit 1
fi
echo "PHP {{VER}} 已卸载"`
	tpl = strings.ReplaceAll(tpl, "{{VER}}", ver)
	tpl = strings.ReplaceAll(tpl, "{{REMI}}", remi)
	tpl = strings.ReplaceAll(tpl, "{{APT_EXT}}", aptExts)
	return strings.ReplaceAll(tpl, "{{REMI_EXT}}", remiExts)
}

// nodeInstallScript 生成指定 Node.js 版本的一键安装脚本（通过 nvm）
func nodeInstallScript(ver string) string {
	return `#!/bin/bash
set -e
export NVM_DIR="$HOME/.nvm"
if [ ! -s "$NVM_DIR/nvm.sh" ]; then
    curl -fsSL https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.0/install.sh | bash \
        || curl -fsSL https://gitee.com/mirrors/nvm/raw/v0.40.0/install.sh | bash
fi
[ -s "$NVM_DIR/nvm.sh" ] && . "$NVM_DIR/nvm.sh"
# 优先阿里云镜像下载 Node 二进制，失败回退官方源，再切 npmmirror。
# nvm 通过 NVM_NODEJS_ORG_MIRROR 决定下载源，目录结构与官方一致可直接使用。
export NVM_NODEJS_ORG_MIRROR=https://mirrors.aliyun.com/nodejs-release
nvm install ` + ver + ` \
    || { echo "阿里云镜像下载失败，回退官方源 nodejs.org"; unset NVM_NODEJS_ORG_MIRROR; nvm install ` + ver + `; } \
    || { echo "官方源下载失败，切换 npmmirror 镜像重试"; export NVM_NODEJS_ORG_MIRROR=https://npmmirror.com/mirrors/node; nvm install ` + ver + `; }
nvm alias default ` + ver + `
echo "Node ` + ver + ` 安装完成"
`
}

// nodeUninstallScript 生成指定 Node.js 版本的卸载脚本
func nodeUninstallScript(ver string) string {
	return `#!/bin/bash
set -e
export NVM_DIR="$HOME/.nvm"
[ -s "$NVM_DIR/nvm.sh" ] && . "$NVM_DIR/nvm.sh"
nvm uninstall ` + ver + ` 2>/dev/null || true
echo "Node ` + ver + ` 已卸载"
`
}

// pythonInstallScript 生成指定 Python 版本的一键安装脚本（通过 pyenv + 预下载源码编译）
// ver 为主版本号（如 3.13），full 为完整补丁版本号（如 3.13.15）。
// 源码预下载到 pyenv 缓存目录（$PYENV_ROOT/cache），pyenv 会优先复用缓存，
// 从而避开 PYTHON_BUILD_MIRROR_URL 只支持"带版本子目录"结构、无法用阿里云扁平结构的问题。
func pythonInstallScript(ver, full string) string {
	return `#!/bin/bash
set -e
export PYENV_ROOT="$HOME/.pyenv"
export PATH="$PYENV_ROOT/bin:$PATH"

# 1. 安装编译依赖（pyenv 编译 Python 需要）
if command -v apt-get >/dev/null 2>&1; then
    sudo -n apt-get update -y >/dev/null 2>&1 || true
    sudo -n apt-get install -y --no-install-recommends \
        git ca-certificates curl build-essential \
        libssl-dev zlib1g-dev libbz2-dev libreadline-dev \
        libsqlite3-dev libncursesw5-dev xz-utils tk-dev \
        libxml2-dev libxmlsec1-dev libffi-dev liblzma-dev >/dev/null 2>&1 || true
elif command -v yum >/dev/null 2>&1; then
    sudo -n yum install -y git ca-certificates gcc make \
        zlib-devel bzip2 bzip2-devel readline-devel sqlite sqlite-devel \
        openssl-devel xz xz-devel libffi-devel findutils >/dev/null 2>&1 || true
fi

# 2. 拉取 pyenv（优先清华源，失败回退 gitee）
if [ ! -d "$PYENV_ROOT" ]; then
    git clone --depth=1 https://mirrors.tuna.tsinghua.edu.cn/github/pyenv/pyenv.git "$PYENV_ROOT" 2>/dev/null \
        || git clone --depth=1 https://gitee.com/mirrors/pyenv.git "$PYENV_ROOT" 2>/dev/null \
        || git clone --depth=1 https://github.com/pyenv/pyenv.git "$PYENV_ROOT"
    git -C "$PYENV_ROOT" submodule update --init --depth=1 || true
fi

# 3. 预下载 Python 源码到 pyenv 缓存（优先阿里云扁平结构，回退清华/官方带版本子目录）
CACHE="$PYENV_ROOT/cache/Python-` + full + `.tgz"
mkdir -p "$PYENV_ROOT/cache"
if [ ! -f "$CACHE" ]; then
    curl -fsSL --retry 3 --connect-timeout 20 -o "$CACHE" \
        "https://mirrors.aliyun.com/python-release/source/Python-` + full + `.tgz" \
        || { echo "阿里云镜像下载失败，切换清华源"; curl -fsSL --retry 3 --connect-timeout 20 -o "$CACHE" \
            "https://mirrors.tuna.tsinghua.edu.cn/python/` + full + `/Python-` + full + `.tgz"; } \
        || { echo "清华源下载失败，切换官方 python.org"; curl -fsSL --retry 3 --connect-timeout 20 -o "$CACHE" \
            "https://www.python.org/ftp/python/` + full + `/Python-` + full + `.tgz"; }
    [ -s "$CACHE" ] || { echo "所有镜像下载失败，请检查网络"; exit 1; }
fi
export PATH="$PYENV_ROOT/bin:$PATH"
eval "$(pyenv init -)"

# 4. 安装并切换默认版本
pyenv install -v ` + full + `
pyenv global ` + full + `
echo "Python ` + full + ` 安装完成"
`
}

// pythonUninstallScript 生成指定 Python 版本的卸载脚本（full 为完整补丁版本号）
func pythonUninstallScript(full string) string {
	return `#!/bin/bash
set -e
export PYENV_ROOT="$HOME/.pyenv"
export PATH="$PYENV_ROOT/bin:$PATH"
eval "$(pyenv init -)" 2>/dev/null || true
pyenv uninstall -f ` + full + ` 2>/dev/null || true
echo "Python ` + full + ` 已卸载"
`
}

// goInstallScript 生成指定 Go 版本的一键安装脚本（通过官方 tarball）。
// ver 为主版本号（如 1.25，用于安装目录 /usr/local/go1.25 与软链 go125），
// full 为官方 tarball 完整补丁版本号（如 1.25.14，go.dev/dl 文件名必须带补丁号否则 404）。
func goInstallScript(ver, full string) string {
	return `#!/bin/bash
set -e
ARCH=$(uname -m)
if [ "$ARCH" = "x86_64" ]; then GOARCH=amd64
elif [ "$ARCH" = "aarch64" ]; then GOARCH=arm64
else GOARCH=amd64
fi
INSTALL_DIR="/usr/local/go` + ver + `"
rm -rf "$INSTALL_DIR"
mkdir -p "$INSTALL_DIR"
# 国内服务器优先走阿里云镜像，失败回退 golang.google.cn，最后官方源 go.dev。
# --strip-components=1 去掉 tarball 顶层 go/ 目录直接解压到 INSTALL_DIR，
# 避免 mkdir 后 mv /usr/local/go 被嵌套成 /usr/local/go<ver>/go 导致探测不到。
DL_OK=0
for u in \
    "https://mirrors.aliyun.com/golang/go` + full + `.linux-${GOARCH}.tar.gz" \
    "https://golang.google.cn/dl/go` + full + `.linux-${GOARCH}.tar.gz" \
    "https://go.dev/dl/go` + full + `.linux-${GOARCH}.tar.gz"; do
    if curl -fsSL --retry 3 --connect-timeout 20 "$u" | tar -C "$INSTALL_DIR" --strip-components=1 -xzf -; then
        DL_OK=1
        break
    fi
    echo "源下载失败，切换下一镜像: $u"
done
[ "$DL_OK" = "1" ] || { echo "所有镜像下载失败，请检查网络"; exit 1; }
ln -sf "$INSTALL_DIR/bin/go" /usr/local/bin/go` + strings.ReplaceAll(ver, ".", "") + `
ln -sf "$INSTALL_DIR/bin/gofmt" /usr/local/bin/gofmt` + strings.ReplaceAll(ver, ".", "") + `
echo "Go ` + full + ` 安装完成"
`
}

// goUninstallScript 生成指定 Go 版本的卸载脚本
func goUninstallScript(ver string) string {
	return `#!/bin/bash
set -e
INSTALL_DIR="/usr/local/go` + ver + `"
rm -rf "$INSTALL_DIR"
rm -f /usr/local/bin/go` + strings.ReplaceAll(ver, ".", "") + `
rm -f /usr/local/bin/gofmt` + strings.ReplaceAll(ver, ".", "") + `
echo "Go ` + ver + ` 已卸载"
`
}

// pmaInstallScript phpMyAdmin 安装脚本：下载解压到 /www/server/phpmyadmin，
// Nginx 只监听本地 Unix Socket /run/lp_pma.sock，由面板 /pma 反代，不对外开放端口
const pmaInstallScript = `#!/bin/bash
set -e
if ! command -v curl >/dev/null 2>&1; then
    echo "缺少 curl，无法下载 phpMyAdmin" >&2
    exit 1
fi
if ! command -v mysql >/dev/null 2>&1 && ! command -v mariadb >/dev/null 2>&1; then
    echo "未检测到 MySQL/MariaDB，请先在应用商店安装数据库" >&2
    exit 1
fi
# 优先使用面板选择（或默认最高）的 PHP-FPM socket
if [ -n "$SELECTED_PHP_SOCKET" ]; then
    FPM=$SELECTED_PHP_SOCKET
else
    FPM=$(ls /run/php/php*-fpm.sock /run/php-fpm/*.sock /var/run/php/php*-fpm.sock 2>/dev/null | head -n1)
    [ -n "$FPM" ] && FPM="unix:$FPM" || FPM="127.0.0.1:9000"
fi
mkdir -p /www/server
cd /tmp
curl -fsSLo phpmyadmin.tar.gz "https://files.phpmyadmin.net/phpMyAdmin/5.2.2/phpMyAdmin-5.2.2-all-languages.tar.gz"
rm -rf /www/server/phpmyadmin /www/server/phpMyAdmin-5.2.2-all-languages
tar xzf phpmyadmin.tar.gz -C /www/server
mv /www/server/phpMyAdmin-5.2.2-all-languages /www/server/phpmyadmin
SECRET=$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')
cat > /www/server/phpmyadmin/config.inc.php <<'PHPEOF'
<?php
$cfg['blowfish_secret'] = 'PLACEHOLDER_SECRET';
$i = 0;
$i++;
$cfg['Servers'][$i]['auth_type'] = 'cookie';
$cfg['Servers'][$i]['host'] = 'localhost';
$cfg['Servers'][$i]['compress'] = false;
$cfg['Servers'][$i]['AllowNoPassword'] = true;
$cfg['Servers'][$i]['hide_db'] = '^(information_schema|mysql|performance_schema|sys)$';
$cfg['PmaAbsoluteUri'] = '/phpmyadmin/';
$cfg['TitleDefault'] = '@DATABASE - phpMyAdmin';
$cfg['TitleServer'] = '@VSERVER - phpMyAdmin';
$cfg['TitleDatabase'] = '@DATABASE - phpMyAdmin';
$cfg['TitleTable'] = '@TABLE - @DATABASE - phpMyAdmin';
$cfg['DefaultLang'] = 'zh_CN';
PHPEOF
sed -i "s|PLACEHOLDER_SECRET|$SECRET|" /www/server/phpmyadmin/config.inc.php
mkdir -p /etc/nginx/conf.d
cat > /etc/nginx/conf.d/lp_phpmyadmin.conf <<'NGINXEOF'
server {
    listen unix:/run/lp_pma.sock;
    server_name _;
    root /www/server/phpmyadmin;
    index index.php;
    access_log off;
    error_log /var/log/nginx/phpmyadmin-error.log warn;
    location / {
        try_files $uri $uri/ /index.php$is_args$args;
    }
    location ~ \.php$ {
        fastcgi_pass PLACEHOLDER_FPM;
        fastcgi_index index.php;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        include fastcgi_params;
    }
    location ~ \.(sql|log|txt)$ { deny all; }
}
NGINXEOF
sed -i "s|PLACEHOLDER_FPM|$FPM|" /etc/nginx/conf.d/lp_phpmyadmin.conf
rm -f /run/lp_pma.sock
if command -v nginx >/dev/null 2>&1; then
    nginx -t && nginx -s reload
fi
echo "phpMyAdmin 5.2.2 安装完成，通过面板「数据库」-> MySQL 的「管理」按钮进入"`

// pmaUninstallScript phpMyAdmin 卸载脚本
const pmaUninstallScript = `#!/bin/bash
rm -rf /www/server/phpmyadmin
rm -f /etc/nginx/conf.d/lp_phpmyadmin.conf
if command -v nginx >/dev/null 2>&1; then
    nginx -t && nginx -s reload || true
fi
echo "phpMyAdmin 已卸载"`

// vsftpdInstallScript vsftpd 安装脚本：
// 1. apt/yum 安装 vsftpd
// 2. 写 /etc/vsftpd.conf：启用本地用户、root 宿主目录、被动模式 39000-40000
// 3. 启动服务
const vsftpdInstallScript = `#!/bin/bash
set -e
if command -v apt-get >/dev/null 2>&1; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get install -y vsftpd
elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
    PM=dnf
    command -v dnf >/dev/null 2>&1 || PM=yum
    $PM install -y vsftpd
fi

CONF=/etc/vsftpd.conf
if [ ! -f "$CONF" ]; then
    CONF=/etc/vsftpd/vsftpd.conf
fi
if [ -f "$CONF" ]; then
    # 备份原始配置
    cp "$CONF" "$CONF.bak.$(date +%s)"
    # 确保关键配置存在（幂等）
    ensure_opt() {
        local key=$1 val=$2
        if grep -q "^\\s*${key}\\s*=" "$CONF"; then
            sed -i "s|^\\s*${key}\\s*=.*|${key}=${val}|" "$CONF"
        else
            echo "${key}=${val}" >> "$CONF"
        fi
    }
    ensure_opt "listen" "NO"
    ensure_opt "listen_ipv6" "YES"
    ensure_opt "anonymous_enable" "NO"
    ensure_opt "local_enable" "YES"
    ensure_opt "write_enable" "YES"
    ensure_opt "local_umask" "022"
    ensure_opt "dirmessage_enable" "YES"
    ensure_opt "use_localtime" "YES"
    ensure_opt "xferlog_enable" "YES"
    ensure_opt "connect_from_port_20" "YES"
    ensure_opt "pam_service_name" "vsftpd"
    ensure_opt "local_root" "/home"
    ensure_opt "chroot_local_user" "YES"
    ensure_opt "allow_writeable_chroot" "YES"
    ensure_opt "pasv_enable" "YES"
    ensure_opt "pasv_min_port" "39000"
    ensure_opt "pasv_max_port" "40000"
    ensure_opt "seccomp_sandbox" "NO"
fi

systemctl enable vsftpd 2>/dev/null || true
systemctl restart vsftpd 2>/dev/null || systemctl start vsftpd 2>/dev/null || true
echo "FTP (vsftpd) 安装完成"`

// vsftpdUninstallScript vsftpd 卸载脚本
const vsftpdUninstallScript = `#!/bin/bash
systemctl stop vsftpd 2>/dev/null || true
systemctl disable vsftpd 2>/dev/null || true
if command -v apt-get >/dev/null 2>&1; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get remove -y vsftpd 2>/dev/null || true
elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
    PM=dnf
    command -v dnf >/dev/null 2>&1 || PM=yum
    $PM remove -y vsftpd 2>/dev/null || true
fi
echo "FTP (vsftpd) 已卸载"`

// mongodbInstallScript MongoDB 安装脚本
// 官方 apt/yum 仓库密钥使用 SHA1 签名，Debian 13+ 自 2026-02-01 起默认拒绝（sqv 策略），
// 无法通过签名校验，故改用官方 tarball 部署（glibc 向后兼容），兼容 Debian 13。
const mongodbInstallScript = `#!/bin/bash
set -e
install_mongodb_org() {
    local version="${SELECTED_VERSION:-7.0}"
    # 清理历史遗留的官方 apt 源（其 SHA1 签名在 Debian 13+ 无法通过校验）
    rm -f /etc/apt/sources.list.d/mongodb-org-*.list /usr/share/keyrings/mongodb-server-*.gpg 2>/dev/null || true
    if command -v apt-get >/dev/null 2>&1; then
        export DEBIAN_FRONTEND=noninteractive
        apt-get update -y
        apt-get install -y curl ca-certificates
    fi
    local patch="" osbuild="debian12"
    case "$version" in
        7.0) patch="7.0.22" ;;
        6.0) patch="6.0.23"; osbuild="debian11" ;;
        *) echo "不支持的 MongoDB 版本: $version" >&2; exit 1 ;;
    esac
    local arch=$(uname -m)
    case "$arch" in
        x86_64) ;;
        aarch64) osbuild="ubuntu2204" ;;
        *) echo "不支持的架构: $arch" >&2; exit 1 ;;
    esac
    if [ ! -x /usr/local/bin/mongod ]; then
        local url="https://fastdl.mongodb.org/linux/mongodb-linux-${arch}-${osbuild}-${patch}.tgz"
        echo "下载 MongoDB ${patch} (${arch}) ..."
        local tmpdir=$(mktemp -d)
        curl -fsSL --retry 3 --retry-delay 3 --connect-timeout 30 "$url" | tar -xz -C "$tmpdir"
        mkdir -p /opt/mongodb
        cp -r "$tmpdir"/mongodb-linux-*/bin /opt/mongodb/
        ln -sf /opt/mongodb/bin/mongod /usr/local/bin/mongod
        for tool in mongosh mongo mongodump mongorestore mongoexport mongoimport; do
            [ -f "/opt/mongodb/bin/$tool" ] && ln -sf "/opt/mongodb/bin/$tool" "/usr/local/bin/$tool" || true
        done
        rm -rf "$tmpdir"
    fi
    id -u mongodb >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin mongodb
    mkdir -p /var/lib/mongodb /var/log/mongodb /var/run/mongodb
    chown -R mongodb:mongodb /var/lib/mongodb /var/log/mongodb /var/run/mongodb
    cat > /etc/mongod.conf <<'EOF'
systemLog:
  destination: file
  logAppend: true
  path: /var/log/mongodb/mongod.log
storage:
  dbPath: /var/lib/mongodb
  journal:
    enabled: true
net:
  port: 27017
  bindIp: 127.0.0.1
EOF
    cat > /etc/systemd/system/mongod.service <<'EOF'
[Unit]
Description=MongoDB Database Server
After=network-online.target
Wants=network-online.target

[Service]
User=mongodb
Group=mongodb
Type=simple
ExecStart=/usr/local/bin/mongod --config /etc/mongod.conf
Restart=on-failure
LimitNOFILE=64000

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    systemctl enable mongod
    systemctl start mongod || systemctl restart mongod
    echo "MongoDB ${patch} 安装完成"
}
install_mongodb_org
echo "MongoDB 安装完成"`

// mongodbUninstallScript MongoDB 卸载脚本（兼容 tarball 与旧 apt/rpm 安装）
const mongodbUninstallScript = `#!/bin/bash
systemctl stop mongod 2>/dev/null || true
systemctl disable mongod 2>/dev/null || true
rm -f /etc/systemd/system/mongod.service
rm -f /usr/local/bin/mongod /usr/local/bin/mongosh /usr/local/bin/mongo
rm -f /usr/local/bin/mongodump /usr/local/bin/mongorestore
rm -f /usr/local/bin/mongoexport /usr/local/bin/mongoimport
rm -rf /opt/mongodb
if command -v apt-get >/dev/null 2>&1; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get remove -y --purge mongodb-org* 2>/dev/null || true
    apt-get autoremove -y
    rm -f /etc/apt/sources.list.d/mongodb-org-*.list
    rm -f /usr/share/keyrings/mongodb-server-*.gpg
elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
    PM=dnf
    command -v dnf >/dev/null 2>&1 || PM=yum
    $PM remove -y mongodb-org* 2>/dev/null || true
    rm -f /etc/yum.repos.d/mongodb-org-*.repo
fi
systemctl daemon-reload
rm -rf /var/log/mongodb /var/lib/mongodb /var/run/mongodb
echo "MongoDB 已卸载"`

// sqlserverInstallScript SQLServer for Linux 官方仓库安装脚本
const sqlserverInstallScript = `#!/bin/bash
set -e
. /etc/os-release
install_mssql() {
    local version="${SELECTED_VERSION:-2022}"
    if command -v apt-get >/dev/null 2>&1; then
        export DEBIAN_FRONTEND=noninteractive
        apt-get update -y
        apt-get install -y curl apt-transport-https gnupg2 ca-certificates
        # software-properties-common 仅 Ubuntu 提供，Debian 上安装会失败，忽略即可
        apt-get install -y software-properties-common >/dev/null 2>&1 || true
        # 引擎与工具源分离：mssql-server 在独立仓库 mssql-server-<版本>
        local dist="" path=""
        if [ "$ID" = "debian" ]; then
            # 微软官方不支持 Debian，回退 Ubuntu LTS 源尝试安装
            case "$version" in
                2019) path="20.04"; dist="focal" ;;
                *)    path="22.04"; dist="jammy" ;;
            esac
        else
            dist="${UBUNTU_CODENAME:-$VERSION_CODENAME}"
            case "$dist" in
                noble) path="24.04" ;;
                jammy) path="22.04" ;;
                focal) path="20.04" ;;
                *) path="$dist" ;;
            esac
        fi
        if [ -z "$dist" ]; then
            echo "无法识别 Debian/Ubuntu 版本代号" >&2
            exit 1
        fi
        curl -fsSL https://packages.microsoft.com/keys/microsoft.asc | gpg --batch --yes --no-tty --dearmor -o /usr/share/keyrings/microsoft-prod.gpg
        echo "deb [arch=amd64 signed-by=/usr/share/keyrings/microsoft-prod.gpg] https://packages.microsoft.com/ubuntu/${path}/prod ${dist} main" > /etc/apt/sources.list.d/mssql-release.list
        echo "deb [arch=amd64 signed-by=/usr/share/keyrings/microsoft-prod.gpg] https://packages.microsoft.com/ubuntu/${path}/mssql-server-${version} ${dist} main" > /etc/apt/sources.list.d/mssql-server.list
        apt-get update -y
        # SQLServer 引擎
        ACCEPT_EULA=Y apt-get install -y mssql-server
        # 命令行工具
        ACCEPT_EULA=Y apt-get install -y mssql-tools18 unixodbc-dev
        # 让 sqlcmd 在 PATH 中
        mkdir -p /usr/local/bin
        ln -sf /opt/mssql-tools18/bin/sqlcmd /usr/local/bin/sqlcmd 2>/dev/null || true
    elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
        PM=dnf
        command -v dnf >/dev/null 2>&1 || PM=yum
        curl -fsSL https://packages.microsoft.com/config/rhel/${VERSION_ID:-8}/mssql-server-${version}.repo -o /etc/yum.repos.d/mssql-server.repo
        curl -fsSL https://packages.microsoft.com/config/rhel/${VERSION_ID:-8}/prod.repo -o /etc/yum.repos.d/msprod.repo
        ACCEPT_EULA=Y $PM install -y mssql-server
        ACCEPT_EULA=Y $PM install -y mssql-tools18 unixODBC-devel
        ln -sf /opt/mssql-tools18/bin/sqlcmd /usr/local/bin/sqlcmd 2>/dev/null || true
    else
        echo "不支持的系统包管理器" >&2
        exit 1
    fi
}
install_mssql
# 提示用户运行 mssql-conf setup 设置 SA 密码
if [ -x /opt/mssql/bin/mssql-conf ]; then
    echo "============================================================"
    echo "SQLServer 已安装。请使用以下命令设置 SA 密码并启动服务："
    echo "  /opt/mssql/bin/mssql-conf setup"
    echo "设置完成后，请在面板「数据库」->「SQLServer」标签使用 SA 密码。"
    echo "============================================================"
fi
systemctl daemon-reload
echo "SQLServer 安装完成"`

// sqlserverUninstallScript SQLServer 卸载脚本
const sqlserverUninstallScript = `#!/bin/bash
systemctl stop mssql-server 2>/dev/null || true
systemctl disable mssql-server 2>/dev/null || true
if command -v apt-get >/dev/null 2>&1; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get remove -y --purge mssql-server* mssql-tools* unixodbc-dev || true
    apt-get autoremove -y
    rm -f /etc/apt/sources.list.d/mssql-release.list /etc/apt/sources.list.d/mssql-server.list
    rm -f /usr/share/keyrings/microsoft-prod.gpg
elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
    PM=dnf
    command -v dnf >/dev/null 2>&1 || PM=yum
    $PM remove -y mssql-server* mssql-tools* unixODBC-devel || true
    rm -f /etc/yum.repos.d/mssql-server-*.repo /etc/yum.repos.d/msprod.repo
fi
rm -f /usr/local/bin/sqlcmd
rm -rf /var/opt/mssql /opt/mssql
echo "SQLServer 已卸载"`
