# GitHub 推送完整指南

## 当前状态

✅ **代码已提交** - 提交 ID: `77a8cbf`
✅ **修改的文件已暂存**
❌ **推送失败** - 网络或认证问题

## 推送方法

### 方法 1: 使用个人访问令牌（推荐）

1. **生成 GitHub 个人访问令牌**
   - 访问: https://github.com/settings/tokens
   - 点击 "Generate new token"
   - 选择权限: `repo` (完整权限)
   - 复制生成的令牌

2. **配置 Git 凭据**
   ```bash
   cd /vol1/1000/deepseek_harness/SimpleNVR
   
   # 设置远程仓库 URL（包含令牌）
   git remote set-url origin https://<YOUR_TOKEN>@github.com/upcyan/SimpleNVR.git
   
   # 推送
   git push origin main
   ```

3. **示例**（替换 `ghp_xxxx` 为您的令牌）:
   ```bash
   git remote set-url origin https://ghp_abc123def456@github.com/upcyan/SimpleNVR.git
   git push origin main
   ```

### 方法 2: 使用 SSH 密钥

1. **生成 SSH 密钥**
   ```bash
   ssh-keygen -t ed25519 -C "your_email@example.com"
   ```

2. **添加公钥到 GitHub**
   - 复制 `~/.ssh/id_ed25519.pub` 内容
   - 访问: https://github.com/settings/keys
   - 点击 "New SSH key"
   - 粘贴公钥

3. **配置远程仓库**
   ```bash
   cd /vol1/1000/deepseek_harness/SimpleNVR
   git remote set-url origin git@github.com:upcyan/SimpleNVR.git
   git push origin main
   ```

### 方法 3: 使用 GitHub CLI

1. **安装 GitHub CLI**
   ```bash
   # macOS
   brew install gh
   
   # Ubuntu
   sudo apt install gh
   ```

2. **登录**
   ```bash
   gh auth login
   ```

3. **推送**
   ```bash
   cd /vol1/1000/deepseek_harness/SimpleNVR
   git push origin main
   ```

### 方法 4: 手动下载上传

1. **导出代码**
   ```bash
   cd /vol1/1000/deepseek_harness/SimpleNVR
   git format-patch -1 HEAD --stdout > my_changes.patch
   ```

2. **在有网络的机器上应用**
   ```bash
   git am < my_changes.patch
   git push origin main
   ```

## 已提交的内容

### 提交信息
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

### 修改的文件
1. `server/pkg/onvifx/onvifx.go` - ONVIF 子码流探测
2. `web/src/App.vue` - 导航栏图标修复
3. `web/src/router/index.ts` - 路由优化
4. `web/src/styles/theme.css` - 关怀模式样式
5. `web/src/views/LoginView.vue` - 登录界面修复
6. `web/src/views/SettingsView.vue` - 设置页面优化
7. `push_to_github.sh` - 推送脚本
8. `final_push.sh` - 最终推送脚本
9. `GITHUB_PUSH_INSTRUCTIONS.md` - 推送说明
10. `README_PUSH.md` - 推送指南

## 验证推送

推送成功后，访问:
- 仓库: https://github.com/upcyan/SimpleNVR
- 提交: https://github.com/upcyan/SimpleNVR/commits/main

## 常见问题

### Q: 推送超时？
A: 检查网络连接，或使用代理。

### Q: 权限被拒绝？
A: 确保使用正确的令牌或 SSH 密钥。

### Q: 如何撤销推送？
A: 使用 `git reset --hard HEAD~1` 然后强制推送。

## 联系支持

如需帮助，请提供:
1. 错误信息
2. 使用的推送方法
3. 网络环境
