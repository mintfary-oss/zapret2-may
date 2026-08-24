package com.freenet.vpn

import android.app.PendingIntent
import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.widget.RemoteViews

/**
 * FreeNetWidget — 2×1 home-screen toggle widget.
 *
 * Tapping the button sends ACTION_WIDGET_TOGGLE to this receiver, which
 * either starts or stops [FreenetVpnService] depending on the current state.
 *
 * The widget colour reflects the VPN state:
 *   • Green  (#2E7D32) — VPN is running
 *   • Red    (#C62828) — VPN is stopped
 *
 * State changes are broadcast from [FreenetVpnService] via
 * [FreenetVpnService.ACTION_START] / [FreenetVpnService.ACTION_STOP],
 * which trigger [onReceive] and update all active widget instances.
 */
class FreeNetWidget : AppWidgetProvider() {

    companion object {
        /** Broadcast action sent when the widget button is tapped. */
        const val ACTION_WIDGET_TOGGLE = "com.freenet.vpn.WIDGET_TOGGLE"

        /** Widget button background colours. */
        private const val COLOR_ON  = -0xD17CE  // #FF2E7D32 dark green
        private const val COLOR_OFF = -0x397D8  // #FFC62828 dark red

        /**
         * Forces all active widget instances to redraw with the latest state.
         * Call this whenever VPN state changes.
         */
        fun update(ctx: Context) {
            val mgr = AppWidgetManager.getInstance(ctx)
            val ids = mgr.getAppWidgetIds(ComponentName(ctx, FreeNetWidget::class.java))
            if (ids.isNotEmpty()) {
                val widget = FreeNetWidget()
                widget.onUpdate(ctx, mgr, ids)
            }
        }
    }

    // -------------------------------------------------------------------------
    // AppWidgetProvider callbacks
    // -------------------------------------------------------------------------

    override fun onUpdate(ctx: Context, mgr: AppWidgetManager, appWidgetIds: IntArray) {
        appWidgetIds.forEach { id -> updateWidget(ctx, mgr, id) }
    }

    override fun onReceive(ctx: Context, intent: Intent) {
        super.onReceive(ctx, intent)
        when (intent.action) {
            ACTION_WIDGET_TOGGLE -> handleToggle(ctx)
            // Redraw on service start/stop broadcasts from FreenetVpnService.
            FreenetVpnService.ACTION_START,
            FreenetVpnService.ACTION_STOP -> update(ctx)
        }
    }

    // -------------------------------------------------------------------------
    // Internal helpers
    // -------------------------------------------------------------------------

    private fun handleToggle(ctx: Context) {
        if (FreenetVpnService.isRunning.get()) {
            // Stop the VPN directly — no permission dialog needed to stop.
            ctx.startService(
                Intent(ctx, FreenetVpnService::class.java)
                    .setAction(FreenetVpnService.ACTION_STOP)
            )
        } else {
            // Launch MainActivity to handle the VPN permission dialog if needed,
            // then start the VPN.  Widgets cannot show dialogs themselves.
            val launch = Intent(ctx, MainActivity::class.java)
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
                .putExtra(MainActivity.EXTRA_START_VPN, true)
            ctx.startActivity(launch)
        }
        // Optimistically redraw so the widget feels responsive.
        update(ctx)
    }

    private fun updateWidget(ctx: Context, mgr: AppWidgetManager, widgetId: Int) {
        val running = FreenetVpnService.isRunning.get()

        // PendingIntent sent when the button is tapped.
        val toggleIntent = Intent(ctx, FreeNetWidget::class.java)
            .setAction(ACTION_WIDGET_TOGGLE)
        val togglePi = PendingIntent.getBroadcast(
            ctx, 0, toggleIntent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )

        val views = RemoteViews(ctx.packageName, R.layout.widget_toggle)
        views.setOnClickPendingIntent(R.id.widget_btn_toggle, togglePi)
        views.setTextViewText(
            R.id.widget_btn_toggle,
            if (running) ctx.getString(R.string.widget_btn_on)
            else         ctx.getString(R.string.widget_btn_off)
        )
        views.setInt(
            R.id.widget_btn_toggle, "setBackgroundColor",
            if (running) COLOR_ON else COLOR_OFF
        )

        mgr.updateAppWidget(widgetId, views)
    }
}
