<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { showConfirmDialog, showToast } from 'vant'
import type { Device, DiscoveredDevice } from '../types'
import { useDeviceStore } from '../stores/devices'
import { isBackend, isDemoMode, liveStreamUrl } from '../api'
import CameraCard from '../components/CameraCard.vue'
import LiveViewer from '../components/LiveViewer.vue'
import VideoGrid from '../components/VideoGrid.vue'
import AddDevice from '../components/AddDevice.vue'

const store = useDeviceStore()
const router = useRouter()

const showAdd = ref(false)
const discovering = ref(false)
const foundDevices = ref<DiscoveredDevice[]>([])
const showMulti = ref(false)
const gridCols = ref(0) // 0 = auto square layout
const layoutOptions = [
  { label: '自适应', value: 0 },
  { label: '1', value: 1 },
  { label: '4', value: 4 },
  { label: '9', value: 9 },
]
const viewerDevice = ref<Device | null>(null)
const showViewer = ref(false)
const showActions = ref(false)
const actionDevice = ref<Device | null>(null)
const editingDevice = ref<Device | null>(null)

const onlineDevices = computed(() => store.devices.filter((d) => d.online))
const streamUrlFor = (d: Device) => (isBackend() && !isDemoMode() ? liveStreamUrl(d.id) : undefined)

const actionItems = computed(() => [
  { name: '历史回放', icon: 'clock-o' },
  { name: '编辑设备', icon: 'edit' },
  { name: '删除设备', icon: 'delete-o', color: '#ff5d5d' },
])

function openViewer(d: Device) {
  viewerDevice.value = d
  showViewer.value = true
}

function openActions(d: Device) {
  actionDevice.value = d
  showActions.value = true
}

async function onActionSelect(action: { name: string }) {
  showActions.value = false
  const d = actionDevice.value
  if (!d) return
  if (action.name === '历史回放') {
    router.push({ path: '/playback', query: { device: d.id } })
  } else if (action.name === '编辑设备') {
    editingDevice.value = d
    showAdd.value = true
  } else if (action.name === '删除设备') {
    try {
      await showConfirmDialog({
        title: '删除设备',
        message: `确定删除「${d.name}」吗？录像文件将一并清除。`,
      })
    } catch {
      return
    }
    await store.remove(d.id)
    showToast('已删除')
  }
}

async function onAdd(input: Partial<Device>) {
  if (editingDevice.value) {
    await store.update(editingDevice.value.id, input)
    showToast('设备已更新')
  } else {
    await store.add(input)
    showToast('设备已添加')
  }
  showAdd.value = false
  editingDevice.value = null
}

function onAddClose() {
  showAdd.value = false
  editingDevice.value = null
}

async function onDiscover() {
  discovering.value = true
  try {
    const list = await store.discover()
    const existing = new Set(store.devices.map((d) => d.ip))
    const fresh = list.filter((d) => !existing.has(d.ip))
    foundDevices.value = fresh.length ? fresh : list
  } finally {
    discovering.value = false
  }
}

function onViewerPlayback(d: Device) {
  showViewer.value = false
  router.push({ path: '/playback', query: { device: d.id } })
}
</script>

<template>
  <div class="page live2">
    <header class="hd">
      <h1>监控中心</h1>
      <div class="hd-actions">
        <button class="icon-btn" title="添加摄像机" @click="showAdd = true"><van-icon name="plus" size="22" /></button>
        <button class="icon-btn" title="设置" @click="router.push('/settings')"><van-icon name="setting-o" size="20" /></button>
        <button class="icon-btn" title="多画面预览" @click="showMulti = true"><van-icon name="apps-o" size="20" /></button>
      </div>
    </header>

    <div class="chips">
      <button class="chip" @click="showMulti = true">
        <van-icon name="video-o" size="15" color="#4da3ff" />
        多画面 {{ onlineDevices.length }}
      </button>
      <button class="chip" @click="router.push('/events')">
        <van-icon name="bell-o" size="15" color="#b78cff" />
        最近事件
      </button>
    </div>

    <div class="cards">
      <CameraCard
        v-for="d in store.devices"
        :key="d.id"
        :device="d"
        :stream-url="streamUrlFor(d)"
        @enter="openViewer"
        @play="openViewer"
        @more="openActions"
      />
      <div v-if="!store.devices.length" class="empty">
        <van-icon name="video-o" size="46" color="#3a4252" />
        <p>暂无设备</p>
        <p class="sub">点击右上角 + 添加摄像机</p>
      </div>
    </div>

    <AddDevice
      v-model:show="showAdd"
      :devices="foundDevices"
      :discovering="discovering"
      :edit-device="editingDevice"
      @add="onAdd"
      @discover="onDiscover"
      @update:show="!$event && onAddClose()"
    />

    <LiveViewer
      v-model:show="showViewer"
      :device="viewerDevice"
      :stream-url="viewerDevice ? streamUrlFor(viewerDevice) : undefined"
      @playback="onViewerPlayback"
    />

    <van-action-sheet
      v-model:show="showActions"
      :actions="actionItems"
      cancel-text="取消"
      close-on-click-action
      @select="onActionSelect"
    />

    <van-popup v-model:show="showMulti" position="right" :style="{ width: '100%', height: '100%', background: '#000' }">
      <div class="multi">
        <div class="mhd">
          <span>多画面</span>
          <div class="mhd-right">
            <div class="layout-switch">
              <button
                v-for="opt in layoutOptions"
                :key="opt.value"
                class="layout-btn"
                :class="{ on: gridCols === opt.value }"
                @click="gridCols = opt.value"
              >{{ opt.label }}</button>
            </div>
            <van-icon name="cross" size="20" @click="showMulti = false" />
          </div>
        </div>
        <VideoGrid :devices="onlineDevices" :cols="gridCols" :stream-url-for="streamUrlFor" @cell="openViewer" />
      </div>
    </van-popup>
  </div>
</template>

<style scoped>
.live2 {
  background: var(--nvr-bg);
}
.hd {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 18px 16px 12px;
}
.hd h1 {
  margin: 0;
  font-size: 22px;
  font-weight: 700;
}
.hd-actions {
  display: flex;
  gap: 6px;
}
.icon-btn {
  width: 38px;
  height: 38px;
  border-radius: 50%;
  border: none;
  background: transparent;
  color: var(--nvr-text);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}
.icon-btn:active {
  background: var(--nvr-panel-2);
}
.chips {
  display: flex;
  gap: 10px;
  padding: 0 16px 14px;
}
.chip {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 14px;
  border-radius: 10px;
  border: 1px solid var(--nvr-border);
  background: var(--nvr-panel);
  color: var(--nvr-text);
  font-size: 13px;
  cursor: pointer;
}
.chip:active {
  background: var(--nvr-panel-2);
}
.cards {
  flex: 1;
  padding-top: 2px;
}
.empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 60px 0;
  color: var(--nvr-text-2);
}
.empty p {
  margin: 12px 0 0;
  font-size: 15px;
}
.empty .sub {
  margin-top: 6px;
  font-size: 12px;
}
.multi {
  height: 100%;
  display: flex;
  flex-direction: column;
}
.mhd {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px;
  color: #fff;
  font-size: 16px;
  font-weight: 600;
}
.mhd-right {
  display: flex;
  align-items: center;
  gap: 12px;
}
.layout-switch {
  display: flex;
  gap: 2px;
  background: rgba(255, 255, 255, 0.08);
  border-radius: 8px;
  padding: 2px;
}
.layout-btn {
  border: none;
  background: transparent;
  color: rgba(255, 255, 255, 0.65);
  font-size: 12px;
  padding: 5px 10px;
  border-radius: 6px;
  cursor: pointer;
}
.layout-btn.on {
  background: rgba(46, 168, 255, 0.9);
  color: #fff;
}

@media (min-width: 900px) {
  .hd {
    padding: 22px 24px 14px;
  }
  .chips {
    padding: 0 24px 16px;
  }
  .cards {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(380px, 1fr));
    gap: 16px;
    padding: 0 24px 20px;
    align-content: start;
  }
  .cards :deep(.cam-card) {
    margin: 0;
  }
}

@media (hover: hover) {
  .icon-btn:hover {
    background: var(--nvr-panel-2);
  }
  .chip:hover {
    background: var(--nvr-panel-2);
    border-color: rgba(255, 255, 255, 0.14);
  }
}
</style>
