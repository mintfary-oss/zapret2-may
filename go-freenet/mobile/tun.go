// Package mobile — minimal IPv4/TCP TUN forwarder for Android VpnService.
//
// ForwardTUN reads raw IP packets from the TUN file descriptor created by
// Android's VpnService, intercepts TCP SYN packets, and transparently proxies
// each connection through the local SOCKS5 bypass engine.  The userspace TCP
// state machine is intentionally minimal: it handles the common case (SYN →
// SYN-ACK → data exchange → FIN) without implementing IP fragmentation,
// TCP options negotiation, or SACK.
//
// Production note: for higher throughput consider replacing this implementation
// with github.com/xjasonlyu/tun2socks/v2 (gVisor-based, handles edge cases).
package mobile

import (
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

	conns  sync.Map // key: connKey → *tunConn
	closed atomic.Bool
	done   chan struct{}
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

// ForwardTUN starts the TUN packet-forwarding loop.
//
// Parameters:
//   - tunFd:     file descriptor of the TUN interface (from VpnService.Builder.establish()).
//   - socksAddr: local SOCKS5 bypass proxy address, e.g. "127.0.0.1:1080".
//   - protector: Android VpnService socket protector (may be nil in unit tests).
//
// Blocks until the TUN fd is closed or an unrecoverable error occurs.
func ForwardTUN(tunFd int64, socksAddr string, protector SocketProtector) error {
	f := os.NewFile(uintptr(tunFd), "tun")
	if f == nil {
		return fmt.Errorf("tun: invalid file descriptor %d", tunFd)
	}

	fw := &tunForwarder{
		tun:       f,
		socksAddr: socksAddr,
		protector: protector,
		done:      make(chan struct{}),
	}

	log.Printf("tun: forwarder started (socks5 → %s)", socksAddr)
	return fw.run()
}

// run is the main read-loop.
func (fw *tunForwarder) run() error {
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
	if proto != 6 { // TCP only
		return
	}

	var srcIP, dstIP [4]byte
	copy(srcIP[:], pkt[12:16])
	copy(dstIP[:], pkt[16:20])

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
			tc.mu.Unlock()
			_ = seqNum // used for validation above
			_ = ackNum

			if tc.upstream != nil {
				if _, err := tc.upstream.Write(payload); err != nil {
					fw.conns.Delete(k)
					_ = tc.upstream.Close()
				}
			}
		}
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
