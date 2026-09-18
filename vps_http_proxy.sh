#!/bin/bash

# VPS HTTP 代理搭建脚本
# 使用 Squid 创建 HTTP 代理服务器

set -e

echo "=== VPS HTTP 代理搭建脚本 ==="
echo ""

# 检查是否为 root 用户
if [ "$EUID" -ne 0 ]; then
    echo "❌ 请以 root 用户执行此脚本"
    echo "使用: sudo ./vps_http_proxy.sh"
    exit 1
fi

echo "✅ 检测到 root 权限"

# 安装 Squid 代理服务器
echo ""
echo "📦 安装 Squid 代理服务器..."
apt-get update
apt-get install -y squid

# 备份原始配置
echo "🔧 备份原始配置..."
cp /etc/squid/squid.conf /etc/squid/squid.conf.backup

# 创建新的 Squid 配置
echo "🔧 创建代理配置..."
cat > /etc/squid/squid.conf << EOF
# Squid 代理服务器配置
# 用于 GitHub 代理访问

# 监听端口
http_port 3128

# 允许的客户端
acl localnet src 0.0.0.1-0.255.255.255
acl localnet src 10.0.0.0/8
acl localnet src 100.64.0.0/10
acl localnet src 169.254.0.0/16
acl localnet src 172.16.0.0/12
acl localnet src 192.168.0.0/16

# 允许本地网络访问
http_access allow localnet
http_access allow localhost

# 允许 GitHub 访问
acl github dstdomain .github.com
acl github dstdomain github.com
http_access allow github

# 拒绝其他访问
http_access deny all

# 优化设置
cache_mem 256 MB
maximum_object_size 64 MB
cache_dir ufs /var/spool/squid 10000 16 256

# 日志设置
access_log /var/log/squid/access.log squid
cache_log /var/log/squid/cache.log

# 超时设置
connect_timeout 60 seconds
read_timeout 60 seconds
request_timeout 60 seconds
EOF

# 重启 Squid 服务
echo "🔄 重启 Squid 服务..."
systemctl restart squid
systemctl enable squid

# 检查服务状态
echo "🔍 检查服务状态..."
systemctl status squid --no-pager

# 显示代理信息
echo ""
echo "✅ VPS HTTP 代理搭建完成！"
echo ""
echo "📋 代理信息:"
echo "代理地址: $(hostname -I | awk '{print $1}'):3128"
echo "代理类型: HTTP"
echo ""
echo "🔧 配置 Git 使用代理:"
echo "git config --global http.proxy http://$(hostname -I | awk '{print $1}'):3128"
echo "git config --global https.proxy http://$(hostname -I | awk '{print $1}'):3128"
echo ""
echo "🚀 测试代理连接:"
echo "curl -x http://$(hostname -I | awk '{print $1}'):3128 https://github.com"
echo ""
echo "📝 推送到 GitHub:"
echo "cd /vol1/1000/deepseek_harness/SimpleNVR"
echo "git push origin main"
