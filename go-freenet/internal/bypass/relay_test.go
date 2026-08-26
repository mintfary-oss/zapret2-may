// Tests for relay strategy functions: split, tlsrec, disorder.
//
// All tests use net.Pipe() so no real network is needed.  The pattern is:
//  1. Build a synthetic TLS ClientHello payload.
//  2. Create two net.Pipe() pairs: client↔relay and relay↔remote.
//  3. Feed the ClientHello from the client side.
//  4. Run the relay function under test in a goroutine.
//  5. Read all bytes from the remote side and verify:
//     - total bytes received == bytes sent
//     - content is intact (same bytes, possibly reordered for disorder)
package bypass

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

// deadline is the read timeout applied to all pipe reads in tests.
const deadline = 2 * time.Second

// runRelay runs f(client, remote) in a goroutine, feeds payload to client,
// closes the client write side after the payload, and collects everything
// that arrives at the remote side.  It returns the collected bytes.
func runRelay(t *testing.T, f func(client, remote net.Conn), payload []byte) []byte {
	t.Helper()

	// Two in-memory pipes: client side feeds the relay, remote side receives.
	clientA, clientB := net.Pipe() // clientA → relay reads from here
	remoteA, remoteB := net.Pipe() // relay writes to remoteA; test reads from remoteB

	// Collect everything that arrives at the remote (remoteB side).
	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		_ = remoteB.SetReadDeadline(time.Now().Add(deadline))
		data, err := io.ReadAll(remoteB)
		ch <- result{data, err}
	}()

	// Run the relay function: it reads from clientB and writes to remoteA.
	go func() {
		defer clientB.Close()
		defer remoteA.Close()
		f(clientB, remoteA)
	}()

	// Feed the payload to clientA (the "client" side).
	_ = clientA.SetWriteDeadline(time.Now().Add(deadline))
	if _, err := clientA.Write(payload); err != nil {
		t.Fatalf("Write payload: %v", err)
	}
	// Close clientA so the relay sees EOF and terminates its relay loop.
	_ = clientA.Close()

	res := <-ch
	if res.err != nil && res.err != io.EOF {
		t.Logf("remote read ended with: %v (may be normal pipe close)", res.err)
	}
	return res.data
}

// ---------------------------------------------------------------------------
// relaySplit
// ---------------------------------------------------------------------------

func TestRelaySplit_DataIntact(t *testing.T) {
	hello := buildClientHello("example.com", false)
	got := runRelay(t, func(c, r net.Conn) { relaySplit(c, r, 2) }, hello)
	if !bytes.Equal(got, hello) {
		t.Errorf("relaySplit: got %d bytes, want %d; data mismatch", len(got), len(hello))
	}
}

func TestRelaySplit_WithSNI(t *testing.T) {
	hello := buildClientHello("blocked-site.ru", false)
	got := runRelay(t, func(c, r net.Conn) { relaySplit(c, r, 2) }, hello)
	if len(got) != len(hello) {
		t.Errorf("relaySplit SNI: got %d bytes, want %d", len(got), len(hello))
	}
	if !bytes.Equal(got, hello) {
		t.Error("relaySplit SNI: content does not match original ClientHello")
	}
}

func TestRelaySplit_ECHPassthrough(t *testing.T) {
	// ECH ClientHellos contain a cover domain; split should still relay correctly.
	hello := buildClientHello("cover.example.com", true /* addECH */)
	got := runRelay(t, func(c, r net.Conn) { relaySplit(c, r, 1) }, hello)
	if !bytes.Equal(got, hello) {
		t.Errorf("relaySplit ECH: got %d bytes %v", len(got), got[:min(10, len(got))])
	}
}

func TestRelaySplit_TinyPayload(t *testing.T) {
	// One-byte payload — split position is clamped to 1.
	payload := []byte{0x16}
	got := runRelay(t, func(c, r net.Conn) { relaySplit(c, r, 2) }, payload)
	if !bytes.Equal(got, payload) {
		t.Errorf("relaySplit tiny: got %v, want %v", got, payload)
	}
}

func TestRelaySplit_NonTLSPayload(t *testing.T) {
	// HTTP/1.1 payload — relaySplit falls back to plain relay.
	http := []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")
	got := runRelay(t, func(c, r net.Conn) { relaySplit(c, r, 2) }, http)
	if !bytes.Equal(got, http) {
		t.Errorf("relaySplit non-TLS: data mismatch (got %d bytes, want %d)", len(got), len(http))
	}
}

// ---------------------------------------------------------------------------
// relayTLSRec
// ---------------------------------------------------------------------------

func TestRelayTLSRec_DataIntact(t *testing.T) {
	hello := buildClientHello("youtube.com", false)
	got := runRelay(t, func(c, r net.Conn) { relayTLSRec(c, r, 2) }, hello)
	if len(got) == 0 {
		t.Fatal("relayTLSRec: received no data")
	}
	// TLS record splitting adds two 5-byte headers instead of one, so the
	// total bytes on the wire = original + 5 (one extra TLS record header).
	// Reconstruct the original payload by stripping the TLS record framing.
	// Simpler invariant: all original payload bytes must be present.
	payload := hello[5:] // strip original TLS record header
	// got = rec1_header(5) + payload[:split] + rec2_header(5) + payload[split:]
	// Concatenate both record bodies and compare with original payload.
	if len(got) < 10 {
		t.Fatalf("relayTLSRec: too few bytes (%d)", len(got))
	}
	// Strip the two TLS record headers (5 bytes each) to recover payload.
	var body []byte
	for i := 0; i < len(got); {
		if i+5 > len(got) {
			break
		}
		recLen := int(got[i+3])<<8 | int(got[i+4])
		recEnd := i + 5 + recLen
		if recEnd > len(got) {
			recEnd = len(got)
		}
		body = append(body, got[i+5:recEnd]...)
		i = recEnd
	}
	if !bytes.Equal(body, payload) {
		t.Errorf("relayTLSRec: reassembled payload mismatch (%d vs %d bytes)", len(body), len(payload))
	}
}

func TestRelayTLSRec_NonTLS(t *testing.T) {
	// Non-TLS data must pass through unmodified.
	raw := []byte("CONNECT example.com:443 HTTP/1.1\r\n\r\n")
	got := runRelay(t, func(c, r net.Conn) { relayTLSRec(c, r, 2) }, raw)
	if !bytes.Equal(got, raw) {
		t.Errorf("relayTLSRec non-TLS: data mismatch")
	}
}

func TestRelayTLSRec_SmallDefaultPos(t *testing.T) {
	// defaultPos=0 → fall back to midpoint split; data must still arrive.
	hello := buildClientHello("vk.com", false)
	got := runRelay(t, func(c, r net.Conn) { relayTLSRec(c, r, 0) }, hello)
	if len(got) == 0 {
		t.Error("relayTLSRec small pos: received no data")
	}
}

// ---------------------------------------------------------------------------
// relayDisorder
// ---------------------------------------------------------------------------

func TestRelayDisorder_AllBytesDelivered(t *testing.T) {
	// Disorder sends head and tail swapped, then continues relaying.
	// net.Pipe reassembles in arrival order, so we verify all bytes arrive.
	hello := buildClientHello("instagram.com", false)
	got := runRelay(t, func(c, r net.Conn) { relayDisorder(c, r, 2) }, hello)
	if len(got) != len(hello) {
		t.Errorf("relayDisorder: got %d bytes, want %d", len(got), len(hello))
	}
	// The bytes must be a permutation of the original (head+tail are swapped).
	gotCopy := make([]byte, len(got))
	copy(gotCopy, got)
	wantCopy := make([]byte, len(hello))
	copy(wantCopy, hello)
	// Both slices must contain the same multiset of bytes.
	gotSorted := sortBytes(gotCopy)
	wantSorted := sortBytes(wantCopy)
	if !bytes.Equal(gotSorted, wantSorted) {
		t.Error("relayDisorder: bytes are not a permutation of original")
	}
}

func TestRelayDisorder_NonTLS(t *testing.T) {
	raw := []byte("POST / HTTP/1.1\r\nContent-Length: 4\r\n\r\ntest")
	got := runRelay(t, func(c, r net.Conn) { relayDisorder(c, r, 1) }, raw)
	if len(got) != len(raw) {
		t.Errorf("relayDisorder non-TLS: got %d bytes, want %d", len(got), len(raw))
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// sortBytes returns a sorted copy of buf (used for permutation checking).
func sortBytes(b []byte) []byte {
	counts := make([]int, 256)
	for _, v := range b {
		counts[v]++
	}
	out := make([]byte, 0, len(b))
	for i, c := range counts {
		for j := 0; j < c; j++ {
			out = append(out, byte(i))
		}
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
