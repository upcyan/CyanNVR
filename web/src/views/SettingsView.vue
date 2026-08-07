<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { showToast } from 'vant'
import type { RecordMode } from '../types'
import { useSettingsStore } from '../stores/settings'
import { changeOwnPassword, fetchAppSettings, isBackend, saveAppSettings, type AppSettings } from '../api'

const store = useSettingsStore()
const s = store.settings

const saving = ref(false)
const showStartPicker = ref(false)
const showEndPicker = ref(false)
const pwd = ref({ old: '', next: '', confirm: '' })

const ai = ref<AppSettings['ai']>({
  enabled: false,
  baseUrl: 'https://api.openai.com/v1',
  model: 'gpt-4o-mini',
  apiKey: '',
  prompt: '你是安防监控分析助手。分析图中画面，仅输出JSON：{"alert":true/false,"label":"事件类别","description":"简短中文描述"}。出现人员、车辆、异常闯入、火焰烟雾等视为 alert=true。',
  interval: 10,
  cooldown: 60,
  threshold: 0.5,
})
const aiLoading = ref(false)

async function loadAi() {
  if (!isBackend()) return
  aiLoading.value = true
  try {
    const st = await fetchAppSettings()
    ai.value = st.ai
  } catch {
    /* ignore */
  } finally {
    aiLoading.value = false
  }
}

onMounted(loadAi)

const usedPct = Math.round((s.storageUsedGB / s.storageTotalGB) * 100)

const modeOptions = [
  { label: '连续录制', desc: '全天不间断录像', value: 'continuous' },
  { label: '移动侦测', desc: '检测到移动才录像', value: 'motion' },
  { label: '定时录制', desc: '仅在设定时间段录像', value: 'schedule' },
] as const

function timeToColumns(t: string): string[] {
  return [t.slice(0, 2), t.slice(3, 5)]
}
function columnsToTime(c: string[]): string {
  return `${c[0]}:${c[1]}`
}
function onStartConfirm(c: string[]) {
  s.scheduleStart = columnsToTime(c)
  showStartPicker.value = false
}
function onEndConfirm(c: string[]) {
  s.scheduleEnd = columnsToTime(c)
  showEndPicker.value = false
}

async function save() {
  if (pwd.value.next !== pwd.value.confirm) {
    showToast('两次输入的新密码不一致')
    return
  }
  saving.value = true
  try {
    if (isBackend() && pwd.value.old && pwd.value.next) {
      await changeOwnPassword(pwd.value.old, pwd.value.next)
      pwd.value = { old: '', next: '', confirm: '' }
    }
    await saveAi()
    await store.save()
    showToast('设置已保存')
  } catch (e: any) {
    showToast(e?.response?.data?.error || '保存失败')
  } finally {
    saving.value = false
  }
}

async function saveAi() {
  if (!isBackend()) return
  await saveAppSettings({ ...defaultAppSettings(), ai: ai.value })
}

function logout() {
  const auth = localStorage
  auth.removeItem('nvr_token')
  auth.removeItem('nvr_user')
  location.hash = '#/login'
}

function setMode(m: RecordMode) {
  store.set({ recordMode: m })
}

function setTheme() {
  store.set({ theme: s.theme === 'dark' ? 'light' : 'dark' })
}

const fontSizeOptions = [
  { label: '标准', value: 'normal', size: 14 },
  { label: '大字', value: 'large', size: 17 },
  { label: '特大', value: 'xlarge', size: 20 },
] as const

function setFontSize(v: 'normal' | 'large' | 'xlarge') {
  store.set({ fontSize: v })
}

function setCareMode(v: boolean) {
  store.set({ careMode: v })
}

function defaultAppSettings(): AppSettings {
  return {
    retentionDays: s.retentionDays,
    recordMode: s.recordMode,
    scheduleStart: s.scheduleStart,
    scheduleEnd: s.scheduleEnd,
    motionPush: s.motionPush,
    offlinePush: s.offlinePush,
    https: s.httpsEnabled,
    ai: ai.value,
  }
}
</script>

<template>
  <div class="page settings-page">
    <van-nav-bar title="设置" left-arrow @click-left="$router.back()" />

    <van-cell-group title="存储管理">
      <van-cell title="存储空间" :label="`已用 ${s.storageUsedGB}GB / 共 ${s.storageTotalGB}GB`">
        <template #value>
          <div class="progress">
            <div class="bar">
              <i :style="{ width: usedPct + '%' }" :class="{ warn: usedPct > 85 }" />
            </div>
            <span>{{ usedPct }}%</span>
          </div>
        </template>
      </van-cell>
      <van-cell title="循环覆盖" label="超过保留天数自动删除最早录像">
        <template #value>
          <div class="slider-box">
            <van-slider
              v-model="s.retentionDays"
              :min="7"
              :max="180"
              :step="1"
              style="width: 120px"
              @change="store.set({ retentionDays: s.retentionDays })"
            />
            <span class="days">{{ s.retentionDays }} 天</span>
          </div>
        </template>
      </van-cell>
    </van-cell-group>

    <van-cell-group title="录像策略">
      <van-radio-group :model-value="s.recordMode" @update:model-value="setMode">
        <van-cell v-for="o in modeOptions" :key="o.value" clickable @click="setMode(o.value)">
          <template #title>
            <span class="mode-title">{{ o.label }}</span>
            <span class="mode-desc">{{ o.desc }}</span>
          </template>
          <template #right-icon>
            <van-radio :name="o.value" />
          </template>
        </van-cell>
      </van-radio-group>
      <van-cell v-if="s.recordMode === 'schedule'" title="录像时间段">
        <template #value>
          <span class="time-field mono" @click="showStartPicker = true">{{ s.scheduleStart }}</span>
          <span class="time-sep">-</span>
          <span class="time-field mono" @click="showEndPicker = true">{{ s.scheduleEnd }}</span>
        </template>
      </van-cell>
    </van-cell-group>

    <van-cell-group title="通知">
      <van-cell title="移动侦测告警" label="检测到移动时推送通知">
        <template #right-icon>
          <van-switch v-model="s.motionPush" size="20" @update:model-value="store.set({ motionPush: $event })" />
        </template>
      </van-cell>
      <van-cell title="设备离线提醒" label="设备断线时推送通知">
        <template #right-icon>
          <van-switch v-model="s.offlinePush" size="20" @update:model-value="store.set({ offlinePush: $event })" />
        </template>
      </van-cell>
    </van-cell-group>

    <van-cell-group title="网络">
      <van-field v-model="s.httpPort" type="number" label="HTTP 端口" placeholder="8080" />
      <van-cell title="启用 HTTPS" label="通过安全通道访问">
        <template #right-icon>
          <van-switch v-model="s.httpsEnabled" size="20" @update:model-value="store.set({ httpsEnabled: $event })" />
        </template>
      </van-cell>
    </van-cell-group>

    <van-cell-group title="显示与无障碍">
      <van-cell title="深色模式" label="切换界面主题">
        <template #right-icon>
          <van-switch :model-value="s.theme === 'dark'" size="20" @update:model-value="setTheme" />
        </template>
      </van-cell>
      <van-cell title="字体大小" label="全局文字大小，立即生效">
        <template #value>
          <div class="font-opts">
            <span
              v-for="o in fontSizeOptions"
              :key="o.value"
              class="font-opt"
              :class="{ on: s.fontSize === o.value && !s.careMode }"
              :style="{ fontSize: o.size + 'px' }"
              @click="setFontSize(o.value)"
            >
              A
            </span>
          </div>
        </template>
      </van-cell>
      <van-cell title="关怀模式" label="更大字体与按钮、更高对比度，方便长辈使用">
        <template #right-icon>
          <van-switch :model-value="s.careMode" size="20" @update:model-value="setCareMode" />
        </template>
      </van-cell>
    </van-cell-group>

    <van-cell-group title="服务器">
      <van-cell
        title="服务器地址"
        label="局域网 / 公网连接，自动判断"
        is-link
        @click="$router.push('/server')"
      />
    </van-cell-group>

    <van-cell-group title="AI 画面识别">
      <van-cell title="启用 AI 识别" label="对画面进行分析并记录事件与动图">
        <template #right-icon>
          <van-switch v-model="ai.enabled" size="20" />
        </template>
      </van-cell>
      <template v-if="ai.enabled">
        <van-field v-model="ai.baseUrl" label="接口地址" placeholder="https://api.openai.com/v1" />
        <van-field v-model="ai.model" label="模型" placeholder="gpt-4o-mini" />
        <van-field v-model="ai.apiKey" label="API Key" placeholder="sk-..." />
        <van-cell title="识别间隔(秒)" label="每隔多久分析一帧">
          <template #value>
            <van-stepper v-model="ai.interval" :min="5" :max="120" step="5" />
          </template>
        </van-cell>
        <van-cell title="事件冷却(秒)" label="同一设备事件间隔">
          <template #value>
            <van-stepper v-model="ai.cooldown" :min="10" :max="600" step="10" />
          </template>
        </van-cell>
        <van-cell title="触发阈值" label="置信度达到该值才记录">
          <template #value>
            <van-slider v-model="ai.threshold" :min="0.3" :max="1" :step="0.05" style="width: 120px" />
          </template>
        </van-cell>
        <van-field
          v-model="ai.prompt"
          type="textarea"
          rows="3"
          autosize
          label="识别提示词"
          placeholder="分析画面并输出 JSON..."
        />
      </template>
      <van-cell v-if="!isBackend()" title="演示模式" label="连接服务器后可配置 AI 识别" />
      <van-cell v-if="isBackend() && aiLoading" title="加载中..." />
    </van-cell-group>

    <van-cell-group title="账户安全">
      <van-field v-model="pwd.old" type="password" label="原密码" placeholder="请输入原密码" />
      <van-field v-model="pwd.next" type="password" label="新密码" placeholder="请输入新密码" />
      <van-field v-model="pwd.confirm" type="password" label="确认密码" placeholder="再次输入新密码" />
    </van-cell-group>

    <div class="save-area">
      <van-button type="primary" block round :loading="saving" @click="save">
        保存设置
      </van-button>
      <van-button
        v-if="isBackend()"
        plain
        block
        round
        type="danger"
        style="margin-top: 10px"
        @click="logout"
      >
        退出登录
      </van-button>
    </div>

    <van-popup v-model:show="showStartPicker" position="bottom" round>
      <van-time-picker
        :columns-type="['hour', 'minute']"
        :model-value="timeToColumns(s.scheduleStart)"
        title="开始时间"
        @confirm="onStartConfirm"
        @cancel="showStartPicker = false"
      />
    </van-popup>
    <van-popup v-model:show="showEndPicker" position="bottom" round>
      <van-time-picker
        :columns-type="['hour', 'minute']"
        :model-value="timeToColumns(s.scheduleEnd)"
        title="结束时间"
        @confirm="onEndConfirm"
        @cancel="showEndPicker = false"
      />
    </van-popup>
  </div>
</template>

<style scoped>
.settings-page {
  padding-bottom: 30px;
}
.progress {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
}
.bar {
  width: 110px;
  height: 6px;
  border-radius: 3px;
  background: var(--nvr-panel-2);
  overflow: hidden;
}
.bar i {
  display: block;
  height: 100%;
  background: var(--nvr-green);
  border-radius: 3px;
  transition: width 0.3s;
}
.bar i.warn {
  background: var(--nvr-amber);
}
.slider-box {
  display: flex;
  align-items: center;
  gap: 8px;
}
.days {
  font-size: 12px;
  color: var(--nvr-text-2);
  min-width: 44px;
}
.mode-title {
  display: block;
}
.mode-desc {
  display: block;
  font-size: 12px;
  color: var(--nvr-text-2);
  margin-top: 2px;
}
.time-field {
  padding: 4px 10px;
  border-radius: 6px;
  background: var(--nvr-panel-2);
  border: 1px solid var(--nvr-border);
  font-size: 13px;
}
.time-sep {
  margin: 0 6px;
  color: var(--nvr-text-2);
}
.save-area {
  padding: 16px;
}

.font-opts {
  display: flex;
  align-items: flex-end;
  gap: 6px;
}
.font-opt {
  width: 36px;
  height: 36px;
  border-radius: 8px;
  border: 1px solid var(--nvr-border);
  background: var(--nvr-panel-2);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--nvr-text-2);
  cursor: pointer;
}
.font-opt.on {
  border-color: var(--nvr-accent);
  color: var(--nvr-accent);
  background: rgba(46, 168, 255, 0.12);
}

@media (min-width: 900px) {
  .settings-page {
    max-width: 760px;
    margin: 0 auto;
    width: 100%;
  }
}
</style>
