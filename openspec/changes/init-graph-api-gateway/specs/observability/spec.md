## ADDED Requirements

### Requirement: Inbound OTLP Tracing
The service SHALL register `otelgin` middleware on the Gin router so every inbound request opens a server span that honours any incoming W3C `traceparent` and emits the span via the OTLP/HTTP exporter.

#### Scenario: Server span inherits inbound trace
- **WHEN** a client sends `GET /v1/graph` with header `traceparent: 00-<trace>-<span>-01`
- **THEN** the exported server span's trace ID equals `<trace>`
- **AND** the server span's parent span ID equals `<span>`

### Requirement: Outbound OTLP Tracing
Backend calls SHALL be issued through an `otelhttp`-instrumented transport (wrapped under resty) so each upstream call opens a client span and injects `traceparent` outbound.

#### Scenario: Client span chains under server span
- **WHEN** an inbound `GET /v1/graph` is being served
- **AND** the backend client issues its upstream call
- **THEN** the exported client span's parent is the server span for that request

### Requirement: Zero-Overhead When OTLP Disabled
When `OTEL_EXPORTER_OTLP_ENDPOINT` is unset, the service SHALL install a no-op tracer provider so neither inbound nor outbound paths attempt to export spans.

#### Scenario: No exporter is configured
- **WHEN** the binary is launched with `OTEL_EXPORTER_OTLP_ENDPOINT` unset
- **THEN** the service starts successfully
- **AND** no OTLP network calls are issued for inbound requests

### Requirement: Structured Logging with slog
The service SHALL use `log/slog` as its sole logging facade, with a JSON handler by default and a text handler when `LOG_FORMAT=text`, and SHALL respect `LOG_LEVEL` (`debug|info|warn|error`).

#### Scenario: Default JSON output
- **WHEN** the service starts with no `LOG_FORMAT` env set
- **THEN** log lines are valid JSON with at least `time`, `level`, `msg` fields

#### Scenario: Text format override
- **WHEN** the service is launched with `LOG_FORMAT=text`
- **THEN** log lines are in slog's text key=value format

### Requirement: Trace-Correlated Log Records
Log records emitted with a request context SHALL carry `trace_id` and `span_id` attributes derived from the active OTel span when one is present.

#### Scenario: Log inside a traced request
- **WHEN** a handler calls `slog.InfoContext(ctx, "msg")` inside a request whose span context is valid
- **THEN** the emitted log record contains string attributes `trace_id` and `span_id` matching the active span

#### Scenario: Log outside a trace
- **WHEN** `slog.Info("msg")` is called with no active span in context
- **THEN** the emitted log record contains neither `trace_id` nor `span_id`
