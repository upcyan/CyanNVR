<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { showToast } from 'vant'
import { useAuthStore } from '../stores/auth'
import { backendOk } from '../api'

const auth = useAuthStore()
const route = useRoute()
const router = useRouter()

const username = ref('admin')
const password = ref('admin123')
const loading = ref(false)

async function submit() {
  if (!username.value.trim() || !password.value) {
    showToast('请输入用户名和密码')
    return
  }
  loading.value = true
  try {
    await auth.login(username.value.trim(), password.value)
    const redirect = (route.query.redirect as string) || '/live'
    router.replace(redirect)
  } catch (e: any) {
    showToast(e?.response?.data?.error || '登录失败')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="page login">
    <div class="brand">
      <svg viewBox="0 0 100 100" class="logo">
        <rect width="100" height="100" rx="22" fill="#171a21" />
        <g fill="none" stroke="#2ea8ff" stroke-width="6" stroke-linecap="round" stroke-linejoin="round">
          <rect x="16" y="32" width="50" height="38" rx="6" />
          <path d="M66 45l16-9v28l-16-9" />
        </g>
        <circle cx="41" cy="51" r="9" fill="#2ea8ff" />
      </svg>
      <h1>SimpleNVR</h1>
      <p>网络视频录像机</p>
    </div>

    <van-cell-group inset class="form">
      <van-field v-model="username" label="用户名" placeholder="请输入用户名" />
      <van-field
        v-model="password"
        type="password"
        label="密码"
        placeholder="请输入密码"
        @keyup.enter="submit"
      />
    </van-cell-group>

    <div class="btns">
      <van-button type="primary" block round :loading="loading" @click="submit">登 录</van-button>
      <van-button plain block round style="margin-top: 10px" @click="$router.push('/server')">
        服务器设置
      </van-button>
    </div>

    <p class="tip" v-if="!backendOk">
      当前为演示模式（未检测到后端），任意账号密码均可进入
    </p>
  </div>
</template>

<style scoped>
.login {
  align-items: center;
  padding-top: 12vh;
}
.brand {
  text-align: center;
  margin-bottom: 40px;
}
.logo {
  width: 64px;
  height: 64px;
  border-radius: 16px;
}
.brand h1 {
  margin: 16px 0 4px;
  font-size: 26px;
}
.brand p {
  margin: 0;
  color: var(--nvr-text-2);
  font-size: 13px;
}
.form {
  width: 100%;
  max-width: 420px;
  margin: 0 auto;
}
.btns {
  width: 100%;
  max-width: 420px;
  margin: 24px auto 0;
  padding: 0 16px;
}
.tip {
  margin-top: 24px;
  font-size: 12px;
  color: var(--nvr-amber);
}
</style>
