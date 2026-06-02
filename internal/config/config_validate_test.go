package config

import (
	"strings"
	"testing"
)

// TestLoad_SwitchTimeoutAndReloadAndFormat pins the Load() error branches not
// already covered by TestLoad: the switch backend timeout parse/positivity
// checks, the reload-interval parse-error branch, and the LOG_FORMAT validate
// branch. clearEnv + t.Setenv mirror config_test.go.
func TestLoad_SwitchTimeoutAndReloadAndFormat(t *testing.T) {
	base := map[string]string{
		"VICTORIA_METRICS_URL": "http://a:8428",
		"SWITCH_GRAPH_URL":     "http://b:8080",
	}
	tests := []struct {
		name          string
		extra         map[string]string
		wantErrSubstr string
	}{
		{
			name:          "switch timeout not a valid duration",
			extra:         map[string]string{"SWITCH_GRAPH_TIMEOUT": "nope"},
			wantErrSubstr: "SWITCH_GRAPH_TIMEOUT is not a valid duration",
		},
		{
			name:          "switch timeout non-positive is rejected",
			extra:         map[string]string{"SWITCH_GRAPH_TIMEOUT": "0s"},
			wantErrSubstr: "must be > 0",
		},
		{
			name:          "reload interval unparseable",
			extra:         map[string]string{"API_KEYS_RELOAD_INTERVAL": "nope"},
			wantErrSubstr: "is not a valid duration",
		},
		{
			name:          "log format invalid",
			extra:         map[string]string{"LOG_FORMAT": "yaml"},
			wantErrSubstr: "LOG_FORMAT invalid",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			for k, v := range base {
				t.Setenv(k, v)
			}
			for k, v := range tc.extra {
				t.Setenv(k, v)
			}

			cfg, err := Load()
			if err == nil {
				t.Fatalf("want error containing %q, got nil (cfg=%+v)", tc.wantErrSubstr, cfg)
			}
			if cfg != nil {
				t.Errorf("want nil cfg on error, got %+v", cfg)
			}
			if !strings.Contains(err.Error(), tc.wantErrSubstr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErrSubstr)
			}
		})
	}
}

// TestConfigValidate_DirectBranches exercises (*Config).validate() for the two
// failures that Load() can never surface: an empty ListenAddr (Load always
// defaults it to :8080) and an invalid LogFormat reached after a valid LogLevel.
// validate is unexported and same-package, so we call it directly on a
// hand-built Config rather than going through env loading.
func TestConfigValidate_DirectBranches(t *testing.T) {
	tests := []struct {
		name          string
		cfg           Config
		wantErrSubstr string
	}{
		{
			name:          "empty listen addr rejected",
			cfg:           Config{LogLevel: "info", LogFormat: "json", ListenAddr: ""},
			wantErrSubstr: "LISTEN_ADDR must not be empty",
		},
		{
			name:          "invalid log format rejected after valid level",
			cfg:           Config{LogLevel: "info", LogFormat: "bogus", ListenAddr: ":8080"},
			wantErrSubstr: "LOG_FORMAT invalid",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := tc.cfg
			err := c.validate()
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantErrSubstr)
			}
			if !strings.Contains(err.Error(), tc.wantErrSubstr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErrSubstr)
			}
		})
	}
}

// TestConfigValidate_AcceptsValidConfig pins the success path of validate: a
// fully-valid Config returns nil so the negative branches above are meaningful.
func TestConfigValidate_AcceptsValidConfig(t *testing.T) {
	c := Config{LogLevel: "warn", LogFormat: "text", ListenAddr: ":9090"}
	if err := c.validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}
