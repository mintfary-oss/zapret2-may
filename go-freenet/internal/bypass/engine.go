// Package bypass implements DPI evasion strategies.
// The Engine selects and applies the appropriate strategy for each
// relayed TCP connection.
package bypass

import (
	"context"
	"log"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/mintfary-oss/freenet/internal/config"
	"github.com/mintfary-oss/freenet/internal/types"
)

// Engine selects and runs the active bypass strategy.
type Engine struct {
	cfg             *config.Config
	hostlist        *Hostlist
	echPassthroughs atomic.Int64 // connections forwarded unmodified due to ECH
}

// ECHPassthroughs returns the number of connections that were passed through
// without DPI bypass because the ClientHello contained an ECH extension.
func (e *Engine) ECHPassthroughs() int64 { return e.echPassthroughs.Load() }

// NewEngine constructs an Engine from cfg and initialises the hostlist.
func NewEngine(cfg *config.Config) *Engine {
	hl := NewHostlist()
	e := &Engine{cfg: cfg, hostlist: hl}

	if cfg.Hostlist.Enabled {
		hl.Enable(true)
		if cfg.Hostlist.AutoUpdate && cfg.Hostlist.URL != "" {
			go func() {
				path := cfg.Hostlist.Path
				if path == "" {
					path = "domains.lst"
				}
				if err := hl.DownloadAndSave(context.Background(), cfg.Hostlist.URL, path); err != nil {
					log.Printf("hostlist download failed: %v (using local file if available)", err)
					// Try loading a previously downloaded list.
					if cfg.Hostlist.Path != "" {
						_ = hl.LoadFile(cfg.Hostlist.Path)
					}
				}
			}()
		} else if cfg.Hostlist.Path != "" {
			if err := hl.LoadFile(cfg.Hostlist.Path); err != nil {
				log.Printf("hostlist load %s: %v", cfg.Hostlist.Path, err)
			} else {
				log.Printf("hostlist loaded %d domains", hl.Size())
			}
		}
	}

	return e
}

// Reload replaces the active configuration without restarting.
func (e *Engine) Reload(cfg *config.Config) {
	e.cfg = cfg
	e.hostlist.Enable(cfg.Hostlist.Enabled)
}

// Hostlist returns the engine's domain filter (read-only access for stats).
func (e *Engine) Hostlist() *Hostlist { return e.hostlist }

// SetHTTPClient sets the HTTP client used for hostlist downloads.
// Call this with a DoH-aware client (dns.NewDoHHTTPClient) before the engine
// starts its download goroutine to ensure name resolution bypasses the ISP.
func (e *Engine) SetHTTPClient(c *http.Client) {
	e.hostlist.SetHTTPClient(c)
}

// Relay pipes data between client and remote, applying the configured
// bypass strategy to the first outbound segment (TLS ClientHello).
// domain is extracted from the SOCKS5/transparent destination for hostlist
// filtering; pass an empty string to always apply bypass.
func (e *Engine) Relay(client, remote net.Conn) {
	e.RelayDomain(client, remote, "")
}

// RelayDomain is like Relay but checks the hostlist for domain before
// applying bypass.
func (e *Engine) RelayDomain(client, remote net.Conn, domain string) {
	if !e.hostlist.ShouldBypass(domain) {
		relayPlain(client, remote)
		return
	}

	strategy := e.cfg.Bypass.Strategy
	if strategy == "auto" {
		strategy = globalDetector.Winner()
	}

	// Peek at the first application-data chunk to detect an
	// encrypted_client_hello (ECH) extension before applying any bypass
	// strategy.  When ECH is present the outer ClientHello carries only
	// a "cover" domain — the real SNI is already HPKE-encrypted inside
	// the inner ClientHello (RFC 9601).  Applying split/fake/tlsrec in
	// that case is harmful: it splits already-opaque bytes and confuses
	// the server without providing any additional protection.
	//
	// We read the first chunk once here, check for ECH, then wrap the
	// connection so the chosen strategy receives the same bytes it would
	// have read itself.
	//
	// A 15-second deadline prevents the goroutine from blocking forever
	// on non-TLS protocols (plain HTTP, SSH, etc.) where the client may
	// not immediately send its first payload.  The deadline is cleared
	// after the read so it does not affect the relay itself.
	_ = client.SetReadDeadline(time.Now().Add(15 * time.Second))
	first, err := readFirst(client, 4096)
	_ = client.SetReadDeadline(time.Time{}) // clear deadline
	if err != nil {
		return
	}
	if info, _ := ParseClientHello(first); info != nil && info.HasECH {
		log.Printf("bypass: ECH detected (%s) — forwarding unmodified", info.SNI)
		e.echPassthroughs.Add(1)
		relayPlain(&peekConn{Conn: client, buf: first}, remote)
		return
	}
	// Re-wrap so the strategy sees the already-read bytes first.
	client = &peekConn{Conn: client, buf: first}

	switch strategy {
	case "split":
		relaySplit(client, remote, e.cfg.Bypass.SplitPos)
	case "disorder":
		relayDisorder(client, remote, e.cfg.Bypass.SplitPos)
	case "fake":
		relayFake(client, remote, fakeConfig{
			FakeTTL:  e.cfg.Bypass.FakeTTL,
			SplitPos: e.cfg.Bypass.SplitPos,
			MD5Fake:  e.cfg.Bypass.MD5Fake,
		})
	case "tlsrec":
		relayTLSRec(client, remote, e.cfg.Bypass.SplitPos)
	case "combined":
		relayCombined(client, remote, fakeConfig{
			FakeTTL:  e.cfg.Bypass.FakeTTL,
			SplitPos: e.cfg.Bypass.SplitPos,
			MD5Fake:  e.cfg.Bypass.MD5Fake,
		})
	case "none":
		relayPlain(client, remote)
	default:
		relaySplit(client, remote, e.cfg.Bypass.SplitPos)
	}
}

// peekConn wraps a net.Conn with a pre-read buffer prepended to the read
// stream.  Bytes in buf are returned first; once exhausted all subsequent
// reads go directly to the underlying connection.
type peekConn struct {
	net.Conn
	buf []byte
	off int
}

func (p *peekConn) Read(b []byte) (int, error) {
	if p.off < len(p.buf) {
		n := copy(b, p.buf[p.off:])
		p.off += n
		return n, nil
	}
	return p.Conn.Read(b)
}

// readFirst performs a single Read on conn of up to n bytes and returns the
// result.  It returns an error only when the underlying Read fails.
func readFirst(conn net.Conn, n int) ([]byte, error) {
	buf := make([]byte, n)
	nr, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:nr], nil
}

// RunAutoDetect tests all strategies against target and caches the winner.
// Results are returned for display in the web UI.
func (e *Engine) RunAutoDetect(target string) []types.ProbeResult {
	strategies := []string{"combined", "fake", "tlsrec", "split", "disorder", "none"}
	return globalDetector.Run(target, strategies, e.cfg.Bypass.SplitPos)
}
