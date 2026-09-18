#!/bin/bash

# VPS 代理搭建脚本
# 用于通过 VPS 代理连接 GitHub

set -e

echo "=== VPS 代理搭建脚本 ==="
echo ""

# 检查是否为 root 用户
if [ "$EUID" -ne 0 ]; then
    echo "❌ 请以 root 用户执行此脚本"
    echo "使用: sudo ./vps_proxy_setup.sh"
    exit 1
fi

echo "✅ 检测到 root 权限"

# 安装必要软件
echo ""
echo "📦 安装必要软件..."
apt-get update
apt-get install -y git curl wget

# 配置 Git 代理
echo ""
echo "🔧 配置 Git 代理..."
echo "请输入您的 VPS 信息："

read -p "VPS IP 地址: " VPS_IP
read -p "VPS SSH 端口 (默认 22): " VPS_PORT
VPS_PORT=${VPS_PORT:-22}
read -p "VPS 用户名 (默认 root): " VPS_USER
VPS_USER=${VPS_USER:-root}

# 创建 SSH 配置
echo ""
echo "🔧 创建 SSH 配置..."
mkdir -p ~/.ssh
chmod 700 ~/.ssh

# 添加 SSH 配置
cat >> ~/.ssh/config << EOF

# GitHub 代理配置
Host github.com
    HostName github.com
    User git
    ProxyCommand ssh -p $VPS_PORT -W %h:%p $VPS_USER@$VPS_IP
    IdentityFile ~/.ssh/id_ed25519
EOF

chmod 600 ~/.ssh/config

echo "✅ SSH 配置已创建"

# 生成 SSH 密钥（如果不存在）
if [ ! -f ~/.ssh/id_ed25519 ]; then
    echo ""
    echo "🔑 生成 SSH 密钥..."
    ssh-keygen -t ed25519 -C "proxy-key" -f ~/.ssh/id_ed25519 -N ""
    echo "✅ SSH 密钥已生成"
else
    echo "ℹ️  SSH 密钥已存在"
fi

# 显示公钥
echo ""
echo "📋 请将以下公钥添加到 GitHub:"
echo "访问: https://github.com/settings/keys"
echo ""
cat ~/.ssh/id_ed25519.pub
echo ""

# 配置 Git 使用 SSH
echo "🔧 配置 Git 使用 SSH..."
cd /vol1/1000/deepseek_harness/SimpleNVR
git remote set-url origin git@github.com:upcyan/SimpleNVR.git

echo "✅ Git 已配置使用 SSH"

# 测试连接
echo ""
echo "🧪 测试 SSH 连接..."
if ssh -T git@github.com 2>&1 | grep -q "successfully authenticated"; then
    echo "✅ SSH 连接测试成功"
else
    echo "⚠️  SSH 连接测试失败，请检查配置"
fi

# 显示当前状态
echo ""
echo "📋 当前 Git 远程仓库:"
git remote -v

echo ""
echo "✅ VPS 代理搭建完成！"
echo ""
echo "🚀 现在可以执行推送:"
echo "git push origin main"
echo ""
echo "或者使用脚本:"
echo "./EXECUTE_PUSH.sh"
