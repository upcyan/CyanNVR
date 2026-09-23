<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { showToast } from 'vant'
import QRCode from 'qrcode'
import { detectAndApply, probe, server, setServerAddrs } from '../api/server'
import {
  fetchAppSettings,
  fetchTLSStatus,
  saveAppSettings,
  uploadManualCert,
  type TLSStatus,
} from '../api'

const router = useRouter()
const form = reactive({
  lanUrl: server.lanUrl,
  publicUrl: server.publicUrl,
  mode: server.mode,
})
const testing = ref(false)
const result = ref('')

// ---- HTTPS / 证书 ----
const tls = reactive({
  https: false,
  httpsPort: 443,
  certMode: 'auto',
  domain: '',
  email: '',
  certPem: '',
  keyPem: '',
})
const tlsStatus = ref<TLSStatus | null>(null)
const tlsEditable = ref(false)
const uploading = ref(false)

const tlsStatusLine = computed(() => {
  const st = tlsStatus.value
  if (!st) return tlsEditable.value ? '加载中…' : '登录管理员账号后可在此配置'
  if (!st.enabled) return '未启用'
  if (!st.running) return st.message ? `未运行：${st.message}` : '未运行'
  const exp = st.notAfter ? `，证书到期 ${st.notAfter.slice(0, 10)}` : ''
  const msg = st.message ? `（${st.message}）` : ''
  return `运行中 · https://${st.domain || '?'}:${st.port}${exp}${msg}`
})

async function loadTLS() {
  try {
    const s = await fetchAppSettings()
    tls.https = !!s.https
    tls.httpsPort = s.httpsPort || 443
    tls.certMode = s.tlsCertMode === 'manual' ? 'manual' : 'auto'
    tls.domain = s.tlsDomain || ''
    tls.email = s.acmeEmail || ''
    tlsEditable.value = true
    tlsStatus.value = await fetchTLSStatus()
  } catch {
    tlsEditable.value = false
  }
}
onMounted(loadTLS)

async function uploadCert() {
  if (!tls.certPem.trim() || !tls.keyPem.trim()) {
    showToast('请先粘贴证书与私钥')
    return
  }
  uploading.value = true
  try {
    tlsStatus.value = await uploadManualCert(tls.certPem, tls.keyPem)
    tls.certPem = ''
    tls.keyPem = ''
    showToast('证书已保存')
  } catch (e: unknown) {
    const msg = (e as { response?: { data?: { error?: string } } })?.response?.data?.error
    showToast(msg || '上传失败（需要管理员）')
  } finally {
    uploading.value = false
  }
}

// PUT /settings 是整对象替换：先拉取最新设置，仅合并 HTTPS 字段后再保存
async function saveTLS(): Promise<string | null> {
  if (!tlsEditable.value) return null
  try {
    const s = await fetchAppSettings()
    s.https = tls.https
    s.httpsPort = Number(tls.httpsPort) || 443
    s.tlsCertMode = tls.https ? tls.certMode : ''
    s.tlsDomain = tls.domain.trim()
    s.acmeEmail = tls.email.trim()
    await saveAppSettings(s)
    setTimeout(loadTLS, 1500) // 等待热生效后刷新状态
    return null
  } catch {
    return 'HTTPS 设置保存失败（需要管理员账号）'
  }
}

// ---- 配对二维码：内容为 cyannvr://host[:port]，供 CyanNVR App 扫码/深链添加 ----
const qrDataUrl = ref('')
let qrTimer: ReturnType<typeof setTimeout> | undefined

function pairingUrl(): string {
  const raw = form.lanUrl.trim()
  if (!raw) return ''
  const s = /^https?:\/\//.test(raw) ? raw : `http://${raw}`
  try {
    return `cyannvr://${new URL(s).host}`
  } catch {
    return ''
  }
}

function scheduleQr() {
  clearTimeout(qrTimer)
  qrTimer = setTimeout(buildQr, 300)
}

async function buildQr() {
  const url = pairingUrl()
  if (!url) {
    qrDataUrl.value = ''
    return
  }
  try {
    qrDataUrl.value = await QRCode.toDataURL(url, { width: 220, margin: 1 })
  } catch {
    qrDataUrl.value = ''
  }
}

watch(() => form.lanUrl, scheduleQr, { immediate: true })

async function test() {
  testing.value = true
  result.value = ''
  try {
    const sameOrigin = await probe('')
    const lan = form.lanUrl.replace(/\/+$/, '')
    const pub = form.publicUrl.replace(/\/+$/, '')
    const lines: string[] = []
    if (sameOrigin) lines.push('本机地址（同源）: 可达')
    else lines.push('本机地址（同源）: 不可达')
    lines.push(`局域网 ${form.lanUrl || '(未设置)'}: ${lan ? await probe(lan) ? '可达' : '不可达' : '-'}`)
    lines.push(`公网 ${form.publicUrl || '(未设置)'}: ${pub ? await probe(pub) ? '可达' : '不可达' : '-'}`)
    result.value = lines.join('\n')
  } finally {
    testing.value = false
  }
}

async function save() {
  setServerAddrs(form.lanUrl.trim(), form.publicUrl.trim(), form.mode)
  const tlsErr = await saveTLS()
  const ok = await detectAndApply()
  if (ok) {
    showToast(`已连接：${server.current === 'same-origin' ? '本机' : server.current === 'lan' ? '局域网' : '公网'}${tlsErr ? '；' + tlsErr : ''}`)
  } else if (tlsErr) {
    showToast(tlsErr)
  } else {
    showToast('未找到可达的服务器，请检查地址')
  }
}

function goBack() {
  if (location.hash.includes('/server')) router.back()
  else router.push('/login')
}
</script>

<template>
  <div class="page server-page">
    <van-nav-bar title="服务器设置" left-arrow @click-left="goBack" />

    <van-cell-group title="连接地址">
      <van-field v-model="form.lanUrl" label="局域网地址" placeholder="如：192.168.1.100:8080" />
      <van-field v-model="form.publicUrl" label="公网地址" placeholder="如：nvr.example.com:8080" />
      <van-cell class="opt-cell" title="连接策略" label="自动：先局域网后公网">
        <van-radio-group v-model="form.mode" direction="horizontal" class="strategy-radios">
          <van-radio name="auto">自动</van-radio>
          <van-radio name="lan">仅局域网</van-radio>
          <van-radio name="public">仅公网</van-radio>
        </van-radio-group>
      </van-cell>
    </van-cell-group>

    <div class="actions">
      <van-button type="primary" block round :loading="testing" @click="test">测试连通性</van-button>
      <van-button plain block round style="margin-top: 10px" @click="save">保存并应用</van-button>
    </div>

    <div v-if="result" class="result">
      <pre>{{ result }}</pre>
    </div>

    <van-cell-group title="配对二维码">
      <div class="qr-box">
        <template v-if="qrDataUrl">
          <img :src="qrDataUrl" alt="配对二维码" class="qr-img" />
          <p class="qr-text">用 CyanNVR App「扫码添加」即可自动填入服务器地址</p>
          <p class="qr-url">{{ pairingUrl() }}</p>
        </template>
        <p v-else class="qr-text qr-empty">填写局域网地址后，这里会生成配对二维码</p>
      </div>
    </van-cell-group>

    <van-cell-group title="HTTPS 证书">
      <van-cell title="启用 HTTPS" label="供公网域名安全访问，不影响局域网直连">
        <template #right-icon>
          <van-switch
            :model-value="tls.https"
            :disabled="!tlsEditable"
            @update:model-value="tls.https = $event"
          />
        </template>
      </van-cell>
      <template v-if="tls.https && tlsEditable">
        <van-field
          v-model="tls.httpsPort"
          type="digit"
          label="HTTPS 端口"
          placeholder="443"
        />
        <van-cell class="opt-cell" title="证书来源">
          <template #label>
            自动申请：域名需解析到本机，且 80/443 端口对公网可达
          </template>
          <van-radio-group v-model="tls.certMode" direction="horizontal">
            <van-radio name="auto">自动申请</van-radio>
            <van-radio name="manual">手动上传</van-radio>
          </van-radio-group>
        </van-cell>
        <van-field
          v-model="tls.domain"
          label="域名"
          placeholder="如 nvr.example.com"
        />
        <template v-if="tls.certMode === 'auto'">
          <van-field
            v-model="tls.email"
            label="邮箱"
            placeholder="证书通知邮箱（可选）"
          />
        </template>
        <template v-else>
          <van-field
            v-model="tls.certPem"
            type="textarea"
            rows="3"
            label="证书"
            placeholder="粘贴 certificate.pem 全文"
          />
          <van-field
            v-model="tls.keyPem"
            type="textarea"
            rows="3"
            label="私钥"
            placeholder="粘贴 private.key 全文"
          />
          <div class="tls-actions">
            <van-button size="small" plain round :loading="uploading" @click="uploadCert">
              校验并保存证书
            </van-button>
          </div>
        </template>
      </template>
      <van-cell title="当前状态" :label="tlsStatusLine" />
    </van-cell-group>

    <p class="tip">提示：地址可不带协议（默认 http），也可填写完整地址。探测到可达服务器后自动切换连接方式。</p>
  </div>
</template>

<style scoped>
.server-page {
  width: 100%;
  max-width: 760px;
  margin: 0 auto;
}
.actions {
  padding: 20px 16px;
}
.result {
  margin: 0 16px;
  padding: 12px;
  border-radius: 10px;
  background: var(--nvr-panel-2);
  border: 1px solid var(--nvr-border);
  font-size: 13px;
}
.result pre {
  margin: 0;
  white-space: pre-wrap;
  font-family: ui-monospace, Menlo, monospace;
}
.tip {
  padding: 16px;
  font-size: 12px;
  color: var(--nvr-text-2);
  line-height: 1.6;
}
.qr-box {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 20px 16px;
}
.qr-img {
  width: 200px;
  height: 200px;
  border-radius: 8px;
  background: #fff;
}
.qr-text {
  margin: 12px 0 4px;
  font-size: 12px;
  color: var(--nvr-text-2);
  text-align: center;
}
.qr-url {
  margin: 0;
  font-size: 12px;
  font-family: ui-monospace, Menlo, monospace;
  color: var(--nvr-text-2);
  overflow-wrap: anywhere;
  text-align: center;
}
.qr-empty {
  padding: 12px 0;
}
.tls-actions {
  padding: 8px 16px 12px;
}
/* 关怀模式（1.4 倍缩放）下 radio 文字放大，允许换行避免溢出截断 */
.strategy-radios {
  flex-wrap: wrap;
  gap: 4px 12px;
}
</style>
