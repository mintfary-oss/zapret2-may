// SOCKS5 accept loop and handshake implementation (RFC 1928).
package proxy

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
)

const (
	socks5Version    = 0x05
	authNone         = 0x00
	authNoAcceptable = 0xFF
	cmdConnect       = 0x01
	atypIPv4         = 0x01
	atypDomain       = 0x03
	atypIPv6         = 0x04
	repSuccess       = 0x00
	repConnRefused   = 0x05
	repCmdNotSupport = 0x07
)

func (s *Server) acceptSOCKS(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			// Listener closed — normal shutdown.
			return
		}
		go s.handleSOCKS(conn)
	}
}

func (s *Server) handleSOCKS(client net.Conn) {
	defer client.Close()

	if err := socks5Handshake(client); err != nil {
		log.Printf("socks5 handshake from %s: %v", client.RemoteAddr(), err)
		return
	}

	target, err := socks5ReadRequest(client)
	if err != nil {
		log.Printf("socks5 request from %s: %v", client.RemoteAddr(), err)
		return
	}

	remote, err := net.Dial("tcp", target)
	if err != nil {
		_ = socks5WriteReply(client, repConnRefused)
		return
	}
	defer remote.Close()

	if err := socks5WriteReply(client, repSuccess); err != nil {
		return
	}

	log.Printf("→ %s", target)

	s.Stats.Active.Add(1)
	s.Stats.Total.Add(1)
	defer s.Stats.Active.Add(-1)

	// Extract the host part (strip port) for hostlist lookup.
	host, _, _ := net.SplitHostPort(target)

	// Relay traffic through the bypass engine if enabled.
	if s.enabled.Load() {
		s.Stats.Bypassed.Add(1)
		s.engine.RelayDomain(client, remote, host)
	} else {
		s.Stats.Passthrough.Add(1)
		relay(client, remote)
	}
}

// socks5Handshake performs the SOCKS5 greeting (no-auth only).
func socks5Handshake(conn net.Conn) error {
	// VER NMETHODS METHODS…
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		return err
	}
	if hdr[0] != socks5Version {
		return fmt.Errorf("unsupported version %d", hdr[0])
	}
	methods := make([]byte, hdr[1])
	if _, err := io.ReadFull(conn, methods); err != nil {
		return err
	}
	for _, m := range methods {
		if m == authNone {
			_, err := conn.Write([]byte{socks5Version, authNone})
			return err
		}
	}
	_, _ = conn.Write([]byte{socks5Version, authNoAcceptable})
	return fmt.Errorf("no acceptable auth method")
}

// socks5ReadRequest reads a CONNECT request and returns "host:port".
func socks5ReadRequest(conn net.Conn) (string, error) {
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		return "", err
	}
	if hdr[0] != socks5Version {
		return "", fmt.Errorf("bad version in request")
	}
	if hdr[1] != cmdConnect {
		_ = socks5WriteReply(conn, repCmdNotSupport)
		return "", fmt.Errorf("unsupported command %d", hdr[1])
	}

	var host string
	switch hdr[3] {
	case atypIPv4:
		addr := make([]byte, 4)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return "", err
		}
		host = net.IP(addr).String()
	case atypIPv6:
		addr := make([]byte, 16)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return "", err
		}
		host = "[" + net.IP(addr).String() + "]"
	case atypDomain:
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return "", err
		}
		domain := make([]byte, lenBuf[0])
		if _, err := io.ReadFull(conn, domain); err != nil {
			return "", err
		}
		host = string(domain)
	default:
		return "", fmt.Errorf("unknown address type %d", hdr[3])
	}

	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBuf); err != nil {
		return "", err
	}
	port := binary.BigEndian.Uint16(portBuf)
	return fmt.Sprintf("%s:%d", host, port), nil
}

// socks5WriteReply sends a minimal SOCKS5 reply.
func socks5WriteReply(conn net.Conn, rep byte) error {
	// VER REP RSV ATYP BND.ADDR(4) BND.PORT(2)
	reply := []byte{socks5Version, rep, 0x00, atypIPv4, 0, 0, 0, 0, 0, 0}
	_, err := conn.Write(reply)
	return err
}

// relay copies data between two connections concurrently (no bypass).
func relay(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(a, b); done <- struct{}{} }()
	go func() { _, _ = io.Copy(b, a); done <- struct{}{} }()
	<-done
}
