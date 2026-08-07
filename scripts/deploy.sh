#!/usr/bin/env bash
# 构建并推送镜像到容器仓库（Docker Hub / 私有 registry）
# 用法：
#   REGISTRY=myuser TAG=v0.1.0 ./scripts/deploy.sh
#   REGISTRY=registry.example.com/nvr TAG=v0.1.0 PLATFORMS=linux/amd64 ./scripts/deploy.sh
set -euo pipefail

REGISTRY="${REGISTRY:-your-dockerhub-username}"
IMAGE="${IMAGE:-simplenvr}"
TAG="${TAG:-latest}"
PLATFORMS="${PLATFORMS:-linux/amd64,linux/arm64}"

FULL="$REGISTRY/$IMAGE:$TAG"
echo "==> 目标镜像: $FULL  (platforms: $PLATFORMS)"

echo "==> docker login ..."
docker login

echo "==> 构建并推送 ..."
docker buildx create --use --name nvr-builder >/dev/null 2>&1 || docker buildx use nvr-builder
docker buildx build --platform "$PLATFORMS" -t "$FULL" --push .

echo "==> 完成: $FULL"
