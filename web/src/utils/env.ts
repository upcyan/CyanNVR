// 运行环境检测：CyanNVR Android App 内嵌 WebView 与普通浏览器。
//
// Android 套壳 App（android/）在 WebView 里注入 UA 后缀 " CyanNVRApp/1.0"，
// 且 App 自带原生 TopAppBar（显示服务器名与地址）+ 原生底部安全区处理。
// 前端据此在 App 环境隐藏 Web 端自绘的 van-nav-bar 标题栏，避免「双重标题栏」。

let cached: boolean | null = null

/** 是否运行在 CyanNVR Android App 内嵌 WebView 里 */
export function isNvrApp(): boolean {
  if (cached !== null) return cached
  try {
    cached = typeof navigator !== 'undefined' && /CyanNVRApp\//.test(navigator.userAgent)
  } catch {
    cached = false
  }
  return cached
}

/** 供测试重置缓存（一般无需调用） */
export function resetEnvCache(): void {
  cached = null
}
