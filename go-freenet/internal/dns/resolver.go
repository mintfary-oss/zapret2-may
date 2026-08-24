package dns

import (
	"context"
	"log"
	"net"
	"sync/atomic"
	"time"
)

// Resolver is a local UDP-to-DoH DNS proxy.
//
// It listens on a UDP port (typically 127.0.0.1:5300) and forwards every
// incoming DNS query to the DoH client, then writes the response back to the
// original caller.  Configure the OS or application to use this address as its
// DNS server to protect all name resolution from ISP-level poisoning.
//
// All methods are safe for concurrent use.
type Resolver struct {
	client     *Client
	listenAddr string
	conn       *net.UDPConn
	running    atomic.Bool
	queries    atomic.Int64
	errors     atomic.Int64
}

// NewResolver creates a Resolver that listens on addr (e.g. "127.0.0.1:5300").
// If client is nil a default DoH client using DefaultServers is created.
func NewResolver(addr string, client *Client) *Resolver {
	if client == nil {
		client = NewClient(nil)
	}
	return &Resolver{
		client:     client,
		listenAddr: addr,
	}
}

// Start opens the UDP socket and begins serving queries in background
// goroutines.  Returns an error if the address cannot be bound.
// The resolver runs until Stop is called or ctx is cancelled.
func (r *Resolver) Start(ctx context.Context) error {
	udpAddr, err := net.ResolveUDPAddr("udp4", r.listenAddr)
	if err != nil {
		return err
	}
	r.conn, err = net.ListenUDP("udp4", udpAddr)
	if err != nil {
		return err
	}
	r.running.Store(true)
	log.Printf("dns: DoH resolver listening on %s", r.listenAddr)
	go r.serve(ctx)
	return nil
}

// Stop closes the listener and stops the serving goroutine.
// Safe to call multiple times or before Start.
func (r *Resolver) Stop() {
	r.running.Store(false)
	if r.conn != nil {
		_ = r.conn.Close()
	}
}

// Queries returns the total number of DNS queries handled since Start.
func (r *Resolver) Queries() int64 { return r.queries.Load() }

// Errors returns the number of DoH forwarding failures since Start.
func (r *Resolver) Errors() int64 { return r.errors.Load() }

// ListenAddr returns the local UDP address the resolver is bound to.
func (r *Resolver) ListenAddr() string { return r.listenAddr }

// serve is the main read loop — reads UDP datagrams and spawns a goroutine per query.
func (r *Resolver) serve(ctx context.Context) {
	buf := make([]byte, 4096)
	for r.running.Load() {
		_ = r.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, src, err := r.conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				select {
				case <-ctx.Done():
					return
				default:
					continue
				}
			}
			if r.running.Load() {
				log.Printf("dns: read error: %v", err)
			}
			return
		}
		q := make([]byte, n)
		copy(q, buf[:n])
		go r.forward(ctx, src, q)
	}
}

// forward sends a single DNS query to the DoH client and writes the response back.
func (r *Resolver) forward(ctx context.Context, src *net.UDPAddr, query []byte) {
	r.queries.Add(1)

	fwdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := r.client.Exchange(fwdCtx, query)
	if err != nil {
		r.errors.Add(1)
		log.Printf("dns: DoH forward failed: %v", err)
		return
	}
	if _, err := r.conn.WriteToUDP(resp, src); err != nil {
		r.errors.Add(1)
		log.Printf("dns: write to %v: %v", src, err)
	}
}
