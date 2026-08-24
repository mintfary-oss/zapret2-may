//go:build !linux

// Stub transparent-proxy implementation for non-Linux platforms.
// Transparent proxy (iptables REDIRECT + SO_ORIGINAL_DST) is a Linux-only
// feature. On other platforms the transparent listener is simply never started.
package proxy

import "net"

// acceptTransparent is a no-op on non-Linux platforms.
// The transparent listener is not started if the platform is not Linux, so
// this function is never called. It exists only to satisfy the compiler when
// server.go references it in a build-tag-independent code path.
func (s *Server) acceptTransparent(_ net.Listener) {}
