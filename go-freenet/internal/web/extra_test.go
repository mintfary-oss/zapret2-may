// Additional handler tests: handleDownload, writeJSON, handleIndex path.
package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// handleDownload
// ---------------------------------------------------------------------------

func TestHandleDownload_Redirect(t *testing.T) {
	mc := &mockController{enabled: true, strategy: "auto"}
	_, mux := newTestUI(mc)

	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		// Obtain the UI from the closure to call handleDownload.
	})

	// Wire directly.
	mc2 := &mockController{}
	ui, _ := newTestUI(mc2)
	req := httptest.NewRequest(http.MethodGet, "/download", nil)
	rec := httptest.NewRecorder()
	ui.handleDownload(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("handleDownload status = %d, want %d (SeeOther)", rec.Code, http.StatusSeeOther)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "tab=download") {
		t.Errorf("Location = %q, want to contain 'tab=download'", loc)
	}
}

// ---------------------------------------------------------------------------
// handleIndex
// ---------------------------------------------------------------------------

func TestHandleIndex_ContentType(t *testing.T) {
	mc := &mockController{}
	ui, _ := newTestUI(mc)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	ui.handleIndex(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("handleIndex status = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<!DOCTYPE html>") && !strings.Contains(body, "<html") {
		t.Error("handleIndex body does not look like HTML")
	}
}

// ---------------------------------------------------------------------------
// writeJSON
// ---------------------------------------------------------------------------

func TestWriteJSON_EncodesCorrectly(t *testing.T) {
	rec := httptest.NewRecorder()
	v := map[string]int{"a": 1, "b": 2}
	writeJSON(rec, v)

	if rec.Code != http.StatusOK {
		t.Errorf("writeJSON status = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var got map[string]int
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("writeJSON: cannot unmarshal response: %v", err)
	}
	if got["a"] != 1 || got["b"] != 2 {
		t.Errorf("writeJSON decoded = %v, want {a:1 b:2}", got)
	}
}

// ---------------------------------------------------------------------------
// NewUI
// ---------------------------------------------------------------------------

func TestNewUI_NotNil(t *testing.T) {
	mc := &mockController{}
	ui, _ := newTestUI(mc)
	if ui == nil {
		t.Error("NewUI returned nil")
	}
}

// ---------------------------------------------------------------------------
// handleAutoDetect — bad-method branch
// ---------------------------------------------------------------------------

func TestHandleAutoDetect_MethodNotAllowed(t *testing.T) {
	mc := &mockController{}
	_, mux := newTestUI(mc)

	req := httptest.NewRequest(http.MethodGet, "/api/autodetect", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("autodetect GET status = %d, want 405", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// handleStrategy — bad-JSON branch  (not covered by existing tests)
// ---------------------------------------------------------------------------

func TestHandleStrategy_BadJSON(t *testing.T) {
	mc := &mockController{strategy: "split"}
	_, mux := newTestUI(mc)

	req := httptest.NewRequest(http.MethodPost, "/api/strategy",
		strings.NewReader("{invalid json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("handleStrategy bad JSON status = %d, want 400", rec.Code)
	}
}
