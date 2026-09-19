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

# AI 推理后端变体：
#   cpu（默认）—— 只装 onnxruntime，镜像小，任何机器都能跑
#   gpu         —— 额外装 onnxruntime-gpu 与 CUDA 12 运行时，可用 NVIDIA 显卡
#
# 为什么 GPU 变体固定在 CUDA 12 世代（1.19.x）而不是最新版：
#   新版 onnxruntime-gpu 要求 CUDA 13，而 CUDA 13 的最低支持架构是 Turing
#   (sm_75)。Pascal 及更早的卡（例如 Tesla P4, sm_61）会被直接拒绝，
#   报 "Failed to create CUDAExecutionProvider"。
#   注意：即便用 CUDA 12 世代，Pascal 上仍可能在卷积层报
#   CUDNN_STATUS_EXECUTION_FAILED_CUDART —— 因此代码里对每个后端都会实跑
#   一次推理验证，失败自动回退 CPU，不会让服务起不来。
ARG AI_BACKEND=cpu
ARG ORT_GPU_VERSION=1.19.2

RUN sed -i 's@deb.debian.org@mirrors.aliyun.com@g' /etc/apt/sources.list.d/debian.sources /etc/apt/sources.list 2>/dev/null || true \
    && apt-get update \
    && apt-get install -y --no-install-recommends ffmpeg python3 python3-pip ca-certificates tzdata wget \
        libva2 libva-drm2 mesa-va-drivers vainfo \
    && rm -rf /var/lib/apt/lists/* \
    && pip3 install --no-cache-dir --break-system-packages -i https://mirrors.aliyun.com/pypi/simple \
        opencv-python-headless numpy \
    && if [ "$AI_BACKEND" = "gpu" ]; then \
         pip3 install --no-cache-dir --break-system-packages \
           --index-url https://pypi.org/simple \
           --extra-index-url https://pypi.nvidia.com \
           "onnxruntime-gpu==${ORT_GPU_VERSION}" \
           nvidia-cublas-cu12 nvidia-cudnn-cu12 nvidia-cufft-cu12 \
           nvidia-curand-cu12 nvidia-cuda-runtime-cu12 ; \
       else \
         pip3 install --no-cache-dir --break-system-packages \
           -i https://mirrors.aliyun.com/pypi/simple onnxruntime ; \
       fi \
    && ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime

# 内置一个开箱可用的检测模型。镜像只带 yolov8n 以控制体积，
# 其余模型（yolov8s/m、yolo11n/s、pose、seg）在设置页按需下载。
# 用 ultralytics 官方 Release 作为源：同时发布 .pt 与 .onnx，比第三方镜像可靠。
ARG YOLO_MODEL_URL=https://github.com/ultralytics/assets/releases/download/v8.4.0/yolov8n.onnx
ADD ${YOLO_MODEL_URL} /models/yolov8n.onnx

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