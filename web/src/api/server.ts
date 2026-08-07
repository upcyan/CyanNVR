import { reactive } from 'vue'

const LS_LAN = 'nvr_server_lan'
const LS_PUB = 'nvr_server_pub'
const LS_MODE = 'nvr_server_mode'
const LS_CUR = 'nvr_server_cur'

export const server = reactive({
  base: '',
  lanUrl: localStorage.getItem(LS_LAN) || '',
  publicUrl: localStorage.getItem(LS_PUB) || '',
  mode: (localStorage.getItem(LS_MODE) as 'auto' | 'lan' | 'public') || 'auto',
  current: localStorage.getItem(LS_CUR) || '',
})

export function setServerAddrs(lan: string, pub: string, mode: string) {
  server.lanUrl = lan
  server.publicUrl = pub
  server.mode = mode as 'auto' | 'lan' | 'public'
  localStorage.setItem(LS_LAN, lan)
  localStorage.setItem(LS_PUB, pub)
  localStorage.setItem(LS_MODE, mode)
}

export function resolveBase(url: string) {
  return url.replace(/\/+$/, '')
}

export async function probe(base: string): Promise<boolean> {
  try {
    const ctl = new AbortController()
    const t = setTimeout(() => ctl.abort(), 2500)
    const res = await fetch(base + '/api/health', { signal: ctl.signal })
    clearTimeout(t)
    if (!res.ok) return false
    const j = await res.json()
    return j && j.name === 'SimpleNVR'
  } catch {
    return false
  }
}

function apply(base: string, current: string) {
  server.base = base
  server.current = current
  localStorage.setItem(LS_CUR, current)
}

// 自动判断：同源 > 局域网 > 公网
export async function detectAndApply(): Promise<boolean> {
  if (await probe('')) {
    apply('', 'same-origin')
    return true
  }
  const lan = resolveBase(server.lanUrl)
  const pub = resolveBase(server.publicUrl)
  const candidates: Array<[string, string]> = []
  if (server.mode === 'lan' && lan) candidates.push([lan, 'lan'])
  else if (server.mode === 'public' && pub) candidates.push([pub, 'public'])
  else {
    if (lan) candidates.push([lan, 'lan'])
    if (pub) candidates.push([pub, 'public'])
  }
  for (const [url, name] of candidates) {
    if (await probe(url)) {
      apply(url, name)
      return true
    }
  }
  apply('', '')
  return false
}
