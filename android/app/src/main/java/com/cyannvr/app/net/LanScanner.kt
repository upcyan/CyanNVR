package com.cyannvr.app.net

import com.cyannvr.app.model.ScanHit
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Semaphore
import kotlinx.coroutines.sync.withPermit
import kotlinx.coroutines.withContext
import java.net.Inet4Address
import java.net.InetSocketAddress
import java.net.NetworkInterface
import java.net.Socket
import java.util.concurrent.atomic.AtomicInteger

/**
 * 局域网扫描：对本机所在网段的 1-254 主机做 TCP 端口探测，
 * 命中后用 /api/health 校验 CyanNVR 指纹。无需任何服务端配合。
 */
object LanScanner {

    /** 第一轮优先扫 CyanNVR 默认端口；无结果时第二轮扩展常见端口 */
    private const val PRIMARY_PORT = 8080
    private val FALLBACK_PORTS = intArrayOf(80, 8081, 8000, 9080)

    private const val CONNECT_TIMEOUT_MS = 400
    private const val CONCURRENCY = 64

    data class Progress(
        val scanning: Boolean,
        val done: Int,
        val total: Int,
        val subnet: String,
        val hits: List<ScanHit>,
    )

    fun localSubnets(): List<String> {
        val out = LinkedHashSet<String>()
        try {
            val nis = NetworkInterface.getNetworkInterfaces()
            while (nis.hasMoreElements()) {
                val ni = nis.nextElement()
                if (!ni.isUp) continue
                val addrs = ni.inetAddresses
                while (addrs.hasMoreElements()) {
                    val a = addrs.nextElement()
                    if (a is Inet4Address && !a.isLoopbackAddress && a.isSiteLocalAddress) {
                        val o = a.address
                        out.add("${o[0].toInt() and 0xFF}.${o[1].toInt() and 0xFF}.${o[2].toInt() and 0xFF}")
                    }
                }
            }
        } catch (_: Exception) {
        }
        return out.toList().take(3)
    }

    /**
     * 开始扫描。回调均在主线程之外触发，调用方自行切线程。
     * 返回可取消的 Job。
     */
    fun scan(
        scope: kotlinx.coroutines.CoroutineScope,
        onProgress: (Progress) -> Unit,
    ): Job = scope.launch {
        val subnets = localSubnets()
        if (subnets.isEmpty()) {
            onProgress(Progress(false, 0, 0, "", emptyList()))
            return@launch
        }

        withContext(Dispatchers.IO) {
            for ((idx, subnet) in subnets.withIndex()) {
                val ports = if (idx == 0) intArrayOf(PRIMARY_PORT) else intArrayOf(PRIMARY_PORT)
                val hits = scanSubnet(subnet, ports.toList(), onProgress).toMutableList()

                // 仅在所有网段第一轮都无结果时，才用扩展端口重扫首个网段
                if (idx == subnets.lastIndex && hits.isEmpty() && subnets.size == 1) {
                    hits += scanSubnet(subnet, FALLBACK_PORTS.toList(), onProgress)
                }
                if (hits.isNotEmpty()) {
                    onProgress(Progress(false, 1, 1, subnet, hits))
                    return@withContext
                }
            }
            onProgress(Progress(false, 1, 1, subnets.first(), emptyList()))
        }
    }

    private suspend fun scanSubnet(
        subnet: String,
        ports: List<Int>,
        onProgress: (Progress) -> Unit,
    ): List<ScanHit> {
        val done = AtomicInteger(0)
        val hits = mutableListOf<ScanHit>()
        val sem = Semaphore(CONCURRENCY)
        val total = 254 * ports.size
        onProgress(Progress(true, 0, total, subnet, emptyList()))

        coroutineScope {
            val jobs = (1..254).map { last ->
                async {
                    val ip = "$subnet.$last"
                    for (port in ports) {
                        sem.withPermit {
                            if (portOpen(ip, port)) {
                                val base = "http://$ip:$port"
                                NvrClient.probe(NvrClient.probeClient, base)?.let { info ->
                                    synchronized(hits) {
                                        hits.add(ScanHit(base, info.name, info.latencyMs))
                                    }
                                }
                            }
                        }
                        val d = done.incrementAndGet()
                        if (d % 16 == 0 || d == total) {
                            onProgress(Progress(true, d, total, subnet, hits.toList()))
                        }
                    }
                }
            }
            jobs.awaitAll()
        }
        return hits.sortedBy { it.latencyMs }
    }

    private fun portOpen(ip: String, port: Int): Boolean = try {
        Socket().use { s ->
            s.connect(InetSocketAddress(ip, port), CONNECT_TIMEOUT_MS)
            true
        }
    } catch (_: Exception) {
        false
    }
}
