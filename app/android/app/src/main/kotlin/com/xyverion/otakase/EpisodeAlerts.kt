package com.xyverion.otakase

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.job.JobInfo
import android.app.job.JobParameters
import android.app.job.JobScheduler
import android.app.job.JobService
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.os.Build
import org.json.JSONArray
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL

/// New episode alerts. The app hands over the shows being watched that have
/// an episode coming; a few times a day Android runs this job, which asks
/// AniList's public API which of those episodes came out. No sign-in, no
/// push service: the list never leaves the phone except as AniList ids.
class EpisodeAlerts : JobService() {
    override fun onStartJob(params: JobParameters?): Boolean {
        Thread {
            try {
                check(this)
            } catch (_: Exception) {
                // Tried again at the next run.
            }
            jobFinished(params, false)
        }.start()
        return true
    }

    override fun onStopJob(params: JobParameters?): Boolean = true

    companion object {
        private const val jobId = 47614
        private const val prefsName = "otakase_episode_alerts"
        private const val channelId = "episodes"
        private const val query =
            "query(\$ids:[Int]){Page(perPage:50){media(id_in:\$ids){id status episodes nextAiringEpisode{episode}}}}"

        /// Saves the shows to follow and starts or stops the checks.
        fun update(context: Context, on: Boolean, shows: List<Map<String, Any?>>) {
            val list = JSONArray()
            for (s in shows) {
                val id = (s["id"] as? Number)?.toInt() ?: continue
                list.put(
                    JSONObject()
                        .put("id", id)
                        .put("title", s["title"] as? String ?: "")
                        .put("episode", (s["episode"] as? Number)?.toInt() ?: 0)
                )
            }
            val prefs = context.getSharedPreferences(prefsName, Context.MODE_PRIVATE)
            // Keep what was already counted as out, so an episode isn't
            // announced twice when the app hands over an older number.
            val known = known(prefs.getString("shows", null))
            for (i in 0 until list.length()) {
                val s = list.getJSONObject(i)
                val before = known[s.getInt("id")] ?: continue
                if (before > s.getInt("episode")) s.put("episode", before)
            }
            prefs.edit().putString("shows", list.toString()).putBoolean("on", on).apply()
            val jobs = context.getSystemService(JobScheduler::class.java) ?: return
            if (!on || list.length() == 0) {
                jobs.cancel(jobId)
                return
            }
            if (jobs.getPendingJob(jobId) != null) return
            jobs.schedule(
                JobInfo.Builder(jobId, ComponentName(context, EpisodeAlerts::class.java))
                    .setPeriodic(3 * 60 * 60 * 1000L)
                    .setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY)
                    .setPersisted(true)
                    .build()
            )
        }

        private fun known(text: String?): Map<Int, Int> {
            if (text == null) return emptyMap()
            return try {
                val list = JSONArray(text)
                (0 until list.length()).associate {
                    val s = list.getJSONObject(it)
                    s.getInt("id") to s.getInt("episode")
                }
            } catch (_: Exception) {
                emptyMap()
            }
        }

        private fun check(context: Context) {
            val prefs = context.getSharedPreferences(prefsName, Context.MODE_PRIVATE)
            if (!prefs.getBoolean("on", false)) return
            val shows = JSONArray(prefs.getString("shows", "[]"))
            if (shows.length() == 0) return
            val ids = JSONArray()
            for (i in 0 until shows.length()) ids.put(shows.getJSONObject(i).getInt("id"))
            val connection = URL("https://graphql.anilist.co").openConnection() as HttpURLConnection
            connection.connectTimeout = 15000
            connection.readTimeout = 15000
            connection.requestMethod = "POST"
            connection.doOutput = true
            connection.setRequestProperty("Content-Type", "application/json")
            connection.setRequestProperty("Accept", "application/json")
            connection.outputStream.use {
                it.write(JSONObject().put("query", query).put("variables", JSONObject().put("ids", ids)).toString().toByteArray())
            }
            if (connection.responseCode != 200) return
            val body = connection.inputStream.bufferedReader().use { it.readText() }
            val media = JSONObject(body).getJSONObject("data").getJSONObject("Page").getJSONArray("media")
            val aired = mutableMapOf<Int, Int>()
            for (i in 0 until media.length()) {
                val m = media.getJSONObject(i)
                val next = m.optJSONObject("nextAiringEpisode")
                aired[m.getInt("id")] = when {
                    next != null -> next.getInt("episode") - 1
                    m.optString("status") == "FINISHED" -> m.optInt("episodes", 0)
                    else -> 0
                }
            }
            var changed = false
            for (i in 0 until shows.length()) {
                val s = shows.getJSONObject(i)
                val episode = s.getInt("episode")
                val out = aired[s.getInt("id")] ?: continue
                if (episode <= 0 || out < episode) continue
                notify(context, s.getInt("id"), s.optString("title"), out)
                s.put("episode", out + 1)
                changed = true
            }
            if (changed) prefs.edit().putString("shows", shows.toString()).apply()
        }

        private fun notify(context: Context, id: Int, title: String, episode: Int) {
            val manager = context.getSystemService(NotificationManager::class.java) ?: return
            val builder = if (Build.VERSION.SDK_INT >= 26) {
                if (manager.getNotificationChannel(channelId) == null) {
                    manager.createNotificationChannel(
                        NotificationChannel(channelId, "New episodes", NotificationManager.IMPORTANCE_DEFAULT).apply {
                            description = "A new episode of a show you watch is out"
                        }
                    )
                }
                Notification.Builder(context, channelId)
            } else {
                @Suppress("DEPRECATION")
                Notification.Builder(context)
            }
            val open = PendingIntent.getActivity(
                context, id,
                Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
                PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
            )
            try {
                manager.notify(
                    "episode", id,
                    builder
                        .setSmallIcon(R.drawable.ic_launcher_monochrome)
                        .setContentTitle(title.ifEmpty { "New episode" })
                        .setContentText("Episode $episode is out")
                        .setContentIntent(open)
                        .setAutoCancel(true)
                        .build()
                )
            } catch (_: SecurityException) {
                // Notifications are not allowed.
            }
        }
    }
}
