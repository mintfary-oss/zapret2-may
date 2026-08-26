package com.freenet.vpn

import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import java.lang.reflect.Method
import java.nio.ByteBuffer
import java.nio.ByteOrder

/**
 * Instrumented tests for [PacketForwarder] packet construction logic.
 *
 * PacketForwarder builds raw IPv4/TCP packets to inject into the TUN
 * interface.  These tests verify correctness of the checksum routines
 * and packet structure using reflection (the methods are private).
 *
 * Tests run on a real device / emulator but require no network or VPN
 * permission — they exercise pure byte-manipulation logic only.
 */
@RunWith(AndroidJUnit4::class)
class PacketForwarderTest {

    // Create a dummy PacketForwarder with a no-op protect lambda.
    // We only use its private helper methods via reflection.
    private val forwarder = PacketForwarder(
        tunFd     = -1L,
        socksAddr = "127.0.0.1:1080",
        protect   = { _ -> false },
    )

    // -------------------------------------------------------------------------
    // Reflection helpers
    // -------------------------------------------------------------------------

    private fun ipChecksum(data: ByteArray, offset: Int, length: Int): Short {
        val m: Method = PacketForwarder::class.java.getDeclaredMethod(
            "ipChecksum", ByteArray::class.java, Int::class.java, Int::class.java
        ).also { it.isAccessible = true }
        return m.invoke(forwarder, data, offset, length) as Short
    }

    private fun tcpChecksum(
        buf: ByteArray, srcIp: Int, dstIp: Int,
        tcpOffset: Int, tcpLength: Int,
    ): Short {
        val m: Method = PacketForwarder::class.java.getDeclaredMethod(
            "tcpChecksum",
            ByteArray::class.java, Int::class.java, Int::class.java,
            Int::class.java, Int::class.java,
        ).also { it.isAccessible = true }
        return m.invoke(forwarder, buf, srcIp, dstIp, tcpOffset, tcpLength) as Short
    }

    private fun buildTcpPacket(
        srcIp: Int, dstIp: Int,
        srcPort: Short, dstPort: Short,
        seq: Long, ack: Long,
        flags: Int,
        payload: ByteArray = ByteArray(0),
    ): ByteArray {
        val m: Method = PacketForwarder::class.java.getDeclaredMethod(
            "buildTcpPacket",
            Int::class.java, Int::class.java,
            Short::class.java, Short::class.java,
            Long::class.java, Long::class.java,
            Int::class.java, ByteArray::class.java,
        ).also { it.isAccessible = true }
        return m.invoke(forwarder, srcIp, dstIp, srcPort, dstPort, seq, ack, flags, payload)
            as ByteArray
    }

    // -------------------------------------------------------------------------
    // IP checksum
    // -------------------------------------------------------------------------

    @Test
    fun ipChecksum_allZeros_returnsAllOnes() {
        // A 20-byte all-zero IP header (excluding checksum field) should
        // produce 0xFFFF after one's complement.
        val header = ByteArray(20)
        // Ignore the placeholder checksum field — set it to 0.
        val csum = ipChecksum(header, 0, 20)
        assertEquals(0xFFFF.toShort(), csum)
    }

    @Test
    fun ipChecksum_isNonZeroForRealHeader() {
        // A minimal real IPv4 header (version=4, IHL=5, total_len=40, TTL=64, proto=6).
        val hdr = byteArrayOf(
            0x45, 0x00, 0x00, 0x28,  // version/IHL, DSCP, total length = 40
            0x00, 0x00, 0x40, 0x00,  // ID=0, DF=1
            0x40, 0x06, 0x00, 0x00,  // TTL=64, proto=TCP(6), checksum placeholder
            127, 0, 0, 1,             // src 127.0.0.1
            127, 0, 0, 2,             // dst 127.0.0.2
        )
        val csum = ipChecksum(hdr, 0, 20)
        // Placing the checksum back and recomputing must yield 0 (standard check).
        val buf = hdr.copyOf()
        buf[10] = (csum.toInt() ushr 8).toByte()
        buf[11] = (csum.toInt() and 0xFF).toByte()
        val verify = ipChecksum(buf, 0, 20)
        assertEquals(0.toShort(), verify)
    }

    // -------------------------------------------------------------------------
    // buildTcpPacket — size and structure
    // -------------------------------------------------------------------------

    @Test
    fun buildTcpPacket_noPayload_is40Bytes() {
        // IPv4 header (20) + TCP header (20) = 40 bytes minimum.
        val pkt = buildTcpPacket(
            srcIp = 0x7F000001, dstIp = 0x7F000002,
            srcPort = 1234.toShort(), dstPort = 443.toShort(),
            seq = 0L, ack = 1L,
            flags = 0x002, // SYN
        )
        assertEquals(40, pkt.size)
    }

    @Test
    fun buildTcpPacket_withPayload_hasCorrectSize() {
        val payload = "GET / HTTP/1.1\r\n\r\n".toByteArray()
        val pkt = buildTcpPacket(
            srcIp = 0x7F000001, dstIp = 0x7F000002,
            srcPort = 1234.toShort(), dstPort = 80.toShort(),
            seq = 100L, ack = 200L,
            flags = 0x018, // PSH+ACK
            payload = payload,
        )
        assertEquals(40 + payload.size, pkt.size)
    }

    @Test
    fun buildTcpPacket_ipVersion_is4() {
        val pkt = buildTcpPacket(
            0x01020304, 0x05060708,
            srcPort = 8080.toShort(), dstPort = 443.toShort(),
            seq = 0L, ack = 0L, flags = 0x002,
        )
        val versionIHL = pkt[0].toInt() and 0xFF
        assertEquals(4, versionIHL shr 4)  // IPv4
        assertEquals(5, versionIHL and 0x0F) // IHL = 5 (no options)
    }

    @Test
    fun buildTcpPacket_ipProtocol_isTcp() {
        val pkt = buildTcpPacket(
            0x01020304, 0x05060708,
            srcPort = 1234.toShort(), dstPort = 443.toShort(),
            seq = 0L, ack = 0L, flags = 0x002,
        )
        // Protocol field is at byte offset 9.
        assertEquals(6, pkt[9].toInt() and 0xFF) // TCP = 6
    }

    @Test
    fun buildTcpPacket_containsCorrectPorts() {
        val srcPort: Short = 0x1234.toShort()
        val dstPort: Short = 0x01BB.toShort() // 443
        val pkt = buildTcpPacket(
            0x7F000001, 0x7F000002,
            srcPort, dstPort,
            seq = 0L, ack = 0L, flags = 0x002,
        )
        val buf = ByteBuffer.wrap(pkt).order(ByteOrder.BIG_ENDIAN)
        val ihl = (buf.get(0).toInt() and 0x0F) * 4
        assertEquals(srcPort, buf.getShort(ihl))
        assertEquals(dstPort, buf.getShort(ihl + 2))
    }

    @Test
    fun buildTcpPacket_ipChecksumIsValid() {
        val pkt = buildTcpPacket(
            srcIp = 0xC0A80001.toInt(), // 192.168.0.1
            dstIp = 0xC0A80002.toInt(), // 192.168.0.2
            srcPort = 5000.toShort(), dstPort = 443.toShort(),
            seq = 42L, ack = 100L, flags = 0x010, // ACK
        )
        // Verifying the IP checksum: summing the header (including checksum) must be 0.
        val verify = ipChecksum(pkt, 0, 20)
        assertEquals(0.toShort(), verify)
    }

    // -------------------------------------------------------------------------
    // PacketForwarder class is on classpath
    // -------------------------------------------------------------------------

    @Test
    fun packetForwarderClass_isLoadable() {
        assertNotNull(Class.forName("com.freenet.vpn.PacketForwarder"))
    }

    @Test
    fun packetForwarder_stopDoesNotThrow() {
        // stop() just sets an AtomicBoolean — safe to call even when not running.
        forwarder.stop()
        assertTrue(true) // reached without exception
    }
}
