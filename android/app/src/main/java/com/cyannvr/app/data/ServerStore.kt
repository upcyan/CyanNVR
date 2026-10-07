package com.cyannvr.app.data

import android.content.Context
import android.content.SharedPreferences
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import com.cyannvr.app.model.NvrServer
import org.json.JSONArray
import org.json.JSONObject

/**
 * 服务器列表与登录凭据的本地持久化。
 *
 * 结构：
 *  - 普通 SharedPreferences 存服务器列表（JSON 数组），不含敏感信息。
 *  - EncryptedSharedPreferences 存每台服务器的密码与 token（加密落盘）。
 */
object ServerStore {
    private const val PREFS = "cyannvr_servers"
    private const val SECRET_PREFS = "cyannvr_secrets"
    private const val KEY_LIST = "servers"
    private const val KEY_LAST = "last_server_id"

    private lateinit var prefs: SharedPreferences
    private lateinit var secretPrefs: SharedPreferences

    /** 已保存凭据的字段集合 */
    data class Secret(val password: String?, val token: String?)

    fun init(context: Context) {
        if (::prefs.isInitialized) return
        prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        secretPrefs = runCatching {
            val masterKey = MasterKey.Builder(context)
                .setKeyScheme(MasterKey.KeyScheme.AES256_GCM)
                .build()
            EncryptedSharedPreferences.create(
                context,
                SECRET_PREFS,
                masterKey,
                EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
                EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
            )
        }.getOrElse {
            // 加密存储不可用时降级为普通存储，保证功能可用（凭据不再加密）
            context.getSharedPreferences(SECRET_PREFS, Context.MODE_PRIVATE)
        }
    }

    fun list(): List<NvrServer> {
        val raw = prefs.getString(KEY_LIST, "[]") ?: "[]"
        return runCatching { parseList(raw) }.getOrDefault(emptyList())
    }

    fun get(id: String): NvrServer? = list().firstOrNull { it.id == id }

    fun save(server: NvrServer) {
        val servers = list().filterNot { it.id == server.id } + server
        prefs.edit().putString(KEY_LIST, serialize(servers)).apply()
    }

    fun delete(id: String) {
        val servers = list().filterNot { it.id == id }
        prefs.edit().putString(KEY_LIST, serialize(servers)).apply()
        // 删除服务器时清掉其凭据
        secretPrefs.edit().remove(secretKey(id, "password")).remove(secretKey(id, "token")).apply()
    }

    fun secretOf(id: String): Secret = Secret(
        password = secretPrefs.getString(secretKey(id, "password"), null),
        token = secretPrefs.getString(secretKey(id, "token"), null),
    )

    fun savePassword(id: String, password: String?) {
        if (password == null) secretPrefs.edit().remove(secretKey(id, "password")).apply()
        else secretPrefs.edit().putString(secretKey(id, "password"), password).apply()
    }

    fun saveToken(id: String, token: String?) {
        if (token == null) secretPrefs.edit().remove(secretKey(id, "token")).apply()
        else secretPrefs.edit().putString(secretKey(id, "token"), token).apply()
    }

    fun lastServerId(): String? = prefs.getString(KEY_LAST, null)

    fun setLastServerId(id: String) {
        prefs.edit().putString(KEY_LAST, id).apply()
    }

    // ---- 序列化 ----

    private fun secretKey(id: String, field: String) = "$id.$field"

    private fun serialize(servers: List<NvrServer>): String =
        JSONArray().apply {
            servers.forEach { s ->
                put(
                    JSONObject()
                        .put("id", s.id)
                        .put("name", s.name)
                        .put("lanUrl", s.lanUrl)
                        .put("pubUrl", s.pubUrl ?: JSONObject.NULL)
                        .put("username", s.username)
                        .put("trustAnyCert", s.trustAnyCert)
                        .put("createdAt", s.createdAt),
                )
            }
        }.toString()

    private fun parseList(raw: String): List<NvrServer> {
        val arr = JSONArray(raw)
        return buildList {
            for (i in 0 until arr.length()) {
                val o = arr.getJSONObject(i)
                add(
                    NvrServer(
                        id = o.getString("id"),
                        name = o.getString("name"),
                        lanUrl = o.getString("lanUrl"),
                        pubUrl = if (o.isNull("pubUrl")) null else o.getString("pubUrl"),
                        username = o.optString("username", "admin"),
                        trustAnyCert = o.optBoolean("trustAnyCert", false),
                        createdAt = o.optLong("createdAt", System.currentTimeMillis()),
                    ),
                )
            }
        }
    }
}
