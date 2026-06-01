# graph-api-gateway

HTTP gateway that stitches together the **kube-state-graph** and **switch**
Cytoscape backends into a single `/v1/graph` response. The pipeline is
sequential:

1. forward the inbound query to kube-state-graph;
2. extract every `data.ipaddress` from `node`-type entries in the response;
3. if any IPs were collected, call the switch backend as
   `POST /v1/graph` with a batched JSON body `[{"ip":"<a>"},{"ip":"<b>"}]`;
4. re-anchor the switch graph onto kube node IDs by IP match (switch shadow
   nodes are dropped, their edge references rewritten);
5. merge primary + reconciled switch into one Cytoscape envelope (union nodes
   by `data.id`, dedup edges by `(type, source, target)`).

Structured logging is emitted via `log/slog` (JSON by default, text via
`LOG_FORMAT=text`). Inbound requests can be gated by an `X-API-Key` header — see
**Authentication** below.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/graph` | Merged Cytoscape graph (see swagger for query params) |
| GET | `/livez` | Liveness probe — always `200 ok` |
| GET | `/readyz` | Readiness probe — `200 ok` only if both backends are reachable, else `503` |
| GET | `/openapi.yaml` | Embedded OpenAPI 3.1 YAML |
| GET | `/openapi.json` | Embedded OpenAPI 3.1 JSON |
| GET | `/docs/` | Swagger UI — offline, embedded (swaggo/files) |

Backend fetch errors map to:

- `context.DeadlineExceeded` → `504 {"error":"upstream_timeout"}`
- inbound client disconnect → `499` (no body)
- anything else (transport / non-2xx / decode) → `502 {"error":"upstream_unavailable"}`

## Authentication

Inbound auth is **disabled by default**. When `API_KEYS` (or `API_KEYS_FILE`) is
set, every request to `/v1/*` MUST carry an `X-API-Key: <key>` header; a missing
or invalid key returns `401 {"error":"unauthorized"}`. Health probes (`/livez`,
`/readyz`), the OpenAPI spec (`/openapi.*`), and the Swagger UI (`/docs/*`) are
always exempt. Keys are compared in constant time; `API_KEYS_FILE` is
hot-reloaded so a Kubernetes Secret rotation is picked up without a restart.

This is independent of the per-backend `KUBE_STATE_GRAPH_API_KEY` /
`SWITCH_GRAPH_API_KEY`, which are the **outbound** credentials the gateway
presents to its upstreams.

## Configuration

All settings come from the environment.

| Env | Default | Purpose |
|---|---|---|
| `LISTEN_ADDR` | `:8080` | HTTP listen address |
| `LOG_LEVEL` | `info` | `debug` \| `info` \| `warn` \| `error` |
| `LOG_FORMAT` | `json` | `json` \| `text` |
| `KUBE_STATE_GRAPH_URL` | _required_ | kube-state-graph base URL (`http`/`https`, no userinfo/query/fragment) |
| `KUBE_STATE_GRAPH_API_KEY` | `""` | forwarded as `X-API-Key` when non-empty |
| `KUBE_STATE_GRAPH_TIMEOUT` | `10s` | per-call timeout |
| `SWITCH_GRAPH_URL` | _required_ | switch backend base URL (same shape rules) |
| `SWITCH_GRAPH_API_KEY` | `""` | as above |
| `SWITCH_GRAPH_TIMEOUT` | `10s` | as above |
| `API_KEYS` | `""` | comma-separated **inbound** API keys; clients present `X-API-Key`. Empty = auth disabled |
| `API_KEYS_FILE` | `""` | path to a keys file (one per line, `#` comments). Takes precedence over `API_KEYS`; hot-reloaded |
| `API_KEYS_RELOAD_INTERVAL` | `30s` | how often to re-read `API_KEYS_FILE`; `0` disables hot reload |

Backends are named after the upstream domain (not pipeline role) so a future
second-tier switch or alternate kube source can be added without renaming the
existing ones.

## Quick start

```bash
make build           # binary at ./bin/graph-api-gateway
make test            # unit + integration tests, race detector on
make docs            # regenerate docs/swagger.{yaml,json} + embedded copies
make check-docs      # CI-style check that committed docs match the source
make docker-build    # build the distroless container image
make docker-docs     # run the container locally so /docs/ is reachable
```

## Layout

```
cmd/graph-api-gateway      # main entrypoint
internal/api               # gin server, handlers, middleware, auth, swagger UI
internal/auth              # inbound X-API-Key validation (constant-time KeySet)
internal/build             # ldflags-injected version/commit
internal/client            # kube-state-graph + switch backend wrappers
internal/config            # env loading + validation
internal/merge             # graph union helper (pure)
internal/observability     # slog logger wiring
internal/pipeline          # IP extraction + switch ID reconciliation
tools/openapi-postprocess  # swag → OpenAPI renderer example fix-up
```

See `openspec/changes/init-graph-api-gateway/` for the full design rationale.
