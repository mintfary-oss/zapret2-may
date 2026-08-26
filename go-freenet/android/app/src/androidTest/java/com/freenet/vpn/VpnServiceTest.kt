package com.freenet.vpn

import android.content.Context
import android.content.Intent
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Test
import org.junit.runner.RunWith

/**
 * Instrumented tests for [FreenetVpnService] lifecycle and constants.
 *
 * Note: VpnService.prepare() requires user confirmation and cannot be
 * automated in standard instrumented tests.  These tests therefore verify
 * the service class structure, intent actions, and static state management
 * without actually starting the VPN.
 */
@RunWith(AndroidJUnit4::class)
class VpnServiceTest {

    private val ctx: Context
        get() = InstrumentationRegistry.getInstrumentation().targetContext

    // -------------------------------------------------------------------------
    // Service class sanity checks
    // -------------------------------------------------------------------------

    @Test
    fun serviceClass_isLoadable() {
        // The class must be on the classpath (compiled + registered in manifest).
        val cls = Class.forName("com.freenet.vpn.FreenetVpnService")
        assertNotNull(cls)
    }

    @Test
    fun intentActions_areNonEmpty() {
        assertFalse(FreenetVpnService.ACTION_START.isBlank())
        assertFalse(FreenetVpnService.ACTION_STOP.isBlank())
    }

    @Test
    fun intentActions_haveCorrectPackagePrefix() {
        val pkg = "com.freenet.vpn"
        assert(FreenetVpnService.ACTION_START.startsWith(pkg)) {
            "ACTION_START should start with $pkg"
        }
        assert(FreenetVpnService.ACTION_STOP.startsWith(pkg)) {
            "ACTION_STOP should start with $pkg"
        }
    }

    @Test
    fun isRunning_initiallyFalse() {
        // The VPN is not running when tests start (no VPN permission granted).
        assertFalse(FreenetVpnService.isRunning.get())
    }

    // -------------------------------------------------------------------------
    // Intent construction
    // -------------------------------------------------------------------------

    @Test
    fun startIntent_isConstructible() {
        val intent = Intent(ctx, FreenetVpnService::class.java)
            .setAction(FreenetVpnService.ACTION_START)
        assertNotNull(intent)
    }

    @Test
    fun stopIntent_isConstructible() {
        val intent = Intent(ctx, FreenetVpnService::class.java)
            .setAction(FreenetVpnService.ACTION_STOP)
        assertNotNull(intent)
    }

    // -------------------------------------------------------------------------
    // Application context
    // -------------------------------------------------------------------------

    @Test
    fun applicationContext_isCorrectPackage() {
        val appCtx = ApplicationProvider.getApplicationContext<Context>()
        assert(appCtx.packageName.startsWith("com.freenet.vpn")) {
            "Unexpected package: ${appCtx.packageName}"
        }
    }
}
