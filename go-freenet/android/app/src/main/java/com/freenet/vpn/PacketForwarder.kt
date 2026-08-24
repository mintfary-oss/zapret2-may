package com.freenet.vpn

import android.util.Log
import java.io.FileDescriptor
import java.io.FileInputStream
import java.io.FileOutputStream
import java.io.IOException
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.Socket
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean

/**
 * PacketForwarder is a pure-Kotlin fallback tun2socks implementation.
 *
 * It reads raw IPv4 packets from the TUN file descriptor, intercepts TCP
 * connections, and proxies them through a local SOCKS5 server.  Responses from
 * the SOCKS5 server are wrapped back into IPv4/TCP packets and written to TUN.
 *
 * This class is used when the gomobile AAR (which provides the Go-based TUN
 * forwarder) is not available — for example during CI builds or development
 * without a compiled AAR.  In production both paths lead to the same Go SOCKS5
 * bypass engine; the difference is only the TUN-level packet processing.
 *
 * Limitations:
 *  - IPv4 TCP only (IPv6 / UDP are dropped silently).
 *  - Simplified TCP sequence number management (no SACK, no options).
 *  - No IP fragmentation reassembly.
 */
class PacketForwarder(
    private val tunFd: Long,
    private val socksAddr: String,
    /** Android VpnService.protect() — marks bypass sockets to skip the TUN. */
    private val protect: (Int) -> Boolean,
) {
    companion object {
        private const val TAG = "PacketForwarder"
        private const val MAX_PACKET = 65535

        // TCP flags.
        private const val FLAG_FIN: Byte = 0x01
        private const val FLAG_SYN: Byte = 0x02
        private const val FLAG_RST: Byte = 0x04
        private const val FLAG_PSH: Byte = 0x08
        private const val FLAG_ACK: Byte = 0x10

        private const val SOCKS5_PORT_DEFAULT = 1080
    }

    /** Tracks one proxied TCP connection. */
    private data class ConnKey(
        val srcIp: Int, val dstIp: Int,
        val srcPort: Short, val dstPort: Short,
    )

    private inner class TunConn(
        val key: ConnKey,
        val socket: Socket,
        var clientNext: Long = 0L,  // next expected seq from device (Long to avoid sign issues)
        var serverNext: Long = 0L,  // next seq we send to device
        val closed: AtomicBoolean = AtomicBoolean(false),
    )

    private val conns  = ConcurrentHashMap<ConnKey, TunConn>()
    private val pool   = Executors.newCachedThreadPool()
    private val active = AtomicBoolean(true)

    /** Reads packets from TUN in a loop.  Blocks until the fd is closed. */
    fun run() {
        val fd = FileDescriptor()
        // Reflectively set the int fd on the FileDescriptor.
        try {
            val field = FileDescriptor::class.java.getDeclaredField("descriptor")
            field.isAccessible = true
            field.set(fd, tunFd.toInt())
        } catch (e: Exception) {
            Log.e(TAG, "Cannot access FileDescriptor.descriptor: $e")
            return
        }

        val input  = FileInputStream(fd)
        val output = FileOutputStream(fd)
        val buf    = ByteArray(MAX_PACKET)

        while (active.get()) {
            val n = try { input.read(buf) } catch (_: IOException) { break }
            if (n <= 0) break

            val pkt = ByteBuffer.wrap(buf, 0, n).order(ByteOrder.BIG_ENDIAN)
            handlePacket(pkt, output)
        }

        active.set(false)
        pool.shutdownNow()
        conns.values.forEach { it.socket.close() }
        Log.i(TAG, "PacketForwarder stopped")
    }

    /** Stop the forwarding loop from another thread. */
    fun stop() { active.set(false) }

    // -------------------------------------------------------------------------
    // Packet dispatch
    // -------------------------------------------------------------------------

    private fun handlePacket(pkt: ByteBuffer, out: FileOutputStream) {
        pkt.position(0)
        if (pkt.limit() < 20) return

        val versionIHL = pkt.get(0).toInt() and 0xFF
        val version = versionIHL shr 4
        if (version != 4) return // IPv6 not handled here

        val ihl    = (versionIHL and 0x0F) * 4
        val proto  = pkt.get(9).toInt() and 0xFF
        if (proto != 6) return  // TCP only

        val srcIp = pkt.getInt(12)
        val dstIp = pkt.getInt(16)

        if (pkt.limit() < ihl + 20) return
        val tcpOff = ihl

        val srcPort = pkt.getShort(tcpOff)
        val dstPort = pkt.getShort(tcpOff + 2)
        val seqNum  = pkt.getInt(tcpOff + 4).toLong() and 0xFFFFFFFFL
        val ackNum  = pkt.getInt(tcpOff + 8).toLong() and 0xFFFFFFFFL
        val dataOff = ((pkt.get(tcpOff + 12).toInt() and 0xFF) shr 4) * 4
        val flags   = pkt.get(tcpOff + 13)

        val isSyn = (flags and FLAG_SYN) != 0.toByte()
        val isAck = (flags and FLAG_ACK) != 0.toByte()
        val isFin = (flags and FLAG_FIN) != 0.toByte()
        val isRst = (flags and FLAG_RST) != 0.toByte()

        val key = ConnKey(srcIp, dstIp, srcPort, dstPort)

        when {
            isSyn && !isAck -> pool.execute { handleSyn(key, seqNum, out) }

            isFin || isRst -> {
                conns.remove(key)?.let { tc ->
                    tc.closed.set(true)
                    tc.socket.close()
                }
            }

            isAck -> {
                val payloadStart = tcpOff + dataOff
                val payloadLen   = pkt.limit() - payloadStart
                if (payloadLen <= 0) return

                val tc = conns[key] ?: return
                synchronized(tc) {
                    if (tc.clientNext != seqNum) return // out-of-order
                    tc.clientNext += payloadLen
                }

                val payload = ByteArray(payloadLen)
                pkt.position(payloadStart)
                pkt.get(payload)
                try {
                    tc.socket.getOutputStream().write(payload)
                } catch (_: IOException) {
                    conns.remove(key)
                    tc.socket.close()
                }
            }
        }
    }

    // -------------------------------------------------------------------------
    // SYN handler — dials SOCKS5 and completes TCP handshake
    // -------------------------------------------------------------------------

    private fun handleSyn(key: ConnKey, clientISN: Long, out: FileOutputStream) {
        val dstAddr = InetAddress.getByAddress(intToBytes(key.dstIp))
        val dstPort = key.dstPort.toInt() and 0xFFFF

        val socket = try {
            dialSocks5(dstAddr.hostAddress!!, dstPort)
        } catch (e: Exception) {
            Log.w(TAG, "SOCKS5 dial $dstAddr:$dstPort failed: $e")
            // Send RST to device.
            synchronized(out) {
                out.write(buildTcpPacket(
                    key.dstIp, key.srcIp, key.dstPort, key.srcPort,
                    seq = 0, ack = (clientISN + 1) and 0xFFFFFFFFL,
                    flags = FLAG_RST.toInt(),
                ))
            }
            return
        }

        val serverISN = (System.nanoTime() and 0xFFFFFFFFL)
        val tc = TunConn(
            key        = key,
            socket     = socket,
            clientNext = clientISN + 1,
            serverNext = serverISN + 1,
        )
        conns[key] = tc

        // Send SYN-ACK to device.
        synchronized(out) {
            out.write(buildTcpPacket(
                key.dstIp, key.srcIp, key.dstPort, key.srcPort,
                seq   = serverISN,
                ack   = clientISN + 1,
                flags = (FLAG_SYN.toInt() or FLAG_ACK.toInt()),
            ))
        }

        // Relay: socket → TUN.
        pool.execute { relaySocketToTun(tc, out) }
    }

    private fun relaySocketToTun(tc: TunConn, out: FileOutputStream) {
        val buf = ByteArray(4096)
        try {
            val ins = tc.socket.getInputStream()
            while (!tc.closed.get()) {
                val n = ins.read(buf)
                if (n < 0) break

                val seq: Long
                val ack: Long
                synchronized(tc) {
                    seq = tc.serverNext
                    ack = tc.clientNext
                    tc.serverNext += n
                }

                synchronized(out) {
                    out.write(buildTcpPacket(
                        tc.key.dstIp, tc.key.srcIp,
                        tc.key.dstPort, tc.key.srcPort,
                        seq = seq, ack = ack,
                        flags = (FLAG_PSH.toInt() or FLAG_ACK.toInt()),
                        payload = buf.copyOf(n),
                    ))
                }
            }
        } catch (_: IOException) {}

        conns.remove(tc.key)
        tc.closed.set(true)
        // Send FIN.
        val seq: Long; val ack: Long
        synchronized(tc) { seq = tc.serverNext; ack = tc.clientNext }
        try {
            synchronized(out) {
                out.write(buildTcpPacket(
                    tc.key.dstIp, tc.key.srcIp, tc.key.dstPort, tc.key.srcPort,
                    seq = seq, ack = ack,
                    flags = (FLAG_FIN.toInt() or FLAG_ACK.toInt()),
                ))
            }
        } catch (_: IOException) {}
        tc.socket.close()
    }

    // -------------------------------------------------------------------------
    // SOCKS5 handshake
    // -------------------------------------------------------------------------

    private fun dialSocks5(host: String, port: Int): Socket {
        val parts = socksAddr.split(":")
        val socksHost = parts[0]
        val socksPort = if (parts.size > 1) parts[1].toInt() else SOCKS5_PORT_DEFAULT

        val s = Socket()
        protect(s.fd())
        s.connect(InetSocketAddress(socksHost, socksPort), 10_000)
        s.soTimeout = 60_000

        val out = s.getOutputStream()
        val ins = s.getInputStream()

        // Greeting: no-auth.
        out.write(byteArrayOf(0x05, 0x01, 0x00))
        val resp = ins.readNBytes(2)
        check(resp[0] == 0x05.toByte() && resp[1] == 0x00.toByte()) { "SOCKS5 auth failed" }

        // CONNECT request.
        val req = buildSocks5ConnectRequest(host, port)
        out.write(req)

        // Read response header (4 bytes).
        val hdr = ins.readNBytes(4)
        check(hdr[1] == 0x00.toByte()) { "SOCKS5 connect refused (code ${hdr[1]})" }

        // Skip bound address.
        when (hdr[3].toInt()) {
            0x01 -> ins.readNBytes(6)  // IPv4 + port
            0x03 -> ins.readNBytes(ins.read() + 2) // domain + port
            0x04 -> ins.readNBytes(18) // IPv6 + port
        }

        return s
    }

    private fun buildSocks5ConnectRequest(host: String, port: Int): ByteArray {
        val ip = InetAddress.getByName(host).address
        return if (ip.size == 4) {
            byteArrayOf(0x05, 0x01, 0x00, 0x01) + ip +
                byteArrayOf((port shr 8).toByte(), port.toByte())
        } else {
            byteArrayOf(0x05, 0x01, 0x00, 0x03, host.length.toByte()) +
                host.toByteArray() +
                byteArrayOf((port shr 8).toByte(), port.toByte())
        }
    }

    private fun Socket.fd(): Int {
        return try {
            val implField = Socket::class.java.getDeclaredField("impl")
            implField.isAccessible = true
            val impl = implField.get(this)
            val fdField = impl.javaClass.superclass!!.getDeclaredField("fd")
            fdField.isAccessible = true
            val fd = fdField.get(impl) as FileDescriptor
            val intField = FileDescriptor::class.java.getDeclaredField("descriptor")
            intField.isAccessible = true
            intField.getInt(fd)
        } catch (_: Exception) { -1 }
    }

    // -------------------------------------------------------------------------
    // Packet construction
    // -------------------------------------------------------------------------

    private fun buildTcpPacket(
        srcIp: Int, dstIp: Int,
        srcPort: Short, dstPort: Short,
        seq: Long, ack: Long,
        flags: Int,
        payload: ByteArray = ByteArray(0),
    ): ByteArray {
        val ipHdrLen  = 20
        val tcpHdrLen = 20
        val total     = ipHdrLen + tcpHdrLen + payload.size
        val buf       = ByteBuffer.allocate(total).order(ByteOrder.BIG_ENDIAN)

        // IPv4 header.
        buf.put(0x45.toByte())           // version=4, IHL=5
        buf.put(0)                        // DSCP/ECN
        buf.putShort(total.toShort())
        buf.putShort(0)                   // ID
        buf.putShort(0x4000.toShort())    // DF flag
        buf.put(64)                       // TTL
        buf.put(6)                        // TCP
        buf.putShort(0)                   // checksum placeholder
        buf.putInt(srcIp)
        buf.putInt(dstIp)

        // Compute IP checksum over the 20-byte header.
        val raw = buf.array()
        val ipCsum = ipChecksum(raw, 0, ipHdrLen)
        buf.putShort(10, ipCsum)

        // TCP header.
        buf.putShort(srcPort)
        buf.putShort(dstPort)
        buf.putInt((seq and 0xFFFFFFFFL).toInt())
        buf.putInt((ack and 0xFFFFFFFFL).toInt())
        buf.put(((tcpHdrLen / 4) shl 4).toByte())
        buf.put(flags.toByte())
        buf.putShort(-1)                   // window = 65535
        buf.putShort(0)                    // checksum placeholder
        buf.putShort(0)                    // urgent pointer
        buf.put(payload)

        // Compute TCP checksum.
        val tcpCsum = tcpChecksum(raw, srcIp, dstIp, ipHdrLen, tcpHdrLen + payload.size)
        buf.putShort(ipHdrLen + 16, tcpCsum)

        return raw
    }

    /** One's complement checksum for the IPv4 header. */
    private fun ipChecksum(data: ByteArray, offset: Int, length: Int): Short {
        var sum = 0
        var i = offset
        while (i < offset + length - 1) {
            val word = ((data[i].toInt() and 0xFF) shl 8) or (data[i + 1].toInt() and 0xFF)
            sum += word
            i += 2
        }
        while (sum shr 16 != 0) sum = (sum and 0xFFFF) + (sum shr 16)
        return (sum.inv() and 0xFFFF).toShort()
    }

    /** TCP checksum over the pseudo-header + TCP segment. */
    private fun tcpChecksum(
        buf: ByteArray,
        srcIp: Int, dstIp: Int,
        tcpOffset: Int, tcpLength: Int,
    ): Short {
        var sum = 0

        // Pseudo-header.
        sum += (srcIp ushr 16) and 0xFFFF
        sum += srcIp and 0xFFFF
        sum += (dstIp ushr 16) and 0xFFFF
        sum += dstIp and 0xFFFF
        sum += 6          // protocol TCP
        sum += tcpLength

        // TCP segment.
        var i = tcpOffset
        while (i < tcpOffset + tcpLength - 1) {
            val word = ((buf[i].toInt() and 0xFF) shl 8) or (buf[i + 1].toInt() and 0xFF)
            sum += word
            i += 2
        }
        if (tcpLength % 2 != 0) {
            sum += (buf[tcpOffset + tcpLength - 1].toInt() and 0xFF) shl 8
        }

        while (sum shr 16 != 0) sum = (sum and 0xFFFF) + (sum shr 16)
        return (sum.inv() and 0xFFFF).toShort()
    }

    private fun intToBytes(v: Int): ByteArray =
        byteArrayOf((v ushr 24).toByte(), (v ushr 16).toByte(), (v ushr 8).toByte(), v.toByte())
}
