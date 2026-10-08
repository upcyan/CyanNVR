<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { showConfirmDialog, showToast } from 'vant'
import type { Device, DiscoveredDevice } from '../types'
import { useDeviceStore } from '../stores/devices'
import { useAuthStore } from '../stores/auth'
import { fetchRecordPause, isBackend, isDemoMode, liveStreamUrl, refreshConnection, setRecordPause } from '../api'
import { server } from '../api/server'
import { isNvrApp } from '../utils/env'
const sourceLabel = computed(() => server.base || window.location.origin)
async function retryDevices() {
  if (!isBackend()) await refreshConnection()
  await store.load()
}
import CameraCard from '../components/CameraCard.vue'
import LiveViewer from '../components/LiveViewer.vue'
import VideoGrid from '../components/VideoGrid.vue'
import AddDevice from '../components/AddDevice.vue'

const store = useDeviceStore()
const router = useRouter()
const auth = useAuthStore()

// ── 全局暂停录制（仅管理员可见）──
//
// 暂停 = 停止监控：直播与录像一起停，设备随即显示离线。
// 因此点击前必须确认——这是高影响操作，误触会让用户以为摄像头坏了。
const paused = ref(false)
const pauseBusy = ref(false)
const canPause = computed(() => auth.isAdmin && isBackend() && !isDemoMode())

async function loadPauseState() {
  if (!canPause.value) return
  try {
    const st = await fetchRecordPause()
    paused.value = st.paused
  } catch {
    /* 读不到不影响其它功能 */
  }
}

async function togglePause() {
  if (pauseBusy.value) return
  const next = !paused.value
  if (next) {
    try {
      await showConfirmDialog({
        title: '暂停录制',
        message: '将停止所有摄像机的直播与录像，设备会显示为离线。\n确认暂停？',
        confirmButtonText: '暂停录制',
        confirmButtonColor: '#ee0a24',
        cancelButtonText: '取消',
      })
    } catch {
      return // 用户取消
    }
  }
  pauseBusy.value = true
  try {
    const st = await setRecordPause(next)
    paused.value = st.paused
    showToast(st.paused ? '已暂停录制' : '已恢复录制')
    // 重新拉设备列表让 online 状态立即刷新（暂停后应显示离线）
    await store.refresh()
  } catch {
    showToast(next ? '暂停失败，请重试' : '恢复失败，请重试')
    await loadPauseState()
  } finally {
    pauseBusy.value = false
  }
}

onMounted(loadPauseState)

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

async function onAdd(input: Partial<Device>, force = false) {
  try {
    if (editingDevice.value) {
      await store.update(editingDevice.value.id, input, { force })
      showToast('设备已更新')
    } else {
      await store.add(input, { force })
      showToast('设备已添加')
    }
    showAdd.value = false
    editingDevice.value = null
  } catch (err: any) {
    // 后端 409 = 疑似重复添加（同 IP/RTSP 已存在），弹二次确认后带 force 重试
    if (err?.response?.status === 409) {
      const msg = err.response.data?.error || '疑似重复添加同一台摄像机'
      try {
        await showConfirmDialog({
          title: '可能重复添加',
          message: `${msg}。同一台摄像机会重复占用取流路数，可能导致两边黑屏。仍要添加吗？`,
          confirmButtonText: '仍要添加',
          cancelButtonText: '取消',
        })
      } catch {
        return // 用户取消：弹窗保持打开，可修改后重存
      }
      try {
        if (editingDevice.value) {
          await store.update(editingDevice.value.id, input, { force: true })
        } else {
          await store.add(input, { force: true })
        }
        showAdd.value = false
        editingDevice.value = null
        showToast('已按确认保存')
      } catch (e2: any) {
        showToast(e2?.response?.data?.error || '保存失败')
      }
      return
    }
    showToast(err?.response?.data?.error || '保存失败')
  }
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
    <header class="hd" :class="{ 'app-bare': isNvrApp() }">
      <div class="hd-title" v-if="!isNvrApp()">
        <h1>监控中心</h1>
        <span class="hd-sub">在线 {{ onlineDevices.length }} / {{ store.devices.length }}</span>
      </div>
      <div class="hd-actions">
        <button
          v-if="canPause"
          class="icon-btn pause-btn"
          :class="{ paused }"
          :title="paused ? '恢复录制（重新开启直播与录像）' : '暂停录制（停止直播与录像）'"
          :aria-label="paused ? '恢复录制' : '暂停录制'"
          :aria-pressed="paused"
          :disabled="pauseBusy"
          @click="togglePause"
        >
          <van-icon :name="paused ? 'play-circle-o' : 'pause-circle-o'" size="22" />
        </button>
        <button class="icon-btn" title="添加摄像机" @click="showAdd = true"><van-icon name="plus" size="22" /></button>
        <button class="icon-btn" title="多画面预览" @click="showMulti = true"><van-icon name="apps-o" size="20" /></button>
      </div>
    </header>

    <!-- 暂停状态横幅：状态是全局的，必须一直可见，
         否则用户看到全部离线会以为设备故障 -->
    <div v-if="paused" class="pause-banner" role="status">
      <van-icon name="pause-circle-o" size="16" />
      <span>录制已暂停，直播与录像均已停止</span>
      <button type="button" class="pause-banner-btn" @click="togglePause">恢复录制</button>
    </div>

    <p class="data-source" role="status">{{ isDemoMode() ? '演示数据（非已添加设备）' : `当前服务器：${sourceLabel}` }}</p>
    <div class="cards">
      <CameraCard
        v-for="d in store.devices"
        :key="d.id"
        :device="d"
        :stream-url="streamUrlFor(d)"
        @enter="openViewer"
        @more="openActions"
      />
      <div v-if="store.loading && !store.devices.length" class="empty">
        <van-loading size="30" color="#2ea8ff" />
        <p>正在加载摄像机…</p>
      </div>
      <div v-else-if="store.status === 'error'" class="empty" role="alert">
        <p>摄像头读取失败，不代表没有已添加设备</p>
        <p class="sub">{{ store.error }}</p>
        <van-button @click="retryDevices">重试读取</van-button>
      </div>
      <div v-else-if="store.isEmpty" class="empty">
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

    <van-popup v-model:show="showMulti" position="right" teleport="body" :style="{ width: '100%', height: '100%', background: '#000' }">
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
            <button class="control-button" aria-label="关闭多画面" @click="showMulti = false"><van-icon name="cross" size="20" /></button>
          </div>
        </div>
        <VideoGrid :devices="onlineDevices" :cols="gridCols" :stream-url-for="streamUrlFor" @cell="openViewer" />
      </div>
    </van-popup>
  </div>
</template>

<style scoped>
.data-source { margin: 0; padding: 4px 16px 12px; color: var(--nvr-text-2); font-size: calc(12px * var(--nvr-font-scale, 1)); overflow-wrap: anywhere; }
.live2 {
  background: var(--nvr-bg);
}
.hd {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 18px 16px 12px;
}
/* 标题下的品牌强调线：与侧边栏 logo、在线率条同一渐变语言 */
.hd-title::after {
  content: '';
  display: block;
  width: 28px;
  height: 3px;
  margin-top: 7px;
  border-radius: var(--nvr-radius-full);
  background: var(--nvr-grad-accent);
}
.hd-title {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.hd h1 {
  margin: 0;
  font-size: calc(22px * var(--nvr-font-scale, 1));
  font-weight: 700;
  line-height: 1.2;
}
.hd-sub {
  font-size: calc(12px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
  white-space: nowrap;
}
.hd-actions {
  display: flex;
  gap: 6px;
}
/* App 内嵌环境：标题（监控中心 + 在线数）已由原生 TopAppBar 表达，
   隐藏后按钮右对齐，避免出现「原生标题 + 页面标题」双重标题。 */
.hd.app-bare {
  justify-content: flex-end;
  padding-top: 12px;
}
.icon-btn {
  width: 44px;
  height: 44px;
  border-radius: var(--nvr-radius-full);
  border: 1px solid var(--nvr-border);
  background: color-mix(in srgb, var(--nvr-panel) 72%, transparent);
  color: var(--nvr-text);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}
.icon-btn:active {
  background: var(--nvr-accent-soft);
  border-color: var(--nvr-accent);
}
/* 暂停录制按钮：暂停态用警示色，与普通图标按钮区分开 */
.icon-btn.pause-btn.paused {
  color: var(--nvr-red, #ff4d4f);
  border-color: var(--nvr-red, #ff4d4f);
  background: color-mix(in srgb, var(--nvr-red, #ff4d4f) 14%, transparent);
}
.icon-btn.pause-btn:disabled {
  opacity: 0.5;
  cursor: default;
}
/* 暂停横幅：全局状态需持续可见，否则用户看到全部离线会以为设备坏了 */
.pause-banner {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 8px 16px 0;
  padding: 8px 12px;
  border-radius: var(--nvr-radius-sm);
  background: color-mix(in srgb, var(--nvr-red, #ff4d4f) 14%, transparent);
  border: 1px solid color-mix(in srgb, var(--nvr-red, #ff4d4f) 45%, transparent);
  color: var(--nvr-text);
  font-size: calc(13px * var(--nvr-font-scale, 1));
}
.pause-banner span {
  flex: 1;
  min-width: 0;
}
.pause-banner-btn {
  flex: 0 0 auto;
  padding: 4px 12px;
  border: 1px solid var(--nvr-red, #ff4d4f);
  border-radius: var(--nvr-radius-full);
  background: transparent;
  color: var(--nvr-red, #ff4d4f);
  font-size: calc(12px * var(--nvr-font-scale, 1));
  cursor: pointer;
}
.pause-banner-btn:active {
  background: color-mix(in srgb, var(--nvr-red, #ff4d4f) 22%, transparent);
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
  font-size: calc(15px * var(--nvr-font-scale, 1));
}
.empty .sub {
  margin-top: 6px;
  font-size: calc(12px * var(--nvr-font-scale, 1));
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
  font-size: calc(16px * var(--nvr-font-scale, 1));
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
  border-radius: var(--nvr-radius-sm);
  padding: 2px;
}
.layout-btn {
  border: none;
  background: transparent;
  color: rgba(255, 255, 255, 0.65);
  font-size: calc(12px * var(--nvr-font-scale, 1));
  padding: 5px 10px;
  border-radius: var(--nvr-radius-sm);
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
    background: var(--nvr-accent-soft);
    border-color: rgba(46, 168, 255, 0.45);
  }
}
</style>
