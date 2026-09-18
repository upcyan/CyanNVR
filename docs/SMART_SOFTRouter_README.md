# 智能软路由部署说明

## 🎯 功能特性

### ✅ 支持的协议
- **VLESS** - 最新协议，性能优秀
- **VMESS** - 经典协议，兼容性好
- **Shadowsocks** - 简单高效
- **Trojan** - 伪装 TLS 流量

### ✅ 支持的传输方式
- **WebSocket (ws)** - 推荐，兼容性好
- **gRPC** - 高性能，低延迟
- **TCP** - 基础传输
- **HTTP/2** - 伪装 HTTPS 流量

### ✅ 智能分流规则
- **国内 IP**: 直连（geoip:cn）
- **国内域名**: 直连（geosite:cn）
- **国外常用网站**: 代理（Google、YouTube、GitHub、Telegram 等）
- **其他流量**: 代理

### ✅ 代理核心选择
- **Xray** - 推荐，功能强大，支持更多协议
- **Sing-box** - 轻量级，性能优秀
- **V2Ray** - 经典版本，稳定可靠

---

## 🚀 快速开始

### 一键部署（推荐）

```bash
cd /vol1/1000/deepseek_harness/SimpleNVR
sudo ./scripts/deploy_smart_softrouter.sh
```

### 步骤说明

1. **选择代理核心**
   ```
   1. Xray（推荐）
   2. Sing-box
   3. V2Ray
   ```

2. **选择配置方式**
   ```
   1. 手动输入服务器信息
   2. 导入配置链接（推荐）
   3. 导入配置文件
   ```

3. **输入配置信息**
   - 如果选择配置链接，直接粘贴链接
   - 如果手动输入，按提示填写服务器信息

4. **启动服务**
   ```bash
   cd /vol1/1000/NasAppFiles/softrouter
   ./start.sh
   ```

---

## 📋 配置链接格式

### VLESS 配置示例
```
vless://uuid@server:port?type=ws&security=tls&sni=domain.com&path=/v2ray#名称
```

### VMESS 配置示例
```
vmess://base64_encoded_json
```

### Shadowsocks 配置示例
```
ss://base64_encoded_method:password@server:port#名称
```

### Trojan 配置示例
```
trojan://password@server:port?security=tls&sni=domain.com#名称
```

---

## 🔧 手动配置

### 1. 选择代理核心

编辑 `/vol1/1000/NasAppFiles/softrouter/start.sh`，设置 `CORE` 变量：

```bash
CORE="xray"  # 或 "singbox" 或 "v2ray"
```

### 2. 配置服务器信息

编辑对应的配置文件：

#### Xray 配置
```bash
nano /vol1/1000/NasAppFiles/softrouter/xray/config.json
```

#### Sing-box 配置
```bash
nano /vol1/1000/NasAppFiles/softrouter/singbox/config.json
```

### 3. 配置分流规则

在配置文件的 `routing.rules` 中添加自定义规则：

```json
{
  "type": "field",
  "domain": ["your-custom-domain.com"],
  "outboundTag": "direct"
}
```

---

## 📊 服务端口

| 服务 | 端口 | 用途 |
|------|------|------|
| SOCKS5 代理 | 10808 | SOCKS5 代理 |
| HTTP 代理 | 10809 | HTTP 代理 |
| AdGuard Home | 3000 | 广告过滤管理 |
| SmartDNS | 5353 | DNS 服务 |

---

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

---

## ✅ 验证服务

### 1. 检查容器状态
```bash
docker ps | grep -E "xray|singbox|v2ray|smartdns|adguardhome"
```

### 2. 测试代理连接
```bash
# SOCKS5 代理
curl --socks5 127.0.0.1:10808 https://github.com

# HTTP 代理
curl -x http://127.0.0.1:10809 https://github.com
```

### 3. 测试 DNS 解析
```bash
# SmartDNS
nslookup google.com 127.0.0.1#5353

# AdGuard Home
nslookup google.com 127.0.0.1#53
```

### 4. 测试广告过滤
1. 访问 AdGuard Home 管理界面: `http://IP:3000`
2. 检查过滤规则
3. 访问广告网站测试

---

## 🛠️ 故障排查

### 问题 1: 代理无法连接

**检查步骤**:
1. 检查容器状态: `docker ps | grep xray`
2. 查看日志: `docker logs xray`
3. 测试 VPS 连接: `telnet VPS_IP 443`

**解决方案**:
- 检查 VPS 防火墙设置
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

2. **启用 HTTPS**
   - 配置 SSL 证书
   - 使用 HTTPS 访问管理界面

3. **限制访问**
   - 配置防火墙规则
   - 限制管理界面访问 IP

4. **定期更新**
   - 更新 Docker 镜像
   - 更新代理核心

---

## 📚 更多文档

- [软路由创建指南](SOFTRouter_CREATE_GUIDE.md)
- [视频教程](SOFTRouter_VIDEO_TUTORIAL.md)
- [快速开始卡片](QUICK_START_CARD.md)

---

## 🎯 总结

✅ **支持多种协议**: VLESS/VMESS/Shadowsocks/Trojan
✅ **支持多种传输**: WebSocket/gRPC/TCP/HTTP/2
✅ **智能分流策略**: 国内直连，国外代理
✅ **一键部署**: 简单快捷
✅ **广告过滤**: AdGuard Home
✅ **DNS 加速**: SmartDNS

**现在您可以开始使用智能软路由了！** 🚀
