<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { showToast } from 'vant'
import type { Device, DiscoveredDevice, Stream } from '../types'
import { buildBrandUrl, fetchBrands, isBackend, probeStreams, testDevice, type BrandTemplate } from '../api'
import VendorBadge from './VendorBadge.vue'

const props = defineProps<{
  show: boolean
  devices: DiscoveredDevice[]
  discovering: boolean
  editDevice?: Device | null
}>()
const emit = defineEmits<{
  (e: 'update:show', v: boolean): void
  (e: 'add', input: Partial<Device>): void
  (e: 'discover'): void
}>()

const isEdit = ref(false)
const mode = ref<'form' | 'discover'>('form')
const testing = ref(false)
const testResult = ref<'ok' | 'fail' | null>(null)
// 「测试连接」返回的编码/分辨率/H.265 提示，展示在结果标签下方
const testInfo = ref<{ codec?: string; width?: number; height?: number; advice?: string } | null>(null)
const form = reactive({
  name: '',
  ip: '',
  port: 554,
  username: 'admin',
  password: '',
  rtspUrl: '',
  recordEnabled: true,
  recordMode: 'continuous' as 'continuous' | 'motion' | 'schedule',
  scheduleStart: '08:00',
  scheduleEnd: '20:00',
  retentionDays: '',
  retentionSizeGB: '',
  aiEnabled: 'default' as 'default' | 'on' | 'off',
  previewStream: '',
  recordStream: '',
})
const selectedIp = ref('')
// ---- 品牌模板 + 通道号：接 NVR 通道时无需手算 RTSP 地址 ----
const brands = ref<BrandTemplate[]>([])
const brand = ref('auto')
const channel = ref<number | string>(1)
const brandMainUrl = ref('')
const brandSubUrl = ref('')
const streams = ref<Stream[]>([])
const streamsLoading = ref(false)
const showModePicker = ref(false)
const showSchedulePicker = ref(false)
const showPreviewPicker = ref(false)
const showRecordPicker = ref(false)
const showAIPicker = ref(false)
const showBrandPicker = ref(false)
const timePick = ref<string[]>(['08', '00', '20', '00'])

const modeOptions = [
  { text: '连续录制', value: 'continuous' },
  { text: '移动侦测', value: 'motion' },
  { text: '定时录制', value: 'schedule' },
]

const modeLabel = computed(() => modeOptions.find((o) => o.value === form.recordMode)?.text ?? form.recordMode)

const aiModeOptions = [
  { text: '跟随全局设置', value: 'default' },
  { text: '启用', value: 'on' },
  { text: '关闭', value: 'off' },
]

const aiModeLabel = computed(
  () => aiModeOptions.find((o) => o.value === form.aiEnabled)?.text || '跟随全局设置',
)

function onAIConfirm({ selectedValues }: any) {
  form.aiEnabled = selectedValues[0] as any
  showAIPicker.value = false
}

const streamColumns = computed(() => streams.value.map((s) => ({ text: s.name, value: s.id })))
const streamNameOf = (id: string) => streams.value.find((s) => s.id === id)?.name || id
const previewStreamName = computed(() => streamNameOf(form.previewStream))
const recordStreamName = computed(() => streamNameOf(form.recordStream))

function onPreviewConfirm({ selectedValues }: any) {
  form.previewStream = selectedValues[0]
  showPreviewPicker.value = false
}

function onRecordConfirm({ selectedValues }: any) {
  form.recordStream = selectedValues[0]
  showRecordPicker.value = false
}

const scheduleLabel = computed(() => `${form.scheduleStart} - ${form.scheduleEnd}`)

function onModeConfirm({ selectedValues }: any) {
  form.recordMode = selectedValues[0] as any
  showModePicker.value = false
}

function onScheduleConfirm({ selectedValues }: any) {
  form.scheduleStart = `${selectedValues[0]}:${selectedValues[1]}`
  form.scheduleEnd = `${selectedValues[2]}:${selectedValues[3]}`
  showSchedulePicker.value = false
}

function close() {
  emit('update:show', false)
}

function reset() {
  mode.value = 'form'
  isEdit.value = false
  testing.value = false
  testResult.value = null
  testInfo.value = null
  form.name = ''
  form.ip = ''
  form.port = 554
  form.username = 'admin'
  form.password = ''
  form.rtspUrl = ''
  form.recordEnabled = true
  form.recordMode = 'continuous'
  form.scheduleStart = '08:00'
  form.scheduleEnd = '20:00'
  form.retentionDays = ''
  form.retentionSizeGB = ''
  form.aiEnabled = 'default'
  form.previewStream = ''
  form.recordStream = ''
  streams.value = []
  selectedIp.value = ''
  brand.value = 'auto'
  channel.value = 1
  brandMainUrl.value = ''
  brandSubUrl.value = ''
}

/** 加载品牌模板（只拉一次） */
async function loadBrands() {
  if (brands.value.length || !isBackend()) return
  try {
    brands.value = await fetchBrands()
  } catch {
    /* 拉不到就退化为「自定义地址」，不阻断添加流程 */
  }
}

const currentBrand = computed(() => brands.value.find((b) => b.id === brand.value))
const brandColumns = computed(() => brands.value.map((b) => ({ text: b.name, value: b.id })))

/** 选好品牌/通道后预览将要使用的地址 */
async function refreshBrandUrl() {
  if (!isBackend() || !form.ip.trim()) {
    brandMainUrl.value = ''
    brandSubUrl.value = ''
    return
  }
  if (brand.value === 'auto' || brand.value === 'custom') {
    brandMainUrl.value = ''
    brandSubUrl.value = ''
    return
  }
  try {
    const r = await buildBrandUrl({
      brand: brand.value,
      ip: form.ip.trim(),
      port: Number(form.port) || 554,
      username: form.username.trim(),
      password: form.password,
      channel: Math.max(1, Number(channel.value) || 1),
    })
    brandMainUrl.value = r.main
    brandSubUrl.value = r.sub
    // 选中品牌且通道有效时，直接用生成的地址，省去「测试连接」再回填
    if (r.main) form.rtspUrl = r.main
  } catch {
    brandMainUrl.value = ''
    brandSubUrl.value = ''
  }
}

function onBrandConfirm({ selectedValues }: any) {
  brand.value = selectedValues[0]
  showBrandPicker.value = false
  refreshBrandUrl()
}

async function onFetchStreams() {
  if (!form.ip.trim()) {
    showToast('请先填写 IP 地址')
    return
  }
  streamsLoading.value = true
  try {
    const list = await probeStreams({
      ip: form.ip.trim(),
      port: Number(form.port) || 554,
      username: form.username.trim(),
      password: form.password,
    })
    streams.value = list
    if (list.length) {
      if (!form.previewStream) form.previewStream = list[0].id
      if (!form.recordStream) form.recordStream = list[0].id
      showToast(`获取到 ${list.length} 路视频流`)
    } else {
      showToast('未发现视频流')
    }
  } catch {
    showToast('获取视频流失败')
  } finally {
    streamsLoading.value = false
  }
}

async function onTest() {
  if (!form.ip.trim()) {
    showToast('请先填写 IP 地址')
    return
  }
  testing.value = true
  testResult.value = null
  testInfo.value = null
  try {
    const res = await testDevice({
      ip: form.ip.trim(),
      port: Number(form.port) || 554,
      username: form.username.trim(),
      password: form.password,
      rtspUrl: form.rtspUrl || undefined,
    })
    testResult.value = res.ok ? 'ok' : 'fail'
    if (res.ok) {
      form.rtspUrl = res.url || ''
      testInfo.value = {
        codec: res.codec,
        width: res.width,
        height: res.height,
        advice: res.advice,
      }
      // 非 H.264 用醒目方式提示（浏览器播不了，是用户最容易踩的坑）
      if (res.h265 || (res.codec && res.codec !== 'h264')) {
        showToast(res.advice || `码流为 ${res.codec}，浏览器可能无法直接播放`)
      } else {
        showToast('连接成功')
      }
    } else {
      showToast(res.error || '连接失败')
    }
  } catch {
    testResult.value = 'fail'
    showToast('测试请求失败')
  } finally {
    testing.value = false
  }
}

/** 编码标识 → 用户可读名（与后端 codecName 保持一致的展示口径） */
const codecDisplay: Record<string, string> = {
  h264: 'H.264',
  hevc: 'H.265(HEVC)',
  mpeg4: 'MPEG-4',
  vp8: 'VP8',
  vp9: 'VP9',
  av1: 'AV1',
}
const testSummary = computed(() => {
  const t = testInfo.value
  if (!t) return ''
  const parts: string[] = []
  if (t.codec) parts.push(codecDisplay[t.codec] || t.codec.toUpperCase())
  if (t.width && t.height) parts.push(`${t.width}×${t.height}`)
  return parts.join(' · ')
})

// IP / 端口 / 账号变化后，若选了具体品牌则刷新预览地址
watch(
  () => [form.ip, form.port, form.username] as const,
  () => {
    if (brand.value !== 'auto' && brand.value !== 'custom') refreshBrandUrl()
  },
)

watch(
  () => props.editDevice,
  (d) => {
    if (d) applyDevice(d)
  },
  { immediate: true },
)

function submitForm() {
  if (!form.name.trim() || !form.ip.trim()) {
    showToast('请填写设备名称和 IP 地址')
    return
  }
  const input: Partial<Device> = {
    name: form.name.trim(),
    ip: form.ip.trim(),
    port: Number(form.port) || 554,
    username: form.username.trim() || 'admin',
    source: 'rtsp',
  }
  if (form.rtspUrl) input.rtspUrl = form.rtspUrl
  if (form.password) input.password = form.password
  input.recordEnabled = form.recordEnabled
  input.recordMode = form.recordMode
  input.scheduleStart = form.scheduleStart
  input.scheduleEnd = form.scheduleEnd
  input.retentionDays = Math.max(0, Math.round(Number(form.retentionDays) || 0))
  input.retentionSizeGB = Math.max(0, Math.round(Number(form.retentionSizeGB) || 0))
  input.aiEnabled = form.aiEnabled === 'default' ? undefined : form.aiEnabled === 'on'
  if (streams.value.length) input.streams = streams.value
  if (form.previewStream) input.previewStream = form.previewStream
  if (form.recordStream) input.recordStream = form.recordStream
  emit('add', input)
  close()
}

function submitDiscover() {
  if (!selectedIp.value) {
    showToast('请选择要添加的设备')
    return
  }
  const d = props.devices.find((x) => x.ip === selectedIp.value)
  if (d) {
    // Pre-fill the form with the discovered device (RTSP port normalized to
    // 554 unless an explicit RTSP port was reported) so the user can enter
    // credentials before saving.
    form.name = d.name === d.ip ? '' : d.name
    form.ip = d.ip
    form.port = d.port === 80 || d.port === 8080 || d.port === 2020 ? 554 : d.port
    form.username = 'admin'
    form.password = ''
    // 小米摄像头通常使用非标准 RTSP 端口(8554)与型号专属路径，
    // 直接采用探测到的地址可免去手工试路径
    form.rtspUrl = d.rtspUrl || ''
    mode.value = 'form'
    showToast(d.rtspUrl ? '已填入探测到的 RTSP 地址，请填写密码后保存' : '请填写摄像头密码后保存')
  }
}

function switchDiscover() {
  mode.value = 'discover'
  emit('discover')
}

function onOpen() {
  loadBrands()
  if (props.editDevice) {
    // Editing: re-apply the device (watch already ran, but onOpen fires after
    // on some popup transitions — be safe).
    applyDevice(props.editDevice)
    return
  }
  reset()
  mode.value = 'form'
}

function applyDevice(d: Device) {
  isEdit.value = true
  brand.value = 'custom' // 已有设备地址是确定的，默认按自定义展示，避免误改
  brandMainUrl.value = ''
  brandSubUrl.value = ''
  loadBrands()
  mode.value = 'form'
  form.name = d.name
  form.ip = d.ip
  form.port = d.port || 554
  form.username = d.username || 'admin'
  form.password = ''
  form.rtspUrl = d.rtspUrl || ''
  form.recordEnabled = d.recordEnabled ?? true
  form.recordMode = (d.recordMode || 'continuous') as 'continuous' | 'motion' | 'schedule'
  form.scheduleStart = d.scheduleStart || '08:00'
  form.scheduleEnd = d.scheduleEnd || '20:00'
  form.retentionDays = String(d.retentionDays ?? 0)
  form.retentionSizeGB = String(d.retentionSizeGB ?? 0)
  form.aiEnabled = d.aiEnabled === undefined ? 'default' : d.aiEnabled ? 'on' : 'off'
  streams.value = d.streams || []
  form.previewStream = d.previewStream || streams.value[0]?.id || ''
  form.recordStream = d.recordStream || streams.value[0]?.id || ''
}
</script>

<template>
  <van-popup
    :show="show"
    position="bottom"
    round
    :style="{ maxHeight: '82%' }"
    @update:show="emit('update:show', $event)"
    @open="onOpen"
    @closed="close"
  >
    <div class="header">
      <span>{{ isEdit ? '编辑设备' : mode === 'form' ? '添加设备' : '自动发现' }}</span>
      <van-icon name="cross" size="18" @click="close" />
    </div>

    <div class="body">
      <template v-if="mode === 'form'">
        <div class="form">
          <van-cell-group inset>
            <van-field v-model="form.name" label="设备名称" placeholder="如：大厅摄像头" />
            <van-field v-model="form.ip" label="IP 地址" placeholder="192.168.1.100" />
            <van-field v-model="form.port" type="number" label="RTSP端口" placeholder="554" />
            <van-field v-model="form.username" label="用户名" placeholder="admin" />
            <van-field
              v-model="form.password"
              type="password"
              :label="isEdit ? '新密码（留空不改）' : '密码'"
              :placeholder="isEdit ? '留空则不修改' : '******'"
            />
          </van-cell-group>

          <!-- 品牌模板：接 NVR 通道时按品牌+通道号自动拼地址，免手算 -->
          <van-cell-group v-if="!isEdit" inset title="品牌与通道">
            <van-field
              :model-value="currentBrand?.name || '自动探测（推荐）'"
              is-link
              readonly
              label="摄像头品牌"
              @click="showBrandPicker = true"
            />
            <van-field
              v-if="currentBrand?.needCh"
              v-model="channel"
              type="number"
              label="通道号"
              :placeholder="currentBrand?.chHint || '1'"
              @blur="refreshBrandUrl"
            />
            <van-cell v-if="currentBrand?.note" :label="currentBrand.note" />
            <van-cell
              v-if="brandMainUrl"
              title="将使用的地址"
              :label="brandMainUrl"
            >
              <template #right-icon>
                <van-icon name="passed" color="#2ecc8f" />
              </template>
            </van-cell>
            <van-cell v-if="brandSubUrl" title="子码流" :label="brandSubUrl" />
          </van-cell-group>

          <van-popup v-model:show="showBrandPicker" position="bottom" round>
            <van-picker
              title="摄像头品牌"
              :columns="brandColumns"
              :model-value="[brand]"
              @confirm="onBrandConfirm"
              @cancel="showBrandPicker = false"
            />
          </van-popup>

          <van-cell-group inset title="录像策略">
            <van-cell title="启用录像" label="关闭后仅直播不录像">
              <template #right-icon>
                <van-switch v-model="form.recordEnabled" />
              </template>
            </van-cell>
            <van-field
              :model-value="modeLabel"
              is-link
              readonly
              label="录像模式"
              placeholder="连续录制"
              @click="showModePicker = true"
            />
            <van-field
              v-if="form.recordMode === 'schedule'"
              v-model="scheduleLabel"
              is-link
              readonly
              label="定时时段"
              @click="showSchedulePicker = true"
            />
            <van-field
              v-model="aiModeLabel"
              is-link
              readonly
              label="AI 智能识别"
              placeholder="跟随全局"
              @click="showAIPicker = true"
            />
            <van-cell
              title="存储限额（仅本摄像头）"
              label="天数为 0 跟随全局保留天数；容量为 0 不单独限制，仍受全局总限额约束"
            />
            <van-field
              v-model="form.retentionDays"
              type="number"
              label="保留天数"
              placeholder="0 = 跟随全局"
            />
            <van-field
              v-model="form.retentionSizeGB"
              type="number"
              label="容量限额"
              placeholder="0 = 不单独限制（GB）"
            />
          </van-cell-group>

          <van-popup v-model:show="showAIPicker" position="bottom" round>
            <van-picker
              title="AI 智能识别"
              :columns="aiModeOptions"
              :model-value="[form.aiEnabled]"
              @confirm="onAIConfirm"
              @cancel="showAIPicker = false"
            />
          </van-popup>

          <van-cell-group inset title="视频流">
            <van-cell title="获取可用视频流" label="读取摄像头 ONVIF 多码流列表">
              <template #right-icon>
                <van-button size="small" round :loading="streamsLoading" @click="onFetchStreams">
                  获取
                </van-button>
              </template>
            </van-cell>
            <template v-if="streams.length">
              <van-field
                v-model="previewStreamName"
                is-link
                readonly
                label="预览码流"
                :placeholder="'共 ' + streams.length + ' 路'"
                @click="showPreviewPicker = true"
              />
              <van-field
                v-model="recordStreamName"
                is-link
                readonly
                label="录像码流"
                :placeholder="'共 ' + streams.length + ' 路'"
                @click="showRecordPicker = true"
              />
            </template>
          </van-cell-group>

          <van-popup v-model:show="showPreviewPicker" position="bottom" round>
            <van-picker
              title="预览码流"
              :columns="streamColumns"
              :model-value="[form.previewStream]"
              @confirm="onPreviewConfirm"
              @cancel="showPreviewPicker = false"
            />
          </van-popup>
          <van-popup v-model:show="showRecordPicker" position="bottom" round>
            <van-picker
              title="录像码流"
              :columns="streamColumns"
              :model-value="[form.recordStream]"
              @confirm="onRecordConfirm"
              @cancel="showRecordPicker = false"
            />
          </van-popup>

          <van-popup v-model:show="showModePicker" position="bottom" round>
            <van-picker
              title="录像模式"
              :columns="modeOptions"
              :model-value="[form.recordMode]"
              @confirm="onModeConfirm"
              @cancel="showModePicker = false"
            />
          </van-popup>
          <van-popup v-model:show="showSchedulePicker" position="bottom" round>
            <van-time-picker
              v-model="timePick"
              title="定时时段"
              :min-hour="0"
              :max-hour="23"
              @confirm="onScheduleConfirm"
              @cancel="showSchedulePicker = false"
            />
          </van-popup>
          <van-button
            v-if="isBackend() && !isEdit"
            plain
            block
            round
            style="margin-top: 12px"
            :loading="testing"
            @click="onTest"
          >
            <van-icon name="wifi-o" style="margin-right: 4px" />
            测试连接
          </van-button>
          <van-tag v-if="testResult === 'ok'" type="success" style="margin-top: 8px; display: inline-block">
            连接正常{{ testSummary ? ' · ' + testSummary : '' }}
          </van-tag>
          <van-tag v-else-if="testResult === 'fail'" type="danger" style="margin-top: 8px; display: inline-block">
            连接失败
          </van-tag>
          <!-- 编码非 H.264 时的可操作提示：说明后果 + 修改路径 -->
          <div v-if="testInfo?.advice" class="codec-advice">
            <van-icon name="warning-o" size="14" />
            <span>{{ testInfo.advice }}</span>
          </div>
          <van-button type="primary" block round style="margin-top: 14px" @click="submitForm">
            保存
          </van-button>
          <van-button v-if="!isEdit" plain block round style="margin-top: 10px" @click="switchDiscover">
            <van-icon name="search" style="margin-right: 4px" />
            自动发现局域网设备
          </van-button>
        </div>
      </template>

      <template v-else>
        <div class="discover-tip">
          <van-loading v-if="discovering" size="18" style="margin-right: 8px" />
          <span v-if="discovering">正在扫描局域网 ONVIF 设备...</span>
          <span v-else>发现 {{ devices.length }} 台设备</span>
        </div>
        <van-radio-group v-model="selectedIp">
          <van-cell-group inset>
            <van-cell
              v-for="d in devices"
              :key="d.ip"
              clickable
              @click="selectedIp = d.ip"
            >
              <template #title>
                <div class="dev-title">
                  <span class="dev-name">{{ d.name }}</span>
                  <VendorBadge :vendor="d.vendor" :source="d.vendorSource" />
                </div>
              </template>
              <template #label>
                <div class="dev-label">
                  <span class="dev-addr">{{ d.ip }}:{{ d.port }}</span>
                  <span v-if="d.hardware" class="dev-extra">{{ d.hardware }}</span>
                  <span v-if="d.mac" class="dev-extra dev-mac">{{ d.mac }}</span>
                </div>
              </template>
              <template #right-icon>
                <van-radio :name="d.ip" @click.stop />
              </template>
            </van-cell>
          </van-cell-group>
        </van-radio-group>
        <van-button type="primary" block round style="margin-top: 14px" @click="submitDiscover">
          添加所选设备
        </van-button>
      </template>
    </div>
  </van-popup>
</template>

<style scoped>
.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px;
  font-weight: 600;
  font-size: 16px;
}
.body {
  padding: 0 10px 24px;
}
/* H.265 等非 H.264 码流的引导提示 */
.codec-advice {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  margin-top: 8px;
  padding: 8px 10px;
  border-radius: 8px;
  background: rgba(255, 176, 32, 0.12);
  border: 1px solid rgba(255, 176, 32, 0.4);
  color: var(--nvr-amber, #ffb054);
  font-size: 12px;
  line-height: 1.6;
}
.codec-advice .van-icon {
  margin-top: 3px;
  flex-shrink: 0;
}
.discover-tip {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 10px 0 14px;
  font-size: 13px;
  color: var(--nvr-text-2);
}
/* 发现结果一行：设备名 + 厂商徽章 */
.dev-title {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  min-width: 0;
}
.dev-name {
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.dev-addr {
  font-variant-numeric: tabular-nums;
}
/* 发现结果的次级信息：型号、MAC */
.dev-label {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  row-gap: 2px;
}
.dev-extra {
  color: var(--nvr-text-3, #969799);
}
.dev-mac {
  font-variant-numeric: tabular-nums;
  font-size: 11px;
}
</style>
