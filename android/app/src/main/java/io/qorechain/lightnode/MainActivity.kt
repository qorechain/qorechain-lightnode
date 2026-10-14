package io.qorechain.lightnode

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.widget.Button
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import org.json.JSONObject
import java.util.concurrent.Executors

/**
 * Status screen. Everything shown comes from the Go node as JSON; this side
 * only formats it, so the meaning of each number stays defined in one place.
 */
class MainActivity : AppCompatActivity() {

    private val io = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())
    private lateinit var status: TextView
    private lateinit var rewardPool: TextView
    private lateinit var heartbeat: TextView
    private lateinit var toggle: Button

    private val refresh = object : Runnable {
        override fun run() {
            refreshOnce()
            main.postDelayed(this, 15_000)
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)
        status = findViewById(R.id.status)
        rewardPool = findViewById(R.id.rewardPool)
        heartbeat = findViewById(R.id.heartbeat)
        toggle = findViewById(R.id.toggle)

        title = "${getString(R.string.app_name)} ${NodeHolder.version()}"
        val node = NodeHolder.open(filesDir.absolutePath)
        findViewById<TextView>(R.id.earnNotice).text =
            if (node.canEarn()) "This node can register and earn." else node.cannotEarnReason()

        toggle.setOnClickListener { toggleService() }
        renderToggle()
    }

    override fun onResume() {
        super.onResume()
        main.post(refresh)
    }

    override fun onPause() {
        main.removeCallbacks(refresh)
        super.onPause()
    }

    private fun toggleService() {
        val intent = Intent(this, NodeService::class.java)
        if (running) {
            stopService(intent)
            running = false
        } else {
            if (Build.VERSION.SDK_INT >= 33 &&
                ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS)
                != PackageManager.PERMISSION_GRANTED
            ) {
                requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), 1)
            }
            ContextCompat.startForegroundService(this, intent)
            running = true
        }
        renderToggle()
    }

    private fun renderToggle() {
        toggle.setText(if (running) R.string.stop else R.string.start)
    }

    private fun refreshOnce() {
        val node = NodeHolder.node ?: return
        io.execute {
            // Network calls happen in Go; keep them off the main thread.
            val s = node.statusJSON()
            val p = node.rewardPoolJSON()
            val h = node.heartbeatWindowJSON()
            main.post {
                status.text = formatStatus(s)
                rewardPool.text = formatPool(p)
                heartbeat.text = formatHeartbeat(h)
            }
        }
    }

    private fun formatStatus(json: String): String {
        val o = JSONObject(json)
        val sb = StringBuilder("CHAIN\n")
        sb.append("chain      ").append(o.optString("chain_id", "?")).append('\n')
        sb.append("height     ").append(o.optLong("latest_height")).append('\n')
        // A header seen from one endpoint is one endpoint's word, not a
        // verification; say which it is.
        sb.append("assurance  ").append(o.optString("assurance", "?"))
            .append(" (").append(o.optInt("witness_count")).append(" source(s))\n")
        o.optString("last_conflict").takeIf { it.isNotEmpty() }?.let {
            sb.append("CONFLICT   ").append(it).append('\n')
        }
        o.optString("chain_error").takeIf { it.isNotEmpty() }?.let {
            sb.append("error      ").append(it).append('\n')
        }
        return sb.toString()
    }

    private fun formatPool(json: String): String {
        val o = JSONObject(json)
        if (o.has("error")) return "REWARD POOL\nunavailable: ${o.getString("error")}"
        val sb = StringBuilder("REWARD POOL\n")
        sb.append("reserved       ").append(qor(o.optString("reserved"))).append(" QOR (nobody earned it)\n")
        sb.append("distributable  ").append(qor(o.optString("distributable"))).append(" QOR\n")
        if (o.has("max_reward_per_month_per_node")) {
            sb.append("ceiling/node   ").append(qor(o.getString("max_reward_per_month_per_node"))).append(" QOR/month")
            if (o.optBoolean("monthly_ceiling_from_chain")) {
                sb.append(" (stated by the chain)")
            } else {
                // Derived: the chain states it per block, so the monthly value
                // moves with block time. Show the inputs, not just the result.
                sb.append("\n               from ").append(o.optString("max_reward_per_block_per_node"))
                    .append(" uqor/block at ").append("%.3f".format(o.optDouble("block_time_seconds")))
                    .append(" s/block")
            }
        } else {
            sb.append("ceiling/node   ").append(o.optString("max_reward_per_block_per_node")).append(" uqor/block")
        }
        return sb.toString()
    }

    private fun formatHeartbeat(json: String): String {
        val o = JSONObject(json)
        if (o.has("error")) return "HEARTBEAT\nunavailable: ${o.getString("error")}"
        val sb = StringBuilder("HEARTBEAT (what a registered node must meet)\n")
        sb.append("every     ").append(o.optLong("interval_blocks")).append(" blocks")
        if (o.has("interval_seconds")) sb.append(" ≈ ").append(o.getLong("interval_seconds") / 60).append(" min")
        sb.append("\ndeadline  ").append(o.optLong("deadline_blocks")).append(" blocks")
        if (o.has("deadline_seconds")) sb.append(" ≈ ").append(o.getLong("deadline_seconds") / 60).append(" min")
        sb.append("\nmin uptime ").append(
            o.optString("min_uptime_for_rewards").toBigDecimalOrNull()?.stripTrailingZeros()?.toPlainString() ?: "?"
        )
        return sb.toString()
    }

    private fun qor(uqor: String): String =
        uqor.toBigDecimalOrNull()?.movePointLeft(6)?.toPlainString() ?: "?"

    companion object {
        // Survives activity recreation; the service itself is the source of truth.
        @Volatile private var running = false
    }
}
