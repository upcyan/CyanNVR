#!/bin/bash

# VPS SOCKS5 代理搭建脚本
# 使用 SSH 隧道创建 SOCKS5 代理

set -e

echo "=== VPS SOCKS5 代理搭建脚本 ==="
echo ""

# 检查是否为 root 用户
if [ "$EUID" -ne 0 ]; then
    echo "❌ 请以 root 用户执行此脚本"
    echo "使用: sudo ./vps_socks5_proxy.sh"
    exit 1
fi

echo "✅ 检测到 root 权限"

# 获取 VPS 信息
echo "📋 请输入 VPS 信息："
read -p "VPS IP 地址: " VPS_IP
read -p "VPS SSH 端口 (默认 22): " VPS_PORT
VPS_PORT=${VPS_PORT:-22}
read -p "VPS 用户名 (默认 root): " VPS_USER
VPS_USER=${VPS_USER:-root}

# 测试 SSH 连接
echo ""
echo "🧪 测试 SSH 连接..."
if ssh -p $VPS_PORT -o ConnectTimeout=10 $VPS_USER@$VPS_IP "echo 'SSH 连接成功'" 2>&1 | grep -q "成功"; then
    echo "✅ SSH 连接测试成功"
else
    echo "❌ SSH 连接测试失败，请检查 VPS 信息"
    exit 1
fi

# 创建 SOCKS5 代理
echo ""
echo "🔧 创建 SOCKS5 代理..."
echo "正在建立 SSH 隧道..."

# 启动 SSH 隧道（后台运行）
ssh -p $VPS_PORT -D 1080 -f -C -q -N $VPS_USER@$VPS_IP

echo "✅ SOCKS5 代理已启动"
echo "代理地址: 127.0.0.1:1080"

# 配置 Git 使用 SOCKS5 代理
echo ""
echo "🔧 配置 Git 使用 SOCKS5 代理..."
git config --global http.proxy socks5://127.0.0.1:1080
git config --global https.proxy socks5://127.0.0.1:1080

echo "✅ Git 已配置使用 SOCKS5 代理"

# 测试代理连接
echo ""
echo "🧪 测试代理连接..."
if curl --socks5 127.0.0.1:1080 https://github.com -s -o /dev/null -w "%{http_code}" | grep -q "200"; then
    echo "✅ 代理连接测试成功"
else
    echo "⚠️  代理连接测试失败"
fi

# 显示当前状态
echo ""
echo "📋 当前 Git 远程仓库:"
git remote -v

echo ""
echo "✅ VPS SOCKS5 代理搭建完成！"
echo ""
echo "🚀 现在可以执行推送:"
echo "cd /vol1/1000/deepseek_harness/SimpleNVR"
echo "git push origin main"
echo ""
echo "或者使用脚本:"
echo "./EXECUTE_PUSH.sh"
echo ""
echo "⚠️  注意：SSH 隧道会在后台运行"
echo "停止代理: pkill -f 'ssh -D 1080'"
