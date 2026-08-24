// FreeNet — cross-platform DPI bypass tool for Go.
// Intercepts TCP/UDP traffic and applies desynchronization strategies
// to evade Deep Packet Inspection used by Russian ISPs (TSPU/RKN).
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/mintfary-oss/freenet/internal/config"
	"github.com/mintfary-oss/freenet/internal/logs"
	"github.com/mintfary-oss/freenet/internal/proxy"
	"github.com/mintfary-oss/freenet/internal/web"
)

const version = "0.1.0"

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config file")
	webAddr := flag.String("web", ":8080", "web UI listen address")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("freenet %s\n", version)
		os.Exit(0)
	}

	// Load configuration (creates default if missing).
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// Initialise shared ring-buffer log that the web UI streams.
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

	log.Printf("web UI → http://localhost%s", *webAddr)
	log.Printf("SOCKS5 proxy → %s", cfg.Proxy.ListenAddr)

	// Block until SIGINT/SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down…")
	srv.Stop()
	ui.Stop()
}
