//go:build !windows

package main

// Non-Windows stub — no system tray.
// runApp simply waits for SIGINT / SIGTERM and then stops the server and UI.

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/mintfary-oss/freenet/internal/config"
	"github.com/mintfary-oss/freenet/internal/proxy"
	"github.com/mintfary-oss/freenet/internal/web"
)

// runApp on non-Windows blocks until SIGINT or SIGTERM is received, then
// gracefully stops the proxy server and web UI.
func runApp(_ *config.Config, _ string, srv *proxy.Server, ui *web.UI) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	srv.Stop()
	ui.Stop()
}
