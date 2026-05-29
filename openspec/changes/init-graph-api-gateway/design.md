## Context

`graph-api-gateway` is a greenfield Go HTTP service that orchestrates a sequential pipeline:

1. Forward the inbound query to **kube-state-graph** (primary) and parse the Cytoscape.js response.
2. Walk every `node`-type entry, collect `data.ipaddress[]` into a deduped list. (Pods are not queried — only K8s node IPs go to switch.)
3. If any IPs were collected, issue one batched call to the **switch backend** as `POST /v1/graph` with a JSON body `[{"ip":…},…]` (every IP in one request) and parse its Cytoscape response.
4. **Reconcile** switch IDs onto kube IDs: switch backend uses its own ID space (e.g., `sw-host:xyz`); the gateway matches switch nodes to kube nodes by shared `data.ipaddress`, drops the switch-side shadows, and rewrites switch edges to point at the canonical kube node IDs.
5. Merge primary + reconciled switch into a single envelope (union nodes by `data.id` with kube winning, dedup edges by `(type, source, target)`).
6. Return the merged result.

It must propagate W3C trace context end-to-end (otelgin inbound + otelhttp outbound under resty), structure logs with `log/slog`, and ship machine-readable API docs (OpenAPI 3.1 via `swag`) plus a `/docs` Scalar UI in the same style as `kube-state-graph`.

The proposal locked: (a) both backend clients share a `GraphBackend` interface with identical method signatures — the orchestrator builds different query strings per backend; (b) merge is union-by-node-id with edge de-duplication; (c) the gateway exposes a single endpoint `GET /v1/graph`; (d) any stage failure → `502` (no partial success).

## Goals / Non-Goals

**Goals:**
- Idiomatic Go module layout that mirrors `kube-state-graph` (`cmd/`, `internal/{api,client,merge,config,observability,build}`, `docs/`, `tools/`, `deploy/`, `scripts/`) so contributors moving between the two repos feel at home.
- Reusable backend wrappers as independent `struct`s satisfying a single `GraphBackend` interface — enables future N>2 fan-out without changing the merger or handler.
- Sequential pipeline with per-stage timeout. Any stage failure returns HTTP `502`. If primary returns no entries with `ipaddress`, the switch stage is skipped and the primary response is returned (still wrapped through merge so the envelope is identical).
- End-to-end OTLP tracing: inbound `traceparent` honoured (otelgin), outbound calls inject `traceparent` (otelhttp transport under resty), gateway span is parent of two child client spans.
- `log/slog` JSON logs in prod, text in dev, with `trace_id` / `span_id` automatically attached via a slog `Handler` that reads OTel span context.
- OpenAPI 3.1 spec **generated from `swag` annotations on handlers** (the kube-state-graph pattern), postprocessed to lift parameter `example` into `schema.example` for Scalar UI, copied into `internal/api/static/openapi/`, served at `/openapi.yaml` + `/openapi.json`, with **Scalar API Reference** UI at `/docs` (vendored bundle in `internal/api/static/scalar/`).
- Health endpoints `/livez` and `/readyz` (both return `200 ok`).
- Zero-overhead tracing: when `OTEL_EXPORTER_OTLP_ENDPOINT` is unset, a no-op tracer provider is installed.

**Non-Goals:**
- Inbound auth on the gateway (gateway just forwards per-backend `X-API-Key` outbound).
- Caching, ETag, or `If-None-Match` revalidation. Stateless pass-through + merge.
- Prometheus `/metrics` endpoint, per-backend latency histograms, expvar.
- Panic-recovery beyond Gin's default; expvar.
- Retry on transient errors (timeouts and 5xx surface as 502 directly).
- Partial-success path / `warnings[]` field — any backend failure → `502`.
- Single-backend fallback mode — both backend URLs are required at startup.
- Graceful shutdown drain logic (Gin's default `Shutdown` is sufficient).
- Schema transformation / cross-backend label reconciliation.
- Backend discovery, service registry, N>2 backends.

## Decisions

### D1. Project layout — mirror kube-state-graph

```
graph-api-gateway/
├── cmd/graph-api-gateway/main.go      # entrypoint, wires config → server
├── internal/
│   ├── api/                           # gin handlers, router, middleware, docs UI
│   │   ├── server.go
│   │   ├── handlers.go                # /v1/graph handler (with swag annotations)
│   │   ├── health.go                  # /livez, /readyz
│   │   ├── docs.go                    # /openapi.{yaml,json}, /docs, /docs/assets/* (with swag annotations)
│   │   ├── middleware.go              # recovery, request-id, otelgin, slog access log
│   │   └── static/                    # embedded via //go:embed
│   │       ├── openapi/               # openapi.yaml / openapi.json (copied from docs/)
│   │       └── scalar/                # vendored Scalar API Reference bundle
│   ├── client/                        # resty-based backend wrappers
│   │   ├── backend.go                 # GraphBackend interface, Response types (NodeData carries ipaddress)
│   │   ├── kubestategraph.go          # KubeStateGraphClient — primary
│   │   ├── switch.go                  # SwitchGraphClient — node-to-switch lookup
│   │   └── transport.go               # otelhttp-wrapped http.Client used by both
│   ├── pipeline/                      # pure orchestration helpers
│   │   ├── extract.go                 # ExtractIPs(*CytoscapeGraph) []string (node-type only)
│   │   └── reconcile.go               # ReconcileSwitch(primary, switch) *CytoscapeGraph
│   ├── merge/                         # pure merge logic, no I/O
│   │   └── merge.go
│   ├── config/                        # env + flag loading, validation
│   │   └── config.go
│   ├── observability/                 # OTLP setup, slog handler with trace ctx
│   │   ├── tracing.go
│   │   └── logging.go
│   └── build/                         # version / commit ldflags target
│       └── build.go
├── docs/                              # generated openapi + hand-written markdown
│   ├── api.md                         # hand-written narrative API reference
│   ├── openapi.yaml                   # generated by `swag init` + openapi-postprocess
│   ├── openapi.json                   # generated by `swag init` + openapi-postprocess
│   └── docs.go                        # swag boilerplate (committed, gitignore-exempt)
├── tools/openapi-postprocess/         # rewrites parameter `example` → `schema.example` for Scalar
│   └── main.go
├── deploy/docker/Dockerfile
├── local/docker-compose.yaml          # gateway + two stub backends + otel-collector
├── scripts/refresh-docs-ui.sh         # vendor scalar bundle into internal/api/static/scalar/
├── Makefile
├── go.mod
└── README.md
```

**Why**: this is the `cmd/` + `internal/` layout endorsed by [golang-standards/project-layout](https://github.com/golang-standards/project-layout) and `kube-state-graph` already follows it. Keeping `internal/api/static/{openapi,scalar}` embedded (via `//go:embed`) lets us ship a single binary with no runtime asset dependency. Handlers carry `swag` annotations and are the source of truth; `make docs` regenerates `docs/openapi.{yaml,json}` and copies them into `internal/api/static/openapi/` for embedding — identical pipeline to kube-state-graph.

**Alternatives considered**: (a) flat layout — rejected, doesn't scale once we add backends. (b) `pkg/` for client wrappers — rejected for v1, the clients are not stable public API yet; promote to `pkg/graphclient` only when an external consumer needs them. (c) hand-authored `api/openapi.yaml` — rejected, diverges from kube-state-graph and forces docs to drift from handler signatures.

### D2. Backend abstraction — single interface, two structs

```go
type GraphBackend interface {
    FetchGraph(ctx context.Context, q GraphQuery) (*CytoscapeGraph, error)
}
```

Backend identity (`kube-state-graph` vs `switch`) is encoded by concrete struct type and surfaces through (a) the `otelhttp` transport's span name / `http.url` attributes and (b) per-stage log fields (`s.logger.ErrorContext(ctx, "ksg fetch failed", …)` / `"switch fetch failed"`). No `Name()` method on the interface keeps it minimal and avoids forcing every future backend to declare a string tag.

`KubeStateGraphClient` and `SwitchGraphClient` share one resty transport, each constructed with `(baseURL, apiKey, timeout)`, but expose **request methods matched to each upstream's contract**: kube-state-graph is queried via `FetchGraph` (`GET <baseURL>/v1/graph?<rawQuery>`, the inbound query forwarded verbatim); the switch via `FetchGraphByIPs` (`POST <baseURL>/v1/graph` with a batched `[{"ip":…}]` JSON body). The shared `GraphBackend` interface (`FetchGraph`) is satisfied by the kube-state-graph client.

`NodeData` carries an optional `IPAddress []string` (JSON tag `ipaddress`, `omitempty`) so kube-state-graph's extended contract decodes losslessly. The switch backend is expected to emit Cytoscape responses whose nodes / edges use IDs compatible with primary's `<cluster>/<uid>` convention (see Open Questions for the reconciliation policy if this assumption fails).

**Why**: the orchestrator accepts the typed `*KubeStateGraphClient` + `*SwitchGraphClient` directly (no slice of interface) since the two stages have asymmetric roles. Both embed a shared unexported `graphClient` transport core (resty client, base URL, `Probe`); only the request-shaping method differs per type, so neither carries a method it never uses. The `GraphBackend` interface documents the kube-state-graph GET contract (asserted on `*KubeStateGraphClient`); tests exercise both clients against `httptest` servers rather than interface mocks.

**Alternatives considered**: threading the switch's batched IPs through `GraphQuery` (e.g. an `IPs []string` field) so both backends keep a single `FetchGraph` method — rejected, it forces kube-state-graph to ignore a field it never reads and hides the GET-vs-POST distinction behind one signature. A dedicated `FetchGraphByIPs` keeps each request shape explicit at the call site.

### D3. Outbound HTTP — resty wrapping an otelhttp-instrumented transport

Resty exposes its underlying `*http.Client`. We construct it as:

```go
httpClient := &http.Client{
    Transport: otelhttp.NewTransport(
        http.DefaultTransport,
        otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
            return r.Method + " " + r.URL.Path
        }),
    ),
}
restyClient := resty.NewWithClient(httpClient).
    SetTimeout(cfg.BackendTimeout).
    SetRetryCount(cfg.BackendRetries).
    SetRetryWaitTime(200 * time.Millisecond)
```

Both client structs accept this configured `*resty.Client`. `otelhttp` injects `traceparent` automatically; no manual propagation needed.

**Why**: `otelresty` exists as a third-party package but is thin and not as widely maintained as `otelhttp`. Wrapping the transport gets us OTel propagation, span creation, and HTTP metrics for free, while keeping resty's ergonomics (typed responses, retries, headers).

**Alternatives considered**: (a) `otelresty` middleware — rejected, less mainstream; (b) hand-rolled `OnBeforeRequest` propagation — rejected, reinvents otelhttp.

### D4. Merge algorithm — union by node id, edge dedup by (source,target,type)

```go
type edgeKey struct{ Type, Source, Target string }

func Merge(graphs ...*CytoscapeGraph) *CytoscapeGraph {
    nodes := map[string]Node{}        // first-writer-wins on id
    edges := map[edgeKey]Edge{}       // first-writer-wins on (type,source,target)
    for _, g := range graphs {
        if g == nil { continue }
        for _, n := range g.Elements.Nodes {
            if _, dup := nodes[n.Data.ID]; !dup { nodes[n.Data.ID] = n }
        }
        for _, e := range g.Elements.Edges {
            k := edgeKey{e.Data.Type, e.Data.Source, e.Data.Target}
            if _, dup := edges[k]; !dup { edges[k] = e }
        }
    }
    return assemble(nodes, edges)
}
```

**Rules**:
- Node id collision → **first-writer-wins** (kube-state-graph takes precedence). Deterministic given the fixed (kube, reconciled-switch) call order.
- Edge dedup uses a typed struct key `(type, source, target)` (not the backend's UUIDv5 `id`, which differs per source). This collapses true duplicates while preserving structurally distinct edges; using a struct key (vs concatenated string) avoids collisions if any field ever contains a separator char.
- Output is a fresh struct; inputs are not mutated (immutability rule).
- Output `clusters` is the sorted union of every input graph's `clusters` field. To preserve switch-only clusters end-to-end, `ReconcileSwitch` passes `switchGraph.Clusters` through into its returned graph (defensive copy) before `Merge` unions them with the kube side.

**Why first-writer-wins**: deterministic, debuggable, no schema-aware diffing required. The `warnings[]` field that earlier drafts proposed is explicitly dropped (see Non-Goals): any backend failure produces an HTTP error, not a partial-success body.

**Alternatives considered**: last-writer-wins (rejected, less intuitive — primary is the "trusted" source); deep-merge of labels (rejected for v1, opens up semantic questions about who owns which label namespace).

### D5. Sequential pipeline and failure

The `GET /v1/graph` handler is straight-line code (no goroutines, no errgroup):

```go
// 1. Primary fetch
primaryCtx, cancel := context.WithTimeout(ctx, cfg.Primary.Timeout)
primary, err := primaryClient.FetchGraph(primaryCtx, client.GraphQuery{RawQuery: c.Request.URL.RawQuery})
cancel()
if err != nil {
    return 502 upstream_unavailable
}

// 2. Extract node IPs
ips := pipeline.ExtractIPs(primary)

// 3. Conditional switch fetch
var switchGraph *client.CytoscapeGraph
if len(ips) > 0 {
    switchCtx, cancel := context.WithTimeout(ctx, cfg.Switch.Timeout)
    switchGraph, err = switchClient.FetchGraphByIPs(switchCtx, ips)
    cancel()
    if err != nil {
        return 502 upstream_unavailable
    }
}

// 4. Reconcile switch IDs onto kube IDs (no-op when switchGraph == nil)
reconciled := pipeline.ReconcileSwitch(primary, switchGraph)

// 5. Merge — kube wins on id collision; reconciled switch contributes edges + switch-only nodes
merged := merge.Merge(primary, reconciled)
return 200 merged
```

`SwitchGraphClient.FetchGraphByIPs([]string)` builds a `[]ipRequest` and POSTs it as a single JSON array body `[{"ip":"<a>"},{"ip":"<b>"}]` to `/v1/graph`; JSON encoding escapes each IP. Order is preserved from `ExtractIPs` (insertion-order dedup) so the wire form is stable for caching/log diffing.

**Why sequential, not parallel**: the switch call genuinely depends on the primary's output, so parallel fan-out would only be possible via speculative execution (call switch with stale IPs while primary runs). Not worth the complexity for v1.

**Why no `errgroup`**: with only two synchronous calls, plain control flow is clearer than wrapping in a group. We lose nothing — errors short-circuit naturally via the `if err != nil` returns.

**Span shape**: otelgin opens a server span; otelhttp opens two client spans (primary, then switch when called). Both client spans are children of the server span. Stage timings drop out naturally in trace waterfall views.

### D6. Observability stack

- **Tracing**: `go.opentelemetry.io/otel/sdk/trace` with OTLP/HTTP exporter (`otlptracehttp`). Resource attrs: `service.name=graph-api-gateway`, `service.version=<ldflag>`. If `OTEL_EXPORTER_OTLP_ENDPOINT` is empty → install `noop.NewTracerProvider()`.
- **Inbound**: `otelgin.Middleware("graph-api-gateway")`.
- **Outbound**: see D3.
- **Logging**: `slog.Handler` wrapping `slog.NewJSONHandler` (or `NewTextHandler` when `LOG_FORMAT=text`). The wrapper's `Handle` pulls `trace.SpanContextFromContext(ctx)` and adds `trace_id` / `span_id` attrs when valid (~30 lines).
- **Request ID + access log middleware** (mirror kube-state-graph): a `requestIDMiddleware` honours/generates `X-Request-ID`; a `loggingMiddleware` emits one slog `InfoContext` per request with `method`, `path`, `status`, `duration_ms`, `request_id`. `/livez` and `/readyz` are quiet on `2xx` to avoid drowning the log under probes.
- No `/metrics`, no expvar — explicit non-goals.

### D7. OpenAPI documentation — swag annotations + postprocess + Scalar UI (mirror kube-state-graph)

Adopt kube-state-graph's pipeline verbatim so the two services are operationally identical:

1. **Source of truth**: `swag` annotations (`// @Summary`, `// @Description`, `// @Param`, `// @Success`, `// @Router`, …) on Gin handler funcs in `internal/api/handlers.go` and `internal/api/docs.go`. Top-level `// @title` / `// @version` / `// @BasePath` etc. live above `main()` in `cmd/graph-api-gateway/main.go`.
2. **Generate**: `make docs` runs

   ```
   go tool swag init \
     -g cmd/graph-api-gateway/main.go \
     --output docs \
     --parseDependency --parseInternal --v3.1=true
   ```

   producing `docs/swagger.json` + `docs/swagger.yaml` + `docs/docs.go`. Use `swaggo/swag/v2` (RC for OpenAPI 3.1), pinned via `go.mod` tool dependency exactly as kube-state-graph does.
3. **Postprocess**: `go run ./tools/openapi-postprocess docs/swagger.json docs/swagger.yaml` — port the same tool (mutates parameters so `parameter.example` is also written under `parameter.schema.example`; Scalar reads only the latter for query/path params).
4. **Embed**: `make docs` then copies `docs/swagger.{yaml,json}` → `internal/api/static/openapi/openapi.{yaml,json}`. The static dir is included at build time via `//go:embed static/*`.
5. **Serve**: handler routes (all open, no auth gating, matching kube-state-graph):
   - `GET /openapi.yaml` → embedded YAML, `Content-Type: application/yaml`
   - `GET /openapi.json` → embedded JSON, `Content-Type: application/json`
   - `GET /docs` → HTML shell that loads Scalar API Reference and points at `/openapi.json`
   - `GET /docs/assets/*path` → vendored Scalar JS/CSS bundle from `internal/api/static/scalar/`
6. **UI**: **Scalar API Reference** (not Swagger UI, not Redoc) — bundle vendored by `scripts/refresh-docs-ui.sh` into `internal/api/static/scalar/`.
7. **CI gate**: `make check-docs` runs `make docs` then `git diff --quiet -- docs/ internal/api/static/openapi/` — fails CI if the committed spec is stale. Same target name and behaviour as kube-state-graph.
8. **Docker preview**: a `docker-docs` Makefile target that runs the container with a placeholder backend URL so `/docs` is reachable for spec review without needing real upstreams.

**Why this pattern**: the user explicitly asked to follow kube-state-graph. Sharing the toolchain means contributors moving between repos use the same `make docs` / `make check-docs` muscle memory, the same Scalar UI, and the same postprocess fix. The annotation-driven spec also stays in lock-step with handler signatures automatically.

**Alternatives considered**: (a) hand-authored `api/openapi.yaml` — rejected, diverges from kube-state-graph and silently drifts from handlers; (b) `oapi-codegen` server-side stubs — rejected, codegen-first inverts the workflow and is overkill for two handlers; (c) Swagger UI / Redoc instead of Scalar — rejected, kube-state-graph already chose Scalar.

### D8. Configuration surface

Env only, parsed with stdlib `os.Getenv` (no `flag`, no Viper).

| Env | Default | Purpose |
|---|---|---|
| `LISTEN_ADDR` | `:8080` | HTTP listen address |
| `LOG_LEVEL` | `info` | `debug \| info \| warn \| error` |
| `LOG_FORMAT` | `json` | `json \| text` |
| `KUBE_STATE_GRAPH_URL` | _required_ | kube-state-graph base URL |
| `KUBE_STATE_GRAPH_API_KEY` | `""` | forwarded as `X-API-Key` |
| `KUBE_STATE_GRAPH_TIMEOUT` | `10s` | per-call timeout |
| `SWITCH_GRAPH_URL` | _required_ | switch backend base URL |
| `SWITCH_GRAPH_API_KEY` | `""` | as above |
| `SWITCH_GRAPH_TIMEOUT` | `10s` | as above |
| `OTEL_*` | stdlib OTel env | standard OTLP exporter config |

Env keys are named after the upstream domain (not its pipeline role) so a future second-tier switch or alternative kube source can be added without renaming the existing ones. Startup validation: either `*_URL` empty → fail fast naming the missing key.

### D9. IP extraction, switch query, and ID reconciliation

The gateway and the switch backend share **one** join key: `data.ipaddress`. Neither side needs to know the other's ID format.

**Contract with kube-state-graph** (already being added upstream):
- Every `node`-type entry carries `data.ipaddress: []string` listing that K8s node's IPs (typically `InternalIP`, may include multi-NIC IPs).
- `pod`-type entries may also carry `ipaddress`, but the gateway does NOT use them — only K8s node IPs are forwarded to the switch backend, so pod-IP collisions don't enter the picture.

**Contract with switch backend** (confirmed):
- Switch backend returns Cytoscape graph for the queried IPs.
- For every endpoint shadow (a node that represents "a host with IP X" in the switch's topology), the switch backend MUST populate `data.ipaddress: ["X"]`. The shadow's `data.id` is free-form (e.g., `sw-host:<uuid>`).
- Switch-only nodes (switch chassis, ports, fabrics) have no `ipaddress` and pass through unchanged.

**Stage 2 — `pipeline.ExtractIPs(primary)`**:

```
seen := map[string]struct{}{}
out  := []string{}
for n in primary.Elements.Nodes:
    if n.Data.Type != "node": continue              # node-only, per scope
    for ip in n.Data.IPAddress:
        if ip == "" || ip in seen: continue
        seen[ip] = {}; out = append(out, ip)
return out
```

Insertion-order dedup → stable request body → friendlier to logs and any future caching.

**Stage 3 wire format**: `POST <switchBaseURL>/v1/graph` with `Content-Type: application/json` and body `[{"ip":"10.0.0.1"},{"ip":"10.0.0.2"}]`, built by `SwitchGraphClient.FetchGraphByIPs(ips)`. Every IP is sent in a single request; the switch backend MUST accept the JSON-array form.

**Skip rule**: zero IPs → no switch call. Handler still passes `nil` through `ReconcileSwitch` → `Merge`, both documented to handle nil cleanly.

**Stage 4 — `pipeline.ReconcileSwitch(primary, switchGraph)`** (pure, non-mutating):

```
# Index primary node IPs → kube ID
ipToNodeID := {}
for n in primary.Elements.Nodes:
    if n.Data.Type != "node": continue
    for ip in n.Data.IPAddress:
        if _, dup := ipToNodeID[ip]; !dup:
            ipToNodeID[ip] = n.Data.ID   # one IP → one node; primary's natural order wins on the rare dup

# Map switch shadow IDs → kube IDs
rewrite := {}
for n in switchGraph.Elements.Nodes:
    for ip in n.Data.IPAddress:
        if kid, ok := ipToNodeID[ip]; ok:
            rewrite[n.Data.ID] = kid
            break   # first matched IP wins per switch node

# Build output: drop collapsed shadows, rewrite edge endpoints
out := &CytoscapeGraph{APIVersion: "v1"}
for n in switchGraph.Elements.Nodes:
    if _, dropped := rewrite[n.Data.ID]; dropped: continue
    out.Elements.Nodes = append(out.Elements.Nodes, n)   # value copy; not aliasing
for e in switchGraph.Elements.Edges:
    e2 := e                            # value copy
    if kid, ok := rewrite[e2.Data.Source]; ok: e2.Data.Source = kid
    if kid, ok := rewrite[e2.Data.Target]; ok: e2.Data.Target = kid
    out.Elements.Edges = append(out.Elements.Edges, e2)
return out
```

**Stage 5 — `Merge(primary, reconciled)`**:
- Already implemented; unchanged. Because reconciled never reintroduces a shadow with a kube-node ID (those were dropped) and kube nodes come first in input order, the existing first-writer-wins rule cleanly keeps the kube-side metadata.
- Edges go through standard `(type, source, target)` dedup — if both sides happen to describe the same edge (rare), they collapse.

**Properties of the algorithm**:
- O(N+M) in node count; one pass over each graph.
- No mutation of either input — `out` holds value copies of nodes/edges.
- Switch-only nodes (no `ipaddress` match) pass through with original IDs → switch chassis / ports remain addressable from edges.
- IPs that switch knows about but kube doesn't (foreign endpoints) stay as their own shadow nodes — data preserved, just disconnected from kube subgraph. Acceptable behaviour; can be surfaced later if needed.
- Gateway is forward-compatible: if switch backend forgets to set `ipaddress`, `rewrite` is empty, switch graph appears alongside but disconnected — no crash, no data loss.

## Risks / Trade-offs

- **First-writer-wins hides backend disagreement** → Acceptable for v1; revisit only if observed in practice.
- **No inbound auth on the gateway** → Explicit non-goal; backends still enforce their own `X-API-Key`.
- **swag-generated spec drifts from handlers** → `make check-docs` in CI fails the build on diff. Matches kube-state-graph.
- **swag v2 RC dependency** → Pin the exact version in `go.mod` to match kube-state-graph.
- **Edge key `type|source|target` collapses true multi-edges** → Acceptable for v1.
- **Any stage failure → 502** → Operators lose primary data when only switch is down. Acceptable v1 trade-off; revisit with a concrete consumer ask.
- **Switch backend depends on kube-state-graph contract change** → Gateway only decodes `data.ipaddress` if present (`omitempty`); when the field is absent the switch stage simply receives zero IPs and is skipped, so the gateway stays compatible with the pre-change kube-state-graph contract.
- **Long IP lists inflate the switch request body** → POSTing the IPs as a `[{"ip":…}]` JSON body sidesteps the URL/header-length limits the old `?ip=…` query form would have hit (a 1000-node cluster is fine), but a very large body could still stress the switch backend. Mitigation: cap at 500 IPs per call (TBD threshold) and either truncate with a WARN log or chunk into multiple switch calls. Out of scope for v1 — flag if observed.
- **Sequential latency** = primary RTT + switch RTT (no overlap). Acceptable since the chain is short. Speculative parallel fetch (call switch with the previous request's IPs while primary runs) is a future optimisation, not v1.

## Migration Plan

Greenfield repo — no migration. Rollout:
1. Land `cmd/`, `internal/`, generated `docs/swagger.{yaml,json}` + embedded copies in `internal/api/static/openapi/`, Makefile, Dockerfile.
2. Wire CI: `go test ./...`, `go vet`, `golangci-lint`, `make check-docs`.
3. Local: `local/docker-compose.yaml` spins up gateway + `stub-ksg` + `stub-switch` + an OTel collector → trace and merged response verifiable end-to-end.
4. Promote to staging once `/v1/graph` returns a merged Cytoscape payload with both backends reachable.

Rollback: standalone service, drop the deployment. Backends are untouched.

## Open Questions

- **Module path**: placeholder `github.com/marz32one/graph-api-gateway` until corrected.
- **Switch backend ID reconciliation**: resolved — switch backend emits free-form IDs and tags endpoint shadows with `data.ipaddress`. Gateway does IP-keyed reconciliation in `pipeline.ReconcileSwitch` (see D9).
- **Long IP list handling**: at what threshold do we chunk the switch query? Defer until we have a real cluster-size data point.
- **kube-state-graph cutover**: the `data.ipaddress` field is being added on the kube-state-graph side as a parallel workstream. Gateway code tolerates its absence (empty IP list → switch skipped), so the two repos can ship asynchronously.
