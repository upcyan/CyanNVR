# 智能软路由快速开始

## 🎯 30秒启动

```bash
cd /vol1/1000/deepseek_harness/SimpleNVR
./start_softrouter.sh
```

## 📋 步骤说明

### 1. 选择代理核心
```
1. Xray（推荐，功能强大）
2. Sing-box（轻量级，性能好）
3. V2Ray（经典版本）
```

### 2. 输入配置链接
支持以下格式：
- `vless://uuid@server:port?type=ws&security=tls&sni=domain.com&path=/v2ray#名称`
- `vmess://base64_encoded_json`
- `ss://base64_encoded_method:password@server:port#名称`
- `trojan://password@server:port?security=tls&sni=domain.com#名称`

### 3. 等待启动
脚本会自动：
1. 创建必要的目录
2. 生成配置文件
3. 启动 Docker 容器
4. 配置智能分流规则

## 📊 服务端口

| 服务 | 端口 | 用途 |
|------|------|------|
| SOCKS5 代理 | 10808 | SOCKS5 代理 |
| HTTP 代理 | 10809 | HTTP 代理 |
| AdGuard Home | 3000 | 广告过滤管理 |
| SmartDNS | 5353 | DNS 服务 |

## ✅ 验证服务

```bash
# 检查容器状态
docker ps | grep -E "xray|singbox|v2ray|smartdns|adguardhome"

# 测试代理连接
curl --socks5 127.0.0.1:10808 https://github.com

# 测试 DNS 解析
nslookup google.com 127.0.0.1#5353
```

## 📱 客户端配置

### Windows (V2RayN)
1. 下载 V2RayN: https://github.com/2dust/v2rayN/releases
2. 添加服务器
3. 配置分流规则

### macOS (V2RayU)
1. 下载 V2RayU: https://github.com/yanue/V2rayU/releases
2. 添加服务器
3. 配置分流规则

### iOS (Shadowrocket)
1. 下载 Shadowrocket (需要海外 Apple ID)
2. 添加服务器
3. 配置分流规则

### Android (V2RayNG)
1. 下载 V2RayNG: https://github.com/2dust/v2rayNG/releases
2. 添加服务器
3. 配置分流规则

## 🛠️ 故障排查

### 问题 1: 代理无法连接
```bash
# 检查容器状态
docker ps | grep xray

# 查看日志
docker logs xray
```

### 问题 2: DNS 解析失败
```bash
# 检查 SmartDNS
docker ps | grep smartdns

# 测试 DNS
nslookup google.com 127.0.0.1#5353
```

### 问题 3: 广告过滤不生效
```bash
# 检查 AdGuard Home
docker ps | grep adguardhome

# 访问管理界面
http://IP:3000
```

## 📚 更多文档

- [智能软路由部署说明](docs/SMART_SOFTRouter_README.md)
- [软路由创建指南](docs/SOFTRouter_CREATE_GUIDE.md)
- [视频教程文字版](docs/SOFTRouter_VIDEO_TUTORIAL.md)

## 🎯 一键部署

```bash
# 快速启动
./start_softrouter.sh

# 或使用完整部署脚本
sudo ./scripts/deploy_smart_softrouter.sh
```

**现在您可以开始使用智能软路由了！** 🚀
