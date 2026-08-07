<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { Device } from '../types'
import { createPlayable, type Playable } from '../utils/player'

const props = defineProps<{
  device: Device
  streamUrl?: string
}>()
const emit = defineEmits<{
  (e: 'enter', d: Device): void
  (e: 'play', d: Device): void
  (e: 'more', d: Device): void
}>()

const rootRef = ref<HTMLElement | null>(null)
const videoEl = ref<HTMLVideoElement | null>(null)
const visible = ref(false)
let playable: Playable | null = null
let observer: IntersectionObserver | null = null

function hash(s: string) {
  let h = 0
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0
  return h
}

function attach() {
  if (playable || !videoEl.value || !props.device.online || !visible.value) return
  playable = createPlayable({
    url: props.streamUrl,
    seed: hash(props.device.id),
    label: props.device.name,
  })
  playable.attach(videoEl.value)
}

function detach() {
  playable?.destroy()
  playable = null
  if (videoEl.value) videoEl.value.srcObject = null
}

onMounted(() => {
  observer = new IntersectionObserver(
    (entries) => {
      visible.value = entries[0]?.isIntersecting ?? false
      if (visible.value) attach()
      else detach()
    },
    { threshold: 0.25 },
  )
  if (rootRef.value) observer.observe(rootRef.value)
})

watch(
  () => [props.device.online, props.streamUrl] as const,
  (on) => {
    detach()
    if (on[0]) attach()
  },
)

onBeforeUnmount(() => {
  observer?.disconnect()
  detach()
})
</script>

<template>
  <div ref="rootRef" class="cam-card" :class="{ offline: !device.online }">
    <video ref="videoEl" class="bg" muted playsinline v-show="device.online" />
    <div class="shade top" />
    <div class="shade bottom" />

    <div class="hd">
      <span class="cam-icon"><van-icon name="video-o" size="16" /></span>
      <div class="meta">
        <span class="name">{{ device.name }}</span>
        <span class="model">{{ device.model || 'ONVIF Camera' }}</span>
      </div>
      <button class="enter" @click.stop="emit('enter', device)">
        进入
        <van-icon name="arrow" size="11" />
      </button>
    </div>

    <div v-if="device.online" class="center" @click.stop="emit('play', device)">
      <span class="play"><van-icon name="play" size="22" /></span>
    </div>
    <div v-else class="center offline-tip">
      <svg viewBox="0 0 24 24" width="30" height="30" fill="none" stroke="currentColor"
        stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
        <line x1="2" y1="2" x2="22" y2="22" />
        <path d="M8.5 16.5a5 5 0 0 1 7 0" />
        <path d="M2 8.82a15 15 0 0 1 4.17-2.65" />
        <path d="M10.66 5c4.01-.36 8.14.9 11.34 3.76" />
        <path d="M16.85 11.25a10 10 0 0 1 2.22 1.68" />
        <path d="M5 13a10 10 0 0 1 5.24-2.76" />
        <line x1="12" y1="20" x2="12.01" y2="20" />
      </svg>
      <span>摄像机已离线</span>
    </div>

    <button class="more" @click.stop="emit('more', device)">
      <van-icon name="ellipsis" size="18" />
    </button>
  </div>
</template>

<style scoped>
.cam-card {
  position: relative;
  border-radius: var(--nvr-radius);
  overflow: hidden;
  aspect-ratio: 16 / 10.5;
  background: linear-gradient(135deg, #1a1e26, #0c0e13);
  border: 1px solid var(--nvr-border);
  margin: 0 14px 14px;
}
.bg {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.shade {
  position: absolute;
  left: 0;
  right: 0;
  height: 56px;
  pointer-events: none;
}
.shade.top {
  top: 0;
  background: linear-gradient(180deg, rgba(0, 0, 0, 0.45), transparent);
}
.shade.bottom {
  bottom: 0;
  background: linear-gradient(0deg, rgba(0, 0, 0, 0.35), transparent);
}
.hd {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 12px;
}
.cam-icon {
  width: 34px;
  height: 34px;
  border-radius: 10px;
  background: rgba(255, 255, 255, 0.92);
  color: #1a1e26;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.meta {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
.name {
  font-size: 15px;
  font-weight: 600;
  color: #fff;
  text-shadow: 0 1px 3px rgba(0, 0, 0, 0.6);
}
.model {
  font-size: 11px;
  color: rgba(255, 255, 255, 0.75);
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.6);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.enter {
  display: flex;
  align-items: center;
  gap: 2px;
  padding: 5px 12px;
  border-radius: 999px;
  border: none;
  background: rgba(255, 255, 255, 0.2);
  color: #fff;
  font-size: 12px;
  backdrop-filter: blur(6px);
  cursor: pointer;
}
.enter:active {
  background: rgba(255, 255, 255, 0.32);
}
.center {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}
.play {
  width: 52px;
  height: 52px;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.28);
  backdrop-filter: blur(4px);
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  cursor: pointer;
  transition: transform 0.15s;
}
.play:active {
  transform: scale(0.9);
}
.offline-tip {
  flex-direction: column;
  gap: 8px;
  color: rgba(255, 255, 255, 0.6);
  font-size: 13px;
}
.more {
  position: absolute;
  right: 12px;
  bottom: 12px;
  width: 34px;
  height: 34px;
  border-radius: 50%;
  border: none;
  background: rgba(255, 255, 255, 0.18);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  backdrop-filter: blur(4px);
}
.more:active {
  background: rgba(255, 255, 255, 0.3);
}

@media (hover: hover) {
  .cam-card:hover {
    border-color: rgba(255, 255, 255, 0.18);
  }
  .play:hover {
    background: rgba(255, 255, 255, 0.4);
  }
  .enter:hover {
    background: rgba(255, 255, 255, 0.32);
  }
  .more:hover {
    background: rgba(255, 255, 255, 0.3);
  }
}
</style>
