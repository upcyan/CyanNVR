#!/bin/bash

# SimpleNVR GitHub 推送脚本
# 使用方法: ./push_to_github.sh

set -e

echo "=== SimpleNVR GitHub 推送脚本 ==="
echo ""

# 检查 git 权限
if ! git status &> /dev/null; then
    echo "❌ Git 权限不足，请以 root 用户执行或修复 .git 目录权限"
    echo "   修复命令: sudo chmod -R 755 /vol1/1000/deepseek_harness/SimpleNVR/.git/"
    exit 1
fi

# 添加修改的文件
echo "📁 添加修改的文件..."
git add server/pkg/onvifx/onvifx.go
git add web/src/App.vue
git add web/src/router/index.ts
git add web/src/styles/theme.css
git add web/src/views/LoginView.vue
git add web/src/views/SettingsView.vue

# 检查是否有修改
if git diff --cached --quiet; then
    echo "⚠️  没有需要提交的修改"
    exit 0
fi

# 显示修改的文件
echo ""
echo "📝 修改的文件:"
git diff --cached --stat

# 创建提交
echo ""
echo "💾 创建提交..."
git commit -m "feat: 多项功能优化与修复

- 优化 ONVIF 子码流自动探测功能
- 支持海康威视、大华、华为等主流品牌
- 修复登录界面移动端显示溢出问题
- 优化关怀模式布局和样式
- 增强用户管理功能（默认角色为普通用户）
- 修复导航栏事件图标显示
- 优化设置页面交互体验"

# 推送到 GitHub
echo ""
echo "🚀 推送到 GitHub..."
git push origin main

echo ""
echo "✅ 推送完成！"
echo ""
echo "📋 提交信息:"
git log -1 --oneline
