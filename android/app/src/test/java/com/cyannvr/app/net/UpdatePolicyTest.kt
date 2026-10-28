package com.cyannvr.app.net

import org.junit.Assert.*
import org.junit.Test

class UpdatePolicyTest {
    @Test fun releaseTagCannotTurnOlderApkIntoUpdate() {
        val apk = apkAssetVersion("CyanNVR_1.10.33_android.apk")!!
        assertTrue(compareApkVersions(apk, "1.10.35") < 0)
    }
    @Test fun equalVersionIsNotUpdate() { assertEquals(0, compareApkVersions("1.10.35", "1.10.35")) }
    @Test fun numericOrdering() { assertTrue(compareApkVersions("1.10.40", "1.10.9") > 0) }
    @Test fun rejectsUnrelatedAndMalformedNames() {
        listOf("CyanNVR_1.10.35-3_x86.fpk", "other_1.10.38_android.apk", "CyanNVR_1.10.38_android.apk.bak").forEach { assertNull(apkAssetVersion(it)) }
    }
    private fun validate(pkg: String = "com.cyannvr.app", version: String = "1.10.38", code: Long = 11038,
                         signers: Set<String> = setOf("release"), expected: String = "1.10.38") =
        requireApkIdentity(pkg, "com.cyannvr.app", version, "1.10.35", code, 11035, signers, setOf("release"), expected)
    @Test fun acceptsMatchingUpgrade() { validate() }
    @Test fun rejectsWrongPackage() { assertThrows(IllegalStateException::class.java) { validate(pkg = "other") } }
    @Test fun rejectsRenamedOldApk() { assertThrows(IllegalStateException::class.java) { validate(version = "1.10.33") } }
    @Test fun rejectsUnchangedVersionCode() { assertThrows(IllegalStateException::class.java) { validate(code = 11035) } }
    @Test fun rejectsWrongSignature() { assertThrows(IllegalStateException::class.java) { validate(signers = setOf("other")) } }
    @Test fun rejectsUnsignedApk() { assertThrows(IllegalStateException::class.java) { validate(signers = emptySet()) } }
}
