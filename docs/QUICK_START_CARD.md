# 软路由快速开始卡片

## 🎯 30秒快速部署

### 方案一：Docker 软路由（推荐）

```bash
# 1. 进入项目目录
cd /vol1/1000/deepseek_harness/SimpleNVR

# 2. 执行部署脚本
sudo ./scripts/deploy_softrouter.sh

# 3. 输入服务器信息
# VPS IP: xxx.xxx.xxx.xxx
# 端口: 443
# UUID: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
# 域名: your-domain.com

# 4. 启动服务
cd /vol1/1000/NasAppFiles/softrouter
./start.sh

# 5. 验证
curl --socks5 127.0.0.1:10808 https://github.com
```

### 方案二：OpenWrt 软路由

```bash
# 1. 进入项目目录
cd /vol1/1000/deepseek_harness/SimpleNVR

# 2. 执行部署脚本
sudo ./scripts/deploy_openwrt.sh

# 3. 启动 OpenWrt
cd /vol1/1000/NasAppFiles/openwrt
./start.sh

# 4. 访问管理界面
# http://IP:8080
# 用户名: root
# 密码: password

# 5. 安装 V2Ray 插件
./install_v2ray.sh
```

---

## 📋 需要准备什么？

### 必需信息
1. **VPS IP 地址** - 您的服务器 IP
2. **VPS 端口** - 通常是 443 或 8443
3. **UUID** - V2Ray 用户唯一标识

### 可选信息
4. **域名** - 用于 TLS 证书

### 如何获取 UUID？
```bash
# 在 VPS 上执行
cat /proc/sys/kernel/random/uuid
```

---

## 🔧 服务端口

| 服务 | 端口 | 用途 |
|------|------|------|
| V2Ray SOCKS5 | 10808 | SOCKS5 代理 |
| V2Ray HTTP | 10809 | HTTP 代理 |
| AdGuard Home | 3000 | 广告过滤管理 |
| SmartDNS | 5353 | DNS 服务 |
| OpenWrt | 8080 | 路由器管理 |

---

## 📱 客户端配置

### Windows (V2RayN)
```
服务器: YOUR_VPS_IP
端口: 443
UUID: YOUR_UUID
传输: ws
路径: /v2ray
TLS: tls
```

### macOS (V2RayU)
```
服务器: YOUR_VPS_IP
端口: 443
UUID: YOUR_UUID
传输: ws
路径: /v2ray
TLS: tls
```

### iOS (Shadowrocket)
```
类型: V2Ray
地址: YOUR_VPS_IP
端口: 443
UUID: YOUR_UUID
传输: ws
路径: /v2ray
TLS: tls
```

### Android (V2RayNG)
```
类型: V2Ray
地址: YOUR_VPS_IP
端口: 443
UUID: YOUR_UUID
传输: ws
路径: /v2ray
TLS: tls
```

---

## ✅ 验证清单

- [ ] Docker 已安装
- [ ] Docker Compose 已安装
- [ ] VPS 已购买
- [ ] V2Ray 已安装
- [ ] UUID 已生成
- [ ] 部署脚本已执行
- [ ] 服务已启动
- [ ] 代理测试成功
- [ ] 客户端已配置

---

## 🚨 常见问题

### Q: 部署脚本执行失败？
A: 检查是否以 root 权限执行：`sudo ./scripts/deploy_softrouter.sh`

### Q: 代理连接失败？
A: 检查 VPS 防火墙是否开放 443 端口

### Q: DNS 解析失败？
A: 检查 SmartDNS 是否正常运行：`docker ps | grep smartdns`

### Q: 广告过滤不生效？
A: 检查 AdGuard Home 配置：`http://IP:3000`

---

## 📚 更多文档

- [详细部署指南](SOFTRouter_CREATE_GUIDE.md)
- [视频教程](SOFTRouter_VIDEO_TUTORIAL.md)
- [故障排查](SOFTRouter_GUIDE.md#故障排查)
- [性能优化](SOFTRouter_VIDEO_TUTORIAL.md#第八集性能优化)

---

## 🎯 一键部署

```bash
# 快速开始
sudo ./scripts/quick_start_softrouter.sh

# 或选择方案
sudo ./scripts/deploy_all.sh
```

---

**准备好了吗？开始部署吧！** 🚀
