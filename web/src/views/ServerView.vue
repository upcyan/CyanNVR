<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { showToast } from 'vant'
import { detectAndApply, probe, server, setServerAddrs } from '../api/server'

const router = useRouter()
const form = reactive({
  lanUrl: server.lanUrl,
  publicUrl: server.publicUrl,
  mode: server.mode,
})
const testing = ref(false)
const result = ref('')

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
  const ok = await detectAndApply()
  if (ok) {
    showToast(`已连接：${server.current === 'same-origin' ? '本机' : server.current === 'lan' ? '局域网' : '公网'}`)
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
      <van-cell title="连接策略" label="自动：先局域网后公网">
        <template #value>
          <van-radio-group v-model="form.mode" direction="horizontal">
            <van-radio name="auto">自动</van-radio>
            <van-radio name="lan">仅局域网</van-radio>
            <van-radio name="public">仅公网</van-radio>
          </van-radio-group>
        </template>
      </van-cell>
    </van-cell-group>

    <div class="actions">
      <van-button type="primary" block round :loading="testing" @click="test">测试连通性</van-button>
      <van-button plain block round style="margin-top: 10px" @click="save">保存并应用</van-button>
    </div>

    <div v-if="result" class="result">
      <pre>{{ result }}</pre>
    </div>

    <p class="tip">提示：地址可不带协议（默认 http），也可填写完整地址。探测到可达服务器后自动切换连接方式。</p>
  </div>
</template>

<style scoped>
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
</style>
