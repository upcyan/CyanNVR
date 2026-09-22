#!/bin/bash
# CyanNVR 飞牛 FPK 版 · 密码重置辅助脚本
#
# 用途：忘记密码时，在 NAS 上直接重置管理员密码。
# 需要 root（sudo）权限，因为飞牛应用数据目录归应用用户所有。
#
# 用法：
#   sudo ./reset-password.sh                       # 重置唯一管理员，交互式输入新密码
#   sudo ./reset-password.sh --user admin          # 指定用户
#   sudo ./reset-password.sh --list                # 列出所有用户
#
# 说明：本脚本只是把正确的 NVR_DATA 传给应用自带的 CLI，
#       真正的重置逻辑在 /var/apps/CyanNVR/target/cyannvr reset-password。

set -e

APP_NAME="CyanNVR"
APP_DIR="/var/apps/${APP_NAME}"
BIN="${APP_DIR}/target/cyannvr"
DATA_DIR="/vol1/@appdata/${APP_NAME}"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
info() { echo -e "${GREEN}[INFO]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

[ "$(id -u)" = "0" ] || error "需要 root 权限：sudo $0 $*"
[ -x "$BIN" ] || error "找不到应用可执行文件：$BIN（应用是否已安装？）"

# 数据目录：优先用应用保存的自定义目录（datadir.conf）
if [ -f "${APP_DIR}/etc/datadir.conf" ]; then
    CFG_DIR=$(head -n 1 "${APP_DIR}/etc/datadir.conf" | tr -d '[:space:]')
    [ -n "$CFG_DIR" ] && DATA_DIR="$CFG_DIR"
fi

# 否则跟随飞牛默认的 TRIM_PKGVAR 软链
if [ "${DATA_DIR}" = "/vol1/@appdata/${APP_NAME}" ]; then
    # 飞牛把 TRIM_PKGVAR 软链到 @appdata，优先跟随软链
    if [ -L "${APP_DIR}/var" ]; then
        REAL_VAR=$(readlink -f "${APP_DIR}/var")
        [ -d "$REAL_VAR" ] && DATA_DIR="$REAL_VAR"
    fi
fi

[ -d "$DATA_DIR" ] || error "找不到数据目录：$DATA_DIR"
[ -f "${DATA_DIR}/nvr.db" ] || error "数据目录中缺少 nvr.db：$DATA_DIR"

info "应用目录: $APP_DIR"
info "数据目录: $DATA_DIR"
echo ""

APP_USER=$(stat -c '%U' "$DATA_DIR" 2>/dev/null || echo root)

# 以应用用户身份运行，避免在数据目录里生成 root 所有的文件
if [ "$APP_USER" != "root" ] && id "$APP_USER" >/dev/null 2>&1; then
    info "以应用用户 $APP_USER 身份执行..."
    exec runuser -u "$APP_USER" -- env NVR_DATA="$DATA_DIR" "$BIN" "$@"
else
    warn "未能识别应用用户，改为直接执行"
    exec env NVR_DATA="$DATA_DIR" "$BIN" "$@"
fi
