// Auto-detect strategy: test bypass methods against a probe target and pick
// the first one that succeeds.
package bypass

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/mintfary-oss/freenet/internal/types"
)

// AutoDetector tests bypass strategies and selects the best one.
type AutoDetector struct {
	mu     sync.Mutex
	winner string
	done   bool
}

var globalDetector = &AutoDetector{}

// GlobalDetector returns the package-level AutoDetector singleton used by
// the auto bypass strategy.  Use this from the mobile package to trigger
// background probing on Android where the web UI is not available.
func GlobalDetector() *AutoDetector { return globalDetector }

// Winner returns the last detected winning strategy.
// Returns "split" before any probe has run: TCP split is the broadest
// first-try bypass against ТСПУ that works on both HTTP and HTTPS without
// requiring extra TTL tuning.  The real winner is set after RunAutoDetect
// completes.
func (d *AutoDetector) Winner() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.winner == "" {
		return "split"
	}
	return d.winner
}

// Run tests the provided strategies against target and caches the winner.
func (d *AutoDetector) Run(target string, strategies []string, splitPos int) []types.ProbeResult {
	d.mu.Lock()
	defer d.mu.Unlock()

	results := make([]types.ProbeResult, 0, len(strategies))
	for _, s := range strategies {
		r := probeStrategy(target, s, splitPos)
		results = append(results, r)
		log.Printf("auto-detect [%s] → ok=%v latency=%dms err=%s",
			s, r.OK, r.LatencyMs, r.Err)
		if r.OK && d.winner == "" {
			d.winner = s
		}
	}

	if d.winner == "" {
		d.winner = "split"
		log.Printf("auto-detect: no strategy worked, defaulting to split")
	} else {
		log.Printf("auto-detect: selected strategy → %s", d.winner)
	}
	d.done = true
	return results
}

func probeStrategy(target, strategy string, splitPos int) types.ProbeResult {
	r := types.ProbeResult{Strategy: strategy}
	start := time.Now()

	conn, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		r.Err = fmt.Sprintf("dial: %v", err)
		r.LatencyMs = time.Since(start).Milliseconds()
		return r
	}
	defer conn.Close()

	probe := buildMinimalClientHello("www.youtube.com")

	switch strategy {
	case "split":
		pos := splitPos
		if pos <= 0 || pos >= len(probe) {
			pos = 1
		}
		setTCPNoDelay(conn, true)
		_, err = conn.Write(probe[:pos])
		if err == nil {
			_, err = conn.Write(probe[pos:])
		}
		setTCPNoDelay(conn, false)
	case "disorder":
		pos := splitPos
		if pos <= 0 || pos >= len(probe) {
			pos = 1
		}
		setTCPNoDelay(conn, true)
		_, err = conn.Write(probe[pos:])
		if err == nil {
			time.Sleep(2 * time.Millisecond)
			_, err = conn.Write(probe[:pos])
		}
		setTCPNoDelay(conn, false)
	default:
		_, err = conn.Write(probe)
	}

	r.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		r.Err = err.Error()
		return r
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 64)
	n, readErr := conn.Read(buf)
	if n > 0 || (readErr != nil && readErr.Error() != "EOF") {
		r.OK = true
	}
	return r
}

func buildMinimalClientHello(hostname string) []byte {
	sni := buildSNIExtension(hostname)
	random := make([]byte, 32)
	sessionID := []byte{0}
	cipherSuites := []byte{0x13, 0x01}
	compression := []byte{0x01, 0x00}
	extLenBytes := uint16Bytes(uint16(len(sni)))

	body := concat(
		[]byte{0x03, 0x03},
		random,
		sessionID,
		uint16Bytes(uint16(len(cipherSuites))),
		cipherSuites,
		compression,
		extLenBytes,
		sni,
	)

	hdr := []byte{
		0x01,
		0x00, byte(len(body) >> 8), byte(len(body)),
	}
	fragment := concat(hdr, body)
	return concat(
		[]byte{0x16, 0x03, 0x01},
		uint16Bytes(uint16(len(fragment))),
		fragment,
	)
}

func buildSNIExtension(hostname string) []byte {
	name := []byte(hostname)
	nameLen := uint16Bytes(uint16(len(name)))
	listLen := uint16Bytes(uint16(1 + 2 + len(name)))
	extLen := uint16Bytes(uint16(2 + len(listLen) + len(nameLen) + len(name)))
	return concat(
		[]byte{0x00, 0x00},
		extLen,
		listLen,
		[]byte{0x00},
		nameLen,
		name,
	)
}

func uint16Bytes(v uint16) []byte { return []byte{byte(v >> 8), byte(v)} }

func concat(slices ...[]byte) []byte {
	var total int
	for _, s := range slices {
		total += len(s)
	}
	out := make([]byte, 0, total)
	for _, s := range slices {
		out = append(out, s...)
	}
	return out
}
