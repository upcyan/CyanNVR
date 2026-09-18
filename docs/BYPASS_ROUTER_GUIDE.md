# 旁路由配置完整指南

## 📋 什么是旁路由？

旁路由（Bypass Router）是在不改变现有网络结构的情况下，为特定设备或全部设备提供代理服务的路由设备。

### 优点
- ✅ 不影响主路由功能
- ✅ 可随时关闭或移除
- ✅ 配置灵活
- ✅ 故障隔离

### 缺点
- ⚠️ 需要额外设备
- ⚠️ 网络拓扑稍复杂

---

## 🚀 方案一：全局代理（推荐）

### 步骤 1：部署软路由

```bash
cd /vol1/1000/deepseek_harness/SimpleNVR
sudo ./deploy_softrouter_bypass.sh
```

选择 **旁路由模式**，输入网络信息：
- 旁路由 IP: `192.168.1.2`（根据你的网络）
- 子网掩码: `255.255.255.0`
- 网关: `192.168.1.1`（主路由 IP）
- DNS: `8.8.8.8`

### 步骤 2：启动服务

```bash
cd /vol1/1000/NasAppFiles/softrouter
./start.sh
```

### 步骤 3：配置主路由

#### 方法 A：DHCP 全局设置（推荐）

1. 登录主路由管理界面
2. 找到 DHCP 设置
3. 修改 DHCP 选项：
   - **网关**: 设置为旁路由 IP (`192.168.1.2`)
   - **DNS**: 设置为旁路由 IP (`192.168.1.2`)

4. 保存并重启主路由

#### 方法 B：设备手动设置

在需要代理的设备上手动设置：
- **HTTP 代理**: `192.168.1.2:10809`
- **SOCKS5 代理**: `192.168.1.2:10808`

### 步骤 4：验证

```bash
# 测试代理连接
curl --socks5 192.168.1.2:10808 https://github.com

# 测试 DNS 解析
nslookup google.com 192.168.1.2#5353
```

---

## 🌐 方案二：指定设备代理

### 步骤 1：部署软路由

同方案一，但不需要配置主路由 DHCP。

### 步骤 2：设备配置

#### Windows
1. 打开 **设置** → **网络和 Internet** → **代理**
2. 手动设置代理：
   - 地址: `192.168.1.2`
   - 端口: `10809`

#### macOS
1. 打开 **系统偏好设置** → **网络**
2. 选择网络连接 → **高级** → **代理**
3. 勤打 **Web 代理 (HTTP)** 和 **安全 Web 代理 (HTTPS)**
4. 输入代理地址和端口

#### iOS
1. 打开 **设置** → **Wi-Fi**
2. 点击当前 Wi-Fi 旁边的 **i** 图标
3. 滑动到底部，点击 **配置代理**
4. 选择 **手动**
5. 输入代理地址和端口

#### Android
1. 打开 **设置** → **网络和 Internet** → **Wi-Fi**
2. 长按当前 Wi-Fi 网络 → **修改网络**
3. 展开 **高级选项** → **代理**
4. 选择 **手动**
5. 输入代理地址和端口

---

## 🔧 方案三：透明代理（高级）

### 步骤 1：部署软路由

同方案一。

### 步骤 2：配置透明代理

在软路由上配置 iptables 规则：

```bash
# 启用 IP 转发
echo 1 > /proc/sys/net/ipv4/ip_forward

# 配置 iptables 规则
iptables -t nat -A PREROUTING -i eth0 -p tcp -j REDIRECT --to-ports 10808
iptables -t nat -A PREROUTING -i eth0 -p udp -j REDIRECT --to-ports 10808
```

### 步骤 3：配置路由

在主路由上添加静态路由：
- 目标网络: `0.0.0.0/0`
- 下一跳: 旁路由 IP

---

## 📊 网络拓扑

### 旁路由模式
```
互联网
   │
主路由 (192.168.1.1)
   │
   ├── 设备 1 (DHCP → 旁路由)
   ├── 设备 2 (DHCP → 旁路由)
   └── 旁路由 (192.168.1.2)
          │
          ├── Xray (代理服务)
          ├── SmartDNS (DNS 加速)
          └── AdGuard Home (广告过滤)
```

### 指定设备代理模式
```
互联网
   │
主路由 (192.168.1.1)
   │
   ├── 设备 1 (直连)
   ├── 设备 2 (直连)
   └── 设备 3 (代理 → 旁路由)
          │
          └── 旁路由 (192.168.1.2)
```

---

## ⚙️ 服务配置

### 端口说明

| 服务 | 端口 | 用途 |
|------|------|------|
| SOCKS5 代理 | 10808 | SOCKS5 代理 |
| HTTP 代理 | 10809 | HTTP 代理 |
| AdGuard Home | 3000 | 广告过滤管理 |
| SmartDNS | 5353 | DNS 服务 |

### 分流规则

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
      }
    ]
  }
}
```

---

## 🛠️ 故障排查

### 问题 1: 代理无法连接

**检查步骤**:
1. 检查容器状态: `docker ps | grep xray`
2. 查看日志: `docker logs xray`
3. 测试本地连接: `curl --socks5 127.0.0.1:10808 https://github.com`

**解决方案**:
- 检查防火墙设置
- 检查配置是否正确
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
- 清除浏览器缓存
- 检查 DNS 配置

---

## 🔐 安全建议

1. **修改默认密码**
   - AdGuard Home: 设置管理员密码

2. **限制访问**
   - 配置防火墙规则
   - 限制管理界面访问 IP

3. **定期更新**
   - 更新 Docker 镜像
   - 更新代理核心

---

## 📚 更多文档

- [智能软路由部署说明](SMART_SOFTRouter_README.md)
- [软路由创建指南](SOFTRouter_CREATE_GUIDE.md)
- [视频教程文字版](SOFTRouter_VIDEO_TUTORIAL.md)

---

## 🎯 总结

✅ **旁路由模式**: 不影响现有网络，灵活配置
✅ **全局代理**: 所有设备自动使用代理
✅ **指定设备代理**: 仅为特定设备提供代理
✅ **透明代理**: 高级配置，无需手动设置

**现在您可以开始配置旁路由了！** 🚀
