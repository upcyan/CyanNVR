package com.cyannvr.app.net

import com.cyannvr.app.model.HealthInfo
import com.cyannvr.app.model.LoginResult
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject
import java.security.SecureRandom
import java.security.cert.X509Certificate
import java.util.concurrent.TimeUnit
import javax.net.ssl.SSLContext
import javax.net.ssl.TrustManager
import javax.net.ssl.X509TrustManager

sealed interface NvrError {
    data class Message(val text: String) : NvrError
}

/** CyanNVR 服务端 HTTP 客户端：健康探测、登录、令牌校验。 */
object NvrClient {

    /** 探测用：快速失败 */
    val probeClient: OkHttpClient = baseClient()
        .connectTimeout(1500, TimeUnit.MILLISECONDS)
        .callTimeout(2500, TimeUnit.MILLISECONDS)
        .build()

    /** 登录等常规请求 */
    val apiClient: OkHttpClient = baseClient()
        .connectTimeout(4, TimeUnit.SECONDS)
        .callTimeout(8, TimeUnit.SECONDS)
        .build()

    /** 信任一切证书的客户端（仅在用户为该服务器开启"信任自签名证书"时使用） */
    val trustAllClient: OkHttpClient by lazy {
        val tm = object : X509TrustManager {
            override fun checkClientTrusted(chain: Array<X509Certificate>, authType: String) {}
            override fun checkServerTrusted(chain: Array<X509Certificate>, authType: String) {}
            override fun getAcceptedIssuers(): Array<X509Certificate> = arrayOf()
        }
        val ssl = SSLContext.getInstance("TLS")
        ssl.init(null, arrayOf<TrustManager>(tm), SecureRandom())
        baseClient()
            .sslSocketFactory(ssl.socketFactory, tm)
            .hostnameVerifier { _, _ -> true }
            .connectTimeout(4, TimeUnit.SECONDS)
            .callTimeout(8, TimeUnit.SECONDS)
            .build()
    }

    private fun baseClient(): OkHttpClient.Builder = OkHttpClient.Builder()

    fun clientFor(trustAnyCert: Boolean): OkHttpClient =
        if (trustAnyCert) trustAllClient else apiClient

    /**
     * 探测服务端健康状态并校验 CyanNVR 指纹。
     * @return 成功返回 HealthInfo；失败返回 null（网络不通/超时/非 CyanNVR）。
     */
    fun probe(client: OkHttpClient, baseUrl: String): HealthInfo? {
        val start = System.currentTimeMillis()
        return try {
            val req = Request.Builder()
                .url(trimBase(baseUrl) + "/api/health")
                .get()
                .build()
            client.newCall(req).execute().use { resp ->
                val latency = System.currentTimeMillis() - start
                if (!resp.isSuccessful) return null
                val body = resp.body?.string() ?: return null
                val o = JSONObject(body)
                val info = HealthInfo(
                    name = o.optString("name", ""),
                    status = o.optString("status", ""),
                    latencyMs = latency,
                )
                if (info.isNvr) info else null
            }
        } catch (_: Exception) {
            null
        }
    }

    /** 登录，返回 token + 用户 JSON。失败抛 [NvrException]。 */
    fun login(client: OkHttpClient, baseUrl: String, username: String, password: String): LoginResult {
        val body = JSONObject().apply {
            put("username", username.trim())
            put("password", password)
        }.toString().toRequestBody("application/json; charset=utf-8".toMediaType())
        val req = Request.Builder()
            .url(trimBase(baseUrl) + "/api/auth/login")
            .post(body)
            .build()
        try {
            client.newCall(req).execute().use { resp ->
                val text = resp.body?.string() ?: ""
                when {
                    resp.isSuccessful -> {
                        val o = JSONObject(text)
                        val token = o.optString("token", "")
                        if (token.isEmpty()) throw NvrException("服务端响应缺少令牌")
                        val user = o.optJSONObject("user") ?: JSONObject()
                        return LoginResult(
                            token = token,
                            userJson = user.toString(),
                            username = user.optString("username", username.trim()),
                        )
                    }
                    resp.code == 401 -> throw NvrException("用户名或密码错误")
                    resp.code == 429 -> throw NvrException("尝试过于频繁，请 1 分钟后再试")
                    else -> throw NvrException("登录失败（HTTP ${resp.code}）")
                }
            }
        } catch (e: NvrException) {
            throw e
        } catch (e: Exception) {
            throw NvrException("无法连接服务器：${e.message ?: "网络错误"}")
        }
    }

    /** 用已有 token 校验会话，返回用户 JSON；token 失效返回 null。 */
    fun checkToken(client: OkHttpClient, baseUrl: String, token: String): String? {
        return try {
            val req = Request.Builder()
                .url(trimBase(baseUrl) + "/api/auth/me")
                .header("Authorization", "Bearer $token")
                .get()
                .build()
            client.newCall(req).execute().use { resp ->
                if (!resp.isSuccessful) return null
                val o = JSONObject(resp.body?.string() ?: return null)
                o.optJSONObject("user")?.toString()
            }
        } catch (_: Exception) {
            null
        }
    }

    fun trimBase(url: String): String = url.trim().trimEnd('/')

    /**
     * 解析配对内容（二维码扫码 / cyannvr:// 深链 / 手动粘贴）。
     * 支持 cyannvr://host[:port]、http(s)://… 与裸地址，返回规范化 base url 或 null。
     */
    fun parsePairingPayload(raw: String): String? {
        val s = raw.trim()
        if (s.isEmpty()) return null
        return when {
            s.startsWith("cyannvr://") -> {
                val rest = s.removePrefix("cyannvr://").substringBefore('/').trimEnd('/')
                if (rest.isEmpty() || !rest.contains('.') && !rest.contains(':')) return null
                if (rest.contains(':')) "http://$rest" else "http://$rest:8080"
            }
            else -> normalizeBaseUrl(s)
        }
    }

    /** 智能归一化用户输入的地址：补协议、补默认端口 8080，去掉路径与尾斜杠 */
    fun normalizeBaseUrl(input: String): String? {
        var s = input.trim()
        if (s.isEmpty()) return null
        if (!s.startsWith("http://") && !s.startsWith("https://")) {
            // 容错：可能用户粘贴了带路径的完整地址
            s = "http://$s"
        }
        return try {
            val url = java.net.URL(s)
            val port = when {
                url.port != -1 -> url.port
                url.protocol == "https" -> -1 // https 走 443，多为反向代理
                else -> 8080 // CyanNVR 默认端口
            }
            val hostPart = if (port == -1) url.host else "${url.host}:$port"
            "${url.protocol}://$hostPart"
        } catch (_: Exception) {
            null
        }
    }
}

class NvrException(message: String) : Exception(message)
