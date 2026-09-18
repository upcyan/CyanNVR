# VPS 代理快速开始

## 🚀 一键部署（推荐）

### 步骤 1：在 VPS 上执行
```bash
# 下载脚本到 VPS
scp vps_proxy_setup.sh root@你的VPS_IP:/root/

# SSH 登录 VPS
ssh root@你的VPS_IP

# 执行脚本
chmod +x vps_proxy_setup.sh
./vps_proxy_setup.sh
```

### 步骤 2：添加 SSH 公钥到 GitHub
```bash
# 复制公钥
cat ~/.ssh/id_ed25519.pub

# 访问 https://github.com/settings/keys
# 点击 "New SSH key"
# 粘贴公钥
```

### 步骤 3：推送代码
```bash
cd /vol1/1000/deepseek_harness/SimpleNVR
git push origin main
```

---

## 📋 方案选择

### 方案 1：SSH 隧道（最简单）
**适合**: 个人使用，快速部署
```bash
# 在 VPS 上执行
sudo ./vps_proxy_setup.sh
```

### 方案 2：HTTP 代理
**适合**: 团队使用，多设备共享
```bash
# 在 VPS 上执行
sudo ./vps_http_proxy.sh
```

### 方案 3：SOCKS5 代理
**适合**: 高性能需求，大文件传输
```bash
# 在 VPS 上执行
sudo ./vps_socks5_proxy.sh
```

### 方案 4：一键选择
```bash
# 自动选择最佳方案
sudo ./deploy_proxy.sh
```

---

## 🔧 配置 Git 代理

### 使用 HTTP 代理
```bash
git config --global http.proxy http://VPS_IP:3128
git config --global https.proxy http://VPS_IP:3128
```

### 使用 SOCKS5 代理
```bash
git config --global http.proxy socks5://127.0.0.1:1080
git config --global https.proxy socks5://127.0.0.1:1080
```

### 移除代理配置
```bash
git config --global --unset http.proxy
git config --global --unset https.proxy
```

---

## 🧪 测试代理连接

### 测试 HTTP 代理
```bash
curl -x http://VPS_IP:3128 https://github.com
```

### 测试 SOCKS5 代理
```bash
curl --socks5 127.0.0.1:1080 https://github.com
```

### 测试 SSH 连接
```bash
ssh -T git@github.com
```

---

## 🚀 推送代码

### 方法 1：直接推送
```bash
cd /vol1/1000/deepseek_harness/SimpleNVR
git push origin main
```

### 方法 2：使用脚本
```bash
cd /vol1/1000/deepseek_harness/SimpleNVR
./EXECUTE_PUSH.sh
```

### 方法 3：使用令牌
```bash
cd /vol1/1000/deepseek_harness/SimpleNVR
./push_with_token.sh <YOUR_GITHUB_TOKEN>
```

---

## 📊 性能对比

| 方案 | 延迟 | 吞吐量 | 配置难度 | 安全性 |
|------|------|--------|----------|--------|
| SSH 隧道 | 低 | 高 | 简单 | 高 |
| HTTP 代理 | 中 | 中 | 中等 | 中 |
| SOCKS5 代理 | 低 | 高 | 简单 | 高 |

---

## ❓ 常见问题

### Q: 推送超时？
A: 检查代理是否正常运行，测试连接：
```bash
curl --socks5 127.0.0.1:1080 https://github.com
```

### Q: 权限被拒绝？
A: 检查 SSH 密钥是否添加到 GitHub：
```bash
ssh -vT git@github.com
```

### Q: 如何停止代理？
```bash
# 停止 SOCKS5 代理
pkill -f 'ssh -D 1080'

# 停止 HTTP 代理
sudo systemctl stop squid
```

---

## 📚 更多信息

- [VPS 代理完整指南](VPS_PROXY_GUIDE.md)
- [GitHub 推送指南](PUSH_GUIDE.md)
- [最终状态报告](FINAL_STATUS.md)

---

## 🎯 快速开始

1. **选择方案**: SSH 隧道（最简单）
2. **在 VPS 执行**: `sudo ./vps_proxy_setup.sh`
3. **添加公钥到 GitHub**
4. **推送代码**: `git push origin main`

**就是这么简单！** 🚀
