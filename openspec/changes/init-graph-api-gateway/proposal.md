## Why

We have two related Cytoscape.js graph backends. `kube-state-graph` exposes `GET /v1/graph` returning a multi-cluster pod / node / PVC topology. A second backend (the **switch backend**) exposes the **node-to-switch (L2/L3 network)** topology, but it only knows endpoints by IP address — it has no concept of kube clusters or UIDs.

To render a "pod → node → switch" view end-to-end, a client today would have to (1) call kube-state-graph, (2) walk every `node`-type entry to harvest IPs, (3) call the switch backend with those IPs, and (4) reconcile + merge the two graphs (the switch backend returns its own IDs; gateway re-anchors them onto the kube node IDs by matching IP). This gateway centralises that sequential pipeline so the UI sees a single endpoint and a single merged response, with W3C trace context propagated across all upstream calls.

## What Changes

- **NEW** Go HTTP service `graph-api-gateway` built on **Gin**, idiomatic project layout (`cmd/`, `internal/`).
- **NEW** Single public endpoint `GET /v1/graph` that runs a **sequential pipeline**: (1) call kube-state-graph with the inbound query string, (2) extract `data.ipaddress` from every `node`-type entry in the response, (3) if any IPs were collected, POST those IPs to the switch backend batched as a single `[{"ip":"<a>"},{"ip":"<b>"}]` JSON body, (4) reconcile switch IDs onto kube node IDs by IP match, (5) merge the two Cytoscape payloads (union by node `id`, edge de-duplication), (6) return the merged `{apiVersion, elements:{nodes,edges}}` envelope.
- **NEW** Two backend wrapper clients on **resty** — `KubeStateGraphClient` (primary, queried via `FetchGraph` over `GET`) and `SwitchGraphClient` (node-to-switch lookup, queried via `FetchGraphByIPs` which `POST`s a batched IP body) — sharing one resty transport; `KubeStateGraphClient` satisfies the `GraphBackend` interface.
- **NEW** Pure helpers in `internal/pipeline/`: `ExtractIPs(*CytoscapeGraph) []string` (walks `node`-type entries, collects `data.ipaddress[]`, dedup, preserves order) and `ReconcileSwitch(primary, switch) *CytoscapeGraph` (builds IP→kube-node-ID index from primary, finds switch shadow nodes by matching `data.ipaddress`, rewrites their references in switch edges, drops the now-redundant shadow nodes).
- **NO tracing** — the service ships no OpenTelemetry/OTLP instrumentation; `OTEL_*` env vars have no effect. Observability is structured logging only.
- **NEW** Structured logging with **`log/slog`** (stdlib), JSON by default / text via `LOG_FORMAT=text`; one access-log record per request with `method`/`path`/`status`/`duration_ms`/`request_id`.
- **NEW** Optional inbound API-key auth mirroring kube-state-graph: `X-API-Key` validated against a constant-time `KeySet`, configured via `API_KEYS` / `API_KEYS_FILE` (hot-reloaded) / `API_KEYS_RELOAD_INTERVAL`; health, OpenAPI spec, and Swagger UI routes are exempt. Disabled when no keys are configured.
- **NEW** Minimal env-driven config: listen address, per-backend base URL + API key + timeout, inbound API keys, log level, log format.
- **NEW** Health endpoints `/livez`, `/readyz` (both return `200 ok`).
- **NEW** OpenAPI 3.1 docs pipeline: `swag` annotations on handlers → `go tool swag init` → `tools/openapi-postprocess` → embedded at `internal/api/static/openapi/` → served at `/openapi.{yaml,json}`, with an **offline Swagger UI** (swaggo/files, all assets in-binary) at `/docs/` pointed at `/openapi.json`. CI gate via `make check-docs`.
- The inbound query string is forwarded **only to the primary** (kube-state-graph). The switch backend receives only the IP list derived from the primary's response.
- Failure policy: any stage failure (primary call, switch call) returns HTTP `502`. No partial-success warnings, no fallback.
- Short-circuit: if the primary returns zero entries with `ipaddress`, the switch call is skipped and the primary response is returned as-is (still merged through the same code path so the envelope is consistent).

## Capabilities

### New Capabilities
- `http-gateway`: Gin server, `GET /v1/graph` (sequential primary → IP-extract → switch → merge), `/livez` `/readyz`, OpenAPI spec + offline Swagger UI doc routes, optional inbound `X-API-Key` auth, env-driven config.
- `backend-clients`: Two resty-based wrappers (`KubeStateGraphClient` via `FetchGraph`/`GET`, `SwitchGraphClient` via `FetchGraphByIPs`/`POST`) sharing one transport — base URL, optional `X-API-Key`, per-call timeout, typed Cytoscape decoding (now including `data.ipaddress` on `NodeData`).
- `graph-merging`: Pure helpers — `Merge` (union nodes by `data.id` first-writer-wins, dedup edges by `(type, source, target)`), `ExtractIPs` (collect `data.ipaddress[]` from `node`-type entries, dedup, preserve insertion order), and `ReconcileSwitch` (IP-keyed re-anchoring of switch graph onto kube node IDs).
- `observability`: `log/slog` structured logging (JSON/text) with a per-request access log; no tracing backend.

Inbound `X-API-Key` authentication is folded into the `http-gateway` capability (constant-time `KeySet`, CSV or hot-reloaded file, mirroring kube-state-graph; health/spec/UI routes exempt).

### Modified Capabilities
_None — greenfield repo._

## Impact

- **New Go module** `github.com/<org>/graph-api-gateway` (module path TBD in design).
- **Dependencies**: `github.com/gin-gonic/gin`, `github.com/go-resty/resty/v2`, `github.com/google/uuid`, `github.com/swaggo/files/v2` (embedded Swagger UI 5.x), `github.com/swaggo/swag/v2` (docs-generation tool). (`log/slog` and `crypto/subtle` are stdlib.) **No OpenTelemetry dependencies.**
- **No upstream changes** to `kube-state-graph` — gateway consumes its public `/v1/graph` contract as-is.
- **Switch backend**: client struct scaffolded against the Cytoscape envelope; queried with a `POST /v1/graph` `[{"ip":…}]` JSON body batching every IP into a single call; concrete base URL injected via `SWITCH_GRAPH_URL`.
- **kube-state-graph dependency**: assumes the upstream contract has been extended so `node` and `pod` entries carry `data.ipaddress: []string` (tracked separately on the kube-state-graph side).
- **Operational**: one new container/binary to deploy; expects two backend URLs reachable on the network; emits structured slog records to stdout (no external tracing collector).
