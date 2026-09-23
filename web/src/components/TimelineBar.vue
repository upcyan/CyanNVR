<script setup lang="ts">
import { computed, ref } from 'vue'
import type { RecordingSegment } from '../types'

const props = defineProps<{
  dayStart: number
  segments: RecordingSegment[]
  value: number
  /** 当日事件（time 已由调用方转为毫秒）；用于时间轴着色与刻度 */
  events?: { time: number; type: string }[]
}>()

const EV_STYLE: Record<string, string> = {
  motion: 'rgba(255, 176, 32, .38)',
  ai: 'rgba(255, 77, 79, .38)',
  manual: 'rgba(162, 107, 240, .38)',
  offline: 'rgba(139, 147, 167, .38)',
  online: 'rgba(46, 204, 143, .30)',
}
const EV_TICK: Record<string, string> = {
  motion: '#ffb020',
  ai: '#ff4d4f',
  manual: '#a26bf0',
  offline: '#8b93a7',
  online: '#2ecc8f',
}

const evList = computed(() => props.events ?? [])

// 含事件的录像段整段着色；同段多事件时按首个事件取色
const eventRanges = computed(() => {
  const out: { id: string; start: number; end: number; color: string }[] = []
  for (const s of props.segments) {
    const hit = evList.value.find((e) => e.time >= s.start && e.time <= s.end)
    if (hit) {
      out.push({
        id: s.id,
        start: s.start,
        end: s.end,
        color: EV_STYLE[hit.type] || EV_STYLE.motion,
      })
    }
  }
  return out
})
const emit = defineEmits<{
  (e: 'seek', ts: number): void
  (e: 'seekend', ts: number): void
}>()

const trackRef = ref<HTMLElement | null>(null)
const DAY = 86400000

const pct = (ts: number) =>
  Math.min(100, Math.max(0, ((ts - props.dayStart) / DAY) * 100))

let dragging = false

function ratioFromX(clientX: number): number {
  const el = trackRef.value
  if (!el) return 0
  const rect = el.getBoundingClientRect()
  return Math.min(1, Math.max(0, (clientX - rect.left) / rect.width))
}

function tsFromX(clientX: number): number {
  return props.dayStart + ratioFromX(clientX) * DAY
}

function handleMove(e: PointerEvent) {
  emit('seek', tsFromX(e.clientX))
}

function onDown(e: PointerEvent) {
  dragging = true
  ;(e.currentTarget as Element).setPointerCapture?.(e.pointerId)
  handleMove(e)
}
function onMove(e: PointerEvent) {
  if (dragging) handleMove(e)
}
// 松手时提交最终位置：pointermove 不一定落在抬手的那一点，
// 只靠 move 会出现「拖到末端却停在中间」的偏差。
function onUp(e: PointerEvent) {
  if (!dragging) return
  dragging = false
  const ts = tsFromX(e.clientX)
  emit('seek', ts)
  emit('seekend', ts)
}

const timeLabel = computed(() => {
  const ms = props.value - props.dayStart
  const d = new Date(ms)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
})
</script>

<template>
  <div class="timeline">
    <div
      ref="trackRef"
      class="track"
      @pointerdown="onDown"
      @pointermove="onMove"
      @pointerup="onUp"
      @pointercancel="onUp"
    >
      <div class="seg"
        v-for="s in segments"
        :key="s.id"
        :style="{
          left: pct(s.start) + '%',
          width: Math.max(0.5, pct(s.end) - pct(s.start)) + '%',
        }"
      />
      <div
        class="ev-range"
        v-for="r in eventRanges"
        :key="'er-' + r.id"
        :style="{
          left: pct(r.start) + '%',
          width: Math.max(0.5, pct(r.end) - pct(r.start)) + '%',
          background: r.color,
        }"
      />
      <div
        class="ev-tick"
        v-for="(e, i) in evList"
        :key="'et-' + i"
        :style="{ left: pct(e.time) + '%', background: EV_TICK[e.type] || '#ff4d4f' }"
      />
      <div class="cursor" :style="{ left: pct(value) + '%' }">
        <span class="knob" />
      </div>
    </div>
    <div class="labels">
      <span>00:00</span>
      <span class="current mono">{{ timeLabel }}</span>
      <span>24:00</span>
    </div>
  </div>
</template>

<style scoped>
.timeline {
  padding: 6px 14px 10px;
}
.track {
  position: relative;
  height: 36px;
  background: var(--nvr-panel-2);
  border-radius: 8px;
  border: 1px solid var(--nvr-border);
  cursor: ew-resize;
  touch-action: none;
  overflow: hidden;
}
.seg {
  position: absolute;
  top: 0;
  bottom: 0;
  background: rgba(46, 204, 143, 0.55);
  border-right: 1px solid rgba(46, 204, 143, 0.9);
}
.ev-range {
  position: absolute;
  top: 0;
  bottom: 0;
  pointer-events: none;
}
.ev-tick {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 3px;
  transform: translateX(-1.5px);
  pointer-events: none;
  z-index: 2;
}
.cursor {
  position: absolute;
  top: 0;
  bottom: 0;
  z-index: 3;
  width: 2px;
  background: var(--nvr-accent);
  transform: translateX(-1px);
}
.knob {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: #fff;
  border: 3px solid var(--nvr-accent);
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.4);
}
.labels {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 4px;
  font-size: 11px;
  color: var(--nvr-text-2);
}
.labels .current {
  color: var(--nvr-accent);
  font-weight: 600;
}
</style>
