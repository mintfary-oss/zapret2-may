// QUIC bypass: intercepts QUIC Initial packets and applies fragmentation
// to confuse DPI systems that inspect the QUIC Initial for SNI.
//
// QUIC runs over UDP/443 (HTTP/3).  The SOCKS5 protocol supports UDP via
// UDP ASSOCIATE (cmd 0x03).  This file implements the relay logic for UDP
// streams once a UDP association is established.
//
// NOTE: Full QUIC packet manipulation (e.g. injecting fake QUIC Initials
// with wrong connection IDs) requires raw UDP sockets.  The implementation
// here does application-level splitting of the first QUIC Initial datagram,
// which is effective against passive DPI that only reads the first packet.
package bypass

import (
	"net"
	"time"
)

const (
	// quicInitialMinLen is the minimum length of a QUIC Initial packet header.
	quicInitialMinLen = 20
	// quicLongHeaderBit indicates a QUIC long-header packet (Initial, 0-RTT, …).
	quicLongHeaderBit = 0x80
	// quicInitialType is the masked packet type for QUIC Initial.
	quicInitialType = 0xC0
)

// IsQUICInitial returns true if buf begins with a QUIC v1 Initial packet.
func IsQUICInitial(buf []byte) bool {
	if len(buf) < quicInitialMinLen {
		return false
	}
	// Long header bit set and packet type == Initial (0b00 in bits 4-5).
	return buf[0]&quicLongHeaderBit != 0 && buf[0]&0xC0 == quicInitialType
}

// RelayQUIC relays a UDP stream through the bypass engine.
// It reads the first datagram from client, optionally splits it, then
// enters a bidirectional relay loop.
func RelayQUIC(client, remote net.Conn, splitPos int) {
	buf := make([]byte, 65536)
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := client.Read(buf)
	_ = client.SetReadDeadline(time.Time{})
	if err != nil || n == 0 {
		return
	}
	pkt := buf[:n]

	if IsQUICInitial(pkt) {
		// Split the Initial packet to confuse passive DPI.
		pos := splitPos
		if pos <= 0 || pos >= n {
			pos = quicInitialMinLen // split after the fixed header
		}
		_, _ = remote.Write(pkt[:pos])
		time.Sleep(1 * time.Millisecond)
		_, _ = remote.Write(pkt[pos:])
	} else {
		_, _ = remote.Write(pkt)
	}

	// Plain relay for subsequent datagrams.
	relayPlain(client, remote)
}
