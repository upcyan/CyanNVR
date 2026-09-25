<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { showToast } from 'vant'
import type { Device } from '../types'
import { createPlayable, type Playable } from '../utils/player'

const props = defineProps<{
  show: boolean
  device: Device | null
  streamUrl?: string
}>()
const emit = defineEmits<{
  (e: 'update:show', v: boolean): void
  (e: 'playback', d: Device): void
}>()

const videoEl = ref<HTMLVideoElement | null>(null)
let playable: Playable | null = null
// 进入大画面后 HLS 出帧前给明确加载态，而不是全黑一片
const videoReady = ref(false)

function hash(s: string) {
  let h = 0
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0
  return h
}

async function openStream() {
  playable?.destroy()
  playable = null
  videoReady.value = false
  if (!props.device?.online) return
  await nextTick()
  if (videoEl.value && props.device) {
    playable = createPlayable({
      url: props.streamUrl,
      seed: hash(props.device.id),
      label: props.device.name,
    })
    playable.attach(videoEl.value)
  }
}

function closeStream() {
  playable?.destroy()
  playable = null
  videoReady.value = false
}

watch(
  () => props.show,
  (v) => {
    if (v) openStream()
    else closeStream()
  },
)

function close() {
  emit('update:show', false)
}

function snapshot() {
  const v = videoEl.value
  if (!v || !v.videoWidth) {
    showToast('无法截图')
    return
  }
  const canvas = document.createElement('canvas')
  canvas.width = v.videoWidth
  canvas.height = v.videoHeight
  const ctx = canvas.getContext('2d')
  if (!ctx) return
  ctx.drawImage(v, 0, 0)
  canvas.toBlob((blob) => {
    if (!blob) return
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    const now = new Date()
    const p2 = (n: number) => String(n).padStart(2, '0')
    const ts = `${now.getFullYear()}${p2(now.getMonth() + 1)}${p2(now.getDate())}_${p2(now.getHours())}${p2(now.getMinutes())}${p2(now.getSeconds())}`
    a.href = url
    a.download = `screenshot_${props.device?.name || 'camera'}_${ts}.jpg`
    a.click()
    URL.revokeObjectURL(url)
    showToast('截图已保存')
  }, 'image/jpeg', 0.95)
}

function goPlayback() {
  if (props.device) {
    emit('playback', props.device)
  }
}
const isFullscreen = ref(false)
function toggleFullscreen() {
  const root = document.documentElement
  if (!document.fullscreenElement) {
    root.requestFullscreen?.().catch(() => {})
  } else {
    document.exitFullscreen?.().catch(() => {})
  }
}
if (typeof document !== 'undefined') {
  document.addEventListener('fullscreenchange', () => {
    isFullscreen.value = !!document.fullscreenElement
  })
}
</script>

<template>
  <van-popup
    :show="show"
    position="right"
    :style="{ width: '100%', height: '100%', background: '#000' }"
    @update:show="emit('update:show', $event)"
  >
    <div class="viewer">
      <div class="vhd">
        <button class="control-button" aria-label="关闭实时预览" @click="close"><van-icon name="arrow-left" size="20" /></button>
        <span class="vname">{{ device?.name ?? '' }}</span>
        <span class="vstate" :class="{ on: device?.online }">
          {{ device?.online ? '在线' : '离线' }}
        </span>
      </div>
      <div class="vbody">
        <video
          ref="videoEl"
          muted
          playsinline
          v-show="device?.online"
          @loadeddata="videoReady = true"
          @playing="videoReady = true"
        />
        <div v-if="device?.online && !videoReady" class="voffline">
          <van-loading size="30" color="#2ea8ff" />
          <span>画面加载中…</span>
        </div>
        <div v-if="!device?.online" class="voffline">
          <van-icon name="warning-o" size="36" />
          <span>摄像机已离线</span>
        </div>
      </div>
      <div class="vfoot">
        <button class="vbtn" @click="snapshot">
          <van-icon name="photograph" size="20" />
          <span>截图</span>
        </button>
        <button class="vbtn" @click="goPlayback">
          <van-icon name="clock-o" size="20" />
          <span>回放</span>
        </button>
        <button class="vbtn" @click="toggleFullscreen">
          <van-icon :name="isFullscreen ? 'shrink-o' : 'expand-o'" size="20" />
          <span>{{ isFullscreen ? '退出全屏' : '全屏' }}</span>
        </button>
      </div>
    </div>
  </van-popup>
</template>

<style scoped>
.viewer {
  height: 100%;
  display: flex;
  flex-direction: column;
  background: #000;
}
.vhd {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 16px;
  color: #fff;
}
.vname {
  font-size: calc(16px * var(--nvr-font-scale, 1));
  font-weight: 600;
}
.vstate {
  font-size: calc(12px * var(--nvr-font-scale, 1));
  color: var(--nvr-red);
}
.vstate.on {
  color: var(--nvr-green);
}
.vbody {
  flex: 1;
  position: relative;
  min-height: 0;
}
.vbody video {
  width: 100%;
  height: 100%;
  object-fit: contain;
  display: block;
}
.voffline {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: var(--nvr-text-2);
  font-size: calc(13px * var(--nvr-font-scale, 1));
}
.vfoot {
  display: flex;
  justify-content: space-around;
  padding: 16px 0 calc(16px + env(safe-area-inset-bottom));
}
.vbtn {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  background: none;
  border: none;
  color: #fff;
  font-size: calc(12px * var(--nvr-font-scale, 1));
  cursor: pointer;
}
.vbtn:active {
  color: var(--nvr-accent);
}
</style>
