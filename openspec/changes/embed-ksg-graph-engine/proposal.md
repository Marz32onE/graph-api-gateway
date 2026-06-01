## Why

The gateway reaches `kube-state-graph` (ksg) over HTTP today: `KubeStateGraphClient` issues `GET {KUBE_STATE_GRAPH_URL}/v1/graph`, then decodes the JSON body into gateway-local `CytoscapeGraph` structs before the IP-extract → switch → reconcile → merge pipeline runs.

ksg has lifted its graph engine out of `internal/` into a public `pkg/` (ksg design **D32**): `pkg/{graph,build,promql,clock,cytoscape}` plus a `pkg/kubegraph` facade whose `Engine.BuildFromValues(ctx, url.Values) (cytoscape.Body, error)` runs parse → build → project → serialise in a single in-process call. This change embeds that engine so the gateway builds the kube graph **in-process** against VictoriaMetrics directly — reusing ksg's exact graph logic byte-for-byte and dropping the serialise → HTTP → deserialise round-trip. The switch backend stays an external HTTP source; only the ksg hop becomes in-process.

## What Changes

- **MODIFIED** `KubeStateGraphClient` stops issuing HTTP and becomes an in-process adapter over `kubegraph.Engine`. It still satisfies `GraphBackend`, but `FetchGraph(ctx, GraphQuery)` now parses `GraphQuery.RawQuery` into `url.Values` and calls `engine.BuildFromValues`. No resty, no `GET /v1/graph` to ksg, no outbound `X-API-Key`.
- **MODIFIED** DTO unification: the gateway deletes its hand-maintained `CytoscapeGraph` / `Elements` / `Node` / `NodeData` / `Edge` / `EdgeData` structs and adopts `pkg/cytoscape` types throughout. The switch backend's JSON decodes into the same `pkg/cytoscape` types (identical shape). `ExtractIPs` / `ReconcileSwitch` / `Merge` keep their logic; only the type names change.
- **MODIFIED** config surface: `KUBE_STATE_GRAPH_URL` is replaced by a **VictoriaMetrics** base URL (the upstream the engine queries; recommended rename `VICTORIA_METRICS_URL`), plus new `KSG_METRIC_PREFIX` and `KSG_BUILD_TIMEOUT` knobs mirroring ksg. `KUBE_STATE_GRAPH_API_KEY` is **removed** (no ksg HTTP call). Switch config and inbound `API_KEYS*` are unchanged.
- **UNCHANGED** the `GET /v1/graph` pipeline shape, merge semantics, failure policy (`502` / `504`), switch backend contract (`POST /v1/graph` IP body), inbound API-key auth, health, OpenAPI / docs routes, and `log/slog` observability.
- **NEW dependency** `github.com/marz32one/kube-state-graph` (for `pkg/...`); `resty` is retained for the switch client only.

## Capabilities

### Modified Capabilities
- `backend-clients`: `KubeStateGraphClient` becomes an in-process `kubegraph.Engine` adapter (no resty, no HTTP, no outbound `X-API-Key`, no per-call HTTP timeout — the engine queries VictoriaMetrics directly under a build timeout). `SwitchGraphClient` is unchanged. Cytoscape decoding narrows to the switch side; both clients now return `pkg/cytoscape.Body`.
- `http-gateway`: the primary fetch is in-process; the config surface swaps the ksg HTTP URL for a VictoriaMetrics URL + metric prefix + build timeout; backend-URL validation changes accordingly.
- `graph-merging`: `ExtractIPs` / `ReconcileSwitch` / `Merge` retype onto `pkg/cytoscape` (mechanical; logic and scenarios preserved).

### New Capabilities
_None — this change rewires existing capabilities onto the embedded engine._

## Impact

- **New module dependency** `github.com/marz32one/kube-state-graph` (pulls in `prometheus/client_golang`, `google/uuid`, `golang.org/x/sync`, and the OpenTelemetry SDK family transitively via `pkg/`). `resty` retained for the switch client only.
- **Upstream topology change**: the gateway now talks **directly to VictoriaMetrics** (Prometheus HTTP API) instead of to the ksg service. VM must be routable from the gateway; the ksg service is no longer a runtime dependency of the gateway.
- **Config migration (breaking)**: `KUBE_STATE_GRAPH_URL` meaning changes (ksg API URL → VM URL; recommended rename to `VICTORIA_METRICS_URL`); `KUBE_STATE_GRAPH_API_KEY` is removed; `KSG_METRIC_PREFIX` and `KSG_BUILD_TIMEOUT` are added. Deployment manifests and docs must be updated.
- **No wire change** on the gateway's own `GET /v1/graph` response — the merged Cytoscape envelope is byte-compatible (the embedded engine produces the same DTO the ksg HTTP API did, per ksg D6 / D32 determinism).
- **Self-metrics**: the gateway passes a no-op metrics sink to the engine, so it does not register `kube_state_graph_*` series in its own Prometheus registry (ksg D32).
- **Sequencing**: depends on ksg D32 landing the public `pkg/` and a tagged module version first; the gateway then bumps to it.
