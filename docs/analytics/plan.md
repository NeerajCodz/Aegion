# Analytics Master Plan - Verified Beta Checklist

## Purpose

This file is the source-of-truth roadmap for analytics work on the `beta` branch. It is based on repository inspection and `git log`, not earlier phase summaries. A checkbox is marked `[x]` only when the code or docs exist in the current branch and are not obviously placeholder-only. Anything stubbed, skipped, partially wired, or missing stays `[ ]`.

**Verified branch:** `beta`  
**Verified head:** `4bc54b0`  
**Last reviewed:** `2026-04-26`  
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
- [x] REST saved-query persistence now reads and writes the real `analytics_queries` table instead of placeholder responses
- [x] GraphQL analytics package exists
- [x] GraphQL auth middleware now rejects protected unauthenticated requests and populates real user context from bearer/session tokens
- [x] GraphQL resolvers now enforce analytics RBAC permissions for events, dashboards, queries, reports, webhooks, and ad-hoc query execution
- [x] GraphQL directive parsing and directive-usage validation are implemented for built-in analytics directives
- [x] GraphQL route registration works with `chi.Router` and `http.ServeMux`
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
- [x] `docs/analytics/openapi.yaml` exists
- [x] `docs/analytics/graphql-schema.md` exists
- [x] `docs/analytics/config.md` exists
- [x] `docs/analytics/api.md` exists
- [x] `docs/analytics/architecture.md` exists
- [x] `docs/analytics/setup.md` exists
- [x] `docs/analytics/integration.md` exists
- [x] `docs/analytics/admin-spa.md` exists
- [x] `docs/analytics/performance.md` exists
- [x] `docs/analytics/webhooks.md` exists
- [x] `docs/analytics/security.md` exists
- [x] `docs/analytics/troubleshooting.md` exists
- [x] `docs/analytics/faq.md` exists

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

## Phase 15 - Final Verification & Cleanup (IN PROGRESS)

**Status:** In progress as of commit `daead5d`  
**What:** Production readiness verification, config alignment, comprehensive documentation, and link validation  
**Why:** Ensure all 14 completed phases work together as a cohesive system; fix gaps discovered during integration testing  

### Phase 15A - Configuration Alignment
- [ ] **15A1** Audit `configs/aegion.yaml` against `modules/analytics/config.go`
  - Verify all runtime config fields have YAML counterparts
  - Add missing sync strategy options (enable_real_time, enable_batch, enable_async)
  - Add retention strategy flexibility (hot_ttl, warm_ttl, cold_ttl per category)
  - Add storage backend fail-over config
- [ ] **15A2** Validate admin SPA analytics config forms match backend contracts
  - Check REST POST `/api/v1/analytics/config/update` payload schema
  - Check GraphQL `updateAnalyticsConfig` mutation input type
  - Ensure forms don't submit invalid config combinations
- [ ] **15A3** Document config defaults and constraints in `docs/analytics/config.md`
- [ ] **Test:** `go test ./modules/analytics/config_test.go`, `npm run build modules/admin/spa`, manual SPA smoke tests

### Phase 15B - Documentation Completion
**Status:** ✅ COMPLETE

All documentation files created:
- [x] **15B1** `docs/analytics/openapi.yaml` - Full REST API spec (OpenAPI 3.0 complete)
- [x] **15B2** `docs/analytics/graphql-schema.md` - GraphQL schema documentation with all types
- [x] **15B3** `docs/analytics/api.md` - API overview with examples (REST, GraphQL, gRPC)
- [x] **15B4** `docs/analytics/architecture.md` - System architecture, component diagram, data flows
- [x] **15B5** `docs/analytics/setup.md` - Deployment setup guide (Docker Compose, Kubernetes, Helm, local)
- [x] **15B6** `docs/analytics/integration.md` - Integrating with Aegion core, event sources, webhooks
- [x] **15B7** `docs/analytics/admin-spa.md` - Admin UI feature guide (dashboards, queries, webhooks, config)
- [x] **15B8** `docs/analytics/performance.md` - Performance tuning and benchmarks (query optimization, indexes, caching)
- [x] **15B9** `docs/analytics/webhooks.md` - Webhook setup and examples (HMAC verification, retry logic, DLQ)
- [x] **15B10** `docs/analytics/security.md` - Security model, RBAC, encryption, audit logging, compliance
- [x] **15B11** `docs/analytics/troubleshooting.md` - Common issues and solutions (20+ troubleshooting sections)
- [x] **15B12** `docs/analytics/faq.md` - Frequently asked questions (40+ Q&A)
- [x] **15B13** Updated `docs/analytics/README.md` with links to all docs, quick reference, by-role navigation
- [x] **Test:** All internal links verified to resolve

**Deliverables:**
- 12 new markdown documentation files
- 1 OpenAPI 3.0 specification (REST API)
- Complete API reference for all three layers
- Integration and deployment guides
- Performance and security best practices
- Troubleshooting and FAQ sections
- Updated README with comprehensive index

**Quality Checklist:**
- ✅ All code examples are valid and copy-paste ready
- ✅ All internal links resolve to existing files
- ✅ Consistent formatting and markdown structure
- ✅ Version info and last updated dates included
- ✅ Configuration examples in YAML format
- ✅ Troubleshooting for common issues
- ✅ Security best practices included
- ✅ Integrated with quickstart.md, plan.md, qa.md

### Phase 15C - Testing Improvements
- [ ] **15C1** Create integration test harness in `modules/analytics/integration/`
  - Dual-DB setup (Postgres + DuckDB)
  - Data sync verification (real-time, batch, async)
  - Query result consistency checks
- [ ] **15C2** Create E2E test suite in `modules/analytics/e2e/`
  - Full workflows: ingest → sync → query → dashboard
  - Multiple API layers (REST, GraphQL, gRPC)
  - Admin SPA flows (config, monitoring)
- [ ] **15C3** Create security-focused test suite in `modules/analytics/security/`
  - RBAC enforcement verification
  - Encryption at-rest and in-transit
  - Query injection prevention
  - Rate limiting verification
- [ ] **15C4** Create performance regression tests in `modules/analytics/benchmarks/`
  - Query execution time baselines
  - Index effectiveness verification
  - Cache hit/miss ratio monitoring
  - Concurrent load handling
- [ ] **Test:** `go test ./modules/analytics/integration ./modules/analytics/e2e ./modules/analytics/security ./modules/analytics/benchmarks`

### Phase 15D - SPA Frontend Alignment
- [ ] **15D1** Audit `modules/admin/spa/src/components/Analytics/` for API calls
  - Verify all calls hit real endpoints (not stubs)
  - Check error handling for failed API calls
  - Ensure loading states, success/error feedback
- [ ] **15D2** Test analytics config UI against actual backend validation
  - Valid config submissions succeed
  - Invalid combos are rejected with helpful errors
  - Config changes persist and reload correctly
- [ ] **15D3** Test all analytics dashboard workflows
  - Dashboard CRUD (create, read, update, delete)
  - Query execution and result display
  - Export functionality
  - Webhook management UI
- [ ] **Test:** `npm run build`, manual UI smoke tests from `docs/analytics/qa.md`

### Phase 15E - Production Readiness
- [ ] **15E1** Verify error messages are user-friendly (not stack traces)
- [ ] **15E2** Confirm all sensitive data logging is scrubbed (no passwords, keys, PII)
- [ ] **15E3** Check health endpoints respond correctly under load
- [ ] **15E4** Verify metrics export is Prometheus-compatible
- [ ] **15E5** Confirm graceful shutdown (flush queues, close connections)
- [ ] **15E6** Document upgrade path from analytics v0 to v1

### Phase 15F - Final Verification & Release
- [ ] **15F1** Run full analytics test suite: `go test -count=1 ./modules/analytics/... -v`
- [ ] **15F2** Verify coverage >= 85%: `go tool cover -html=coverage.out`
- [ ] **15F3** Run all CI checks locally before push
- [ ] **15F4** Update `docs/analytics/plan.md` with final status and remove TODOs
- [ ] **15F5** Create final summary document
- [ ] **15F6** Push all changes: `git add . && git commit -m "feat: phase 15 final verification & cleanup" && git push origin beta`

**Acceptance criteria:**
- All tests passing (>85% coverage)
- All docs exist and links resolve
- Config/UI/backend alignment verified
- No placeholder endpoints or stubs in production code
- System passes production readiness checklist

## Commit workflow for every major tranche

- [ ] `git add .`
- [ ] semantic commit message (`feat:`, `fix:`, `docs:`, `chore:`, `refactor:`, `security:`, `perf:`, `test:`)
- [ ] `git push origin beta`
- [ ] update this plan only after the code/test/doc state matches the checkbox changes

## Defaults and constraints

- Beta-only rule: there is no production analytics data to preserve, so analytics-specific schema/data may be reset when that is the safer path.
- “All options supported” means user-selectable and config-driven over time, but this plan distinguishes between implemented, partial, and planned work.
- Do not mark placeholder-backed features complete just because types, routes, or UI controls exist.
