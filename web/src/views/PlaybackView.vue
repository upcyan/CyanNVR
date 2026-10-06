<script setup lang="ts">
import { computed, nextTick, onActivated, onDeactivated, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { showToast } from 'vant'
import type { DayRecord, Device, EventItem, RecordingSegment } from '../types'
import { useDeviceStore } from '../stores/devices'
import { apiBase, createExport, createPlayback, deleteExport, downloadRecordingURL, exportFileURL, fetchDaySegments, fetchEvents, fetchExports, fetchMonthRecords, isBackend, isDemoMode, stopPlayback, type ExportTask } from '../api'
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

const p2 = (n: number) => String(n).padStart(2, '0')
/** 本地「今天」的日期串（每次重新求值，跨零点后不能沿用建页时的常量） */
const todayLocal = () => {
  const d = new Date()
  return `${d.getFullYear()}-${p2(d.getMonth() + 1)}-${p2(d.getDate())}`
}

const deviceId = ref('')
const dateStr = ref(todayLocal())
/** 用户是否主动选过日期（翻页/日历）。未选过时，跨零点激活页面要对齐到新的一天 */
let datePicked = false
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

// ── 时间轴标记：在当前播放位置打点，导出弹窗可一键填入起止时间 ──
const marks = ref<number[]>([])
const markBtnOn = computed(() => marks.value.some((m) => Math.abs(m - currentTs.value) < 1500))
function toggleMark() {
  if (!segments.value.length) {
    showToast('当前没有录像，无法标记')
    return
  }
  const hit = marks.value.findIndex((m) => Math.abs(m - currentTs.value) < 1500)
  if (hit >= 0) {
    marks.value = marks.value.filter((_, i) => i !== hit)
    showToast('已取消该标记')
    return
  }
  marks.value = [...marks.value, currentTs.value].sort((a, b) => a - b)
  if (marks.value.length > 12) marks.value = marks.value.slice(-12)
  showToast(`已标记 ${fmtHM(currentTs.value)}`)
}

// ── 导出：独立按钮 + 弹窗（起止时间滚轮选择 / 标记快填 / 任务列表）──
const exportOpen = ref(false)
const exports = ref<ExportTask[]>([])
let exportTimer: number | null = null
const exportStart = ref(0) // 当日 00:00 起的秒数
const exportEnd = ref(0)
const markStage = ref(0) // 0=等待选起点 1=等待选终点
const creating = ref(false)
const timePickOpen = ref(false)
const timePickValues = ref<number[]>([0, 0, 0])
let timePickField: 'start' | 'end' = 'start'

const runningExports = computed(() => exports.value.filter((t) => t.status === 'running').length)

async function refreshExports() {
  if (!isBackend() || isDemoMode()) return
  try {
    const prev = new Map(exports.value.map((t) => [t.id, t.status]))
    exports.value = await fetchExports()
    // 只对「亲眼看着它跑起来」的任务提示完成，避免进页面被历史任务刷屏
    for (const t of exports.value) {
      if (t.status === 'done' && prev.get(t.id) === 'running') {
        showToast('导出完成，点导出按钮可下载')
      }
    }
  } catch {
    /* 轮询失败静默，下一轮再取 */
  }
}
function startExportPoll() {
  if (exportTimer != null) return
  void refreshExports()
  exportTimer = window.setInterval(() => void refreshExports(), 3000)
}
function stopExportPoll() {
  if (exportTimer != null) {
    window.clearInterval(exportTimer)
    exportTimer = null
  }
}

function secOfDay(ts: number) {
  const d = new Date(ts)
  return d.getHours() * 3600 + d.getMinutes() * 60 + d.getSeconds()
}
function fmtSec(sec: number) {
  const v = Math.max(0, Math.min(86399, Math.round(sec)))
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(Math.floor(v / 3600))}:${p(Math.floor(v / 60) % 60)}:${p(v % 60)}`
}
function fmtSizeMB(n: number) {
  const mb = n / 1048576
  return mb >= 1024 ? `${(mb / 1024).toFixed(2)} GB` : `${mb.toFixed(1)} MB`
}
function exportStatusText(t: ExportTask) {
  if (t.status === 'running') return '导出中…'
  if (t.status === 'done') return '已完成'
  if (t.status === 'pending') return '排队中'
  return t.error || '失败'
}

function openExportDialog() {
  // 默认导出范围 = 当天有录像的区间（首段起点 → 末段终点），无录像则当前时刻前 30 分钟
  if (segments.value.length) {
    exportStart.value = secOfDay(segments.value[0].start)
    exportEnd.value = Math.max(
      exportStart.value + 60,
      secOfDay(segments.value[segments.value.length - 1].end - 1000),
    )
  } else {
    const now = secOfDay(Date.now())
    exportStart.value = Math.max(0, now - 1800)
    exportEnd.value = now
  }
  markStage.value = 0
  exportOpen.value = true
  void refreshExports()
}

const hourCol = Array.from({ length: 24 }, (_, i) => ({ text: `${String(i).padStart(2, '0')} 时`, value: i }))
const minCol = Array.from({ length: 60 }, (_, i) => ({ text: `${String(i).padStart(2, '0')} 分`, value: i }))
const secCol = Array.from({ length: 60 }, (_, i) => ({ text: `${String(i).padStart(2, '0')} 秒`, value: i }))
const timeColumns = [hourCol, minCol, secCol]

function openTimePick(field: 'start' | 'end') {
  const v = field === 'start' ? exportStart.value : exportEnd.value
  timePickValues.value = [Math.floor(v / 3600), Math.floor(v / 60) % 60, v % 60]
  timePickField = field
  timePickOpen.value = true
}
function onTimePickConfirm({ selectedValues }: { selectedValues: number[] }) {
  const [h, m, s] = selectedValues.map(Number)
  const v = h * 3600 + m * 60 + s
  if (timePickField === 'start') exportStart.value = v
  else exportEnd.value = v
  timePickOpen.value = false
}

// 标记快填：第一次点 = 起点，第二次点 = 终点（终点更早时自动对调）
function pickMark(m: number) {
  const sec = secOfDay(m)
  if (markStage.value === 0) {
    exportStart.value = sec
    markStage.value = 1
    showToast('已设为起点，再点一个标记作为终点')
  } else if (sec > exportStart.value) {
    exportEnd.value = sec
    markStage.value = 0
  } else {
    exportEnd.value = exportStart.value
    exportStart.value = sec
    markStage.value = 0
  }
}

async function confirmExport() {
  if (!deviceId.value || creating.value) return
  if (exportEnd.value <= exportStart.value) {
    showToast('截止时间必须晚于起始时间')
    return
  }
  creating.value = true
  try {
    await createExport(deviceId.value, dayStart.value + exportStart.value * 1000, dayStart.value + exportEnd.value * 1000)
    showToast('导出任务已创建')
    exportOpen.value = false
    void refreshExports()
  } catch (err: any) {
    showToast(err?.response?.data?.error || '导出任务创建失败')
  } finally {
    creating.value = false
  }
}
async function removeExport(t: ExportTask) {
  try {
    await deleteExport(t.id)
    exports.value = exports.value.filter((x) => x.id !== t.id)
  } catch {
    showToast('删除失败')
  }
}

// ── 事件页/深链跳转：?device=&date=YYYY-MM-DD&t=<毫秒> ──
// 消费一次后按 key 去重；日期变化走 watch 触发重载，
// 分段加载完成后 rebuildPlayable 直接用 pendingSeekTs 定位（避免二次建会话）。
let pendingSeekTs = 0
let lastSeekQuery = ''
function consumeSeekQuery(): boolean {
  const q = route.query
  const date = typeof q.date === 'string' ? q.date : ''
  const t = Number(q.t)
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date) || !Number.isFinite(t) || t <= 0) return false
  const key = `${date}|${t}`
  if (key === lastSeekQuery) return false
  lastSeekQuery = key
  datePicked = true
  pendingSeekTs = t
  if (dateStr.value !== date) dateStr.value = date
  else loadSegments()
  return true
}
// 把请求时刻吸附到真实存在的录像段（与 onSeekEnd 同一套兜底）
function snapToSegment(ts: number): number {
  const containing = segments.value.find((s) => ts >= s.start && ts < s.end)
  if (containing) return ts
  const next = segments.value.find((s) => s.start >= ts)
  if (next) return next.start
  const last = segments.value[segments.value.length - 1]
  return Math.max(last.start, last.end - 1000)
}
watch(
  () => [String(route.query.date ?? ''), String(route.query.t ?? '')].join('|'),
  () => {
    if (route.path === '/playback') consumeSeekQuery()
  },
)

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

/**
 * 分段在「当前查看日期」内的可见区间。
 *
 * 跨午夜的录像段（如 10-06 23:59:02 → 10-07 00:04:02）按「与当日有交集」
 * 被后端纳入当天查询，但若直接用它的 start 分组/显示，就会在 10-07 页面里
 * 冒出一条「23:00-23:59」——用户会认为「昨天的时段被错误引入今天」。
 * 这里把区间夹到当日范围内：同一条分段在 10-07 页显示为 00:00:00-00:04:02，
 * 在 10-06 页显示为 23:59:02-24:00:00，两侧都符合直觉。
 * 注意：仅用于显示与分组，跳转仍用分段真实起点（文件本身就是从那里开始的）。
 */
function segDayRange(start: number, end: number): { a: number; b: number } {
  const ds = dayStart.value
  const de = ds + 86400000
  return { a: Math.max(start, ds), b: Math.min(end, de) }
}

const halfGroups = computed<HalfGroup[]>(() => {
  const byHalf = new Map<string, Map<number, SegGroup>>()
  for (const seg of segments.value) {
    // 按「当日可见区间」的小时分组，跨午夜分段不会落到昨天的小时里
    const { a } = segDayRange(seg.start, seg.end)
    const d = new Date(a)
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
        start: a,
        end: seg.end,
        segs: [],
        durationMin: 0,
      }
      hours.set(h, g)
    }
    g.segs.push(seg)
    g.start = Math.min(g.start, a)
    g.end = Math.max(g.end, seg.end)
    // 时长只计当日可见部分，避免「1 段」却把跨天前的分钟也算进去
    g.durationMin += (segDayRange(seg.start, seg.end).b - a) / 60000
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
  if (!active || !el || !device.value || !segments.value.length) {
    // 没有分段时定位无从谈起：放弃挂起的跳转请求
    pendingSeekTs = 0
    return
  }
  let start: number
  if (pendingSeekTs) {
    const snapped = snapToSegment(pendingSeekTs)
    if (snapped !== pendingSeekTs) showToast('所选时间无录像，已定位到最近录像')
    start = snapped
    pendingSeekTs = 0
  } else {
    start = earliestInterestingStart()
  }
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

// 分段下载：先探一下文件是否还在（此前「空目录误删」会让 DB 里的段没有
// 对应文件，下载表现为「点了没反应」），确认可用后交给浏览器原生下载
// （流式写盘，不占内存）。每一步都有提示，不再静默失败。
const dlBusyId = ref('')
async function downloadSegment(s: RecordingSegment) {
  if (!deviceId.value || dlBusyId.value) return
  const d = new Date(s.start)
  const p2 = (n: number) => String(n).padStart(2, '0')
  const date = `${d.getFullYear()}-${p2(d.getMonth() + 1)}-${p2(d.getDate())}`
  const time = `${p2(d.getHours())}${p2(d.getMinutes())}${p2(d.getSeconds())}`
  const url = downloadRecordingURL(deviceId.value, date, time)
  dlBusyId.value = s.id
  try {
    // Range: bytes=0-0 只探可用性；无论服务器回 206 还是整段 200，读完首块即断开
    const res = await fetch(url, { headers: { Range: 'bytes=0-0' } })
    if (!res.ok) {
      if (res.status === 404) showToast('该段录像文件已不存在（可能已被清理）')
      else if (res.status === 401) showToast('登录已过期，请重新登录后再下载')
      else showToast(`下载失败（HTTP ${res.status}）`)
      return
    }
    res.body?.cancel().catch(() => {})
    const a = document.createElement('a')
    a.href = url
    a.download = `${device.value?.name || 'recording'}_${date}_${time}.mp4`
    a.rel = 'noopener'
    a.click()
    showToast('已开始下载录像文件')
  } catch {
    showToast('下载失败，请检查网络后重试')
  } finally {
    dlBusyId.value = ''
  }
}

function onSelectDate(date: string) {
  datePicked = true
  dateStr.value = date
}

function shiftDay(delta: number) {
  datePicked = true
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
  // 事件页/外部深链：?device=&date=&t= 直接定位到对应时刻
  consumeSeekQuery()
  if (isBackend() && !isDemoMode()) startExportPoll()
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
  marks.value = []
  loadMonth()
  loadSegments()
})
watch(ym, loadMonth)
watch(dateStr, () => {
  marks.value = []
  loadSegments()
})
onDeactivated(() => {
  active = false
  loadToken++
  monthToken++
  releasePlayback()
  if (timer) window.clearInterval(timer)
  timer = null
  stopExportPoll()
})
onActivated(() => {
  if (active) {
    // 页面还活着时带着新的跳转参数回来（事件页跳转）：消费并按需重载
    if (consumeSeekQuery()) loadMonth()
    startExportPoll()
    startTimer()
    return
  }
  active = true
  // 跨零点：keep-alive 页面不会自己刷新日期，此前凌晨重新打开时仍停在昨天的
  // 日期与昨天的段数上（会被误认为「自动回落昨天」）。用户没主动选过日期时
  // 激活即对齐到当天；主动选过则尊重用户选择（可能就是要看昨天）。
  // 事件页/深链跳转优先：?date=&t= 未消费则按参数定位（datePicked 会被置真）
  const consumed = consumeSeekQuery()
  const dayChanged = !consumed && !datePicked && dateStr.value !== todayLocal()
  if (dayChanged) dateStr.value = todayLocal()
  const q = route.query.device as string | undefined
  if (q && q !== deviceId.value && store.byId(q)) deviceId.value = q
  else if (!consumed && !dayChanged) { loadMonth(); loadSegments() }
  startExportPoll()
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
  stopExportPoll()
})
</script>

<template>
  <div class="page playback-page">
    <van-nav-bar title="录像管理">
      <template #right>
        <button type="button" class="device-picker control-button" aria-label="选择回放设备" @click="pickDevice">
          <span class="dp-name">{{ device?.name ?? '选择设备' }}</span>
          <van-icon name="arrow-down" size="12" />
        </button>
      </template>
    </van-nav-bar>

    <div class="pb-tabs" role="tablist">
      <button type="button" class="pb-tab" :class="{ on: activeTab === 'playback' }" role="tab" :aria-selected="activeTab === 'playback'" @click="activeTab = 'playback'">回放</button>
      <button type="button" class="pb-tab" :class="{ on: activeTab === 'events' }" role="tab" :aria-selected="activeTab === 'events'" @click="activeTab = 'events'">事件</button>
      <!-- 移动端日期与日历入口合并为同一控件：点击日期即弹出月历（原独立日历按钮间隔过大） -->
      <PlaybackDateBar
        class="pb-tab-date"
        :date="dateStr"
        :compact="true"
        :show-calendar-button="false"
        @prev="shiftDay(-1)"
        @next="shiftDay(1)"
        @pick-calendar="calOpen = true"
      />
      <!-- 独立导出入口：自定义起止时间后台拼接；徽标 = 进行中的导出任务数 -->
      <button
        v-if="isBackend() && !isDemoMode()"
        type="button"
        class="export-btn control-button"
        :class="{ live: runningExports > 0 }"
        aria-label="导出录像"
        @click="openExportDialog"
      >
        <van-badge :content="runningExports" :show-zero="false" max="9+">
          <van-icon name="down" />
        </van-badge>
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
          <!-- 移动端隐藏整行：日期已在页签行、段数/时长在下方分段工具行，
               播放状态在播放器控制条上，此处属重复信息（用户要求删除）。
               桌面端保留（左栏较宽，信息行有价值）。 -->
          <div class="tl-head">
            <span class="mono">{{ dateStr }} 录像</span>
            <span class="play-status" :class="{ on: playing }">{{ playing ? '播放中' : '已暂停' }}</span>
            <!-- 段数/时长/事件数在移动端与下方分段工具行重复，<1200px 隐藏（见 .tl-stats） -->
            <span class="tl-stats">{{ segments.length }} 段 · 共 {{ totalMinutes }} 分钟 · {{ dayEvents.length }} 事件</span>
          </div>
          <div class="tl-row">
            <div class="tl-main">
              <TimelineBar
                :day-start="dayStart"
                :segments="segments"
                :events="timelineEvents"
                :value="currentTs"
                :marks="marks"
                @seek="onSeek"
                @seekend="onSeekEnd"
              />
            </div>
            <button
              v-if="isBackend() && !isDemoMode()"
              type="button"
              class="mark-btn control-button"
              :class="{ on: markBtnOn }"
              :title="markBtnOn ? '取消标记当前时间点' : '标记当前时间点（导出时可快速选用）'"
              :aria-label="markBtnOn ? '取消标记当前时间点' : '标记当前时间点'"
              @click="toggleMark"
            >
              <van-icon :name="markBtnOn ? 'bookmark' : 'bookmark-o'" />
            </button>
          </div>
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
              当日 {{ segments.length }} 段 · {{ fmtDur(totalMinutes) }} · {{ dayEvents.length }} 事件
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
                    <button class="seg-time mono control-button" @click="onSelectSegment(s)">{{ fmtRange(segDayRange(s.start, s.end).a, segDayRange(s.start, s.end).b) }}</button>
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
              <button class="seg-time mono control-button" @click="onSelectSegment(s)">{{ fmtRange(segDayRange(s.start, s.end).a, segDayRange(s.start, s.end).b) }}</button>
              <van-icon v-if="isBackend() && !isDemoMode()" name="down" class="seg-dl" @click.stop="downloadSegment(s)" />
            </span>
          </div>

          <span v-if="!segments.length" class="none">当日无录制</span>
        </div>
      </div>
    </div>

    <!-- 导出：自定义起止时间（滚轮快选 / 标记快填）+ 任务列表 -->
    <van-popup v-model:show="exportOpen" position="bottom" round :style="{ maxHeight: '88%' }">
      <div class="exp-head">
        <span class="exp-title">导出录像</span>
        <span class="exp-date mono">{{ dateStr }}</span>
      </div>
      <div class="exp-body">
        <button type="button" class="exp-row control-button" @click="openTimePick('start')">
          <span class="exp-label">起始时间</span>
          <span class="exp-val mono">{{ fmtSec(exportStart) }} <van-icon name="arrow" /></span>
        </button>
        <button type="button" class="exp-row control-button" @click="openTimePick('end')">
          <span class="exp-label">截止时间</span>
          <span class="exp-val mono">{{ fmtSec(exportEnd) }} <van-icon name="arrow" /></span>
        </button>

        <div v-if="marks.length" class="exp-marks">
          <div class="exp-marks-hint">{{ markStage === 1 ? '已选起点：再点一个标记作为终点' : '点标记快速填充（先起点后终点）' }}</div>
          <div class="exp-mark-chips">
            <button
              v-for="(m, i) in marks" :key="i" type="button"
              class="mark-chip mono"
              :class="{ sel: markStage === 1 && secOfDay(m) === exportStart }"
              @click="pickMark(m)"
            >{{ fmtHM(m) }}</button>
          </div>
        </div>

        <div v-if="exports.length" class="exp-tasks">
          <div class="exp-tasks-title">导出任务</div>
          <div v-for="t in exports" :key="t.id" class="exp-task">
            <div class="exp-task-main">
              <span class="exp-task-name">{{ t.deviceName }} {{ fmtHM(new Date(t.start).getTime()) }}-{{ fmtHM(new Date(t.end).getTime()) }}</span>
              <span class="exp-task-status" :class="t.status">{{ exportStatusText(t) }}<template v-if="t.status === 'done' && t.size"> · {{ fmtSizeMB(t.size) }}</template></span>
            </div>
            <div class="exp-task-ops">
              <a v-if="t.status === 'done'" :href="exportFileURL(t.id)" class="exp-dl control-button" title="下载导出文件"><van-icon name="down" /></a>
              <button class="exp-del control-button" aria-label="删除导出任务" @click="removeExport(t)"><van-icon name="delete-o" /></button>
            </div>
          </div>
        </div>

        <van-button type="primary" block :loading="creating" :disabled="exportEnd <= exportStart" class="exp-confirm" @click="confirmExport">
          确定导出
        </van-button>
      </div>
    </van-popup>

    <!-- 起止时间滚轮选择 -->
    <van-popup v-model:show="timePickOpen" position="bottom" round>
      <van-picker
        v-picker-desktop
        :columns="timeColumns"
        v-model="timePickValues"
        title="选择时间点"
        @confirm="onTimePickConfirm"
        @cancel="timePickOpen = false"
      />
    </van-popup>

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
  min-width: 0;
}
/* 设备名过长时省略：完整名称在「选择设备」弹层里可见 */
.device-picker .dp-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
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
/* 时间轴 + 标记按钮：按钮固定在时间轴右侧，时间轴让位 */
.tl-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 14px;
}
.tl-main {
  flex: 1;
  min-width: 0;
}
.mark-btn {
  flex: 0 0 auto;
  width: calc(34px * var(--nvr-font-scale, 1));
  height: calc(34px * var(--nvr-font-scale, 1));
  border-radius: var(--nvr-radius-full);
  border: 1px solid var(--nvr-border);
  background: var(--nvr-panel-2);
  color: var(--nvr-text-2);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: calc(16px * var(--nvr-font-scale, 1));
}
.mark-btn.on {
  color: #ffd447;
  border-color: rgba(255, 212, 71, 0.6);
  background: rgba(255, 212, 71, 0.12);
}
/* 独立导出按钮（日期条右侧）：有进行中任务时徽标计数 + 呼吸提示 */
.export-btn {
  margin-left: auto;
  flex: 0 0 auto;
  min-width: calc(34px * var(--nvr-font-scale, 1));
  height: calc(32px * var(--nvr-font-scale, 1));
  padding: 0 calc(10px * var(--nvr-font-scale, 1));
  border-radius: var(--nvr-radius-full);
  border: 1px solid var(--nvr-border);
  background: var(--nvr-panel);
  color: var(--nvr-text-2);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: calc(16px * var(--nvr-font-scale, 1));
}
.export-btn.live {
  color: var(--nvr-accent);
  border-color: rgba(46, 168, 255, 0.55);
  animation: export-pulse 1.6s ease-in-out infinite;
}
@keyframes export-pulse {
  0%, 100% { box-shadow: 0 0 0 0 rgba(46, 168, 255, 0.35); }
  50% { box-shadow: 0 0 0 6px rgba(46, 168, 255, 0); }
}
/* 导出弹窗 */
.exp-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 16px 6px;
}
.exp-title {
  font-weight: 700;
  font-size: calc(16px * var(--nvr-font-scale, 1));
}
.exp-date {
  color: var(--nvr-text-2);
  font-size: calc(13px * var(--nvr-font-scale, 1));
}
.exp-body {
  padding: 4px 16px 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  overflow-y: auto;
}
.exp-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  padding: calc(12px * var(--nvr-font-scale, 1)) calc(14px * var(--nvr-font-scale, 1));
  border-radius: var(--nvr-radius-sm);
  border: 1px solid var(--nvr-border);
  background: var(--nvr-panel-2);
  font-size: calc(14px * var(--nvr-font-scale, 1));
}
.exp-label {
  color: var(--nvr-text-2);
}
.exp-val {
  color: var(--nvr-text);
}
.exp-marks-hint {
  font-size: calc(12px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
  margin-bottom: 6px;
}
.exp-mark-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.mark-chip {
  padding: calc(6px * var(--nvr-font-scale, 1)) calc(12px * var(--nvr-font-scale, 1));
  border-radius: var(--nvr-radius-full);
  border: 1px solid var(--nvr-border);
  background: var(--nvr-panel-2);
  color: var(--nvr-text-2);
  font-size: calc(13px * var(--nvr-font-scale, 1));
  cursor: pointer;
}
.mark-chip.sel {
  border-color: var(--nvr-accent);
  color: var(--nvr-accent);
  background: rgba(46, 168, 255, 0.12);
}
.exp-tasks-title {
  font-size: calc(13px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
  margin-bottom: 4px;
}
.exp-task {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 8px 0;
  border-bottom: 1px solid var(--nvr-border);
  font-size: calc(13px * var(--nvr-font-scale, 1));
}
.exp-task-main {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.exp-task-name {
  color: var(--nvr-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.exp-task-status {
  color: var(--nvr-text-2);
  font-size: calc(12px * var(--nvr-font-scale, 1));
}
.exp-task-status.done { color: #2ecc8f; }
.exp-task-status.error { color: #ff4d4f; }
.exp-task-ops {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}
.exp-confirm {
  margin-top: 6px;
  font-size: calc(15px * var(--nvr-font-scale, 1));
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
/* 大字下统计行允许换行（单行省略会把新增的「事件数」裁掉） */
:global(body.care) .seg-count,
:global(html[data-font-size='xlarge']) .seg-count {
  white-space: normal;
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
  /* 移动端按内容高度占位（flex: none），不与下方列表平分剩余高度。
     此前 flex:1 会与 .pb-side 各分一半高度，而「播放区 + 时间轴」的实际高度
     大于分到的份额（实测 350px vs 300px），多出的部分向下溢出，
     把列表顶部压在时间轴标签上（实测重叠 20–50px）。
     桌面端（≥1200px）在下方媒体查询里恢复 flex:1 的左右分栏。 */
  flex: 0 0 auto;
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
  /* 段数/时长/事件数改在下方分段工具行展示，时间轴上方信息行不再重复 */
  .tl-stats {
    display: none;
  }
  /* 「日期 录像 + 播放中」整行在移动端全部删除（用户要求）：
     日期在页签行、播放状态在控制条、段数在下方工具行，均属重复信息。
     整行隐藏后时间轴直接紧贴播放器下方。 */
  .timeline-wrap .tl-head {
    display: none;
  }
  /* 大字（关怀模式 / 特大字体）下固定区可能吃掉几乎全部竖向空间：
     实测 320×568 关怀模式下列表区仅剩 8px，时段列表上滑不到、看不到上午。
     此时改为一整列外层滚动：列表按内容展开、不再内滚，保证每个小时段都能滚到。
     注意：:global() 必须包住完整选择器，且不要写成逗号分组——
     `:global(body.care) .pb-side, :global(html[...]) .pb-side` 会被编译成
     `body.care,html[...] .pb-side[data-v-...]` 这种错误分组，两条规则全部失效。

     但「整列滚动」会让播放器随内容一起滚出视口——用户反馈「播放组件固定
     失效」。因此在同一滚动容器里把固定区改为 sticky 钉在顶部：
     列表照常按内容展开（可达性不丢），播放器依旧常驻可见（固定性不丢）。 */
  :global(body.care .pb-side) {
    flex: 0 0 auto;
    min-height: 180px;
    overflow: visible;
  }
  :global(body.care .pb-body) {
    overflow-y: auto;
  }
  :global(body.care .pb-main) {
    position: sticky;
    top: 0;
    z-index: 6;
    /* 不透明背景：否则下方列表滚动时会从固定区底下透出来 */
    background: var(--nvr-bg);
    box-shadow: 0 6px 12px -8px rgba(0, 0, 0, 0.55);
  }
  :global(html[data-font-size='xlarge'] .pb-side) {
    flex: 0 0 auto;
    min-height: 180px;
    overflow: visible;
  }
  :global(html[data-font-size='xlarge'] .pb-body) {
    overflow-y: auto;
  }
  :global(html[data-font-size='xlarge'] .pb-main) {
    position: sticky;
    top: 0;
    z-index: 6;
    /* 不透明背景：否则下方列表滚动时会从固定区底下透出来 */
    background: var(--nvr-bg);
    box-shadow: 0 6px 12px -8px rgba(0, 0, 0, 0.55);
  }
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
    /* flex:1 让日期条吃掉页签行的剩余宽度——否则它是 flex:0 1 auto（按内容宽），
       clientWidth 实测仅 123px，连 320px 窄屏和 834px 宽屏都放不下「2026-10-07」，
       年份永远显示不出来。配合组件内的实际文本测量即可做到：
       空间够显示年份、不够压缩为月日。 */
    flex: 1 1 auto;
    justify-content: center;
    min-width: 0;
    /* 组件默认 gap 22px：窄屏收窄但保留可点按间距。
       此前 6px 过挤（日期与左右箭头几乎贴住），10px 兼顾可点性又不挤掉年份。 */
    gap: 10px;
  }
  .pb-tab-date :deep(.date) {
    font-size: calc(13px * var(--nvr-font-scale, 1));
    white-space: nowrap;
  }
  .pb-tab-date :deep(.control-button) {
    min-width: 30px;
    min-height: 34px;
  }
  .pb-tab {
    padding: 7px 14px;
  }
  :global(body.care .pb-tab) {
    padding: 8px 14px;
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
    /* 62vh 之外再按「视口 - 固定占位」封顶：播放器+时间轴+导航+页签+底部导航
       约占 500px，若播放器仍按 62vh 取值，平板竖屏（如 900×700）下固定区
       会占满整屏、下方列表仅剩十几像素。这里为列表保底约 200px 可视高度。 */
    height: clamp(176px, 56.25vw, min(62vh, calc(100dvh - 500px)));
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
  /* 固定区 + 列表在矮屏已放不下：整列改为滚动（原保底高度仍会溢出被裁），
     保证列表内容可达。 */
  .pb-body {
    overflow-y: auto;
  }
  .pb-main {
    flex: 0 0 auto;
  }
  .pb-side {
    flex: 0 0 auto;
    min-height: 0;
    overflow: visible;
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

/* 超窄屏（≤360px）：进一步压缩页签与日期组，避免横向溢出 */
@media (max-width: 360px) {
  .pb-tabs {
    padding: 10px 12px 6px;
    gap: 4px;
  }
  .pb-tab {
    padding: 7px 12px;
  }
  .pb-tab-date :deep(.control-button) {
    min-width: 28px;
  }
}

/* ── 关怀模式：字号统一上调 ──
   本页自绘元素多为 calc(Npx * --nvr-font-scale)，而关怀模式下该变量恒为 1，
   打开关怀模式后仍是 11–13px，与 Vant 组件（16–20px）不协调、对适老场景过小。
   这里按信息层级整体上调：主文字 15–16px，辅助信息 14px。 */
:global(body.care .playback-page .device-picker) {
  font-size: calc(16px * var(--nvr-font-scale, 1));
}
/* Vant 导航栏标题默认居中（max-width:60%），右侧设备名为绝对定位：
   关怀模式大字号下两者必然重叠（实测 320px「录像管理」压住「摄像头」）。
   窄屏下改为标题左对齐、设备名右对齐的标准 action bar 布局，并限制设备名宽度。 */
@media (max-width: 600px) {
  /* :global(...) 与 :deep(...) 组合会被编译丢规则（实测未产出任何 CSS），
     这里用完整的全局选择器；.van-nav-bar__title 由 Vant 渲染、本就不带
     scoped 属性，无需 :deep。 */
  :global(body.care .playback-page .van-nav-bar__title) {
    margin: 0 0 0 14px;
    max-width: 46%;
    text-align: left;
  }
  :global(body.care .playback-page .device-picker) {
    max-width: 44vw;
  }
}
:global(body.care .playback-page .pb-tab) {
  font-size: calc(16px * var(--nvr-font-scale, 1));
}
:global(body.care .playback-page .pb-tab-date .date) {
  font-size: calc(16px * var(--nvr-font-scale, 1));
}
:global(body.care .playback-page .tl-head),
:global(body.care .playback-page .play-status),
:global(body.care .playback-page .seg-toolbar),
:global(body.care .playback-page .seg-mode),
:global(body.care .playback-page .hour-head) {
  font-size: calc(15px * var(--nvr-font-scale, 1));
}
:global(body.care .playback-page .half-title),
:global(body.care .playback-page .seg-item) {
  font-size: calc(16px * var(--nvr-font-scale, 1));
}
:global(body.care .playback-page .half-meta),
:global(body.care .playback-page .hour-meta),
:global(body.care .playback-page .evt-badge),
:global(body.care .playback-page .none) {
  font-size: calc(14px * var(--nvr-font-scale, 1));
}
</style>
