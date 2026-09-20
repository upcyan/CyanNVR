package com.cyannvr.app

import android.content.Intent
import android.os.Bundle
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.OnBackPressedCallback
import androidx.activity.addCallback
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import com.cyannvr.app.data.ServerStore
import com.cyannvr.app.model.ConnState
import com.cyannvr.app.model.NvrServer
import com.cyannvr.app.net.ConnectionManager
import com.cyannvr.app.net.NvrClient
import com.cyannvr.app.ui.AddServerSheet
import com.cyannvr.app.ui.ConnectingOverlay
import com.cyannvr.app.ui.CyanNvrTheme
import com.cyannvr.app.ui.HelpDialog
import com.cyannvr.app.ui.LoginSheet
import com.cyannvr.app.ui.QrScanSheet
import com.cyannvr.app.ui.ScanSheet
import com.cyannvr.app.ui.ServerListScreen
import com.cyannvr.app.web.WebViewHolder
import com.cyannvr.app.web.NvrWebViewScreen

class MainActivity : ComponentActivity() {

    /** 待处理的配对内容（cyannvr:// 深链），进入组合后被消费置空 */
    private var pendingPairing by mutableStateOf<String?>(null)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        ServerStore.init(applicationContext)
        ConnectionManager.startNetworkWatch(applicationContext)
        pendingPairing = extractPairing(intent)
        enableEdgeToEdge()
        setContent {
            CyanNvrTheme {
                Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.surface) {
                    AppRoot(
                        pendingPairing = pendingPairing,
                        onPairingConsumed = { pendingPairing = null },
                    )
                }
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        pendingPairing = extractPairing(intent)
    }

    private fun extractPairing(intent: Intent?): String? {
        val data = intent?.data ?: return null
        return if (data.scheme == "cyannvr") data.toString() else null
    }
}

@Composable
fun AppRoot(pendingPairing: String?, onPairingConsumed: () -> Unit) {
    val state = ConnectionManager.state
    val activity = LocalContext.current as? ComponentActivity
    val context = LocalContext.current

    var listVersion by remember { mutableIntStateOf(0) }
    var addOpen by remember { mutableStateOf(false) }
    var editing by remember { mutableStateOf<NvrServer?>(null) }
    var addPrefill by remember { mutableStateOf<Pair<String, String>?>(null) } // url to name
    var scanOpen by remember { mutableStateOf(false) }
    var qrOpen by remember { mutableStateOf(false) }
    var loginTarget by remember { mutableStateOf<Triple<NvrServer, String?, String>?>(null) } // server/base/reason
    var helpOpen by remember { mutableStateOf(false) }

    // 扫码 / 深链配对：解析出地址后打开预填的添加表单
    LaunchedEffect(pendingPairing) {
        val raw = pendingPairing ?: return@LaunchedEffect
        onPairingConsumed()
        val base = NvrClient.parsePairingPayload(raw)
        if (base == null) {
            Toast.makeText(context, "二维码内容无法识别：$raw", Toast.LENGTH_LONG).show()
        } else {
            editing = null
            addPrefill = base to "CyanNVR"
            addOpen = true
        }
    }

    // 连接流程发现需要登录（无凭证/令牌过期）→ 弹出原生登录
    LaunchedEffect(state) {
        val s = state
        if (s is ConnState.NeedsLogin && loginTarget == null) {
            loginTarget = Triple(s.server, s.baseUrl, s.reason)
        }
    }

    // 冷启动自动重连上次使用的服务器
    var autoTried by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) {
        if (autoTried) return@LaunchedEffect
        autoTried = true
        if (ConnectionManager.state is ConnState.Idle) {
            ServerStore.get(ServerStore.lastServerId() ?: "")?.let { ConnectionManager.connect(it) }
        }
    }

    val failed = state as? ConnState.Failed

    when (state) {
        is ConnState.Connected -> {
            NvrWebViewScreen(
                onSwitchServer = { ConnectionManager.disconnect() },
                onLoginRedirect = { reason ->
                    val c = ConnectionManager.connected()
                    if (c != null) {
                        loginTarget = Triple(c.server, c.baseUrl, reason)
                    }
                },
            )
        }
        is ConnState.Connecting -> {
            ServerListScreen(
                version = listVersion,
                failedMessage = null,
                onConnect = { ConnectionManager.connect(it) },
                onDismissFailed = {},
                onRetryFailed = {},
                onAddServer = { editing = null; addPrefill = null; addOpen = true },
                onEditServer = { editing = it; addPrefill = null; addOpen = true },
                onScan = { scanOpen = true },
                onQrScan = { qrOpen = true },
                onHelp = { helpOpen = true },
                onDataChanged = { listVersion++ },
            )
            ConnectingOverlay(step = state.step, onCancel = { ConnectionManager.cancelConnect() })
        }
        else -> {
            ServerListScreen(
                version = listVersion,
                failedMessage = failed?.message,
                onConnect = { ConnectionManager.connect(it) },
                onDismissFailed = { ConnectionManager.resetToIdle() },
                onRetryFailed = { ConnectionManager.retryCurrent() },
                onAddServer = { editing = null; addPrefill = null; addOpen = true },
                onEditServer = { editing = it; addPrefill = null; addOpen = true },
                onScan = { scanOpen = true },
                onQrScan = { qrOpen = true },
                onHelp = { helpOpen = true },
                onDataChanged = { listVersion++ },
            )
        }
    }

    if (addOpen) {
        AddServerSheet(
            editing = editing,
            prefillUrl = addPrefill?.first,
            onDismiss = { addOpen = false; editing = null; addPrefill = null },
            onSaved = {
                addOpen = false
                editing = null
                addPrefill = null
                listVersion++
            },
        )
    }

    if (scanOpen) {
        ScanSheet(
            onPick = { url, name ->
                scanOpen = false
                editing = null
                addPrefill = url to name
                addOpen = true
            },
            onDismiss = { scanOpen = false },
        )
    }

    if (qrOpen) {
        QrScanSheet(
            onResult = { raw ->
                qrOpen = false
                val base = NvrClient.parsePairingPayload(raw)
                if (base == null) {
                    Toast.makeText(context, "二维码内容无法识别", Toast.LENGTH_LONG).show()
                } else {
                    editing = null
                    addPrefill = base to "CyanNVR"
                    addOpen = true
                }
            },
            onDismiss = { qrOpen = false },
        )
    }

    loginTarget?.let { (server, base, reason) ->
        LoginSheet(
            server = server,
            baseUrl = base,
            reason = reason,
            onDismiss = { loginTarget = null },
        )
    }

    if (helpOpen) {
        HelpDialog(onDismiss = { helpOpen = false })
    }

    // 返回键：全屏视频 → 网页后退 → 退到桌面 → 关闭弹层 → 退出
    DisposableEffect(activity) {
        if (activity == null) return@DisposableEffect onDispose { }
                val cb = object : OnBackPressedCallback(true) {
                override fun handleOnBackPressed() {
                when {
                    WebViewHolder.customView != null -> {
                        WebViewHolder.customView = null
                        WebViewHolder.customViewCallback?.onCustomViewHidden()
                        WebViewHolder.customViewCallback = null
                    }
                    ConnectionManager.state is ConnState.Connected && WebViewHolder.webView?.canGoBack() == true -> {
                        WebViewHolder.webView?.goBack()
                    }
                    ConnectionManager.state is ConnState.Connected -> activity.moveTaskToBack(true)
                    loginTarget != null -> loginTarget = null
                    qrOpen -> qrOpen = false
                    scanOpen -> scanOpen = false
                    addOpen -> { addOpen = false; editing = null; addPrefill = null }
                    helpOpen -> helpOpen = false
                    else -> activity.finish()
                }
            }
        }
        activity.onBackPressedDispatcher.addCallback(activity, cb)
        onDispose { cb.remove() }
    }
}
