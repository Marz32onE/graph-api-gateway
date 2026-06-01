package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDocsUI_ServesIndex(t *testing.T) {
	s := newTestServer(t, "http://unused", "http://unused", io.Discard)
	req := httptest.NewRequest(http.MethodGet, "/docs/", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /docs/: want 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "swagger-ui") {
		t.Errorf("index.html should reference swagger-ui bundle, got: %s", w.Body.String())
	}
}

func TestDocsUI_InitializerPointsAtOpenAPI(t *testing.T) {
	s := newTestServer(t, "http://unused", "http://unused", io.Discard)
	req := httptest.NewRequest(http.MethodGet, "/docs/swagger-initializer.js", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `url: "/openapi.json"`) {
		t.Errorf("initializer must point the UI at /openapi.json, got: %s", w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("content-type: want javascript, got %q", ct)
	}
}

func TestDocsUI_ServesEmbeddedAsset(t *testing.T) {
	s := newTestServer(t, "http://unused", "http://unused", io.Discard)
	req := httptest.NewRequest(http.MethodGet, "/docs/swagger-ui.css", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET embedded asset: want 200, got %d", w.Code)
	}
}

func TestOpenAPIRoutes_ServeEmbeddedSpec(t *testing.T) {
	s := newTestServer(t, "http://unused", "http://unused", io.Discard)
	for _, p := range []string{"/openapi.json", "/openapi.yaml"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("%s: want 200, got %d", p, w.Code)
		}
	}
}
