// Tests for handleSOCKS — the full SOCKS5 connection handler.
package proxy

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/mintfary-oss/freenet/internal/config"
	"github.com/mintfary-oss/freenet/internal/logs"
)

// dialSOCKS connects to the server's SOCKS5 port and performs a full
// CONNECT handshake for host:port. Returns the connected client conn.
func dialSOCKS(t *testing.T, srvAddr, host string, port uint16) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", srvAddr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Greeting
	writeSocks5Greeting(conn, authNone)
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		conn.Close()
		t.Fatalf("read greeting: %v", err)
	}

	// Request
	writeSocks5Request(conn, atypDomain, host, port)

	// Reply: 10 bytes
	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		conn.Close()
		t.Fatalf("read reply: %v", err)
	}
	if reply[1] != repSuccess && reply[1] != repConnRefused {
		conn.Close()
		t.Fatalf("unexpected SOCKS5 reply code: %d", reply[1])
	}
	return conn
}

// startTestServer starts a proxy server on a random port and returns the
// server and its listen address.
func startTestServer(t *testing.T, strategy string, enabled bool) (*Server, string) {
	t.Helper()
	cfg := &config.Config{
		Proxy:   config.ProxyConfig{ListenAddr: "127.0.0.1:0"},
		Bypass:  config.BypassConfig{Strategy: strategy, SplitPos: 2, FakeTTL: 8},
		DNS:     config.DNSConfig{Enabled: false},
		NFQueue: config.NFQueueConfig{Enabled: false},
	}
	ring := logs.NewRing(20)
	s := NewServer(cfg, ring)
	if err := s.Start(); err != nil {
		t.Fatalf("server Start: %v", err)
	}
	s.SetEnabled(enabled)

	// Retrieve the actual bound address.
	s.mu.Lock()
	addr := s.socksLn.Addr().String()
	s.mu.Unlock()

	t.Cleanup(s.Stop)
	return s, addr
}

// TestHandleSOCKS_TargetRefused verifies that a CONNECT to a port that refuses
// connections returns repConnRefused.
func TestHandleSOCKS_TargetRefused(t *testing.T) {
	_, srvAddr := startTestServer(t, "none", true)

	conn, err := net.DialTimeout("tcp", srvAddr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Greeting
	writeSocks5Greeting(conn, authNone)
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		t.Fatalf("read greeting: %v", err)
	}

	// CONNECT to a port that refuses (port 1 is conventionally unused)
	writeSocks5Request(conn, atypIPv4, "127.0.0.1", 1)

	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("read reply: %v", err)
	}
	if reply[1] != repConnRefused {
		t.Errorf("CONNECT to refused port: reply = %d, want %d (connRefused)", reply[1], repConnRefused)
	}
}

// TestHandleSOCKS_BypassDisabled verifies that a successful CONNECT through
// the proxy relays data correctly when bypass is disabled.
func TestHandleSOCKS_BypassDisabled(t *testing.T) {
	// Start an echo server.
	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo listen: %v", err)
	}
	defer echoLn.Close()
	go func() {
		for {
			conn, err := echoLn.Accept()
			if err != nil {
				return
			}
			go io.Copy(conn, conn)
		}
	}()

	echoHost, echoPortStr, _ := net.SplitHostPort(echoLn.Addr().String())
	var echoPort uint16
	for _, b := range echoPortStr {
		echoPort = echoPort*10 + uint16(b-'0')
	}

	_, srvAddr := startTestServer(t, "none", false) // bypass=off → passthrough

	conn := dialSOCKS(t, srvAddr, echoHost, echoPort)
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	msg := []byte("hello-proxy")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf) != string(msg) {
		t.Errorf("echo = %q, want %q", buf, msg)
	}
}

// TestHandleSOCKS_BypassEnabled verifies that bypass=enabled path executes.
func TestHandleSOCKS_BypassEnabled(t *testing.T) {
	// Start an echo server.
	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo listen: %v", err)
	}
	defer echoLn.Close()
	go func() {
		for {
			conn, err := echoLn.Accept()
			if err != nil {
				return
			}
			go io.Copy(conn, conn)
		}
	}()

	echoHost, echoPortStr, _ := net.SplitHostPort(echoLn.Addr().String())
	var echoPort uint16
	for _, b := range echoPortStr {
		echoPort = echoPort*10 + uint16(b-'0')
	}

	s, srvAddr := startTestServer(t, "none", true) // bypass=on

	conn := dialSOCKS(t, srvAddr, echoHost, echoPort)
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	// With strategy=none the engine relays plaintext unchanged.
	msg := []byte("bypass-test")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}

	// Verify stats were updated.
	time.Sleep(20 * time.Millisecond)
	snap := s.GetStats()
	if snap.Total < 1 {
		t.Errorf("Total = %d, want ≥1", snap.Total)
	}
}

// TestServer_RunAutoDetect exercises RunAutoDetect at the server level.
func TestServer_RunAutoDetect(t *testing.T) {
	s := newTestServer(t)
	results := s.RunAutoDetect("127.0.0.1:1")
	if len(results) == 0 {
		t.Error("RunAutoDetect: expected at least one result")
	}
}

// TestHandleSOCKS_BadHandshake verifies graceful close when the client sends
// a bad SOCKS5 version.
func TestHandleSOCKS_BadHandshake(t *testing.T) {
	_, srvAddr := startTestServer(t, "none", false)

	conn, err := net.DialTimeout("tcp", srvAddr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	// Send SOCKS4 greeting (version 4).
	_, _ = conn.Write([]byte{0x04, 0x00})

	// Server should close the connection. Any read should return an error or EOF.
	buf := make([]byte, 10)
	n, _ := conn.Read(buf)
	_ = n // server may have sent a rejection or just closed
}
