//go:build linux

// NFQueue integration: kernel-level packet interception via Linux netfilter.
//
// When enabled, freenet registers as a netfilter queue handler so the kernel
// delivers outgoing TCP packets directly to the process.  No SOCKS5 proxy
// configuration is required — all applications are covered automatically.
//
// Prerequisites:
//  1. Kernel modules: nfnetlink_queue (usually loaded automatically).
//  2. iptables rules to redirect packets to the queue, e.g.:
//     iptables -A OUTPUT -p tcp --dport 443 \
//     -m connbytes --connbytes 0:6 \
//     --connbytes-dir original --connbytes-mode packets \
//     -j NFQUEUE --queue-num 200
//     (Only the first 6 outbound packets of each connection are sent to the
//     queue — this keeps overhead low while still catching the ClientHello.)
//  3. Privilege: CAP_NET_ADMIN + CAP_NET_RAW.
package proxy

import (
	"context"
	"encoding/binary"
	"log"
	"sync/atomic"

	nfqueue "github.com/florianl/go-nfqueue/v2"
	"github.com/mintfary-oss/freenet/internal/bypass"
	"github.com/mintfary-oss/freenet/internal/config"
)

// NFQueueServer intercepts outgoing TCP packets via a Linux netfilter queue
// and applies DPI bypass transformations.
type NFQueueServer struct {
	cfg     *config.Config
	engine  *bypass.Engine
	q       *nfqueue.Nfqueue
	enabled atomic.Bool
	cancel  context.CancelFunc
}

// NewNFQueueServer constructs an NFQueueServer.
func NewNFQueueServer(cfg *config.Config, engine *bypass.Engine) *NFQueueServer {
	return &NFQueueServer{cfg: cfg, engine: engine}
}

// Start opens the netfilter queue and begins processing packets.
func (n *NFQueueServer) Start() error {
	if !n.cfg.NFQueue.Enabled {
		return nil
	}

	qcfg := nfqueue.Config{
		NfQueue:      uint16(n.cfg.NFQueue.QueueNum),
		MaxPacketLen: 65535,
		MaxQueueLen:  256,
		Copymode:     nfqueue.NfQnlCopyPacket,
	}

	q, err := nfqueue.Open(&qcfg)
	if err != nil {
		return err
	}
	n.q = q

	ctx, cancel := context.WithCancel(context.Background())
	n.cancel = cancel
	n.enabled.Store(true)

	errFn := func(e error) int {
		if ctx.Err() == nil { // not cancelled
			log.Printf("nfqueue error: %v", e)
		}
		return 0
	}

	hookFn := func(a nfqueue.Attribute) int {
		if !n.enabled.Load() {
			_ = q.SetVerdict(*a.PacketID, nfqueue.NfAccept)
			return 0
		}
		n.handlePacket(q, *a.PacketID, *a.Payload)
		return 0
	}

	if err := q.RegisterWithErrorFunc(ctx, hookFn, errFn); err != nil {
		cancel()
		_ = q.Close()
		return err
	}

	log.Printf("nfqueue listening on queue %d", n.cfg.NFQueue.QueueNum)
	return nil
}

// Stop closes the netfilter queue.
func (n *NFQueueServer) Stop() {
	if n.cancel != nil {
		n.cancel()
	}
	if n.q != nil {
		_ = n.q.Close()
	}
}

// SetEnabled pauses/resumes packet processing without closing the queue.
func (n *NFQueueServer) SetEnabled(v bool) { n.enabled.Store(v) }

// handlePacket inspects one IPv4+TCP packet and either accepts it unchanged
// or drops it and re-injects split/fake replacements.
func (n *NFQueueServer) handlePacket(q *nfqueue.Nfqueue, id uint32, raw []byte) {
	pkt, ok := parseIPv4TCP(raw)
	if !ok {
		_ = q.SetVerdict(id, nfqueue.NfAccept)
		return
	}

	// Only act on packets whose payload looks like a TLS ClientHello.
	if len(pkt.payload) < 6 || pkt.payload[0] != 0x16 {
		_ = q.SetVerdict(id, nfqueue.NfAccept)
		return
	}

	info, err := bypass.ParseClientHello(pkt.payload)
	if err != nil || info == nil {
		_ = q.SetVerdict(id, nfqueue.NfAccept)
		return
	}

	splitPos := bypass.SplitPosition(info, n.cfg.Bypass.SplitPos)

	// Drop the original packet — we will re-inject modified versions.
	_ = q.SetVerdict(id, nfqueue.NfDrop)

	// Re-inject via raw socket.
	n.reinjectSplit(pkt, splitPos)
}

// reinjectSplit drops the original and sends two replacement fragments using
// the raw-socket sender already initialised for fake packets.
func (n *NFQueueServer) reinjectSplit(pkt ipv4TCPPacket, splitPos int) {
	payload := pkt.payload
	if splitPos <= 0 || splitPos >= len(payload) {
		splitPos = 1
	}

	send := bypass.RawSend // package-level helper, see below
	if send == nil {
		log.Printf("nfqueue: raw sender unavailable, cannot reinject")
		return
	}

	// First fragment (before SNI).
	_ = send(pkt.srcIP, pkt.dstIP,
		pkt.srcPort, pkt.dstPort,
		pkt.seqNum,
		payload[:splitPos],
		64, false)

	// Second fragment (SNI + rest).
	_ = send(pkt.srcIP, pkt.dstIP,
		pkt.srcPort, pkt.dstPort,
		pkt.seqNum+uint32(splitPos),
		payload[splitPos:],
		64, false)
}

// ---- minimal IPv4+TCP parser ----

type ipv4TCPPacket struct {
	srcIP, dstIP []byte
	srcPort      uint16
	dstPort      uint16
	seqNum       uint32
	payload      []byte
}

func parseIPv4TCP(raw []byte) (ipv4TCPPacket, bool) {
	if len(raw) < 20 {
		return ipv4TCPPacket{}, false
	}
	ihl := int(raw[0]&0x0f) * 4
	if len(raw) < ihl+20 {
		return ipv4TCPPacket{}, false
	}
	tcp := raw[ihl:]
	dataOffset := int(tcp[12]>>4) * 4
	if len(tcp) < dataOffset {
		return ipv4TCPPacket{}, false
	}
	return ipv4TCPPacket{
		srcIP:   raw[12:16],
		dstIP:   raw[16:20],
		srcPort: binary.BigEndian.Uint16(tcp[0:2]),
		dstPort: binary.BigEndian.Uint16(tcp[2:4]),
		seqNum:  binary.BigEndian.Uint32(tcp[4:8]),
		payload: tcp[dataOffset:],
	}, true
}
