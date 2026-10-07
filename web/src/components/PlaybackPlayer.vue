<script setup lang="ts">
import { ref } from 'vue'
import { showToast } from 'vant'

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
  (e: 'seek-rel', deltaMs: number): void
  (e: 'jump', timeText: string): void
}>()

// 跳转时间：自绘 HH:MM:SS 输入（自动补冒号），从时间轴下方的独立工具行
// 收编进播放器控制条（时钟图标弹出），与倍速区同处一片操作区。
const jumpOpen = ref(false)
const jumpText = ref('')
const jumpInvalid = ref(false)

function onJumpInput() {
  const digits = jumpText.value.replace(/\D/g, '').slice(0, 6)
  let out = digits
  if (digits.length > 4) out = `${digits.slice(0, 2)}:${digits.slice(2, 4)}:${digits.slice(4)}`
  else if (digits.length > 2) out = `${digits.slice(0, 2)}:${digits.slice(2)}`
  jumpText.value = out
  jumpInvalid.value = false
}

function submitJump() {
  if (!props.hasStream) return
  emit('jump', jumpText.value.trim())
}

function toggleJump() {
  jumpOpen.value = !jumpOpen.value
}

const videoEl = ref<HTMLVideoElement | null>(null)
const wrapRef = ref<HTMLElement | null>(null)

function getVideoEl() {
  return videoEl.value
}
function getWrap() {
  return wrapRef.value
}
defineExpose({ getVideoEl, getWrap })

// 截图当前回放帧：video 为同源 HLS，可直接 drawImage 到 canvas 后导出 jpg。
// 命名含设备与时间戳，便于区分多台设备/多次截图。
function snapshot() {
  const v = videoEl.value
  if (!v || !v.videoWidth) {
    showToast('无法截图：画面尚未就绪')
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
    a.download = `playback_${props.deviceName || 'camera'}_${ts}.jpg`
    a.click()
    URL.revokeObjectURL(url)
    showToast('截图已保存')
  }, 'image/jpeg', 0.95)
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


    <!-- 时间跳转弹层：从控制条时钟图标呼出，输入即跳（自动补冒号） -->
    <div v-if="hasStream && jumpOpen" class="jump-pop">
      <input
        v-model="jumpText"
        type="text"
        inputmode="numeric"
        placeholder="HH:MM:SS"
        maxlength="8"
        autocomplete="off"
        aria-label="跳转到指定时间"
        :class="{ invalid: jumpInvalid }"
        @input="onJumpInput"
        @keyup.enter="submitJump"
      />
      <button type="button" class="jump-go" :disabled="!hasStream" @click="submitJump">跳转</button>
    </div>

    <div v-if="hasStream" class="controls">
      <button type="button" class="btn" :aria-label="playing ? '暂停回放' : '播放回放'" @click="emit('toggle')">
        <van-icon :name="playing ? 'pause-circle-o' : 'play-circle-o'" size="26" />
      </button>
      <!-- 截图按钮：相机图标，位于后退30秒之前，截取当前回放帧 -->
      <button type="button" class="btn" :disabled="!hasStream" aria-label="截图" @click="snapshot">
        <van-icon name="photograph" size="20" />
      </button>
      <button type="button" class="btn skip" :disabled="!hasStream" aria-label="后退30秒" @click="emit('seek-rel', -30000)">
        <van-icon name="arrow-double-left" size="18" />
        <span class="skip-num">30秒</span>
      </button>
      <!-- 桌面：四档并列；移动端：合并为单按钮循环切换（1x→2x→4x→8x→1x） -->
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
      <button
        class="sp sp-single"
        type="button"
        :aria-label="'倍速 ' + speed + 'x，点击切换'"
        @click="emit('speed', speeds[(speeds.indexOf(speed) + 1) % speeds.length])"
      >
        {{ speed }}x
      </button>
      <button type="button" class="btn skip" :disabled="!hasStream" aria-label="前进30秒" @click="emit('seek-rel', 30000)">
        <span class="skip-num">30秒</span>
        <van-icon name="arrow-double-right" size="18" />
      </button>
      <button type="button" class="btn" :aria-label="jumpOpen ? '收起时间跳转' : '按时间跳转'" :aria-expanded="jumpOpen" @click="toggleJump">
        <van-icon name="clock-o" size="22" />
      </button>
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
.controls {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 6px;
  /* 禁止换行：换行会让倍速键竖摞、盖满画面（移动端实测）。宽度不足时
     整条横向滚动兜底（关怀模式大字号下按钮总宽可能超屏）。 */
  flex-wrap: nowrap;
  overflow-x: auto;
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
  flex-wrap: nowrap;
  min-width: 0;
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
/* 移动端单按钮形态：多档并列在窄屏放不下（曾爆版竖摞），合并为循环切换 */
.sp-single {
  display: none;
  min-width: 44px;
  min-height: 40px;
  padding: 2px 10px;
  background: transparent;
  border-radius: 10px;
  border: 1px solid rgba(255, 255, 255, 0.35);
  color: rgba(255, 255, 255, 0.9);
  font-size: calc(13px * var(--nvr-font-scale, 1));
  font-weight: 600;
  cursor: pointer;
}
@media (max-width: 1199px) {
  .speeds {
    display: none;
  }
  /* 倍速按钮不再拉伸：原先 flex:1 会占满剩余空间，长条边框视觉上像整块背景；
     改为内容宽度（保底 52px 触控区），剩余空隙由 space-between 均分。 */
  .sp-single {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: 0 0 auto;
    min-width: 52px;
    padding: 2px 12px;
  }
  .controls {
    justify-content: space-between;
  }
}
/* ±30 秒图标按钮：双箭头 + 数字，替代原时间轴下方的文字按钮 */
.btn.skip {
  gap: 1px;
  padding: 0 6px;
  min-width: 52px;
}
.btn.skip:disabled {
  opacity: 0.4;
}
.skip-num {
  font-size: calc(12px * var(--nvr-font-scale, 1));
  font-weight: 600;
  color: rgba(255, 255, 255, 0.85);
  /* 大字号窄屏下「30秒」会被拆成两行（实测 320px 关怀模式），禁止换行 */
  white-space: nowrap;
}
/* 时间跳转弹层：悬于控制条上方 */
.jump-pop {
  position: absolute;
  left: 8px;
  right: 8px;
  bottom: 56px;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px;
  border-radius: 10px;
  background: rgba(0, 0, 0, 0.82);
  border: 1px solid rgba(255, 255, 255, 0.18);
  z-index: 2;
}
.jump-pop input {
  flex: 1;
  min-width: 0;
  min-height: 40px;
  font: inherit;
  font-variant-numeric: tabular-nums;
  text-align: center;
  color: #fff;
  background: rgba(255, 255, 255, 0.12);
  border: 1px solid rgba(255, 255, 255, 0.25);
  border-radius: 8px;
  padding: 4px 8px;
}
.jump-pop input::placeholder {
  color: rgba(255, 255, 255, 0.5);
}
.jump-pop input.invalid {
  border-color: var(--nvr-red);
  box-shadow: 0 0 0 1px var(--nvr-red);
}
.jump-go {
  flex-shrink: 0;
  min-height: 40px;
  padding: 0 16px;
  border: 0;
  border-radius: 8px;
  background: var(--nvr-accent);
  color: #fff;
  font-size: calc(13px * var(--nvr-font-scale, 1));
  cursor: pointer;
}
.jump-go:disabled {
  opacity: 0.4;
}
/* 移动端（<1200px）控制条紧凑化：按钮与间距缩小，保证单行放下。
   关怀模式字号放大后总宽可能仍超屏，由 .controls 的横向滚动兜底。 */
@media (max-width: 1199px) {
  .controls {
    gap: 2px;
    padding: 4px 6px;
  }
  .btn {
    min-width: 36px;
    min-height: 40px;
  }
  .btn.skip {
    min-width: 42px;
    padding: 0 4px;
  }
  .sp {
    min-width: 36px;
    min-height: 36px;
    padding: 2px 4px;
  }
  .skip-num {
    font-size: calc(11px * var(--nvr-font-scale, 1));
  }
}

/* 关怀模式：控制条文字放大（适老场景 11–13px 过小） */
:global(body.care .skip-num) {
  font-size: calc(14px * var(--nvr-font-scale, 1));
}
:global(body.care .sp-single) {
  font-size: calc(15px * var(--nvr-font-scale, 1));
  min-height: 44px;
}
</style>
