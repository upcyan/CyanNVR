<script setup lang="ts">
import { reactive, ref } from 'vue'
import { showToast } from 'vant'
import type { Device } from '../types'

const props = defineProps<{
  show: boolean
  devices: Device[]
  discovering: boolean
}>()
const emit = defineEmits<{
  (e: 'update:show', v: boolean): void
  (e: 'add', input: Partial<Device>): void
  (e: 'discover'): void
}>()

const mode = ref<'form' | 'discover'>('form')
const form = reactive({
  name: '',
  ip: '',
  port: 554,
  username: 'admin',
  password: 'admin123',
})
const selectedId = ref('')

function close() {
  emit('update:show', false)
}

function reset() {
  mode.value = 'form'
  form.name = ''
  form.ip = ''
  form.port = 554
  form.username = 'admin'
  form.password = 'admin123'
  selectedId.value = ''
}

function submitForm() {
  if (!form.name.trim() || !form.ip.trim()) {
    showToast('请填写设备名称和 IP 地址')
    return
  }
  emit('add', {
    name: form.name.trim(),
    ip: form.ip.trim(),
    port: Number(form.port) || 554,
    username: form.username.trim() || 'admin',
    password: form.password,
  })
  close()
}

function submitDiscover() {
  if (!selectedId.value) {
    showToast('请选择要添加的设备')
    return
  }
  const d = props.devices.find((x) => x.id === selectedId.value)
  if (d) {
    emit('add', {
      name: d.name,
      ip: d.ip,
      port: d.port,
      username: d.username,
      password: d.password,
      model: d.model,
      rtspUrl: d.rtspUrl,
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
      <span>{{ mode === 'form' ? '添加设备' : '自动发现' }}</span>
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
            <van-field v-model="form.password" type="password" label="密码" placeholder="******" />
          </van-cell-group>
          <van-button type="primary" block round style="margin-top: 14px" @click="submitForm">
            保存
          </van-button>
          <van-button plain block round style="margin-top: 10px" @click="switchDiscover">
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
        <van-radio-group v-model="selectedId">
          <van-cell-group inset>
            <van-cell
              v-for="d in devices"
              :key="d.id"
              :title="d.name"
              :label="`${d.ip} · ${d.model}`"
              clickable
              @click="selectedId = d.id"
            >
              <template #right-icon>
                <van-radio :name="d.id" @click.stop />
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
