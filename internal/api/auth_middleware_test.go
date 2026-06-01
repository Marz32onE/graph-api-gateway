package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marz32one/graph-api-gateway/internal/auth"
	"github.com/marz32one/graph-api-gateway/internal/client"
	"github.com/marz32one/graph-api-gateway/internal/config"
)

// newAuthServer builds a Server whose backends are healthy stubs and whose
// inbound auth is configured with the supplied keys (none = auth disabled).
func newAuthServer(t *testing.T, keys ...string) *Server {
	t.Helper()
	primary := newStub(t, primaryWithIPs, 200, nil)
	swSrv := newStub(t, switchResponse, 200, nil)

	cfg := &config.Config{
		ListenAddr: ":0",
		LogLevel:   "debug",
		LogFormat:  "json",
		KSG:        config.Backend{BaseURL: primary.URL, Timeout: 2 * time.Second},
		Switch:     config.Backend{BaseURL: swSrv.URL, Timeout: 2 * time.Second},
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ks := auth.NewKeySet()
	if len(keys) > 0 {
		ks.LoadCSV(strings.Join(keys, ","))
	}
	pc := client.NewKubeStateGraphClient(primary.URL, "", cfg.KSG.Timeout)
	sc := client.NewSwitchGraphClient(swSrv.URL, "", cfg.Switch.Timeout)
	return New(cfg, logger, pc, sc, ks)
}

func doReq(s *Server, method, path, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if key != "" {
		req.Header.Set(APIKeyHeader, key)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	return w
}

func TestAuth_Disabled_AllRoutesPassThrough(t *testing.T) {
	s := newAuthServer(t) // no keys = auth disabled

	for _, path := range []string{"/livez", "/v1/graph"} {
		if w := doReq(s, http.MethodGet, path, ""); w.Code != http.StatusOK {
			t.Errorf("path %s: want 200 with auth disabled, got %d", path, w.Code)
		}
	}
}

func TestAuth_MissingHeader_Returns401(t *testing.T) {
	s := newAuthServer(t, "k1")

	w := doReq(s, http.MethodGet, "/v1/graph", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
	var er errorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &er); err != nil {
		t.Fatalf("decode body %q: %v", w.Body.String(), err)
	}
	if er.Error != "unauthorized" {
		t.Errorf("error: want unauthorized, got %q", er.Error)
	}
}

func TestAuth_WrongKey_Returns401(t *testing.T) {
	s := newAuthServer(t, "k1")
	if w := doReq(s, http.MethodGet, "/v1/graph", "wrong-key"); w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestAuth_ValidKey_Passes(t *testing.T) {
	s := newAuthServer(t, "k1", "k2")
	for _, key := range []string{"k1", "k2"} {
		if w := doReq(s, http.MethodGet, "/v1/graph", key); w.Code != http.StatusOK {
			t.Errorf("key %s: want 200, got %d (body=%s)", key, w.Code, w.Body.String())
		}
	}
}

func TestAuth_OpenPaths_BypassWithoutKey(t *testing.T) {
	s := newAuthServer(t, "k1")

	// All open paths must answer without a key and must NOT be 401.
	for _, path := range []string{"/livez", "/readyz", "/openapi.json", "/docs/"} {
		w := doReq(s, http.MethodGet, path, "")
		if w.Code == http.StatusUnauthorized {
			t.Errorf("open path %s should bypass auth, got 401", path)
		}
		if w.Code != http.StatusOK {
			t.Errorf("open path %s: want 200, got %d", path, w.Code)
		}
	}
}

func TestAuth_GraphRoute_RequiresKey(t *testing.T) {
	s := newAuthServer(t, "k1")
	if w := doReq(s, http.MethodGet, "/v1/graph?start=a&end=b", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}
