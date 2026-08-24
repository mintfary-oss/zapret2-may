// Connection statistics tracked by the proxy server.
package proxy

import (
	"sync/atomic"

	"github.com/mintfary-oss/freenet/internal/types"
)

// Stats holds live atomic counters.
type Stats struct {
	Active      atomic.Int64
	Total       atomic.Int64
	BytesIn     atomic.Int64
	BytesOut    atomic.Int64
	Bypassed    atomic.Int64
	Passthrough atomic.Int64
}

// Snapshot converts the atomic counters into a types.StatsSnapshot.
func (s *Stats) Snapshot() types.StatsSnapshot {
	return types.StatsSnapshot{
		Active:      s.Active.Load(),
		Total:       s.Total.Load(),
		BytesIn:     s.BytesIn.Load(),
		BytesOut:    s.BytesOut.Load(),
		Bypassed:    s.Bypassed.Load(),
		Passthrough: s.Passthrough.Load(),
	}
}
