<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { Device } from '../types'
import { createPlayable, type Playable } from '../utils/player'

const props = defineProps<{
  device: Device
  streamUrl?: string
}>()
const emit = defineEmits<{ (e: 'click'): void }>()

const videoEl = ref<HTMLVideoElement | null>(null)
let playable: Playable | null = null

function hash(s: string) {
  let h = 0
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0
  return h
}

// HLS 首帧未出时显示加载态，避免格子里一片死黑让人以为是空画面
const videoReady = ref(false)

function attach() {
  if (playable || !videoEl.value || !props.device.online) return
  videoReady.value = false
  playable = createPlayable({
    url: props.streamUrl,
    seed: hash(props.device.id),
    label: props.device.name,
  })
  playable.attach(videoEl.value)
}

onMounted(attach)

watch(
  () => [props.device.online, props.streamUrl] as const,
  () => {
    playable?.destroy()
    playable = null
    attach()
  },
)

onBeforeUnmount(() => {
  playable?.destroy()
  playable = null
})
</script>

<template>
  <div class="cell" :class="{ offline: !device.online }" @click="emit('click')">
    <video
      ref="videoEl"
      class="video"
      muted
      playsinline
      v-show="device.online"
      @loadeddata="videoReady = true"
      @playing="videoReady = true"
    />
    <div class="noise" v-if="device.online" />
    <div v-if="device.online && !videoReady" class="empty loading">
      <van-loading size="24" color="#2ea8ff" />
      <span>画面加载中…</span>
    </div>
    <div class="overlay">
      <span class="name">{{ device.name }}</span>
      <span class="badge live" v-if="device.online">LIVE</span>
      <span class="badge dead" v-else>离线</span>
    </div>
    <div class="empty" v-if="!device.online">
      <van-icon name="close" size="28" />
      <span>设备离线</span>
    </div>
  </div>
</template>

<style scoped>
.cell {
  position: relative;
  overflow: hidden;
  border-radius: 10px;
  background: #0a0c10;
  border: 1px solid var(--nvr-border);
  cursor: pointer;
  min-height: 0;
}
.cell:active {
  border-color: var(--nvr-accent);
}
.video {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
.noise {
  pointer-events: none;
  position: absolute;
  inset: 0;
  background-image: repeating-linear-gradient(
    0deg,
    rgba(255, 255, 255, 0.012) 0 1px,
    transparent 1px 3px
  );
}
.overlay {
  position: absolute;
  left: 6px;
  right: 6px;
  top: 6px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  pointer-events: none;
}
.name {
  font-size: calc(11px * var(--nvr-font-scale, 1));
  color: rgba(255, 255, 255, 0.92);
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.8);
  background: rgba(0, 0, 0, 0.35);
  padding: 2px 6px;
  border-radius: 4px;
}
.badge {
  font-size: calc(10px * var(--nvr-font-scale, 1));
  padding: 2px 6px;
  border-radius: 4px;
  color: #fff;
}
.badge.live {
  background: rgba(46, 204, 143, 0.9);
}
.badge.dead {
  background: rgba(255, 93, 93, 0.85);
}
.empty {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  color: var(--nvr-text-2);
  font-size: calc(12px * var(--nvr-font-scale, 1));
}
.empty.loading {
  color: rgba(255, 255, 255, 0.55);
  background: rgba(0, 0, 0, 0.25);
  pointer-events: none;
}
</style>
