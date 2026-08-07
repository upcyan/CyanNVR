<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { showConfirmDialog, showToast } from 'vant'
import type { EventItem } from '../types'
import { deleteEvent, fetchEvents, isBackend } from '../api'
import { useAuthStore } from '../stores/auth'

const route = useRoute()
const auth = useAuthStore()

const deviceId = ref('')
const events = ref<EventItem[]>([])
const loading = ref(false)

const typeMap: Record<string, { text: string; cls: string; icon: string }> = {
  motion: { text: '移动', cls: 'motion', icon: 'aim' },
  ai: { text: 'AI', cls: 'ai', icon: 'underway-o' },
  offline: { text: '离线', cls: 'offline', icon: 'close' },
  online: { text: '上线', cls: 'online', icon: 'success' },
  manual: { text: '手动', cls: 'manual', icon: 'records-o' },
}

async function load() {
  loading.value = true
  try {
    events.value = await fetchEvents(deviceId.value)
  } finally {
    loading.value = false
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

const imgBase = computed(() => {
  const base = (window as any).__NVR_BASE__ || ''
  return base
})

onMounted(() => {
  deviceId.value = (route.query.device as string) || ''
  load()
})
</script>

<template>
  <div class="page events-page">
    <van-nav-bar title="事件记录" left-arrow @click-left="$router.back()">
      <template #right>
        <span class="reload" @click="load">刷新</span>
      </template>
    </van-nav-bar>

    <van-loading v-if="loading" class="loading" />

    <div v-else class="list">
      <div v-for="e in events" :key="e.id" class="ev-card">
        <div class="media">
          <img
            v-if="e.gif"
            :src="(e.gif || '').replace('/api', imgBase + '/api')"
            alt="gif"
            class="gif"
          />
          <img v-else-if="e.snapshot" :src="(e.snapshot || '').replace('/api', imgBase + '/api')" alt="snap" class="gif" />
          <div v-else class="ph">
            <van-icon :name="typeMap[e.type]?.icon || 'records-o'" size="30" />
          </div>
        </div>
        <div class="info">
          <div class="row">
            <span class="badge" :class="typeMap[e.type]?.cls">{{ typeMap[e.type]?.text || e.type }}</span>
            <span class="name">{{ e.deviceName }}</span>
          </div>
          <div class="desc">{{ e.description || e.label || '事件记录' }}</div>
          <div class="time mono">{{ fmtTime(e.time) }}</div>
        </div>
        <van-icon
          v-if="isBackend() && auth.canEdit"
          name="delete-o"
          class="del"
          @click="onDelete(e)"
        />
      </div>

      <div v-if="!loading && !events.length" class="empty">
        <van-icon name="records-o" size="46" color="#3a4252" />
        <p>暂无事件</p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.loading {
  display: flex;
  justify-content: center;
  padding: 60px 0;
}
.reload {
  font-size: 13px;
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
  font-size: 11px;
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
  font-size: 14px;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.desc {
  font-size: 12px;
  color: var(--nvr-text-2);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.time {
  font-size: 11px;
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
.empty {
  text-align: center;
  padding: 60px 0;
  color: var(--nvr-text-2);
}
.empty p {
  font-size: 13px;
}
</style>
