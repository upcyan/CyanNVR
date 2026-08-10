import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { VitePWA } from 'vite-plugin-pwa'

export default defineConfig({
  plugins: [
    vue(),
    VitePWA({
      registerType: 'autoUpdate',
      includeAssets: ['icon.svg', 'favicon.ico'],
      manifest: {
        name: 'SimpleNVR',
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
            src: 'icon.svg',
            sizes: 'any',
            type: 'image/svg+xml',
            purpose: 'any',
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
