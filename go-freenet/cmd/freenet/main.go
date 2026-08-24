// FreeNet — cross-platform DPI bypass tool.
// Intercepts TCP traffic and applies desynchronization strategies to evade
// Deep Packet Inspection used by Russian ISPs (TSPU/RKN).
//
// Usage:
//
//	freenet                          # start proxy + web UI (default ports)
//	freenet -web :9000               # custom web UI port
//	freenet -install                 # Windows: install as auto-start service
//	freenet -uninstall               # Windows: remove service
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/mintfary-oss/freenet/internal/config"
	"github.com/mintfary-oss/freenet/internal/logs"
	"github.com/mintfary-oss/freenet/internal/proxy"
	"github.com/mintfary-oss/freenet/internal/web"
)

const version = "1.1.0"

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config file")
	webAddr := flag.String("web", ":8080", "web UI listen address (use 0.0.0.0:8080 for LAN access)")
	showVersion := flag.Bool("version", false, "print version and exit")
	doInstall := flag.Bool("install", false, "Windows: install as auto-start Windows service (run as Administrator)")
	doUninstall := flag.Bool("uninstall", false, "Windows: remove Windows service")
	flag.Parse()

	if *showVersion {
		fmt.Printf("freenet %s\n", version)
		os.Exit(0)
	}

	// Windows service management (no-op on non-Windows via build tags).
	if *doInstall {
		if err := installService(*cfgPath, *webAddr); err != nil {
			fmt.Fprintf(os.Stderr, "install error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("FreeNet service installed and started.")
		fmt.Printf("Web UI → http://localhost%s\n", *webAddr)
		return
	}
	if *doUninstall {
		if err := uninstallService(); err != nil {
			fmt.Fprintf(os.Stderr, "uninstall error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Load configuration (creates a default config.yaml if missing).
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// Initialise shared ring-buffer log streamed to the web UI.
	ring := logs.NewRing(1000)
	log.SetOutput(ring)
	log.SetFlags(log.Ltime | log.Lmsgprefix)
	log.SetPrefix("[freenet] ")

	log.Printf("starting freenet %s", version)

	// Start SOCKS5 + transparent proxy.
	srv := proxy.NewServer(cfg, ring)
	if err := srv.Start(); err != nil {
		log.Fatalf("proxy: %v", err)
	}

	// Start web UI.
	ui := web.NewUI(*webAddr, cfg, srv, ring)
	if err := ui.Start(); err != nil {
		log.Fatalf("web ui: %v", err)
	}

	log.Printf("web UI  → http://localhost%s", *webAddr)
	log.Printf("SOCKS5  → %s", cfg.Proxy.ListenAddr)

	// On Windows (interactive), open the browser and show the tray icon.
	// On Linux/macOS or when running as a service, just wait for a signal.
	if !isWindowsService() {
		openBrowser("http://localhost" + *webAddr)
	}

	// runApp blocks until the user quits (via tray on Windows, or SIGINT on Linux).
	runApp(cfg, *webAddr, srv, ui)
	log.Println("shutting down…")
}
