#!/bin/bash

# 一键部署代理脚本
# 自动选择最佳方案并部署

set -e

echo "=== 一键部署代理脚本 ==="
echo ""

# 检查是否为 root 用户
if [ "$EUID" -ne 0 ]; then
    echo "❌ 请以 root 用户执行此脚本"
    echo "使用: sudo ./deploy_proxy.sh"
    exit 1
fi

echo "✅ 检测到 root 权限"

# 显示菜单
echo "📋 请选择代理方案:"
echo "1. SSH 隧道（推荐，最简单）"
echo "2. HTTP 代理（多设备共享）"
echo "3. SOCKS5 代理（高性能）"
echo "4. 退出"
echo ""

read -p "请输入选择 (1-4): " choice

case $choice in
    1)
        echo ""
        echo "🚀 选择方案：SSH 隧道"
        chmod +x vps_proxy_setup.sh
        ./vps_proxy_setup.sh
        ;;
    2)
        echo ""
        echo "🚀 选择方案：HTTP 代理"
        chmod +x vps_http_proxy.sh
        ./vps_http_proxy.sh
        ;;
    3)
        echo ""
        echo "🚀 选择方案：SOCKS5 代理"
        chmod +x vps_socks5_proxy.sh
        ./vps_socks5_proxy.sh
        ;;
    4)
        echo "👋 退出"
        exit 0
        ;;
    *)
        echo "❌ 无效选择"
        exit 1
        ;;
esac

echo ""
echo "✅ 代理部署完成！"
echo ""
echo "🚀 现在可以推送代码:"
echo "cd /vol1/1000/deepseek_harness/SimpleNVR"
echo "git push origin main"
