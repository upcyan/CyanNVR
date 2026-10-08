plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
}

// 版本号从 server/version.go 的 CoreVersion 读取（单一真相源）。
// 此前 Android 版本号写死，与 fpk 版本经常不同步（如 fpk 1.10.15 / apk 1.10.13），
// 导致 App 内「检查更新」的版本比对失真。这里在配置阶段直接解析该文件。
val coreVersion: String = run {
    val f = rootProject.file("../server/version.go")
    if (f.exists()) {
        Regex("""const CoreVersion = "([^"]+)"""")
            .find(f.readText())
            ?.groupValues?.get(1)
            ?: "0.0.0"
    } else {
        "0.0.0"
    }
}
// versionCode 用 x*10000 + y*100 + z，保证随 x.y.z 单调递增
val coreVersionCode: Int = coreVersion.split(".").let { p ->
    (p.getOrNull(0)?.toIntOrNull() ?: 0) * 10000 +
        (p.getOrNull(1)?.toIntOrNull() ?: 0) * 100 +
        (p.getOrNull(2)?.toIntOrNull() ?: 0)
}

android {
    namespace = "com.cyannvr.app"
    compileSdk = 35

    signingConfigs {
        create("release") {
            // 仓库内签名文件 release.keystore（被 gitignore，不提交公开仓库）。
            // 首次构建前先执行：keytool -genkeypair -keystore release.keystore
            //   -alias cyannvr -keyalg RSA -keysize 2048 -validity 10950
            //   -storepass cyannvr -keypass cyannvr
            //   -dname "CN=CyanNVR,O=CyanNVR,C=CN"
            storeFile = rootProject.file("release.keystore")
            storePassword = "cyannvr"
            keyAlias = "cyannvr"
            keyPassword = "cyannvr"
        }
    }

    defaultConfig {
        applicationId = "com.cyannvr.app"
        minSdk = 24
        targetSdk = 35
        versionCode = coreVersionCode
        versionName = coreVersion
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            // keystore 存在时才签名；否则仍产出未签名 APK（便于 CI 首次构建）
            signingConfig = if (rootProject.file("release.keystore").exists()) {
                signingConfigs.getByName("release")
            } else {
                null
            }
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions {
        jvmTarget = "17"
    }
    buildFeatures {
        compose = true
    }
}

dependencies {
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.runtime.ktx)
    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.ui.tooling.preview)
    implementation(libs.androidx.compose.material3)
    implementation(libs.okhttp)
    implementation(libs.kotlinx.coroutines.android)
    implementation(libs.androidx.security.crypto)
    // 显式钉住 16KB 对齐版本（compose-ui 传递拉入的旧版 .so 未对齐，16KB 页设备弹兼容警告）
    implementation("androidx.graphics:graphics-path:1.0.1")
    implementation(libs.androidx.camera.camera2)
    implementation(libs.androidx.camera.lifecycle)
    implementation(libs.androidx.camera.view)
    implementation(libs.zxing.core)
}
