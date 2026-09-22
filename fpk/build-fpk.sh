#!/bin/bash
set -e

# CyanNVR fnOS FPK 构建脚本
#
# 使用官方 fnpack 工具打包，保证 fpk 格式与飞牛应用中心一致。
# fnpack 会把 app/ 目录内容打成 app.tgz，并与 manifest / ICON / cmd / config / wizard
# 一同组成 fpk；自行手写 tar 很容易漏字段导致「应用包数据不完整」。
#
# 项目结构（与 fnpack create 生成的模板一致）：
#   fpk/
#   ├── manifest          应用元数据
#   ├── ICON.PNG          512x512
#   ├── ICON_256.PNG      256x256
#   ├── app/              应用运行文件（打包进 app.tgz）
#   │   ├── cyannvr       Go 二进制      ← 构建时生成
#   │   ├── dist/         前端静态文件    ← 构建时生成
#   │   ├── ai_detect.py  AI 检测脚本
#   │   ├── models/       预置模型
#   │   └── ui/           桌面入口（desktop_uidir=ui）
#   ├── cmd/              9 个生命周期脚本
#   ├── config/           privilege + resource
#   └── wizard/           安装向导

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

APPNAME="CyanNVR"
VERSION_ENV="$SCRIPT_DIR/version.env"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'
info() { echo -e "${GREEN}[INFO]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

# ── 版本解析 ──
# 版本分离：核心版本 x.y.z 来自 server/version.go，
# FPK 修订号 r 由本脚本自动 +1，最终 fpk 版本为 x.y.z-r
# （fnOS 只接受 x.y.z[-r]，四段式 x.y.z.r 会被拒绝）。
#
# 参数：
#   --revision N   指定修订号（默认自动 +1）
#   --no-bump      沿用当前修订号，不递增（用于重出同一个包）
BUMP=1
FORCE_REV=""
for arg in "$@"; do
    case "$arg" in
        --no-bump) BUMP=0 ;;
        --revision=*) FORCE_REV="${arg#*=}" ;;
        -h|--help)
            echo "用法: $0 [--no-bump] [--revision=N]"
            echo "  默认每次打包自动把 FPK 修订号 +1"
            exit 0
            ;;
        *) error "未知参数: $arg" ;;
    esac
done

# 从 server/version.go 读取核心版本（单一真相源）
VERSION_GO="$PROJECT_ROOT/server/version.go"
[ -f "$VERSION_GO" ] || error "找不到 $VERSION_GO"
CORE_VERSION=$(sed -n 's/^const CoreVersion = "\(.*\)"$/\1/p' "$VERSION_GO" | head -1)
[ -n "$CORE_VERSION" ] || error "无法从 server/version.go 解析 CoreVersion"
echo "$CORE_VERSION" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$' || \
    error "CoreVersion 必须是 x.y.z 形式，当前为 \"$CORE_VERSION\""

# 读取/初始化 FPK 修订号
FPK_REVISION=0
FPK_REVISION_BASE=""
if [ -f "$VERSION_ENV" ]; then
    # shellcheck disable=SC1090
    . "$VERSION_ENV"
fi

# 核心版本变化 -> 修订号归零，重新从 1 开始
if [ "$FPK_REVISION_BASE" != "$CORE_VERSION" ]; then
    info "核心版本变化 (${FPK_REVISION_BASE:-无} -> $CORE_VERSION)，FPK 修订号重置为 1"
    FPK_REVISION=0
    FPK_REVISION_BASE="$CORE_VERSION"
fi

if [ -n "$FORCE_REV" ]; then
    echo "$FORCE_REV" | grep -qE '^[0-9]+$' || error "--revision 必须是整数"
    FPK_REVISION="$FORCE_REV"
elif [ "$BUMP" = "1" ]; then
    FPK_REVISION=$((FPK_REVISION + 1))
fi

[ "$FPK_REVISION" -ge 1 ] || error "FPK 修订号必须 >= 1（fnOS 的 x.y.z-r 不接受 0）"

VERSION="${CORE_VERSION}-${FPK_REVISION}"

# 写回修订号（--no-bump 也写回，保证 BASE 被记录）
cat > "$VERSION_ENV" <<EOF
# CyanNVR FPK 打包版本配置（由 build-fpk.sh 自动维护，请勿手工编辑）
#
# 核心版本（x.y.z）定义在 server/version.go 的 CoreVersion。
# 下面的 FPK_REVISION 是打包修订号，每次打包自动 +1；
# 核心版本变化时自动重置为 1。
#
# 完整 fpk 版本 = \${CORE_VERSION}-\${FPK_REVISION}，例如 ${CORE_VERSION}-${FPK_REVISION}

FPK_REVISION=${FPK_REVISION}
FPK_REVISION_BASE=${FPK_REVISION_BASE}
EOF

# 定位 fnpack
if command -v fnpack &>/dev/null; then
    FNPACK="fnpack"
elif [ -x "$SCRIPT_DIR/fnpack" ]; then
    FNPACK="$SCRIPT_DIR/fnpack"
else
    error "未找到 fnpack。请从 https://developer.fnnas.com/docs/cli/fnpack/ 下载并放入 PATH。"
fi

info "=========================================="
info "  CyanNVR fnOS FPK Build"
info "  核心版本: ${CORE_VERSION}    FPK 版本: ${VERSION}"
info "=========================================="

# ── 0. 图标 ──
if [ -f "$SCRIPT_DIR/gen-icons.py" ]; then
    info "生成图标..."
    (cd "$SCRIPT_DIR" && python3 gen-icons.py >/dev/null)
fi
[ -f "$SCRIPT_DIR/ICON.PNG" ] && [ -f "$SCRIPT_DIR/ICON_256.PNG" ] || error "缺少 ICON.PNG / ICON_256.PNG"

# ── 1. 前端 ──
# 关键：必须使用「新鲜」的前端产物，否则会把修复前的旧界面打进 FPK。
# Docker 构建在容器内执行 npm run build，不会回写主机的 web/dist，
# 因此主机上的 web/dist 往往是陈旧的（曾导致导航栏事件图标为空白）。
DIST="$PROJECT_ROOT/web/dist"
DIST_STALE=0
if [ ! -f "$DIST/index.html" ]; then
    DIST_STALE=1
elif [ -n "$(find "$PROJECT_ROOT/web/src" "$PROJECT_ROOT/web/public" "$PROJECT_ROOT/web/index.html" \
        -newer "$DIST/index.html" -type f 2>/dev/null | head -1)" ]; then
    DIST_STALE=1
    warn "web/dist 早于源码修改，判定为陈旧"
fi

BUILT_OK=0
if [ "$DIST_STALE" = "0" ]; then
    info "复用已有的新鲜 web/dist"
    BUILT_OK=1
else
    info "需要重新构建前端..."
    if (cd "$PROJECT_ROOT/web" && npm run build) 2>/dev/null; then
        info "主机 npm 构建成功"
        BUILT_OK=1
    else
        warn "主机 npm 构建不可用（node_modules 可能归 root 所有），改从 Docker 镜像提取"
    fi
fi

IMAGE="simplenvr-cyannvr:latest"

# 镜像是否过期：源码比镜像新，或镜像不存在，都需要重建。
# 必须同时覆盖后端与前端——镜像里既有编译好的二进制，也有构建好的 dist，
# 只看 server/ 会漏掉前端改动（曾把旧图标、旧界面打进 fpk）。
IMAGE_STALE=0
if ! docker image inspect "$IMAGE" &>/dev/null; then
    IMAGE_STALE=1
else
    IMG_TIME=$(docker image inspect "$IMAGE" --format '{{.Created}}' 2>/dev/null)
    if [ -n "$IMG_TIME" ]; then
        # 只比对「源」文件：生成的 PNG 是派生产物，内容由 gen-icons.py + icon.svg
        # 决定；把它们算进来会永远判定陈旧（镜像全缓存复用时 Created 时间不更新），
        # 导致每次构建都白跑一次镜像构建。
        CHANGED=$(find \
            "$PROJECT_ROOT/server" \
            "$PROJECT_ROOT/web/src" \
            "$PROJECT_ROOT/web/public/icon.svg" \
            -type f -newermt "$IMG_TIME" 2>/dev/null | head -3)
        if [ -z "$CHANGED" ]; then
            CHANGED=$(find \
                "$PROJECT_ROOT/web/index.html" \
                "$PROJECT_ROOT/web/vite.config.ts" \
                "$PROJECT_ROOT/Dockerfile" \
                "$SCRIPT_DIR/gen-icons.py" \
                -newermt "$IMG_TIME" 2>/dev/null | head -3)
        fi
        if [ -n "$CHANGED" ]; then
            IMAGE_STALE=1
            warn "以下源码比镜像新，需要重建镜像：$(echo "$CHANGED" | tr '\n' ' ')"
        fi
    fi
fi

if [ "$BUILT_OK" = "0" ] || [ "$IMAGE_STALE" = "1" ]; then
    info "构建 Docker 镜像（同时产出最新前端与后端二进制）..."
    (cd "$PROJECT_ROOT" && docker build -t "$IMAGE" -f Dockerfile .) >/dev/null 2>&1 || \
        error "Docker 构建失败"
    if [ "$BUILT_OK" = "0" ]; then
        info "从镜像提取 dist..."
        rm -rf "$PROJECT_ROOT/web/dist.docker"
        mkdir -p "$PROJECT_ROOT/web/dist.docker"
        docker run --rm --entrypoint sh "$IMAGE" -c "cd /app/dist && tar cf - ." \
            | tar xf - -C "$PROJECT_ROOT/web/dist.docker"
        DIST="$PROJECT_ROOT/web/dist.docker"
    fi
fi

# 陈旧性断言：事件图标若仍是 Vant 中不存在的 bell-o，说明产物过期
if grep -q "bell-o" "$DIST"/assets/index-*.js 2>/dev/null; then
    error "前端产物仍含 Vant 不存在的 bell-o 图标，产物过期，请检查构建流程"
fi

# ── 2. 填充 app/ 目录 ──
info "填充 app/ ..."
APP_DIR="$SCRIPT_DIR/app"
rm -rf "$APP_DIR/dist" "$APP_DIR/models"
mkdir -p "$APP_DIR/dist" "$APP_DIR/models"

cp -a "$DIST/"* "$APP_DIR/dist/"

# Go 二进制（优先本地编译，否则从 Docker 镜像提取）
if command -v go &>/dev/null; then
    info "本地编译 Go 二进制..."
    (cd "$PROJECT_ROOT/server" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
        go build -ldflags="-s -w" -o "$APP_DIR/cyannvr" .)
else
    docker image inspect "$IMAGE" &>/dev/null || error "未安装 Go 且找不到 Docker 镜像 $IMAGE"
    info "从 Docker 镜像提取 Go 二进制..."
    docker run --rm --entrypoint sh "$IMAGE" -c "cat /app/cyannvr" > "$APP_DIR/cyannvr"
fi
chmod 755 "$APP_DIR/cyannvr"

# 版本一致性断言：二进制里必须能查到当前核心版本，否则说明取到了旧镜像/旧二进制
# （曾因此把不含新功能的包发出去）。二进制是 stripped 的，但字符串常量仍在。
if grep -aq "$CORE_VERSION" "$APP_DIR/cyannvr"; then
    info "二进制版本校验通过（含 $CORE_VERSION）"
else
    error "二进制中找不到核心版本 $CORE_VERSION，可能取到了过期产物；请先重建镜像再重试。"
fi

# AI 检测脚本与预置模型
[ -f "$PROJECT_ROOT/server/pkg/ai/ai_detect.py" ] && \
    cp "$PROJECT_ROOT/server/pkg/ai/ai_detect.py" "$APP_DIR/"
[ -f "$PROJECT_ROOT/ai_models/yolov8n.onnx" ] && \
    cp "$PROJECT_ROOT/ai_models/yolov8n.onnx" "$APP_DIR/models/"

# 权限归一：避免 Docker 产物带来的 000 权限
chmod -R u+rwX "$APP_DIR"
chmod 755 "$APP_DIR/cyannvr"

# ── 3. 生成 manifest（版本由脚本注入，避免手写版本号漂移）──
info "生成 manifest (version=${VERSION})..."
sed -E "s|^version[[:space:]]*=.*|version               = ${VERSION}|" \
    "$SCRIPT_DIR/manifest.tpl" > "$SCRIPT_DIR/manifest"
grep -q "^version" "$SCRIPT_DIR/manifest" || error "manifest.tpl 缺少 version 字段"

# ── 4. 用 fnpack 打包 ──
info "调用 fnpack 打包..."

# 权限归一：目录必须可进入（755），否则 fnpack 复制文件时会 permission denied。
# Docker 产物与部分工具会带回 000 权限，且 cp 会保留源权限。
find "$SCRIPT_DIR" -type d -exec chmod 755 {} \; 2>/dev/null
find "$SCRIPT_DIR" -type f -exec chmod 644 {} \; 2>/dev/null
chmod 755 "$SCRIPT_DIR"/cmd/* "$SCRIPT_DIR/build-fpk.sh" 2>/dev/null
chmod 755 "$SCRIPT_DIR/app/cyannvr" 2>/dev/null

cd "$SCRIPT_DIR"
rm -f "$SCRIPT_DIR/${APPNAME}.fpk"
"$FNPACK" build 2>&1 | tail -3

# fnpack 输出 <appname>.fpk，重命名为带完整版本号的规范名
if [ -f "$SCRIPT_DIR/${APPNAME}.fpk" ]; then
    mv -f "$SCRIPT_DIR/${APPNAME}.fpk" "$SCRIPT_DIR/${APPNAME}_${VERSION}_x86.fpk"
fi

FPK_FILE="$SCRIPT_DIR/${APPNAME}_${VERSION}_x86.fpk"
[ -f "$FPK_FILE" ] || error "打包失败，未生成 fpk"

info "=========================================="
info "  构建完成: $FPK_FILE ($(du -h "$FPK_FILE" | cut -f1))"
info "=========================================="
echo ""
echo "安装：飞牛应用中心 → 手动安装 → 上传该 fpk"
echo "端口：安装向导中可自定义（默认 18182）"
echo "数据：安装后如需迁移旧 Docker 数据，执行 sudo ./migrate-data.sh"
echo "账号：安装向导中可自定义（默认 admin / admin123）"
