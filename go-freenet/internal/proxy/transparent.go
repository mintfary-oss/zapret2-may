//go:build linux

// Transparent proxy accept loop (Linux only).
// iptables REDIRECT sends packets here; we recover the original destination
// via SO_ORIGINAL_DST and relay through the bypass engine.
package proxy

import (
	"fmt"
	"log"
	"net"
	"unsafe"

	"golang.org/x/sys/unix"
)

func (s *Server) acceptTransparent(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go s.handleTransparent(conn)
	}
}

func (s *Server) handleTransparent(client net.Conn) {
	defer client.Close()

	target, err := originalDst(client)
	if err != nil {
		log.Printf("transparent: original dst: %v", err)
		return
	}

	remote, err := net.Dial("tcp", target)
	if err != nil {
		log.Printf("transparent: dial %s: %v", target, err)
		return
	}
	defer remote.Close()

	log.Printf("transparent → %s", target)

	if s.enabled.Load() {
		s.engine.Relay(client, remote)
	} else {
		relay(client, remote)
	}
}

// originalDst recovers the pre-REDIRECT destination using SO_ORIGINAL_DST.
func originalDst(conn net.Conn) (string, error) {
	tc, ok := conn.(*net.TCPConn)
	if !ok {
		return "", fmt.Errorf("not a TCPConn")
	}
	raw, err := tc.SyscallConn()
	if err != nil {
		return "", err
	}

	var addr unix.RawSockaddrInet4
	var sockErr error
	const soOriginalDst = 80 // Linux SO_ORIGINAL_DST

	controlErr := raw.Control(func(fd uintptr) {
		addrLen := uint32(unsafe.Sizeof(addr))
		_, _, errno := unix.Syscall6(
			unix.SYS_GETSOCKOPT,
			fd,
			unix.SOL_IP,
			soOriginalDst,
			uintptr(unsafe.Pointer(&addr)),
			uintptr(unsafe.Pointer(&addrLen)),
			0,
		)
		if errno != 0 {
			sockErr = errno
		}
	})
	if controlErr != nil {
		return "", controlErr
	}
	if sockErr != nil {
		return "", sockErr
	}

	ip := net.IP(addr.Addr[:])
	port := uint16(addr.Port>>8) | uint16(addr.Port<<8) // big-endian swap
	return fmt.Sprintf("%s:%d", ip.String(), port), nil
}
