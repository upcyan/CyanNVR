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

/**
 * 进入全屏并按视频宽高比自动锁定方向。
 *
 * 设计原则（重要）：
 *   · 真横屏优先：Android Chrome / 支持 Screen Orientation API 的 WebView 上
 *     调用 screen.orientation.lock('landscape')，系统会**真正旋转屏幕**，
 *     返回手势、状态栏、系统 UI 的方向都随之正确——这才是「真横屏」。
 *   · 不用「CSS 旋转兜底」：如果 lock 不可用（iOS Safari 不支持、桌面
 *     显示器不能旋转、某些 WebView 未实现），绝不能把画面用 transform 转
 *     90° 伪装成横屏——那会让画面横着、返回交互仍是竖屏，形成「伪横屏」，
 *     系统手势与视觉方向割裂，比保持竖屏更糟。此时保持竖屏全屏，iOS 上
 *     用户可手动旋转设备由系统自动适配，桌面则无旋转需求。
 *
 * 因此这里只做「尝试真锁」，锁不动就保持竖屏全屏，绝不欺骗方向。
 */
export async function enterFullscreen(el: HTMLElement, video?: HTMLVideoElement | null): Promise<void> {
  // 先进入全屏：Screen Orientation API 要求元素处于全屏状态 lock 才生效。
  const req = el.requestFullscreen ?? (el as any).webkitRequestFullscreen
  if (req) await req.call(el).catch(() => {})
  if (!isMobileDevice()) return

  // 确保全屏已激活再 lock：Chromium 下 requestFullscreen() await 返回时
  // fullscreenElement 通常已就绪，但 Android WebView 上 fullscreen 激活可能
  // 晚于 Promise resolve，此时直接 lock 会拿到 AbortError 而被静默放弃。
  // 若尚未激活，等一次 fullscreenchange（最多 500ms）。
  if (!document.fullscreenElement && !(document as any).webkitFullscreenElement) {
    await new Promise<void>((resolve) => {
      const done = () => {
        cleanup()
        resolve()
      }
      const cleanup = () => {
        document.removeEventListener('fullscreenchange', done)
        document.removeEventListener('webkitfullscreenchange', done)
        clearTimeout(timer)
      }
      const timer = setTimeout(done, 500)
      document.addEventListener('fullscreenchange', done)
      document.addEventListener('webkitfullscreenchange', done)
    })
  }

  // 按视频流宽高比自动判断目标方向：横构图（宽>=高，常见 16:9 摄像头）要横屏，
  // 竖构图（如倒装/门铃 9:16）要竖屏；取不到视频尺寸时默认横屏。
  let orientation: 'landscape' | 'portrait' = 'landscape'
  if (video && video.videoWidth > 0 && video.videoHeight > 0) {
    orientation = video.videoWidth >= video.videoHeight ? 'landscape' : 'portrait'
  }

  // 尝试原生方向锁定。lock 只在支持它的环境生效（Android Chrome/WebView）。
  // iOS、桌面、未实现 lock 的 WebView 会抛错或静默无效——无论哪种情况，
  // 都不做伪横屏兜底，直接保持竖屏全屏。
  try {
    await (screen.orientation as any)?.lock?.(orientation)
  } catch {
    /* lock 不可用：保持竖屏全屏，不伪装横屏（见函数头注释） */
  }

  // 无需再轮询或做 CSS 旋转兜底。真锁生效则系统已横屏；不生效则竖屏全屏，
  // 两者都是方向自洽的「真」状态，不存在画面与交互割裂的中间态。
}

export async function exitFullscreen(): Promise<void> {
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
