# Aegion observability

Aegion emits operational and security events as Loza wide events. Loza is the sole application event pipeline; applications do not write operational records to analytics DuckDB, stdout JSON, Loki, or an OTLP logs exporter.

## Architecture

```text
Aegion core and modules
        │ authenticated, batched Loza events
        ▼
Loza Collector (:9308)
        │ validation, redaction, deduplication, durability, retention
        ▼
Collector-owned DuckDB
        │
        └── LQL query API / CLI
```

Traces and metrics remain separate signals. Loza events carry correlation fields such as `event_id`, `trace_id`, `request_id`, `service`, `environment`, `method`, `path`, `status_code`, `outcome`, and `duration_ms`.

## Event lifecycle

Each request, job, workflow, message, security action, or audit operation creates one lifecycle event, enriches it while work executes, and emits it once the outcome is known. Success and failure outcomes are explicit. Errors are structured (`error.type`, `error.message`, `error.stack`, and optional cause fields), never flattened into a free-form log line.

Event kinds:

- `request`: HTTP, RPC, and API boundaries
- `job`: background work
- `workflow`: multi-step operations
- `message`: queue processing
- `system`: startup, health, and shutdown
- `security`: authentication and authorization decisions
- `audit`: compliance-sensitive administrative actions

Sensitive values are never event attributes. Do not set passwords, API keys, bearer tokens, authorization headers, cookies, session values, raw bodies, email addresses, or client IPs. Use identifiers or approved hashes where correlation is required.

## Configuration

Required process settings:

```bash
AEGION_LOZA_COLLECTOR_URL=http://loza-collector:9308/events
AEGION_LOZA_API_KEY_FILE=/run/secrets/loza_ingest_key
```

Production requires an HTTPS collector URL and an API key supplied through a secret file. Development may use the local memory event bus and an explicitly configured insecure HTTP endpoint.

Collector configuration must enable authentication, validation, redaction, size limits, deduplication, and the DuckDB primary exporter. The collector owns retention, deletion, and query behavior.

## Local stack

The base Compose stack includes a Loza collector. The development overlay adds the normal metrics, tracing, and dashboard services:

```bash
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.dev.yml up -d
```

Local Loza endpoints:

- ingest: `http://localhost:9308/events`
- health: `http://localhost:9308/health`
- readiness: `http://localhost:9308/readyz`

Use the collector's LQL query interface or CLI for event investigation. Analytics domain tables remain in the analytics database and are queried through analytics APIs; they are not redirected into Loza.

## Operations

Check collector health and readiness before investigating application behavior. Query by `request_id`, `trace_id`, `service`, `event`, `outcome`, and time range. For slow operations, use `duration_ms`; for failures, use structured error fields. Event delivery failures must be visible as collector delivery failures, not hidden by a stdout fallback.

The Admin observability endpoint reports configured infrastructure health and the Loza collector health URL. It does not expose credentials or event payloads.

## Deployment requirements

- Run one authenticated Loza collector deployment per environment or an approved shared collector.
- Persist the collector DuckDB volume and protect it as operational telemetry data.
- Supply ingest keys and collector secrets through secret files or a secret manager.
- Use HTTPS for production application-to-collector traffic.
- Keep collector and SDK versions pinned and upgrade them together.
- Do not reintroduce Loki, OTLP log export, analytics xlog RPC, or stdout fallback.

## Troubleshooting

1. Check `GET /health` and `GET /readyz` on the collector.
2. Verify `AEGION_LOZA_COLLECTOR_URL` includes `/events`.
3. Verify the API key file is readable by the application user and matches the collector ingest key.
4. Inspect collector delivery and validation failures through its operational interface.
5. Query by `trace_id` or `request_id`; avoid searching raw bodies or secrets.
