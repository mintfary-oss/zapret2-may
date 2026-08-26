package bypass

import (
	"net/http"
	"os"
	"testing"

	"github.com/mintfary-oss/freenet/internal/config"
)

// TestEngine_SetHTTPClient verifies that SetHTTPClient propagates to the
// underlying Hostlist without panicking.
func TestEngine_SetHTTPClient(t *testing.T) {
	e := NewEngine(newTestConfig("split"))
	e.SetHTTPClient(&http.Client{})
	// A second call should also be safe.
	e.SetHTTPClient(nil)
}

// TestEngine_RunAutoDetect_ReturnsResults verifies that RunAutoDetect returns
// one result per strategy without crashing, even when the target is
// unreachable.
func TestEngine_RunAutoDetect(t *testing.T) {
	cfg := &config.Config{
		Bypass: config.BypassConfig{
			Strategy: "split",
			SplitPos: 2,
		},
	}
	e := NewEngine(cfg)
	// Use a port that refuses connections so probes fail fast.
	results := e.RunAutoDetect("127.0.0.1:1")
	if len(results) == 0 {
		t.Error("RunAutoDetect: expected at least one result")
	}
	for _, r := range results {
		if r.Strategy == "" {
			t.Error("RunAutoDetect: result has empty Strategy")
		}
	}
}

// TestEngine_NewEngine_WithHostlistPath verifies that NewEngine loads a local
// hostlist file when Hostlist.Path is set and AutoUpdate is false.
func TestEngine_NewEngine_WithHostlistPath(t *testing.T) {
	tmp := t.TempDir()
	path := tmp + "/domains.lst"
	if err := writeTempList(path, "example.com\nyoutube.com\n"); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Bypass: config.BypassConfig{Strategy: "split", SplitPos: 2},
		Hostlist: config.HostlistConfig{
			Enabled:    true,
			AutoUpdate: false,
			Path:       path,
		},
	}
	e := NewEngine(cfg)
	if e == nil {
		t.Fatal("NewEngine returned nil")
	}
	// Hostlist must be populated.
	if e.Hostlist().Size() != 2 {
		t.Errorf("hostlist size = %d, want 2", e.Hostlist().Size())
	}
}

// TestEngine_NewEngine_WithAutoUpdate exercises the auto-update goroutine path
// (download will fail; fallback to local file should be attempted).
func TestEngine_NewEngine_WithAutoUpdateFallback(t *testing.T) {
	tmp := t.TempDir()
	path := tmp + "/domains.lst"
	if err := writeTempList(path, "fallback.com\n"); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Bypass: config.BypassConfig{Strategy: "split", SplitPos: 2},
		Hostlist: config.HostlistConfig{
			Enabled:    true,
			AutoUpdate: true,
			URL:        "http://127.0.0.1:1/domains.txt", // will fail
			Path:       path,
		},
	}
	// This should not panic; the goroutine will fail and fall back to local.
	e := NewEngine(cfg)
	if e == nil {
		t.Fatal("NewEngine returned nil")
	}
}

// writeTempList is a helper that writes a domain list to a temp file.
func writeTempList(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
