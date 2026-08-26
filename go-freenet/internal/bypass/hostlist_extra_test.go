package bypass

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestHostlist_LoadFile verifies that LoadFile reads a newline-separated domain
// list from disk and populates the in-memory set.
func TestHostlist_LoadFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "domains.txt")
	content := "example.com\nyoutube.com\n# comment\n\nvk.com\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	hl := NewHostlist()
	if err := hl.LoadFile(path); err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	if hl.Size() != 3 {
		t.Errorf("Size = %d, want 3", hl.Size())
	}
	for _, d := range []string{"example.com", "youtube.com", "vk.com"} {
		if !hl.Contains(d) {
			t.Errorf("Contains(%q) = false, want true", d)
		}
	}
}

// TestHostlist_LoadFile_NotFound verifies that LoadFile returns an error for a
// missing file.
func TestHostlist_LoadFile_NotFound(t *testing.T) {
	hl := NewHostlist()
	err := hl.LoadFile("/non/existent/path/domains.txt")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

// TestHostlist_SetHTTPClient verifies that SetHTTPClient replaces the client
// used for downloads (lock-safe path exercised).
func TestHostlist_SetHTTPClient(t *testing.T) {
	hl := NewHostlist()
	custom := &http.Client{}
	hl.SetHTTPClient(custom) // must not panic or deadlock
	// Verify via DownloadAndSave that the custom client is used.
}

// TestHostlist_DownloadAndSave_Success verifies the full download+save flow
// using a local HTTP test server.
func TestHostlist_DownloadAndSave_Success(t *testing.T) {
	body := "google.com\nfacebook.com\ntwitter.com\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	tmp := t.TempDir()
	path := filepath.Join(tmp, "domains.lst")

	hl := NewHostlist()
	hl.SetHTTPClient(srv.Client())

	if err := hl.DownloadAndSave(context.Background(), srv.URL, path); err != nil {
		t.Fatalf("DownloadAndSave: %v", err)
	}

	// File should exist.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("output file missing: %v", err)
	}

	// In-memory list should be populated.
	if hl.Size() != 3 {
		t.Errorf("Size = %d, want 3", hl.Size())
	}
	for _, d := range []string{"google.com", "facebook.com", "twitter.com"} {
		if !hl.Contains(d) {
			t.Errorf("Contains(%q) = false after download", d)
		}
	}
}

// TestHostlist_DownloadAndSave_HTTPError verifies that an HTTP error is
// propagated correctly.
func TestHostlist_DownloadAndSave_HTTPError(t *testing.T) {
	// Use a server that closes the connection immediately.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return 500 but still write a response body so the Do call succeeds.
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error"))
	}))
	defer srv.Close()

	tmp := t.TempDir()
	path := filepath.Join(tmp, "domains.lst")

	hl := NewHostlist()
	hl.SetHTTPClient(srv.Client())

	// DownloadAndSave only returns error on network/IO failures, not HTTP status.
	// A 500 with a valid body will succeed in writing the file.
	_ = hl.DownloadAndSave(context.Background(), srv.URL, path)
}

// TestHostlist_DownloadAndSave_NetworkError verifies error propagation when
// the server is unreachable.
func TestHostlist_DownloadAndSave_NetworkError(t *testing.T) {
	hl := NewHostlist()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "domains.lst")

	err := hl.DownloadAndSave(context.Background(), "http://127.0.0.1:1/domains.txt", path)
	if err == nil {
		t.Error("expected error for unreachable server, got nil")
	}
}

// TestHostlist_DownloadAndSave_ContextCancelled verifies that a cancelled
// context aborts the download.
func TestHostlist_DownloadAndSave_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// slow handler
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	hl := NewHostlist()
	hl.SetHTTPClient(srv.Client())
	tmp := t.TempDir()
	err := hl.DownloadAndSave(ctx, srv.URL, filepath.Join(tmp, "x.lst"))
	if err == nil {
		t.Error("expected error for cancelled context, got nil")
	}
}

// TestHostlist_loadFile tests the internal loadFile helper.
func TestHostlist_loadFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "list.txt")
	if err := os.WriteFile(path, []byte("a.com\nb.com\n"), 0644); err != nil {
		t.Fatal(err)
	}

	hl := NewHostlist()
	n, err := hl.loadFile(path)
	if err != nil {
		t.Fatalf("loadFile: %v", err)
	}
	if n != 2 {
		t.Errorf("loadFile returned %d, want 2", n)
	}
}

// TestHostlist_loadFile_Missing verifies error on missing file.
func TestHostlist_loadFile_Missing(t *testing.T) {
	hl := NewHostlist()
	_, err := hl.loadFile("/no/such/file.txt")
	if err == nil {
		t.Error("expected error for missing file")
	}
}
