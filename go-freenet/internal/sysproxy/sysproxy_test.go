//go:build !windows

// Package sysproxy manages the system-wide proxy setting.
// On non-Windows platforms both functions are no-ops; this file
// verifies they compile and return the correct values.
package sysproxy

import "testing"

func TestSet_Stub(t *testing.T) {
	if err := Set("127.0.0.1:1080"); err != nil {
		t.Errorf("Set() on non-Windows should return nil, got: %v", err)
	}
}

func TestRestore_Stub(t *testing.T) {
	// Should not panic or return any error.
	Restore()
}

func TestSet_EmptyAddr(t *testing.T) {
	if err := Set(""); err != nil {
		t.Errorf("Set(\"\") on non-Windows should return nil, got: %v", err)
	}
}
