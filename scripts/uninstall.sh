#!/usr/bin/env bash
# kypanel 卸载脚本（保留数据目录，仅停止服务并删除二进制/服务文件）
set -e

SERVICE_NAME="kypanel"
INSTALL_DIR="/opt/kypanel"

if [ "$(id -u)" -ne 0 ]; then
  echo "[错误] 请以 root 用户执行卸载"
  exit 1
fi

echo "[1/3] 停止并禁用服务"
systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
systemctl disable "${SERVICE_NAME}" 2>/dev/null || true

echo "[2/3] 删除 systemd 服务文件"
rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
systemctl daemon-reload

echo "[3/3] 删除程序文件（保留 ${INSTALL_DIR}/data 数据）"
rm -f "${INSTALL_DIR}/panel" "${INSTALL_DIR}/config.json"

echo "卸载完成。面板数据保留在 ${INSTALL_DIR}/data，如需彻底删除请手动执行: rm -rf ${INSTALL_DIR}"
