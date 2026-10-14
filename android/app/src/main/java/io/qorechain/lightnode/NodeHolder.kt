package io.qorechain.lightnode

import mobile.Mobile
import mobile.Node

/**
 * The one light node this process runs.
 *
 * The node lives in the foreground service and the screen only reads it, so
 * both sides need the same instance. A process-wide holder is the simplest
 * thing that gives them that; the Go side is safe to call from any thread.
 */
object NodeHolder {
    // Mainnet defaults. The REST gateway is a separate host from the RPC here,
    // and the Go client takes both.
    const val RPC = "https://rpc.qore.host"
    const val API = "https://api.qore.host"

    @Volatile
    var node: Node? = null
        private set

    @Synchronized
    fun open(dataDir: String): Node =
        node ?: Mobile.newNode(dataDir, RPC, API, "").also { node = it }

    @Synchronized
    fun close() {
        node?.stop()
        node = null
    }

    fun version(): String = Mobile.version()
}
