package com.cyannvr.app.model

/** 一台已保存的 CyanNVR 服务器（不含密码/令牌等敏感信息） */
data class NvrServer(
    val id: String,
    val name: String,
    /** 局域网地址，形如 http://192.168.1.100:8080（无尾斜杠） */
    val lanUrl: String,
    /** 可选的公网地址，用于外网访问时的故障切换 */
    val pubUrl: String? = null,
    val username: String = "admin",
    val trustAnyCert: Boolean = false,
    val createdAt: Long = System.currentTimeMillis(),
) {
    /** 按优先级返回候选地址：局域网优先，公网兜底 */
    fun candidateUrls(): List<String> =
        listOf(lanUrl, pubUrl ?: "").filter { it.isNotBlank() }.distinct()
}

/** /api/health 探测结果 */
data class HealthInfo(
    val name: String,
    val status: String,
    val latencyMs: Long,
) {
    val isNvr: Boolean get() = name == "CyanNVR"
}

data class LoginResult(
    val token: String,
    val userJson: String,
    val username: String,
)

/** 局域网扫描发现的一台主机 */
data class ScanHit(
    val baseUrl: String,
    val name: String,
    val latencyMs: Long,
)

/** 连接状态机 */
sealed interface ConnState {
    data object Idle : ConnState
    data class Connecting(val server: NvrServer, val step: String) : ConnState
    data class NeedsLogin(val server: NvrServer, val baseUrl: String, val reason: String) : ConnState
    data class Connected(
        val server: NvrServer,
        val baseUrl: String,
        val viaLan: Boolean,
        val login: LoginResult,
    ) : ConnState

    data class Failed(val server: NvrServer?, val message: String) : ConnState
}
