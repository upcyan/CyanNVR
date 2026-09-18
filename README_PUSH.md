# SimpleNVR 代码推送指南

## 当前状态
- ✅ 所有功能修改已完成
- ✅ Docker 容器正常运行
- ⚠️ Git 目录权限需要修复

## 推送步骤

### 第一步：修复 Git 权限

以 root 用户执行以下命令：

```bash
# 修复 .git 目录权限
sudo chmod -R 755 /vol1/1000/deepseek_harness/SimpleNVR/.git/

# 修改所有者
sudo chown -R fn-deepseek-harness:fn-deepseek-harness /vol1/1000/deepseek_harness/SimpleNVR/.git/
```

### 第二步：添加文件并提交

```bash
cd /vol1/1000/deepseek_harness/SimpleNVR

# 添加修改的文件
git add server/pkg/onvifx/onvifx.go
git add web/src/App.vue
git add web/src/router/index.ts
git add web/src/styles/theme.css
git add web/src/views/LoginView.vue
git add web/src/views/SettingsView.vue
git add push_to_github.sh
git add GITHUB_PUSH_INSTRUCTIONS.md
git add README_PUSH.md

# 创建提交
git commit -m "feat: 多项功能优化与修复

- 优化 ONVIF 子码流自动探测功能
- 支持海康威视、大华、华为等主流品牌
- 修复登录界面移动端显示溢出问题
- 优化关怀模式布局和样式
- 增强用户管理功能（默认角色为普通用户）
- 修复导航栏事件图标显示
- 优化设置页面交互体验"
```

### 第三步：推送到 GitHub

```bash
git push origin main
```

## 修改内容总结

### 1. ONVIF 子码流自动探测
**文件**: `server/pkg/onvifx/onvifx.go`
- 新增 `guessSubStreamURL` 函数
- 新增 `guessStreams` 函数
- 支持海康威视、大华、华为等品牌

### 2. 登录界面优化
**文件**: `web/src/views/LoginView.vue`
- 修复移动端显示溢出
- 添加响应式样式
- 优化布局

### 3. 关怀模式优化
**文件**: `web/src/styles/theme.css`
- 增强设置页面样式
- 优化触摸区域
- 提高对比度

### 4. 导航栏修复
**文件**: `web/src/App.vue`
- 修复事件图标显示

### 5. 路由优化
**文件**: `web/src/router/index.ts`
- 首页重定向到登录页

### 6. 设置页面优化
**文件**: `web/src/views/SettingsView.vue`
- 优化开关按钮交互
- 增强用户管理功能

## 验证推送

推送完成后，访问以下链接：
- 仓库: https://github.com/upcyan/SimpleNVR
- 提交记录: https://github.com/upcyan/SimpleNVR/commits/main

## 常见问题

### Q: 权限被拒绝？
A: 使用 `sudo` 执行命令，或修复目录权限。

### Q: 推送失败？
A: 检查 GitHub 凭据是否正确配置。

### Q: 如何撤销提交？
A: 使用 `git reset --soft HEAD~1` 撤销最后一次提交。

## 联系支持

如有问题，请检查：
1. Git 权限是否正确
2. GitHub 凭据是否配置
3. 网络连接是否正常
