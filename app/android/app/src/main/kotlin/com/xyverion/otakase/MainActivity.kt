package com.xyverion.otakase

import android.app.Activity
import android.content.Intent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.IntentFilter
import android.os.BatteryManager
import android.os.Build
import android.os.Bundle
import android.view.WindowManager
import android.text.format.DateFormat
import android.view.WindowInsets
import android.view.WindowInsetsController
import io.flutter.plugin.common.EventChannel
import android.speech.RecognizerIntent
import android.content.pm.PackageManager
import android.os.Handler
import android.os.Looper
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.net.wifi.WifiManager
import com.xyverion.otakase.mobilecore.Mobilecore
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import java.net.NetworkInterface
import java.util.concurrent.Executors

class MainActivity : FlutterActivity() {
    private var appUpdates: AppUpdates? = null
    private var deviceChannel: MethodChannel? = null
    private var statusEvents: EventChannel.EventSink? = null
    private var statusReceiver: BroadcastReceiver? = null
    private var pendingLogin: String? = null
    private var pendingRemote: String? = null
    private var multicastLock: WifiManager.MulticastLock? = null
    private var workText: String? = null
    private fun remoteUri(intent: Intent?): String? = intent?.data?.takeIf {
        it.scheme == "otakase" && it.host == "remote"
    }?.toString()
    private fun loginUri(intent: Intent?): String? = intent?.data?.takeIf {
        // Tracker logins and Otakase account sign-ins share one route.
        it.scheme == "otakase" && (it.host == "login" || it.host == "account")
    }?.toString()
    // The Wi-Fi or Ethernet interface this device is on, as {name, index}.
    // Android 11 and later hide the interface list from the Go core, which
    // needs it to search for Chromecasts.
    private fun localNetwork(): Map<String, Any>? {
        val connectivity = getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager ?: return null
        return try {
            @Suppress("DEPRECATION")
            val networks = connectivity.allNetworks.toList()
            val local = networks.sortedBy { if (it == connectivity.activeNetwork) 0 else 1 }.firstOrNull { network ->
                val caps = connectivity.getNetworkCapabilities(network) ?: return@firstOrNull false
                caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) || caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET)
            } ?: return null
            val name = connectivity.getLinkProperties(local)?.interfaceName ?: return null
            val index = NetworkInterface.getByName(name)?.index ?: return null
            if (index <= 0) null else mapOf("name" to name, "index" to index)
        } catch (_: Exception) {
            null
        }
    }

    /** The name people gave this phone or TV in its settings, else its maker and model. */
    private fun deviceName(): String {
        val named = try {
            android.provider.Settings.Global.getString(contentResolver, "device_name")
        } catch (_: Exception) { null }
        if (!named.isNullOrBlank()) return named.trim()
        val maker = Build.MANUFACTURER.orEmpty().replaceFirstChar { it.uppercase() }
        val model = Build.MODEL.orEmpty()
        return if (model.startsWith(maker, ignoreCase = true)) model else "$maker $model".trim()
    }

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
    override fun onCreate(savedInstanceState: Bundle?) {
        // Draw under the camera cutout too. Without this Android leaves a
        // black strip on the cutout side, most visibly in the landscape
        // player; Flutter keeps content out of it through its safe area.
        if (Build.VERSION.SDK_INT >= 28) {
            window.attributes = window.attributes.apply {
                layoutInDisplayCutoutMode = WindowManager.LayoutParams.LAYOUT_IN_DISPLAY_CUTOUT_MODE_SHORT_EDGES
            }
        }
        super.onCreate(savedInstanceState)
    }
    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        remoteUri(intent)?.let { uri ->
            pendingRemote = uri
            deviceChannel?.invokeMethod("remote", uri)
            intent.data = null
        }
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
        // Torrent sources keep the episode being played here; Go's default
        // temp directory is not writable for apps.
        core.setCacheDir(cacheDir.absolutePath)
        appUpdates = AppUpdates(this, flutterEngine.dartExecutor.binaryMessenger)
        pendingLogin = pendingLogin ?: loginUri(intent)
        pendingRemote = pendingRemote ?: remoteUri(intent)
        intent.data = null
        deviceChannel = MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "otakase/device")
        deviceChannel!!.setMethodCallHandler { call, result ->
            when (call.method) {
                "initialLogin" -> { result.success(pendingLogin); pendingLogin = null }
                "initialRemote" -> { result.success(pendingRemote); pendingRemote = null }
                "localDiscovery" -> try {
                    if (call.arguments == true && multicastLock == null) {
                        val wifi = applicationContext.getSystemService(Context.WIFI_SERVICE) as WifiManager
                        multicastLock = wifi.createMulticastLock("otakase-discovery").apply { setReferenceCounted(false); acquire() }
                    } else if (call.arguments != true) {
                        multicastLock?.let { if (it.isHeld) it.release() }
                        multicastLock = null
                    }
                    result.success(if (call.arguments == true) localNetwork() else null)
                } catch (_: Exception) { result.error("unavailable", "Local discovery unavailable", null) }
                "episodeAlerts" -> {
                    val on = call.argument<Boolean>("on") == true
                    @Suppress("UNCHECKED_CAST")
                    val shows = call.argument<List<Map<String, Any?>>>("shows") ?: emptyList()
                    if (on && Build.VERSION.SDK_INT >= 33 &&
                        checkSelfPermission(android.Manifest.permission.POST_NOTIFICATIONS) != android.content.pm.PackageManager.PERMISSION_GRANTED) {
                        requestPermissions(arrayOf(android.Manifest.permission.POST_NOTIFICATIONS), 47614)
                    }
                    EpisodeAlerts.update(applicationContext, on, shows)
                    result.success(null)
                }
                "status" -> result.success(deviceStatus())
                "deviceName" -> result.success(deviceName())
                "keepAwake" -> {
                    keepAwake(call.argument<Boolean>("screen") == true, call.argument<String>("work"))
                    result.success(null)
                }
                "launchUri" -> try {
                    val uri = android.net.Uri.parse(call.arguments as String)
                    require(uri.scheme == "https" && uri.host in listOf("anilist.co", "myanimelist.net", "otakase.com", "account.otakase.com", "otakase.xyverion.com", "bot.xyverion.com", "discord.com", "discord.gg", "trakt.tv", "auth.trakt.tv"))
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
                // Downloads for watching offline. The app's own folder needs
                // no storage permission and is removed with the app.
                "downloadsDir" -> result.success(
                    (getExternalFilesDir(android.os.Environment.DIRECTORY_MOVIES) ?: java.io.File(filesDir, "downloads")).absolutePath
                )
                "metered" -> result.success(
                    (getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager)
                        ?.isActiveNetworkMetered ?: false
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

    /// [screen] keeps the display on while the app is in front. Its own view
    /// flag, so the player's keep-awake while a video plays stays separate.
    /// [work] names background work (downloads) that must go on after the
    /// screen turns off; null when there is none.
    private fun keepAwake(screen: Boolean, work: String?) {
        window.decorView.keepScreenOn = screen
        if (work == workText) return
        try {
            if (work == null) WorkService.stop(this) else WorkService.start(this, work)
            workText = work
        } catch (_: Exception) {
            // Android blocks starting it from the background; the next
            // change while the app is open tries again.
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
        appUpdates?.close()
        appUpdates = null
        multicastLock?.let { if (it.isHeld) it.release() }
        multicastLock = null
        // Downloads run in the core, which closes with the app.
        if (workText != null) WorkService.stop(this)
        workText = null
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
