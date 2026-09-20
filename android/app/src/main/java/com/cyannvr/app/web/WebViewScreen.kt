package com.cyannvr.app.web

import android.app.Activity
import android.app.DownloadManager
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.net.Uri
import android.os.Environment
import android.view.View
import android.view.ViewGroup
import android.view.WindowManager
import android.webkit.CookieManager
import android.webkit.DownloadListener
import android.webkit.WebChromeClient
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.view.ViewCompat
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import com.cyannvr.app.net.ConnectionManager
import com.cyannvr.app.net.NvrClient
import org.json.JSONObject

/** WebView 单例句柄：供返回键/全屏逻辑在 Compose 之外访问 */
object WebViewHolder {
    var webView: WebView? = null
    var customView by mutableStateOf<View?>(null)
    var customViewCallback: WebChromeClient.CustomViewCallback? = null
    /** 系统手势条高度（CSS px），由 inset 监听更新、随页面加载注入 */
    var safeBottomPx by mutableIntStateOf(0)
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NvrWebViewScreen(
    onSwitchServer: () -> Unit,
    onLoginRedirect: (reason: String) -> Unit,
) {
    val context = LocalContext.current
    val activity = context as? Activity
    val conn = ConnectionManager.connected() ?: return
    val reloadTick = ConnectionManager.reloadTick
    val banner = ConnectionManager.banner
    val server = conn.server
    val base = conn.baseUrl

    var progress by remember { mutableIntStateOf(0) }
    var loadError by remember { mutableStateOf<Pair<String, String>?>(null) }
    var menuOpen by remember { mutableStateOf(false) }
    var infoOpen by remember { mutableStateOf(false) }

    fun exitFullscreen() {
        WebViewHolder.customView = null
        WebViewHolder.customViewCallback?.onCustomViewHidden()
        WebViewHolder.customViewCallback = null
        activity?.window?.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        activity?.let { a ->
            val decor = a.window.decorView
            WindowInsetsControllerCompat(a.window, decor).apply {
                show(WindowInsetsCompat.Type.systemBars())
            }
            WindowCompat.setDecorFitsSystemWindows(a.window, true)
        }
    }

    LaunchedEffect(base, reloadTick) {
        val url = "$base/#/live"
        loadError = null
        var tries = 0
        while (WebViewHolder.webView == null && tries < 100) {
            kotlinx.coroutines.delay(20)
            tries++
        }
        WebViewHolder.webView?.loadUrl(url)
    }

    Column(Modifier.fillMaxSize()) {
        TopAppBar(
            title = {
                Column {
                    Text(server.name, style = MaterialTheme.typography.titleMedium)
                    Text(
                        if (conn.viaLan) "局域网 · ${base.removePrefix("http://").removePrefix("https://")}"
                        else "公网 · ${base.removePrefix("http://").removePrefix("https://")}",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            },
            actions = {
                IconButton(onClick = { loadError = null; WebViewHolder.webView?.reload() }) {
                    Icon(Icons.Filled.Refresh, contentDescription = "刷新")
                }
                IconButton(onClick = { menuOpen = true }) {
                    Icon(Icons.Filled.MoreVert, contentDescription = "菜单")
                }
                DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                    DropdownMenuItem(
                        text = { Text("切换服务器") },
                        onClick = { menuOpen = false; onSwitchServer() },
                    )
                    DropdownMenuItem(
                        text = { Text("重新登录") },
                        onClick = {
                            menuOpen = false
                            ConnectionManager.onSessionExpired()
                        },
                    )
                    DropdownMenuItem(
                        text = { Text("在浏览器打开") },
                        onClick = {
                            menuOpen = false
                            runCatching {
                                context.startActivity(
                                    android.content.Intent(android.content.Intent.ACTION_VIEW, Uri.parse(base)),
                                )
                            }
                        },
                    )
                    DropdownMenuItem(
                        text = { Text("复制服务器地址") },
                        onClick = {
                            menuOpen = false
                            val cm = context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
                            cm.setPrimaryClip(ClipData.newPlainText("CyanNVR", base))
                            android.widget.Toast.makeText(context, "已复制 $base", android.widget.Toast.LENGTH_SHORT).show()
                        },
                    )
                    DropdownMenuItem(
                        text = { Text("连接说明") },
                        onClick = { menuOpen = false; infoOpen = true },
                    )
                }
            },
            colors = TopAppBarDefaults.topAppBarColors(
                containerColor = MaterialTheme.colorScheme.surface,
            ),
        )

        if (banner != null) {
            Surface(color = MaterialTheme.colorScheme.tertiaryContainer, modifier = Modifier.fillMaxWidth()) {
                Text(
                    banner,
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onTertiaryContainer,
                )
            }
        }

        // 黑色背景与 Web 深色主题一致；WebView 保持全屏延伸（沉浸式），
        // 手势条高度由 inset 监听注入 CSS 变量，Web 导航栏自行抬高
        Box(
            Modifier
                .weight(1f)
                .fillMaxWidth()
                .background(Color.Black)
        ) {
            if (progress in 1..99) {
                LinearProgressIndicator(
                    progress = { progress / 100f },
                    modifier = Modifier.fillMaxWidth().height(2.dp).align(Alignment.TopCenter),
                )
            }

            AndroidView(
                modifier = Modifier.fillMaxSize(),
                factory = { ctx ->
                    WebView(ctx).apply {
                        layoutParams = ViewGroup.LayoutParams(
                            ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT,
                        )
                        settings.javaScriptEnabled = true
                        settings.domStorageEnabled = true
                        settings.mediaPlaybackRequiresUserGesture = false
                        settings.cacheMode = WebSettings.LOAD_DEFAULT
                        settings.useWideViewPort = true
                        settings.loadWithOverviewMode = true
                        settings.mixedContentMode = WebSettings.MIXED_CONTENT_COMPATIBILITY_MODE
                        settings.textZoom = 100
                        settings.userAgentString = settings.userAgentString + " CyanNVRApp/1.0"
                        CookieManager.getInstance().setAcceptThirdPartyCookies(this, false)

                        // 把系统手势条高度注入页面 CSS 变量（WebView 不支持 env(safe-area-inset-*)）
                        ViewCompat.setOnApplyWindowInsetsListener(this) { v, insets ->
                            val bar = insets.getInsets(WindowInsetsCompat.Type.navigationBars()).bottom
                            val cssPx = (bar / v.resources.displayMetrics.density).toInt()
                            if (cssPx != WebViewHolder.safeBottomPx) {
                                WebViewHolder.safeBottomPx = cssPx
                                WebViewHolder.webView?.evaluateJavascript(injectSafeBottomScript(cssPx), null)
                            }
                            WindowInsetsCompat.CONSUMED
                        }

                        webViewClient = object : WebViewClient() {
                            override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                                val url = request.url
                                val baseHost = try { Uri.parse(base).host } catch (_: Exception) { null }
                                if (url.host == baseHost) return false
                                // 外部链接交给系统浏览器
                                if (url.scheme == "http" || url.scheme == "https") {
                                    runCatching {
                                        ctx.startActivity(android.content.Intent(android.content.Intent.ACTION_VIEW, url))
                                    }
                                    return true
                                }
                                return false
                            }

                            override fun onPageStarted(view: WebView, url: String, favicon: android.graphics.Bitmap?) {
                                super.onPageStarted(view, url, favicon)
                                injectSession(view, base)
                            }

                            override fun onPageFinished(view: WebView, url: String) {
                                super.onPageFinished(view, url)
                                if (!url.endsWith("#/login")) injectSession(view, base)
                            }

                            override fun doUpdateVisitedHistory(view: WebView, url: String, isReload: Boolean) {
                                super.doUpdateVisitedHistory(view, url, isReload)
                                if (url.endsWith("#/login")) {
                                    onLoginRedirect("登录已过期，请重新登录")
                                }
                            }

                            override fun onReceivedError(view: WebView, request: WebResourceRequest, error: WebResourceError) {
                                if (request.isForMainFrame) {
                                    loadError = request.url.toString() to (error.description?.toString() ?: "加载失败")
                                }
                            }
                        }

                        webChromeClient = object : WebChromeClient() {
                            override fun onProgressChanged(view: WebView, newProgress: Int) {
                                progress = newProgress
                            }

                            override fun onShowCustomView(view: View, callback: CustomViewCallback) {
                                if (WebViewHolder.customView != null) { callback.onCustomViewHidden(); return }
                                WebViewHolder.customView = view
                                WebViewHolder.customViewCallback = callback
                                activity?.window?.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
                                activity?.let { a ->
                                    WindowCompat.setDecorFitsSystemWindows(a.window, false)
                                    WindowInsetsControllerCompat(a.window, a.window.decorView).apply {
                                        hide(WindowInsetsCompat.Type.systemBars())
                                    }
                                }
                            }

                            override fun onHideCustomView() {
                                WebViewHolder.customView = null
                                WebViewHolder.customViewCallback = null
                                activity?.window?.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
                                activity?.let { a ->
                                    WindowCompat.setDecorFitsSystemWindows(a.window, true)
                                    WindowInsetsControllerCompat(a.window, a.window.decorView).apply {
                                        show(WindowInsetsCompat.Type.systemBars())
                                    }
                                }
                            }
                        }

                        setDownloadListener(DownloadListener { url, _, contentDisposition, mimetype, _ ->
                            try {
                                val fileName = fileNameFrom(contentDisposition, url)
                                val req = DownloadManager.Request(Uri.parse(url))
                                    .setTitle(fileName)
                                    .setDescription("CyanNVR 录像下载")
                                    .setNotificationVisibility(DownloadManager.Request.VISIBILITY_VISIBLE_NOTIFY_COMPLETED)
                                    .setDestinationInExternalPublicDir(Environment.DIRECTORY_DOWNLOADS, fileName)
                                mimetype?.takeIf { it.isNotBlank() }?.let { req.setMimeType(it) }
                                ctx.getSystemService(DownloadManager::class.java).enqueue(req)
                                android.widget.Toast.makeText(ctx, "开始下载：$fileName", android.widget.Toast.LENGTH_SHORT).show()
                            } catch (e: Exception) {
                                android.widget.Toast.makeText(ctx, "下载失败：${e.message}", android.widget.Toast.LENGTH_LONG).show()
                            }
                        })
                        WebViewHolder.webView = this
                    }
                },
            )

            loadError?.let { (url, desc) ->
                ErrorOverlay(
                    url = url,
                    desc = desc,
                    onRetry = { loadError = null; WebViewHolder.webView?.reload() },
                    onSwitch = onSwitchServer,
                )
            }
        }
    }

    // 全屏视频覆盖层
    WebViewHolder.customView?.let { cv ->
        Box(
            Modifier.fillMaxSize().background(Color.Black),
        ) {
            AndroidView(factory = { cv }, modifier = Modifier.fillMaxSize())
            DisposableEffect(Unit) { onDispose { exitFullscreen() } }
        }
    }

    if (infoOpen) {
        AlertDialog(
            onDismissRequest = { infoOpen = false },
            confirmButton = { TextButton(onClick = { infoOpen = false }) { Text("知道了") } },
            title = { Text("连接说明") },
            text = {
                Text(
                    "当前连接：$base\n\n" +
                        "· 局域网：手机与 NVR 同一网络时直连，速度最快。\n" +
                        "· 公网：需在路由器做端口映射，或使用内网穿透/frps。\n" +
                        "· 网络切换时本应用会自动在局域网与公网地址间切换。\n" +
                        "· 视频/下载使用令牌鉴权，会话 24 小时有效，过期会自动引导重新登录。",
                )
            },
        )
    }

    DisposableEffect(Unit) {
        onDispose {
            WebViewHolder.webView?.let { wv ->
                wv.loadUrl("about:blank")
                wv.removeAllViews()
                wv.destroy()
            }
            WebViewHolder.webView = null
            WebViewHolder.customView = null
            WebViewHolder.customViewCallback = null
        }
    }
}

private fun injectSession(view: WebView, base: String) {
    val c = ConnectionManager.connected() ?: return
    val script = buildString {
        append("(function(){try{")
        append("localStorage.setItem('nvr_token',").append(JSONObject.quote(c.login.token)).append(");")
        append("localStorage.setItem('nvr_user',").append(JSONObject.quote(c.login.userJson)).append(");")
        append("localStorage.setItem('nvr_server_lan',").append(JSONObject.quote(c.server.lanUrl)).append(");")
        append("localStorage.setItem('nvr_server_pub',").append(JSONObject.quote(c.server.pubUrl ?: "")).append(");")
        append("localStorage.setItem('nvr_server_mode','auto');")
        append("localStorage.setItem('nvr_server_cur',").append(JSONObject.quote(base)).append(");")
        append("localStorage.setItem('nvr_demo_mode','0');")
        append("document.documentElement.style.setProperty('--nvr-safe-bottom','")
        append(WebViewHolder.safeBottomPx)
        append("px');")
        append("}catch(e){}})();")
    }
    view.evaluateJavascript(script, null)
}

private fun injectSafeBottomScript(cssPx: Int): String =
    "(function(){document.documentElement.style.setProperty('--nvr-safe-bottom','${cssPx}px')})();"

private fun fileNameFrom(contentDisposition: String?, url: String): String {
    contentDisposition?.let { cd ->
        Regex("filename\\*?=\"?([^\";]+)\"?").find(cd)?.groupValues?.get(1)?.let { return it }
    }
    return url.substringBefore('?').substringAfterLast('/').ifEmpty { "recording.mp4" }
}

@Composable
private fun ErrorOverlay(url: String, desc: String, onRetry: () -> Unit, onSwitch: () -> Unit) {
    Box(
        Modifier.fillMaxSize().background(MaterialTheme.colorScheme.surface),
        contentAlignment = Alignment.Center,
    ) {
        Column(
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(12.dp),
            modifier = Modifier.padding(32.dp),
        ) {
            Icon(
                Icons.Filled.Info,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.error,
                modifier = Modifier.height(48.dp),
            )
            Text("页面加载失败", style = MaterialTheme.typography.titleLarge)
            Text(
                desc,
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                textAlign = TextAlign.Center,
            )
            Text(
                NvrClient.trimBase(url),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.outline,
            )
            androidx.compose.material3.Button(onClick = onRetry) { Text("重试") }
            TextButton(onClick = onSwitch) { Text("切换服务器") }
        }
    }
}
