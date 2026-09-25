import { ref } from 'vue'
import { server } from '../api/server'
import { useSettingsStore } from '../stores/settings'

export interface PushNotification {
  type: string
  deviceId: string
  deviceName: string
  eventId?: string
  label?: string
  description?: string
  time: string
}

const notifications = ref<PushNotification[]>([])
let evtSource: EventSource | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | null = null
let retryDelay = 1000
const maxRetryDelay = 30000

export function useNotifications() {
  function connect() {
    if (evtSource || !server.base) return
    const token = localStorage.getItem('nvr_token')
    if (!token) return

    const url = `${server.base}/api/events/sse?token=${token}`
    evtSource = new EventSource(url)

    evtSource.onmessage = (ev) => {
      try {
        const n: PushNotification = JSON.parse(ev.data)
        notifications.value = [n, ...notifications.value].slice(0, 50)
        // 浏览器推送受设置页两个开关控制（此前开关只存不生效）：
        //   设备离线提醒  -> offline / online
        //   移动侦测告警  -> motion / ai / manual
        // 通知中心列表始终记录，只是不再弹系统通知。
        if (pushAllowed(n.type)) showBrowserNotification(n)
        retryDelay = 1000 // Reset backoff on successful message
      } catch {
        /* ignore parse errors */
      }
    }

    evtSource.onerror = () => {
      evtSource?.close()
      evtSource = null
      reconnectTimer = setTimeout(connect, retryDelay)
      retryDelay = Math.min(retryDelay * 2, maxRetryDelay)
    }
  }

  function disconnect() {
    if (reconnectTimer) {
      clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
    evtSource?.close()
    evtSource = null
  }

  return { notifications, connect, disconnect }
}

// pushAllowed 判断该类型事件是否应当弹出系统通知。
// 读设置失败（store 未就绪、demo 模式等）时放行，保持旧行为，避免误吞通知。
function pushAllowed(type: string): boolean {
  try {
    const s = useSettingsStore().settings
    if (type === 'offline' || type === 'online') return s.offlinePush
    return s.motionPush
  } catch {
    return true
  }
}

function showBrowserNotification(n: PushNotification) {
  if (typeof Notification === 'undefined') return
  if (Notification.permission !== 'granted') return
  const title = n.type === 'offline' ? '设备离线' : n.type === 'online' ? '设备上线' : n.label || '事件通知'
  const body = n.description || n.deviceName
  try {
    new Notification(title, { body, icon: '/favicon.ico', tag: `nvr-${n.deviceId}-${n.time}` })
  } catch {
    /* ignore */
  }
}

export async function requestNotificationPermission() {
  if (typeof Notification === 'undefined') return
  if (Notification.permission === 'default') {
    await Notification.requestPermission()
  }
}
