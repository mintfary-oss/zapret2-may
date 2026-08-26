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

        /** SOCKS5 port that the Go bypass proxy listens on. */
        const val SOCKS5_PORT = 1080

        /** TUN address assigned to this device in the virtual network. */
        private const val TUN_ADDRESS = "10.89.0.2"
        private const val TUN_PREFIX  = 24

        /** DNS server routed through the bypass engine. */
        private const val DNS_SERVER = "1.1.1.1"

        /** Android notification channel id. */
        private const val NOTIFICATION_CHANNEL_ID = "freenet_vpn"
        private const val NOTIFICATION_ID = 1

        private const val TAG = "FreenetVpnService"

        /** Shared running state — used by MainActivity to update the UI. */
        val isRunning = AtomicBoolean(false)
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
        val builder = Builder()
            .setSession(getString(R.string.app_name))
            .addAddress(TUN_ADDRESS, TUN_PREFIX)
            .addRoute("0.0.0.0", 0)
            .addDnsServer(DNS_SERVER)
            .setMtu(1500)
            // Always exclude FreeNet itself so our SOCKS5 dial doesn't loop.
            .addDisallowedApplication(packageName)

        // Apply per-app split-tunnel configuration.
        val splitCfg = SplitTunnelConfig.load(this)
        if (splitCfg.mode != SplitTunnelConfig.MODE_DISABLED && splitCfg.apps.isNotEmpty()) {
            val pm = packageManager
            when (splitCfg.mode) {
                SplitTunnelConfig.MODE_ALLOWLIST -> {
                    // Only listed apps go through the VPN.
                    splitCfg.apps.forEach { pkg ->
                        if (isPackageInstalled(pm, pkg)) {
                            builder.addAllowedApplication(pkg)
                        }
                    }
                    Log.i(TAG, "Split tunnel ALLOWLIST: ${splitCfg.apps.size} app(s)")
                }
                SplitTunnelConfig.MODE_BLOCKLIST -> {
                    // All apps except listed ones go through the VPN.
                    splitCfg.apps.forEach { pkg ->
                        if (isPackageInstalled(pm, pkg) && pkg != packageName) {
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
    // Go engine integration (gomobile AAR)
    // -------------------------------------------------------------------------

    /**
     * Instantiates the Go bypass engine using reflection so that the code
     * compiles and runs even when the gomobile AAR is absent (e.g., during
     * plain Gradle sync or in CI without a pre-built AAR).
     */
    private fun initGoEngine() {
        try {
            val cls = Class.forName("mobile.FreenetEngine")
            val newEngine = Class.forName("mobile.Mobile")
                .getMethod("newFreenetEngine")
            goEngine = newEngine.invoke(null)
            Log.i(TAG, "Go engine initialised")
        } catch (e: ClassNotFoundException) {
            Log.w(TAG, "Go engine AAR not found — bypass disabled (run scripts/build-android.sh)")
        } catch (e: Exception) {
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
     * Runs the VPN loop:
     *  - If the Go engine AAR is available: calls [mobile.FreenetEngine.startVPN]
     *    which starts the SOCKS5 proxy and the TUN forwarder in Go.
     *  - Fallback: uses the pure-Kotlin [PacketForwarder] (basic TCP forwarding
     *    without DPI bypass; useful for testing without a built AAR).
     */
    private fun runVpnLoop(tunFd: Long) {
        val goStarted = tryStartGoVPN(tunFd)
        if (!goStarted) {
            Log.i(TAG, "Falling back to Kotlin PacketForwarder (no bypass)")
            PacketForwarder(
                tunFd      = tunFd,
                socksAddr  = "127.0.0.1:$SOCKS5_PORT",
                protect    = ::protect,
            ).run()
        }
    }

    /**
     * Attempts to start the Go VPN (SOCKS5 + TUN forwarder) via reflection.
     * Returns true if the Go engine started and ran; false if the AAR is absent.
     */
    private fun tryStartGoVPN(tunFd: Long): Boolean {
        val eng = goEngine ?: return false
        return try {
            // Wrap Android's VpnService.protect() as a gomobile SocketProtector.
            // gomobile: interfaces are top-level Java classes, NOT nested in Mobile.
            // Correct: "mobile.SocketProtector"  Wrong: "mobile.Mobile$SocketProtector"
            val protectorCls = Class.forName("mobile.SocketProtector")
            val protector = java.lang.reflect.Proxy.newProxyInstance(
                protectorCls.classLoader,
                arrayOf(protectorCls)
            ) { _, _, args ->
                val fd = (args[0] as Long).toInt()
                protect(fd)
            }

            // gomobile uses Java primitive types (long, int), not boxed types.
            // Long::class.java = java.lang.Long (boxed) → NoSuchMethodException
            // java.lang.Long.TYPE = long (primitive)    → correct
            eng.javaClass
                .getMethod("startVPN",
                    java.lang.Long.TYPE,
                    java.lang.Integer.TYPE,
                    protectorCls)
                .invoke(eng, tunFd, SOCKS5_PORT, protector)

            true
        } catch (e: ClassNotFoundException) {
            Log.w(TAG, "Go AAR absent (mobile.SocketProtector) — using Kotlin fallback")
            false
        } catch (e: Exception) {
            Log.e(TAG, "tryStartGoVPN failed: $e")
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
