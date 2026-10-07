plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
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
        versionCode = 1013
        versionName = "1.10.13"
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
    implementation(libs.androidx.camera.camera2)
    implementation(libs.androidx.camera.lifecycle)
    implementation(libs.androidx.camera.view)
    implementation(libs.zxing.core)
}
