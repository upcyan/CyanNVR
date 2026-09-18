# 软路由创建视频教程（文字版）

## 🎬 第一集：环境准备

### 1.1 检查系统环境
```bash
# 检查 Docker
docker --version
docker info

# 检查 Docker Compose
docker-compose --version
# 或
docker compose version

# 检查磁盘空间
df -h

# 检查内存
free -h
```

### 1.2 安装 Docker（如果未安装）
```bash
# Ubuntu/Debian
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh

# CentOS/RHEL
sudo yum install -y docker-ce docker-ce-cli containerd.io

# 启动 Docker
sudo systemctl start docker
sudo systemctl enable docker
```

### 1.3 安装 Docker Compose（如果未安装）
```bash
# 下载 Docker Compose
sudo curl -L "https://github.com/docker/compose/releases/latest/download/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose

# 添加执行权限
sudo chmod +x /usr/local/bin/docker-compose

# 验证安装
docker-compose --version
```

---

## 🎬 第二集：V2Ray 服务器准备

### 2.1 购买 VPS
推荐服务商：
- **Vultr**: https://www.vultr.com/
- **DigitalOcean**: https://www.digitalocean.com/
- **Linode**: https://www.linode.com/
- **阿里云**: https://www.aliyun.com/

### 2.2 安装 V2Ray 服务端
```bash
# SSH 登录 VPS
ssh root@YOUR_VPS_IP

# 一键安装 V2Ray
bash <(curl -L https://raw.githubusercontent.com/v2fly/fhs-install-v2ray/master/install-release.sh)

# 生成 UUID
cat /proc/sys/kernel/random/uuid
```

### 2.3 配置 V2Ray 服务端
```bash
# 编辑配置文件
nano /usr/local/etc/v2ray/config.json
```

配置内容：
```json
{
  "inbounds": [
    {
      "port": 443,
      "protocol": "vmess",
      "settings": {
        "clients": [
          {
            "id": "YOUR_UUID",
            "alterId": 0
          }
        ]
      },
      "streamSettings": {
        "network": "ws",
        "security": "tls",
        "wsSettings": {
          "path": "/v2ray"
        },
        "tlsSettings": {
          "certificates": [
            {
              "certificateFile": "/path/to/cert.pem",
              "keyFile": "/path/to/key.pem"
            }
          ]
        }
      }
    }
  ],
  "outbounds": [
    {
      "protocol": "freedom",
      "tag": "direct"
    },
    {
      "protocol": "blackhole",
      "tag": "block"
    }
  ]
}
```

### 2.4 启动 V2Ray 服务
```bash
# 启动服务
systemctl start v2ray

# 设置开机启动
systemctl enable v2ray

# 检查状态
systemctl status v2ray
```

---

## 🎬 第三集：部署软路由

### 3.1 下载部署脚本
```bash
# 下载项目
git clone https://github.com/upcyan/SimpleNVR.git
cd SimpleNVR/scripts
```

### 3.2 执行部署脚本
```bash
# 方案一：Docker 软路由（推荐）
sudo ./deploy_softrouter.sh

# 方案二：OpenWrt 软路由
sudo ./deploy_openwrt.sh
```

### 3.3 输入服务器信息
脚本会提示您输入：
- VPS IP 地址
- VPS 端口（默认 443）
- UUID
- 域名（可选）

### 3.4 启动服务
```bash
# 进入配置目录
cd /vol1/1000/NasAppFiles/softrouter

# 启动服务
./start.sh
```

---

## 🎬 第四集：客户端配置

### 4.1 Windows 配置 (V2RayN)

1. **下载 V2RayN**
   - 访问: https://github.com/2dust/v2rayN/releases
   - 下载最新版本

2. **安装并运行**
   - 解压下载的文件
   - 运行 `v2rayN.exe`

3. **添加服务器**
   - 右键托盘图标 → 添加 VMess 服务器
   - 填写服务器信息：
     - 地址: `YOUR_VPS_IP`
     - 端口: `443`
     - UUID: `YOUR_UUID`
     - 传输协议: `ws`
     - 路径: `/v2ray`
     - TLS: `tls`

4. **启用代理**
   - 右键托盘图标 → 系统代理 → 自动配置系统代理

### 4.2 macOS 配置 (V2RayU)

1. **下载 V2RayU**
   - 访问: https://github.com/yanue/V2rayU/releases
   - 下载最新版本

2. **安装并运行**
   - 双击安装包
   - 拖动到应用程序文件夹

3. **添加服务器**
   - 点击菜单栏图标 → 服务器设置
   - 添加 VMess 服务器
   - 填写服务器信息

4. **启用代理**
   - 点击菜单栏图标 → 开启代理

### 4.3 iOS 配置 (Shadowrocket)

1. **下载 Shadowrocket**
   - 需要海外 Apple ID
   - App Store 搜索 "Shadowrocket"

2. **添加服务器**
   - 点击右上角 +
   - 选择 V2Ray
   - 填写服务器信息

3. **启用代理**
   - 点击开关启用

### 4.4 Android 配置 (V2RayNG)

1. **下载 V2RayNG**
   - 访问: https://github.com/2dust/v2rayNG/releases
   - 下载 APK 安装

2. **添加服务器**
   - 点击右上角 +
   - 选择从剪贴板导入
   - 填写服务器信息

3. **启用代理**
   - 点击开关启用

---

## 🎬 第五集：验证和测试

### 5.1 测试代理连接
```bash
# 测试 SOCKS5 代理
curl --socks5 127.0.0.1:10808 https://github.com

# 测试 HTTP 代理
curl -x http://127.0.0.1:10809 https://github.com
```

### 5.2 测试 DNS 解析
```bash
# 测试 SmartDNS
nslookup google.com 127.0.0.1#5353

# 测试 AdGuard Home
nslookup google.com 127.0.0.1#53
```

### 5.3 测试广告过滤
1. 访问 AdGuard Home 管理界面: `http://IP:3000`
2. 检查过滤规则
3. 访问广告网站测试

### 5.4 测试智能分流
1. 访问国内网站（如 baidu.com）→ 应直连
2. 访问国外网站（如 google.com）→ 应代理
3. 检查连接速度

---

## 🎬 第六集：高级配置

### 6.1 配置自定义分流规则
编辑 V2Ray 配置文件，添加自定义规则：

```json
{
  "type": "field",
  "domain": ["your-custom-domain.com"],
  "outboundTag": "direct"
}
```

### 6.2 配置 DNS 轮询
编辑 SmartDNS 配置：

```bash
# 添加多个 DNS 服务器
server 8.8.8.8
server 8.8.4.4
server 1.1.1.1
```

### 6.3 配置 AdGuard Home 过滤规则
1. 访问管理界面
2. 过滤器 → DNS 黑名单
3. 添加自定义规则

### 6.4 配置防火墙
```bash
# 允许代理端口
iptables -A INPUT -p tcp --dport 10808 -j ACCEPT
iptables -A INPUT -p tcp --dport 10809 -j ACCEPT

# 允许 DNS
iptables -A INPUT -p udp --dport 5353 -j ACCEPT
```

---

## 🎬 第七集：故障排查

### 7.1 检查服务状态
```bash
# 检查容器状态
docker ps | grep -E "v2ray|smartdns|adguardhome"

# 检查端口监听
netstat -tlnp | grep -E "10808|10809|3000|5353"

# 检查日志
docker logs v2ray
docker logs smartdns
docker logs adguardhome
```

### 7.2 常见问题解决

**问题 1: V2Ray 无法连接**
```bash
# 检查 VPS 连接
telnet VPS_IP 443

# 检查防火墙
iptables -L -n | grep 443
```

**问题 2: DNS 解析失败**
```bash
# 检查 SmartDNS
dig @127.0.0.1 -p 5353 google.com

# 检查 AdGuard Home
dig @127.0.0.1 google.com
```

**问题 3: 广告过滤不生效**
1. 检查过滤规则是否启用
2. 清除浏览器缓存
3. 检查 DNS 配置

---

## 🎬 第八集：性能优化

### 8.1 优化 DNS 解析
```bash
# 编辑 SmartDNS 配置
nano /vol1/1000/NasAppFiles/softrouter/smartdns/smartdns.conf

# 添加缓存设置
cache-size 10000
cache-persist yes
```

### 8.2 优化网络连接
```bash
# 调整内核参数
sysctl -w net.core.rmem_max=16777216
sysctl -w net.core.wmem_max=16777216
sysctl -w net.ipv4.tcp_rmem="4096 87380 16777216"
sysctl -w net.ipv4.tcp_wmem="4096 65536 16777216"
```

### 8.3 监控资源使用
```bash
# 查看容器资源使用
docker stats

# 查看系统资源
htop
```

---

## 📚 相关资源

- [V2Ray 官方文档](https://www.v2ray.com/)
- [OpenWrt 官方网站](https://openwrt.org/)
- [SmartDNS](https://github.com/pymumu/smartdns)
- [AdGuard Home](https://adguard.com/adguard-home/overview.html)
- [Docker 官方文档](https://docs.docker.com/)

---

## 🎯 总结

通过本教程，您已经学会了：

1. ✅ 环境准备和软件安装
2. ✅ V2Ray 服务器配置
3. ✅ 软路由部署
4. ✅ 客户端配置
5. ✅ 验证和测试
6. ✅ 高级配置
7. ✅ 故障排查
8. ✅ 性能优化

**现在您可以开始使用软路由 + V2Ray 智能分流了！** 🚀
