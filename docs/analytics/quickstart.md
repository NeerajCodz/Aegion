# Quick Start Guide - Aegion Analytics

This quickstart is a beta-branch bootstrap guide. It is intentionally limited to workflows and docs that are present in this repository today; deeper API/spec documentation is still tracked in `docs/analytics/plan.md`.

## Prerequisites

- **Docker & Docker Compose** or a local Go/PostgreSQL development environment
- **curl** or another HTTP client
- **Terminal/Shell** access
- **4GB RAM** minimum for development

## Fast local bootstrap

### Step 1: Clone the repository

```bash
git clone https://github.com/Astraive/Aegion.git
cd Aegion
```

### Step 2: Start the local stack

```bash
docker-compose up -d
```

This is expected to start the project services needed for a local beta environment, including PostgreSQL, Redis, Aegion, and the analytics module surfaces currently wired in the repo.

### Step 3: Bootstrap an admin operator

```bash
docker-compose exec aegion aegion bootstrap --admin-email admin@example.com
```

Save the generated password from the command output.

### Step 4: Open the admin UI

- **Admin Panel**: http://localhost:8080/aegion
- **Analytics Area**: http://localhost:8080/aegion/analytics

### Step 5: Make a first analytics request

```bash
export AEGION_TOKEN="your-bearer-token-here"

curl -H "Authorization: Bearer $AEGION_TOKEN" \
  http://localhost:8080/api/v1/analytics/events
```

This confirms the beta analytics surfaces are reachable in your environment.

## What is currently documented here

- [Master plan](plan.md) - shipped vs partial vs pending
- [QA runbook](qa.md) - regression commands and manual smoke checks
- [Docs index](README.md) - current inventory for this folder

## Concepts at a glance

### Events

Analytics centers around structured events with categories, event types, timestamps, and payload data.

### Dashboards

The Admin SPA includes analytics routes for overview, dashboards, events, reports, health, and configuration. Some screens are more complete than others; the current implementation status is tracked in `docs/analytics/plan.md`.

### Webhooks

Webhook flows are part of the analytics design, but parts of the REST webhook management path are still under active implementation on `beta`.

### Queries and reports

The repo contains query, dashboard, report, REST, GraphQL, and gRPC analytics surfaces. Some of the deeper saved-query/report execution paths remain partial.

## Common checks

### Containers are not responding

```bash
docker-compose ps
docker-compose down && docker-compose up -d
```

### Admin panel does not load

```bash
docker-compose logs aegion
```

Look for bootstrap, migration, or configuration errors.

### Analytics data looks incomplete

```bash
docker-compose logs aegion
```

Then compare current known gaps in `docs/analytics/plan.md` and validation steps in `docs/analytics/qa.md`.

## Security and hardening note

Security and production-hardening work exists on the branch, but the complete analytics security guide is still pending. Treat this quickstart as a local beta setup note, not a production operations guide.

## Next steps

1. Read `docs/analytics/plan.md` for the verified implementation state.
2. Use `docs/analytics/qa.md` for regression commands and smoke checks.
3. Use `configs/aegion.yaml` as the current sample config source.
4. Inspect `modules/admin/spa/src/components/Analytics` for the current UI surface.
5. Add deeper docs only after the corresponding implementation slice is real.
