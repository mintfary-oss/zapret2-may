//go:build !linux

// Stub implementation for platforms that don't support AF_INET/SOCK_RAW the
// same way as Linux (Windows, macOS, etc.).
// Returning an error causes globalFakeSender to stay nil, so relayFake
// automatically falls back to the split strategy on these platforms.
package bypass

import (
	"fmt"
	"net"
)

func newFakeSender() (fakeSender, error) {
	return nil, fmt.Errorf("fake packets require Linux with CAP_NET_RAW")
}

// unreachableFakeSender satisfies the interface but is never used.
type unreachableFakeSender struct{}

func (unreachableFakeSender) sendFake(_, _ net.IP, _, _ uint16, _ uint32, _ []byte, _ FakeMode, _ uint8) error {
	return nil
}
func (unreachableFakeSender) close() {}
