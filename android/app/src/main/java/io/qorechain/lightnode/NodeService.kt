package io.qorechain.lightnode

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import android.util.Log

/**
 * Runs the light client in a foreground service.
 *
 * Foreground, not WorkManager, and the reason is a number. The chain marks a
 * node inactive once it goes heartbeat_interval + heartbeat_grace_period blocks
 * without a beat; at the October 2026 cadence of ~3.2 s/block that is about 59
 * minutes. Doze defers deferrable work on a stationary unplugged phone for
 * longer than that, so a periodic job cannot be relied on to meet it. This
 * build does not heartbeat (see Node.cannotEarnReason), but a node that is
 * supposed to be verifying the chain should not silently stop doing so either,
 * and if heartbeat signing is ever added it needs this shape already.
 */
class NodeService : Service() {

    private var worker: Thread? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        startInForeground()
        if (worker == null) {
            val node = NodeHolder.open(filesDir.absolutePath)
            worker = Thread({
                try {
                    node.start() // blocks until stop()
                } catch (e: Exception) {
                    Log.e(TAG, "light client stopped", e)
                }
            }, "qore-lightnode").apply { isDaemon = true; start() }
        }
        return START_STICKY
    }

    override fun onDestroy() {
        NodeHolder.close()
        worker = null
        super.onDestroy()
    }

    private fun startInForeground() {
        val nm = getSystemService(NotificationManager::class.java)
        if (nm.getNotificationChannel(CHANNEL) == null) {
            nm.createNotificationChannel(
                NotificationChannel(CHANNEL, getString(R.string.notif_channel), NotificationManager.IMPORTANCE_LOW)
            )
        }
        val n: Notification = Notification.Builder(this, CHANNEL)
            .setContentTitle(getString(R.string.app_name))
            .setContentText(getString(R.string.notif_running))
            .setSmallIcon(android.R.drawable.stat_notify_sync)
            .setOngoing(true)
            .build()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            startForeground(NOTIF_ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC)
        } else {
            startForeground(NOTIF_ID, n)
        }
    }

    companion object {
        private const val TAG = "QoreLightNode"
        private const val CHANNEL = "lightnode"
        private const val NOTIF_ID = 1
    }
}
