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
</script>

<template>
  <div
    class="grid"
    :style="{
      gridTemplateColumns: `repeat(${nCols}, 1fr)`,
      gridTemplateRows: `repeat(${nRows}, 1fr)`,
    }"
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
</style>
