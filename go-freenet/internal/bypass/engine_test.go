package bypass

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// peekConn tests
// ---------------------------------------------------------------------------

func TestPeekConn_ReturnsBufferedThenNetwork(t *testing.T) {
	pipeSide, remote := net.Pipe()
	defer pipeSide.Close()
	defer remote.Close()

	// Write "world" to the remote end.
	go func() { _, _ = remote.Write([]byte("world")) }()

	// Wrap pipeSide with pre-buffered prefix "hello".
	pc := &peekConn{Conn: pipeSide, buf: []byte("hello")}

	// First Read must come from the buffer.
	var buf [5]byte
	n, err := pc.Read(buf[:])
	if err != nil {
		t.Fatalf("first Read error: %v", err)
	}
	if got := string(buf[:n]); got != "hello" {
		t.Errorf("first Read = %q, want %q", got, "hello")
	}

	// Second Read must come from the underlying connection.
	_ = pipeSide.SetReadDeadline(time.Now().Add(time.Second))
	n, err = pc.Read(buf[:])
	if err != nil {
		t.Fatalf("second Read error: %v", err)
	}
	if got := string(buf[:n]); got != "world" {
		t.Errorf("second Read = %q, want %q", got, "world")
	}
}

func TestPeekConn_EmptyBuffer(t *testing.T) {
	pipeSide, remote := net.Pipe()
	defer pipeSide.Close()
	defer remote.Close()

	go func() { _, _ = remote.Write([]byte("data")) }()

	// Empty buffer → reads straight from conn.
	pc := &peekConn{Conn: pipeSide, buf: []byte{}}
	_ = pipeSide.SetReadDeadline(time.Now().Add(time.Second))
	var buf [4]byte
	n, err := pc.Read(buf[:])
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if got := string(buf[:n]); got != "data" {
		t.Errorf("Read = %q, want %q", got, "data")
	}
}

func TestPeekConn_PartialRead(t *testing.T) {
	// Read buffer smaller than peekConn.buf → returns only what fits.
	pipeSide, _ := net.Pipe()
	defer pipeSide.Close()

	pc := &peekConn{Conn: pipeSide, buf: []byte("abcdefgh")}

	var small [3]byte
	n, err := pc.Read(small[:])
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if n != 3 || string(small[:]) != "abc" {
		t.Errorf("partial read = %q (%d bytes), want abc (3 bytes)", string(small[:n]), n)
	}
	if pc.off != 3 {
		t.Errorf("pc.off = %d, want 3", pc.off)
	}
}

func TestPeekConn_MultipleSmallReads(t *testing.T) {
	pipeSide, _ := net.Pipe()
	defer pipeSide.Close()

	pc := &peekConn{Conn: pipeSide, buf: []byte("hello")}

	// Read one byte at a time from the buffer.
	want := "hello"
	for i := 0; i < len(want); i++ {
		var b [1]byte
		n, err := pc.Read(b[:])
		if err != nil {
			t.Fatalf("byte %d Read error: %v", i, err)
		}
		if n != 1 || b[0] != want[i] {
			t.Errorf("byte %d = %q, want %q", i, b[0], want[i])
		}
	}
	if pc.off != len(want) {
		t.Errorf("after draining buffer pc.off = %d, want %d", pc.off, len(want))
	}
}

// ---------------------------------------------------------------------------
// readFirst tests
// ---------------------------------------------------------------------------

func TestReadFirst_Success(t *testing.T) {
	pipeSide, remote := net.Pipe()
	defer pipeSide.Close()
	defer remote.Close()

	payload := []byte("GET / HTTP/1.1\r\n")
	go func() { _, _ = remote.Write(payload) }()

	_ = pipeSide.SetReadDeadline(time.Now().Add(time.Second))
	got, err := readFirst(pipeSide, 512)
	if err != nil {
		t.Fatalf("readFirst error: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("readFirst = %q, want %q", got, payload)
	}
}

func TestReadFirst_Error(t *testing.T) {
	// Close before any write → EOF.
	pipeSide, remote := net.Pipe()
	remote.Close()

	_, err := readFirst(pipeSide, 512)
	if err == nil {
		t.Error("expected error when conn is closed before data, got nil")
	}
	pipeSide.Close()
}

// ---------------------------------------------------------------------------
// ECH detection round-trip
// ---------------------------------------------------------------------------

// TestECHDetection verifies the full path:
//
//	buildClientHello(ECH=true) → ParseClientHello → HasECH=true
func TestECHDetection_RoundTrip(t *testing.T) {
	buf := buildClientHello("example.com", true)
	info, err := ParseClientHello(buf)
	if err != nil {
		t.Fatalf("ParseClientHello: %v", err)
	}
	if !info.HasECH {
		t.Error("HasECH should be true after buildClientHello(ECH=true)")
	}
}

func TestNoECH_RoundTrip(t *testing.T) {
	buf := buildClientHello("example.com", false)
	info, err := ParseClientHello(buf)
	if err != nil {
		t.Fatalf("ParseClientHello: %v", err)
	}
	if info.HasECH {
		t.Error("HasECH should be false after buildClientHello(ECH=false)")
	}
}

// Ensure peekConn satisfies io.Reader at compile time.
var _ io.Reader = (*peekConn)(nil)
