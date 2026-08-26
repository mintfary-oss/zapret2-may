//go:build linux

// Tests for the Linux-only parseIPv4TCP helper and NFQueueServer.
package proxy

import (
	"encoding/binary"
	"testing"

	"github.com/mintfary-oss/freenet/internal/bypass"
	"github.com/mintfary-oss/freenet/internal/config"
)

// ---------------------------------------------------------------------------
// parseIPv4TCP
// ---------------------------------------------------------------------------

// buildMinimalIPv4TCP constructs a valid IPv4+TCP packet with the given payload.
func buildMinimalIPv4TCP(srcIP, dstIP [4]byte, srcPort, dstPort uint16, payload []byte) []byte {
	ihl := 20
	dataOffset := 20
	total := ihl + dataOffset + len(payload)

	pkt := make([]byte, total)

	// IP header
	pkt[0] = 0x45 // version + IHL=5
	binary.BigEndian.PutUint16(pkt[2:], uint16(total))
	pkt[8] = 64   // TTL
	pkt[9] = 0x06 // TCP
	copy(pkt[12:16], srcIP[:])
	copy(pkt[16:20], dstIP[:])

	// TCP header (starts at byte 20)
	tcp := pkt[20:]
	binary.BigEndian.PutUint16(tcp[0:], srcPort)
	binary.BigEndian.PutUint16(tcp[2:], dstPort)
	binary.BigEndian.PutUint32(tcp[4:], 1000) // seq
	tcp[12] = 0x50                            // data offset = 5*4 = 20 bytes
	copy(tcp[20:], payload)

	return pkt
}

func TestParseIPv4TCP_ValidPacket(t *testing.T) {
	payload := []byte("hello")
	srcIP := [4]byte{1, 2, 3, 4}
	dstIP := [4]byte{5, 6, 7, 8}
	raw := buildMinimalIPv4TCP(srcIP, dstIP, 54321, 443, payload)

	pkt, ok := parseIPv4TCP(raw)
	if !ok {
		t.Fatal("parseIPv4TCP: expected ok=true")
	}
	if pkt.srcPort != 54321 {
		t.Errorf("srcPort = %d, want 54321", pkt.srcPort)
	}
	if pkt.dstPort != 443 {
		t.Errorf("dstPort = %d, want 443", pkt.dstPort)
	}
	if pkt.seqNum != 1000 {
		t.Errorf("seqNum = %d, want 1000", pkt.seqNum)
	}
	if string(pkt.payload) != "hello" {
		t.Errorf("payload = %q, want %q", pkt.payload, "hello")
	}
}

func TestParseIPv4TCP_TooShort(t *testing.T) {
	cases := [][]byte{
		nil,
		{},
		make([]byte, 5),
		make([]byte, 19),
	}
	for _, raw := range cases {
		_, ok := parseIPv4TCP(raw)
		if ok {
			t.Errorf("parseIPv4TCP(%d bytes): expected ok=false", len(raw))
		}
	}
}

func TestParseIPv4TCP_TruncatedTCPHeader(t *testing.T) {
	// IP header only, no room for TCP header.
	raw := make([]byte, 20)
	raw[0] = 0x45 // IHL=5
	_, ok := parseIPv4TCP(raw)
	if ok {
		t.Error("parseIPv4TCP: expected ok=false for packet with no TCP header")
	}
}

func TestParseIPv4TCP_BadDataOffset(t *testing.T) {
	// Build a packet where the TCP data offset points beyond the end.
	raw := make([]byte, 40)
	raw[0] = 0x45 // IHL=5 (20 bytes)
	tcp := raw[20:]
	tcp[12] = 0xF0 // data offset = 15 * 4 = 60 bytes (too large for 20-byte TCP)
	_, ok := parseIPv4TCP(raw)
	if ok {
		t.Error("parseIPv4TCP: expected ok=false for bad data offset")
	}
}

func TestParseIPv4TCP_EmptyPayload(t *testing.T) {
	srcIP := [4]byte{127, 0, 0, 1}
	dstIP := [4]byte{127, 0, 0, 2}
	raw := buildMinimalIPv4TCP(srcIP, dstIP, 1000, 2000, nil)

	pkt, ok := parseIPv4TCP(raw)
	if !ok {
		t.Fatal("parseIPv4TCP: expected ok=true for empty payload")
	}
	if len(pkt.payload) != 0 {
		t.Errorf("payload len = %d, want 0", len(pkt.payload))
	}
}

// ---------------------------------------------------------------------------
// NFQueueServer construction
// ---------------------------------------------------------------------------

func TestNewNFQueueServer_NotNil(t *testing.T) {
	cfg := &config.Config{NFQueue: config.NFQueueConfig{Enabled: false}}
	eng := bypass.NewEngine(&config.Config{
		Bypass: config.BypassConfig{Strategy: "split"},
	})
	s := NewNFQueueServer(cfg, eng)
	if s == nil {
		t.Fatal("NewNFQueueServer returned nil")
	}
}

func TestNFQueueServer_StartDisabled(t *testing.T) {
	cfg := &config.Config{NFQueue: config.NFQueueConfig{Enabled: false}}
	eng := bypass.NewEngine(&config.Config{
		Bypass: config.BypassConfig{Strategy: "split"},
	})
	s := NewNFQueueServer(cfg, eng)
	// Start should be a no-op when NFQueue is disabled.
	if err := s.Start(); err != nil {
		t.Errorf("Start() with NFQueue disabled: %v", err)
	}
	s.Stop() // must not panic
}

func TestNFQueueServer_SetEnabled(t *testing.T) {
	cfg := &config.Config{NFQueue: config.NFQueueConfig{Enabled: false}}
	eng := bypass.NewEngine(&config.Config{
		Bypass: config.BypassConfig{Strategy: "split"},
	})
	s := NewNFQueueServer(cfg, eng)
	s.SetEnabled(true)
	s.SetEnabled(false)
}
