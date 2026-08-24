package com.freenet.vpn

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/**
 * Restarts the VPN service after device reboot if auto-start is enabled.
 */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != Intent.ACTION_BOOT_COMPLETED) return

        val prefs = context.getSharedPreferences("freenet_prefs", Context.MODE_PRIVATE)
        if (!prefs.getBoolean("auto_start", false)) return

        // Start the VPN service — user has previously granted permission.
        val vpnIntent = Intent(context, FreenetVpnService::class.java)
            .setAction(ACTION_START)
        context.startForegroundService(vpnIntent)
    }

    companion object {
        const val ACTION_START = "com.freenet.vpn.ACTION_START"
        const val ACTION_STOP  = "com.freenet.vpn.ACTION_STOP"
    }
}
