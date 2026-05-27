package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
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
		wantKSG       wantBackend
		wantSwitch    wantBackend
	}{
		{
			name:          "missing ksg url",
			env:           map[string]string{"SWITCH_GRAPH_URL": "http://b:8080"},
			wantErrSubstr: "KUBE_STATE_GRAPH_URL is required",
		},
		{
			name: "missing switch url",
			env: map[string]string{
				"KUBE_STATE_GRAPH_URL": "http://a:8080",
			},
			wantErrSubstr: "SWITCH_GRAPH_URL is required",
		},
		{
			name: "invalid url no scheme",
			env: map[string]string{
				"KUBE_STATE_GRAPH_URL": "not-a-url",
				"SWITCH_GRAPH_URL":     "http://b:8080",
			},
			wantErrSubstr: "KUBE_STATE_GRAPH_URL is not a valid",
		},
		{
			name: "url with query is rejected",
			env: map[string]string{
				"KUBE_STATE_GRAPH_URL": "http://a:8080?tenant=x",
				"SWITCH_GRAPH_URL":     "http://b:8080",
			},
			wantErrSubstr: "must not contain a query or fragment",
		},
		{
			name: "url with fragment is rejected",
			env: map[string]string{
				"KUBE_STATE_GRAPH_URL": "http://a:8080",
				"SWITCH_GRAPH_URL":     "http://b:8080/#frag",
			},
			wantErrSubstr: "must not contain a query or fragment",
		},
		{
			name: "invalid timeout",
			env: map[string]string{
				"KUBE_STATE_GRAPH_URL":     "http://a:8080",
				"KUBE_STATE_GRAPH_TIMEOUT": "nope",
				"SWITCH_GRAPH_URL":         "http://b:8080",
			},
			wantErrSubstr: "KUBE_STATE_GRAPH_TIMEOUT is not a valid duration",
		},
		{
			name: "invalid log level",
			env: map[string]string{
				"KUBE_STATE_GRAPH_URL": "http://a:8080",
				"SWITCH_GRAPH_URL":     "http://b:8080",
				"LOG_LEVEL":            "verbose",
			},
			wantErrSubstr: "LOG_LEVEL invalid",
		},
		{
			name: "invalid log format",
			env: map[string]string{
				"KUBE_STATE_GRAPH_URL": "http://a:8080",
				"SWITCH_GRAPH_URL":     "http://b:8080",
				"LOG_FORMAT":           "xml",
			},
			wantErrSubstr: "LOG_FORMAT invalid",
		},
		{
			name: "defaults applied",
			env: map[string]string{
				"KUBE_STATE_GRAPH_URL": "http://a:8080",
				"SWITCH_GRAPH_URL":     "http://b:8080",
			},
			wantListen: ":8080",
			wantLevel:  "info",
			wantFormat: "json",
			wantKSG:    wantBackend{baseURL: "http://a:8080", apiKey: "", timeout: 10 * time.Second},
			wantSwitch: wantBackend{baseURL: "http://b:8080", apiKey: "", timeout: 10 * time.Second},
		},
		{
			name: "happy path",
			env: map[string]string{
				"LISTEN_ADDR":              ":9090",
				"LOG_LEVEL":                "debug",
				"LOG_FORMAT":               "text",
				"KUBE_STATE_GRAPH_URL":     "http://a:8080/",
				"KUBE_STATE_GRAPH_API_KEY": "key-a",
				"KUBE_STATE_GRAPH_TIMEOUT": "3s",
				"SWITCH_GRAPH_URL":         "http://b:8080",
				"SWITCH_GRAPH_API_KEY":     "key-b",
				"SWITCH_GRAPH_TIMEOUT":     "7s",
			},
			wantListen: ":9090",
			wantLevel:  "debug",
			wantFormat: "text",
			wantKSG:    wantBackend{baseURL: "http://a:8080", apiKey: "key-a", timeout: 3 * time.Second},
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
			assertBackend(t, "KSG", cfg.KSG, tc.wantKSG)
			assertBackend(t, "Switch", cfg.Switch, tc.wantSwitch)
		})
	}
}

func assertBackend(t *testing.T, name string, got Backend, want struct {
	baseURL string
	apiKey  string
	timeout time.Duration
}) {
	t.Helper()
	if got.BaseURL != want.baseURL {
		t.Errorf("%s.BaseURL: want %q, got %q", name, want.baseURL, got.BaseURL)
	}
	if got.APIKey != want.apiKey {
		t.Errorf("%s.APIKey: want %q, got %q", name, want.apiKey, got.APIKey)
	}
	if got.Timeout != want.timeout {
		t.Errorf("%s.Timeout: want %s, got %s", name, want.timeout, got.Timeout)
	}
}

func clearEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"LISTEN_ADDR", "LOG_LEVEL", "LOG_FORMAT",
		"KUBE_STATE_GRAPH_URL", "KUBE_STATE_GRAPH_API_KEY", "KUBE_STATE_GRAPH_TIMEOUT",
		"SWITCH_GRAPH_URL", "SWITCH_GRAPH_API_KEY", "SWITCH_GRAPH_TIMEOUT",
	}
	for _, k := range keys {
		t.Setenv(k, "")
	}
}
