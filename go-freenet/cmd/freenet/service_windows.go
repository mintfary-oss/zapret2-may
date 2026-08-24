//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"

	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

const svcName = "FreeNet"
const svcDesc = "FreeNet — DPI bypass / Обход блокировок"

// installService registers the current executable as a Windows service
// named "FreeNet" with auto-start.  Must run as Administrator.
func installService(cfgPath, webAddr string) error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("open service manager: %w", err)
	}
	defer m.Disconnect()

	// Check if the service already exists.
	s, err := m.OpenService(svcName)
	if err == nil {
		s.Close()
		return fmt.Errorf("service %q already installed — run -uninstall first", svcName)
	}

	args := []string{
		"-config", cfgPath,
		"-web", webAddr,
	}

	s, err = m.CreateService(svcName, exePath, mgr.Config{
		DisplayName: svcName,
		Description: svcDesc,
		StartType:   mgr.StartAutomatic,
	}, args...)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()

	_ = eventlog.InstallAsEventCreate(svcName, eventlog.Error|eventlog.Warning|eventlog.Info)

	// Write the web UI port to the registry so tray/UI tools can find it.
	k, _, _ := registry.CreateKey(registry.LOCAL_MACHINE,
		`SOFTWARE\FreeNet`, registry.SET_VALUE)
	if k != 0 {
		_ = k.SetStringValue("WebAddr", webAddr)
		k.Close()
	}

	fmt.Printf("Service %q installed successfully.\n", svcName)
	fmt.Println("Starting service…")
	return s.Start()
}

// uninstallService stops and removes the "FreeNet" Windows service.
func uninstallService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("open service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(svcName)
	if err != nil {
		return fmt.Errorf("service %q not found: %w", svcName, err)
	}
	defer s.Close()

	// Stop the service first (ignore error if already stopped).
	_, _ = s.Control(svc.Stop)

	if err := s.Delete(); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	_ = eventlog.Remove(svcName)

	fmt.Printf("Service %q removed.\n", svcName)
	return nil
}

// isWindowsService reports whether the process was launched by the SCM.
func isWindowsService() bool {
	ok, _ := svc.IsWindowsService()
	return ok
}

// openBrowser opens the default browser to the given URL (Windows).
func openBrowser(url string) {
	_ = exec.Command("cmd", "/c", "start", url).Start()
}
