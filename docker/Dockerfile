# ---- Stage 1: build frontend ----
FROM node:20-alpine AS web
WORKDIR /app/web
COPY web/package*.json ./
RUN npm install --registry=https://registry.npmmirror.com
COPY web/ ./
RUN npm run build

# ---- Stage 2: build server ----
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/cyannvr .

# ---- Stage 3: runtime ----
FROM alpine:3.20
RUN apk add --no-cache ffmpeg tzdata ca-certificates && ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime
WORKDIR /app
COPY --from=build /out/cyannvr /app/cyannvr
COPY --from=web /app/web/dist /app/dist
ENV NVR_PORT=8080 \
    NVR_DATA=/data \
    NVR_WEB=/app/dist \
    GIN_MODE=release
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/app/cyannvr"]
