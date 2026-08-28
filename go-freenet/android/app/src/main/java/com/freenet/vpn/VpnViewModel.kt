package com.freenet.vpn

import android.app.Application
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.net.VpnService
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

/**
 * VpnViewModel holds the UI state for the main screen and provides helpers
 * for starting / stopping the VPN service.
 *
 * Survives configuration changes (screen rotation).  The [ConnectionState]
 * is kept in sync with the [FreenetVpnService] via a BroadcastReceiver and
 * the static [FreenetVpnService.instance] reference.
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
    /** JSON stats string from the Go engine (refreshed while connected). */
    val stats: StateFlow<String> = _stats

    private val _engineStatus = MutableStateFlow(FreenetVpnService.engineStatus)
    /**
     * Short diagnostic line: "Go engine v1.0.0 — активен ✓" or
     * "Kotlin fallback — только TCP, DNS не работает!"
     * Always visible so the user can see what mode the VPN is in.
     */
    val engineStatus: StateFlow<String> = _engineStatus.asStateFlow()

    private val _logs = MutableStateFlow("")
    /** Recent log lines from the Go engine (refreshed while connected). */
    val logs: StateFlow<String> = _logs.asStateFlow()

    /** Available strategy options shown in the strategy picker. */
    val strategies = listOf("auto", "split", "tlsrec", "combined", "fake", "none")

    // Background polling job — active only while connected.
    private var pollingJob: Job? = null

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
    // BroadcastReceiver — listens for service start/stop broadcasts.
    // -------------------------------------------------------------------------

    private val serviceReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            when (intent.action) {
                FreenetVpnService.ACTION_START -> {
                    _connectionState.value = ConnectionState.CONNECTED
                    startPolling()
                }
                FreenetVpnService.ACTION_STOP -> {
                    _connectionState.value = ConnectionState.DISCONNECTED
                    stopPolling()
                    _stats.value = ""
                    _logs.value = ""
                }
            }
        }
    }

    init {
        val filter = IntentFilter().apply {
            addAction(FreenetVpnService.ACTION_START)
            addAction(FreenetVpnService.ACTION_STOP)
        }
        app.registerReceiver(serviceReceiver, filter, Context.RECEIVER_NOT_EXPORTED)

        // If the service is already running when the ViewModel is created (e.g.
        // after a configuration change), start polling immediately.
        if (FreenetVpnService.isRunning.get()) startPolling()
    }

    override fun onCleared() {
        getApplication<Application>().unregisterReceiver(serviceReceiver)
        stopPolling()
        super.onCleared()
    }

    // -------------------------------------------------------------------------
    // Stats / log polling
    // -------------------------------------------------------------------------

    /**
     * Polls the running engine every 2 s for stats and log lines.
     * Runs on the IO dispatcher to avoid blocking the main thread.
     */
    private fun startPolling() {
        pollingJob?.cancel()
        pollingJob = viewModelScope.launch(Dispatchers.IO) {
            while (true) {
                // Always refresh engine status (visible even before full connect).
                _engineStatus.value = FreenetVpnService.engineStatus

                val svc = FreenetVpnService.instance
                if (svc != null) {
                    val statsJson = svc.getStats()
                    val logText   = svc.getRecentLogs(60)
                    // Post to main thread via StateFlow (thread-safe).
                    _stats.value = statsJson
                    _logs.value  = logText
                }
                delay(2_000)
            }
        }
    }

    private fun stopPolling() {
        pollingJob?.cancel()
        pollingJob = null
    }

    // -------------------------------------------------------------------------
    // VPN control
    // -------------------------------------------------------------------------

    /**
     * Returns the VPN permission intent if the user hasn't granted permission
     * yet, or null if permission is already granted (service can start directly).
     */
    fun prepareVpn(context: Context): Intent? =
        VpnService.prepare(context)

    /** Starts the VPN service after permission has been granted. */
    fun startVpn(context: Context) {
        _connectionState.value = ConnectionState.CONNECTING
        val intent = Intent(context, FreenetVpnService::class.java)
            .setAction(FreenetVpnService.ACTION_START)
        context.startForegroundService(intent)
        // Show CONNECTING state; the service broadcasts ACTION_START on success
        // and ACTION_STOP on failure — both update _connectionState above.
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

    /**
     * Changes the bypass strategy.
     * Persists the selection in the UI and propagates to the running Go engine
     * via the static [FreenetVpnService.instance] reference (no-op when not running).
     */
    fun setStrategy(s: String) {
        _strategy.value = s
        FreenetVpnService.instance?.applyStrategy(s)
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

    // -------------------------------------------------------------------------
    // DNS setup banner
    // -------------------------------------------------------------------------

    /** Key used to persist the "DNS banner dismissed" flag across restarts. */
    private val DNS_PREF_KEY = "dns_banner_dismissed"
    private val prefs = app.getSharedPreferences("freenet_prefs", Context.MODE_PRIVATE)

    private val _dnsBannerDismissed = MutableStateFlow(
        prefs.getBoolean(DNS_PREF_KEY, false)
    )
    /**
     * True when the user has tapped "Got it" on the DNS-setup reminder card.
     * Persisted in SharedPreferences so it stays dismissed across restarts.
     */
    val dnsBannerDismissed: StateFlow<Boolean> = _dnsBannerDismissed.asStateFlow()

    /** Called when the user taps the dismiss button on the DNS setup card. */
    fun dismissDnsBanner() {
        prefs.edit().putBoolean(DNS_PREF_KEY, true).apply()
        _dnsBannerDismissed.value = true
    }

    // -------------------------------------------------------------------------
    // First-launch permission setup
    // -------------------------------------------------------------------------

    /**
     * Key used to persist whether the user has completed the first-launch
     * permission setup screen.  Once dismissed it is never shown again.
     */
    private val SETUP_PREF_KEY = "setup_dismissed"

    private val _setupDismissed = MutableStateFlow(
        prefs.getBoolean(SETUP_PREF_KEY, false)
    )
    /**
     * True after the user taps "Готово" on the first-launch setup card.
     * Persisted in SharedPreferences so the card is shown only once.
     */
    val setupDismissed: StateFlow<Boolean> = _setupDismissed.asStateFlow()

    /** Called when the user completes / dismisses the first-launch setup card. */
    fun dismissSetup() {
        prefs.edit().putBoolean(SETUP_PREF_KEY, true).apply()
        _setupDismissed.value = true
    }

    // -------------------------------------------------------------------------
    // Diagnostic report
    // -------------------------------------------------------------------------

    /**
     * Assembles a plain-text diagnostic report from current engine state.
     * Pure string manipulation — safe to call on the main thread from a
     * button click handler.
     */
    fun buildReport(): String {
        val ts = java.text.SimpleDateFormat(
            "yyyy-MM-dd HH:mm:ss", java.util.Locale.getDefault()
        ).format(java.util.Date())

        val stateStr = when (_connectionState.value) {
            ConnectionState.CONNECTED    -> "ВКЛЮЧЕНО ✓"
            ConnectionState.CONNECTING   -> "ПОДКЛЮЧЕНИЕ..."
            ConnectionState.DISCONNECTED -> "ВЫКЛЮЧЕНО"
        }

        // Tiny JSON extractor — avoids adding a library dependency.
        fun stat(key: String): String =
            Regex(""""$key"\s*:\s*(\d+)""").find(_stats.value)
                ?.groupValues?.get(1) ?: "0"

        val bytesIn  = stat("bytes_in").toLongOrNull()  ?: 0L
        val bytesOut = stat("bytes_out").toLongOrNull() ?: 0L

        val logLines   = _logs.value.lines()
        val errorLines = logLines.filter { line ->
            line.lowercase().let { "error" in it || "fatal" in it || "fail" in it }
        }
        val warnLines = logLines.filter { "warn" in it.lowercase() }

        val sep = "─".repeat(52)

        return buildString {
            appendLine("╔══════════════════════════════════════════════════╗")
            appendLine("║       FreeNet — Диагностика Android              ║")
            appendLine("╚══════════════════════════════════════════════════╝")
            appendLine()
            appendLine("Сгенерирован:    $ts")
            appendLine("Движок:          ${_engineStatus.value}")
            appendLine()
            appendLine("СОСТОЯНИЕ")
            appendLine(sep)
            appendLine("Обход DPI:       $stateStr")
            appendLine("Стратегия:       ${_strategy.value}")
            appendLine()
            appendLine("СТАТИСТИКА СОЕДИНЕНИЙ")
            appendLine(sep)
            appendLine("Активных:        ${stat("active")}")
            appendLine("Всего:           ${stat("total")}")
            appendLine("Обойдено DPI:    ${stat("bypassed")}")
            appendLine("Без обхода:      ${stat("passthrough")}")
            appendLine("Принято:         ${fmtBytes(bytesIn)}")
            appendLine("Отправлено:      ${fmtBytes(bytesOut)}")
            appendLine()
            appendLine("ДИАГНОСТИКА")
            appendLine(sep)
            appendLine("Ошибок в логе:   ${errorLines.size}")
            appendLine("Предупреждений:  ${warnLines.size}")
            if (errorLines.isNotEmpty()) {
                appendLine()
                appendLine("ОШИБКИ:")
                errorLines.forEach { appendLine("  $it") }
            }
            if (warnLines.isNotEmpty()) {
                appendLine()
                appendLine("ПРЕДУПРЕЖДЕНИЯ:")
                warnLines.forEach { appendLine("  $it") }
            }
            appendLine()
            appendLine("ЖУРНАЛ (последние 100 строк)")
            appendLine(sep)
            logLines.takeLast(100).forEach { appendLine("  $it") }
            appendLine()
            appendLine("── конец отчёта ─────────────────────────────────")
        }
    }

    private fun fmtBytes(n: Long): String = when {
        n >= 1_073_741_824L -> "%.2f ГБ".format(n / 1_073_741_824.0)
        n >= 1_048_576L     -> "%.1f МБ".format(n / 1_048_576.0)
        n >= 1_024L         -> "%.1f КБ".format(n / 1_024.0)
        else                -> "$n Б"
    }
}
