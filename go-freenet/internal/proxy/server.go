// Package proxy implements a SOCKS5 proxy server and (on Linux) a
// transparent-proxy listener that redirect traffic through the DPI bypass
// engine.
package proxy

import (
	"context"
	"log"
	"net"
	"sync"
	"sync/atomic"

	"github.com/mintfary-oss/freenet/internal/bypass"
	"github.com/mintfary-oss/freenet/internal/config"
	"github.com/mintfary-oss/freenet/internal/dns"
	"github.com/mintfary-oss/freenet/internal/logs"
	"github.com/mintfary-oss/freenet/internal/sysproxy"
	"github.com/mintfary-oss/freenet/internal/types"
)

// compile-time interface check — all web.Controller methods must be present.
var _ interface {
	Enabled() bool
	SetEnabled(bool)
	Strategy() string
	SetStrategy(string)
	GetStats() types.StatsSnapshot
	HostlistSize() int
	RunAutoDetect(string) []types.ProbeResult
	DNSEnabled() bool
	DNSStats() (int64, int64)
	ECHPassthroughs() int64
} = (*Server)(nil)

// Server manages the proxy listeners and exposes a toggle to enable/disable
// bypass at runtime.
type Server struct {
	cfg     *config.Config
	ring    *logs.Ring
	engine  *bypass.Engine
	nfq     *NFQueueServer
	dnsRes  *dns.Resolver // optional; nil if DNS protection disabled
	enabled atomic.Bool
	Stats   Stats

	mu            sync.Mutex
	socksLn       net.Listener
	transparentLn net.Listener
}

// NewServer constructs a Server from cfg. Call Start to begin accepting
// connections.
func NewServer(cfg *config.Config, ring *logs.Ring) *Server {
	eng := bypass.NewEngine(cfg)
	return &Server{
		cfg:    cfg,
		ring:   ring,
		engine: eng,
		nfq:    NewNFQueueServer(cfg, eng),
	}
}

// Start opens the listening sockets and launches accept loops in background
// goroutines.  If DNS protection is enabled in config it also starts the local
// DoH resolver and wires a DoH-aware HTTP client into the hostlist downloader.
func (s *Server) Start() error {
	// Start the DoH DNS resolver before any network activity so that hostlist
	// downloads use encrypted resolution from the very first request.
	if s.cfg.DNS.Enabled {
		dohClient := dns.NewClient(s.cfg.DNS.Servers)
		res := dns.NewResolver(s.cfg.DNS.ListenAddr, dohClient)
		if err := res.Start(context.Background()); err != nil {
			log.Printf("dns: resolver start failed: %v (DNS protection disabled)", err)
		} else {
			s.dnsRes = res
			s.engine.SetHTTPClient(dns.NewDoHHTTPClient(s.cfg.DNS.ListenAddr))
			log.Printf("dns: DoH protection active on %s", s.cfg.DNS.ListenAddr)
			// Attempt to enable ECH for DoH connections in the background.
			// This is non-blocking and non-fatal — ECH improves privacy of
			// our own DNS requests but is not required for operation.
			go dohClient.EnableECH(context.Background())
		}
	}

	ln, err := net.Listen("tcp", s.cfg.Proxy.ListenAddr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.socksLn = ln
	s.mu.Unlock()

	s.enabled.Store(true)
	go s.acceptSOCKS(ln)
	log.Printf("SOCKS5 listening on %s", s.cfg.Proxy.ListenAddr)

	// On Windows, register FreeNet as the system-wide SOCKS5 proxy so that
	// Chrome, Edge, and other WinInet apps work without manual configuration.
	if err := sysproxy.Set(s.cfg.Proxy.ListenAddr); err != nil {
		log.Printf("sysproxy: %v (continuing without system proxy)", err)
	}

	// Start kernel-level nfqueue handler (Linux, optional).
	if err := s.nfq.Start(); err != nil {
		log.Printf("nfqueue: %v (continuing without kernel-level bypass)", err)
	}

	// Optional transparent proxy (Linux only).
	if s.cfg.Proxy.TransparentAddr != "" {
		tln, err := net.Listen("tcp", s.cfg.Proxy.TransparentAddr)
		if err != nil {
			log.Printf("transparent proxy disabled: %v", err)
		} else {
			s.mu.Lock()
			s.transparentLn = tln
			s.mu.Unlock()
			go s.acceptTransparent(tln)
			log.Printf("transparent proxy listening on %s", s.cfg.Proxy.TransparentAddr)
		}
	}

	return nil
}

// Stop closes all listeners and the nfqueue handler.
// It also restores the original Windows system proxy settings (no-op elsewhere).
func (s *Server) Stop() {
	sysproxy.Restore()
	s.nfq.Stop()
	if s.dnsRes != nil {
		s.dnsRes.Stop()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.socksLn != nil {
		_ = s.socksLn.Close()
	}
	if s.transparentLn != nil {
		_ = s.transparentLn.Close()
	}
}

// Enabled returns whether bypass is currently active.
func (s *Server) Enabled() bool { return s.enabled.Load() }

// SetEnabled toggles bypass on or off at runtime.
// On Windows it also sets or restores the system-wide SOCKS5 proxy so that
// browsers react immediately without any manual configuration.
func (s *Server) SetEnabled(v bool) {
	s.enabled.Store(v)
	if v {
		if err := sysproxy.Set(s.cfg.Proxy.ListenAddr); err != nil {
			log.Printf("sysproxy: %v", err)
		}
		log.Println("bypass ENABLED")
	} else {
		sysproxy.Restore()
		log.Println("bypass DISABLED")
	}
}

// Strategy returns the current bypass strategy name.
func (s *Server) Strategy() string { return s.cfg.Bypass.Strategy }

// SetStrategy changes the active strategy at runtime and reconfigures the
// engine.
func (s *Server) SetStrategy(name string) {
	s.cfg.Bypass.Strategy = name
	s.engine.Reload(s.cfg)
	log.Printf("strategy changed to: %s", name)
}

// GetStats returns a point-in-time snapshot of proxy counters.
func (s *Server) GetStats() types.StatsSnapshot { return s.Stats.Snapshot() }

// HostlistSize returns the number of domains in the filter list (0 if
// filtering is disabled).
func (s *Server) HostlistSize() int { return s.engine.Hostlist().Size() }

// RunAutoDetect tests bypass strategies against target and caches the winner.
func (s *Server) RunAutoDetect(target string) []types.ProbeResult {
	return s.engine.RunAutoDetect(target)
}

// DNSStats returns DNS query and error counters from the local DoH resolver.
// Returns (0, 0) when DNS protection is disabled.
func (s *Server) DNSStats() (queries, errors int64) {
	if s.dnsRes != nil {
		return s.dnsRes.Queries(), s.dnsRes.Errors()
	}
	return 0, 0
}

// DNSEnabled reports whether the local DoH resolver is running.
func (s *Server) DNSEnabled() bool { return s.dnsRes != nil }

// ECHPassthroughs returns the number of connections forwarded unmodified
// because the incoming ClientHello carried an ECH extension.
func (s *Server) ECHPassthroughs() int64 { return s.engine.ECHPassthroughs() }
