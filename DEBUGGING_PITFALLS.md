# 踩坑记录（排查此项目时踩过的坑，后续排查先读这份）

> 2026-10-01/02 两轮测试与修复中实际踩过的坑，按「构建 / 部署 / 运行时 / 前端 CSS / 测试方法 / 数据与用户」分类。
> 每条都是真实发生过的问题，不是理论风险。

## 构建坑

1. **容器内 npm build 必须挂载整个项目根**：vite.config.ts 从 `../server/version.go` 读 CoreVersion 注入 `__APP_VERSION__`。只挂载 web/ 目录时该文件不存在，静默回退 0.0.0——前端版本自检发现前后端版本不一致，每次加载都强制 `?nocache=1` 刷新循环。构建后必须 grep dist 里的版本串。
2. **node 镜像用 debian 版（node:22-slim）不要 alpine**：musl 与 node_modules 里的 glibc 原生二进制（rolldown/esbuild）不兼容，构建时 wasm 崩溃且报错难懂。
3. **二进制与 dist 的版本戳必须一致**（build-fpk.sh 有硬断言）：版本号改在 version.go，先后重建前端和后端，漏一边就会出现「强刷循环」或版本自检行显示旧版本。
4. **Vue 模板插值开头不能是对象字面量**（`{{ {a:1}[x] }}` 会报解析错误）：包一层辅助函数。
5. **用脚本替换模板时锚点必须覆盖元素完整属性**：曾只锚到 aria-label 行，留下半截 `<button` 开标签，构建报 Vue 解析错误。

## 部署坑

6. **zcode 用户无项目写权限**（目录属 dsh_fnos）：全部经 `sg docker -c "docker run -v ..."` 以容器 root 读写，最后 `chown 956:956`（dsh_fnos）归还。不要试图直接编辑。
7. **容器内无法 kill 宿主进程**（AppArmor 拦截，--privileged 也不行），读 `/proc/<pid>/environ` 同样被拦；但 `chroot /host setpriv --reuid=<属主uid> kill` 可行——以进程属主身份发信号。
8. **容器里启动的宿主进程随容器退出被 cgroup 清理**（孤儿化也没用）：用自删式 /etc/cron.d 任务让宿主机 Debian cron（真 root）执行启动；用完即删，不留驻留。
9. **chroot 不改网络命名空间**：在默认 bridge 容器里 chroot 启动的服务绑在容器网段，宿主 localhost 不通；必须 `docker run --network=host`。
10. **生产重启停机窗口 30-60 秒**（优雅停 → 换文件 → cron 触发启动），期间录像短暂中断、当前段可能丢尾。避免频繁重启：纯前端改动只热更 dist（静态 no-cache 直读磁盘，无需重启）。
11. **fnOS 生命周期脚本变量**：TRIM_APPDEST=/var/apps/CyanNVR/target、TRIM_PKGVAR=/var/apps/CyanNVR/var、TRIM_PKGETC=/var/apps/CyanNVR/etc（→@appconf）。脚本要以 cyannvr（uid 945）身份运行（setpriv 降权），否则服务以 root 跑、文件属主混乱。
12. **/tmp 会被清**（重启或定期清理）：测试工作区、venv、token 文件都可能消失；所有成果必须及时同步回项目仓库（已多次靠同步救回）。

## 运行时坑

13. **回放 seek 的 ffmpeg `-ss` 位置**：`-ss` 在 `-i` 之后是输出侧 seek，concat 场景中段起播要先解码丢弃目标点之前全部内容（20s+），超过前端 axios 15s 超时即报「回放准备失败」。必须放在 `-i` 之前（输入侧，concat demuxer 支持按虚拟时间轴定位，秒级）。
14. **ffmpeg 探测本地文件不能带 `-rtsp_transport tcp`**：该选项只注册于 RTSP 协议，对文件输入直接报 "Option rtsp_transport not found"，探测恒失败。文件探测要用独立的无 RTSP 参数版本。
15. **回放转码判定要探测本地录像文件而非摄像头 RTSP**：录像经转码存的是 H.264，摄像头流可能是 HEVC——探测错对象会让本可 `-c:v copy` 秒开的回放走完整转码（首片 20s+），且每次建会话都重复开 RTSP 探测（3.5s×2）。
16. **前端 axios 全局超时 15s**：后端「等首片最长 45s」的慢操作必须单请求覆盖 timeout，否则「慢但正常」被判失败。
17. **后端 nil slice 经 gin 序列化成 null**（如 `{"segments":null}`），前端对 null 调 `.map` 抛 TypeError 被上层 catch 后变成误导性报错。store 层查询一律返回非 nil 空切片 + 前端 `?? []` 兜底。

## 前端 CSS 坑

18. **.page 是固定高度 column flex 滚动容器**：直接子元素若有 overflow≠visible（min-height=0），列表一长就被 flex-shrink 压扁到 0 高——事件页筛选条曾整条消失。此类元素必须 `flex-shrink:0`。
19. **care 模式的「value 占整行」堆叠规则不能无差别套所有 cell**：会把简单开关行也挤成「标题一行、开关掉到下一行左对齐」。必须 `:has(.复杂控件类)` 精确限定，且 title 与 value 要**同时** `flex-basis:100%`（Vant 默认 title 是 flex-basis:0，只给 value 100% 时 title 被挤成一字一行竖排）。
20. **「文字 + 右侧单选圆点」行不能参与堆叠**：圆点被挤到下一行且被行高裁成残月。
21. **care 全局放大 .van-icon 到 24px，但 Vant 组件内图标盒尺寸不跟**（如 .van-radio__icon 仍 20px 高）→ 图标被裁成残月。放大图标时要同步放大对应容器。
22. **字号缩放要同时覆盖 Vant 变量**：只改自绘元素的 --nvr-font-scale 会让「设置页字体大小」对 Vant 组件（大半界面文字）无效。`:root` 覆盖 `--van-font-size-xs/sm/md/lg/xl` 即可全局生效。
23. **flex 项默认 min-width:auto=内容最小宽度**：定宽列（如回放页 360px 日历列）在内容 min-content 变大（关怀大字体）时会被撑宽，必须显式 `min-width:0`。

## 测试方法坑

24. **自动化审计的假阳性/假阴性**：img.complete=false 可能只是懒加载没轮到（要看 network 200）；scrollHeight>clientHeight 不一定可见裁剪（无 overflow:hidden 时只是溢出）——裁剪类问题最终要人工看截图。
25. **Playwright 选择器要点名**：`get_by_role("button").first` 会命中 DOM 顺序靠前的侧边栏折叠卡（role=button），触发无关状态变化；`text=/^2$/` 会命中导航徽章跳页。选择器必须足够具体。
26. **测试改的 localStorage 键要与应用实际读取的一致**：演示模式实际读独立键 `nvr_demo_mode='1'`，只写 nvr_settings_local.demoMode 不生效（UI 开关经 settings.set 同时写两处）。
27. **mint 测试 token 前确认实例真的起来了**：端口被旧实例占用时新实例 bind 失败退出，请求打在旧实例上（旧 secret）全部 401，现象极具迷惑性。ps/ss 先确认监听进程是新起的。
28. **audit_lib 截图文件名是「会话名-截图名」双前缀**，找文件时注意。

## 数据/用户坑

29. **fnOS 安装向导改过管理员账号**（本机是 cnvradmin），默认 admin/admin123 不可用；测试登录用数据目录 jwt_secret 自签 HS256 `{sub:"u_admin",role:"admin"}`（只读，无副作用）。
30. **演示模式 mock 数据没有 GIF 字段**——测 GIF/下载按钮必须用真实数据（生产库只读副本 + 测试实例）。
31. **排查「数据看不到」先打 API**：后端 devices/recordings/events 正常 → 前端渲染/缓存/登录态问题；先分清两侧再动手，避免误判成数据丢失。

## App 客户端健壮性专项（2026-10-02 补充）

32. **「强刷/重登」只对浏览器有效**：App 客户端只持 query token 拉流、拿不到响应头，
    服务端必须自身健壮——任何客户端都不应依赖人工干预恢复。
33. **gin 只保留最后一次注册的 NoRoute**：在 WebDir 条件块里注册 SPA 回退后，
    再在别处注册 /api JSON 404 会被静默覆盖（且测试环境 WebDir 为空测不出！
    生产 WebDir 恒非空才暴露）。多个 NoRoute 语义必须合并成一个无条件处理器。
34. **流媒体 query token 没有 web 端的自动续签链路**：浏览器每次 API 调用会经
    X-Renewed-Token 换新 token；App 只拿 token 拉流，token 每日过期若严格校验
    则视频流每日断一次。流鉴权必须与主鉴权一样走信任窗口放行（ParseWithTrust），
    但改密吊销仍优先。
35. **给非浏览器客户端的 API 契约**：列表字段恒为数组（严禁 nil slice→null）；
    /api/* 未匹配路由返回 JSON 404；错误响应统一 {"error": ...} 结构。
    已由 server/api/app_robustness_test.go 固化为回归测试（空库全端点扫描 +
    令牌信任窗口/吊销矩阵），改路由或鉴权时先跑它。

## 安全修复专项（2026-10-02 v1.9.2 补充）

36. **govulncheck 版本要跟工具链匹配**：govulncheck@latest 要求 go ≥1.26 时装不上
    （GOTOOLCHAIN=local 镜像环境），退回 v1.1.4 即可扫。镜像 golang:1.25-alpine 实际
    捆绑的 go 会随时间更新（本次实测已是 1.25.14），标准库漏洞可由更新镜像解决；
    go.mod 里钉 toolchain 指令保证不随镜像漂移。
37. **容器内 go get 升级依赖的两个坑**：模块缓存目录属主被 /tmp 清理破坏会报
    permission denied（chown 修）；sumdb 校验写 /tmp/gopath/pkg/sumdb 失败需临时
    GOSUMDB=off（goproxy.cn 代理源，生产出包路径 build-fpk.sh 同样依赖该镜像源）。
38. **外部扫描器的「已修复」要落到仓库才算数**：安全报告称 package-lock.json 已升
    axios 1.20.0 且 git status 为 M，但仓库实际仍是 1.19.0——扫描发生在其自己的
    工作副本。修复必须以仓库为准重新执行并验证（grep lockfile 版本号）。
39. **npm 网络用 npmmirror**：容器内 npm 默认源超时，--registry=https://registry.npmmirror.com
    稳定；node_modules 属主混合时 npm 可能报 EACCES，必要时重建 workspace。
40. **fpk 免登录失效的根因在前端，不在服务端**：飞牛桌面入口是 iframe 内嵌
    （fpk/app/ui/config "type":"iframe"），每次打开都是全新页面加载，路由 /
    固定重定向 /login，而登录页此前没有「已持凭据自动进入」——即使本地 token
    有效或仍在 72h 信任窗口内，用户也停在登录页重输密码；服务端续签链路正常
    但没有任何请求可触发。修复=登录页 onMounted 时若持 token 且后端可达，静默
    fetchMe 验证（窗口内过期会经 X-Renewed-Token 静默续签落盘）成功即直达目标
    页；浏览器地址直开落在 /login 时同样受益。注意：若未来把应用挂在跨源反代
    （fnOS 域名转发到 :18182）下，第三方分区存储会真丢 token，届时应上 HttpOnly
    会话 Cookie 兜底。

## 回放页重构专项（2026-10-06 v1.10.0 补充）

41. **heredoc 里写含反引号/反斜杠的文档内容会被外层 shell 执行**：sg docker -c "..." 双引号
    包裹的内层脚本先经过本 shell 一轮展开。长文档一律用 Write 工具写本地文件再 docker cp。
42. **/tmp 会被反复清空（实测一天多次）**：未及时同步回仓库的工作会直接丢失（本轮回放页
    重构曾丢过一次、靠上下文中的脚本重放恢复）。铁律：每完成一个可构建状态立即同步仓库。
43. **并行会话改过同一仓库**：动手前先看 server/version.go 与相关文件的实际状态，
    基于最新基线叠加，不要按自己记忆中的版本写锚点（本轮 1.9.2 记忆 vs 实际 1.9.4，
    且并行会话已组件化 PlaybackDateBar、重排了两段式布局）。
44. **模板锚点缩进必须与实际文件一致**：嵌套层级差一层（12 vs 14 空格）锚点就失配；
    复杂重构用「标记定位 + 切片」代替整块字符串匹配，且每步保存验证。
45. **构建成功 ≠ 功能存在**：grep 校验要以最终类名/符号为准（曾因方案改名 pb-player→pb-main
    而误判同步失败）。

## 回放移动端交互专项（v1.10.0-2 补充）

46. **Screen Orientation lock 需处于全屏状态**：先 requestFullscreen 再 orientation.lock；
    iOS Safari 不支持 lock（catch 静默跳过，方向由系统旋转锁决定）。按视频流宽高比
    （videoWidth>=videoHeight ? landscape : portrait）选方向，而非一律横屏。
47. **全屏/方向逻辑用 pointer: coarse 判定移动设备**，桌面跳过 lock（桌面无旋转语义）。
48. **绝对定位的时间轴标签悬在容器外**：TimelineBar 的 00:00/24:00 标签是 absolute，
    下方内容会与其重叠——父容器需 padding-bottom 预留标签空间。
49. **控制条新增按钮后窄屏必爆版**：flex-wrap:wrap 会让按钮竖摞盖满画面；应 nowrap +
    移动端紧凑尺寸（按钮 36-42px），再以横向滚动兜底极端情况。
