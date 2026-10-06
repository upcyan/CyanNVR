// 全屏与屏幕方向工具：全屏进入/退出 + 移动端自动横屏。
//
// 仅移动端生效：桌面显示器没有「旋转」语义，锁定方向无意义；
// 以 pointer: coarse（触屏主指针）判定移动设备，桌面保持普通全屏。
// 屏幕方向锁定依赖 Screen Orientation API（Android Chrome 等支持，
// 要求处于全屏状态；iOS Safari 不支持——catch 后静默跳过，由系统
// 自身的旋转锁决定方向，不影响全屏本身）。

export function isMobileDevice(): boolean {
  try {
    return window.matchMedia('(pointer: coarse)').matches
  } catch {
    return false
  }
}

export async function enterFullscreen(el: HTMLElement, video?: HTMLVideoElement | null): Promise<void> {
  const req = el.requestFullscreen ?? (el as any).webkitRequestFullscreen
  if (req) await req.call(el).catch(() => {})
  if (isMobileDevice()) {
    // 按视频流宽高比自动判断锁定方向：横构图（宽>=高，常见 16:9 摄像头）锁横屏，
    // 竖构图（如倒装/门铃 9:16）锁竖屏；取不到视频尺寸时默认横屏。
    let orientation = 'landscape'
    if (video && video.videoWidth > 0 && video.videoHeight > 0) {
      orientation = video.videoWidth >= video.videoHeight ? 'landscape' : 'portrait'
    }
    try {
      await (screen.orientation as any)?.lock?.(orientation)
      el.classList.remove('force-landscape')
      return
    } catch {
      /* 设备不支持方向锁定（微信/iOS WebView 常见）——走 CSS 旋转兜底 */
    }
    // CSS 兜底：横视频 + 竖屏视口时把全屏容器旋转 90°，等效横屏
    const wantLandscape = orientation === 'landscape' && window.innerHeight > window.innerWidth
    const wantPortrait = orientation === 'portrait' && window.innerWidth > window.innerHeight
    if (wantLandscape || wantPortrait) {
      el.classList.add(wantLandscape ? 'force-landscape' : 'force-portrait')
    }
  }
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
  document.querySelectorAll('.force-landscape, .force-portrait').forEach((n) => n.classList.remove('force-landscape', 'force-portrait'))
}

