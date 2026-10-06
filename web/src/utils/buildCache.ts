const appBase = new URL(import.meta.env.BASE_URL, location.href)

/** Migrate only this application's SW/cache; never clear unrelated origin storage. */
export async function migrateAppCache() {
  let ownedSW = false
  if ('serviceWorker' in navigator) {
    for (const reg of await navigator.serviceWorker.getRegistrations()) {
      const worker = reg.active || reg.waiting || reg.installing
      if (worker && new URL(worker.scriptURL).href === new URL('sw.js', appBase).href && reg.scope === appBase.href) {
        ownedSW = true
        await reg.unregister()
      }
    }
  }
  if ('caches' in window) {
    for (const key of await caches.keys()) {
      const owned = /^(cyannvr|nvr)[-_:]/i.test(key)
      const legacy = ownedSW && key.startsWith('workbox-precache') && key.includes(appBase.href)
      if (owned || legacy) await caches.delete(key)
    }
  }
}

/** CoreVersion cannot distinguish two frontend revisions; compare actual source hash. */
export async function checkBuild(): Promise<void> {
  const ctl = new AbortController()
  const timer = setTimeout(() => ctl.abort(), 4000)
  try {
    const response = await fetch(new URL('build.json', appBase), { cache: 'no-store', signal: ctl.signal })
    if (!response.ok) return
    const data = await response.json()
    if (data.app !== 'CyanNVR' || typeof data.buildId !== 'string' || data.buildId === __APP_BUILD_ID__) return
    const key = `nvr_build_reload:${appBase.href}:${data.buildId}`
    if (sessionStorage.getItem(key)) return // mismatch after one reload is a deployment problem, not a loop
    sessionStorage.setItem(key, '1')
    const url = new URL(location.href)
    url.searchParams.set('build', data.buildId)
    location.replace(url)
  } catch { /* network failure must not erase session/configuration */ }
  finally { clearTimeout(timer) }
}
