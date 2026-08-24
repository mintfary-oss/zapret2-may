package com.freenet.vpn

import android.app.Application
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.net.VpnService
import androidx.lifecycle.AndroidViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * VpnViewModel holds the UI state for the main screen and provides helpers
 * for starting / stopping the VPN service.
 *
 * Survives configuration changes (screen rotation).  The [ConnectionState]
 * is kept in sync with the [FreenetVpnService] via a BroadcastReceiver.
 */
class VpnViewModel(app: Application) : AndroidViewModel(app) {

    /** High-level VPN connection states shown in the UI. */
    enum class ConnectionState {
        DISCONNECTED, CONNECTING, CONNECTED
    }

    private val _connectionState = MutableStateFlow(
        if (FreenetVpnService.isRunning.get()) ConnectionState.CONNECTED
        else ConnectionState.DISCONNECTED
    )
    /** Observable connection state — collect in Compose with [StateFlow.collectAsState]. */
    val connectionState: StateFlow<ConnectionState> = _connectionState

    private val _strategy = MutableStateFlow("auto")
    /** Currently selected bypass strategy. */
    val strategy: StateFlow<String> = _strategy

    private val _stats = MutableStateFlow("")
    /** JSON stats string from the Go engine (refreshed periodically). */
    val stats: StateFlow<String> = _stats

    /** Available strategy options shown in the strategy picker. */
    val strategies = listOf("auto", "split", "tlsrec", "combined", "fake", "none")

    // -------------------------------------------------------------------------
    // Split-tunnel (per-app VPN filtering) state
    // -------------------------------------------------------------------------

    private val _splitTunnel = MutableStateFlow(
        SplitTunnelConfig.load(app)
    )
    /**
     * Observable split-tunnel configuration.
     * Collect in Compose with [StateFlow.collectAsState].
     */
    val splitTunnel: StateFlow<SplitTunnelConfig> = _splitTunnel.asStateFlow()

    // -------------------------------------------------------------------------
    // BroadcastReceiver — listens for service stop broadcasts.
    // -------------------------------------------------------------------------

    private val stopReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            if (intent.action == FreenetVpnService.ACTION_STOP) {
                _connectionState.value = ConnectionState.DISCONNECTED
            }
        }
    }

    init {
        val filter = IntentFilter(FreenetVpnService.ACTION_STOP)
        app.registerReceiver(stopReceiver, filter, Context.RECEIVER_NOT_EXPORTED)
    }

    override fun onCleared() {
        getApplication<Application>().unregisterReceiver(stopReceiver)
        super.onCleared()
    }

    // -------------------------------------------------------------------------
    // VPN control
    // -------------------------------------------------------------------------

    /**
     * Returns the VPN permission intent if the user hasn't granted permission
     * yet, or null if permission is already granted (service can start directly).
     *
     * Usage in an Activity:
     *
     * ```kotlin
     * val permIntent = viewModel.prepareVpn(this)
     * if (permIntent != null) {
     *     vpnPermissionLauncher.launch(permIntent)
     * } else {
     *     viewModel.startVpn(this)
     * }
     * ```
     */
    fun prepareVpn(context: Context): Intent? =
        VpnService.prepare(context)

    /** Starts the VPN service after permission has been granted. */
    fun startVpn(context: Context) {
        _connectionState.value = ConnectionState.CONNECTING
        val intent = Intent(context, FreenetVpnService::class.java)
            .setAction(FreenetVpnService.ACTION_START)
        context.startForegroundService(intent)
        // Optimistically show CONNECTED; the service broadcasts STOP on failure.
        _connectionState.value = ConnectionState.CONNECTED
    }

    /** Stops the VPN service. */
    fun stopVpn(context: Context) {
        _connectionState.value = ConnectionState.DISCONNECTED
        val intent = Intent(context, FreenetVpnService::class.java)
            .setAction(FreenetVpnService.ACTION_STOP)
        context.startService(intent)
    }

    /** Toggles the VPN on / off. */
    fun toggle(context: Context): Intent? {
        return when (_connectionState.value) {
            ConnectionState.DISCONNECTED -> {
                val prep = prepareVpn(context)
                if (prep == null) {
                    startVpn(context)
                }
                prep // caller launches this intent if non-null
            }
            ConnectionState.CONNECTING,
            ConnectionState.CONNECTED -> {
                stopVpn(context)
                null
            }
        }
    }

    /** Changes the bypass strategy. */
    fun setStrategy(s: String) {
        _strategy.value = s
        // Strategy change is applied to the running engine via the service.
        // When the Go AAR is present, the engine reloads the strategy at runtime.
    }

    /** Called by MainActivity when fresh stats JSON arrives from the service. */
    fun updateStats(json: String) {
        _stats.value = json
    }

    // -------------------------------------------------------------------------
    // Split-tunnel helpers
    // -------------------------------------------------------------------------

    /**
     * Sets the split-tunnel mode ("disabled", "allowlist", or "blocklist") and
     * persists the change.  The new config takes effect on the next VPN start.
     */
    fun setSplitTunnelMode(mode: String) {
        val ctx: Context = getApplication()
        val updated = _splitTunnel.value.copy(mode = mode)
        SplitTunnelConfig.save(ctx, updated)
        _splitTunnel.value = updated
    }

    /**
     * Toggles the presence of [pkg] in the split-tunnel app list and persists
     * the change.
     */
    fun toggleSplitTunnelApp(pkg: String) {
        val ctx: Context = getApplication()
        val current = _splitTunnel.value
        val newApps = if (pkg in current.apps) {
            current.apps - pkg
        } else {
            current.apps + pkg
        }
        val updated = current.copy(apps = newApps)
        SplitTunnelConfig.save(ctx, updated)
        _splitTunnel.value = updated
    }

    /** Returns true when [pkg] is currently in the split-tunnel app list. */
    fun isSplitTunnelApp(pkg: String): Boolean = pkg in _splitTunnel.value.apps
}
