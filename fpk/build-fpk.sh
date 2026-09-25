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

# ── 版本解析与 bump 规则 ──
# 版本分离：核心版本 x.y.z 写入 server/version.go，
# FPK 修订号 r 由本脚本维护，最终 fpk 版本为 x.y.z-r
# （fnOS 只接受 x.y.z[-r]，四段式 x.y.z.r 会被拒绝）。
#
# bump 规则（脚本只自动做 patch，minor/major 由人工指定）：
#   只动 fpk/ 或源码无变化 -> 核心版本不变，仅递增修订号 r 重新出包
#   动了 server/、web/     -> patch +1（默认 auto 行为）
#   成规模的功能改动       -> 打包时加 --bump=minor，中间位 +1（x.y.0）
#   大改动                 -> 打包时加 --bump=major，第一位 +1（y.0.0）
#
# 为什么 minor/major 不自动判定：「这次改动算小改还是大改」是语义判断，
# 无法从源码哈希差异里可靠推断（改动 3 行也可能是重要特性）。因此 auto
# 保守地只升 patch，并在检测到核心变更时提示可用 --bump=minor 重跑。
#
# 判定依据是源码内容哈希（记录在 version.env），不依赖 git 提交状态，
# 因此在未提交的工作区上也能正确判断。
#
# 参数：
#   --bump=auto|major|minor|patch|none   核心版本处理方式（默认 auto）
#   --revision N   指定 FPK 修订号（默认自动 +1）
#   --no-bump      修订号也不递增（用于原样重出同一个包）
BUMP_MODE="auto"
REV_BUMP=1
FORCE_REV=""
for arg in "$@"; do
    case "$arg" in
        --no-bump) REV_BUMP=0 ;;
        --revision=*) FORCE_REV="${arg#*=}" ;;
        --bump=*) BUMP_MODE="${arg#*=}" ;;
        -h|--help)
            echo "用法: $0 [--bump=auto|major|minor|patch|none] [--revision=N] [--no-bump]"
            echo "  auto  : 核心源码有变更 -> patch+1；只动 fpk/ 或无变化 -> 核心版本不变"
            echo "  minor : 中间位 +1（x.y.0），用于成规模的功能改动"
            echo "  major : 第一位 +1（y.0.0），用于大改动"
            echo "  patch : 末位 +1（x.y.z）"
            echo "  none  : 核心版本保持不变"
            echo "  默认每次打包都会递增 FPK 修订号 r"
            exit 0
            ;;
        *) error "未知参数: $arg" ;;
    esac
done
case "$BUMP_MODE" in
    auto|major|minor|patch|none) ;;
    *) error "--bump 只接受 auto|major|minor|patch|none，当前为 \"$BUMP_MODE\"" ;;
esac

# 从 server/version.go 读取核心版本（写入目标，自动 bump 会就地更新它）
VERSION_GO="$PROJECT_ROOT/server/version.go"
[ -f "$VERSION_GO" ] || error "找不到 $VERSION_GO"
CORE_VERSION=$(sed -n 's/^const CoreVersion = "\(.*\)"$/\1/p' "$VERSION_GO" | head -1)
[ -n "$CORE_VERSION" ] || error "无法从 server/version.go 解析 CoreVersion"
echo "$CORE_VERSION" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$' || \
    error "CoreVersion 必须是 x.y.z 形式，当前为 \"$CORE_VERSION\""

semver_bump() {
    local v="$1" part="$2" maj min pat
    IFS=. read -r maj min pat <<< "$v"
    case "$part" in
        major) echo "$((maj + 1)).0.0" ;;
        minor) echo "${maj}.$((min + 1)).0" ;;
        patch) echo "${maj}.${min}.$((pat + 1))" ;;
    esac
}

# ── 源码内容哈希（判定核心源码是否变化）──
# 用 git 跟踪列表取「源码」：.gitignore 已经把 app/cyannvr、app/dist、
# manifest、package/、*.fpk、version.env 等构建产物排除在外，
# 因此这里天然只覆盖真正的源文件（含 fpk/app/ui 这类手工维护的资源）。
# version.go 由本脚本改写，必须排除，否则每次打包都会自我触发 bump。
source_hash() {
    local pathspec="$1"
    {
        git -C "$PROJECT_ROOT" ls-files -z -- $pathspec 2>/dev/null |
            grep -zv '^server/version\.go$' |
            tr '\0' '\n'
    } | sort | while IFS= read -r f; do
        [ -f "$PROJECT_ROOT/$f" ] && printf '%s  %s\n' "$(sha256sum "$PROJECT_ROOT/$f" | cut -c1-16)" "$f"
    done | sha256sum | cut -c1-16
}

# 只对核心源码取哈希：fpk/ 的改动（向导、图标、生命周期脚本）不影响应用功能，
# 按规则不 bump 核心版本，因此无需参与判定。
# FPK_HASH 仍会被记录到 version.env，仅用于诊断。
CORE_NOW=$(source_hash "server web Dockerfile")
[ -n "$CORE_NOW" ] || error "无法计算核心源码哈希（git 仓库状态异常？）"

# 读取上次打包的状态
FPK_REVISION=0
FPK_REVISION_BASE=""

if [ -f "$VERSION_ENV" ]; then
    # shellcheck disable=SC1090
    . "$VERSION_ENV"
fi

# 决定新的核心版本
#
# auto 只负责 patch：核心源码变了就 +1；只动 fpk/ 或完全没变则不动核心版本。
# minor / major 交由人工判断（--bump=minor / --bump=major）——
# 「这次改动算小改还是大改」是语义判断，脚本无法从哈希差异可靠推断。
NEW_CORE="$CORE_VERSION"
case "$BUMP_MODE" in
    none)
        info "核心版本保持不变（--bump=none）：$CORE_VERSION"
        ;;
    major|minor|patch)
        NEW_CORE=$(semver_bump "$CORE_VERSION" "$BUMP_MODE")
        info "按 --bump=$BUMP_MODE 提升核心版本：$CORE_VERSION -> $NEW_CORE"
        ;;
    auto)
        if [ -z "$CORE_HASH" ]; then
            warn "首次运行（无源码基线）：记录基线，核心版本保持 $CORE_VERSION"
        elif [ "$CORE_NOW" != "$CORE_HASH" ]; then
            NEW_CORE=$(semver_bump "$CORE_VERSION" patch)
            info "检测到核心源码（server/、web/）变更 -> 核心版本 $CORE_VERSION -> $NEW_CORE（patch）"
            info "  若本次是成规模的功能改动，请改用 --bump=minor 重跑以取得正确的版本号"
        else
            # 只动 fpk/（向导、图标、生命周期脚本）或完全没变：
            # 都不是应用功能变化，核心版本保持，仅递增修订号 r 重新出包。
            info "核心源码无变化 -> 核心版本保持 $CORE_VERSION（仅递增 FPK 修订号重新出包）"
        fi
        ;;
esac

# 写回 version.go（核心版本的单一真相源）
CORE_CHANGED=0
if [ "$NEW_CORE" != "$CORE_VERSION" ]; then
    CORE_CHANGED=1
    # sed -i 需要往同目录写临时文件；目录不可写时的报错很晦涩
    # （"couldn't open temporary file ... Permission denied"），这里先给出可操作的提示。
    if [ ! -w "$(dirname "$VERSION_GO")" ]; then
        error "$(dirname "$VERSION_GO") 目录不可写，无法更新 CoreVersion。
  修复：sudo chown \$(id -un):\$(id -gn) $(dirname "$VERSION_GO") && chmod u+w $(dirname "$VERSION_GO")"
    fi
    sed -i "s|^const CoreVersion = \".*\"\$|const CoreVersion = \"${NEW_CORE}\"|" "$VERSION_GO"
    grep -q "^const CoreVersion = \"${NEW_CORE}\"\$" "$VERSION_GO" || \
        error "写入 $VERSION_GO 失败"
    CORE_VERSION="$NEW_CORE"
    info "已更新 server/version.go: CoreVersion = \"$CORE_VERSION\""
fi

# 核心版本变化 -> 修订号归零，重新从 1 开始
if [ "$FPK_REVISION_BASE" != "$CORE_VERSION" ]; then
    [ "$FPK_REVISION_BASE" != "" ] && \
        info "核心版本变化 (${FPK_REVISION_BASE} -> $CORE_VERSION)，FPK 修订号重置为 1"
    FPK_REVISION=0
    FPK_REVISION_BASE="$CORE_VERSION"
    REV_BUMP=0   # 下面直接置 1，不再叠加
    FPK_REVISION=1
fi

if [ -n "$FORCE_REV" ]; then
    echo "$FORCE_REV" | grep -qE '^[0-9]+$' || error "--revision 必须是整数"
    FPK_REVISION="$FORCE_REV"
elif [ "$REV_BUMP" = "1" ]; then
    FPK_REVISION=$((FPK_REVISION + 1))
fi

[ "$FPK_REVISION" -ge 1 ] || error "FPK 修订号必须 >= 1（fnOS 的 x.y.z-r 不接受 0）"

VERSION="${CORE_VERSION}-${FPK_REVISION}"

# 写回状态（哈希在 version.go 更新之后重算，作为下次比对的基线）
cat > "$VERSION_ENV" <<EOF
# CyanNVR FPK 打包版本配置（由 build-fpk.sh 自动维护，请勿手工编辑）
#
# CORE_VERSION 与 server/version.go 的 CoreVersion 同步；
# 本脚本按源码变更范围自动 bump（只动 fpk/ → patch，动核心 → minor），
# FPK_REVISION 是同一核心版本下的重新出包计数。
#
# 完整 fpk 版本 = \${CORE_VERSION}-\${FPK_REVISION}，例如 ${CORE_VERSION}-${FPK_REVISION}

CORE_VERSION=${CORE_VERSION}
FPK_REVISION=${FPK_REVISION}
FPK_REVISION_BASE=${FPK_REVISION_BASE}
CORE_HASH=$(source_hash "server web Dockerfile")
FPK_HASH=$(source_hash "fpk")
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

# ── 0.5 代码格式检查 ──
# 为什么放在打包流程里：仓库没有 CI，格式漂移只能靠「打包」这个必经步骤拦住。
# 历史问题：多处结构体/常量块在增删字段后没重排对齐，gofmt -l 长期有输出，
# 既干扰 git diff 评审，也让「格式是否干净」失去信号价值。
#
# 本机通常没有 Go 工具链（见 AGENTS.md），此时借用 Docker 镜像里的 gofmt；
# 两者都不可用则只警告不阻断（不能因为环境缺工具就无法出包）。
check_gofmt() {
    local out=""
    if command -v gofmt &>/dev/null; then
        out=$(cd "$PROJECT_ROOT/server" && gofmt -l . 2>/dev/null)
    elif command -v docker &>/dev/null; then
        out=$(docker run --rm -v "$PROJECT_ROOT/server:/src" -w /src \
            hub.rat.dev/library/golang:1.25-alpine gofmt -l . 2>/dev/null)
    else
        warn "未找到 gofmt 也无 Docker，跳过代码格式检查"
        return 0
    fi
    if [ -n "$out" ]; then
        warn "以下文件不符合 gofmt（建议修复后再打包）："
        echo "$out" | sed 's/^/    /'
        warn "修复：cd server && gofmt -w <上述文件>"
        # 只警告不阻断：格式问题不该阻塞发版，但必须显式可见
    else
        info "代码格式检查通过（gofmt 无差异）"
    fi
}
check_gofmt

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

# 镜像只有两个真实用途，两者都不成立时**跳过构建**，让「本机只出 fpk」不碰 docker：
#   1) 主机 npm 构建失败（BUILT_OK=0）-> 从镜像提取 dist
#   2) 主机没有 go                     -> 从镜像提取后端二进制
# 原条件是 `BUILT_OK=0 || IMAGE_STALE=1`，镜像不存在就无条件建，导致
# 「有 go、dist 也新鲜」的常规出包也白跑一次 docker build（镜像一个字节都用不上）。
NEED_IMAGE=0
if [ "$BUILT_OK" = "0" ] || ! command -v go &>/dev/null; then
    NEED_IMAGE=1
fi

if [ "$NEED_IMAGE" = "1" ] && [ "$IMAGE_STALE" = "1" ]; then
    info "构建 Docker 镜像（用于提取前端 dist / 后端二进制）..."
    # 失败时必须把输出露出来：原先 >/dev/null 2>&1 只剩一句「Docker 构建失败」，
    # 真实原因（如 docker 需要可写 HOME 才能放 buildx 状态）全被吞掉。
    BUILD_LOG=$(mktemp)
    if ! (cd "$PROJECT_ROOT" && docker build -t "$IMAGE" -f Dockerfile .) >"$BUILD_LOG" 2>&1; then
        tail -40 "$BUILD_LOG" >&2
        rm -f "$BUILD_LOG"
        error "Docker 构建失败"
    fi
    rm -f "$BUILD_LOG"
    info "镜像构建完成"
fi

if [ "$BUILT_OK" = "0" ]; then
    info "从镜像提取 dist..."
    rm -rf "$PROJECT_ROOT/web/dist.docker"
    mkdir -p "$PROJECT_ROOT/web/dist.docker"
    docker run --rm --entrypoint sh "$IMAGE" -c "cd /app/dist && tar cf - ." \
        | tar xf - -C "$PROJECT_ROOT/web/dist.docker"
    DIST="$PROJECT_ROOT/web/dist.docker"
fi

# 陈旧性断言：事件图标若仍是 Vant 中不存在的 bell-o，说明产物过期
if grep -q "bell-o" "$DIST"/assets/index-*.js 2>/dev/null; then
    error "前端产物仍含 Vant 不存在的 bell-o 图标，产物过期，请检查构建流程"
fi

# ── 彻底禁用前端缓存（构建期覆写，不依赖插件版本）──
# 背景：Service Worker 会拦截导航并从预缓存供旧页面，普通 F5 绕不过去；
# sw.js 无缓存头还会被浏览器启发式缓存拖延更新——局域网 NVR 排错成本
# 远高于离线收益，直接禁用：
#   sw.js         -> 透传型（仅承担「旧 sw 换代」的触发作用，不做任何缓存）
#   registerSW.js -> 注销全部历史 SW 并清空 CacheStorage（解掉已卡死的客户端）
cat > "$DIST/sw.js" <<'SWEOF'
/* CyanNVR: 缓存已禁用 —— 透传型 Service Worker，仅用于触发旧 sw 换代 */
self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (e) => e.waitUntil(self.clients.claim()));
self.addEventListener('fetch', (e) => { e.respondWith(fetch(e.request)); });
SWEOF
cat > "$DIST/registerSW.js" <<'SWEOF'
/* CyanNVR: 卸载历史 Service Worker 并清空预缓存 —— 所有请求直达服务器 */
if ('serviceWorker' in navigator) {
  window.addEventListener('load', async () => {
    try {
      const regs = await navigator.serviceWorker.getRegistrations();
      for (const r of regs) await r.unregister();
      if (window.caches) {
        const keys = await caches.keys();
        await Promise.all(keys.map((k) => caches.delete(k)));
      }
    } catch (_) { /* ignore */ }
  });
}
SWEOF
info "已覆写 sw.js/registerSW.js（缓存禁用）"

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
if compgen -G "$PROJECT_ROOT/ai_models/*.onnx" > /dev/null; then
    cp "$PROJECT_ROOT"/ai_models/*.onnx "$APP_DIR/models/"
fi

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
