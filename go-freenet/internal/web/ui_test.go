// Tests for the web UI HTTP handlers.
//
// Each test creates a mockController, wraps it in a UI, registers handlers
// via a test-local ServeMux, and exercises the handler via httptest.
package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mintfary-oss/freenet/internal/config"
	"github.com/mintfary-oss/freenet/internal/logs"
	"github.com/mintfary-oss/freenet/internal/types"
)

// ---------------------------------------------------------------------------
// mockController — minimal in-memory implementation of Controller
// ---------------------------------------------------------------------------

type mockController struct {
	enabled         bool
	strategy        string
	stats           types.StatsSnapshot
	hostlistSize    int
	dnsEnabled      bool
	dnsQueries      int64
	dnsErrors       int64
	echPassthroughs int64
}

func (m *mockController) Enabled() bool                 { return m.enabled }
func (m *mockController) SetEnabled(v bool)             { m.enabled = v }
func (m *mockController) Strategy() string              { return m.strategy }
func (m *mockController) SetStrategy(s string)          { m.strategy = s }
func (m *mockController) GetStats() types.StatsSnapshot { return m.stats }
func (m *mockController) HostlistSize() int             { return m.hostlistSize }
func (m *mockController) DNSEnabled() bool              { return m.dnsEnabled }
func (m *mockController) DNSStats() (int64, int64)      { return m.dnsQueries, m.dnsErrors }
func (m *mockController) ECHPassthroughs() int64        { return m.echPassthroughs }
func (m *mockController) RunAutoDetect(_ string) []types.ProbeResult {
	return []types.ProbeResult{
		{Strategy: "split", OK: true, LatencyMs: 42},
	}
}

// newTestUI creates a UI backed by mock, wired to a test-local ServeMux.
// The returned *http.ServeMux is used directly via httptest.NewRecorder.
func newTestUI(mc *mockController) (*UI, *http.ServeMux) {
	cfg := &config.Config{
		Proxy: config.ProxyConfig{ListenAddr: "127.0.0.1:1080"},
	}
	ring := logs.NewRing(10)
	ui := NewUI(":0", cfg, mc, ring)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", ui.handleStatus)
	mux.HandleFunc("/api/stats", ui.handleStats)
	mux.HandleFunc("/api/toggle", ui.handleToggle)
	mux.HandleFunc("/api/strategy", ui.handleStrategy)
	mux.HandleFunc("/api/autodetect", ui.handleAutoDetect)
	mux.HandleFunc("/", ui.handleIndex)

	return ui, mux
}

// ---------------------------------------------------------------------------
// /api/status
// ---------------------------------------------------------------------------

func TestHandleStatus_JSON(t *testing.T) {
	mc := &mockController{enabled: true, strategy: "split", hostlistSize: 42, dnsEnabled: true}
	_, mux := newTestUI(mc)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var resp statusResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Enabled {
		t.Error("Enabled = false, want true")
	}
	if resp.Strategy != "split" {
		t.Errorf("Strategy = %q, want split", resp.Strategy)
	}
	if resp.HostlistSize != 42 {
		t.Errorf("HostlistSize = %d, want 42", resp.HostlistSize)
	}
	if !resp.DNSEnabled {
		t.Error("DNSEnabled = false, want true")
	}
}

func TestHandleStatus_DisabledAndNoHostlist(t *testing.T) {
	mc := &mockController{enabled: false, strategy: "auto"}
	_, mux := newTestUI(mc)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp statusResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Enabled {
		t.Error("Enabled = true, want false")
	}
	if resp.HostlistSize != 0 {
		t.Errorf("HostlistSize = %d, want 0", resp.HostlistSize)
	}
}

// ---------------------------------------------------------------------------
// /api/stats
// ---------------------------------------------------------------------------

func TestHandleStats_Fields(t *testing.T) {
	mc := &mockController{
		stats: types.StatsSnapshot{
			Active: 3, Total: 100, Bypassed: 80, Passthrough: 20,
		},
	}
	_, mux := newTestUI(mc)

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var snap types.StatsSnapshot
	if err := json.NewDecoder(rec.Body).Decode(&snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if snap.Active != 3 {
		t.Errorf("active = %d, want 3", snap.Active)
	}
	if snap.Total != 100 {
		t.Errorf("total = %d, want 100", snap.Total)
	}
	if snap.Bypassed != 80 {
		t.Errorf("bypassed = %d, want 80", snap.Bypassed)
	}
}

// ---------------------------------------------------------------------------
// /api/toggle
// ---------------------------------------------------------------------------

func TestHandleToggle_Enable(t *testing.T) {
	mc := &mockController{enabled: false, strategy: "auto"}
	_, mux := newTestUI(mc)

	body := bytes.NewBufferString(`{"enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/toggle", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !mc.enabled {
		t.Error("controller not enabled after POST /api/toggle {enabled:true}")
	}
	var resp statusResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if !resp.Enabled {
		t.Error("response Enabled = false, want true")
	}
}

func TestHandleToggle_Disable(t *testing.T) {
	mc := &mockController{enabled: true, strategy: "split"}
	_, mux := newTestUI(mc)

	body := bytes.NewBufferString(`{"enabled":false}`)
	req := httptest.NewRequest(http.MethodPost, "/api/toggle", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if mc.enabled {
		t.Error("controller still enabled after {enabled:false}")
	}
}

func TestHandleToggle_MethodNotAllowed(t *testing.T) {
	mc := &mockController{}
	_, mux := newTestUI(mc)

	req := httptest.NewRequest(http.MethodGet, "/api/toggle", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestHandleToggle_BadJSON(t *testing.T) {
	mc := &mockController{}
	_, mux := newTestUI(mc)

	body := bytes.NewBufferString(`not-json`)
	req := httptest.NewRequest(http.MethodPost, "/api/toggle", body)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// /api/strategy
// ---------------------------------------------------------------------------

func TestHandleStrategy_Change(t *testing.T) {
	mc := &mockController{strategy: "auto"}
	_, mux := newTestUI(mc)

	body := bytes.NewBufferString(`{"strategy":"tlsrec"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/strategy", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if mc.strategy != "tlsrec" {
		t.Errorf("strategy = %q, want tlsrec", mc.strategy)
	}
	var resp statusResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Strategy != "tlsrec" {
		t.Errorf("response Strategy = %q, want tlsrec", resp.Strategy)
	}
}

func TestHandleStrategy_MethodNotAllowed(t *testing.T) {
	mc := &mockController{}
	_, mux := newTestUI(mc)

	req := httptest.NewRequest(http.MethodGet, "/api/strategy", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// /api/autodetect
// ---------------------------------------------------------------------------

func TestHandleAutoDetect_ReturnsResults(t *testing.T) {
	mc := &mockController{}
	_, mux := newTestUI(mc)

	body := bytes.NewBufferString(`{"target":"youtube.com:443"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/autodetect", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var results []types.ProbeResult
	if err := json.NewDecoder(rec.Body).Decode(&results); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least one probe result")
	}
}

func TestHandleAutoDetect_EmptyTargetUsesDefault(t *testing.T) {
	mc := &mockController{}
	_, mux := newTestUI(mc)

	// Empty target → handler substitutes "youtube.com:443".
	body := bytes.NewBufferString(`{"target":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/autodetect", body)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// /  (index page)
// ---------------------------------------------------------------------------

func TestHandleIndex_HTML(t *testing.T) {
	mc := &mockController{}
	_, mux := newTestUI(mc)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html; charset=utf-8", ct)
	}
	body := rec.Body.String()
	if len(body) < 100 {
		t.Error("index page body is unexpectedly short")
	}
}
