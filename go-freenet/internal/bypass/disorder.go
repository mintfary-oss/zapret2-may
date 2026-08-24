// Disorder strategy: send TCP segments in a non-standard order.
//
// At the application layer (without raw sockets) we simulate disorder by:
//  1. Sending a small "probe" write (pos bytes) with a very short sleep.
//  2. Sending the rest with TCP_NODELAY set.
//
// This is a best-effort approach that works against some DPI systems.
// True out-of-order segment injection requires root + raw sockets and is
// implemented in the optional nfqueue integration (Linux-only).
package bypass

import (
	"net"
	"time"
)

// relayDisorder attempts an application-level disorder attack and falls back
// to split if the connection is not a TCPConn.
func relayDisorder(client, remote net.Conn, splitPos int) {
	first := make([]byte, 4096)
	n, err := client.Read(first)
	if err != nil || n == 0 {
		return
	}
	first = first[:n]

	info, _ := ParseClientHello(first)
	pos := SplitPosition(info, splitPos)
	if pos <= 0 || pos >= n {
		pos = 1
	}

	setTCPNoDelay(remote, true)

	// Write the tail first — some DPI systems assume ordering and get confused.
	if _, err := remote.Write(first[pos:]); err != nil {
		return
	}

	// Brief pause so the kernel flushes the segment.
	time.Sleep(2 * time.Millisecond)

	// Write the head — the server receives both and reassembles correctly
	// (TCP guarantees ordering for the receiver), but passive DPI that only
	// looks at the first segment may miss the SNI.
	//
	// NOTE: This only confuses passive/stateless DPI.  Full stateful
	//       reassembly will see the original stream unchanged.
	if _, err := remote.Write(first[:pos]); err != nil {
		return
	}

	setTCPNoDelay(remote, false)

	relayPlain(client, remote)
}

// setNoDelay is a package-level alias kept for compatibility.
func setNoDelay(conn net.Conn, v bool) {
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(v)
	}
}
