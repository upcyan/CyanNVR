<script setup lang="ts">
import { computed } from 'vue'

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
  /** 紧凑模式（移动端页签行）：只显示「月-日」——窄屏下年份会把日期挤成两行 */
  compact?: boolean
}>()

const displayDate = computed(() => (props.compact ? props.date.slice(5) : props.date))

const emit = defineEmits<{
  (e: 'prev'): void
  (e: 'next'): void
  (e: 'pick-calendar'): void
}>()
</script>

<template>
  <div class="date-bar">
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
