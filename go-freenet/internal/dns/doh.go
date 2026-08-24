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
	"fmt"
	"io"
	"net"
	"net/http"
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
	var lastErr error
	for _, srv := range c.servers {
		resp, err := c.doQuery(ctx, srv, query)
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

// doQuery sends query to a single DoH server and returns the raw response.
func (c *Client) doQuery(ctx context.Context, server string, query []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server, bytes.NewReader(query))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DoH server %s returned HTTP %d", server, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
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
