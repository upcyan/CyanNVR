import { ref } from 'vue'
import { server } from '../api/server'

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
        showBrowserNotification(n)
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
