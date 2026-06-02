package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// renderOpenAPISpec must emit a valid OpenAPI 3.x JSON document with the legacy
// Swagger-2.0 "schemes" root field stripped (swag v2 emits it as null, which is
// invalid under OpenAPI 3.1).
func TestRenderOpenAPISpec(t *testing.T) {
	out := renderOpenAPISpec()

	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("rendered spec is not valid JSON: %v\n%s", err, out)
	}
	if _, ok := doc["schemes"]; ok {
		t.Errorf("schemes key must be stripped from OpenAPI 3.x doc, but is present")
	}
	if _, ok := doc["openapi"]; !ok {
		t.Errorf("rendered spec missing the openapi field; keys present: %v", keysOf(doc))
	}
}

// openAPISpec is rendered once at init from the same path; it must be non-empty
// and itself parse as valid JSON.
func TestOpenAPISpecVarIsValidJSON(t *testing.T) {
	if len(openAPISpec) == 0 {
		t.Fatal("openAPISpec is empty")
	}
	var doc map[string]any
	if err := json.Unmarshal(openAPISpec, &doc); err != nil {
		t.Fatalf("openAPISpec is not valid JSON: %v", err)
	}
	if _, ok := doc["openapi"]; !ok {
		t.Errorf("openAPISpec missing the openapi field; keys present: %v", keysOf(doc))
	}
}

// The /openapi.json route is open (no auth) and serves the compiled spec as a
// JSON document.
func TestHandleOpenAPIJSON_ServesSpec(t *testing.T) {
	swSrv := newStub(t, switchResponse, 200, nil)
	s := newTestServer(t, &fakeKSG{body: mustBody(t, primaryWithIPs)}, swSrv.URL, io.Discard)

	req := httptest.NewRequest("GET", "/openapi.json", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d, body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type: want application/json, got %q", ct)
	}
	var doc map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("/openapi.json body is not valid JSON: %v\n%s", err, w.Body.String())
	}
}

func keysOf(m map[string]any) []string {
	t := make([]string, 0, len(m))
	for k := range m {
		t = append(t, k)
	}
	return t
}
