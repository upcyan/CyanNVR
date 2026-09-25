<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { showConfirmDialog, showToast } from 'vant'
import type { EventItem } from '../types'
import { deleteEvent, fetchEvents, isBackend, isDemoMode, mediaURL } from '../api'
import { downloadEventSnapshotURL, downloadEventGIFURL } from '../api'
import { useAuthStore } from '../stores/auth'
import { useDeviceStore } from '../stores/devices'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const deviceStore = useDeviceStore()

// entering from the tabbar there may be no history to go back to
function goBack() {
  if (window.history.state.back == null) router.replace('/live')
  else router.back()
}

const deviceId = ref('')
const dateStr = ref('')
const typeFilter = ref('')
const events = ref<EventItem[]>([])
const total = ref(0)
const loading = ref(false)
const loadingMore = ref(false)
const PAGE_SIZE = 50

const typeMap: Record<string, { text: string; cls: string; icon: string }> = {
  motion: { text: '移动', cls: 'motion', icon: 'aim' },
  ai: { text: 'AI', cls: 'ai', icon: 'underway-o' },
  offline: { text: '离线', cls: 'offline', icon: 'close' },
  online: { text: '上线', cls: 'online', icon: 'success' },
  manual: { text: '手动', cls: 'manual', icon: 'records-o' },
}

const typeChips = computed(() => [
  { value: '', text: '全部' },
  ...Object.entries(typeMap).map(([value, m]) => ({ value, text: m.text })),
])

function toggleType(v: string) {
  typeFilter.value = typeFilter.value === v ? '' : v
}

const today = new Date()
const p2 = (n: number) => String(n).padStart(2, '0')

const filterOpen = ref<string[]>([])
const showDevicePicker = ref(false)
const showDatePicker = ref(false)
const datePickModel = ref([p2(today.getFullYear()), p2(today.getMonth() + 1), p2(today.getDate())])

const deviceColumns = computed(() => [
  { text: '全部设备', value: '' },
  ...deviceStore.devices.map(d => ({ text: d.name || d.ip, value: d.id })),
])

function onDevicePick({ selectedValues }: any) {
  deviceId.value = selectedValues[0] || ''
  showDevicePicker.value = false
}

function onDatePick({ selectedValues }: any) {
  dateStr.value = selectedValues.join('-')
  showDatePicker.value = false
}

async function load() {
  loading.value = true
  try {
    const res = await fetchEvents(deviceId.value, dateStr.value, typeFilter.value, 0, PAGE_SIZE)
    events.value = res.events
    total.value = res.total
  } finally {
    loading.value = false
  }
}

async function loadMore() {
  if (loadingMore.value || events.value.length >= total.value) return
  loadingMore.value = true
  try {
    const res = await fetchEvents(deviceId.value, dateStr.value, typeFilter.value, events.value.length, PAGE_SIZE)
    events.value = [...events.value, ...res.events]
    total.value = res.total
  } finally {
    loadingMore.value = false
  }
}

function fmtTime(ts: number) {
  const d = new Date(ts)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

async function onDelete(e: EventItem) {
  try {
    await showConfirmDialog({ title: '删除事件', message: '确定删除该事件吗？' })
  } catch {
    return
  }
  try {
    await deleteEvent(e.id)
    showToast('已删除')
    load()
  } catch (err: any) {
    showToast(err?.response?.data?.error || '删除失败')
  }
}

// 点缩略图/信息区看大图：原先 110x72 的缩略图根本看不清发生了什么
const showPreview = ref(false)
const previewEvent = ref<EventItem | null>(null)
const previewUrl = ref('')
const previewCap = ref('')

function onPreview(e: EventItem) {
  const src = e.gif || e.snapshot
  if (!src) return
  previewEvent.value = e
  previewUrl.value = mediaURL(src)
  previewCap.value = `${e.deviceName} · ${fmtTime(new Date(e.time).getTime())}`
  showPreview.value = true
}

watch([deviceId, dateStr, typeFilter], () => load())

onMounted(() => {
  deviceId.value = (route.query.device as string) || ''
  load()
})
</script>

<template>
  <div class="page events-page">
    <van-nav-bar title="事件记录" left-arrow @click-left="goBack">
      <template #right>
        <span class="reload" @click="load">刷新</span>
      </template>
    </van-nav-bar>

    <div class="type-chips">
      <button
        v-for="t in typeChips"
        :key="t.value"
        class="chip"
        :class="{ on: typeFilter === t.value }"
        @click="toggleType(t.value)"
      >
        {{ t.text }}
      </button>
    </div>

    <van-collapse v-model="filterOpen" class="filter-collapse">
      <van-collapse-item name="filter">
        <template #title>
          <van-icon name="filter-o" style="margin-right: 4px" />
          筛选设备与日期
        </template>
        <div class="filter-row">
          <div class="filter-item">
            <van-field
              :model-value="deviceColumns.find(d => d.value === deviceId)?.text || '全部设备'"
              label="筛选设备"
              is-link
              readonly
              placeholder="全部设备"
              @click="showDevicePicker = true"
              @keydown.enter.prevent="showDevicePicker = true"
              @keydown.space.prevent="showDevicePicker = true"
            />
          </div>
          <div class="filter-item">
            <van-field
              v-model="dateStr"
              label="筛选日期"
              is-link
              readonly
              placeholder="全部日期"
              @click="showDatePicker = true"
              @keydown.enter.prevent="showDatePicker = true"
              @keydown.space.prevent="showDatePicker = true"
            />
          </div>
          <van-button v-if="deviceId || dateStr" plain size="small" @click="deviceId = ''; dateStr = ''">
            清除筛选
          </van-button>
        </div>
      </van-collapse-item>
    </van-collapse>

    <van-loading v-if="loading" class="loading" />

    <div v-else class="list">
      <div v-for="e in events" :key="e.id" class="ev-card">
        <div class="media" @click="onPreview(e)">
          <img
            v-if="e.gif"
            :src="mediaURL(e.gif)"
            alt="gif"
            class="gif"
            loading="lazy"
          />
          <img v-else-if="e.snapshot" :src="mediaURL(e.snapshot)" alt="snap" class="gif" loading="lazy" />
          <div v-else class="ph">
            <van-icon :name="typeMap[e.type]?.icon || 'records-o'" size="30" />
          </div>
          <span class="zoom" title="查看大图"><van-icon name="preview-o" size="16" /></span>
        </div>
        <div class="info" @click="onPreview(e)">
          <div class="row">
            <span class="badge" :class="typeMap[e.type]?.cls">{{ typeMap[e.type]?.text || e.type }}</span>
            <span class="name">{{ e.deviceName }}</span>
          </div>
          <div class="desc">{{ e.description || e.label || '事件记录' }}</div>
          <div class="time mono">{{ fmtTime(e.time) }}</div>
        </div>
        <button
          v-if="(isBackend() || isDemoMode()) && auth.canEdit"
          type="button"
          class="del control-button"
          :aria-label="'删除' + e.deviceName + '的事件'"
          @click="onDelete(e)"
        ><van-icon name="delete-o" /></button>
        <div class="dl-btns">
          <a v-if="e.snapshot" :href="downloadEventSnapshotURL(e.id)" class="dl-btn" title="下载截图" @click.stop>
            <van-icon name="down" size="14" />
          </a>
          <a v-if="e.gif" :href="downloadEventGIFURL(e.id)" class="dl-btn" title="下载 GIF" @click.stop>
            <van-icon name="down" size="14" />
          </a>
        </div>
      </div>

    <div v-if="!loading && !events.length" class="empty">
      <van-icon name="records-o" size="46" color="#3a4252" />
      <p>暂无事件</p>
      <van-button v-if="deviceId || dateStr || typeFilter" plain @click="deviceId = ''; dateStr = ''; typeFilter = ''">清除筛选</van-button>
    </div>

    <div v-if="!loading && events.length" class="load-more">
      <span v-if="events.length >= total" class="all-loaded">已加载全部 {{ total }} 条</span>
      <van-button v-else size="small" plain round :loading="loadingMore" @click="loadMore">
        加载更多（{{ events.length }}/{{ total }}）
      </van-button>
    </div>

    <van-popup v-model:show="showPreview" position="center" :style="{ maxWidth: '92vw', background: '#000', borderRadius: '12px', overflow: 'hidden' }">
      <img v-if="previewUrl" :src="previewUrl" class="preview-img" alt="事件大图" />
      <div class="preview-bar">
        <span class="preview-cap">{{ previewCap }}</span>
        <a v-if="previewEvent?.snapshot" :href="downloadEventSnapshotURL(previewEvent.id)" class="dl-btn" title="下载截图">
          <van-icon name="down" size="16" />
        </a>
        <a v-if="previewEvent?.gif" :href="downloadEventGIFURL(previewEvent.id)" class="dl-btn" title="下载 GIF">
          <van-icon name="down" size="16" />
        </a>
        <button class="preview-close control-button" aria-label="关闭事件预览" @click="showPreview = false"><van-icon name="cross" /></button>
      </div>
    </van-popup>

    <van-popup v-model:show="showDevicePicker" position="bottom" round>
      <van-picker v-picker-desktop :option-height="56" :visible-option-num="5"
        :columns="deviceColumns"
        @confirm="onDevicePick"
        @cancel="showDevicePicker = false"
      />
    </van-popup>

    <van-popup v-model:show="showDatePicker" position="bottom" round>
      <van-date-picker v-picker-desktop :option-height="56" :visible-option-num="5"
        v-model="datePickModel"
        :min-date="new Date(2020, 0, 1)"
        :max-date="new Date()"
        @confirm="onDatePick"
        @cancel="showDatePicker = false"
      />
    </van-popup>
  </div>
  </div>
</template>

<style scoped>
.loading {
  display: flex;
  justify-content: center;
  padding: 60px 0;
}
.type-chips {
  display: flex;
  gap: 8px;
  padding: 10px 14px 0;
  overflow-x: auto;
}
.type-chips .chip {
  flex-shrink: 0;
  padding: 6px 14px;
  border-radius: 999px;
  border: 1px solid var(--nvr-border);
  background: var(--nvr-panel);
  color: var(--nvr-text-2);
  font-size: calc(12px * var(--nvr-font-scale, 1));
}
.type-chips .chip.on {
  background: rgba(46, 168, 255, 0.15);
  border-color: var(--nvr-accent);
  color: var(--nvr-accent);
}
.load-more {
  display: flex;
  justify-content: center;
  padding: 4px 0 16px;
}
.all-loaded {
  font-size: calc(12px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
}
.reload {
  font-size: calc(13px * var(--nvr-font-scale, 1));
  color: var(--nvr-accent);
}
.list {
  padding: 12px;
}
.ev-card {
  display: flex;
  gap: 12px;
  padding: 10px;
  border-radius: var(--nvr-radius);
  background: var(--nvr-panel);
  border: 1px solid var(--nvr-border);
  margin-bottom: 12px;
  position: relative;
}
/* 关怀模式：事件卡缩略图加大、删除/下载按钮触控区放大 */
:global(body.care .ev-card) {
  padding: 14px;
  gap: 14px;
}
:global(body.care .ev-card .media) {
  width: 140px;
  height: 92px;
}
:global(body.care .ev-card .badge) {
  font-size: calc(13px * var(--nvr-font-scale, 1));
  padding: 4px 10px;
}
:global(body.care .ev-card .del),
:global(body.care .ev-card .dl-btn) {
  width: 36px;
  height: 36px;
  font-size: calc(18px * var(--nvr-font-scale, 1));
}
.media {
  width: 110px;
  height: 72px;
  border-radius: 8px;
  overflow: hidden;
  flex-shrink: 0;
  background: #000;
  display: flex;
  align-items: center;
  justify-content: center;
  position: relative;
  cursor: zoom-in;
}
.media .zoom {
  position: absolute;
  right: 4px;
  bottom: 4px;
  width: 22px;
  height: 22px;
  border-radius: 50%;
  background: rgba(0, 0, 0, 0.55);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  opacity: 0;
  transition: opacity 0.15s;
  pointer-events: none;
}
.media:hover .zoom {
  opacity: 1;
}
.info {
  cursor: zoom-in;
}
/* 大图预览 */
.preview-img {
  max-width: 88vw;
  max-height: 76vh;
  display: block;
  background: #000;
}
.preview-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 12px;
  background: var(--nvr-panel);
  color: var(--nvr-text);
}
.preview-cap {
  flex: 1;
  min-width: 0;
  font-size: calc(12px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.preview-close {
  font-size: calc(18px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
  cursor: pointer;
  padding: 4px;
}
.gif {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
.ph {
  color: var(--nvr-text-2);
}
.info {
  flex: 1;
  padding-right: 44px;
  padding-bottom: 34px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.badge {
  font-size: calc(11px * var(--nvr-font-scale, 1));
  padding: 2px 8px;
  border-radius: 999px;
  color: #fff;
  flex-shrink: 0;
}
.badge.motion { background: var(--nvr-accent); }
.badge.ai { background: #9c5cff; }
.badge.offline { background: var(--nvr-red); }
.badge.online { background: var(--nvr-green); }
.badge.manual { background: var(--nvr-text-2); }
.name {
  font-size: calc(14px * var(--nvr-font-scale, 1));
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.desc {
  font-size: calc(12px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.time {
  overflow-wrap: anywhere;
  font-size: calc(11px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
}
.del {
  position: absolute;
  right: 10px;
  bottom: 10px;
  color: var(--nvr-text-2);
}
.del:active {
  color: var(--nvr-red);
}
.dl-btns {
  position: absolute;
  right: 10px;
  top: 10px;
  display: flex;
  gap: 6px;
}
.dl-btn {
  width: 24px;
  height: 24px;
  border-radius: 50%;
  background: var(--nvr-panel-2);
  border: 1px solid var(--nvr-border);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--nvr-text-2);
  text-decoration: none;
}
@media (max-width: 600px) {
  .ev-card {
    display: grid;
    grid-template-columns: 96px minmax(0, 1fr);
    gap: 10px;
  }
  .ev-card .media { grid-column: 1; grid-row: 1; width: 100%; }
  .ev-card .info { grid-column: 2; grid-row: 1; padding: 0; }
  .ev-card .row { flex-wrap: wrap; gap: 6px; }
  .ev-card .name { white-space: normal; overflow-wrap: anywhere; }
  .ev-card .del { position: static; grid-column: 2; grid-row: 2; justify-self: end; }
  .ev-card .dl-btns { position: static; grid-column: 1; grid-row: 2; }
  :global(body.care .events-page .ev-card .media) { width: 100%; }
  :global(body.care .events-page .ev-card .del),
  :global(body.care .events-page .ev-card .dl-btn) { width: 44px; height: 44px; }
}
.dl-btn:active {
  color: var(--nvr-accent);
}
.empty {
  text-align: center;
  padding: 60px 0;
  color: var(--nvr-text-2);
}
.empty p {
  font-size: calc(13px * var(--nvr-font-scale, 1));
}
.filter-collapse {
  margin: 0 12px 12px;
  border-radius: var(--nvr-radius);
  overflow: hidden;
}
.filter-row {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.filter-item {
  flex: 1;
  min-width: 120px;
}
.filter-item label {
  font-size: calc(12px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
  margin-bottom: 2px;
  display: block;
}
</style>
