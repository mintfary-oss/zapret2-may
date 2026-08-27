package com.freenet.vpn

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Intent
import android.net.VpnService
import android.os.Build
import android.os.IBinder
import android.os.ParcelFileDescriptor
import android.util.Log
import java.io.IOException
import java.util.concurrent.atomic.AtomicBoolean
import android.content.pm.PackageManager

/**
 * FreenetVpnService manages the full VPN lifecycle:
 *  1. Creates a TUN interface via VpnService.Builder (no root required).
 *  2. Starts the Go DPI-bypass SOCKS5 proxy (via gomobile AAR).
 *  3. Launches the Go TUN forwarder that routes all device traffic through
 *     the bypass proxy.
 *
 * Intent actions:
 *  - [ACTION_START]: start (or restart) the VPN.
 *  - [ACTION_STOP]:  gracefully stop the VPN.
 *
 * The service runs as a foreground service with a persistent notification.
 */
class FreenetVpnService : VpnService() {

    companion object {
        const val ACTION_START = "com.freenet.vpn.ACTION_START"
        const val ACTION_STOP  = "com.freenet.vpn.ACTION_STOP"

        /** Broadcast this intent to change the bypass strategy at runtime. */
        const val ACTION_SET_STRATEGY = "com.freenet.vpn.ACTION_SET_STRATEGY"
        const val EXTRA_STRATEGY      = "strategy"

        /** SOCKS5 port that the Go bypass proxy listens on. */
        const val SOCKS5_PORT = 1080

        /** TUN address assigned to this device in the virtual network. */
        private const val TUN_ADDRESS = "10.89.0.2"
        private const val TUN_PREFIX  = 24

        /** DNS server routed through the bypass engine (intercepted via DoH). */
        private const val DNS_SERVER = "1.1.1.1"

        /** Android notification channel id. */
        private const val NOTIFICATION_CHANNEL_ID = "freenet_vpn"
        private const val NOTIFICATION_ID = 1

        private const val TAG = "FreenetVpnService"

        /** Shared running state — used by MainActivity/ViewModel to check the UI. */
        val isRunning = AtomicBoolean(false)

        /**
         * Whether the Go AAR engine is loaded and active.  False means the
         * pure-Kotlin PacketForwarder fallback is running (TCP only, no DNS).
         * Exposed so the UI can warn users when the AAR is absent.
         */
        @Volatile
        var goEngineActive = false
            private set

        /**
         * Short human-readable engine status line shown in the UI diagnostics
         * area (e.g. "Go engine v1.0.0 (bypass active)" or "Kotlin fallback").
         */
        @Volatile
        var engineStatus = "initialising…"
            private set

        /**
         * Singleton reference set while the service is alive.
         * Used by [VpnViewModel] to read logs/stats and set strategy without
         * binding to the service.  Nullable — always check before use.
         */
        @Volatile
        var instance: FreenetVpnService? = null
            private set
    }

    private var vpnInterface: ParcelFileDescriptor? = null
    private var vpnThread: Thread? = null

    // -------------------------------------------------------------------------
    // Go engine — instantiated only when the gomobile AAR is present.
    // When building without the AAR the try/catch silently skips Go calls.
    // -------------------------------------------------------------------------
    private var goEngine: Any? = null  // mobile.FreenetEngine

    // -------------------------------------------------------------------------
    // Service lifecycle
    // -------------------------------------------------------------------------

    override fun onBind(intent: Intent?): IBinder? = super.onBind(intent)

    override fun onCreate() {
        super.onCreate()
        instance = this
        createNotificationChannel()
        initGoEngine()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_STOP -> {
                stopVpn()
                stopSelf()
                return START_NOT_STICKY
            }
            ACTION_SET_STRATEGY -> {
                val strategy = intent.getStringExtra(EXTRA_STRATEGY)
                if (!strategy.isNullOrBlank()) applyStrategy(strategy)
                return START_NOT_STICKY
            }
            else -> startVpn()  // ACTION_START or default (re-start after kill)
        }
        return START_STICKY
    }

    override fun onRevoke() {
        // System has revoked VPN permission (user disconnected from Settings).
        Log.i(TAG, "VPN permission revoked by system")
        stopVpn()
        stopSelf()
    }

    override fun onDestroy() {
        instance = null
        super.onDestroy()
        stopVpn()
    }

    // -------------------------------------------------------------------------
    // VPN start / stop
    // -------------------------------------------------------------------------

    private fun startVpn() {
        if (isRunning.getAndSet(true)) {
            Log.d(TAG, "startVpn: already running")
            return
        }

        startForeground(NOTIFICATION_ID, buildNotification())

        try {
            val pfd = buildTunInterface()
            vpnInterface = pfd

            val tunFd = pfd.fd
            Log.i(TAG, "TUN interface created, fd=$tunFd")

            vpnThread = Thread({
                runVpnLoop(tunFd.toLong())
            }, "freenet-vpn").also {
                it.isDaemon = true
                it.start()
            }

            // Notify widget and MainActivity that VPN started.
            sendBroadcast(Intent(ACTION_START).setPackage(packageName))

        } catch (e: Exception) {
            Log.e(TAG, "startVpn failed: $e")
            isRunning.set(false)
            stopSelf()
        }
    }

    private fun stopVpn() {
        if (!isRunning.getAndSet(false)) return

        Log.i(TAG, "Stopping VPN")

        // Stop the Go engine — closing the fd makes ForwardTUN return.
        stopGoEngine()

        vpnThread?.interrupt()
        vpnThread = null

        try {
            vpnInterface?.close()
        } catch (_: IOException) {}
        vpnInterface = null

        stopForeground(STOP_FOREGROUND_REMOVE)

        // Notify MainActivity.
        sendBroadcast(Intent(ACTION_STOP).setPackage(packageName))
    }

    // -------------------------------------------------------------------------
    // TUN interface
    // -------------------------------------------------------------------------

    /**
     * Configures and establishes the TUN interface.
     * All IPv4 device traffic is routed through it.  IPv6 is intentionally
     * excluded (Go TUN forwarder is IPv4-only; Russian DPI targets IPv4).
     *
     * Per-app split-tunnel filtering is applied from [SplitTunnelConfig].
     */
    private fun buildTunInterface(): ParcelFileDescriptor {
        val splitCfg = SplitTunnelConfig.load(this)

        val builder = Builder()
            .setSession(getString(R.string.app_name))
            .addAddress(TUN_ADDRESS, TUN_PREFIX)
            .addRoute("0.0.0.0", 0)
            .addDnsServer(DNS_SERVER)
            .setMtu(1400) // leave headroom for any encapsulation / fragmentation

        // Per-app split-tunnel configuration.
        //
        // IMPORTANT: Android forbids mixing addAllowedApplication and
        // addDisallowedApplication on the same Builder — doing so throws
        // IllegalStateException.  Therefore we choose one approach:
        //
        //  DISABLED  → addDisallowedApplication(us)     — all others go through VPN
        //  BLOCKLIST → addDisallowedApplication(us+list) — all others go through VPN
        //  ALLOWLIST → addAllowedApplication(list)       — only listed apps go through VPN
        //              (FreeNet is NOT in the allowlist → its traffic bypasses VPN automatically)
        val pm = packageManager
        when {
            splitCfg.mode == SplitTunnelConfig.MODE_ALLOWLIST && splitCfg.apps.isNotEmpty() -> {
                // Allowlist: only the chosen apps are routed through VPN.
                // We intentionally do NOT add packageName here — FreeNet is excluded
                // because it is not in the allowlist (Android bypasses unlisted apps).
                splitCfg.apps.forEach { pkg ->
                    if (pkg != packageName && isPackageInstalled(pm, pkg)) {
                        builder.addAllowedApplication(pkg)
                    }
                }
                Log.i(TAG, "Split tunnel ALLOWLIST: ${splitCfg.apps.size} app(s)")
            }
            else -> {
                // DISABLED or BLOCKLIST: use disallowed list.
                // Always exclude FreeNet so its own SOCKS5 connections don't loop.
                builder.addDisallowedApplication(packageName)
                if (splitCfg.mode == SplitTunnelConfig.MODE_BLOCKLIST && splitCfg.apps.isNotEmpty()) {
                    splitCfg.apps.forEach { pkg ->
                        if (pkg != packageName && isPackageInstalled(pm, pkg)) {
                            builder.addDisallowedApplication(pkg)
                        }
                    }
                    Log.i(TAG, "Split tunnel BLOCKLIST: ${splitCfg.apps.size} app(s) excluded")
                }
            }
        }

        return builder.establish()
            ?: throw IllegalStateException("VpnService.Builder.establish() returned null")
    }

    /** Checks whether a package is installed without throwing. */
    private fun isPackageInstalled(pm: PackageManager, pkg: String): Boolean {
        return try {
            pm.getPackageInfo(pkg, 0)
            true
        } catch (_: PackageManager.NameNotFoundException) {
            false
        }
    }

    // -------------------------------------------------------------------------
    // Engine control (strategy, logs, stats) — called by ViewModel / UI
    // -------------------------------------------------------------------------

    /**
     * Changes the active bypass strategy on the running Go engine.
     * Safe to call at any time; no-op if the engine is not running.
     */
    fun applyStrategy(strategy: String) {
        try {
            goEngine?.javaClass?.getMethod("setStrategy", String::class.java)
                ?.invoke(goEngine, strategy)
            Log.i(TAG, "Strategy set to: $strategy")
        } catch (e: Exception) {
            Log.e(TAG, "applyStrategy failed: $e")
        }
    }

    /**
     * Returns the most recent [n] log lines from the Go engine, or an empty
     * string if the engine is not running.
     */
    fun getRecentLogs(n: Int = 100): String {
        return try {
            goEngine?.javaClass?.getMethod("getRecentLogs", Int::class.java)
                ?.invoke(goEngine, n) as? String ?: ""
        } catch (_: Exception) { "" }
    }

    /**
     * Returns a JSON stats string from the Go engine, e.g.:
     * {"active":2,"total":15,"bytes_in":102400,...}
     * Returns an empty string if the engine is not running.
     */
    fun getStats(): String {
        return try {
            goEngine?.javaClass?.getMethod("getStats")
                ?.invoke(goEngine) as? String ?: ""
        } catch (_: Exception) { "" }
    }

    // -------------------------------------------------------------------------
    // Go engine integration (gomobile AAR)
    // -------------------------------------------------------------------------

    /**
     * Instantiates the Go bypass engine using reflection so that the code
     * compiles and runs even when the gomobile AAR is absent (e.g., during
     * plain Gradle sync or in CI without a pre-built AAR).
     *
     * After `gomobile bind -javapkg com.freenet.bypass ./mobile` all generated
     * types live in the `com.freenet.bypass` package:
     *   - `com.freenet.bypass.Mobile`        — package-level factory class
     *   - `com.freenet.bypass.FreenetEngine` — engine struct
     */
    private fun initGoEngine() {
        try {
            // gomobile bind -javapkg com.freenet.bypass ./mobile generates classes in
            // com.freenet.bypass.mobile (Go package name appended to Java package prefix):
            //   com.freenet.bypass.mobile.Mobile        — package-level factory
            //   com.freenet.bypass.mobile.FreenetEngine — engine struct
            //   com.freenet.bypass.mobile.SocketProtector — interface
            val newEngine = Class.forName("com.freenet.bypass.mobile.Mobile")
                .getMethod("newFreenetEngine")
            goEngine = newEngine.invoke(null)

            // Read the engine version for diagnostic display.
            val ver = try {
                goEngine!!.javaClass.getMethod("getVersion")
                    .invoke(goEngine) as? String ?: "?"
            } catch (_: Exception) { "?" }

            engineStatus = "Go engine v$ver — загружен ✓"
            Log.i(TAG, "Go engine initialised (v$ver)")
        } catch (e: ClassNotFoundException) {
            engineStatus = "Kotlin fallback (нет AAR) — только TCP"
            Log.w(TAG, "Go engine AAR not found — Kotlin PacketForwarder will handle traffic")
        } catch (e: Exception) {
            engineStatus = "Ошибка инициализации Go: $e"
            Log.e(TAG, "Go engine init error: $e")
        }
    }

    private fun stopGoEngine() {
        try {
            goEngine?.let { eng ->
                eng.javaClass.getMethod("stop").invoke(eng)
            }
        } catch (e: Exception) {
            Log.e(TAG, "stopGoEngine: $e")
        }
    }

    /**
     * Runs the VPN loop.
     *
     * If the Go AAR is present ([goEngine] != null) the Go TUN forwarder handles
     * everything — TCP, UDP, and DNS-over-HTTPS intercept.  The Kotlin
     * [PacketForwarder] is used ONLY when the AAR is NOT compiled in (e.g. a
     * local build without scripts/build-android.sh).
     *
     * IMPORTANT: Do NOT fall back to [PacketForwarder] when the Go engine is
     * present but returned an error.  PacketForwarder is TCP-only; it silently
     * drops UDP, so DNS queries (UDP → 1.1.1.1:53) are never answered and every
     * domain resolution fails → "VPN on but no traffic" symptom.
     */
    private fun runVpnLoop(tunFd: Long) {
        // Optimistically mark the Go engine as active; tryStartGoVPN clears this
        // if the AAR is absent or immediately fails.
        if (goEngine != null) {
            goEngineActive = true
            engineStatus = engineStatus.replace("загружен ✓", "активен ✓")
        }
        val aarPresent = tryStartGoVPN(tunFd)
        if (!aarPresent) {
            // Go AAR not compiled in — use pure-Kotlin TCP-only fallback.
            // DNS (UDP port 53) will NOT work in this mode.  This path is only
            // intended for development builds without a prebuilt AAR.
            Log.w(TAG, "Go AAR absent — Kotlin PacketForwarder running (TCP only, no DNS)")
            engineStatus = "Kotlin fallback — только TCP, DNS не работает!"
            goEngineActive = false
            PacketForwarder(
                tunFd     = tunFd,
                socksAddr = "127.0.0.1:$SOCKS5_PORT",
                protect   = ::protect,
            ).run()
        }
        // aarPresent == true: Go engine ran and has now stopped; VPN will be torn down.
    }

    /**
     * Starts the Go VPN engine via reflection and **blocks** until it stops.
     *
     * Uses [FreenetEngine.startVPNSimple] which does NOT require a
     * SocketProtector callback.  This is safe because [buildTunInterface] calls
     * [android.net.VpnService.Builder.addDisallowedApplication] for FreeNet's
     * own package, so all bypass-proxy sockets already bypass the TUN without
     * any per-socket protect() call.
     *
     * Returns:
     *  - **true**  — Go AAR was present and ran.  Caller must NOT start PacketForwarder.
     *  - **false** — AAR absent or engine could not start.  Caller may try Kotlin fallback.
     */
    private fun tryStartGoVPN(tunFd: Long): Boolean {
        val eng = goEngine ?: return false
        return try {
            // startVPNSimple(long tunFd, int port) — no SocketProtector needed.
            // Using Long.TYPE / Integer.TYPE (primitive) avoids NoSuchMethodException.
            eng.javaClass
                .getMethod("startVPNSimple",
                    java.lang.Long.TYPE,
                    java.lang.Integer.TYPE)
                .invoke(eng, tunFd, SOCKS5_PORT)

            // startVPNSimple returned — TUN fd was closed (normal shutdown).
            goEngineActive = false
            true
        } catch (e: java.lang.reflect.InvocationTargetException) {
            // Go error propagated back.  Most common: TUN fd closed by stopVpn().
            goEngineActive = false
            val cause = e.cause
            if (!isRunning.get()) {
                Log.d(TAG, "Go VPN ended (normal shutdown): ${cause?.message}")
            } else {
                Log.e(TAG, "Go VPN error while running: $cause")
                engineStatus = "Ошибка движка: ${cause?.message}"
            }
            true  // AAR was present — do NOT start PacketForwarder
        } catch (e: NoSuchMethodException) {
            // AAR older than v1.8.4 — startVPNSimple not available.
            // Fall back to startVPN with SocketProtector (legacy path).
            Log.w(TAG, "startVPNSimple not found, trying legacy startVPN: $e")
            tryStartGoVPNLegacy(tunFd, eng)
        } catch (e: Exception) {
            Log.e(TAG, "tryStartGoVPN failed: $e")
            engineStatus = "Ошибка запуска Go: $e"
            false
        }
    }

    /**
     * Legacy fallback for AAR versions < 1.8.4 that only expose [startVPN]
     * with a SocketProtector parameter.  Uses a [java.lang.reflect.Proxy] to
     * implement the interface.
     */
    private fun tryStartGoVPNLegacy(tunFd: Long, eng: Any): Boolean {
        return try {
            val protectorCls = Class.forName("com.freenet.bypass.mobile.SocketProtector")
            val protector = java.lang.reflect.Proxy.newProxyInstance(
                protectorCls.classLoader,
                arrayOf(protectorCls)
            ) { _, _, args ->
                val fd = (args?.getOrNull(0) as? Long)?.toInt() ?: return@newProxyInstance false
                protect(fd)
            }
            eng.javaClass
                .getMethod("startVPN",
                    java.lang.Long.TYPE,
                    java.lang.Integer.TYPE,
                    protectorCls)
                .invoke(eng, tunFd, SOCKS5_PORT, protector)
            goEngineActive = false
            true
        } catch (e: ClassNotFoundException) {
            Log.w(TAG, "Go AAR classes not found — using Kotlin PacketForwarder")
            false
        } catch (e: java.lang.reflect.InvocationTargetException) {
            goEngineActive = false
            val cause = e.cause
            if (!isRunning.get()) {
                Log.d(TAG, "Go VPN (legacy) ended: ${cause?.message}")
            } else {
                Log.e(TAG, "Go VPN (legacy) error: $cause")
            }
            true
        } catch (e: Exception) {
            Log.e(TAG, "tryStartGoVPNLegacy failed: $e")
            false
        }
    }

    // -------------------------------------------------------------------------
    // Notification
    // -------------------------------------------------------------------------

    private fun createNotificationChannel() {
        val channel = NotificationChannel(
            NOTIFICATION_CHANNEL_ID,
            getString(R.string.notification_channel_name),
            NotificationManager.IMPORTANCE_LOW
        ).apply {
            description = getString(R.string.notification_channel_desc)
            setShowBadge(false)
        }
        val nm = getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(channel)
    }

    private fun buildNotification(): Notification {
        val stopIntent = PendingIntent.getService(
            this, 0,
            Intent(this, FreenetVpnService::class.java).setAction(ACTION_STOP),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val contentIntent = PendingIntent.getActivity(
            this, 0,
            Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )

        return Notification.Builder(this, NOTIFICATION_CHANNEL_ID)
            .setContentTitle(getString(R.string.notification_title))
            .setContentText(getString(R.string.notification_text))
            .setSmallIcon(R.drawable.ic_launcher_foreground)
            .setContentIntent(contentIntent)
            .setOngoing(true)
            .addAction(
                Notification.Action.Builder(
                    null,
                    getString(R.string.notification_action_stop),
                    stopIntent,
                ).build()
            )
            .build()
    }
}
