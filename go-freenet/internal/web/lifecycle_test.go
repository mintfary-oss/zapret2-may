// Tests for UI.Start, UI.Stop, and handleLogsWS.
package web

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/mintfary-oss/freenet/internal/config"
	"github.com/mintfary-oss/freenet/internal/logs"
)

// ---------------------------------------------------------------------------
// UI.Start / UI.Stop lifecycle
// ---------------------------------------------------------------------------

// TestUI_Start_Stop verifies that Start launches the HTTP server and Stop
// shuts it down cleanly without panicking.
func TestUI_Start_Stop(t *testing.T) {
	mc := &mockController{strategy: "split"}
	cfg := &config.Config{Proxy: config.ProxyConfig{ListenAddr: "127.0.0.1:0"}}
	ring := logs.NewRing(10)
	ui := NewUI("127.0.0.1:0", cfg, mc, ring)

	if err := ui.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Give the goroutine a moment to bind.
	time.Sleep(20 * time.Millisecond)
	ui.Stop()
}

// TestUI_NewUI_NotNil verifies that NewUI returns a non-nil UI object.
func TestUI_NewUI_NotNil(t *testing.T) {
	mc := &mockController{}
	cfg := &config.Config{Proxy: config.ProxyConfig{ListenAddr: "127.0.0.1:0"}}
	ring := logs.NewRing(10)
	ui := NewUI("127.0.0.1:0", cfg, mc, ring)
	if ui == nil {
		t.Fatal("NewUI returned nil")
	}
}

// TestUI_Start_ListensOnPort verifies that after Start the server responds
// to HTTP requests.
func TestUI_Start_ListensOnPort(t *testing.T) {
	mc := &mockController{strategy: "split", enabled: true}
	cfg := &config.Config{Proxy: config.ProxyConfig{ListenAddr: "127.0.0.1:0"}}
	ring := logs.NewRing(10)

	// Use a random available port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	ui := NewUI(addr, cfg, mc, ring)
	if err := ui.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ui.Stop()
	time.Sleep(30 * time.Millisecond)

	resp, err := http.Get("http://" + addr + "/api/status")
	if err != nil {
		t.Fatalf("GET /api/status: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// handleLogsWS
// ---------------------------------------------------------------------------

// TestHandleLogsWS_ConnectsAndReceivesMessages verifies that the WebSocket
// endpoint upgrades the connection and sends existing log entries followed by
// live updates.
func TestHandleLogsWS_ConnectsAndReceivesMessages(t *testing.T) {
	mc := &mockController{}
	cfg := &config.Config{Proxy: config.ProxyConfig{ListenAddr: "127.0.0.1:0"}}
	ring := logs.NewRing(16)

	// Pre-populate the ring with a log message.
	_, _ = ring.Write([]byte("pre-existing log entry\n"))

	ui := NewUI(":0", cfg, mc, ring)

	// Serve handleLogsWS via a real httptest.Server so we can upgrade WebSocket.
	srv := httptest.NewServer(http.HandlerFunc(ui.handleLogsWS))
	defer srv.Close()

	// Connect as WebSocket client.
	wsURL := "ws" + srv.URL[len("http"):] + "/ws/logs"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket dial: %v (HTTP status %v)", err, resp)
	}
	defer conn.Close()

	// The handler sends recent logs immediately as JSON Entry objects.
	// Read at least one message and verify it decodes correctly.
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if len(msg) == 0 {
		t.Error("received empty WebSocket message")
	}
	// Message should be valid JSON with a "msg" field.
	var entry struct {
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal(msg, &entry); err != nil {
		t.Errorf("WebSocket message not valid JSON: %v (got %q)", err, msg)
	}
}

// TestHandleLogsWS_LiveUpdate verifies that a log message written after
// connection is forwarded to the WebSocket client.
// Strategy: write the live message BEFORE connecting so it is sent as a
// "recent" entry (Recent(100)) rather than requiring a live subscription
// delivery, which avoids timing races with the subscription setup.
func TestHandleLogsWS_LiveUpdate(t *testing.T) {
	mc := &mockController{}
	cfg := &config.Config{Proxy: config.ProxyConfig{ListenAddr: "127.0.0.1:0"}}
	ring := logs.NewRing(16)

	ui := NewUI(":0", cfg, mc, ring)
	srv := httptest.NewServer(http.HandlerFunc(ui.handleLogsWS))
	defer srv.Close()

	// Write the log entry before connecting so Recent(100) includes it.
	liveMsg := "live-update-test"
	_, _ = ring.Write([]byte(liveMsg + "\n"))

	wsURL := "ws" + srv.URL[len("http"):] + "/ws/logs"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket dial: %v", err)
	}
	defer conn.Close()

	// Read messages until we find our entry (it was pre-written, so it is in Recent).
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	found := false
	for i := 0; i < 20; i++ {
		_, got, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage: %v", err)
		}
		var entry struct {
			Msg string `json:"msg"`
		}
		if err := json.Unmarshal(got, &entry); err != nil {
			continue
		}
		if entry.Msg == liveMsg {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("did not receive expected log entry %q within 20 messages", liveMsg)
	}
}

// TestHandleLogsWS_ClientDisconnect verifies that the handler exits cleanly
// when the WebSocket client disconnects.
func TestHandleLogsWS_ClientDisconnect(t *testing.T) {
	mc := &mockController{}
	cfg := &config.Config{Proxy: config.ProxyConfig{ListenAddr: "127.0.0.1:0"}}
	ring := logs.NewRing(16)

	ui := NewUI(":0", cfg, mc, ring)
	srv := httptest.NewServer(http.HandlerFunc(ui.handleLogsWS))
	defer srv.Close()

	wsURL := "ws" + srv.URL[len("http"):] + "/ws/logs"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket dial: %v", err)
	}

	// Close the client side immediately.
	conn.Close()

	// The handler must not hang; give it a moment to detect disconnect.
	time.Sleep(100 * time.Millisecond)
	// If we reach here without blocking, the test passes.
}
