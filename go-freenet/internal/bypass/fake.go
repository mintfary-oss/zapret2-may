// Package bypass — fake packet injection interface.
//
// The fake-packet strategy sends one or more decoy TCP segments to the
// remote server immediately before the real TLS ClientHello.  DPI boxes
// process the decoy and misclassify the connection; the server ignores
// the decoy (wrong checksum or expired TTL).
//
// Two variants are supported:
//
//	ttl  — decoy has a low TTL (e.g. 4) and expires at a router before
//	        reaching the server.  Effective when the DPI box is 2–4 hops
//	        closer than the destination server.
//
//	md5  — decoy has a correct TTL but an invalid TCP checksum (all bytes
//	        flipped).  The TCP stack on the server drops it silently;
//	        most DPI boxes do not validate checksums and inspect the payload.
//
// Both variants require CAP_NET_RAW (raw socket) privilege on Linux.
// On other platforms the strategy falls back to split.
package bypass

import "net"

// FakeMode selects which decoy technique to use.
type FakeMode int

const (
	FakeModeTTL FakeMode = iota // low-TTL decoy
	FakeModeMD5                 // bad-checksum decoy
)

// fakeSender is the platform-specific implementation.
// Defined in fake_linux.go and fake_stub.go.
type fakeSender interface {
	sendFake(srcIP, dstIP net.IP, srcPort, dstPort uint16, seqOff uint32, payload []byte, mode FakeMode, ttl uint8) error
	close()
}

// globalFakeSender is initialised once on startup; nil if unavailable.
var globalFakeSender fakeSender

// RawSend is a package-level helper exposed to the nfqueue handler so it can
// re-inject split packets using the same raw socket. Nil if CAP_NET_RAW is
// not available.
var RawSend func(srcIP, dstIP net.IP, srcPort, dstPort uint16, seq uint32, payload []byte, ttl uint8, badCS bool) error

func init() {
	s, err := newFakeSender()
	if err == nil {
		globalFakeSender = s
		// Expose a thin wrapper for nfqueue re-injection.
		RawSend = func(srcIP, dstIP net.IP, srcPort, dstPort uint16, seq uint32, payload []byte, ttl uint8, badCS bool) error {
			mode := FakeModeTTL
			if badCS {
				mode = FakeModeMD5
			}
			return s.sendFake(srcIP, dstIP, srcPort, dstPort, seq, payload, mode, ttl)
		}
	}
	// If opening the raw socket fails (no CAP_NET_RAW), both globalFakeSender
	// and RawSend stay nil; callers check for nil before using them.
}

// relayFake sends decoy packets then relays with a split.
// conn must be the already-connected outbound *net.TCPConn.
func relayFake(client, remote net.Conn, cfg fakeConfig) {
	if globalFakeSender == nil {
		// No raw-socket access — fall back to split.
		relaySplit(client, remote, cfg.SplitPos)
		return
	}

	// Read the first data chunk from the client (TLS ClientHello).
	first := make([]byte, 4096)
	n, err := client.Read(first)
	if err != nil || n == 0 {
		return
	}
	first = first[:n]

	localTCP, remoteAddr := tcpAddrPair(remote)
	if localTCP == nil || remoteAddr == nil {
		// Can't determine addresses — fall back.
		setTCPNoDelay(remote, true)
		_, _ = remote.Write(first)
		setTCPNoDelay(remote, false)
		relayPlain(client, remote)
		return
	}

	// Build a fake TLS ClientHello with a benign-looking domain.
	fakeHello := buildMinimalClientHello("www.bing.com")

	// Decoy sequence number: we use offset 0 for the fake and 0 for the
	// real as well.  The server TCP stack silently reorders/drops duplicates.
	// The DPI box (stateless or loosely stateful) processes the first one it
	// sees and may classify the connection based on the fake SNI.
	mode := FakeModeTTL
	if cfg.MD5Fake {
		mode = FakeModeMD5
	}
	_ = globalFakeSender.sendFake(
		localTCP.IP, remoteAddr.IP,
		uint16(localTCP.Port), uint16(remoteAddr.Port),
		0,
		fakeHello,
		mode,
		uint8(cfg.FakeTTL),
	)

	// Now send the real ClientHello, split at the SNI boundary.
	info, _ := ParseClientHello(first)
	pos := SplitPosition(info, cfg.SplitPos)
	if pos <= 0 || pos >= n {
		pos = 1
	}

	setTCPNoDelay(remote, true)
	_, _ = remote.Write(first[:pos])
	_, _ = remote.Write(first[pos:])
	setTCPNoDelay(remote, false)

	relayPlain(client, remote)
}

// fakeConfig holds parameters for the fake strategy.
type fakeConfig struct {
	FakeTTL  int
	SplitPos int
	MD5Fake  bool
}

// tcpAddrPair extracts local and remote *net.TCPAddr from a connection.
func tcpAddrPair(conn net.Conn) (local, remote *net.TCPAddr) {
	if tc, ok := conn.(*net.TCPConn); ok {
		if l, ok2 := tc.LocalAddr().(*net.TCPAddr); ok2 {
			local = l
		}
		if r, ok2 := tc.RemoteAddr().(*net.TCPAddr); ok2 {
			remote = r
		}
	}
	return
}
