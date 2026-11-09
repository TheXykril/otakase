package com.xyverion.otakase

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.wifi.WifiManager
import android.os.Build
import android.os.IBinder
import android.os.PowerManager

/// Keeps the app running while it downloads an update or saves episodes, so
/// the work goes on after the screen turns off or the viewer switches apps.
/// Android shows its notification for as long as the work lasts.
class WorkService : Service() {
    private var cpu: PowerManager.WakeLock? = null
    private var wifi: WifiManager.WifiLock? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val text = intent?.getStringExtra(extraText) ?: "Downloading"
        try {
            val notification = notification(text)
            if (Build.VERSION.SDK_INT >= 29) {
                startForeground(notificationId, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC)
            } else {
                startForeground(notificationId, notification)
            }
        } catch (_: Exception) {
            // Android refused to run in the foreground; the work carries on
            // only while the app is open.
            stopSelf()
            return START_NOT_STICKY
        }
        hold()
        return START_NOT_STICKY
    }

    @Suppress("DEPRECATION")
    private fun hold() {
        if (cpu == null) {
            cpu = (getSystemService(Context.POWER_SERVICE) as PowerManager)
                .newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "otakase:work").apply {
                    setReferenceCounted(false)
                    // A safety net in case the app never says the work ended.
                    acquire(6 * 60 * 60 * 1000L)
                }
        }
        if (wifi == null) {
            wifi = try {
                (applicationContext.getSystemService(Context.WIFI_SERVICE) as WifiManager)
                    .createWifiLock(WifiManager.WIFI_MODE_FULL_HIGH_PERF, "otakase:work").apply {
                        setReferenceCounted(false)
                        acquire()
                    }
            } catch (_: Exception) { null }
        }
    }

    private fun notification(text: String): Notification {
        val open = PendingIntent.getActivity(
            this, 0,
            Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        val builder = if (Build.VERSION.SDK_INT >= 26) {
            val manager = getSystemService(NotificationManager::class.java)
            if (manager.getNotificationChannel(channelId) == null) {
                manager.createNotificationChannel(
                    NotificationChannel(channelId, "Downloads", NotificationManager.IMPORTANCE_LOW).apply {
                        description = "Shown while Otakase downloads in the background"
                        setShowBadge(false)
                    }
                )
            }
            Notification.Builder(this, channelId)
        } else {
            @Suppress("DEPRECATION")
            Notification.Builder(this)
        }
        return builder
            .setSmallIcon(R.drawable.ic_launcher_monochrome)
            .setContentTitle("Otakase")
            .setContentText(text)
            .setContentIntent(open)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .build()
    }

    override fun onDestroy() {
        cpu?.let { if (it.isHeld) it.release() }
        cpu = null
        wifi?.let { if (it.isHeld) it.release() }
        wifi = null
        super.onDestroy()
    }

    companion object {
        private const val channelId = "work"
        private const val notificationId = 4101
        private const val extraText = "text"

        fun start(context: Context, text: String) {
            val intent = Intent(context, WorkService::class.java).putExtra(extraText, text)
            if (Build.VERSION.SDK_INT >= 26) context.startForegroundService(intent)
            else context.startService(intent)
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, WorkService::class.java))
        }
    }
}
