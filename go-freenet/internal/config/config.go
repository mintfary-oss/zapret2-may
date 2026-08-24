// Package config loads and validates FreeNet configuration from a YAML file.
package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration structure.
type Config struct {
	Proxy    ProxyConfig    `yaml:"proxy"`
	Bypass   BypassConfig   `yaml:"bypass"`
	Hostlist HostlistConfig `yaml:"hostlist"`
}

// ProxyConfig controls the listening addresses.
type ProxyConfig struct {
	// ListenAddr is the SOCKS5 listen address, e.g. "127.0.0.1:1080".
	ListenAddr string `yaml:"listen_addr"`
	// TransparentAddr is the transparent-proxy listen address used with
	// iptables REDIRECT (Linux only). Empty string disables transparent mode.
	TransparentAddr string `yaml:"transparent_addr"`
}

// BypassConfig selects the active DPI evasion strategy.
type BypassConfig struct {
	// Strategy is one of: auto, split, disorder, fake, quic, none.
	Strategy string `yaml:"strategy"`
	// SplitPos is the byte position inside the TLS ClientHello where the
	// stream is split. 0 means split after the first byte of the record.
	SplitPos int `yaml:"split_pos"`
	// FakeTTL is the IP TTL assigned to decoy packets so they die before
	// reaching the server but are processed by the DPI box.
	FakeTTL int `yaml:"fake_ttl"`
	// DisorderFrag enables TCP segment re-ordering (disorder attack).
	DisorderFrag bool `yaml:"disorder_frag"`
}

// HostlistConfig controls domain filtering.
type HostlistConfig struct {
	// Enabled activates per-domain bypass (only bypass listed domains).
	Enabled bool `yaml:"enabled"`
	// Path is the file path to a newline-separated list of domains.
	Path string `yaml:"path"`
	// AutoUpdate downloads an updated list from URL on startup.
	AutoUpdate bool   `yaml:"auto_update"`
	URL        string `yaml:"url"`
}

// defaults returns a Config pre-filled with safe defaults.
func defaults() *Config {
	return &Config{
		Proxy: ProxyConfig{
			ListenAddr:      "127.0.0.1:1080",
			TransparentAddr: "",
		},
		Bypass: BypassConfig{
			Strategy: "auto",
			SplitPos: 2,
			FakeTTL:  8,
		},
		Hostlist: HostlistConfig{
			Enabled:    false,
			AutoUpdate: true,
			URL:        "https://antifilter.download/list/domains.lst",
		},
	}
}

// Load reads cfgPath and returns a validated Config.
// If the file does not exist a default config is written and returned.
func Load(cfgPath string) (*Config, error) {
	cfg := defaults()

	data, err := os.ReadFile(cfgPath)
	if os.IsNotExist(err) {
		// Write defaults so the user has a template to edit.
		if werr := write(cfgPath, cfg); werr != nil {
			// Non-fatal: continue with in-memory defaults.
			_ = werr
		}
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Save persists cfg to cfgPath.
func Save(cfgPath string, cfg *Config) error {
	return write(cfgPath, cfg)
}

func write(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
