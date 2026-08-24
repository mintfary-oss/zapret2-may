package dns

import (
	"encoding/binary"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

// ---------------------------------------------------------------------------
// buildQuery tests
// ---------------------------------------------------------------------------

func TestBuildQuery_A(t *testing.T) {
	raw, err := buildQuery("youtube.com", dnsmessage.TypeA)
	if err != nil {
		t.Fatalf("buildQuery error: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("buildQuery returned empty bytes")
	}

	// Parse back and verify the question.
	var msg dnsmessage.Message
	if err := msg.Unpack(raw); err != nil {
		t.Fatalf("unpack error: %v", err)
	}
	if len(msg.Questions) != 1 {
		t.Fatalf("got %d questions, want 1", len(msg.Questions))
	}
	q := msg.Questions[0]
	if q.Type != dnsmessage.TypeA {
		t.Errorf("question type = %v, want A", q.Type)
	}
	if !msg.Header.RecursionDesired {
		t.Error("RecursionDesired should be true")
	}
}

func TestBuildQuery_EmptyName(t *testing.T) {
	_, err := buildQuery("", dnsmessage.TypeA)
	if err == nil {
		t.Error("expected error for empty hostname, got nil")
	}
}

func TestBuildQuery_TrailingDot(t *testing.T) {
	// Name with trailing dot should work (already FQDN).
	_, err := buildQuery("example.com.", dnsmessage.TypeA)
	if err != nil {
		t.Errorf("buildQuery with trailing dot: %v", err)
	}
}

func TestBuildQuery_HTTPS(t *testing.T) {
	raw, err := buildQuery("cloudflare.com", typeHTTPS)
	if err != nil {
		t.Fatalf("buildQuery HTTPS error: %v", err)
	}
	var msg dnsmessage.Message
	if err := msg.Unpack(raw); err != nil {
		t.Fatalf("unpack: %v", err)
	}
	if len(msg.Questions) == 0 || msg.Questions[0].Type != typeHTTPS {
		t.Errorf("question type = %v, want HTTPS (65)", msg.Questions[0].Type)
	}
}

// ---------------------------------------------------------------------------
// parseAddrs tests
// ---------------------------------------------------------------------------

func TestParseAddrs_A(t *testing.T) {
	// Build a synthetic DNS A response.
	raw, err := buildAResponse("example.com", [4]byte{93, 184, 216, 34})
	if err != nil {
		t.Fatalf("buildAResponse: %v", err)
	}

	addrs, err := parseAddrs(raw)
	if err != nil {
		t.Fatalf("parseAddrs error: %v", err)
	}
	if len(addrs) != 1 || addrs[0] != "93.184.216.34" {
		t.Errorf("parseAddrs = %v, want [93.184.216.34]", addrs)
	}
}

func TestParseAddrs_Empty(t *testing.T) {
	// A valid DNS response with NOERROR but no answer records.
	raw, err := buildEmptyResponse("no-answer.example")
	if err != nil {
		t.Fatalf("buildEmptyResponse: %v", err)
	}
	addrs, err := parseAddrs(raw)
	if err != nil {
		t.Fatalf("parseAddrs error: %v", err)
	}
	if len(addrs) != 0 {
		t.Errorf("parseAddrs returned %d addresses for empty response, want 0", len(addrs))
	}
}

// ---------------------------------------------------------------------------
// parseSVCBECH tests
// ---------------------------------------------------------------------------

func TestParseSVCBECH_Present(t *testing.T) {
	echBytes := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	rdata := buildSVCBRdata(1, ".", []svcParam{{key: svcParamKeyECH, val: echBytes}})

	got := parseSVCBECH(rdata)
	if len(got) == 0 {
		t.Fatal("parseSVCBECH returned nil, want ECH bytes")
	}
	if string(got) != string(echBytes) {
		t.Errorf("parseSVCBECH = %x, want %x", got, echBytes)
	}
}

func TestParseSVCBECH_Absent(t *testing.T) {
	// SVCB record with only ALPN (key 1), no ECH.
	rdata := buildSVCBRdata(1, ".", []svcParam{{key: 1, val: []byte{0x02, 'h', '2'}}})
	got := parseSVCBECH(rdata)
	if got != nil {
		t.Errorf("parseSVCBECH = %x, want nil when ECH absent", got)
	}
}

func TestParseSVCBECH_TooShort(t *testing.T) {
	got := parseSVCBECH([]byte{0x00}) // too short
	if got != nil {
		t.Error("parseSVCBECH should return nil for too-short input")
	}
}

func TestParseSVCBECH_MultipleSvcParams(t *testing.T) {
	// ECH is after ALPN — parser must walk all params.
	echBytes := []byte{0x01, 0x02, 0x03}
	rdata := buildSVCBRdata(1, ".", []svcParam{
		{key: 1, val: []byte{0x02, 'h', '2'}}, // ALPN
		{key: svcParamKeyECH, val: echBytes},  // ECH
		{key: 6, val: []byte{0x20, 0x01}},     // IPv6Hint
	})
	got := parseSVCBECH(rdata)
	if string(got) != string(echBytes) {
		t.Errorf("parseSVCBECH = %x, want %x", got, echBytes)
	}
}

// ---------------------------------------------------------------------------
// helpers for building test DNS messages
// ---------------------------------------------------------------------------

func buildAResponse(name string, ip [4]byte) ([]byte, error) {
	fqdn, _ := dnsmessage.NewName(name + ".")
	msg := dnsmessage.Message{
		Header: dnsmessage.Header{
			ID:                 1,
			Response:           true,
			RecursionDesired:   true,
			RecursionAvailable: true,
		},
		Questions: []dnsmessage.Question{
			{Name: fqdn, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET},
		},
		Answers: []dnsmessage.Resource{
			{
				Header: dnsmessage.ResourceHeader{
					Name:  fqdn,
					Type:  dnsmessage.TypeA,
					Class: dnsmessage.ClassINET,
					TTL:   300,
				},
				Body: &dnsmessage.AResource{A: ip},
			},
		},
	}
	return msg.Pack()
}

func buildEmptyResponse(name string) ([]byte, error) {
	fqdn, _ := dnsmessage.NewName(name + ".")
	msg := dnsmessage.Message{
		Header: dnsmessage.Header{
			ID:                 1,
			Response:           true,
			RecursionDesired:   true,
			RecursionAvailable: true,
		},
		Questions: []dnsmessage.Question{
			{Name: fqdn, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET},
		},
	}
	return msg.Pack()
}

// svcParam is a key/value pair for SVCB wire format construction.
type svcParam struct {
	key uint16
	val []byte
}

// buildSVCBRdata builds a minimal SVCB/HTTPS rdata payload:
//
//	Priority(2) + TargetName(DNS wire) + SvcParams
func buildSVCBRdata(priority uint16, targetName string, params []svcParam) []byte {
	var out []byte

	// Priority
	out = append(out, byte(priority>>8), byte(priority))

	// TargetName in DNS wire format (root = single zero byte)
	if targetName == "." || targetName == "" {
		out = append(out, 0x00)
	} else {
		for _, label := range splitLabels(targetName) {
			out = append(out, byte(len(label)))
			out = append(out, []byte(label)...)
		}
		out = append(out, 0x00)
	}

	// SvcParams
	for _, p := range params {
		lenBytes := make([]byte, 2)
		binary.BigEndian.PutUint16(lenBytes, uint16(len(p.val)))
		keyBytes := []byte{byte(p.key >> 8), byte(p.key)}
		out = append(out, keyBytes...)
		out = append(out, lenBytes...)
		out = append(out, p.val...)
	}
	return out
}

func splitLabels(name string) []string {
	var labels []string
	cur := ""
	for _, c := range name {
		if c == '.' {
			if cur != "" {
				labels = append(labels, cur)
			}
			cur = ""
		} else {
			cur += string(c)
		}
	}
	if cur != "" {
		labels = append(labels, cur)
	}
	return labels
}
