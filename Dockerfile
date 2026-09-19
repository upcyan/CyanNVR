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
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/simplenvr .

# ---- Stage 3: runtime ----
FROM hub.rat.dev/library/debian:bookworm-slim
RUN sed -i 's@deb.debian.org@mirrors.aliyun.com@g' /etc/apt/sources.list.d/debian.sources /etc/apt/sources.list 2>/dev/null || true \
    && apt-get update \
    && apt-get install -y --no-install-recommends ffmpeg python3 python3-pip ca-certificates tzdata wget \
    && rm -rf /var/lib/apt/lists/* \
    && pip3 install --no-cache-dir --break-system-packages -i https://mirrors.aliyun.com/pypi/simple \
        onnxruntime opencv-python-headless numpy \
    && ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime

# AI 检测模型（构建时自动从 hf-mirror 下载）
ARG YOLO_MODEL_URL=https://hf-mirror.com/salim4n/yolov8n-detect-onnx/resolve/main/yolov8n-onnx-web/yolov8n.onnx
ADD ${YOLO_MODEL_URL} /models/yolov8n.onnx

WORKDIR /app
COPY --from=build /out/simplenvr /app/simplenvr
COPY --from=web /app/web/dist /app/dist
COPY server/pkg/ai/ai_detect.py /app/ai_detect.py

ENV NVR_PORT=8080 \
    NVR_DATA=/data \
    NVR_WEB=/app/dist \
    NVR_AI_DETECT_SCRIPT=/app/ai_detect.py \
    NVR_AI_DETECT_URL=unix:/tmp/simplenvr-ai.sock \
    NVR_AI_MODEL_PATH=/models/yolov8n.onnx \
    PYTHON=python3 \
    GIN_MODE=release
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
  CMD ["wget", "-qO-", "http://127.0.0.1:8080/api/health"]
ENTRYPOINT ["/app/simplenvr"]