# Aegion Analytics - Quick Reference Table

**Total Endpoints:** 56 | **Version:** 1.0.0 | **Base URL:** `/api/v1/analytics`

## Health & Status (6 endpoints)

| # | Method | Path | Description | Auth | Rate Limited |
|---|--------|------|-------------|------|-------------|
| 1 | GET | `/health` | System health check | No | No |
| 2 | GET | `/ready` | Readiness check | No | No |
| 3 | GET | `/live` | Liveness probe | No | No |
| 4 | GET | `/metrics` | Prometheus metrics | No | No |
| 5 | GET | `/stats` | System statistics | No | No |
| 6 | GET | `/export-formats` | Supported export formats | No | No |

## Events (5 endpoints)

| # | Method | Path | Description | Auth | Rate Limited |
|---|--------|------|-------------|------|-------------|
| 7 | GET | `/events` | List events with pagination | Yes | Yes |
| 8 | POST | `/events/search` | Advanced search events | Yes | Yes |
| 9 | GET | `/events/{id}` | Get single event | Yes | Yes |
| 10 | GET | `/events/{id}/related` | Get related events | Yes | Yes |
| 11 | POST | `/events/export` | Export events (CSV/JSON/Parquet) | Yes | Yes |

## Dashboards (8 endpoints)

| # | Method | Path | Description | Auth | Rate Limited |
|---|--------|------|-------------|------|-------------|
| 12 | GET | `/dashboards` | List dashboards | Yes | Yes |
| 13 | POST | `/dashboards` | Create dashboard | Yes | Yes |
| 14 | GET | `/dashboards/{id}` | Get dashboard | Yes | Yes |
| 15 | PUT | `/dashboards/{id}` | Update dashboard | Yes | Yes |
| 16 | DELETE | `/dashboards/{id}` | Delete dashboard | Yes | Yes |
| 17 | POST | `/dashboards/{id}/share` | Share dashboard with user | Yes | Yes |
| 18 | POST | `/dashboards/{id}/components/{componentId}/execute` | Execute dashboard component query | Yes | Yes |

## Queries (4 endpoints)

| # | Method | Path | Description | Auth | Rate Limited |
|---|--------|------|-------------|------|-------------|
| 19 | GET | `/queries` | List saved queries | Yes | Yes |
| 20 | POST | `/queries` | Save new query | Yes | Yes |
| 21 | GET | `/queries/{id}/execute` | Execute saved query | Yes | Yes |
| 22 | DELETE | `/queries/{id}` | Delete query | Yes | Yes |

## Reports (7 endpoints)

| # | Method | Path | Description | Auth | Rate Limited |
|---|--------|------|-------------|------|-------------|
| 23 | GET | `/reports` | List reports | Yes | Yes |
| 24 | POST | `/reports` | Create report template | Yes | Yes |
| 25 | GET | `/reports/{id}` | Get report | Yes | Yes |
| 26 | PUT | `/reports/{id}` | Update report | Yes | Yes |
| 27 | DELETE | `/reports/{id}` | Delete report | Yes | Yes |
| 28 | POST | `/reports/{id}/generate` | Generate report now | Yes | Yes |
| 29 | GET | `/reports/{id}/download` | Download generated report | Yes | Yes |

## Configuration - Storage (3 endpoints)

| # | Method | Path | Description | Auth | Rate Limited |
|---|--------|------|-------------|------|-------------|
| 30 | GET | `/config/storage` | Get storage config | Yes | Yes |
| 31 | PUT | `/config/storage` | Update storage config | Yes | Yes |
| 32 | POST | `/config/storage/test` | Test storage connection | Yes | No |

## Configuration - Sync (4 endpoints)

| # | Method | Path | Description | Auth | Rate Limited |
|---|--------|------|-------------|------|-------------|
| 33 | GET | `/config/sync` | Get sync config | Yes | Yes |
| 34 | PUT | `/config/sync` | Update sync config | Yes | Yes |
| 35 | POST | `/config/sync/trigger` | Trigger manual sync | Yes | No |
| 36 | GET | `/config/sync/{syncId}/status` | Get sync status | Yes | Yes |

## Configuration - Retention (4 endpoints)

| # | Method | Path | Description | Auth | Rate Limited |
|---|--------|------|-------------|------|-------------|
| 37 | GET | `/config/retention` | Get retention policy | Yes | Yes |
| 38 | PUT | `/config/retention` | Update retention policy | Yes | Yes |
| 39 | POST | `/config/retention/archive` | Trigger archival | Yes | No |
| 40 | GET | `/config/retention/archive-history` | Get archive history | Yes | Yes |

## Validation (4 endpoints)

| # | Method | Path | Description | Auth | Rate Limited |
|---|--------|------|-------------|------|-------------|
| 41 | POST | `/validate/storage` | Validate storage config | Yes | No |
| 42 | POST | `/validate/sync` | Validate sync config | Yes | No |
| 43 | POST | `/validate/retention` | Validate retention policy | Yes | No |
| 44 | POST | `/validate/webhook` | Validate webhook config | Yes | No |

## Webhooks (8 endpoints)

| # | Method | Path | Description | Auth | Rate Limited |
|---|--------|------|-------------|------|-------------|
| 45 | GET | `/webhooks` | List webhooks | Yes | Yes |
| 46 | POST | `/webhooks` | Create webhook | Yes | Yes |
| 47 | GET | `/webhooks/{id}` | Get webhook | Yes | Yes |
| 48 | PUT | `/webhooks/{id}` | Update webhook | Yes | Yes |
| 49 | DELETE | `/webhooks/{id}` | Delete webhook | Yes | Yes |
| 50 | POST | `/webhooks/{id}/test` | Test webhook delivery | Yes | No |
| 51 | GET | `/webhooks/{id}/delivery-history` | Get delivery history | Yes | Yes |
| 52 | POST | `/webhooks/{id}/replay` | Replay failed deliveries | Yes | No |

## User Preferences (4 endpoints)

| # | Method | Path | Description | Auth | Rate Limited |
|---|--------|------|-------------|------|-------------|
| 53 | GET | `/user/preferences` | Get user preferences | Yes | Yes |
| 54 | PUT | `/user/preferences` | Update user preferences | Yes | Yes |
| 55 | POST | `/user/favorites/dashboards/{dashboardId}` | Add favorite dashboard | Yes | Yes |
| 56 | DELETE | `/user/favorites/dashboards/{dashboardId}` | Remove favorite dashboard | Yes | Yes |

---

## Quick Lookup by Use Case

### Getting Started
- `GET /health` - Check service is running
- `GET /ready` - Check all dependencies ready
- `GET /metrics` - Monitor performance

### Working with Events
- `GET /events` - Browse recent events
- `POST /events/search` - Find specific events
- `GET /events/{id}` - Get event details
- `POST /events/export` - Export for analysis

### Building Dashboards
- `POST /dashboards` - Create new dashboard
- `GET /dashboards/{id}` - View dashboard
- `PUT /dashboards/{id}` - Modify dashboard
- `POST /dashboards/{id}/components/{componentId}/execute` - Update dashboard data

### Running Queries
- `POST /queries` - Save custom query
- `GET /queries/{id}/execute` - Run query
- `GET /queries` - View saved queries

### Generating Reports
- `POST /reports` - Schedule report
- `POST /reports/{id}/generate` - Generate now
- `GET /reports/{id}/download` - Retrieve report

### System Configuration
- `GET /config/storage` - Check storage
- `GET /config/sync` - Check sync status
- `GET /config/retention` - Check data retention
- `POST /config/retention/archive` - Archive old data

### Setting Up Webhooks
- `POST /webhooks` - Create webhook
- `POST /webhooks/{id}/test` - Test delivery
- `GET /webhooks/{id}/delivery-history` - Check deliveries

### Managing Preferences
- `PUT /user/preferences` - Update settings
- `POST /user/favorites/dashboards/{id}` - Mark dashboard as favorite

---

## HTTP Methods Reference

| Method | Purpose | Idempotent |
|--------|---------|-----------|
| GET | Retrieve data | Yes |
| POST | Create/execute operation | No |
| PUT | Update/replace resource | Yes |
| DELETE | Remove resource | Yes |

---

## Status Codes

| Code | Meaning |
|------|---------|
| 200 | Success |
| 201 | Created |
| 202 | Accepted (async) |
| 204 | No Content |
| 400 | Bad Request |
| 401 | Unauthorized |
| 403 | Forbidden |
| 404 | Not Found |
| 429 | Rate Limited |
| 500 | Server Error |

---

## Authentication Template

All endpoints marked "Auth: Yes" require:

```
Authorization: Bearer <JWT_TOKEN>
```

---

## Common Query Parameters

| Parameter | Type | Default | Notes |
|-----------|------|---------|-------|
| page | integer | 1 | Page number (1-indexed) |
| limit | integer | 50 | Results per page (max 100) |
| offset | integer | 0 | Offset for alternate pagination |
| sort | string | - | Sort field and order |
| filter | string | - | Filter by field |

---

**Last Updated:** January 2024 | **Version:** 1.0.0
