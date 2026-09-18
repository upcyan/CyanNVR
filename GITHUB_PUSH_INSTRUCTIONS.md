# GitHub 推送说明

## 问题
Git 目录权限不足，无法直接执行 git 命令。

## 解决方案

### 方法 1: 修复权限后推送（推荐）

1. **以 root 用户登录或使用 sudo**
2. **修复 .git 目录权限**:
   ```bash
   sudo chmod -R 755 /vol1/1000/deepseek_harness/SimpleNVR/.git/
   sudo chown -R fn-deepseek-harness:fn-deepseek-harness /vol1/1000/deepseek_harness/SimpleNVR/.git/
   ```

3. **执行推送脚本**:
   ```bash
   cd /vol1/1000/deepseek_harness/SimpleNVR
   ./push_to_github.sh
   ```

### 方法 2: 手动推送

1. **添加文件**:
   ```bash
   cd /vol1/1000/deepseek_harness/SimpleNVR
   git add server/pkg/onvifx/onvifx.go
   git add web/src/App.vue
   git add web/src/router/index.ts
   git add web/src/styles/theme.css
   git add web/src/views/LoginView.vue
   git add web/src/views/SettingsView.vue
   git add push_to_github.sh
   ```

2. **创建提交**:
   ```bash
   git commit -m "feat: 多项功能优化与修复

   - 优化 ONVIF 子码流自动探测功能
   - 支持海康威视、大华、华为等主流品牌
   - 修复登录界面移动端显示溢出问题
   - 优化关怀模式布局和样式
   - 增强用户管理功能（默认角色为普通用户）
   - 修复导航栏事件图标显示
   - 优化设置页面交互体验"
   ```

3. **推送到 GitHub**:
   ```bash
   git push origin main
   ```

## 修改的文件

| 文件 | 修改内容 |
|------|----------|
| `server/pkg/onvifx/onvifx.go` | 优化 ONVIF 子码流自动探测 |
| `web/src/App.vue` | 修复导航栏事件图标 |
| `web/src/router/index.ts` | 首页重定向到登录页 |
| `web/src/styles/theme.css` | 优化关怀模式布局 |
| `web/src/views/LoginView.vue` | 修复移动端显示溢出 |
| `web/src/views/SettingsView.vue` | 优化设置页面交互 |

## 提交信息

```
feat: 多项功能优化与修复

- 优化 ONVIF 子码流自动探测功能
- 支持海康威视、大华、华为等主流品牌
- 修复登录界面移动端显示溢出问题
- 优化关怀模式布局和样式
- 增强用户管理功能（默认角色为普通用户）
- 修复导航栏事件图标显示
- 优化设置页面交互体验
```

## 验证

推送完成后，可以访问以下链接查看：
- 仓库地址: https://github.com/upcyan/SimpleNVR
- 查看提交: https://github.com/upcyan/SimpleNVR/commits/main
