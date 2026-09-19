<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { showToast } from 'vant'
import type { Device, DiscoveredDevice, Stream } from '../types'
import { isBackend, probeStreams, testDevice } from '../api'
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
  aiEnabled: 'default' as 'default' | 'on' | 'off',
  previewStream: '',
  recordStream: '',
})
const selectedIp = ref('')
const streams = ref<Stream[]>([])
const streamsLoading = ref(false)
const showModePicker = ref(false)
const showSchedulePicker = ref(false)
const showPreviewPicker = ref(false)
const showRecordPicker = ref(false)
const showAIPicker = ref(false)
const timePick = ref<string[]>(['08', '00', '20', '00'])

const modeOptions = [
  { text: '连续录制', value: 'continuous' },
  { text: '移动侦测', value: 'motion' },
  { text: '定时录制', value: 'schedule' },
]

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
  form.aiEnabled = 'default'
  form.previewStream = ''
  form.recordStream = ''
  streams.value = []
  selectedIp.value = ''
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
  try {
    const res = await testDevice({
      ip: form.ip.trim(),
      port: Number(form.port) || 554,
      username: form.username.trim(),
      password: form.password,
    })
    testResult.value = res.ok ? 'ok' : 'fail'
    if (res.ok) {
      form.rtspUrl = res.url || ''
      showToast('连接成功')
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

          <van-cell-group inset title="录像策略">
            <van-cell title="启用录像" label="关闭后仅直播不录像">
              <template #right-icon>
                <van-switch v-model="form.recordEnabled" size="20" />
              </template>
            </van-cell>
            <van-field
              v-model="form.recordMode"
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
            连接正常
          </van-tag>
          <van-tag v-else-if="testResult === 'fail'" type="danger" style="margin-top: 8px; display: inline-block">
            连接失败
          </van-tag>
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
