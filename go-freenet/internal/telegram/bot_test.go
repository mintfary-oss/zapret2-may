// Tests for Telegram bot command handlers and sendMessage / getUpdates over a
// local httptest server (no real Telegram API needed).
package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mintfary-oss/freenet/internal/types"
)

// ---------------------------------------------------------------------------
// mockController
// ---------------------------------------------------------------------------

type mockController struct {
	enabled  bool
	strategy string
	stats    types.StatsSnapshot
}

func (m *mockController) Enabled() bool                 { return m.enabled }
func (m *mockController) SetEnabled(v bool)             { m.enabled = v }
func (m *mockController) Strategy() string              { return m.strategy }
func (m *mockController) SetStrategy(s string)          { m.strategy = s }
func (m *mockController) GetStats() types.StatsSnapshot { return m.stats }

// Ensure mockController satisfies Controller at compile time.
var _ Controller = (*mockController)(nil)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// newTestBot creates a Bot wired to a test server so no real HTTP is needed.
func newTestBot(ctrl Controller) (*Bot, *httptest.Server) {
	// Minimal fake Telegram API: always responds with ok=true, empty updates.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "getUpdates") {
			_ = json.NewEncoder(w).Encode(updatesResponse{OK: true, Result: []apiUpdate{}})
			return
		}
		if strings.Contains(r.URL.Path, "sendMessage") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		http.NotFound(w, r)
	}))

	b := NewBot("test-token", ctrl, 0)
	b.baseURL = srv.URL
	b.client = srv.Client()
	return b, srv
}

// makeMsg creates a minimal apiMessage.
func makeMsg(chatID int64, text string) *apiMessage {
	return &apiMessage{
		MessageID: 1,
		Chat:      apiChat{ID: chatID},
		Text:      text,
	}
}

// ---------------------------------------------------------------------------
// cmdStatus
// ---------------------------------------------------------------------------

func TestCmdStatus_Enabled(t *testing.T) {
	mc := &mockController{enabled: true, strategy: "split"}
	b, srv := newTestBot(mc)
	defer srv.Close()

	reply := b.cmdStatus()
	if !strings.Contains(reply, "ВКЛЮЧЁН") {
		t.Errorf("status reply should contain ВКЛЮЧЁН, got: %q", reply)
	}
	if !strings.Contains(reply, "split") {
		t.Errorf("status reply should contain strategy, got: %q", reply)
	}
}

func TestCmdStatus_Disabled(t *testing.T) {
	mc := &mockController{enabled: false, strategy: "auto"}
	b, srv := newTestBot(mc)
	defer srv.Close()

	reply := b.cmdStatus()
	if !strings.Contains(reply, "ВЫКЛЮЧЕН") {
		t.Errorf("status reply should contain ВЫКЛЮЧЕН, got: %q", reply)
	}
}

// ---------------------------------------------------------------------------
// cmdSetEnabled
// ---------------------------------------------------------------------------

func TestCmdSetEnabled_On(t *testing.T) {
	mc := &mockController{enabled: false, strategy: "auto"}
	b, srv := newTestBot(mc)
	defer srv.Close()

	reply := b.cmdSetEnabled(true)
	if !mc.enabled {
		t.Error("controller not enabled after cmdSetEnabled(true)")
	}
	if !strings.Contains(reply, "включён") {
		t.Errorf("reply should mention enabled, got: %q", reply)
	}
}

func TestCmdSetEnabled_Off(t *testing.T) {
	mc := &mockController{enabled: true, strategy: "split"}
	b, srv := newTestBot(mc)
	defer srv.Close()

	reply := b.cmdSetEnabled(false)
	if mc.enabled {
		t.Error("controller still enabled after cmdSetEnabled(false)")
	}
	if !strings.Contains(reply, "выключен") {
		t.Errorf("reply should mention disabled, got: %q", reply)
	}
}

// ---------------------------------------------------------------------------
// cmdStrategy
// ---------------------------------------------------------------------------

func TestCmdStrategy_Valid(t *testing.T) {
	strategies := []string{"auto", "split", "tlsrec", "disorder", "fake", "combined", "none"}
	for _, s := range strategies {
		mc := &mockController{strategy: "auto"}
		b, srv := newTestBot(mc)

		reply := b.cmdStrategy([]string{s})
		if mc.strategy != s {
			t.Errorf("strategy = %q, want %q", mc.strategy, s)
		}
		if !strings.Contains(reply, s) {
			t.Errorf("reply should contain strategy name %q, got: %q", s, reply)
		}
		srv.Close()
	}
}

func TestCmdStrategy_Invalid(t *testing.T) {
	mc := &mockController{strategy: "auto"}
	b, srv := newTestBot(mc)
	defer srv.Close()

	reply := b.cmdStrategy([]string{"unknownstrat"})
	if mc.strategy != "auto" {
		t.Error("strategy should not change on invalid input")
	}
	if !strings.Contains(strings.ToLower(reply), "неизвестная") {
		t.Errorf("reply should mention unknown strategy, got: %q", reply)
	}
}

func TestCmdStrategy_NoArgs(t *testing.T) {
	mc := &mockController{strategy: "tlsrec"}
	b, srv := newTestBot(mc)
	defer srv.Close()

	reply := b.cmdStrategy(nil)
	if !strings.Contains(reply, "tlsrec") {
		t.Errorf("no-arg reply should show current strategy, got: %q", reply)
	}
}

// ---------------------------------------------------------------------------
// cmdStats
// ---------------------------------------------------------------------------

func TestCmdStats_Fields(t *testing.T) {
	mc := &mockController{
		stats: types.StatsSnapshot{
			Active: 3, Total: 50, Bypassed: 45, Passthrough: 5,
		},
	}
	b, srv := newTestBot(mc)
	defer srv.Close()

	reply := b.cmdStats()
	for _, want := range []string{"3", "50", "45", "5"} {
		if !strings.Contains(reply, want) {
			t.Errorf("stats reply missing %q: %q", want, reply)
		}
	}
}

// ---------------------------------------------------------------------------
// handleMessage dispatch
// ---------------------------------------------------------------------------

func TestHandleMessage_Help(t *testing.T) {
	mc := &mockController{}
	b, srv := newTestBot(mc)
	defer srv.Close()

	// Should not panic and should send a message.
	b.handleMessage(context.Background(), makeMsg(42, "/help"))
}

func TestHandleMessage_UnknownCommand(t *testing.T) {
	mc := &mockController{}
	b, srv := newTestBot(mc)
	defer srv.Close()

	b.handleMessage(context.Background(), makeMsg(42, "/thiscommanddoesnotexist"))
}

func TestHandleMessage_AllowedChatID_Rejects(t *testing.T) {
	mc := &mockController{enabled: false}
	b, srv := newTestBot(mc)
	defer srv.Close()
	b.allowedChatID = 99 // only chat 99 allowed

	// Chat 42 sends /on — should be silently ignored.
	b.handleMessage(context.Background(), makeMsg(42, "/on"))
	if mc.enabled {
		t.Error("bot should reject unauthorised chat ID")
	}
}

func TestHandleMessage_AllowedChatID_Accepts(t *testing.T) {
	mc := &mockController{enabled: false, strategy: "auto"}
	b, srv := newTestBot(mc)
	defer srv.Close()
	b.allowedChatID = 99

	b.handleMessage(context.Background(), makeMsg(99, "/on"))
	if !mc.enabled {
		t.Error("bot should accept authorised chat ID")
	}
}

func TestHandleMessage_BotUsernameStripped(t *testing.T) {
	mc := &mockController{enabled: false, strategy: "auto"}
	b, srv := newTestBot(mc)
	defer srv.Close()

	// /on@MyBotName should work the same as /on.
	b.handleMessage(context.Background(), makeMsg(1, "/on@FreeNetBot"))
	if !mc.enabled {
		t.Error("bot should strip @username suffix from command")
	}
}

// ---------------------------------------------------------------------------
// sendMessage over httptest
// ---------------------------------------------------------------------------

func TestSendMessage_Success(t *testing.T) {
	mc := &mockController{}
	b, srv := newTestBot(mc)
	defer srv.Close()

	err := b.sendMessage(context.Background(), 12345, "test message")
	if err != nil {
		t.Errorf("sendMessage error: %v", err)
	}
}

func TestSendMessage_ServerError(t *testing.T) {
	mc := &mockController{}
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer errSrv.Close()

	b := NewBot("tok", mc, 0)
	b.baseURL = errSrv.URL
	b.client = errSrv.Client()

	err := b.sendMessage(context.Background(), 1, "hi")
	if err == nil {
		t.Error("expected error on HTTP 500, got nil")
	}
}

// ---------------------------------------------------------------------------
// getUpdates over httptest
// ---------------------------------------------------------------------------

func TestGetUpdates_Empty(t *testing.T) {
	mc := &mockController{}
	b, srv := newTestBot(mc)
	defer srv.Close()

	updates, err := b.getUpdates(context.Background(), 0, 1)
	if err != nil {
		t.Fatalf("getUpdates: %v", err)
	}
	if len(updates) != 0 {
		t.Errorf("expected 0 updates, got %d", len(updates))
	}
}

func TestGetUpdates_APIError(t *testing.T) {
	mc := &mockController{}
	// Server returns ok=false.
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false}`))
	}))
	defer errSrv.Close()

	b := NewBot("tok", mc, 0)
	b.baseURL = errSrv.URL
	b.client = errSrv.Client()

	_, err := b.getUpdates(context.Background(), 0, 1)
	if err == nil {
		t.Error("expected error when ok=false, got nil")
	}
}
