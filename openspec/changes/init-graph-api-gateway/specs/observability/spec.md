## ADDED Requirements

### Requirement: Structured Logging with slog
The service SHALL use `log/slog` as its sole logging facade, with a JSON handler by default and a text handler when `LOG_FORMAT=text`, and SHALL respect `LOG_LEVEL` (`debug|info|warn|error`).

#### Scenario: Default JSON output
- **WHEN** the service starts with no `LOG_FORMAT` env set
- **THEN** log lines are valid JSON with at least `time`, `level`, `msg` fields

#### Scenario: Text format override
- **WHEN** the service is launched with `LOG_FORMAT=text`
- **THEN** log lines are in slog's text key=value format

### Requirement: Per-Request Access Log
Every non-health request SHALL emit one structured access-log record carrying at least `method`, `path`, `status`, `duration_ms`, and `request_id`. Successful (`2xx`) probes to `/livez` and `/readyz` SHALL be suppressed to avoid drowning the log.

#### Scenario: Access log carries the request id
- **WHEN** a client issues `GET /v1/graph` with header `X-Request-ID: abc-123`
- **THEN** the emitted access-log record contains `request_id=abc-123`

#### Scenario: Health probes are quiet on success
- **WHEN** a client issues `GET /livez` and the response is `200`
- **THEN** no access-log record is emitted for that request

### Requirement: No Tracing Backend
The service SHALL NOT emit OpenTelemetry/OTLP traces. It carries no tracing SDK, exporter, or otel HTTP instrumentation; `OTEL_*` environment variables have no effect on the process.

#### Scenario: OTLP endpoint is ignored
- **WHEN** the binary is launched with `OTEL_EXPORTER_OTLP_ENDPOINT` set to any value
- **THEN** the service starts successfully
- **AND** no OTLP network calls are issued for inbound or outbound requests
- **AND** log records carry no `trace_id` or `span_id` fields
