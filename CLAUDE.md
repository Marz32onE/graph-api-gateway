# graph-api-gateway

HTTP gateway (Gin) that merges two Cytoscape.js graph backends into a single
`GET /v1/graph` response.

## What it does

Per request to `GET /v1/graph`, a **sequential pipeline**:

1. Build the **kube-state-graph** graph **in-process** from the inbound query — an embedded engine (kube-state-graph's public `pkg/kubegraph`) querying VictoriaMetrics directly; no HTTP hop, no JSON round-trip.
2. Extract `data.ipaddress` from every `node`-type entry (deduped, insertion-order preserved).
3. If any IPs: POST them to the **switch backend** batched as `[{"ip":"…"}]` (`POST /v1/graph`).
4. Reconcile switch IDs onto kube node IDs by IP match (drop switch shadow nodes, rewrite their edge endpoints).
5. Merge: union nodes by `data.id` (kube wins), dedup edges by `(type, source, target)`.

Any stage failure → `502`; per-stage timeout → `504`; inbound client cancel → `499`. No partial success.

## Layout

- `cmd/graph-api-gateway/` — entrypoint; loads config, API keys, clients, server. Top-level swag annotations (`@title`, `@securityDefinitions…`) live here.
- `internal/api/` — Gin server, `/v1/graph` + health handlers, middleware (request-id → access log → API-key), and the offline Swagger UI (`swagger.go`) + `/openapi.json` (`docs.go`).
- `internal/auth/` — constant-time `KeySet` + `Validator` for inbound `X-API-Key`.
- `internal/client/` — `KubeStateGraphClient` (in-process `kubegraph.Engine` adapter, primary) and the resty `SwitchGraphClient` (POST IP body); both return the shared `pkg/cytoscape.Body`.
- `internal/pipeline/` — pure `ExtractIPs` + `ReconcileSwitch`.
- `internal/merge/` — pure `Merge`.
- `internal/config/` — env-only config loading + validation.
- `internal/observability/` — slog logger wiring (JSON/text).
- `internal/build/` — ldflags `Version`/`Commit` + `ServiceName`.
- `docs/` — swag-generated OpenAPI spec; the `docs` package is compiled into the binary.

## Conventions & invariants (read before changing behaviour)

- **The kube graph is built in-process** by embedding kube-state-graph's public `pkg/` engine (`go.mod` requires `github.com/marz32one/kube-state-graph`). The gateway uses its `pkg/cytoscape.Body` DTO directly — **no local copy** — and `kubegraph.Engine.BuildFromValues` for the build. `kubegraph.ParseValues` owns the `/v1/graph` query contract, so the gateway adds no parsing of its own. Only the switch backend is a remote HTTP upstream.
- **Config is env-only** (no flags). Required: `VICTORIA_METRICS_URL` (the upstream the embedded engine queries), `SWITCH_GRAPH_URL`. Optional: `KSG_METRIC_PREFIX`, `KSG_BUILD_TIMEOUT` (default `15s`), `SWITCH_GRAPH_API_KEY`/`SWITCH_GRAPH_TIMEOUT`, `LISTEN_ADDR`, `LOG_LEVEL`, `LOG_FORMAT`, and the inbound auth vars below. Full table in `README.md`.
- **Inbound auth is optional and mirrors kube-state-graph.** Set `API_KEYS` (CSV) or `API_KEYS_FILE` (one per line, `#` comments, hot-reloaded every `API_KEYS_RELOAD_INTERVAL`, default `30s`, `0` disables). Clients then send `X-API-Key`; missing/invalid → `401 {"error":"unauthorized"}` (flat envelope). Exempt routes (`openPaths`): `/livez`, `/readyz`, `/openapi.json`, `/docs/*`. Only `/v1/graph` is protected. This inbound credential is **independent** of the outbound `SWITCH_GRAPH_API_KEY` (the kube-state-graph side is in-process and has no outbound credential).
- **No tracing.** Observability is `log/slog` only. There is **no** OpenTelemetry/OTLP; `OTEL_*` env vars are ignored. Do not reintroduce otel/otelgin/otelhttp deps.
- **OpenAPI / docs.** The spec comes from `swag` annotations on handlers, generated into the `docs` package and served at `/openapi.json` from `docs.SwaggerInfo.ReadDoc()` (compiled into the binary — **no `//go:embed`** of spec files). An offline **Swagger UI** (`github.com/swaggo/files/v2`, all assets in-binary) is served at `/docs/`. After editing any handler/annotation, run `make docs` and commit `docs/`; `make check-docs` (CI `docs-drift`) fails on stale docs. Do **not** use `swaggo/gin-swagger` — it is a Swagger-2.0 / UI-4.15.5 toolchain that cannot render OpenAPI 3.1.
- **Pure helpers** (`internal/pipeline`, `internal/merge`) must never mutate inputs — return fresh structs.
- Middleware order: `Recovery → request-id → access log → API-key → routes`.

## Commands

```bash
make build           # ./bin/graph-api-gateway
make test            # go test ./... -count=1 -race -shuffle=on
make lint            # golangci-lint
make vuln            # govulncheck
make ci              # lint + vuln + test + check-docs (mirrors GitHub Actions)
make docs            # regenerate docs/ from swag annotations — commit the result
make check-docs      # fail if committed docs are stale
make docs-preview    # run with placeholder backends so /docs/ is reachable
```

## OpenSpec

The full design rationale lives in `openspec/changes/init-graph-api-gateway/`
(`proposal.md`, `design.md`, `specs/`, `tasks.md`). Keep it in sync when
changing behaviour.
