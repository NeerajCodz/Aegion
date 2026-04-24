# Analytics QA & Regression (beta)

This document is the practical test/runbook companion to `docs/analytics/plan.md`.

**Verified head:** `daead5d`  
**Last reviewed:** `2026-04-24`

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

Note: day-to-day work should prioritize feature coverage for the tranche being changed, then widen toward full-module coverage.

## Feature Coverage Map (Current)

- Config validation: `modules/analytics/config_test.go`
- Sync strategies (real-time, batch, async, hybrid): `modules/analytics/sync/*_test.go`
- Retention (policy, tiering, cleanup, archival, sqlite-backed execution): `modules/analytics/retention/*_test.go`
- Webhooks (matching, signing, retry, queue, store, worker): `modules/analytics/webhooks/*_test.go`
- REST API (query parsing, validation, handlers): `modules/analytics/rest/*_test.go`
- GraphQL (schema, resolvers, subscriptions): `modules/analytics/graphql/*_test.go`
- gRPC (service and interceptors): `modules/analytics/grpc/*_test.go`
- Dashboards (query catalog and handlers): `modules/analytics/dashboards/*_test.go`

## Manual Smoke Checklist

Backend:

1. Start Aegion with analytics enabled in `configs/aegion.yaml` or your active environment config.
2. Verify each enabled API surface responds:
   - REST: `GET /api/v1/analytics/events`
   - GraphQL: `POST /graphql` with a basic query
   - gRPC: analytics service round-trip with your internal client or `grpcurl`
3. Verify sync strategy toggles for the environment you are testing:
   - real-time path
   - batch path
   - async path
   - hybrid behavior/fallback
4. Verify retention behavior:
   - hot to warm transitions
   - warm to cold transitions
   - archival artifact creation for the configured backend
5. Verify webhook behavior as implemented:
   - create/update/delete when the REST manager wiring exists
   - event matching and delivery attempts
   - retry behavior on 5xx responses

Admin SPA:

1. Build passes with `npm run build`.
2. Open analytics settings pages for storage, sync, retention, and webhooks.
3. Open overview, dashboards, events, reports, and health pages.
4. Confirm the UI behavior matches the backend functionality that is actually wired on the branch.

## Known Gaps (Verified)

- Integration/E2E tests requiring Postgres plus DuckDB runtime remain explicit skips until the harness/config is supplied.
- GraphQL middleware now enforces token presence for protected operations, but resolver/directive authorization still needs deeper RBAC hardening.
- Iceberg storage now has local warehouse-backed lifecycle coverage, but external catalog integration still needs deeper production verification.
- Several deep-dive docs promised in earlier docs are not present yet; `docs/analytics/README.md` now treats them as pending.

## Docs Acceptance

- `docs/analytics/plan.md` checkbox state must match the repo state at the commit where it is updated.
- `docs/analytics` links should point only to files that actually exist on the branch.
