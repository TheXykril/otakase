package com.xyverion.otakase

import android.content.pm.PackageManager
import android.os.Handler
import android.os.Looper
import com.xyverion.otakase.mobilecore.Mobilecore
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import java.util.concurrent.Executors

class MainActivity : FlutterActivity() {
    private val core = Mobilecore.newCore()
    private val workers = Executors.newFixedThreadPool(4)
    private val main = Handler(Looper.getMainLooper())
    private var channel: MethodChannel? = null
    private var detached = false

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        detached = false
        channel = MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "otakase/core")
        channel!!.setMethodCallHandler { call, result ->
            when (call.method) {
                "device" -> result.success(
                    packageManager.hasSystemFeature(PackageManager.FEATURE_LEANBACK) ||
                        !packageManager.hasSystemFeature(PackageManager.FEATURE_TOUCHSCREEN)
                )
                "start" -> {
                    val input = call.arguments as? String
                    if (input == null) result.error("invalid_request", "JSON required", null)
                    else {
                        val id = core.start(input)
                        if (id == 0L) result.error("busy", "Core closed or busy", null)
                        else result.success(id)
                    }
                }
                "result" -> {
                    val id = (call.arguments as? Number)?.toLong()
                    if (id == null) result.error("invalid_handle", "Handle required", null)
                    else workers.execute {
                        try {
                            val response = core.result(id)
                            main.post { if (!detached) result.success(response) }
                        } catch (_: Exception) {
                            main.post { if (!detached) result.error("core_error", "Core request failed", null) }
                        }
                    }
                }
                "cancel" -> {
                    (call.arguments as? Number)?.toLong()?.let { core.cancel(it) }
                    result.success(null)
                }
                else -> result.notImplemented()
            }
        }
    }

    override fun cleanUpFlutterEngine(flutterEngine: FlutterEngine) {
        detached = true
        channel?.setMethodCallHandler(null)
        channel = null
        core.close()
        workers.shutdown()
        super.cleanUpFlutterEngine(flutterEngine)
    }
}
