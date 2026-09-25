import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { VitePWA } from 'vite-plugin-pwa'

// 前端版本号 = server/version.go 的 CoreVersion（构建期注入，供启动自清理
// 与服务端 /api/health 的 version 比对，判断是否拿到了旧缓存）
import { readFileSync } from 'fs'
import { fileURLToPath } from 'url'
const coreVersion = (() => {
  try {
    const p = fileURLToPath(new URL('../server/version.go', import.meta.url))
    const m = readFileSync(p, 'utf8').match(/CoreVersion = "([^"]+)"/)
    return m ? m[1] : '0.0.0'
  } catch {
    return '0.0.0'
  }
})()

export default defineConfig({
  define: {
    __APP_VERSION__: JSON.stringify(coreVersion),
  },
  plugins: [
    vue(),
    VitePWA({
      registerType: 'autoUpdate',
      // 自毁模式：sw.js 安装即注销自身并清空预缓存，此后所有请求直达
      // 服务器（配合后端 no-cache 头），升级刷新一次即生效，根治
      // 「Service Worker 拦截导致 F5 仍是旧界面」的排错黑洞。
      selfDestroying: true,
      includeAssets: ['icon.svg', 'icon-192.png', 'icon-512.png'],
      manifest: {
        name: 'CyanNVR',
        short_name: 'NVR',
        description: 'Simple Network Video Recorder',
        start_url: '.',
        display: 'standalone',
        orientation: 'portrait',
        background_color: '#0f1115',
        theme_color: '#0f1115',
        lang: 'zh-CN',
        icons: [
          {
            src: 'icon-192.png',
            sizes: '192x192',
            type: 'image/png',
          },
          {
            src: 'icon-512.png',
            sizes: '512x512',
            type: 'image/png',
          },
          {
            src: 'icon-maskable-512.png',
            sizes: '512x512',
            type: 'image/png',
            purpose: 'maskable',
          },
        ],
      },
      workbox: {
        globPatterns: ['**/*.{js,css,html,ico,svg,png,woff2}'],
        // 注意：这里不配 runtimeCaching。自毁模式下 sw.js 安装即注销自身，
        // 运行时缓存规则永远不会生效，配了只是给排错添乱；
        // 所有请求直达服务器，缓存策略交给 HTTP 头（后端 no-cache/no-store）。
      },
    }),
  ],
  base: './',
  server: {
    host: true,
    port: 5173,
  },
})
