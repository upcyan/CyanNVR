<script setup lang="ts">
import { computed } from 'vue'
import type { Device } from '../types'
import GridCell from './GridCell.vue'

const props = defineProps<{
  devices: Device[]
  cols?: number // 0 or undefined = auto square layout
  streamUrlFor?: (d: Device) => string | undefined
}>()
const emit = defineEmits<{ (e: 'cell', d: Device): void }>()

const nCols = computed(() => {
  if (props.cols && props.cols > 0) return props.cols
  return Math.max(1, Math.ceil(Math.sqrt(props.devices.length)))
})
const nRows = computed(() => {
  if (props.cols && props.cols > 0) return Math.max(1, Math.ceil(props.devices.length / props.cols))
  return nCols.value
})
// 单元格最小高度（cols=1 时 36vh）由下方 scoped 样式 .grid-single 负责，
// 模板里无需运行时计算；早前遗留的 cellMinHeight 已删除（vue-tsc TS6133）。
</script>

<template>
  <div
    class="grid"
    :style="{
      gridTemplateColumns: `repeat(${nCols}, 1fr)`,
      gridTemplateRows: `repeat(${nRows}, 1fr)`,
    }"
    :class="{ 'grid-single': props.cols === 1 }"
  >
    <GridCell
      v-for="d in devices"
      :key="d.id"
      :device="d"
      :stream-url="streamUrlFor?.(d)"
      @click="emit('cell', d)"
    />
  </div>
</template>

<style scoped>
.grid {
  flex: 1;
  min-height: 0;
  display: grid;
  gap: 6px;
  padding: 10px;
  overflow-y: auto;
}
/* cols=1 单列时，让每行有合理高度，画面不会被拉伸到异常形状 */
.grid-single :deep(.cell) {
  min-height: 36vh;
}
</style>
