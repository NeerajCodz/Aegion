# Aegion Analytics Documentation

Complete documentation for the Aegion Analytics module. All analytics features and APIs are fully documented.

**Version:** 1.0  
**Last Updated:** 2026-04-24  
**Module:** `modules/analytics`

---

## Quick Links

| Document | Purpose |
|----------|---------|
| [Quickstart](./quickstart.md) | 5-minute setup guide |
| [API Reference](./api.md) | REST, GraphQL, gRPC API overview |
| [Endpoint Reference](./ENDPOINT_REFERENCE.md) | All 56+ endpoints documented |
| [Quick Reference](./ENDPOINT_QUICK_REFERENCE.md) | Endpoint table lookup |
| [Test Scenarios](./ENDPOINT_TEST_SCENARIOS.md) | End-to-end testing with curl |
| [Setup Guide](./setup.md) | Installation and deployment |
| [Architecture](./architecture.md) | System design and data flow |
| [Security](./security.md) | Authentication, authorization, encryption |
| [FAQ](./faq.md) | Frequently asked questions |

---

## Getting Started

**New to Aegion Analytics?**
1. Read [Quickstart](./quickstart.md) (5 min)
2. Try local setup with Docker Compose (10 min)
3. Explore Admin SPA dashboard

**Integrating with your system?**
1. Review [API Reference](./api.md) (choose REST/GraphQL/gRPC)
2. Check [Integration Guide](./integration.md)
3. Follow [Security Guide](./security.md) best practices

**Operating in production?**
1. Follow [Setup Guide](./setup.md) deployment instructions
2. Configure using [Security Guide](./security.md)
3. Monitor with health endpoints
4. Use [Troubleshooting Guide](./troubleshooting.md) if issues arise

---

## Complete Documentation Index

### API & Integration
- **[API Reference](./api.md)** - REST, GraphQL, gRPC API overview, authentication, rate limiting
- **[Endpoint Reference](./ENDPOINT_REFERENCE.md)** - Complete REST API endpoint documentation (56+ endpoints)
- **[Quick Reference](./ENDPOINT_QUICK_REFERENCE.md)** - All endpoints in table format for quick lookup
- **[Response Examples](./ENDPOINT_RESPONSES.md)** - Success and error response examples for each endpoint
- **[Test Scenarios](./ENDPOINT_TEST_SCENARIOS.md)** - 9 end-to-end testing scenarios with curl commands
- **[Endpoint Performance](./ENDPOINT_PERFORMANCE.md)** - Response times, caching, optimization, benchmarks
- **[OpenAPI Specification](./openapi.yaml)** - Complete REST API spec (OpenAPI 3.0)
- **[GraphQL Schema](./graphql-schema.md)** - GraphQL types, queries, mutations, subscriptions
- **[Webhooks](./webhooks.md)** - Real-time event notifications, configuration, signature verification
- **[Integration Guide](./integration.md)** - Integrating with Aegion core, event sources, data sync

### Operations & Deployment
- **[Setup Guide](./setup.md)** - Installation, Docker, Kubernetes, DuckDB, storage backend setup
- **[Architecture](./architecture.md)** - System design, components, data flow, deployment models
- **[Performance Tuning](./performance.md)** - Query optimization, indexes, caching, benchmarks
- **[Security](./security.md)** - Authentication, RBAC, encryption, audit logging, compliance

### Administration & Support
- **[Admin SPA Guide](./admin-spa.md)** - Web interface features, dashboard builder, query editor
- **[Troubleshooting](./troubleshooting.md)** - Common issues, solutions, debugging techniques
- **[FAQ](./faq.md)** - Frequently asked questions and answers

### Project Management
- **[Plan](./plan.md)** - Implementation roadmap, completion status (Phase 15)
- **[QA Runbook](./qa.md)** - Test procedures, verification checklist
- **[Quickstart](./quickstart.md)** - Quick local setup reference

---

## API Quick Reference

### REST API
```bash
curl -H "Authorization: Bearer TOKEN" \
  http://localhost:8080/api/v1/analytics/health
```

### GraphQL
```graphql
query {
  health { status }
  events(first: 10) { id category eventType }
}
```

### gRPC
```bash
grpcurl -plaintext localhost:50051 list analytics.Analytics
```

See [API Reference](./api.md) for complete examples.

---

## Common Tasks

### Deploy Locally
```bash
docker-compose -f deploy/docker-compose.yml up -d
curl http://localhost:8080/api/v1/analytics/health
```

See [Setup Guide](./setup.md).

### Create Dashboard
1. Navigate to Admin SPA: `http://localhost:3000/admin/analytics`
2. Click **Dashboards → + New Dashboard**
3. Add widgets using query builder
4. Save and share

See [Admin SPA Guide](./admin-spa.md).

### Integrate with API
```javascript
const response = await fetch('http://api/v1/analytics/events', {
  headers: { 'Authorization': 'Bearer token' }
});
const events = await response.json();
```

See [API Reference](./api.md) and [Integration Guide](./integration.md).

### Setup Webhooks
1. Navigate to Admin SPA: **Webhooks → + New Webhook**
2. Enter webhook URL and select events
3. Configure filters and retry policy
4. Click **Send Test** to verify

See [Webhooks Guide](./webhooks.md).

### Monitor System Health
```bash
curl http://localhost:8080/api/v1/analytics/stats
curl http://localhost:8080/api/v1/analytics/metrics
```

See [Troubleshooting Guide](./troubleshooting.md).

---

## Documentation Standards

All documentation includes:
- **Code examples** - Copy-paste ready (valid, tested)
- **Internal links** - All links resolve to existing files
- **Configuration examples** - YAML syntax with explanations
- **Troubleshooting** - Common issues and solutions
- **Version info** - Last updated date and relevant versions

---

## Support & Community

- **Issues:** Report bugs on [GitHub](https://github.com/neerajcodz/aegion/issues)
- **Docs:** All documentation in this directory
- **Questions:** Check [FAQ](./faq.md) first
- **Help:** Use [Troubleshooting Guide](./troubleshooting.md)

---

## Navigation

**By Role:**
- **Developers** → [API Reference](./api.md) → [Integration Guide](./integration.md)
- **Operators** → [Setup Guide](./setup.md) → [Performance Tuning](./performance.md)
- **DevOps** → [Architecture](./architecture.md) → [Security](./security.md)
- **Admins** → [Admin SPA Guide](./admin-spa.md) → [Troubleshooting](./troubleshooting.md)

**By Task:**
- Getting started → [Quickstart](./quickstart.md)
- Choosing API → [API Reference](./api.md)
- Deploying → [Setup Guide](./setup.md)
- Integrating → [Integration Guide](./integration.md)
- Securing → [Security](./security.md)
- Optimizing → [Performance Tuning](./performance.md)
- Fixing issues → [Troubleshooting](./troubleshooting.md)

---

## About Aegion Analytics

Aegion Analytics is a real-time event processing and analytics platform built into the Aegion system. It provides:

- **Event ingestion** - Stream events from Aegion Core or custom sources
- **Real-time queries** - Sub-100ms response times on hot data
- **Multiple APIs** - REST, GraphQL, gRPC for different use cases
- **Dashboards** - Build and share analytics dashboards
- **Webhooks** - Real-time notifications to external systems
- **Storage tiering** - Automatic archival to cold storage
- **Compliance** - GDPR, HIPAA-ready with audit logging

**Key Features:**
- ✅ All 3 API layers (REST, GraphQL, gRPC)
- ✅ Real-time + batch + async sync strategies
- ✅ DuckDB-based OLAP queries
- ✅ Multi-tier storage (hot/warm/cold)
- ✅ Admin SPA for full management
- ✅ 85%+ test coverage
- ✅ Production-ready

See [Architecture](./architecture.md) for technical details.
