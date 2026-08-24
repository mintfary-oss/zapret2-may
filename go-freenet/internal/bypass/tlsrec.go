// TLS record layer splitting (tlsrec strategy).
//
// Unlike the "split" strategy — which fragments one TLS record across
// multiple TCP segments — tlsrec splits the TLS ClientHello into two
// separate TLS records, each with its own 5-byte header, sent inside a
// single TCP segment (or two, if TCP_NODELAY is set).
//
// Why this is more effective:
//
//  1. Stateless DPI that inspects individual TLS records never sees a
//     complete ClientHello and cannot extract the SNI.
//
//  2. Even DPI that reassembles TCP streams may parse TLS record-by-record
//     and stop after the first (incomplete) record.
//
//  3. Fully compliant: RFC 5246 §6.2.1 explicitly allows a Handshake
//     message to be split across multiple TLS records.
//
// The split position defaults to 1 byte before the SNI hostname, forcing
// the hostname to arrive in the second record.
package bypass

import (
	"encoding/binary"
	"net"
)

const (
	tlsRecordHeaderLen  = 5
	tlsContentHandshake = 0x16
	// Maximum fragment length per RFC 5246 §6.2.1.
	tlsMaxFragmentLen = 16384
)

// relayTLSRec reads the TLS ClientHello from client, splits it into two TLS
// records at the SNI boundary, and sends them to remote inside one or two TCP
// writes.  Falls back to a single send if the payload is not a valid TLS
// Handshake record.
func relayTLSRec(client, remote net.Conn, defaultPos int) {
	// Read the first chunk from the client — should be a TLS ClientHello.
	buf := make([]byte, 16384+tlsRecordHeaderLen)
	n, err := client.Read(buf)
	if err != nil || n < tlsRecordHeaderLen {
		return
	}
	raw := buf[:n]

	// Only process TLS Handshake records.
	if raw[0] != tlsContentHandshake || n < tlsRecordHeaderLen {
		// Pass through unmodified.
		_, _ = remote.Write(raw)
		relayPlain(client, remote)
		return
	}

	recordVersion := raw[1:3] // preserve the original version bytes
	payload := raw[tlsRecordHeaderLen:]
	payloadLen := len(payload)

	// Determine split position within the payload.
	info, _ := ParseClientHello(raw)
	splitPos := SplitPosition(info, defaultPos)
	if splitPos <= 0 || splitPos >= payloadLen {
		splitPos = payloadLen / 2
	}
	if splitPos <= 0 {
		// Payload too small to split — send as-is.
		_, _ = remote.Write(raw)
		relayPlain(client, remote)
		return
	}

	// Build record 1: bytes [0, splitPos).
	rec1 := buildTLSRecord(tlsContentHandshake, recordVersion, payload[:splitPos])
	// Build record 2: bytes [splitPos, payloadLen).
	rec2 := buildTLSRecord(tlsContentHandshake, recordVersion, payload[splitPos:])

	// Send both records with TCP_NODELAY to ensure separate TCP segments.
	setTCPNoDelay(remote, true)
	if _, err := remote.Write(rec1); err != nil {
		return
	}
	if _, err := remote.Write(rec2); err != nil {
		return
	}
	setTCPNoDelay(remote, false)

	relayPlain(client, remote)
}

// buildTLSRecord wraps payload in a TLS record header.
//
//	struct {
//	    ContentType type;      // 1 byte
//	    ProtocolVersion ver;   // 2 bytes
//	    uint16 length;         // 2 bytes  (big-endian)
//	    opaque fragment[length];
//	} TLSPlaintext;
func buildTLSRecord(contentType byte, version, payload []byte) []byte {
	if len(payload) > tlsMaxFragmentLen {
		payload = payload[:tlsMaxFragmentLen]
	}
	rec := make([]byte, tlsRecordHeaderLen+len(payload))
	rec[0] = contentType
	rec[1] = version[0]
	rec[2] = version[1]
	binary.BigEndian.PutUint16(rec[3:5], uint16(len(payload)))
	copy(rec[tlsRecordHeaderLen:], payload)
	return rec
}

// relayCombined applies both TLS record splitting AND fake packet injection.
// This is the most aggressive mode: the DPI sees a fake ClientHello (wrong TTL
// or bad checksum) followed by two TLS records that individually don't contain
// a complete SNI.
func relayCombined(client, remote net.Conn, cfg fakeConfig) {
	buf := make([]byte, 16384+tlsRecordHeaderLen)
	n, err := client.Read(buf)
	if err != nil || n == 0 {
		return
	}
	raw := buf[:n]

	// Send fake decoy first (if raw socket available).
	if globalFakeSender != nil {
		localTCP, remoteAddr := tcpAddrPair(remote)
		if localTCP != nil && remoteAddr != nil {
			fakeHello := buildMinimalClientHello("www.microsoft.com")
			mode := FakeModeTTL
			if cfg.MD5Fake {
				mode = FakeModeMD5
			}
			_ = globalFakeSender.sendFake(
				localTCP.IP, remoteAddr.IP,
				uint16(localTCP.Port), uint16(remoteAddr.Port),
				0, fakeHello, mode, uint8(cfg.FakeTTL),
			)
		}
	}

	// Apply TLS record splitting.
	if raw[0] != tlsContentHandshake || n < tlsRecordHeaderLen {
		_, _ = remote.Write(raw)
		relayPlain(client, remote)
		return
	}

	recordVersion := raw[1:3]
	payload := raw[tlsRecordHeaderLen:]
	payloadLen := len(payload)

	info, _ := ParseClientHello(raw)
	splitPos := SplitPosition(info, cfg.SplitPos)
	if splitPos <= 0 || splitPos >= payloadLen {
		splitPos = payloadLen / 2
	}
	if splitPos <= 0 {
		_, _ = remote.Write(raw)
		relayPlain(client, remote)
		return
	}

	rec1 := buildTLSRecord(tlsContentHandshake, recordVersion, payload[:splitPos])
	rec2 := buildTLSRecord(tlsContentHandshake, recordVersion, payload[splitPos:])

	setTCPNoDelay(remote, true)
	_, _ = remote.Write(rec1)
	_, _ = remote.Write(rec2)
	setTCPNoDelay(remote, false)

	relayPlain(client, remote)
}
