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

function hash(s: string) {
  let h = 0
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0
  return h
}

async function openStream() {
  playable?.destroy()
  playable = null
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
  showToast('截图已保存')
}

function goPlayback() {
  if (props.device) {
    emit('playback', props.device)
  }
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
        <van-icon name="arrow-left" size="20" @click="close" />
        <span class="vname">{{ device?.name ?? '' }}</span>
        <span class="vstate" :class="{ on: device?.online }">
          {{ device?.online ? '在线' : '离线' }}
        </span>
      </div>
      <div class="vbody">
        <video ref="videoEl" muted playsinline v-show="device?.online" />
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
  font-size: 16px;
  font-weight: 600;
}
.vstate {
  font-size: 12px;
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
  font-size: 13px;
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
  font-size: 12px;
  cursor: pointer;
}
.vbtn:active {
  color: var(--nvr-accent);
}
</style>
