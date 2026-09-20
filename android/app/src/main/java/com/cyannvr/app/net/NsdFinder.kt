package com.cyannvr.app.net

import android.content.Context
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import com.cyannvr.app.model.ScanHit

/**
 * 通过 mDNS（NSD）发现局域网内广播 _cyannvr._tcp 的 CyanNVR 服务端。
 * 与网段扫描互补：有广播的服务器可秒级发现，无广播的靠端口扫描兜底。
 */
object NsdFinder {

    const val SERVICE_TYPE = "_cyannvr._tcp."

    interface Controller {
        fun stop()
    }

    fun discover(context: Context, onFound: (ScanHit) -> Unit, onError: (String) -> Unit): Controller? {
        val nsdManager = try {
            context.getSystemService(NsdManager::class.java) ?: return null
        } catch (_: Exception) {
            return null
        }
        val found = HashSet<String>()

        @Suppress("DEPRECATION")
        val resolveListener = object : NsdManager.ResolveListener {
            override fun onResolveFailed(info: NsdServiceInfo, errorCode: Int) {}
            override fun onServiceResolved(info: NsdServiceInfo) {
                val host = info.host?.hostAddress ?: return
                if (host == "0.0.0.0") return
                val key = "$host:${info.port}"
                if (!found.add(key)) return
                val name = if (info.serviceName.isNullOrBlank()) "CyanNVR" else info.serviceName!!
                onFound(ScanHit("http://$host:${info.port}", name, 0))
            }
        }

        @Suppress("DEPRECATION")
        val discoveryListener = object : NsdManager.DiscoveryListener {
            override fun onDiscoveryStarted(regType: String) {}
            override fun onStartDiscoveryFailed(serviceType: String, errorCode: Int) {
                onError("mDNS 发现启动失败（errorCode=$errorCode）")
            }
            override fun onStopDiscoveryFailed(serviceType: String, errorCode: Int) {}
            override fun onDiscoveryStopped(serviceType: String) {}
            override fun onServiceLost(serviceInfo: NsdServiceInfo) {}
            override fun onServiceFound(serviceInfo: NsdServiceInfo) {
                if (!serviceInfo.serviceType.startsWith("_cyannvr._tcp")) return
                nsdManager.resolveService(serviceInfo, resolveListener)
            }
        }

        return try {
            nsdManager.discoverServices(SERVICE_TYPE, NsdManager.PROTOCOL_DNS_SD, discoveryListener)
            object : Controller {
                override fun stop() {
                    runCatching { nsdManager.stopServiceDiscovery(discoveryListener) }
                }
            }
        } catch (e: Exception) {
            onError(e.message ?: "NSD 不可用")
            null
        }
    }
}
