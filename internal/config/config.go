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
	KSG        Backend
	Switch     Backend
}

// Backend is a single upstream graph backend.
type Backend struct {
	BaseURL string
	APIKey  string
	Timeout time.Duration
}

const (
	defaultListenAddr     = ":8080"
	defaultLogLevel       = "info"
	defaultLogFormat      = "json"
	defaultBackendTimeout = 10 * time.Second
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

	ksg, err := loadBackend("KUBE_STATE_GRAPH")
	if err != nil {
		return nil, err
	}
	cfg.KSG = ksg

	switchBackend, err := loadBackend("SWITCH_GRAPH")
	if err != nil {
		return nil, err
	}
	cfg.Switch = switchBackend

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func loadBackend(prefix string) (Backend, error) {
	urlKey := prefix + "_URL"
	keyKey := prefix + "_API_KEY"
	toKey := prefix + "_TIMEOUT"

	raw := os.Getenv(urlKey)
	if raw == "" {
		return Backend{}, fmt.Errorf("config: %s is required", urlKey)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return Backend{}, fmt.Errorf("config: %s is not a valid absolute URL: %q", urlKey, raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return Backend{}, fmt.Errorf("config: %s scheme must be http or https, got %q", urlKey, u.Scheme)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return Backend{}, fmt.Errorf("config: %s must not contain a query or fragment: %q", urlKey, raw)
	}
	if u.User != nil {
		return Backend{}, fmt.Errorf("config: %s must not embed userinfo; use %s instead", urlKey, keyKey)
	}

	timeout := defaultBackendTimeout
	if v := os.Getenv(toKey); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Backend{}, fmt.Errorf("config: %s is not a valid duration: %q", toKey, v)
		}
		if d <= 0 {
			return Backend{}, fmt.Errorf("config: %s must be > 0, got %s", toKey, d)
		}
		timeout = d
	}

	return Backend{
		BaseURL: strings.TrimRight(raw, "/"),
		APIKey:  os.Getenv(keyKey),
		Timeout: timeout,
	}, nil
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
