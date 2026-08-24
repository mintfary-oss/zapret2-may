// TLS ClientHello parser.
// Locates the SNI (Server Name Indication) extension within a raw
// TLS record so bypass strategies can split the stream at the exact
// byte where the DPI box performs hostname extraction.
package bypass

import (
	"encoding/binary"
	"errors"
)

// TLSInfo holds offsets extracted from a TLS ClientHello record.
type TLSInfo struct {
	// RecordEnd is the total length of the TLS record (5-byte header +
	// fragment length).
	RecordEnd int
	// SNIOffset is the byte offset of the first character of the SNI
	// hostname within the raw record bytes (0 = not found).
	SNIOffset int
	// SNI is the server name extracted from the extension.
	SNI string
	// HasECH is true when the ClientHello contains the
	// encrypted_client_hello extension (type 0xFE0D, RFC 9601).
	// When HasECH is true the outer ClientHello carries only a cover
	// domain — the real SNI is already encrypted inside the ECH inner
	// ClientHello.  DPI bypass strategies are unnecessary and may
	// corrupt the handshake; the relay should forward bytes unmodified.
	HasECH bool
}

const (
	tlsRecordTypeHandshake  = 0x16
	tlsHandshakeClientHello = 0x01
	tlsExtSNI               = 0x0000
	// tlsExtECH is the IANA-assigned type for encrypted_client_hello
	// (RFC 9601, formerly draft-ietf-tls-esni). Chrome and Firefox
	// 2024+ send this extension when the server advertises ECH support
	// via its DNS HTTPS record.
	tlsExtECH = 0xFE0D
)

// ParseClientHello attempts to parse buf as a TLS ClientHello record and
// returns information useful for bypass strategies.  It returns an error
// if buf is not a valid TLS ClientHello; partial records are handled
// gracefully (SNIOffset will be 0 if extensions were not reached).
func ParseClientHello(buf []byte) (*TLSInfo, error) {
	info := &TLSInfo{}

	// ---- TLS record header (5 bytes) ----
	if len(buf) < 5 {
		return nil, errors.New("tls: record too short")
	}
	if buf[0] != tlsRecordTypeHandshake {
		return nil, errors.New("tls: not a handshake record")
	}
	recLen := int(binary.BigEndian.Uint16(buf[3:5]))
	info.RecordEnd = 5 + recLen

	// ---- Handshake header (4 bytes) ----
	if len(buf) < 9 {
		return info, nil // partial
	}
	if buf[5] != tlsHandshakeClientHello {
		return nil, errors.New("tls: not a ClientHello")
	}

	// ---- ClientHello body ----
	// skip: msg_type(1) + length(3) + client_version(2) + random(32)
	pos := 5 + 4 + 2 + 32

	// session_id
	if len(buf) < pos+1 {
		return info, nil
	}
	pos += 1 + int(buf[pos])

	// cipher_suites
	if len(buf) < pos+2 {
		return info, nil
	}
	pos += 2 + int(binary.BigEndian.Uint16(buf[pos:pos+2]))

	// compression_methods
	if len(buf) < pos+1 {
		return info, nil
	}
	pos += 1 + int(buf[pos])

	// extensions length
	if len(buf) < pos+2 {
		return info, nil
	}
	extEnd := pos + 2 + int(binary.BigEndian.Uint16(buf[pos:pos+2]))
	pos += 2

	// Walk extensions looking for SNI (0x0000) and ECH (0xFE0D).
	for pos+4 <= extEnd && pos+4 <= len(buf) {
		extType := binary.BigEndian.Uint16(buf[pos : pos+2])
		extLen := int(binary.BigEndian.Uint16(buf[pos+2 : pos+4]))
		extDataStart := pos + 4

		switch extType {
		case tlsExtSNI:
			if extDataStart+extLen <= len(buf) && extLen >= 5 {
				// SNI list: list_length(2) + name_type(1) + name_length(2) + name
				nameLen := int(binary.BigEndian.Uint16(buf[extDataStart+3 : extDataStart+5]))
				nameStart := extDataStart + 5
				if nameStart+nameLen <= len(buf) {
					info.SNI = string(buf[nameStart : nameStart+nameLen])
					info.SNIOffset = nameStart
				}
			}
		case tlsExtECH:
			// encrypted_client_hello (RFC 9601) — real SNI is already
			// encrypted; bypass strategies must not be applied.
			info.HasECH = true
		}

		pos = extDataStart + extLen
	}

	return info, nil
}

// SplitPosition returns the recommended byte offset at which to split the
// ClientHello stream for maximum DPI confusion.
//
//   - If the SNI field was found: split 1 byte before the SNI hostname start
//     (separates the SNI length field from the hostname data).
//   - Otherwise: fall back to defaultPos (caller's configured value).
func SplitPosition(info *TLSInfo, defaultPos int) int {
	if info != nil && info.SNIOffset > 1 {
		return info.SNIOffset - 1
	}
	if defaultPos > 0 {
		return defaultPos
	}
	return 2 // safe default: after the TLS record type byte
}
