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

// ===== 多源下载渠道（面板安装时先测速选最快源，下载失败自动切换下一个源）=====
// 每项为下载地址模板，支持占位符：${VER}（可选版本）、${FULL}（完整补丁版本号）、
// ${ARCH}（架构 amd64/arm64）、${OSBUILD}（发行版代号，如 debian12）。
var (
	// Go 官方 tarball（命名 go1.25.14.linux-amd64.tar.gz）
	goChannels = []string{
		"https://mirrors.aliyun.com/golang/go${FULL}.linux-${ARCH}.tar.gz",
		"https://golang.google.cn/dl/go${FULL}.linux-${ARCH}.tar.gz",
		"https://go.dev/dl/go${FULL}.linux-${ARCH}.tar.gz",
	}
	// Node base 镜像（目录结构与 nodejs.org/dist 一致，nvm 的 NVM_NODEJS_ORG_MIRROR 直接使用）
	nodeChannels = []string{
		"https://mirrors.aliyun.com/nodejs-release",
		"https://npmmirror.com/mirrors/node",
		"https://nodejs.org/dist",
	}
	// Python 完整源码 tarball（命名 Python-3.13.15.tar.xz）
	pythonChannels = []string{
		"https://mirrors.huaweicloud.com/python/${FULL}/Python-${FULL}.tar.xz",
		"https://mirrors.aliyun.com/python-release/source/Python-${FULL}.tar.xz",
		"https://npmmirror.com/mirrors/python/${FULL}/Python-${FULL}.tar.xz",
		"https://mirrors.tuna.tsinghua.edu.cn/python/${FULL}/Python-${FULL}.tar.xz",
		"https://www.python.org/ftp/python/${FULL}/Python-${FULL}.tar.xz",
	}
	// phpMyAdmin 完整包
	pmaChannels = []string{
		"https://files.phpmyadmin.net/phpMyAdmin/${VER}/phpMyAdmin-${VER}-all-languages.tar.gz",
		"https://mirrors.tuna.tsinghua.edu.cn/phpmyadmin/phpMyAdmin-${VER}-all-languages.tar.gz",
	}
	// MongoDB linux tarball（命名 mongodb-linux-x86_64-debian12-7.0.22.tgz）
	mongodbChannels = []string{
		"https://fastdl.mongodb.org/linux/mongodb-linux-${ARCH}-${OSBUILD}-${FULL}.tgz",
		"https://mirrors.huaweicloud.com/mongodb/linux/mongodb-linux-${ARCH}-${OSBUILD}-${FULL}.tgz",
	}
	// Docker 官方安装脚本（get.docker.com 支持 --mirror Aliyun 加速）
	dockerChannels = []string{
		"https://get.docker.com/",
		"https://mirrors.aliyun.com/docker-ce/linux/static/stable/${ARCH}/docker-${FULL}.tgz",
	}
)

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
			VersionCmd:      `ls "$HOME/.nvm/versions/node" 2>/dev/null | grep -oE '^v` + v + `\.[0-9]+\.[0-9]+' | head -n1 || /usr/local/node` + v + `/bin/node -v 2>/dev/null || true`,
			InstallScript:   nodeInstallScript(v),
			UninstallScript: nodeUninstallScript(v),
			Channels:        nodeChannels,
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
			VersionCmd:      `ls "$HOME/.pyenv/versions" 2>/dev/null | grep -x "` + pv.Full + `" | head -n1 || /usr/local/python` + v + `/bin/python` + pv.Major + ` -V 2>/dev/null || true`,
			InstallScript:   pythonInstallScript(v, pv.Full),
			UninstallScript: pythonUninstallScript(pv.Full, v),
			Channels:        pythonChannels,
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
			Channels:        goChannels,
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
		Channels:         pmaChannels,
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
		// 该应用无真实可卸载二进制，探测命令恒真（tar 系统自带），
		// 依赖面板按 InstallMarkFile 标记文件判定安装状态（安装后 touch、卸载后删除）。
		InstallMarkFile: "{DataDir}/logs/apps/.site-migrate.installed",
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
    # 官方源已包含目标版本（如 Debian 13 自带 PHP 8.4）时无需 sury 仓库
    if ! apt-cache policy php{{VER}}-fpm 2>/dev/null | grep -q 'Candidate:'; then
        code=$(lsb_release -sc 2>/dev/null)
        [ -z "$code" ] && code=$(sed -n 's/^VERSION_CODENAME=//p' /etc/os-release 2>/dev/null)
        if [ -n "$code" ] && [ -d /etc/apt/sources.list.d ]; then
            # 下载失败重试 3 次（曾因瞬时网络失败导致 sury 源未启用而安装报"无候选包"）
            for i in 1 2 3; do
                curl -fsSL --connect-timeout 10 --max-time 120 https://packages.sury.org/php/apt.gpg -o /usr/share/keyrings/sury-php.gpg && break
                sleep 3
            done
            if [ -s /usr/share/keyrings/sury-php.gpg ]; then
                echo "deb [signed-by=/usr/share/keyrings/sury-php.gpg] https://packages.sury.org/php/ $code main" > /etc/apt/sources.list.d/php-sury.list
                apt-get update -y || true
            else
                echo "警告: 下载 sury 仓库密钥失败，若系统源无 php{{VER}}-fpm 将无法安装" >&2
            fi
        fi
    fi
    if ! apt-get install -y php{{VER}}-fpm php{{VER}}-cli{{APT_EXT}}; then
        # 首次失败多为源索引未刷新，重新 update 后重试一次
        apt-get update -y || true
        apt-get install -y php{{VER}}-fpm php{{VER}}-cli{{APT_EXT}}
    fi
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
    DEBIAN_FRONTEND=noninteractive apt-get remove -y --purge php{{VER}}-fpm php{{VER}}-cli{{APT_EXT}} || true
    apt-get autoremove -y || true
    systemctl stop php{{VER}}-fpm 2>/dev/null || true
    systemctl disable php{{VER}}-fpm 2>/dev/null || true
    # 多版本共存时保留 sury 源，仅当无任何已装 PHP-FPM 时才清理
    if ! dpkg -l 2>/dev/null | grep -q '^ii  php.*-fpm'; then
        rm -f /etc/apt/sources.list.d/php-sury.list
        rm -f /usr/share/keyrings/sury-php.gpg
    fi
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
// 全链路多渠道 + 超时保护：
//  1. nvm 本体：gitee clone 优先（国内快）、GitHub clone 兜底，均带 timeout 防无限挂起，
//     再兜底官方 install.sh（gitee/GitHub 双源，curl 带超时）；
//  2. node 二进制：走面板注入的 LP_CHANNELS 测速选最快镜像，下载失败自动切换下一个；
//  3. 所有 nvm 调用都是 shell 函数，必须经 bash -c 重新 source nvm.sh 后用 timeout 包裹。
func nodeInstallScript(ver string) string {
	return `#!/bin/bash
set -e
export NVM_DIR="${NVM_DIR:-$HOME/.nvm}"
NVM_TAG="v0.40.0"
# ===== 1. 安装 nvm（多渠道 + 超时，避免 GitHub 不通时无限挂起）=====
if [ ! -s "$NVM_DIR/nvm.sh" ]; then
    echo "开始安装 nvm ..."
    rm -rf "$NVM_DIR"
    mkdir -p "$NVM_DIR"
    if timeout 90 git clone --depth 1 https://gitee.com/mirrors/nvm.git "$NVM_DIR" >/dev/null 2>&1; then
        echo "nvm 已通过 gitee 镜像安装"
    elif timeout 180 git clone --depth 1 --branch "$NVM_TAG" https://github.com/nvm-sh/nvm.git "$NVM_DIR" >/dev/null 2>&1; then
        echo "nvm 已通过 GitHub 安装"
    else
        echo "git clone 失败，改用 install.sh 安装 nvm ..."
        curl -fsSL --connect-timeout 10 --max-time 60 https://gitee.com/mirrors/nvm/raw/$NVM_TAG/install.sh -o /tmp/nvm-install.sh \
            || curl -fsSL --connect-timeout 10 --max-time 60 https://raw.githubusercontent.com/nvm-sh/nvm/$NVM_TAG/install.sh -o /tmp/nvm-install.sh
        NVM_DIR="$NVM_DIR" bash /tmp/nvm-install.sh >/dev/null 2>&1 || true
    fi
fi
[ -s "$NVM_DIR/nvm.sh" ] && . "$NVM_DIR/nvm.sh"
# ===== 2. 多渠道加速：测速选最快 Node 镜像，下载失败自动切换下一个镜像 =====
if [ -n "$LP_CHANNELS" ]; then
    FAST=$(lp_pick_fastest_url $LP_CHANNELS)
    if [ -n "$FAST" ]; then
        export NVM_NODEJS_ORG_MIRROR="$FAST"
        echo "已选择最快 Node 镜像: $FAST"
    fi
fi
# 未测出最快源时用 npmmirror 保底，避免直连 nodejs.org 卡死
export NVM_NODEJS_ORG_MIRROR="${NVM_NODEJS_ORG_MIRROR:-https://npmmirror.com/mirrors/node}"
# npm registry 国内加速
touch "$HOME/.npmrc"
grep -q '^registry=' "$HOME/.npmrc" 2>/dev/null || echo 'registry=https://registry.npmmirror.com' >> "$HOME/.npmrc"
# ===== 3. 安装 Node（nvm 为 shell 函数，经 bash -c 重新 source；timeout 防挂起）=====
if ! timeout 600 bash -c 'source "$NVM_DIR/nvm.sh" && nvm install ` + ver + `'; then
    echo "当前镜像下载失败，切换下一个源"
    DL2=0
    for m in $LP_CHANNELS; do
        [ "$m" = "$FAST" ] && continue
        export NVM_NODEJS_ORG_MIRROR="$m"
        if timeout 600 bash -c 'source "$NVM_DIR/nvm.sh" && nvm install ` + ver + `'; then DL2=1; break; fi
    done
    [ "$DL2" = "1" ] || timeout 900 bash -c 'unset NVM_NODEJS_ORG_MIRROR; source "$NVM_DIR/nvm.sh" && nvm install ` + ver + `'
fi
timeout 60 bash -c 'source "$NVM_DIR/nvm.sh" && nvm alias default ` + ver + `' >/dev/null 2>&1 || true
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
# 兜底：nvm uninstall 因别名/引用等失败时，手动删除版本目录（探测按 nvm versions/node/v<ver>.* 判断）
for base in "$HOME/.nvm" /root/.nvm /.nvm; do
    rm -rf "$base"/versions/node/v` + ver + `.*
done
# 清理历史遗留的 /usr/local 安装目录（早期手动安装 / 安装脚本残留）
rm -rf /usr/local/node` + ver + `
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
    timeout 90 git clone --depth=1 https://mirrors.tuna.tsinghua.edu.cn/github/pyenv/pyenv.git "$PYENV_ROOT" 2>/dev/null \
        || timeout 120 git clone --depth=1 https://gitee.com/mirrors/pyenv.git "$PYENV_ROOT" 2>/dev/null \
        || timeout 180 git clone --depth=1 https://github.com/pyenv/pyenv.git "$PYENV_ROOT"
    git -C "$PYENV_ROOT" submodule update --init --depth=1 || true
fi

# 3. 预下载 Python 源码到 pyenv 缓存（文件名必须是 Python-<ver>.tar.xz，
#    否则 pyenv 不会命中缓存、会从 python.org 重新下载极慢）
CACHE="$PYENV_ROOT/cache/Python-` + full + `.tar.xz"
TMPCACHE="$CACHE.downloading"
mkdir -p "$PYENV_ROOT/cache"
if [ ! -s "$CACHE" ]; then
    rm -f "$TMPCACHE"
    DL_OK=0
    # 多渠道加速：LP_CHANNELS 由面板注入（每行一个源码包 URL 模板，含 ${FULL} 占位符）。
    # 替换占位符后测速选最快源；下载失败自动切换下一个源。
    # 先下载到 .downloading 临时文件，成功后原子 mv 到正式缓存，避免半截文件残留导致下次跳过下载。
    PY_URLS=""
    [ -n "$LP_CHANNELS" ] && PY_URLS=$(echo "$LP_CHANNELS" | sed "s|\${FULL}|` + full + `|g")
    if [ -n "$PY_URLS" ]; then
        FAST=$(lp_pick_fastest_url $PY_URLS)
        if [ -n "$FAST" ]; then
            echo "已选择最快 Python 源码源: $FAST"
            if curl -fsSL --retry 3 --connect-timeout 20 --max-time 600 -o "$TMPCACHE" "$FAST" && [ -s "$TMPCACHE" ]; then
                mv "$TMPCACHE" "$CACHE"
                DL_OK=1
            fi
        fi
        if [ "$DL_OK" != "1" ]; then
            if lp_download_fallback "$TMPCACHE" $PY_URLS; then
                mv "$TMPCACHE" "$CACHE"
                DL_OK=1
            fi
        fi
    fi
    # 兜底：面板未注入渠道时走内置多镜像级联
    if [ "$DL_OK" != "1" ]; then
        for u in \
            "https://mirrors.huaweicloud.com/python/` + full + `/Python-` + full + `.tar.xz" \
            "https://mirrors.aliyun.com/python-release/source/Python-` + full + `.tar.xz" \
            "https://npmmirror.com/mirrors/python/` + full + `/Python-` + full + `.tar.xz" \
            "https://mirrors.tuna.tsinghua.edu.cn/python/` + full + `/Python-` + full + `.tar.xz" \
            "https://www.python.org/ftp/python/` + full + `/Python-` + full + `.tar.xz"; do
            echo "下载 Python 源码: $u"
            if curl -fsSL --retry 3 --connect-timeout 20 --max-time 600 -o "$TMPCACHE" "$u" && [ -s "$TMPCACHE" ]; then
                mv "$TMPCACHE" "$CACHE"
                DL_OK=1
                break
            fi
            rm -f "$TMPCACHE"
            echo "镜像下载失败，切换下一个源"
        done
    fi
    [ "$DL_OK" = "1" ] || { echo "所有镜像下载失败，请检查网络"; exit 1; }
fi
# 若 pyenv 因故未命中缓存，也会走 PYTHON_BUILD_MIRROR_URL（清华源，目录结构与官方一致），
# 避免 pyenv 自行回退到极慢的 python.org。
export PYTHON_BUILD_MIRROR_URL="https://mirrors.tuna.tsinghua.edu.cn/python"
export PATH="$PYENV_ROOT/bin:$PATH"
eval "$(pyenv init -)"

# 4. 安装并切换默认版本
pyenv install -v ` + full + `
pyenv global ` + full + `
echo "Python ` + full + ` 安装完成"
`
}

// pythonUninstallScript 生成指定 Python 版本的卸载脚本
// full 为完整补丁版本号（pyenv 目录名，如 3.13.15），ver 为主版本号（用于清理 /usr/local 遗留，如 3.13）
func pythonUninstallScript(full, ver string) string {
	return `#!/bin/bash
set -e
export PYENV_ROOT="$HOME/.pyenv"
export PATH="$PYENV_ROOT/bin:$PATH"
eval "$(pyenv init -)" 2>/dev/null || true
pyenv uninstall -f ` + full + ` 2>/dev/null || true
# 兜底：pyenv uninstall 失败时手动删除版本目录（探测按 pyenv versions/<full> 判断）
for base in "$HOME/.pyenv" /root/.pyenv /.pyenv; do
    rm -rf "$base/versions/` + full + `"
done
# 重建 shims，清除已删除版本的残留入口（避免 PATH 探测误判「仍已安装」）
pyenv rehash 2>/dev/null || true
# 清理历史遗留的 /usr/local 安装目录
rm -rf /usr/local/python` + ver + `
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
# 多渠道加速：LP_CHANNELS 由面板注入（每行一个下载地址模板，含 ${FULL}/${ARCH} 占位符）。
# 替换占位符得到实际 URL 后测速选最快源；下载失败自动切换下一个源。
# --strip-components=1 去掉 tarball 顶层 go/ 目录直接解压到 INSTALL_DIR。
DL_OK=0
GO_URLS=""
[ -n "$LP_CHANNELS" ] && GO_URLS=$(echo "$LP_CHANNELS" | sed "s|\${FULL}|` + full + `|g; s|\${ARCH}|$GOARCH|g")
if [ -n "$GO_URLS" ]; then
    FAST=$(lp_pick_fastest_url $GO_URLS)
    if [ -n "$FAST" ]; then
        echo "已选择最快 Go 源: $FAST"
        if curl -fsSL --retry 3 --connect-timeout 20 --max-time 600 "$FAST" | tar -C "$INSTALL_DIR" --strip-components=1 -xzf -; then
            DL_OK=1
        fi
    fi
    if [ "$DL_OK" != "1" ]; then
        TMPTGZ=$(mktemp)
        if lp_download_fallback "$TMPTGZ" $GO_URLS; then
            tar -C "$INSTALL_DIR" --strip-components=1 -xzf "$TMPTGZ" && DL_OK=1
        fi
        rm -f "$TMPTGZ"
    fi
fi
# 兜底：面板未注入渠道时走内置多镜像
if [ "$DL_OK" != "1" ]; then
    for u in \
        "https://mirrors.aliyun.com/golang/go` + full + `.linux-${GOARCH}.tar.gz" \
        "https://golang.google.cn/dl/go` + full + `.linux-${GOARCH}.tar.gz" \
        "https://go.dev/dl/go` + full + `.linux-${GOARCH}.tar.gz"; do
        if curl -fsSL --retry 3 --connect-timeout 20 --max-time 600 "$u" | tar -C "$INSTALL_DIR" --strip-components=1 -xzf -; then
            DL_OK=1
            break
        fi
        echo "源下载失败，切换下一镜像: $u"
    done
fi
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
# 多渠道加速：LP_CHANNELS 由面板注入（每行一个下载地址模板，含 ${VER} 占位符），
# 替换占位符后测速选最快源；下载失败自动切换下一个源。
PMA_URLS=""
[ -n "$LP_CHANNELS" ] && PMA_URLS=$(echo "$LP_CHANNELS" | sed "s|\${VER}|5.2.2|g")
DL_OK=0
if [ -n "$PMA_URLS" ]; then
    FAST=$(lp_pick_fastest_url $PMA_URLS)
    if [ -n "$FAST" ]; then
        echo "已选择最快 phpMyAdmin 源: $FAST"
        if curl -fsSL --retry 3 --connect-timeout 20 --max-time 600 -o phpmyadmin.tar.gz "$FAST" && [ -s phpmyadmin.tar.gz ]; then
            DL_OK=1
        fi
    fi
    if [ "$DL_OK" != "1" ]; then
        rm -f phpmyadmin.tar.gz
        if lp_download_fallback phpmyadmin.tar.gz $PMA_URLS; then
            DL_OK=1
        fi
    fi
fi
if [ "$DL_OK" != "1" ]; then
    curl -fsSL --connect-timeout 20 --max-time 600 -o phpmyadmin.tar.gz "https://files.phpmyadmin.net/phpMyAdmin/5.2.2/phpMyAdmin-5.2.2-all-languages.tar.gz"
fi
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
set -e
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
        local tmpdir=$(mktemp -d)
        # 多渠道加速：LP_CHANNELS 由面板注入（每行一个下载地址模板，含 ${FULL}/${ARCH}/${OSBUILD} 占位符）。
        # 替换占位符后测速选最快源；下载失败自动切换下一个源。
        local DL_OK=0 MG_URLS="" FAST="" TGZ=""
        [ -n "$LP_CHANNELS" ] && MG_URLS=$(echo "$LP_CHANNELS" | sed "s|\${FULL}|${patch}|g; s|\${ARCH}|${arch}|g; s|\${OSBUILD}|${osbuild}|g")
        if [ -n "$MG_URLS" ]; then
            FAST=$(lp_pick_fastest_url $MG_URLS)
            if [ -n "$FAST" ]; then
                echo "已选择最快 MongoDB 源: $FAST"
                if curl -fsSL --retry 3 --connect-timeout 30 --max-time 600 "$FAST" | tar -xz -C "$tmpdir"; then
                    DL_OK=1
                fi
            fi
            if [ "$DL_OK" != "1" ]; then
                TGZ="$tmpdir/mongodb.tgz"
                if lp_download_fallback "$TGZ" $MG_URLS; then
                    tar -xz -f "$TGZ" -C "$tmpdir" && DL_OK=1
                fi
                rm -f "$TGZ"
            fi
        fi
        # 兜底：面板未注入渠道时走官方源
        if [ "$DL_OK" != "1" ]; then
            curl -fsSL --retry 3 --retry-delay 3 --connect-timeout 30 --max-time 600 "https://fastdl.mongodb.org/linux/mongodb-linux-${arch}-${osbuild}-${patch}.tgz" | tar -xz -C "$tmpdir"
        fi
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
        # SQLServer 引擎依赖 OpenLDAP 2.5（liblber-2.5.so.0），而 Debian 官方源只有
        # libldap 2.6（提供 liblber.so.2），不补装会导致 sqlservr 启动失败。
        # 从 Ubuntu jammy 仓库下载 libldap-2.5-0 补装（微软官方仅支持 Ubuntu）。
        if [ "$ID" = "debian" ]; then
            if ! ldconfig -p | grep -q "liblber-2.5"; then
                curl -fsSL -o /tmp/libldap-2.5-0.deb "https://archive.ubuntu.com/ubuntu/pool/main/o/openldap/libldap-2.5-0_2.5.20+dfsg-0ubuntu0.22.04.1_amd64.deb" || true
                if [ -s /tmp/libldap-2.5-0.deb ]; then
                    dpkg -i /tmp/libldap-2.5-0.deb || apt-get install -f -y
                    rm -f /tmp/libldap-2.5-0.deb
                fi
            fi
        fi
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
# 引擎已安装，EULA 接受与 SA 密码设置由面板在安装完成后自动完成（EnsureSqlserverSetup）
if [ -x /opt/mssql/bin/mssql-conf ]; then
    echo "SQLServer 引擎已安装，面板将自动完成 EULA 接受与 SA 密码设置。"
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
