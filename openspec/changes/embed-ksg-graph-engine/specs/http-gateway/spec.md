## MODIFIED Requirements

### Requirement: Sequential Graph Pipeline Endpoint
The service SHALL expose `GET /v1/graph` that runs a two-stage sequential pipeline — primary build from the **in-process kube-state-graph engine**, then a switch-backend lookup keyed on IPs extracted from the primary result — and SHALL return a merged Cytoscape.js payload. The primary stage builds the kube graph in-process via `kubegraph.Engine.BuildFromValues` (no HTTP to a kube-state-graph service); the inbound query parameters are applied to that build and are NOT forwarded to the switch backend, which receives only the derived IP set.

#### Scenario: Primary succeeds with IPs, switch succeeds
- **WHEN** a client issues `GET /v1/graph?start=...&end=...`
- **AND** the in-process primary build returns a payload containing at least one `node` or `pod` entry whose `data.ipaddress` is non-empty
- **AND** the switch backend returns `200` for the derived IP query
- **THEN** the gateway responds with HTTP `200`
- **AND** the body matches `{"apiVersion":"v1","elements":{"nodes":[...],"edges":[...]}}`
- **AND** the body contains the union of nodes (first-writer-wins on `data.id`) and edges (deduped by `(type, source, target)`) from both results

#### Scenario: Primary succeeds with no IPs, switch is skipped
- **WHEN** the in-process primary build returns a payload in which no entry has a non-empty `data.ipaddress`
- **THEN** the gateway SHALL NOT issue any HTTP call to the switch backend
- **AND** the gateway responds with HTTP `200`
- **AND** the body contains the primary result unchanged (still wrapped through the merge envelope so the shape is identical to the two-backend success path)

#### Scenario: Primary build fails returns 502
- **WHEN** the in-process primary build returns an error other than a deadline (e.g. an upstream VictoriaMetrics failure)
- **THEN** the gateway responds with HTTP `502`
- **AND** the body matches `{"error":"upstream_unavailable"}`
- **AND** the switch backend SHALL NOT be called

#### Scenario: Primary build times out returns 504
- **WHEN** the in-process primary build exceeds the configured build timeout (`context.DeadlineExceeded`)
- **THEN** the gateway responds with HTTP `504`
- **AND** the body matches `{"error":"upstream_timeout"}`

#### Scenario: Switch fetch fails returns 502
- **WHEN** the primary build succeeds and yields a non-empty IP set
- **AND** the switch backend returns a non-`2xx` response or transport error
- **THEN** the gateway responds with HTTP `502`
- **AND** the body matches `{"error":"upstream_unavailable"}`

#### Scenario: Inbound query is applied to the primary build only
- **WHEN** the gateway receives `GET /v1/graph?cluster=prod&namespace=ns1`
- **THEN** the primary build receives `cluster=prod&namespace=ns1` as the `url.Values` passed to `BuildFromValues`
- **AND** the switch backend (if called) receives ONLY the extracted IPs in its `[{"ip":…}]` POST body, with none of the inbound parameters forwarded

#### Scenario: Switch backend receives the extracted IP set
- **WHEN** the primary build returns entries with `data.ipaddress` values `["10.0.0.1"]`, `["10.0.0.2","10.0.0.3"]`, and `[]`
- **THEN** the gateway issues exactly one `POST /v1/graph` to the switch backend with body `[{"ip":"10.0.0.1"},{"ip":"10.0.0.2"},{"ip":"10.0.0.3"}]` (one object per distinct IP)
- **AND** duplicate IPs appearing across multiple entries are deduplicated before the call

### Requirement: Configuration via Environment
The service SHALL load configuration from environment variables and SHALL fail fast if a required URL is missing or invalid. The required URLs are `VICTORIA_METRICS_URL` (the upstream the embedded engine queries via the Prometheus HTTP API) and `SWITCH_GRAPH_URL`, each parsed as a valid http(s) URL without userinfo / query / fragment. Optional kube-graph knobs: `KSG_METRIC_PREFIX` (metric-name prefix forwarded to `kubegraph.Options.MetricPrefix`) and `KSG_BUILD_TIMEOUT` (in-process build deadline, default `15s`). The legacy `KUBE_STATE_GRAPH_URL` and `KUBE_STATE_GRAPH_API_KEY` keys are **removed**.

#### Scenario: Missing required URL aborts startup
- **WHEN** the binary is launched with `VICTORIA_METRICS_URL` or `SWITCH_GRAPH_URL` unset
- **THEN** the process exits non-zero with stderr naming the missing key

#### Scenario: Legacy kube-state-graph HTTP keys are not consulted
- **WHEN** the binary is launched with only the legacy `KUBE_STATE_GRAPH_URL` set and `VICTORIA_METRICS_URL` unset
- **THEN** startup fails naming `VICTORIA_METRICS_URL` as required (the legacy key is ignored)
