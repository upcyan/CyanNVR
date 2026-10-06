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
    if (evtSource) return
    // 关键修复：此前用 `!server.base` 判定「未配置服务器」并直接放弃连接，
    // 但同源部署时 base 正是空字符串——于是同源用户的通知**永远不连接**。
    // 这里改为始终使用（可能为空的）base；真正的登录态由 token 决定。
    const token = localStorage.getItem('nvr_token')
    if (!token || token === 'demo') return

    const url = `${server.base}/api/events/sse?token=${token}`
    evtSource = new EventSource(url)

    evtSource.onopen = () => {
      // 连接真正建立后才重置退避，避免「连上又立刻断开」时退避永远为 1s
      retryDelay = 1000
    }

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
      if (reconnectTimer) clearTimeout(reconnectTimer)
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
    retryDelay = 1000
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
