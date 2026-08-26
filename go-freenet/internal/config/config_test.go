package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDefaults verifies the hard-coded default values returned by defaults().
func TestDefaults(t *testing.T) {
	cfg := defaults()

	if cfg.Proxy.ListenAddr != "127.0.0.1:1080" {
		t.Errorf("Proxy.ListenAddr = %q, want 127.0.0.1:1080", cfg.Proxy.ListenAddr)
	}
	if cfg.Bypass.Strategy != "auto" {
		t.Errorf("Bypass.Strategy = %q, want auto", cfg.Bypass.Strategy)
	}
	if cfg.Bypass.SplitPos != 2 {
		t.Errorf("Bypass.SplitPos = %d, want 2", cfg.Bypass.SplitPos)
	}
	if cfg.Bypass.FakeTTL != 8 {
		t.Errorf("Bypass.FakeTTL = %d, want 8", cfg.Bypass.FakeTTL)
	}
	if cfg.NFQueue.Enabled {
		t.Error("NFQueue.Enabled should be false by default")
	}
	if cfg.NFQueue.QueueNum != 200 {
		t.Errorf("NFQueue.QueueNum = %d, want 200", cfg.NFQueue.QueueNum)
	}
	if !cfg.DNS.Enabled {
		t.Error("DNS.Enabled should be true by default")
	}
	if cfg.DNS.ListenAddr != "127.0.0.1:5300" {
		t.Errorf("DNS.ListenAddr = %q, want 127.0.0.1:5300", cfg.DNS.ListenAddr)
	}
}

// TestLoad_MissingFile verifies that Load creates a default config file
// when the target path does not exist and returns valid defaults.
func TestLoad_MissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "freenet.yaml")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load on missing file returned error: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load returned nil config")
	}

	// The file should have been written by Load.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("Load should have created a default config file, but the file is absent")
	}

	// Defaults must be intact.
	if cfg.Bypass.Strategy != "auto" {
		t.Errorf("Bypass.Strategy after Load = %q, want auto", cfg.Bypass.Strategy)
	}
}

// TestLoad_ValidFile verifies that Load correctly unmarshals a YAML file.
func TestLoad_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "freenet.yaml")

	yaml := `
proxy:
  listen_addr: "0.0.0.0:8080"
bypass:
  strategy: "split"
  split_pos: 3
  fake_ttl: 5
dns:
  enabled: false
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if cfg.Proxy.ListenAddr != "0.0.0.0:8080" {
		t.Errorf("Proxy.ListenAddr = %q, want 0.0.0.0:8080", cfg.Proxy.ListenAddr)
	}
	if cfg.Bypass.Strategy != "split" {
		t.Errorf("Bypass.Strategy = %q, want split", cfg.Bypass.Strategy)
	}
	if cfg.Bypass.SplitPos != 3 {
		t.Errorf("Bypass.SplitPos = %d, want 3", cfg.Bypass.SplitPos)
	}
	if cfg.Bypass.FakeTTL != 5 {
		t.Errorf("Bypass.FakeTTL = %d, want 5", cfg.Bypass.FakeTTL)
	}
	if cfg.DNS.Enabled {
		t.Error("DNS.Enabled should be false after YAML override")
	}
}

// TestLoad_InvalidYAML verifies that Load returns an error for malformed YAML.
func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")

	if err := os.WriteFile(path, []byte("proxy: [invalid yaml {"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Error("Load should return an error for invalid YAML")
	}
}

// TestLoad_ReadError verifies that Load returns an error when the config file
// exists but cannot be read (permission denied).
func TestLoad_ReadError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can read any file, skipping permission test")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "noperm.yaml")

	if err := os.WriteFile(path, []byte("proxy:\n  listen_addr: 127.0.0.1:1080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	_, err := Load(path)
	if err == nil {
		t.Error("Load should return an error for unreadable file")
	}
}

// TestSave_RoundTrip verifies that Save writes valid YAML that Load can read back.
func TestSave_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "save_test.yaml")

	original := defaults()
	original.Bypass.Strategy = "tlsrec"
	original.Proxy.ListenAddr = "127.0.0.1:9090"
	original.DNS.Enabled = false
	original.Telegram.Token = "test-token-123"
	original.Telegram.AllowedChatID = 42

	if err := Save(path, original); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save error: %v", err)
	}

	if loaded.Bypass.Strategy != "tlsrec" {
		t.Errorf("round-trip Bypass.Strategy = %q, want tlsrec", loaded.Bypass.Strategy)
	}
	if loaded.Proxy.ListenAddr != "127.0.0.1:9090" {
		t.Errorf("round-trip Proxy.ListenAddr = %q, want 127.0.0.1:9090", loaded.Proxy.ListenAddr)
	}
	if loaded.DNS.Enabled {
		t.Error("round-trip DNS.Enabled should be false")
	}
	if loaded.Telegram.Token != "test-token-123" {
		t.Errorf("round-trip Telegram.Token = %q, want test-token-123", loaded.Telegram.Token)
	}
	if loaded.Telegram.AllowedChatID != 42 {
		t.Errorf("round-trip Telegram.AllowedChatID = %d, want 42", loaded.Telegram.AllowedChatID)
	}
}

// TestLoad_DefaultsFieldsPreserved verifies that unset YAML fields keep
// defaults rather than zero-values when Load merges a partial config.
func TestLoad_DefaultsFieldsPreserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "partial.yaml")

	// Only override strategy; everything else should stay at defaults.
	yaml := "bypass:\n  strategy: fake\n"
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Bypass.Strategy != "fake" {
		t.Errorf("Bypass.Strategy = %q, want fake", cfg.Bypass.Strategy)
	}
	// Proxy.ListenAddr was not overridden — should be the default.
	if cfg.Proxy.ListenAddr != "127.0.0.1:1080" {
		t.Errorf("Proxy.ListenAddr = %q, want 127.0.0.1:1080 (default)", cfg.Proxy.ListenAddr)
	}
}

// TestTelegramConfig_ZeroToken verifies that an empty Token field disables
// the bot by leaving the default zero value.
func TestTelegramConfig_ZeroToken(t *testing.T) {
	cfg := defaults()
	if cfg.Telegram.Token != "" {
		t.Errorf("default Telegram.Token should be empty, got %q", cfg.Telegram.Token)
	}
	if cfg.Telegram.AllowedChatID != 0 {
		t.Errorf("default Telegram.AllowedChatID should be 0, got %d", cfg.Telegram.AllowedChatID)
	}
}
