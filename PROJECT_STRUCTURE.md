# 项目结构说明

## 📁 目录结构

```
SimpleNVR/
├── server/                    # Go 后端
│   ├── api/                   # REST API
│   ├── auth/                  # JWT 认证
│   ├── config/                # 配置管理
│   ├── models/                # 数据模型
│   ├── pkg/                   # 核心包
│   │   ├── ai/                # AI 识别
│   │   ├── ffmpeg/            # FFmpeg 封装
│   │   ├── gif/               # GIF 生成
│   │   ├── hls/               # HLS 流处理
│   │   ├── onvifx/            # ONVIF 探测
│   │   ├── recorder/          # 录像管理
│   │   └── snapshot/          # 快照处理
│   ├── store/                 # SQLite 存储
│   ├── go.mod                 # Go 模块
│   ├── go.sum                 # 依赖校验
│   └── main.go                # 入口文件
│
├── web/                       # Vue3 前端
│   ├── public/                # 静态资源
│   ├── src/                   # 源代码
│   │   ├── api/               # API 调用
│   │   ├── assets/            # 资源文件
│   │   ├── components/        # 组件
│   │   ├── mocks/             # 模拟数据
│   │   ├── router/            # 路由配置
│   │   ├── stores/            # Pinia 状态
│   │   ├── styles/            # 样式文件
│   │   ├── types/             # TypeScript 类型
│   │   ├── utils/             # 工具函数
│   │   ├── views/             # 页面组件
│   │   ├── App.vue            # 根组件
│   │   └── main.ts            # 入口文件
│   ├── package.json           # 依赖配置
│   └── vite.config.ts         # Vite 配置
│
├── docker/                    # Docker 配置
├── scripts/                   # 部署脚本（本地使用）
├── docs/                      # 文档（本地使用）
│
├── Dockerfile                 # Docker 构建文件
├── docker-compose.yml         # Docker Compose 配置
├── .gitignore                 # Git 忽略配置
└── README.md                  # 项目说明
```

## 🔧 核心功能

### 后端 (server/)
- **ONVIF 子码流自动探测** - 支持海康威视、大华、华为等品牌
- **RTSP 流处理** - 支持主码流和子码流
- **录像管理** - 分段录制、循环覆盖
- **AI 识别** - 本地 YOLOv8 或云端视觉模型
- **用户管理** - 4级权限控制 (admin/operator/user/viewer)

### 前端 (web/)
- **PWA 支持** - 可安装为原生应用
- **关怀模式** - 大字体、高对比度
- **响应式设计** - 移动端优先
- **HLS 播放** - 实时预览和回放

## 📦 部署脚本

### scripts/ 目录
- `deploy_softrouter.sh` - Docker 软路由部署
- `deploy_openwrt.sh` - OpenWrt 软路由部署
- `deploy_proxy.sh` - VPS 代理部署
- `vps_proxy_setup.sh` - SSH 隧道搭建
- `vps_http_proxy.sh` - HTTP 代理搭建
- `vps_socks5_proxy.sh` - SOCKS5 代理搭建

### docs/ 目录
- `SOFTRouter_GUIDE.md` - 软路由指南
- `VPS_PROXY_GUIDE.md` - VPS 代理指南
- `PUSH_GUIDE.md` - GitHub 推送指南

## 🎯 智能分流规则

### 国内流量（直连）
- 国内 IP 地址（geoip:cn）
- 国内域名（geosite:cn）
- 私有 IP 地址

### 国外流量（代理）
- Google 系列
- GitHub
- Telegram
- Twitter/X
- Facebook
- Netflix
- OpenAI

## 📊 服务端口

| 服务 | 端口 | 说明 |
|------|------|------|
| SimpleNVR | 18181 | 网络录像机 |
| V2Ray SOCKS5 | 10808 | SOCKS5 代理 |
| V2Ray HTTP | 10809 | HTTP 代理 |
| AdGuard Home | 3000 | 广告过滤 |
| SmartDNS | 5353 | DNS 服务 |
| OpenWrt | 8080 | 路由器管理 |

## 🔐 安全特性

- JWT 认证
- 密码哈希存储
- 权限分级控制
- CORS 配置
- 请求限流

## 📱 PWA 支持

- 可安装为原生应用
- 离线访问支持
- 自动更新
- 推送通知

## 🎨 关怀模式

- 大字体（18px+）
- 更大的触摸区域
- 更高的对比度
- 简化的界面

## 🚀 快速开始

### 1. Docker 部署（推荐）
```bash
docker compose up -d --build
```

### 2. 本地开发
```bash
# 后端
cd server && go run .

# 前端
cd web && npm run dev
```

### 3. 软路由部署
```bash
sudo ./scripts/deploy_softrouter.sh
```

## 📚 文档

- [README.md](README.md) - 项目说明
- [PROJECT_STRUCTURE.md](PROJECT_STRUCTURE.md) - 项目结构（本文档）
- [docs/SOFTRouter_GUIDE.md](docs/SOFTRouter_GUIDE.md) - 软路由指南
- [docs/VPS_PROXY_GUIDE.md](docs/VPS_PROXY_GUIDE.md) - VPS 代理指南

## 🤝 贡献

欢迎提交 Issue 和 Pull Request！

## 📄 许可证

MIT License
