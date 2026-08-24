package bypass

import (
	"testing"
)

// buildClientHello constructs a minimal but spec-compliant TLS ClientHello
// record for testing purposes.  When sni is non-empty the SNI extension is
// included; when addECH is true the encrypted_client_hello extension
// (type 0xFE0D) is appended after the SNI.
func buildClientHello(sni string, addECH bool) []byte {
	// Extensions
	var exts []byte
	if sni != "" {
		nameBytes := []byte(sni)
		extBody := concat16(
			u16be(uint16(1+2+len(nameBytes))), // server_name_list length
			[]byte{0x00},                      // name_type: host_name
			u16be(uint16(len(nameBytes))),     // name length
			nameBytes,
		)
		exts = append(exts, concat16(
			[]byte{0x00, 0x00}, // extension type: SNI
			u16be(uint16(len(extBody))),
			extBody,
		)...)
	}
	if addECH {
		echPayload := []byte{0xAA, 0xBB, 0xCC, 0xDD} // opaque ECH data
		exts = append(exts, concat16(
			[]byte{0xFE, 0x0D}, // extension type: ECH
			u16be(uint16(len(echPayload))),
			echPayload,
		)...)
	}

	// ClientHello body
	body := concat16(
		[]byte{0x03, 0x03},       // legacy_version TLS 1.2
		make([]byte, 32),         // random (32 zero bytes)
		[]byte{0x00},             // session_id length: 0
		[]byte{0x00, 0x02},       // cipher_suites length: 2
		[]byte{0x13, 0x01},       // TLS_AES_128_GCM_SHA256
		[]byte{0x01, 0x00},       // compression_methods: 1, null
		u16be(uint16(len(exts))), // extensions length
		exts,
	)

	// Handshake header: msg_type(1) + length(3)
	handshake := concat16(
		[]byte{0x01},             // ClientHello
		u24be(uint32(len(body))), // 3-byte length
		body,
	)

	// TLS record header: content_type(1) + legacy_version(2) + length(2)
	return concat16(
		[]byte{0x16, 0x03, 0x01}, // handshake, TLS 1.0 legacy
		u16be(uint16(len(handshake))),
		handshake,
	)
}

// --- helpers ----------------------------------------------------------------

func u16be(v uint16) []byte { return []byte{byte(v >> 8), byte(v)} }
func u24be(v uint32) []byte { return []byte{byte(v >> 16), byte(v >> 8), byte(v)} }

func concat16(slices ...[]byte) []byte {
	var total int
	for _, s := range slices {
		total += len(s)
	}
	out := make([]byte, 0, total)
	for _, s := range slices {
		out = append(out, s...)
	}
	return out
}

// --- tests ------------------------------------------------------------------

func TestParseClientHello_SNI(t *testing.T) {
	want := "youtube.com"
	buf := buildClientHello(want, false)

	info, err := ParseClientHello(buf)
	if err != nil {
		t.Fatalf("ParseClientHello returned error: %v", err)
	}
	if info == nil {
		t.Fatal("ParseClientHello returned nil TLSInfo")
	}
	if info.SNI != want {
		t.Errorf("SNI = %q, want %q", info.SNI, want)
	}
	if info.SNIOffset == 0 {
		t.Error("SNIOffset should be > 0 when SNI is present")
	}
	if info.HasECH {
		t.Error("HasECH should be false when no ECH extension is present")
	}
}

func TestParseClientHello_ECH(t *testing.T) {
	buf := buildClientHello("cloudflare.com", true /* addECH */)

	info, err := ParseClientHello(buf)
	if err != nil {
		t.Fatalf("ParseClientHello returned error: %v", err)
	}
	if info == nil {
		t.Fatal("ParseClientHello returned nil TLSInfo")
	}
	if !info.HasECH {
		t.Error("HasECH should be true when ECH extension is present")
	}
	// SNI should also still be parsed when present alongside ECH.
	if info.SNI != "cloudflare.com" {
		t.Errorf("SNI = %q, want %q", info.SNI, "cloudflare.com")
	}
}

func TestParseClientHello_ECHOnly(t *testing.T) {
	// Build a ClientHello with ECH but no SNI (the cover domain case).
	buf := buildClientHello("", true /* addECH */)

	info, err := ParseClientHello(buf)
	if err != nil {
		t.Fatalf("ParseClientHello error: %v", err)
	}
	if !info.HasECH {
		t.Error("HasECH should be true")
	}
	if info.SNI != "" {
		t.Errorf("SNI = %q, want empty (no SNI extension)", info.SNI)
	}
}

func TestParseClientHello_Short(t *testing.T) {
	_, err := ParseClientHello([]byte{0x16, 0x03}) // too short
	if err == nil {
		t.Error("expected error for truncated input, got nil")
	}
}

func TestParseClientHello_NotHandshake(t *testing.T) {
	buf := make([]byte, 64)
	buf[0] = 0x17 // application_data, not handshake
	_, err := ParseClientHello(buf)
	if err == nil {
		t.Error("expected error for non-handshake record")
	}
}

func TestSplitPosition_WithSNI(t *testing.T) {
	buf := buildClientHello("www.instagram.com", false)
	info, err := ParseClientHello(buf)
	if err != nil || info == nil || info.SNIOffset == 0 {
		t.Fatalf("could not obtain TLSInfo: err=%v info=%+v", err, info)
	}
	pos := SplitPosition(info, 2)
	// Split position should be 1 byte before the SNI hostname starts,
	// separating the SNI length field from the hostname data.
	if pos != info.SNIOffset-1 {
		t.Errorf("SplitPosition = %d, want %d (SNIOffset-1)", pos, info.SNIOffset-1)
	}
}

func TestSplitPosition_Fallback(t *testing.T) {
	pos := SplitPosition(nil, 5)
	if pos != 5 {
		t.Errorf("SplitPosition with nil info = %d, want 5 (default)", pos)
	}
}

func TestSplitPosition_Default(t *testing.T) {
	// When both info has no SNI and defaultPos is 0, expect safe default 2.
	pos := SplitPosition(nil, 0)
	if pos != 2 {
		t.Errorf("SplitPosition nil/0 = %d, want 2", pos)
	}
}
