package com.dickyr15.dtv

import io.flutter.embedding.android.FlutterActivity
import android.Manifest
import android.media.MediaScannerConnection
import android.app.UiModeManager
import android.app.ActivityManager
import android.app.PictureInPictureParams
import android.os.Build
import android.os.Environment
import android.os.Bundle
import android.os.BatteryManager
import android.os.PowerManager
import android.os.SystemClock
import android.content.ContentValues
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.ActivityInfo
import android.content.pm.PackageManager
import android.content.res.Configuration
import android.graphics.Rect
import android.net.ConnectivityManager
import android.net.Uri
import android.util.Rational
import android.view.InputDevice
import android.view.WindowManager
import android.provider.MediaStore
import android.provider.Settings
import java.io.File
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import androidx.annotation.ChecksSdkIntAtLeast
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.embedding.engine.FlutterShellArgs
import io.flutter.plugin.common.MethodChannel

class MainActivity : FlutterActivity() {
    private var headroomReadAt = 0L
    private var thermalHeadroom: Double? = null
    private var deviceChannel: MethodChannel? = null
    private var televisionMode = false
    private var pendingExportPlaybackLogText: String? = null
    private var pendingExportPlaybackLogPrefix: String = "D-TV-playback-log"
    private var pendingExportPlaybackLogResult: MethodChannel.Result? = null
    private var pendingExportSettingsBackupContent: String? = null
    private var pendingExportSettingsBackupResult: MethodChannel.Result? = null

    companion object {
        private const val EXPORT_PLAYBACK_LOG_PERMISSION_REQUEST = 7412
    }

    @Suppress("DEPRECATION")
    private fun isTelevisionDevice(): Boolean {
        val configuration = resources.configuration
        val mode = (getSystemService(Context.UI_MODE_SERVICE) as? UiModeManager)?.currentModeType
            ?: (configuration.uiMode and Configuration.UI_MODE_TYPE_MASK)
        if (mode == Configuration.UI_MODE_TYPE_TELEVISION ||
            packageManager.hasSystemFeature(PackageManager.FEATURE_LEANBACK) ||
            packageManager.hasSystemFeature(PackageManager.FEATURE_LEANBACK_ONLY) ||
            packageManager.hasSystemFeature(PackageManager.FEATURE_TELEVISION) ||
            packageManager.hasSystemFeature("amazon.hardware.fire_tv")) {
            return true
        }
        if (mode == Configuration.UI_MODE_TYPE_CAR ||
            mode == Configuration.UI_MODE_TYPE_WATCH ||
            mode == Configuration.UI_MODE_TYPE_VR_HEADSET ||
            configuration.touchscreen != Configuration.TOUCHSCREEN_NOTOUCH ||
            packageManager.hasSystemFeature(PackageManager.FEATURE_TOUCHSCREEN) ||
            packageManager.hasSystemFeature(PackageManager.FEATURE_TELEPHONY) ||
            packageManager.hasSystemFeature(PackageManager.FEATURE_SENSOR_ACCELEROMETER) ||
            packageManager.hasSystemFeature(PackageManager.FEATURE_AUTOMOTIVE) ||
            packageManager.hasSystemFeature(PackageManager.FEATURE_WATCH) ||
            packageManager.hasSystemFeature(PackageManager.FEATURE_PC)) {
            return false
        }
        val remoteNavigation = configuration.navigation == Configuration.NAVIGATION_DPAD ||
            InputDevice.getDeviceIds().any { id ->
                val device = InputDevice.getDevice(id)
                device != null && !device.isVirtual && device.supportsSource(InputDevice.SOURCE_DPAD)
            }
        return remoteNavigation || packageManager.hasSystemFeature(PackageManager.FEATURE_LIVE_TV)
    }

    private fun renderingPreferences() =
        getSharedPreferences("duanju_rendering", Context.MODE_PRIVATE)

    private fun compatibleRendering(): Boolean =
        renderingPreferences().getBoolean("compatibleRendering", false)

    override fun getFlutterShellArgs(): FlutterShellArgs {
        val args = super.getFlutterShellArgs()
        // Android TV boxes using Mali/Vulkan have shown graphics-buffer allocation
        // failures under Impeller. Use the compatible Skia renderer on TV by default;
        // the saved preference continues to enable Skia on phones/tablets as well.
        if (compatibleRendering() || isTelevisionDevice()) {
            args.add("--enable-impeller=false")
        }
        return args
    }

    private fun restartApplication() {
        startActivity(Intent.makeRestartActivityTask(ComponentName(this, MainActivity::class.java)))
        Runtime.getRuntime().exit(0)
    }

    override fun setRequestedOrientation(requestedOrientation: Int) {
        super.setRequestedOrientation(
            if (televisionMode) ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE else requestedOrientation
        )
    }

    private fun exportPlaybackLog(
        text: String,
        fileNamePrefix: String = "D-TV-playback-log"
    ): Map<String, String> {
        val stamp = SimpleDateFormat("yyyyMMdd_HHmmss", Locale.US).format(Date())
        val safePrefix = fileNamePrefix.replace(Regex("[^A-Za-z0-9_-]"), "-").trim('-')
            .take(64).ifEmpty { "D-TV-diagnostic" }
        val fileName = "${safePrefix}_$stamp.txt"

        // Android 9 and earlier use legacy public-storage access. These
        // releases do not expose MediaStore.Downloads / RELATIVE_PATH.
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.Q) {
            val downloads = Environment.getExternalStoragePublicDirectory(
                Environment.DIRECTORY_DOWNLOADS
            )
            val directory = File(downloads, "D-TV")
            if (!directory.exists() && !directory.mkdirs()) {
                throw IllegalStateException("無法建立 Download/D-TV 資料夾")
            }
            val outputFile = File(directory, fileName)
            outputFile.writeText(text, Charsets.UTF_8)
            MediaScannerConnection.scanFile(
                this,
                arrayOf(outputFile.absolutePath),
                arrayOf("text/plain"),
                null
            )
            return mapOf("fileName" to fileName, "path" to "Download/D-TV/$fileName")
        }

        // Android 10+ uses MediaStore and needs no broad storage permission.
        val values = ContentValues().apply {
            put(MediaStore.MediaColumns.DISPLAY_NAME, fileName)
            put(MediaStore.MediaColumns.MIME_TYPE, "text/plain")
            put(MediaStore.MediaColumns.RELATIVE_PATH, "${Environment.DIRECTORY_DOWNLOADS}/D-TV")
            put(MediaStore.MediaColumns.IS_PENDING, 1)
        }
        val uri = contentResolver.insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, values)
            ?: throw IllegalStateException("無法在下載資料夾建立日誌檔")
        try {
            val output = contentResolver.openOutputStream(uri)
                ?: throw IllegalStateException("無法寫入日誌檔")
            output.use {
                it.write(text.toByteArray(Charsets.UTF_8))
                it.flush()
            }
            val published = ContentValues().apply {
                put(MediaStore.MediaColumns.IS_PENDING, 0)
            }
            if (contentResolver.update(uri, published, null, null) <= 0) {
                throw IllegalStateException("無法完成日誌檔案儲存")
            }
            return mapOf("fileName" to fileName, "path" to "Download/D-TV/$fileName")
        } catch (error: Exception) {
            contentResolver.delete(uri, null, null)
            throw error
        }
    }

    private fun exportSettingsBackup(content: String): Map<String, String> {
        val stamp = SimpleDateFormat("yyyyMMdd_HHmmss", Locale.US).format(Date())
        val fileName = "D-TV-settings-backup_$stamp.json"

        // Android 9 and earlier need the existing public Downloads permission.
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.Q) {
            val downloads = Environment.getExternalStoragePublicDirectory(
                Environment.DIRECTORY_DOWNLOADS
            )
            val directory = File(downloads, "D-TV")
            if (!directory.exists() && !directory.mkdirs()) {
                throw IllegalStateException("無法建立 Download/D-TV 資料夾")
            }
            val outputFile = File(directory, fileName)
            outputFile.writeText(content, Charsets.UTF_8)
            MediaScannerConnection.scanFile(
                this,
                arrayOf(outputFile.absolutePath),
                arrayOf("application/json"),
                null
            )
            return mapOf("fileName" to fileName, "path" to "Download/D-TV/$fileName")
        }

        // Android 10+ can write straight to Downloads without opening a file picker.
        val values = ContentValues().apply {
            put(MediaStore.MediaColumns.DISPLAY_NAME, fileName)
            put(MediaStore.MediaColumns.MIME_TYPE, "application/json")
            put(MediaStore.MediaColumns.RELATIVE_PATH, "${Environment.DIRECTORY_DOWNLOADS}/D-TV")
            put(MediaStore.MediaColumns.IS_PENDING, 1)
        }
        val uri = contentResolver.insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, values)
            ?: throw IllegalStateException("無法在下載資料夾建立設定備份")
        try {
            val output = contentResolver.openOutputStream(uri)
                ?: throw IllegalStateException("無法寫入設定備份")
            output.use {
                it.write(content.toByteArray(Charsets.UTF_8))
                it.flush()
            }
            val published = ContentValues().apply {
                put(MediaStore.MediaColumns.IS_PENDING, 0)
            }
            if (contentResolver.update(uri, published, null, null) <= 0) {
                throw IllegalStateException("無法完成設定備份儲存")
            }
            return mapOf("fileName" to fileName, "path" to "Download/D-TV/$fileName")
        } catch (error: Exception) {
            contentResolver.delete(uri, null, null)
            throw error
        }
    }

    private fun handleExportSettingsBackup(content: String, result: MethodChannel.Result) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.Q &&
            checkSelfPermission(Manifest.permission.WRITE_EXTERNAL_STORAGE) !=
                PackageManager.PERMISSION_GRANTED
        ) {
            if (pendingExportPlaybackLogResult != null ||
                pendingExportSettingsBackupResult != null
            ) {
                result.error("export_in_progress", "匯出作業正在等待儲存權限確認", null)
                return
            }
            pendingExportSettingsBackupContent = content
            pendingExportSettingsBackupResult = result
            requestPermissions(
                arrayOf(Manifest.permission.WRITE_EXTERNAL_STORAGE),
                EXPORT_PLAYBACK_LOG_PERMISSION_REQUEST
            )
            return
        }
        try {
            result.success(exportSettingsBackup(content))
        } catch (error: Exception) {
            result.error(
                "settings_backup_export_failed",
                "匯出設定備份失敗：${error.message ?: "未知錯誤"}",
                null
            )
        }
    }

    private fun handleExportPlaybackLog(
        text: String,
        fileNamePrefix: String,
        result: MethodChannel.Result
    ) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.Q &&
            checkSelfPermission(Manifest.permission.WRITE_EXTERNAL_STORAGE) !=
                PackageManager.PERMISSION_GRANTED
        ) {
            if (pendingExportPlaybackLogResult != null ||
                pendingExportSettingsBackupResult != null
            ) {
                result.error("export_in_progress", "匯出作業正在等待儲存權限確認", null)
                return
            }
            pendingExportPlaybackLogText = text
            pendingExportPlaybackLogPrefix = fileNamePrefix
            pendingExportPlaybackLogResult = result
            requestPermissions(
                arrayOf(Manifest.permission.WRITE_EXTERNAL_STORAGE),
                EXPORT_PLAYBACK_LOG_PERMISSION_REQUEST
            )
            return
        }

        try {
            result.success(exportPlaybackLog(text, fileNamePrefix))
        } catch (error: Exception) {
            result.error(
                "export_failed",
                "匯出失敗：${error.message ?: "未知錯誤"}",
                null
            )
        }
    }

    override fun onRequestPermissionsResult(
        requestCode: Int,
        permissions: Array<out String>,
        grantResults: IntArray
    ) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        if (requestCode != EXPORT_PLAYBACK_LOG_PERMISSION_REQUEST) return

        val backupResult = pendingExportSettingsBackupResult
        val backupContent = pendingExportSettingsBackupContent
        pendingExportSettingsBackupResult = null
        pendingExportSettingsBackupContent = null
        if (backupResult != null && backupContent != null) {
            if (grantResults.firstOrNull() != PackageManager.PERMISSION_GRANTED) {
                backupResult.error(
                    "storage_permission_denied",
                    "需要允許儲存空間權限，才能將設定備份匯出到 Download/D-TV。",
                    null
                )
                return
            }
            try {
                backupResult.success(exportSettingsBackup(backupContent))
            } catch (error: Exception) {
                backupResult.error(
                    "settings_backup_export_failed",
                    "匯出設定備份失敗：${error.message ?: "未知錯誤"}",
                    null
                )
            }
            return
        }

        val pendingResult = pendingExportPlaybackLogResult
        val pendingText = pendingExportPlaybackLogText
        val pendingPrefix = pendingExportPlaybackLogPrefix
        pendingExportPlaybackLogResult = null
        pendingExportPlaybackLogText = null
        pendingExportPlaybackLogPrefix = "D-TV-playback-log"
        if (pendingResult == null || pendingText == null) return

        if (grantResults.firstOrNull() != PackageManager.PERMISSION_GRANTED) {
            pendingResult.error(
                "storage_permission_denied",
                "需要允許儲存空間權限，才能將 TXT 匯出到 Download/D-TV。",
                null
            )
            return
        }
        try {
            pendingResult.success(exportPlaybackLog(pendingText, pendingPrefix))
        } catch (error: Exception) {
            pendingResult.error(
                "export_failed",
                "匯出失敗：${error.message ?: "未知錯誤"}",
                null
            )
        }
    }

    private fun playbackPower(): Map<String, Any?> {
        val power = getSystemService(Context.POWER_SERVICE) as? PowerManager
        val activity = getSystemService(Context.ACTIVITY_SERVICE) as? ActivityManager
        val battery = registerReceiver(null, IntentFilter(Intent.ACTION_BATTERY_CHANGED))
        val now = SystemClock.elapsedRealtime()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R &&
            (headroomReadAt == 0L || now - headroomReadAt >= 10000L)) {
            headroomReadAt = now
            thermalHeadroom = runCatching {
                power?.getThermalHeadroom(0)?.toDouble()?.takeIf { it.isFinite() }
            }.getOrNull()
        }
        val thermalStatus = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            power?.currentThermalStatus ?: PowerManager.THERMAL_STATUS_NONE
        } else 0
        return mapOf(
            "batterySaver" to (power?.isPowerSaveMode ?: false),
            "onBattery" to ((battery?.getIntExtra(BatteryManager.EXTRA_PLUGGED, -1) ?: -1) <= 0),
            "thermalStatus" to thermalStatus,
            "headroom" to thermalHeadroom,
            "lowMemory" to (activity?.isLowRamDevice ?: true),
            "gles" to (activity?.deviceConfigurationInfo?.reqGlEsVersion ?: 0)
        )
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        televisionMode = if (savedInstanceState?.containsKey("duanju.televisionMode") == true) {
            savedInstanceState.getBoolean("duanju.televisionMode")
        } else isTelevisionDevice()
        super.onCreate(savedInstanceState)
        if (televisionMode) requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            window.isNavigationBarContrastEnforced = false
            window.isStatusBarContrastEnforced = false
        }
    }

    override fun onResume() {
        super.onResume()
        if (televisionMode) requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE
    }

    override fun onSaveInstanceState(outState: Bundle) {
        outState.putBoolean("duanju.televisionMode", televisionMode)
        super.onSaveInstanceState(outState)
    }

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        deviceChannel = MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "duanju/device")
            .also { channel ->
                channel.setMethodCallHandler { call, result ->
                    when (call.method) {
                        "deviceInfo" -> {
                            val packageInfo = packageManager.getPackageInfo(packageName, 0)
                            val versionName = packageInfo.versionName.orEmpty()
                            val versionCode = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
                                packageInfo.longVersionCode
                            } else {
                                @Suppress("DEPRECATION")
                                packageInfo.versionCode.toLong()
                            }
                            // Include the Android build number in About and startup diagnostics,
                            // so every CI APK can be identified unambiguously.
                            val displayVersion = if (versionName.isNotBlank()) {
                                "$versionName+$versionCode"
                            } else versionCode.toString()
                            result.success(mapOf("television" to isTelevisionDevice(), "version" to displayVersion))
                        }
                        "setTelevisionMode" -> {
                            val enabled = call.argument<Boolean>("enabled")
                            if (enabled == null) {
                                result.error("invalid_display_mode", "缺少電視模式狀態", null)
                            } else {
                                val changed = televisionMode != enabled
                                televisionMode = enabled
                                if (enabled || changed) {
                                    requestedOrientation = if (enabled) {
                                        ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE
                                    } else ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED
                                }
                                result.success(null)
                            }
                        }
                        "exportPlaybackLog" -> {
                            val text = call.argument<String>("text")
                            if (text.isNullOrBlank()) {
                                result.error("empty_log", "沒有可匯出的日誌內容", null)
                            } else {
                                handleExportPlaybackLog(text, "D-TV-playback-log", result)
                            }
                        }
                        "exportDiagnosticText" -> {
                            val text = call.argument<String>("text")
                            val prefix = call.argument<String>("fileNamePrefix")
                                ?.replace(Regex("[^A-Za-z0-9_-]"), "-")
                                ?.trim('-')
                                ?.take(64)
                                ?.takeIf { it.isNotEmpty() }
                                ?: "D-TV-diagnostic"
                            if (text.isNullOrBlank()) {
                                result.error("empty_diagnostic", "沒有可匯出的診斷資訊", null)
                            } else {
                                handleExportPlaybackLog(text, prefix, result)
                            }
                        }
                        "exportSettingsBackup" -> {
                            val content = call.argument<String>("content")
                            if (content.isNullOrBlank()) {
                                result.error("empty_settings_backup", "沒有可匯出的設定備份內容", null)
                            } else {
                                handleExportSettingsBackup(content, result)
                            }
                        }
                        "playbackPower" -> result.success(runCatching { playbackPower() }.getOrNull())
                        "compatibleRendering" -> result.success(compatibleRendering())
                        "setCompatibleRendering" -> {
                            val enabled = call.argument<Boolean>("enabled")
                            if (enabled == null) {
                                result.error("invalid_rendering_mode", "缺少相容渲染模式狀態", null)
                            } else if (!renderingPreferences().edit()
                                    .putBoolean("compatibleRendering", enabled).commit()) {
                                result.error("rendering_mode_not_saved", "相容渲染模式未能儲存", null)
                            } else {
                                result.success(null)
                                restartApplication()
                            }
                        }
                        "systemProxy" -> {
                            val connection = getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
                            val proxy = connection.defaultProxy
                            val host = proxy?.host.orEmpty()
                            val address = if (host.isNotEmpty() && (proxy?.port ?: 0) > 0) {
                                "http://${if (host.contains(':')) "[$host]" else host}:${proxy!!.port}"
                            } else ""
                            result.success(mapOf(
                                "http" to address,
                                "https" to address,
                                "bypass" to (proxy?.exclusionList?.toList() ?: emptyList<String>()),
                                "pac" to (proxy != null && proxy.pacFileUrl != Uri.EMPTY)
                            ))
                        }
                        "pictureInPictureStatus" -> result.success(pictureInPictureStatus())
                        "enterPictureInPicture" -> {
                            val width = call.argument<Int>("width") ?: 16
                            val height = call.argument<Int>("height") ?: 9
                            result.success(enterPlayerPictureInPicture(
                                width,
                                height,
                                call.argument<Int>("left"),
                                call.argument<Int>("top"),
                                call.argument<Int>("right"),
                                call.argument<Int>("bottom")
                            ))
                        }
                        "getBrightness" -> {
                            val lp = window.attributes
                            val current = if (lp.screenBrightness >= 0f) {
                                lp.screenBrightness.toDouble()
                            } else {
                                try {
                                    val sys = Settings.System.getInt(
                                        contentResolver,
                                        Settings.System.SCREEN_BRIGHTNESS
                                    )
                                    (sys / 255.0).coerceIn(0.0, 1.0)
                                } catch (e: Exception) {
                                    0.5
                                }
                            }
                            result.success(current)
                        }
                        "setBrightness" -> {
                            val value = call.argument<Double>("brightness")?.toFloat()
                            if (value != null) {
                                val lp = window.attributes
                                lp.screenBrightness = value.coerceIn(0.01f, 1.0f)
                                window.attributes = lp
                                result.success(true)
                            } else {
                                result.error("invalid_arg", "缺少亮度數值", null)
                            }
                        }
                        "resetBrightness" -> {
                            val lp = window.attributes
                            lp.screenBrightness = WindowManager.LayoutParams.BRIGHTNESS_OVERRIDE_NONE
                            window.attributes = lp
                            result.success(true)
                        }
                        "openExternalPlayer" -> {
                            val url = call.argument<String>("url")
                            val title = call.argument<String>("title") ?: "短劇"
                            if (!url.isNullOrEmpty()) {
                                try {
                                    val intent = Intent(Intent.ACTION_VIEW).apply {
                                        setDataAndType(Uri.parse(url), "video/*")
                                        putExtra("title", title)
                                        putExtra("return_result", true)
                                        addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                                    }
                                    startActivity(intent)
                                    result.success(true)
                                } catch (e: Exception) {
                                    result.error("no_player", "未找到外部播放器: ${e.message}", null)
                                }
                            } else {
                                result.error("invalid_url", "播放地址為空", null)
                            }
                        }
                        else -> result.notImplemented()
                    }
                }
            }
    }

    @ChecksSdkIntAtLeast(api = Build.VERSION_CODES.O)
    private fun pictureInPictureSupported(): Boolean {
        return Build.VERSION.SDK_INT >= Build.VERSION_CODES.O &&
            packageManager.hasSystemFeature(PackageManager.FEATURE_PICTURE_IN_PICTURE)
    }

    private fun pictureInPictureStatus(): Map<String, Any> {
        return mapOf(
            "supported" to pictureInPictureSupported(),
            "active" to (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N && isInPictureInPictureMode)
        )
    }

    private fun enterPlayerPictureInPicture(
        width: Int,
        height: Int,
        left: Int?,
        top: Int?,
        right: Int?,
        bottom: Int?
    ): Map<String, Any> {
        if (!pictureInPictureSupported()) return pictureInPictureStatus()
        val safeWidth = width.coerceIn(1, 10000)
        val safeHeight = height.coerceIn(1, 10000)
        return runCatching {
            val builder = PictureInPictureParams.Builder()
            builder.setAspectRatio(Rational(safeWidth, safeHeight))
            if (left != null && top != null && right != null && bottom != null &&
                right > left && bottom > top) {
                builder.setSourceRectHint(Rect(left, top, right, bottom))
            }
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
                builder.setAutoEnterEnabled(true)
            }
            val entered = enterPictureInPictureMode(builder.build())
            pictureInPictureStatus() + ("requested" to entered)
        }.getOrElse { pictureInPictureStatus() }
    }

    override fun onPictureInPictureModeChanged(
        isInPictureInPictureMode: Boolean,
        newConfig: Configuration
    ) {
        super.onPictureInPictureModeChanged(isInPictureInPictureMode, newConfig)
        deviceChannel?.invokeMethod(
            "pictureInPictureChanged",
            mapOf("active" to isInPictureInPictureMode)
        )
    }

    override fun cleanUpFlutterEngine(flutterEngine: FlutterEngine) {
        deviceChannel?.setMethodCallHandler(null)
        deviceChannel = null
        super.cleanUpFlutterEngine(flutterEngine)
    }
}
