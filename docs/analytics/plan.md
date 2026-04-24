# Analytics Master Plan - Verified Beta Checklist

## Purpose

This file is the source-of-truth roadmap for analytics work on the `beta` branch. It is based on repository inspection and `git log`, not earlier phase summaries. A checkbox is marked `[x]` only when the code or docs exist in the current branch and are not obviously placeholder-only. Anything stubbed, skipped, partially wired, or missing stays `[ ]`.

**Verified branch:** `beta`  
**Verified head:** `daead5d`  
**Last reviewed:** `2026-04-24`  
**Primary verification inputs:** `modules/analytics`, `modules/admin/spa/src/components/Analytics`, `configs/aegion.yaml`, `docs/analytics`, recent `git log`

## Verification rules

- `[x]` means code exists, is wired enough to be considered shipped on `beta`, and is not contradicted by obvious `NOT_IMPLEMENTED`, `Unimplemented`, `TODO`, stub, or permanent skip markers.
- `[ ]` means any of the following:
  - not implemented,
  - only modeled in config/types,
  - exposed in UI but not backed by working handlers,
  - guarded by explicit skipped test harnesses,
  - documented but the promised file/spec does not exist.
- For each major tranche, done means:
  - code merged locally,
  - focused tests run,
  - broader analytics verification run as appropriate,
  - docs updated,
  - committed and pushed to `origin/beta`.

## Verified shipped on `beta`

### Analytics foundation
- [x] Analytics module structure exists under `modules/analytics`
- [x] DuckDB-backed analytics store exists
- [x] Analytics migrations exist under `modules/analytics/migrations`
- [x] Admin SPA analytics routes/pages exist under `modules/admin/spa/src`
- [x] Analytics CI workflow exists
- [x] Recent security/perf/test milestones are present in `git log`

### Sync and ingestion
- [x] Real-time sync implementation exists
- [x] Batch sync implementation exists
- [x] Async sync implementation exists
- [x] Hybrid sync implementation exists
- [x] Sync strategy tests exist

### Storage and retention
- [x] Local storage backend exists
- [x] S3 storage backend exists
- [x] Storage type modeling exists for `local`, `s3`, `iceberg`, and `k8s`
- [x] Iceberg storage now supports local warehouse-backed read, write, list, delete, and health operations
- [x] Retention policy, tiering, cleanup, and archival code exist
- [x] Retention tests exist, including sqlite-backed execution coverage

### API and UI surface
- [x] REST analytics package exists
- [x] REST webhook handlers are wired to the webhook manager when the manager dependency is configured
- [x] REST dashboard CRUD now reads and writes the real `analytics_dashboards` table instead of placeholder responses
- [x] GraphQL analytics package exists
- [x] GraphQL auth middleware now rejects protected unauthenticated requests and populates real user context from bearer/session tokens
- [x] GraphQL resolvers now enforce analytics RBAC permissions for events, dashboards, queries, reports, webhooks, and ad-hoc query execution
- [x] gRPC analytics package exists with generated/internal proto
- [x] gRPC saved-query execution resolves and executes stored read-only SQL
- [x] Dashboard query exports now resolve real stored/common queries with time-range and filter composition
- [x] Dashboard persistence now round-trips real JSON config, pinned state, and cached query metadata without placeholder helpers
- [x] Dashboard, event, report, health, and config screens exist in the Admin SPA
- [x] SPA client methods exist for storage, sync, retention, dashboards, reports, events, and webhooks

### Current docs
- [x] `docs/analytics/plan.md` exists as the execution tracker
- [x] `docs/analytics/qa.md` exists as the regression companion
- [x] `docs/analytics/quickstart.md` exists
- [x] `docs/analytics/README.md` exists

## Partial or incomplete on `beta`

### Backend gaps confirmed in code
- [ ] Iceberg storage external catalog integration is production-ready across deployed catalog backends

### Config and contract alignment
- [ ] `configs/aegion.yaml` matches the richer runtime analytics config model in `modules/analytics/config.go`
- [ ] `configs/aegion.yaml` exposes per-strategy sync settings with the same shape used by runtime validation
- [ ] `configs/aegion.yaml` exposes storage and retention flexibility at the same fidelity as runtime config
- [ ] Admin SPA config forms match the backend contract actually enforced by the runtime
- [ ] Config docs clearly distinguish implemented vs modeled-only options

### Test and verification gaps
- [ ] Integration tests run by default in CI with a real Postgres/DuckDB harness
- [ ] E2E analytics workflows run by default in CI with a real harness
- [ ] Performance benchmarks are part of normal regression verification
- [ ] Security-specific analytics tests are part of normal regression verification
- [ ] Link validation exists for `docs/analytics`

### Documentation gaps
- [ ] `docs/analytics/openapi.yaml` exists
- [ ] `docs/analytics/graphql-schema.md` exists
- [ ] `docs/analytics/config.md` exists
- [ ] `docs/analytics/api.md` exists
- [ ] `docs/analytics/architecture.md` exists
- [ ] `docs/analytics/setup.md` exists
- [ ] `docs/analytics/integration.md` exists
- [ ] `docs/analytics/admin-spa.md` exists
- [ ] `docs/analytics/performance.md` exists
- [ ] `docs/analytics/webhooks.md` exists
- [ ] `docs/analytics/security.md` exists
- [ ] `docs/analytics/troubleshooting.md` exists
- [ ] `docs/analytics/faq.md` exists

## Execution tranches

### Tranche A - Docs truth reset
- [x] Rewrite this plan to reflect the actual `beta` branch state
- [x] Separate shipped vs partial vs missing work
- [x] Stop claiming missing docs/specs already exist
- [ ] Add automated doc-link verification to the project toolchain

**Done criteria**
- Plan checked against current head
- README/quickstart links only point to files that exist, or clearly call out pending docs
- Commit/push completed

### Tranche B - Config and contract alignment
- [ ] Align `configs/aegion.yaml` with `modules/analytics/config.go`
- [ ] Align Admin SPA analytics config payloads with backend/runtime contracts
- [ ] Add or extend validation coverage for the aligned config shape
- [ ] Update docs and examples after contract alignment lands

**Focused verification**
- `go test ./modules/analytics/...`
- `npm run build` in `modules/admin/spa`

### Tranche C - Remaining backend gaps
- [x] Finish REST webhook CRUD/test/history/replay wiring
- [x] Replace GraphQL placeholder auth/token parsing with real Aegion auth integration
- [x] Enforce analytics RBAC consistently in GraphQL
- [x] Complete gRPC saved-query execution
- [x] Replace Iceberg stub behavior with a real local implementation and focused tests
- [ ] Deepen Iceberg external catalog integration and production verification

**Focused verification**
- `go test ./modules/analytics/rest`
- `go test ./modules/analytics/graphql`
- `go test ./modules/analytics/grpc`
- `go test ./modules/analytics/webhooks`
- `go test ./modules/analytics/store`
- `go test ./modules/analytics/dashboards`

### Tranche D - Frontend and docs completion
- [ ] Remove or disable SPA controls that call placeholder endpoints
- [ ] Reconcile SPA analytics flows with real backend behavior
- [ ] Add missing docs only after the corresponding slice is real
- [ ] Keep `docs/analytics/qa.md` synchronized with the real verification path

**Focused verification**
- `npm run build` in `modules/admin/spa`
- Manual smoke checks from `docs/analytics/qa.md`

### Tranche E - Full verification and release discipline
- [ ] Run focused tests for each change set before broader sweeps
- [ ] Run broader analytics module verification after each major tranche
- [ ] Update this plan and `docs/analytics/qa.md` in the same change set
- [ ] Commit and push every major tranche to `origin/beta`

**Broader verification target**
- `go test -count=1 ./modules/analytics/grpc ./modules/analytics/dashboards ./modules/analytics/rest ./modules/analytics/graphql ./modules/analytics/integration ./modules/analytics/e2e ./modules/analytics/store ./modules/analytics/retention ./modules/analytics/sync ./modules/analytics/webhooks`
- `npm run build` in `modules/admin/spa`

## Concrete acceptance gates

### Backend
- [x] Webhook REST endpoints stop returning `NOT_IMPLEMENTED`
- [x] GraphQL rejects bad auth and sets a real identity context
- [x] GraphQL authorization is no longer placeholder-only
- [x] gRPC query execution returns real results instead of `Unimplemented`
- [x] Iceberg support is functional locally with meaningful tests
- [ ] Iceberg support is fully production-ready for external catalog deployments

### Configuration and UI
- [ ] Analytics config in `aegion.yaml`, runtime validation, and Admin SPA forms all agree on the same contract
- [ ] The SPA does not present successful UX for endpoints that are still placeholder-only

### Tests
- [ ] Integration/E2E coverage no longer depends on permanent skip paths once harness/config is supplied
- [ ] Performance and security verification are represented by runnable analytics-specific suites or documented deferred tasks

### Docs
- [ ] Every link in `docs/analytics` resolves to an existing file
- [ ] This plan matches the repo state at the commit where it is updated

## Commit workflow for every major tranche

- [ ] `git add .`
- [ ] semantic commit message (`feat:`, `fix:`, `docs:`, `chore:`, `refactor:`, `security:`, `perf:`, `test:`)
- [ ] `git push origin beta`
- [ ] update this plan only after the code/test/doc state matches the checkbox changes

## Defaults and constraints

- Beta-only rule: there is no production analytics data to preserve, so analytics-specific schema/data may be reset when that is the safer path.
- “All options supported” means user-selectable and config-driven over time, but this plan distinguishes between implemented, partial, and planned work.
- Do not mark placeholder-backed features complete just because types, routes, or UI controls exist.
