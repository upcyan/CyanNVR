<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { showToast } from 'vant'
import type { DayRecord, Device, EventItem, RecordingSegment } from '../types'
import { useDeviceStore } from '../stores/devices'
import { apiBase, createPlayback, downloadRecordingURL, fetchDaySegments, fetchEvents, fetchMonthRecords, isBackend, isDemoMode, stopPlayback } from '../api'
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
const dayEvents = ref<EventItem[]>([])
const currentTs = ref(0)
const playing = ref(true)
const speed = ref(1)
const showDevicePicker = ref(false)
const loading = ref(false)
// 已请求跳转、但新会话/新位置尚未生效。此期间抑制定时器回写游标。
const seekPending = ref(false)
// 当前回放会话名：离开页面/换会话时通知服务端停掉转码进程
let currentSession = ''
// 回放准备失败的原因（如「无录像」「转码超时」），显示给用户
const playError = ref('')

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
  if (!deviceId.value) {
    monthDays.value = []
    return
  }
  try {
    monthDays.value = await fetchMonthRecords(deviceId.value, ym.value)
  } catch {
    monthDays.value = []
  }
}

async function loadSegments() {
  if (!deviceId.value) {
    segments.value = []
    dayEvents.value = []
    return
  }
  loadEvents() // 与分段并行加载，供时间轴着色与事件列表使用
  loading.value = true
  try {
    segments.value = await fetchDaySegments(deviceId.value, dateStr.value)
  } catch {
    segments.value = []
  } finally {
    loading.value = false
  }
  rebuildPlayable()
}

async function loadEvents() {
  if (!deviceId.value) {
    dayEvents.value = []
    return
  }
  try {
    dayEvents.value = (await fetchEvents(deviceId.value, dateStr.value, '', 0, 500)).events
  } catch {
    dayEvents.value = []
  }
}

// 后端返回 RFC3339 字符串、演示模式返回毫秒数，new Date() 两者通吃。
function evTime(e: EventItem): number {
  return new Date(e.time).getTime()
}

const EV_TYPE_LABEL: Record<string, string> = {
  motion: '移动侦测',
  ai: 'AI 识别',
  offline: '设备离线',
  online: '设备上线',
  manual: '手动',
}

function fmtHM(ts: number) {
  const d = new Date(ts)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

// 传给时间轴的事件（统一转毫秒时间戳）
const timelineEvents = computed(() =>
  dayEvents.value.map((e) => ({ time: evTime(e), type: e.type })),
)

// ---- 分段三级折叠：时段(上午/下午) → 小时 → 分段 ----
// 一天最多 24 小时、上百个分段；平铺渲染会很长且难定位。
// 按「小时」折叠后，默认只展开当前播放所在的那一小时。
interface SegGroup {
  key: string
  hourLabel: string
  start: number
  end: number
  segs: RecordingSegment[]
  durationMin: number
}

interface HalfGroup {
  key: string
  title: string
  hours: SegGroup[]
  segCount: number
  durationMin: number
}

const groupMode = ref(true) // 默认折叠；用户可关掉看平铺
const openHours = ref<string[]>([])

const halfGroups = computed<HalfGroup[]>(() => {
  const byHalf = new Map<string, Map<number, SegGroup>>()
  for (const seg of segments.value) {
    const d = new Date(seg.start)
    const halfKey = d.getHours() < 12 ? 'am' : 'pm'
    const h = d.getHours()
    let hours = byHalf.get(halfKey)
    if (!hours) {
      hours = new Map()
      byHalf.set(halfKey, hours)
    }
    let g = hours.get(h)
    if (!g) {
      g = {
        key: `${halfKey}-${h}`,
        hourLabel: `${String(h).padStart(2, '0')}:00-${String(h).padStart(2, '0')}:59`,
        start: seg.start,
        end: seg.end,
        segs: [],
        durationMin: 0,
      }
      hours.set(h, g)
    }
    g.segs.push(seg)
    g.start = Math.min(g.start, seg.start)
    g.end = Math.max(g.end, seg.end)
    g.durationMin += Math.round((seg.end - seg.start) / 60000)
  }
  const order = ['am', 'pm']
  const titles: Record<string, string> = { am: '上午', pm: '下午' }
  const out: HalfGroup[] = []
  for (const hk of order) {
    const hours = byHalf.get(hk)
    if (!hours) continue
    const list = [...hours.values()].sort((a, b) => a.start - b.start)
    out.push({
      key: hk,
      title: titles[hk],
      hours: list,
      segCount: list.reduce((a, g) => a + g.segs.length, 0),
      durationMin: list.reduce((a, g) => a + g.durationMin, 0),
    })
  }
  return out
})

const fmtDur = (min: number) => {
  if (min < 60) return `${min} 分钟`
  const h = Math.floor(min / 60)
  const m = min % 60
  return m ? `${h} 小时 ${m} 分` : `${h} 小时`
}

// 播放位置所在的小时 key，用于自动展开
const currentHourKey = computed(() => {
  if (!currentTs.value) return ''
  const h = new Date(currentTs.value).getHours()
  return `${h < 12 ? 'am' : 'pm'}-${h}`
})

// 切换分段/日期后，自动展开当前小时，让用户立刻看到正在播放的那一段
watch([segments, currentHourKey], () => {
  const k = currentHourKey.value
  if (k && !openHours.value.includes(k)) openHours.value = [k]
}, { immediate: true })

/** 在折叠模式下点击小时头：展开/收起 */
function toggleHour(key: string) {
  openHours.value = openHours.value.includes(key)
    ? openHours.value.filter((k) => k !== key)
    : [...openHours.value, key]
}

// 点击事件跳到对应时刻播放，与时间轴拖动走同一套会话重建逻辑
function onSelectEvent(e: EventItem) {
  const ts = Math.min(dayStart.value + 86400000 - 1000, Math.max(dayStart.value, evTime(e)))
  currentTs.value = ts
  onSeekEnd(ts)
}

// 事件所在录像段（供列表下载按钮使用）
function segOfEvent(e: EventItem): RecordingSegment | undefined {
  const ts = evTime(e)
  return segments.value.find((s) => ts >= s.start && ts <= s.end)
}

// 初次建立回放会话时，选到「当日最早的录像段」而不是第一段的起点：
// 第一段可能在凌晨（00:46），用户一进页面就看到半夜画面且时间轴游标
// 在最左边，直觉上会以为「今天没录上」。默认从有代表性的最早时段播放。
async function rebuildPlayable() {
  playable?.destroy()
  playable = null
  // 等待 DOM 更新完成再取 video 元素，避免 ref 尚未就绪导致画面空白
  await nextTick()
  const el = playerRef.value?.getVideoEl()
  if (!el || !device.value || !segments.value.length) return
  const start = earliestInterestingStart()
  currentTs.value = start
  playing.value = true
  if (isBackend() && !isDemoMode()) {
    // 首屏也要显示「正在准备回放」：H.265 源需服务端转码，
    // 实测首个分片要约 18 秒，没有提示用户会以为页面坏了。
    seekPending.value = true
    buildSession(start)
  } else {
    seekPending.value = false
    playable = createPlayable({ seed: hashStr(deviceId.value), label: device.value.name, baseTs: start })
    playable.attach(el)
    playable.setSpeed(speed.value)
  }
}

// 在已排序的录像段里挑一个更「可看」的起点：
// 优先取 06:00 之后的第一段（避免默认停在深夜），否则退回第一段。
//
// 关键：必须落在**真实存在的录像段内**。此前实现是「取 06:00 后的第一段、
// 并把起点截到 06:00」，当 06:00~首段之间有长时间空档时（例如当天服务重启过、
// 录像从 10:01 才开始），算出的时间点会落进空档 → 后端返回
// "no recordings in range" → 回放页一直转圈。
// 这里改为：找到 06:00 之后的第一段后，起点直接取该段自身的 start。
function earliestInterestingStart(): number {
  const day = dayStart.value
  const morning = day + 6 * 3600000
  for (const s of segments.value) {
    // 段的结束时间在 06:00 之后，说明这段属于「白天」，从它开头看即可
    if (s.end > morning) return s.start
  }
  return segments.value[0].start
}

async function buildSession(fromTs: number) {
  const token = ++sessionToken
  const winStart = Math.max(dayStart.value, fromTs - 120000)
  const winEnd = Math.min(dayStart.value + 86400000, fromTs + 600000)
  if (winEnd <= winStart) return
  try {
    const url = await createPlayback(deviceId.value, winStart, winEnd)
    // 从 URL 中解析会话名：/api/stream/playback/<session>/index.m3u8
    const m = /\/playback\/([^/]+)\//.exec(url)
    const prev = currentSession
    currentSession = m ? m[1] : ''
    // 换会话时停掉上一个，避免多个转码进程同时占编码器
    if (prev && prev !== currentSession) stopPlayback(prev)
    if (token !== sessionToken) return
    const el = playerRef.value?.getVideoEl()
    if (!el) return
    playable?.destroy()
    playable = createPlayable({ url, baseTs: winStart })
    playable.attach(el)
    playable.setSpeed(speed.value)
    // 新会话已就绪：游标回到真实播放位置，并恢复由播放位置驱动进度条
    currentTs.value = playable.time
    seekPending.value = false
    playError.value = ''
  } catch (e: any) {
    // 请求失败：可能是「窗口内无录像」，也可能是后端等首个分片超时
    // （HEVC 源需转码，实测首片要数秒）。把后端给的原因显示出来，
    // 否则用户只看到进度条不动，无从判断。
    seekPending.value = false
    const msg = e?.response?.data?.error
    if (msg) playError.value = msg
  }
}

function startTimer() {
  if (timer) return
  timer = window.setInterval(() => {
    // 拖动中或跳转尚未生效时，绝不能用旧会话的播放位置回写 currentTs：
    // 重建会话有防抖、ffmpeg 还要 1-3 秒才产出首个分片，这期间旧位置会把
    // 游标拉回原处，用户感受就是「进度条拖不动」。
    if (seekPending.value) return
    if (playing.value && playable) currentTs.value = playable.time
  }, 500)
}

// 拖动过程中持续触发：只更新游标，并尽量就地跳转。
// 真实后端下回放是 ffmpeg 边转码边切片的 HLS，窗口未就绪时不能 seek，
// 这种情况留给 seekend 重建会话，避免拖动时反复触发转码。
function onSeek(ts: number) {
  currentTs.value = ts
  if (!isBackend() || isDemoMode()) {
    playable?.seek(ts)
    return
  }
  seekPending.value = true
  if (playable?.canSeekTo(ts)) {
    playable.seek(ts)
    seekPending.value = false
  }
}

// 松手后提交最终位置：能就地跳转就直接跳，否则防抖重建会话。
function onSeekEnd(ts: number) {
  currentTs.value = ts
  if (!isBackend() || isDemoMode()) {
    playable?.seek(ts)
    return
  }
  if (playable?.canSeekTo(ts)) {
    playable.seek(ts)
    seekPending.value = false
    return
  }
  seekPending.value = true
  if (seekTimer) window.clearTimeout(seekTimer)
  seekTimer = window.setTimeout(() => {
    if (segments.value.length) buildSession(ts)
    else seekPending.value = false
  }, 300)
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
  // 走 seekend：点击不像拖动那样随后一定会有抬手事件来收尾，
  // 否则 seekPending 会一直挂起，进度条就再也不会跟随播放。
  onSeekEnd(s.start)
}

function downloadSegment(s: RecordingSegment) {
  if (!deviceId.value) return
  const d = new Date(s.start)
  const p2 = (n: number) => String(n).padStart(2, '0')
  const date = `${d.getFullYear()}-${p2(d.getMonth() + 1)}-${p2(d.getDate())}`
  const time = `${p2(d.getHours())}${p2(d.getMinutes())}${p2(d.getSeconds())}`
  const url = downloadRecordingURL(deviceId.value, date, time)
  const a = document.createElement('a')
  a.href = url
  a.download = `${device.value?.name || 'recording'}_${date}_${time}.mp4`
  a.click()
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

// 选择设备：优先用路由参数，否则用第一个设备。
// 返回是否成功选到设备。
function pickInitialDevice(): boolean {
  const q = route.query.device as string | undefined
  if (q && store.byId(q)) {
    deviceId.value = q
    return true
  }
  if (store.devices.length) {
    deviceId.value = store.devices[0].id
    return true
  }
  return false
}

onMounted(() => {
  pickInitialDevice()
  startTimer()
})

// 关键修复：App.vue 的 devices.load() 是异步的，直接进入/刷新本页时
// 设备列表可能尚未就绪。此处等列表到位后自动选设备并加载，
// 否则 deviceId 为空会请求 /api/devices//recordings 而始终没有画面。
watch(
  () => store.devices.length,
  () => {
    // 只负责选设备；后续加载由 deviceId 的 watch 统一驱动，避免重复请求
    if (!deviceId.value) pickInitialDevice()
  },
)

watch(deviceId, () => {
  loadMonth()
  loadSegments()
})
watch(ym, loadMonth)
watch(dateStr, () => {
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
  // 离开回放页立即结束服务端会话：转码是「一次性顺序转码整个请求窗口」，
  // 不主动停就会在后台白跑十几分钟并持续占用编码器（实测 80% CPU）。
  // 用 sendBeacon 而非普通请求：页面卸载时 fetch 可能被浏览器取消。
  if (currentSession && isBackend() && !isDemoMode()) {
    const token = localStorage.getItem('nvr_token')
    const url = `${apiBase()}/api/playback/${currentSession}/stop${token ? `?token=${encodeURIComponent(token)}` : ''}`
    const ok = navigator.sendBeacon?.(url)
    // sendBeacon 以 POST 发送空体，不带 Authorization 头，因此后端
    // 通过 query 里的 token 鉴权；不支持时退回普通请求。
    if (!ok) stopPlayback(currentSession)
    currentSession = ''
  }
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
          <!-- 播放器常驻：原先 v-if/v-else 会在加载时销毁并重建组件，
               导致 playerRef 时序错乱、画面闪烁与播放位置重置 -->
          <PlaybackPlayer
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
          <div v-if="loading || seekPending || playError" class="loading-mask">
            <van-loading v-if="loading || seekPending" />
            <span v-if="seekPending && !loading" class="seek-hint">正在准备回放…</span>
            <span v-else-if="playError && !loading" class="seek-hint err">{{ playError }}</span>
          </div>
        </div>

        <div class="timeline-wrap">
          <div class="tl-head">
            <span class="mono">{{ dateStr }} 录像</span>
            <span>{{ segments.length }} 段 · 共 {{ totalMinutes }} 分钟 · {{ dayEvents.length }} 事件</span>
          </div>
          <TimelineBar
            :day-start="dayStart"
            :segments="segments"
            :events="timelineEvents"
            :value="currentTs"
            @seek="onSeek"
            @seekend="onSeekEnd"
          />
        </div>

        <!-- 事件联动：点事件跳到对应时刻播放；含事件的录像段在时间轴上已着色 -->
        <!-- 事件与分段不再互斥：当天有事件时，事件列表在上、按小时折叠的分段在下。
             原先用 v-if/v-else 二选一，导致「有事件的那天看不到分段列表」——
             而恰恰是有事件的日子更需要按时间翻找录像。 -->
        <div v-if="dayEvents.length" class="seg-list evt-list">
          <span
            v-for="e in dayEvents"
            :key="e.id"
            class="seg-item evt-item"
            :class="{ on: Math.abs(currentTs - evTime(e)) < 20000 }"
          >
            <span class="evt-time mono" @click="onSelectEvent(e)">{{ fmtHM(evTime(e)) }}</span>
            <span class="evt-badge" :class="e.type">{{ EV_TYPE_LABEL[e.type] || e.type }}</span>
            <span class="evt-label" @click="onSelectEvent(e)">{{ e.label || e.description || '查看详情' }}</span>
            <van-icon
              v-if="segOfEvent(e) && isBackend() && !isDemoMode()"
              name="down"
              class="seg-dl"
              @click.stop="downloadSegment(segOfEvent(e)!)"
            />
          </span>
        </div>
        <!-- 分段列表：默认按「上午/下午 → 小时」折叠；段多时避免超长平铺 -->
        <div class="seg-wrap">
          <div v-if="segments.length" class="seg-toolbar">
            <span class="seg-count">
              当日 {{ segments.length }} 段 · {{ fmtDur(totalMinutes) }}
            </span>
            <span class="seg-mode" @click="groupMode = !groupMode">
              <van-icon :name="groupMode ? 'bars' : 'wap-nav'" size="14" />
              {{ groupMode ? '按小时折叠' : '平铺显示' }}
            </span>
          </div>

          <template v-if="groupMode">
            <div v-for="half in halfGroups" :key="half.key" class="half-block">
              <div class="half-head">
                <span class="half-title">{{ half.title }}</span>
                <span class="half-meta">{{ half.segCount }} 段 · {{ fmtDur(half.durationMin) }}</span>
              </div>
              <div v-for="h in half.hours" :key="h.key" class="hour-block">
                <div class="hour-head" :class="{ open: openHours.includes(h.key) }" @click="toggleHour(h.key)">
                  <van-icon :name="openHours.includes(h.key) ? 'arrow-down' : 'arrow'" size="13" />
                  <span class="hour-label mono">{{ h.hourLabel }}</span>
                  <span class="hour-meta">{{ h.segs.length }} 段 · {{ fmtDur(h.durationMin) }}</span>
                </div>
                <div v-show="openHours.includes(h.key)" class="seg-list">
                  <span
                    v-for="s in h.segs"
                    :key="s.id"
                    class="seg-item"
                    :class="{ on: currentTs >= s.start && currentTs <= s.end }"
                  >
                    <span class="seg-time mono" @click="onSelectSegment(s)">{{ fmtRange(s.start, s.end) }}</span>
                    <van-icon
                      v-if="isBackend() && !isDemoMode()"
                      name="down"
                      class="seg-dl"
                      @click.stop="downloadSegment(s)"
                    />
                  </span>
                </div>
              </div>
            </div>
          </template>

          <div v-else class="seg-list">
            <span
              v-for="s in segments"
              :key="s.id"
              class="seg-item"
              :class="{ on: currentTs >= s.start && currentTs <= s.end }"
            >
              <span class="seg-time mono" @click="onSelectSegment(s)">{{ fmtRange(s.start, s.end) }}</span>
              <van-icon v-if="isBackend() && !isDemoMode()" name="down" class="seg-dl" @click.stop="downloadSegment(s)" />
            </span>
          </div>

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
  overflow: hidden;
}
.loading-mask {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  background: rgba(0, 0, 0, 0.45);
  z-index: 2;
}
.seek-hint.err {
  color: var(--nvr-amber, #ffb054);
  max-width: 80%;
  text-align: center;
  line-height: 1.6;
}
.seek-hint {
  font-size: 12px;
  color: rgba(255, 255, 255, 0.85);
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
.seg-wrap {
  padding: 4px 14px 14px;
}
/* 工具栏：段数统计 + 折叠/平铺切换 */
.seg-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 2px 0 8px;
  font-size: 12px;
  color: var(--nvr-text-2);
}
.seg-count {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.seg-mode {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
  padding: 3px 10px;
  border-radius: 999px;
  background: var(--nvr-panel-2);
  border: 1px solid var(--nvr-border);
  cursor: pointer;
}
.seg-mode:active {
  color: var(--nvr-accent);
}
/* 上午 / 下午 */
.half-block + .half-block {
  margin-top: 10px;
}
.half-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
  padding: 6px 2px;
}
.half-title {
  font-size: 13px;
  font-weight: 600;
}
.half-meta {
  font-size: 11px;
  color: var(--nvr-text-2);
}
/* 小时行 */
.hour-block {
  border-radius: 10px;
  overflow: hidden;
  border: 1px solid var(--nvr-border);
  margin-bottom: 6px;
}
.hour-head {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 9px 10px;
  background: var(--nvr-panel-2);
  cursor: pointer;
  font-size: 12px;
}
.hour-head.open {
  color: var(--nvr-accent);
}
.hour-label {
  font-weight: 600;
}
.hour-meta {
  margin-left: auto;
  color: var(--nvr-text-2);
  font-size: 11px;
}
.seg-wrap .seg-list {
  padding: 8px 10px 10px;
  background: var(--nvr-panel);
}
.seg-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding: 4px 14px 14px;
}
.seg-item {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 4px 10px;
  border-radius: 6px;
  background: var(--nvr-panel-2);
  border: 1px solid var(--nvr-border);
  font-size: 12px;
  cursor: pointer;
}
.seg-item.on {
  border-color: var(--nvr-accent);
  color: var(--nvr-accent);
}
.seg-time {
  cursor: pointer;
}
/* 事件联动列表 */
.evt-list {
  flex-direction: column;
  flex-wrap: nowrap;
  gap: 6px;
  max-height: 180px;
  overflow-y: auto;
}
.evt-item {
  width: 100%;
  gap: 8px;
}
.evt-time {
  font-weight: 600;
  cursor: pointer;
  flex-shrink: 0;
}
.evt-badge {
  flex-shrink: 0;
  font-size: 11px;
  line-height: 1;
  padding: 3px 7px;
  border-radius: 9px;
  color: #fff;
}
.evt-badge.motion { background: #ffb020; color: #4a2c00; }
.evt-badge.ai { background: #ff4d4f; }
.evt-badge.manual { background: #a26bf0; }
.evt-badge.offline { background: #8b93a7; }
.evt-badge.online { background: #2ecc8f; color: #053b28; }
.evt-label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  cursor: pointer;
}
.seg-dl {
  font-size: 14px;
  color: var(--nvr-text-2);
  cursor: pointer;
  padding: 2px;
}
.seg-dl:active {
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
