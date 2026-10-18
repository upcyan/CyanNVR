// 三条发布线的版本各自独立，界面必须如实展示「当前运行体的版本」。
//
//   web    —— 前端静态产物（__APP_VERSION__ + buildId），随 fpk/服务器一起更新
//   fpk    —— fnOS 安装包，形如 1.10.34-6
//   android—— 安卓 App（原生桥 CyanNVRApp.appVersion()），形如 1.10.33
//
// 历史教训：三端曾共用一个版本号，检查更新只看 release 的 tag_name。于是
// 「检查到更新 → 下载 → 装完版本号没变 → 再检查还是新版本」无限循环
// （v1.10.35 的 tag 下挂着 1.10.34-6 的 fpk 与 1.10.33 的 apk）。
// 现在版本按端解析，检查更新按端比对，见 server/api/release.go。

/** 当前界面所属的发布端 */
export type VersionSource = 'app' | 'server' | 'none'

/** 检查更新时要问后端的平台 */
export type UpdatePlatform = 'fpk' | 'android' | 'web'

export interface AboutVersion {
  version: string
  source: VersionSource
}

export function aboutVersion(app?: string, server?: string): AboutVersion {
  if (app) return { version: app, source: 'app' }
  if (server) return { version: server, source: 'server' }
  return { version: '', source: 'none' }
}

/**
 * 当前界面该向哪个平台的更新源比对。
 *
 * App 内嵌 WebView 走原生桥自查 APK，不需要后端；浏览器/fnOS 桌面里
 * 服务器是 fpk 部署的，比对 fpk 资产版本才是用户真正能安装的东西。
 */
export function updatePlatform(): UpdatePlatform {
  return 'fpk'
}

/**
 * 版本号展示归一：空值显示为占位符，避免出现「v」后面什么都没有。
 */
export function fmtVersion(v?: string): string {
  const s = (v || '').trim().replace(/^v/i, '')
  return s || '—'
}
