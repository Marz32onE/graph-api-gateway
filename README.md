# graph-api-gateway

HTTP gateway that stitches together the **kube-state-graph** and **switch**
Cytoscape backends into a single `/v1/graph` response. The pipeline is
sequential:

1. forward the inbound query to kube-state-graph;
2. extract every `data.ipaddress` from `node`-type entries in the response;
3. if any IPs were collected, call the switch backend as
   `GET /v1/graph?ip=<a>&ip=<b>…`;
4. re-anchor the switch graph onto kube node IDs by IP match (switch shadow
   nodes are dropped, their edge references rewritten);
5. merge primary + reconciled switch into one Cytoscape envelope (union nodes
   by `data.id`, dedup edges by `(type, source, target)`).

End-to-end W3C trace context is propagated; structured logs (slog) auto-inject
`trace_id` / `span_id` when emitted inside a span.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/graph` | Merged Cytoscape graph (see swagger for query params) |
| GET | `/livez` | Liveness probe — always `200 ok` |
| GET | `/readyz` | Readiness probe — `200 ok` only if both backends are reachable, else `503` |
| GET | `/openapi.yaml` | Embedded OpenAPI 3.1 YAML |
| GET | `/openapi.json` | Embedded OpenAPI 3.1 JSON |
| GET | `/docs` | Scalar API Reference UI |

Backend fetch errors map to:

- `context.DeadlineExceeded` → `504 {"error":"upstream_timeout"}`
- inbound client disconnect → `499` (no body)
- anything else (transport / non-2xx / decode) → `502 {"error":"upstream_unavailable"}`

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
| `OTEL_EXPORTER_OTLP_ENDPOINT` | _unset_ | enable OTLP/HTTP exporter; when unset, tracing runs in noop mode |
| `OTEL_*` | stdlib OTel env | standard OTLP exporter config |

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
make docker-docs     # run the container locally so /docs is reachable
```

Local end-to-end rig with stub backends + OTel collector:

```bash
docker compose -f local/docker-compose.yaml up
```

## Layout

```
cmd/graph-api-gateway      # main entrypoint
internal/api               # gin server, handlers, middleware, swagger handlers
internal/build             # ldflags-injected version/commit
internal/client            # kube-state-graph + switch backend wrappers
internal/config            # env loading + validation
internal/merge             # graph union helper (pure)
internal/observability     # OTel + slog wiring
internal/pipeline          # IP extraction + switch ID reconciliation
tools/openapi-postprocess  # swag → Scalar example fix-up
```

See `openspec/changes/init-graph-api-gateway/` for the full design rationale.
