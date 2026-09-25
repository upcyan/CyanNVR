# ---- Runtime only (binaries built on host) ----
# debian 而非 alpine：onnxruntime 仅有 glibc wheel，无 musl 版
FROM debian:bookworm-slim
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
COPY ai_models/*.onnx /models/

WORKDIR /app

# Binaries cross-compiled on host (linux/amd64)
COPY simplenvr-linux /app/cyannvr
COPY web/dist /app/dist
COPY server/pkg/ai/ai_detect.py /app/ai_detect.py

ENV NVR_PORT=8080 \
    NVR_DATA=/data \
    NVR_WEB=/app/dist \
    NVR_AI_DETECT_SCRIPT=/app/ai_detect.py \
    NVR_AI_DETECT_URL=unix:/tmp/cyannvr-ai.sock \
    NVR_AI_MODEL_PATH=/models/yolo11n.onnx \
    NVR_AI_MODELS_DIR=/data/models \
    PYTHON=python3 \
    GIN_MODE=release
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
  CMD ["wget", "-qO-", "http://127.0.0.1:8080/api/health"]
ENTRYPOINT ["/app/cyannvr"]
