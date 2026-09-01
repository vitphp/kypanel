package service

import (
	"strings"
	"sync"

	"kypanel/internal/model"
)

// builtinChannels 应用商店内置下载渠道（多渠道加速）。
//
// Key 约定（安装脚本通过 LP_* 变量读取，injectChannelHelpers 按前缀匹配注入）：
//   - go             完整下载 URL 模板（含 ${FULL} 完整版本号、${ARCH} 架构占位符）
//   - node-mirror    Node base 镜像（目录结构与 nodejs.org/dist 一致，供 nvm 使用）
//   - python         完整源码 URL 模板（含 ${FULL} 占位符）
//   - python-mirror  Python base 镜像（带版本子目录结构，供 pyenv 编译兜底）
//   - phpmyadmin     phpMyAdmin 完整包 URL 模板（含 ${VER} 版本占位符）
//   - mongodb        MongoDB 完整包 URL 模板（含 ${FULL}/${ARCH}/${OSBUILD} 占位符）
//   - nvm            nvm 安装脚本 URL（无占位符）
//   - pyenv          pyenv 仓库 git 地址（无占位符）
//
// 面板安装时会对这些渠道做延迟探测，优先选择最快渠道，下载失败自动切换下一个。
var builtinChannels = []model.AppChannel{
	// Go（完整 tarball，命名 go1.25.14.linux-amd64.tar.gz）
	{Key: "go", Name: "阿里云", Order: 1, URL: "https://mirrors.aliyun.com/golang/go${FULL}.linux-${ARCH}.tar.gz"},
	{Key: "go", Name: "Go 中国官方", Order: 2, URL: "https://golang.google.cn/dl/go${FULL}.linux-${ARCH}.tar.gz"},
	{Key: "go", Name: "Go 官方", Order: 3, URL: "https://go.dev/dl/go${FULL}.linux-${ARCH}.tar.gz"},

	// Node.js（base 镜像，nvm 的 NVM_NODEJS_ORG_MIRROR 直接使用）
	{Key: "node-mirror", Name: "阿里云", Order: 1, URL: "https://mirrors.aliyun.com/nodejs-release"},
	{Key: "node-mirror", Name: "npmmirror", Order: 2, URL: "https://npmmirror.com/mirrors/node"},
	{Key: "node-mirror", Name: "Node 官方", Order: 3, URL: "https://nodejs.org/dist"},

	// nvm 安装脚本（先装 nvm 再装 Node）
	{Key: "nvm", Name: "GitHub 官方", Order: 1, URL: "https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.0/install.sh"},
	{Key: "nvm", Name: "Gitee 镜像", Order: 2, URL: "https://gitee.com/mirrors/nvm/raw/v0.40.0/install.sh"},

	// Python（完整源码 tarball，命名 Python-3.13.15.tar.xz）
	{Key: "python", Name: "华为云", Order: 1, URL: "https://mirrors.huaweicloud.com/python/${FULL}/Python-${FULL}.tar.xz"},
	{Key: "python", Name: "阿里云", Order: 2, URL: "https://mirrors.aliyun.com/python-release/source/Python-${FULL}.tar.xz"},
	{Key: "python", Name: "npmmirror", Order: 3, URL: "https://npmmirror.com/mirrors/python/${FULL}/Python-${FULL}.tar.xz"},
	{Key: "python", Name: "清华", Order: 4, URL: "https://mirrors.tuna.tsinghua.edu.cn/python/${FULL}/Python-${FULL}.tar.xz"},
	{Key: "python", Name: "官方", Order: 5, URL: "https://www.python.org/ftp/python/${FULL}/Python-${FULL}.tar.xz"},

	// Python（base 镜像，带版本子目录结构，供 PYTHON_BUILD_MIRROR_URL 兜底）
	{Key: "python-mirror", Name: "华为云", Order: 1, URL: "https://mirrors.huaweicloud.com/python"},
	{Key: "python-mirror", Name: "npmmirror", Order: 2, URL: "https://npmmirror.com/mirrors/python"},
	{Key: "python-mirror", Name: "清华", Order: 3, URL: "https://mirrors.tuna.tsinghua.edu.cn/python"},
	{Key: "python-mirror", Name: "官方", Order: 4, URL: "https://www.python.org/ftp/python"},

	// pyenv 仓库（先装 pyenv 再编译 Python）
	{Key: "pyenv", Name: "GitHub 官方", Order: 1, URL: "https://github.com/pyenv/pyenv.git"},
	{Key: "pyenv", Name: "清华 GitHub 代理", Order: 2, URL: "https://mirrors.tuna.tsinghua.edu.cn/github/pyenv/pyenv.git"},
	{Key: "pyenv", Name: "Gitee 镜像", Order: 3, URL: "https://gitee.com/mirrors/pyenv.git"},

	// phpMyAdmin（all-languages 完整包）
	{Key: "phpmyadmin", Name: "官方", Order: 1, URL: "https://files.phpmyadmin.net/phpMyAdmin/${VER}/phpMyAdmin-${VER}-all-languages.tar.gz"},
	{Key: "phpmyadmin", Name: "清华镜像", Order: 2, URL: "https://mirrors.tuna.tsinghua.edu.cn/phpmyadmin/phpMyAdmin-${VER}-all-languages.tar.gz"},

	// MongoDB（linux tarball，命名 mongodb-linux-x86_64-debian12-7.0.22.tgz）
	{Key: "mongodb", Name: "MongoDB 官方", Order: 1, URL: "https://fastdl.mongodb.org/linux/mongodb-linux-${ARCH}-${OSBUILD}-${FULL}.tgz"},
	{Key: "mongodb", Name: "华为云镜像", Order: 2, URL: "https://mirrors.huaweicloud.com/mongodb/linux/mongodb-linux-${ARCH}-${OSBUILD}-${FULL}.tgz"},
}

// channelHelpers 注入到安装脚本顶部的多渠道加速辅助函数：
//   - lp_pick_fastest_url <urls...>：对候选 URL 逐个做 HEAD 测速（-r 0-1023 只拉首块），输出响应最快的 URL
//   - lp_download_fallback <out> <urls...>：按传入顺序逐个下载，成功（非空文件）即返回 0
const channelHelpers = `# ===== 多渠道加速（面板注入）=====
# lp_pick_fastest_url <urls...>：测速选最快源
# 注意：安装脚本以 set -e 运行，此处任何 curl 网络错误（连接重置/超时，如退出码 56）
# 都会让整个赋值语句以非零退出、中断脚本。因此所有 curl 必须追加 || true，
# 并显式 return 0，确保测速失败（全部源不可测）时只是返回空、由调用方走 fallback。
lp_pick_fastest_url() {
    local best="" best_ms=999999 url ms
    for url in "$@"; do
        ms=$(curl -o /dev/null -s -m 6 -w '%{time_total}' -r 0-1023 -I "$url" 2>/dev/null) || true
        ms=$(echo "$ms" | awk '{printf "%d", $1*1000}')
        case "$ms" in
            ''|*[!0-9]*) ms=999999 ;;
        esac
        if [ "$ms" -lt "$best_ms" ]; then best_ms=$ms; best="$url"; fi
    done
    echo "$best"
    return 0
}
# lp_download_fallback <out> <urls...>：按顺序逐个下载，成功返回 0
lp_download_fallback() {
    local out="$1"; shift
    local url
    for url in "$@"; do
        echo "尝试下载: $url"
        if curl -fsSL --retry 2 --connect-timeout 15 --max-time 600 -o "$out" "$url" && [ -s "$out" ]; then
            echo "下载成功: $url"
            return 0
        fi
        rm -f "$out"
        echo "下载失败，切换下一个源"
    done
    return 1
}
# ===== 多渠道加速结束 =====
`

// injectChannelHelpers 把应用的多源渠道以 LP_CHANNELS 变量注入安装脚本头部。
// LP_CHANNELS 每行一个下载地址模板，支持 ${VER}/${FULL}/${ARCH}/${OSBUILD} 占位符，
// 脚本内先调用 lp_pick_fastest_url 测速选最快源，下载失败调用 lp_download_fallback 逐个回退。
func injectChannelHelpers(channels []string, script string) string {
	if len(channels) == 0 {
		return script
	}
	export := "LP_CHANNELS='" + strings.Join(channels, "\n") + "'\n"
	return channelHelpers + export + script
}

var channelsOnce sync.Once

// EnsureAppChannels 把内置渠道写入数据库（首次启动时执行，之后用户可在面板中增删改）。
// 仅当渠道表为空时写入，避免覆盖用户自维护的数据。
func EnsureAppChannels() {
	channelsOnce.Do(func() {
		n, err := model.CountAppChannels()
		if err != nil {
			return
		}
		if n > 0 {
			return
		}
		if err := model.UpsertAppChannels(builtinChannels); err != nil {
			return
		}
	})
}
