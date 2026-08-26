package com.freenet.vpn

import android.content.Context
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith

/**
 * Instrumented tests for [SplitTunnelConfig].
 *
 * These tests verify that split-tunnel configuration is correctly persisted
 * to and restored from SharedPreferences on a real Android device or emulator.
 * They do not require VPN permission.
 */
@RunWith(AndroidJUnit4::class)
class SplitTunnelConfigTest {

    private lateinit var ctx: Context

    @Before
    fun setUp() {
        ctx = InstrumentationRegistry.getInstrumentation().targetContext
        // Start each test with a clean slate.
        ctx.getSharedPreferences("freenet_split_tunnel", Context.MODE_PRIVATE)
            .edit().clear().commit()
    }

    @After
    fun tearDown() {
        ctx.getSharedPreferences("freenet_split_tunnel", Context.MODE_PRIVATE)
            .edit().clear().commit()
    }

    // -------------------------------------------------------------------------
    // Default state
    // -------------------------------------------------------------------------

    @Test
    fun defaultConfig_isDisabledWithNoApps() {
        val cfg = SplitTunnelConfig.default()
        assertEquals(SplitTunnelConfig.MODE_DISABLED, cfg.mode)
        assertTrue(cfg.apps.isEmpty())
    }

    @Test
    fun load_returnsDefaultWhenNothingSaved() {
        val cfg = SplitTunnelConfig.load(ctx)
        assertEquals(SplitTunnelConfig.MODE_DISABLED, cfg.mode)
        assertTrue(cfg.apps.isEmpty())
    }

    // -------------------------------------------------------------------------
    // Persist and reload
    // -------------------------------------------------------------------------

    @Test
    fun saveAndLoad_disabled_roundTrips() {
        val cfg = SplitTunnelConfig(SplitTunnelConfig.MODE_DISABLED, emptySet())
        SplitTunnelConfig.save(ctx, cfg)

        val loaded = SplitTunnelConfig.load(ctx)
        assertEquals(SplitTunnelConfig.MODE_DISABLED, loaded.mode)
        assertTrue(loaded.apps.isEmpty())
    }

    @Test
    fun saveAndLoad_allowlist_roundTrips() {
        val apps = setOf("com.example.app1", "com.example.app2")
        val cfg = SplitTunnelConfig(SplitTunnelConfig.MODE_ALLOWLIST, apps)
        SplitTunnelConfig.save(ctx, cfg)

        val loaded = SplitTunnelConfig.load(ctx)
        assertEquals(SplitTunnelConfig.MODE_ALLOWLIST, loaded.mode)
        assertEquals(apps, loaded.apps)
    }

    @Test
    fun saveAndLoad_blocklist_roundTrips() {
        val apps = setOf("com.telegram.messenger", "org.videolan.vlc")
        val cfg = SplitTunnelConfig(SplitTunnelConfig.MODE_BLOCKLIST, apps)
        SplitTunnelConfig.save(ctx, cfg)

        val loaded = SplitTunnelConfig.load(ctx)
        assertEquals(SplitTunnelConfig.MODE_BLOCKLIST, loaded.mode)
        assertEquals(apps, loaded.apps)
    }

    // -------------------------------------------------------------------------
    // Overwrite behaviour
    // -------------------------------------------------------------------------

    @Test
    fun save_overwritesPreviousConfig() {
        // Save allowlist first.
        SplitTunnelConfig.save(
            ctx,
            SplitTunnelConfig(SplitTunnelConfig.MODE_ALLOWLIST, setOf("com.example.a"))
        )
        // Then overwrite with disabled.
        SplitTunnelConfig.save(
            ctx,
            SplitTunnelConfig(SplitTunnelConfig.MODE_DISABLED, emptySet())
        )

        val loaded = SplitTunnelConfig.load(ctx)
        assertEquals(SplitTunnelConfig.MODE_DISABLED, loaded.mode)
        assertTrue(loaded.apps.isEmpty())
    }

    // -------------------------------------------------------------------------
    // Resilience
    // -------------------------------------------------------------------------

    @Test
    fun load_withCorruptedJson_returnsEmptyAppSet() {
        // Manually write garbage JSON to simulate a corrupted prefs entry.
        ctx.getSharedPreferences("freenet_split_tunnel", Context.MODE_PRIVATE)
            .edit()
            .putString("mode", SplitTunnelConfig.MODE_ALLOWLIST)
            .putString("apps_json", "not-valid-json{{{{")
            .commit()

        val loaded = SplitTunnelConfig.load(ctx)
        // Mode should be restored; corrupt apps_json falls back to empty set.
        assertEquals(SplitTunnelConfig.MODE_ALLOWLIST, loaded.mode)
        assertTrue(loaded.apps.isEmpty())
    }

    // -------------------------------------------------------------------------
    // Large app list
    // -------------------------------------------------------------------------

    @Test
    fun saveAndLoad_manyApps_roundTrips() {
        val apps = (1..50).map { "com.app.package$it" }.toSet()
        val cfg = SplitTunnelConfig(SplitTunnelConfig.MODE_BLOCKLIST, apps)
        SplitTunnelConfig.save(ctx, cfg)

        val loaded = SplitTunnelConfig.load(ctx)
        assertEquals(apps.size, loaded.apps.size)
        apps.forEach { pkg -> assertTrue("$pkg missing", pkg in loaded.apps) }
    }

    // -------------------------------------------------------------------------
    // Containment helpers
    // -------------------------------------------------------------------------

    @Test
    fun appsSet_containsAndNotContains() {
        val cfg = SplitTunnelConfig(
            SplitTunnelConfig.MODE_ALLOWLIST,
            setOf("com.example.yes")
        )
        assertTrue("com.example.yes" in cfg.apps)
        assertFalse("com.example.no" in cfg.apps)
    }
}
