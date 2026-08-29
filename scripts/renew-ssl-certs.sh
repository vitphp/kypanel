#!/bin/bash
# ============================================================
# kypanel 自动续签 HTTPS 证书
#
# 供面板「计划任务」（Shell 脚本）每天调用一次：
#   扫描面板管理的所有 ACME 证书，剩余天数 ≤ RENEW_SSL_DAYS
#   （默认 2，含已过期）的证书自动续签并重新部署，防止 HTTPS
#   证书到期后网站打不开。
#
# 用法：
#   bash /opt/kypanel/scripts/renew-ssl-certs.sh
#
# 可选环境变量：
#   KYPANEL_BIN     面板二进制路径（默认 /opt/kypanel/panel）
#   KYPANEL_CONFIG  面板配置文件路径（默认 /opt/kypanel/config.json）
#   RENEW_SSL_DAYS  续签阈值天数（默认 2）
# ============================================================
set -u

PANEL_BIN="${KYPANEL_BIN:-/opt/kypanel/panel}"
PANEL_CONFIG="${KYPANEL_CONFIG:-/opt/kypanel/config.json}"
DAYS="${RENEW_SSL_DAYS:-2}"

# 二进制不存在时尝试软链接 /usr/local/bin/lp
if [ ! -x "${PANEL_BIN}" ]; then
  if command -v lp >/dev/null 2>&1; then
    PANEL_BIN="$(command -v lp)"
  else
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] 错误: 找不到面板二进制: ${PANEL_BIN}"
    exit 1
  fi
fi

if [ ! -f "${PANEL_CONFIG}" ]; then
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] 错误: 找不到面板配置文件: ${PANEL_CONFIG}"
  exit 1
fi

LOG_DIR="$(dirname "${PANEL_CONFIG}")/logs"
mkdir -p "${LOG_DIR}" 2>/dev/null
LOG_FILE="${LOG_DIR}/renew-ssl.log"

log() { echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*" >> "${LOG_FILE}"; }

log "======== 自动续签 HTTPS 证书（剩余 ≤ ${DAYS} 天）开始 ========"

# 执行续签：输出同时打印到终端（cron 日志）并追加到日志文件
"${PANEL_BIN}" -renew-ssl -renew-days "${DAYS}" -config "${PANEL_CONFIG}" 2>&1 | tee -a "${LOG_FILE}"
EC=${PIPESTATUS[0]}

log "======== 续签结束，退出码 ${EC} ========"
exit "${EC}"
