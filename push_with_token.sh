#!/bin/bash

# 使用个人访问令牌推送脚本
# 使用方法: ./push_with_token.sh <YOUR_GITHUB_TOKEN>

set -e

if [ -z "$1" ]; then
    echo "❌ 请提供 GitHub 个人访问令牌"
    echo "使用方法: ./push_with_token.sh <YOUR_GITHUB_TOKEN>"
    echo ""
    echo "生成令牌: https://github.com/settings/tokens"
    exit 1
fi

TOKEN=$1

echo "=== 使用个人访问令牌推送 ==="
echo ""

# 配置远程仓库 URL
echo "🔧 配置远程仓库..."
git remote set-url origin https://${TOKEN}@github.com/upcyan/SimpleNVR.git

# 验证配置
echo "✅ 远程仓库已配置"
git remote -v

# 推送
echo ""
echo "🚀 推送到 GitHub..."
git push origin main

echo ""
echo "✅ 推送完成！"
echo ""
echo "🔗 查看提交: https://github.com/upcyan/SimpleNVR/commits/main"
