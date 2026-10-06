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

/**
 * 判断当前视口是否真的已是期望方向。
 *
 * 关键：screen.orientation.lock() 在部分环境（桌面 Chrome 模拟移动、
 * 某些 Android WebView / 微信）**既不抛错也不生效**——实测调用
 * lock('landscape') 后 screen.orientation.type 仍是 portrait-primary。
 * 若仅以「未抛错」判定成功就直接 return，就会跳过 CSS 旋转兜底，
 * 于是横屏视频在全屏下依旧是竖屏（用户反馈的「全屏只有竖屏」）。
 * 因此以「实际视口方向」为准，而不是以 lock 是否抛错为准。
 */
function isLandscapeViewport(): boolean {
  if (typeof window === 'undefined') return true
  // screen.orientation.type 更准确，取不到时退回宽高比较
  const t = (screen.orientation as any)?.type
  if (typeof t === 'string' && t) return t.startsWith('landscape')
  return window.innerWidth > window.innerHeight
}

export async function enterFullscreen(el: HTMLElement, video?: HTMLVideoElement | null): Promise<void> {
  const req = el.requestFullscreen ?? (el as any).webkitRequestFullscreen
  if (req) await req.call(el).catch(() => {})
  if (!isMobileDevice()) return

  // 按视频流宽高比自动判断目标方向：横构图（宽>=高，常见 16:9 摄像头）要横屏，
  // 竖构图（如倒装/门铃 9:16）要竖屏；取不到视频尺寸时默认横屏。
  let orientation: 'landscape' | 'portrait' = 'landscape'
  if (video && video.videoWidth > 0 && video.videoHeight > 0) {
    orientation = video.videoWidth >= video.videoHeight ? 'landscape' : 'portrait'
  }

  // 先尝试原生方向锁定（Android Chrome 等真正支持）
  try {
    await (screen.orientation as any)?.lock?.(orientation)
  } catch {
    /* 不支持方向锁定（微信/iOS WebView 常见）——下方走 CSS 旋转兜底 */
  }

  // 以「锁定后视口是否真的变成目标方向」判定是否需要兜底。
  // 必须轮询等待而不是固定 sleep：真机上 lock 生效有延迟，等太短会把
  // 「已生效」误判为「未生效」，于是原生横屏之上再叠一层 CSS 旋转，
  // 画面被旋转 180°（双重旋转）。这里最多等 700ms，一旦方向正确立即返回。
  const wantLandscape = orientation === 'landscape'
  let ok = false
  for (let i = 0; i < 14; i++) {
    if (isLandscapeViewport() === wantLandscape) {
      ok = true
      break
    }
    await new Promise((r) => setTimeout(r, 50))
  }
  el.classList.remove('force-landscape', 'force-portrait')
  if (ok) return

  // CSS 旋转兜底：把内容旋转 90° 等效横屏（或保持竖屏）。
  //
  // 关键一：transform **不能加在全屏元素本身**——Chrome 的 :fullscreen UA 规则
  //   会覆盖用户样式里的 transform（实测同一元素加类后 rotate(90deg) 生效为
  //   matrix(0,1,-1,0,...)，一进入全屏 computed transform 立刻变回 none，
  //   画面依旧竖屏）。这正是「全屏只有竖屏」的真正根因。
  // 关键二：要旋转的是**单一内容包裹层** .force-inner（PlaybackPlayer 模板里
  //   已声明），而不是全屏容器的各个子元素——.player 下有 video、controls、
  //   jump-pop 等多个直接子元素，逐个旋转会各自定位而互相错位。
  el.classList.add(wantLandscape ? 'force-landscape' : 'force-portrait')
}

/**
 * 兜底：只要退出全屏就清掉旋转类。
 *
 * 为什么必须有：用户可能不经我们的 exitFullscreen() 退出全屏
 * （Android 返回键、iOS 手势、桌面 Esc、系统强制退出）。实测直接调用
 * document.exitFullscreen() 后 .force-landscape 仍留在元素上，
 * 于是页面在**非全屏**状态下继续被旋转 90°，画面整体歪掉。
 * 由 fullscreenchange 统一兜底，比在每个调用点清理更可靠。
 */
function onFullscreenChange(): void {
  if (document.fullscreenElement) return
  document.querySelectorAll('.force-landscape, .force-portrait').forEach((n) => n.classList.remove('force-landscape', 'force-portrait'))
}

if (typeof document !== 'undefined') {
  document.addEventListener('fullscreenchange', onFullscreenChange)
  document.addEventListener('webkitfullscreenchange', onFullscreenChange)
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

