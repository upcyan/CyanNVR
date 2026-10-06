import { ref } from 'vue'
import { detectAndApply } from './server'

/**
 * 连接状态与演示模式。
 *
 * 为什么单独成模块：这两件事此前被混在同一个布尔量 `backendOk` 里，
 * 于是「探测不到后端」被直接当成「可以用模拟数据」，真实故障被静默
 * 掩饰成登录成功 + 空数据。现在把它们彻底分开：
 *   · connState 描述**服务器是否可达**（故障状态，需要重试/提示）；
 *   · demoMode 描述**用户是否显式选择用模拟数据**（产品功能）。
 * 两者互不推导。
 */

/** checking=探测中，online=后端可用，offline=不可用 */
export type ConnState = 'checking' | 'online' | 'offline'

export const connState = ref<ConnState>('checking')
/** 最近一次探测失败原因，用于界面提示（不含任何敏感信息） */
export const connError = ref('')

/** 演示模式：只有用户在界面显式开启才为真 */
export const demoMode = ref(false)

const DEMO_KEY = 'nvr_demo_mode'

/** 演示模式持久化在独立键上；这是它的唯一真相源。 */
export function loadDemoMode(): void {
  try {
    demoMode.value = localStorage.getItem(DEMO_KEY) === '1'
  } catch {
    demoMode.value = false
  }
}

export function isDemoMode(): boolean {
  return demoMode.value
}

export function setDemoMode(on: boolean): void {
  demoMode.value = !!on
  try {
    localStorage.setItem(DEMO_KEY, on ? '1' : '0')
  } catch {
    /* 隐私模式下写入可能失败：内存态仍然生效 */
  }
}

export function isBackend(): boolean {
  return connState.value === 'online'
}

export function isConnChecking(): boolean {
  return connState.value === 'checking'
}

/** 服务器不可达时抛出的类型化错误，供界面区分「连接故障」与「业务失败」 */
export class ConnUnavailableError extends Error {
  readonly code = 'CONN_UNAVAILABLE'
  constructor(message = '无法连接服务器，请检查网络连接或服务器地址') {
    super(message)
    this.name = 'ConnUnavailableError'
  }
}

/**
 * 需要真实后端的数据接口统一走这里（演示模式走 mock，放行）。
 * 离线时**抛出错误**而不是返回空数组/模拟数据——返回假成功会让
 * 「服务不可用」在界面上表现为「暂无设备」「当日无录像」。
 */
export function requireBackend(): void {
  if (isDemoMode()) return
  if (!isBackend()) throw new ConnUnavailableError(connError.value || undefined)
}

/**
 * 探测连接：同源优先，其次已配置的局域网/公网地址（见 server.detectAndApply）。
 * 连接恢复后调用它即可让上层重新加载数据。
 */
export async function refreshConnection(): Promise<boolean> {
  connState.value = 'checking'
  connError.value = ''
  let ok = false
  try {
    ok = await detectAndApply()
  } catch {
    ok = false
  }
  connState.value = ok ? 'online' : 'offline'
  if (!ok) connError.value = '无法连接服务器，请检查网络连接或服务器地址'
  return ok
}

// 模块加载即同步一次本地演示模式，避免首屏 UI 与数据源不一致
loadDemoMode()
