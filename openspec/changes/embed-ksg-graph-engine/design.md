## Context

`graph-api-gateway` merges two Cytoscape.js graphs into one `GET /v1/graph` response: the multi-cluster kube topology from `kube-state-graph` (ksg) and a network-switch topology from an external switch backend, reconciled by IP (this repo's init design D4 / D9). Today the kube side is fetched over HTTP — `KubeStateGraphClient` issues `GET {KUBE_STATE_GRAPH_URL}/v1/graph` and decodes the JSON into gateway-local structs.

ksg has lifted its graph engine out of `internal/` into a public `pkg/` (ksg design **D32**): `pkg/{graph,build,promql,clock,cytoscape}` plus a `pkg/kubegraph` facade whose `Engine.BuildFromValues(ctx, url.Values) (cytoscape.Body, error)` runs parse → build → project → serialise in one call. This change embeds that engine so the gateway builds the kube graph in-process — same logic, no HTTP, no JSON round-trip. The switch backend remains an external HTTP source.

## Goals / Non-Goals

**Goals:**
- Replace the HTTP call to ksg with an in-process `kubegraph.Engine` that queries VictoriaMetrics directly.
- Eliminate the serialise → HTTP → deserialise round-trip while keeping byte-identical kube-graph output (ksg D6 / D9 / D32).
- Unify on `pkg/cytoscape` DTO types; delete the gateway's duplicate structs.
- Preserve the gateway's external contract: pipeline shape, merge semantics, failure policy, auth, docs, observability.

**Non-Goals:**
- Changing the switch backend integration (still an external `POST /v1/graph` keyed by IP).
- Rewriting `ExtractIPs` / `ReconcileSwitch` / `Merge` logic (retype only).
- Adding tracing / metrics backends to the gateway (the init D6 slog-only stance holds; engine metrics are no-op'd).
- Caching the built graph (ksg D2 no-cache stance carries over; each request builds fresh).

## Decisions

### D1. Embed `kubegraph.Engine`; drop the ksg HTTP hop
The gateway depends on `github.com/marz32one/kube-state-graph` and constructs one `kubegraph.Engine` at startup from a `promql.Querier` (built from the VictoriaMetrics URL via `promql.New`) and `kubegraph.Options{MetricPrefix, Clock, Metrics}`. The primary "fetch" becomes `engine.BuildFromValues(ctx, values)` — fully in-process. No `GET /v1/graph` to ksg, no resty for the primary.

- Why: the round-trip ksg performs (build → serialise → HTTP → the gateway decodes) is pure overhead when the gateway can run the same `pkg/` engine. ksg D32 made the engine importable precisely for this consumer.

### D2. `KubeStateGraphClient` stays the `GraphBackend`, reimplemented in-process
Rather than delete the abstraction, `KubeStateGraphClient` keeps satisfying `GraphBackend`, but its body changes: it holds `*kubegraph.Engine` and implements `FetchGraph(ctx, GraphQuery)` by parsing `GraphQuery.RawQuery` into `url.Values` and calling `engine.BuildFromValues`. The handler pipeline (`s.ksg.FetchGraph(...)` → `ExtractIPs` → switch → reconcile → merge) is untouched at the call-site level.

- Why: keeps the pipeline and its tests stable; the backend swap is localised to one struct. The `GraphBackend` seam stays clean — a future HTTP fallback could reappear behind it without touching the handler (see Risks: VM reachability).

### D3. Unify DTOs on `pkg/cytoscape`
Delete the gateway's local `CytoscapeGraph` / `Elements` / `Node` / `NodeData` / `Edge` / `EdgeData` structs. Use `pkg/cytoscape` types everywhere: the engine returns them, the switch client decodes its JSON into them, and `ExtractIPs` / `ReconcileSwitch` / `Merge` operate on them.

- Why: a single DTO definition with no drift between the gateway's model and ksg's wire shape — the gateway's structs were always a hand-copy of ksg's output.
- Depends on: `pkg/cytoscape` exporting JSON-tagged DTO fields including `data.ipaddress` and `data.parent` (tracked in ksg D32).

### D4. Config surface — VictoriaMetrics URL + metric prefix, drop the ksg API key
- `KUBE_STATE_GRAPH_URL` is **replaced by the VictoriaMetrics base URL** (the upstream the engine queries). **Recommended: rename to `VICTORIA_METRICS_URL`** since the meaning changes fundamentally; keep it required and validated as an http(s) URL without userinfo/query/fragment (mirroring the existing backend-URL validation).
- `KSG_METRIC_PREFIX` (optional) → `kubegraph.Options.MetricPrefix`, mirroring ksg's `KSG_METRIC_PREFIX` (ksg D26).
- `KSG_BUILD_TIMEOUT` (optional, default `15s`) bounds the in-process build, replacing the old per-call HTTP timeout to the ksg service.
- `KUBE_STATE_GRAPH_API_KEY` is **removed** — no HTTP call to ksg means no outbound key.
- Switch config (`SWITCH_GRAPH_URL`, `SWITCH_GRAPH_API_KEY`, `SWITCH_GRAPH_TIMEOUT`) and inbound `API_KEYS*` are unchanged.

### D5. Query parsing lives in the engine facade
The gateway no longer forwards a raw query string to a remote ksg; it hands the inbound `url.Values` (`c.Request.URL.Query()`) to `engine.BuildFromValues`, which owns `start` / `end` validation and `graph.Scope` construction (ksg D32). The gateway adds no parsing of its own and preserves the "inbound parameters go to the primary only" contract — the switch still receives only the derived IP set.

- Why: one parser (ksg's), zero drift; the gateway's existing parameter-forwarding contract is preserved unchanged.

### D6. Switch backend and merge stay at the DTO layer
The switch graph is still fetched via `SwitchGraphClient.FetchGraphByIPs` (external `POST /v1/graph`, IP body) and merged by `ReconcileSwitch` + `Merge` over `pkg/cytoscape` DTOs. No native-graph merging: ksg's `GraphNode` is a sealed interface (ksg D11 / D32), so switch nodes cannot be minted as `GraphNode`s — the Cytoscape DTO remains the integration point, exactly as today.

### D7. Engine metrics and tracing are no-op'd
The gateway passes a no-op metrics sink and no OTLP configuration into `kubegraph.Options`, so it does not register ksg's `kube_state_graph_*` self-metrics in its own registry and adds no tracing backend (consistent with init D6 slog-only). Engine spans stay no-op unless an OTLP endpoint is configured in the environment (ksg D25).

### D8. Build timeout via the existing per-stage context
The handler already wraps each pipeline stage in a context with a deadline; the primary stage's deadline (now `KSG_BUILD_TIMEOUT`) bounds `engine.BuildFromValues`. On `context.DeadlineExceeded` the gateway returns `504`, unchanged from today's timeout handling.

### D9. Error mapping preserved
`engine.BuildFromValues` returns typed errors (mirroring ksg's `build.Reason`). The gateway maps `context.DeadlineExceeded` → `504 {"error":"upstream_timeout"}` and every other engine error → `502 {"error":"upstream_unavailable"}`, preserving today's external status contract. Refining validation errors (bad `start` / `end`) to `400` is deferred — see Open Questions.

## Risks / Trade-offs

- [Gateway now couples to ksg's module + transitive deps] → brings `prometheus/client_golang`, the OTel SDK, etc. into the gateway binary. Accepted: it is the same code that would otherwise run in the ksg process; binary size grows modestly.
- [Config migration is breaking] → `KUBE_STATE_GRAPH_URL` changes meaning (or is renamed) and `KUBE_STATE_GRAPH_API_KEY` is dropped; deployments must update env. Mitigate with a clear fail-fast startup error naming the new key and a migration note.
- [Gateway must reach VictoriaMetrics directly] → a network-topology change; VM must be routable from the gateway. If VM access is restricted in some environment, the embedded approach cannot be used there; the `GraphBackend` seam (D2) keeps the HTTP client re-introducible as a fallback.
- [Engine version skew] → the gateway pins a ksg module version; a ksg contract change requires a coordinated bump. Single-source-of-truth (ksg D32) is the upside; version choreography is the cost — smaller than maintaining a forked copy.
- [Depends on ksg D32 landing first] → `pkg/cytoscape` + `pkg/kubegraph` must exist and be tagged before the gateway can import them. Sequencing handled in the Migration Plan.

## Migration Plan

1. Land ksg D32 (public `pkg/`, `pkg/kubegraph` facade, `pkg/cytoscape` DTO) and tag a ksg module version.
2. Add `require github.com/marz32one/kube-state-graph <version>` to the gateway `go.mod`.
3. Replace `KubeStateGraphClient` internals (in-process engine adapter); delete the local DTO structs; retype `ExtractIPs` / `ReconcileSwitch` / `Merge` onto `pkg/cytoscape`.
4. Swap config: `KUBE_STATE_GRAPH_URL` → VM URL (rename to `VICTORIA_METRICS_URL`); add `KSG_METRIC_PREFIX`, `KSG_BUILD_TIMEOUT`; drop `KUBE_STATE_GRAPH_API_KEY`.
5. Update deployment manifests + docs; verify the merged `/v1/graph` output is byte-compatible against a captured golden response from the HTTP-backed gateway.

Rollback: revert the gateway commit (and env) to the resty-backed `KubeStateGraphClient`; ksg's HTTP API still exists and is unaffected.

## Open Questions

- Rename `KUBE_STATE_GRAPH_URL` → `VICTORIA_METRICS_URL`, or keep the name with new meaning? (Recommended: rename, for honesty about what it now points at.)
- Surface engine validation errors (bad `start` / `end`) as `400` instead of `502`? A small contract improvement; deferred to keep this change behavior-preserving (today a bad param reaches ksg over HTTP and the non-2xx is folded into `502`).
- Should the gateway expose `kubegraph`'s lower-level `Build` / `Project` for future native augmentation of the kube graph, or stay DTO-only? (Stay DTO-only for now per D6.)
