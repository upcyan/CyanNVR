import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { readFileSync, readdirSync } from 'fs'
import { createHash } from 'crypto'
import path from 'path'
import { fileURLToPath } from 'url'

const coreVersion = readFileSync(fileURLToPath(new URL('../server/version.go', import.meta.url)), 'utf8').match(/CoreVersion = "([^"]+)"/)?.[1] || '0.0.0'
const sourceRoot = fileURLToPath(new URL('./src/', import.meta.url))
const hash = createHash('sha256')
function hashTree(dir: string) {
  for (const entry of readdirSync(dir, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
    const file = path.join(dir, entry.name)
    if (entry.isDirectory()) hashTree(file)
    else { hash.update(path.relative(sourceRoot, file)); hash.update(readFileSync(file)) }
  }
}
hashTree(sourceRoot)
hash.update(readFileSync(fileURLToPath(import.meta.url)))
hash.update(readFileSync(fileURLToPath(new URL('./package-lock.json', import.meta.url))))
hash.update(coreVersion)
const buildId = hash.digest('hex').slice(0, 20)

export default defineConfig({
  define: { __APP_VERSION__: JSON.stringify(coreVersion), __APP_BUILD_ID__: JSON.stringify(buildId) },
  plugins: [vue(), {
    name: 'cyannvr-build-manifest',
    generateBundle() {
      this.emitFile({ type: 'asset', fileName: 'build.json', source: JSON.stringify({ app: 'CyanNVR', version: coreVersion, buildId }) })
      this.emitFile({ type: 'asset', fileName: 'manifest.webmanifest', source: JSON.stringify({
        name: 'CyanNVR', short_name: 'NVR', description: 'Simple Network Video Recorder', start_url: '.',
        display: 'standalone', background_color: '#0f1115', theme_color: '#0f1115', lang: 'zh-CN',
        icons: [{ src: 'icon-192.png', sizes: '192x192', type: 'image/png' },
          { src: 'icon-512.png', sizes: '512x512', type: 'image/png' },
          { src: 'icon-maskable-512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' }],
      }) })
      // Compatibility endpoints for cached HTML/legacy registered workers. No new SW is registered.
      this.emitFile({ type: 'asset', fileName: 'registerSW.js', source: '/* CyanNVR no longer registers offline caches. */' })
      this.emitFile({ type: 'asset', fileName: 'sw.js', source: `
self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', event => event.waitUntil((async () => {
  const scope = self.registration.scope;
  for (const key of await caches.keys()) {
    if (/^(cyannvr|nvr)[-_:]/i.test(key) || (key.startsWith('workbox-precache') && key.includes(scope))) await caches.delete(key);
  }
  await self.registration.unregister();
})()));
// Deliberately no fetch handler: all requests use the network.
` })
    },
  }],
  base: './',
  server: { host: true, port: 5173 },
})
