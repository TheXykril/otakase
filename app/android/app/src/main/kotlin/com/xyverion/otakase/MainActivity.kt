package com.xyverion.otakase

import android.app.Activity
import android.content.Intent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.IntentFilter
import android.os.BatteryManager
import android.os.Build
import android.text.format.DateFormat
import android.view.WindowInsets
import android.view.WindowInsetsController
import io.flutter.plugin.common.EventChannel
import android.speech.RecognizerIntent
import android.content.pm.PackageManager
import android.os.Handler
import android.os.Looper
import com.xyverion.otakase.mobilecore.Mobilecore
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import java.util.concurrent.Executors

class MainActivity : FlutterActivity() {
    private var deviceChannel: MethodChannel? = null
    private var statusEvents: EventChannel.EventSink? = null
    private var statusReceiver: BroadcastReceiver? = null
    private var pendingLogin: String? = null
    private fun loginUri(intent: Intent?): String? = intent?.data?.takeIf {
        it.scheme == "otakase" && it.host == "login"
    }?.toString()
    private fun deviceStatus(): Map<String, Any> {
        val battery = registerReceiver(null, IntentFilter(Intent.ACTION_BATTERY_CHANGED))
        val level = battery?.getIntExtra(BatteryManager.EXTRA_LEVEL, -1) ?: -1
        val scale = battery?.getIntExtra(BatteryManager.EXTRA_SCALE, -1) ?: -1
        val state = battery?.getIntExtra(BatteryManager.EXTRA_STATUS, -1) ?: -1
        val values = mutableMapOf<String, Any>(
            "use24Hour" to DateFormat.is24HourFormat(this),
            "voiceAvailable" to (Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH).resolveActivity(packageManager) != null)
        )
        if (level >= 0 && scale > 0) {
            values["battery"] = (level * 100 / scale).coerceIn(0, 100)
            values["charging"] = state == BatteryManager.BATTERY_STATUS_CHARGING || state == BatteryManager.BATTERY_STATUS_FULL
        }
        return values
    }
    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        loginUri(intent)?.let { uri ->
            if (deviceChannel != null) deviceChannel!!.invokeMethod("login", uri)
            else pendingLogin = uri
            intent.data = null
        }
    }
    override fun onResume() {
        super.onResume()
        if (Build.VERSION.SDK_INT >= 30) {
            window.insetsController?.apply {
                hide(WindowInsets.Type.statusBars())
                systemBarsBehavior = WindowInsetsController.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
            }
        }
        statusEvents?.success(deviceStatus())
    }
    private val core = Mobilecore.newCore()
    private val workers = Executors.newFixedThreadPool(4)
    private val main = Handler(Looper.getMainLooper())
    private var channel: MethodChannel? = null
    private var detached = false
    private var voiceResult: MethodChannel.Result? = null
    private val voiceRequest = 3101

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        detached = false
        pendingLogin = pendingLogin ?: loginUri(intent)
        intent.data = null
        deviceChannel = MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "otakase/device")
        deviceChannel!!.setMethodCallHandler { call, result ->
            when (call.method) {
                "initialLogin" -> { result.success(pendingLogin); pendingLogin = null }
                "status" -> result.success(deviceStatus())
                "launchUri" -> try {
                    val uri = android.net.Uri.parse(call.arguments as String)
                    require(uri.scheme == "https" && uri.host in listOf("anilist.co", "myanimelist.net", "otakase.xyverion.com", "bot.xyverion.com", "discord.com", "discord.gg"))
                    startActivity(Intent(Intent.ACTION_VIEW, uri))
                    result.success(null)
                } catch (_: Exception) { result.error("unavailable", "Browser unavailable", null) }
                else -> result.notImplemented()
            }
        }
        EventChannel(flutterEngine.dartExecutor.binaryMessenger, "otakase/status").setStreamHandler(object : EventChannel.StreamHandler {
            override fun onListen(arguments: Any?, events: EventChannel.EventSink) {
                statusEvents = events
                statusReceiver = object : BroadcastReceiver() {
                    override fun onReceive(context: Context?, intent: Intent?) { events.success(deviceStatus()) }
                }
                val filter = IntentFilter().apply {
                    addAction(Intent.ACTION_BATTERY_CHANGED)
                    addAction(Intent.ACTION_TIME_CHANGED)
                    addAction(Intent.ACTION_TIMEZONE_CHANGED)
                }
                if (Build.VERSION.SDK_INT >= 33) registerReceiver(statusReceiver, filter, Context.RECEIVER_NOT_EXPORTED)
                else registerReceiver(statusReceiver, filter)
                events.success(deviceStatus())
            }
            override fun onCancel(arguments: Any?) {
                statusReceiver?.let { unregisterReceiver(it) }
                statusReceiver = null
                statusEvents = null
            }
        })
        channel = MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "otakase/core")
        channel!!.setMethodCallHandler { call, result ->
            when (call.method) {
                "device" -> result.success(
                    packageManager.hasSystemFeature(PackageManager.FEATURE_LEANBACK) ||
                        !packageManager.hasSystemFeature(PackageManager.FEATURE_TOUCHSCREEN)
                )
                "voiceSearch" -> {
                    if (voiceResult != null) result.error("busy", "Voice search already open", null)
                    else try {
                        voiceResult = result
                        startActivityForResult(Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH).apply {
                            putExtra(RecognizerIntent.EXTRA_LANGUAGE_MODEL, RecognizerIntent.LANGUAGE_MODEL_FREE_FORM)
                            putExtra(RecognizerIntent.EXTRA_PROMPT, "Search Otakase")
                        }, voiceRequest)
                    } catch (_: Exception) {
                        voiceResult = null
                        result.error("unavailable", "Voice search unavailable", null)
                    }
                }
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

    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode == voiceRequest) {
            voiceResult?.success(if (resultCode == Activity.RESULT_OK)
                data?.getStringArrayListExtra(RecognizerIntent.EXTRA_RESULTS)?.firstOrNull() else null)
            voiceResult = null
        }
    }

    override fun cleanUpFlutterEngine(flutterEngine: FlutterEngine) {
        voiceResult?.success(null)
        voiceResult = null
        detached = true
        statusReceiver?.let { unregisterReceiver(it) }
        statusReceiver = null
        statusEvents = null
        deviceChannel?.setMethodCallHandler(null)
        deviceChannel = null
        channel?.setMethodCallHandler(null)
        channel = null
        core.close()
        workers.shutdown()
        super.cleanUpFlutterEngine(flutterEngine)
    }
}
