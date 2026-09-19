<template>
  <span class="vendor-badge" :class="{ compact }" :title="title">
    <span class="logo" :style="{ background: meta.color, width: size + 'px', height: size + 'px' }">
      <span v-if="meta.text" class="logo-text" :style="{ fontSize: textSize }">{{ meta.text }}</span>
      <svg v-else viewBox="0 0 24 24" :width="size * 0.62" :height="size * 0.62" aria-hidden="true">
        <!-- 花瓣：华为 -->
        <g v-if="meta.glyph === 'flower'" fill="#fff">
          <circle cx="12" cy="4.4" r="2.7" />
          <circle cx="12" cy="19.6" r="2.7" />
          <circle cx="4.4" cy="12" r="2.7" />
          <circle cx="19.6" cy="12" r="2.7" />
          <circle cx="6.6" cy="6.6" r="2.3" />
          <circle cx="17.4" cy="6.6" r="2.3" />
          <circle cx="6.6" cy="17.4" r="2.3" />
          <circle cx="17.4" cy="17.4" r="2.3" />
        </g>
        <!-- 镜头眼：海康威视 -->
        <g v-else-if="meta.glyph === 'eye'">
          <path
            d="M12 6.2C7.2 6.2 3.7 9.6 2.2 12c1.5 2.4 5 5.8 9.8 5.8s8.3-3.4 9.8-5.8c-1.5-2.4-5-5.8-9.8-5.8z"
            fill="none"
            stroke="#fff"
            stroke-width="2"
          />
          <circle cx="12" cy="12" r="3.1" fill="#fff" />
        </g>
        <!-- 同轴圆环：TP-LINK / 博世 -->
        <g v-else-if="meta.glyph === 'rings'">
          <circle cx="12" cy="12" r="8.4" fill="none" stroke="#fff" stroke-width="2.2" />
          <circle cx="12" cy="12" r="3.4" fill="#fff" />
        </g>
        <!-- 通用摄像机：未知厂商 / 通用模组 -->
        <g v-else fill="#fff">
          <rect x="2.6" y="7" width="12.8" height="10" rx="2.2" />
          <path d="M17 10.6l4.4-2.6v8l-4.4-2.6z" />
        </g>
      </svg>
    </span>
    <span v-if="!compact" class="name">{{ meta.name }}</span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { vendorMeta, vendorSourceLabel } from '../utils/vendors'

const props = withDefaults(
  defineProps<{
    /** 归一化厂商标识（后端 Found.vendor） */
    vendor?: string
    /** 厂商判定依据（后端 Found.vendorSource），用于悬浮提示 */
    source?: string
    /** 紧凑模式：只显示 logo 方块，不显示厂商名 */
    compact?: boolean
    /** logo 边长（像素） */
    size?: number
  }>(),
  { vendor: '', source: '', compact: false, size: 22 },
)

const meta = computed(() => vendorMeta(props.vendor))

const size = computed(() => props.size)

/** 徽章文字长度不一（MI / AXIS / UNV），按字数微调字号避免溢出。 */
const textSize = computed(() => {
  const n = (meta.value.text || '').length
  if (n <= 2) return `${Math.round(size.value * 0.5)}px`
  if (n === 3) return `${Math.round(size.value * 0.4)}px`
  return `${Math.round(size.value * 0.32)}px`
})

const title = computed(() => `${meta.value.name}（${vendorSourceLabel(props.source)}）`)
</script>

<style scoped>
.vendor-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  vertical-align: middle;
}

.logo {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 5px;
  flex: none;
}

.logo-text {
  color: #fff;
  font-weight: 700;
  line-height: 1;
  letter-spacing: -0.02em;
}

.name {
  font-size: 12px;
  color: var(--van-text-color-2, #646566);
  white-space: nowrap;
}
</style>
