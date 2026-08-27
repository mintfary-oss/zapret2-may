package diagnostics

import (
	"strings"
	"testing"
	"time"

	"github.com/mintfary-oss/freenet/internal/logs"
	"github.com/mintfary-oss/freenet/internal/types"
)

// fakeServer implements StatusProvider for tests.
type fakeServer struct{}

func (f *fakeServer) Enabled() bool                      { return true }
func (f *fakeServer) Strategy() string                   { return "auto" }
func (f *fakeServer) GetStats() types.StatsSnapshot      { return types.StatsSnapshot{Total: 42, Bypassed: 38} }
func (f *fakeServer) HostlistSize() int                  { return 12345 }
func (f *fakeServer) DNSEnabled() bool                   { return true }
func (f *fakeServer) DNSStats() (int64, int64)           { return 100, 2 }
func (f *fakeServer) ECHPassthroughs() int64             { return 7 }

func TestMonitorErrorCount(t *testing.T) {
	ring := logs.NewRing(100)
	m := NewMonitor(ring)

	// Give the subscription goroutine time to start.
	time.Sleep(10 * time.Millisecond)

	// Write messages directly to the ring via Write (implements io.Writer).
	ring.Write([]byte("connection established"))
	ring.Write([]byte("error: dial timeout"))
	ring.Write([]byte("warning: strategy fallback"))
	ring.Write([]byte("fatal: cannot bind port"))
	ring.Write([]byte("normal log line"))

	// Allow the watch goroutine to process the entries.
	time.Sleep(20 * time.Millisecond)

	if got := m.ErrorCount(); got != 2 {
		t.Errorf("ErrorCount = %d, want 2", got)
	}
	if got := m.WarnCount(); got != 1 {
		t.Errorf("WarnCount = %d, want 1", got)
	}
}

func TestBuildReportContainsKeyFields(t *testing.T) {
	ring := logs.NewRing(100)
	m := NewMonitor(ring)
	ring.Write([]byte("starting freenet"))
	ring.Write([]byte("error: test error for report"))

	time.Sleep(20 * time.Millisecond)

	report := m.BuildReport("1.9.9", &fakeServer{})

	checks := []string{
		"FreeNet",
		"1.9.9",
		"ВКЛЮЧЕНО",
		"auto",
		"12345",
		"100",
		"ВКЛЮЧЁН",
		"ECH соединений",
		"Ошибок в логе",
	}

	for _, want := range checks {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q", want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 Б"},
		{512, "512 Б"},
		{1024, "1.0 КБ"},
		{1536, "1.5 КБ"},
		{1048576, "1.0 МБ"},
		{1073741824, "1.00 ГБ"},
	}
	for _, c := range cases {
		if got := formatBytes(c.n); got != c.want {
			t.Errorf("formatBytes(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}
