//go:build windows

package main

// Windows system-tray integration for FreeNet.
//
// When the application is launched interactively (not as a Windows Service),
// this file replaces the plain signal-wait loop with a proper tray icon that
// lets the user toggle bypass, switch strategies, and open the web UI without
// ever touching a terminal.
//
// The icon is embedded at compile time from icon.png (64×64 PNG).

import (
	_ "embed"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/getlantern/systray"

	"github.com/mintfary-oss/freenet/internal/config"
	"github.com/mintfary-oss/freenet/internal/proxy"
	"github.com/mintfary-oss/freenet/internal/web"
)

//go:embed icon.png
var iconData []byte

// runApp on Windows:
//   - Shows a system-tray icon when running interactively.
//   - Falls back to a plain signal wait when running as a Windows Service
//     (the SCM controls the lifecycle in that case).
func runApp(cfg *config.Config, webAddr string, srv *proxy.Server, ui *web.UI) {
	if isWindowsService() {
		// Running as a service — no tray needed, just block on signal.
		waitForSignal(srv, ui)
		return
	}
	systray.Run(
		func() { trayOnReady(webAddr, srv) },
		func() {
			srv.Stop()
			ui.Stop()
			os.Exit(0)
		},
	)
}

// trayOnReady is called by systray once the tray icon is initialised.
// It sets up the menu and launches goroutines to handle each item.
func trayOnReady(webAddr string, srv *proxy.Server) {
	systray.SetIcon(iconData)
	systray.SetTooltip("FreeNet — Обход блокировок / DPI bypass")

	// ── Status label (non-clickable) ─────────────────────────────────────────
	mStatus := systray.AddMenuItem(statusLabel(srv.Enabled()), "Текущее состояние bypass")
	mStatus.Disable()
	systray.AddSeparator()

	// ── Toggle bypass ─────────────────────────────────────────────────────────
	mToggle := systray.AddMenuItem(toggleLabel(srv.Enabled()), "Включить или выключить обход DPI")
	systray.AddSeparator()

	// ── Strategy selector ─────────────────────────────────────────────────────
	mStratLabel := systray.AddMenuItem(stratLabel(srv.Strategy()), "Текущая стратегия обхода")
	mStratLabel.Disable()

	type strategyEntry struct {
		id    string
		title string
	}
	strategies := []strategyEntry{
		{"auto", "Auto — авто-определение"},
		{"split", "Split — TCP фрагментация"},
		{"tlsrec", "TLS Record splitting"},
		{"combined", "Combined — fake + tlsrec"},
		{"fake", "Fake packets (нужен root/admin)"},
		{"quic", "QUIC bypass"},
		{"none", "Нет bypass (отладка)"},
	}

	stratItems := make([]*systray.MenuItem, len(strategies))
	for i, s := range strategies {
		checked := s.id == srv.Strategy()
		stratItems[i] = systray.AddMenuItemCheckbox("  "+s.title, "", checked)
	}
	systray.AddSeparator()

	// ── Open web UI ───────────────────────────────────────────────────────────
	mOpenUI := systray.AddMenuItem("Открыть веб-интерфейс", fmt.Sprintf("Открыть http://localhost%s", webAddr))
	systray.AddSeparator()

	// ── Quit ──────────────────────────────────────────────────────────────────
	mQuit := systray.AddMenuItem("Выйти из FreeNet", "Остановить прокси и закрыть")

	// ── Toggle handler ────────────────────────────────────────────────────────
	go func() {
		for range mToggle.ClickedCh {
			next := !srv.Enabled()
			srv.SetEnabled(next)
			mToggle.SetTitle(toggleLabel(next))
			mStatus.SetTitle(statusLabel(next))
		}
	}()

	// ── Strategy handlers ─────────────────────────────────────────────────────
	for i, item := range stratItems {
		i, item := i, item // capture loop variables
		go func() {
			for range item.ClickedCh {
				id := strategies[i].id
				srv.SetStrategy(id)
				mStratLabel.SetTitle(stratLabel(id))
				for j, mi := range stratItems {
					if j == i {
						mi.Check()
					} else {
						mi.Uncheck()
					}
				}
			}
		}()
	}

	// ── Open UI handler ───────────────────────────────────────────────────────
	go func() {
		for range mOpenUI.ClickedCh {
			openBrowser("http://localhost" + webAddr)
		}
	}()

	// ── Quit handler ──────────────────────────────────────────────────────────
	go func() {
		<-mQuit.ClickedCh
		systray.Quit()
	}()
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func statusLabel(enabled bool) string {
	if enabled {
		return "● Bypass включён"
	}
	return "○ Bypass выключен"
}

func toggleLabel(enabled bool) string {
	if enabled {
		return "Выключить bypass"
	}
	return "Включить bypass"
}

func stratLabel(strategy string) string {
	return fmt.Sprintf("Стратегия: %s", strategy)
}

// waitForSignal blocks until SIGINT or SIGTERM, then gracefully stops the
// server and web UI.
func waitForSignal(srv *proxy.Server, ui *web.UI) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	srv.Stop()
	ui.Stop()
}
