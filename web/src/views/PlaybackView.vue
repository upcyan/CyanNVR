<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { showToast } from 'vant'
import type { DayRecord, Device, RecordingSegment } from '../types'
import { useDeviceStore } from '../stores/devices'
import { createPlayback, fetchDaySegments, fetchMonthRecords, isBackend, isDemoMode } from '../api'
import { hashStr } from '../mocks/generator'
import { createPlayable, type Playable } from '../utils/player'
import CalendarHeat from '../components/CalendarHeat.vue'
import TimelineBar from '../components/TimelineBar.vue'
import PlaybackPlayer from '../components/PlaybackPlayer.vue'

const store = useDeviceStore()
const route = useRoute()

const today = new Date()
const p2 = (n: number) => String(n).padStart(2, '0')
const todayStr = `${today.getFullYear()}-${p2(today.getMonth() + 1)}-${p2(today.getDate())}`

const deviceId = ref('')
const dateStr = ref(todayStr)
const monthDays = ref<DayRecord[]>([])
const segments = ref<RecordingSegment[]>([])
const currentTs = ref(0)
const playing = ref(true)
const speed = ref(1)
const showDevicePicker = ref(false)
const loading = ref(false)

const playerRef = ref<InstanceType<typeof PlaybackPlayer> | null>(null)
let playable: Playable | null = null
let timer: number | null = null
let seekTimer: number | null = null
let sessionToken = 0

function fmtRange(a: number, b: number) {
  const f = (ts: number) => {
    const d = new Date(ts)
    return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  }
  return `${f(a)}-${f(b)}`
}

const device = computed<Device | undefined>(() => store.byId(deviceId.value))
const dayStart = computed(() => new Date(`${dateStr.value}T00:00:00`).getTime())
const ym = computed(() => dateStr.value.slice(0, 7))
const totalMinutes = computed(() => Math.round(segments.value.reduce((a, s) => a + (s.end - s.start), 0) / 60000))

async function loadMonth() {
  monthDays.value = await fetchMonthRecords(deviceId.value, ym.value)
}

async function loadSegments() {
  loading.value = true
  segments.value = await fetchDaySegments(deviceId.value, dateStr.value)
  loading.value = false
  rebuildPlayable()
}

function rebuildPlayable() {
  playable?.destroy()
  playable = null
  const el = playerRef.value?.getVideoEl()
  if (!el || !device.value || !segments.value.length) return
  const start = segments.value[0].start
  currentTs.value = start
  playing.value = true
  if (isBackend() && !isDemoMode()) {
    buildSession(start)
  } else {
    playable = createPlayable({ seed: hashStr(deviceId.value), label: device.value.name, baseTs: start })
    playable.attach(el)
    playable.setSpeed(speed.value)
  }
}

async function buildSession(fromTs: number) {
  const token = ++sessionToken
  const winStart = Math.max(dayStart.value, fromTs - 120000)
  const winEnd = Math.min(dayStart.value + 86400000, fromTs + 600000)
  if (winEnd <= winStart) return
  try {
    const url = await createPlayback(deviceId.value, winStart, winEnd)
    if (token !== sessionToken) return
    const el = playerRef.value?.getVideoEl()
    if (!el) return
    playable?.destroy()
    playable = createPlayable({ url, baseTs: winStart })
    playable.attach(el)
    playable.setSpeed(speed.value)
  } catch {
    /* no recordings in window */
  }
}

function startTimer() {
  if (timer) return
  timer = window.setInterval(() => {
    if (playing.value && playable) currentTs.value = playable.time
  }, 500)
}

function onSeek(ts: number) {
  currentTs.value = ts
  if (!isBackend() || isDemoMode()) {
    playable?.seek(ts)
    return
  }
  if (seekTimer) window.clearTimeout(seekTimer)
  seekTimer = window.setTimeout(() => {
    if (segments.value.length) buildSession(ts)
  }, 500)
}

function onToggle() {
  if (!playable) return
  if (playing.value) playable.pause()
  else playable.resume()
  playing.value = !playing.value
}

function onSpeed(n: number) {
  speed.value = n
  playable?.setSpeed(n)
}

function onSelectSegment(s: RecordingSegment) {
  onSeek(s.start)
}

function onSelectDate(date: string) {
  dateStr.value = date
}

function shiftDay(delta: number) {
  const d = new Date(`${dateStr.value}T12:00:00`)
  d.setDate(d.getDate() + delta)
  dateStr.value = `${d.getFullYear()}-${p2(d.getMonth() + 1)}-${p2(d.getDate())}`
}

function onFullscreen() {
  const wrap = playerRef.value?.getWrap()
  if (!wrap) return
  if (document.fullscreenElement) document.exitFullscreen()
  else wrap.requestFullscreen?.().catch(() => {})
}

function selectDevice(d: Device) {
  deviceId.value = d.id
  showDevicePicker.value = false
}

function pickDevice() {
  if (!store.devices.length) {
    showToast('请先在实时预览页添加设备')
    return
  }
  showDevicePicker.value = true
}

onMounted(() => {
  const q = route.query.device as string | undefined
  if (q && store.byId(q)) {
    deviceId.value = q
  } else if (store.devices.length) {
    deviceId.value = store.devices[0].id
  }
  loadMonth()
  loadSegments()
  startTimer()
})

watch(deviceId, () => {
  loadMonth()
  loadSegments()
})
watch(dateStr, () => {
  if (ym.value !== dateStr.value.slice(0, 7)) loadMonth()
  loadSegments()
})
watch(playerRef, () => {
  if (playerRef.value) rebuildPlayable()
})

onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer)
  if (seekTimer) window.clearTimeout(seekTimer)
  playable?.destroy()
  playable = null
})
</script>

<template>
  <div class="page playback-page">
    <van-nav-bar title="录像管理">
      <template #right>
        <span class="device-picker" @click="pickDevice">
          {{ device?.name ?? '选择设备' }}
          <van-icon name="arrow-down" size="12" />
        </span>
      </template>
    </van-nav-bar>

    <div class="pb-body">
      <div class="pb-left">
        <div class="date-bar">
          <van-icon name="arrow-left" size="20" @click="shiftDay(-1)" />
          <span class="date mono">{{ dateStr }}</span>
          <van-icon name="arrow" size="20" @click="shiftDay(1)" />
        </div>
        <CalendarHeat :days="monthDays" :selected="dateStr" @select="onSelectDate" />
      </div>

      <div class="pb-right">
        <div class="player-wrap">
          <van-loading v-if="loading" class="center" />
          <PlaybackPlayer
            v-else
            ref="playerRef"
            :display-time="currentTs"
            :playing="playing"
            :speed="speed"
            :has-stream="segments.length > 0"
            :device-name="device?.name ?? ''"
            @toggle="onToggle"
            @speed="onSpeed"
            @fullscreen="onFullscreen"
          />
        </div>

        <div class="timeline-wrap">
          <div class="tl-head">
            <span class="mono">{{ dateStr }} 录像</span>
            <span>{{ segments.length }} 段 · 共 {{ totalMinutes }} 分钟</span>
          </div>
          <TimelineBar :day-start="dayStart" :segments="segments" :value="currentTs" @seek="onSeek" />
        </div>

        <div class="seg-list">
          <span
            v-for="s in segments"
            :key="s.id"
            class="chip mono"
            :class="{ on: currentTs >= s.start && currentTs <= s.end }"
            @click="onSelectSegment(s)"
          >
            {{ fmtRange(s.start, s.end) }}
          </span>
          <span v-if="!segments.length" class="none">当日无录制</span>
        </div>
      </div>
    </div>

    <van-popup v-model:show="showDevicePicker" position="bottom" round>
      <div class="picker-head">选择设备</div>
      <van-cell-group inset>
        <van-cell
          v-for="d in store.devices"
          :key="d.id"
          :title="d.name"
          :label="d.ip"
          clickable
          @click="selectDevice(d)"
        >
          <template #right-icon>
            <van-icon v-if="d.id === deviceId" name="success" color="#2ecc8f" />
          </template>
        </van-cell>
      </van-cell-group>
    </van-popup>
  </div>
</template>

<style scoped>
.device-picker {
  font-size: 13px;
  display: flex;
  align-items: center;
  gap: 4px;
}
.date-bar {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 22px;
  padding: 8px 0;
  font-size: 15px;
  font-weight: 600;
}
.player-wrap {
  position: relative;
  padding: 0 12px;
  min-height: 120px;
}
.center {
  display: flex;
  justify-content: center;
  padding: 40px 0;
}
.timeline-wrap {
  margin-top: 8px;
}
.tl-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 14px 4px;
  font-size: 12px;
  color: var(--nvr-text-2);
}
.tl-head span:first-child {
  color: var(--nvr-text);
  font-weight: 600;
}
.seg-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding: 4px 14px 14px;
}
.chip {
  padding: 4px 10px;
  border-radius: 6px;
  background: var(--nvr-panel-2);
  border: 1px solid var(--nvr-border);
  font-size: 12px;
  cursor: pointer;
}
.chip.on {
  border-color: var(--nvr-accent);
  color: var(--nvr-accent);
}
.none {
  font-size: 12px;
  color: var(--nvr-text-2);
}
.picker-head {
  padding: 14px;
  font-weight: 600;
  text-align: center;
}

.pb-body {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

@media (min-width: 900px) {
  .pb-body {
    flex-direction: row;
    gap: 20px;
    padding: 0 24px;
  }
  .pb-left {
    width: 360px;
    flex-shrink: 0;
    overflow-y: auto;
    padding-bottom: 20px;
  }
  .pb-right {
    flex: 1;
    min-width: 0;
    overflow-y: auto;
    padding-bottom: 20px;
  }
  .player-wrap {
    padding: 0;
  }
  .timeline-wrap .tl-head {
    padding: 0 2px 4px;
  }
  .timeline-wrap .timeline {
    padding-left: 0;
    padding-right: 0;
  }
  .seg-list {
    padding-left: 2px;
    padding-right: 2px;
  }
}

@media (hover: hover) {
  .chip:hover {
    border-color: var(--nvr-accent);
    color: var(--nvr-accent);
  }
  .date-bar .van-icon:hover {
    color: var(--nvr-accent);
    cursor: pointer;
  }
}
</style>
