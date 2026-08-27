// Tests for the SOCKS5 handshake and request-parsing helpers.
package proxy

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// helpers for building SOCKS5 messages in-process
// ---------------------------------------------------------------------------

// writeSocks5Greeting writes a SOCKS5 greeting (VER NMETHODS METHOD...).
func writeSocks5Greeting(conn net.Conn, methods ...byte) {
	msg := []byte{socks5Version, byte(len(methods))}
	msg = append(msg, methods...)
	_, _ = conn.Write(msg)
}

// writeSocks5Request writes a SOCKS5 CONNECT request for the given host:port.
// addrType must be atypIPv4, atypIPv6, or atypDomain.
func writeSocks5Request(conn net.Conn, addrType byte, host string, port uint16) {
	hdr := []byte{socks5Version, cmdConnect, 0x00, addrType}
	var addr []byte
	switch addrType {
	case atypIPv4:
		ip := net.ParseIP(host).To4()
		addr = ip
	case atypIPv6:
		ip := net.ParseIP(host).To16()
		addr = ip
	case atypDomain:
		addr = append([]byte{byte(len(host))}, []byte(host)...)
	}
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, port)
	msg := append(hdr, addr...)
	msg = append(msg, portBytes...)
	_, _ = conn.Write(msg)
}

// ---------------------------------------------------------------------------
// socks5Handshake tests
// ---------------------------------------------------------------------------

func TestSocks5Handshake_NoAuth(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- socks5Handshake(serverConn)
	}()

	// Client sends: VER=5, NMETHODS=1, METHOD=0x00 (no-auth)
	writeSocks5Greeting(clientConn, authNone)

	// Server should reply: VER=5, METHOD=0x00
	resp := make([]byte, 2)
	if _, err := clientConn.Read(resp); err != nil {
		t.Fatalf("read server response: %v", err)
	}
	if resp[0] != socks5Version || resp[1] != authNone {
		t.Errorf("server greeting = %v, want [%d, %d]", resp, socks5Version, authNone)
	}
	if err := <-errCh; err != nil {
		t.Errorf("socks5Handshake error: %v", err)
	}
}

func TestSocks5Handshake_MultipleMethodsIncludingNoAuth(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- socks5Handshake(serverConn)
	}()

	// Client advertises methods 0x02, 0x00 — server picks 0x00.
	writeSocks5Greeting(clientConn, 0x02, authNone)

	resp := make([]byte, 2)
	if _, err := clientConn.Read(resp); err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp[1] != authNone {
		t.Errorf("selected method = %d, want %d (no-auth)", resp[1], authNone)
	}
	if err := <-errCh; err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSocks5Handshake_NoAcceptableMethod(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- socks5Handshake(serverConn)
	}()

	// Client only offers method 0x02 (username/password) — server rejects.
	writeSocks5Greeting(clientConn, 0x02)

	resp := make([]byte, 2)
	if _, err := clientConn.Read(resp); err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp[1] != authNoAcceptable {
		t.Errorf("method = %d, want %d (no-acceptable)", resp[1], authNoAcceptable)
	}
	if err := <-errCh; err == nil {
		t.Error("expected error when no acceptable auth method, got nil")
	}
}

func TestSocks5Handshake_BadVersion(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- socks5Handshake(serverConn)
	}()

	// socks5Handshake reads exactly 2 bytes (VER + NMETHODS) before checking
	// the version — write only those 2 bytes to avoid a net.Pipe deadlock.
	// VER=4 (SOCKS4), NMETHODS=0 — version check fails immediately.
	_, _ = clientConn.Write([]byte{0x04, 0x00})

	if err := <-errCh; err == nil {
		t.Error("expected error for SOCKS4 greeting, got nil")
	}
}

// ---------------------------------------------------------------------------
// socks5ReadRequest tests
// ---------------------------------------------------------------------------

func TestSocks5ReadRequest_IPv4(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	type result struct {
		target string
		err    error
	}
	ch := make(chan result, 1)
	go func() {
		target, err := socks5ReadRequest(serverConn)
		ch <- result{target, err}
	}()

	writeSocks5Request(clientConn, atypIPv4, "1.2.3.4", 443)

	res := <-ch
	if res.err != nil {
		t.Fatalf("socks5ReadRequest: %v", res.err)
	}
	if res.target != "1.2.3.4:443" {
		t.Errorf("target = %q, want %q", res.target, "1.2.3.4:443")
	}
}

func TestSocks5ReadRequest_Domain(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	type result struct {
		target string
		err    error
	}
	ch := make(chan result, 1)
	go func() {
		target, err := socks5ReadRequest(serverConn)
		ch <- result{target, err}
	}()

	writeSocks5Request(clientConn, atypDomain, "example.com", 80)

	res := <-ch
	if res.err != nil {
		t.Fatalf("socks5ReadRequest: %v", res.err)
	}
	if res.target != "example.com:80" {
		t.Errorf("target = %q, want %q", res.target, "example.com:80")
	}
}

func TestSocks5ReadRequest_IPv6(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	type result struct {
		target string
		err    error
	}
	ch := make(chan result, 1)
	go func() {
		target, err := socks5ReadRequest(serverConn)
		ch <- result{target, err}
	}()

	writeSocks5Request(clientConn, atypIPv6, "::1", 8080)

	res := <-ch
	if res.err != nil {
		t.Fatalf("socks5ReadRequest: %v", res.err)
	}
	// IPv6 is wrapped in brackets: "[::1]:8080"
	if res.target != "[::1]:8080" {
		t.Errorf("target = %q, want %q", res.target, "[::1]:8080")
	}
}

func TestSocks5ReadRequest_UnsupportedCommand(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	type result struct {
		target string
		err    error
	}
	ch := make(chan result, 1)
	go func() {
		target, err := socks5ReadRequest(serverConn)
		ch <- result{target, err}
	}()

	// socks5ReadRequest reads exactly 4 bytes (VER CMD RSV ATYP) before the
	// command check — write only those 4 bytes so net.Pipe does not deadlock.
	// CMD=0x02 (BIND) triggers the "unsupported command" path.
	_, _ = clientConn.Write([]byte{socks5Version, 0x02, 0x00, atypIPv4})
	// Drain the CMD_NOT_SUPPORT reply (10 bytes) sent back to the client.
	reply := make([]byte, 10)
	_, _ = clientConn.Read(reply)

	res := <-ch
	if res.err == nil {
		t.Error("expected error for BIND command, got nil")
	}
	if reply[1] != repCmdNotSupport {
		t.Errorf("reply[1] = %d, want %d (repCmdNotSupport)", reply[1], repCmdNotSupport)
	}
}

// ---------------------------------------------------------------------------
// Stats tests
// ---------------------------------------------------------------------------

func TestStats_Snapshot_Zero(t *testing.T) {
	var s Stats
	snap := s.Snapshot()
	if snap.Active != 0 || snap.Total != 0 || snap.Bypassed != 0 ||
		snap.Passthrough != 0 || snap.BytesIn != 0 || snap.BytesOut != 0 {
		t.Errorf("zero Stats Snapshot = %+v, want all zeros", snap)
	}
}

func TestStats_Snapshot_Values(t *testing.T) {
	var s Stats
	s.Active.Add(3)
	s.Total.Add(100)
	s.Bypassed.Add(80)
	s.Passthrough.Add(20)
	s.BytesIn.Add(1024)
	s.BytesOut.Add(2048)

	snap := s.Snapshot()
	if snap.Active != 3 {
		t.Errorf("Active = %d, want 3", snap.Active)
	}
	if snap.Total != 100 {
		t.Errorf("Total = %d, want 100", snap.Total)
	}
	if snap.Bypassed != 80 {
		t.Errorf("Bypassed = %d, want 80", snap.Bypassed)
	}
	if snap.Passthrough != 20 {
		t.Errorf("Passthrough = %d, want 20", snap.Passthrough)
	}
	if snap.BytesIn != 1024 {
		t.Errorf("BytesIn = %d, want 1024", snap.BytesIn)
	}
	if snap.BytesOut != 2048 {
		t.Errorf("BytesOut = %d, want 2048", snap.BytesOut)
	}
}

func TestStats_Snapshot_Decrement(t *testing.T) {
	var s Stats
	s.Active.Add(5)
	s.Active.Add(-2) // two connections closed
	snap := s.Snapshot()
	if snap.Active != 3 {
		t.Errorf("Active after decrement = %d, want 3", snap.Active)
	}
}

// ---------------------------------------------------------------------------
// countingConn
// ---------------------------------------------------------------------------

// bufConn is a net.Conn backed by a bytes.Buffer — suitable for unit tests
// that need a simple in-memory connection with no goroutine overhead.
type bufConn struct {
	r *bytes.Buffer // data to be Read()
	w *bytes.Buffer // data that was Write()-n
}

func (c *bufConn) Read(b []byte) (int, error)  { return c.r.Read(b) }
func (c *bufConn) Write(b []byte) (int, error) { return c.w.Write(b) }
func (c *bufConn) Close() error                { return nil }
func (c *bufConn) LocalAddr() net.Addr         { return nil }
func (c *bufConn) RemoteAddr() net.Addr        { return nil }
func (c *bufConn) SetDeadline(_ time.Time) error      { return nil }
func (c *bufConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *bufConn) SetWriteDeadline(_ time.Time) error { return nil }

func TestCountingConn_Read_IncrementsIn(t *testing.T) {
	data := []byte("hello world")
	bc := &bufConn{r: bytes.NewBuffer(data), w: new(bytes.Buffer)}
	var in, out atomic.Int64
	cc := &countingConn{Conn: bc, in: &in, out: &out}

	buf := make([]byte, len(data))
	n, err := cc.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read error: %v", err)
	}
	if n != len(data) {
		t.Fatalf("Read n = %d, want %d", n, len(data))
	}
	if in.Load() != int64(len(data)) {
		t.Errorf("BytesIn = %d, want %d", in.Load(), len(data))
	}
	if out.Load() != 0 {
		t.Errorf("BytesOut should be 0 after Read, got %d", out.Load())
	}
}

func TestCountingConn_Write_IncrementsOut(t *testing.T) {
	bc := &bufConn{r: new(bytes.Buffer), w: new(bytes.Buffer)}
	var in, out atomic.Int64
	cc := &countingConn{Conn: bc, in: &in, out: &out}

	payload := []byte("response data")
	n, err := cc.Write(payload)
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != len(payload) {
		t.Fatalf("Write n = %d, want %d", n, len(payload))
	}
	if out.Load() != int64(len(payload)) {
		t.Errorf("BytesOut = %d, want %d", out.Load(), len(payload))
	}
	if in.Load() != 0 {
		t.Errorf("BytesIn should be 0 after Write, got %d", in.Load())
	}
}

func TestCountingConn_MultipleOps(t *testing.T) {
	bc := &bufConn{
		r: bytes.NewBuffer([]byte("12345")),
		w: new(bytes.Buffer),
	}
	var in, out atomic.Int64
	cc := &countingConn{Conn: bc, in: &in, out: &out}

	buf := make([]byte, 3)
	n1, _ := cc.Read(buf)  // 3 bytes
	n2, _ := cc.Write([]byte("ab"))   // 2 bytes out
	n3, _ := cc.Write([]byte("cdef")) // 4 bytes out

	if in.Load() != int64(n1) {
		t.Errorf("BytesIn = %d, want %d", in.Load(), n1)
	}
	if out.Load() != int64(n2+n3) {
		t.Errorf("BytesOut = %d, want %d", out.Load(), n2+n3)
	}
}

