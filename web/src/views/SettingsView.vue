<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { showConfirmDialog, showToast } from 'vant'
import type { RecordMode } from '../types'
import { useSettingsStore } from '../stores/settings'
import { useAuthStore } from '../stores/auth'
import { http } from '../api/client'
import {
  changeOwnPassword,
  createUser,
  deleteUser,
  fetchUsers,
  isBackend,
  updateUser,
  type ManagedUser,
} from '../api'

const store = useSettingsStore()
const auth = useAuthStore()
const router = useRouter()

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
const pwd = ref({ old: '', next: '', confirm: '' })
const showPwdDialog = ref(false)

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

onMounted(() => {
  store.loadFromServer()
  loadUsers()
  loadAboutVersion()
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
function onStartConfirm(c: string[]) {
  store.set({ scheduleStart: columnsToTime(c) })
  showStartPicker.value = false
}
function onEndConfirm(c: string[]) {
  store.set({ scheduleEnd: columnsToTime(c) })
  showEndPicker.value = false
}

async function save() {
  if (pwd.value.next !== pwd.value.confirm) {
    showToast('两次输入的新密码不一致')
    return
  }
  saving.value = true
  try {
    if (isBackend() && pwd.value.old && pwd.value.next) {
      await changeOwnPassword(pwd.value.old, pwd.value.next)
      pwd.value = { old: '', next: '', confirm: '' }
      showPwdDialog.value = false
    }
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

function setTheme() {
  store.set({ theme: s.theme === 'dark' ? 'light' : 'dark' })
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

const backendHint = computed(() => {
  if (aiInfo.value.backend_fallback) return `已回退：${aiInfo.value.backend_fallback}`
  if (aiError.value) return `${aiError.value}（依赖缺失时请安装 opencv-python-headless 与 onnxruntime 后重启应用，页面每 5 秒自动重试）`
  return ''
})

async function loadAIModels() {
  try {
    const { data: j } = await http.get('/api/ai/models')
    aiInfo.value = j
    aiError.value = ''
    aiModels.value = (j.models || []).map((p: string) => p.split(/[\\/]/).pop())
    if (!s.ai.modelPath && aiModels.value.length) {
      const def =
        aiModels.value.find(m => m.includes('yolo11n')) ||
        aiModels.value.find(m => m.includes('yolov8n')) ||
        aiModels.value[0]
      store.set({ ai: { ...s.ai, modelPath: def } })
    }
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

// ---- user management ----

const users = ref<ManagedUser[]>([])
const usersLoaded = ref(false)
const showUserDialog = ref(false)
const editingUser = ref<ManagedUser | null>(null)
const userForm = ref({ username: '', password: '', role: 'user' as string })
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
  showUserDialog.value = true
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
  const role = v.selectedOptions[0]?.value || 'user'
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
  <div class="page settings-page">
    <van-nav-bar title="设置" left-arrow @click-left="goBack" />

    <van-cell-group title="存储管理">
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
            <van-icon name="arrow-down" class="combo-arrow" @click="showDaysPicker = true" />
          </div>
        </template>
      </van-cell>
      <van-popup v-model:show="showDaysPicker" position="bottom" round>
        <van-picker
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

    <van-cell-group title="录像策略">
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
          <span class="time-field mono" @click="showStartPicker = true">{{ s.scheduleStart }}</span>
          <span class="time-sep">-</span>
          <span class="time-field mono" @click="showEndPicker = true">{{ s.scheduleEnd }}</span>
        </template>
      </van-cell>
    </van-cell-group>

    <van-cell-group title="通知">
      <van-cell title="移动侦测告警" label="检测到移动时推送通知">
        <template #right-icon>
          <van-switch :model-value="s.motionPush" @update:model-value="store.set({ motionPush: $event })" />
        </template>
      </van-cell>
      <van-cell title="设备离线提醒" label="设备断线时推送通知">
        <template #right-icon>
          <van-switch :model-value="s.offlinePush" @update:model-value="store.set({ offlinePush: $event })" />
        </template>
      </van-cell>
    </van-cell-group>

    <van-cell-group title="显示与无障碍">
      <van-cell title="深色模式" label="切换界面主题">
        <template #right-icon>
          <van-switch :model-value="s.theme === 'dark'" @update:model-value="setTheme" />
        </template>
      </van-cell>
      <van-cell v-if="!s.careMode" title="字体大小" label="全局文字大小，立即生效">
        <template #value>
          <div class="font-opts">
            <span
              v-for="o in fontSizeOptions"
              :key="o.value"
              class="font-opt"
              :class="{ on: s.fontSize === o.value && !s.careMode }"
              :style="{ fontSize: o.size + 'px' }"
              @click="setFontSize(o.value)"
            >
              A
            </span>
          </div>
        </template>
      </van-cell>
      <van-cell title="关怀模式" label="更大字体与按钮、更高对比度，方便长辈使用">
        <template #right-icon>
          <van-switch :model-value="s.careMode" @update:model-value="setCareMode" />
        </template>
      </van-cell>
      <van-cell title="演示模式" label="开启后使用内置模拟设备与事件数据，便于功能预览">
        <template #right-icon>
          <van-switch :model-value="s.demoMode" @update:model-value="setDemoMode" />
        </template>
      </van-cell>
    </van-cell-group>

    <van-cell-group title="服务器">
      <van-cell
        title="服务器地址"
        label="局域网 / 公网连接，自动判断"
        is-link
        @click="$router.push('/server')"
      />
    </van-cell-group>

    <van-cell-group title="AI 画面识别">
      <van-cell title="启用 AI 识别" label="本地对象检测（默认），或切换云端视觉模型">
        <template #right-icon>
          <van-switch :model-value="s.ai.enabled" @update:model-value="store.set({ ai: { ...s.ai, enabled: $event } })" />
        </template>
      </van-cell>
      <template v-if="s.ai.enabled">
        <van-field
          v-model="s.ai.mode"
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
            <van-picker
              title="推理后端"
              :columns="providerColumns"
              :model-value="[s.ai.provider || 'auto']"
              @confirm="onProviderConfirm"
              @cancel="showProviderPicker = false"
            />
          </van-popup>
          <!-- 实测档：启动基准的完整结果，按名次（均值）排列 -->
          <div v-if="benchRows.length" class="bench-card">
            <div class="bench-title">实测结果（启动时自动执行 · 按均值排名）</div>
            <div class="bench-row" v-for="(b, i) in benchRows" :key="b.provider">
              <span class="bench-rank" :class="{ win: i === 0 }">{{ i + 1 }}</span>
              <span class="bench-name">{{ b.provider }}</span>
              <span class="bench-detail mono">{{ b.detail }}</span>
              <van-tag v-if="i === 0" type="primary" size="small">最快</van-tag>
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
            <van-picker title="检测模型" :columns="aiModelColumns" :model-value="[s.ai.modelPath || '' ]" @confirm="onAIModelConfirm" @cancel="showAIModelPicker = false" />
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
              <van-slider
                v-model="s.ai.threshold"
                :min="0.1"
                :max="1"
                :step="0.05"
                style="width: 110px"
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
        <van-picker
          title="识别引擎"
          :columns="aiModeOptions"
          :model-value="[s.ai.mode]"
          @confirm="onAIModeConfirm"
          @cancel="showAIModePicker = false"
        />
      </van-popup>
    </van-cell-group>

    <!-- 账户安全：只放「改我的密码」，点开即改，一级目录不再裸露三个密码框 -->
    <van-cell-group title="账户安全">
      <van-cell
        title="修改我的密码"
        :label="`当前账号 ${auth.user?.username || ''} · 修改后需重新登录`"
        is-link
        @click="showPwdDialog = true"
      >
        <template #right-icon>
          <van-icon name="lock" class="user-action lock" />
        </template>
      </van-cell>
    </van-cell-group>

    <!-- 用户管理：独立成组，管理员可见 -->
    <van-cell-group v-if="isBackend() && auth.isAdmin" title="用户管理">
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

    <div v-if="appVersion" class="about-version">
      CyanNVR v{{ appVersion }} · 看到此行 = 已加载最新界面（否则请强制刷新 Ctrl+Shift+R 或重开窗口）
    </div>

    <div class="save-area">
      <van-button type="primary" block round :loading="saving" @click="save">
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

    <!-- 修改我的密码（从「账户安全」点入，收起一级目录的裸露字段） -->
    <van-popup v-model:show="showPwdDialog" position="bottom" round :style="{ maxHeight: '85%' }">
      <div class="dialog-head">
        <span>修改我的密码</span>
        <van-icon name="cross" size="18" @click="showPwdDialog = false" />
      </div>
      <van-cell-group inset style="margin: 0 10px">
        <van-field v-model="pwd.old" type="password" label="原密码" placeholder="请输入原密码" />
        <van-field v-model="pwd.next" type="password" label="新密码" placeholder="请输入新密码（至少8位）" />
        <van-field v-model="pwd.confirm" type="password" label="确认密码" placeholder="再次输入新密码" />
      </van-cell-group>
      <div class="dialog-actions">
        <van-button type="primary" block round :loading="saving" @click="save">确认修改</van-button>
      </div>
    </van-popup>

    <van-popup v-model:show="showStartPicker" position="bottom" round>
      <van-time-picker
        :columns-type="['hour', 'minute']"
        :model-value="timeToColumns(s.scheduleStart)"
        title="开始时间"
        @confirm="onStartConfirm"
        @cancel="showStartPicker = false"
      />
    </van-popup>
    <van-popup v-model:show="showEndPicker" position="bottom" round>
      <van-time-picker
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
      <div v-if="editingUser" class="dialog-quick">
        <van-button type="primary" size="small" round icon="edit" @click="renameUser">改名</van-button>
        <van-button type="warning" plain size="small" round icon="lock" @click="changeUserPassword">改密码</van-button>
      </div>
      <van-cell-group inset style="margin: 0 10px">
        <van-field
          v-if="editingUser"
          label="用户UID"
          :model-value="String(editingUser.uid ?? '')"
          disabled
          class="uid-field"
        />
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
      </van-cell-group>
      <div class="dialog-actions">
        <van-button v-if="editingUser" plain block round @click="closeUserDialog">完成（角色已随选择即时保存）</van-button>
        <van-button v-else type="primary" block round @click="saveUser">创建用户</van-button>
      </div>
      <div v-if="editingUser" class="uid-note">UID 为纯数字且不随改名变化，事件与会话都以它识别身份</div>
    </van-popup>

    <van-popup v-model:show="showRolePicker" position="bottom" round>
      <van-picker
        :columns="roleOptions.map((r) => ({ text: r.label, value: r.value }))"
        @confirm="onRoleConfirm"
        @cancel="showRolePicker = false"
      />
    </van-popup>
  </div>
</template>

<style scoped>
.settings-page {
  padding-bottom: 30px;
}
.about-version {
  margin: 14px 16px 0;
  text-align: center;
  font-size: 11px;
  color: var(--nvr-text-2);
}
.progress {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
}
.bar {
  width: 110px;
  height: 6px;
  border-radius: 3px;
  background: var(--nvr-panel-2);
  overflow: hidden;
}
.bar i {
  display: block;
  height: 100%;
  background: var(--nvr-green);
  border-radius: 3px;
  transition: width 0.3s;
}
.bar i.warn {
  background: var(--nvr-amber);
}
.group-sub {
  padding: 14px 16px 6px;
  font-size: 12px;
  color: var(--nvr-text-2);
  background: var(--nvr-panel-2);
  border-top: 1px solid var(--nvr-border);
}
/* 数值输入：圆角胶囊，和整页卡片风格一致 */
.days-input {
  width: 96px;
  padding: 0 10px;
  border-radius: 10px;
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
  font-size: 14px;
  color: var(--nvr-text-2);
}
/* 推理后端实测结果卡片 */
.bench-card {
  margin: 8px 16px 4px;
  padding: 10px 12px;
  border-radius: 10px;
  background: var(--nvr-panel-2);
  border: 1px solid var(--nvr-border);
}
.bench-title {
  font-size: 12px;
  color: var(--nvr-text-2);
  margin-bottom: 6px;
}
.bench-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 0;
  font-size: 13px;
}
.bench-rank {
  width: 18px;
  height: 18px;
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
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
  font-size: 12px;
  color: var(--nvr-text-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.bench-note {
  margin-top: 6px;
  font-size: 10px;
  color: var(--nvr-text-2);
  opacity: .8;
}
/* 对话框按钮区与 UID 展示 */
/* 弹窗顶部快速操作按钮 */
.dialog-quick {
  display: flex;
  gap: 8px;
  justify-content: center;
  padding: 8px 16px 0;
}
.dialog-quick .van-button.flash {
  animation: btnFlash 1s ease 2;
}
@keyframes btnFlash {
  0%, 100% { box-shadow: 0 0 0 0 rgba(46, 168, 255, 0); }
  50% { box-shadow: 0 0 0 6px rgba(46, 168, 255, .4); }
}
/* 行内改名图标颜色 */
.user-action.rename {
  color: var(--nvr-accent);
}

/* 字段正下方的动作按钮：改名/改密 与对应输入框紧贴，免去到底部找 */
.field-action {
  display: flex;
  justify-content: flex-end;
  padding: 2px 16px 10px;
  background: #fff;
}
.dialog-actions {
  padding: 16px;
}
.dialog-actions > * + * {
  margin-top: 10px;
}
.uid-field :deep(.van-field__control) {
  font-family: monospace;
  font-size: 12px;
}
.uid-note {
  padding: 0 26px 16px;
  font-size: 11px;
  color: var(--nvr-text-2);
}
.slider-box {
  display: flex;
  align-items: center;
  gap: 8px;
}
.days {
  font-size: 12px;
  color: var(--nvr-text-2);
  min-width: 44px;
}
.mode-title {
  display: block;
}
.mode-desc {
  display: block;
  font-size: 12px;
  color: var(--nvr-text-2);
  margin-top: 2px;
}
.time-field {
  padding: 4px 10px;
  border-radius: 6px;
  background: var(--nvr-panel-2);
  border: 1px solid var(--nvr-border);
  font-size: 13px;
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
}
.font-opt {
  width: 36px;
  height: 36px;
  border-radius: 8px;
  border: 1px solid var(--nvr-border);
  background: var(--nvr-panel-2);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--nvr-text-2);
  cursor: pointer;
}
.font-opt.on {
  border-color: var(--nvr-accent);
  color: var(--nvr-accent);
  background: rgba(46, 168, 255, 0.12);
}
.user-action {
  margin-left: 12px;
  color: var(--nvr-text-2);
  font-size: 18px;
}
.user-action.lock {
  color: var(--nvr-accent);
  font-size: 18px;
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
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: var(--nvr-panel-2);
  border: 1px solid var(--nvr-border);
  color: var(--nvr-text-2);
  font-size: 16px;
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
  font-size: 16px;
}

@media (min-width: 900px) {
  .settings-page {
    max-width: 760px;
    margin: 0 auto;
    width: 100%;
  }
}
/* 推理后端标签：GPU 类后端用醒目色，CPU 用中性色，避免用户误以为已开硬件加速 */
.backend-tag {
  display: inline-block;
  padding: 1px 8px;
  border-radius: 10px;
  font-size: 12px;
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
