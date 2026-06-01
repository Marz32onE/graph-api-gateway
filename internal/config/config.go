// Package config loads runtime configuration from the environment.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

// Config is the fully-resolved runtime configuration.
type Config struct {
	ListenAddr string
	LogLevel   string
	LogFormat  string
	KSG        KubeGraph
	Switch     Backend

	// Inbound API-key auth. These are independent of the per-backend outbound
	// X-API-Key credentials: they gate the gateway's OWN endpoints. When both
	// APIKeys and APIKeysFile are empty, auth is disabled and every route is
	// served without a key.
	APIKeys               string        // comma-separated accepted keys (API_KEYS)
	APIKeysFile           string        // path to a keys file, one per line (API_KEYS_FILE)
	APIKeysReloadInterval time.Duration // re-read APIKeysFile every interval; 0 disables hot reload
}

// KubeGraph configures the in-process kube-state-graph engine (the primary). It
// queries VictoriaMetrics directly via the embedded pkg/kubegraph engine instead
// of calling a kube-state-graph HTTP service.
type KubeGraph struct {
	VictoriaMetricsURL string        // VICTORIA_METRICS_URL — the upstream the engine queries
	MetricPrefix       string        // KSG_METRIC_PREFIX — kube-state-metrics metric-name prefix (D26)
	BuildTimeout       time.Duration // KSG_BUILD_TIMEOUT — bounds the in-process build
}

// Backend is the HTTP switch backend.
type Backend struct {
	BaseURL string
	APIKey  string
	Timeout time.Duration
}

const (
	defaultListenAddr            = ":8080"
	defaultLogLevel              = "info"
	defaultLogFormat             = "json"
	defaultBackendTimeout        = 10 * time.Second
	defaultBuildTimeout          = 15 * time.Second
	defaultAPIKeysReloadInterval = 30 * time.Second
)

var validLogLevels = map[string]struct{}{
	"debug": {}, "info": {}, "warn": {}, "error": {},
}

var validLogFormats = map[string]struct{}{
	"json": {}, "text": {},
}

// Load reads configuration from the environment, applies defaults, and validates.
func Load() (*Config, error) {
	cfg := &Config{
		ListenAddr: getenvDefault("LISTEN_ADDR", defaultListenAddr),
		LogLevel:   strings.ToLower(getenvDefault("LOG_LEVEL", defaultLogLevel)),
		LogFormat:  strings.ToLower(getenvDefault("LOG_FORMAT", defaultLogFormat)),
	}

	ksg, err := loadKubeGraph()
	if err != nil {
		return nil, err
	}
	cfg.KSG = ksg

	switchBackend, err := loadBackend("SWITCH_GRAPH")
	if err != nil {
		return nil, err
	}
	cfg.Switch = switchBackend

	cfg.APIKeys = os.Getenv("API_KEYS")
	cfg.APIKeysFile = os.Getenv("API_KEYS_FILE")
	reload, err := loadReloadInterval()
	if err != nil {
		return nil, err
	}
	cfg.APIKeysReloadInterval = reload

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// loadReloadInterval parses API_KEYS_RELOAD_INTERVAL, defaulting when unset. A
// value of 0 disables hot reload; negative or unparseable values are rejected.
func loadReloadInterval() (time.Duration, error) {
	v := os.Getenv("API_KEYS_RELOAD_INTERVAL")
	if v == "" {
		return defaultAPIKeysReloadInterval, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("config: API_KEYS_RELOAD_INTERVAL is not a valid duration: %q", v)
	}
	if d < 0 {
		return 0, fmt.Errorf("config: API_KEYS_RELOAD_INTERVAL must be >= 0, got %s", d)
	}
	return d, nil
}

// loadKubeGraph reads the in-process engine config. VICTORIA_METRICS_URL is
// required; KSG_METRIC_PREFIX and KSG_BUILD_TIMEOUT are optional.
func loadKubeGraph() (KubeGraph, error) {
	raw, err := parseHTTPURL("VICTORIA_METRICS_URL")
	if err != nil {
		return KubeGraph{}, err
	}
	timeout, err := loadTimeout("KSG_BUILD_TIMEOUT", defaultBuildTimeout)
	if err != nil {
		return KubeGraph{}, err
	}
	return KubeGraph{
		VictoriaMetricsURL: raw,
		MetricPrefix:       os.Getenv("KSG_METRIC_PREFIX"),
		BuildTimeout:       timeout,
	}, nil
}

// loadBackend reads an HTTP backend config from {prefix}_URL / _API_KEY / _TIMEOUT.
func loadBackend(prefix string) (Backend, error) {
	raw, err := parseHTTPURL(prefix + "_URL")
	if err != nil {
		return Backend{}, err
	}
	timeout, err := loadTimeout(prefix+"_TIMEOUT", defaultBackendTimeout)
	if err != nil {
		return Backend{}, err
	}
	return Backend{
		BaseURL: raw,
		APIKey:  os.Getenv(prefix + "_API_KEY"),
		Timeout: timeout,
	}, nil
}

// loadTimeout parses a positive duration from key, falling back to def when unset.
func loadTimeout(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("config: %s is not a valid duration: %q", key, v)
	}
	if d <= 0 {
		return 0, fmt.Errorf("config: %s must be > 0, got %s", key, d)
	}
	return d, nil
}

// parseHTTPURL reads and validates an http(s) base URL from env key, returning
// the trailing-slash-trimmed value. It rejects a missing/invalid URL, a
// non-http(s) scheme, embedded userinfo, a query, or a fragment.
func parseHTTPURL(key string) (string, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return "", fmt.Errorf("config: %s is required", key)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("config: %s is not a valid absolute URL: %q", key, raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("config: %s scheme must be http or https, got %q", key, u.Scheme)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("config: %s must not contain a query or fragment: %q", key, raw)
	}
	if u.User != nil {
		return "", fmt.Errorf("config: %s must not embed userinfo", key)
	}
	return strings.TrimRight(raw, "/"), nil
}

func (c *Config) validate() error {
	if _, ok := validLogLevels[c.LogLevel]; !ok {
		return fmt.Errorf("config: LOG_LEVEL invalid: %q (want debug|info|warn|error)", c.LogLevel)
	}
	if _, ok := validLogFormats[c.LogFormat]; !ok {
		return fmt.Errorf("config: LOG_FORMAT invalid: %q (want json|text)", c.LogFormat)
	}
	if c.ListenAddr == "" {
		return errors.New("config: LISTEN_ADDR must not be empty")
	}
	return nil
}

func getenvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
