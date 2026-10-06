<script setup lang="ts">
import { computed, nextTick, onActivated, onDeactivated, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { showToast } from 'vant'
import type { DayRecord, Device, EventItem, RecordingSegment } from '../types'
import { useDeviceStore } from '../stores/devices'
import { apiBase, createPlayback, downloadRecordingURL, fetchDaySegments, fetchEvents, fetchMonthRecords, isBackend, isDemoMode, stopPlayback } from '../api'
import { hashStr } from '../mocks/generator'
import { createPlayable, type Playable } from '../utils/player'
import CalendarHeat from '../components/CalendarHeat.vue'
import EventsView from './EventsView.vue'
import TimelineBar from '../components/TimelineBar.vue'
import PlaybackPlayer from '../components/PlaybackPlayer.vue'
import PlaybackDateBar from '../components/PlaybackDateBar.vue'
import { enterFullscreen, exitFullscreen } from '../utils/screen'

const store = useDeviceStore()
const route = useRoute()
const router = useRouter()

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
let loadToken = 0
let monthToken = 0
let active = true
let sessionEnd = 0
// 页内主 tab：回放 / 事件（原独立「事件中心」页并入此处，路由 /events 仍可直达）
const activeTab = ref<'playback' | 'events'>(
  (route.query.tab as string) === 'events' ? 'events' : 'playback',
)
const eventsMounted = ref(activeTab.value === 'events')
// 移动端内嵌日历收进弹层：<900px 隐藏侧栏日历，改点「选日期」弹出
const calOpen = ref(false)
watch(activeTab, (t) => {
  if (t === 'events') eventsMounted.value = true
  if (route.query.tab !== (t === 'events' ? 'events' : undefined)) {
    router.replace({ query: { ...route.query, tab: t === 'events' ? 'events' : undefined } })
  }
})
// 已挂载时直达 /playback?tab=events（如从 /events 旧链接重定向）也要切过去
watch(() => route.query.tab, (t) => {
  const want = t === 'events' ? 'events' : 'playback'
  if (want !== activeTab.value) activeTab.value = want
})

function releasePlayback() {
  sessionToken++
  if (seekTimer) window.clearTimeout(seekTimer)
  seekTimer = null
  playable?.destroy()
  playable = null
  if (currentSession) void stopPlayback(currentSession)
  currentSession = ''
  seekPending.value = false
}

function fmtRange(a: number, b: number) {
  const f = (ts: number) => {
    const d = new Date(ts)
    return `${p2(d.getHours())}:${p2(d.getMinutes())}:${p2(d.getSeconds())}`
  }
  return `${f(a)}-${f(b)}`
}

const device = computed<Device | undefined>(() => store.byId(deviceId.value))
const dayStart = computed(() => new Date(`${dateStr.value}T00:00:00`).getTime())
const ym = computed(() => dateStr.value.slice(0, 7))
const totalMinutes = computed(() => Math.round(segments.value.reduce((a, s) => a + (s.end - s.start), 0) / 60000))

async function loadMonth() {
  const token = ++monthToken
  if (!deviceId.value) {
    monthDays.value = []
    return
  }
  try {
    const days = await fetchMonthRecords(deviceId.value, ym.value)
    if (token === monthToken && active) monthDays.value = days
  } catch {
    if (token === monthToken) monthDays.value = []
  }
}

async function loadSegments() {
  if (!active) return
  const token = ++loadToken
  releasePlayback()
  segments.value = []
  dayEvents.value = []
  currentTs.value = dayStart.value
  playError.value = ''
  playing.value = false
  if (!deviceId.value) {
    segments.value = []
    dayEvents.value = []
    return
  }
  loadEvents(token)
  loading.value = true
  try {
    const result = await fetchDaySegments(deviceId.value, dateStr.value)
    if (token !== loadToken || !active) return
    segments.value = result.sort((a, b) => a.start - b.start)
  } catch (e: any) {
    if (token !== loadToken || !active) return
    // 连接故障（ConnUnavailableError）没有 response，回退到 message，
    // 否则把「服务器不可达」显示成「录像加载失败」，误导排查方向
    playError.value = e?.response?.data?.error || e?.message || '录像加载失败，请重试'
  } finally {
    if (token === loadToken) loading.value = false
  }
  if (token === loadToken && active) rebuildPlayable()
}

async function loadEvents(token: number) {
  if (!deviceId.value) {
    dayEvents.value = []
    return
  }
  try {
    const result = await fetchEvents(deviceId.value, dateStr.value, '', 0, 500)
    if (token === loadToken && active) dayEvents.value = result.events
  } catch {
    if (token === loadToken) dayEvents.value = []
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
    g.durationMin += (seg.end - seg.start) / 60000
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
  min = Math.round(min)
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
  if (!active || !el || !device.value || !segments.value.length) return
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
  const winStart = Math.max(dayStart.value, fromTs)
  // 不把录像空档拼接成连续时间：时间轴与视频必须保持同一时钟。
  let coverageEnd = fromTs
  for (const segment of segments.value) {
    if (segment.end <= fromTs) continue
    if (segment.start > coverageEnd + 1000) break
    coverageEnd = Math.max(coverageEnd, segment.end)
  }
  const winEnd = Math.min(dayStart.value + 86400000, fromTs + 600000, coverageEnd)
  if (winEnd <= winStart) return
  try {
    const url = await createPlayback(deviceId.value, winStart, winEnd)
    // 从 URL 中解析会话名：/api/stream/playback/<session>/index.m3u8
    const m = /\/playback\/([^/]+)\//.exec(url)
    const session = m ? m[1] : ''
    if (token !== sessionToken || !active) {
      if (session) void stopPlayback(session)
      return
    }
    const prev = currentSession
    currentSession = session
    // 换会话时停掉上一个，避免多个转码进程同时占编码器
    if (prev && prev !== currentSession) stopPlayback(prev)
    const el = playerRef.value?.getVideoEl()
    if (!el) return
    playable?.destroy()
    playable = createPlayable({ url, baseTs: winStart })
    playable.attach(el)
    playable.setSpeed(speed.value)
    if (!playing.value) playable.pause()
    sessionEnd = winEnd
    // 新会话已就绪：游标回到真实播放位置，并恢复由播放位置驱动进度条
    currentTs.value = playable.time
    seekPending.value = false
    playError.value = ''
  } catch (e: any) {
    if (token !== sessionToken || !active) return
    // 请求失败：可能是「窗口内无录像」，也可能是后端等首个分片超时
    // （HEVC 源需转码，实测首片要数秒）。把后端给的原因显示出来，
    // 否则用户只看到进度条不动，无从判断。
    seekPending.value = false
    const msg = e?.response?.data?.error || e?.message
    playError.value = msg || '回放准备失败，请重试'
  }
}

function startTimer() {
  if (timer) return
  timer = window.setInterval(() => {
    // 拖动中或跳转尚未生效时，绝不能用旧会话的播放位置回写 currentTs：
    // 重建会话有防抖、ffmpeg 还要 1-3 秒才产出首个分片，这期间旧位置会把
    // 游标拉回原处，用户感受就是「进度条拖不动」。
    if (seekPending.value) return
    if (playing.value && playable) {
      currentTs.value = playable.time
      if (isDemoMode() && !segments.value.some(s => currentTs.value >= s.start && currentTs.value < s.end)) {
        const next = segments.value.find(s => s.start > currentTs.value)
        if (next) onSeekEnd(next.start)
        else { playable.pause(); playing.value = false }
      }
    }
  }, 500)
}

// 拖动过程中持续触发：只更新游标，并尽量就地跳转。
// 真实后端下回放是 ffmpeg 边转码边切片的 HLS，窗口未就绪时不能 seek，
// 这种情况留给 seekend 重建会话，避免拖动时反复触发转码。
function onSeek(ts: number) {
  if (!segments.value.length || loading.value) return
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
  if (!segments.value.length || loading.value) return
  sessionToken++
  if (seekTimer) window.clearTimeout(seekTimer)
  seekTimer = null
  const containing = segments.value.find(s => ts >= s.start && ts < s.end)
  if (!containing) {
    const next = segments.value.find(s => s.start >= ts)
    const last = segments.value[segments.value.length - 1]
    ts = next ? next.start : Math.max(last.start, last.end - 1000)
    showToast('所选时间无录像，已定位到最近录像')
  }
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

// 跳转时间：输入移入播放器控制条（时钟图标弹出），此处只做校验与定位
function handleJump(text: string) {
  const m = /^(\d{1,2}):(\d{2}):(\d{2})$/.exec(text)
  const h = m ? Number(m[1]) : NaN
  const min = m ? Number(m[2]) : NaN
  const sec = m ? Number(m[3]) : NaN
  if (!m || h > 23 || min > 59 || sec > 59) {
    showToast('请输入有效时间，格式 HH:MM:SS')
    return
  }
  onSeekEnd(dayStart.value + (h * 3600 + min * 60 + sec) * 1000)
}

// 播放器控制条的 ±30 秒图标
function onSeekRel(deltaMs: number) {
  if (!segments.value.length || loading.value) return
  onSeekEnd(Math.max(dayStart.value, currentTs.value + deltaMs))
}

function onEnded() {
  const next = segments.value.find(s => s.end > sessionEnd + 500)
  if (next) {
    currentTs.value = Math.max(sessionEnd, next.start)
    seekPending.value = true
    void buildSession(currentTs.value)
  } else playing.value = false
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

function onCalPick(d: string) {
  calOpen.value = false
  onSelectDate(d)
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

async function onFullscreen() {
  const wrap = playerRef.value?.getWrap()
  if (!wrap) return
  if (document.fullscreenElement) {
    await exitFullscreen()
  } else {
    await enterFullscreen(wrap, playerRef.value?.getVideoEl())
  }
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

// 设备列表变化时保证 deviceId 始终指向一个真实存在的设备。
// 关键修复：此前只监听 devices.length 且只在「当前无选中」时选设备，
// 以下情况会留下失效的 deviceId（请求落在已删除的设备上，页面永远空）：
//   · 设备被删除或不再是本账号可见；
//   · 设备数量不变但整体被替换（同数量替换）；
//   · 从设备 A 的直链进入后 A 消失，B 才是有效设备。
watch(
  () => store.devices.map((d) => d.id).join(','),
  () => {
    const q = route.query.device as string | undefined
    // 当前选中仍有效：什么都不做，避免打断正在播放的回放
    if (deviceId.value && store.byId(deviceId.value)) return
    // 直链指定的设备可用时优先选它
    if (q && store.byId(q)) {
      deviceId.value = q
      return
    }
    if (store.devices.length) deviceId.value = store.devices[0].id
    else deviceId.value = ''
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
onDeactivated(() => {
  active = false
  loadToken++
  monthToken++
  releasePlayback()
  if (timer) window.clearInterval(timer)
  timer = null
})
onActivated(() => {
  if (active) return
  active = true
  const q = route.query.device as string | undefined
  if (q && q !== deviceId.value && store.byId(q)) deviceId.value = q
  else { loadMonth(); loadSegments() }
  startTimer()
})

// 结束服务端回放会话（停掉转码进程）并返回被结束的会话名。
// 转码是「一次性顺序转码整个请求窗口」，不主动停就会在后台白跑十几分钟
// 并持续占用编码器（实测 80% CPU）。
function takeCurrentSession(): string {
  const s = currentSession
  currentSession = ''
  return s
}

// 用 sendBeacon 而非普通请求：页面卸载时 fetch 会被浏览器取消，
// beacon 由浏览器在后台发出。注意 beacon 以 POST 发空体、不带
// Authorization 头，只能靠 query token 鉴权（后端 streamAuth 支持 ?token=）。
function releaseSessionOnUnload() {
  const session = takeCurrentSession()
  if (!session) return
  if (!isBackend() || isDemoMode()) return
  const token = localStorage.getItem('nvr_token')
  const url = `${apiBase()}/api/playback/${session}/stop${token ? `?token=${encodeURIComponent(token)}` : ''}`
  const ok = navigator.sendBeacon?.(url)
  if (!ok) void stopPlayback(session)
}

onBeforeUnmount(() => {
  active = false
  loadToken++
  monthToken++
  if (timer) window.clearInterval(timer)
  if (seekTimer) window.clearTimeout(seekTimer)
  playable?.destroy()
  playable = null
  // 兜底：keep-alive 的 onDeactivated 通常已释放会话，但 App 被杀死、
  // 标签页直接关闭等场景它可能没执行，这里必须再做一次。
  releaseSessionOnUnload()
})
</script>

<template>
  <div class="page playback-page">
    <van-nav-bar title="录像管理">
      <template #right>
        <button type="button" class="device-picker control-button" aria-label="选择回放设备" @click="pickDevice">
          {{ device?.name ?? '选择设备' }}
          <van-icon name="arrow-down" size="12" />
        </button>
      </template>
    </van-nav-bar>

    <div class="pb-tabs" role="tablist">
      <button type="button" class="pb-tab" :class="{ on: activeTab === 'playback' }" role="tab" :aria-selected="activeTab === 'playback'" @click="activeTab = 'playback'">回放</button>
      <button type="button" class="pb-tab" :class="{ on: activeTab === 'events' }" role="tab" :aria-selected="activeTab === 'events'" @click="activeTab = 'events'">事件</button>
      <PlaybackDateBar
        class="pb-tab-date"
        :date="dateStr"
        :show-calendar-button="false"
        @prev="shiftDay(-1)"
        @next="shiftDay(1)"
      />
      <!-- 移动端「选日期」入口：日历组件收进页签行尾（原在日期条上） -->
      <button type="button" class="control-button cal-toggle pb-tab-cal" aria-label="选择日期" @click="calOpen = true">
        <van-icon name="calendar-o" size="18" />
      </button>
    </div>

    <div v-show="activeTab === 'events'" class="pb-events-panel">
      <EventsView v-if="eventsMounted" embedded />
    </div>

    <!-- 移动端日期选择弹层（内嵌日历在小屏隐藏，见 .pb-inline-cal） -->
    <van-popup v-model:show="calOpen" position="bottom" round :style="{ maxHeight: '80vh', overflowY: 'auto' }">
      <CalendarHeat :days="monthDays" :selected="dateStr" @select="onCalPick" />
    </van-popup>

    <div v-show="activeTab === 'playback'" class="pb-body">
      <!-- 左列：固定播放区 + 时间轴，桌面/移动端都不随下方内容滚动 -->
      <div class="pb-main">
        <div class="pb-pinned">
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
              @ended="onEnded"
              @seek-rel="onSeekRel"
              @jump="handleJump"
              @error="playError = '视频加载失败，请重试'; playing = false"
            />
<div v-if="loading || seekPending || playError" class="loading-mask">
              <van-loading v-if="loading || seekPending" />
              <span v-if="seekPending && !loading" class="seek-hint">正在准备回放…</span>
              <span v-else-if="playError && !loading" class="seek-hint err">{{ playError }}</span>
              <van-button v-if="playError && !loading && !seekPending" size="small" @click="loadSegments">重新加载</van-button>
            </div>
          </div>
        </div><!-- /.pb-pinned -->

        <div class="timeline-wrap">
          <div class="tl-head">
            <span class="mono">{{ dateStr }} 录像</span>
            <span class="play-status" :class="{ on: playing }">{{ playing ? '播放中' : '已暂停' }}</span>
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
      </div>

      <!-- 右列：桌面端=日期条+日历+当日事件/分段列表（独立滚动）；移动端=日期条+列表 -->
      <div class="pb-side">
        <div class="pb-desktop-cal">
          <PlaybackDateBar
            :date="dateStr"
            :show-calendar-button="false"
            @prev="shiftDay(-1)"
            @next="shiftDay(1)"
            @pick-calendar="calOpen = true"
          />
          <CalendarHeat class="pb-inline-cal" :days="monthDays" :selected="dateStr" @select="onSelectDate" />
        </div>


        <!-- 事件联动：点事件跳到对应时刻播放；含事件的录像段在时间轴上已着色 -->
        <!-- 事件与分段不再互斥：当天有事件时，事件列表在上、按小时折叠的分段在下。
             原先用 v-if/v-else 二选一，导致「有事件的那天看不到分段列表」——
             而恰恰是有事件的日子更需要按时间翻找录像。 -->
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
            <button type="button" class="seg-mode" :aria-pressed="groupMode" @click="groupMode = !groupMode">
              <van-icon :name="groupMode ? 'bars' : 'wap-nav'" size="14" />
              {{ groupMode ? '按小时折叠' : '平铺显示' }}
            </button>
          </div>

          <template v-if="groupMode">
            <div v-for="half in halfGroups" :key="half.key" class="half-block">
              <div class="half-head">
                <span class="half-title">{{ half.title }}</span>
                <span class="half-meta">{{ half.segCount }} 段 · {{ fmtDur(half.durationMin) }}</span>
              </div>
              <div v-for="h in half.hours" :key="h.key" class="hour-block">
                <button type="button" class="hour-head" :aria-expanded="openHours.includes(h.key)" :class="{ open: openHours.includes(h.key) }" @click="toggleHour(h.key)">
                  <van-icon :name="openHours.includes(h.key) ? 'arrow-down' : 'arrow'" size="13" />
                  <span class="hour-label mono">{{ h.hourLabel }}</span>
                  <span class="hour-meta">{{ h.segs.length }} 段 · {{ fmtDur(h.durationMin) }}</span>
                </button>
                <div v-show="openHours.includes(h.key)" class="seg-list">
                  <span
                    v-for="s in h.segs"
                    :key="s.id"
                    class="seg-item"
                    :class="{ on: currentTs >= s.start && currentTs <= s.end }"
                  >
                    <button class="seg-time mono control-button" @click="onSelectSegment(s)">{{ fmtRange(s.start, s.end) }}</button>
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
              <button class="seg-time mono control-button" @click="onSelectSegment(s)">{{ fmtRange(s.start, s.end) }}</button>
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
  font-size: calc(13px * var(--nvr-font-scale, 1));
  display: flex;
  align-items: center;
  gap: 4px;
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
  font-size: calc(12px * var(--nvr-font-scale, 1));
  color: rgba(255, 255, 255, 0.85);
}
.timeline-wrap {
  margin-top: 8px;
}
.tl-head {
  flex-wrap: wrap;
  gap: 6px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 14px 4px;
  font-size: calc(12px * var(--nvr-font-scale, 1));
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
  font-size: calc(12px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
}
.seg-count {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.seg-mode {
  color: inherit;
  font: inherit;
  min-height: 44px;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
  padding: 3px 10px;
  border-radius: var(--nvr-radius-full);
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
  font-size: calc(13px * var(--nvr-font-scale, 1));
  font-weight: 600;
}
.half-meta {
  font-size: calc(11px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
}
/* 小时行 */
.hour-block {
  border-radius: var(--nvr-radius-sm);
  overflow: hidden;
  border: 1px solid var(--nvr-border);
  margin-bottom: 6px;
}
.hour-head {
  width: 100%;
  border: 0;
  color: var(--nvr-text);
  text-align: left;
  min-height: 44px;
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 9px 10px;
  background: var(--nvr-panel-2);
  cursor: pointer;
  font-size: calc(12px * var(--nvr-font-scale, 1));
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
  font-size: calc(11px * var(--nvr-font-scale, 1));
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
  border-radius: var(--nvr-radius-sm);
  background: var(--nvr-panel-2);
  border: 1px solid var(--nvr-border);
  font-size: calc(12px * var(--nvr-font-scale, 1));
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
  font-size: calc(11px * var(--nvr-font-scale, 1));
  line-height: 1;
  padding: 3px 7px;
  border-radius: var(--nvr-radius-sm);
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
  font-size: calc(14px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
  cursor: pointer;
  padding: 2px;
}
.seg-dl:active {
  color: var(--nvr-accent);
}
.none {
  font-size: calc(12px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
}
.picker-head {
  padding: 14px;
  font-weight: 600;
  text-align: center;
}

/* 移动端日历收纳：<900px 隐藏内嵌日历（占用近一屏），改由日期条的
   「选日期」按钮弹出底部弹层（按钮显隐见 PlaybackDateBar.vue）。 */
@media (max-width: 899px) {
  .pb-left {
    display: none;
  }
}
:global(body.care .pb-tab) {
  min-height: 44px;
  padding: 8px 22px;
}

/* 页内主 tab：回放 / 事件 */
.pb-tabs {
  display: flex;
  gap: 6px;
  padding: 10px 16px 6px;
  flex-shrink: 0;
}
.pb-tab {
  padding: 7px 18px;
  border-radius: var(--nvr-radius-full);
  border: 1px solid var(--nvr-border);
  background: var(--nvr-panel);
  color: var(--nvr-text-2);
  font-size: calc(14px * var(--nvr-font-scale, 1));
  min-height: 36px;
}
.pb-tab.on {
  background: var(--nvr-accent-soft-2);
  border-color: var(--nvr-accent);
  color: var(--nvr-accent);
  font-weight: 600;
}
/* 播放状态：原为播放器左上角角标，按反馈移到时间轴信息行（组件外） */
.play-status {
  font-size: calc(12px * var(--nvr-font-scale, 1));
  color: var(--nvr-amber);
}
.play-status.on {
  color: var(--nvr-accent);
}
.pb-events-panel {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
}
.pb-body {
  flex: 1;
  min-height: 0;
  /* 移动端滚动完全交给 .pb-scroll；本容器自身不滚动 */
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

/* .pb-right 是「固定播放区 + 滚动内容区」的纵向 flex 容器。
   它必须显式 min-height:0，否则 flex 项的默认 min-height:auto 会让它按
   内容高度撑开（实测 1918px > 可视 638px），于是 .pb-scroll 的 flex:1
   没有可用空间、被撑成内容高度（scrollHeight == clientHeight），
   滚动就落到 .page 上——播放器随之被滚走，「固定在顶部」失效。 */
.pb-main {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

@media (min-width: 1200px) {
  /* PC 布局：左列播放器+时间轴固定不随页面滚动（视口高度内右列独立滚动），
     右列=日期条+日历+当日事件/分段列表 */
  .pb-body {
    flex-direction: row;
    gap: 20px;
    padding: 0 24px;
    overflow: visible;
  }
  .pb-main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .pb-pinned {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .player-wrap {
    flex: 1;
    min-height: 0;
    padding: 0;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  /* 播放器撑满可用高度（16:9 由视频 object-fit:contain 自适应，超出留黑边） */
  .player-wrap :deep(.player) {
    width: 100%;
    height: 100%;
    aspect-ratio: auto;
  }
  .timeline-wrap {
    flex-shrink: 0;
  }
  .pb-side {
    width: 380px;
    flex-shrink: 0;
    min-width: 0;
    overflow-y: auto;
    padding-bottom: 16px;
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
}

/* ── 移动端：播放组件固定在顶部，其余组件在下方独立滚动 ──
   与之前用 position:sticky 的区别：sticky 下播放器**先随内容下移**
   （它初始位于日期栏之下），滚到顶才吸附，视觉上是「跟着滚一段再钉住」。
   这里改为显式两段式布局：
     .pb-pinned —— 固定在内容区顶部的播放区，自始至终不移动；
     .pb-scroll —— 下方唯一的滚动容器，日期条/时间轴/分段列表都在其中。
   两者是上下排列的兄弟节点而非覆盖关系，因此不存在「固定区遮挡内容」。
   桌面端（>=1200）在下方恢复左右分栏、关闭两段式。 */
.pb-pinned {
  flex-shrink: 0;
}
.pb-side {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding-bottom: 8px;
}
/* 移动端日期条：桌面日历列在 <1200px 隐藏，移动日期条补位（<1200 显示）；
   与时间轴拉开间距（贴着时间轴的时间标签显得重叠） */
.pb-tab-date {
  display: none;
}
@media (max-width: 1199px) {
  /* TimelineBar 的 00:00/24:00 时间标签是绝对定位、悬在 wrap 之外，
     不预留空间会与下方日期条重叠（实测 -18px） */
  .pb-main .timeline-wrap {
    padding-bottom: 24px;
  }
  .pb-desktop-cal {
    display: none;
  }
  .pb-tab-date {
    display: flex;
    align-items: center;
    margin: 0 2px 0 4px;
  }
  .pb-tab-date .date {
    font-size: calc(13px * var(--nvr-font-scale, 1));
  }
  .pb-tab-date .control-button {
    min-width: 30px;
    min-height: 34px;
  }
  .pb-tab-cal {
    display: inline-flex;
    margin-left: auto;
  }
}

@media (max-width: 1199px) {
  .player-wrap {
    padding: 8px 12px;
    background: var(--nvr-bg);
  }
  /* 播放器高度：竖屏接近 16:9；横屏（如 844×390）下 56.25vw=475px 会占满整屏，
     因此用 62vh 封顶，视频自身 object-fit:contain 居中留黑边。 */
  .player-wrap :deep(.player) {
    aspect-ratio: auto;
    height: clamp(176px, 56.25vw, 62vh);
  }
}

/* 矮屏横屏（手机横放，如 844×390 / 740×360）：
   竖向空间要同时容纳导航栏+页签+固定播放区+内容区+底部导航栏。
   关怀模式这些控件更高（实测 56+54+72=182px），若播放器仍按 62vh 取值，
   固定播放区会高达 258px 而内容区只剩 8px——下方「其他组件」实际不可用。
   这里改为按「扣除固定占位后的剩余高度」来计算播放器高度，
   并给内容区保底高度，确保横屏关怀模式下仍能看到并操作下方组件。 */
@media (max-width: 1199px) and (max-height: 520px) {
  .player-wrap :deep(.player) {
    /* 190px ≈ 导航栏+页签+底部导航栏（按关怀模式取较大值），96px 留给内容区，
       再给播放器 80px 下限：避免在 360px 高的屏上把画面压到不可辨认。
       上限 45vh 防止普通字体下播放器又占掉大半屏。 */
    height: clamp(80px, calc(100dvh - 190px - 96px), 45vh);
  }
  .pb-side {
    min-height: 80px;
  }
}

/* <900px：左栏（日期条 + 内嵌月历）整体隐藏，日期条改挂到下方滚动区 */
@media (min-width: 1200px) {
  /* 桌面端：右列自身滚动，两段式布局与移动日期条都关闭 */
  .pb-pinned,
  .pb-side {
    flex: none;
    min-height: 0;
    overflow: visible;
    padding-bottom: 0;
  }
  .pb-side {
    overflow-y: auto;
    padding-bottom: 16px;
  }
  .pb-mobile-date {
    display: none;
  }
}
</style>
