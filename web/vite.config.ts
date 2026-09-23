import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { VitePWA } from 'vite-plugin-pwa'

export default defineConfig({
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
        runtimeCaching: [
          {
            urlPattern: /^https?:\/\/.*\/api\/health$/i,
            handler: 'NetworkFirst',
            options: {
              cacheName: 'api-health',
              networkTimeoutSeconds: 3,
              expiration: { maxEntries: 1, maxAgeSeconds: 30 },
            },
          },
        ],
      },
    }),
  ],
  base: './',
  server: {
    host: true,
    port: 5173,
  },
})
