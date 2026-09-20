package com.cyannvr.app.net

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.cyannvr.app.data.ServerStore
import com.cyannvr.app.model.ConnState
import com.cyannvr.app.model.LoginResult
import com.cyannvr.app.model.NvrServer
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/**
 * 连接中枢：负责"如何连上服务端"的全部决策——
 * 地址候选探测（局域网→公网）、令牌校验与静默重登、网络变化时的自动故障切换。
 */
object ConnectionManager {

    val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    var state by mutableStateOf<ConnState>(ConnState.Idle)
        private set

    /** 连接横幅提示（已切换公网 / 连接断开等待恢复 等），null 表示不显示 */
    var banner by mutableStateOf<String?>(null)
        private set

    /** 递增以通知 WebView 用最新会话重新加载 */
    var reloadTick by mutableStateOf(0)
        private set

    private var netCallbackRegistered = false
    private var connectJob: kotlinx.coroutines.Job? = null

    fun connected(): ConnState.Connected? = state as? ConnState.Connected

    // ---- 连接流程 ----

    /** 连接一台服务器：探测候选地址 → 校验/恢复会话。全程在 IO 线程。 */
    fun connect(server: NvrServer, passwordFromSheet: String? = null, remember: Boolean = true) {
        state = ConnState.Connecting(server, "正在探测服务器…")
        connectJob = scope.launch {
            val client = NvrClient.clientFor(server.trustAnyCert)
            // 1) 探测候选地址：局域网优先，公网兜底
            var baseUrl: String? = null
            var viaLan = true
            for ((i, candidate) in server.candidateUrls().withIndex()) {
                state = ConnState.Connecting(server, "正在探测 ${NvrClient.trimBase(candidate)} …")
                if (NvrClient.probe(client, candidate) != null) {
                    baseUrl = NvrClient.trimBase(candidate)
                    viaLan = i == 0
                    break
                }
            }
            if (baseUrl == null) {
                state = ConnState.Failed(
                    server,
                    "所有地址均不可达（${server.candidateUrls().joinToString("、")}）。\n" +
                        "请检查：手机与 NVR 是否同一局域网、IP 端口是否正确、服务端是否已启动。",
                )
                return@launch
            }

            // 2) 会话恢复：缓存的 token 仍有效则直接用；失效且有记住的密码则静默重登
            val secret = ServerStore.secretOf(server.id)
            val cachedToken = secret.token
            if (cachedToken != null) {
                state = ConnState.Connecting(server, "正在恢复登录会话…")
                val userJson = NvrClient.checkToken(client, baseUrl, cachedToken)
                if (userJson != null) {
                    succeed(server, baseUrl, viaLan, LoginResult(cachedToken, userJson, server.username))
                    return@launch
                }
            }
            val password = passwordFromSheet ?: secret.password
            if (password != null) {
                state = ConnState.Connecting(server, "正在登录…")
                try {
                    val result = NvrClient.login(client, baseUrl, server.username, password)
                    if (remember && passwordFromSheet != null) {
                        ServerStore.savePassword(server.id, password)
                    }
                    ServerStore.saveToken(server.id, result.token)
                    succeed(server, baseUrl, viaLan, result)
                } catch (e: NvrException) {
                    state = ConnState.NeedsLogin(server, baseUrl, e.message ?: "登录失败")
                }
                return@launch
            }

            // 3) 没有可用凭证，请用户登录
            state = ConnState.NeedsLogin(server, baseUrl, "请登录 ${server.name}")
        }
    }

    /** 登录页提交（含"登录已过期"场景） */
    fun loginAndFinish(server: NvrServer, baseUrl: String?, username: String, password: String, remember: Boolean, onResult: (String?) -> Unit) {
        val base = baseUrl ?: (state as? ConnState.Connected)?.baseUrl ?: server.lanUrl
        connectJob = scope.launch {            try {
                val client = NvrClient.clientFor(server.trustAnyCert)
                val result = NvrClient.login(client, base, username, password)
                if (remember) ServerStore.savePassword(server.id, password)
                ServerStore.saveToken(server.id, result.token)
                val updated = server.copy(username = username)
                ServerStore.save(updated)
                val viaLan = base == NvrClient.trimBase(updated.lanUrl)
                succeed(updated, base, viaLan, result)
                onResult(null)
            } catch (e: NvrException) {
                onResult(e.message)
            }
        }
    }

    private fun succeed(server: NvrServer, baseUrl: String, viaLan: Boolean, login: LoginResult) {
        ServerStore.setLastServerId(server.id)
        banner = if (viaLan) null else "已通过公网地址连接"
        state = ConnState.Connected(server, baseUrl, viaLan, login)
        reloadTick++
    }

    fun disconnect() {
        connected()?.let { ServerStore.saveToken(it.server.id, null) }
        state = ConnState.Idle
        banner = null
    }

    /** 用户取消连接过程 */
    fun cancelConnect() {
        connectJob?.cancel()
        connectJob = null
        if (state is ConnState.Connecting) state = ConnState.Idle
    }

    /** 从"连接失败"返回列表 */
    fun resetToIdle() {
        if (state is ConnState.Failed) {
            state = ConnState.Idle
            banner = null
        }
    }

    /** 登录过期回调（WebView 内 401 → 前端跳 #/login） */
    fun onSessionExpired(): NvrServer? {
        val c = connected() ?: return null
        state = ConnState.NeedsLogin(c.server, c.baseUrl, "登录已过期，请重新登录")
        return c.server
    }

    // ---- 网络监控与故障切换 ----

    fun startNetworkWatch(context: Context) {
        if (netCallbackRegistered) return
        val cm = context.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
        val request = NetworkRequest.Builder()
            .addTransportType(NetworkCapabilities.TRANSPORT_WIFI)
            .addTransportType(NetworkCapabilities.TRANSPORT_CELLULAR)
            .addTransportType(NetworkCapabilities.TRANSPORT_ETHERNET)
            .build()
        cm.registerNetworkCallback(request, object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) {
                scheduleRecheck("网络已切换")
            }

            override fun onLost(network: Network) {
                scheduleRecheck(null)
            }
        })
        netCallbackRegistered = true
    }

    private var recheckPending = false

    private fun scheduleRecheck(reason: String?) {
        if (state !is ConnState.Connected) return
        if (recheckPending) return
        recheckPending = true
        scope.launch {
            delay(1000)
            recheckPending = false
            revalidate(reason)
        }
    }

    /** 检查当前地址是否仍然可达，不通则切换候选地址 */
    fun revalidate(reason: String?) {
        val c = connected() ?: return
        val client = NvrClient.clientFor(c.server.trustAnyCert)
        if (NvrClient.probe(client, c.baseUrl) != null) {
            if (banner?.contains("断开") == true) banner = null
            return
        }
        // 当前地址不可达 → 尝试其它候选
        for ((i, candidate) in c.server.candidateUrls().withIndex()) {
            val trimmed = NvrClient.trimBase(candidate)
            if (trimmed == c.baseUrl) continue
            if (NvrClient.probe(client, trimmed) != null) {
                val secret = ServerStore.secretOf(c.server.id)
                val token = secret.token
                if (token != null && NvrClient.checkToken(client, trimmed, token) != null) {
                    banner = "${reason ?: "地址失效"}，已切换到${if (i == 0) "局域网" else "公网"}地址"
                    succeed(c.server, trimmed, i == 0, c.login.copy(token = token))
                    return
                }
            }
        }
        banner = "连接已断开，正在等待网络恢复…"
    }

    /** 手动重试当前服务器（WebView 错误页/横幅使用） */
    fun retryCurrent() {
        val server: NvrServer? = when (val s = state) {
            is ConnState.Connected -> s.server
            is ConnState.Failed -> s.server
            is ConnState.NeedsLogin -> s.server
            else -> ServerStore.get(ServerStore.lastServerId() ?: "")
        }
        if (server == null) return
        banner = null
        connect(server)
    }
}
