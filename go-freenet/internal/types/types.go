// Package types contains shared data structures used by both the proxy and
// web packages to avoid import cycles.
package types

import "time"

// StatsSnapshot is a point-in-time snapshot of proxy counters.
type StatsSnapshot struct {
	Active      int64 `json:"active"`
	Total       int64 `json:"total"`
	BytesIn     int64 `json:"bytes_in"`
	BytesOut    int64 `json:"bytes_out"`
	Bypassed    int64 `json:"bypassed"`
	Passthrough int64 `json:"passthrough"`
}

// ProbeResult records the outcome of a single auto-detect strategy test.
type ProbeResult struct {
	Strategy  string        `json:"strategy"`
	LatencyMs int64         `json:"latency_ms"`
	Latency   time.Duration `json:"-"` // internal use
	OK        bool          `json:"ok"`
	Err       string        `json:"err,omitempty"`
}
