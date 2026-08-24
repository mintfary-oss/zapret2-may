//go:build !windows

// Package sysproxy manages the system-wide proxy setting.
// On non-Windows platforms this is a no-op — transparent proxy via iptables
// handles traffic interception instead.
package sysproxy

// Set is a no-op on non-Windows systems.
func Set(_ string) error { return nil }

// Restore is a no-op on non-Windows systems.
func Restore() {}
