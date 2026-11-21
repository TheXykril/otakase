@file:androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)

package com.xyverion.otakase.car

import android.app.Presentation
import android.content.Context
import android.content.Intent
import android.content.pm.ApplicationInfo
import android.graphics.Color
import android.hardware.display.DisplayManager
import android.hardware.display.VirtualDisplay
import android.os.Bundle
import android.util.TypedValue
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.widget.FrameLayout
import android.widget.LinearLayout
import android.widget.TextView
import androidx.car.app.AppManager
import androidx.car.app.CarAppService
import androidx.car.app.CarContext
import androidx.car.app.Screen
import androidx.car.app.Session
import androidx.car.app.SessionInfo
import androidx.car.app.SurfaceCallback
import androidx.car.app.SurfaceContainer
import androidx.car.app.hardware.CarHardwareManager
import androidx.car.app.hardware.common.CarValue
import androidx.car.app.hardware.common.OnCarDataAvailableListener
import androidx.car.app.hardware.info.Speed
import androidx.car.app.model.Action
import androidx.car.app.model.ActionStrip
import androidx.car.app.model.CarIcon
import androidx.car.app.model.ItemList
import androidx.car.app.model.ListTemplate
import androidx.car.app.model.Row
import androidx.car.app.model.Template
import androidx.car.app.navigation.model.NavigationTemplate
import androidx.car.app.validation.HostValidator
import androidx.core.content.ContextCompat
import androidx.core.graphics.drawable.IconCompat
import androidx.lifecycle.DefaultLifecycleObserver
import androidx.lifecycle.LifecycleOwner
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.ui.AspectRatioFrameLayout
import androidx.media3.ui.PlayerView
import com.xyverion.otakase.R

/**
 * Otakase on the Android Auto screen.
 *
 * Android Auto gives a drawing surface only to navigation apps, so the
 * service says it is one. That needs Android Auto's developer setting
 * "Unknown sources" for an app from outside the Play Store, and nothing
 * else: no helper app and no root. The picture is a video player drawn
 * into that surface; the car's speed pauses it whenever the car moves.
 */
class CarScreenService : CarAppService() {
    override fun createHostValidator(): HostValidator =
        // Android Auto from the Play Store, plus any host for a debug build.
        if (applicationInfo.flags and ApplicationInfo.FLAG_DEBUGGABLE != 0) {
            HostValidator.ALLOW_ALL_HOSTS_VALIDATOR
        } else {
            HostValidator.Builder(applicationContext)
                .addAllowedHosts(androidx.car.app.R.array.hosts_allowlist_sample)
                .build()
        }

    override fun onCreateSession(sessionInfo: SessionInfo): Session {
        CarPlayer.knowContext(this)
        return CarSession()
    }
}

private class CarSession : Session() {
    override fun onCreateScreen(intent: Intent): Screen {
        val screen = VideoScreen(carContext)
        lifecycle.addObserver(object : DefaultLifecycleObserver {
            override fun onCreate(owner: LifecycleOwner) = CarPlayer.carOpened()
            override fun onDestroy(owner: LifecycleOwner) = CarPlayer.carClosed()
        })
        watchSpeed()
        return screen
    }

    /** Pauses video while the car moves. Cars that share no speed play on. */
    private fun watchSpeed() {
        if (carContext.carAppApiLevel < 3) return
        val permission = "com.google.android.gms.permission.CAR_SPEED"
        val listen = {
            try {
                val info = carContext.getCarService(CarHardwareManager::class.java).carInfo
                val listener = OnCarDataAvailableListener<Speed> { speed ->
                    val raw = speed.rawSpeedMetersPerSecond
                    val shown = speed.displaySpeedMetersPerSecond
                    val value = when {
                        raw.status == CarValue.STATUS_SUCCESS -> raw.value
                        shown.status == CarValue.STATUS_SUCCESS -> shown.value
                        else -> null
                    }
                    value?.let { CarPlayer.speed(kotlin.math.abs(it)) }
                }
                val executor = ContextCompat.getMainExecutor(carContext)
                info.addSpeedListener(executor, listener)
                lifecycle.addObserver(object : DefaultLifecycleObserver {
                    override fun onDestroy(owner: LifecycleOwner) {
                        try { info.removeSpeedListener(listener) } catch (_: Exception) {}
                    }
                })
            } catch (_: Exception) {
                // No speed from this car.
            }
        }
        if (carContext.checkSelfPermission(permission) == android.content.pm.PackageManager.PERMISSION_GRANTED) {
            listen()
        } else {
            try {
                carContext.requestPermissions(listOf(permission)) { granted, _ ->
                    if (permission in granted) listen()
                }
            } catch (_: Exception) {}
        }
    }
}

/** The video, with play, skip and the Continue watching list as buttons. */
private class VideoScreen(context: CarContext) : Screen(context), SurfaceCallback {
    private var display: VirtualDisplay? = null
    private var shown: CarPicture? = null
    private val redraw: () -> Unit = {
        shown?.update()
        invalidate()
    }

    init {
        context.getCarService(AppManager::class.java).setSurfaceCallback(this)
        CarPlayer.watch(redraw)
        lifecycle.addObserver(object : DefaultLifecycleObserver {
            override fun onDestroy(owner: LifecycleOwner) {
                CarPlayer.unwatch(redraw)
                dropSurface()
            }
        })
    }

    override fun onSurfaceAvailable(container: SurfaceContainer) {
        val surface = container.surface ?: return
        dropSurface()
        val manager = carContext.getSystemService(Context.DISPLAY_SERVICE) as DisplayManager
        val vd = manager.createVirtualDisplay(
            "Otakase car screen", container.width, container.height, container.dpi, surface,
            DisplayManager.VIRTUAL_DISPLAY_FLAG_OWN_CONTENT_ONLY,
        )
        display = vd
        try {
            val picture = CarPicture(carContext, vd.display)
            picture.show()
            shown = picture
            CarPlayer.showOn { picture.attach(it) }
        } catch (_: Exception) {
            // Without a window on the car, the video goes straight onto the
            // surface: stretched, but playing.
            vd.release()
            display = null
            CarPlayer.showOn { it?.setVideoSurface(surface) }
        }
    }

    override fun onSurfaceDestroyed(container: SurfaceContainer) = dropSurface()

    private fun dropSurface() {
        CarPlayer.showOn(null)
        shown?.let { try { it.dismiss() } catch (_: Exception) {} }
        shown = null
        display?.release()
        display = null
    }

    override fun onClick(x: Float, y: Float) = CarPlayer.togglePause()

    private fun icon(id: Int) = CarIcon.Builder(IconCompat.createWithResource(carContext, id)).build()

    override fun onGetTemplate(): Template {
        val strip = ActionStrip.Builder()
        if (CarPlayer.hasEpisode) {
            strip.addAction(
                Action.Builder()
                    .setIcon(icon(if (CarPlayer.playing) R.drawable.car_pause else R.drawable.car_play))
                    .setOnClickListener { CarPlayer.togglePause() }
                    .build()
            )
            strip.addAction(
                Action.Builder().setIcon(icon(R.drawable.car_back))
                    .setOnClickListener { CarPlayer.skip(-10) }.build()
            )
            strip.addAction(
                Action.Builder().setIcon(icon(R.drawable.car_forward))
                    .setOnClickListener { CarPlayer.skip(30) }.build()
            )
        }
        strip.addAction(
            Action.Builder().setIcon(icon(R.drawable.car_list))
                .setOnClickListener { screenManager.push(ShelfScreen(carContext)) }
                .build()
        )
        return NavigationTemplate.Builder()
            .setActionStrip(strip.build())
            .build()
    }
}

/** Continue watching from the phone; a pick plays on the car screen. */
private class ShelfScreen(context: CarContext) : Screen(context) {
    private var asked: String? = null
    private val redraw: () -> Unit = { invalidate() }

    init {
        CarPlayer.watch(redraw)
        lifecycle.addObserver(object : DefaultLifecycleObserver {
            override fun onDestroy(owner: LifecycleOwner) = CarPlayer.unwatch(redraw)
        })
    }

    override fun onGetTemplate(): Template {
        val list = ItemList.Builder()
        val limit = try {
            carContext.getCarService(androidx.car.app.constraints.ConstraintManager::class.java)
                .getContentLimit(androidx.car.app.constraints.ConstraintManager.CONTENT_LIMIT_TYPE_LIST)
        } catch (_: Exception) { 6 }
        for (item in CarPlayer.shelf.take(limit)) {
            val row = Row.Builder().setTitle(item.title.ifBlank { "Episode" })
            if (item.detail.isNotBlank()) row.addText(item.detail)
            row.setOnClickListener {
                asked = item.title
                if (CarPlayer.pick(item.key)) {
                    screenManager.pop()
                } else {
                    asked = null
                    invalidate()
                }
            }
            list.addItem(row.build())
        }
        list.setNoItemsMessage(
            if (CarPlayer.shelf.isEmpty()) "Open Otakase on your phone once to fill this list."
            else "Nothing to resume."
        )
        @Suppress("DEPRECATION")
        return ListTemplate.Builder()
            .setTitle("Continue watching")
            .setHeaderAction(Action.BACK)
            .setSingleList(list.build())
            .build()
    }
}

/** The window drawn on the car's surface: the video, or a short note. */
private class CarPicture(context: Context, display: android.view.Display) :
    Presentation(context, display, android.R.style.Theme_Black_NoTitleBar_Fullscreen) {
    private lateinit var video: PlayerView
    private lateinit var message: TextView
    private lateinit var detail: TextView
    private lateinit var notes: LinearLayout

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val root = FrameLayout(context).apply { setBackgroundColor(Color.BLACK) }
        video = PlayerView(context).apply {
            useController = false
            resizeMode = AspectRatioFrameLayout.RESIZE_MODE_FIT
            setShutterBackgroundColor(Color.BLACK)
            setKeepContentOnPlayerReset(true)
        }
        root.addView(video, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
        fun text(size: Float, color: Int) = TextView(context).apply {
            setTextColor(color)
            setTextSize(TypedValue.COMPLEX_UNIT_SP, size)
            gravity = Gravity.CENTER
        }
        message = text(26f, Color.WHITE)
        detail = text(16f, 0xFFB8B8B8.toInt())
        notes = LinearLayout(context).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER
            setPadding(64, 32, 64, 32)
            setBackgroundColor(0xCC000000.toInt())
            addView(message)
            addView(detail)
        }
        root.addView(notes, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
        setContentView(root)
        update()
    }

    fun attach(player: ExoPlayer?) {
        if (::video.isInitialized) video.player = player
        update()
    }

    fun update() {
        if (!::message.isInitialized) return
        val (title, more) = when {
            CarPlayer.driving && CarPlayer.hasEpisode -> "Paused while driving" to
                "Video plays only while the car is parked."
            CarPlayer.failure.isNotEmpty() -> "Could not play" to CarPlayer.failure
            !CarPlayer.hasEpisode -> "Otakase" to
                "Pick an episode with the list button, or play one on your phone and choose Car screen from the cast button."
            else -> "" to ""
        }
        message.text = title
        detail.text = more
        notes.visibility = if (title.isEmpty()) View.GONE else View.VISIBLE
    }
}
