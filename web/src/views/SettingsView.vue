<script setup lang="ts">
import { computed, nextTick, onActivated, onDeactivated, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { showConfirmDialog, showToast } from 'vant'
import type { RecordMode } from '../types'
import { useSettingsStore } from '../stores/settings'
import { useAuthStore } from '../stores/auth'
import { http } from '../api/client'
import {
  createUser,
  deleteUser,
  fetchStatus,
  fetchUsers,
  isBackend,
  updateUser,
  type ManagedUser,
  type StatusInfo,
} from '../api'

const store = useSettingsStore()
const auth = useAuthStore()
const router = useRouter()
const buildId = __APP_BUILD_ID__

/* ── 设置分组折叠状态 ──
   高频分组默认展开，低频分组默认收起，减少首屏认知负荷。
   状态持久化到 localStorage，跨会话保持。
   解析必须兜底：旧版本残留/手动篡改的非合法 JSON 会让 JSON.parse 抛异常，
   导致整个设置页白屏；解析失败时回退到默认收起集。 */
const DEFAULT_COLLAPSED: string[] = ['server', 'ai', 'users']
function loadCollapsed(): Set<string> {
  try {
    const raw = localStorage.getItem('nvr_settings_collapsed')
    if (!raw) return new Set(DEFAULT_COLLAPSED)
    const parsed = JSON.parse(raw)
    if (Array.isArray(parsed) && parsed.every((k) => typeof k === 'string')) {
      return new Set(parsed)
    }
  } catch { /* 非法存储回退默认 */ }
  return new Set(DEFAULT_COLLAPSED)
}
const collapsedGroups = ref<Set<string>>(loadCollapsed())
function toggleGroup(key: string) {
  if (collapsedGroups.value.has(key)) {
    collapsedGroups.value.delete(key)
  } else {
    collapsedGroups.value.add(key)
  }
  localStorage.setItem('nvr_settings_collapsed', JSON.stringify([...collapsedGroups.value]))
}
function isCollapsed(key: string) {
  return collapsedGroups.value.has(key)
}

/* ── 桌面端左侧分区导航（>=900px）──
   宽屏下设置项平铺成「卡片流」需要不断滚动才能找目标分组；
   改为「左侧固定分区导航 + 右侧扁平表单区」：一次点击直达分组，
   当前分组高亮。窄屏（<1200px）完全走原有单列布局，导航不渲染。 */
const isDesktopSettings = ref(false)
const activeSection = ref('display')
const pageRef = ref<HTMLElement | null>(null)
let mqSettings: MediaQueryList | null = null
function onSettingsMqChange(e: MediaQueryListEvent | MediaQueryList) {
  isDesktopSettings.value = e.matches
}
// 分组键与模板里 set-block 的 key 一一对应；可见性条件必须与模板保持一致，
// 否则导航会出现「点了没反应」的死项（如非管理员的登录安全/用户管理）。
const navGroups = computed<Array<{ key: string; label: string; icon: string }>>(() => {
  // 权限分级：
  //   - display（显示与无障碍）是个人化设置，所有登录用户可见。
  //   - notify/server/storage/record/ai 是全局管理配置，仅 operator/admin
  //     可见（canEdit 已排除 viewer 与 user）。
  //   - security/users 仅 admin。
  // 只读账号（viewer）此前能看到并浏览这些管理区块（虽不能保存），
  // 属于「设置权限过大」，此处收紧为只看个人化项。
  const items = [
    { key: 'display', label: '显示与无障碍', icon: 'eye-o' },
  ]
  if (auth.canEdit) {
    items.push(
      { key: 'notify', label: '通知', icon: 'bell' },
      { key: 'server', label: '服务器', icon: 'desktop-o' },
    )
  }
  if (auth.isAdmin) items.push({ key: 'security', label: '登录安全', icon: 'shield-o' })
  if (isBackend() && auth.isAdmin) items.push({ key: 'users', label: '用户管理', icon: 'friends-o' })
  if (auth.canEdit) {
    items.push(
      { key: 'storage', label: '存储管理', icon: 'cluster-o' },
      { key: 'record', label: '录像策略', icon: 'video-o' },
      { key: 'ai', label: 'AI 画面识别', icon: 'photo-o' },
    )
  }
  return items
})
function persistCollapsed() {
  localStorage.setItem('nvr_settings_collapsed', JSON.stringify([...collapsedGroups.value]))
}
function goToSection(key: string) {
  if (collapsedGroups.value.has(key)) {
    collapsedGroups.value.delete(key)
    persistCollapsed()
  }
  activeSection.value = key
  // 展开是 v-show 切换，等一帧再测量位置，否则滚动到折叠前的高度
  nextTick(() => {
    const el = document.getElementById('sec-' + key)
    const container = pageRef.value
    if (!el || !container) return
    // 用「两者视口顶边之差 + 当前滚动量」换算目标滚动位置。
    // 不用 el.offsetTop - container.offsetTop：offsetTop 是相对 offsetParent 的，
    // 而本页的 offsetParent 实际是 .app-shell（不是滚动容器），两者坐标系不同，
    // 只是当前布局恰好让 .app-shell 顶边与容器顶边重合才没出错。
    // 另外滚动会被容器高度钳制：目标分组靠页面底部时无法滚到顶边，
    // 此时浏览器停在最大滚动量，属于正确行为（由界面测试断言「可见」而非「贴顶」）。
    const delta = el.getBoundingClientRect().top - container.getBoundingClientRect().top
    container.scrollTo({ top: container.scrollTop + delta, behavior: 'smooth' })
  })
}
// 滚动时高亮「最靠近顶部」的分组，让导航始终反映当前阅读位置
function onSettingsScroll() {
  if (!isDesktopSettings.value) return
  const container = pageRef.value
  if (!container) return
  const base = container.getBoundingClientRect().top
  let current = navGroups.value[0]?.key ?? 'display'
  for (const g of navGroups.value) {
    const el = document.getElementById('sec-' + g.key)
    if (!el) continue
    if (el.getBoundingClientRect().top - base <= 32) current = g.key
  }
  activeSection.value = current
}

// entering from the tabbar there may be no history to go back to
function goBack() {
  if (window.history.state.back == null) router.replace('/live')
  else router.back()
}
const s = store.settings

const saving = ref(false)

// ---- 循环覆盖：保留天数（1-3650）与总容量限额（GB，0 = 不限制） ----
const retentionDaysText = ref(String(s.retentionDays || 30))
const retentionSizeText = ref(String(s.retentionSizeGB ?? 0))
watch(() => s.retentionDays, value => { retentionDaysText.value = String(value) })
watch(() => s.retentionSizeGB, value => { retentionSizeText.value = String(value ?? 0) })
const retentionPresets = [30, 90, 180, 365, 730, 1825, 3650]
const showDaysPicker = ref(false)
const daysColumns = retentionPresets.map((d) => ({ text: `${d} 天`, value: String(d) }))

function onDaysConfirm({ selectedValues }: { selectedValues: string[] }) {
  retentionDaysText.value = selectedValues[0]
  applyRetentionDays()
  showDaysPicker.value = false
}

function applyRetentionDays() {
  let v = Math.round(Number(retentionDaysText.value) || 0)
  if (v < 1) v = 1
  if (v > 3650) v = 3650
  retentionDaysText.value = String(v)
  store.set({ retentionDays: v })
}

function applyRetentionSize() {
  let v = Math.round(Number(retentionSizeText.value) || 0)
  if (v < 0) v = 0
  if (v > 1048576) v = 1048576
  retentionSizeText.value = String(v)
  store.set({ retentionSizeGB: v })
}

const showStartPicker = ref(false)
const showEndPicker = ref(false)
const showAIModePicker = ref(false)

const aiModeOptions = [
  { text: '本地对象检测', value: 'local' },
  { text: '云端视觉模型', value: 'openai' },
]

function onAIModeConfirm({ selectedValues }: any) {
  store.set({ ai: { ...s.ai, mode: selectedValues[0] } })
  showAIModePicker.value = false
}

// 让用户明确当前检测服务是"本机进程间通信"还是"网络调用"
const aiTransportHint = computed(() => {
  const url = s.ai.detectUrl || ''
  if (url.startsWith('unix:')) {
    return 'Unix Socket 本机进程间通信：不经网络协议栈、不占端口、不对外暴露'
  }
  if (url.startsWith('http://127.0.0.1') || url.startsWith('http://localhost')) {
    return '本机 TCP 回环（127.0.0.1），数据不出本机'
  }
  return '远程检测服务（http://host:port），请确认网络与鉴权'
})

const usedPct = computed(() => {
  const { totalGB, usedGB } = store.storage
  if (!totalGB) return 0
  return Math.round((usedGB / totalGB) * 100)
})

// ---- 运行状态：资源占用 / 进程概况（点开才拉取，打开期间每 5 秒刷新） ----
const status = ref<StatusInfo | null>(null)
const statusLoading = ref(false)
const statusErr = ref('')
const showStatus = ref(false)
let statusTimer: number | undefined

async function loadStatus() {
  if (!isBackend()) return
  statusLoading.value = true
  statusErr.value = ''
  try {
    status.value = await fetchStatus()
  } catch (e: any) {
    statusErr.value = e?.response?.data?.error || '读取失败'
  } finally {
    statusLoading.value = false
  }
}

/* 弹窗代次：打开时 +1，关闭/停用时 +1 使在途的打开流程失效。
   此前 openStatus 先 await 两次请求、最后才注册轮询——若用户在这 1.2 秒内
   关闭弹窗，异步流程仍会在「已关闭」状态下把 5 秒轮询装上，导致弹窗关着
   却持续请求 /api/status。 */
let statusEpoch = 0

// CPU 是「两次采样之间的占用」，单次调用拿不到，因此连采两次
async function openStatus() {
  const epoch = ++statusEpoch
  showStatus.value = true
  await loadStatus()
  if (epoch !== statusEpoch || !showStatus.value) return
  await new Promise((r) => setTimeout(r, 1200))
  if (epoch !== statusEpoch || !showStatus.value) return
  await loadStatus()
  if (epoch !== statusEpoch || !showStatus.value) return
  closeStatusTimer()
  statusTimer = window.setInterval(loadStatus, 5000)
}
function closeStatusTimer() {
  // 让在途的 openStatus 失效
  statusEpoch++
  if (statusTimer) {
    window.clearInterval(statusTimer)
    statusTimer = undefined
  }
}

const fmtUptime = (sec?: number) => {
  if (sec === undefined || sec === null) return '—'
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  if (d > 0) return `${d} 天 ${h} 小时`
  if (h > 0) return `${h} 小时 ${m} 分`
  return `${m} 分`
}
onUnmounted(closeStatusTimer)

const appVersion = ref('')

// 版本自检：底部展示服务端版本。看不到这一行 = 页面还是旧缓存。
async function loadAboutVersion() {
  try {
    const { data } = await http.get('/api/health')
    appVersion.value = data.version || ''
  } catch {
    appVersion.value = ''
  }
}

// ---- 检查更新 ----
// 两种环境：
//   · App 内嵌 WebView：检查 App 自身版本（走原生桥 CyanNVRApp.checkAppUpdate）。
//     App 版本与服务器版本是两条线——此前误用服务器版本比对，导致
//     「App 明明有新版却一直显示最新」。
//   · 浏览器 / fnOS 桌面：检查服务器版本（后端查 GitHub release），
//     发现新版跳转 release 页面自行下载。
const checkingUpdate = ref(false)
const updateState = ref<null | {
  hasUpdate: boolean
  latest: string
  current?: string
  url?: string
  inApp?: boolean
  error?: string
}>(null)

function appBridge(): any {
  return (window as any).CyanNVRApp ?? null
}

async function checkForUpdate() {
  checkingUpdate.value = true
  updateState.value = null
  try {
    const bridge = appBridge()
    if (bridge?.checkAppUpdate) {
      const info = JSON.parse(bridge.checkAppUpdate())
      updateState.value = {
        hasUpdate: !!info.hasUpdate,
        latest: info.latest || '',
        current: info.current || '',
        inApp: true,
      }
      return
    }
    const { data } = await http.get('/api/update/check')
    updateState.value = data.update
  } catch {
    updateState.value = { hasUpdate: false, latest: '', error: '检查更新失败，请稍后再试' }
  } finally {
    checkingUpdate.value = false
  }
}

function openRelease() {
  if (updateState.value?.url) {
    window.open(updateState.value.url, '_blank', 'noopener')
  }
}

/** App 环境：走原生下载 + 系统安装器 */
function installAppUpdate() {
  const bridge = appBridge()
  if (bridge?.installAppUpdate) bridge.installAppUpdate()
}

onMounted(() => {
  // 设置加载完成后才能做「自动补默认模型」这类写操作：
  // 写操作会整份 PUT，必须建立在服务端真实值之上，否则会用前端默认值
  // 覆盖用户已保存的设置（AI 开关被改回关闭即由此引起）。
  store.loadFromServer().then(() => {
    initTrustHours()
  })
  loadUsers()
  loadAboutVersion()
  initTrustHours()
  // 桌面分区导航：>=1200px 才启用（与 CSS 断点一致，避免「导航在但布局没切」）
  mqSettings = window.matchMedia('(min-width: 900px)')
  onSettingsMqChange(mqSettings)
  mqSettings.addEventListener('change', onSettingsMqChange)
})

onUnmounted(() => {
  mqSettings?.removeEventListener('change', onSettingsMqChange)
  mqSettings = null
  closeStatusTimer()
})

/* KeepAlive：设置页被缓存时 onUnmounted 不会触发，
   若此时「运行状态」弹窗仍开着，5 秒轮询会一直空转。
   停用时停止轮询；再次激活且弹窗仍开着则恢复。 */
onDeactivated(() => {
  closeStatusTimer()
})
onActivated(() => {
  void store.loadFromServer().then(initTrustHours)
  if (showStatus.value) {
    closeStatusTimer()
    statusTimer = window.setInterval(loadStatus, 5000)
  }
})

const modeOptions = [
  { label: '连续录制', desc: '全天不间断录像', value: 'continuous' },
  { label: '移动侦测', desc: '检测到移动才录像', value: 'motion' },
  { label: '定时录制', desc: '仅在设定时间段录像', value: 'schedule' },
] as const

function timeToColumns(t: string): string[] {
  return [t.slice(0, 2), t.slice(3, 5)]
}
function columnsToTime(c: string[]): string {
  return `${c[0]}:${c[1]}`
}
function onStartConfirm({ selectedValues }: { selectedValues: string[] }) {
  store.set({ scheduleStart: columnsToTime(selectedValues) })
  showStartPicker.value = false
}
function onEndConfirm({ selectedValues }: { selectedValues: string[] }) {
  store.set({ scheduleEnd: columnsToTime(selectedValues) })
  showEndPicker.value = false
}

// 保存设置（不含密码：改密码统一走「用户管理 → 编辑用户」）
async function save() {
  saving.value = true
  try {
    await store.save()
    showToast('设置已保存')
  } catch (e: any) {
    showToast(e?.response?.data?.error || '保存失败')
  } finally {
    saving.value = false
  }
}

function logout() {
  auth.logout()
  location.hash = '#/login'
}

function setMode(m: RecordMode) {
  store.set({ recordMode: m })
}

const themeOptions = [
  { value: 'auto', text: '跟随系统' },
  { value: 'light', text: '浅色' },
  { value: 'dark', text: '深色' },
] as const
function setTheme(v: 'auto' | 'dark' | 'light') {
  store.set({ theme: v })
}

const fontSizeOptions = [
  { label: '标准', value: 'normal', size: 14 },
  { label: '大字', value: 'large', size: 17 },
  { label: '特大', value: 'xlarge', size: 20 },
] as const

function setFontSize(v: 'normal' | 'large' | 'xlarge') {
  store.set({ fontSize: v })
}

function setCareMode(v: boolean) {
  // 统一走 store action；样式应用由 App.vue 的 watch(applyA11y) 负责
  store.set({ careMode: v })
}

function setDemoMode(v: boolean) {
  store.set({ demoMode: v })
}


// ---- AI model hot-swap & 推理后端 ----
interface AIModelInfo { path: string; name: string; size_mb: number; active: boolean }
interface AICatalogItem { id: string; file: string; desc: string; url: string; approx_mb: number }
interface AIInfoPayload {
  backend?: string
  backend_chain?: string[]
  backend_fallback?: string
  backend_bench?: string
  capability?: {
    tier: string
    mean_ms: number
    peak_ms: number
    effective_ms: number
    max_streams: number
    suggest_interval_sec: number
    backend: string
    reason: string
  }
  active?: string
  installed?: AIModelInfo[]
  catalog?: AICatalogItem[]
}

const aiInfo = ref<AIInfoPayload>({})
const aiModels = ref<string[]>([])
const aiBusy = ref('')
const aiError = ref('')
let aiRetryTimer: number | undefined

/** 推理后端手动选择：auto 按可用性挑最快（本机有 CUDA 即 GPU）。 */
const showProviderPicker = ref(false)
const providerColumns = [
  { text: '自动 · 预设（按顺序取最快可用，推荐）', value: 'auto' },
  { text: '自动 · 实测（启动时实测择优）', value: 'auto-bench' },
  { text: 'CPU（软件推理）', value: 'cpu' },
  { text: 'NVIDIA CUDA', value: 'cuda' },
  { text: 'NVIDIA TensorRT（首次较慢）', value: 'tensorrt' },
  { text: 'AMD ROCm', value: 'rocm' },
  { text: 'Intel OpenVINO', value: 'openvino' },
  { text: 'DirectML', value: 'directml' },
]
const providerShort: Record<string, string> = {
  auto: '自动·预设',
  'auto-bench': '自动·实测',
  cpu: 'CPU',
  cuda: 'CUDA',
  tensorrt: 'TensorRT',
  rocm: 'ROCm',
  openvino: 'OpenVINO',
  directml: 'DirectML',
}
const providerLabel = computed(() => {
  const v = s.ai.provider || 'auto'
  return providerShort[v] || providerColumns.find((c) => c.value === v)?.text || v
})
function onProviderConfirm({ selectedValues }: { selectedValues: string[] }) {
  store.set({ ai: { ...s.ai, provider: selectedValues[0] } })
  showProviderPicker.value = false
  showToast('已保存，重启应用后生效')
}

/** 推理后端展示名：把 EP 归一化标识换成用户看得懂的叫法。 */
const backendLabel = computed(() => {
  const map: Record<string, string> = {
    cpu: 'CPU（软件推理）', cuda: 'NVIDIA CUDA', rocm: 'AMD ROCm',
    openvino: 'Intel OpenVINO', directml: 'DirectML', tensorrt: 'NVIDIA TensorRT',
  }
  if (aiError.value && !aiInfo.value.backend) return '检测服务未连接'
  return map[aiInfo.value.backend || ''] || aiInfo.value.backend || '未知'
})

/** 发生了后端回退时给一句人话解释。 */
// 实测结果卡片：把 backend_bench 字符串拆成按名次排列的行
const benchRows = computed(() => {
  const raw = aiInfo.value.backend_bench
  if (!raw) return []
  return raw
    .split(' · ')
    .map((p) => {
      const t = p.trim().split(' ')
      return { provider: t[0], detail: t.slice(1).join(' ') }
    })
    .filter((r) => r.provider)
})

// ---- AI 算力档位 ----
// 由 worker 实测单帧推理延迟推导（不是按 NAS 型号写死映射表）：
// 同型号不同显卡/驱动/散热实测能差数倍，按算力分档才准。
const TIER_LABEL: Record<string, string> = {
  high: '强',
  medium: '中',
  low: '弱',
  minimal: '最低',
}
const cap = computed(() => aiInfo.value.capability || null)
const capTierLabel = computed(() => {
  const t = cap.value?.tier
  return t ? TIER_LABEL[t] || t : ''
})
/** 档位说明：给用户看清楚「为什么是这个档」以及「能带几路」。 */
const capHint = computed(() => {
  const c = cap.value
  if (!c?.tier) return '正在实测本机 AI 算力…'
  return `实测单帧 ${c.mean_ms}ms（峰值 ${c.peak_ms}ms）· 建议最多 ${c.max_streams} 路，间隔 ${c.suggest_interval_sec}s`
})

const backendHint = computed(() => {
  if (aiInfo.value.backend_fallback) return `已回退：${aiInfo.value.backend_fallback}`
  if (aiError.value) return `${aiError.value}（依赖缺失时请安装 opencv-python-headless 与 onnxruntime 后重启应用，页面每 5 秒自动重试）`
  return ''
})

/**
 * 服务端 AI 列表就绪后，若用户还没选过模型，补一个可用默认值。
 *
 * 为什么不能直接在 loadAIModels 里补：loadAIModels 与 loadFromServer 并行，
 * 它可能先返回，此时 store 里还是前端默认值（ai.enabled=false）。一旦在
 * 这个时刻调用 store.set()，store 会对非本地键做整份 PUT，把用户已保存的
 * AI 开关覆盖成关闭——实测打开设置页 +53ms 就发出 PUT enabled=false，
 * 表现为「每次打开 AI 识别都是关闭状态」。
 * 因此这里改为等 store.loaded（服务端设置已合并）之后再补。
 */
// Loading the catalog is read-only. Model selection is persisted only after an explicit user action.

async function loadAIModels() {
  try {
    const { data: j } = await http.get('/api/ai/models')
    aiInfo.value = j
    aiError.value = ''
    aiModels.value = (j.models || []).map((p: string) => p.split(/[\\/]/).pop())
  } catch (e: any) {
    // worker 未就绪（依赖缺失 / 正在启动）：给出原因并自动重试，
    // 启动成功后推理后端会自动刷出来，而不是永远显示「未知」。
    aiInfo.value = {}
    aiError.value = e?.response?.data?.error || '检测服务未连接'
    if (aiRetryTimer) clearTimeout(aiRetryTimer)
    aiRetryTimer = window.setTimeout(() => {
      aiRetryTimer = undefined
      loadAIModels()
    }, 5000)
  }
}

/** 目录里尚未安装的模型，可按需下载，避免把镜像撑大。 */
const downloadable = computed(() => {
  const have = new Set((aiInfo.value.installed || []).map(i => i.name))
  return (aiInfo.value.catalog || []).filter(
    c => !have.has(c.file.replace(/\.onnx$/, ''))
  )
})

async function downloadModel(item: AICatalogItem) {
  if (!item.url) {
    showToast('该模型未内置下载直链，请自行导出 ONNX 后放入模型目录')
    return
  }
  aiBusy.value = item.id
  try {
    const { data: r } = await http.post('/api/ai/download', { name: item.id }, { timeout: 600000 })
    if (r.error) throw new Error(r.error)
    showToast(`${item.id} 下载完成（${r.size_mb} MB）`)
    await loadAIModels()
  } catch (e: any) {
    showToast('下载失败：' + (e?.response?.data?.error || e?.message || '网络错误'))
  } finally {
    aiBusy.value = ''
  }
}

onMounted(loadAIModels)
onUnmounted(() => {
  if (aiRetryTimer) clearTimeout(aiRetryTimer)
})
const showAIModelPicker = ref(false)
const aiModelColumns = computed(() => aiModels.value.map(m => ({ text: m, value: m })))
async function onAIModelConfirm({ selectedValues }: any) {
  const m = selectedValues[0] as string
  store.set({ ai: { ...s.ai, modelPath: m } })
  showAIModelPicker.value = false
  try {
    await http.post('/api/ai/load', { path: aiModels.value.find(x => x.endsWith('/' + m) || x === m) })
    showToast('模型已切换：' + m)
    await loadAIModels()
  } catch {
    showToast('切换请求失败')
  }
}

// ---- 免登录信任窗口（仅管理员；全局默认） ----
// 本地草稿：空串=清除（跟随默认 72h 由后端默认值兜底）；数字=小时。
const trustHoursText = ref('')
const trustHoursSaving = ref(false)

function initTrustHours() {
  // fetchAppSettings 返回值会进 store.settings；这里从 store 读
  const v = (store.settings as any).trustWindowHours
  trustHoursText.value = v === undefined || v === null ? '' : String(v)
}

async function saveTrustHours() {
  if (trustHoursSaving.value) return
  const raw = trustHoursText.value.trim()
  let hours: number
  if (raw === '') {
    hours = 0
  } else {
    hours = Number(raw)
    if (!Number.isFinite(hours) || hours < 0 || hours > 8760) {
      showToast('免登录窗口需为 0-8760 小时')
      return
    }
  }
  trustHoursSaving.value = true
  try {
    store.set({ trustWindowHours: hours }, false)
    await store.save()
    showToast(hours > 0 ? `已保存：登录后 ${hours} 小时内免重复登录` : '已关闭免登录')
  } catch (e: any) {
    showToast(e?.response?.data?.error || '保存失败')
  } finally {
    trustHoursSaving.value = false
  }
}

// ---- user management ----

const users = ref<ManagedUser[]>([])
const usersLoaded = ref(false)
const showUserDialog = ref(false)
// 用户级关怀生效位：当前登录账号被勾选了关怀模式（隐藏设置页字号/关怀开关）
const userCareActive = computed(() => !!auth.user?.careMode)
// 编辑弹窗中的关怀开关（即点即存，与改名/改密码一致）
const userCareDraft = ref(false)
const careAppliedMsg = ref('')
const editingUser = ref<ManagedUser | null>(null)
const userForm = ref({ username: '', password: '', role: 'user' as string })
// 用户级免登录窗口草稿：''=未改；数字串=小时；'-1'=清除跟随全局
const userTrustText = ref('')
const trustAppliedId = ref('')
const trustAppliedMsg = ref('')
const showRolePicker = ref(false)

const roleOptions = [
  { label: '管理员', value: 'admin' },
  { label: '操作员', value: 'operator' },
  { label: '普通用户', value: 'user' },
  { label: '只读', value: 'viewer' },
]

async function loadUsers() {
  if (!isBackend()) return
  try {
    users.value = await fetchUsers()
    usersLoaded.value = true
  } catch {
    /* ignore */
  }
}

function openAddUser() {
  editingUser.value = null
  userForm.value = { username: '', password: '', role: 'user' }
  showUserDialog.value = true
}

function openEditUser(u: ManagedUser) {
  editingUser.value = u
  userForm.value = { username: u.username, password: '', role: u.role }
  userTrustText.value = u.trustWindowHours == null ? '' : String(u.trustWindowHours)
  userCareDraft.value = !!u.careMode
  trustAppliedId.value = ''
  trustAppliedMsg.value = ''
  careAppliedMsg.value = ''
  showUserDialog.value = true
}

// 保存该用户的关怀模式：开启后其登录即进入关怀界面
async function applyUserCare() {
  const u = editingUser.value
  if (!u) return
  try {
    const { data } = await http.put(`/api/users/${u.id}`, { careMode: userCareDraft.value })
    await loadUsers()
    editingUser.value = { ...u, careMode: userCareDraft.value }
    if (auth.user && auth.user.id === u.id) {
      await auth.refreshMe()
    }
    careAppliedMsg.value = userCareDraft.value
      ? '已开启：该用户下次打开应用即进入关怀模式'
      : '已关闭：该用户恢复常规界面与字号设置'
    showToast((data as any)?.ok ? '已保存' : '已保存')
  } catch (e: any) {
    showToast(e?.response?.data?.error || '保存关怀模式失败')
  }
}

// 保存该用户的免登录窗口：空串=清除跟随全局；数字=专属小时数
async function applyUserTrust() {
  const u = editingUser.value
  if (!u) return
  const raw = userTrustText.value.trim()
  let patch: { trustWindowHours?: number | null }
  if (raw === '') {
    patch = { trustWindowHours: null }
  } else {
    const h = Number(raw)
    if (!Number.isFinite(h) || h < 0 || h > 8760) {
      showToast('免登录窗口需为 0-8760 小时，留空则跟随全局')
      return
    }
    patch = { trustWindowHours: h }
  }
  try {
    await updateUser(u.id, patch)
    trustAppliedId.value = u.id
    trustAppliedMsg.value = raw === '' ? '已跟随全局' : `该用户登录后 ${raw} 小时内免重复登录`
    await loadUsers()
  } catch (e: any) {
    showToast(e?.response?.data?.error || '保存免登录窗口失败')
  }
}

// 行点击即可进入编辑（内含「改名」「改密码」按钮）

// 编辑模式拆成独立动作按钮：改名 / 改密码 即点即生效，
// 避免「改完字段点 X 关闭等于没改」的歧义。
async function renameUser() {
  if (!editingUser.value) return
  const nu = userForm.value.username.trim()
  if (!nu) {
    showToast('请填写用户名')
    return
  }
  if (nu === editingUser.value.username) {
    showToast('用户名没有变化')
    return
  }
  try {
    await updateUser(editingUser.value.id, { username: nu, role: userForm.value.role })
    showToast(`已改名为 ${nu}`)
    editingUser.value = { ...editingUser.value, username: nu }
    await loadUsers()
  } catch (e: any) {
    showToast(e?.response?.data?.error || '改名失败')
  }
}

async function changeUserPassword() {
  if (!editingUser.value) return
  const pw = userForm.value.password
  if (!pw || pw.length < 8) {
    showToast('密码至少 8 个字符')
    return
  }
  try {
    await updateUser(editingUser.value.id, { password: pw, role: userForm.value.role })
    showToast('密码已修改')
    userForm.value.password = ''
  } catch (e: any) {
    showToast(e?.response?.data?.error || '修改密码失败')
  }
}

// 角色：编辑模式下选择即保存（无需再点别的按钮）
async function onRoleConfirm(v: { selectedOptions: Array<{ text: string; value: string }> }) {
  const selectedRole = v.selectedOptions[0]?.value
  const role: ManagedUser['role'] = roleOptions.some((item) => item.value === selectedRole)
    ? selectedRole as ManagedUser['role']
    : 'user'
  userForm.value.role = role
  showRolePicker.value = false
  if (editingUser.value && role !== editingUser.value.role) {
    try {
      await updateUser(editingUser.value.id, { role })
      editingUser.value = { ...editingUser.value, role }
      showToast('角色已更新')
      await loadUsers()
    } catch (e: any) {
      showToast(e?.response?.data?.error || '角色更新失败')
    }
  }
}

function closeUserDialog() {
  showUserDialog.value = false
}

// 创建新用户（编辑动作全部走上面的独立按钮）
async function saveUser() {
  if (!userForm.value.username || !userForm.value.password) {
    showToast('请填写用户名和密码')
    return
  }
  try {
    await createUser(userForm.value.username, userForm.value.password, userForm.value.role)
    showToast('已创建')
    showUserDialog.value = false
    await loadUsers()
  } catch (e: any) {
    showToast(e?.response?.data?.error || '操作失败')
  }
}

async function removeUser(u: ManagedUser) {
  try {
    await showConfirmDialog({ title: '删除用户', message: `确定删除「${u.username}」吗？` })
  } catch {
    return
  }
  try {
    await deleteUser(u.id)
    showToast('已删除')
    await loadUsers()
  } catch (e: any) {
    showToast(e?.response?.data?.error || '删除失败')
  }
}
</script>

<template>
  <div ref="pageRef" class="page settings-page" :class="{ 'has-sidenav': isDesktopSettings }" @scroll.passive="onSettingsScroll">
    <van-nav-bar title="设置" left-arrow @click-left="goBack" />

    <!-- 桌面端分区导航：窄屏不渲染（完全走原有单列布局） -->
    <nav v-if="isDesktopSettings" class="settings-nav" aria-label="设置分区导航">
      <button
        v-for="g in navGroups"
        :key="g.key"
        type="button"
        class="settings-nav-item"
        :class="{ on: activeSection === g.key }"
        :aria-current="activeSection === g.key ? 'true' : undefined"
        @click="goToSection(g.key)"
      >
        <van-icon :name="g.icon" size="16" />
        <span>{{ g.label }}</span>
      </button>
    </nav>

    <div v-if="isBackend() && (!store.loaded || store.loading || store.error || store.storageError)" class="settings-load-state" role="status">
      <span v-if="store.error">设置读取失败：{{ store.error }}。AI 状态尚未确认，不代表已关闭。</span>
      <span v-else-if="!store.loaded || store.loading">正在读取服务器设置…</span>
      <span v-else>{{ store.storageError }}</span>
      <van-button size="small" :loading="store.loading" @click="store.loadFromServer()">重试读取</van-button>
    </div>
    <div class="settings-columns" :inert="isBackend() && (!store.loaded || store.loading || !!store.error) ? true : undefined">
      <section class="settings-column" aria-label="显示、通知与账户">
    <div id="sec-display" class="set-block">
      <div class="set-block-header" role="button" tabindex="0" :aria-expanded="!isCollapsed('display')" @keydown.enter.prevent="toggleGroup('display')" @keydown.space.prevent="toggleGroup('display')" @click="toggleGroup('display')">
        <span class="set-block-title">显示与无障碍</span>
        <van-icon :name="isCollapsed('display') ? 'arrow-down' : 'arrow-up'" class="set-block-arrow" />
      </div>
      <van-cell-group v-show="!isCollapsed('display')" title="">
      <van-cell title="深色模式" label="跟随系统：随系统夜间自动切换" class="opt-cell">
        <template #value>
          <div class="theme-opts" role="radiogroup" aria-label="界面主题">
            <button
              v-for="o in themeOptions" :key="o.value" type="button" class="theme-opt control-button"
              role="radio" :aria-checked="s.theme === o.value" :class="{ on: s.theme === o.value }"
              @click="setTheme(o.value)"
            >{{ o.text }}</button>
          </div>
        </template>
      </van-cell>
      <!-- 用户级关怀（管理员在用户管理为本账号勾选）生效时，
           字号/关怀两项由用户资料控制，本地开关隐藏以免互相覆盖 -->
      <van-cell v-if="userCareActive" title="关怀模式已开启" label="已由管理员在本账号的用户设置中启用；如需调整请联系管理员" />
      <van-cell v-if="!s.careMode && !userCareActive" title="字体大小" label="立即生效" class="opt-cell">
        <template #value>
          <div class="font-opts">
            <button
              v-for="o in fontSizeOptions"
              :key="o.value"
              class="font-opt"
              type="button"
              :class="{ on: s.fontSize === o.value }"
              :aria-pressed="s.fontSize === o.value"
              :aria-label="o.label + '字体'"
              @click="setFontSize(o.value)"
            >
              {{ o.label }}
            </button>
          </div>
        </template>
      </van-cell>
      <van-cell v-if="!userCareActive" title="关怀模式" label="更大字体与按钮、更高对比度，方便长辈使用">
        <template #right-icon>
          <van-switch aria-label="关怀模式" :model-value="s.careMode" @update:model-value="setCareMode" />
        </template>
      </van-cell>
      <!-- 演示模式会整体切换数据源，属于管理员级操作，普通用户/只读不见 -->
      <van-cell v-if="auth.isAdmin || !isBackend()" title="演示模式" label="开启后使用内置模拟设备与事件数据，便于功能预览">
        <template #right-icon>
          <van-switch aria-label="演示模式" :model-value="s.demoMode" @update:model-value="setDemoMode" />
        </template>
      </van-cell>
    </van-cell-group>
    </div>

    <div id="sec-notify" class="set-block" v-if="auth.canEdit">
      <div class="set-block-header" role="button" tabindex="0" :aria-expanded="!isCollapsed('notify')" @keydown.enter.prevent="toggleGroup('notify')" @keydown.space.prevent="toggleGroup('notify')" @click="toggleGroup('notify')">
        <span class="set-block-title">通知</span>
        <van-icon :name="isCollapsed('notify') ? 'arrow-down' : 'arrow-up'" class="set-block-arrow" />
      </div>
      <van-cell-group v-show="!isCollapsed('notify')" title="">
      <van-cell title="移动侦测告警" label="检测到移动时推送通知">
        <template #right-icon>
          <van-switch aria-label="移动侦测告警" :model-value="s.motionPush" @update:model-value="store.set({ motionPush: $event })" />
        </template>
      </van-cell>
      <van-cell title="设备离线提醒" label="设备断线时推送通知">
        <template #right-icon>
          <van-switch aria-label="设备离线提醒" :model-value="s.offlinePush" @update:model-value="store.set({ offlinePush: $event })" />
        </template>
      </van-cell>
    </van-cell-group>
    </div>

    <div id="sec-server" class="set-block" v-if="auth.canEdit">
      <div class="set-block-header" role="button" tabindex="0" :aria-expanded="!isCollapsed('server')" @keydown.enter.prevent="toggleGroup('server')" @keydown.space.prevent="toggleGroup('server')" @click="toggleGroup('server')">
        <span class="set-block-title">服务器</span>
        <van-icon :name="isCollapsed('server') ? 'arrow-down' : 'arrow-up'" class="set-block-arrow" />
      </div>
      <van-cell-group v-show="!isCollapsed('server')" title="">
      <van-cell
        title="服务器地址"
        label="局域网 / 公网连接，自动判断"
        is-link
        @click="$router.push('/server')"
      />
    </van-cell-group>
    </div>

    <!-- 免登录信任窗口：仅管理员可见。token 有效期内本就免登录；
         这里配置的是 token 过期后的静默续期窗口。 -->
    <div id="sec-security" class="set-block" v-if="auth.isAdmin">
      <div class="set-block-header" role="button" tabindex="0" :aria-expanded="!isCollapsed('security')" @keydown.enter.prevent="toggleGroup('security')" @keydown.space.prevent="toggleGroup('security')" @click="toggleGroup('security')">
        <span class="set-block-title">登录安全</span>
        <van-icon :name="isCollapsed('security') ? 'arrow-down' : 'arrow-up'" class="set-block-arrow" />
      </div>
      <van-cell-group v-show="!isCollapsed('security')" title="">
      <van-cell title="免登录窗口" label="登录一次后，在此窗口内再次打开无需重新登录（token 过期也可静默续期）。0 = 关闭；用户可在「用户管理」里单独覆盖。">
        <template #value>
          <div class="trust-box">
            <van-field
              v-model="trustHoursText"
              type="digit"
              class="trust-input"
              placeholder="72"
              aria-label="免登录窗口小时数"
            />
            <span class="trust-unit">小时</span>
            <van-button type="primary" size="small" round :loading="trustHoursSaving" @click="saveTrustHours">保存</van-button>
          </div>
        </template>
      </van-cell>
    </van-cell-group>
    </div>

    <div id="sec-users" class="set-block" v-if="isBackend() && auth.isAdmin">
      <div class="set-block-header" role="button" tabindex="0" :aria-expanded="!isCollapsed('users')" @keydown.enter.prevent="toggleGroup('users')" @keydown.space.prevent="toggleGroup('users')" @click="toggleGroup('users')">
        <span class="set-block-title">用户管理</span>
        <van-icon :name="isCollapsed('users') ? 'arrow-down' : 'arrow-up'" class="set-block-arrow" />
      </div>
      <van-cell-group v-show="!isCollapsed('users')" title="">
      <van-cell
        v-for="u in users"
        :key="u.id"
        :title="u.username"
        :label="(roleOptions.find((r) => r.value === u.role)?.label || u.role) + ' · UID ' + u.uid"
        is-link
        @click="openEditUser(u)"
      >
        <template #right-icon>
          <span class="ua-btns">
            <span class="ua-btn" title="编辑 / 改名 / 改密码" @click.stop="openEditUser(u)">
              <van-icon name="edit" />
            </span>
            <span
              v-if="u.id !== auth.user?.id"
              class="ua-btn danger"
              title="删除"
              @click.stop="removeUser(u)"
            >
              <van-icon name="delete-o" />
            </span>
          </span>
        </template>
      </van-cell>
      <van-cell v-if="!usersLoaded" title="加载中..." />
      <van-cell v-if="usersLoaded && !users.length" title="暂无用户" />
      <div style="padding: 12px 16px">
        <van-button plain block round size="small" @click="openAddUser">
          <van-icon name="plus" style="margin-right: 4px" />添加用户
        </van-button>
      </div>
    </van-cell-group>
    </div>

      </section>
      <section class="settings-column" aria-label="存储、录像与识别">
    <div id="sec-storage" class="set-block" v-if="auth.canEdit">
      <div class="set-block-header" role="button" tabindex="0" :aria-expanded="!isCollapsed('storage')" @keydown.enter.prevent="toggleGroup('storage')" @keydown.space.prevent="toggleGroup('storage')" @click="toggleGroup('storage')">
        <span class="set-block-title">存储管理</span>
        <van-icon :name="isCollapsed('storage') ? 'arrow-down' : 'arrow-up'" class="set-block-arrow" />
      </div>
      <van-cell-group v-show="!isCollapsed('storage')" title="">
      <van-cell
        v-if="store.storage.totalGB > 0"
        title="存储空间"
        :label="`已用 ${store.storage.usedGB}GB / 共 ${store.storage.totalGB}GB`"
      >
        <template #value>
          <div class="progress">
            <div class="bar">
              <i :style="{ width: usedPct + '%' }" :class="{ warn: usedPct > 85 }" />
            </div>
            <span>{{ usedPct }}%</span>
          </div>
        </template>
      </van-cell>
      <van-cell
        v-if="isBackend()"
        title="运行状态"
        label="CPU / 内存占用、磁盘水位、录像与进程概况"
        is-link
        @click="openStatus"
      >
        <template #right-icon>
          <van-icon name="bar-chart-o" class="user-action" />
        </template>
      </van-cell>
      <van-cell title="循环覆盖 · 保留天数" label="超期自动删除最早录像；直接输入 1-3650 天，或点右侧箭头下拉选快捷档">
        <template #value>
          <div class="days-combo">
            <van-field
              v-model="retentionDaysText"
              type="number"
              :border="false"
              class="days-input"
              placeholder="30"
              @blur="applyRetentionDays"
            />
            <span class="days">天</span>
            <button type="button" class="combo-arrow control-button" aria-label="选择保留天数" @click="showDaysPicker = true"><van-icon name="arrow-down" /></button>
          </div>
        </template>
      </van-cell>
      <van-popup v-model:show="showDaysPicker" position="bottom" round>
        <van-picker v-picker-desktop :option-height="56" :visible-option-num="5"
          title="循环覆盖保留天数"
          :columns="daysColumns"
          :model-value="[retentionDaysText]"
          @confirm="onDaysConfirm"
          @cancel="showDaysPicker = false"
        />
      </van-popup>
      <van-cell title="循环覆盖 · 容量限额" label="所有摄像头录像合计的磁盘上限，超出后优先删除最旧的录像；0 = 不限制">
        <template #value>
          <div class="slider-box">
            <van-field
              v-model="retentionSizeText"
              type="number"
              :border="false"
              class="days-input"
              placeholder="0"
              @blur="applyRetentionSize"
            />
            <span class="days">GB</span>
          </div>
        </template>
      </van-cell>
    </van-cell-group>
    </div>

    <div id="sec-record" class="set-block" v-if="auth.canEdit">
      <div class="set-block-header" role="button" tabindex="0" :aria-expanded="!isCollapsed('record')" @keydown.enter.prevent="toggleGroup('record')" @keydown.space.prevent="toggleGroup('record')" @click="toggleGroup('record')">
        <span class="set-block-title">录像策略</span>
        <van-icon :name="isCollapsed('record') ? 'arrow-down' : 'arrow-up'" class="set-block-arrow" />
      </div>
      <van-cell-group v-show="!isCollapsed('record')" title="">
      <van-radio-group :model-value="s.recordMode" @update:model-value="setMode">
        <van-cell v-for="o in modeOptions" :key="o.value" clickable @click="setMode(o.value)">
          <template #title>
            <span class="mode-title">{{ o.label }}</span>
            <span class="mode-desc">{{ o.desc }}</span>
          </template>
          <template #right-icon>
            <van-radio :name="o.value" />
          </template>
        </van-cell>
      </van-radio-group>
      <van-cell v-if="s.recordMode === 'schedule'" title="录像时间段">
        <template #value>
          <button class="time-field mono control-button" aria-label="录像开始时间" @click="showStartPicker = true">{{ s.scheduleStart }}</button>
          <span class="time-sep">-</span>
          <button class="time-field mono control-button" aria-label="录像结束时间" @click="showEndPicker = true">{{ s.scheduleEnd }}</button>
        </template>
      </van-cell>
    </van-cell-group>
    </div>

    <div id="sec-ai" class="set-block" v-if="auth.canEdit">
      <div class="set-block-header" role="button" tabindex="0" :aria-expanded="!isCollapsed('ai')" @keydown.enter.prevent="toggleGroup('ai')" @keydown.space.prevent="toggleGroup('ai')" @click="toggleGroup('ai')">
        <span class="set-block-title">AI 画面识别</span>
        <van-icon :name="isCollapsed('ai') ? 'arrow-down' : 'arrow-up'" class="set-block-arrow" />
      </div>
      <van-cell-group v-show="!isCollapsed('ai')" title="">
      <van-cell title="启用 AI 识别" label="本地对象检测（默认），或切换云端视觉模型">
        <template #right-icon>
          <van-switch aria-label="启用 AI 识别" :model-value="s.ai.enabled" @update:model-value="store.set({ ai: { ...s.ai, enabled: $event } })" />
        </template>
      </van-cell>
      <template v-if="s.ai.enabled">
        <van-field
          :model-value="aiModeOptions.find(option => option.value === s.ai.mode)?.text || '本地对象检测'"
          is-link
          readonly
          label="识别引擎"
          placeholder="本地检测"
          @click="showAIModePicker = true"
        />
        <template v-if="s.ai.mode === 'local'">
          <van-field
            v-model="s.ai.detectUrl"
            label="检测服务地址"
            placeholder="unix:/tmp/cyannvr-ai.sock"
          />
          <van-cell title="通信方式" :label="aiTransportHint" />
          <!-- 推理后端：点击手动切换（重启生效），标签显示当前选择 -->
          <van-cell
            title="推理后端"
            is-link
            :label="backendHint || `实际生效：${backendLabel} · 选择后重启应用生效`"
            @click="showProviderPicker = true"
          >
            <template #value>
              <span class="backend-tag" :class="'backend-' + (aiInfo.backend || 'none')">
                {{ providerLabel }}
              </span>
            </template>
          </van-cell>
          <van-popup v-model:show="showProviderPicker" position="bottom" round>
            <van-picker v-picker-desktop :option-height="56" :visible-option-num="5"
              title="推理后端"
              :columns="providerColumns"
              :model-value="[s.ai.provider || 'auto']"
              @confirm="onProviderConfirm"
              @cancel="showProviderPicker = false"
            />
          </van-popup>
          <!-- 算力档位：由实测单帧延迟推导，决定分析频率与可带路数 -->
          <div class="cap-card">
            <div class="cap-head">
              <span class="cap-label">本机 AI 算力</span>
              <span v-if="capTierLabel" class="cap-tier" :class="'tier-' + (cap?.tier || '')">{{ capTierLabel }}</span>
              <span v-else class="cap-tier tier-unknown">检测中</span>
            </div>
            <div class="cap-hint">{{ capHint }}</div>
            <div v-if="cap?.tier" class="cap-note">
              分析频率已按本机算力自动调整：算力越强分析越勤，路数超过上限时自动降频，
              避免任务在推理端排队拖慢所有通道。
            </div>
          </div>
          <!-- 实测档：启动基准的完整结果，按名次（均值）排列 -->
          <div v-if="benchRows.length" class="bench-card">
            <div class="bench-title">实测结果（启动时自动执行 · 按均值排名）</div>
            <div class="bench-row" v-for="(b, i) in benchRows" :key="b.provider">
              <span class="bench-rank" :class="{ win: i === 0 }">{{ i + 1 }}</span>
              <span class="bench-name">{{ b.provider }}</span>
              <span class="bench-detail mono">{{ b.detail }}</span>
              <van-tag v-if="i === 0" type="primary">最快</van-tag>
            </div>
            <div class="bench-note">均=平均毫秒/帧 · 峰=峰值 · 首帧=冷启动 · Δ=与CPU基准偏差（&lt;1 合格） · +MB=内存增量</div>
          </div>
          <van-field
            v-model="s.ai.modelPath"
            is-link
            readonly
            label="检测模型"
            placeholder="yolov8n.onnx"
            @click="showAIModelPicker = true"
          />
          <van-popup v-model:show="showAIModelPicker" position="bottom" round>
            <van-picker v-picker-desktop :option-height="56" :visible-option-num="5" title="检测模型" :columns="aiModelColumns" :model-value="[s.ai.modelPath || '' ]" @confirm="onAIModelConfirm" @cancel="showAIModelPicker = false" />
          </van-popup>
          <!-- 可下载模型：镜像只内置 yolov8n，其余按需拉取，避免镜像膨胀 -->
          <template v-if="downloadable.length">
            <van-cell title="可添加模型" label="按需下载，不占镜像体积" />
            <van-cell
              v-for="c in downloadable"
              :key="c.id"
              :title="c.id"
              :label="`${c.desc} · 约 ${c.approx_mb}MB`"
            >
              <template #right-icon>
                <van-button
                  size="mini"
                  type="primary"
                  :loading="aiBusy === c.id"
                  :disabled="!c.url"
                  @click.stop="downloadModel(c)"
                >{{ c.url ? '下载' : '需手动放置' }}</van-button>
              </template>
            </van-cell>
          </template>
        </template>
        <template v-else>
          <van-field v-model="s.ai.baseUrl" label="接口地址" placeholder="http://localhost:11434/v1（Ollama）" />
          <van-field v-model="s.ai.model" label="模型" placeholder="llava（本地视觉模型）" />
          <van-field v-model="s.ai.apiKey" label="API Key" placeholder="本地模型可留空" />
        </template>
        <van-cell title="识别间隔(秒)" label="每隔多久分析一帧">
          <template #value>
            <van-stepper v-model="s.ai.interval" :min="5" :max="120" step="5" />
          </template>
        </van-cell>
        <van-cell title="事件冷却(秒)" label="同一设备事件间隔">
          <template #value>
            <van-stepper v-model="s.ai.cooldown" :min="10" :max="600" step="10" />
          </template>
        </van-cell>
        <van-cell title="触发阈值" label="置信度达到该值才记录">
          <template #value>
            <!-- 原先只有滑块、无数值显示，用户无法得知当前阈值；
                 现补充实时数值，并在松手时持久化 -->
            <div class="slider-box">
              <input
                v-model.number="s.ai.threshold"
                class="threshold-range"
                type="range"
                aria-label="AI 识别触发阈值"
                min="0.1"
                max="1"
                step="0.05"
                @change="store.set({ ai: { ...s.ai, threshold: s.ai.threshold } })"
              />
              <span class="days">{{ (s.ai.threshold ?? 0.5).toFixed(2) }}</span>
            </div>
          </template>
        </van-cell>
        <van-field
          v-if="s.ai.mode === 'openai'"
          v-model="s.ai.prompt"
          type="textarea"
          rows="3"
          autosize
          label="识别提示词"
          placeholder="分析画面并输出 JSON..."
        />
      </template>
      <van-popup v-model:show="showAIModePicker" position="bottom" round>
        <van-picker v-picker-desktop :option-height="56" :visible-option-num="5"
          title="识别引擎"
          :columns="aiModeOptions"
          :model-value="[s.ai.mode]"
          @confirm="onAIModeConfirm"
          @cancel="showAIModePicker = false"
        />
      </van-popup>
    </van-cell-group>
    </div>

      </section>
    </div>
    <div v-if="appVersion" class="about-version">
      CyanNVR v{{ appVersion }} · 界面 {{ buildId }}
      <button type="button" class="check-update-btn" :disabled="checkingUpdate" @click="checkForUpdate">
        {{ checkingUpdate ? '检查中…' : '检查更新' }}
      </button>
      <div v-if="updateState" class="update-panel">
        <p v-if="updateState.error" class="update-err">{{ updateState.error }}</p>
        <template v-else-if="updateState.hasUpdate">
          <p class="update-new">发现新版本 v{{ updateState.latest }}<template v-if="updateState.inApp && updateState.current">（当前 v{{ updateState.current }}）</template></p>
          <button v-if="updateState.inApp" type="button" class="update-open-btn" @click="installAppUpdate">下载并安装</button>
          <button v-else type="button" class="update-open-btn" @click="openRelease">前往 GitHub 下载</button>
        </template>
        <p v-else class="update-ok">已是最新版本<template v-if="updateState.inApp && updateState.current">（v{{ updateState.current }}）</template></p>
      </div>
    </div>

    <div class="save-area">
      <van-button type="primary" block round :loading="saving" :disabled="isBackend() && (!store.loaded || store.loading || !!store.error)" @click="save">
        保存设置
      </van-button>
      <van-button
        v-if="isBackend()"
        plain
        block
        round
        type="danger"
        style="margin-top: 10px"
        @click="logout"
      >
        退出登录
      </van-button>
    </div>

    <van-popup v-model:show="showStatus" position="bottom" round :style="{ maxHeight: '80%' }" @closed="closeStatusTimer">
      <div class="dialog-head">
        <span>运行状态</span>
        <van-icon name="cross" size="18" @click="showStatus = false" />
      </div>
      <div class="status-body">
        <div v-if="statusLoading && !status" class="status-loading">
          <van-loading size="20" />
          <span>读取中…</span>
        </div>
        <div v-else-if="statusErr" class="status-err">{{ statusErr }}</div>
        <template v-else-if="status">
          <div class="status-grid">
            <div class="status-item">
              <span class="k">CPU 占用</span>
              <span class="v">{{ status.process?.cpuPercent !== undefined ? status.process.cpuPercent + '%' : '采样中…' }}</span>
            </div>
            <div class="status-item">
              <span class="k">内存占用</span>
              <span class="v">{{ status.process?.memMB !== undefined ? status.process.memMB + ' MB' : '—' }}</span>
            </div>
            <div class="status-item">
              <span class="k">运行时长</span>
              <span class="v">{{ fmtUptime(status.process?.uptimeSec) }}</span>
            </div>
            <div class="status-item">
              <span class="k">ffmpeg 进程</span>
              <span class="v">{{ status.ffmpeg?.total ?? '—' }} 个</span>
            </div>
            <div class="status-item">
              <span class="k">摄像机</span>
              <span class="v">{{ status.devices?.online ?? 0 }} / {{ status.devices?.total ?? 0 }} 在线</span>
            </div>
            <div class="status-item">
              <span class="k">磁盘可用</span>
              <span class="v" :class="{ warn: status.disk?.low }">{{ status.disk?.freeGB ?? '—' }} GB</span>
            </div>
          </div>
          <div v-if="status.disk" class="status-disk">
            <div class="progress">
              <div class="bar">
                <i :style="{ width: (status.disk.usedPct || 0) + '%' }" :class="{ warn: (status.disk.usedPct || 0) > 85 }" />
              </div>
              <span>{{ status.disk.usedPct }}%</span>
            </div>
            <div class="disk-path mono">{{ status.disk.path }}</div>
          </div>
          <!-- 水位告警：低于该阈值服务会自动暂停录像，这里明确告知用户 -->
          <div v-if="status.disk?.low" class="status-warn">
            <van-icon name="warning-o" size="14" />
            <span>可用空间已低于最低水位 {{ status.disk.minFreeMB }}MB，录像已暂停；请清理磁盘或调低「容量限额」。</span>
          </div>
          <div class="status-note">
            版本 v{{ status.version }} · 数据每 5 秒刷新 · CPU 为两次采样之间的平均占用
          </div>
        </template>
      </div>
    </van-popup>

    <van-popup v-model:show="showStartPicker" position="bottom" round>
      <van-time-picker v-picker-desktop :option-height="56" :visible-option-num="5"
        :columns-type="['hour', 'minute']"
        :model-value="timeToColumns(s.scheduleStart)"
        title="开始时间"
        @confirm="onStartConfirm"
        @cancel="showStartPicker = false"
      />
    </van-popup>
    <van-popup v-model:show="showEndPicker" position="bottom" round>
      <van-time-picker v-picker-desktop :option-height="56" :visible-option-num="5"
        :columns-type="['hour', 'minute']"
        :model-value="timeToColumns(s.scheduleEnd)"
        title="结束时间"
        @confirm="onEndConfirm"
        @cancel="showEndPicker = false"
      />
    </van-popup>

    <van-popup v-model:show="showUserDialog" position="bottom" round :style="{ maxHeight: '80%' }">
      <div class="dialog-head">
        <span>{{ editingUser ? '编辑用户' : '添加用户' }}</span>
        <van-icon name="cross" size="18" @click="showUserDialog = false" />
      </div>
      <van-cell-group inset style="margin: 0 10px">
        <van-field
          v-if="editingUser"
          label="用户UID"
          :model-value="String(editingUser.uid ?? '')"
          disabled
          class="uid-field"
        />
        <!-- 用户名 + 改名：按钮贴在对应字段下方并右对齐，改完立即生效 -->
        <van-field
          v-model="userForm.username"
          label="用户名"
          placeholder="请输入用户名"
        />
        <div v-if="editingUser" class="field-action">
          <van-button type="primary" size="small" round @click="renameUser">改名</van-button>
        </div>
        <van-field
          v-model="userForm.password"
          type="password"
          label="密码"
          :placeholder="editingUser ? '至少 8 位，点下方按钮生效' : '请输入密码'"
        />
        <div v-if="editingUser" class="field-action">
          <van-button type="warning" plain size="small" round @click="changeUserPassword">修改密码</van-button>
        </div>
        <van-field label="角色" :model-value="roleOptions.find((r) => r.value === userForm.role)?.label" is-link @click="showRolePicker = true" />
        <van-field
          v-model="userTrustText"
          type="digit"
          label="免登录"
          placeholder="留空 = 跟随全局"
          aria-label="该用户免登录窗口小时数"
        />
        <div v-if="editingUser" class="field-action">
          <van-button plain type="primary" size="small" round @click="applyUserTrust">应用免登录窗口</van-button>
          <span v-if="trustAppliedId === editingUser.id" class="trust-applied">{{ trustAppliedMsg }}</span>
        </div>
        <!-- 用户级关怀模式：该账号登录即进入大字关怀界面，
             且其设置页不再展示「字体大小」「关怀模式」两项 -->
        <van-field v-if="editingUser" label="关怀模式" center clearable>
          <template #input>
            <van-switch v-model="userCareDraft" aria-label="该用户关怀模式" @change="applyUserCare" />
          </template>
        </van-field>
        <div v-if="editingUser && careAppliedMsg" class="field-action">
          <span class="trust-applied">{{ careAppliedMsg }}</span>
        </div>
      </van-cell-group>
      <div class="dialog-actions">
        <van-button v-if="editingUser" plain block round @click="closeUserDialog">完成（角色已随选择即时保存）</van-button>
        <van-button v-else type="primary" block round @click="saveUser">创建用户</van-button>
      </div>
      <div v-if="editingUser" class="uid-note">UID 为纯数字且不随改名变化，事件与会话都以它识别身份</div>
    </van-popup>

    <van-popup v-model:show="showRolePicker" position="bottom" round>
      <van-picker v-picker-desktop :option-height="56" :visible-option-num="5"
        :columns="roleOptions.map((r) => ({ text: r.label, value: r.value }))"
        @confirm="onRoleConfirm"
        @cancel="showRolePicker = false"
      />
    </van-popup>
  </div>
</template>

<style scoped>
.settings-load-state { padding: 12px 16px; color: var(--nvr-warning); display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
@media (min-width: 900px) {
  .settings-page.has-sidenav:has(.settings-load-state) > .settings-load-state { grid-row: 2; }
  .settings-page.has-sidenav:has(.settings-load-state) > .settings-nav,
  .settings-page.has-sidenav:has(.settings-load-state) > .settings-columns { grid-row: 3; }
}
.settings-page {
  padding-bottom: 30px;
}
/* 免登录窗口：输入 + 单位 + 保存按钮 一行排布 */
.trust-box {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.trust-input {
  width: 92px;
  padding: 2px 6px;
}
.trust-unit {
  color: var(--nvr-text-2);
  font-size: calc(13px * var(--nvr-font-scale, 1));
}
.trust-applied {
  margin-left: 10px;
  color: var(--nvr-success);
  font-size: calc(12px * var(--nvr-font-scale, 1));
}
.about-version {
  margin: 14px 16px 0;
  text-align: center;
  /* 11px 低于本页最小可读字号，抬到 12px 与其余说明文字齐平 */
  font-size: calc(12px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
}
.check-update-btn {
  margin-left: 10px;
  padding: 2px 12px;
  border: 1px solid var(--nvr-border);
  border-radius: var(--nvr-radius-full);
  background: var(--nvr-panel-2);
  color: var(--nvr-accent);
  font-size: calc(12px * var(--nvr-font-scale, 1));
  cursor: pointer;
}
.check-update-btn:disabled {
  opacity: 0.6;
  cursor: default;
}
.update-panel {
  margin-top: 10px;
  padding: 10px 12px;
  border-radius: var(--nvr-radius-sm);
  background: var(--nvr-panel-2);
  border: 1px solid var(--nvr-border);
}
.update-new {
  margin: 0 0 8px;
  color: var(--nvr-accent);
  font-weight: 600;
}
.update-ok {
  margin: 0;
  color: var(--nvr-green);
}
.update-err {
  margin: 0;
  color: var(--nvr-red);
}
.update-open-btn {
  padding: 6px 16px;
  border: 1px solid var(--nvr-accent);
  border-radius: var(--nvr-radius-full);
  background: rgba(46, 168, 255, 0.12);
  color: var(--nvr-accent);
  font-size: calc(12px * var(--nvr-font-scale, 1));
  cursor: pointer;
}
.settings-page :deep(.van-cell__title),
.settings-page :deep(.van-cell__value) {
  min-width: 0;
}
.settings-page :deep(.van-cell__label) {
  overflow-wrap: anywhere;
}
.progress {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: calc(12px * var(--nvr-font-scale, 1));
}
.bar {
  width: 110px;
  flex-shrink: 1;
  min-width: 36px;
  height: 6px;
  border-radius: var(--nvr-radius-sm);
  background: var(--nvr-panel-2);
  overflow: hidden;
}
.bar i {
  display: block;
  height: 100%;
  background: var(--nvr-green);
  border-radius: var(--nvr-radius-sm);
  transition: width 0.3s;
}
.bar i.warn {
  background: var(--nvr-amber);
}
.group-sub {
  padding: 14px 16px 6px;
  font-size: calc(12px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
  background: var(--nvr-panel-2);
  border-top: 1px solid var(--nvr-border);
}
/* 数值输入：圆角胶囊，和整页卡片风格一致 */
.days-input {
  width: 96px;
  padding: 0 10px;
  border-radius: var(--nvr-radius-sm);
  background: var(--nvr-panel-2);
  border: 1px solid var(--nvr-border);
}
.days-input :deep(.van-field__control) {
  text-align: right;
  font-weight: 600;
}
/* 循环覆盖：输入框 + 下拉箭头融合控件 */
.days-combo {
  display: flex;
  align-items: center;
  gap: 4px;
}
.combo-arrow {
  padding: 6px 2px;
  font-size: calc(14px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
}
/* 推理后端实测结果卡片 */
/* AI 算力档位卡片 */
.cap-card {
  margin: 8px 16px 4px;
  padding: 10px 12px;
  border-radius: var(--nvr-radius-sm);
  background: var(--nvr-panel-2);
  border: 1px solid var(--nvr-border);
}
.cap-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 4px;
}
.cap-label {
  font-size: calc(13px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-1);
  font-weight: 600;
}
.cap-tier {
  padding: 1px 8px;
  border-radius: var(--nvr-radius-full);
  font-size: calc(11px * var(--nvr-font-scale, 1));
  font-weight: 600;
  color: #fff;
  background: var(--nvr-text-3);
}
.cap-tier.tier-high { background: #16a34a; }
.cap-tier.tier-medium { background: #0891b2; }
.cap-tier.tier-low { background: #d97706; }
.cap-tier.tier-minimal { background: #dc2626; }
.cap-tier.tier-unknown { background: var(--nvr-text-3); }
.cap-hint {
  font-size: calc(12px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
  line-height: 1.5;
}
.cap-note {
  margin-top: 6px;
  font-size: calc(11px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-3);
  line-height: 1.5;
}
.bench-card {
  margin: 8px 16px 4px;
  padding: 10px 12px;
  border-radius: var(--nvr-radius-sm);
  background: var(--nvr-panel-2);
  border: 1px solid var(--nvr-border);
}
.bench-title {
  font-size: calc(12px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
  margin-bottom: 6px;
}
.bench-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 0;
  font-size: calc(13px * var(--nvr-font-scale, 1));
}
.bench-rank {
  width: 18px;
  height: 18px;
  border-radius: var(--nvr-radius-full);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: calc(11px * var(--nvr-font-scale, 1));
  background: var(--nvr-border);
  color: var(--nvr-text-2);
  flex-shrink: 0;
}
.bench-rank.win {
  background: var(--nvr-accent);
  color: #fff;
}
.bench-name {
  font-weight: 600;
  min-width: 48px;
}
.bench-detail {
  flex: 1;
  min-width: 0;
  font-size: calc(12px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.bench-note {
  margin-top: 6px;
  font-size: calc(10px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
  opacity: .8;
}
/* 对话框按钮区与 UID 展示 */
/* 行内改名图标颜色 */
.user-action.rename {
  color: var(--nvr-accent);
}

/* 字段正下方的动作按钮：改名/改密 与对应输入框紧贴，免去到底部找 */
.field-action {
  display: flex;
  justify-content: flex-end;
  padding: 2px 16px 10px;
  background: var(--van-cell-background, var(--nvr-panel));
}
.dialog-actions {
  padding: 16px;
}
.dialog-actions > * + * {
  margin-top: 10px;
}
.uid-field :deep(.van-field__control) {
  font-family: monospace;
  font-size: calc(12px * var(--nvr-font-scale, 1));
}
.uid-note {
  padding: 0 26px 16px;
  font-size: calc(11px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
}
.slider-box {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.threshold-range {
  width: 110px;
  flex: 1 1 110px;
  min-width: 80px;
  min-height: 36px;
  margin: 0;
  accent-color: var(--nvr-accent);
  cursor: ew-resize;
  touch-action: none;
  user-select: none;
  -webkit-user-select: none;
}
:global(body.care .threshold-range) {
  min-height: 44px;
}
.days {
  font-size: calc(12px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
  min-width: 44px;
}
.mode-title {
  display: block;
}
.mode-desc {
  display: block;
  font-size: calc(12px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
  margin-top: 2px;
}
.time-field {
  padding: 4px 10px;
  border-radius: var(--nvr-radius-sm);
  background: var(--nvr-panel-2);
  border: 1px solid var(--nvr-border);
  font-size: calc(13px * var(--nvr-font-scale, 1));
}
.time-sep {
  margin: 0 6px;
  color: var(--nvr-text-2);
}
.save-area {
  padding: 16px;
}

.font-opts {
  display: flex;
  align-items: flex-end;
  gap: 6px;
  justify-content: flex-end;
  flex-wrap: wrap;
}
.font-opt {
  min-width: 60px;
  min-height: 44px;
  padding: 8px;
  font-size: calc(14px * var(--nvr-font-scale, 1));
  border-radius: var(--nvr-radius-sm);
  border: 1px solid var(--nvr-border);
  background: var(--nvr-panel-2);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--nvr-text-2);
  cursor: pointer;
  flex-shrink: 0;
}
.font-opt.on {
  border-color: var(--nvr-accent);
  color: var(--nvr-accent);
  background: rgba(46, 168, 255, 0.12);
}
/* 主题三选项（跟随系统/浅色/深色）：与字号选项同款分段按钮 */
.theme-opts {
  display: flex;
  align-items: center;
  gap: 6px;
  justify-content: flex-end;
  flex-wrap: wrap;
}
.theme-opt {
  min-width: 64px;
  min-height: 44px;
  padding: 8px 10px;
  font-size: calc(14px * var(--nvr-font-scale, 1));
  border-radius: var(--nvr-radius-sm);
  border: 1px solid var(--nvr-border);
  background: var(--nvr-panel-2);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--nvr-text-2);
  cursor: pointer;
  flex-shrink: 0;
}
.theme-opt.on {
  border-color: var(--nvr-accent);
  color: var(--nvr-accent);
  background: rgba(46, 168, 255, 0.12);
}
:global(body.care .settings-page .theme-opt) {
  min-width: 88px;
  min-height: 52px;
  font-size: calc(17px * var(--nvr-font-scale, 1));
}
@media (max-width: 480px) {
  .settings-page :deep(.van-cell:has(.slider-box)),
  .settings-page :deep(.van-cell:has(.days-combo)),
  .settings-page :deep(.van-cell:has(.font-opts)),
  .settings-page :deep(.van-cell:has(.theme-opts)),
  .settings-page :deep(.van-cell:has(.progress)) {
    flex-wrap: wrap;
    gap: 12px;
  }
  .settings-page :deep(.van-cell:has(.slider-box) .van-cell__value),
  .settings-page :deep(.van-cell:has(.days-combo) .van-cell__value),
  .settings-page :deep(.van-cell:has(.font-opts) .van-cell__value),
  .settings-page :deep(.van-cell:has(.theme-opts) .van-cell__value),
  .settings-page :deep(.van-cell:has(.progress) .van-cell__value) {
    flex: 1 0 100%;
    text-align: left;
  }
  .slider-box,
  .days-combo,
  .progress,
  .font-opts,
  .theme-opts {
    justify-content: flex-start;
    flex-wrap: nowrap;
  }
  /* 循环覆盖输入框在窄屏独占一行，输入框右对齐到行尾，不再溢出/被标题挤压 */
  .days-combo {
    width: 100%;
  }
  .days-combo .days-input,
  .slider-box .days-input {
    flex: 1 1 auto;
    min-width: 0;
    width: auto;
  }
  /* 主题三选项在窄屏独占一行后平均分配，不再换行（此前「深色」掉到第二行） */
  .theme-opts {
    width: 100%;
  }
  .theme-opts .theme-opt {
    flex: 1 1 0;
    min-width: 0;
    padding: 8px 4px;
  }
}
.user-action {
  margin-left: 12px;
  color: var(--nvr-text-2);
  font-size: calc(18px * var(--nvr-font-scale, 1));
}
.user-action.lock {
  color: var(--nvr-accent);
  font-size: calc(18px * var(--nvr-font-scale, 1));
}
/* 用户管理行内：圆形描边小按钮（比裸图标更清晰、更好点） */
.ua-btns {
  display: inline-flex;
  gap: 8px;
  align-items: center;
}
.ua-btn {
  width: 30px;
  height: 30px;
  border-radius: var(--nvr-radius-full);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: var(--nvr-panel-2);
  border: 1px solid var(--nvr-border);
  color: var(--nvr-text-2);
  font-size: calc(16px * var(--nvr-font-scale, 1));
  transition: all .15s ease;
}
.ua-btn:active {
  transform: scale(.92);
}
.ua-btn:not(.danger):active {
  border-color: var(--nvr-accent);
  color: var(--nvr-accent);
}
.ua-btn.danger {
  color: var(--nvr-red);
}
.ua-btn.danger:active {
  background: rgba(255, 77, 79, .12);
  border-color: var(--nvr-red);
}
.user-action.del:active {
  color: var(--nvr-red);
}
.dialog-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px;
  font-weight: 600;
  font-size: calc(16px * var(--nvr-font-scale, 1));
}

/* ---- 运行状态弹层 ---- */
.status-body {
  padding: 0 16px 20px;
}
.status-loading,
.status-err {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 28px 0;
  color: var(--nvr-text-2);
  font-size: calc(13px * var(--nvr-font-scale, 1));
}
.status-err {
  color: var(--nvr-red);
}
.status-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 10px;
}
.status-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 10px 12px;
  border-radius: var(--nvr-radius-sm);
  background: var(--nvr-panel-2);
  border: 1px solid var(--nvr-border);
  min-width: 0;
}
.status-item .k {
  font-size: calc(12px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
}
.status-item .v {
  font-size: calc(18px * var(--nvr-font-scale, 1));
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.status-item .v.warn {
  color: var(--nvr-amber);
}
.status-disk {
  margin-top: 12px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.status-disk .progress {
  justify-content: flex-start;
}
.disk-path {
  font-size: calc(11px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
  overflow-wrap: anywhere;
}
.status-warn {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  margin-top: 12px;
  padding: 8px 10px;
  border-radius: var(--nvr-radius-sm);
  background: rgba(255, 176, 32, 0.12);
  border: 1px solid rgba(255, 176, 32, 0.4);
  color: var(--nvr-amber);
  font-size: calc(12px * var(--nvr-font-scale, 1));
  line-height: 1.6;
}
.status-warn .van-icon {
  margin-top: 3px;
  flex-shrink: 0;
}
.status-note {
  margin-top: 14px;
  font-size: calc(11px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
  text-align: center;
  line-height: 1.6;
}

.settings-columns {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 20px;
  align-items: start;
}
.settings-column { min-width: 0; }
.set-block:empty { display: none; }

/* ── 分组折叠头部 ── */
.set-block-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  cursor: pointer;
  user-select: none;
  background: var(--nvr-panel);
  border-radius: var(--nvr-radius-md) var(--nvr-radius-md) 0 0;
  border-bottom: 1px solid var(--nvr-border);
}
.set-block-header:active {
  background: var(--nvr-panel-2);
}
.set-block-header:focus-visible {
  outline: 2px solid var(--nvr-primary);
  outline-offset: -2px;
}
.set-block-title {
  font-size: calc(14px * var(--nvr-font-scale, 1));
  font-weight: 600;
  color: var(--nvr-text-2);
}
.set-block-arrow {
  font-size: calc(16px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-3);
  transition: transform 0.2s ease;
}
.set-block-header:hover .set-block-arrow {
  color: var(--nvr-text-2);
}
/* 折叠时隐藏底部圆角，展开时保持 */
.set-block:has(.set-block-header) .van-cell-group {
  border-radius: 0 0 var(--nvr-radius-md) var(--nvr-radius-md);
}
.set-block:has(.set-block-header) .van-cell-group__title {
  display: none; /* 隐藏 Vant 默认标题，用自定义头部替代 */
}

@media (min-width: 900px) {
  .settings-page {
    width: 100%;
    padding: 0 24px 20px;
    margin: 0;
    max-width: none;
  }
  .settings-page > .van-nav-bar { flex-shrink: 0; }
  .settings-columns {
    width: 100%;
    max-width: 1440px;
    margin: 0 auto;
  }
  .settings-page .save-area {
    position: sticky;
    bottom: 0;
    z-index: 2;
    display: flex;
    justify-content: flex-end;
    gap: 12px;
    padding: 14px 0;
    background: var(--nvr-bg);
    border-top: 1px solid var(--nvr-border);
  }
  .save-area .van-button {
    width: auto;
    min-width: 160px;
    margin-top: 0 !important;
  }
  .set-block { margin-bottom: 20px; }
  /* 注：这里不再给 .van-cell-group 加边框+圆角。
     桌面端统一走下方 >=900px 的「扁平表单工作区」，任何卡片外壳
     （边框/圆角/阴影）都会与去卡片化的目标冲突。 */
  .settings-page :deep(.van-cell__title) { min-width: 0; }
  .settings-page :deep(.van-cell__label) { line-height: 1.6; }
}
@media (min-width: 900px) {
  /* ── 桌面端：分区导航 + 扁平表单工作区（取代原两列卡片流）──
     原先是「两列卡片流」：每个分组自成一张卡片、分两列平铺，宽屏下视线要在
     两条纵列间来回跳，且要不断滚动才能找到目标分组。
     现改为「左侧固定分区导航 + 右侧单列扁平表单」：
     · 导航一次点击直达分组，滚动时自动高亮当前阅读位置；
     · 右侧去掉卡片外壳（无边框/无圆角卡片），改为带分隔线的扁平区块，
       视觉上是一条连续表单流，而不是一堆并列卡片；
     · 单列保证「标题 → 控件」的阅读方向唯一。 */
  .settings-page.has-sidenav {
    display: grid;
    grid-template-columns: 208px minmax(0, 1fr);
    /* 第 2 行必须按内容高度（auto），不能用 minmax(0, 1fr)：
       用 1fr 时该行被固定为「可视高度」，而 .settings-columns 的内容
       远高于它（实测 1595px vs 633px，overflow 可见），内容会溢出到
       下一行区域，与底部的保存栏/版本行重叠——保存栏因此盖住
       「免登录窗口」等表单文字。改成 auto 后内容与保存栏回到正常文档流，
       页面整体滚动，保存栏的 sticky bottom 才符合预期。 */
    grid-template-rows: auto auto;
    column-gap: 28px;
    align-content: start;
  }
  .settings-page.has-sidenav > .van-nav-bar,
  .settings-page.has-sidenav > .settings-load-state,
  .settings-page.has-sidenav > .about-version,
  .settings-page.has-sidenav > .save-area { grid-column: 1 / -1; }

  .settings-nav {
    grid-column: 1;
    grid-row: 2;
    align-self: start;
    position: sticky;
    top: 12px;
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 6px;
    background: var(--nvr-panel);
    border: 1px solid var(--nvr-border);
    border-radius: var(--nvr-radius-md);
  }
  .settings-nav-item {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    min-height: 40px;
    padding: 8px 10px;
    border: 0;
    border-radius: var(--nvr-radius-sm);
    background: transparent;
    color: var(--nvr-text-2);
    font-size: calc(13px * var(--nvr-font-scale, 1));
    text-align: left;
    cursor: pointer;
  }
  .settings-nav-item:hover { background: var(--nvr-panel-2); color: var(--nvr-text); }
  .settings-nav-item.on {
    background: var(--nvr-accent-soft-2);
    color: var(--nvr-accent);
    font-weight: 600;
  }
  .settings-nav-item span { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  /* 右侧：单列扁平；两段 settings-column 依次堆叠，顺序与导航一致 */
  .settings-page.has-sidenav .settings-columns {
    grid-column: 2;
    grid-row: 2;
    grid-template-columns: minmax(0, 1fr);
    gap: 0;
    max-width: 980px;
    margin: 0;
  }
  .settings-page.has-sidenav .set-block { margin-bottom: 30px; }
  /* 去卡片化：分组变成「带下划线的标题 + 扁平行」，不再是独立卡片 */
  .settings-page.has-sidenav .set-block-header {
    padding: 4px 2px 10px;
    background: transparent;
    border-bottom: 1px solid var(--nvr-border);
    border-radius: 0;
  }
  .settings-page.has-sidenav .set-block-header:active { background: transparent; }
  .settings-page.has-sidenav .set-block-title { font-size: calc(15px * var(--nvr-font-scale, 1)); }
  .settings-page.has-sidenav .set-block :deep(.van-cell-group),
  .settings-page.has-sidenav .set-block:has(.set-block-header) :deep(.van-cell-group) {
    border: 0;
    border-radius: 0;
    background: transparent;
  }
  /* 行只靠分隔线区分，不再嵌套卡片 */
  .settings-page.has-sidenav .set-block :deep(.van-cell) {
    background: transparent;
    padding-left: 2px;
    padding-right: 2px;
  }
  .settings-page.has-sidenav .set-block :deep(.van-cell)::after {
    left: 2px;
    right: 2px;
  }
  /* 窄控件行仍是「标题左、控件右」；仅在真的放不下时才换行 */
  .settings-page.has-sidenav :deep(.van-cell:has(.days-combo)),
  .settings-page.has-sidenav :deep(.van-cell:has(.slider-box)) { flex-wrap: wrap; gap: 10px; }
}
/* 推理后端标签：GPU 类后端用醒目色，CPU 用中性色，避免用户误以为已开硬件加速 */
.backend-tag {
  display: inline-block;
  padding: 1px 8px;
  border-radius: var(--nvr-radius-sm);
  font-size: calc(12px * var(--nvr-font-scale, 1));
  line-height: 18px;
  white-space: nowrap;
  color: #fff;
  background: var(--nvr-text-3, #969799);
}
.backend-tag.backend-cuda,
.backend-tag.backend-tensorrt {
  background: #76b900;
}
.backend-tag.backend-rocm {
  background: #ed1c24;
}
.backend-tag.backend-openvino,
.backend-tag.backend-directml {
  background: #0068b7;
}
.backend-tag.backend-cpu {
  background: #8a8a8a;
}
</style>
