## ADDED Requirements

### Requirement: HTTP Server
The service SHALL start a Gin HTTP server bound to a configurable address (default `:8080`).

#### Scenario: Server binds to configured address
- **WHEN** the binary is launched with `LISTEN_ADDR=:9090`
- **THEN** the server accepts HTTP connections on TCP port `9090`

### Requirement: Sequential Graph Pipeline Endpoint
The service SHALL expose `GET /v1/graph` that runs a two-stage sequential pipeline — primary fetch from kube-state-graph, then switch-backend lookup keyed on IPs extracted from the primary response — and SHALL return a merged Cytoscape.js payload.

#### Scenario: Primary succeeds with IPs, switch succeeds
- **WHEN** a client issues `GET /v1/graph?start=...&end=...`
- **AND** the primary backend (kube-state-graph) returns `200` with a payload containing at least one `node` or `pod` entry whose `data.ipaddress` is non-empty
- **AND** the switch backend returns `200` for the derived IP query
- **THEN** the gateway responds with HTTP `200`
- **AND** the body matches `{"apiVersion":"v1","elements":{"nodes":[...],"edges":[...]}}`
- **AND** the body contains the union of nodes (first-writer-wins on `data.id`) and edges (deduped by `(type, source, target)`) from both responses

#### Scenario: Primary succeeds with no IPs, switch is skipped
- **WHEN** the primary backend returns `200` and no entry in the response has a non-empty `data.ipaddress`
- **THEN** the gateway SHALL NOT issue any HTTP call to the switch backend
- **AND** the gateway responds with HTTP `200`
- **AND** the body contains the primary response unchanged (still wrapped through the merge envelope so the shape is identical to the two-backend success path)

#### Scenario: Primary fetch fails returns 502
- **WHEN** the primary backend returns a non-`2xx` response or transport error
- **THEN** the gateway responds with HTTP `502`
- **AND** the body matches `{"error":"upstream_unavailable"}`
- **AND** the switch backend SHALL NOT be called

#### Scenario: Switch fetch fails returns 502
- **WHEN** the primary backend succeeds and yields a non-empty IP set
- **AND** the switch backend returns a non-`2xx` response or transport error
- **THEN** the gateway responds with HTTP `502`
- **AND** the body matches `{"error":"upstream_unavailable"}`

#### Scenario: Inbound query string is forwarded to primary only
- **WHEN** the gateway receives `GET /v1/graph?cluster=prod&namespace=ns1`
- **THEN** the primary backend receives query string `cluster=prod&namespace=ns1` verbatim
- **AND** the switch backend (if called) receives ONLY `ip=<ip>` parameters derived from the primary response, with none of the inbound parameters forwarded

#### Scenario: Switch backend receives the extracted IP set
- **WHEN** the primary returns entries with `data.ipaddress` values `["10.0.0.1"]`, `["10.0.0.2","10.0.0.3"]`, and `[]`
- **THEN** the gateway issues exactly one `GET` to the switch backend with query string containing one `ip=` parameter per distinct IP (`ip=10.0.0.1&ip=10.0.0.2&ip=10.0.0.3`)
- **AND** duplicate IPs appearing across multiple entries are deduplicated before the call

### Requirement: Health Endpoints
The service SHALL expose `GET /livez` and `GET /readyz` returning HTTP `200` with body `ok` whenever the process is running.

#### Scenario: Liveness probe
- **WHEN** a client issues `GET /livez`
- **THEN** the response is HTTP `200` with body `ok`

#### Scenario: Readiness probe
- **WHEN** a client issues `GET /readyz`
- **THEN** the response is HTTP `200` with body `ok`

### Requirement: OpenAPI Documentation Routes
The service SHALL serve its generated OpenAPI 3.1 spec and a Scalar API Reference UI at fixed routes without authentication.

#### Scenario: OpenAPI YAML is served
- **WHEN** a client issues `GET /openapi.yaml`
- **THEN** the response is HTTP `200` with the embedded YAML body

#### Scenario: OpenAPI JSON is served
- **WHEN** a client issues `GET /openapi.json`
- **THEN** the response is HTTP `200` with the embedded JSON body

#### Scenario: Scalar UI is served
- **WHEN** a client issues `GET /docs`
- **THEN** the response is HTTP `200` with an HTML body that loads Scalar and points at `/openapi.json`

### Requirement: Request ID Middleware
Every inbound request SHALL be tagged with a request ID following the kube-state-graph pattern: honour an inbound `X-Request-ID` header when present, otherwise generate a UUIDv4; the ID SHALL be placed in the gin context under key `request_id`, echoed back as the `X-Request-ID` response header, and included as a `request_id` field in every access log record for that request.

#### Scenario: Inbound header is honoured
- **WHEN** a client issues a request with header `X-Request-ID: abc-123`
- **THEN** the response carries header `X-Request-ID: abc-123`
- **AND** any access log line for that request contains `request_id=abc-123`

#### Scenario: Missing header is generated
- **WHEN** a client issues a request without an `X-Request-ID` header
- **THEN** the response carries an `X-Request-ID` header whose value is a UUIDv4
- **AND** any access log line for that request contains `request_id=<that uuid>`

### Requirement: Configuration via Environment
The service SHALL load configuration from environment variables and SHALL fail fast if either backend URL is missing.

#### Scenario: Missing backend URL aborts startup
- **WHEN** the binary is launched with `KUBE_STATE_GRAPH_URL` or `SWITCH_GRAPH_URL` unset
- **THEN** the process exits non-zero with stderr naming the missing key
