#!/usr/bin/env bash
# 构建 kypanel：先构建前端（vite build -> webui/dist），再交叉编译后端并内嵌前端
# 用法: ./scripts/build.sh [amd64|arm64] [版本号]   (默认架构 amd64，默认版本 0.30)
#       也可用环境变量 ARCH / VERSION 覆盖，例如: VERSION=0.31 ./scripts/build.sh arm64
set -e

cd "$(dirname "$0")/.."

ARCH="${1:-${ARCH:-amd64}}"
VERSION="${2:-${VERSION:-0.30}}"
echo ">> 构建架构 ${ARCH}，版本 ${VERSION}"
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "dev")
DATE=$(date '+%Y-%m-%d %H:%M:%S')
OUT="bin/kypanel-linux-${ARCH}"

# 1) 构建前端并输出到 webui/dist（go:embed 内嵌，实现前后端单文件）
if [ -d web ] && [ -f web/package.json ]; then
  echo ">> 构建前端 (vite build -> webui/dist)"
  (cd web && npm install --no-audit --no-fund >/dev/null 2>&1 && npm run build)
  echo ">> 前端构建完成"
fi

# 2) 交叉编译后端（内嵌前端）
echo ">> 编译 linux/${ARCH} -> ${OUT}"
CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} go build \
  -ldflags "-s -w \
    -X kypanel/internal/version.Version=${VERSION} \
    -X kypanel/internal/version.Commit=${COMMIT} \
    -X 'kypanel/internal/version.Date=${DATE}'" \
  -o "${OUT}" ./cmd/panel

echo ">> 完成: ${OUT} ($(du -h "${OUT}" | cut -f1))"

# 3) 生成发布包到 bin/（安装脚本 i.sh + 后端二进制 + 前端独立包 + 离线 IP 库）
echo ">> 生成发布包到 bin/"
mkdir -p bin
cp scripts/install.sh bin/i.sh
cp "${OUT}" bin/kypanel_${ARCH}
(cd webui/dist && tar -czf ../../bin/panel-web.tar.gz .)
if [ -f data/ip2region.xdb ]; then
  cp data/ip2region.xdb bin/ip2region.xdb
  echo ">> 已打包离线 IP 库 (ip2region.xdb)"
else
  echo ">> [warn] data/ip2region.xdb 不存在，发布包不含离线 IP 库"
fi
echo ">> 发布包:"
ls -lh bin/i.sh "bin/kypanel_${ARCH}" bin/panel-web.tar.gz bin/ip2region.xdb
