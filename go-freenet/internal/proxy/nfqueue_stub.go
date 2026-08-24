//go:build !linux

// Stub for non-Linux platforms.
package proxy

import (
	"github.com/mintfary-oss/freenet/internal/bypass"
	"github.com/mintfary-oss/freenet/internal/config"
)

// NFQueueServer is a no-op on non-Linux systems.
type NFQueueServer struct{}

func NewNFQueueServer(_ *config.Config, _ *bypass.Engine) *NFQueueServer {
	return &NFQueueServer{}
}

func (n *NFQueueServer) Start() error      { return nil }
func (n *NFQueueServer) Stop()             {}
func (n *NFQueueServer) SetEnabled(_ bool) {}
