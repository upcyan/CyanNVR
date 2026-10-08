package com.cyannvr.app.net

import android.content.Context
import android.content.pm.PackageManager
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
                    return UpdateInfo(false, "", current, null, null, "检查失败（HTTP ${resp.code}）")
                }
                val o = JSONObject(resp.body?.string() ?: return UpdateInfo(false, "", current, null, null, "响应为空"))
                val tag = o.optString("tag_name", "").removePrefix("v")
                var apkUrl: String? = null
                var apkName: String? = null
                val assets = o.optJSONArray("assets")
                if (assets != null) {
                    for (i in 0 until assets.length()) {
                        val a = assets.optJSONObject(i)
                        val name = a?.optString("name", "") ?: ""
                        if (name.endsWith(".apk")) {
                            apkUrl = a.optString("browser_download_url", "")
                            apkName = name
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
                )
            }
        } catch (e: Exception) {
            return UpdateInfo(false, "", current, null, null, "检查失败：${e.message ?: "网络错误"}")
        }
    }

    /** 下载 APK 到应用外部缓存目录（可被安装器读取），返回文件。 */
    fun download(context: Context, info: UpdateInfo, onProgress: ((Long, Long) -> Unit)? = null): DownloadResult {
        val url = info.apkUrl ?: throw IllegalStateException("没有可下载的 APK 地址")
        val name = info.apkName ?: "CyanNVR_${info.latest}.apk"
        val dir = context.getExternalFilesDir("updates") ?: context.cacheDir
        dir.mkdirs()
        val target = File(dir, name)

        // 已存在且大小非 0，视为已下载
        if (target.exists() && target.length() > 0) {
            return DownloadResult(target, true)
        }

        val req = Request.Builder().url(url).header("User-Agent", "CyanNVRApp").get().build()
        downloadClient.newCall(req).execute().use { resp ->
            if (!resp.isSuccessful) throw IllegalStateException("下载失败（HTTP ${resp.code}）")
            val body = resp.body ?: throw IllegalStateException("下载响应为空")
            val total = body.contentLength()
            val tmp = File(dir, "$name.part")
            tmp.outputStream().use { out ->
                val input = body.byteStream()
                val buf = ByteArray(64 * 1024)
                var read: Int
                var done = 0L
                while (input.read(buf).also { read = it } != -1) {
                    out.write(buf, 0, read)
                    done += read
                    onProgress?.invoke(done, total)
                }
            }
            if (tmp.length() == 0L) {
                tmp.delete()
                throw IllegalStateException("下载内容为空")
            }
            tmp.renameTo(target)
            return DownloadResult(target, false)
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

    /**
     * 检查 → 下载 → 调起安装器（后台线程）。
     * 供 Web 端 bridge 调用：WebView 里点「安装更新」走这里。
     */
    fun installLatest(activity: android.app.Activity) {
        Thread {
            try {
                val info = check(activity)
                if (!info.hasUpdate || info.apkUrl == null) {
                    activity.runOnUiThread {
                        android.widget.Toast.makeText(activity, "已是最新版本", android.widget.Toast.LENGTH_SHORT).show()
                    }
                    return@Thread
                }
                activity.runOnUiThread {
                    android.widget.Toast.makeText(activity, "正在下载 v${info.latest}…", android.widget.Toast.LENGTH_SHORT).show()
                }
                val result = download(activity, info)
                activity.runOnUiThread { installApk(activity, result.file) }
            } catch (e: Exception) {
                activity.runOnUiThread {
                    android.widget.Toast.makeText(activity, "更新失败：${e.message}", android.widget.Toast.LENGTH_LONG).show()
                }
            }
        }.start()
    }

    /** 调起系统安装器安装已下载的 APK。 */
    fun installApk(context: android.content.Context, file: java.io.File) {
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
