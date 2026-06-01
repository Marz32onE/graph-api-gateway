## ADDED Requirements

### Requirement: Shared Transport, Per-Backend Request Shape
The codebase SHALL define a `GraphBackend` interface exposing `FetchGraph(ctx, GraphQuery) (*CytoscapeGraph, error)`, satisfied by every backend wrapper. The two upstreams differ in request shape: kube-state-graph is queried via `FetchGraph` (the inbound query forwarded verbatim as `GraphQuery.RawQuery`), while the switch backend is queried via `FetchGraphByIPs(ctx, []string)`, which POSTs every collected IP in one `[{"ip":…}]` JSON body. Backend identity (`"kube-state-graph"` vs `"switch"`) is encoded by concrete type, not by an interface method, and surfaces through per-stage log fields.

#### Scenario: Both built-in clients satisfy the interface
- **WHEN** the codebase is compiled
- **THEN** `KubeStateGraphClient` and `SwitchGraphClient` both satisfy `GraphBackend`

### Requirement: Resty HTTP Client
Each backend wrapper SHALL use a `*resty.Client` backed by a plain `&http.Client{Timeout: …}` with no tracing instrumentation; outbound calls SHALL NOT carry a `traceparent` header.

#### Scenario: No tracing header on outbound calls
- **WHEN** a backend client issues its upstream `GET /v1/graph`
- **THEN** the outbound request carries no `traceparent` header

### Requirement: Configurable Base URL, API Key, and Timeout
Each backend wrapper SHALL be constructed with its own base URL, optional API key (forwarded as `X-API-Key`), and per-call timeout.

#### Scenario: API key is forwarded when configured
- **WHEN** a backend client is constructed with API key `secret-1` and issues an upstream call
- **THEN** the outbound request carries header `X-API-Key: secret-1`

#### Scenario: No API key header when unconfigured
- **WHEN** a backend client is constructed with an empty API key
- **THEN** outbound requests do not carry an `X-API-Key` header

#### Scenario: Timeout aborts a slow upstream
- **WHEN** a backend is configured with a `5s` timeout and the upstream takes longer
- **THEN** the call returns a deadline-exceeded error within `5s`

### Requirement: Cytoscape Response Decoding
Each backend wrapper SHALL decode upstream responses into a typed `CytoscapeGraph` value containing `apiVersion`, `elements.nodes`, `elements.edges`, and (on node entries) the optional `data.ipaddress` string array used by the gateway to derive switch queries.

#### Scenario: Valid payload decodes
- **WHEN** the upstream returns `{"apiVersion":"v1","elements":{"nodes":[{"data":{"id":"a"}}],"edges":[]}}`
- **THEN** the client returns a `*CytoscapeGraph` whose `Elements.Nodes[0].Data.ID == "a"`

#### Scenario: ipaddress array decodes onto NodeData
- **WHEN** the upstream returns `{"apiVersion":"v1","elements":{"nodes":[{"data":{"id":"a","type":"node","ipaddress":["10.0.0.1","10.0.0.2"]}}],"edges":[]}}`
- **THEN** the client returns `*CytoscapeGraph` whose `Elements.Nodes[0].Data.IPAddress == ["10.0.0.1","10.0.0.2"]`

#### Scenario: Missing ipaddress decodes as empty/nil
- **WHEN** the upstream returns a node entry whose `data` field has no `ipaddress` key
- **THEN** that node's `Data.IPAddress` is `nil` or empty (never panics on access)

### Requirement: Two Built-In Backend Wrappers
The codebase SHALL ship `KubeStateGraphClient` (kube-state-graph primary) and `SwitchGraphClient`, both targeting a `/v1/graph` Cytoscape contract, each with an independently injected base URL.

#### Scenario: Each client uses its own base URL
- **WHEN** `KubeStateGraphClient` is constructed with `baseURL=http://ksg:8080` and `SwitchGraphClient` with `baseURL=http://switchsvc:8080`
- **THEN** outbound calls hit `http://ksg:8080/v1/graph` and `http://switchsvc:8080/v1/graph` respectively

#### Scenario: Switch client POSTs a batched IP body
- **WHEN** `SwitchGraphClient.FetchGraphByIPs(ctx, []string{"10.0.0.1", "10.0.0.2"})` is called
- **THEN** the outbound request is `POST <baseURL>/v1/graph` with `Content-Type: application/json` and body `[{"ip":"10.0.0.1"},{"ip":"10.0.0.2"}]`
