// Package diagnostics monitors the proxy's log stream and generates
// plain-text diagnostic reports that the user can download or copy.
package diagnostics

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mintfary-oss/freenet/internal/logs"
	"github.com/mintfary-oss/freenet/internal/types"
)

// StatusProvider is satisfied by proxy.Server and supplies the data
// needed to build a report.
type StatusProvider interface {
	Enabled() bool
	Strategy() string
	GetStats() types.StatsSnapshot
	HostlistSize() int
	DNSEnabled() bool
	DNSStats() (queries, errors int64)
	ECHPassthroughs() int64
}

// Monitor subscribes to a logs.Ring and tracks the number of error-level
// and warning-level messages seen since it was created.
type Monitor struct {
	ring      *logs.Ring
	ch        chan logs.Entry
	startedAt time.Time

	errCount  atomic.Int64
	warnCount atomic.Int64
}

// NewMonitor creates a Monitor and starts a background goroutine that
// watches ring for new log entries.  The goroutine exits when ring is
// garbage-collected (ring.Unsubscribe closes the channel).
func NewMonitor(ring *logs.Ring) *Monitor {
	m := &Monitor{
		ring:      ring,
		startedAt: time.Now(),
	}
	m.ch = ring.Subscribe()
	go m.watch()
	return m
}

// watch classifies incoming log entries and increments the counters.
func (m *Monitor) watch() {
	for e := range m.ch {
		lower := strings.ToLower(e.Message)
		switch {
		case strings.Contains(lower, "error") ||
			strings.Contains(lower, "fatal") ||
			strings.Contains(lower, "fail"):
			m.errCount.Add(1)
		case strings.Contains(lower, "warn"):
			m.warnCount.Add(1)
		}
	}
}

// ErrorCount returns the total number of error-level messages observed.
func (m *Monitor) ErrorCount() int64 { return m.errCount.Load() }

// WarnCount returns the total number of warning-level messages observed.
func (m *Monitor) WarnCount() int64 { return m.warnCount.Load() }

// BuildReport assembles a human-readable plain-text diagnostic report.
// version is the application version string; srv provides live status.
func (m *Monitor) BuildReport(version string, srv StatusProvider) string {
	now := time.Now()
	stats := srv.GetStats()
	dnsQ, dnsE := srv.DNSStats()
	uptime := now.Sub(m.startedAt).Round(time.Second)

	// Collect log entries once so the snapshot is consistent.
	entries := m.ring.Recent(500)

	// Split entries into error / warning / info buckets.
	var errLines, warnLines []string
	for _, e := range entries {
		lower := strings.ToLower(e.Message)
		ts := e.Time.Format("15:04:05")
		switch {
		case strings.Contains(lower, "error") ||
			strings.Contains(lower, "fatal") ||
			strings.Contains(lower, "fail"):
			errLines = append(errLines, fmt.Sprintf("  %s  %s", ts, e.Message))
		case strings.Contains(lower, "warn"):
			warnLines = append(warnLines, fmt.Sprintf("  %s  %s", ts, e.Message))
		}
	}

	var b strings.Builder
	ln := func(s string) {
		b.WriteString(s)
		b.WriteByte('\n')
	}
	sep := func() { ln(strings.Repeat("─", 62)) }
	hdr := func(s string) {
		ln("")
		ln(s)
		sep()
	}

	ln("╔══════════════════════════════════════════════════════════════╗")
	ln("║          FreeNet — Диагностический отчёт                    ║")
	ln("╚══════════════════════════════════════════════════════════════╝")

	hdr("ОБЩАЯ ИНФОРМАЦИЯ")
	ln(fmt.Sprintf("  Версия:           %s", version))
	ln(fmt.Sprintf("  Сгенерирован:     %s", now.Format("2006-01-02 15:04:05")))
	ln(fmt.Sprintf("  Время работы:     %s", uptime))

	hdr("СОСТОЯНИЕ ОБХОДА DPI")
	bypassStatus := "ВЫКЛЮЧЕНО"
	if srv.Enabled() {
		bypassStatus = "ВКЛЮЧЕНО ✓"
	}
	ln(fmt.Sprintf("  Обход DPI:        %s", bypassStatus))
	ln(fmt.Sprintf("  Стратегия:        %s", srv.Strategy()))
	ln(fmt.Sprintf("  Доменов в списке: %d", srv.HostlistSize()))

	hdr("СТАТИСТИКА СОЕДИНЕНИЙ")
	ln(fmt.Sprintf("  Активных:         %d", stats.Active))
	ln(fmt.Sprintf("  Всего:            %d", stats.Total))
	ln(fmt.Sprintf("  Обойдено DPI:     %d", stats.Bypassed))
	ln(fmt.Sprintf("  Без обхода:       %d", stats.Passthrough))
	ln(fmt.Sprintf("  Принято байт:     %s", formatBytes(stats.BytesIn)))
	ln(fmt.Sprintf("  Отправлено байт:  %s", formatBytes(stats.BytesOut)))

	hdr("DNS / ECH")
	dnsStatus := "ВЫКЛЮЧЕН  ⚠ DNS может быть подменён"
	if srv.DNSEnabled() {
		dnsStatus = "ВКЛЮЧЁН ✓  (DNS-over-HTTPS, RFC 8484)"
	}
	ln(fmt.Sprintf("  DNS защита:       %s", dnsStatus))
	ln(fmt.Sprintf("  DNS запросов:     %d", dnsQ))
	ln(fmt.Sprintf("  DNS ошибок:       %d", dnsE))
	ln(fmt.Sprintf("  ECH соединений:   %d", srv.ECHPassthroughs()))

	hdr("ДИАГНОСТИКА ОШИБОК")
	ln(fmt.Sprintf("  Ошибок в логе:    %d", m.errCount.Load()))
	ln(fmt.Sprintf("  Предупреждений:   %d", m.warnCount.Load()))

	if len(errLines) > 0 {
		hdr("СПИСОК ОШИБОК")
		for _, l := range errLines {
			ln(l)
		}
	}

	if len(warnLines) > 0 {
		hdr("ПРЕДУПРЕЖДЕНИЯ")
		for _, l := range warnLines {
			ln(l)
		}
	}

	hdr(fmt.Sprintf("ЖУРНАЛ (последние %d записей)", min(len(entries), 200)))
	start := 0
	if len(entries) > 200 {
		start = len(entries) - 200
	}
	for _, e := range entries[start:] {
		ln(fmt.Sprintf("  %s  %s", e.Time.Format("15:04:05"), e.Message))
	}

	ln("")
	ln("── конец отчёта ─────────────────────────────────────────────────")
	return b.String()
}

// formatBytes converts a byte count to a human-readable string.
func formatBytes(n int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.2f ГБ", float64(n)/gb)
	case n >= mb:
		return fmt.Sprintf("%.1f МБ", float64(n)/mb)
	case n >= kb:
		return fmt.Sprintf("%.1f КБ", float64(n)/kb)
	default:
		return fmt.Sprintf("%d Б", n)
	}
}

// min returns the smaller of a and b.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
