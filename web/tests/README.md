# 界面与回放回归

这些脚本使用 Node.js、Playwright 和本机 Edge。先在 `web` 目录启动 `npm run dev -- --host 127.0.0.1`。Playwright 可以从正常依赖中解析，也可以用 `PLAYWRIGHT_MODULE` 指定现有 Playwright 模块目录；`QA_OUTPUT` 指定截图和结果目录，建议使用仓库中已忽略的 `ui-test`。

```powershell
node tests/ui-audit.cjs
node tests/ui-interactions.cjs
```

- `ui-audit.cjs`：96 组页面/尺寸/主题检查，输出布局测量和截图，出现控件越界或页面异常时返回失败。
- `ui-interactions.cjs`：关怀模式、字号恢复、选择器滚轮/拖动/确认/取消、定时录像选择器、暂停/倍速/时间跳转、键盘操作、空结果筛选恢复。

以上两项使用内置演示数据，并拦截测试页面的后端请求。

`playback-real.cjs` 连接 `127.0.0.1:8088` 的独立测试服务，使用其托管的最新 `web/dist`。运行前需要构建前后端并准备隔离的数据目录，不能连接生产服务。`QA_OUTPUT/fixture.json` 需要包含该测试服务登录接口返回的 `token`、`user`，以及带测试录像的 `device`。录像应包含当天 12:00:00–12:00:40、12:01:00–12:01:40 两段 H.264 MP4，已登记到测试服务录像索引。

```powershell
node tests/playback-real.cjs
```

真实回放检查包括解码、暂停、精确跳转、4 倍速、空档定位、自动续播、切页停止会话、设置保存失败提示，以及延迟会话响应的回收。脚本会对该独立测试服务创建/停止回放会话；失败保存请求由浏览器拦截。

测试生成的凭据、数据库、录像、截图和日志均保留在忽略的目录中。
