// Split strategy: fragment the TLS ClientHello across multiple TCP writes.
// Many Russian ISP DPI boxes (TSPU) fail to reconstruct the SNI field
// when it arrives in separate TCP segments, so the block trigger is not fired.
package bypass

import (
	"io"
	"net"
)

// relaySplit reads the first chunk from client, parses it for a TLS
// ClientHello, splits at the SNI boundary (or defaultPos if not found),
// and then falls back to plain bidirectional relay.
func relaySplit(client, remote net.Conn, defaultPos int) {
	first := make([]byte, 4096)
	n, err := client.Read(first)
	if err != nil || n == 0 {
		return
	}
	first = first[:n]

	// Try to parse as TLS ClientHello to find the best split position.
	info, _ := ParseClientHello(first)
	pos := SplitPosition(info, defaultPos)

	// Clamp to valid range.
	if pos <= 0 || pos >= n {
		pos = 1
	}

	// Enable TCP_NODELAY: each Write() flushes as a separate TCP segment.
	setTCPNoDelay(remote, true)

	if _, err := remote.Write(first[:pos]); err != nil {
		return
	}
	if _, err := remote.Write(first[pos:]); err != nil {
		return
	}

	// Restore Nagle algorithm after the split.
	setTCPNoDelay(remote, false)

	relayPlain(client, remote)
}

// relayPlain is a simple bidirectional copy with no DPI modifications.
func relayPlain(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(b, a)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(a, b)
		done <- struct{}{}
	}()
	<-done
}

// setTCPNoDelay enables/disables the Nagle algorithm on conn if it is a
// *net.TCPConn.
func setTCPNoDelay(conn net.Conn, v bool) {
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(v)
	}
}
