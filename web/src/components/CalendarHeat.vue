<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { DayRecord } from '../types'

const props = defineProps<{
  days: DayRecord[]
  selected: string
}>()
const emit = defineEmits<{ (e: 'select', date: string): void }>()

type Cell = { day: number; date: string; duration: number; has: boolean } | null

// 视图内的年月：父组件 selected 决定初始值；用户在本组件内左右切月后同步给父级
const view = ref({
  y: Number(props.selected.slice(0, 4)),
  m: Number(props.selected.slice(5, 7)) - 1,
})
watch(
  () => props.selected,
  (v) => {
    view.value = {
      y: Number(v.slice(0, 4)),
      m: Number(v.slice(5, 7)) - 1,
    }
  },
)

const ymText = computed(() => `${view.value.y}年${view.value.m + 1}月`)
const cells = computed<Cell[]>(() => {
  const y = view.value.y
  const m = view.value.m
  const dim = new Date(y, m + 1, 0).getDate()
  const startDow = (new Date(y, m, 1).getDay() + 6) % 7
  const map = new Map(props.days.map((d) => [d.date, d]))
  const out: Cell[] = []
  for (let i = 0; i < startDow; i++) out.push(null)
  for (let day = 1; day <= dim; day++) {
    const date = `${y}-${String(m + 1).padStart(2, '0')}-${String(day).padStart(2, '0')}`
    const rec = map.get(date)
    out.push({
      day,
      date,
      duration: rec?.duration ?? 0,
      has: rec?.hasRecording ?? false,
    })
  }
  return out
})

function shiftMonth(delta: number) {
  const d = new Date(view.value.y, view.value.m + delta, 1)
  const date = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-01`
  emit('select', date)
}
function goToday() {
  const d = new Date()
  const date = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  emit('select', date)
}

function bg(c: NonNullable<Cell>) {
  if (!c.has) return 'transparent'
  const a = 0.16 + Math.min(c.duration, 300) / 300 * 0.5
  return `rgba(46, 204, 143, ${a.toFixed(2)})`
}
</script>

<template>
  <div class="calendar">
    <div class="cal-head">
      <van-icon name="arrow-left" size="18" class="cal-nav" @click="shiftMonth(-1)" />
      <span class="cal-ym">{{ ymText }}</span>
      <van-icon name="arrow" size="18" class="cal-nav" @click="shiftMonth(1)" />
      <button class="cal-today" @click="goToday">今日</button>
    </div>
    <div class="week">
      <span v-for="w in ['一', '二', '三', '四', '五', '六', '日']" :key="w">{{ w }}</span>
    </div>
    <div class="grid">
      <div v-for="(c, i) in cells" :key="i" class="slot">
        <div
          v-if="c"
          class="day"
          :class="{
            has: c.has,
            selected: c.date === selected,
          }"
          :style="{ background: bg(c) }"
          @click="emit('select', c.date)"
        >
          <span>{{ c.day }}</span>
          <i v-if="c.has" class="mark" />
        </div>
      </div>
    </div>
    <div class="legend">
      <span><i class="sw" style="background: rgba(46, 204, 143, 0.22)" />少量</span>
      <span><i class="sw" style="background: rgba(46, 204, 143, 0.6)" />中等</span>
      <span><i class="sw" style="background: rgba(46, 204, 143, 0.95)" />全天</span>
    </div>
  </div>
</template>

<style scoped>
.calendar {
  padding: 10px 12px;
}
.cal-head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 4px 8px;
}
.cal-nav {
  padding: 6px;
  border-radius: 50%;
  background: var(--nvr-panel-2);
  color: var(--nvr-text-2);
  cursor: pointer;
}
.cal-nav:active {
  background: rgba(46, 168, 255, 0.18);
  color: var(--nvr-accent);
}
.cal-ym {
  flex: 1;
  text-align: center;
  font-size: 15px;
  font-weight: 600;
}
.cal-today {
  border: none;
  background: var(--nvr-panel-2);
  color: var(--nvr-accent);
  font-size: 12px;
  padding: 5px 12px;
  border-radius: 999px;
  cursor: pointer;
}
.cal-today:active {
  opacity: 0.7;
}
/* 关怀模式：月份切换按钮放大 */
:global(body.care) .calendar .cal-ym {
  font-size: 18px;
}
:global(body.care) .calendar .cal-today {
  font-size: 14px;
  padding: 7px 16px;
}
:global(body.care) .calendar .cal-nav {
  font-size: 22px;
  padding: 8px;
}
.week {
  display: grid;
  grid-template-columns: repeat(7, 1fr);
  text-align: center;
  font-size: 11px;
  color: var(--nvr-text-2);
  margin-bottom: 6px;
}
.grid {
  display: grid;
  grid-template-columns: repeat(7, 1fr);
  gap: 4px;
}
.slot {
  aspect-ratio: 1.15;
}
.day {
  height: 100%;
  border-radius: 8px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  border: 1px solid transparent;
  color: var(--nvr-text);
  cursor: pointer;
  position: relative;
}
.day:active {
  opacity: 0.7;
}
.day.selected {
  border-color: var(--nvr-accent);
  box-shadow: 0 0 0 1px var(--nvr-accent) inset;
  font-weight: 700;
}
.mark {
  width: 4px;
  height: 4px;
  border-radius: 50%;
  background: var(--nvr-green);
  margin-top: 3px;
}
.legend {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  margin-top: 8px;
  font-size: 11px;
  color: var(--nvr-text-2);
}
.legend .sw {
  display: inline-block;
  width: 12px;
  height: 12px;
  border-radius: 3px;
  vertical-align: -2px;
  margin-right: 3px;
}
</style>
