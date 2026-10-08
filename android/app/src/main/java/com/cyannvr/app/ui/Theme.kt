package com.cyannvr.app.ui

import android.content.res.Configuration
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalConfiguration

private val Cyan = Color(0xFF0891B2)
private val CyanDark = Color(0xFF22D3EE)

private val LightColors = lightColorScheme(
    primary = Cyan,
    onPrimary = Color.White,
    primaryContainer = Color(0xFFCFFAFE),
    onPrimaryContainer = Color(0xFF083344),
    secondary = Color(0xFF0E7490),
)

private val DarkColors = darkColorScheme(
    primary = CyanDark,
    onPrimary = Color(0xFF083344),
    primaryContainer = Color(0xFF155E75),
    onPrimaryContainer = Color(0xFFCFFAFE),
    secondary = Color(0xFF67E8F9),
)

/**
 * 跟随系统深色模式。
 *
 * 用 LocalConfiguration.current.uiMode 实时读取，而不是 isSystemInDarkTheme()：
 * MainActivity 的 configChanges 声明了 uiMode，系统切深色时 Activity 不重建，
 * 但 LocalConfiguration 仍会随 onConfigurationChanged 更新并触发重组——
 * isSystemInDarkTheme() 依赖旧 snapshot，会读到过时值（系统已深色、App 仍浅色）。
 */
@Composable
fun CyanNvrTheme(
    darkTheme: Boolean? = null,
    content: @Composable () -> Unit,
) {
    val config = LocalConfiguration.current
    val systemDark = (config.uiMode and Configuration.UI_MODE_NIGHT_MASK) ==
        Configuration.UI_MODE_NIGHT_YES
    val dark = darkTheme ?: systemDark
    MaterialTheme(
        colorScheme = if (dark) DarkColors else LightColors,
        content = content,
    )
}
