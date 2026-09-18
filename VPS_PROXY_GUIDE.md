# VPS 代理连接 GitHub 完整指南

## 📋 方案概览

提供三种通过 VPS 代理连接 GitHub 的方案：

| 方案 | 难度 | 速度 | 安全性 | 适用场景 |
|------|------|------|--------|----------|
| **SSH 隧道** | ⭐⭐ | 🚀🚀🚀 | 🔒🔒🔒 | 推荐方案，最简单 |
| **HTTP 代理** | ⭐⭐⭐ | 🚀🚀 | 🔒🔒 | 多设备共享代理 |
| **SOCKS5 代理** | ⭐⭐ | 🚀🚀🚀 | 🔒🔒🔒 | 高性能需求 |

---

## 🚀 方案一：SSH 隧道（推荐）

### 优点
- ✅ 最简单，无需安装额外软件
- ✅ 加密传输，安全性高
- ✅ 自动配置，一键完成

### 搭建步骤

#### 1. 在 VPS 上准备
```bash
# 确保 VPS 有 SSH 服务
sudo systemctl status sshd

# 记录 VPS 信息
VPS_IP="你的VPS_IP"
VPS_PORT="22"
VPS_USER="root"
```

#### 2. 在本地执行
```bash
# 下载并执行脚本
chmod +x vps_proxy_setup.sh
sudo ./vps_proxy_setup.sh
```

#### 3. 添加 SSH 公钥到 GitHub
```bash
# 复制公钥
cat ~/.ssh/id_ed25519.pub

# 访问 https://github.com/settings/keys
# 点击 "New SSH key"
# 粘贴公钥
```

#### 4. 测试连接
```bash
ssh -T git@github.com
# 应显示: "Hi username! You've successfully authenticated"
```

#### 5. 推送代码
```bash
cd /vol1/1000/deepseek_harness/SimpleNVR
git push origin main
```

---

## 🌐 方案二：HTTP 代理

### 优点
- ✅ 多设备共享代理
- ✅ 可配置访问控制
- ✅ 适合团队使用

### 搭建步骤

#### 1. 在 VPS 上执行
```bash
chmod +x vps_http_proxy.sh
sudo ./vps_http_proxy.sh
```

#### 2. 记录代理信息
```bash
# 脚本会显示代理地址，例如：
# 代理地址: 192.168.1.100:3128
```

#### 3. 在本地配置 Git
```bash
# 替换为你的 VPS IP
git config --global http.proxy http://192.168.1.100:3128
git config --global https.proxy http://192.168.1.100:3128
```

#### 4. 测试代理
```bash
curl -x http://192.168.1.100:3128 https://github.com
```

#### 5. 推送代码
```bash
cd /vol1/1000/deepseek_harness/SimpleNVR
git push origin main
```

---

## 🔌 方案三：SOCKS5 代理

### 优点
- ✅ 高性能，低延迟
- ✅ 支持所有协议
- ✅ 适合大文件传输

### 搭建步骤

#### 1. 执行脚本
```bash
chmod +x vps_socks5_proxy.sh
sudo ./vps_socks5_proxy.sh
```

#### 2. 按提示输入 VPS 信息

#### 3. 推送代码
```bash
cd /vol1/1000/deepseek_harness/SimpleNVR
git push origin main
```

---

## 🔧 常见问题解决

### Q: 推送超时？
```bash
# 检查代理是否正常
curl --socks5 127.0.0.1:1080 https://github.com

# 检查 SSH 连接
ssh -T git@github.com

# 检查 Git 配置
git config --global --list | grep proxy
```

### Q: 权限被拒绝？
```bash
# 检查 SSH 密钥
ssh-add -l

# 测试 SSH 连接
ssh -vT git@github.com
```

### Q: 如何停止代理？
```bash
# 停止 SOCKS5 代理
pkill -f 'ssh -D 1080'

# 停止 HTTP 代理
sudo systemctl stop squid
```

### Q: 如何查看代理日志？
```bash
# HTTP 代理日志
sudo tail -f /var/log/squid/access.log

# SSH 连接日志
ssh -vT git@github.com
```

---

## 📊 性能对比

| 指标 | SSH 隧道 | HTTP 代理 | SOCKS5 代理 |
|------|----------|-----------|-------------|
| 延迟 | 低 | 中 | 低 |
| 吞吐量 | 高 | 中 | 高 |
| 配置难度 | 简单 | 中等 | 简单 |
| 安全性 | 高 | 中 | 高 |
| 多设备支持 | 否 | 是 | 否 |

---

## 🎯 推荐方案

### 个人使用
**推荐：SSH 隧道**
- 最简单，一键配置
- 安全性高
- 无需额外软件

### 团队使用
**推荐：HTTP 代理**
- 多设备共享
- 可配置访问控制
- 适合企业环境

### 高性能需求
**推荐：SOCKS5 代理**
- 低延迟
- 高吞吐量
- 适合大文件传输

---

## 📞 技术支持

如需帮助，请提供：
1. 使用的代理方案
2. VPS 操作系统信息
3. 错误信息
4. 网络环境

## 🔗 相关资源

- [GitHub SSH 密钥配置](https://docs.github.com/en/authentication/connecting-to-github-with-ssh)
- [Git 代理配置](https://git-scm.com/docs/git-config)
- [Squid 代理服务器](http://www.squid-cache.org/)
