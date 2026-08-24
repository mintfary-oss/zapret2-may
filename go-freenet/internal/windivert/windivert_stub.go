//go:build !windows

package windivert

import (
	"errors"
	"net"
)

// ErrNotAvailable is returned on non-Windows platforms.
var ErrNotAvailable = errors.New("windivert: only available on Windows")

// Handle is a no-op placeholder on non-Windows platforms.
type Handle struct{}

// Open always returns ErrNotAvailable on non-Windows platforms.
func Open() (*Handle, error) { return nil, ErrNotAvailable }

// Close is a no-op on non-Windows platforms.
func (h *Handle) Close() {}

// RunIntercept is a no-op on non-Windows platforms.
func (h *Handle) RunIntercept(_ string) {}

// Available reports false on non-Windows platforms.
func Available() bool { return false }

// IsLoopback always returns false on non-Windows platforms.
func IsLoopback(_ []byte) bool { return false }

// SrcIP always returns nil on non-Windows platforms.
func SrcIP(_ []byte) net.IP { return nil }

// DstIP always returns nil on non-Windows platforms.
func DstIP(_ []byte) net.IP { return nil }
