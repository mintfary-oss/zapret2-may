//go:build windows

package windivert

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"syscall"
	"unsafe"
)

// ─── WinDivert constants ──────────────────────────────────────────────────────

const (
	winDivertLayerNetwork = 0
	winDivertFlagNone     = 0

	// WINDIVERT_ADDRESS is always 80 bytes in WinDivert 2.x.
	addrSize = 80
	// Maximum Ethernet MTU that fits in a WinDivert recv buffer.
	pktBufSize = 65535
)

// ─── Lazy DLL bindings ───────────────────────────────────────────────────────

var (
	winDivertDLL        = syscall.NewLazyDLL("WinDivert.dll")
	procWinDivertOpen   = winDivertDLL.NewProc("WinDivertOpen")
	procWinDivertRecv   = winDivertDLL.NewProc("WinDivertRecv")
	procWinDivertSend   = winDivertDLL.NewProc("WinDivertSend")
	procWinDivertClose  = winDivertDLL.NewProc("WinDivertClose")
	procWinDivertCalcCS = winDivertDLL.NewProc("WinDivertHelperCalcChecksums")
)

// ─── Handle ──────────────────────────────────────────────────────────────────

// Handle wraps a WinDivert kernel-driver handle.
type Handle struct {
	h      uintptr // HANDLE value returned by WinDivertOpen
	stopCh chan struct{}
}

// Open opens a WinDivert handle with the given filter string.
// The handle captures outbound TCP packets destined for port 443.
func Open() (*Handle, error) {
	if err := winDivertDLL.Load(); err != nil {
		return nil, fmt.Errorf("windivert: cannot load WinDivert.dll: %w", err)
	}

	// Capture only outbound non-loopback TCP packets to port 443.
	filter := "outbound and !loopback and tcp.DstPort == 443"
	filterPtr, err := syscall.BytePtrFromString(filter)
	if err != nil {
		return nil, err
	}

	h, _, lastErr := procWinDivertOpen.Call(
		uintptr(unsafe.Pointer(filterPtr)),
		winDivertLayerNetwork,
		0, // priority
		winDivertFlagNone,
	)
	if h == ^uintptr(0) { // INVALID_HANDLE_VALUE
		return nil, fmt.Errorf("windivert: WinDivertOpen failed: %w", lastErr)
	}
	return &Handle{h: h, stopCh: make(chan struct{})}, nil
}

// Close releases the WinDivert handle and stops the intercept loop.
func (hd *Handle) Close() {
	select {
	case <-hd.stopCh:
	default:
		close(hd.stopCh)
	}
	procWinDivertClose.Call(hd.h) //nolint:errcheck
}

// ─── Intercept loop ──────────────────────────────────────────────────────────

// RunIntercept reads packets from the WinDivert handle, applies the bypass
// strategy, and re-injects the (possibly modified) packets.  It blocks until
// the handle is closed.  strategy is the string name used throughout FreeNet
// ("split", "tlsrec", "combined", "fake", "auto", "none").
func (hd *Handle) RunIntercept(strategy string) {
	pkt := make([]byte, pktBufSize)
	addr := make([]byte, addrSize)

	for {
		select {
		case <-hd.stopCh:
			return
		default:
		}

		var recvLen uint32
		ret, _, lastErr := procWinDivertRecv.Call(
			hd.h,
			uintptr(unsafe.Pointer(&pkt[0])),
			uintptr(pktBufSize),
			uintptr(unsafe.Pointer(&recvLen)),
			uintptr(unsafe.Pointer(&addr[0])),
		)
		if ret == 0 {
			// Recv failed — handle was likely closed.
			log.Printf("windivert: recv error: %v", lastErr)
			return
		}

		raw := make([]byte, int(recvLen))
		copy(raw, pkt[:recvLen])

		processed, err := processPacket(raw, addr, strategy)
		if err != nil {
			// Cannot process — re-inject original.
			log.Printf("windivert: process error: %v", err)
			processed = [][]byte{raw}
		}

		for _, p := range processed {
			recalcChecksums(p, addr)
			hd.inject(p, addr)
		}
	}
}

// inject sends a single packet through the WinDivert handle.
func (hd *Handle) inject(pkt []byte, addr []byte) {
	var sent uint32
	ret, _, lastErr := procWinDivertSend.Call(
		hd.h,
		uintptr(unsafe.Pointer(&pkt[0])),
		uintptr(len(pkt)),
		uintptr(unsafe.Pointer(&sent)),
		uintptr(unsafe.Pointer(&addr[0])),
	)
	if ret == 0 {
		log.Printf("windivert: send error: %v", lastErr)
	}
}

// recalcChecksums asks WinDivert to recalculate IP, TCP, and UDP checksums.
func recalcChecksums(pkt []byte, addr []byte) {
	procWinDivertCalcCS.Call(
		uintptr(unsafe.Pointer(&pkt[0])),
		uintptr(len(pkt)),
		uintptr(unsafe.Pointer(&addr[0])),
		0, // flags
	)
}

// ─── Packet processing ───────────────────────────────────────────────────────

// IPv4/TCP header offsets (no IP options assumed — IHL == 5).
const (
	ipProtoOffset = 9  // IP protocol byte
	ipTotalLenOff = 2  // IP total length (big-endian uint16)
	ipSrcOff      = 12 // source IP
	ipDstOff      = 16 // destination IP
	ipHdrMinLen   = 20
	tcpSrcPortOff = 0 // relative to TCP header start
	tcpDstPortOff = 2
	tcpSeqOff     = 4
	tcpDataOffOff = 12 // high nibble = data offset in 32-bit words
	tcpHdrMinLen  = 20
	ipProtoTCP    = 6
)

// ipHeaderLen returns the IPv4 header length in bytes (IHL field × 4).
func ipHeaderLen(pkt []byte) int {
	if len(pkt) < 1 {
		return 0
	}
	return int(pkt[0]&0x0F) * 4
}

// tcpHeaderLen returns the TCP header length from the data-offset field.
func tcpHeaderLen(tcpHdr []byte) int {
	if len(tcpHdr) < 13 {
		return tcpHdrMinLen
	}
	return int(tcpHdr[tcpDataOffOff]>>4) * 4
}

// processPacket inspects and optionally transforms one raw IP packet.
// Returns one or more raw packets to inject.
// strategy is the FreeNet strategy string ("split", "tlsrec", "combined", "auto", etc.).
func processPacket(pkt []byte, _ []byte, strategy string) ([][]byte, error) {
	if len(pkt) < ipHdrMinLen {
		return [][]byte{pkt}, nil
	}
	// Only handle IPv4 TCP.
	if pkt[0]>>4 != 4 || pkt[ipProtoOffset] != ipProtoTCP {
		return [][]byte{pkt}, nil
	}

	ihl := ipHeaderLen(pkt)
	if len(pkt) < ihl+tcpHdrMinLen {
		return [][]byte{pkt}, nil
	}
	tcpHdr := pkt[ihl:]
	thl := tcpHeaderLen(tcpHdr)
	if len(pkt) < ihl+thl {
		return [][]byte{pkt}, nil
	}

	payload := pkt[ihl+thl:]
	if len(payload) == 0 {
		// ACK/SYN with no data — passthrough.
		return [][]byte{pkt}, nil
	}

	// Parse TLS ClientHello.
	info, err := parseTLSClientHello(payload)
	if err != nil {
		// Not a TLS ClientHello (could be later TLS records or non-TLS data).
		return [][]byte{pkt}, nil
	}
	if info.hasECH {
		// ECH is already encrypting SNI — no need to bypass, passthrough.
		return [][]byte{pkt}, nil
	}

	// Apply strategy-based splitting.
	switch strategy {
	case "split", "combined", "auto":
		if info.sniOffset > 0 {
			return splitPacket(pkt, ihl, thl, payload, info.sniOffset)
		}
	case "tlsrec":
		// Split after the 5-byte TLS record header.
		return splitPacket(pkt, ihl, thl, payload, 5)
	default:
		// "fake", "disorder", "nfqueue", "none" — handled at OS level or no-op.
	}
	return [][]byte{pkt}, nil
}

// tlsInfo holds minimal TLS ClientHello information needed for WinDivert bypass.
type tlsInfo struct {
	sniOffset int  // byte offset of SNI hostname in the payload (0 = not found)
	hasECH    bool // encrypted_client_hello extension present
}

// parseTLSClientHello checks whether payload is a TLS ClientHello and
// extracts the SNI offset and ECH flag.
func parseTLSClientHello(payload []byte) (*tlsInfo, error) {
	// TLS record: type(1) + version(2) + length(2) + handshake...
	if len(payload) < 9 {
		return nil, errors.New("too short")
	}
	if payload[0] != 0x16 { // ContentType: Handshake
		return nil, errors.New("not handshake")
	}
	if payload[5] != 0x01 { // HandshakeType: ClientHello
		return nil, errors.New("not ClientHello")
	}

	info := &tlsInfo{}

	// Walk extensions looking for SNI (0x0000) and ECH (0xFE0D).
	// Offset to extensions: 5(rec hdr)+4(hs hdr)+2(version)+32(random)+1(sess_id_len)+sess_id+2(cipher_len)+cipher+1(comp_len)+comp
	pos := 9 // after TLS record hdr + hs hdr type+length bytes; skip client_version
	if len(payload) < pos+2 {
		return info, nil
	}
	pos += 2 // skip client_version

	if len(payload) < pos+32 {
		return info, nil
	}
	pos += 32 // skip random

	if len(payload) < pos+1 {
		return info, nil
	}
	sessIDLen := int(payload[pos])
	pos += 1 + sessIDLen

	if len(payload) < pos+2 {
		return info, nil
	}
	cipherLen := int(binary.BigEndian.Uint16(payload[pos:]))
	pos += 2 + cipherLen

	if len(payload) < pos+1 {
		return info, nil
	}
	compLen := int(payload[pos])
	pos += 1 + compLen

	if len(payload) < pos+2 {
		return info, nil
	}
	extTotal := int(binary.BigEndian.Uint16(payload[pos:]))
	pos += 2
	end := pos + extTotal

	for pos+4 <= end && pos+4 <= len(payload) {
		extType := binary.BigEndian.Uint16(payload[pos:])
		extLen := int(binary.BigEndian.Uint16(payload[pos+2:]))
		pos += 4

		switch extType {
		case 0x0000: // SNI
			// SNI format: list_len(2) + type(1) + name_len(2) + name
			if extLen >= 5 && pos+5 <= len(payload) {
				info.sniOffset = pos + 5 // byte offset of hostname
			}
		case 0xFE0D: // ECH
			info.hasECH = true
		}
		pos += extLen
	}
	return info, nil
}

// splitPacket splits the TCP payload at splitPos, producing two packets.
// The second packet has its TCP sequence number incremented by splitPos.
func splitPacket(pkt []byte, ihl, thl int, payload []byte, splitPos int) ([][]byte, error) {
	if splitPos <= 0 || splitPos >= len(payload) {
		return [][]byte{pkt}, nil
	}

	part1 := buildPacket(pkt, ihl, thl, payload[:splitPos], 0)
	part2 := buildPacket(pkt, ihl, thl, payload[splitPos:], uint32(splitPos))
	return [][]byte{part1, part2}, nil
}

// buildPacket constructs a new IP+TCP packet with the given payload and an
// optional sequence-number offset applied to the original seq number.
func buildPacket(orig []byte, ihl, thl int, payload []byte, seqOffset uint32) []byte {
	hdrLen := ihl + thl
	pkt := make([]byte, hdrLen+len(payload))
	copy(pkt, orig[:hdrLen])
	copy(pkt[hdrLen:], payload)

	// Update IP total length.
	binary.BigEndian.PutUint16(pkt[ipTotalLenOff:], uint16(hdrLen+len(payload)))

	// Update TCP sequence number.
	if seqOffset > 0 {
		tcpHdr := pkt[ihl:]
		seq := binary.BigEndian.Uint32(tcpHdr[tcpSeqOff:])
		binary.BigEndian.PutUint32(tcpHdr[tcpSeqOff:], seq+seqOffset)
	}
	return pkt
}

// ─── Exported helpers ────────────────────────────────────────────────────────

// Available reports whether WinDivert.dll can be loaded.
func Available() bool {
	return winDivertDLL.Load() == nil
}

// IsLoopback reports whether addr flags the packet as loopback.
// Bit 2 of byte 8 in the WINDIVERT_ADDRESS struct is the Loopback flag.
func IsLoopback(addr []byte) bool {
	if len(addr) < 9 {
		return false
	}
	return addr[8]&0x04 != 0
}

// SrcIP extracts the source IPv4 address from a raw packet.
func SrcIP(pkt []byte) net.IP {
	if len(pkt) < 20 {
		return nil
	}
	return net.IP(pkt[ipSrcOff : ipSrcOff+4])
}

// DstIP extracts the destination IPv4 address from a raw packet.
func DstIP(pkt []byte) net.IP {
	if len(pkt) < 20 {
		return nil
	}
	return net.IP(pkt[ipDstOff : ipDstOff+4])
}

// ErrNotAvailable is returned when WinDivert.dll is missing.
var ErrNotAvailable = errors.New("windivert: WinDivert.dll not found — place WinDivert.dll and WinDivert64.sys next to freenet.exe")
