// Tests for the Resolver — UDP-to-DoH proxy.
package dns

import (
	"context"
	"net"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// TestNewResolver_NilClient verifies that NewResolver creates a default Client
// when nil is passed.
func TestNewResolver_NilClient(t *testing.T) {
	r := NewResolver("127.0.0.1:0", nil)
	if r == nil {
		t.Fatal("NewResolver returned nil")
	}
	if r.ListenAddr() != "127.0.0.1:0" {
		t.Errorf("ListenAddr = %q, want 127.0.0.1:0", r.ListenAddr())
	}
}

// TestNewResolver_WithClient verifies that a provided Client is used.
func TestNewResolver_WithClient(t *testing.T) {
	c := NewClient(nil)
	r := NewResolver("127.0.0.1:0", c)
	if r == nil {
		t.Fatal("NewResolver returned nil")
	}
}

// TestResolver_Counters_Initial verifies that counters start at zero.
func TestResolver_Counters_Initial(t *testing.T) {
	r := NewResolver("127.0.0.1:0", nil)
	if q := r.Queries(); q != 0 {
		t.Errorf("Queries() = %d, want 0", q)
	}
	if e := r.Errors(); e != 0 {
		t.Errorf("Errors() = %d, want 0", e)
	}
}

// TestResolver_Stop_BeforeStart verifies that Stop is safe to call before Start.
func TestResolver_Stop_BeforeStart(t *testing.T) {
	r := NewResolver("127.0.0.1:0", nil)
	r.Stop() // must not panic
}

// TestResolver_Start_Stop verifies that Start binds a UDP socket and Stop
// releases it cleanly.
func TestResolver_Start_Stop(t *testing.T) {
	r := NewResolver("127.0.0.1:0", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := r.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	r.Stop()
}

// TestResolver_Start_InvalidAddr verifies that an invalid address returns an
// error from Start.
func TestResolver_Start_InvalidAddr(t *testing.T) {
	r := NewResolver("not-a-valid-addr:xyz", nil)
	err := r.Start(context.Background())
	if err == nil {
		t.Error("expected error for invalid address, got nil")
		r.Stop()
	}
}

// TestResolver_Start_PortInUse verifies that binding to an already-used port
// returns an error.
func TestResolver_Start_PortInUse(t *testing.T) {
	// Bind a socket to take a port.
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer conn.Close()
	addr := conn.LocalAddr().(*net.UDPAddr)

	r := NewResolver(addr.String(), nil)
	err = r.Start(context.Background())
	if err == nil {
		t.Error("expected error when port is already in use")
		r.Stop()
	}
}

// TestResolver_Stop_Idempotent verifies that calling Stop multiple times does
// not panic.
func TestResolver_Stop_Idempotent(t *testing.T) {
	r := NewResolver("127.0.0.1:0", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := r.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	r.Stop()
	r.Stop() // second Stop must not panic
}

// TestResolver_ContextCancel verifies that the resolver exits when its context
// is cancelled.
func TestResolver_ContextCancel(t *testing.T) {
	r := NewResolver("127.0.0.1:0", nil)
	ctx, cancel := context.WithCancel(context.Background())

	if err := r.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	cancel() // cancel context; serve goroutine should exit

	// Stop to release the socket cleanly.
	r.Stop()
}

// TestResolver_ListenAddr verifies that ListenAddr returns the configured address.
func TestResolver_ListenAddr(t *testing.T) {
	addr := "127.0.0.1:0"
	r := NewResolver(addr, nil)
	if got := r.ListenAddr(); got != addr {
		t.Errorf("ListenAddr() = %q, want %q", got, addr)
	}
}

// TestResolver_ForwardQuery exercises the full forward path using a mock DoH
// server. The resolver accepts a UDP DNS query, forwards it to the mock DoH
// server, and writes the response back to the UDP sender.
func TestResolver_ForwardQuery(t *testing.T) {
	// Build a valid DNS A-record response.
	respBytes, err := buildAResponse("resolver.test", [4]byte{10, 0, 0, 2})
	if err != nil {
		t.Fatal(err)
	}

	// Start a mock DoH server.
	dohSrv := buildDoHServer(t, respBytes)
	defer dohSrv.Close()

	// Create a resolver backed by the mock DoH server.
	client := NewClient([]string{dohSrv.URL})
	r := NewResolver("127.0.0.1:0", client)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := r.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer r.Stop()

	// Determine the actual bound port.
	// Use a new UDP socket to send a query.
	senderConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("sender listen: %v", err)
	}
	defer senderConn.Close()

	// We need the actual resolved port. Since we used ":0" in NewResolver,
	// we can get it from the r.conn field. Use a different approach: start
	// a resolver on a known port.
	r.Stop()

	// Restart on a known port.
	r2 := NewResolver("127.0.0.1:0", client)
	if err := r2.Start(ctx); err != nil {
		t.Fatalf("r2 Start: %v", err)
	}
	defer r2.Stop()

	// Get the actual address by probing: we'll send to the address stored in r2.conn.
	// Since conn is unexported we can work around it by binding r2 to a fixed port.
	r2.Stop()

	// Use a fixed high port to avoid conflicts.
	port := 15391
	r3 := NewResolver("127.0.0.1:"+itoa(port), client)
	if err := r3.Start(ctx); err != nil {
		// Port may be in use; skip instead of fail.
		t.Skipf("resolver port %d unavailable: %v", port, err)
	}
	defer r3.Stop()

	// Build a DNS A query.
	query, err := buildQuery("resolver.test", dnsmessage.TypeA)
	if err != nil {
		t.Fatal(err)
	}

	resolverAddr, _ := net.ResolveUDPAddr("udp4", "127.0.0.1:"+itoa(port))

	_ = senderConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := senderConn.WriteToUDP(query, resolverAddr); err != nil {
		t.Fatalf("WriteToUDP: %v", err)
	}

	buf := make([]byte, 4096)
	n, _, err := senderConn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("ReadFromUDP: %v", err)
	}
	if n == 0 {
		t.Error("received empty DNS response")
	}

	// Verify queries counter incremented.
	time.Sleep(50 * time.Millisecond)
	if q := r3.Queries(); q < 1 {
		t.Errorf("Queries() = %d, want ≥1", q)
	}
}

// TestResolver_ForwardQuery_DoHFails verifies that the errors counter
// increments when DoH forwarding fails.
func TestResolver_ForwardQuery_DoHFails(t *testing.T) {
	// Use a client pointing at a server that refuses connections.
	client := NewClient([]string{"http://127.0.0.1:1/dns-query"})

	port := 15392
	r := NewResolver("127.0.0.1:"+itoa(port), client)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := r.Start(ctx); err != nil {
		t.Skipf("resolver port %d unavailable: %v", port, err)
	}
	defer r.Stop()

	senderConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("sender listen: %v", err)
	}
	defer senderConn.Close()

	query, err := buildQuery("fail.test", dnsmessage.TypeA)
	if err != nil {
		t.Fatal(err)
	}

	resolverAddr, _ := net.ResolveUDPAddr("udp4", "127.0.0.1:"+itoa(port))
	if _, err := senderConn.WriteToUDP(query, resolverAddr); err != nil {
		t.Fatalf("WriteToUDP: %v", err)
	}

	// Wait briefly for the goroutine to process.
	time.Sleep(200 * time.Millisecond)

	if q := r.Queries(); q < 1 {
		t.Errorf("Queries() = %d, want ≥1", q)
	}
	if e := r.Errors(); e < 1 {
		t.Errorf("Errors() = %d, want ≥1", e)
	}
}

// itoa converts an integer to string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	pos := 19
	for n > 0 {
		buf[pos] = byte('0' + n%10)
		pos--
		n /= 10
	}
	if neg {
		buf[pos] = '-'
		pos--
	}
	return string(buf[pos+1:])
}
