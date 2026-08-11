<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { showToast } from 'vant'
import type { Device, DiscoveredDevice } from '../types'
import { isBackend, testDevice } from '../api'

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
})
const selectedIp = ref('')

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
  selectedIp.value = ''
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
    if (d) {
      isEdit.value = true
      mode.value = 'form'
      form.name = d.name
      form.ip = d.ip
      form.port = d.port || 554
      form.username = d.username || 'admin'
      form.password = ''
      form.rtspUrl = d.rtspUrl || ''
    }
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
    emit('add', {
      name: d.name,
      ip: d.ip,
      port: d.port,
      username: 'admin',
      source: 'rtsp',
    })
  }
  close()
}

function switchDiscover() {
  mode.value = 'discover'
  emit('discover')
}

function onOpen() {
  reset()
  mode.value = 'form'
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
              :title="d.name"
              :label="`${d.ip}:${d.port}`"
              clickable
              @click="selectedIp = d.ip"
            >
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
</style>
