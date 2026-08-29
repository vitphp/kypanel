#!/usr/bin/env bash
set -e

# ===== 修复模式（独立工具，跳过安装流程）=====
# 用法: bash <(curl -fsSL .../i.sh) -- --fix-https
if [ "${1:-}" = "--fix-https" ]; then
  set +e
  CERT_DIR=/opt/kypanel/certs
  LOG=/opt/kypanel/logs/panel.log
  PORT=$(grep -oP '"port"\s*:\s*\K[0-9]+' /opt/kypanel/config.json 2>/dev/null || echo 47568)

  echo "===== 1. 当前证书 ====="
  ls -la "$CERT_DIR" 2>&1 || echo "证书目录不存在"
  openssl x509 -in "$CERT_DIR/panel.crt" -noout -subject -dates 2>&1 || echo "证书读取失败"

  echo ""
  echo "===== 2. 最近错误日志 ====="
  if [ -f "$LOG" ]; then
    grep -iE 'error|证书|cert|tls' "$LOG" 2>/dev/null | tail -5
  else
    echo "（日志文件不存在）"
  fi

  echo ""
  echo "===== 3. 重新生成证书 ====="
  IP=$(hostname -I 2>/dev/null | awk '{print $1}')
  mkdir -p "$CERT_DIR"
  if openssl req -x509 -newkey rsa:2048 \
       -keyout "$CERT_DIR/panel.key" \
       -out "$CERT_DIR/panel.crt" \
       -days 3650 -nodes \
       -subj "/CN=${IP}" 2>&1; then
    chmod 600 "$CERT_DIR/panel.key"
    echo "[OK] 证书已重新生成"
  else
    echo "[FAIL] 证书生成失败（见上方 openssl 错误）"
    exit 1
  fi

  echo ""
  echo "===== 4. 重启服务 ====="
  systemctl restart kypanel
  sleep 3

  echo ""
  echo "===== 5. 自测 ====="
  if [ "$(curl -ks --max-time 4 -o /dev/null -w '%{http_code}' "https://127.0.0.1:${PORT}/api/ping" 2>/dev/null)" = "200" ]; then
    echo "[OK] HTTPS 已生效"
    echo "访问面板: https://${IP}:${PORT}"
    echo "（自签名证书，浏览器点【高级】→【继续访问】）"
  else
    echo "[FAIL] HTTPS 仍未生效，新错误："
    grep -iE 'error|证书|cert|tls' "$LOG" 2>/dev/null | tail -5
    journalctl -u kypanel -n 20 --no-pager 2>/dev/null | grep -iE 'error|失败|fail|cannot|panic|tls|cert' | tail -5
    echo ""
    echo "如果还是 HTTP，检查云服务器安全组是否放行 ${PORT}/tcp"
  fi
  exit 0
fi

INSTALL_DIR="/opt/kypanel"
SERVICE_NAME="kypanel"
ENV_FILE="${INSTALL_DIR}/panel.env"
SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
BIN_SRC=""
XDB_SRC=""
WEB_PKG=""
WEB_PKG_GIVEN=0
PORT="${PANEL_PORT:-}"
INSTALL_URL="${INSTALL_URL:-}"
XDB_URL="${XDB_URL:-}"
# 在线安装时同目录根 URL（不写死域名）：
#   1) 命令行 --base-url
#   2) 环境变量 INSTALL_BASE_URL
#   3) 都没填则从 INSTALL_URL（若已指定）反推同名目录
# 传入后会自动拼接 {BASE_URL}/kypanel_amd64 与 {BASE_URL}/ip2region.xdb。
INSTALL_BASE_URL="${INSTALL_BASE_URL:-}"
INSTALL_BIN_NAME="${INSTALL_BIN_NAME:-kypanel_amd64}"

# 根据 BASE_URL 推导各文件下载 URL（不写死域名）
derive_urls_from_base() {
  local base="$1"
  [ -z "$base" ] && return 0
  # 去掉尾部斜杠
  base="${base%/}"
  [ -z "$INSTALL_URL" ] && INSTALL_URL="${base}/${INSTALL_BIN_NAME}"
  [ -z "$XDB_URL" ]     && XDB_URL="${base}/ip2region.xdb"
}

parse_args() {
  while [ $# -gt 0 ]; do
    case "$1" in
      --url)   INSTALL_URL="$2"; shift 2 ;;
      --base-url) INSTALL_BASE_URL="$2"; shift 2 ;;
      --bin-name) INSTALL_BIN_NAME="$2"; shift 2 ;;
      --port)  PORT="$2"; shift 2 ;;
      --update)    MODE="update"; shift ;;
      --reinstall) MODE="reinstall"; shift ;;
      --uninstall) MODE="uninstall"; shift ;;
      --help|-h)
        echo "用法:"
        echo "  在线一键安装（已内置默认资源目录，零参数可用）:"
        echo "    curl -fsSL https://panel.apihot.cn/sh/i.sh | bash"
        echo "    curl -fsSL https://panel.apihot.cn/sh/i.sh | bash -s -- --reinstall"
        echo "  在线一键安装（自定义资源目录）:"
        echo "    curl -fsSL https://你的域名/sh/i.sh | bash -s -- --reinstall --base-url https://你的域名/sh"
        echo "    INSTALL_BASE_URL=https://你的域名/sh curl -fsSL https://你的域名/sh/i.sh | bash -s -- --reinstall"
        echo "  在线安装（直接给二进制完整 URL）:"
        echo "    bash install.sh --url https://你的域名/sh/kypanel_amd64 [--port 端口]"
        echo "  本地安装（二进制与脚本放同一目录）:"
        echo "    bash install.sh [/path/to/kypanel-二进制文件]"
        echo ""
        echo "  已安装面板时可选操作："
        echo "  --update     更新面板（保留数据/配置，仅升级程序）"
        echo "  --reinstall  重新安装（删除旧程序与数据，全新安装）"
        echo "  --uninstall  卸载面板（停止服务并删除全部文件）"
        echo ""
        echo "  --base-url URL  资源同目录 URL，自动拼接 kypanel_amd64 与 ip2region.xdb"
        echo "  --bin-name NAME 二进制文件名（默认 kypanel_amd64）"
        exit 0
        ;;
      *)
        # 位置参数：以 http(s):// 开头的当作下载 URL，否则当作本地二进制路径
        case "$1" in
          http://*|https://*)
            INSTALL_URL="$1" ;;
          *)
            if [ -z "$BIN_SRC" ]; then BIN_SRC="$1"; else WEB_PKG="$1"; WEB_PKG_GIVEN=1; fi
            ;;
        esac
        shift
        ;;
    esac
  done
}
parse_args "$@"

# 根据 base-url 推导 URL（仅在未手动指定时）
if [ -n "$INSTALL_BASE_URL" ]; then
  derive_urls_from_base "$INSTALL_BASE_URL"
fi

# 零参数 + 零环境变量时，使用默认在线资源目录（用户已同意写死）
# 可被下列任一方式覆盖：
#   - 命令行 --base-url URL
#   - 命令行 --url URL 或位置参数 URL
#   - 环境变量 INSTALL_BASE_URL / LP_URL / INSTALL_URL
DEFAULT_BASE_URL="${DEFAULT_BASE_URL:-https://panel.apihot.cn/sh}"
if [ -z "$INSTALL_URL" ] && [ -z "$INSTALL_BASE_URL" ] && [ -z "$BIN_SRC" ]; then
  INSTALL_BASE_URL="$DEFAULT_BASE_URL"
  derive_urls_from_base "$INSTALL_BASE_URL"
fi

ok()  { printf "  \033[32m✓\033[0m %s\n" "$*"; }
warn(){ printf "  \033[33m!\033[0m %s\n" "$*"; }
err() { printf "  \033[31m✗\033[0m %s\n" "$*"; }

if [ "$(id -u)" -ne 0 ]; then
  err "请以 root 用户执行安装"
  exit 1
fi

PKG_MGR=""
command -v apt-get >/dev/null 2>&1 && PKG_MGR="apt-get"
command -v dnf     >/dev/null 2>&1 && PKG_MGR="dnf"
command -v yum     >/dev/null 2>&1 && PKG_MGR="yum"
command -v zypper  >/dev/null 2>&1 && PKG_MGR="zypper"
command -v apk     >/dev/null 2>&1 && PKG_MGR="apk"
if command -v systemctl >/dev/null 2>&1; then INIT="systemd"
elif [ -d /etc/init.d ]; then INIT="sysv"
else INIT="unknown"; fi

command -v tar  >/dev/null 2>&1 || missing+=("tar")
command -v curl >/dev/null 2>&1 || missing+=("curl")
command -v od   >/dev/null 2>&1 || missing+=("coreutils")
if [ ${#missing[@]} -gt 0 ]; then
  case "$PKG_MGR" in
    apt-get) apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y "${missing[@]}" >/dev/null 2>&1 ;;
    dnf)     dnf install -y "${missing[@]}" >/dev/null 2>&1 ;;
    yum)     yum install -y "${missing[@]}" >/dev/null 2>&1 ;;
    zypper)  zypper install -y "${missing[@]}" >/dev/null 2>&1 ;;
    apk)     apk add --no-cache "${missing[@]}" >/dev/null 2>&1 ;;
  esac
fi

random_port() {
  local p n
  while :; do
    n=$(od -An -N2 -tu2 /dev/urandom 2>/dev/null | tr -d ' ')
    [ -z "$n" ] && n=$(( RANDOM + 1 ))
    p=$(( (n % 56648) + 8888 ))
    case "$p" in
      8888|8889|9000|9001|9090|9200|9300|8080|8081|80|81|443|3306|6379|5432|27017|11211|22|21|23|25) continue ;;
    esac
    if command -v ss >/dev/null 2>&1; then
      ss -tln 2>/dev/null | grep -qE "[:.]${p}\\b" && continue
    elif command -v netstat >/dev/null 2>&1; then
      netstat -tln 2>/dev/null | grep -qE "[:.]${p}\\b" && continue
    fi
    echo "$p"; return 0
  done
}

random_str() {
  local len=$1 out
  out=$(head -c 200 /dev/urandom 2>/dev/null | tr -dc 'a-zA-Z0-9' | head -c "$len")
  [ -z "$out" ] && out=$(printf '%s' "$RANDOM$RANDOM$RANDOM" | head -c "$len")
  echo -n "$out"
}

[ -z "$PORT" ] && PORT=$(random_port)

# 下载一个完整 URL 到 $1（不再做任何拼接；URL 必须含文件名）
download_to() {
  local out="$1" url="$2"
  if curl -fSL --connect-timeout 10 --progress-bar -o "$out" "$url"; then
    if [ -s "$out" ]; then return 0; fi
  fi
  rm -f "$out"
  return 1
}

# 解析离线 IP 库来源（IP 归属查询依赖 ip2region.xdb，约 11MB）：
#   1) 本地同目录 ip2region.xdb（本地安装/发布包解压后）
#   2) XDB_URL 环境变量指定的完整下载 URL
#   3) 在线安装时从 INSTALL_URL 同目录推导（如 https://域名/sh/ip2region.xdb）
# 成功时设置 XDB_SRC；失败仅 warn（不影响面板安装，IP 归属功能降级为不可用）。
resolve_xdb_src() {
  XDB_SRC=""
  if [ -n "$SELF_DIR" ] && [ -f "$SELF_DIR/ip2region.xdb" ]; then
    XDB_SRC="$SELF_DIR/ip2region.xdb"
    ok "使用本地离线 IP 库: ip2region.xdb"
    return 0
  fi
  local xurl=""
  [ -n "$XDB_URL" ] && xurl="$XDB_URL"
  [ -z "$xurl" ] && [ -n "$INSTALL_URL" ] && xurl="${INSTALL_URL%/*}/ip2region.xdb"
  if [ -n "$xurl" ]; then
    if download_to /tmp/lpxdb "$xurl"; then
      XDB_SRC=/tmp/lpxdb
      ok "离线 IP 库下载完成"
      return 0
    fi
    warn "离线 IP 库下载失败（不影响安装，IP 归属功能暂不可用）"
    return 1
  fi
  warn "未找到离线 IP 库（不影响安装，IP 归属功能暂不可用）"
  return 1
}

# 部署离线 IP 库到面板数据目录（幂等，仅当 XDB_SRC 非空时执行）
deploy_xdb() {
  if [ -z "${XDB_SRC:-}" ] || [ ! -f "$XDB_SRC" ]; then return 0; fi
  mkdir -p "${INSTALL_DIR}/data"
  cp "$XDB_SRC" "${INSTALL_DIR}/data/ip2region.xdb"
  rm -f /tmp/lpxdb
  ok "离线 IP 库已部署: ${INSTALL_DIR}/data/ip2region.xdb"
}

# 解析二进制来源：
#   1) 优先用 SELF_DIR 下的本地 kypanel_* 文件
#   2) INSTALL_URL 给出时直接 curl 该完整 URL（必须含文件名）
#   3) 都没有 → 交互询问完整 URL（管道模式经 /dev/tty，非交互直接报错）
# 成功时设置 BIN_SRC 并 chmod +x，返回 0。
resolve_bin_src() {
  BIN_SRC=""

  # 1) 本地同目录
  if [ -n "$SELF_DIR" ] && [ -d "$SELF_DIR" ]; then
    local f
    for f in "$SELF_DIR"/kypanel_*; do
      [ -f "$f" ] || continue
      BIN_SRC="$f"
      chmod +x "$BIN_SRC" 2>/dev/null || true
      ok "使用本地二进制: ${BIN_SRC##*/}"
      return 0
    done
  fi

  # 2) 在线：INSTALL_URL 必须是完整下载 URL（含文件名）
  if [ -n "$INSTALL_URL" ]; then
    if download_to /tmp/lpbin "$INSTALL_URL"; then
      chmod +x /tmp/lpbin
      BIN_SRC=/tmp/lpbin
      return 0
    fi
    err "下载失败: ${INSTALL_URL}"
    return 1
  fi

  # 3) 交互询问
  local input_url=""
  if [ -t 0 ]; then
    printf "未找到本地二进制，请输入完整下载 URL（含文件名，回车退出）：\n  如 https://你的域名/sh/kypanel_amd64\n> "
    read -r input_url || input_url=""
  elif [ -e /dev/tty ]; then
    printf "未找到本地二进制，请输入完整下载 URL（含文件名，回车退出）：\n  如 https://你的域名/sh/kypanel_amd64\n> " >/dev/tty
    read -r input_url </dev/tty || input_url=""
  fi
  if [ -n "$input_url" ]; then
    INSTALL_URL="$input_url"
    if download_to /tmp/lpbin "$INSTALL_URL"; then
      chmod +x /tmp/lpbin
      BIN_SRC=/tmp/lpbin
      return 0
    fi
    err "下载失败: ${INSTALL_URL}"
    return 1
  fi
  err "未找到面板二进制（本地同目录与在线源均不可用）"
  return 1
}

# ===== 已安装检测：更新 / 重新安装 / 卸载 =====
INSTALLED=0
[ -f "${INSTALL_DIR}/panel" ] && INSTALLED=1

confirm_yes() {
  local ans
  if [ -t 0 ]; then
    read -r -p "$1 [y/N] " ans
  elif [ "${CONFIRM:-}" = "yes" ]; then
    ans=y
  else
    ans=
  fi
  [ "$ans" = "y" ] || [ "$ans" = "Y" ]
}

do_update() {
  echo "========================================"
  echo " kypanel 正在更新..."
  echo "========================================"
  # 下载并覆盖二进制（新二进制已内置前端，web 包仅作可选覆盖）
  if ! resolve_bin_src; then
    err "更新失败：请用 --url 指定完整下载 URL（含文件名），或将二进制与 i.sh 放同一目录"
    exit 1
  fi

  # 先停止服务再替换二进制（运行中的二进制无法覆盖：Text file busy）
  if command -v systemctl >/dev/null 2>&1; then
    systemctl stop kypanel 2>/dev/null || true
  else
    /etc/init.d/kypanel stop 2>/dev/null || true
  fi
  sleep 1
  cp "${BIN_SRC}" "${INSTALL_DIR}/panel" || { sleep 2; cp "${BIN_SRC}" "${INSTALL_DIR}/panel" || true; }
  chmod +x "${INSTALL_DIR}/panel"
  ln -sf "${INSTALL_DIR}/panel" /usr/local/bin/ky
  ok "二进制已更新"

  # 更新前端：本地同目录有 web 包时覆盖（磁盘 web 优先于内置前端，不覆盖则新前端不生效）
  if [ -z "${WEB_PKG}" ] || [ ! -f "${WEB_PKG}" ]; then
    WEB_PKG=""
    for cand in "panel-web.tar.gz" "web.tar.gz"; do
      if [ -f "${SELF_DIR}/${cand}" ]; then WEB_PKG="${SELF_DIR}/${cand}"; break; fi
    done
  fi
  if [ -n "${WEB_PKG}" ] && [ -f "${WEB_PKG}" ]; then
    mkdir -p "${INSTALL_DIR}/web"
    rm -rf "${INSTALL_DIR}/web/assets"
    tar -xzf "${WEB_PKG}" -C "${INSTALL_DIR}/web/"
    ok "前端已更新（覆盖内置前端）"
  else
    ok "前端已内置在新二进制中"
  fi

  # 更新离线 IP 库（本地同目录优先，在线从安装源同目录推导）
  if [ -n "$SELF_DIR" ] && [ -f "$SELF_DIR/ip2region.xdb" ]; then
    cp "$SELF_DIR/ip2region.xdb" "${INSTALL_DIR}/data/ip2region.xdb"
    ok "离线 IP 库已更新"
  elif [ -n "${XDB_URL:-}" ] || [ -n "$INSTALL_URL" ]; then
    local xurl="${XDB_URL:-}"
    [ -z "$xurl" ] && xurl="${INSTALL_URL%/*}/ip2region.xdb"
    if download_to /tmp/lpxdb "$xurl"; then
      cp /tmp/lpxdb "${INSTALL_DIR}/data/ip2region.xdb" && rm -f /tmp/lpxdb
      ok "离线 IP 库已更新"
    else
      warn "离线 IP 库下载失败（保留现有库）"
    fi
  fi

  # 重启服务
  if command -v systemctl >/dev/null 2>&1; then
    systemctl start kypanel 2>/dev/null || systemctl restart kypanel 2>/dev/null || warn "启动失败，请手动执行 systemctl start kypanel"
  else
    /etc/init.d/kypanel start 2>/dev/null || warn "请手动启动服务"
  fi
  sleep 2

  # 输出当前访问信息（保留原有配置）
  UP_PORT=$(sed -n 's/.*"port"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p' "${INSTALL_DIR}/config.json" 2>/dev/null | head -1)
  UP_HTTPS=$(sed -n 's/.*"https"[[:space:]]*:[[:space:]]*\(true\|false\).*/\1/p' "${INSTALL_DIR}/config.json" 2>/dev/null | head -1)
  UP_ENTRANCE=$(grep -oE '"security_entrance"[[:space:]]*:[[:space:]]*"[^"]+"' "${INSTALL_DIR}/config.json" 2>/dev/null | head -1 | sed -E 's/.*"([A-Za-z0-9]+)".*/\1/')
  [ -z "$UP_PORT" ] && UP_PORT="${PORT}"
  UP_IP=$(curl -fsSL --max-time 5 https://api.ipify.org 2>/dev/null || curl -fsSL --max-time 5 https://ifconfig.me/ip 2>/dev/null || hostname -I 2>/dev/null | awk '{print $1}')
  UP_USER=$(sed -n 's/^PANEL_ADMIN_USER=//p' "${ENV_FILE}" 2>/dev/null | head -1)

  printf "\n\033[1;32m========================================\n ✓ 更新完成！\n========================================\033[0m\n\n"
  if [ "$UP_HTTPS" = "true" ]; then
    if [ -n "$UP_ENTRANCE" ]; then
      echo "  访问面板    https://${UP_IP}:${UP_PORT}/${UP_ENTRANCE}/"
    else
      echo "  访问面板    https://${UP_IP}:${UP_PORT}"
    fi
    echo "  (自签名证书，浏览器提示不安全属正常，点【高级】→【继续访问】即可)"
  else
    if [ -n "$UP_ENTRANCE" ]; then
      echo "  访问面板    http://${UP_IP}:${UP_PORT}/${UP_ENTRANCE}/"
    else
      echo "  访问面板    http://${UP_IP}:${UP_PORT}"
    fi
  fi
  [ -n "$UP_USER" ] && echo "  账    号    ${UP_USER}"
  echo "  (密码已加密存储，忘记请用 ky 命令重置)"
  echo ""
  echo "  ----------------------------------------"
  echo "  命令行菜单  输入 ky"
  echo "========================================"
}

do_uninstall() {
  echo "========================================"
  echo " kypanel 正在卸载..."
  echo "========================================"
  if ! confirm_yes "确认卸载？将删除 ${INSTALL_DIR} 全部数据"; then
    echo "已取消"
    exit 0
  fi
  systemctl stop kypanel 2>/dev/null || true
  systemctl disable kypanel 2>/dev/null || true
  rm -f /etc/systemd/system/kypanel.service
  /etc/init.d/kypanel stop 2>/dev/null || true
  rm -f /usr/local/bin/ky
  rm -rf "${INSTALL_DIR}"
  systemctl daemon-reload 2>/dev/null || true
  ok "卸载完成"
}

if [ "$INSTALLED" = "1" ]; then
  if [ -z "${MODE:-}" ]; then
    # 无参数且已安装 → 交互菜单
    if [ -t 0 ]; then
      echo "检测到面板已安装：${INSTALL_DIR}"
      echo ""
      echo "  1) 更新面板（保留数据/配置，仅升级程序）"
      echo "  2) 重新安装（删除旧程序与数据，全新安装）"
      echo "  3) 卸载面板（停止服务并删除全部文件）"
      echo "  4) 退出"
      echo ""
      read -r -p "请选择 [1-4]： " CHOICE
      case "$CHOICE" in
        1) do_update; exit 0 ;;
        2) rm -rf "${INSTALL_DIR}"; echo "旧程序已删除，开始全新安装";;
        3) do_uninstall; exit 0 ;;
        *) echo "已取消"; exit 0 ;;
      esac
    else
      echo "检测到面板已安装：${INSTALL_DIR}"
      echo "请使用参数指定操作："
      echo "  更新面板      bash <(curl -fsSL .../i.sh) -- --update"
      echo "  重新安装      bash <(curl -fsSL .../i.sh) -- --reinstall"
      echo "  卸载面板      bash <(curl -fsSL .../i.sh) -- --uninstall"
      exit 0
    fi
  else
    case "${MODE}" in
      update)     do_update; exit 0 ;;
      uninstall)  do_uninstall; exit 0 ;;
      reinstall)
        echo "将删除 ${INSTALL_DIR} 全部数据并重新安装"
        if confirm_yes "确认重新安装？"; then
          rm -rf "${INSTALL_DIR}"
          echo "旧程序已删除，开始全新安装"
        else
          echo "已取消"
          exit 0
        fi
        ;;
      *) echo "未知操作: ${MODE}"; exit 1 ;;
    esac
  fi
fi

echo "========================================"
echo " kypanel 正在安装..."
echo "========================================"

mkdir -p "${INSTALL_DIR}" "${INSTALL_DIR}/logs" "${INSTALL_DIR}/web" "${INSTALL_DIR}/data"

echo ""
echo "[1/4] 下载面板（前后端单文件，前端已内置）"
if ! resolve_bin_src; then
  err "安装失败"
  err "  - 在线安装: LP_URL=https://你的域名/sh/kypanel_amd64 curl -fsSL https://你的域名/sh/i.sh | bash"
  err "  - 本地安装: 将 kypanel_amd64 与 i.sh 放同一目录后执行 bash i.sh"
  exit 1
fi
if [ -n "$INSTALL_URL" ]; then
  ok "下载完成"
else
  ok "本地文件: ${BIN_SRC##*/}"
fi
cp "${BIN_SRC}" "${INSTALL_DIR}/panel"
chmod +x "${INSTALL_DIR}/panel"
ln -sf "${INSTALL_DIR}/panel" /usr/local/bin/ky
ok "已安装"

echo ""
echo "[2/4] 部署前端（二进制已内置前端，本地有 web 包时覆盖）"
# 仅在本地模式（无 INSTALL_URL）下探测同目录的 web 包
if [ "${WEB_PKG_GIVEN}" = "0" ] && [ -z "$INSTALL_URL" ] && [ -z "${WEB_PKG}" ]; then
  for cand in "panel-web.tar.gz" "web.tar.gz"; do
    if [ -f "${SELF_DIR}/${cand}" ]; then WEB_PKG="${SELF_DIR}/${cand}"; break; fi
  done
fi
if [ -n "${WEB_PKG}" ] && [ -f "${WEB_PKG}" ]; then
  rm -rf "${INSTALL_DIR}/web/assets"
  tar -xzf "${WEB_PKG}" -C "${INSTALL_DIR}/web/"
  ok "前端已部署（覆盖内置前端）"
else
  ok "使用二进制内置前端"
fi

echo ""
echo "[2.5/4] 部署离线 IP 库（IP 归属/按地区拉黑）"
resolve_xdb_src || true
deploy_xdb

echo ""
echo "[3/4] 配置服务"
ADMIN_USER="$(random_str 10)"
ADMIN_PASS="$(random_str 16)"

PUBLIC_IP=""
if command -v curl >/dev/null 2>&1; then
  PUBLIC_IP=$(curl -fsSL --max-time 5 https://api.ipify.org 2>/dev/null || true)
  [ -z "$PUBLIC_IP" ] && PUBLIC_IP=$(curl -fsSL --max-time 5 https://ifconfig.me/ip 2>/dev/null || true)
fi
[ -z "$PUBLIC_IP" ] && PUBLIC_IP=$(hostname -I 2>/dev/null | awk '{print $1}')
[ -z "$PUBLIC_IP" ] && PUBLIC_IP="localhost"

# HTTPS: 生成自签名证书（浏览器会提示不受信任，属正常）
if ! command -v openssl >/dev/null 2>&1; then
  case "$PKG_MGR" in
    apt-get) DEBIAN_FRONTEND=noninteractive apt-get install -y openssl >/dev/null 2>&1 || true ;;
    dnf)     dnf install -y openssl >/dev/null 2>&1 || true ;;
    yum)     yum install -y openssl >/dev/null 2>&1 || true ;;
    zypper)  zypper install -y openssl >/dev/null 2>&1 || true ;;
    apk)     apk add --no-cache openssl >/dev/null 2>&1 || true ;;
  esac
fi
CERT_DIR="${INSTALL_DIR}/certs"
CERT_CN="$(echo "${PUBLIC_IP}" | tr -c 'a-zA-Z0-9.-' '_')"
[ -z "$CERT_CN" ] && CERT_CN="localhost"
HTTPS_ON=0
if command -v openssl >/dev/null 2>&1; then
  mkdir -p "${CERT_DIR}"
  ERR=$(openssl req -x509 -newkey rsa:2048 -keyout "${CERT_DIR}/panel.key" \
        -out "${CERT_DIR}/panel.crt" -days 3650 -nodes \
        -subj "/C=CN/ST=Guangdong/L=Shenzhen/O=kypanel/CN=${CERT_CN}" 2>&1 >/dev/null)
  if [ $? -eq 0 ]; then
    chmod 600 "${CERT_DIR}/panel.key"
    HTTPS_ON=1
  else
    warn "自签名证书生成失败，本次使用 HTTP"
    warn "openssl 错误：$(echo "$ERR" | head -3 | tr '\n' ' ')"
  fi
else
  warn "openssl 不可用，本次使用 HTTP"
fi

HTTPS_BOOL=false
[ "$HTTPS_ON" = "1" ] && HTTPS_BOOL=true
cat > "${INSTALL_DIR}/config.json" <<EOF
{
  "server": {
    "port": ${PORT},
    "https": ${HTTPS_BOOL},
    "cert_file": "${CERT_DIR}/panel.crt",
    "key_file": "${CERT_DIR}/panel.key",
    "firewall_default_drop": true
  },
  "db": { "path": "${INSTALL_DIR}/data/panel.db" },
  "auth": { "token_hour": 24 },
  "log": {
    "level": "info",
    "file": "${INSTALL_DIR}/logs/panel.log",
    "max_day": 30
  },
  "data_dir": "${INSTALL_DIR}"
}
EOF

cat > "${ENV_FILE}" <<EOF
PANEL_ADMIN_USER=${ADMIN_USER}
PANEL_ADMIN_PASS=${ADMIN_PASS}
EOF
chmod 600 "${ENV_FILE}"

if [ "$INIT" = "systemd" ]; then
  cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<EOF
[Unit]
Description=kypanel - Server Management Panel
After=network.target

[Service]
Type=simple
WorkingDirectory=${INSTALL_DIR}
EnvironmentFile=${ENV_FILE}
ExecStart=${INSTALL_DIR}/panel -config ${INSTALL_DIR}/config.json
Restart=on-failure
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload >/dev/null 2>&1
  systemctl enable "${SERVICE_NAME}" >/dev/null 2>&1 || true
  systemctl restart "${SERVICE_NAME}" || warn "首次启动失败，将在最后重试"
elif [ "$INIT" = "sysv" ]; then
  cat > "/etc/init.d/${SERVICE_NAME}" <<EOF
#!/bin/sh
### BEGIN INIT INFO
# Provides:          ${SERVICE_NAME}
# Required-Start:    \$network
# Required-Stop:     \$network
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
# Short-Description: kypanel server panel
# Description:       kypanel - Server Management Panel
### END INIT INFO

PIDFILE=/var/run/${SERVICE_NAME}.pid
DAEMON=${INSTALL_DIR}/panel
ENVFILE=${ENV_FILE}
CONFIG=${INSTALL_DIR}/config.json

case "\$1" in
  start)
    [ -f "\$ENVFILE" ] && . "\$ENVFILE"
    start-stop-daemon --start --background --make-pidfile --pidfile \$PIDFILE \
      --exec \$DAEMON -- -config \$CONFIG
    echo "${SERVICE_NAME} started"
    ;;
  stop)
    start-stop-daemon --stop --pidfile \$PIDFILE --retry 5
    rm -f \$PIDFILE
    echo "${SERVICE_NAME} stopped"
    ;;
  restart)
    \$0 stop; sleep 1; \$0 start
    ;;
  status)
    if [ -f \$PIDFILE ] && kill -0 "\$(cat \$PIDFILE 2>/dev/null)" 2>/dev/null; then
      echo "${SERVICE_NAME} running (pid \$(cat \$PIDFILE))"
    else
      echo "${SERVICE_NAME} not running"
    fi
    ;;
  *)
    echo "Usage: \$0 {start|stop|restart|status}"
    exit 1
    ;;
esac
EOF
  chmod +x "/etc/init.d/${SERVICE_NAME}"
  if command -v update-rc.d >/dev/null 2>&1; then
    update-rc.d "${SERVICE_NAME}" defaults >/dev/null 2>&1 || true
  elif command -v chkconfig >/dev/null 2>&1; then
    chkconfig --add "${SERVICE_NAME}" >/dev/null 2>&1 || true
    chkconfig "${SERVICE_NAME}" on >/dev/null 2>&1 || true
  fi
  /etc/init.d/${SERVICE_NAME} restart || warn "首次启动失败，将在最后重试"
else
  warn "未检测到 init 系统，请手动启动: ${INSTALL_DIR}/panel -config ${INSTALL_DIR}/config.json"
fi

# 默认仅放行 SSH(22)/HTTP(80)/HTTPS(443)/面板端口，其余端口一律拒绝（IPv4+IPv6）
# 后续需要开放其他端口，请在面板「系统 → 防火墙」中添加入站放行规则
OPEN_PORTS=( 22 80 443 "${PORT}" )

FW_TYPE="无"
if command -v firewall-cmd >/dev/null 2>&1 && [ "$(firewall-cmd --state 2>/dev/null)" = "running" ]; then
  FW_TYPE="firewalld"
elif command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q "Status: active"; then
  FW_TYPE="ufw"
elif command -v iptables >/dev/null 2>&1; then
  FW_TYPE="iptables"
fi
: # 服务已配置 — 静默
: # 防火墙: ${FW_TYPE} — 静默

echo ""
echo "[4/4] 启动服务"
sleep 1
service_ok=0
for i in 1 2 3 4 5 6 7 8; do
  if [ "$INIT" = "systemd" ]; then
    systemctl is-active --quiet "${SERVICE_NAME}" && service_ok=1 && break
  elif [ "$INIT" = "sysv" ]; then
    /etc/init.d/${SERVICE_NAME} status 2>/dev/null | grep -q running && service_ok=1 && break
  fi
  sleep 1
done

if [ "$service_ok" = "0" ] && [ "$INIT" = "systemd" ]; then
  systemctl restart "${SERVICE_NAME}" 2>/dev/null || true
  sleep 3
  # 判断服务真的起来：既不是崩溃重启循环，且端口确实在监听
  service_ok=0
  if systemctl is-active --quiet "${SERVICE_NAME}" 2>/dev/null; then
    if ss -tln 2>/dev/null | grep -q ":${PORT} "; then
      service_ok=1
    fi
  fi
fi

if [ "$FW_TYPE" != "无" ]; then
  # firewalld/ufw 本身即白名单（默认拒绝未放行的入站流量），IPv4+IPv6 自动覆盖
  FW_ZONE=""
  [ "$FW_TYPE" = "firewalld" ] && FW_ZONE="$(firewall-cmd --get-default-zone 2>/dev/null || echo public)"
  for P in "${OPEN_PORTS[@]}"; do
    case "$FW_TYPE" in
      firewalld)
        firewall-cmd --permanent --zone="${FW_ZONE}" --add-port="${P}/tcp" >/dev/null 2>&1 || true
        ;;
      ufw)
        ufw allow "${P}/tcp" >/dev/null 2>&1 || true
        ;;
      iptables)
        # 面板启动时已开启默认拒绝（INPUT policy DROP），此处放行作为双保险
        iptables -C INPUT -p tcp --dport "${P}" -j ACCEPT 2>/dev/null || \
          iptables -A INPUT -p tcp --dport "${P}" -j ACCEPT >/dev/null 2>&1 || true
        ;;
    esac
  done
  [ "$FW_TYPE" = "firewalld" ] && firewall-cmd --reload >/dev/null 2>&1 || true
fi

if [ "$service_ok" = "1" ]; then
  ok "服务运行中"

  # HTTPS 与否直接以安装配置为准（是否已生成并配置证书），不做网络探测：
  # 面板端口是随机生成的，安装完成输出前安全组可能未放行该端口，
  # 用 curl 状态码探测会误报"以 HTTP 运行"。
  ACTUAL_HTTP="https"
  [ "$HTTPS_BOOL" != "true" ] && ACTUAL_HTTP="http"

  printf "\n\033[1;32m========================================\n ✓ 安装成功！\n========================================\033[0m\n\n"
  # 读取安全入口（启动日志会打印并保存到 config.json）
  ENTRANCE=""
  if [ -f "${INSTALL_DIR}/config.json" ]; then
    ENTRANCE=$(grep -oE '"security_entrance"[[:space:]]*:[[:space:]]*"[^"]+"' "${INSTALL_DIR}/config.json" 2>/dev/null | head -1 | sed -E 's/.*"([A-Za-z0-9]+)".*/\1/')
  fi
  # 兜底：日志里再抓一次
  if [ -z "$ENTRANCE" ] && [ -f "${INSTALL_DIR}/logs/panel.log" ]; then
    ENTRANCE=$(grep -oE '/[A-Za-z0-9]{6}/' "${INSTALL_DIR}/logs/panel.log" 2>/dev/null | head -1 | tr -d '/')
  fi

  echo ""
  if [ "$ACTUAL_HTTP" = "https" ]; then
    if [ -n "$ENTRANCE" ]; then
      echo "  访问面板    https://${PUBLIC_IP}:${PORT}/${ENTRANCE}/"
    else
      echo "  访问面板    https://${PUBLIC_IP}:${PORT}"
    fi
    echo "  (自签名证书，浏览器提示不安全属正常，点【高级】→【继续访问】即可)"
  else
    if [ -n "$ENTRANCE" ]; then
      echo "  访问面板    http://${PUBLIC_IP}:${PORT}/${ENTRANCE}/"
    else
      echo "  访问面板    http://${PUBLIC_IP}:${PORT}"
    fi
    echo "  https 访问  https://${PUBLIC_IP}:${PORT}"
    warn "面板当前以 HTTP 运行（HTTPS 证书未生效）"
    if [ -f "${INSTALL_DIR}/logs/panel.log" ]; then
      ERR_LINES=$(grep -iE 'ERROR|HTTPS|证书|cert|tls' "${INSTALL_DIR}/logs/panel.log" 2>/dev/null | tail -5)
      if [ -n "$ERR_LINES" ]; then
        echo ""
        echo "  ── 错误日志 ──"
        echo "$ERR_LINES" | sed 's/^/  /'
        echo "  ──────────────"
        echo ""
        echo "  一键修复（重生成证书并重启）："
        if [ -n "${INSTALL_URL}" ]; then
          echo "    bash <(curl -fsSL ${INSTALL_URL%/*}/i.sh)"
        else
          echo "    bash ${SELF_DIR}/i.sh"
        fi
      fi
    fi
  fi
  echo ""
  echo "  账    号    ${ADMIN_USER}"
  echo "  密    码    ${ADMIN_PASS}  (仅此一次显示，密码已加密存储，忘记请用 ky 重置)"
  echo ""
  echo "  ----------------------------------------"
  echo "  打不开时先检查云服务器安全组："
  echo "  ${PORT}/tcp 是否已放行（腾讯云/阿里云控制台 → 安全组）"
  echo "  ----------------------------------------"
  echo "  命令行菜单  输入 ky"
  echo "========================================"
else
  printf "\n\033[1;31m========================================\n ✗ 安装失败（服务未起来）\n========================================\033[0m\n\n"
  echo "  ── 错误日志 ──"
  journalctl -u "${SERVICE_NAME}" -n 25 --no-pager 2>/dev/null | grep -iE 'error|失败|fail|cannot|panic|tls|cert' | tail -6 | sed 's/^/  /'
  echo "  ──────────────"
  echo ""
  echo "  排查命令："
  echo "  systemctl status kypanel --no-pager"
  echo "  journalctl -u kypanel -n 80 --no-pager"
  echo "  ss -tlnp | grep ${PORT}"
  echo ""
  echo "  端口: ${PORT}  账号: ${ADMIN_USER}  密码: ${ADMIN_PASS}"
  exit 1
fi