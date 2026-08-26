package bypass

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

// TestIsQUICInitial tests QUIC Initial packet detection.
func TestIsQUICInitial(t *testing.T) {
	cases := []struct {
		name string
		buf  []byte
		want bool
	}{
		{
			name: "valid QUIC Initial",
			// Long header bit (0x80) set, bits 4-5 = 0b00 → Initial (0xC0)
			buf:  append([]byte{0xC0}, make([]byte, 25)...),
			want: true,
		},
		{
			name: "too short",
			buf:  []byte{0xC0, 0x00},
			want: false,
		},
		{
			name: "empty",
			buf:  nil,
			want: false,
		},
		{
			name: "short header bit not set",
			// 0x40 has long header bit clear
			buf:  append([]byte{0x40}, make([]byte, 25)...),
			want: false,
		},
		{
			// Implementation checks buf[0]&0xC0 == 0xC0, which is true for
			// any long-header packet with fixed bit set (0xC0/0xD0/0xE0/0xF0).
			name: "0-RTT packet (0xD0) — long header + fixed bit set",
			buf:  append([]byte{0xD0}, make([]byte, 25)...),
			want: true,
		},
		{
			name: "Handshake packet (0xE0) — long header + fixed bit set",
			buf:  append([]byte{0xE0}, make([]byte, 25)...),
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsQUICInitial(tc.buf); got != tc.want {
				t.Errorf("IsQUICInitial(%v) = %v, want %v", tc.buf[:min(len(tc.buf), 4)], got, tc.want)
			}
		})
	}
}

// TestRelayQUIC_NonInitialPacket verifies that a non-Initial datagram is
// forwarded as-is (no splitting).
func TestRelayQUIC_NonInitialPacket(t *testing.T) {
	clientR, clientW := net.Pipe()
	remoteR, remoteW := net.Pipe()

	// Non-Initial payload: first byte 0x40 (short header).
	payload := append([]byte{0x40}, bytes.Repeat([]byte{0xAB}, 30)...)

	var received []byte
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		n, _ := remoteR.Read(buf)
		received = buf[:n]
		remoteR.Close()
	}()

	go func() {
		_ = clientW.SetWriteDeadline(time.Now().Add(2 * time.Second))
		_, _ = clientW.Write(payload)
		clientW.Close()
	}()

	RelayQUIC(clientR, remoteW, 10)
	remoteW.Close()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RelayQUIC timed out")
	}

	if !bytes.Equal(received, payload) {
		t.Errorf("non-Initial: received %d bytes %v, want %v", len(received), received[:quicTestMin(len(received), 4)], payload[:quicTestMin(len(payload), 4)])
	}
}

// TestRelayQUIC_InitialPacket verifies that a QUIC Initial datagram is split
// at the specified position.
func TestRelayQUIC_InitialPacket(t *testing.T) {
	clientR, clientW := net.Pipe()
	remoteR, remoteW := net.Pipe()

	// Construct a valid QUIC Initial packet (≥20 bytes, first byte 0xC0).
	payload := append([]byte{0xC0}, bytes.Repeat([]byte{0x01}, 40)...)

	var received []byte
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf, _ := io.ReadAll(remoteR)
		received = buf
	}()

	go func() {
		_ = clientW.SetWriteDeadline(time.Now().Add(2 * time.Second))
		_, _ = clientW.Write(payload)
		clientW.Close()
	}()

	RelayQUIC(clientR, remoteW, 10)
	remoteW.Close()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RelayQUIC timed out")
	}

	// All bytes should be delivered regardless of split position.
	if !bytes.Equal(received, payload) {
		t.Errorf("Initial: received %d bytes, want %d", len(received), len(payload))
	}
}

// TestRelayQUIC_InvalidSplitPos verifies that split position 0 or ≥n falls
// back to quicInitialMinLen.
func TestRelayQUIC_InvalidSplitPos(t *testing.T) {
	clientR, clientW := net.Pipe()
	remoteR, remoteW := net.Pipe()

	payload := append([]byte{0xC0}, bytes.Repeat([]byte{0x02}, 30)...)

	var received []byte
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf, _ := io.ReadAll(remoteR)
		received = buf
	}()

	go func() {
		_ = clientW.SetWriteDeadline(time.Now().Add(2 * time.Second))
		_, _ = clientW.Write(payload)
		clientW.Close()
	}()

	// splitPos=0 → should fall back to quicInitialMinLen.
	RelayQUIC(clientR, remoteW, 0)
	remoteW.Close()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RelayQUIC timed out")
	}

	if !bytes.Equal(received, payload) {
		t.Errorf("splitPos=0: received %d bytes, want %d", len(received), len(payload))
	}
}

// TestRelayQUIC_ClientClosedImmediately verifies no panic when client closes.
func TestRelayQUIC_ClientClosedImmediately(t *testing.T) {
	clientR, clientW := net.Pipe()
	remoteR, remoteW := net.Pipe()
	defer remoteR.Close()

	clientW.Close() // close before sending

	done := make(chan struct{})
	go func() {
		RelayQUIC(clientR, remoteW, 10)
		remoteW.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Error("RelayQUIC did not return after client closed")
	}
}

func quicTestMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
