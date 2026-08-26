//go:build linux

package bypass

import (
	"encoding/binary"
	"net"
	"testing"
)

// TestOnesComplementSum tests the Internet checksum (RFC 1071).
func TestOnesComplementSum(t *testing.T) {
	// Empty slice should produce 0xFFFF (complement of 0).
	if got := onesComplementSum([]byte{}); got != 0xFFFF {
		t.Errorf("empty slice: got %#x, want 0xFFFF", got)
	}

	// Standard example: checksum of header with zeroed checksum field
	// should equal the 1s complement of the actual checksum.
	data := []byte{0x45, 0x00, 0x00, 0x28, 0x00, 0x01, 0x00, 0x00,
		0x40, 0x06, 0x00, 0x00, 0x7f, 0x00, 0x00, 0x01, 0x7f, 0x00, 0x00, 0x01}
	cs := onesComplementSum(data)
	if cs == 0 {
		t.Error("onesComplementSum: unexpected zero for non-zero input")
	}

	// Odd-length slice — last byte must be handled correctly (no panic).
	odd := []byte{0xAA, 0xBB, 0xCC}
	_ = onesComplementSum(odd)
}

// TestIPv4Checksum verifies that applying the checksum then re-computing gives
// the expected result (verifiable checksum = 0).
func TestIPv4Checksum(t *testing.T) {
	// Build a minimal IPv4 header with zeroed checksum field.
	hdr := make([]byte, 20)
	hdr[0] = 0x45 // version + IHL
	hdr[8] = 64   // TTL
	hdr[9] = 0x06 // TCP
	// src: 127.0.0.1, dst: 127.0.0.2
	copy(hdr[12:], []byte{127, 0, 0, 1})
	copy(hdr[16:], []byte{127, 0, 0, 2})
	binary.BigEndian.PutUint16(hdr[2:], 20) // total length

	cs := ipv4Checksum(hdr)
	if cs == 0 {
		t.Error("ipv4Checksum: checksum should not be zero")
	}

	// Write checksum back and verify: the ones-complement sum of all header
	// words (including the checksum field) wraps to all-ones (0xFFFF raw),
	// so onesComplementSum returns ^0xFFFF == 0x0000.
	binary.BigEndian.PutUint16(hdr[10:], cs)
	verify := onesComplementSum(hdr)
	if verify != 0x0000 {
		t.Errorf("ipv4Checksum verification: re-sum got %#x, want 0x0000", verify)
	}
}

// TestTCPv4Checksum verifies the TCP checksum calculation.
func TestTCPv4Checksum(t *testing.T) {
	src4 := net.ParseIP("127.0.0.1").To4()
	dst4 := net.ParseIP("127.0.0.2").To4()

	tcpHdr := make([]byte, 20)
	binary.BigEndian.PutUint16(tcpHdr[0:], 12345) // src port
	binary.BigEndian.PutUint16(tcpHdr[2:], 443)   // dst port
	tcpHdr[12] = 0x50                             // data offset

	payload := []byte("hello")
	cs := tcpv4Checksum(src4, dst4, tcpHdr, payload)
	if cs == 0 {
		t.Error("tcpv4Checksum: checksum should not be zero")
	}

	// Different payload should produce different checksum.
	cs2 := tcpv4Checksum(src4, dst4, tcpHdr, []byte("world"))
	if cs == cs2 {
		t.Error("tcpv4Checksum: different payloads should produce different checksums")
	}
}

// TestBuildIPv4TCPPacket verifies the structure of the constructed packet.
func TestBuildIPv4TCPPacket(t *testing.T) {
	srcIP := net.ParseIP("192.168.1.1")
	dstIP := net.ParseIP("8.8.8.8")
	payload := []byte("fake-hello")

	pkt := buildIPv4TCPPacket(srcIP, dstIP, 54321, 443, 0, payload, 64, false)

	// Must be exactly 20 (IP) + 20 (TCP) + len(payload) bytes.
	want := 20 + 20 + len(payload)
	if len(pkt) != want {
		t.Fatalf("packet length = %d, want %d", len(pkt), want)
	}

	// IP version + IHL
	if pkt[0] != 0x45 {
		t.Errorf("IP version/IHL = 0x%02x, want 0x45", pkt[0])
	}
	// TTL
	if pkt[8] != 64 {
		t.Errorf("TTL = %d, want 64", pkt[8])
	}
	// Protocol = TCP
	if pkt[9] != 0x06 {
		t.Errorf("Protocol = 0x%02x, want 0x06", pkt[9])
	}
	// Source IP
	gotSrc := net.IP(pkt[12:16])
	if !gotSrc.Equal(srcIP.To4()) {
		t.Errorf("src IP = %v, want %v", gotSrc, srcIP.To4())
	}
	// Destination IP
	gotDst := net.IP(pkt[16:20])
	if !gotDst.Equal(dstIP.To4()) {
		t.Errorf("dst IP = %v, want %v", gotDst, dstIP.To4())
	}
	// Src port
	gotSrcPort := binary.BigEndian.Uint16(pkt[20:22])
	if gotSrcPort != 54321 {
		t.Errorf("src port = %d, want 54321", gotSrcPort)
	}
	// Dst port
	gotDstPort := binary.BigEndian.Uint16(pkt[22:24])
	if gotDstPort != 443 {
		t.Errorf("dst port = %d, want 443", gotDstPort)
	}
}

// TestBuildIPv4TCPPacket_BadChecksum verifies that badChecksum=true flips the
// TCP checksum.
func TestBuildIPv4TCPPacket_BadChecksum(t *testing.T) {
	srcIP := net.ParseIP("10.0.0.1")
	dstIP := net.ParseIP("10.0.0.2")
	payload := []byte("data")

	good := buildIPv4TCPPacket(srcIP, dstIP, 1111, 2222, 0, payload, 64, false)
	bad := buildIPv4TCPPacket(srcIP, dstIP, 1111, 2222, 0, payload, 64, true)

	// Both packets have same length.
	if len(good) != len(bad) {
		t.Fatalf("length mismatch: good=%d bad=%d", len(good), len(bad))
	}
	// TCP checksum is at offset 36 (20 IP + 16 TCP).
	csGood := binary.BigEndian.Uint16(good[36:38])
	csBad := binary.BigEndian.Uint16(bad[36:38])
	if csGood == csBad {
		t.Error("bad checksum should differ from good checksum")
	}
	// The bad checksum should be the bitwise complement of the good one.
	if csBad != ^csGood {
		t.Errorf("bad checksum %#x is not complement of good %#x", csBad, csGood)
	}
}

// TestBuildIPv4TCPPacket_EmptyPayload verifies that empty payload is handled.
func TestBuildIPv4TCPPacket_EmptyPayload(t *testing.T) {
	srcIP := net.ParseIP("1.2.3.4")
	dstIP := net.ParseIP("5.6.7.8")
	pkt := buildIPv4TCPPacket(srcIP, dstIP, 100, 200, 42, nil, 128, false)
	if len(pkt) != 40 {
		t.Errorf("empty payload packet len = %d, want 40", len(pkt))
	}
}
