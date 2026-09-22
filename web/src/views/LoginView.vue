<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { showToast } from 'vant'
import { useAuthStore } from '../stores/auth'
import { apiResetConfirm, apiResetRequest, apiResetVerify, backendOk } from '../api'

const auth = useAuthStore()
const route = useRoute()
const router = useRouter()

const username = ref('')
const password = ref('')
const loading = ref(false)

async function submit() {
  if (!username.value.trim() || !password.value) {
    showToast('请输入用户名和密码')
    return
  }
  loading.value = true
  try {
    await auth.login(username.value.trim(), password.value)
    const raw = (route.query.redirect as string) || '/live'
    // Prevent open redirect: only allow relative paths starting with /
    const redirect = raw.startsWith('/') && !raw.startsWith('//') && !raw.includes('://') ? raw : '/live'
    router.replace(redirect)
  } catch (e: any) {
    showToast(e?.response?.data?.error || '登录失败')
  } finally {
    loading.value = false
  }
}

// ---- 忘记密码 ----
const showReset = ref(false)
const issuing = ref(false)
const verifying = ref(false)
const confirming = ref(false)
const codeFile = ref('')
const code = ref('')
const newPassword = ref('')
const confirmPassword = ref('')

// 三步流程：1 生成重置码 → 2 验证换凭证 → 3 设置新密码
const step = ref<1 | 2 | 3>(1)
const grant = ref('')
const resetUser = ref('')

// 倒计时（秒），依据服务端返回的过期时刻计算，避免本地计时漂移
const remain = ref(0)
let ticker: ReturnType<typeof setInterval> | null = null
let deadline = 0

function stopTicker() {
  if (ticker) {
    clearInterval(ticker)
    ticker = null
  }
}

function startCountdown(expiresAt: string) {
  stopTicker()
  deadline = new Date(expiresAt).getTime()
  const sync = () => {
    remain.value = Math.max(0, Math.round((deadline - Date.now()) / 1000))
    if (remain.value <= 0) {
      stopTicker()
      // 服务端到期会自行删除文件，这里只做界面收尾
      if (step.value !== 3) {
        step.value = 1
        codeFile.value = ''
        code.value = ''
      }
    }
  }
  sync()
  ticker = setInterval(sync, 1000)
}

const countdownText = computed(() => {
  const m = Math.floor(remain.value / 60)
  const s = remain.value % 60
  return `${m}:${String(s).padStart(2, '0')}`
})

onBeforeUnmount(stopTicker)

function openReset() {
  showReset.value = true
}

async function issueCode() {
  issuing.value = true
  try {
    const r = await apiResetRequest()
    codeFile.value = r.file
    step.value = 2
    startCountdown(r.expiresAt)
    showToast('重置码已生成，请尽快查看')
  } catch (e: any) {
    showToast(e?.response?.data?.error || '生成重置码失败')
  } finally {
    issuing.value = false
  }
}

async function verifyCode() {
  if (!code.value.trim()) {
    showToast('请输入重置码')
    return
  }
  verifying.value = true
  try {
    const r = await apiResetVerify(code.value.trim(), username.value.trim())
    grant.value = r.grant
    resetUser.value = r.user
    username.value = r.user || username.value
    step.value = 3
    startCountdown(r.expiresAt)
    showToast('验证通过，请设置新密码')
  } catch (e: any) {
    showToast(e?.response?.data?.error || '重置码无效或已过期')
  } finally {
    verifying.value = false
  }
}

async function confirmReset() {
  if (newPassword.value.length < 8) {
    showToast('密码至少需要 8 个字符')
    return
  }
  if (newPassword.value !== confirmPassword.value) {
    showToast('两次输入的密码不一致')
    return
  }
  confirming.value = true
  try {
    const r = await apiResetConfirm(grant.value, newPassword.value)
    showToast(r.message || '密码已重置')
    username.value = r.user || username.value
    password.value = ''
    closeReset()
  } catch (e: any) {
    showToast(e?.response?.data?.error || '重置失败')
  } finally {
    confirming.value = false
  }
}

function closeReset() {
  showReset.value = false
  stopTicker()
  step.value = 1
  codeFile.value = ''
  code.value = ''
  grant.value = ''
  newPassword.value = ''
  confirmPassword.value = ''
  remain.value = 0
}
</script>

<template>
  <div class="page login">
    <div class="brand">
      <!-- 与 web/public/icon.svg 同一标志：镜头 + 录制指示点 -->
      <svg viewBox="0 0 100 100" class="logo" role="img" aria-label="CyanNVR">
        <circle cx="44" cy="54" r="28.25" fill="none" stroke="#2ea8ff" stroke-width="7.5" />
        <circle cx="44" cy="54" r="20" fill="#2ea8ff" />
        <circle cx="36" cy="46" r="6" fill="#ffffff" fill-opacity="0.88" />
        <circle cx="50" cy="62" r="2.5" fill="#ffffff" fill-opacity="0.3" />
        <circle cx="80" cy="22" r="7.5" fill="#ff4d4f" />
      </svg>
      <h1>CyanNVR</h1>
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
      <button v-if="backendOk" class="forgot" type="button" @click="openReset">忘记密码？</button>
    </div>

    <p class="tip" v-if="!backendOk">
      当前为演示模式（未检测到后端），任意账号密码均可进入
    </p>

    <!-- 重置密码：1 生成重置码 → 2 验证换凭证 → 3 设置新密码 -->
    <van-popup v-model:show="showReset" round position="bottom" :style="{ paddingBottom: '24px' }">
      <div class="reset">
        <h3>重置密码</h3>

        <!-- 步骤 1：生成重置码 -->
        <template v-if="step === 1">
          <p class="reset-step">第 1 步 · 生成重置码</p>
          <p class="reset-desc">
            重置码不会显示在网页上，而是写入服务器数据目录的
            <code>reset-code.txt</code>。生成后请在
            <b>5 分钟内</b>通过 SSH、Docker exec 或文件管理器查看该文件，取回重置码。
          </p>
          <p class="reset-desc warn">超过 5 分钟该文件会被服务器自动删除，需要重新生成。</p>
          <van-button type="primary" block round :loading="issuing" @click="issueCode">
            生成重置码
          </van-button>
        </template>

        <!-- 步骤 2：查看文件并输入重置码 -->
        <template v-else-if="step === 2">
          <p class="reset-step">第 2 步 · 输入重置码</p>
          <div class="reset-file">
            <span class="lbl">重置码文件（在服务器上）</span>
            <code class="path">{{ codeFile }}</code>
          </div>
          <div class="countdown" :class="{ urgent: remain <= 60 }">
            <van-icon name="clock-o" size="14" />
            <span>剩余 {{ countdownText }}</span>
          </div>
          <van-cell-group inset class="reset-form">
            <van-field
              v-model="code"
              label="重置码"
              placeholder="8 位重置码"
              autocapitalize="characters"
              @keyup.enter="verifyCode"
            />
            <van-field
              v-model="username"
              label="用户名"
              placeholder="留空则重置唯一的管理员"
            />
          </van-cell-group>
          <div class="reset-btns">
            <van-button
              type="primary"
              block
              round
              :loading="verifying"
              :disabled="remain <= 0"
              @click="verifyCode"
            >
              验证重置码
            </van-button>
            <van-button size="small" plain round class="again" :loading="issuing" @click="issueCode">
              重新生成重置码
            </van-button>
          </div>
        </template>

        <!-- 步骤 3：设置新密码 -->
        <template v-else>
          <p class="reset-step">第 3 步 · 设置新密码</p>
          <p class="reset-desc">
            重置码已通过验证并销毁，正在为
            <b>{{ resetUser || username }}</b> 设置新密码。
          </p>
          <div class="countdown" :class="{ urgent: remain <= 60 }">
            <van-icon name="clock-o" size="14" />
            <span>请在 {{ countdownText }} 内完成</span>
          </div>
          <van-cell-group inset class="reset-form">
            <van-field
              v-model="newPassword"
              type="password"
              label="新密码"
              placeholder="至少 8 个字符"
            />
            <van-field
              v-model="confirmPassword"
              type="password"
              label="确认密码"
              placeholder="再次输入新密码"
              @keyup.enter="confirmReset"
            />
          </van-cell-group>
          <div class="reset-btns">
            <van-button
              type="primary"
              block
              round
              :loading="confirming"
              :disabled="remain <= 0"
              @click="confirmReset"
            >
              保存新密码
            </van-button>
          </div>
        </template>

        <div class="reset-btns">
          <van-button plain block round @click="closeReset">取消</van-button>
        </div>

        <p class="reset-hint">
          无法访问服务器文件？可直接在服务器上执行命令重置：<br />
          <code>cyannvr reset-password</code>
        </p>
      </div>
    </van-popup>
  </div>
</template>

<style scoped>
.login {
  display: flex;
  flex-direction: column;
  align-items: center;
  /* 用 flex 居中代替 vh 定位：关怀模式 zoom 放大后 vh 不会同步缩放，会溢出 */
  justify-content: center;
  padding: 32px 16px;
  box-sizing: border-box;
  min-height: 100%;
  width: 100%;
}
.brand {
  text-align: center;
  margin-bottom: 40px;
}
.logo {
  width: 64px;
  height: 64px;
  /* 标志本身没有底板（透明），无需圆角裁切 */
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
}
.tip {
  margin-top: 24px;
  font-size: 12px;
  color: var(--nvr-amber);
  text-align: center;
}

/* ---- 忘记密码 ---- */
.forgot {
  display: block;
  width: 100%;
  margin-top: 14px;
  padding: 6px 0;
  background: none;
  border: none;
  color: var(--nvr-text-2);
  font-size: 13px;
  text-decoration: underline;
  cursor: pointer;
}
.forgot:active {
  opacity: 0.6;
}
.reset {
  padding: 20px 16px 8px;
}
.reset h3 {
  margin: 0 0 16px;
  font-size: 17px;
  text-align: center;
}
.reset-step {
  margin: 0 0 8px;
  font-size: 13px;
  font-weight: 600;
  color: var(--nvr-accent);
}
.reset-desc {
  margin: 0 0 14px;
  font-size: 12px;
  line-height: 1.7;
  color: var(--nvr-text-2);
}
.reset-desc code {
  padding: 1px 4px;
  border-radius: 4px;
  background: var(--nvr-panel-2);
  font-family: ui-monospace, Menlo, monospace;
  word-break: break-all;
}
.reset-file {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-bottom: 10px;
  padding: 10px 12px;
  border-radius: 8px;
  background: var(--nvr-panel-2);
  border: 1px solid var(--nvr-border);
}
.reset-file .lbl {
  font-size: 11px;
  color: var(--nvr-text-2);
}
.reset-file .path {
  font-family: ui-monospace, Menlo, monospace;
  font-size: 12px;
  word-break: break-all;
}
.again {
  margin-bottom: 14px;
}
.reset-form {
  margin: 6px 0 0;
}
.reset-btns {
  margin: 18px 16px 0;
}
.reset-hint {
  margin: 18px 0 0;
  font-size: 12px;
  line-height: 1.8;
  color: var(--nvr-text-2);
  text-align: center;
}
.reset-hint code {
  padding: 1px 5px;
  border-radius: 4px;
  background: var(--nvr-panel-2);
  font-family: ui-monospace, Menlo, monospace;
}

/* 移动端适配 */
@media (max-width: 375px) {
  .login {
    padding: 24px 12px;
  }
  .brand h1 {
    font-size: 22px;
  }
  .logo {
    width: 56px;
    height: 56px;
  }
}

@media (max-width: 320px) {
  .login {
    padding: 16px 10px;
  }
  .brand h1 {
    font-size: 20px;
  }
  .brand p {
    font-size: 12px;
  }
}
</style>
