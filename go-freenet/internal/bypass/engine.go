// Package bypass implements DPI evasion strategies.
// The Engine selects and applies the appropriate strategy for each
// relayed TCP connection.
package bypass

import (
	"context"
	"log"
	"net"

	"github.com/mintfary-oss/freenet/internal/config"
	"github.com/mintfary-oss/freenet/internal/types"
)

// Engine selects and runs the active bypass strategy.
type Engine struct {
	cfg      *config.Config
	hostlist *Hostlist
}

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

// RunAutoDetect tests all strategies against target and caches the winner.
// Results are returned for display in the web UI.
func (e *Engine) RunAutoDetect(target string) []types.ProbeResult {
	strategies := []string{"combined", "fake", "tlsrec", "split", "disorder", "none"}
	return globalDetector.Run(target, strategies, e.cfg.Bypass.SplitPos)
}
