## 1. Project scaffolding

- [x] 1.1 Initialise `go.mod` with module path `github.com/marz32one/graph-api-gateway` and Go 1.22+
- [x] 1.2 Create directory layout: `cmd/graph-api-gateway/`, `internal/{api,client,merge,config,observability,build}/`, `internal/api/static/{openapi,scalar}/`, `docs/`, `tools/openapi-postprocess/`, `deploy/docker/`, `local/`, `scripts/`
- [x] 1.3 Add core dependencies: `github.com/gin-gonic/gin`, `github.com/go-resty/resty/v2`, `go.opentelemetry.io/otel`, `go.opentelemetry.io/otel/sdk`, `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp`, `go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin`, `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp`, `github.com/google/uuid` (no `errgroup` — sequential pipeline per D5)
- [x] 1.4 Add `github.com/swaggo/swag/v2` as a tool dependency (`go.mod` `tool` directive), matching the version used by kube-state-graph
- [x] 1.5 Add baseline `Makefile` targets: `build`, `test`, `vet`, `lint`, `docs`, `check-docs`, `refresh-docs-ui`, `docker-build`, `docker-docs`
- [x] 1.6 Add `.golangci.yml`, `.editorconfig`, `.dockerignore`, `.gitignore` (mirror kube-state-graph defaults)
- [x] 1.7 Add `internal/build/build.go` with `Version` and `Commit` vars set via `-ldflags`

## 2. Config

- [x] 2.1 Implement `internal/config/config.go` defining `Config` struct with fields for listen addr, log level/format, `KSG`/`Switch` backend URL+API key+timeout (domain-named per D8)
- [x] 2.2 Implement `Load()` that reads from env, applies defaults (`LISTEN_ADDR=:8080`, `LOG_LEVEL=info`, `LOG_FORMAT=json`, backend timeouts `10s`)
- [x] 2.3 Implement startup validation: both `KUBE_STATE_GRAPH_URL` and `SWITCH_GRAPH_URL` required and must parse as valid http(s) URLs without userinfo/query/fragment; fail with clear error naming the missing/invalid key
- [x] 2.4 Table-driven test covering: missing required vars, invalid URLs, defaults applied, full happy path

## 3. Observability foundations

- [x] 3.1 Implement `internal/observability/tracing.go` with `SetupTracing(ctx, cfg)` that returns a shutdown func; install OTLP/HTTP exporter when `OTEL_EXPORTER_OTLP_ENDPOINT` is set, else `noop.NewTracerProvider()`
- [x] 3.2 Set resource attrs `service.name=graph-api-gateway`, `service.version=<build.Version>`
- [x] 3.3 Implement `internal/observability/logging.go` with `NewLogger(cfg)` returning `*slog.Logger`; wrap `slog.NewJSONHandler` (or `NewTextHandler` when `LOG_FORMAT=text`) with a custom handler that injects `trace_id` / `span_id` from `trace.SpanContextFromContext` when the span context is valid
- [x] 3.4 Unit test: log emitted inside an OTel span carries `trace_id` / `span_id`; log outside a span does not
- [x] 3.5 Unit test: tracing setup is a no-op (no exporter created) when `OTEL_EXPORTER_OTLP_ENDPOINT` is empty

## 4. Backend clients

- [x] 4.1 Define types in `internal/client/backend.go`: `GraphBackend` interface, `GraphQuery`, `CytoscapeGraph` / `Elements` / `Node` / `Edge`. **Add `IPAddress []string` (`json:"ipaddress,omitempty"`) to `NodeData`** so kube-state-graph's extended contract decodes losslessly.
- [x] 4.2 Implement `internal/client/transport.go` with `NewHTTPClient(timeout)` that returns an `*http.Client` whose transport is `otelhttp.NewTransport(http.DefaultTransport, ...)`
- [x] 4.3 Implement `internal/client/kubestategraph.go` (`KubeStateGraphClient` struct): constructor takes base URL, API key, timeout; uses a `*resty.Client` built from the otel-instrumented http client; identity surfaces via concrete struct type + per-stage log fields (no `Name()` method on the interface — see D2); `FetchGraph` issues `GET <baseURL>/v1/graph?<rawQuery>`, sets `X-API-Key` header when configured, decodes into `CytoscapeGraph`
- [x] 4.4 Rename `internal/client/secondary.go` → `internal/client/switch.go`; rename type `SecondaryGraphClient` → `SwitchGraphClient` and constructor accordingly (identity continues to flow through the concrete struct type, no `Name()` method)
- [x] 4.5 Compile-time interface assertions: `var _ GraphBackend = (*KubeStateGraphClient)(nil)` and same for `SwitchGraphClient`
- [x] 4.6 Unit test against `httptest.Server`: API key forwarded / omitted, valid payload decodes, **`ipaddress` array decodes onto `NodeData`** (and missing `ipaddress` decodes as nil), malformed JSON returns wrapped error, missing `elements` decodes as empty slices, timeout triggers deadline-exceeded error, `traceparent` propagated when a parent span exists, `SwitchGraphClient.FetchGraphByIPs` POSTs the batched `[{"ip":…}]` body with `Content-Type: application/json`

## 5. Merge function

- [x] 5.1 Implement `internal/merge/merge.go` `Merge(graphs ...*CytoscapeGraph) *CytoscapeGraph` — pure, no I/O, returns fresh struct with `apiVersion: "v1"`
- [x] 5.2 Node merge: keyed by `data.id`, first-writer-wins on collision
- [x] 5.3 Edge merge: keyed by `(type, source, target)` triple, first-writer-wins on collision
- [x] 5.4 Table-driven unit tests: disjoint inputs union, duplicate node ids resolve to first writer, duplicate edge triples collapse, distinct triples are kept, inputs are not mutated (deep-compare before/after), nil inputs are skipped

## 6. HTTP gateway

- [x] 6.1 Implement `internal/api/server.go`: `Server` struct holding `*gin.Engine`, `*slog.Logger`, `[]GraphBackend`, config; constructor wires routes and middleware
- [x] 6.2 Implement `internal/api/middleware.go`: `requestIDMiddleware` (read or generate `X-Request-ID` via `uuid.NewString`, set in gin context under key `request_id`, echo as response header), `loggingMiddleware` (one slog `InfoContext` per request with `method`, `path`, `status`, `duration_ms`, `request_id`; quiet on `2xx` for `/livez` `/readyz`)
- [x] 6.3 Register middleware order: `gin.Recovery()` → `otelgin.Middleware("graph-api-gateway")` → `requestIDMiddleware` (also decorates the active span with `http.request_id`) → `loggingMiddleware`
- [x] 6.4 Implement `internal/api/health.go`: `GET /livez` returns `200 ok`; `GET /readyz` probes both backends (`GET <baseURL>/livez` with a short timeout) and returns `200 ok` only if both are reachable, else `503 not ready: <backend>`
- [x] 6.5 **Rewrite** `internal/api/handlers.go` `GET /v1/graph` as a sequential pipeline: (1) call primary with `context.WithTimeout(cfg.KSG.Timeout)` forwarding `c.Request.URL.RawQuery`; (2) `ips := pipeline.ExtractIPs(primary)` (node-only); (3) if `len(ips) > 0`, call switch with `context.WithTimeout(cfg.Switch.Timeout)` using `s.switchClient.FetchGraphByIPs(ips)`; (4) `reconciled := pipeline.ReconcileSwitch(primary, switchOrNil)`; (5) `merge.Merge(primary, reconciled)`; (6) on stage error: `context.DeadlineExceeded` → `504 {"error":"upstream_timeout"}`, client-cancelled inbound → `499` no body, anything else → `502 {"error":"upstream_unavailable"}`
- [x] 6.6 Add `swag` annotations to `handlers.go` (`@Summary`, `@Description`, `@Param start`, `@Param end`, `@Param cluster`, …, `@Success`, `@Failure`, `@Router /v1/graph [get]`) and to health handlers _(update `@Description` to describe the sequential pipeline + IP-extract + switch stage)_
- [x] 6.7 Add top-level `// @title`, `// @version`, `// @BasePath /`, `// @description` annotations above `main()`
- [x] 6.8 **Rewrite** integration test using `httptest` and two stub backends to cover: primary+switch both succeed → merged 200; primary node with `ipaddress` → switch receives exact deduped IPs as a `[{"ip":…}]` POST body; **switch shadow with matching `ipaddress` collapses onto kube node ID in the merged output, edges point at kube ID**; primary returns zero `ipaddress` entries → switch NOT called; primary fails → 502 + switch NOT called; switch fails → 502; inbound query forwarded to primary verbatim and NOT to switch; `traceparent` chain holds inbound → primary → switch; `X-Request-ID` echoed and logged

## 7. OpenAPI docs pipeline

- [x] 7.1 Port `tools/openapi-postprocess/main.go` from kube-state-graph verbatim (rewrites parameter-level `example` into `schema.example` for Scalar)
- [x] 7.2 Implement `internal/api/docs.go` with handlers and `swag` annotations: `GET /openapi.yaml`, `GET /openapi.json`, `GET /docs` (HTML shell loading Scalar), `GET /docs/assets/*path` — all served from embedded `//go:embed static/openapi/* static/scalar/*`
- [x] 7.3 Implement `scripts/refresh-docs-ui.sh` that vendors the Scalar API Reference bundle into `internal/api/static/scalar/`
- [x] 7.4 Wire `make docs`: runs `go tool swag init -g cmd/graph-api-gateway/main.go --output docs --parseDependency --parseInternal --v3.1=true`, then `go run ./tools/openapi-postprocess docs/swagger.json docs/swagger.yaml`, then copies the YAML/JSON into `internal/api/static/openapi/`
- [x] 7.5 Wire `make check-docs`: runs `make docs`, then `git diff --quiet -- docs/ internal/api/static/openapi/` (fails CI if generated spec drifts from committed)
- [x] 7.6 Run `make docs` once and commit the generated `docs/openapi.{yaml,json}` plus the embedded copies

## 8. Entrypoint and packaging

- [x] 8.1 Implement `cmd/graph-api-gateway/main.go`: load config → setup logging → setup tracing (deferred shutdown) → construct backend clients → construct server → start with `http.Server` and `signal.NotifyContext(ctx, SIGINT, SIGTERM)` for graceful `Shutdown`
- [x] 8.2 Add `deploy/docker/Dockerfile` (multi-stage: golang builder → distroless runtime), build via `make docker-build`
- [x] 8.3 Add `local/docker-compose.yaml`: gateway + two stub graph backends (using a minimal Go stub or echo image) + an OpenTelemetry Collector
- [x] 8.4 Add `Makefile docker-docs` target: runs the gateway container with placeholder backend URLs so `/docs`, `/openapi.{yaml,json}` are reachable for spec review

## 9. Verification

- [x] 9.1 `go test ./...` passes — 59 tests in 10 packages, race-clean
- [x] 9.2 `go vet ./...` clean
- [x] 9.3 `golangci-lint run` clean (0 issues)
- [x] 9.4 `make check-docs` regenerated cleanly
- [x] 9.5 Local smoke: gateway against primary stub (kube node with `ipaddress:["10.0.0.1"]` + pod + pod-runs-on-node edge) and switch stub (`sw-host:xyz` shadow on 10.0.0.1 + `switch:tor-1` + host-attached edge) → merged `/v1/graph` correctly drops `sw-host:xyz` and rewrites the host-attached edge to `prod/abc → switch:tor-1`; switch stub received a `POST /v1/graph` `[{"ip":"10.0.0.1"}]` body; access log JSON carries `request_id`.

## 10. Sequential pipeline integration (switch backend)

- [x] 10.1 Add `internal/pipeline/extract.go` exporting `ExtractIPs(*client.CytoscapeGraph) []string`: walk `Elements.Nodes`, include **only** entries where `Data.Type == "node"`, append every value from `Data.IPAddress` into a deduped slice preserving insertion order; non-mutating; returns non-nil empty slice on nil input
- [x] 10.2 Table-driven test for `ExtractIPs`: nil input → empty; node-only collection (pods/PVCs/external excluded even with `ipaddress`); duplicates across multiple nodes collapse; entry with nil/empty `ipaddress` skipped; insertion order preserved
- [x] 10.3 Add `internal/pipeline/reconcile.go` exporting `ReconcileSwitch(primary, switchGraph *client.CytoscapeGraph) *client.CytoscapeGraph` per D9: build `ipToNodeID` from primary `node`-type entries; map switch shadow IDs → kube IDs via shared `ipaddress`; return new graph with collapsed shadow nodes dropped and edge endpoints rewritten; value-copy nodes/edges (no aliasing); return `nil` when `switchGraph == nil`
- [x] 10.4 Table-driven test for `ReconcileSwitch`: shadow collapses onto kube node; switch-only nodes (no `ipaddress`) preserved; foreign-IP shadow stays as orphan with original ID; multi-IP kube node still matches via any of its IPs; edge endpoints rewritten when source/target matches a shadow; inputs deep-compare unchanged after the call; nil switch returns nil
- [x] 10.5 Switch IP batching lives in `SwitchGraphClient.FetchGraphByIPs(ips)`: build a `[]{ip}` slice and POST it as a single JSON body to `/v1/graph`; unit test asserts method `POST`, `Content-Type: application/json`, and body `[{"ip":"10.0.0.1"},{"ip":"10.0.0.2"}]`
- [x] 10.6 Rename `internal/client/secondary.go` → `internal/client/switch.go`; rename struct/constructor from `secondary` to `switch`; update package-level docs (no `Name()` method — identity via concrete struct type)
- [x] 10.7 Rename env keys per D8: `BACKEND_SECONDARY_URL` → `SWITCH_GRAPH_URL`, `BACKEND_SECONDARY_API_KEY` → `SWITCH_GRAPH_API_KEY`, `BACKEND_SECONDARY_TIMEOUT` → `SWITCH_GRAPH_TIMEOUT`; `BACKEND_PRIMARY_*` → `KUBE_STATE_GRAPH_*`; rename `Config.Secondary` → `Config.Switch` and `Config.Primary` → `Config.KSG`; update `config_test.go` table cases
- [x] 10.8 Update `cmd/graph-api-gateway/main.go` wiring: construct `KubeStateGraphClient` and `SwitchGraphClient`, pass to `api.New` as typed dependencies (drop the `[]GraphBackend` slice signature)
- [x] 10.9 Update `internal/api/server.go` constructor: replace `backends []client.GraphBackend` with explicit `primary *client.KubeStateGraphClient, switchClient *client.SwitchGraphClient`; inline per-backend timeout lookup
- [x] 10.10 Update `local/docker-compose.yaml`: rename `stub-secondary` → `stub-switch`; primary stub payload includes a node with `"ipaddress": ["10.0.0.1"]`; switch stub returns a shadow `{id:"sw-host:x", ipaddress:["10.0.0.1"]}` + switch chassis + edge, so the smoke verifies reconciliation end-to-end
- [x] 10.11 Re-run `make docs` and commit regenerated `docs/swagger.{yaml,json}` and embedded copies
