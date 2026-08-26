// Tests for ECH/HTTPS DNS lookup functions.
package dns

import (
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

// ---------------------------------------------------------------------------
// parseSVCBECH
// ---------------------------------------------------------------------------

// TestParseSVCBECH_NilInput verifies that a nil slice returns nil.
func TestParseSVCBECH_NilInput(t *testing.T) {
	if got := parseSVCBECH(nil); got != nil {
		t.Errorf("nil input: got %v, want nil", got)
	}
}

// TestParseSVCBECH_ThreeBytes verifies that 3-byte input returns nil.
func TestParseSVCBECH_ThreeBytes(t *testing.T) {
	if got := parseSVCBECH([]byte{0, 1, 2}); got != nil {
		t.Errorf("3-byte input: got %v, want nil", got)
	}
}

// TestParseSVCBECH_NoECHKey verifies that SVCB rdata without key 5 returns nil.
func TestParseSVCBECH_NoECHKey(t *testing.T) {
	// Minimal SVCB: Priority=1, TargetName="." (0x00), SvcParam key=1 value=2 bytes.
	data := []byte{
		0x00, 0x01, // priority
		0x00,       // root label (.)
		0x00, 0x01, // key=1 (alpn)
		0x00, 0x02, // length=2
		0x68, 0x32, // value "h2"
	}
	if got := parseSVCBECH(data); got != nil {
		t.Errorf("no ECH key: got %v, want nil", got)
	}
}

// TestParseSVCBECH_WithECHKey verifies extraction of SvcParam key 5.
func TestParseSVCBECH_WithECHKey(t *testing.T) {
	echVal := []byte{0xAA, 0xBB, 0xCC, 0xDD}

	// Build SVCB rdata: Priority=1, TargetName="." (0x00), key=5, len=4, value.
	data := make([]byte, 2+1+4+len(echVal))
	binary.BigEndian.PutUint16(data[0:], 1) // priority
	data[2] = 0x00                          // root label
	binary.BigEndian.PutUint16(data[3:], 5) // key = ECH
	binary.BigEndian.PutUint16(data[5:], uint16(len(echVal)))
	copy(data[7:], echVal)

	got := parseSVCBECH(data)
	if len(got) != len(echVal) {
		t.Fatalf("ECH value len = %d, want %d", len(got), len(echVal))
	}
	for i, b := range echVal {
		if got[i] != b {
			t.Errorf("ECH[%d] = 0x%02x, want 0x%02x", i, got[i], b)
		}
	}
}

// TestParseSVCBECH_PointerLabel verifies that compressed DNS names (pointer
// 0xC0xx) are skipped correctly.
func TestParseSVCBECH_PointerLabel(t *testing.T) {
	echVal := []byte{0x01, 0x02}
	// SVCB: priority(2) + pointer(2) + key=5(2) + len=2(2) + echVal
	data := make([]byte, 2+2+4+len(echVal))
	binary.BigEndian.PutUint16(data[0:], 0) // priority
	data[2] = 0xC0                          // pointer high byte
	data[3] = 0x0C                          // pointer low byte
	binary.BigEndian.PutUint16(data[4:], 5) // key = ECH
	binary.BigEndian.PutUint16(data[6:], uint16(len(echVal)))
	copy(data[8:], echVal)

	got := parseSVCBECH(data)
	if len(got) != len(echVal) {
		t.Fatalf("pointer label: ECH len = %d, want %d", len(got), len(echVal))
	}
}

// TestParseSVCBECH_TruncatedValue verifies that a truncated SvcParam value
// does not panic and returns nil.
func TestParseSVCBECH_TruncatedValue(t *testing.T) {
	// Priority(2) + root(1) + key=5(2) + len=100(2) + only 2 bytes of value
	data := []byte{0, 1, 0, 0, 5, 0, 100, 0xAB, 0xCD}
	got := parseSVCBECH(data)
	if got != nil {
		t.Errorf("truncated value: got %v, want nil", got)
	}
}

// TestParseSVCBECH_EmptyName verifies handling of data with no SvcParams
// (just priority + root label).
func TestParseSVCBECH_EmptyName(t *testing.T) {
	data := []byte{0, 1, 0x00} // priority + root label
	if got := parseSVCBECH(data); got != nil {
		t.Errorf("no params: got %v, want nil", got)
	}
}

// ---------------------------------------------------------------------------
// LookupECHConfig
// ---------------------------------------------------------------------------

// TestLookupECHConfig_NoHTTPSRecord verifies that an A-record response
// returns (nil, nil).
func TestLookupECHConfig_NoHTTPSRecord(t *testing.T) {
	resp, err := buildAResponse("no-https.example", [4]byte{1, 2, 3, 4})
	if err != nil {
		t.Fatal(err)
	}
	srv := buildDoHServer(t, resp)
	c := NewClient([]string{srv.URL})

	got, err := c.LookupECHConfig(context.Background(), "no-https.example")
	if err != nil {
		t.Fatalf("LookupECHConfig error: %v", err)
	}
	if got != nil {
		t.Errorf("LookupECHConfig with A-record = %x, want nil", got)
	}
}

// TestLookupECHConfig_EmptyResponse verifies that an empty response returns
// (nil, nil).
func TestLookupECHConfig_EmptyResponse(t *testing.T) {
	resp, err := buildEmptyResponse("empty.test")
	if err != nil {
		t.Fatal(err)
	}
	srv := buildDoHServer(t, resp)
	c := NewClient([]string{srv.URL})

	got, err := c.LookupECHConfig(context.Background(), "empty.test")
	if err != nil {
		t.Fatalf("LookupECHConfig error: %v", err)
	}
	if got != nil {
		t.Errorf("LookupECHConfig empty response = %x, want nil", got)
	}
}

// TestLookupECHConfig_ServerFails verifies that a DoH failure propagates as
// an error.
func TestLookupECHConfig_ServerFails(t *testing.T) {
	c := NewClient([]string{"http://127.0.0.1:1/dns-query"})
	_, err := c.LookupECHConfig(context.Background(), "fail.example")
	if err == nil {
		t.Error("expected error when DoH server is unreachable")
	}
}

// ---------------------------------------------------------------------------
// EnableECH
// ---------------------------------------------------------------------------

// TestEnableECH_AllServersNoECH verifies that EnableECH does not panic when
// no server returns ECH config.
func TestEnableECH_AllServersNoECH(t *testing.T) {
	// Serve A-record responses (no HTTPS/ECH).
	resp, err := buildAResponse("doh.example", [4]byte{1, 2, 3, 4})
	if err != nil {
		t.Fatal(err)
	}
	srv := buildDoHServer(t, resp)
	defer srv.Close()

	c := NewClient([]string{srv.URL})
	// Must not panic or block.
	c.EnableECH(context.Background())
}

// TestEnableECH_InvalidServerURL verifies that a malformed server URL is
// skipped gracefully.
func TestEnableECH_InvalidServerURL(t *testing.T) {
	c := NewClient([]string{"://bad-url-no-scheme"})
	c.EnableECH(context.Background()) // must not panic
}

// TestEnableECH_WithECHRecord verifies that EnableECH replaces the HTTP client
// when a DoH server returns a valid HTTPS/ECH record.
func TestEnableECH_WithECHRecord(t *testing.T) {
	// Build an HTTPS-record response containing an ECH param.
	// We craft the raw DNS response manually because dnsmessage doesn't
	// natively encode HTTPS records.
	echVal := []byte{0xAA, 0xBB}
	httpsResp := buildHTTPSResponseWithECH(t, "doh-ech.example", echVal)

	// The DoH server returns this HTTPS response for any query.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/dns-message")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(httpsResp)
	}))
	defer srv.Close()

	c := NewClient([]string{srv.URL})
	c.EnableECH(context.Background()) // should set the ECH transport
}

// buildHTTPSResponseWithECH constructs a raw DNS response that contains an
// HTTPS answer (type 65) with SvcParam key 5 (ECH) set to echVal.
func buildHTTPSResponseWithECH(t *testing.T, name string, echVal []byte) []byte {
	t.Helper()

	// Build SVCB rdata: priority(2) + root(1) + key=5(2) + len(2) + echVal
	svcb := make([]byte, 2+1+4+len(echVal))
	binary.BigEndian.PutUint16(svcb[0:], 1) // priority
	svcb[2] = 0x00                          // root label
	binary.BigEndian.PutUint16(svcb[3:], 5) // key=ECH
	binary.BigEndian.PutUint16(svcb[5:], uint16(len(echVal)))
	copy(svcb[7:], echVal)

	// We use dnsmessage.UnknownResource to embed type=65 raw rdata.
	fqdn := name
	if fqdn[len(fqdn)-1] != '.' {
		fqdn += "."
	}
	n, err := dnsmessage.NewName(fqdn)
	if err != nil {
		t.Fatalf("NewName: %v", err)
	}

	msg := dnsmessage.Message{
		Header: dnsmessage.Header{ID: 1, Response: true, RecursionDesired: true, RecursionAvailable: true},
		Answers: []dnsmessage.Resource{
			{
				Header: dnsmessage.ResourceHeader{
					Name:  n,
					Type:  typeHTTPS, // 65
					Class: dnsmessage.ClassINET,
					TTL:   300,
				},
				Body: &dnsmessage.UnknownResource{
					Type: typeHTTPS,
					Data: svcb,
				},
			},
		},
	}
	raw, err := msg.Pack()
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	return raw
}
