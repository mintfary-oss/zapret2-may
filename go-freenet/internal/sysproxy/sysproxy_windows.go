//go:build windows

// Package sysproxy manages the Windows system-wide SOCKS5 proxy setting.
//
// When FreeNet's bypass is enabled, [Set] writes the SOCKS5 address to the
// Windows Internet Settings registry key so that Chrome, Edge, and any app
// that honours the WinInet proxy automatically route traffic through FreeNet.
// [Restore] reverts the settings that were saved by the last [Set] call.
//
// All changes are scoped to HKCU (current user) — no administrator rights
// required for the proxy toggle itself.
package sysproxy

import (
	"fmt"
	"log"
	"sync"

	"golang.org/x/sys/windows/registry"
)

const (
	// inetSettingsKey is the registry path for Internet Explorer / WinInet proxy.
	inetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

	valProxyEnable = "ProxyEnable"
	valProxyServer = "ProxyServer"
	valProxyBypass = "ProxyOverride"
)

// saved holds the original proxy settings so we can restore them on exit.
type saved struct {
	proxyEnable uint32
	proxyServer string
	proxyBypass string
	wasSet      bool // true once a snapshot has been taken
}

var (
	mu       sync.Mutex
	snapshot saved
)

// Set saves the current Windows proxy settings and then activates a SOCKS5
// proxy at socksAddr (e.g. "127.0.0.1:1080").
//
// Bypass list is set to "localhost;127.*;10.*;172.16.*;192.168.*;<local>"
// so that LAN and local addresses are never routed through the proxy.
func Set(socksAddr string) error {
	mu.Lock()
	defer mu.Unlock()

	k, err := registry.OpenKey(registry.CURRENT_USER, inetSettingsKey,
		registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("sysproxy: open registry key: %w", err)
	}
	defer k.Close()

	// ── Snapshot existing settings ────────────────────────────────────────────
	if !snapshot.wasSet {
		enable, _, _ := k.GetIntegerValue(valProxyEnable)
		server, _, _ := k.GetStringValue(valProxyServer)
		bypass, _, _ := k.GetStringValue(valProxyBypass)
		snapshot = saved{
			proxyEnable: uint32(enable),
			proxyServer: server,
			proxyBypass: bypass,
			wasSet:      true,
		}
		log.Printf("sysproxy: saved previous settings (enable=%d server=%q)", enable, server)
	}

	// ── Apply FreeNet proxy ────────────────────────────────────────────────────
	// ProxyServer format for SOCKS5: "socks=host:port"
	// Internet Explorer also accepts "socks5=host:port" but "socks=" is safer
	// for compatibility with older WinInet.
	proxyStr := "socks=" + socksAddr
	bypass := "localhost;127.*;10.*;172.16.*;192.168.*;<local>"

	if err := k.SetDWordValue(valProxyEnable, 1); err != nil {
		return fmt.Errorf("sysproxy: set ProxyEnable: %w", err)
	}
	if err := k.SetStringValue(valProxyServer, proxyStr); err != nil {
		return fmt.Errorf("sysproxy: set ProxyServer: %w", err)
	}
	if err := k.SetStringValue(valProxyBypass, bypass); err != nil {
		return fmt.Errorf("sysproxy: set ProxyOverride: %w", err)
	}

	// Notify WinInet of the change so running browsers pick it up immediately.
	notifyWinInet()

	log.Printf("sysproxy: system SOCKS5 proxy set → %s", socksAddr)
	return nil
}

// Restore reverts the Windows proxy settings to the values that were
// saved during the last [Set] call.  Safe to call multiple times.
func Restore() {
	mu.Lock()
	defer mu.Unlock()

	if !snapshot.wasSet {
		return
	}

	k, err := registry.OpenKey(registry.CURRENT_USER, inetSettingsKey,
		registry.SET_VALUE)
	if err != nil {
		log.Printf("sysproxy: restore — cannot open registry key: %v", err)
		return
	}
	defer k.Close()

	_ = k.SetDWordValue(valProxyEnable, snapshot.proxyEnable)

	if snapshot.proxyServer != "" {
		_ = k.SetStringValue(valProxyServer, snapshot.proxyServer)
	} else {
		_ = k.DeleteValue(valProxyServer)
	}

	if snapshot.proxyBypass != "" {
		_ = k.SetStringValue(valProxyBypass, snapshot.proxyBypass)
	} else {
		_ = k.DeleteValue(valProxyBypass)
	}

	notifyWinInet()
	log.Println("sysproxy: system proxy settings restored")
	snapshot.wasSet = false
}
