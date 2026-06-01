package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	type wantKubeGraph struct {
		vmURL        string
		prefix       string
		buildTimeout time.Duration
	}
	type wantBackend struct {
		baseURL string
		apiKey  string
		timeout time.Duration
	}
	tests := []struct {
		name          string
		env           map[string]string
		wantErrSubstr string
		wantListen    string
		wantLevel     string
		wantFormat    string
		wantKSG       wantKubeGraph
		wantSwitch    wantBackend
	}{
		{
			name:          "missing victoria metrics url",
			env:           map[string]string{"SWITCH_GRAPH_URL": "http://b:8080"},
			wantErrSubstr: "VICTORIA_METRICS_URL is required",
		},
		{
			name:          "missing switch url",
			env:           map[string]string{"VICTORIA_METRICS_URL": "http://a:8428"},
			wantErrSubstr: "SWITCH_GRAPH_URL is required",
		},
		{
			name: "invalid url no scheme",
			env: map[string]string{
				"VICTORIA_METRICS_URL": "not-a-url",
				"SWITCH_GRAPH_URL":     "http://b:8080",
			},
			wantErrSubstr: "VICTORIA_METRICS_URL is not a valid",
		},
		{
			name: "url with query is rejected",
			env: map[string]string{
				"VICTORIA_METRICS_URL": "http://a:8428?tenant=x",
				"SWITCH_GRAPH_URL":     "http://b:8080",
			},
			wantErrSubstr: "must not contain a query or fragment",
		},
		{
			name: "non-http scheme is rejected",
			env: map[string]string{
				"VICTORIA_METRICS_URL": "ftp://a:8428",
				"SWITCH_GRAPH_URL":     "http://b:8080",
			},
			wantErrSubstr: "scheme must be http or https",
		},
		{
			name: "userinfo in url is rejected",
			env: map[string]string{
				"VICTORIA_METRICS_URL": "http://user:pass@a:8428",
				"SWITCH_GRAPH_URL":     "http://b:8080",
			},
			wantErrSubstr: "must not embed userinfo",
		},
		{
			name: "non-positive build timeout is rejected",
			env: map[string]string{
				"VICTORIA_METRICS_URL": "http://a:8428",
				"KSG_BUILD_TIMEOUT":    "0s",
				"SWITCH_GRAPH_URL":     "http://b:8080",
			},
			wantErrSubstr: "must be > 0",
		},
		{
			name: "invalid build timeout",
			env: map[string]string{
				"VICTORIA_METRICS_URL": "http://a:8428",
				"KSG_BUILD_TIMEOUT":    "nope",
				"SWITCH_GRAPH_URL":     "http://b:8080",
			},
			wantErrSubstr: "KSG_BUILD_TIMEOUT is not a valid duration",
		},
		{
			name: "invalid log level",
			env: map[string]string{
				"VICTORIA_METRICS_URL": "http://a:8428",
				"SWITCH_GRAPH_URL":     "http://b:8080",
				"LOG_LEVEL":            "verbose",
			},
			wantErrSubstr: "LOG_LEVEL invalid",
		},
		{
			name: "defaults applied",
			env: map[string]string{
				"VICTORIA_METRICS_URL": "http://a:8428",
				"SWITCH_GRAPH_URL":     "http://b:8080",
			},
			wantListen: ":8080",
			wantLevel:  "info",
			wantFormat: "json",
			wantKSG:    wantKubeGraph{vmURL: "http://a:8428", buildTimeout: 15 * time.Second},
			wantSwitch: wantBackend{baseURL: "http://b:8080", timeout: 10 * time.Second},
		},
		{
			name: "happy path",
			env: map[string]string{
				"LISTEN_ADDR":          ":9090",
				"LOG_LEVEL":            "debug",
				"LOG_FORMAT":           "text",
				"VICTORIA_METRICS_URL": "http://a:8428/",
				"KSG_METRIC_PREFIX":    "tenantA_",
				"KSG_BUILD_TIMEOUT":    "20s",
				"SWITCH_GRAPH_URL":     "http://b:8080",
				"SWITCH_GRAPH_API_KEY": "key-b",
				"SWITCH_GRAPH_TIMEOUT": "7s",
			},
			wantListen: ":9090",
			wantLevel:  "debug",
			wantFormat: "text",
			wantKSG:    wantKubeGraph{vmURL: "http://a:8428", prefix: "tenantA_", buildTimeout: 20 * time.Second},
			wantSwitch: wantBackend{baseURL: "http://b:8080", apiKey: "key-b", timeout: 7 * time.Second},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			cfg, err := Load()
			if tc.wantErrSubstr != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil", tc.wantErrSubstr)
				}
				if !strings.Contains(err.Error(), tc.wantErrSubstr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErrSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.ListenAddr != tc.wantListen {
				t.Errorf("ListenAddr: want %q, got %q", tc.wantListen, cfg.ListenAddr)
			}
			if cfg.LogLevel != tc.wantLevel {
				t.Errorf("LogLevel: want %q, got %q", tc.wantLevel, cfg.LogLevel)
			}
			if cfg.LogFormat != tc.wantFormat {
				t.Errorf("LogFormat: want %q, got %q", tc.wantFormat, cfg.LogFormat)
			}
			if cfg.KSG.VictoriaMetricsURL != tc.wantKSG.vmURL {
				t.Errorf("KSG.VictoriaMetricsURL: want %q, got %q", tc.wantKSG.vmURL, cfg.KSG.VictoriaMetricsURL)
			}
			if cfg.KSG.MetricPrefix != tc.wantKSG.prefix {
				t.Errorf("KSG.MetricPrefix: want %q, got %q", tc.wantKSG.prefix, cfg.KSG.MetricPrefix)
			}
			if cfg.KSG.BuildTimeout != tc.wantKSG.buildTimeout {
				t.Errorf("KSG.BuildTimeout: want %s, got %s", tc.wantKSG.buildTimeout, cfg.KSG.BuildTimeout)
			}
			if cfg.Switch.BaseURL != tc.wantSwitch.baseURL {
				t.Errorf("Switch.BaseURL: want %q, got %q", tc.wantSwitch.baseURL, cfg.Switch.BaseURL)
			}
			if cfg.Switch.APIKey != tc.wantSwitch.apiKey {
				t.Errorf("Switch.APIKey: want %q, got %q", tc.wantSwitch.apiKey, cfg.Switch.APIKey)
			}
			if cfg.Switch.Timeout != tc.wantSwitch.timeout {
				t.Errorf("Switch.Timeout: want %s, got %s", tc.wantSwitch.timeout, cfg.Switch.Timeout)
			}
		})
	}
}

func TestLoad_APIKeys(t *testing.T) {
	base := map[string]string{
		"VICTORIA_METRICS_URL": "http://a:8428",
		"SWITCH_GRAPH_URL":     "http://b:8080",
	}
	setBase := func(t *testing.T) {
		clearEnv(t)
		for k, v := range base {
			t.Setenv(k, v)
		}
	}

	t.Run("disabled by default with 30s reload", func(t *testing.T) {
		setBase(t)
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.APIKeys != "" || cfg.APIKeysFile != "" {
			t.Errorf("auth should be disabled by default, got keys=%q file=%q", cfg.APIKeys, cfg.APIKeysFile)
		}
		if cfg.APIKeysReloadInterval != 30*time.Second {
			t.Errorf("default reload interval: want 30s, got %s", cfg.APIKeysReloadInterval)
		}
	})

	t.Run("csv keys", func(t *testing.T) {
		setBase(t)
		t.Setenv("API_KEYS", "k1,k2")
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.APIKeys != "k1,k2" {
			t.Errorf("APIKeys: want %q, got %q", "k1,k2", cfg.APIKeys)
		}
	})

	t.Run("file with reload disabled via 0", func(t *testing.T) {
		setBase(t)
		t.Setenv("API_KEYS_FILE", "/etc/keys/api-keys.txt")
		t.Setenv("API_KEYS_RELOAD_INTERVAL", "0")
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.APIKeysReloadInterval != 0 {
			t.Errorf("reload interval: want 0 (disabled), got %s", cfg.APIKeysReloadInterval)
		}
	})

	t.Run("negative reload interval is rejected", func(t *testing.T) {
		setBase(t)
		t.Setenv("API_KEYS_RELOAD_INTERVAL", "-5s")
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "must be >= 0") {
			t.Fatalf("want negative-interval error, got %v", err)
		}
	})
}

func clearEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"LISTEN_ADDR", "LOG_LEVEL", "LOG_FORMAT",
		"VICTORIA_METRICS_URL", "KSG_METRIC_PREFIX", "KSG_BUILD_TIMEOUT",
		"SWITCH_GRAPH_URL", "SWITCH_GRAPH_API_KEY", "SWITCH_GRAPH_TIMEOUT",
		"API_KEYS", "API_KEYS_FILE", "API_KEYS_RELOAD_INTERVAL",
	}
	for _, k := range keys {
		t.Setenv(k, "")
	}
}
