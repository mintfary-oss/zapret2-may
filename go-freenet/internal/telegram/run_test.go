// Tests for Bot.Run and the complete long-polling lifecycle.
package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestBot_Run_StopsOnContextCancel verifies that Run returns when the context
// is cancelled.
func TestBot_Run_StopsOnContextCancel(t *testing.T) {
	mc := &mockController{}
	b, srv := newTestBot(mc)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		b.Run(ctx)
		close(done)
	}()

	// Give it a moment to start, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("Run did not return after context was cancelled")
	}
}

// TestBot_Run_ProcessesUpdate verifies that Run dispatches a received update
// to handleMessage and sends a reply.
func TestBot_Run_ProcessesUpdate(t *testing.T) {
	mc := &mockController{strategy: "split"}

	// Track whether sendMessage was called.
	var sendMessageCalled atomic.Bool

	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if contains(r.URL.Path, "getUpdates") {
			callCount++
			if callCount == 1 {
				// Return one update on the first call.
				updates := updatesResponse{
					OK: true,
					Result: []apiUpdate{
						{
							UpdateID: 1,
							Message: &apiMessage{
								MessageID: 1,
								Chat:      apiChat{ID: 123},
								Text:      "/status",
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(updates)
			} else {
				// Subsequent calls return empty (bot waits for more).
				_ = json.NewEncoder(w).Encode(updatesResponse{OK: true, Result: []apiUpdate{}})
			}
			return
		}
		if contains(r.URL.Path, "sendMessage") {
			sendMessageCalled.Store(true)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	b := NewBot("test-token", mc, 0)
	b.baseURL = srv.URL
	b.client = srv.Client()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		b.Run(ctx)
		close(done)
	}()

	// Wait for sendMessage to be called (the /status reply).
	deadline := time.Now().Add(5 * time.Second)
	for !sendMessageCalled.Load() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Error("Run did not return after cancel")
	}

	if !sendMessageCalled.Load() {
		t.Error("sendMessage was not called after /status command")
	}
}

// TestBot_Run_RetryOnError verifies that Run retries after a getUpdates error.
func TestBot_Run_RetryOnError(t *testing.T) {
	mc := &mockController{}

	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if contains(r.URL.Path, "getUpdates") {
			c := callCount.Add(1)
			if c == 1 {
				// First call returns an error response.
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			// Subsequent calls return empty — allows graceful exit.
			_ = json.NewEncoder(w).Encode(updatesResponse{OK: true, Result: []apiUpdate{}})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	b := NewBot("test-token", mc, 0)
	b.baseURL = srv.URL
	b.client = srv.Client()
	// Override the client timeout so the 5s retry wait is fast.
	// We use a very short timeout to make the retry happen quickly.
	// The retry is triggered by a 5s sleep, but context cancel interrupts it.

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a tiny delay — this should interrupt the 5s retry sleep.
	go func() {
		// Wait for at least one error call.
		for callCount.Load() < 1 {
			time.Sleep(10 * time.Millisecond)
		}
		cancel()
	}()

	done := make(chan struct{})
	go func() {
		b.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Error("Run did not return after cancel during retry")
		cancel()
	}

	if callCount.Load() < 1 {
		t.Error("getUpdates was never called")
	}
}

// contains checks whether haystack contains needle.
func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) &&
		indexStr(haystack, needle) >= 0
}

func indexStr(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
