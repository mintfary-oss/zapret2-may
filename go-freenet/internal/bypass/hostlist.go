// Package bypass — hostlist: per-domain filtering.
// When the hostlist is enabled only domains present in the list are routed
// through the DPI bypass engine; all others are relayed without modification.
package bypass

import (
	"bufio"
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Hostlist stores a set of domain names and answers membership queries.
// All methods are safe for concurrent use.
type Hostlist struct {
	mu         sync.RWMutex
	domains    map[string]struct{}
	enabled    bool
	httpClient *http.Client // optional; nil = use default
}

// SetHTTPClient replaces the HTTP client used by DownloadAndSave.
// Pass a DoH-aware client to protect list downloads from DNS poisoning.
// Must be called before DownloadAndSave to take effect.
func (h *Hostlist) SetHTTPClient(c *http.Client) {
	h.mu.Lock()
	h.httpClient = c
	h.mu.Unlock()
}

// NewHostlist returns an empty, disabled Hostlist.
func NewHostlist() *Hostlist {
	return &Hostlist{domains: make(map[string]struct{})}
}

// LoadFile replaces the current domain set by reading a newline-separated
// file. Lines beginning with '#' and empty lines are ignored.
func (h *Hostlist) LoadFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return h.load(f)
}

// DownloadAndSave fetches the domain list from url, saves it to path and
// loads it into memory.  If SetHTTPClient has been called the provided client
// is used; otherwise a default client with a 30-second timeout is created.
func (h *Hostlist) DownloadAndSave(ctx context.Context, url, path string) error {
	log.Printf("hostlist: downloading %s", url)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	h.mu.RLock()
	client := h.httpClient
	h.mu.RUnlock()
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Write to temp file first to avoid partial reads on failure.
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()

	if err := os.Rename(tmp, path); err != nil {
		return err
	}

	count, err := h.loadFile(path)
	if err != nil {
		return err
	}
	log.Printf("hostlist: loaded %d domains from %s", count, url)
	return nil
}

// Contains reports whether domain (or any of its parent labels) is in the
// list.  E.g. "www.youtube.com" matches if "youtube.com" is listed.
func (h *Hostlist) Contains(domain string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	for {
		if _, ok := h.domains[domain]; ok {
			return true
		}
		dot := strings.IndexByte(domain, '.')
		if dot < 0 {
			break
		}
		domain = domain[dot+1:]
	}
	return false
}

// Enable activates or deactivates domain filtering.
func (h *Hostlist) Enable(v bool) {
	h.mu.Lock()
	h.enabled = v
	h.mu.Unlock()
}

// Enabled returns the current filtering state.
func (h *Hostlist) Enabled() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.enabled
}

// ShouldBypass returns true when bypass should be applied for domain.
// If filtering is disabled every domain is bypassed.
func (h *Hostlist) ShouldBypass(domain string) bool {
	if !h.Enabled() {
		return true
	}
	return h.Contains(domain)
}

// Size returns the number of loaded domains.
func (h *Hostlist) Size() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.domains)
}

// --- internal helpers ---

func (h *Hostlist) load(r io.Reader) error {
	m := make(map[string]struct{})
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		m[strings.ToLower(line)] = struct{}{}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	h.mu.Lock()
	h.domains = m
	h.mu.Unlock()
	return nil
}

func (h *Hostlist) loadFile(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if err := h.load(f); err != nil {
		return 0, err
	}
	return h.Size(), nil
}
