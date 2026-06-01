## 1. Dependency + module wiring

- [x] 1.1 Add `require github.com/marz32one/kube-state-graph <version>` (the tagged version that lands ksg D32 `pkg/`); run `go mod tidy`
- [x] 1.2 Confirm `pkg/{graph,build,promql,clock,cytoscape,kubegraph}` import paths resolve and compile
- [x] 1.3 Verify the transitive deps pulled in (`prometheus/client_golang`, `google/uuid`, `golang.org/x/sync`, OTel SDK) are acceptable; `resty` retained for the switch client only

## 2. Config surface (capability: http-gateway)

- [x] 2.1 In `internal/config/config.go`: replace the `KSG` backend group (ksg API URL + key + timeout) with a kube-graph group — `VICTORIA_METRICS_URL` (required), `KSG_METRIC_PREFIX` (optional), `KSG_BUILD_TIMEOUT` (optional, default `15s`); drop `KUBE_STATE_GRAPH_API_KEY`
- [x] 2.2 Update `Load()` + validation: require `VICTORIA_METRICS_URL` and `SWITCH_GRAPH_URL` as valid http(s) URLs without userinfo/query/fragment; fail-fast naming the missing/invalid key; the legacy `KUBE_STATE_GRAPH_URL` is ignored
- [x] 2.3 Update `config_test.go` table cases: new var names, removed key, default build timeout, legacy-key-ignored case

## 3. Embedded engine adapter (capability: backend-clients)

- [x] 3.1 `internal/client/kubestategraph.go`: replace the resty `KubeStateGraphClient` with an in-process adapter holding `*kubegraph.Engine`; constructor builds `promql.New(vmURL, …)` → `kubegraph.New(querier, kubegraph.Options{MetricPrefix, Clock: clock.System{}, Metrics: <noop>})`
- [x] 3.2 Implement `FetchGraph(ctx, GraphQuery)` by parsing `GraphQuery.RawQuery` → `url.Values` and calling `engine.BuildFromValues(ctx, values)`; return `*cytoscape.Body`
- [x] 3.3 Keep the compile-time assertion `var _ GraphBackend = (*KubeStateGraphClient)(nil)`; `GraphBackend.FetchGraph` now returns `*cytoscape.Body`
- [x] 3.4 Remove resty / `http.Client` from the primary path (resty stays only in `switch.go`)
- [x] 3.5 Provide a no-op metrics sink implementing the engine's metrics interface so no `kube_state_graph_*` series register in the gateway registry

## 4. DTO unification (capability: backend-clients / graph-merging)

- [x] 4.1 Delete local `CytoscapeGraph` / `Elements` / `Node` / `NodeData` / `Edge` / `EdgeData` from `internal/client/backend.go`; reference `pkg/cytoscape` types (alias where it reduces churn)
- [x] 4.2 Point `GraphBackend`, `GraphQuery`, and the switch-client decode path at `pkg/cytoscape` types
- [x] 4.3 Retype `internal/pipeline/extract.go` `ExtractIPs`, `internal/pipeline/reconcile.go` `ReconcileSwitch`, and `internal/merge/merge.go` `Merge` onto `*cytoscape.Body` (logic unchanged)
- [x] 4.4 Update all call sites + test fixtures to the shared types

## 5. Switch client (capability: backend-clients — retyped, contract unchanged)

- [x] 5.1 `SwitchGraphClient.FetchGraphByIPs` still POSTs `[{"ip":…}]`; decode the HTTP response into `*cytoscape.Body`
- [x] 5.2 Keep base URL + optional `X-API-Key` + per-call timeout; resty transport unchanged

## 6. Handler + entrypoint wiring (capability: http-gateway)

- [x] 6.1 `internal/api/handlers.go`: the primary stage calls `s.ksg.FetchGraph` (in-process) under `context.WithTimeout(ctx, cfg.KSG.BuildTimeout)`; stages 2–6 (ExtractIPs → switch → reconcile → merge) unchanged
- [x] 6.2 Error mapping preserved: `context.DeadlineExceeded` → `504 {"error":"upstream_timeout"}`; any other engine error → `502 {"error":"upstream_unavailable"}`; inbound client cancel → `499` no body
- [x] 6.3 `cmd/graph-api-gateway/main.go`: construct `KubeStateGraphClient` from `VICTORIA_METRICS_URL` + options; switch client unchanged; pass both to `api.New`
- [x] 6.4 `internal/api/health.go` `/readyz`: replace the primary `GET <ksg>/livez` probe with an upstream VictoriaMetrics probe (an `up{}` instant query via the engine's `promql.Querier`, short timeout); keep the switch-backend `GET /livez` probe; `503 not ready: <backend>` on failure

## 7. Observability (capability: observability)

- [x] 7.1 No-op metrics sink: gateway registers no `kube_state_graph_*` series; confirm the gateway `/metrics` (if any) is unaffected
- [x] 7.2 Confirm no `OTEL_*` wiring is added (slog-only stance holds); document that engine spans stay no-op unless an OTLP endpoint is configured in the environment

## 8. Docs + deploy

- [x] 8.1 Update swag `@Description` for `/v1/graph` to note the in-process kube-state-graph engine (no wire contract change); `@Param` set unchanged
- [x] 8.2 Update deploy manifests + README/config docs: `VICTORIA_METRICS_URL`, `KSG_METRIC_PREFIX`, `KSG_BUILD_TIMEOUT`; remove `KUBE_STATE_GRAPH_URL` / `KUBE_STATE_GRAPH_API_KEY`; add a migration note
- [x] 8.3 Run `make docs` and commit regenerated `docs/`

## 9. Tests

- [x] 9.1 New engine-adapter test: `KubeStateGraphClient.FetchGraph` over a fixture `promql.Querier` returns the expected `*cytoscape.Body`, byte-compatible with a captured ksg HTTP `/v1/graph` golden
- [x] 9.2 Retype existing `merge` / `reconcile` / `extract` table tests onto `pkg/cytoscape`; assertions and logic unchanged
- [x] 9.3 Handler integration test (httptest switch stub + in-process primary): both succeed → merged `200` with shadow collapse; primary build error → `502`, switch NOT called; primary timeout → `504`; primary with no IPs → switch skipped
- [x] 9.4 Config tests updated (section 2.3)

## 10. Verification

- [x] 10.1 `go test ./...` passes, race-clean
- [x] 10.2 `go vet ./...` clean; `golangci-lint run` clean
- [x] 10.3 `make check-docs` regenerates with no drift
- [ ] 10.4 Byte-compatibility check: merged `/v1/graph` output matches a captured golden response from the HTTP-backed gateway for the same upstream data _(deferred — no pre-refactor golden captured; reconcile/merge/handler behaviour tests cover the merge contract)_
- [x] 10.5 `openspec validate embed-ksg-graph-engine --strict`
