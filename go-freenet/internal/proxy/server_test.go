// Tests for the proxy Server lifecycle and relay helper.
package proxy

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"

	"github.com/mintfary-oss/freenet/internal/config"
	"github.com/mintfary-oss/freenet/internal/logs"
)

// ---------------------------------------------------------------------------
// relay helper
// ---------------------------------------------------------------------------

// TestRelay_CopiesBothDirections verifies that relay copies data in both
// directions before returning when one side closes.
func TestRelay_CopiesBothDirections(t *testing.T) {
	a1, a2 := net.Pipe() // a side
	b1, b2 := net.Pipe() // b side

	// We relay between a1 and b1 in a goroutine.
	done := make(chan struct{})
	go func() {
		relay(a1, b1)
		close(done)
	}()

	// Send data from a2 → should arrive at b2.
	msgA := []byte("hello from a")
	go func() {
		_, _ = a2.Write(msgA)
	}()

	// Send data from b2 → should arrive at a2.
	msgB := []byte("hello from b")
	go func() {
		_, _ = b2.Write(msgB)
	}()

	_ = b2.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, len(msgA))
	if _, err := io.ReadFull(b2, buf); err != nil {
		t.Fatalf("b2 ReadFull: %v", err)
	}
	if !bytes.Equal(buf, msgA) {
		t.Errorf("b2 received %q, want %q", buf, msgA)
	}

	_ = a2.SetReadDeadline(time.Now().Add(time.Second))
	buf2 := make([]byte, len(msgB))
	if _, err := io.ReadFull(a2, buf2); err != nil {
		t.Fatalf("a2 ReadFull: %v", err)
	}
	if !bytes.Equal(buf2, msgB) {
		t.Errorf("a2 received %q, want %q", buf2, msgB)
	}

	// Close a2 — relay should return.
	a2.Close()
	b2.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Error("relay did not return after connections closed")
	}
}

// ---------------------------------------------------------------------------
// socks5WriteReply
// ---------------------------------------------------------------------------

func TestSocks5WriteReply_Success(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- socks5WriteReply(server, repSuccess)
	}()

	reply := make([]byte, 10)
	if _, err := io.ReadFull(client, reply); err != nil {
		t.Fatalf("ReadFull: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("socks5WriteReply error: %v", err)
	}
	if reply[0] != socks5Version {
		t.Errorf("reply[0] = %d, want %d (VER)", reply[0], socks5Version)
	}
	if reply[1] != repSuccess {
		t.Errorf("reply[1] = %d, want %d (REP)", reply[1], repSuccess)
	}
	if reply[3] != atypIPv4 {
		t.Errorf("reply[3] = %d, want %d (ATYP IPv4)", reply[3], atypIPv4)
	}
}

func TestSocks5WriteReply_ConnRefused(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go func() { _ = socks5WriteReply(server, repConnRefused) }()

	reply := make([]byte, 10)
	if _, err := io.ReadFull(client, reply); err != nil {
		t.Fatalf("ReadFull: %v", err)
	}
	if reply[1] != repConnRefused {
		t.Errorf("reply[1] = %d, want %d", reply[1], repConnRefused)
	}
}

// ---------------------------------------------------------------------------
// Server lifecycle (NewServer, Enabled, SetEnabled, Strategy, SetStrategy)
// ---------------------------------------------------------------------------

func newTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{
		Proxy: config.ProxyConfig{
			ListenAddr: "127.0.0.1:0",
		},
		Bypass: config.BypassConfig{
			Strategy: "split",
			SplitPos: 2,
			FakeTTL:  8,
		},
		DNS: config.DNSConfig{
			Enabled: false,
		},
		NFQueue: config.NFQueueConfig{
			Enabled: false,
		},
	}
	ring := logs.NewRing(10)
	return NewServer(cfg, ring)
}

func TestNewServer_NotNil(t *testing.T) {
	s := newTestServer(t)
	if s == nil {
		t.Fatal("NewServer returned nil")
	}
}

func TestServer_EnabledDefaultFalse(t *testing.T) {
	s := newTestServer(t)
	// Default enabled state is false (bypass is off until turned on).
	if s.Enabled() {
		t.Error("Enabled() should be false by default")
	}
}

func TestServer_SetEnabled(t *testing.T) {
	s := newTestServer(t)
	s.SetEnabled(true)
	if !s.Enabled() {
		t.Error("SetEnabled(true): Enabled() = false")
	}
	s.SetEnabled(false)
	if s.Enabled() {
		t.Error("SetEnabled(false): Enabled() = true")
	}
}

func TestServer_Strategy(t *testing.T) {
	s := newTestServer(t)
	if got := s.Strategy(); got != "split" {
		t.Errorf("Strategy() = %q, want split", got)
	}
}

func TestServer_SetStrategy(t *testing.T) {
	s := newTestServer(t)
	s.SetStrategy("tlsrec")
	if got := s.Strategy(); got != "tlsrec" {
		t.Errorf("after SetStrategy('tlsrec'), Strategy() = %q", got)
	}
}

func TestServer_GetStats_Zero(t *testing.T) {
	s := newTestServer(t)
	snap := s.GetStats()
	if snap.Total != 0 || snap.Active != 0 {
		t.Errorf("initial stats = %+v, want all-zero", snap)
	}
}

func TestServer_HostlistSize_Zero(t *testing.T) {
	s := newTestServer(t)
	if sz := s.HostlistSize(); sz != 0 {
		t.Errorf("HostlistSize() = %d, want 0 (empty hostlist)", sz)
	}
}

func TestServer_DNSEnabled_FalseWhenNotStarted(t *testing.T) {
	s := newTestServer(t)
	// DNS resolver is nil until Start() succeeds with DNS enabled.
	if s.DNSEnabled() {
		t.Error("DNSEnabled() should be false before Start()")
	}
}

func TestServer_DNSStats_ZeroBeforeStart(t *testing.T) {
	s := newTestServer(t)
	q, e := s.DNSStats()
	if q != 0 || e != 0 {
		t.Errorf("DNSStats() = (%d, %d), want (0, 0)", q, e)
	}
}

func TestServer_ECHPassthroughs_Zero(t *testing.T) {
	s := newTestServer(t)
	if v := s.ECHPassthroughs(); v != 0 {
		t.Errorf("ECHPassthroughs() = %d, want 0", v)
	}
}

// TestServer_StartStop exercises the Start/Stop lifecycle without crashing.
func TestServer_StartStop(t *testing.T) {
	s := newTestServer(t)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	// Give listeners a moment to start.
	time.Sleep(20 * time.Millisecond)
	s.Stop()
}

// TestServer_StartStop_WithBypassEnabled tests Start+SetEnabled+Stop.
func TestServer_StartStop_WithBypassEnabled(t *testing.T) {
	s := newTestServer(t)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	s.SetEnabled(true)
	time.Sleep(20 * time.Millisecond)
	s.Stop()
}
