package com.freenet.vpn

import android.content.Context
import android.content.SharedPreferences
import org.json.JSONArray

/**
 * Persistent split-tunnel (per-app VPN filtering) configuration.
 *
 * Three modes are supported:
 *  - **disabled** — all apps go through the VPN (default).
 *  - **allowlist** — only apps in [apps] go through the VPN.
 *  - **blocklist** — all apps *except* those in [apps] go through the VPN.
 *
 * Configuration is stored in SharedPreferences so it survives process restarts
 * and is applied by [FreenetVpnService.buildTunInterface].
 */
data class SplitTunnelConfig(
    val mode: String,        // "disabled" | "allowlist" | "blocklist"
    val apps: Set<String>,   // package names
) {
    companion object {
        const val MODE_DISABLED   = "disabled"
        const val MODE_ALLOWLIST  = "allowlist"
        const val MODE_BLOCKLIST  = "blocklist"

        private const val PREFS_NAME    = "freenet_split_tunnel"
        private const val KEY_MODE      = "mode"
        private const val KEY_APPS_JSON = "apps_json"

        /** Returns the default (all-traffic) configuration. */
        fun default() = SplitTunnelConfig(MODE_DISABLED, emptySet())

        /** Loads the saved configuration from SharedPreferences. */
        fun load(ctx: Context): SplitTunnelConfig {
            val prefs = prefs(ctx)
            val mode = prefs.getString(KEY_MODE, MODE_DISABLED) ?: MODE_DISABLED
            val json = prefs.getString(KEY_APPS_JSON, "[]") ?: "[]"
            val apps = mutableSetOf<String>()
            try {
                val arr = JSONArray(json)
                for (i in 0 until arr.length()) {
                    apps += arr.getString(i)
                }
            } catch (_: Exception) { /* keep empty set */ }
            return SplitTunnelConfig(mode, apps)
        }

        private fun prefs(ctx: Context): SharedPreferences =
            ctx.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)

        /** Saves configuration to SharedPreferences. */
        fun save(ctx: Context, config: SplitTunnelConfig) {
            val json = JSONArray(config.apps.toList()).toString()
            prefs(ctx).edit()
                .putString(KEY_MODE, config.mode)
                .putString(KEY_APPS_JSON, json)
                .apply()
        }
    }
}
