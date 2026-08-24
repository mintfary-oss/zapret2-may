//go:build linux

package bypass

import (
	"encoding/binary"
	"net"
	"syscall"
)

// rawFakeSender uses AF_INET/SOCK_RAW with IP_HDRINCL to craft and inject
// arbitrary IP+TCP packets.  Requires CAP_NET_RAW.
type rawFakeSender struct {
	fd int
}

func newFakeSender() (fakeSender, error) {
	// IPPROTO_RAW — we write the full IP header ourselves.
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_RAW, syscall.IPPROTO_RAW)
	if err != nil {
		return nil, err
	}
	// Tell the kernel we will supply our own IP header.
	if err := syscall.SetsockoptInt(fd, syscall.IPPROTO_IP, syscall.IP_HDRINCL, 1); err != nil {
		_ = syscall.Close(fd)
		return nil, err
	}
	return &rawFakeSender{fd: fd}, nil
}

func (s *rawFakeSender) close() {
	_ = syscall.Close(s.fd)
}

// sendFake constructs and injects a single decoy TCP+data packet.
//
// seqOff is added to seq=0; for a fresh TCP connection 0 is fine because the
// server will either (TTL) never receive the packet or (MD5) drop it on bad
// checksum before TCP processing.
func (s *rawFakeSender) sendFake(
	srcIP, dstIP net.IP,
	srcPort, dstPort uint16,
	seqOff uint32,
	payload []byte,
	mode FakeMode,
	ttl uint8,
) error {
	badCS := (mode == FakeModeMD5)
	pkt := buildIPv4TCPPacket(srcIP, dstIP, srcPort, dstPort, seqOff, payload, ttl, badCS)

	dst4 := [4]byte{}
	copy(dst4[:], dstIP.To4())
	sa := &syscall.SockaddrInet4{Port: int(dstPort), Addr: dst4}
	return syscall.Sendto(s.fd, pkt, 0, sa)
}

// ---- packet construction ----

// buildIPv4TCPPacket returns a complete IPv4+TCP+payload byte slice.
// If badChecksum is true the TCP checksum is intentionally corrupted.
func buildIPv4TCPPacket(
	srcIP, dstIP net.IP,
	srcPort, dstPort uint16,
	seq uint32,
	payload []byte,
	ttl uint8,
	badChecksum bool,
) []byte {
	src4 := srcIP.To4()
	dst4 := dstIP.To4()

	// ---- IP header (20 bytes) ----
	ip := make([]byte, 20)
	ip[0] = 0x45                                                   // version=4, IHL=5 (20 bytes)
	ip[1] = 0x00                                                   // DSCP/ECN
	binary.BigEndian.PutUint16(ip[2:], uint16(20+20+len(payload))) // total length
	binary.BigEndian.PutUint16(ip[4:], 0x1337)                     // identification (arbitrary)
	binary.BigEndian.PutUint16(ip[6:], 0x4000)                     // Don't Fragment, offset=0
	ip[8] = ttl
	ip[9] = 0x06 // protocol: TCP
	// ip[10:12] = checksum (filled in below)
	copy(ip[12:16], src4)
	copy(ip[16:20], dst4)
	binary.BigEndian.PutUint16(ip[10:], ipv4Checksum(ip))

	// ---- TCP header (20 bytes) ----
	tcp := make([]byte, 20)
	binary.BigEndian.PutUint16(tcp[0:], srcPort)
	binary.BigEndian.PutUint16(tcp[2:], dstPort)
	binary.BigEndian.PutUint32(tcp[4:], seq)    // sequence number
	binary.BigEndian.PutUint32(tcp[8:], 0)      // ack number = 0 (no ack flag)
	tcp[12] = 0x50                              // data offset = 5 (20 bytes), reserved = 0
	tcp[13] = 0x18                              // flags: PSH + ACK
	binary.BigEndian.PutUint16(tcp[14:], 65535) // window size
	// tcp[16:18] = checksum (filled below)
	binary.BigEndian.PutUint16(tcp[18:], 0) // urgent pointer

	cs := tcpv4Checksum(src4, dst4, tcp, payload)
	if badChecksum {
		cs ^= 0xFFFF // flip all bits → invalid checksum
	}
	binary.BigEndian.PutUint16(tcp[16:], cs)

	// Assemble final packet.
	pkt := make([]byte, 0, 20+20+len(payload))
	pkt = append(pkt, ip...)
	pkt = append(pkt, tcp...)
	pkt = append(pkt, payload...)
	return pkt
}

// ipv4Checksum computes the one's-complement checksum of an IPv4 header.
// The checksum field (bytes 10–11) must be zeroed before calling.
func ipv4Checksum(hdr []byte) uint16 {
	return onesComplementSum(hdr)
}

// tcpv4Checksum computes the TCP checksum using the IPv4 pseudo-header.
func tcpv4Checksum(src4, dst4 []byte, tcpHdr, payload []byte) uint16 {
	tcpLen := uint16(len(tcpHdr) + len(payload))

	// IPv4 pseudo-header: srcIP(4) dstIP(4) zero(1) proto(1) tcpLen(2)
	pseudo := make([]byte, 12)
	copy(pseudo[0:4], src4)
	copy(pseudo[4:8], dst4)
	pseudo[8] = 0x00
	pseudo[9] = 0x06
	binary.BigEndian.PutUint16(pseudo[10:], tcpLen)

	data := make([]byte, 0, len(pseudo)+len(tcpHdr)+len(payload))
	data = append(data, pseudo...)
	data = append(data, tcpHdr...)
	data = append(data, payload...)

	return onesComplementSum(data)
}

// onesComplementSum computes the Internet checksum (RFC 1071).
func onesComplementSum(data []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(data); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i:]))
	}
	if len(data)%2 != 0 {
		sum += uint32(data[len(data)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}
	return ^uint16(sum)
}
