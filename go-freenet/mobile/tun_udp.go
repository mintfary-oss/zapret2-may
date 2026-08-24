// UDP relay for the Android TUN forwarder.
//
// Each unique (srcIP, srcPort, dstIP, dstPort) tuple gets a local UDP socket
// protected from VPN routing (via SocketProtector) so responses flow directly
// from the remote host back through the TUN to the app.
//
// Sessions that have not seen traffic for udpIdleTimeout are closed
// automatically by a background sweeper goroutine.
package mobile

import (
	"log"
	"net"
	"sync"
	"time"
)

const (
	// udpIdleTimeout is how long an idle UDP session is kept alive.
	udpIdleTimeout = 30 * time.Second
	// udpMaxPacket is the maximum UDP payload size we relay.
	udpMaxPacket = 65507
)

// udpKey uniquely identifies one UDP flow seen on the TUN interface.
type udpKey struct {
	srcIP   [4]byte
	dstIP   [4]byte
	srcPort uint16
	dstPort uint16
}

// udpSession holds state for one active UDP relay flow.
type udpSession struct {
	mu            sync.Mutex
	conn          *net.UDPConn // protected local socket → remote
	srcIP         [4]byte      // original source IP (device side)
	srcPort       uint16       // original source port (device side)
	remoteSrcPort uint16       // destination port (used as src in responses)
	lastSeen      time.Time
	closed        bool
}

// startUDPRelay launches the idle-session sweeper goroutine.
// Called once from ForwardTUNWithDNS before the packet read loop.
func (fw *tunForwarder) startUDPRelay() {
	go fw.sweepIdleUDPSessions()
}

// handleUDPRelay relays a raw IPv4 UDP payload to its destination and
// registers a reverse goroutine to inject responses back into the TUN.
func (fw *tunForwarder) handleUDPRelay(
	srcIP, dstIP [4]byte,
	srcPort, dstPort uint16,
	payload []byte,
) {
	k := udpKey{srcIP: srcIP, dstIP: dstIP, srcPort: srcPort, dstPort: dstPort}

	// Fast path: session already exists.
	if v, ok := fw.udpConns.Load(k); ok {
		sess := v.(*udpSession)
		sess.mu.Lock()
		alive := !sess.closed
		if alive {
			sess.lastSeen = time.Now()
		}
		sess.mu.Unlock()
		if alive {
			dst := &net.UDPAddr{IP: net.IP(dstIP[:]).To4(), Port: int(dstPort)}
			_, _ = sess.conn.WriteTo(payload, dst)
			return
		}
		// Session was closed — remove stale entry and fall through.
		fw.udpConns.Delete(k)
	}

	// Create a new protected UDP socket bound to an OS-chosen local port.
	conn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		log.Printf("tun/udp: ListenPacket: %v", err)
		return
	}
	udpConn := conn.(*net.UDPConn)

	// Protect the socket so OS routes it outside the VPN (no routing loop).
	if fw.protector != nil {
		rawConn, rerr := udpConn.SyscallConn()
		if rerr == nil {
			_ = rawConn.Control(func(fd uintptr) {
				fw.protector.Protect(int64(fd))
			})
		}
	}

	sess := &udpSession{
		conn:          udpConn,
		srcIP:         srcIP,
		srcPort:       srcPort,
		remoteSrcPort: dstPort,
		lastSeen:      time.Now(),
	}
	fw.udpConns.Store(k, sess)

	// Goroutine: relay responses from remote → TUN.
	go fw.relayUDPResponses(sess, dstIP)

	// Send the first datagram.
	dst := &net.UDPAddr{IP: net.IP(dstIP[:]).To4(), Port: int(dstPort)}
	_, _ = udpConn.WriteTo(payload, dst)
}

// relayUDPResponses reads datagrams arriving on sess.conn and injects each
// one back into the TUN interface addressed to the original sender.
func (fw *tunForwarder) relayUDPResponses(sess *udpSession, remoteSrcIP [4]byte) {
	buf := make([]byte, udpMaxPacket)
	for {
		sess.conn.SetReadDeadline(time.Now().Add(udpIdleTimeout)) //nolint:errcheck
		n, addr, err := sess.conn.ReadFrom(buf)
		if err != nil {
			break
		}
		if n == 0 {
			continue
		}

		sess.mu.Lock()
		if sess.closed {
			sess.mu.Unlock()
			break
		}
		sess.lastSeen = time.Now()
		sess.mu.Unlock()

		// Use the actual response source address if it's an IPv4 address.
		srcIP4 := remoteSrcIP
		if udpAddr, ok := addr.(*net.UDPAddr); ok && udpAddr.IP != nil {
			if ip4 := udpAddr.IP.To4(); ip4 != nil {
				copy(srcIP4[:], ip4)
			}
		}

		// Inject response: src = remote, dst = original app sender.
		pkt := buildUDPPacket(srcIP4[:], sess.srcIP[:], sess.remoteSrcPort, sess.srcPort, buf[:n])
		if werr := fw.writePkt(pkt); werr != nil {
			break
		}
	}

	sess.mu.Lock()
	sess.closed = true
	sess.mu.Unlock()
	sess.conn.Close()
}

// sweepIdleUDPSessions periodically closes UDP sessions idle for longer
// than udpIdleTimeout to release file descriptors.
func (fw *tunForwarder) sweepIdleUDPSessions() {
	ticker := time.NewTicker(udpIdleTimeout / 2)
	defer ticker.Stop()
	for {
		select {
		case <-fw.done:
			return
		case <-ticker.C:
			now := time.Now()
			fw.udpConns.Range(func(k, v any) bool {
				sess := v.(*udpSession)
				sess.mu.Lock()
				idle := !sess.closed && now.Sub(sess.lastSeen) > udpIdleTimeout
				if idle {
					sess.closed = true
					sess.conn.Close()
				}
				sess.mu.Unlock()
				if idle {
					fw.udpConns.Delete(k)
				}
				return true
			})
		}
	}
}
