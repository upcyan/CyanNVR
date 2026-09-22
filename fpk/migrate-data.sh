#!/bin/bash
# CyanNVR 数据迁移脚本：把旧 Docker 部署的数据迁移到飞牛应用数据目录
#
# 用法（需 root）：
#   sudo ./migrate-data.sh              # 自动识别旧目录
#   sudo ./migrate-data.sh /path/to/old # 指定旧数据目录
#
# 迁移内容：nvr.db（设备/事件索引）、settings.json、jwt_secret、
#           recordings/、snapshots/、events/、models/
#
# 安全策略：
#   - 先停掉旧 Docker 容器与新应用，避免数据库写入竞争
#   - 目标已存在 nvr.db 时默认不覆盖，除非加 --force
#   - 迁移完成后校验文件大小并打印结果

set -e

SRC_DEFAULT="/vol1/1000/NasAppFiles/simpleNVR"
DST="/vol1/@appdata/CyanNVR"
# 若应用配置了自定义数据目录，则以 datadir.conf 为准
if [ -f "/var/apps/CyanNVR/etc/datadir.conf" ]; then
    CFG_DIR=$(head -n 1 "/var/apps/CyanNVR/etc/datadir.conf" | tr -d '[:space:]')
    [ -n "$CFG_DIR" ] && DST="$CFG_DIR"
fi
FORCE=0
SRC=""

for arg in "$@"; do
    case "$arg" in
        --force) FORCE=1 ;;
        -*) echo "未知参数: $arg"; exit 1 ;;
        *) SRC="$arg" ;;
    esac
done

[ -z "$SRC" ] && SRC="$SRC_DEFAULT"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
info()  { echo -e "${GREEN}[INFO]${NC} $1"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

[ "$(id -u)" = "0" ] || error "需要 root 权限运行（sudo $0）"

echo "=========================================="
echo "  CyanNVR 数据迁移"
echo "=========================================="
echo "源目录  : $SRC"
echo "目标目录: $DST"
echo ""

# ── 前置检查 ──
[ -d "$SRC" ] || error "源目录不存在: $SRC"
[ -f "$SRC/nvr.db" ] || error "源目录缺少 nvr.db，可能不是 CyanNVR 数据目录: $SRC"

if [ -f "$DST/nvr.db" ] && [ "$FORCE" != "1" ]; then
    error "目标已存在 nvr.db。如需覆盖请加 --force（建议先备份 $DST）"
fi

# ── 停止相关进程 ──
info "停止旧 Docker 容器（若在运行）..."
docker stop cyannvr simplenvr 2>/dev/null || true

info "停止飞牛应用（若在运行）..."
if [ -x "/var/apps/CyanNVR/cmd/main" ]; then
    /var/apps/CyanNVR/cmd/main stop 2>/dev/null || true
fi

# ── 备份目标 ──
if [ -d "$DST" ] && [ -n "$(ls -A "$DST" 2>/dev/null)" ]; then
    BACKUP="${DST}.bak-$(date +%Y%m%d-%H%M%S)"
    info "备份现有目标数据到 $BACKUP"
    cp -a "$DST" "$BACKUP"
fi

mkdir -p "$DST"

# ── 迁移 ──
info "复制数据文件..."
for item in nvr.db nvr.db-wal nvr.db-shm settings.json jwt_secret; do
    if [ -e "$SRC/$item" ]; then
        cp -a "$SRC/$item" "$DST/"
        echo "  ✓ $item"
    fi
done

for dir in recordings snapshots events models; do
    if [ -d "$SRC/$dir" ]; then
        size=$(du -sh "$SRC/$dir" 2>/dev/null | cut -f1)
        info "  复制 $dir/ ($size) ..."
        mkdir -p "$DST/$dir"
        cp -a "$SRC/$dir/." "$DST/$dir/"
    fi
done

# ── 权限修正 ──
# 飞牛应用以专用用户运行，数据目录需归属该用户
APP_USER=""
if [ -f "/var/apps/CyanNVR/manifest" ]; then
    APP_USER=$(id -u CyanNVR 2>/dev/null && echo "CyanNVR")
fi
if [ -n "$APP_USER" ]; then
    info "修正目录属主为 $APP_USER ..."
    chown -R "$APP_USER:$APP_USER" "$DST"
else
    warn "未找到应用用户 CyanNVR（应用可能尚未安装）。"
    warn "请安装应用后重新执行本脚本，或手动执行： chown -R CyanNVR:CyanNVR $DST"
fi
chmod -R 755 "$DST" 2>/dev/null || true

# ── 校验 ──
echo ""
info "校验迁移结果..."
if [ -f "$DST/nvr.db" ]; then
    src_size=$(stat -c%s "$SRC/nvr.db" 2>/dev/null)
    dst_size=$(stat -c%s "$DST/nvr.db" 2>/dev/null)
    echo "  nvr.db: 源 ${src_size} 字节 → 目标 ${dst_size} 字节"
    [ "$src_size" = "$dst_size" ] || warn "大小不一致，请检查！"
fi
for dir in recordings snapshots events; do
    if [ -d "$DST/$dir" ]; then
        n=$(find "$DST/$dir" -type f 2>/dev/null | wc -l)
        echo "  $dir/: $n 个文件"
    fi
done

echo ""
info "=========================================="
info "  迁移完成"
info "=========================================="
echo ""
echo "下一步："
echo "  1. 在飞牛应用中心启动 CyanNVR（或点击桌面图标）"
echo "  2. 用原账号密码登录，设备与录像应已就绪"
echo ""
echo "旧数据仍保留在 $SRC，确认无误后可自行删除。"
