// Package mobile provides a gomobile-bindable API for the FreeNet Android APK.
//
// This package exposes [FreenetEngine], a single object that manages the DPI
// bypass SOCKS5 proxy.  On Android the VpnService creates a TUN interface,
// connects tun2socks to the SOCKS5 port exposed here, and all device traffic
// is transparently routed through the bypass engine.
//
// Usage from Kotlin after "gomobile bind -javapkg com.freenet.bypass ./mobile":
//
//	val engine = Mobile.newFreenetEngine()
//	engine.start(1080)              // SOCKS5 on 127.0.0.1:1080
//	engine.setStrategy("auto")      // or "split", "tlsrec", "combined", "none"
//	...
//	engine.stop()
package mobile

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/mintfary-oss/freenet/internal/config"
	"github.com/mintfary-oss/freenet/internal/logs"
	"github.com/mintfary-oss/freenet/internal/proxy"
)

// FreenetEngine manages the DPI bypass SOCKS5 proxy.
// All methods are safe for concurrent use from multiple goroutines / JVM threads.
type FreenetEngine struct {
	mu      sync.Mutex
	server  *proxy.Server
	ring    *logs.Ring
	running bool
}

// NewFreenetEngine returns a new, idle FreenetEngine.
// Call [FreenetEngine.Start] to begin accepting connections.
func NewFreenetEngine() *FreenetEngine {
	ring := logs.NewRing(500)
	// Route the standard log package output into our ring buffer so that
	// all log.Printf calls from the proxy / bypass packages are captured.
	log.SetOutput(ring)
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	return &FreenetEngine{ring: ring}
}

// Start launches the SOCKS5 DPI-bypass proxy on 127.0.0.1:<port>.
//
// Returns an error if the engine is already running or if the port cannot
// be bound (e.g., in use).  A port value of 0 lets the OS choose a free port;
// use [FreenetEngine.GetListenAddr] to discover the actual address.
func (e *FreenetEngine) Start(port int) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return fmt.Errorf("freenet: already running")
	}

	cfg := &config.Config{
		Proxy: config.ProxyConfig{
			ListenAddr:      fmt.Sprintf("127.0.0.1:%d", port),
			TransparentAddr: "", // disabled on Android
		},
		Bypass: config.BypassConfig{
			Strategy: "auto",
			SplitPos: 2,
			FakeTTL:  8,
			MD5Fake:  false,
		},
		NFQueue: config.NFQueueConfig{
			Enabled: false, // not available on Android
		},
		Hostlist: config.HostlistConfig{
			Enabled:    false,
			AutoUpdate: false,
		},
	}

	srv := proxy.NewServer(cfg, e.ring)
	if err := srv.Start(); err != nil {
		return fmt.Errorf("freenet: start proxy: %w", err)
	}

	e.server = srv
	e.running = true
	log.Printf("FreeNet proxy started on 127.0.0.1:%d", port)
	return nil
}

// Stop shuts down the proxy and releases all resources.
// Safe to call multiple times or when the engine has not been started.
func (e *FreenetEngine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.server != nil {
		e.server.Stop()
		log.Println("FreeNet proxy stopped")
		e.server = nil
	}
	e.running = false
}

// SetStrategy changes the active bypass strategy at runtime.
// Valid values: "auto", "split", "disorder", "fake", "tlsrec", "combined", "none".
// "auto" probes available strategies and picks the best one for the current ISP.
func (e *FreenetEngine) SetStrategy(strategy string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.server != nil {
		e.server.SetStrategy(strategy)
	}
}

// GetStrategy returns the currently configured bypass strategy name.
func (e *FreenetEngine) GetStrategy() string {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.server != nil {
		return e.server.Strategy()
	}
	return "none"
}

// SetBypassEnabled enables or disables the DPI bypass without stopping the
// proxy.  When disabled the engine acts as a plain SOCKS5 forwarder.
func (e *FreenetEngine) SetBypassEnabled(enabled bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.server != nil {
		e.server.SetEnabled(enabled)
	}
}

// IsRunning returns true when the proxy is currently listening for connections.
func (e *FreenetEngine) IsRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

// GetVersion returns the FreeNet version string.
func (e *FreenetEngine) GetVersion() string {
	return "1.0.0"
}

// GetStats returns a JSON-encoded string with proxy statistics.
// Fields: active (active connections), total (all-time connections),
// bytes_in, bytes_out, bypassed, passthrough.
func (e *FreenetEngine) GetStats() string {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.server == nil {
		return `{"active":0,"total":0,"bytes_in":0,"bytes_out":0,"bypassed":0,"passthrough":0}`
	}
	snap := e.server.GetStats()
	data, err := json.Marshal(snap)
	if err != nil {
		return `{}`
	}
	return string(data)
}

// StartVPN starts the DPI bypass SOCKS5 proxy and then begins forwarding
// packets from the TUN interface to that proxy.
//
// Parameters:
//   - tunFd:    the file descriptor returned by Android VpnService.Builder.establish().
//   - port:     local SOCKS5 port, e.g. 1080.
//   - protector: implementation of SocketProtector (VpnService.protect wrapper).
//
// This call blocks inside the TUN read loop.  Call [FreenetEngine.Stop] from
// another goroutine/thread to shut everything down.
func (e *FreenetEngine) StartVPN(tunFd int64, port int, protector SocketProtector) error {
	if err := e.Start(int(port)); err != nil {
		return err
	}
	socksAddr := fmt.Sprintf("127.0.0.1:%d", port)
	return ForwardTUN(tunFd, socksAddr, protector)
}

// StartVPNSimple is identical to [StartVPN] but does not require a
// SocketProtector callback.  Use this when the VPN service process is already
// excluded from the TUN routing table via
// VpnService.Builder.addDisallowedApplication — all bypass-proxy sockets then
// automatically bypass the VPN TUN, so per-socket protect() calls are
// unnecessary.
//
// This is the recommended entry-point for the Android Kotlin integration
// because it avoids the java.lang.reflect.Proxy / gobind callback machinery
// that can silently fail on some devices/Android versions.
func (e *FreenetEngine) StartVPNSimple(tunFd int64, port int) error {
	return e.StartVPN(tunFd, port, nil)
}

// GetListenAddr returns the address the SOCKS5 proxy is listening on
// (e.g. "127.0.0.1:1080").  Returns an empty string if the engine has not
// been started yet.
func (e *FreenetEngine) GetListenAddr() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.server == nil {
		return ""
	}
	return e.server.ListenAddr()
}

// GetRecentLogs returns the most recent log lines (up to n) joined by newlines.
// Useful for displaying a scrolling log in the Android UI.
func (e *FreenetEngine) GetRecentLogs(n int) string {
	entries := e.ring.Recent(n)
	result := make([]byte, 0, 512)
	for i, entry := range entries {
		if i > 0 {
			result = append(result, '\n')
		}
		result = append(result, []byte(entry.Message)...)
	}
	return string(result)
}
