<script setup lang="ts">
import { ref } from 'vue'

const props = defineProps<{
  displayTime: number
  playing: boolean
  speed: number
  hasStream: boolean
  deviceName: string
}>()
const emit = defineEmits<{
  (e: 'toggle'): void
  (e: 'speed', n: number): void
  (e: 'fullscreen'): void
  (e: 'ended'): void
  (e: 'error'): void
}>()

const videoEl = ref<HTMLVideoElement | null>(null)
const wrapRef = ref<HTMLElement | null>(null)

function getVideoEl() {
  return videoEl.value
}
function getWrap() {
  return wrapRef.value
}
defineExpose({ getVideoEl, getWrap })

function fmt(ts: number) {
  const d = new Date(ts)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

const speeds = [1, 2, 4, 8]
</script>

<template>
  <div ref="wrapRef" class="player">
    <video
      ref="videoEl"
      class="video"
      muted
      playsinline
      v-show="hasStream"
      @click="emit('toggle')"
      @ended="emit('ended')"
      @error="emit('error')"
    />
    <div v-if="!hasStream" class="empty">
      <van-icon name="warning-o" size="40" />
      <span>当日无录像</span>
    </div>

    <div v-if="hasStream" class="topbar">
      <span class="rec" :class="{ on: playing }">
        <van-icon :name="playing ? 'pause-circle-o' : 'play-circle-o'" size="16" />
        {{ playing ? '播放中' : '已暂停' }}
      </span>
      <span class="time mono">{{ fmt(displayTime) }}</span>
    </div>

    <div v-if="hasStream" class="controls">
      <button type="button" class="btn" :aria-label="playing ? '暂停回放' : '播放回放'" @click="emit('toggle')">
        <van-icon :name="playing ? 'pause-circle-o' : 'play-circle-o'" size="26" />
      </button>
      <div class="speeds">
        <button
          v-for="s in speeds"
          :key="s"
          class="sp"
          type="button"
          :aria-label="s + '倍速'"
          :aria-pressed="speed === s"
          :class="{ on: speed === s }"
          @click="emit('speed', s)"
        >
          {{ s }}x
        </button>
      </div>
      <button type="button" class="btn" aria-label="切换全屏" @click="emit('fullscreen')">
        <van-icon name="expand-o" size="22" />
      </button>
    </div>
  </div>
</template>

<style scoped>
.player {
  position: relative;
  aspect-ratio: 16 / 9;
  background: #000;
  border-radius: 10px;
  overflow: hidden;
}
.video {
  width: 100%;
  height: 100%;
  object-fit: contain;
  display: block;
}
.empty {
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
.topbar {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 10px;
  background: linear-gradient(180deg, rgba(0, 0, 0, 0.55), transparent);
}
.rec {
  font-size: calc(11px * var(--nvr-font-scale, 1));
  color: rgba(255, 255, 255, 0.55);
  display: flex;
  align-items: center;
  gap: 5px;
}
.rec i {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.4);
}
.rec {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--nvr-amber);
}
.rec.on {
  color: var(--nvr-accent);
}
.time {
  font-size: calc(12px * var(--nvr-font-scale, 1));
  color: rgba(255, 255, 255, 0.92);
  text-shadow: 0 1px 3px rgba(0, 0, 0, 0.8);
}
.controls {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 6px;
  flex-wrap: wrap;
  background: linear-gradient(0deg, rgba(0, 0, 0, 0.6), transparent);
}
.btn {
  border: 0;
  background: transparent;
  min-width: 44px;
  min-height: 44px;
  justify-content: center;
  color: #fff;
  display: flex;
  align-items: center;
  cursor: pointer;
}
.btn:active {
  transform: scale(0.9);
}
.speeds {
  flex: 1;
  display: flex;
  justify-content: center;
  gap: 4px;
  flex-wrap: wrap;
}
.sp {
  min-width: 44px;
  min-height: 44px;
  padding: 3px 6px;
  background: transparent;
  border-radius: 12px;
  border: 1px solid rgba(255, 255, 255, 0.35);
  color: rgba(255, 255, 255, 0.85);
  font-size: calc(12px * var(--nvr-font-scale, 1));
  cursor: pointer;
}
.sp.on {
  background: var(--nvr-accent);
  border-color: var(--nvr-accent);
  color: #fff;
}
</style>
