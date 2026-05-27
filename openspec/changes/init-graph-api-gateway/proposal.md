## Why

We have two related Cytoscape.js graph backends. `kube-state-graph` exposes `GET /v1/graph` returning a multi-cluster pod / node / PVC topology. A second backend (the **switch backend**) exposes the **node-to-switch (L2/L3 network)** topology, but it only knows endpoints by IP address — it has no concept of kube clusters or UIDs.

To render a "pod → node → switch" view end-to-end, a client today would have to (1) call kube-state-graph, (2) walk every `node`-type entry to harvest IPs, (3) call the switch backend with those IPs, and (4) reconcile + merge the two graphs (the switch backend returns its own IDs; gateway re-anchors them onto the kube node IDs by matching IP). This gateway centralises that sequential pipeline so the UI sees a single endpoint and a single merged response, with W3C trace context propagated across all upstream calls.

## What Changes

- **NEW** Go HTTP service `graph-api-gateway` built on **Gin**, idiomatic project layout (`cmd/`, `internal/`).
- **NEW** Single public endpoint `GET /v1/graph` that runs a **sequential pipeline**: (1) call kube-state-graph with the inbound query string, (2) extract `data.ipaddress` from every `node`-type entry in the response, (3) if any IPs were collected, call the switch backend with those IPs batched as `?ip=<a>&ip=<b>&…`, (4) reconcile switch IDs onto kube node IDs by IP match, (5) merge the two Cytoscape payloads (union by node `id`, edge de-duplication), (6) return the merged `{apiVersion, elements:{nodes,edges}}` envelope.
- **NEW** Two backend wrapper clients on **resty** — `KubeStateGraphClient` (primary) and `SwitchGraphClient` (node-to-switch lookup) — both satisfying a shared `GraphBackend` interface (same `FetchGraph(ctx, GraphQuery)` signature; the gateway builds different query strings per backend).
- **NEW** Pure helpers in `internal/pipeline/`: `ExtractIPs(*CytoscapeGraph) []string` (walks `node`-type entries, collects `data.ipaddress[]`, dedup, preserves order) and `ReconcileSwitch(primary, switch) *CytoscapeGraph` (builds IP→kube-node-ID index from primary, finds switch shadow nodes by matching `data.ipaddress`, rewrites their references in switch edges, drops the now-redundant shadow nodes).
- **NEW** OpenTelemetry (OTLP/HTTP) tracing wired via **otelgin** (inbound) and **otelhttp** on resty's underlying transport (outbound), honouring inbound W3C `traceparent` and forwarding it to both backends. Zero overhead when `OTEL_EXPORTER_OTLP_ENDPOINT` is unset.
- **NEW** Structured logging with **`log/slog`** (stdlib), JSON by default / text via `LOG_FORMAT=text`, `trace_id` / `span_id` auto-attached from request context.
- **NEW** Minimal env-driven config: listen address, per-backend base URL + API key + timeout, OTLP endpoint, log level, log format.
- **NEW** Health endpoints `/livez`, `/readyz` (both return `200 ok`).
- **NEW** OpenAPI 3.1 docs pipeline that mirrors kube-state-graph: `swag` annotations on handlers → `go tool swag init` → `tools/openapi-postprocess` → embedded at `internal/api/static/openapi/` → served at `/openapi.{yaml,json}` with **Scalar UI** at `/docs`. CI gate via `make check-docs`.
- The inbound query string is forwarded **only to the primary** (kube-state-graph). The switch backend receives only the IP list derived from the primary's response.
- Failure policy: any stage failure (primary call, switch call) returns HTTP `502`. No partial-success warnings, no fallback.
- Short-circuit: if the primary returns zero entries with `ipaddress`, the switch call is skipped and the primary response is returned as-is (still merged through the same code path so the envelope is consistent).

## Capabilities

### New Capabilities
- `http-gateway`: Gin server, `GET /v1/graph` (sequential primary → IP-extract → switch → merge), `/livez` `/readyz`, OpenAPI/Scalar doc routes, env-driven config.
- `backend-clients`: Two resty-based wrappers (`KubeStateGraphClient`, `SwitchGraphClient`) behind a shared `GraphBackend` interface — base URL, optional `X-API-Key`, per-call timeout, typed Cytoscape decoding (now including `data.ipaddress` on `NodeData`).
- `graph-merging`: Pure helpers — `Merge` (union nodes by `data.id` first-writer-wins, dedup edges by `(type, source, target)`), `ExtractIPs` (collect `data.ipaddress[]` from `node`-type entries, dedup, preserve insertion order), and `ReconcileSwitch` (IP-keyed re-anchoring of switch graph onto kube node IDs).
- `observability`: otelgin inbound spans, otelhttp outbound spans, `log/slog` with trace correlation, no-op when OTLP disabled.

### Modified Capabilities
_None — greenfield repo._

## Impact

- **New Go module** `github.com/<org>/graph-api-gateway` (module path TBD in design).
- **Dependencies**: `github.com/gin-gonic/gin`, `github.com/go-resty/resty/v2`, `go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin`, `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp`, `go.opentelemetry.io/otel`, `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp`, `go.opentelemetry.io/otel/sdk`. (`log/slog` is stdlib.)
- **No upstream changes** to `kube-state-graph` — gateway consumes its public `/v1/graph` contract as-is.
- **Switch backend**: client struct scaffolded against the Cytoscape envelope; queried with `?ip=…` parameters batched into a single call; concrete base URL injected via `BACKEND_SWITCH_URL`.
- **kube-state-graph dependency**: assumes the upstream contract has been extended so `node` and `pod` entries carry `data.ipaddress: []string` (tracked separately on the kube-state-graph side).
- **Operational**: one new container/binary to deploy; expects two backend URLs reachable on the network; emits OTLP traces to whatever collector `OTEL_EXPORTER_OTLP_ENDPOINT` points at.
