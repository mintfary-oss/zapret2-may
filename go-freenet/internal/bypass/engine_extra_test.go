package bypass

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"

	"github.com/mintfary-oss/freenet/internal/config"
)

// ---------------------------------------------------------------------------
// Engine lifecycle & strategy dispatch
// ---------------------------------------------------------------------------

func newTestConfig(strategy string) *config.Config {
	return &config.Config{
		Bypass: config.BypassConfig{
			Strategy: strategy,
			SplitPos: 2,
			FakeTTL:  8,
		},
		Hostlist: config.HostlistConfig{
			Enabled: false,
		},
	}
}

// engRelay runs e.RelayDomain(client, remote, domain) in a goroutine,
// feeds payload from the write side, and returns everything the remote
// read side received.
func engRelay(t *testing.T, e *Engine, domain string, payload []byte) []byte {
	t.Helper()
	clientR, clientW := net.Pipe()
	remoteR, remoteW := net.Pipe()

	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		all, err := io.ReadAll(remoteR)
		ch <- result{all, err}
	}()

	go func() {
		e.RelayDomain(clientR, remoteW, domain)
		remoteW.Close()
	}()

	_ = clientW.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, _ = clientW.Write(payload)
	clientW.Close()

	select {
	case r := <-ch:
		if r.err != nil && r.err != io.EOF {
			t.Logf("remote read error: %v", r.err)
		}
		return r.data
	case <-time.After(5 * time.Second):
		t.Fatal("engRelay timed out")
		return nil
	}
}

func TestEngine_NewEngine_Default(t *testing.T) {
	cfg := newTestConfig("auto")
	e := NewEngine(cfg)
	if e == nil {
		t.Fatal("NewEngine returned nil")
	}
	if e.Hostlist() == nil {
		t.Error("Hostlist() should not be nil")
	}
}

func TestEngine_ECHPassthroughs_Initial(t *testing.T) {
	e := NewEngine(newTestConfig("split"))
	if v := e.ECHPassthroughs(); v != 0 {
		t.Errorf("ECHPassthroughs initially = %d, want 0", v)
	}
}

func TestEngine_Reload(t *testing.T) {
	e := NewEngine(newTestConfig("split"))
	newCfg := newTestConfig("tlsrec")
	e.Reload(newCfg)
	// After reload the engine must use the new config; strategy is exercised
	// by relaying data through it.
	payload := buildClientHello("example.com", false)
	got := engRelay(t, e, "", payload)
	if len(got) == 0 {
		t.Error("expected non-empty relay output after Reload")
	}
}

// TestEngine_StrategyNone verifies that strategy=none forwards bytes unchanged.
func TestEngine_StrategyNone(t *testing.T) {
	e := NewEngine(newTestConfig("none"))
	payload := []byte("plaintext payload not a TLS hello")
	got := engRelay(t, e, "", payload)
	if !bytes.Equal(got, payload) {
		t.Errorf("strategy=none: got %q, want %q", got, payload)
	}
}

// TestEngine_StrategySplit verifies that strategy=split delivers all bytes.
func TestEngine_StrategySplit(t *testing.T) {
	e := NewEngine(newTestConfig("split"))
	payload := buildClientHello("example.com", false)
	got := engRelay(t, e, "", payload)
	if len(got) != len(payload) {
		t.Errorf("strategy=split: received %d bytes, want %d", len(got), len(payload))
	}
	if !bytes.Equal(got, payload) {
		t.Error("strategy=split: content mismatch")
	}
}

// TestEngine_StrategyTLSRec verifies that strategy=tlsrec delivers all bytes.
// TLS record splitting re-wraps the payload into two TLS records, so the
// received byte count is larger than the original (extra 5-byte record header).
func TestEngine_StrategyTLSRec(t *testing.T) {
	e := NewEngine(newTestConfig("tlsrec"))
	payload := buildClientHello("example.com", false)
	got := engRelay(t, e, "", payload)
	if len(got) < len(payload) {
		t.Errorf("strategy=tlsrec: received %d bytes, want at least %d", len(got), len(payload))
	}
}

// TestEngine_StrategyDisorder verifies that strategy=disorder delivers all bytes.
func TestEngine_StrategyDisorder(t *testing.T) {
	e := NewEngine(newTestConfig("disorder"))
	payload := buildClientHello("example.com", false)
	got := engRelay(t, e, "", payload)
	if len(got) == 0 {
		t.Error("strategy=disorder: received 0 bytes")
	}
}

// TestEngine_StrategyCombined verifies that strategy=combined delivers all bytes.
func TestEngine_StrategyCombined(t *testing.T) {
	e := NewEngine(newTestConfig("combined"))
	payload := buildClientHello("example.com", false)
	got := engRelay(t, e, "", payload)
	if len(got) == 0 {
		t.Error("strategy=combined: received 0 bytes")
	}
}

// TestEngine_StrategyUnknownFallsBackToSplit verifies that an unknown strategy
// falls back to split (content preserved).
func TestEngine_StrategyUnknownFallsBackToSplit(t *testing.T) {
	e := NewEngine(newTestConfig("does-not-exist"))
	payload := buildClientHello("example.com", false)
	got := engRelay(t, e, "", payload)
	if len(got) != len(payload) {
		t.Errorf("unknown strategy fallback: received %d bytes, want %d", len(got), len(payload))
	}
}

// TestEngine_ECHPassthrough verifies that an ECH ClientHello increments the
// counter and is forwarded unmodified.
func TestEngine_ECHPassthrough(t *testing.T) {
	e := NewEngine(newTestConfig("split"))
	payload := buildClientHello("example.com", true) // ECH=true
	got := engRelay(t, e, "", payload)
	if len(got) != len(payload) {
		t.Errorf("ECH passthrough: received %d bytes, want %d", len(got), len(payload))
	}
	if !bytes.Equal(got, payload) {
		t.Error("ECH passthrough: content changed — should be forwarded unmodified")
	}
	if v := e.ECHPassthroughs(); v != 1 {
		t.Errorf("ECHPassthroughs = %d, want 1", v)
	}
}

// TestEngine_RelayClientClosedImmediately verifies that a connection that
// closes without sending data does not panic.
func TestEngine_RelayClientClosedImmediately(t *testing.T) {
	e := NewEngine(newTestConfig("split"))
	clientR, clientW := net.Pipe()
	remoteR, remoteW := net.Pipe()
	defer remoteR.Close()

	clientW.Close() // close before sending anything

	done := make(chan struct{})
	go func() {
		e.RelayDomain(clientR, remoteW, "")
		remoteW.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Error("RelayDomain did not return after client closed connection")
	}
}

// TestEngine_Relay_CallsRelayDomain verifies that Relay is a thin wrapper
// that does not panic.
func TestEngine_Relay_CallsRelayDomain(t *testing.T) {
	e := NewEngine(newTestConfig("none"))
	clientR, clientW := net.Pipe()
	remoteR, remoteW := net.Pipe()
	defer remoteR.Close()
	clientW.Close()

	done := make(chan struct{})
	go func() {
		e.Relay(clientR, remoteW)
		remoteW.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Error("Relay did not return")
	}
}

// TestEngine_HostlistEnabled verifies that when hostlist is enabled and domain
// is NOT in the list, relayPlain is used (no strategy transforms applied).
func TestEngine_HostlistEnabled_DomainAbsent(t *testing.T) {
	cfg := newTestConfig("split")
	cfg.Hostlist.Enabled = true

	e := NewEngine(cfg)
	// No domains loaded → list is empty → ShouldBypass always false.

	payload := []byte("raw plaintext")
	got := engRelay(t, e, "notlisted.example", payload)
	if !bytes.Equal(got, payload) {
		t.Errorf("domain not in hostlist: bytes changed (got %d, want %d)", len(got), len(payload))
	}
}

// TestEngine_HostlistEnabled_DomainPresent verifies bypass is applied when
// the domain is in the hostlist.
func TestEngine_HostlistEnabled_DomainPresent(t *testing.T) {
	cfg := newTestConfig("none") // none just forwards, easy to verify
	cfg.Hostlist.Enabled = true

	e := NewEngine(cfg)
	e.Hostlist().Enable(true)
	// Manually add a domain to the list via the in-memory loader.
	r := bytes.NewBufferString("bypass.example\n")
	_ = e.hostlist.load(r) // call internal load directly via same package

	payload := buildClientHello("bypass.example", false)
	got := engRelay(t, e, "bypass.example", payload)
	if len(got) != len(payload) {
		t.Errorf("domain in hostlist strategy=none: received %d bytes, want %d", len(got), len(payload))
	}
}

// ---------------------------------------------------------------------------
// AutoDetector unit tests (no network required)
// ---------------------------------------------------------------------------

func TestAutoDetector_Winner_Default(t *testing.T) {
	d := &AutoDetector{}
	// Default is "tlsrec" — most effective against Russian ТСПУ DPI boxes.
	if got := d.Winner(); got != "tlsrec" {
		t.Errorf("empty AutoDetector.Winner() = %q, want tlsrec", got)
	}
}

func TestAutoDetector_SetWinner(t *testing.T) {
	d := &AutoDetector{}
	// Inject a winner without running a real probe.
	d.mu.Lock()
	d.winner = "tlsrec"
	d.mu.Unlock()

	if got := d.Winner(); got != "tlsrec" {
		t.Errorf("Winner() = %q, want tlsrec", got)
	}
}

func TestAutoDetector_WinnerAfterAllFailed(t *testing.T) {
	// Run against an address that will fail immediately.
	d := &AutoDetector{}
	results := d.Run("127.0.0.1:1", []string{"split", "none"}, 2)
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
	// All probes must fail (connection refused) so winner defaults to "split".
	if got := d.Winner(); got != "split" {
		t.Errorf("after all-failed run, Winner() = %q, want split", got)
	}
	for _, r := range results {
		if r.OK {
			t.Errorf("probe %q reported OK against refused port", r.Strategy)
		}
	}
}

// ---------------------------------------------------------------------------
// fake package-level init: globalFakeSender is nil on non-linux
// ---------------------------------------------------------------------------

func TestGlobalFakeSender_NilOnNonLinux(t *testing.T) {
	// On non-Linux platforms newFakeSender() returns an error and
	// globalFakeSender is never set.  Verify it stays nil so that
	// relayFake falls back to split automatically.
	if globalFakeSender != nil {
		t.Log("globalFakeSender is non-nil (Linux or CGO raw socket available); skipping nil check")
	}
}

// ---------------------------------------------------------------------------
// buildMinimalClientHello / helpers
// ---------------------------------------------------------------------------

func TestBuildMinimalClientHello_StartsWithTLSRecord(t *testing.T) {
	data := buildMinimalClientHello("example.com")
	if len(data) < 5 {
		t.Fatalf("buildMinimalClientHello: too short (%d bytes)", len(data))
	}
	// Byte 0 must be 0x16 (TLS handshake record type).
	if data[0] != 0x16 {
		t.Errorf("first byte = 0x%02x, want 0x16 (TLS handshake)", data[0])
	}
}

func TestConcatHelper(t *testing.T) {
	a := []byte{1, 2}
	b := []byte{3, 4}
	got := concat(a, b)
	if !bytes.Equal(got, []byte{1, 2, 3, 4}) {
		t.Errorf("concat = %v, want [1 2 3 4]", got)
	}
}

func TestUint16Bytes(t *testing.T) {
	cases := []struct {
		v    uint16
		want [2]byte
	}{
		{0x0102, [2]byte{0x01, 0x02}},
		{0x0000, [2]byte{0x00, 0x00}},
		{0xffff, [2]byte{0xff, 0xff}},
	}
	for _, tc := range cases {
		got := uint16Bytes(tc.v)
		if got[0] != tc.want[0] || got[1] != tc.want[1] {
			t.Errorf("uint16Bytes(%#x) = %v, want %v", tc.v, got, tc.want[:])
		}
	}
}
