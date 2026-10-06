<script setup lang="ts">
import { computed, onBeforeUnmount, ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { showToast } from 'vant'
import { useAuthStore } from '../stores/auth'
import { apiResetConfirm, apiResetRequest, apiResetVerify, connState, fetchMe, isBackend, refreshConnection } from '../api'

const auth = useAuthStore()

// 键盘遮挡兜底：visualViewport 缩小时（软键盘弹出）把偏移量注入容器
// padding-bottom，让用户名/密码输入框始终在可视区内。
const kbOffset = ref(0)
function onVVResize() {
  const vv = window.visualViewport
  if (!vv) return
  kbOffset.value = Math.max(0, Math.round(window.innerHeight - vv.height))
}
onMounted(() => {
  const vv = window.visualViewport
  if (!vv) return
  vv.addEventListener('resize', onVVResize)
  onVVResize()
})
onBeforeUnmount(() => {
  window.visualViewport?.removeEventListener('resize', onVVResize)
})
const route = useRoute()
const router = useRouter()

// 免登录自动进入（fpk 场景的关键修复）。
// 飞牛桌面入口是 iframe 内嵌（fpk/app/ui/config "type":"iframe"），每次打开都是
// 全新页面加载，且路由 / 固定重定向到 /login——此前即使本地 token 有效（或已
// 过期但仍在服务端信任窗口内、可经 X-Renewed-Token 静默续签），用户也会被摁在
// 登录页重新输密码，表现为「web 里免登录正常、fpk 里不生效」（浏览器地址直开
// 落在 /login 时同样受影响）。现在登录页挂载时若本地持有 token 且后端可达，
// 静默调 fetchMe 验证：有效或窗口内已续签 → 直达目标页；失败（吊销/窗口已过/
// 改密）→ 留在登录页，行为与旧版一致。自动登录期间：表单预填「当前用户名 +
// 六个点」并置为只读（避免用户刚开始输入就被导航打断），按钮显示「自动登录中」；
// 凭据校验结束后 resuming 归 false，表单自动恢复可交互供手动登录。
const PASSWORD_MASK = '••••••'
const resuming = ref(!!auth.token && isBackend())
const resumeUsername = computed(() => auth.user?.username || '')
function onUsernameInput(v: string | number) {
  if (!resuming.value) username.value = String(v)
}
function onPasswordInput(v: string | number) {
  if (!resuming.value) password.value = String(v)
}
onMounted(async () => {
  if (!resuming.value) return
  try {
    const user = await fetchMe()
    auth.user = user
    localStorage.setItem('nvr_user', JSON.stringify(user))
    const raw = (route.query.redirect as string) || '/live'
    // 与 submit 相同的开放重定向防护：只接受站内相对路径
    const redirect = raw.startsWith('/') && !raw.startsWith('//') && !raw.includes('://') ? raw : '/live'
    router.replace(redirect)
  } catch {
    /* 凭据失效：留在登录页。401 时 client.ts 拦截器已清空失效会话 */
  } finally {
    resuming.value = false
  }
})

const username = ref('')
const password = ref('')
const loading = ref(false)

/* 连接状态驱动界面提示：
   · offline  —— 服务器不可达，给出明确提示与重试入口（不再谎称「演示模式」）；
   · connected —— 后端可用，才显示「忘记密码」（该流程需要服务端生成重置码）。 */
const offline = computed(() => connState.value === 'offline')
const checking = computed(() => connState.value === 'checking')
const connected = computed(() => isBackend())

async function retryConnection() {
  const ok = await refreshConnection()
  if (ok) showToast('已连接服务器')
  else showToast('仍无法连接服务器')
}

async function submit() {
  // 自动登录进行中不接受手动提交（校验结束后表单自动恢复可交互）
  if (resuming.value) return
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
    // 连接类错误由 auth.login 直接抛出 Error（无 response），也要显示出来
    showToast(e?.response?.data?.error || e?.message || '登录失败')
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
  <div class="page login" :style="kbOffset ? { paddingBottom: kbOffset + 'px' } : undefined">
    <div class="brand">
      <!-- 与 web/public/icon.svg 同一标志：镜头 + 录制指示点 -->
      <svg viewBox="0 0 100 100" class="logo" role="img" aria-label="CyanNVR">
        <circle cx="44" cy="54" r="30" fill="none" stroke="#2ea8ff" stroke-width="8" />
        <circle cx="44" cy="54" r="22" fill="#2ea8ff" />
        <path d="M 32 44 A 16 16 0 0 1 44 38" fill="none" stroke="#ffffff" stroke-width="4" stroke-linecap="round" opacity="0.9" />
        <circle cx="78" cy="24" r="9" fill="#ff4d4f" />
      </svg>
      <h1>CyanNVR</h1>
      <p>网络视频录像机</p>
    </div>

    <van-cell-group inset class="form">
      <van-field
        :model-value="resuming ? resumeUsername : username"
        :readonly="resuming"
        name="username"
        autocomplete="username"
        autocapitalize="none"
        label="用户名"
        placeholder="请输入用户名"
        @update:model-value="onUsernameInput"
      />
      <van-field
        :model-value="resuming ? PASSWORD_MASK : password"
        :readonly="resuming"
        type="password"
        name="password"
        autocomplete="current-password"
        label="密码"
        placeholder="请输入密码"
        @update:model-value="onPasswordInput"
        @keyup.enter="submit"
      />
    </van-cell-group>

    <div class="btns">
      <van-button
        type="primary"
        block
        round
        :loading="loading || resuming"
        :loading-text="resuming ? '自动登录中' : undefined"
        @click="submit"
      >登 录</van-button>
      <van-button plain block round style="margin-top: 10px" @click="$router.push('/server')">
        服务器设置
      </van-button>
      <button v-if="connected" class="forgot" type="button" @click="openReset">忘记密码？</button>
    </div>

    <p class="tip offline" v-if="offline">
      无法连接服务器，请检查网络或
      <a href="#/server">服务器设置</a>。
      <button type="button" class="retry" :disabled="checking" @click="retryConnection">
        {{ checking ? '正在重试…' : '重试连接' }}
      </button>
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
  position: relative;
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
/* 氛围底：顶部品牌蓝辉光 + 细点阵，纯 CSS 无图片资源。
   只用 ::before 伪元素，不加 filter/transform：
   本页「忘记密码」弹窗是后代节点，祖先建层会劫持其 fixed 定位。 */
.login::before {
  content: '';
  position: absolute;
  inset: 0;
  background:
    radial-gradient(600px 320px at 50% -8%, rgba(46, 168, 255, 0.16), transparent 65%),
    radial-gradient(rgba(255, 255, 255, 0.05) 1px, transparent 1px);
  background-size: auto, 22px 22px;
  pointer-events: none;
}
body.light .login::before {
  background:
    radial-gradient(600px 320px at 50% -8%, rgba(31, 140, 224, 0.10), transparent 65%),
    radial-gradient(rgba(15, 34, 58, 0.06) 1px, transparent 1px);
  background-size: auto, 22px 22px;
}
/* 内容浮于氛围层之上（只提内容节点，不动弹层） */
.login > .brand,
.login > .form,
.login > .btns,
.login > .tip {
  position: relative;
  z-index: 1;
}
.tip.offline {
  color: var(--nvr-warning);
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  line-height: 1.6;
}
.tip.offline a { color: var(--nvr-accent); }
.retry {
  min-height: 40px;
  padding: 6px 16px;
  border-radius: var(--nvr-radius-full);
  border: 1px solid var(--nvr-border);
  background: var(--nvr-panel-2);
  color: var(--nvr-text);
  font-size: calc(14px * var(--nvr-font-scale, 1));
  cursor: pointer;
}
.retry:disabled { opacity: .6; cursor: progress; }
.tip {
  position: relative;
  z-index: 1;
}
/* 关怀模式：登录页字号偏小，按钮和品牌区需放大；form 限宽让大屏更易扫读 */
:global(body.care .login .brand h1) {
  font-size: calc(30px * var(--nvr-font-scale, 1));
}
:global(body.care .login .brand p) {
  font-size: calc(16px * var(--nvr-font-scale, 1));
}
:global(body.care .login .tip) {
  font-size: calc(15px * var(--nvr-font-scale, 1));
}
:global(body.care .login .forgot) {
  font-size: calc(16px * var(--nvr-font-scale, 1));
  min-height: 44px;
}
:global(body.care .login .reset h3) {
  font-size: calc(22px * var(--nvr-font-scale, 1));
}
:global(body.care .login .reset-step) {
  font-size: calc(16px * var(--nvr-font-scale, 1));
}
:global(body.care .login .reset-desc) {
  font-size: calc(15px * var(--nvr-font-scale, 1));
}
:global(body.care .login .reset-hint) {
  font-size: calc(15px * var(--nvr-font-scale, 1));
}
:global(body.care .login .logo) {
  width: 80px;
  height: 80px;
}
.brand {
  text-align: center;
  margin-bottom: 40px;
}
/* 键盘遮挡修复：移动端软键盘弹出时布局视口不收缩（或不支持
   interactive-widget 的浏览器），居中布局的用户名/密码框会被键盘盖住。
   输入聚焦时收纳品牌区（logo 缩小、副标题隐藏、间距收紧），把表单
   顶到可视区上半部；配合 index.html 的 interactive-widget=resizes-content。 */
.login:focus-within .brand {
  margin-bottom: 10px;
}
.login:focus-within .brand .logo {
  width: 36px;
  height: 36px;
}
.login:focus-within .brand p {
  display: none;
}
.login:focus-within .brand h1 {
  font-size: calc(16px * var(--nvr-font-scale, 1));
}
.logo {
  width: 64px;
  height: 64px;
  /* 标志本身没有底板（透明），无需圆角裁切；光晕用 drop-shadow 跟随镜头形状 */
  filter: drop-shadow(0 10px 26px rgba(46, 168, 255, 0.35));
}
.brand h1 {
  margin: 16px 0 4px;
  font-size: calc(26px * var(--nvr-font-scale, 1));
  /* 品牌名渐变文字：与强调线、在线率条同一渐变语言 */
  background: var(--nvr-grad-accent);
  -webkit-background-clip: text;
  background-clip: text;
  -webkit-text-fill-color: transparent;
}
.brand p {
  margin: 0;
  color: var(--nvr-text-2);
  font-size: calc(13px * var(--nvr-font-scale, 1));
}
.form {
  width: 100%;
  max-width: 420px;
  margin: 0 auto;
}
.form :deep(.van-field__control) {
  box-sizing: border-box;
  min-height: 44px;
  padding: 10px 12px;
  border: 1px solid var(--nvr-border);
  border-radius: 12px;
  background: var(--nvr-panel-2);
  color: var(--nvr-text);
  -webkit-appearance: none;
  appearance: none;
}
.form :deep(.van-field__control:focus) {
  outline: 2px solid var(--nvr-accent);
  outline-offset: 1px;
}
.form :deep(.van-field) {
  align-items: center;
}
.form :deep(.van-field__label) {
  width: 4.6em;
}
.form :deep(input:-webkit-autofill) {
  -webkit-text-fill-color: var(--nvr-text);
  caret-color: var(--nvr-text);
  -webkit-box-shadow: 0 0 0 1000px var(--nvr-panel-2) inset;
  border-radius: 12px;
}
.form :deep(input:autofill) {
  border-radius: 12px;
}
.btns {
  width: 100%;
  max-width: 420px;
  margin: 24px auto 0;
}
.tip {
  margin-top: 24px;
  font-size: calc(12px * var(--nvr-font-scale, 1));
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
  font-size: calc(13px * var(--nvr-font-scale, 1));
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
  font-size: calc(17px * var(--nvr-font-scale, 1));
  text-align: center;
}
.reset-step {
  margin: 0 0 8px;
  font-size: calc(13px * var(--nvr-font-scale, 1));
  font-weight: 600;
  color: var(--nvr-accent);
}
.reset-desc {
  margin: 0 0 14px;
  font-size: calc(12px * var(--nvr-font-scale, 1));
  line-height: 1.7;
  color: var(--nvr-text-2);
}
.reset-desc code {
  padding: 1px 4px;
  border-radius: var(--nvr-radius-sm);
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
  border-radius: var(--nvr-radius-sm);
  background: var(--nvr-panel-2);
  border: 1px solid var(--nvr-border);
}
.reset-file .lbl {
  font-size: calc(11px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
}
.reset-file .path {
  font-family: ui-monospace, Menlo, monospace;
  font-size: calc(12px * var(--nvr-font-scale, 1));
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
  font-size: calc(12px * var(--nvr-font-scale, 1));
  line-height: 1.8;
  color: var(--nvr-text-2);
  text-align: center;
}
.reset-hint code {
  padding: 1px 5px;
  border-radius: var(--nvr-radius-sm);
  background: var(--nvr-panel-2);
  font-family: ui-monospace, Menlo, monospace;
}

/* 移动端适配 */
@media (max-width: 375px) {
  .login {
    padding: 24px 12px;
  }
  .brand h1 {
    font-size: calc(22px * var(--nvr-font-scale, 1));
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
    font-size: calc(20px * var(--nvr-font-scale, 1));
  }
  .brand p {
    font-size: calc(12px * var(--nvr-font-scale, 1));
  }
}
</style>
