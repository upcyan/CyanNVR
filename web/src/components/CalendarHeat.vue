<script setup lang="ts">
import { computed } from 'vue'
import type { DayRecord } from '../types'

const props = defineProps<{
  days: DayRecord[]
  selected: string
}>()
const emit = defineEmits<{ (e: 'select', date: string): void }>()

type Cell = { day: number; date: string; duration: number; has: boolean } | null

const cells = computed<Cell[]>(() => {
  if (!props.days.length) return []
  const first = new Date(props.days[0].date)
  const y = first.getFullYear()
  const m = first.getMonth()
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

function bg(c: NonNullable<Cell>) {
  if (!c.has) return 'transparent'
  const a = 0.16 + Math.min(c.duration, 300) / 300 * 0.5
  return `rgba(46, 204, 143, ${a.toFixed(2)})`
}
</script>

<template>
  <div class="calendar">
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
