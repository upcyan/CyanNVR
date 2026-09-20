package com.cyannvr.app.ui

import android.Manifest
import android.content.pm.PackageManager
import android.os.Handler
import android.os.Looper
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.camera.core.CameraSelector
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.ImageProxy
import androidx.camera.core.Preview
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.content.ContextCompat
import com.google.zxing.BarcodeFormat
import com.google.zxing.BinaryBitmap
import com.google.zxing.DecodeHintType
import com.google.zxing.MultiFormatReader
import com.google.zxing.PlanarYUVLuminanceSource
import com.google.zxing.common.HybridBinarizer
import java.util.concurrent.Executors

/**
 * 扫码添加：对准 Web 端【服务器设置】的配对二维码，
 * 识别后回调原始内容（cyannvr://... 或 http://...），由调用方解析预填。
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun QrScanSheet(onResult: (String) -> Unit, onDismiss: () -> Unit) {
    val context = LocalContext.current
    val lifecycleOwner = LocalLifecycleOwner.current

    var granted by remember {
        mutableStateOf(
            ContextCompat.checkSelfPermission(context, Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED,
        )
    }
    var denied by remember { mutableStateOf(false) }
    val permissionLauncher = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { ok ->
        granted = ok
        denied = !ok
    }
    LaunchedEffect(Unit) {
        if (!granted) permissionLauncher.launch(Manifest.permission.CAMERA)
    }

    val delivered = remember { arrayOf(false) }
    val executor = remember { Executors.newSingleThreadExecutor() }
    val mainHandler = remember { Handler(Looper.getMainLooper()) }

    DisposableEffect(Unit) {
        onDispose {
            executor.shutdown()
            mainHandler.removeCallbacksAndMessages(null)
        }
    }

    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.padding(horizontal = 20.dp).padding(bottom = 24.dp)) {
            Text("扫码添加服务器", style = MaterialTheme.typography.titleLarge)
            Text(
                "对准网页端【服务器设置】页面的配对二维码",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Box(
                Modifier
                    .padding(top = 12.dp)
                    .fillMaxWidth()
                    .height(380.dp)
                    .clip(RoundedCornerShape(16.dp)),
                contentAlignment = Alignment.Center,
            ) {
                when {
                    granted -> AndroidView(
                        modifier = Modifier.fillMaxWidth().height(380.dp),
                        factory = { ctx ->
                            val previewView = PreviewView(ctx)
                            val providerFuture = ProcessCameraProvider.getInstance(ctx)
                            providerFuture.addListener({
                                if (delivered[0]) return@addListener
                                try {
                                    val provider = providerFuture.get()
                                    val preview = Preview.Builder().build().also {
                                        it.setSurfaceProvider(previewView.surfaceProvider)
                                    }
                                    val analysis = ImageAnalysis.Builder()
                                        .setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST)
                                        .build()
                                        .also {
                                            it.setAnalyzer(executor, QrAnalyzer { raw ->
                                                if (delivered[0]) return@QrAnalyzer
                                                delivered[0] = true
                                                runCatching { provider.unbindAll() }
                                                mainHandler.post { onResult(raw) }
                                            })
                                        }
                                    provider.unbindAll()
                                    provider.bindToLifecycle(
                                        lifecycleOwner, CameraSelector.DEFAULT_BACK_CAMERA, preview, analysis,
                                    )
                                } catch (_: Exception) {
                                }
                            }, ContextCompat.getMainExecutor(ctx))
                            previewView
                        },
                    )
                    denied -> Column(
                        horizontalAlignment = Alignment.CenterHorizontally,
                        verticalArrangement = Arrangement.spacedBy(12.dp),
                    ) {
                        Text("需要相机权限才能扫码", style = MaterialTheme.typography.bodyMedium)
                        Button(onClick = { permissionLauncher.launch(Manifest.permission.CAMERA) }) { Text("授予相机权限") }
                    }
                    else -> CircularProgressIndicator(Modifier.size(32.dp))
                }
            }
            TextButton(onClick = onDismiss, modifier = Modifier.align(Alignment.CenterHorizontally)) {
                Text("取消")
            }
        }
    }
}

private class QrAnalyzer(private val onHit: (String) -> Unit) : ImageAnalysis.Analyzer {

    private val reader = MultiFormatReader().apply {
        setHints(
            mapOf(
                DecodeHintType.POSSIBLE_FORMATS to listOf(BarcodeFormat.QR_CODE),
                DecodeHintType.TRY_HARDER to true,
            ),
        )
    }

    override fun analyze(image: ImageProxy) {
        try {
            val source = buildSource(image) ?: return
            // 传感器方向不定，依次尝试 0°/90°/180°/270° 四个朝向
            var current: com.google.zxing.LuminanceSource? = source
            repeat(4) {
                val s = current ?: return
                val text = try {
                    reader.decodeWithState(BinaryBitmap(HybridBinarizer(s))).text
                } catch (_: Exception) {
                    null
                } finally {
                    reader.reset()
                }
                if (!text.isNullOrBlank()) {
                    onHit(text)
                    return
                }
                current = s.rotateCounterClockwise()
            }
        } catch (_: Exception) {
        } finally {
            image.close()
        }
    }

    private fun buildSource(image: ImageProxy): PlanarYUVLuminanceSource? {
        return try {
            val plane = image.planes[0]
            val buffer = plane.buffer
            val w = image.width
            val h = image.height
            val rowStride = plane.rowStride
            val pixelStride = plane.pixelStride
            val data = ByteArray(w * h)
            var out = 0
            for (row in 0 until h) {
                val rowStart = row * rowStride
                if (pixelStride == 1) {
                    buffer.position(rowStart)
                    buffer.get(data, out, w)
                } else {
                    for (col in 0 until w) {
                        data[out + col] = buffer.get(rowStart + col * pixelStride)
                    }
                }
                out += w
            }
            PlanarYUVLuminanceSource(data, w, h, 0, 0, w, h, false)
        } catch (_: Exception) {
            null
        }
    }
}
