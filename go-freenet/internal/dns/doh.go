// Package dns provides a DNS-over-HTTPS (DoH) client and a local UDP resolver
// that forwards plain DNS queries to DoH servers.
//
// Why DoH?
// Russian ISPs perform DNS poisoning alongside DPI filtering.  Even when the
// DPI bypass succeeds, forged DNS responses can still block access.  By
// routing all DNS through encrypted HTTPS the provider cannot see or alter
// the responses.
//
// RFC reference: RFC 8484 — DNS Queries over HTTPS (DoH).
package dns

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// DefaultServers are the DoH server endpoints used when none are configured.
// All three support RFC 8484 POST with application/dns-message content type.
var DefaultServers = []string{
	"https://1.1.1.1/dns-query", // Cloudflare — fastest in most regions
	"https://8.8.8.8/dns-query", // Google
	"https://9.9.9.9/dns-query", // Quad9 — blocks malicious domains
}

// Client sends DNS queries over HTTPS (DoH, RFC 8484).
// It tries each configured server in order and returns the first success.
// All methods are safe for concurrent use.
type Client struct {
	mu         sync.RWMutex
	servers    []string
	httpClient *http.Client
}

// NewClient creates a DoH client with the given server URLs.
// Pass nil or an empty slice to use DefaultServers.
func NewClient(servers []string) *Client {
	if len(servers) == 0 {
		servers = append([]string(nil), DefaultServers...)
	}
	return &Client{
		servers: servers,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Servers returns the list of DoH server URLs this client will use.
func (c *Client) Servers() []string { return c.servers }

// Exchange sends a raw DNS wire-format query (as defined in RFC 1035) to the
// first available DoH server and returns the raw DNS wire-format response.
// It tries each server once and stops at the first success.
func (c *Client) Exchange(ctx context.Context, query []byte) ([]byte, error) {
	c.mu.RLock()
	servers := c.servers
	hc := c.httpClient
	c.mu.RUnlock()

	var lastErr error
	for _, srv := range servers {
		resp, err := c.doQueryWith(ctx, hc, srv, query)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("all DoH servers failed; last error: %w", lastErr)
}

// Lookup resolves name to IPv4 address strings via DoH.
// Returns an error when no A records are found or all servers fail.
func (c *Client) Lookup(ctx context.Context, name string) ([]string, error) {
	q, err := buildQuery(name, dnsmessage.TypeA)
	if err != nil {
		return nil, err
	}
	raw, err := c.Exchange(ctx, q)
	if err != nil {
		return nil, err
	}
	return parseAddrs(raw)
}

// doQueryWith sends query to a single DoH server using the given HTTP client.
func (c *Client) doQueryWith(ctx context.Context, hc *http.Client, server string, query []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server, bytes.NewReader(query))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")

	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DoH server %s returned HTTP %d", server, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// EnableECH fetches ECH configs for all configured DoH servers via DNS HTTPS
// records and upgrades the internal HTTP transport to use ECH for subsequent
// DoH requests.  Non-fatal: if ECH configs cannot be fetched the client
// continues operating without ECH.
//
// Call once after the client is created, in a background goroutine so that
// startup is not blocked.
func (c *Client) EnableECH(ctx context.Context) {
	// Collect ECH configs per DoH server hostname.
	type echEntry struct {
		host string
		cfg  []byte
	}
	var entries []echEntry

	c.mu.RLock()
	servers := c.servers
	c.mu.RUnlock()

	for _, srv := range servers {
		u, err := url.Parse(srv)
		if err != nil {
			continue
		}
		host := u.Hostname()
		echCfg, err := c.LookupECHConfig(ctx, host)
		if err != nil || len(echCfg) == 0 {
			continue // server may not publish ECH — not an error
		}
		entries = append(entries, echEntry{host: host, cfg: echCfg})
		log.Printf("dns: ECH config fetched for DoH server %s (%d bytes)", host, len(echCfg))
	}

	if len(entries) == 0 {
		return // no ECH configs available
	}

	// Build a per-hostname ECH map for the TLS dialer.
	echMap := make(map[string][]byte, len(entries))
	for _, e := range entries {
		echMap[e.host] = e.cfg
	}

	echTransport := &http.Transport{
		// DialTLSContext is called for every HTTPS connection.  We inject
		// the ECH config list for DoH server hosts that support it.
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, _ := net.SplitHostPort(addr)
			tlsCfg := &tls.Config{}
			if echCfg, ok := echMap[host]; ok {
				tlsCfg.EncryptedClientHelloConfigList = echCfg
				tlsCfg.MinVersion = tls.VersionTLS13
			}
			return tls.DialWithDialer(&net.Dialer{}, network, addr, tlsCfg)
		},
	}

	c.mu.Lock()
	c.httpClient = &http.Client{
		Timeout:   5 * time.Second,
		Transport: echTransport,
	}
	c.mu.Unlock()

	log.Printf("dns: ECH enabled for %d DoH server(s)", len(entries))
}

// NewDoHHTTPClient returns an *http.Client whose TCP dialer uses the local
// DoH resolver for all name resolution, preventing ISP-level DNS poisoning
// even for outbound HTTP requests (e.g. hostlist downloads).
//
// resolverAddr should be the UDP listen address of a running Resolver
// (e.g. "127.0.0.1:5300").
func NewDoHHTTPClient(resolverAddr string) *http.Client {
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "udp", resolverAddr)
		},
	}
	d := &net.Dialer{Resolver: r}
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext: d.DialContext,
		},
	}
}

// ---------------------------------------------------------------------------
// HTTPS / SVCB record — ECH config extraction
// ---------------------------------------------------------------------------

// typeHTTPS is the IANA-assigned DNS type for HTTPS records (RFC 9460).
// golang.org/x/net/dns/dnsmessage does not define this constant; it
// returns the rdata as *dnsmessage.UnknownResource for unrecognised types.
const typeHTTPS dnsmessage.Type = 65

// svcParamKeyECH is the SvcParam key for encrypted_client_hello (key 5).
const svcParamKeyECH uint16 = 5

// LookupECHConfig queries the DNS HTTPS record for name and returns the
// raw ECH config list bytes (SvcParam key 5, RFC 9460 §7.3).
//
// Returns (nil, nil) when the domain has no HTTPS record or carries no ECH
// config — not an error, just no ECH support detected.
func (c *Client) LookupECHConfig(ctx context.Context, name string) ([]byte, error) {
	q, err := buildQuery(name, typeHTTPS)
	if err != nil {
		return nil, err
	}
	raw, err := c.Exchange(ctx, q)
	if err != nil {
		return nil, err
	}
	return parseECHFromHTTPS(raw)
}

// parseECHFromHTTPS scans a raw DNS response for HTTPS answers and extracts
// the ECH config bytes from the first record that carries SvcParam key 5.
func parseECHFromHTTPS(raw []byte) ([]byte, error) {
	var msg dnsmessage.Message
	if err := msg.Unpack(raw); err != nil {
		return nil, fmt.Errorf("dns: unpack HTTPS response: %w", err)
	}
	for _, ans := range msg.Answers {
		if ans.Header.Type != typeHTTPS {
			continue
		}
		// For unknown types the library returns *dnsmessage.UnknownResource.
		unknown, ok := ans.Body.(*dnsmessage.UnknownResource)
		if !ok {
			continue
		}
		if ech := parseSVCBECH(unknown.Data); len(ech) > 0 {
			return ech, nil
		}
	}
	return nil, nil
}

// parseSVCBECH decodes SVCB wire-format rdata (RFC 9460 §2.2) and returns
// the value of SvcParam key 5 (ech), or nil when absent.
//
// SVCB rdata layout:
//
//	Priority    uint16
//	TargetName  DNS name (wire-format, variable length)
//	SvcParams   sequence of { key uint16, length uint16, value [length]byte }
func parseSVCBECH(data []byte) []byte {
	if len(data) < 4 {
		return nil
	}
	pos := 2 // skip Priority (2 bytes)

	// Skip TargetName: a DNS wire-format name is a sequence of labels
	// ending with a zero-length label (0x00) or a pointer (0xC0|0x80).
	for pos < len(data) {
		labelLen := int(data[pos])
		switch {
		case labelLen == 0: // root label — name ends here
			pos++
			goto parseSvcParams
		case labelLen&0xC0 == 0xC0: // pointer — 2 bytes, name ends
			pos += 2
			goto parseSvcParams
		default:
			pos += 1 + labelLen
		}
	}

parseSvcParams:
	// Parse SvcParam list: key(2) + length(2) + value(length)
	for pos+4 <= len(data) {
		key := binary.BigEndian.Uint16(data[pos : pos+2])
		vlen := int(binary.BigEndian.Uint16(data[pos+2 : pos+4]))
		pos += 4
		if pos+vlen > len(data) {
			break
		}
		if key == svcParamKeyECH {
			out := make([]byte, vlen)
			copy(out, data[pos:pos+vlen])
			return out
		}
		pos += vlen
	}
	return nil
}

// ---------------------------------------------------------------------------
// DNS wire-format helpers
// ---------------------------------------------------------------------------

// buildQuery constructs a minimal DNS wire-format query for name/qtype.
func buildQuery(name string, qtype dnsmessage.Type) ([]byte, error) {
	if len(name) == 0 {
		return nil, fmt.Errorf("dns: empty hostname")
	}
	// dnsmessage.NewName requires a fully-qualified name ending in '.'.
	fqdn := name
	if fqdn[len(fqdn)-1] != '.' {
		fqdn += "."
	}
	n, err := dnsmessage.NewName(fqdn)
	if err != nil {
		return nil, fmt.Errorf("dns: invalid name %q: %w", name, err)
	}
	msg := dnsmessage.Message{
		Header: dnsmessage.Header{ID: 1, RecursionDesired: true},
		Questions: []dnsmessage.Question{
			{Name: n, Type: qtype, Class: dnsmessage.ClassINET},
		},
	}
	return msg.Pack()
}

// parseAddrs extracts A and AAAA record values from a DNS wire-format response.
func parseAddrs(raw []byte) ([]string, error) {
	var msg dnsmessage.Message
	if err := msg.Unpack(raw); err != nil {
		return nil, fmt.Errorf("dns: unpack response: %w", err)
	}
	var addrs []string
	for _, ans := range msg.Answers {
		switch rr := ans.Body.(type) {
		case *dnsmessage.AResource:
			addrs = append(addrs, fmt.Sprintf("%d.%d.%d.%d",
				rr.A[0], rr.A[1], rr.A[2], rr.A[3]))
		case *dnsmessage.AAAAResource:
			b := rr.AAAA
			addrs = append(addrs, fmt.Sprintf("%x:%x:%x:%x:%x:%x:%x:%x",
				uint16(b[0])<<8|uint16(b[1]),
				uint16(b[2])<<8|uint16(b[3]),
				uint16(b[4])<<8|uint16(b[5]),
				uint16(b[6])<<8|uint16(b[7]),
				uint16(b[8])<<8|uint16(b[9]),
				uint16(b[10])<<8|uint16(b[11]),
				uint16(b[12])<<8|uint16(b[13]),
				uint16(b[14])<<8|uint16(b[15]),
			))
		}
	}
	return addrs, nil
}
