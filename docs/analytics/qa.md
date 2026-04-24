# Analytics QA & Regression (beta)

This document is the practical test/runbook companion to `docs/analytics/plan.md`.

## Automated Regression

Backend (analytics module matrix):

```bash
go test -count=1 ./modules/analytics/grpc \
  ./modules/analytics/dashboards \
  ./modules/analytics/rest \
  ./modules/analytics/graphql \
  ./modules/analytics/integration \
  ./modules/analytics/e2e \
  ./modules/analytics/store \
  ./modules/analytics/retention \
  ./modules/analytics/sync \
  ./modules/analytics/webhooks
```

Frontend (Admin SPA):

```bash
cd modules/admin/spa
npm run build
```

## Coverage (Statement Coverage)

Overall analytics coverage baseline:

```bash
go test -count=1 -covermode=atomic -coverprofile=coverage_analytics ./modules/analytics/...
go tool cover -func=coverage_analytics | tail -n 1
```

PowerShell equivalent:

```powershell
go test -count=1 -covermode=atomic -coverprofile=coverage_analytics ./modules/analytics/...
go tool cover -func=coverage_analytics | Select-Object -Last 1
```

Note: for day-to-day work we target “feature coverage” (tests exist for each plan feature) first, then drive statement coverage upward iteratively.

## Feature Coverage Map (Current)

This is a living mapping from major plan features to concrete tests. Add to it whenever a feature is implemented or a test is added.

- Config validation: `modules/analytics/config_test.go`
- Sync strategies (real-time/batch/async + helpers): `modules/analytics/sync/*_test.go`
- Retention (policy/tiering/cleanup/archival + sqlite-backed execution): `modules/analytics/retention/*_test.go`
- Webhooks (matching/signing/retry/queue/store/worker): `modules/analytics/webhooks/*_test.go`
- REST API (query parsing/validation/handlers): `modules/analytics/rest/*_test.go`
- GraphQL (schema/resolvers/subscriptions): `modules/analytics/graphql/*_test.go`
- gRPC (service + interceptors): `modules/analytics/grpc/*_test.go`
- Dashboards (query catalog + handlers): `modules/analytics/dashboards/*_test.go`

## Manual Smoke Checklist (95%+ Feature Coverage Target)

These are the human “does it work end-to-end?” checks that complement unit tests.

Backend:

1. Start Aegion with analytics enabled in `docs/aegion.yaml` (or your env’s `aegion.yaml`).
2. Verify each API surface responds when enabled and rejects when disabled:
   - REST: `GET /api/v1/analytics/events`
   - GraphQL: `POST /graphql` basic query
   - gRPC: `QueryEvents` round-trip (via grpcurl or internal client)
3. Verify sync strategy toggles:
   - real-time enabled: new events appear without manual batch trigger
   - batch enabled: scheduled job runs (or trigger endpoint/command if present)
   - async enabled: enqueue path works and workers drain the queue
4. Verify retention tiering:
   - hot -> warm transitions happen for stale data
   - warm -> cold transitions happen for older data
   - archival writes an external artifact (backend-dependent)
5. Verify webhooks:
   - create webhook with filters
   - emit an event that matches
   - delivery is attempted, retries happen on 5xx, DLQ on terminal failure

Admin SPA:

1. Build passes (`npm run build`).
2. Open analytics settings UI:
   - enable/disable analytics module
   - choose storage backend (local/S3/Iceberg/K8s)
   - choose sync strategies (real-time, batch, async, hybrid)
   - configure retention tiers + per-category overrides
3. Dashboards:
   - open pre-built dashboards
   - create a custom dashboard (builder)
   - verify “refresh” / auto-refresh behavior
4. Webhooks:
   - create webhook
   - update webhook
   - delete/disable webhook
   - verify delivery history UI when wired

## Known Gaps (Planned)

- Integration/E2E tests requiring Postgres + DuckDB runtime (currently explicit skips).
- Benchmarks/load/security-specific tests (planned in plan Phases 10/12/14).

