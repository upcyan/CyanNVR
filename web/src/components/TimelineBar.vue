<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import type { RecordingSegment } from '../types'

export interface TimelineMark {
  id: string
  time: number
  note?: string
}

/**
 * 窗口化时间轴：可跨日无限滑动 + 双指捏合改变时间分辨率。
 *
 * 与旧版的根本区别：
 *   旧版以「一天」为单位（dayStart 固定 + 24 小时视窗），只能看当天，
 *   捏合也无从谈起——视窗永远是 24 小时。
 *   新版以「可视窗口 [viewStart, viewStart+viewSpan)」为单位，
 *   滑动即平移窗口（可跨越任意日期），捏合即缩放窗口跨度
 *   （从「看 7 天」到「看 10 秒」，实现粗筛与精调）。
 *
 * 手势分工（与主流 NVR / 地图应用一致）：
 *   · 单指拖动 → 平移窗口（滚动时间）
 *   · 单指轻点 → 跳转到该时刻
 *   · 双指捏合 → 缩放窗口跨度（改变时间分辨率，即「采样间隔」）
 *   · 滚轮 → 平移；Ctrl/⌘ + 滚轮 → 缩放
 *
 * 为什么把「拖动」从「拖动跳转」改成「平移」：
 *   跨日无限滑动是本组件的核心能力，若拖动仍用于跳转，就没有手势能滚动时间。
 *   跳转改由轻点承担（点击远比拖动精确，精调反而更容易）。
 */
const props = defineProps<{
  /** 可视窗口起点（毫秒） */
  viewStart: number
  /** 可视窗口跨度（毫秒）。越小越精细 */
  viewSpan: number
  segments: RecordingSegment[]
  value: number
  /** 事件（time 已由调用方转为毫秒）；用于时间轴着色 */
  events?: { time: number; type: string }[]
  /** 用户标记：持久化对象，支持点击跳转与删除 */
  marks?: TimelineMark[]
  /** 录像时间边界：限制可滑动范围；不传则允许任意滑动 */
  bounds?: { earliest: number; latest: number }
}>()

const emit = defineEmits<{
  (e: 'update:viewStart', v: number): void
  (e: 'update:viewSpan', v: number): void
  (e: 'seek', ts: number): void
  (e: 'seekend', ts: number): void
  /** 导航到某时刻（Home/End）：即使当前无录像也应生效 */
  (e: 'navigate', ts: number): void
  (e: 'markseek', mark: TimelineMark): void
  (e: 'markdelete', mark: TimelineMark): void
  /** 窗口变化（滑动/缩放后）——调用方据此按需加载该范围的录像段 */
  (e: 'rangechange', range: { from: number; to: number }): void
}>()

const EV_TICK: Record<string, string> = {
  motion: '#ffb020',
  ai: '#ff4d4f',
  manual: '#a26bf0',
  offline: '#8b93a7',
  online: '#2ecc8f',
}

const evList = computed(() => props.events ?? [])
const markList = computed(() => props.marks ?? [])

// 缩放范围：30 秒（精调到秒）~ 7 天（快速跨日粗筛）
const MIN_SPAN = 30_000
const MAX_SPAN = 7 * 86400_000

const trackRef = ref<HTMLElement | null>(null)
const activeMarkId = ref<string | null>(null)

// ---- 坐标换算 ----
const span = () => Math.max(1, props.viewSpan)
/** 时间 → 轨道内百分比 */
const pct = (ts: number) => ((ts - props.viewStart) / span()) * 100
/**
 * 只渲染落在可视窗口内的元素。
 *
 * 为什么必须过滤：pct() 对窗口外的时间会返回极大值（如缩放后某个事件
 * 落在 800 个窗口之外 → left: 533508px）。因为 .track 用了 overflow:visible
 * （标记弹层需要溢出到轨道外），这些超远元素会撑出巨大的滚动宽度，
 * 在部分布局下把时间轴两侧的按钮盖住。
 * 过滤后既消除溢出，也顺带省掉大量无意义的 DOM。
 * 留 1% 余量让贴边元素不被切掉。
 */
function inWindow(ts: number): boolean {
  const p = pct(ts)
  return p >= -1 && p <= 101
}
const visibleEvents = computed(() => evList.value.filter((e) => inWindow(e.time)))
const visibleMarks = computed(() => markList.value.filter((m) => inWindow(m.time)))
/**
 * 录像段：与窗口有交集即渲染（段可能很长，一端在窗口外），
 * 但把 left/width 夹到 [0,100] 内，避免负值或超宽撑破轨道。
 */
const visibleSegments = computed(() =>
  props.segments
    .filter((s) => s.end > props.viewStart && s.start < props.viewStart + span())
    .map((s) => {
      const a = Math.max(0, pct(s.start))
      const b = Math.min(100, pct(s.end))
      return { id: s.id, left: a, width: Math.max(0.15, b - a) }
    }),
)
/** 轨道内比例 → 时间 */
function tsFromRatio(r: number): number {
  return props.viewStart + r * span()
}
function rectWidth(): number {
  return trackRef.value?.getBoundingClientRect().width || 1
}
function ratioFromX(clientX: number): number {
  const el = trackRef.value
  if (!el) return 0
  const rect = el.getBoundingClientRect()
  return Math.min(1, Math.max(0, (clientX - rect.left) / rect.width))
}

function fmtMarkTime(ts: number): string {
  const d = new Date(ts)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

// ---- 窗口平移 ----
/** 把窗口起点限制在录像边界内（留一屏余量，避免滑到完全空白处） */
function clampStart(start: number, s: number): number {
  const b = props.bounds
  if (!b) return start
  const lo = b.earliest - s * 0.5
  const hi = b.latest - s * 0.5
  if (hi < lo) return lo
  return Math.min(hi, Math.max(lo, start))
}

function setWindow(start: number, s: number) {
  const ns = Math.min(MAX_SPAN, Math.max(MIN_SPAN, s))
  const st = clampStart(start, ns)
  if (st !== props.viewStart) emit('update:viewStart', st)
  if (ns !== props.viewSpan) emit('update:viewSpan', ns)
  emit('rangechange', { from: st, to: st + ns })
}

/** 缩放：以 anchorRatio 处的时刻为锚点，保证手指下的时间不动 */
function zoomAt(anchorRatio: number, factor: number) {
  const anchorTime = tsFromRatio(anchorRatio)
  const ns = Math.min(MAX_SPAN, Math.max(MIN_SPAN, span() / factor))
  const nr = ns / span()
  setWindow(anchorTime - anchorRatio * ns, ns)
  // nr 仅用于说明换算意图，实际窗口已由 setWindow 统一设置
  void nr
}

// ---- 手势：单指平移 / 双指捏合 ----
const pointers = new Map<number, { x: number; y: number }>()
let panStartX = 0
let panStartView = 0
let moved = false
let pinchStartDist = 0
let pinchStartSpan = 0
let pinchAnchorRatio = 0.5
/** 轻点判定：位移与时长都在阈值内才算跳转，否则视为平移 */
const TAP_MOVE_PX = 8
let downAt = 0

function onDown(e: PointerEvent) {
  const el = trackRef.value
  if (!el) return
  // 右键/中键不参与：避免右键弹出上下文菜单时误跳转（回归有断言）
  if (e.button !== 0) return
  pointers.set(e.pointerId, { x: e.clientX, y: e.clientY })
  el.setPointerCapture?.(e.pointerId)
  activeMarkId.value = null

  if (pointers.size === 1) {
    panStartX = e.clientX
    panStartView = props.viewStart
    moved = false
    downAt = Date.now()
  } else if (pointers.size === 2) {
    const [a, b] = [...pointers.values()]
    pinchStartDist = Math.hypot(a.x - b.x, a.y - b.y) || 1
    pinchStartSpan = span()
    pinchAnchorRatio = ratioFromX((a.x + b.x) / 2)
  }
}

function onMove(e: PointerEvent) {
  if (!pointers.has(e.pointerId)) return
  pointers.set(e.pointerId, { x: e.clientX, y: e.clientY })

  if (pointers.size >= 2) {
    // 双指捏合：距离变化 → 跨度反向变化
    const [a, b] = [...pointers.values()]
    const dist = Math.hypot(a.x - b.x, a.y - b.y) || 1
    const factor = dist / pinchStartDist
    const ns = Math.min(MAX_SPAN, Math.max(MIN_SPAN, pinchStartSpan / factor))
    const anchorTime = props.viewStart + pinchAnchorRatio * pinchStartSpan
    setWindow(anchorTime - pinchAnchorRatio * ns, ns)
    moved = true
    return
  }

  // 单指平移：像素位移 → 时间位移
  const dx = e.clientX - panStartX
  if (Math.abs(dx) > TAP_MOVE_PX) moved = true
  if (moved) {
    const dt = (-dx / rectWidth()) * span()
    setWindow(panStartView + dt, span())
  }
}

function onUp(e: PointerEvent) {
  const el = trackRef.value
  const wasTracked = pointers.has(e.pointerId)
  pointers.delete(e.pointerId)
  if (el?.hasPointerCapture?.(e.pointerId)) el.releasePointerCapture(e.pointerId)

  // moved 必须无条件复位：右键/中键在这里提前 return，
  // 若不复位就会把上一次拖拽的 moved=true 泄漏给下一次交互，
  // 导致后续轻点被误判为拖拽（或反之），行为随机。
  const wasMoved = moved
  if (pointers.size === 0) moved = false

  // 非主键（右键/中键）不参与跳转：pointerdown 已被拦，
  // 但 pointerup 仍会到达，不过滤会把右键当成「轻点跳转」而 seek
  if (e.button !== 0 || !wasTracked) return
  if (pointers.size > 0) return

  // 未发生位移且按下时间短 → 视为轻点跳转
  const quick = Date.now() - downAt < 400
  if (!wasMoved && quick) {
    const ts = tsFromRatio(ratioFromX(e.clientX))
    emit('seek', ts)
    emit('seekend', ts)
  }
}

function onCancel(e: PointerEvent) {
  pointers.delete(e.pointerId)
  if (pointers.size === 0) moved = false
}

/** 指针离开轨道时的兜底复位（capture 失效等异常路径） */
function onLostCapture(e: PointerEvent) {
  pointers.delete(e.pointerId)
  if (pointers.size === 0) moved = false
}

/** 滚轮：平移；Ctrl/⌘ 缩放（桌面端无捏合手势的替代） */
function onWheel(e: WheelEvent) {
  if (e.ctrlKey || e.metaKey) {
    e.preventDefault()
    const factor = e.deltaY < 0 ? 1.15 : 1 / 1.15
    zoomAt(ratioFromX(e.clientX), factor)
  } else {
    e.preventDefault()
    const dt = (e.deltaY / rectWidth()) * span()
    setWindow(props.viewStart + dt, span())
  }
}

// ---- 标记交互 ----
let markLeaveTimer = 0
function onMarkClick(m: TimelineMark) {
  activeMarkId.value = m.id
  emit('markseek', m)
}
function onMarkDelete(m: TimelineMark) {
  activeMarkId.value = null
  emit('markdelete', m)
}
function onMarkLeave(m: TimelineMark) {
  window.clearTimeout(markLeaveTimer)
  markLeaveTimer = window.setTimeout(() => {
    if (activeMarkId.value === m.id) activeMarkId.value = null
  }, 400)
}
onBeforeUnmount(() => window.clearTimeout(markLeaveTimer))

// ---- 刻度：随缩放自动切换分辨率（「采样间隔」的可视体现）----
// 粗看（跨度大）显示日期，细看（跨度小）显示到秒——这就是用户要的
// 「精调时间或快速跳转」在同一控件上的两种形态。
const TICK_STEPS = [
  1000, 5000, 15000, 30000, 60000, 300000, 900000, 1800000,
  3600000, 3 * 3600000, 6 * 3600000, 12 * 3600000,
  86400000, 2 * 86400000, 7 * 86400000,
]

/** 选一个「刻度数不超过 8 个」的最小步长 */
const tickStep = computed(() => {
  const s = span()
  for (const st of TICK_STEPS) {
    if (s / st <= 8) return st
  }
  return TICK_STEPS[TICK_STEPS.length - 1]
})

const ticks = computed(() => {
  const st = tickStep.value
  const s = span()
  const first = Math.ceil(props.viewStart / st) * st
  const out: { ts: number; label: string; major: boolean }[] = []
  for (let t = first; t <= props.viewStart + s; t += st) {
    out.push({ ts: t, label: tickLabel(t, st), major: t % 86400000 === 0 })
    if (out.length > 40) break // 防御：异常缩放下的无限循环
  }
  return out
})

function tickLabel(ts: number, st: number): string {
  const d = new Date(ts)
  const p = (n: number) => String(n).padStart(2, '0')
  // 步长 >= 1 天：显示日期
  if (st >= 86400000) {
    return `${d.getMonth() + 1}/${d.getDate()}`
  }
  // 步长 >= 1 小时：显示 HH:00
  if (st >= 3600000) {
    return `${p(d.getHours())}:00`
  }
  // 步长 >= 1 分钟：显示 HH:MM
  if (st >= 60000) {
    return `${p(d.getHours())}:${p(d.getMinutes())}`
  }
  // 秒级：显示 HH:MM:SS（精调模式）
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

/** 当前分辨率的人话描述，显示在标签行 */
const resolutionLabel = computed(() => {
  const s = span()
  const d = new Date(props.value)
  const p = (n: number) => String(n).padStart(2, '0')
  const cur = `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
  if (s >= 86400000) return `${cur} · 窗宽 ${(s / 86400000).toFixed(1)} 天`
  if (s >= 3600000) return `${cur} · 窗宽 ${(s / 3600000).toFixed(1)} 小时`
  if (s >= 60000) return `${cur} · 窗宽 ${Math.round(s / 60000)} 分钟`
  return `${cur} · 窗宽 ${Math.round(s / 1000)} 秒`
})

/** 光标是否在当前窗口内（滑走后提示可一键回到播放位置） */
const cursorVisible = computed(() => props.value >= props.viewStart && props.value <= props.viewStart + span())

function centerOnValue() {
  setWindow(props.value - span() / 2, span())
}

// 双击：以该点为中心放大一档（快速精调）
let lastTapAt = 0
let lastTapX = 0
function onDoubleTap(e: MouseEvent) {
  const now = Date.now()
  if (now - lastTapAt < 300 && Math.abs(e.clientX - lastTapX) < 20) {
    zoomAt(ratioFromX(e.clientX), 2)
    lastTapAt = 0
    return
  }
  lastTapAt = now
  lastTapX = e.clientX
}

onMounted(() => {
  const el = trackRef.value
  el?.addEventListener('wheel', onWheel, { passive: false })
  el?.addEventListener('dblclick', onDoubleTap)
})
onBeforeUnmount(() => {
  const el = trackRef.value
  el?.removeEventListener('wheel', onWheel)
  el?.removeEventListener('dblclick', onDoubleTap)
})

/**
 * 键盘操作（无障碍 + 精确微调）。
 *
 * 窗口化后 Home/End 语义改为「跳到窗口首/尾」而非「当天 00:00/24:00」，
 * 方向键按当前刻度步长移动——步长随缩放变化，放大后自然微调得更细。
 */
function onKeydown(e: KeyboardEvent) {
  const step = e.shiftKey ? tickStep.value * 5 : tickStep.value
  let ts: number
  let nav = false
  if (e.key === 'Home') {
    // 当天 00:00（不是窗口起点）：aria-valuenow 以「当天秒偏移」为契约，
    // Home 必须能回到 0，否则无障碍语义与视觉不一致
    ts = dayStartOf(props.value)
    nav = true
  } else if (e.key === 'End') {
    ts = dayStartOf(props.value) + 86400000 - 1000
    nav = true
  } else if (e.key === 'ArrowLeft' || e.key === 'ArrowDown') {
    ts = props.value - step
  } else if (e.key === 'ArrowRight' || e.key === 'ArrowUp') {
    ts = props.value + step
  } else return
  e.preventDefault()
  if (nav) {
    // Home/End 是窗口导航：无录像时也要把窗口与光标带过去，
    // 否则用户按 Home 毫无反应（无录像日尤其明显）
    emit('navigate', ts)
  } else {
    emit('seek', ts)
    emit('seekend', ts)
  }
}

/** 某个时刻所属自然日的 00:00（本地时区） */
function dayStartOf(ts: number): number {
  const d = new Date(ts)
  d.setHours(0, 0, 0, 0)
  return d.getTime()
}

/** 光标在当天的秒偏移（aria-valuenow 契约：0..86400） */
const secondsOfDay = computed(() => {
  const off = Math.floor((props.value - dayStartOf(props.value)) / 1000)
  return Math.max(0, Math.min(86400, off))
})

/** 供父组件调用：把窗口移到某时刻（如从事件列表跳转） */
function ensureVisible(ts: number) {
  if (ts < props.viewStart || ts > props.viewStart + span()) {
    setWindow(ts - span() / 2, span())
  }
}
defineExpose({ ensureVisible, setWindow })
</script>

<template>
  <div class="timeline">
    <div
      ref="trackRef"
      class="track"
      role="slider"
      tabindex="0"
      aria-label="录像时间轴（可滑动跨日、双指捏合缩放）"
      :aria-valuemin="0"
      :aria-valuemax="86400"
      :aria-valuenow="secondsOfDay"
      :aria-valuetext="resolutionLabel"
      @pointerdown="onDown"
      @pointermove="onMove"
      @pointerup="onUp"
      @pointercancel="onCancel"
      @lostpointercapture="onLostCapture"
      @keydown="onKeydown"
    >
      <div
        v-for="s in visibleSegments"
        :key="s.id"
        class="seg"
        :style="{ left: s.left + '%', width: s.width + '%' }"
      />
      <div
        v-for="e in visibleEvents"
        :key="'et-' + e.time"
        class="ev-tick"
        :style="{ left: pct(e.time) + '%', background: EV_TICK[e.type] || '#ff4d4f' }"
      />
      <!-- 刻度：随缩放切换分辨率 -->
      <div
        v-for="t in ticks"
        :key="'tk-' + t.ts"
        class="tick"
        :class="{ major: t.major }"
        :style="{ left: pct(t.ts) + '%' }"
      >
        <span class="tick-label">{{ t.label }}</span>
      </div>
      <!-- 标记：可点击跳转与删除 -->
      <div
        v-for="m in visibleMarks"
        :key="m.id"
        class="mark-flag"
        :class="{ active: activeMarkId === m.id }"
        :style="{ left: pct(m.time) + '%' }"
        role="button"
        tabindex="0"
        :aria-label="'跳转到标记 ' + fmtMarkTime(m.time)"
        :title="fmtMarkTime(m.time) + (m.note ? ' · ' + m.note : '')"
        @pointerdown.stop
        @click.stop="onMarkClick(m)"
        @keydown.enter.stop="onMarkClick(m)"
        @mouseenter="activeMarkId = m.id"
        @mouseleave="onMarkLeave(m)"
      >
        <span class="mark-hit" />
        <span class="mark-pin" />
        <span v-if="activeMarkId === m.id" class="mark-pop" @click.stop>
          <span class="mark-pop-time mono">{{ fmtMarkTime(m.time) }}</span>
          <span v-if="m.note" class="mark-pop-note">{{ m.note }}</span>
          <button type="button" class="mark-del" aria-label="删除该标记" @click.stop="onMarkDelete(m)">删除</button>
        </span>
      </div>
      <div v-if="cursorVisible" class="cursor" :style="{ left: pct(value) + '%' }">
        <span class="knob" />
      </div>
    </div>
    <div class="labels">
      <button
        v-if="!cursorVisible"
        type="button"
        class="recenter"
        @click="centerOnValue"
      >回到播放位置</button>
      <span class="current mono">{{ resolutionLabel }}</span>
      <span class="hint">拖动滑动 · 双指缩放</span>
    </div>
  </div>
</template>

<style scoped>
.timeline {
  padding: 4px 14px 4px;
  user-select: none;
  -webkit-user-select: none;
  /* 建立层级上下文：标记弹层要盖住下方相邻区块 */
  position: relative;
  z-index: 3;
}
.track {
  position: relative;
  /* 高度随视口高度收缩：横屏（如 844x390）纵向空间紧张，
     固定高度会让时间轴顶到底部导航造成溢出 */
  height: clamp(28px, 4.4vh, 38px);
  background: var(--nvr-panel-2);
  border-radius: 8px;
  border: 1px solid var(--nvr-border);
  cursor: grab;
  /* 交给指针事件自行处理平移/缩放，禁用浏览器默认滚动与缩放 */
  touch-action: none;
  /* 横向裁掉越界内容（刻度标签贴边时会溢出一小截），
     但纵向保持 visible——标记弹层是向下展开的，需要溢出到轨道外才点得到。
     clip-path 能分别控制两个方向，overflow 做不到。 */
  clip-path: inset(-200px 0px -200px 0px);
}
.track:active {
  cursor: grabbing;
}
.seg {
  position: absolute;
  top: 0;
  bottom: 0;
  background: rgba(46, 204, 143, 0.55);
  border-right: 1px solid rgba(46, 204, 143, 0.9);
}
.ev-tick {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 2px;
  transform: translateX(-1px);
  pointer-events: none;
  z-index: 2;
  opacity: 0.8;
}
/* 刻度线：细看时给用户秒级参照 */
.tick {
  position: absolute;
  top: 0;
  height: 6px;
  width: 1px;
  background: rgba(255, 255, 255, 0.22);
  pointer-events: none;
  z-index: 1;
}
.tick.major {
  height: 10px;
  background: rgba(255, 255, 255, 0.4);
}
.tick-label {
  position: absolute;
  top: 10px;
  left: 2px;
  font-size: calc(9px * var(--nvr-font-scale, 1));
  color: rgba(255, 255, 255, 0.5);
  white-space: nowrap;
  pointer-events: none;
  /* 标签宽度有限，靠右的标签允许向左伸展，避免越过轨道右边界 */
  max-width: 60px;
  overflow: hidden;
  text-overflow: clip;
}
/* 标记旗标：可点击，横向用 .mark-hit 扩出 24px 命中区 */
.mark-flag {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 2px;
  cursor: pointer;
  z-index: 4;
}
.mark-flag .mark-pin {
  position: absolute;
  top: 0;
  left: 0;
  width: 2px;
  height: 12px;
  background: #ffd447;
}
.mark-flag .mark-pin::after {
  content: '';
  position: absolute;
  top: 0;
  left: -4px;
  border: 5px solid transparent;
  border-top-color: #ffd447;
}
.mark-hit {
  position: absolute;
  top: 0;
  bottom: 0;
  left: -11px;
  width: 24px;
}
.mark-flag.active .mark-pin,
.mark-flag:hover .mark-pin {
  background: #fff;
  box-shadow: 0 0 6px rgba(255, 212, 71, 0.9);
}
.mark-flag.active .mark-pin::after,
.mark-flag:hover .mark-pin::after {
  border-top-color: #fff;
}
.mark-pop {
  position: absolute;
  /* 向下展开：轨道上方紧邻 .tl-head（会拦截点击），且 .track 的
     overflow:hidden 会把向上溢出的弹层裁掉、导致删除按钮点不到。 */
  top: calc(100% + 4px);
  left: 0;
  transform: translateX(-50%);
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 6px 10px;
  border-radius: var(--nvr-radius-sm);
  background: rgba(0, 0, 0, 0.86);
  border: 1px solid rgba(255, 212, 71, 0.5);
  color: #fff;
  font-size: calc(11px * var(--nvr-font-scale, 1));
  white-space: nowrap;
  z-index: 30;
  cursor: default;
}
.mark-pop-time {
  font-weight: 600;
}
.mark-pop-note {
  color: rgba(255, 255, 255, 0.75);
  max-width: 200px;
  overflow: hidden;
  text-overflow: ellipsis;
}
.mark-del {
  align-self: flex-start;
  padding: 2px 10px;
  border: 1px solid rgba(255, 77, 79, 0.7);
  border-radius: var(--nvr-radius-full);
  background: rgba(255, 77, 79, 0.16);
  color: #ff8b8d;
  font-size: calc(11px * var(--nvr-font-scale, 1));
  cursor: pointer;
}
.mark-del:hover {
  background: rgba(255, 77, 79, 0.32);
  color: #fff;
}
.cursor {
  position: absolute;
  top: 0;
  bottom: 0;
  z-index: 5;
  width: 2px;
  background: var(--nvr-accent);
  transform: translateX(-1px);
  pointer-events: none;
}
.knob {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  width: 14px;
  height: 14px;
  border-radius: 50%;
  background: #fff;
  border: 3px solid var(--nvr-accent);
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.4);
}
.labels {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-top: 2px;
  font-size: calc(11px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
  /* 单行不换行：关怀模式字号放大 + 窄屏时，提示文字换行会把整个
     时间轴撑高几百像素，把下方内容顶出视口 */
  flex-wrap: nowrap;
  white-space: nowrap;
  overflow: hidden;
}
.labels > * {
  flex: 0 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}
.labels .current {
  color: var(--nvr-accent);
  font-weight: 600;
  white-space: nowrap;
}
.labels .hint {
  color: var(--nvr-text-3);
  font-size: calc(10px * var(--nvr-font-scale, 1));
  flex: 0 0 auto;
}
/* 窄屏/矮屏隐藏手势提示：空间不足时优先保证时间与窗宽可读 */
@media (max-width: 480px), (max-height: 480px) {
  .labels .hint {
    display: none;
  }
}
.recenter {
  padding: 2px 8px;
  border: 1px solid var(--nvr-border);
  border-radius: var(--nvr-radius-full);
  background: var(--nvr-panel-2);
  color: var(--nvr-accent);
  font-size: calc(11px * var(--nvr-font-scale, 1));
  cursor: pointer;
}
/* 关怀模式：时间标签放大（11px 对适老场景过小） */
:global(body.care .timeline .labels) {
  font-size: calc(14px * var(--nvr-font-scale, 1));
}
:global(body.care .timeline .track) {
  /* 关怀模式放大但同样受视口高度约束，避免把内容顶出屏幕 */
  height: clamp(32px, 5vh, 44px);
}
</style>
