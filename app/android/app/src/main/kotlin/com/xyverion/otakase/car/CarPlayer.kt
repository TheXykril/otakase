@file:androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)

package com.xyverion.otakase.car

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Handler
import android.os.Looper
import androidx.media3.common.AudioAttributes
import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.MimeTypes
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.datasource.DefaultDataSource
import androidx.media3.datasource.DefaultHttpDataSource
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import com.xyverion.otakase.MainActivity
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.MethodChannel

/**
 * Plays an episode on the Android Auto screen while the car is parked.
 *
 * The phone app casts to the car the way it casts to a TV: it sends the
 * stream it resolved, then polls [status] every second and drives playback
 * with [control]. The car screen ([CarScreenService]) shows the picture and
 * the buttons. Everything here runs on the main thread.
 */
object CarPlayer {
    /** One Continue watching card the phone offered to the car. */
    data class ShelfItem(val key: String, val title: String, val detail: String)

    private val main = Handler(Looper.getMainLooper())
    private var player: ExoPlayer? = null
    private var appContext: Context? = null
    private var channel: MethodChannel? = null

    /** The car screen is open in Android Auto and can show a picture. */
    var carOpen = false
        private set

    /** The car reports moving; video stays paused until it stops. */
    var driving = false
        private set

    /** The car reports its speed, so [driving] can be trusted. */
    var speedKnown = false
        private set

    var title = ""
        private set
    private var load = 0
    private var active = false
    private var error = ""
    private var note = ""
    private var durationHint = 0.0

    var shelf: List<ShelfItem> = emptyList()
        private set

    /** Car screen parts that redraw when anything here changes. */
    private val watchers = mutableSetOf<() -> Unit>()

    /** Where the picture goes; set by the car screen while it has one. */
    private var output: ((ExoPlayer?) -> Unit)? = null

    fun watch(f: () -> Unit) { watchers.add(f) }
    fun unwatch(f: () -> Unit) { watchers.remove(f) }
    private fun changed() = watchers.toList().forEach { it() }

    // ---- The phone app's side ----------------------------------------

    /** Connects the app's Flutter engine, which casts and fills the shelf. */
    fun attach(context: Context, messenger: BinaryMessenger) {
        appContext = context.applicationContext
        val ch = MethodChannel(messenger, "otakase/car")
        channel = ch
        ch.setMethodCallHandler { call, result ->
            when (call.method) {
                "connected" -> result.success(carOpen)
                "start" -> {
                    @Suppress("UNCHECKED_CAST")
                    val args = call.arguments as? Map<String, Any?> ?: emptyMap()
                    val refused = start(args)
                    if (refused == null) result.success(null)
                    else result.error("refused", refused, null)
                }
                "status" -> result.success(status())
                "control" -> {
                    val action = call.argument<String>("action") ?: ""
                    val value = call.argument<Number>("value")?.toDouble() ?: 0.0
                    val refused = control(action, value)
                    if (refused == null) result.success(null)
                    else result.error("refused", refused, null)
                }
                "shelf" -> {
                    @Suppress("UNCHECKED_CAST")
                    val items = call.arguments as? List<Map<String, Any?>> ?: emptyList()
                    shelf = items.mapNotNull {
                        val key = it["key"] as? String ?: return@mapNotNull null
                        ShelfItem(key, it["title"] as? String ?: "", it["detail"] as? String ?: "")
                    }
                    changed()
                    result.success(null)
                }
                else -> result.notImplemented()
            }
        }
        pendingPick?.let { pick(it) }
    }

    /** The app's engine is going away; casting to the car ends with it. */
    fun detach(messenger: BinaryMessenger) {
        channel?.setMethodCallHandler(null)
        channel = null
        stop()
    }

    private fun start(args: Map<String, Any?>): String? {
        if (!carOpen) return "Open Otakase on the car screen first."
        val url = args["url"] as? String
        if (url.isNullOrBlank()) return "Nothing to play."
        val context = appContext ?: return "Otakase is not ready."
        @Suppress("UNCHECKED_CAST")
        val headers = (args["headers"] as? Map<String, Any?>)
            ?.mapNotNull { (k, v) -> (v as? String)?.let { k to it } }?.toMap() ?: emptyMap()
        val hls = args["hls"] == true
        val subtitle = args["subtitle"] as? String ?: ""
        val start = (args["start"] as? Number)?.toDouble() ?: 0.0
        durationHint = (args["duration"] as? Number)?.toDouble() ?: 0.0
        title = args["title"] as? String ?: ""

        val http = DefaultHttpDataSource.Factory()
            .setAllowCrossProtocolRedirects(true)
            .setConnectTimeoutMs(20_000)
            .setReadTimeoutMs(30_000)
        headers.entries.firstOrNull { it.key.equals("User-Agent", true) }?.let { http.setUserAgent(it.value) }
        http.setDefaultRequestProperties(headers.filterKeys { !it.equals("User-Agent", true) })
        val sources = DefaultMediaSourceFactory(DefaultDataSource.Factory(context, http))

        val item = MediaItem.Builder().setUri(uriOf(url))
        if (hls || url.contains(".m3u8")) item.setMimeType(MimeTypes.APPLICATION_M3U8)
        if (subtitle.isNotBlank()) {
            item.setSubtitleConfigurations(listOf(
                MediaItem.SubtitleConfiguration.Builder(uriOf(subtitle))
                    .setMimeType(subtitleType(subtitle))
                    .setSelectionFlags(C.SELECTION_FLAG_DEFAULT)
                    .build()
            ))
        }

        release()
        val p = ExoPlayer.Builder(context)
            .setMediaSourceFactory(sources)
            .setAudioAttributes(
                AudioAttributes.Builder()
                    .setUsage(C.USAGE_MEDIA)
                    .setContentType(C.AUDIO_CONTENT_TYPE_MOVIE)
                    .build(),
                true,
            )
            .build()
        p.addListener(object : Player.Listener {
            override fun onPlayerError(e: PlaybackException) {
                error = "The car could not play this copy (${e.errorCodeName})."
                changed()
            }
            override fun onPlaybackStateChanged(state: Int) = changed()
            override fun onIsPlayingChanged(playing: Boolean) = changed()
        })
        player = p
        load++
        active = true
        error = ""
        note = ""
        p.setMediaItem(item.build(), (start * 1000).toLong().coerceAtLeast(0))
        p.prepare()
        p.playWhenReady = !driving
        if (driving) note = "Video plays only while the car is parked."
        output?.invoke(p)
        changed()
        return null
    }

    private fun uriOf(s: String): Uri =
        if (s.startsWith("/")) Uri.fromFile(java.io.File(s)) else Uri.parse(s)

    private fun subtitleType(s: String): String {
        val path = Uri.parse(s).path?.lowercase() ?: s.lowercase()
        return when {
            path.endsWith(".ass") || path.endsWith(".ssa") -> MimeTypes.TEXT_SSA
            path.endsWith(".srt") -> MimeTypes.APPLICATION_SUBRIP
            path.endsWith(".ttml") || path.endsWith(".dfxp") -> MimeTypes.APPLICATION_TTML
            else -> MimeTypes.TEXT_VTT
        }
    }

    /** What the phone's player shows about the car, as a cast status. */
    fun status(): Map<String, Any?> {
        val p = player
        val state = when {
            !active || p == null -> "idle"
            error.isNotEmpty() -> "error"
            p.playbackState == Player.STATE_ENDED -> "ended"
            p.playbackState == Player.STATE_BUFFERING && p.playWhenReady -> "buffering"
            p.playbackState == Player.STATE_IDLE -> "loading"
            p.playbackState == Player.STATE_BUFFERING && p.duration == C.TIME_UNSET -> "loading"
            p.playWhenReady -> "playing"
            else -> "paused"
        }
        val duration = p?.duration?.takeIf { it != C.TIME_UNSET && it > 0 }?.div(1000.0) ?: durationHint
        return mapOf(
            "active" to active,
            "device" to "Car screen",
            "kind" to "car",
            "state" to state,
            "position" to ((p?.currentPosition ?: 0L) / 1000.0),
            "duration" to duration,
            "volume" to (p?.volume?.toDouble() ?: 1.0),
            "paused" to (p?.playWhenReady != true),
            "load" to load,
            "note" to note,
            "error" to error,
        )
    }

    private fun control(action: String, value: Double): String? {
        val p = player ?: return "Nothing is playing on the car screen."
        when (action) {
            "stop" -> stop()
            "pause" -> p.pause()
            "play" -> {
                if (driving) return "Video plays only while the car is parked."
                p.play()
            }
            "seek" -> p.seekTo((value * 1000).toLong().coerceAtLeast(0))
            "volume" -> p.volume = value.toFloat().coerceIn(0f, 1f)
            else -> return "Unknown action."
        }
        changed()
        return null
    }

    private fun release() {
        output?.invoke(null)
        player?.release()
        player = null
    }

    /** Ends playback on the car; the phone picks up from where it was. */
    fun stop() {
        release()
        active = false
        title = ""
        note = ""
        error = ""
        changed()
    }

    // ---- The car screen's side ---------------------------------------

    fun carOpened() {
        carOpen = true
        changed()
    }

    /** Android Auto closed the screen or the phone was unplugged. */
    fun carClosed() {
        carOpen = false
        output = null
        player?.pause()
        // The phone keeps the episode, at the spot the car reached.
        main.post { if (!carOpen) stop() }
    }

    /** The car screen has a picture to show [player] in, or lost it. */
    fun showOn(target: ((ExoPlayer?) -> Unit)?) {
        output = target
        target?.invoke(player)
    }

    val playing get() = player?.playWhenReady == true && active
    val hasEpisode get() = active && player != null
    val failure get() = error

    fun togglePause() {
        val p = player ?: return
        if (p.playWhenReady) p.pause() else if (!driving) p.play()
        changed()
    }

    fun skip(seconds: Int) {
        val p = player ?: return
        val to = (p.currentPosition + seconds * 1000L).coerceAtLeast(0)
        p.seekTo(if (p.duration > 0) to.coerceAtMost(p.duration - 1000) else to)
        changed()
    }

    /** The car's speed: video pauses as soon as it moves. */
    fun speed(metersPerSecond: Float?) {
        if (metersPerSecond == null) return
        speedKnown = true
        val moving = if (driving) metersPerSecond > 0.3f else metersPerSecond > 1.0f
        if (moving == driving) return
        driving = moving
        if (moving) {
            player?.pause()
            if (active) note = "Video plays only while the car is parked."
        } else {
            note = ""
        }
        changed()
    }

    private var pendingPick: String? = null

    /**
     * The driver picked a Continue watching card on the car screen. The
     * phone app finds the episode and casts it here. When the app is closed
     * it is opened first, and gets the pick once it is ready.
     */
    fun pick(key: String): Boolean {
        val ch = channel
        if (ch == null) {
            pendingPick = key
            val context = appContext ?: return false
            return try {
                context.startActivity(Intent(context, MainActivity::class.java)
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                true
            } catch (_: Exception) {
                false
            }
        }
        pendingPick = null
        ch.invokeMethod("pick", key)
        return true
    }

    /** The car service knows the app context before the app opens. */
    fun knowContext(context: Context) {
        if (appContext == null) appContext = context.applicationContext
    }
}
