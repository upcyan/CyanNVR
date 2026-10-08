// 全屏与屏幕方向工具：全屏进入/退出 + 移动端自动横屏。
//
// 仅移动端生效：桌面显示器没有「旋转」语义，锁定方向无意义；
// 以 pointer: coarse（触屏主指针）判定移动设备，桌面保持普通全屏。

export function isMobileDevice(): boolean {
  try {
    return window.matchMedia('(pointer: coarse)').matches
  } catch {
    return false
  }
}

/** CyanNVR App 内嵌 WebView 暴露的原生桥（见 android WebViewScreen.kt） */
function appBridge(): { setOrientation?: (landscape: boolean) => void } | null {
  return (window as any).CyanNVRApp ?? null
}

/**
 * 进入全屏并按视频宽高比自动锁定方向。
 *
 * 三种环境，三种真横屏路径：
 *   1. Android App（内嵌 WebView）：调用原生桥 CyanNVRApp.setOrientation，
 *      由原生 requestedOrientation 真正旋转屏幕。WebView 不支持
 *      screen.orientation.lock()，必须走原生。
 *   2. Android Chrome 等支持 Screen Orientation API 的浏览器：lock()。
 *   3. iOS Safari / 桌面：不支持 lock，保持原方向（iOS 用户手动旋转由系统适配）。
 *
 * 绝不用 CSS transform 旋转做「伪横屏」——画面横了、返回手势还是竖屏，
 * 方向割裂比保持竖屏更糟。
 */
export async function enterFullscreen(el: HTMLElement, video?: HTMLVideoElement | null): Promise<void> {
  const req = el.requestFullscreen ?? (el as any).webkitRequestFullscreen
  if (req) await req.call(el).catch(() => {})
  if (!isMobileDevice()) return

  // 确保全屏已激活（WebView 上 fullscreen 激活可能晚于 Promise resolve）
  if (!document.fullscreenElement && !(document as any).webkitFullscreenElement) {
    await new Promise<void>((resolve) => {
      const timer = setTimeout(done, 500)
      function done() {
        clearTimeout(timer)
        document.removeEventListener('fullscreenchange', done)
        document.removeEventListener('webkitfullscreenchange', done)
        resolve()
      }
      document.addEventListener('fullscreenchange', done)
      document.addEventListener('webkitfullscreenchange', done)
    })
  }

  // 按视频宽高比决定目标方向：横构图（16:9）要横屏，竖构图（9:16）要竖屏
  let wantLandscape = true
  if (video && video.videoWidth > 0 && video.videoHeight > 0) {
    wantLandscape = video.videoWidth >= video.videoHeight
  }

  // 1) App 原生桥优先
  const bridge = appBridge()
  if (bridge?.setOrientation) {
    try {
      bridge.setOrientation(wantLandscape)
      return
    } catch {
      /* 桥异常则回退到下方 lock */
    }
  }

  // 2) 浏览器 Screen Orientation API
  try {
    await (screen.orientation as any)?.lock?.(wantLandscape ? 'landscape' : 'portrait')
  } catch {
    /* 不支持：保持原方向，不伪装横屏 */
  }
}

export async function exitFullscreen(): Promise<void> {
  // App 原生桥：恢复竖屏。不用 UNSPECIFIED——那会跟随传感器，
  // 横屏拿在手里时退出全屏仍保持横屏，用户看到的就是「退不出来」。
  const bridge = appBridge()
  if (bridge?.setOrientation) {
    try {
      bridge.setOrientation(false)
    } catch {
      /* ignore */
    }
  }

  if (isMobileDevice()) {
    try {
      ;(screen.orientation as any)?.unlock?.()
    } catch {
      /* ignore */
    }
  }
  const exit = document.exitFullscreen ?? (document as any).webkitExitFullscreen
  if (exit) await exit.call(document).catch(() => {})
}

/**
 * 兜底：只要退出全屏就恢复竖屏。
 *
 * 必须有：用户可能不经 exitFullscreen() 退出全屏（Android 返回键、系统手势、
 * Esc）。此时若不恢复方向，App 会一直卡在横屏。由 fullscreenchange 统一兜底，
 * 比在每个调用点处理更可靠。
 */
function onFullscreenChange() {
  const stillFullscreen = document.fullscreenElement || (document as any).webkitFullscreenElement
  if (stillFullscreen) return
  const bridge = appBridge()
  if (bridge?.setOrientation) {
    try {
      bridge.setOrientation(false)
    } catch {
      /* ignore */
    }
  }
}

if (typeof document !== 'undefined') {
  document.addEventListener('fullscreenchange', onFullscreenChange)
  document.addEventListener('webkitfullscreenchange', onFullscreenChange)
}
