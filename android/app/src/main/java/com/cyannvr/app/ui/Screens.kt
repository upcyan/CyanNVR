package com.cyannvr.app.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExtendedFloatingActionButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import com.cyannvr.app.data.ServerStore
import com.cyannvr.app.model.NvrServer
import com.cyannvr.app.net.ConnectionManager
import com.cyannvr.app.net.LanScanner
import com.cyannvr.app.net.NsdFinder
import com.cyannvr.app.net.NvrClient
import com.cyannvr.app.net.NvrException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.util.UUID

// ---------------------------------------------------------------------------
// 服务器列表（首页）
// ---------------------------------------------------------------------------

enum class ServerStatus { CHECKING, ONLINE, OFFLINE }

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ServerListScreen(
    version: Int,
    failedMessage: String?,
    onConnect: (NvrServer) -> Unit,
    onDismissFailed: () -> Unit,
    onRetryFailed: () -> Unit,
    onAddServer: (prefillUrl: String?) -> Unit,
    onEditServer: (NvrServer) -> Unit,
    onScan: () -> Unit,
    onQrScan: () -> Unit,
    onHelp: () -> Unit,
    onDataChanged: () -> Unit,
) {
    val servers = remember(version) { ServerStore.list() }
    var statusMap by remember { mutableStateOf<Map<String, ServerStatus>>(emptyMap()) }
    var menuFor by remember { mutableStateOf<NvrServer?>(null) }
    var toDelete by remember { mutableStateOf<NvrServer?>(null) }
    var refreshTick by remember { mutableStateOf(0) }
    val scope = rememberCoroutineScope()

    // 逐个探测服务器在线状态（绿点/灰点）
    LaunchedEffect(servers, refreshTick) {
        statusMap = servers.associate { it.id to ServerStatus.CHECKING }
        servers.forEach { server ->
            launch(Dispatchers.IO) {
                val ok = NvrClient.probe(NvrClient.probeClient, server.lanUrl) != null
                statusMap = statusMap + (server.id to if (ok) ServerStatus.ONLINE else ServerStatus.OFFLINE)
            }
        }
    }

    Box(Modifier.fillMaxSize().navigationBarsPadding()) {
    Column(Modifier.fillMaxSize()) {
        TopAppBar(
            title = { Text("CyanNVR") },
            actions = {
                IconButton(onClick = { refreshTick++ }) {
                    Icon(Icons.Filled.Refresh, contentDescription = "刷新状态")
                }
                IconButton(onClick = onHelp) {
                    Icon(Icons.Filled.Info, contentDescription = "连接说明")
                }
            },
        )

        if (servers.isEmpty()) {
            EmptyGuide(
                onScan = onScan,
                onAdd = { onAddServer(null) },
                onQrScan = onQrScan,
            )
        } else {
            LazyColumn(
                modifier = Modifier.fillMaxSize(),
                contentPadding = androidx.compose.foundation.layout.PaddingValues(16.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                item {
                    Text(
                        "选择要连接的服务器",
                        style = MaterialTheme.typography.labelMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                items(servers, key = { it.id }) { server ->
                    val status = statusMap[server.id] ?: ServerStatus.CHECKING
                    Card(
                        modifier = Modifier.fillMaxWidth().clickable { onConnect(server) },
                    ) {
                        Row(
                            modifier = Modifier.padding(16.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Box(
                                Modifier.size(12.dp).background(
                                    when (status) {
                                        ServerStatus.ONLINE -> Color(0xFF22C55E)
                                        ServerStatus.CHECKING -> Color(0xFF94A3B8)
                                        ServerStatus.OFFLINE -> Color(0xFFEF4444)
                                    },
                                    CircleShape,
                                ),
                            )
                            Spacer(Modifier.width(12.dp))
                            Column(Modifier.weight(1f)) {
                                Text(server.name, style = MaterialTheme.typography.titleMedium)
                                Text(
                                    server.lanUrl.removePrefix("http://"),
                                    style = MaterialTheme.typography.bodySmall,
                                    fontFamily = FontFamily.Monospace,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                                server.pubUrl?.let {
                                    Text(
                                        "公网 ${it.removePrefix("http://").removePrefix("https://")}",
                                        style = MaterialTheme.typography.bodySmall,
                                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                                    )
                                }
                            }
                            when (status) {
                                ServerStatus.CHECKING -> CircularProgressIndicator(
                                    modifier = Modifier.size(16.dp), strokeWidth = 2.dp,
                                )
                                ServerStatus.ONLINE -> Text(
                                    "在线", style = MaterialTheme.typography.labelSmall,
                                    color = Color(0xFF16A34A),
                                )
                                ServerStatus.OFFLINE -> Text(
                                    "离线", style = MaterialTheme.typography.labelSmall,
                                    color = Color(0xFFDC2626),
                                )
                            }
                            IconButton(onClick = { menuFor = server }) {
                                Icon(Icons.Filled.MoreVert, contentDescription = "更多")
                            }
                        }
                    }
                }
                item {
                    OutlinedButton(
                        onClick = onScan,
                        modifier = Modifier.fillMaxWidth(),
                    ) {
                        Icon(Icons.Filled.Search, contentDescription = null)
                        Spacer(Modifier.width(8.dp))
                        Text("扫描局域网查找服务器")
                    }
                    TextButton(onClick = onQrScan, modifier = Modifier.fillMaxWidth()) {
                        Text("扫二维码添加")
                    }
                }
            }
        }

    }

        ExtendedFloatingActionButton(
            onClick = { onAddServer(null) },
            modifier = Modifier.align(Alignment.BottomEnd).padding(20.dp),
            icon = { Icon(Icons.Filled.Add, contentDescription = null) },
            text = { Text("添加服务器") },
        )
    }

    DropdownMenu(expanded = menuFor != null, onDismissRequest = { menuFor = null }) {
        DropdownMenuItem(
            text = { Text("编辑") },
            leadingIcon = { Icon(Icons.Filled.Edit, null) },
            onClick = { menuFor?.let(onEditServer); menuFor = null },
        )
        DropdownMenuItem(
            text = { Text("删除") },
            leadingIcon = { Icon(Icons.Filled.Delete, null) },
            onClick = { toDelete = menuFor; menuFor = null },
        )
    }

    toDelete?.let { server ->
        AlertDialog(
            onDismissRequest = { toDelete = null },
            title = { Text("删除服务器") },
            text = { Text("确定删除「${server.name}」？已保存的密码与登录状态将一并清除。") },
            confirmButton = {
                TextButton(onClick = {
                    ServerStore.delete(server.id)
                    toDelete = null
                    onDataChanged()
                }) { Text("删除", color = MaterialTheme.colorScheme.error) }
            },
            dismissButton = { TextButton(onClick = { toDelete = null }) { Text("取消") } },
        )
    }

    failedMessage?.let { msg ->
        AlertDialog(
            onDismissRequest = onDismissFailed,
            title = { Text("连接失败") },
            text = { Text(msg) },
            confirmButton = { TextButton(onClick = onRetryFailed) { Text("重试") } },
            dismissButton = { TextButton(onClick = onDismissFailed) { Text("返回") } },
        )
    }
}

@Composable
private fun EmptyGuide(onScan: () -> Unit, onAdd: () -> Unit, onQrScan: () -> Unit) {
    Column(
        modifier = Modifier.fillMaxSize().padding(32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Icon(
            Icons.Filled.Search,
            contentDescription = null,
            modifier = Modifier.size(72.dp),
            tint = MaterialTheme.colorScheme.primary,
        )
        Spacer(Modifier.height(16.dp))
        Text("还没有服务器", style = MaterialTheme.typography.titleLarge)
        Spacer(Modifier.height(8.dp))
        Text(
            "手机与 NVR 在同一局域网时，可以自动扫描发现；\n也可以手动输入服务器地址。",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = TextAlign.Center,
        )
        Spacer(Modifier.height(28.dp))
        Button(onClick = onScan, modifier = Modifier.fillMaxWidth()) {
            Icon(Icons.Filled.Search, contentDescription = null)
            Spacer(Modifier.width(8.dp))
            Text("扫描局域网")
        }
        Spacer(Modifier.height(12.dp))
        OutlinedButton(onClick = onQrScan, modifier = Modifier.fillMaxWidth()) {
            Text("扫码添加服务器")
        }
        Spacer(Modifier.height(12.dp))
        OutlinedButton(onClick = onAdd, modifier = Modifier.fillMaxWidth()) {
            Icon(Icons.Filled.Add, contentDescription = null)
            Spacer(Modifier.width(8.dp))
            Text("手动添加服务器")
        }
    }
}

// ---------------------------------------------------------------------------
// 添加 / 编辑服务器
// ---------------------------------------------------------------------------

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AddServerSheet(
    editing: NvrServer?,
    prefillUrl: String?,
    onDismiss: () -> Unit,
    onSaved: () -> Unit,
) {
    val initialUrl = editing?.lanUrl ?: prefillUrl?.let { NvrClient.normalizeBaseUrl(it) } ?: ""
    var name by remember { mutableStateOf(editing?.name ?: "CyanNVR") }
    var lan by remember { mutableStateOf(initialUrl) }
    var pub by remember { mutableStateOf(editing?.pubUrl ?: "") }
    var username by remember { mutableStateOf(editing?.username ?: "admin") }
    var password by remember { mutableStateOf("") }
    var rememberPwd by remember { mutableStateOf(true) }
    var trustCert by remember { mutableStateOf(editing?.trustAnyCert ?: false) }
    var testState by remember { mutableStateOf<Pair<Boolean, String>?>(null) }
    var testing by remember { mutableStateOf(false) }
    var hasPasswordSaved by remember { mutableStateOf(editing != null && ServerStore.secretOf(editing.id).password != null) }

    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            modifier = Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 20.dp).padding(bottom = 32.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text(
                if (editing == null) "添加服务器" else "编辑服务器",
                style = MaterialTheme.typography.titleLarge,
            )
            OutlinedTextField(
                value = lan,
                onValueChange = { lan = it; testState = null },
                label = { Text("局域网地址 *") },
                placeholder = { Text("192.168.1.100 或 192.168.1.100:8080") },
                supportingText = {
                    val normalized = NvrClient.normalizeBaseUrl(lan)
                    Text(if (normalized != null) "将连接：$normalized/api" else "请输入正确的 IP 或域名")
                },
                singleLine = true,
                isError = lan.isNotBlank() && NvrClient.normalizeBaseUrl(lan) == null,
                modifier = Modifier.fillMaxWidth(),
            )
            OutlinedTextField(
                value = pub,
                onValueChange = { pub = it; testState = null },
                label = { Text("公网地址（可选）") },
                placeholder = { Text("外网访问用，如 nvr.example.com") },
                supportingText = { Text("现场不在局域网时自动改用此地址") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            OutlinedTextField(
                value = name,
                onValueChange = { name = it },
                label = { Text("名称") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            HorizontalDivider()
            OutlinedTextField(
                value = username,
                onValueChange = { username = it },
                label = { Text("用户名") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            OutlinedTextField(
                value = password,
                onValueChange = { password = it },
                label = { Text(if (hasPasswordSaved) "密码（已保存，留空不修改）" else "密码（可选）") },
                placeholder = { Text(if (hasPasswordSaved) "••••••••" else "留空则连接时询问") },
                visualTransformation = PasswordVisualTransformation(),
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            Row(verticalAlignment = Alignment.CenterVertically) {
                Checkbox(checked = rememberPwd, onCheckedChange = { rememberPwd = it })
                Text("记住密码，下次自动登录", style = MaterialTheme.typography.bodyMedium)
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                Switch(checked = trustCert, onCheckedChange = { trustCert = it })
                Spacer(Modifier.width(12.dp))
                Column {
                    Text("信任自签名证书", style = MaterialTheme.typography.bodyMedium)
                    Text(
                        "仅公网 HTTPS 自签名证书时开启",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }

            testState?.let { (ok, msg) ->
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Icon(
                        if (ok) Icons.Filled.Check else Icons.Filled.Close,
                        contentDescription = null,
                        tint = if (ok) Color(0xFF16A34A) else MaterialTheme.colorScheme.error,
                        modifier = Modifier.size(18.dp),
                    )
                    Spacer(Modifier.width(8.dp))
                    Text(msg, style = MaterialTheme.typography.bodySmall)
                }
            }

            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                OutlinedButton(
                    onClick = {
                        val base = NvrClient.normalizeBaseUrl(lan) ?: return@OutlinedButton
                        testing = true
                        testState = null
                        ConnectionManager.scope.launch {
                            val client = NvrClient.clientFor(trustCert)
                            val lanInfo = NvrClient.probe(client, base)
                            val msg = if (lanInfo != null) {
                                "连接成功：${lanInfo.name}（${lanInfo.latencyMs}ms）"
                            } else {
                                "无法连接 $base：请确认服务端已启动、IP 端口正确、手机在同一网络"
                            }
                            val pubBase = NvrClient.normalizeBaseUrl(pub)
                            val pubMsg = when {
                                pubBase == null -> ""
                                NvrClient.probe(client, pubBase) != null -> "；公网地址可达"
                                else -> "；公网地址不可达"
                            }
                            withContext(Dispatchers.Main) {
                                testing = false
                                testState = (lanInfo != null) to (msg + pubMsg)
                            }
                        }
                    },
                    enabled = NvrClient.normalizeBaseUrl(lan) != null && !testing,
                ) {
                    if (testing) {
                        CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp)
                        Spacer(Modifier.width(8.dp))
                    }
                    Text("测试连接")
                }
                Button(
                    onClick = {
                        val base = NvrClient.normalizeBaseUrl(lan) ?: return@Button
                        val server = (editing ?: NvrServer(
                            id = UUID.randomUUID().toString(),
                            name = name.ifBlank { "CyanNVR" },
                            lanUrl = base,
                        )).copy(
                            name = name.ifBlank { "CyanNVR" },
                            lanUrl = base,
                            pubUrl = NvrClient.normalizeBaseUrl(pub),
                            username = username.ifBlank { "admin" },
                            trustAnyCert = trustCert,
                        )
                        ServerStore.save(server)
                        if (password.isNotBlank()) {
                            ServerStore.savePassword(server.id, if (rememberPwd) password else null)
                        } else if (!hasPasswordSaved) {
                            ServerStore.savePassword(server.id, null)
                        }
                        onSaved()
                    },
                    enabled = NvrClient.normalizeBaseUrl(lan) != null,
                ) { Text("保存") }
            }
        }
    }
}

// ---------------------------------------------------------------------------
// 局域网扫描
// ---------------------------------------------------------------------------

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ScanSheet(
    onPick: (baseUrl: String, name: String) -> Unit,
    onDismiss: () -> Unit,
) {
    val context = androidx.compose.ui.platform.LocalContext.current
    var progress by remember { mutableStateOf(LanScanner.Progress(false, 0, 0, "", emptyList())) }
    var nsdHits by remember { mutableStateOf(emptyList<com.cyannvr.app.model.ScanHit>()) }
    var job by remember { mutableStateOf<Job?>(null) }
    var error by remember { mutableStateOf<String?>(null) }

    DisposableEffect(Unit) {
        job = LanScanner.scan(ConnectionManager.scope) { p -> progress = p }
        onDispose { job?.cancel() }
    }
    // 同时监听服务端 mDNS 广播（有广播的设备秒级发现，无广播的靠网段扫描兜底）
    DisposableEffect(Unit) {
        val controller = NsdFinder.discover(
            context,
            onFound = { hit -> nsdHits = (nsdHits + hit).distinctBy { it.baseUrl } },
            onError = { err -> error = err },
        )
        onDispose { controller?.stop() }
    }
    val hits = remember(nsdHits, progress.hits) {
        (nsdHits + progress.hits).distinctBy { it.baseUrl }
    }

    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.padding(horizontal = 20.dp).padding(bottom = 32.dp)) {
            Text("发现服务器", style = MaterialTheme.typography.titleLarge)
            Spacer(Modifier.height(4.dp))
            Text(
                if (progress.scanning) "正在扫描 ${progress.subnet}.x（${progress.done}/${progress.total}）+ mDNS 监听中…"
                else if (hits.isEmpty()) "扫描完成，未发现 CyanNVR 服务器"
                else "发现 ${hits.size} 台 CyanNVR 服务器",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.height(12.dp))
            if (progress.scanning) LinearProgressIndicator(
                progress = { if (progress.total == 0) 0f else progress.done.toFloat() / progress.total },
                modifier = Modifier.fillMaxWidth(),
            )
            error?.let {
                Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
            }
            Spacer(Modifier.height(8.dp))

            LazyColumn(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                items(hits.size) { i ->
                    val hit = hits[i]
                    Card(modifier = Modifier.fillMaxWidth()) {
                        Row(
                            modifier = Modifier.padding(14.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Column(Modifier.weight(1f)) {
                                Text(
                                    hit.baseUrl.removePrefix("http://"),
                                    fontFamily = FontFamily.Monospace,
                                    style = MaterialTheme.typography.titleSmall,
                                )
                                Text(
                                    "${hit.name} · ${hit.latencyMs}ms",
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                            }
                            Button(onClick = { onPick(hit.baseUrl, hit.name) }) { Text("添加") }
                        }
                    }
                }
            }

            if (!progress.scanning) {
                Spacer(Modifier.height(12.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    OutlinedButton(onClick = {
                        job = LanScanner.scan(ConnectionManager.scope) { progress = it }
                    }) {
                        Icon(Icons.Filled.Refresh, contentDescription = null)
                        Spacer(Modifier.width(8.dp))
                        Text("重新扫描")
                    }
                    TextButton(onClick = onDismiss) { Text("关闭") }
                }
                Spacer(Modifier.height(8.dp))
                Text(
                    "找不到？确认手机已连到 NVR 所在的 Wi-Fi，且服务端已启动（默认端口 8080）。也可以扫码或手动添加。",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

// ---------------------------------------------------------------------------
// 登录
// ---------------------------------------------------------------------------

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun LoginSheet(
    server: NvrServer,
    baseUrl: String?,
    reason: String,
    onDismiss: () -> Unit,
) {
    var username by remember { mutableStateOf(server.username) }
    var password by remember { mutableStateOf("") }
    var rememberPwd by remember { mutableStateOf(true) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }

    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            modifier = Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 20.dp).padding(bottom = 32.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text("登录 ${server.name}", style = MaterialTheme.typography.titleLarge)
            Text(
                reason + (baseUrl?.let { "\n$it" } ?: ""),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            OutlinedTextField(
                value = username,
                onValueChange = { username = it },
                label = { Text("用户名") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            OutlinedTextField(
                value = password,
                onValueChange = { password = it },
                label = { Text("密码") },
                visualTransformation = PasswordVisualTransformation(),
                singleLine = true,
                isError = error != null,
                supportingText = error?.let { { Text(it) } },
                modifier = Modifier.fillMaxWidth(),
            )
            Row(verticalAlignment = Alignment.CenterVertically) {
                Checkbox(checked = rememberPwd, onCheckedChange = { rememberPwd = it })
                Text("记住密码", style = MaterialTheme.typography.bodyMedium)
            }
            Button(
                onClick = {
                    busy = true
                    error = null
                    ConnectionManager.loginAndFinish(
                        server, baseUrl, username, password, rememberPwd,
                    ) { err ->
                        busy = false
                        if (err != null) error = err else onDismiss()
                    }
                },
                enabled = username.isNotBlank() && password.isNotBlank() && !busy,
                modifier = Modifier.fillMaxWidth(),
            ) {
                if (busy) {
                    CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp)
                    Spacer(Modifier.width(8.dp))
                }
                Text("登录")
            }
        }
    }
}

// ---------------------------------------------------------------------------
// 连接中（全屏过渡）
// ---------------------------------------------------------------------------

@Composable
fun ConnectingOverlay(step: String, onCancel: () -> Unit) {
    Box(
        Modifier.fillMaxSize().background(MaterialTheme.colorScheme.surface),
        contentAlignment = Alignment.Center,
    ) {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            CircularProgressIndicator()
            Spacer(Modifier.height(16.dp))
            Text(step, style = MaterialTheme.typography.bodyMedium)
            Spacer(Modifier.height(24.dp))
            TextButton(onClick = onCancel) { Text("取消") }
        }
    }
}

// ---------------------------------------------------------------------------
// 连接说明
// ---------------------------------------------------------------------------

@Composable
fun HelpDialog(onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        confirmButton = { TextButton(onClick = onDismiss) { Text("知道了") } },
        title = { Text("如何连接服务器") },
        text = {
            Text(
                "CyanNVR 服务端默认监听 8080 端口（HTTP）。\n\n" +
                    "① 自动发现：App 同时监听服务端的 mDNS 广播并扫描所在网段；也可在网页端【服务器设置】生成配对二维码，用「扫码添加」自动填入，或直接用系统相机扫码唤起本应用。\n\n" +
                    "② 局域网：手机连上与 NVR 相同的 Wi-Fi 后直连，速度最快。\n\n" +
                    "③ 公网：在路由器将外网端口映射到 NVR 的 8080，或使用 frp / Tailscale 等内网穿透，把地址填入「公网地址」。App 会在局域网不通时自动切换。\n\n" +
                    "④ 登录：账号密码与服务端网页版一致；令牌 24 小时有效，之后会提示重新登录。\n\n" +
                    "提示：mDNS 发现要求服务端与手机在同一二层网络，docker 部署时需加 --network=host。",
            )
        },
    )
}
