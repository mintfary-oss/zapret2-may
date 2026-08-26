// Additional tests for the DoH client: constructor, Servers(), Exchange(),
// NewDoHHTTPClient(), and parseECHFromHTTPS helpers.
package dns

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

// ---------------------------------------------------------------------------
// NewClient / Servers
// ---------------------------------------------------------------------------

func TestNewClient_DefaultServers(t *testing.T) {
	c := NewClient(nil)
	if c == nil {
		t.Fatal("NewClient(nil) returned nil")
	}
	servers := c.Servers()
	if len(servers) == 0 {
		t.Error("NewClient(nil) should use DefaultServers, got empty list")
	}
	if len(servers) != len(DefaultServers) {
		t.Errorf("Servers() len = %d, want %d", len(servers), len(DefaultServers))
	}
}

func TestNewClient_CustomServers(t *testing.T) {
	custom := []string{"https://dns.example.com/dns-query"}
	c := NewClient(custom)
	servers := c.Servers()
	if len(servers) != 1 || servers[0] != custom[0] {
		t.Errorf("Servers() = %v, want %v", servers, custom)
	}
}

func TestNewClient_EmptySlice(t *testing.T) {
	c := NewClient([]string{})
	// Empty slice → should fall back to DefaultServers.
	if len(c.Servers()) == 0 {
		t.Error("NewClient([]) should use DefaultServers, got empty list")
	}
}

// ---------------------------------------------------------------------------
// Exchange — using a local httptest server instead of real DoH
// ---------------------------------------------------------------------------

// buildDoHServer returns a test server that responds with a valid empty DNS
// response for any POST request to /dns-query.
func buildDoHServer(t *testing.T, answer []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/dns-message")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(answer)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestExchange_Success(t *testing.T) {
	// Build a valid minimal DNS response.
	resp, err := buildEmptyResponse("exchange.test")
	if err != nil {
		t.Fatal(err)
	}

	srv := buildDoHServer(t, resp)
	c := NewClient([]string{srv.URL})

	query, err := buildQuery("exchange.test", dnsmessage.TypeA)
	if err != nil {
		t.Fatal(err)
	}

	got, err := c.Exchange(context.Background(), query)
	if err != nil {
		t.Fatalf("Exchange error: %v", err)
	}
	if len(got) == 0 {
		t.Error("Exchange returned empty response")
	}
}

func TestExchange_AllServersFail(t *testing.T) {
	// Point at a port that refuses connections.
	c := NewClient([]string{"http://127.0.0.1:1/dns-query"})

	query, err := buildQuery("fail.test", dnsmessage.TypeA)
	if err != nil {
		t.Fatal(err)
	}

	_, err = c.Exchange(context.Background(), query)
	if err == nil {
		t.Error("Exchange should return error when all servers fail")
	}
}

func TestLookup_WithTestServer(t *testing.T) {
	resp, err := buildAResponse("lookup.test", [4]byte{10, 0, 0, 1})
	if err != nil {
		t.Fatal(err)
	}

	srv := buildDoHServer(t, resp)
	c := NewClient([]string{srv.URL})

	addrs, err := c.Lookup(context.Background(), "lookup.test")
	if err != nil {
		t.Fatalf("Lookup error: %v", err)
	}
	if len(addrs) == 0 {
		t.Error("Lookup returned no addresses")
	}
	if addrs[0] != "10.0.0.1" {
		t.Errorf("Lookup = %v, want [10.0.0.1]", addrs)
	}
}

func TestLookup_EmptyResponse(t *testing.T) {
	resp, err := buildEmptyResponse("empty.test")
	if err != nil {
		t.Fatal(err)
	}

	srv := buildDoHServer(t, resp)
	c := NewClient([]string{srv.URL})

	addrs, err := c.Lookup(context.Background(), "empty.test")
	if err != nil {
		t.Fatalf("Lookup error: %v", err)
	}
	if len(addrs) != 0 {
		t.Errorf("Lookup for NOERROR/no-answers = %v, want []", addrs)
	}
}

// ---------------------------------------------------------------------------
// NewDoHHTTPClient
// ---------------------------------------------------------------------------

func TestNewDoHHTTPClient_NotNil(t *testing.T) {
	c := NewDoHHTTPClient("127.0.0.1:5300")
	if c == nil {
		t.Error("NewDoHHTTPClient returned nil")
	}
}

func TestNewDoHHTTPClient_CanMakeRequest(t *testing.T) {
	// Serve a minimal response on a local httptest server and ensure the
	// DoH HTTP client can reach it (the client is just an *http.Client with
	// a custom dialer — it must still work for non-DoH targets).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewDoHHTTPClient("127.0.0.1:5300")
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatalf("DoH HTTP client Get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// parseECHFromHTTPS
// ---------------------------------------------------------------------------

func TestParseECHFromHTTPS_InvalidBytes(t *testing.T) {
	_, err := parseECHFromHTTPS([]byte{0x00, 0x01, 0x02})
	if err == nil {
		t.Error("parseECHFromHTTPS should return error for invalid DNS bytes")
	}
}

func TestParseECHFromHTTPS_EmptyAnswer(t *testing.T) {
	raw, err := buildEmptyResponse("no-https.example")
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseECHFromHTTPS(raw)
	if err != nil {
		t.Fatalf("parseECHFromHTTPS unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("parseECHFromHTTPS with empty answer = %x, want nil", got)
	}
}

func TestParseECHFromHTTPS_ARecordIgnored(t *testing.T) {
	// An A-record response must not return any ECH bytes.
	raw, err := buildAResponse("a.example", [4]byte{1, 2, 3, 4})
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseECHFromHTTPS(raw)
	if err != nil {
		t.Fatalf("parseECHFromHTTPS unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("parseECHFromHTTPS for A-record = %x, want nil", got)
	}
}
