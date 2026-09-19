#!/bin/bash

# 自动配置所有代理并测试

echo "=== 自动配置所有代理 ==="

# 获取宿主机 IP
HOST_IP=$(hostname -I | awk '{print $1}')
echo "宿主机 IP: $HOST_IP"

# 1. 配置 Docker 代理
echo ""
echo "1. 配置 Docker 代理..."

# 创建 Docker 代理配置目录
sudo mkdir -p /etc/systemd/system/docker.service.d

# 创建 HTTP 代理配置
sudo tee /etc/systemd/system/docker.service.d/http-proxy.conf > /dev/null << EOF
[Service]
Environment="HTTP_PROXY=http://${HOST_IP}:10809"
Environment="HTTPS_PROXY=http://${HOST_IP}:10809"
Environment="NO_PROXY=localhost,127.0.0.1,192.168.0.0/16,10.0.0.0/8,172.16.0.0/12"
EOF

# 创建 SOCKS5 代理配置
sudo tee /etc/systemd/system/docker.service.d/socks-proxy.conf > /dev/null << EOF
[Service]
Environment="ALL_PROXY=socks5://${HOST_IP}:10808"
EOF

echo "✅ Docker 代理配置完成"

# 2. 配置 Git 代理
echo ""
echo "2. 配置 Git 代理..."

git config --global http.proxy "http://${HOST_IP}:10809"
git config --global https.proxy "http://${HOST_IP}:10809"
git config --global http socks.proxy "socks5://${HOST_IP}:10808"

echo "✅ Git 代理配置完成"

# 3. 配置环境变量
echo ""
echo "3. 配置环境变量..."

# 创建代理环境变量文件
cat > /tmp/proxy_env.sh << EOF
export http_proxy="http://${HOST_IP}:10809"
export https_proxy="http://${HOST_IP}:10809"
export all_proxy="socks5://${HOST_IP}:10808"
export no_proxy="localhost,127.0.0.1,192.168.0.0/16,10.0.0.0/8,172.16.0.0/12"
EOF

# 添加到 bashrc（如果不存在）
if ! grep -q "proxy_env.sh" ~/.bashrc; then
    echo "source /tmp/proxy_env.sh" >> ~/.bashrc
fi

echo "✅ 环境变量配置完成"

# 4. 重启 Docker
echo ""
echo "4. 重启 Docker..."

sudo systemctl daemon-reload
sudo systemctl restart docker

echo "✅ Docker 已重启"

# 5. 测试代理
echo ""
echo "5. 测试代理连接..."

echo "测试 SOCKS5 代理..."
curl --socks5 ${HOST_IP}:10808 https://github.com -s -o /dev/null -w "HTTP 状态码: %{http_code}\n"

echo "测试 HTTP 代理..."
curl -x http://${HOST_IP}:10809 https://github.com -s -o /dev/null -w "HTTP 状态码: %{http_code}\n"

echo ""
echo "=== 所有代理配置完成 ==="
echo ""
echo "📋 配置信息:"
echo "  - Docker 代理: http://${HOST_IP}:10809"
echo "  - Git 代理: http://${HOST_IP}:10809"
echo "  - SOCKS5 代理: socks5://${HOST_IP}:10808"
echo "  - 环境变量: /tmp/proxy_env.sh"
echo ""
echo "🚀 使用方法:"
echo "  1. 重新连接网络或重启终端"
echo "  2. 测试代理: curl --socks5 ${HOST_IP}:10808 https://github.com"
echo "  3. 使用 Git: git clone https://github.com/user/repo.git"
