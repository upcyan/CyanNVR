#!/bin/bash

# SimpleNVR 最终推送脚本
# 尝试多种方法推送到 GitHub

set -e

echo "=== SimpleNVR 最终推送脚本 ==="
echo ""

# 检查当前状态
echo "📋 当前状态:"
git status --short
echo ""

# 显示最近提交
echo "📝 最近提交:"
git log --oneline -3
echo ""

# 尝试方法 1: 直接推送（使用现有凭据）
echo "🚀 尝试方法 1: 直接推送..."
if git push origin main 2>&1 | grep -q "fatal\|error\|timed out"; then
    echo "⚠️  方法 1 失败，尝试其他方法..."
else
    echo "✅ 方法 1 成功！"
    exit 0
fi

# 尝试方法 2: 使用 gh CLI
echo ""
echo "🚀 尝试方法 2: 使用 gh CLI..."
if command -v gh &> /dev/null; then
    if gh auth status &> /dev/null; then
        echo "✅ gh 已登录，尝试推送..."
        git push origin main
        echo "✅ 推送成功！"
        exit 0
    else
        echo "⚠️  gh 未登录，跳过此方法"
    fi
else
    echo "⚠️  gh 未安装，跳过此方法"
fi

# 尝试方法 3: 使用个人访问令牌
echo ""
echo "🚀 尝试方法 3: 使用个人访问令牌..."
echo "请手动执行以下命令:"
echo ""
echo "1. 生成令牌: https://github.com/settings/tokens"
echo "2. 执行: ./push_with_token.sh <YOUR_TOKEN>"
echo ""

# 尝试方法 4: 使用 SSH
echo ""
echo "🚀 尝试方法 4: 使用 SSH..."
echo "请手动执行以下命令:"
echo ""
echo "1. 生成 SSH 密钥: ssh-keygen -t ed25519"
echo "2. 添加公钥到 GitHub: https://github.com/settings/keys"
echo "3. 执行:"
echo "   git remote set-url origin git@github.com:upcyan/SimpleNVR.git"
echo "   git push origin main"
echo ""

# 显示当前远程仓库
echo "📋 当前远程仓库:"
git remote -v
echo ""

# 显示提交信息
echo "📝 提交信息:"
git log -1 --oneline
echo ""

echo "⏳ 等待手动推送..."
echo "选择上述任一方法执行推送命令"
