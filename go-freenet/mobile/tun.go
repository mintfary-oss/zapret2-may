// Package mobile — IPv4 TUN forwarder for Android VpnService.
//
// ForwardTUN reads raw IPv4 packets from the TUN file descriptor created by
// Android's VpnService and handles them as follows:
//
//   - TCP: proxied through the local SOCKS5 bypass engine via a minimal
//     userspace TCP state machine (SYN→SYN-ACK→data→FIN).
//
//   - UDP port 53 (DNS): resolved via DNS-over-HTTPS to prevent ISP DNS
//     poisoning.
//
//   - UDP (other ports): relayed directly to the destination via a protected
//     UDP socket (UDP NAT table, see tun_udp.go).  This covers Discord,
//     gaming, video calls, and QUIC/HTTP3 on arbitrary ports.
//
//   - IPv6: currently not processed; exclude "::/0" from VpnService routes
//     to let IPv6 traffic bypass the VPN entirely.
package mobile

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mintfary-oss/freenet/internal/dns"
)

// SocketProtector is implemented by the Android VpnService (via gomobile
// binding) to mark bypass-proxy sockets so they are excluded from the VPN
// routing table (preventing a routing loop).
//
// Kotlin example:
//
//	class GoSocketProtector(private val svc: VpnService) : Mobile.SocketProtector {
//	    override fun protect(fd: Long): Boolean = svc.protect(fd.toInt())
//	}
type SocketProtector interface {
	// Protect marks the socket with the given file descriptor so that the OS
	// routes it directly (bypassing the VPN TUN interface).
	Protect(fd int64) bool
}

// tunForwarder holds the state for one active TUN forwarding session.
type tunForwarder struct {
	tun       *os.File
	socksAddr string
	protector SocketProtector
	dohClient *dns.Client // nil = DoH DNS intercept disabled

	conns    sync.Map // key: connKey   → *tunConn    (TCP sessions)
	udpConns sync.Map // key: udpKey    → *udpSession (UDP relay sessions)
	closed   atomic.Bool
	done     chan struct{}
}

// connKey uniquely identifies one TCP connection seen on the TUN interface.
type connKey struct {
	srcIP   [4]byte
	dstIP   [4]byte
	srcPort uint16
	dstPort uint16
}

func (k connKey) String() string {
	return fmt.Sprintf("%d.%d.%d.%d:%d→%d.%d.%d.%d:%d",
		k.srcIP[0], k.srcIP[1], k.srcIP[2], k.srcIP[3], k.srcPort,
		k.dstIP[0], k.dstIP[1], k.dstIP[2], k.dstIP[3], k.dstPort)
}

// tunConn tracks one proxied TCP connection.
type tunConn struct {
	key      connKey
	upstream net.Conn // SOCKS5 connection to the bypass proxy

	// TCP sequence / acknowledgment numbers (device side).
	clientISN  uint32 // initial seq num from device SYN
	serverISN  uint32 // our initial seq num in SYN-ACK
	clientNext uint32 // next expected seq from device
	serverNext uint32 // next seq we will send to device

	mu     sync.Mutex
	closed bool
}

// ForwardTUN starts the TUN packet-forwarding loop with DoH DNS protection
// enabled by default (uses Cloudflare / Google / Quad9).
//
// Parameters:
//   - tunFd:     file descriptor of the TUN interface (from VpnService.Builder.establish()).
//   - socksAddr: local SOCKS5 bypass proxy address, e.g. "127.0.0.1:1080".
//   - protector: Android VpnService socket protector (may be nil in unit tests).
//
// Blocks until the TUN fd is closed or an unrecoverable error occurs.
func ForwardTUN(tunFd int64, socksAddr string, protector SocketProtector) error {
	return ForwardTUNWithDNS(tunFd, socksAddr, nil, protector)
}

// ForwardTUNWithDNS is like ForwardTUN but lets the caller specify the DoH
// server URLs to use for DNS interception.  Pass nil for dohServers to use the
// default set (Cloudflare, Google, Quad9).  Passing an empty non-nil slice
// disables DNS interception entirely.
func ForwardTUNWithDNS(tunFd int64, socksAddr string, dohServers []string, protector SocketProtector) error {
	f := os.NewFile(uintptr(tunFd), "tun")
	if f == nil {
		return fmt.Errorf("tun: invalid file descriptor %d", tunFd)
	}

	// Build a DoH client unless the caller explicitly opts out by passing an
	// empty (non-nil) slice.
	var dohClient *dns.Client
	if dohServers == nil || len(dohServers) > 0 {
		dohClient = dns.NewClient(dohServers)
		log.Printf("tun: DNS-over-HTTPS interception enabled (%d servers)", len(dohClient.Servers()))
	}

	fw := &tunForwarder{
		tun:       f,
		socksAddr: socksAddr,
		protector: protector,
		dohClient: dohClient,
		done:      make(chan struct{}),
	}

	// Start background goroutines.
	fw.startUDPRelay() // UDP NAT sweeper

	log.Printf("tun: forwarder started (socks5 → %s)", socksAddr)
	return fw.run()
}

// run is the main read-loop.
func (fw *tunForwarder) run() error {
	defer close(fw.done) // signal background goroutines (UDP sweeper etc.) to stop
	buf := make([]byte, 65535)
	for {
		n, err := fw.tun.Read(buf)
		if err != nil {
			if fw.closed.Load() {
				return nil
			}
			return fmt.Errorf("tun: read: %w", err)
		}
		if n < 20 {
			continue
		}
		fw.handlePacket(buf[:n])
	}
}

// handlePacket dispatches one IPv4 packet.
func (fw *tunForwarder) handlePacket(pkt []byte) {
	// Require IPv4.
	if pkt[0]>>4 != 4 {
		return
	}
	ihl := int(pkt[0]&0x0f) * 4
	if len(pkt) < ihl+20 {
		return
	}
	proto := pkt[9]

	var srcIP, dstIP [4]byte
	copy(srcIP[:], pkt[12:16])
	copy(dstIP[:], pkt[16:20])

	// Handle UDP packets.
	if proto == 17 {
		udp := pkt[ihl:]
		if len(udp) < 8 {
			return
		}
		srcPort := binary.BigEndian.Uint16(udp[0:2])
		dstPort := binary.BigEndian.Uint16(udp[2:4])
		udpLen := int(binary.BigEndian.Uint16(udp[4:6]))
		if udpLen < 8 || len(udp) < udpLen {
			return
		}
		udpPayload := make([]byte, udpLen-8)
		copy(udpPayload, udp[8:udpLen])

		if dstPort == 53 && fw.dohClient != nil {
			// Intercept DNS queries and resolve via DoH.
			go fw.handleDNSQuery(srcIP, dstIP, srcPort, dstPort, udpPayload)
		} else {
			// Relay all other UDP traffic through a protected socket.
			fw.handleUDPRelay(srcIP, dstIP, srcPort, dstPort, udpPayload)
		}
		return
	}

	if proto != 6 { // everything other than TCP: drop
		return
	}

	tcp := pkt[ihl:]
	if len(tcp) < 20 {
		return
	}
	srcPort := binary.BigEndian.Uint16(tcp[0:2])
	dstPort := binary.BigEndian.Uint16(tcp[2:4])
	seqNum := binary.BigEndian.Uint32(tcp[4:8])
	ackNum := binary.BigEndian.Uint32(tcp[8:12])
	dataOff := int(tcp[12]>>4) * 4
	flags := tcp[13]

	k := connKey{srcIP: srcIP, dstIP: dstIP, srcPort: srcPort, dstPort: dstPort}

	const (
		flagFIN = 0x01
		flagSYN = 0x02
		flagRST = 0x04
		flagPSH = 0x08
		flagACK = 0x10
	)

	isSYN := flags&flagSYN != 0
	isACK := flags&flagACK != 0
	isFIN := flags&flagFIN != 0
	isRST := flags&flagRST != 0

	switch {
	case isSYN && !isACK:
		// New connection request from device.
		go fw.handleSYN(k, seqNum)

	case isFIN || isRST:
		// Teardown.
		if c, ok := fw.conns.LoadAndDelete(k); ok {
			tc := c.(*tunConn)
			tc.mu.Lock()
			tc.closed = true
			tc.mu.Unlock()
			if tc.upstream != nil {
				_ = tc.upstream.Close()
			}
			log.Printf("tun: closed %s", k)
		}

	case isACK:
		// ACK or data packet.
		if dataOff >= len(tcp) {
			return // pure ACK, no data
		}
		payload := tcp[dataOff:]
		if len(payload) == 0 {
			return
		}
		if c, ok := fw.conns.Load(k); ok {
			tc := c.(*tunConn)
			// Validate sequence number.
			tc.mu.Lock()
			if seqNum != tc.clientNext {
				tc.mu.Unlock()
				return // out-of-order; drop (no SACK)
			}
			tc.clientNext += uint32(len(payload))
			seq := tc.serverNext
			ack := tc.clientNext
			tc.mu.Unlock()
			_ = seqNum // used for validation above
			_ = ackNum

			// Send an immediate ACK back to the device so its retransmission
			// timer does not fire before we have received the server response.
			// Without this ACK the device retransmits (typically after ~200 ms),
			// sending a duplicate TLS ClientHello to the bypass proxy and
			// corrupting the TLS handshake — especially visible on blocked sites
			// where the server response arrives slowly.
			_ = fw.writePkt(buildACK(k.dstIP[:], k.srcIP[:], k.dstPort, k.srcPort, seq, ack))

			if tc.upstream != nil {
				if _, err := tc.upstream.Write(payload); err != nil {
					fw.conns.Delete(k)
					_ = tc.upstream.Close()
				}
			}
		}
	}
}

// handleDNSQuery intercepts a UDP DNS query from the TUN and resolves it via
// DNS-over-HTTPS, then injects the response back as a UDP packet on the TUN.
// This prevents the ISP from seeing or forging DNS queries.
//
// Fallback strategy (to prevent "VPN on but no internet"):
//  1. Try DoH (encrypted, bypasses ISP DNS poisoning).
//  2. If DoH fails, try plain UDP DNS to a public resolver via a direct socket
//     (the VPN service process is excluded from the TUN, so the socket bypasses
//     the VPN automatically — no routing loop).
//  3. If both fail, inject a SERVFAIL response so the device knows DNS failed
//     immediately (rather than waiting for its own timeout and getting stuck).
func (fw *tunForwarder) handleDNSQuery(srcIP, dstIP [4]byte, srcPort, dstPort uint16, query []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Attempt 1: DoH.
	resp, err := fw.dohClient.Exchange(ctx, query)
	if err == nil {
		// Build a UDP response packet swapping src/dst so it looks like it came
		// from the DNS server the device originally queried.
		pkt := buildUDPPacket(dstIP[:], srcIP[:], dstPort, srcPort, resp)
		if werr := fw.writePkt(pkt); werr != nil {
			log.Printf("tun: write DoH response: %v", werr)
		}
		return
	}
	log.Printf("tun: DoH failed (%v) — trying direct UDP DNS fallback", err)

	// Attempt 2: plain UDP DNS to 8.8.8.8:53 or 1.1.1.1:53 via a direct socket.
	// Works because the VPN service process bypasses the TUN (addDisallowedApplication).
	for _, fallback := range []string{"8.8.8.8:53", "1.1.1.1:53", "9.9.9.9:53"} {
		resp, err = queryDNSUDP(ctx, fallback, query)
		if err == nil {
			log.Printf("tun: DNS fallback via %s succeeded", fallback)
			pkt := buildUDPPacket(dstIP[:], srcIP[:], dstPort, srcPort, resp)
			if werr := fw.writePkt(pkt); werr != nil {
				log.Printf("tun: write UDP DNS response: %v", werr)
			}
			return
		}
		log.Printf("tun: DNS fallback %s failed: %v", fallback, err)
	}

	// Attempt 3: send SERVFAIL so the device fails immediately instead of
	// waiting for its own DNS timeout.
	log.Printf("tun: all DNS resolvers failed — sending SERVFAIL")
	if servfail := buildSERVFAIL(query); servfail != nil {
		pkt := buildUDPPacket(dstIP[:], srcIP[:], dstPort, srcPort, servfail)
		_ = fw.writePkt(pkt)
	}
}

// queryDNSUDP sends a raw DNS query over UDP to addr and returns the wire-format
// response.  Uses the Go standard library — the VPN service process is excluded
// from the TUN so the socket goes directly to the internet.
func queryDNSUDP(ctx context.Context, addr string, query []byte) ([]byte, error) {
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	if _, err := conn.Write(query); err != nil {
		return nil, err
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

// buildSERVFAIL constructs a minimal DNS SERVFAIL response for query.
// Returns nil if query is too short to extract a transaction ID.
func buildSERVFAIL(query []byte) []byte {
	if len(query) < 2 {
		return nil
	}
	// Response: same ID, QR=1 (response), RCODE=2 (SERVFAIL), zero counts.
	return []byte{
		query[0], query[1], // ID (copied from query)
		0x80, 0x02, // QR=1, OPCODE=0, AA=0, TC=0, RD=1, RA=0, RCODE=2 (SERVFAIL)
		0x00, 0x00, // QDCOUNT = 0 (omit question for simplicity)
		0x00, 0x00, // ANCOUNT = 0
		0x00, 0x00, // NSCOUNT = 0
		0x00, 0x00, // ARCOUNT = 0
	}
}

// handleSYN processes a new TCP SYN: connects to SOCKS5 and sends SYN-ACK.
func (fw *tunForwarder) handleSYN(k connKey, clientISN uint32) {
	// Connect to SOCKS5 proxy.
	dst := fmt.Sprintf("%d.%d.%d.%d:%d",
		k.dstIP[0], k.dstIP[1], k.dstIP[2], k.dstIP[3], k.dstPort)

	upstream, err := fw.dialSocks5(dst)
	if err != nil {
		log.Printf("tun: socks5 dial %s: %v", dst, err)
		// Send RST back to device.
		_ = fw.writePkt(buildRST(k.dstIP[:], k.srcIP[:], k.dstPort, k.srcPort, 0, clientISN+1))
		return
	}

	//nolint:gosec // Pseudo-random ISN is acceptable for a local VPN proxy.
	serverISN := rand.Uint32()

	tc := &tunConn{
		key:        k,
		upstream:   upstream,
		clientISN:  clientISN,
		serverISN:  serverISN,
		clientNext: clientISN + 1,
		serverNext: serverISN + 1,
	}
	fw.conns.Store(k, tc)

	// Reply with SYN-ACK.
	err = fw.writePkt(buildSYNACK(
		k.dstIP[:], k.srcIP[:],
		k.dstPort, k.srcPort,
		serverISN, clientISN+1,
	))
	if err != nil {
		log.Printf("tun: write synack: %v", err)
		fw.conns.Delete(k)
		_ = upstream.Close()
		return
	}

	log.Printf("tun: new conn %s", k)

	// Start goroutine to relay upstream → TUN.
	go fw.relayUpstreamToTUN(tc)
}

// relayUpstreamToTUN reads data from the SOCKS5 upstream and injects TCP
// data packets back into the TUN interface (device side).
func (fw *tunForwarder) relayUpstreamToTUN(tc *tunConn) {
	k := tc.key
	defer func() {
		fw.conns.Delete(k)
		_ = tc.upstream.Close()
		// Send FIN to device.
		tc.mu.Lock()
		seq := tc.serverNext
		ack := tc.clientNext
		tc.mu.Unlock()
		_ = fw.writePkt(buildFIN(k.dstIP[:], k.srcIP[:], k.dstPort, k.srcPort, seq, ack))
	}()

	buf := make([]byte, 4096)
	for {
		_ = tc.upstream.SetReadDeadline(time.Now().Add(60 * time.Second))
		n, err := tc.upstream.Read(buf)
		if n > 0 {
			tc.mu.Lock()
			seq := tc.serverNext
			ack := tc.clientNext
			tc.serverNext += uint32(n)
			tc.mu.Unlock()

			pkt := buildData(k.dstIP[:], k.srcIP[:], k.dstPort, k.srcPort, seq, ack, buf[:n])
			if werr := fw.writePkt(pkt); werr != nil {
				return
			}
		}
		if err != nil {
			if err != io.EOF {
				tc.mu.Lock()
				if !tc.closed {
					log.Printf("tun: upstream read %s: %v", k, err)
				}
				tc.mu.Unlock()
			}
			return
		}
	}
}

// writePkt writes a single IP packet to the TUN interface.
func (fw *tunForwarder) writePkt(pkt []byte) error {
	_, err := fw.tun.Write(pkt)
	return err
}

// dialSocks5 connects to the local SOCKS5 proxy and sends the CONNECT request
// for the target address, returning the established connection.
func (fw *tunForwarder) dialSocks5(target string) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", fw.socksAddr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to socks5 server: %w", err)
	}

	// Protect the socket so it bypasses the TUN (avoids routing loop).
	if fw.protector != nil {
		rawConn, _ := conn.(*net.TCPConn).SyscallConn()
		if rawConn != nil {
			_ = rawConn.Control(func(fd uintptr) {
				fw.protector.Protect(int64(fd))
			})
		}
	}

	// SOCKS5 handshake: no-auth.
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		conn.Close()
		return nil, err
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil || resp[0] != 0x05 || resp[1] != 0x00 {
		conn.Close()
		return nil, fmt.Errorf("socks5 auth failed")
	}

	// SOCKS5 CONNECT request.
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		conn.Close()
		return nil, err
	}
	port, err := net.LookupPort("tcp", portStr)
	if err != nil {
		conn.Close()
		return nil, err
	}

	req := make([]byte, 0, 32)
	req = append(req, 0x05, 0x01, 0x00) // VER CMD RSV
	ip := net.ParseIP(host).To4()
	if ip != nil {
		req = append(req, 0x01) // ATYP: IPv4
		req = append(req, ip...)
	} else {
		req = append(req, 0x03) // ATYP: domain
		req = append(req, byte(len(host)))
		req = append(req, []byte(host)...)
	}
	req = append(req, byte(port>>8), byte(port))
	if _, err := conn.Write(req); err != nil {
		conn.Close()
		return nil, err
	}

	// Read SOCKS5 response (at least 10 bytes for IPv4 reply).
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		conn.Close()
		return nil, err
	}
	if hdr[1] != 0x00 {
		conn.Close()
		return nil, fmt.Errorf("socks5 connect rejected: code %d", hdr[1])
	}
	// Skip bound address.
	switch hdr[3] {
	case 0x01: // IPv4
		tmp := make([]byte, 6)
		_, _ = io.ReadFull(conn, tmp)
	case 0x03: // domain
		lbuf := make([]byte, 1)
		_, _ = io.ReadFull(conn, lbuf)
		tmp := make([]byte, int(lbuf[0])+2)
		_, _ = io.ReadFull(conn, tmp)
	case 0x04: // IPv6
		tmp := make([]byte, 18)
		_, _ = io.ReadFull(conn, tmp)
	}

	return conn, nil
}

// ---------------------------------------------------------------------------
// Packet builders — construct valid IPv4/TCP packets with correct checksums.
// ---------------------------------------------------------------------------

func buildSYNACK(srcIP, dstIP []byte, srcPort, dstPort uint16, seq, ack uint32) []byte {
	return buildTCPPacket(srcIP, dstIP, srcPort, dstPort, seq, ack, 0x12 /*SYN+ACK*/, nil)
}

func buildRST(srcIP, dstIP []byte, srcPort, dstPort uint16, seq, ack uint32) []byte {
	return buildTCPPacket(srcIP, dstIP, srcPort, dstPort, seq, ack, 0x04 /*RST*/, nil)
}

func buildData(srcIP, dstIP []byte, srcPort, dstPort uint16, seq, ack uint32, data []byte) []byte {
	return buildTCPPacket(srcIP, dstIP, srcPort, dstPort, seq, ack, 0x18 /*PSH+ACK*/, data)
}

func buildFIN(srcIP, dstIP []byte, srcPort, dstPort uint16, seq, ack uint32) []byte {
	return buildTCPPacket(srcIP, dstIP, srcPort, dstPort, seq, ack, 0x11 /*FIN+ACK*/, nil)
}

func buildACK(srcIP, dstIP []byte, srcPort, dstPort uint16, seq, ack uint32) []byte {
	return buildTCPPacket(srcIP, dstIP, srcPort, dstPort, seq, ack, 0x10 /*ACK*/, nil)
}

// buildTCPPacket crafts a complete IPv4/TCP packet with correct checksums.
func buildTCPPacket(srcIP, dstIP []byte, srcPort, dstPort uint16, seq, ack uint32, flags byte, payload []byte) []byte {
	const ipHdrLen = 20
	const tcpHdrLen = 20
	totalLen := ipHdrLen + tcpHdrLen + len(payload)

	pkt := make([]byte, totalLen)

	// IPv4 header.
	pkt[0] = 0x45 // version=4, IHL=5 (20 bytes)
	pkt[1] = 0    // DSCP/ECN
	binary.BigEndian.PutUint16(pkt[2:4], uint16(totalLen))
	binary.BigEndian.PutUint16(pkt[4:6], 0)    // ID
	binary.BigEndian.PutUint16(pkt[6:8], 0x40) // flags=DF, frag offset=0
	pkt[8] = 64                                // TTL
	pkt[9] = 6                                 // protocol = TCP
	// checksum at [10:12] — filled below
	copy(pkt[12:16], srcIP[:4])
	copy(pkt[16:20], dstIP[:4])
	binary.BigEndian.PutUint16(pkt[10:12], ipChecksum(pkt[:ipHdrLen]))

	// TCP header.
	tcp := pkt[ipHdrLen:]
	binary.BigEndian.PutUint16(tcp[0:2], srcPort)
	binary.BigEndian.PutUint16(tcp[2:4], dstPort)
	binary.BigEndian.PutUint32(tcp[4:8], seq)
	binary.BigEndian.PutUint32(tcp[8:12], ack)
	tcp[12] = (tcpHdrLen / 4) << 4 // data offset
	tcp[13] = flags
	binary.BigEndian.PutUint16(tcp[14:16], 65535) // window
	// checksum at [16:18] — filled below
	copy(tcp[20:], payload)
	binary.BigEndian.PutUint16(tcp[16:18], tcpChecksum(pkt[12:16], pkt[16:20], tcp))

	return pkt
}

// ipChecksum computes the one's complement checksum for an IPv4 header.
func ipChecksum(hdr []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(hdr); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(hdr[i : i+2]))
	}
	if len(hdr)%2 != 0 {
		sum += uint32(hdr[len(hdr)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

// tcpChecksum computes the TCP checksum using the IPv4 pseudo-header.
func tcpChecksum(srcIP, dstIP, tcpSeg []byte) uint16 {
	// Pseudo-header: src IP, dst IP, zero, protocol (6), TCP length.
	pseudo := make([]byte, 12)
	copy(pseudo[0:4], srcIP)
	copy(pseudo[4:8], dstIP)
	pseudo[8] = 0
	pseudo[9] = 6
	binary.BigEndian.PutUint16(pseudo[10:12], uint16(len(tcpSeg)))

	var sum uint32
	for _, b := range [][]byte{pseudo, tcpSeg} {
		for i := 0; i+1 < len(b); i += 2 {
			sum += uint32(binary.BigEndian.Uint16(b[i : i+2]))
		}
		if len(b)%2 != 0 {
			sum += uint32(b[len(b)-1]) << 8
		}
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

// buildUDPPacket crafts a complete IPv4/UDP packet with correct checksums.
// Used to inject DNS responses back into the TUN interface.
func buildUDPPacket(srcIP, dstIP []byte, srcPort, dstPort uint16, payload []byte) []byte {
	const ipHdrLen = 20
	const udpHdrLen = 8
	totalLen := ipHdrLen + udpHdrLen + len(payload)

	pkt := make([]byte, totalLen)

	// IPv4 header.
	pkt[0] = 0x45 // version=4, IHL=5 (20 bytes)
	pkt[1] = 0    // DSCP/ECN
	binary.BigEndian.PutUint16(pkt[2:4], uint16(totalLen))
	binary.BigEndian.PutUint16(pkt[4:6], 0)    // ID
	binary.BigEndian.PutUint16(pkt[6:8], 0x40) // flags=DF, frag offset=0
	pkt[8] = 64                                // TTL
	pkt[9] = 17                                // protocol = UDP
	// IP checksum at [10:12] — computed below after src/dst filled in.
	copy(pkt[12:16], srcIP[:4])
	copy(pkt[16:20], dstIP[:4])
	binary.BigEndian.PutUint16(pkt[10:12], ipChecksum(pkt[:ipHdrLen]))

	// UDP header.
	udp := pkt[ipHdrLen:]
	binary.BigEndian.PutUint16(udp[0:2], srcPort)
	binary.BigEndian.PutUint16(udp[2:4], dstPort)
	binary.BigEndian.PutUint16(udp[4:6], uint16(udpHdrLen+len(payload)))
	// UDP checksum at [6:8] — computed after copying payload.
	copy(udp[8:], payload)
	binary.BigEndian.PutUint16(udp[6:8], udpChecksum(pkt[12:16], pkt[16:20], udp))

	return pkt
}

// udpChecksum computes the UDP checksum using the IPv4 pseudo-header (RFC 768).
func udpChecksum(srcIP, dstIP, udpSeg []byte) uint16 {
	// Pseudo-header: src IP (4), dst IP (4), zero (1), protocol 17 (1), UDP length (2).
	pseudo := make([]byte, 12)
	copy(pseudo[0:4], srcIP)
	copy(pseudo[4:8], dstIP)
	pseudo[8] = 0
	pseudo[9] = 17
	binary.BigEndian.PutUint16(pseudo[10:12], uint16(len(udpSeg)))

	var sum uint32
	for _, b := range [][]byte{pseudo, udpSeg} {
		for i := 0; i+1 < len(b); i += 2 {
			sum += uint32(binary.BigEndian.Uint16(b[i : i+2]))
		}
		if len(b)%2 != 0 {
			sum += uint32(b[len(b)-1]) << 8
		}
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	result := ^uint16(sum)
	// RFC 768: a computed checksum of 0 must be sent as 0xFFFF.
	if result == 0 {
		return 0xFFFF
	}
	return result
}
