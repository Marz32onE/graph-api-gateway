## MODIFIED Requirements

### Requirement: Shared Transport, Per-Backend Request Shape
The codebase SHALL define a `GraphBackend` interface exposing `FetchGraph(ctx, GraphQuery) (*cytoscape.Body, error)`, satisfied by every backend wrapper. The two upstreams now differ in *kind*, not merely in request shape: the kube-state-graph backend is **in-process** — `KubeStateGraphClient` wraps an embedded `kubegraph.Engine` (from kube-state-graph `pkg/`) and answers `FetchGraph` by parsing `GraphQuery.RawQuery` into `url.Values` and calling `engine.BuildFromValues`, with no HTTP call. The switch backend remains **out-of-process** — `SwitchGraphClient.FetchGraphByIPs(ctx, []string)` POSTs every collected IP in one `[{"ip":…}]` JSON body. Backend identity (`"kube-state-graph"` vs `"switch"`) is encoded by concrete type, not by an interface method, and surfaces through per-stage log fields.

#### Scenario: Both built-in clients satisfy the interface
- **WHEN** the codebase is compiled
- **THEN** `KubeStateGraphClient` and `SwitchGraphClient` both satisfy `GraphBackend`

#### Scenario: Primary fetch issues no HTTP call
- **WHEN** `KubeStateGraphClient.FetchGraph` is invoked
- **THEN** the kube graph is produced in-process by the embedded engine
- **AND** no outbound HTTP request is made to any kube-state-graph service

### Requirement: Resty HTTP Client
The **switch** backend wrapper SHALL use a `*resty.Client` backed by a plain `&http.Client{Timeout: …}` with no tracing instrumentation; outbound calls SHALL NOT carry a `traceparent` header. The **kube-state-graph** backend wrapper SHALL NOT use resty or any HTTP client to a kube-state-graph service — it queries VictoriaMetrics through the embedded engine's `promql.Querier`.

#### Scenario: No tracing header on switch outbound calls
- **WHEN** the switch backend client issues its upstream `POST /v1/graph`
- **THEN** the outbound request carries no `traceparent` header

#### Scenario: Primary uses no kube-state-graph HTTP transport
- **WHEN** `KubeStateGraphClient` is constructed
- **THEN** it opens no HTTP connection to a kube-state-graph service (its only upstream is VictoriaMetrics, reached via the engine's `promql.Querier`)

### Requirement: Configurable Base URL, API Key, and Timeout
The **switch** backend wrapper SHALL be constructed with its own base URL, optional API key (forwarded as `X-API-Key`), and per-call timeout. The **kube-state-graph** backend wrapper SHALL be constructed with a VictoriaMetrics base URL (used to build the engine's `promql.Querier`), an optional metric-name prefix (`kubegraph.Options.MetricPrefix`), and a build timeout; it carries **no API key** — there is no outbound kube-state-graph call.

#### Scenario: Switch API key is forwarded when configured
- **WHEN** the switch client is constructed with API key `secret-1` and issues an upstream call
- **THEN** the outbound request carries header `X-API-Key: secret-1`

#### Scenario: Primary carries no outbound API key
- **WHEN** `KubeStateGraphClient.FetchGraph` runs
- **THEN** no `X-API-Key` header is emitted by the primary path (it makes no kube-state-graph HTTP call)

#### Scenario: Build timeout aborts a slow build
- **WHEN** the kube-state-graph client is configured with a `15s` build timeout and the in-process build exceeds it
- **THEN** `FetchGraph` returns a deadline-exceeded error

### Requirement: Cytoscape Response Decoding
The **switch** backend wrapper SHALL decode its HTTP response into a typed `cytoscape.Body` value (the shared kube-state-graph `pkg/cytoscape` DTO) containing `apiVersion`, `elements.nodes`, `elements.edges`, and (on node entries) the optional `data.ipaddress` string array used to reconcile the switch graph. The **kube-state-graph** backend wrapper performs **no decoding** — the embedded engine returns a `*cytoscape.Body` directly.

#### Scenario: Switch payload decodes into the shared DTO
- **WHEN** the switch upstream returns `{"apiVersion":"v1","elements":{"nodes":[{"data":{"id":"a","type":"host","ipaddress":["10.0.0.1"]}}],"edges":[]}}`
- **THEN** the switch client returns a `*cytoscape.Body` whose first node `Data.ID == "a"` and `Data.IPAddress == ["10.0.0.1"]`

#### Scenario: Primary returns the engine value without a JSON round-trip
- **WHEN** `KubeStateGraphClient.FetchGraph` completes
- **THEN** the returned `*cytoscape.Body` is the engine's output, not a JSON-decoded copy

### Requirement: Two Built-In Backend Wrappers
The codebase SHALL ship `KubeStateGraphClient` (kube-state-graph primary, **in-process engine adapter**) and `SwitchGraphClient` (out-of-process HTTP), both producing the shared `cytoscape.Body` contract.

#### Scenario: Primary builds in-process, switch hits its own base URL
- **WHEN** `KubeStateGraphClient` is constructed with VictoriaMetrics URL `http://vm:8428` and `SwitchGraphClient` with `baseURL=http://switchsvc:8080`
- **THEN** the primary queries VictoriaMetrics at `http://vm:8428` via the embedded engine, and the switch client POSTs to `http://switchsvc:8080/v1/graph`

#### Scenario: Switch client POSTs a batched IP body
- **WHEN** `SwitchGraphClient.FetchGraphByIPs(ctx, []string{"10.0.0.1", "10.0.0.2"})` is called
- **THEN** the outbound request is `POST <baseURL>/v1/graph` with `Content-Type: application/json` and body `[{"ip":"10.0.0.1"},{"ip":"10.0.0.2"}]`
