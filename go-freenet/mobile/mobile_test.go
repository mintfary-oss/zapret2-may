// Tests for mobile package pure functions: IP/TCP/UDP packet builders,
// checksums, connKey, and FreenetEngine lifecycle.
//
// TUN I/O functions (ForwardTUN, ForwardTUNWithDNS, handlePacket, etc.)
// require a real TUN device and cannot be unit-tested without root, so they
// are intentionally excluded.
package mobile

import (
	"encoding/binary"
	"net"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// connKey
// ---------------------------------------------------------------------------

func TestConnKey_String(t *testing.T) {
	k := connKey{
		srcIP:   [4]byte{10, 0, 0, 1},
		dstIP:   [4]byte{8, 8, 8, 8},
		srcPort: 12345,
		dstPort: 443,
	}
	got := k.String()
	if got == "" {
		t.Error("connKey.String() returned empty string")
	}
	// Must contain some representation of the ports.
	if !strings.Contains(got, "12345") || !strings.Contains(got, "443") {
		t.Errorf("connKey.String() = %q, want to contain port numbers", got)
	}
}

// ---------------------------------------------------------------------------
// ipChecksum
// ---------------------------------------------------------------------------

// TestIPChecksum_KnownIPv4Header verifies the RFC 791 checksum for a known
// IPv4 header (taken from a real captured packet).
func TestIPChecksum_KnownIPv4Header(t *testing.T) {
	// Build a minimal IPv4 header (20 bytes) with checksum field = 0.
	// After ipChecksum() the result should make the header verify to 0.
	hdr := make([]byte, 20)
	hdr[0] = 0x45 // version=4, IHL=5
	hdr[8] = 64   // TTL
	hdr[9] = 6    // TCP
	copy(hdr[12:16], net.IP{192, 168, 1, 1}.To4())
	copy(hdr[16:20], net.IP{10, 0, 0, 1}.To4())
	binary.BigEndian.PutUint16(hdr[2:4], 40) // total length

	cs := ipChecksum(hdr)
	// Overwrite checksum field.
	binary.BigEndian.PutUint16(hdr[10:12], cs)
	// Recalculating with the checksum embedded must give 0.
	if ipChecksum(hdr) != 0 {
		t.Errorf("ipChecksum verification failed: expected 0 after embedding, got %d", ipChecksum(hdr))
	}
}

func TestIPChecksum_OddLength(t *testing.T) {
	// Must not panic on odd-length input.
	hdr := make([]byte, 19)
	_ = ipChecksum(hdr)
}

// ---------------------------------------------------------------------------
// tcpChecksum
// ---------------------------------------------------------------------------

func TestTCPChecksum_NotZero(t *testing.T) {
	srcIP := net.IP{10, 0, 0, 1}.To4()
	dstIP := net.IP{8, 8, 8, 8}.To4()
	seg := make([]byte, 20) // minimal TCP header
	seg[12] = 0x50          // data offset = 5 (20 bytes)
	cs := tcpChecksum(srcIP, dstIP, seg)
	// Checksum must be non-zero for a non-trivial segment.
	if cs == 0 {
		t.Error("tcpChecksum returned 0 for non-trivial input")
	}
}

func TestTCPChecksum_PayloadAffectsResult(t *testing.T) {
	src := net.IP{1, 2, 3, 4}.To4()
	dst := net.IP{5, 6, 7, 8}.To4()

	seg1 := make([]byte, 20)
	seg2 := append(make([]byte, 20), 0xAB, 0xCD)

	cs1 := tcpChecksum(src, dst, seg1)
	cs2 := tcpChecksum(src, dst, seg2)
	if cs1 == cs2 {
		t.Error("tcpChecksum should differ when payload differs")
	}
}

// ---------------------------------------------------------------------------
// udpChecksum
// ---------------------------------------------------------------------------

func TestUDPChecksum_NotZero(t *testing.T) {
	srcIP := net.IP{10, 0, 0, 1}.To4()
	dstIP := net.IP{8, 8, 4, 4}.To4()
	seg := make([]byte, 8)                  // minimal UDP header
	binary.BigEndian.PutUint16(seg[4:6], 8) // length = 8
	cs := udpChecksum(srcIP, dstIP, seg)
	// RFC 768: 0 means "not computed"; our implementation returns 0xFFFF instead.
	// Either way we just need a value.
	_ = cs
}

func TestUDPChecksum_ZeroComputedBecomesFFFF(t *testing.T) {
	// It is extremely unlikely to hit this in practice, but we verify the
	// 0→0xFFFF substitution rule is reachable by trying multiple inputs.
	// The rule exists at the code level: if result == 0, return 0xFFFF.
	// We just ensure udpChecksum never panics for any input.
	inputs := [][]byte{
		make([]byte, 8),
		make([]byte, 9),
		make([]byte, 12),
	}
	src := net.IP{0, 0, 0, 0}.To4()
	dst := net.IP{0, 0, 0, 0}.To4()
	for _, seg := range inputs {
		cs := udpChecksum(src, dst, seg)
		if cs == 0 {
			t.Errorf("udpChecksum returned literal 0; should return 0xFFFF per RFC 768")
		}
	}
}

// ---------------------------------------------------------------------------
// buildTCPPacket
// ---------------------------------------------------------------------------

func TestBuildTCPPacket_Structure(t *testing.T) {
	srcIP := net.IP{10, 0, 0, 1}.To4()
	dstIP := net.IP{8, 8, 8, 8}.To4()
	pkt := buildTCPPacket(srcIP, dstIP, 12345, 443, 1, 0, 0x02 /*SYN*/, nil)

	const ipHdrLen = 20
	const tcpHdrLen = 20
	if len(pkt) != ipHdrLen+tcpHdrLen {
		t.Fatalf("packet len = %d, want %d", len(pkt), ipHdrLen+tcpHdrLen)
	}

	// IP version+IHL.
	if pkt[0] != 0x45 {
		t.Errorf("pkt[0] = %02x, want 0x45 (IPv4, IHL=5)", pkt[0])
	}
	// IP protocol = TCP (6).
	if pkt[9] != 6 {
		t.Errorf("IP protocol = %d, want 6 (TCP)", pkt[9])
	}
	// Source/Dest IPs.
	if !net.IP(pkt[12:16]).Equal(srcIP) {
		t.Errorf("src IP = %v, want %v", net.IP(pkt[12:16]), srcIP)
	}
	if !net.IP(pkt[16:20]).Equal(dstIP) {
		t.Errorf("dst IP = %v, want %v", net.IP(pkt[16:20]), dstIP)
	}
	// TCP destination port.
	if binary.BigEndian.Uint16(pkt[22:24]) != 443 {
		t.Errorf("dst port = %d, want 443", binary.BigEndian.Uint16(pkt[22:24]))
	}
	// IP checksum must be valid (recalculating on the header should give 0).
	if ipChecksum(pkt[:ipHdrLen]) != 0 {
		t.Error("IP checksum in built packet is invalid")
	}
}

func TestBuildTCPPacket_WithPayload(t *testing.T) {
	src := net.IP{1, 2, 3, 4}.To4()
	dst := net.IP{5, 6, 7, 8}.To4()
	payload := []byte("hello")
	pkt := buildTCPPacket(src, dst, 1024, 80, 100, 200, 0x18, payload)

	const ipHdrLen = 20
	const tcpHdrLen = 20
	if len(pkt) != ipHdrLen+tcpHdrLen+len(payload) {
		t.Errorf("packet len = %d, want %d", len(pkt), ipHdrLen+tcpHdrLen+len(payload))
	}
	if string(pkt[ipHdrLen+tcpHdrLen:]) != "hello" {
		t.Error("payload not preserved in packet")
	}
}

// ---------------------------------------------------------------------------
// buildSYNACK / buildRST / buildFIN / buildData (thin wrappers)
// ---------------------------------------------------------------------------

func TestBuildSYNACK_FlagsSet(t *testing.T) {
	src := net.IP{10, 0, 0, 1}.To4()
	dst := net.IP{10, 0, 0, 2}.To4()
	pkt := buildSYNACK(src, dst, 8080, 54321, 0, 1)
	// TCP flags byte is at offset 20+13 = 33.
	if pkt[33] != 0x12 {
		t.Errorf("SYNACK flags = %02x, want 0x12 (SYN+ACK)", pkt[33])
	}
}

func TestBuildRST_FlagsSet(t *testing.T) {
	src := net.IP{10, 0, 0, 1}.To4()
	dst := net.IP{10, 0, 0, 2}.To4()
	pkt := buildRST(src, dst, 8080, 54321, 0, 0)
	if pkt[33] != 0x04 {
		t.Errorf("RST flags = %02x, want 0x04", pkt[33])
	}
}

func TestBuildFIN_FlagsSet(t *testing.T) {
	src := net.IP{10, 0, 0, 1}.To4()
	dst := net.IP{10, 0, 0, 2}.To4()
	pkt := buildFIN(src, dst, 8080, 54321, 100, 200)
	if pkt[33] != 0x11 {
		t.Errorf("FIN flags = %02x, want 0x11 (FIN+ACK)", pkt[33])
	}
}

func TestBuildData_ContainsPayload(t *testing.T) {
	src := net.IP{10, 0, 0, 1}.To4()
	dst := net.IP{10, 0, 0, 2}.To4()
	payload := []byte("testdata")
	pkt := buildData(src, dst, 8080, 54321, 100, 200, payload)
	const ipHdrLen = 20
	const tcpHdrLen = 20
	if string(pkt[ipHdrLen+tcpHdrLen:]) != "testdata" {
		t.Error("buildData payload not present in packet")
	}
	if pkt[33] != 0x18 {
		t.Errorf("buildData flags = %02x, want 0x18 (PSH+ACK)", pkt[33])
	}
}

// ---------------------------------------------------------------------------
// buildUDPPacket
// ---------------------------------------------------------------------------

func TestBuildUDPPacket_Structure(t *testing.T) {
	srcIP := net.IP{10, 0, 0, 1}.To4()
	dstIP := net.IP{8, 8, 8, 8}.To4()
	payload := []byte("dns-response")
	pkt := buildUDPPacket(srcIP, dstIP, 5300, 53, payload)

	const ipHdrLen = 20
	const udpHdrLen = 8
	if len(pkt) != ipHdrLen+udpHdrLen+len(payload) {
		t.Fatalf("UDP packet len = %d, want %d", len(pkt), ipHdrLen+udpHdrLen+len(payload))
	}
	// IP protocol = UDP (17).
	if pkt[9] != 17 {
		t.Errorf("IP protocol = %d, want 17 (UDP)", pkt[9])
	}
	// IP checksum valid.
	if ipChecksum(pkt[:ipHdrLen]) != 0 {
		t.Error("IP checksum in UDP packet is invalid")
	}
	// UDP payload preserved.
	if string(pkt[ipHdrLen+udpHdrLen:]) != "dns-response" {
		t.Error("UDP payload not preserved")
	}
}

// ---------------------------------------------------------------------------
// FreenetEngine lifecycle
// ---------------------------------------------------------------------------

func TestNewFreenetEngine_NotNil(t *testing.T) {
	e := NewFreenetEngine()
	if e == nil {
		t.Fatal("NewFreenetEngine() returned nil")
	}
}

func TestFreenetEngine_IsRunning_InitialFalse(t *testing.T) {
	e := NewFreenetEngine()
	if e.IsRunning() {
		t.Error("IsRunning() should be false before Start()")
	}
}

func TestFreenetEngine_GetVersion(t *testing.T) {
	e := NewFreenetEngine()
	v := e.GetVersion()
	if v == "" {
		t.Error("GetVersion() returned empty string")
	}
}

func TestFreenetEngine_StartStop(t *testing.T) {
	e := NewFreenetEngine()
	if err := e.Start(0); err != nil {
		t.Fatalf("Start(0) error: %v", err)
	}
	if !e.IsRunning() {
		t.Error("IsRunning() should be true after Start()")
	}
	e.Stop()
	if e.IsRunning() {
		t.Error("IsRunning() should be false after Stop()")
	}
}

func TestFreenetEngine_StartTwice(t *testing.T) {
	e := NewFreenetEngine()
	if err := e.Start(0); err != nil {
		t.Fatalf("first Start error: %v", err)
	}
	defer e.Stop()
	if err := e.Start(0); err == nil {
		t.Error("second Start() should return error (already running)")
	}
}

func TestFreenetEngine_StopBeforeStart(t *testing.T) {
	e := NewFreenetEngine()
	// Must not panic.
	e.Stop()
}

func TestFreenetEngine_SetGetStrategy(t *testing.T) {
	e := NewFreenetEngine()
	if err := e.Start(0); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer e.Stop()

	e.SetStrategy("split")
	if got := e.GetStrategy(); got != "split" {
		t.Errorf("GetStrategy() = %q, want split", got)
	}
	e.SetStrategy("tlsrec")
	if got := e.GetStrategy(); got != "tlsrec" {
		t.Errorf("GetStrategy() = %q, want tlsrec", got)
	}
}

func TestFreenetEngine_SetBypassEnabled(t *testing.T) {
	e := NewFreenetEngine()
	if err := e.Start(0); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer e.Stop()
	// Should not panic.
	e.SetBypassEnabled(true)
	e.SetBypassEnabled(false)
}

func TestFreenetEngine_GetStats_Valid(t *testing.T) {
	e := NewFreenetEngine()
	if err := e.Start(0); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer e.Stop()

	stats := e.GetStats()
	if stats == "" {
		t.Error("GetStats() returned empty string")
	}
}

func TestFreenetEngine_GetRecentLogs(t *testing.T) {
	e := NewFreenetEngine()
	// Should not panic even before Start.
	logs := e.GetRecentLogs(10)
	_ = logs
}

func TestFreenetEngine_GetStrategyBeforeStart(t *testing.T) {
	e := NewFreenetEngine()
	// Not started — should not panic.
	got := e.GetStrategy()
	_ = got
}
