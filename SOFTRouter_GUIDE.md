# 软路由 + V2Ray 智能分流完整指南

## 📋 方案概览

提供两种软路由方案：

| 方案 | 特点 | 适用场景 | 难度 |
|------|------|----------|------|
| **Docker 软路由** | 轻量级，资源占用少 | 服务器/NAS | ⭐⭐ |
| **OpenWrt 软路由** | 功能完整，插件丰富 | 路由器/NAS | ⭐⭐⭐ |

---

## 🚀 方案一：Docker 软路由（推荐）

### 优点
- ✅ 轻量级，资源占用少
- ✅ 部署简单，一键完成
- ✅ 支持广告过滤、DNS 加速

### 部署步骤

#### 1. 执行部署脚本
```bash
cd /vol1/1000/deepseek_harness/SimpleNVR
sudo ./deploy_softrouter.sh
```

#### 2. 输入 V2Ray 服务器信息
- VPS IP 地址
- VPS 端口
- UUID
- 域名

#### 3. 启动服务
```bash
cd /vol1/1000/NasAppFiles/softrouter
./start.sh
```

#### 4. 配置客户端
- SOCKS5 代理: `IP:10808`
- HTTP 代理: `IP:10809`

---

## 🌐 方案二：OpenWrt 软路由

### 优点
- ✅ 功能完整，类似真实路由器
- ✅ 丰富的插件生态
- ✅ LuCI Web 管理界面

### 部署步骤

#### 1. 执行部署脚本
```bash
cd /vol1/1000/deepseek_harness/SimpleNVR
sudo ./deploy_openwrt.sh
```

#### 2. 启动 OpenWrt
```bash
cd /vol1/1000/NasAppFiles/openwrt
./start.sh
```

#### 3. 访问管理界面
- 地址: `http://IP:8080`
- 用户名: `root`
- 密码: `password`

#### 4. 安装 V2Ray 插件
```bash
./install_v2ray.sh
```

#### 5. 配置 V2Ray
1. 进入 服务 → V2Ray
2. 配置服务器信息
3. 配置智能分流规则

---

## ⚙️ 智能分流规则

### 默认规则
```json
{
  "routing": {
    "rules": [
      {
        "type": "field",
        "ip": ["geoip:cn", "geoip:private"],
        "outboundTag": "direct"
      },
      {
        "type": "field",
        "domain": ["geosite:cn"],
        "outboundTag": "direct"
      },
      {
        "type": "field",
        "domain": [
          "google.com", "youtube.com", "github.com",
          "telegram.org", "twitter.com", "facebook.com",
          "netflix.com", "openai.com"
        ],
        "outboundTag": "proxy"
      },
      {
        "type": "field",
        "port": "0-65535",
        "outboundTag": "proxy"
      }
    ]
  }
}
```

### 规则说明
1. **国内 IP**: 直连
2. **国内域名**: 直连
3. **国外常用网站**: 代理
4. **其他流量**: 代理

---

## 🔧 配置文件位置

### Docker 软路由
```
/vol1/1000/NasAppFiles/softrouter/
├── config/           # 配置文件
├── data/             # 数据文件
├── v2ray/
│   └── config.json   # V2Ray 配置
├── smartdns/
│   └── smartdns.conf # SmartDNS 配置
└── adguard/
    └── conf/
        └── AdGuardHome.yaml # AdGuard Home 配置
```

### OpenWrt 软路由
```
/vol1/1000/NasAppFiles/openwrt/
├── config/           # 配置文件
├── data/             # 数据文件
└── v2ray/
    └── config.json   # V2Ray 配置
```

---

## 📊 服务端口

| 服务 | 端口 | 说明 |
|------|------|------|
| V2Ray SOCKS5 | 10808 | SOCKS5 代理 |
| V2Ray HTTP | 10809 | HTTP 代理 |
| AdGuard Home | 3000 | 广告过滤管理 |
| SmartDNS | 5353 | DNS 服务 |
| OpenWrt | 8080 | 路由器管理 |

---

## 🛠️ 常用命令

### Docker 软路由
```bash
# 启动服务
cd /vol1/1000/NasAppFiles/softrouter && ./start.sh

# 停止服务
cd /vol1/1000/NasAppFiles/softrouter && ./stop.sh

# 查看日志
docker logs v2ray
docker logs smartdns
docker logs adguardhome

# 重启服务
docker restart v2ray smartdns adguardhome
```

### OpenWrt 软路由
```bash
# 启动服务
cd /vol1/1000/NasAppFiles/openwrt && ./start.sh

# 停止服务
cd /vol1/1000/NasAppFiles/openwrt && ./stop.sh

# 进入容器
docker exec -it openwrt /bin/bash

# 安装软件包
docker exec openwrt opkg update
docker exec openwrt opkg install <package>
```

---

## 🔍 故障排查

### V2Ray 无法连接
```bash
# 检查 V2Ray 状态
docker ps | grep v2ray

# 查看日志
docker logs v2ray

# 测试代理
curl --socks5 127.0.0.1:10808 https://github.com
```

### DNS 解析失败
```bash
# 检查 SmartDNS 状态
docker ps | grep smartdns

# 测试 DNS
nslookup google.com 127.0.0.1#5353
```

### 广告过滤不生效
```bash
# 检查 AdGuard Home 状态
docker ps | grep adguardhome

# 访问管理界面
http://IP:3000
```

---

## 📱 客户端配置

### Windows
1. 下载 V2RayN 或 Clash
2. 配置 SOCKS5 代理: `IP:10808`
3. 或配置 HTTP 代理: `IP:10809`

### macOS
1. 下载 V2RayU 或 ClashX
2. 配置 SOCKS5 代理: `IP:10808`
3. 或配置 HTTP 代理: `IP:10809`

### iOS
1. 下载 Shadowrocket 或 Quantumult X
2. 添加 V2Ray 服务器
3. 配置智能分流规则

### Android
1. 下载 V2RayNG 或 Clash
2. 添加 V2Ray 服务器
3. 配置智能分流规则

---

## 🔐 安全建议

1. **修改默认密码**
   - OpenWrt: 修改 root 密码
   - AdGuard Home: 设置管理员密码

2. **启用 HTTPS**
   - 配置 SSL 证书
   - 使用 HTTPS 访问管理界面

3. **限制访问**
   - 配置防火墙规则
   - 限制管理界面访问 IP

4. **定期更新**
   - 更新 Docker 镜像
   - 更新 V2Ray 核心

---

## 📞 技术支持

如需帮助，请提供：
1. 使用的方案（Docker/OpenWrt）
2. 错误信息
3. 配置文件内容
4. 系统环境信息

## 🔗 相关资源

- [V2Ray 官方文档](https://www.v2ray.com/)
- [OpenWrt 官方网站](https://openwrt.org/)
- [SmartDNS](https://github.com/pymumu/smartdns)
- [AdGuard Home](https://adguard.com/adguard-home/overview.html)
