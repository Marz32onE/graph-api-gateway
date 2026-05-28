## ADDED Requirements

### Requirement: Shared Backend Interface
The codebase SHALL define a single `GraphBackend` interface exposing `FetchGraph(ctx, GraphQuery) (*CytoscapeGraph, error)`, implemented by every backend wrapper. The interface is query-shape agnostic: the gateway builds `GraphQuery.RawQuery` differently per backend (kube-state-graph receives the inbound query verbatim; switch receives `ip=…&ip=…`). Backend identity (`"kube-state-graph"` vs `"switch"`) is encoded by concrete type, not by an interface method, and surfaces through the OTel transport's span attributes and per-stage log fields.

#### Scenario: Both built-in clients satisfy the interface
- **WHEN** the codebase is compiled
- **THEN** `KubeStateGraphClient` and `SwitchGraphClient` both satisfy `GraphBackend`

### Requirement: Resty HTTP Client with OTel Transport
Each backend wrapper SHALL use a `*resty.Client` backed by an `otelhttp`-instrumented `http.Transport` so outbound calls automatically inject W3C `traceparent`.

#### Scenario: Outbound call propagates trace context
- **WHEN** an inbound request carries `traceparent: 00-<trace>-<span>-01`
- **AND** a backend client issues its upstream `GET /v1/graph` within that request's context
- **THEN** the outbound request carries a `traceparent` whose trace ID matches the inbound trace ID

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

#### Scenario: Switch client accepts an ip= query
- **WHEN** `SwitchGraphClient.FetchGraph(ctx, GraphQuery{RawQuery: "ip=10.0.0.1&ip=10.0.0.2"})` is called
- **THEN** the outbound request line is `GET <baseURL>/v1/graph?ip=10.0.0.1&ip=10.0.0.2`
