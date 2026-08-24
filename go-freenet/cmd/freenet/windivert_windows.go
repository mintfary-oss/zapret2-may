//go:build windows

package main

import (
	"log"
	"sync"

	"github.com/mintfary-oss/freenet/internal/windivert"
)

// wdManager manages the WinDivert intercept lifecycle.
// When WinDivert.dll is present and bypass is enabled, it intercepts all
// outbound TCP port-443 packets and applies the DPI bypass strategy at the
// network layer — covering every application, not just SOCKS5-aware clients.
type wdManager struct {
	mu       sync.Mutex
	handle   *windivert.Handle
	strategy string // current bypass strategy
}

var globalWD = &wdManager{}

// startWinDivert opens a WinDivert handle and runs the intercept loop in the
// background.  strategy must be one of "split", "tlsrec", "combined", "auto",
// "fake", "none".  It is a no-op if WinDivert is already running or if
// WinDivert.dll is not available.
func startWinDivert(strategy string) {
	globalWD.mu.Lock()
	defer globalWD.mu.Unlock()

	if globalWD.handle != nil {
		// Already running — nothing to do.
		return
	}
	if !windivert.Available() {
		log.Printf("windivert: WinDivert.dll not found — kernel-level bypass unavailable (SOCKS5 bypass still active)")
		return
	}

	h, err := windivert.Open()
	if err != nil {
		log.Printf("windivert: failed to open: %v", err)
		return
	}

	globalWD.handle = h
	globalWD.strategy = strategy
	log.Printf("windivert: kernel-level bypass started (strategy=%s)", strategy)

	go h.RunIntercept(strategy)
}

// stopWinDivert closes the active WinDivert handle.  It is a no-op if
// WinDivert is not running.
func stopWinDivert() {
	globalWD.mu.Lock()
	defer globalWD.mu.Unlock()

	if globalWD.handle == nil {
		return
	}
	globalWD.handle.Close()
	globalWD.handle = nil
	log.Printf("windivert: kernel-level bypass stopped")
}

// winDivertRunning reports whether the WinDivert intercept loop is active.
func winDivertRunning() bool {
	globalWD.mu.Lock()
	defer globalWD.mu.Unlock()
	return globalWD.handle != nil
}

// restartWinDivert stops and restarts WinDivert with a new strategy.
// This is called when the user changes the bypass strategy while WinDivert is
// active.
func restartWinDivert(strategy string) {
	stopWinDivert()
	startWinDivert(strategy)
}
