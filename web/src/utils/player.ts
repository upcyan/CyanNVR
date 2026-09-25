import type HlsType from 'hls.js'

export interface Playable {
  attach(el: HTMLVideoElement): void
  seek(ts: number): void
  setSpeed(n: number): void
  pause(): void
  resume(): void
  readonly time: number
  destroy(): void
  // canSeekTo 判断目标时间点是否落在当前已缓冲/可跳转区间内。
  // 回放会话是 ffmpeg 边转码边切片产出的 HLS，未就绪时不能直接 seek，
  // 此时必须由上层重建会话。就地可跳转时直接 seek 体验会好很多。
  canSeekTo(ts: number): boolean
}

// ---------- simulated stream (mock / offline demo) ----------

export class SimulatedStream implements Playable {
  private canvas: HTMLCanvasElement
  private ctx: CanvasRenderingContext2D
  private stream: MediaStream | null = null
  private raf = 0
  private running = false
  private speed = 1
  private now: number
  private lastT = 0
  private readonly hue: number

  constructor(seed: number, private label: string, baseTime = Date.now()) {
    this.now = baseTime
    this.hue = (seed * 137.508) % 360
    this.canvas = document.createElement('canvas')
    this.canvas.width = 480
    this.canvas.height = 270
    const ctx = this.canvas.getContext('2d')
    if (!ctx) throw new Error('no 2d context')
    this.ctx = ctx
  }

  attach(el: HTMLVideoElement) {
    el.muted = true
    el.playsInline = true
    this.stream = this.canvas.captureStream(15)
    el.srcObject = this.stream
    el.play().catch(() => {})
    this.lastT = performance.now()
    this.running = true
    const loop = (t: number) => {
      if (!this.running) return
      const dt = t - this.lastT
      this.lastT = t
      this.now += dt * this.speed
      this.draw()
      this.raf = requestAnimationFrame(loop)
    }
    this.raf = requestAnimationFrame(loop)
  }

  pause() {
    this.running = false
    cancelAnimationFrame(this.raf)
  }

  resume() {
    if (this.running) return
    this.lastT = performance.now()
    this.running = true
    const loop = (t: number) => {
      if (!this.running) return
      const dt = t - this.lastT
      this.lastT = t
      this.now += dt * this.speed
      this.draw()
      this.raf = requestAnimationFrame(loop)
    }
    this.raf = requestAnimationFrame(loop)
  }

  setSpeed(n: number) {
    this.speed = n
  }

  seek(ts: number) {
    this.now = ts
  }

  canSeekTo() {
    return true
  }

  get time() {
    return this.now
  }

  destroy() {
    this.running = false
    cancelAnimationFrame(this.raf)
    this.stream?.getTracks().forEach((t) => t.stop())
    this.stream = null
  }

  private draw() {
    const { ctx, canvas } = this
    const t = this.now / 1000
    const g = ctx.createLinearGradient(0, 0, canvas.width, canvas.height)
    g.addColorStop(0, `hsl(${this.hue},55%,22%)`)
    g.addColorStop(1, `hsl(${(this.hue + 40) % 360},55%,10%)`)
    ctx.fillStyle = g
    ctx.fillRect(0, 0, canvas.width, canvas.height)

    ctx.strokeStyle = `hsla(${this.hue},90%,65%,0.5)`
    ctx.lineWidth = 2
    for (let i = 0; i < 9; i++) {
      const x = (Math.sin(t * 0.3 + i * 1.7) * 0.5 + 0.5) * canvas.width
      const y = (Math.cos(t * 0.4 + i * 2.1) * 0.5 + 0.5) * canvas.height
      ctx.beginPath()
      ctx.arc(x, y, 6 + (i % 3) * 7, 0, Math.PI * 2)
      ctx.stroke()
    }

    ctx.fillStyle = 'rgba(0,0,0,0.18)'
    ctx.fillRect(0, (t * 30) % canvas.height, canvas.width, 2)

    ctx.fillStyle = 'rgba(255,255,255,0.92)'
    ctx.font = 'bold 13px sans-serif'
    ctx.fillText(this.label, 8, 18)
    ctx.font = '11px ui-monospace, Menlo, monospace'
    ctx.fillText(this.fmt(this.now), 8, canvas.height - 6)
  }

  private fmt(ts: number) {
    const d = new Date(ts)
    const p = (n: number) => String(n).padStart(2, '0')
    return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
  }
}

// ---------- HLS stream (real backend) ----------

// HlsStream 的自愈策略：
//
// 后端直播 HLS 窗口极短（hls_list_size=4 + delete_segments，约 8 秒），
// 且回放会话目录有 TTL 回收。手机切后台几秒、网络抖动或暂停过久后，
// 播放器手里的 playlist 引用的分片可能已被服务端删除，请求 404 后
// hls.js 会报 fatal network error —— 若无人处理，画面就永久冻结/黑屏。
// 因此这里必须监听 ERROR 并逐级恢复：
//   networkError -> startLoad() 重新拉播放列表（指数退避，次数有限）
//   mediaError   -> recoverMediaError()（最多 2 次）
//   其它/连续失败 -> 整个播放器重建（重新 loadSource，拿最新播放列表）
// 页面回到前台（visibilitychange）时检查是否已断流，断流直接重建。
const HLS_MAX_NETWORK_RETRIES = 6
const HLS_MAX_MEDIA_RECOVERIES = 2

export class HlsStream implements Playable {
  private paused = false
  private hls: HlsType | null = null
  private video: HTMLVideoElement | null = null
  private netRetries = 0
  private mediaRecoveries = 0
  private rebuilds = 0
  private retryTimer = 0
  private visibilityHandler: (() => void) | null = null
  // attach 时从动态导入的 Hls 默认导出上取 ErrorTypes 枚举，
  // 供 onHlsError 判断错误类别（HlsType 本身是 type-only 导入，不能当值用）。
  private errorTypes: typeof HlsType.ErrorTypes | null = null

  constructor(private url: string, private baseTs = 0) {}

  async attach(el: HTMLVideoElement) {
    this.destroy()
    this.video = el
    el.srcObject = null
    el.muted = true
    el.playsInline = true
    const { default: Hls } = await import('hls.js')
    if (this.video !== el) return
    if (Hls.isSupported()) {
      this.hls = new Hls({ enableWorker: true, maxBufferLength: 30, liveDurationInfinity: true })
      this.errorTypes = Hls.ErrorTypes
      this.hls.on(Hls.Events.ERROR, (_evt, data) => this.onHlsError(data))
      this.hls.loadSource(this.url)
      this.hls.attachMedia(el)
    } else if (el.canPlayType('application/vnd.apple.mpegurl')) {
      el.src = this.url
    }
    if (!this.visibilityHandler) {
      this.visibilityHandler = () => {
        if (document.visibilityState === 'visible') this.onVisible()
      }
      document.addEventListener('visibilitychange', this.visibilityHandler)
    }
    if (!this.paused) el.play().catch(() => {})
  }

  private onHlsError(data: { fatal?: boolean; type?: string }) {
    if (!data.fatal) return
    if (data.type === this.errorTypes?.NETWORK_ERROR) {
      if (this.netRetries >= HLS_MAX_NETWORK_RETRIES) {
        this.rebuild()
        return
      }
      this.netRetries++
      // 指数退避：1s, 2s, 4s ... 重新 startLoad 拉最新播放列表
      window.clearTimeout(this.retryTimer)
      this.retryTimer = window.setTimeout(
        () => this.hls?.startLoad(),
        Math.min(1000 * 2 ** (this.netRetries - 1), 8000),
      )
      return
    }
    if (data.type === this.errorTypes?.MEDIA_ERROR) {
      if (this.mediaRecoveries < HLS_MAX_MEDIA_RECOVERIES) {
        this.mediaRecoveries++
        this.hls?.recoverMediaError()
        return
      }
    }
    this.rebuild()
  }

  // onVisible：回到前台时若缓冲已断（readyState 不足以继续播放），
  // 说明后台期间分片已被服务端窗口淘汰，直接重建拿最新播放列表。
  private onVisible() {
    const v = this.video
    if (!v) return
    if (v.readyState >= 3) return // HAVE_FUTURE_DATA：缓冲健康，不动它
    this.rebuild()
  }

  // rebuild：整个播放器重建。netRetries/mediaRecoveries 清零，
  // rebuilds 上限防止服务端持续异常时无限循环刷新。
  private rebuild() {
    if (this.rebuilds >= 5) return
    this.rebuilds++
    this.netRetries = 0
    this.mediaRecoveries = 0
    const el = this.video
    if (el) this.attach(el)
  }

  setBase(ts: number) {
    this.baseTs = ts
  }

  seek(ts: number) {
    if (!this.video) return
    this.video.currentTime = Math.max(0, (ts - this.baseTs) / 1000)
  }

  // 只有目标时间落在 video.seekable 区间内才能可靠跳转。
  // 会话刚建立时 ffmpeg 只产出少量分片，seekable 区间很窄；
  // 转码推进后窗口变宽，此时即可就地跳转，无需重建会话。
  canSeekTo(ts: number) {
    const v = this.video
    if (!v) return false
    const target = (ts - this.baseTs) / 1000
    if (!Number.isFinite(target) || target < 0) return false
    const ranges = v.seekable
    if (!ranges) return false
    for (let i = 0; i < ranges.length; i++) {
      if (target >= ranges.start(i) && target <= ranges.end(i)) return true
    }
    return false
  }

  setSpeed(n: number) {
    if (this.video) this.video.playbackRate = n
  }

  pause() {
    this.paused = true
    this.video?.pause()
  }

  resume() {
    this.paused = false
    this.video?.play().catch(() => {})
  }

  get time() {
    return this.baseTs + (this.video?.currentTime || 0) * 1000
  }

  destroy() {
    window.clearTimeout(this.retryTimer)
    if (this.visibilityHandler) {
      document.removeEventListener('visibilitychange', this.visibilityHandler)
      this.visibilityHandler = null
    }
    this.hls?.destroy()
    this.hls = null
    if (this.video) {
      this.video.pause()
      this.video.removeAttribute('src')
    }
    this.video = null
  }
}

// ---------- factory ----------

export interface PlayableOptions {
  url?: string
  baseTs?: number
  seed?: number
  label?: string
}

export function createPlayable(opts: PlayableOptions): Playable {
  if (opts.url) return new HlsStream(opts.url, opts.baseTs ?? 0)
  return new SimulatedStream(opts.seed ?? 0, opts.label ?? '', opts.baseTs)
}

export type { HlsType }
