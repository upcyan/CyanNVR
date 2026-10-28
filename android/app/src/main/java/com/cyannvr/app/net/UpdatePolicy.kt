package com.cyannvr.app.net

internal fun apkAssetVersion(name: String): String? =
    Regex("""^CyanNVR_([0-9]+\.[0-9]+\.[0-9]+)_android\.apk$""").matchEntire(name)?.groupValues?.get(1)

internal fun compareApkVersions(a: String, b: String): Int {
    val aa = a.removePrefix("v").split(".").map { it.toIntOrNull() ?: 0 }
    val bb = b.removePrefix("v").split(".").map { it.toIntOrNull() ?: 0 }
    for (i in 0..2) {
        val result = (aa.getOrElse(i) { 0 }).compareTo(bb.getOrElse(i) { 0 })
        if (result != 0) return result
    }
    return 0
}

internal fun requireApkIdentity(
    packageName: String, installedPackage: String, version: String, installedVersion: String,
    code: Long, installedCode: Long, signers: Set<String>, installedSigners: Set<String>,
    expectedVersion: String?
) {
    check(packageName == installedPackage) { "安装包包名不匹配" }
    check(expectedVersion == null || version == expectedVersion) { "安装包实际版本与更新版本不一致" }
    check(compareApkVersions(version, installedVersion) > 0 && code > installedCode) { "安装包不是更新版本，已阻止安装" }
    check(signers.isNotEmpty() && signers == installedSigners) { "安装包签名与当前 App 不一致" }
}
