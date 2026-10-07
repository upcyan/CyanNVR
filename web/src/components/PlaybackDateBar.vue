<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

/**
 * 回放页日期选择条（前一/后一天 + 当前日期 + 打开日历）。
 *
 * 抽成组件的原因：桌面端它位于左栏日历上方，移动端则需要在播放器**下方**
 * 的内容滚动区内（播放器固定在顶部，日期条随内容滚动）。同一段交互因此有
 * 两个挂载位置，抽取组件可保证只有一份实现，避免两处逻辑漂移。
 */
const props = defineProps<{
  /** 当前日期，形如 2026-10-03 */
  date: string
  /** 是否显示「选日期」按钮（移动端内嵌日历被收起时需要） */
  showCalendarButton?: boolean
  /** 紧凑模式（移动端页签行）：优先「月-日」，空间不足时才省略年份 */
  compact?: boolean
}>()

/**
 * 日期显示：空间足够时带年份（2026-10-07），不足时压缩为月日（10-07）。
 *
 * 不能用「紧凑模式就固定月日」——那样在宽屏（平板/横屏）下明明放得下年份
 * 也被砍掉。这里按实际可用宽度自适应，并用 ResizeObserver 跟随视口与字号
 * （关怀模式会显著加宽文本）变化。
 */
const showYear = ref(true)

/**
 * 用 canvas 按元素实际字体测量文本宽度。
 * 比「按 em 估算」可靠：等宽字体、字号缩放（关怀模式）、字重都会影响宽度，
 * 估算偏差会让年份要么提前消失、要么溢出换行。
 */
function measureText(text: string, el: HTMLElement): number {
  try {
    const cs = getComputedStyle(el)
    const c = measureText.canvas ?? (measureText.canvas = document.createElement('canvas'))
    const ctx = c.getContext('2d')
    if (!ctx) return text.length * parseFloat(cs.fontSize) * 0.62
    ctx.font = `${cs.fontWeight} ${cs.fontSize} ${cs.fontFamily}`
    return ctx.measureText(text).width
  } catch {
    return text.length * 15 * 0.62
  }
}
measureText.canvas = null as HTMLCanvasElement | null

const barRef = ref<HTMLElement | null>(null)

/**
 * 依据容器可用宽度决定是否显示年份。
 * 扣掉左右两个切换按钮与 gap 后的剩余宽度即日期可用空间。
 */
function measure() {
  const el = barRef.value
  if (!el) return
  if (!props.compact) {
    showYear.value = true
    return
  }
  const dateEl = el.querySelector('.date') as HTMLElement | null
  if (!dateEl) return
  const cs = getComputedStyle(el)
  const gap = parseFloat(cs.gap) || 0
  let used = 0
  const btns = el.querySelectorAll('.control-button')
  btns.forEach((b) => (used += (b as HTMLElement).getBoundingClientRect().width))
  const avail = el.clientWidth - used - gap * Math.max(btns.length, 1)
  // 留 4px 余量，避免刚好卡在边界反复抖动
  showYear.value = avail >= measureText(props.date, dateEl) + 4
}

let ro: ResizeObserver | null = null
let fontObserver: MutationObserver | null = null
onMounted(() => {
  measure()
  if (typeof ResizeObserver !== 'undefined') {
    ro = new ResizeObserver(() => measure())
    if (barRef.value) ro.observe(barRef.value)
  }
  // 切换关怀模式时字体会变，但容器宽度可能不变，ResizeObserver 不一定触发。
  fontObserver = new MutationObserver(() => nextTick(measure))
  fontObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['style', 'data-font-size'] })
  fontObserver.observe(document.body, { attributes: true, attributeFilter: ['class'] })
  window.addEventListener('resize', measure)
})
onBeforeUnmount(() => {
  ro?.disconnect()
  fontObserver?.disconnect()
  window.removeEventListener('resize', measure)
})
// 日期变化（跨天切换）后重新测量
watch(() => props.date, () => nextTick(measure))

const displayDate = computed(() =>
  props.compact && !showYear.value ? (props.date || '').slice(5) : props.date,
)

const emit = defineEmits<{
  (e: 'prev'): void
  (e: 'next'): void
  (e: 'pick-calendar'): void
}>()
</script>

<template>
  <div ref="barRef" class="date-bar">
    <button type="button" class="control-button" aria-label="前一天" @click="emit('prev')">
      <van-icon name="arrow-left" size="20" />
    </button>
    <!-- 紧凑模式（移动端页签行）：日期本身就是日历入口，点击弹出月历 -->
    <span
      class="date mono"
      :class="{ tappable: compact }"
      :role="compact ? 'button' : undefined"
      :tabindex="compact ? 0 : undefined"
      :aria-label="compact ? '选择日期' : undefined"
      @click="compact && emit('pick-calendar')"
      @keydown.enter="compact && emit('pick-calendar')"
    >{{ displayDate }}</span>
    <button type="button" class="control-button" aria-label="后一天" @click="emit('next')">
      <van-icon name="arrow" size="20" />
    </button>
    <button
      v-if="showCalendarButton"
      type="button"
      class="control-button cal-toggle"
      aria-label="选择日期"
      @click="emit('pick-calendar')"
    >
      <van-icon name="calendar-o" size="18" />
    </button>
  </div>
</template>

<style scoped>
.date-bar {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 22px;
  padding: 8px 0;
  font-size: calc(15px * var(--nvr-font-scale, 1));
  font-weight: 600;
}
.date-bar .date {
  white-space: nowrap;
}
/* 紧凑模式：日期即日历入口，给出可点击提示 */
.date-bar .date.tappable {
  cursor: pointer;
  text-decoration: underline dotted;
  text-underline-offset: 3px;
}
.date-bar .date.tappable:active {
  color: var(--nvr-accent);
}
.cal-toggle {
  display: none;
}
@media (max-width: 899px) {
  .cal-toggle {
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
}
:global(body.care .date-bar) {
  font-size: calc(18px * var(--nvr-font-scale, 1));
}
:global(body.care .cal-toggle) {
  width: 44px;
  height: 44px;
}
</style>
