<script setup lang="ts">
import type { Device } from '../types'
import GridCell from './GridCell.vue'

defineProps<{ devices: Device[]; streamUrlFor?: (d: Device) => string | undefined }>()
const emit = defineEmits<{ (e: 'cell', d: Device): void }>()
</script>

<template>
  <div
    class="grid"
    :style="{
      gridTemplateColumns: `repeat(${Math.max(1, Math.ceil(Math.sqrt(devices.length)))}, 1fr)`,
      gridTemplateRows: `repeat(${Math.max(1, Math.ceil(Math.sqrt(devices.length)))}, 1fr)`,
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
}
</style>
