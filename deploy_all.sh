#!/bin/bash

# 一键部署脚本
# 自动选择最佳方案并部署

set -e

echo "=== 一键部署脚本 ==="
echo ""

# 检查是否为 root 用户
if [ "$EUID" -ne 0 ]; then
    echo "❌ 请以 root 用户执行此脚本"
    echo "使用: sudo ./deploy_all.sh"
    exit 1
fi

echo "✅ 检测到 root 权限"

# 显示菜单
echo "📋 请选择部署方案:"
echo "1. Docker 软路由 + V2Ray（推荐，轻量级）"
echo "2. OpenWrt 软路由 + V2Ray（功能完整）"
echo "3. 仅部署 V2Ray 代理"
echo "4. 部署 SimpleNVR（网络录像机）"
echo "5. 退出"
echo ""

read -p "请输入选择 (1-5): " choice

case $choice in
    1)
        echo ""
        echo "🚀 选择方案：Docker 软路由 + V2Ray"
        chmod +x deploy_softrouter.sh
        ./deploy_softrouter.sh
        ;;
    2)
        echo ""
        echo "🚀 选择方案：OpenWrt 软路由 + V2Ray"
        chmod +x deploy_openwrt.sh
        ./deploy_openwrt.sh
        ;;
    3)
        echo ""
        echo "🚀 选择方案：仅部署 V2Ray 代理"
        chmod +x deploy_v2ray.sh
        ./deploy_v2ray.sh
        ;;
    4)
        echo ""
        echo "🚀 选择方案：部署 SimpleNVR"
        cd /vol1/1000/NasAppFiles/dockerfiles/simpleNVR
        docker compose up -d
        echo "✅ SimpleNVR 已启动"
        echo "🔗 访问: http://localhost:18181"
        ;;
    5)
        echo "👋 退出"
        exit 0
        ;;
    *)
        echo "❌ 无效选择"
        exit 1
        ;;
esac

echo ""
echo "✅ 部署完成！"
echo ""
echo "📚 文档位置:"
echo "  - 软路由指南: SOFTRouter_GUIDE.md"
echo "  - VPS 代理指南: VPS_PROXY_GUIDE.md"
echo "  - 推送指南: PUSH_GUIDE.md"
