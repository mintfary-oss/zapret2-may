//go:build !windows

package main

import "fmt"

func installService(_, _ string) error {
	return fmt.Errorf("service install is Windows-only; use scripts/install.sh on Linux")
}

func uninstallService() error {
	return fmt.Errorf("service uninstall is Windows-only")
}

func isWindowsService() bool { return false }

func openBrowser(url string) {} // no-op on non-Windows for now
