package com.xyverion.otakase

import android.content.ClipData
import android.content.Intent
import android.content.pm.PackageInfo
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.provider.Settings
import androidx.core.content.FileProvider
import io.flutter.embedding.android.FlutterActivity
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel
import java.io.File
import java.security.MessageDigest
import java.util.concurrent.Executors

class AppUpdates(private val activity: FlutterActivity, messenger: BinaryMessenger) {
    private val channel = MethodChannel(messenger, "otakase/updates")
    private val worker = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())
    private val directory = File(activity.cacheDir, "updates")
    private var closed = false

    init {
        channel.setMethodCallHandler { call, result ->
            when (call.method) {
                "installed" -> {
                    val info = installed()
                    result.success(mapOf("name" to info.versionName, "code" to code(info)))
                }
                "installationAllowed" -> result.success(installationAllowed())
                "createDownload" -> try {
                    directory.mkdirs()
                    // Keep recent files readable while a system installer owns their URI.
                    directory.listFiles()?.filter { System.currentTimeMillis() - it.lastModified() > 86400000L }
                        ?.forEach { it.delete() }
                    result.success(File.createTempFile("otakase-", ".apk", directory).absolutePath)
                } catch (_: Exception) {
                    result.error("storage", "Not enough space to download an update.", null)
                }
                "validate", "install" -> worker.execute {
                    try {
                        val file = validate(call)
                        main.post {
                            if (!closed) {
                                if (call.method == "validate") result.success(null)
                                else launchInstaller(file, result)
                            }
                        }
                    } catch (e: Exception) {
                        main.post {
                            if (!closed) result.error("invalid_update", e.message ?: "Update verification failed.", null)
                        }
                    }
                }
                else -> result.notImplemented()
            }
        }
    }

    @Suppress("DEPRECATION")
    private fun installed(): PackageInfo = activity.packageManager.getPackageInfo(activity.packageName, signatureFlags())

    @Suppress("DEPRECATION")
    private fun signatureFlags() = if (Build.VERSION.SDK_INT >= 28)
        PackageManager.GET_SIGNING_CERTIFICATES else PackageManager.GET_SIGNATURES

    @Suppress("DEPRECATION")
    private fun code(info: PackageInfo): Long = if (Build.VERSION.SDK_INT >= 28)
        info.longVersionCode else info.versionCode.toLong()

    @Suppress("DEPRECATION")
    private fun signers(info: PackageInfo): Set<String> {
        val signatures = if (Build.VERSION.SDK_INT >= 28) info.signingInfo?.apkContentsSigners else info.signatures
        return signatures?.map { signature ->
            MessageDigest.getInstance("SHA-256").digest(signature.toByteArray())
                .joinToString("") { "%02x".format(it) }
        }?.toSet() ?: emptySet()
    }

    @Suppress("DEPRECATION")
    private fun validate(call: MethodCall): File {
        val path = call.argument<String>("path") ?: error("Update download is missing. Please download again.")
        val expectedCode = call.argument<Number>("code")?.toLong() ?: error("Update version is missing.")
        val expectedHash = call.argument<String>("sha256") ?: error("Update checksum is missing.")
        val file = File(path).canonicalFile
        require(file.parentFile == directory.canonicalFile && file.isFile && file.extension == "apk") {
            "Update download is missing. Please download again."
        }
        val digest = MessageDigest.getInstance("SHA-256")
        file.inputStream().use { input ->
            val buffer = ByteArray(65536)
            while (true) {
                if (Thread.currentThread().isInterrupted) error("Update cancelled.")
                val count = input.read(buffer)
                if (count < 0) break
                digest.update(buffer, 0, count)
            }
        }
        require(digest.digest().joinToString("") { "%02x".format(it) } == expectedHash) {
            "Update checksum does not match. Please download again."
        }
        val archive = activity.packageManager.getPackageArchiveInfo(file.path, signatureFlags())
            ?: error("The downloaded file is not a valid Android app.")
        val current = installed()
        require(archive.packageName == activity.packageName) { "The downloaded app is not Otakase." }
        require(code(archive) == expectedCode && code(archive) > code(current)) {
            "This update is not newer than the installed app."
        }
        val trusted = signers(current)
        require(trusted.isNotEmpty() && trusted == signers(archive)) {
            "This update uses a different signing key. Install updates from the same distributor."
        }
        require((archive.applicationInfo?.minSdkVersion ?: Int.MAX_VALUE) <= Build.VERSION.SDK_INT) {
            "This update needs a newer Android version."
        }
        return file
    }

    private fun installationAllowed() = Build.VERSION.SDK_INT < 26 || activity.packageManager.canRequestPackageInstalls()

    private fun launchInstaller(file: File, result: MethodChannel.Result) {
        try {
            if (!installationAllowed()) {
                activity.startActivity(Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES,
                    Uri.parse("package:${activity.packageName}")))
                result.success(false)
                return
            }
            val uri = FileProvider.getUriForFile(activity, "${activity.packageName}.updates", file)
            activity.startActivity(Intent(Intent.ACTION_VIEW).apply {
                setDataAndType(uri, "application/vnd.android.package-archive")
                clipData = ClipData.newRawUri("Otakase update", uri)
                addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
            })
            result.success(true)
        } catch (_: Exception) {
            result.error("installer", "Android blocked the installer. Check your device's install permissions and try again.", null)
        }
    }

    fun close() {
        closed = true
        channel.setMethodCallHandler(null)
        worker.shutdownNow()
    }
}
