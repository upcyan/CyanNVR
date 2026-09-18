# 软路由创建完整操作指南

## 📋 前置条件

### 硬件要求
- **CPU**: 2核以上（推荐4核）
- **内存**: 2GB以上（推荐4GB）
- **存储**: 20GB以上
- **网络**: 至少1个网卡

### 软件要求
- Docker 已安装
- Docker Compose 已安装
- root 权限

### 网络信息
- 记录您的 VPS IP 地址
- 记录您的 VPS SSH 端口（默认22）
- 记录您的 VPS 用户名（默认root）

---

## 🚀 方案一：Docker 软路由（推荐）

### 步骤 1：准备 V2Ray 服务器信息

在执行部署前，您需要准备以下信息：

1. **VPS IP 地址**: 您的服务器 IP
2. **VPS 端口**: 通常是 443 或 8443
3. **UUID**: V2Ray 用户唯一标识
4. **域名**: 可选，用于 TLS 证书

### 步骤 2：执行部署脚本

```bash
cd /vol1/1000/deepseek_harness/SimpleNVR
sudo ./scripts/deploy_softrouter.sh
```

脚本会提示您输入：
- VPS IP 地址
- VPS 端口
- UUID
- 域名

### 步骤 3：启动服务

```bash
cd /vol1/1000/NasAppFiles/softrouter
./start.sh
```

### 步骤 4：验证服务

```bash
# 检查容器状态
docker ps | grep -E "v2ray|smartdns|adguardhome"

# 测试代理
curl --socks5 127.0.0.1:10808 https://github.com
```

### 步骤 5：配置客户端

在您的设备上配置代理：

**Windows/macOS**:
- SOCKS5 代理: `IP:10808`
- HTTP 代理: `IP:10809`

**iOS/Android**:
- 使用 V2Ray 客户端
- 添加服务器信息

---

## 🌐 方案二：OpenWrt 软路由

### 步骤 1：执行部署脚本

```bash
cd /vol1/1000/deepseek_harness/SimpleNVR
sudo ./scripts/deploy_openwrt.sh
```

### 步骤 2：启动 OpenWrt

```bash
cd /vol1/1000/NasAppFiles/openwrt
./start.sh
```

### 步骤 3：访问管理界面

1. 打开浏览器访问: `http://IP:8080`
2. 用户名: `root`
3. 密码: `password`

### 步骤 4：配置网络

1. 进入 **网络 → 接口**
2. 配置 **LAN** 口：
   - IP 地址: `192.168.1.1`（或其他网段）
   - 子网掩码: `255.255.255.0`

3. 配置 **WAN** 口：
   - 协议: 静态 IP
   - IP 地址: `192.168.1.100`（根据您的网络）
   - 网关: `192.168.1.1`
   - DNS: `8.8.8.8`

### 步骤 5：安装 V2Ray 插件

```bash
# 方法1: 使用脚本
./install_v2ray.sh

# 方法2: 手动安装
docker exec openwrt sh -c "
  opkg update
  opkg install v2ray-core
  opkg install luci-app-v2ray
  opkg install luci-i18n-v2ray-base-zh-cn
"
```

### 步骤 6：配置 V2Ray

1. 访问 LuCI 界面
2. 进入 **服务 → V2Ray**
3. 配置服务器信息：
   - 服务器地址: `YOUR_VPS_IP`
   - 端口: `443`
   - UUID: `YOUR_UUID`
   - 传输协议: `ws`
   - 路径: `/v2ray`
   - TLS: `启用`

4. 配置智能分流规则

### 步骤 7：测试连接

```bash
# 在 OpenWrt 容器中测试
docker exec openwrt curl --socks5 127.0.0.1:10808 https://github.com

# 在宿主机测试
curl --socks5 127.0.0.1:10808 https://github.com
```

---

## ⚙️ 智能分流规则配置

### 默认规则说明

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

### 规则优先级

1. **国内 IP**: 直连（geoip:cn）
2. **国内域名**: 直连（geosite:cn）
3. **国外常用网站**: 代理
4. **其他流量**: 代理

### 自定义规则

在 V2Ray 配置文件中添加自定义规则：

```json
{
  "type": "field",
  "domain": ["your-custom-domain.com"],
  "outboundTag": "direct"
}
```

---

## 🔧 高级配置

### 1. 配置 DNS

#### SmartDNS 配置
编辑 `/vol1/1000/NasAppFiles/softrouter/smartdns/smartdns.conf`:

```bash
# 国内 DNS
server 119.29.29.29 -group china
server 223.5.5.5 -group china

# 国外 DNS
server 8.8.8.8 -group bootstrap

# 域名规则
domain-rules /cn/ -address #china
domain-rules /google.com/ -address #bootstrap
```

#### AdGuard Home 配置
访问 `http://IP:3000` 进行配置：

1. 添加过滤规则
2. 配置 DNS 服务器
3. 设置访问控制

### 2. 配置防火墙

```bash
# 允许代理端口
iptables -A INPUT -p tcp --dport 10808 -j ACCEPT
iptables -A INPUT -p tcp --dport 10809 -j ACCEPT

# 允许 DNS
iptables -A INPUT -p udp --dport 5353 -j ACCEPT
```

### 3. 配置日志

```bash
# 查看 V2Ray 日志
docker logs -f v2ray

# 查看 SmartDNS 日志
docker logs -f smartdns

# 查看 AdGuard Home 日志
docker logs -f adguardhome
```

---

## 📊 服务状态检查

### 检查容器状态
```bash
docker ps | grep -E "v2ray|smartdns|adguardhome|openwrt"
```

### 检查端口监听
```bash
netstat -tlnp | grep -E "10808|10809|3000|5353|8080"
```

### 测试代理连接
```bash
# SOCKS5 代理
curl --socks5 127.0.0.1:10808 https://github.com

# HTTP 代理
curl -x http://127.0.0.1:10809 https://github.com
```

### 测试 DNS 解析
```bash
# SmartDNS
nslookup google.com 127.0.0.1#5353

# AdGuard Home
nslookup google.com 127.0.0.1#53
```

---

## 🛠️ 故障排查

### 问题 1: V2Ray 无法连接

**检查步骤**:
1. 检查 V2Ray 容器状态: `docker ps | grep v2ray`
2. 查看日志: `docker logs v2ray`
3. 测试 VPS 连接: `telnet VPS_IP 443`

**解决方案**:
- 检查 VPS 防火墙设置
- 检查 V2Ray 配置是否正确
- 检查网络连接

### 问题 2: DNS 解析失败

**检查步骤**:
1. 检查 SmartDNS 状态: `docker ps | grep smartdns`
2. 测试 DNS: `nslookup google.com 127.0.0.1#5353`

**解决方案**:
- 检查 SmartDNS 配置
- 检查防火墙规则
- 重启 SmartDNS 容器

### 问题 3: 广告过滤不生效

**检查步骤**:
1. 检查 AdGuard Home 状态: `docker ps | grep adguardhome`
2. 访问管理界面: `http://IP:3000`

**解决方案**:
- 检查过滤规则是否启用
- 检查 DNS 配置
- 清除浏览器缓存

---

## 📱 客户端配置

### Windows (V2RayN)

1. 下载 V2RayN
2. 添加服务器:
   - 地址: `YOUR_VPS_IP`
   - 端口: `443`
   - UUID: `YOUR_UUID`
   - 传输协议: `ws`
   - 路径: `/v2ray`
   - TLS: `tls`

3. 启用系统代理

### macOS (V2RayU)

1. 下载 V2RayU
2. 添加服务器
3. 配置分流规则

### iOS (Shadowrocket)

1. 下载 Shadowrocket
2. 添加 V2Ray 服务器
3. 配置分流规则

### Android (V2RayNG)

1. 下载 V2RayNG
2. 添加 V2Ray 服务器
3. 配置分流规则

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
