package com.cyannvr.app.net

import android.content.Context
import okhttp3.OkHttpClient
import okhttp3.Request
import org.json.JSONObject
import java.io.File
import java.util.concurrent.TimeUnit

/**
 * 从 GitHub Releases 检查更新、下载 APK。
 *
 * 更新源：https://github.com/upcyan/CyanNVR/releases/latest
 * 安装由调用方（UI）调起系统安装器完成（应用无系统签名，无法静默安装）。
 */
object UpdateManager {

    private val client: OkHttpClient = OkHttpClient.Builder()
        .connectTimeout(15, TimeUnit.SECONDS)
        .callTimeout(30, TimeUnit.SECONDS)
        .build()

    /** 长下载专用客户端：APK 较大，放宽超时 */
    private val downloadClient: OkHttpClient = OkHttpClient.Builder()
        .connectTimeout(15, TimeUnit.SECONDS)
        .readTimeout(5, TimeUnit.MINUTES)
        .callTimeout(10, TimeUnit.MINUTES)
        .build()

    data class UpdateInfo(
        val hasUpdate: Boolean,
        val latest: String,
        val current: String,
        val apkUrl: String?,
        val apkName: String?,
        val notes: String,
        val apkSize: Long = 0,
        val error: String? = null,
    )

    data class DownloadResult(
        val file: File,
        val cached: Boolean,
    )

    fun currentVersion(context: Context): String =
        runCatching {
            context.packageManager
                .getPackageInfo(context.packageName, 0)
                .versionName ?: "0"
        }.getOrDefault("0")

    /** 查询 GitHub latest release，返回是否有新版本 + APK 下载地址。 */
    fun check(context: Context): UpdateInfo {
        val current = currentVersion(context)
        val req = Request.Builder()
            .url("https://api.github.com/repos/upcyan/CyanNVR/releases/latest")
            .header("Accept", "application/vnd.github+json")
            .header("User-Agent", "CyanNVRApp")
            .get()
            .build()
        try {
            client.newCall(req).execute().use { resp ->
                if (!resp.isSuccessful) {
                    return UpdateInfo(false, "", current, null, null, "", error = "检查失败（HTTP ${resp.code}）")
                }
                val o = JSONObject(resp.body?.string() ?: return UpdateInfo(false, "", current, null, null, "", error = "响应为空"))
                val tag = o.optString("tag_name", "").removePrefix("v")
                var apkUrl: String? = null
                var apkName: String? = null
                var apkSize = 0L
                val assets = o.optJSONArray("assets")
                if (assets != null) {
                    for (i in 0 until assets.length()) {
                        val a = assets.optJSONObject(i)
                        val name = a?.optString("name", "") ?: ""
                        if (name.endsWith(".apk")) {
                            apkUrl = a.optString("browser_download_url", "")
                            apkName = name
                            apkSize = a.optLong("size", 0)
                            break
                        }
                    }
                }
                return UpdateInfo(
                    hasUpdate = compareVersion(tag, current) > 0,
                    latest = tag,
                    current = current,
                    apkUrl = apkUrl,
                    apkName = apkName,
                    notes = o.optString("body", "").trim(),
                    apkSize = apkSize,
                )
            }
        } catch (e: Exception) {
            return UpdateInfo(false, "", current, null, null, "", error = "检查失败：${e.message ?: "网络错误"}")
        }
    }

    /** 下载 APK 到应用外部缓存目录（可被安装器读取），返回文件。 */
    fun download(context: Context, info: UpdateInfo, onProgress: ((Long, Long) -> Unit)? = null): DownloadResult {
        val url = info.apkUrl ?: throw IllegalStateException("没有可下载的 APK 地址")
        val name = info.apkName ?: "CyanNVR_${info.latest}.apk"
        val dir = context.getExternalFilesDir("updates") ?: context.cacheDir
        dir.mkdirs()
        val target = File(dir, name)

        // 只复用完整包，避免上次失败遗留的非空文件被误认为下载完成。
        if (target.exists() && target.length() > 0 && (info.apkSize <= 0 || target.length() == info.apkSize)) {
            onProgress?.invoke(target.length(), target.length())
            return DownloadResult(target, true)
        }
        val req = Request.Builder().url(url).header("User-Agent", "CyanNVRApp").get().build()
        downloadClient.newCall(req).execute().use { resp ->
            if (!resp.isSuccessful) throw IllegalStateException("下载失败（HTTP ${resp.code}）")
            val body = resp.body ?: throw IllegalStateException("下载响应为空")
            val total = if (info.apkSize > 0) info.apkSize else body.contentLength()
            val tmp = File(dir, "$name.part")
            try {
                copyUpdatePackage(body.byteStream(), tmp, total, onProgress)
                check(tmp.renameTo(target)) { "无法保存更新包" }
                return DownloadResult(target, false)
            } finally {
                if (tmp.exists()) tmp.delete()
            }
        }
    }

    /** 比较 x.y.z 版本，a>b 返回正数。 */
    fun compareVersion(a: String, b: String): Int {
        val pa = a.removePrefix("v").split(".")
        val pb = b.removePrefix("v").split(".")
        for (i in 0 until 3) {
            val na = pa.getOrNull(i)?.toIntOrNull() ?: 0
            val nb = pb.getOrNull(i)?.toIntOrNull() ?: 0
            if (na != nb) return na - nb
        }
        return 0
    }

    /** 调起系统安装器安装已下载的 APK。 */
    fun installApk(context: android.content.Context, file: java.io.File) {
        if (android.os.Build.VERSION.SDK_INT >= 26 && !context.packageManager.canRequestPackageInstalls()) {
            context.startActivity(android.content.Intent(
                android.provider.Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES,
                android.net.Uri.parse("package:${context.packageName}"),
            ).addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK))
            android.widget.Toast.makeText(context, "请允许安装更新，返回后点击安装", android.widget.Toast.LENGTH_LONG).show()
            return
        }
        val uri = androidx.core.content.FileProvider.getUriForFile(
            context,
            "${context.packageName}.fileprovider",
            file,
        )
        val intent = android.content.Intent(android.content.Intent.ACTION_VIEW).apply {
            setDataAndType(uri, "application/vnd.android.package-archive")
            addFlags(android.content.Intent.FLAG_GRANT_READ_URI_PERMISSION or android.content.Intent.FLAG_ACTIVITY_NEW_TASK)
        }
        context.startActivity(intent)
    }
}

/** 独立于 Android UI 的下载复制逻辑：报告字节进度，拒绝空包和截断包。 */
internal fun copyUpdatePackage(input: java.io.InputStream, target: File, total: Long,
    onProgress: ((Long, Long) -> Unit)? = null) {
    try {
        var done = 0L
        var lastReport = 0L
        onProgress?.invoke(0, total)
        input.use { source ->
            target.outputStream().use { output ->
                val buffer = ByteArray(64 * 1024)
                while (true) {
                    val read = source.read(buffer)
                    if (read < 0) break
                    output.write(buffer, 0, read)
                    done += read
                    val now = System.nanoTime()
                    if (now - lastReport >= 100_000_000L) {
                        onProgress?.invoke(done, total)
                        lastReport = now
                    }
                }
            }
        }
        check(done > 0) { "下载内容为空" }
        check(total <= 0 || done == total) { "下载不完整，请重试" }
        onProgress?.invoke(done, if (total > 0) total else done)
    } catch (e: Exception) {
        target.delete()
        throw e
    }
}
