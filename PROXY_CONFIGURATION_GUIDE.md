# 代理配置完整指南

## 📋 当前状态

### ✅ 已完成
1. **Xray 代理服务** - 运行正常
2. **代理端口** - 10808 (SOCKS5), 10809 (HTTP)
3. **基本代理功能** - 可以连接 GitHub

### ⚠️ 需要配置
1. **代理服务器信息** - 需要配置真实的代理服务器
2. **分流规则** - 需要配置智能分流
3. **Docker 代理** - 需要配置 Docker daemon 代理

---

## 🚀 快速配置

### 步骤 1：配置代理服务器

编辑 `/vol1/1000/NasAppFiles/softrouter/xray/config.json`，添加代理服务器信息：

```json
{
  "outbounds": [
    {
      "protocol": "freedom",
      "tag": "direct"
    },
    {
      "protocol": "blackhole",
      "tag": "block"
    },
    {
      "protocol": "vless",
      "settings": {
        "vnext": [
          {
            "address": "YOUR_VPS_IP",
            "port": 443,
            "users": [
              {
                "id": "YOUR_UUID",
                "encryption": "none"
              }
            ]
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
          "serverName": "YOUR_DOMAIN.com"
        }
      },
      "tag": "proxy"
    }
  ]
}
```

### 步骤 2：重启 Xray

```bash
docker restart xray
```

### 步骤 3：测试代理

```bash
# 测试 SOCKS5 代理
curl --socks5 192.168.68.51:10808 https://github.com

# 测试 HTTP 代理
curl -x http://192.168.68.51:10809 https://github.com
```

---

## 🔧 完整配置

### 1. 配置 Docker 代理

```bash
# 执行配置脚本
sudo ./configure_all_proxy.sh

# 或手动配置
sudo mkdir -p /etc/systemd/system/docker.service.d
sudo tee /etc/systemd/system/docker.service.d/http-proxy.conf << EOF
[Service]
Environment="HTTP_PROXY=http://192.168.68.51:10809"
Environment="HTTPS_PROXY=http://192.168.68.51:10809"
Environment="NO_PROXY=localhost,127.0.0.1,192.168.0.0/16"
EOF

sudo systemctl daemon-reload
sudo systemctl restart docker
```

### 2. 配置 Git 代理

```bash
# 配置 Git
git config --global http.proxy http://192.168.68.51:10809
git config --global https.proxy http://192.168.68.51:10809

# 测试 Git 代理
git ls-remote https://github.com/octocat/Hello-World.git
```

### 3. 配置环境变量

```bash
# 添加到 ~/.bashrc
cat >> ~/.bashrc << EOF
export http_proxy="http://192.168.68.51:10809"
export https_proxy="http://192.168.68.51:10809"
export all_proxy="socks5://192.168.68.51:10808"
export no_proxy="localhost,127.0.0.1,192.168.0.0/16"
EOF

# 重新加载
source ~/.bashrc
```

---

## 📊 测试结果

### 当前测试
```
✅ GitHub - 成功
❌ Google - 失败
❌ YouTube - 失败
❌ Telegram - 失败
```

### 原因分析
1. **代理服务器未配置** - Xray 只有入站，没有出站代理
2. **分流规则未生效** - 需要配置真实的代理服务器

### 解决方案
1. **配置代理服务器** - 在 Xray 配置中添加真实的代理服务器
2. **重启 Xray** - 应用新配置
3. **测试连接** - 验证代理是否正常

---

## 🎯 推荐配置

### 方案一：使用现有代理服务器
如果您已经有代理服务器（如 VPS），配置 Xray 连接到该服务器。

### 方案二：使用本地代理
如果本地有代理软件（如 Clash、V2RayN），配置 Xray 连接到本地代理。

### 方案三：直连模式
如果不需要代理，可以配置 Xray 只提供本地代理服务。

---

## 📚 更多文档

- [旁路由配置完整指南](docs/BYPASS_ROUTER_GUIDE.md)
- [智能软路由部署说明](docs/SMART_SOFTRouter_README.md)
- [软路由状态报告](SOFTRouter_STATUS.md)

---

## 🎯 总结

✅ **代理服务已运行** - 端口 10808/10809
✅ **基本功能正常** - 可以连接 GitHub
⚠️ **需要配置代理服务器** - 添加真实的代理服务器信息
🚀 **重启后生效** - 配置完成后重启 Xray

**现在您可以开始配置代理服务器了！** 🚀
