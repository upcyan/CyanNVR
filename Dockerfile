# ---- Stage 1: build frontend ----
FROM hub.rat.dev/library/node:20-alpine AS web
WORKDIR /app/web
COPY web/package*.json ./
RUN npm config set registry https://registry.npmmirror.com && npm install
COPY web/ ./
RUN npm run build

# ---- Stage 2: build server ----
FROM hub.rat.dev/library/golang:1.25-alpine AS build
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN go env -w GOPROXY=https://goproxy.cn,direct && go mod download
COPY server/ ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/cyannvr .

# ---- Stage 3: runtime ----
FROM hub.rat.dev/library/debian:bookworm-slim

# AI 推理后端变体（AI_BACKEND 构建参数）：
#   cpu  — 只装 onnxruntime，镜像最小，任何机器都能跑
#   gpu  — 装 onnxruntime-gpu + CUDA 运行时，自动匹配 GPU 架构
#
# GPU 变体的自动适配逻辑（ai_detect.py 中实现）：
#   1. 先尝试 CUDA 13 + cuDNN 9（最新，Turing sm_75+）
#   2. 失败则回退 CUDA 12 + cuDNN 9（Volta+）
#   3. 再失败则回退 CUDA 11 + cuDNN 8（Pascal sm_61）
#   4. 全部失败则回退 CPU
#
# 为什么 Pascal 需要特殊处理：
#   Tesla P4（Pascal sm_61）是常见的性价比显卡，但 NVIDIA 在 CUDA 13 中
#   移除了 Pascal 支持，cuDNN 9 在 CUDA 12 下对 Pascal 的卷积操作会崩溃
#   （CUDNN_STATUS_EXECUTION_FAILED_CUDART）。NVIDIA 官方文档确认
#   Pascal 的正确组合是 cuDNN 9.2.1 + CUDA 11.8。
ARG AI_BACKEND=cpu

RUN sed -i 's@deb.debian.org@mirrors.aliyun.com@g' /etc/apt/sources.list.d/debian.sources /etc/apt/sources.list 2>/dev/null || true \
    && apt-get update \
    && apt-get install -y --no-install-recommends ffmpeg python3 python3-pip ca-certificates tzdata wget \
        libva2 libva-drm2 mesa-va-drivers vainfo \
    && rm -rf /var/lib/apt/lists/* \
    && pip3 install --no-cache-dir --break-system-packages -i https://mirrors.aliyun.com/pypi/simple \
        opencv-python-headless numpy \
    && if [ "$AI_BACKEND" = "gpu" ]; then \
         # 预装 onnxruntime-gpu 1.17 + CUDA 11 + cuDNN 8（Pascal 兼容）
         pip3 install --no-cache-dir --break-system-packages \
           -i https://mirrors.aliyun.com/pypi/simple \
           "onnxruntime-gpu==1.17.1" "numpy<2" \
         && pip3 install --no-cache-dir --break-system-packages \
           -i https://mirrors.aliyun.com/pypi/simple \
           nvidia-cublas-cu11 nvidia-cudnn-cu11 nvidia-cuda-runtime-cu11 \
           nvidia-cufft-cu11 nvidia-curand-cu11 ; \
       else \
         # CPU 镜像：装 onnxruntime-openvino（含 OpenVINO + CPU EP）
         pip3 install --no-cache-dir --break-system-packages \
           -i https://mirrors.aliyun.com/pypi/simple onnxruntime-openvino ; \
       fi \
    && ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime

# 内置一个开箱可用的检测模型。镜像只带 yolov8n 以控制体积，
# 其余模型（yolov8s/m、yolo11n/s、pose、seg）在设置页按需下载。
# 注意：此文件已预下载到构建上下文 ai_models/ 目录，避免构建时拉取外部资源
# （GitHub Releases 在国内构建环境中经常超时）。
# models/ 被 .dockerignore 排除，所以放到 ai_models/ 下
COPY ai_models/yolov8n.onnx /models/yolov8n.onnx

WORKDIR /app
COPY --from=build /out/cyannvr /app/cyannvr
COPY --from=web /app/web/dist /app/dist
COPY server/pkg/ai/ai_detect.py /app/ai_detect.py

ENV NVR_PORT=8080 \
    NVR_DATA=/data \
    NVR_WEB=/app/dist \
    NVR_AI_DETECT_SCRIPT=/app/ai_detect.py \
    NVR_AI_DETECT_URL=unix:/tmp/cyannvr-ai.sock \
    NVR_AI_MODEL_PATH=/models/yolov8n.onnx \
    NVR_AI_MODELS_DIR=/data/models \
    PYTHON=python3 \
    GIN_MODE=release
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
  CMD ["wget", "-qO-", "http://127.0.0.1:8080/api/health"]
ENTRYPOINT ["/app/cyannvr"]
