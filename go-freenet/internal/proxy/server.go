// Package proxy implements a SOCKS5 proxy server and (on Linux) a
// transparent-proxy listener that redirect traffic through the DPI bypass
// engine.
package proxy

import (
	"log"
	"net"
	"sync"
	"sync/atomic"

	"github.com/mintfary-oss/freenet/internal/bypass"
	"github.com/mintfary-oss/freenet/internal/config"
	"github.com/mintfary-oss/freenet/internal/logs"
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
} = (*Server)(nil)

// Server manages the proxy listeners and exposes a toggle to enable/disable
// bypass at runtime.
type Server struct {
	cfg     *config.Config
	ring    *logs.Ring
	engine  *bypass.Engine
	enabled atomic.Bool
	Stats   Stats

	mu            sync.Mutex
	socksLn       net.Listener
	transparentLn net.Listener
}

// NewServer constructs a Server from cfg. Call Start to begin accepting
// connections.
func NewServer(cfg *config.Config, ring *logs.Ring) *Server {
	return &Server{
		cfg:    cfg,
		ring:   ring,
		engine: bypass.NewEngine(cfg),
	}
}

// Start opens the listening sockets and launches accept loops in background
// goroutines.
func (s *Server) Start() error {
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

// Stop closes all listeners.
func (s *Server) Stop() {
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
func (s *Server) SetEnabled(v bool) {
	s.enabled.Store(v)
	if v {
		log.Println("bypass ENABLED")
	} else {
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
