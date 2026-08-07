# SimpleNVR

基于 ONVIF / RTSP 的轻量网络录像机。Go 后端 + Vue3 移动端优先前端（PWA），支持手机网页、Android WebView 壳、PC 浏览器。

## 功能

- **实时预览**：卡片式多路预览、多画面、全屏查看、截图、回放入口
- **历史回放**：日历热力图（当日有录像高亮）、时间轴拖拽跳转、倍速播放（1x/2x/4x/8x）
- **录像**：ffmpeg 分段录制（5 分钟/段）、HLS 实时流与回放流、循环覆盖
- **AI 画面识别**：OpenAI 兼容视觉接口，识别异常画面并生成事件记录 + 动图快照（GIF）
- **事件记录**：事件列表（动图/快照/描述/时间），按类型着色
- **认证与权限**：JWT 登录，角色 admin / operator / user / viewer 分级
- **服务器地址**：App 端可配置局域网/公网地址并自动探测，自动选择可达的连接方式
- **适老化**：大字体三档缩放 + 关怀模式（高对比、大触控热区）

## 目录结构

```
├── server/          Go 后端（Gin + SQLite + ffmpeg）
│   ├── api/          REST API
│   ├── pkg/          ffmpeg/hls/gif/ai/onvif/snapshot/recorder
│   ├── auth/         JWT 认证
│   └── store/        SQLite 存储
├── web/             Vue3 + Vant4 + hls.js 前端（PWA）
├── docker/          备用 Dockerfile
├── Dockerfile       多阶段构建（含 ffmpeg）
└── docker-compose.yml
```

## 快速开始

### 本地开发

```bash
# 前端
cd web
npm install
npm run dev            # http://localhost:5173

# 后端（需安装 Go 1.24+ 与 ffmpeg）
cd server
go mod tidy
go run .              # http://localhost:8080
```

后端默认托管 `web/dist`（如存在）。设置 `NVR_WEB` 指向构建产物，或 `cd web && npm run build` 后重启后端。

### Docker 部署

```bash
docker compose up -d --build
# 打开 http://localhost:8080
# 默认账号 admin / admin123（可用 NVR_ADMIN_PASSWORD 覆盖）
```

数据持久化在 `./data`（录像、快照、事件、SQLite）。

## 验证步骤

### 1. 后端编译与 API

```bash
cd server
go build ./... && go vet ./...
go run .
# 健康检查
curl http://localhost:8080/api/health
# 登录
curl -X POST http://localhost:8080/api/auth/login -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}'
# 用返回的 token 访问受保护接口
curl -H "Authorization: Bearer <token>" http://localhost:8080/api/devices
```

### 2. 前端构建

```bash
cd web
npm run typecheck
npm run build          # 产物在 web/dist
```

### 3. 添加测试源（无需真实摄像头）

```bash
curl -X POST http://localhost:8080/api/devices -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"name":"测试源","source":"test"}'
```

`source: test` 会用 ffmpeg 生成测试画面，自动验证录像、HLS、快照全链路。真实摄像机选 `source: rtsp`（默认），可填写 IP/端口/账号密码。

### 4. 界面验证

- 浏览器打开 http://localhost:8080 → 登录 → 添加测试源 → 实时预览看到动态画面
- 切换到「录像管理」→ 日历选择今天 → 时间轴显示录像段 → 拖拽跳转、倍速播放
- 事件页：配置 AI 后，识别异常会生成事件卡片（动图）
- 登录页「服务器设置」：填写局域网/公网地址 → 测试连通性 → 自动选择连接方式

### 5. AI 识别（可选）

```bash
# 方式一：环境变量
NVR_AI_ENABLED=true NVR_AI_API_KEY=sk-xxx NVR_AI_MODEL=gpt-4o-mini go run .
# 方式二：Web 界面 → 设置 → AI 画面识别
```

## 环境变量

| 变量 | 说明 | 默认 |
|---|---|---|
| NVR_PORT | HTTP 端口 | 8080 |
| NVR_DATA | 数据目录 | ./data |
| NVR_JWT_SECRET | JWT 密钥 | 开发用固定值 |
| NVR_ADMIN_PASSWORD | 初始 admin 密码 | admin123 |
| NVR_AI_ENABLED | 开启 AI 识别 | false |
| NVR_AI_BASE_URL | OpenAI 兼容接口 | https://api.openai.com/v1 |
| NVR_AI_MODEL | 视觉模型 | gpt-4o-mini |
| NVR_AI_API_KEY | API Key | 空 |
