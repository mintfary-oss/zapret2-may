// Package windivert wraps WinDivert 2.x for transparent DPI bypass on Windows.
//
// On Windows, FreeNet can intercept all outbound TCP connections at the
// network layer via the WinDivert kernel driver.  This lets the DPI bypass
// strategies (split, fake, tlsrec) apply to every application — not just
// SOCKS5-aware browsers — without any proxy configuration.
//
// The WinDivert.dll and WinDivert64.sys files must be present in the same
// directory as the freenet.exe binary.  They are bundled automatically in the
// GitHub Releases Windows archive.
//
// On non-Windows platforms this package compiles to a no-op stub so the rest
// of the codebase imports it unconditionally.
package windivert
