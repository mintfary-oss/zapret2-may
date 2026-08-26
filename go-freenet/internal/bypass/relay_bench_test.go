package bypass

// Benchmarks for DPI bypass relay strategies.
//
// All benchmarks use net.Pipe() so no actual network is required.  The
// "remote" end discards every byte it receives; the "client" end sends
// a burst of data that the relay function forwards.
//
// Run with:
//
//	go test -run=^$ -bench=. -benchmem -benchtime=3s ./internal/bypass/
//
// Results are in ns/op, B/op, allocs/op; throughput is visible from the
// combination of bytes transferred per operation and timing.

import (
	"io"
	"net"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// makeSyntheticClientHello builds a minimal TLS 1.3 ClientHello with an SNI
// extension so that the split strategy can locate the SNI boundary.
// The returned buffer is suitable as the first client→remote write.
func makeSyntheticClientHello(payloadSize int) []byte {
	// Use the package's own helper from tls_test.go to build a real enough
	// ClientHello, then pad it to payloadSize bytes so benchmarks measure
	// the cost of copying large payloads too.
	base := buildClientHello("bench.example.com", false)
	if len(base) >= payloadSize {
		return base
	}
	// Append zeroes — relay functions forward all bytes regardless.
	buf := make([]byte, payloadSize)
	copy(buf, base)
	return buf
}

// relayBench is the generic harness for all relay benchmarks.
// It:
//  1. Creates a pipe pair (client ↔ relay ↔ remote).
//  2. Has the "remote" goroutine discard everything it receives.
//  3. Feeds firstChunk as the initial client data, followed by tail bytes.
//  4. Invokes relayFn(client, remote) and measures its wall-clock cost.
//
// b.N controls how many full relay invocations are timed.
func relayBench(b *testing.B, relayFn func(client, remote net.Conn), firstChunk []byte, tail int) {
	b.Helper()

	// Build a tail-segment payload filled with zeroes.
	tailPayload := make([]byte, tail)

	b.ResetTimer()
	b.ReportAllocs()

	b.SetBytes(int64(len(firstChunk) + tail))

	for i := 0; i < b.N; i++ {
		clientSide, remoteSide := net.Pipe()

		// Remote: drain everything, then close.
		done := make(chan struct{})
		go func() {
			defer close(done)
			io.Copy(io.Discard, remoteSide) //nolint:errcheck
			remoteSide.Close()
		}()

		// Client feed: write firstChunk then tail, then close.
		go func() {
			defer clientSide.Close()
			clientSide.Write(firstChunk) //nolint:errcheck
			if tail > 0 {
				clientSide.Write(tailPayload) //nolint:errcheck
			}
		}()

		relayFn(clientSide, remoteSide)
		<-done
	}
}

// ---------------------------------------------------------------------------
// Benchmarks: relayPlain (baseline)
// ---------------------------------------------------------------------------

// BenchmarkRelayPlain_4KB measures a plain bidirectional relay with 4 KB of data.
func BenchmarkRelayPlain_4KB(b *testing.B) {
	chunk := make([]byte, 4*1024)
	relayBench(b, relayPlain, chunk, 0)
}

// BenchmarkRelayPlain_64KB measures throughput for a 64 KB burst.
func BenchmarkRelayPlain_64KB(b *testing.B) {
	chunk := make([]byte, 64*1024)
	relayBench(b, relayPlain, chunk, 0)
}

// ---------------------------------------------------------------------------
// Benchmarks: relaySplit
// ---------------------------------------------------------------------------

// BenchmarkRelaySplit_TLSHello_4KB forwards a synthetic TLS ClientHello (4 KB).
// This exercises SNI parsing + two-segment write + plain relay for the tail.
func BenchmarkRelaySplit_TLSHello_4KB(b *testing.B) {
	first := makeSyntheticClientHello(4 * 1024)
	relayBench(b,
		func(c, r net.Conn) { relaySplit(c, r, 2) },
		first, 0,
	)
}

// BenchmarkRelaySplit_TLSHello_64KB adds a 60 KB tail after the hello.
func BenchmarkRelaySplit_TLSHello_64KB(b *testing.B) {
	first := makeSyntheticClientHello(4 * 1024)
	relayBench(b,
		func(c, r net.Conn) { relaySplit(c, r, 2) },
		first, 60*1024,
	)
}

// BenchmarkRelaySplit_NoTLS benchmarks the fallback path when no TLS hello is found.
func BenchmarkRelaySplit_NoTLS(b *testing.B) {
	// A plaintext HTTP request — split will fall back to pos=1.
	first := []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")
	relayBench(b,
		func(c, r net.Conn) { relaySplit(c, r, 2) },
		first, 0,
	)
}

// ---------------------------------------------------------------------------
// Benchmarks: relayDisorder
// ---------------------------------------------------------------------------

// BenchmarkRelayDisorder_TLSHello measures the disorder strategy overhead.
func BenchmarkRelayDisorder_TLSHello(b *testing.B) {
	first := makeSyntheticClientHello(2 * 1024)
	relayBench(b,
		func(c, r net.Conn) { relayDisorder(c, r, 2) },
		first, 0,
	)
}

// ---------------------------------------------------------------------------
// Benchmarks: relayTLSRec
// ---------------------------------------------------------------------------

// BenchmarkRelayTLSRec_TLSHello measures the TLS record split strategy.
func BenchmarkRelayTLSRec_TLSHello(b *testing.B) {
	first := makeSyntheticClientHello(2 * 1024)
	relayBench(b,
		func(c, r net.Conn) { relayTLSRec(c, r, 2) },
		first, 0,
	)
}

// BenchmarkRelayTLSRec_TLSHello_64KB adds a large tail.
func BenchmarkRelayTLSRec_TLSHello_64KB(b *testing.B) {
	first := makeSyntheticClientHello(2 * 1024)
	relayBench(b,
		func(c, r net.Conn) { relayTLSRec(c, r, 2) },
		first, 62*1024,
	)
}

// ---------------------------------------------------------------------------
// Benchmarks: SNI parsing throughput
// ---------------------------------------------------------------------------

// BenchmarkParseClientHello measures the cost of extracting the SNI from a
// raw TLS ClientHello (no I/O, pure byte parsing).
func BenchmarkParseClientHello_WithSNI(b *testing.B) {
	hello := buildClientHello("benchmark.example.com", false)
	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(len(hello)))
	for i := 0; i < b.N; i++ {
		_, _ = ParseClientHello(hello)
	}
}

// BenchmarkParseClientHello_WithECH measures extra cost of the ECH extension scan.
func BenchmarkParseClientHello_WithECH(b *testing.B) {
	hello := buildClientHello("benchmark.example.com", true)
	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(len(hello)))
	for i := 0; i < b.N; i++ {
		_, _ = ParseClientHello(hello)
	}
}

// BenchmarkSplitPosition_WithSNI measures SplitPosition when info is non-nil.
func BenchmarkSplitPosition_WithSNI(b *testing.B) {
	hello := buildClientHello("benchmark.example.com", false)
	info, _ := ParseClientHello(hello)
	if info == nil {
		b.Skip("ParseClientHello returned nil — cannot benchmark SplitPosition")
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = SplitPosition(info, 2)
	}
}
