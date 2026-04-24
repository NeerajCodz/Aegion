# Quick Start Guide - Aegion Analytics

Get the Aegion Analytics system up and running in 5 minutes.

## Prerequisites

- **Docker & Docker Compose** (or Go 1.21+, PostgreSQL, Redis)
- **curl** or an HTTP client (for testing)
- **Terminal/Shell** access
- **4GB RAM** minimum for development

## ⚡ 5-Minute Setup (Docker)

### Step 1: Clone Repository

```bash
git clone https://github.com/NeerajCodz/Aegion.git
cd Aegion
```

### Step 2: Start Containers

```bash
docker-compose up -d
```

This starts:
- **PostgreSQL** (localhost:5432) - Primary data store
- **Redis** (localhost:6379) - Caching layer
- **Aegion Core** (localhost:8080) - API and admin UI
- **Mailpit** (localhost:1025) - Email testing
- **DuckDB** - Analytics engine

Wait 30 seconds for services to stabilize.

### Step 3: Initialize Admin Account

```bash
# Create admin user (runs once on first boot)
docker-compose exec aegion aegion bootstrap --admin-email admin@example.com

# Password will be generated - save it!
# Output: "Operator password: xxxxx"
```

### Step 4: Open Admin Dashboard

Open your browser:
- **Admin Panel**: http://localhost:8080/aegion
- **Analytics Dashboard**: http://localhost:8080/aegion/analytics
- **API Docs**: http://localhost:8080/api/docs (if enabled)

Login with:
- **Email**: `admin@example.com`
- **Password**: (from Step 3)

### Step 5: Generate Test Data (Optional)

```bash
# Seed analytics events for testing
docker-compose exec aegion aegion analytics seed --count 1000
```

### Step 6: Make Your First Query

```bash
# Get auth token (normally from login)
export AEGION_TOKEN="your-bearer-token-here"

# Query events
curl -H "Authorization: Bearer $AEGION_TOKEN" \
  http://localhost:8080/api/v1/analytics/events
```

**Done!** ✨ You now have a fully functional analytics system.

---

## 📊 What to Do Next

### 1. Explore the Dashboard

- Click "Authentication Dashboard" to see login metrics
- Click "User Activity" to see user behavior
- Try creating a custom dashboard

### 2. Try REST API

```bash
# List all events
curl -H "Authorization: Bearer $AEGION_TOKEN" \
  http://localhost:8080/api/v1/analytics/events

# Export events as CSV
curl -H "Authorization: Bearer $AEGION_TOKEN" \
  http://localhost:8080/api/v1/analytics/events/export/csv \
  > events.csv

# Query specific events
curl -X POST \
  -H "Authorization: Bearer $AEGION_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "filters": [
      {"field": "event_type", "operator": "eq", "value": "authentication.login_failed"}
    ],
    "page_size": 10
  }' \
  http://localhost:8080/api/v1/analytics/events/search
```

### 3. Try GraphQL

```bash
curl -X POST \
  -H "Authorization: Bearer $AEGION_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "query { events(first: 10) { edges { node { id category eventType } } } }"
  }' \
  http://localhost:8080/graphql
```

### 4. Create a Webhook

In the Admin Panel:
1. Go to "Webhooks" section
2. Click "Create Webhook"
3. Set webhook URL (e.g., `https://webhook.site/unique-id`)
4. Select events to subscribe to
5. Save and test

### 5. Read Full Documentation

- **[API Usage Guide](api.md)** - Detailed REST endpoint documentation
- **[GraphQL Schema](graphql-schema.md)** - GraphQL queries and mutations
- **[Integration Guide](integration.md)** - Code examples in multiple languages
- **[Webhook Guide](webhooks.md)** - Event subscription setup
- **[Setup Guide](setup.md)** - Production deployment

---

## 🔑 Key Concepts

### Events
Structured data records with:
- **Category**: Type of event (authentication, user, session, etc.)
- **Type**: Specific event (login_success, logout, etc.)
- **Data**: Event payload with custom fields
- **Timestamp**: When the event occurred
- **User/Session ID**: Associated user or session

Example event:
```json
{
  "id": "evt_123456",
  "category": "authentication",
  "event_type": "login_success",
  "user_id": "usr_789",
  "session_id": "sess_456",
  "data": {
    "method": "password",
    "ip_address": "192.168.1.1",
    "device": "Chrome on Windows"
  },
  "created_at": "2026-04-23T14:30:00Z"
}
```

### Dashboards
Pre-built or custom visualizations showing:
- Real-time metrics (success rates, error counts)
- Time series trends
- Geographic distribution
- Device/browser breakdowns
- User activity patterns

### Webhooks
HTTP callbacks that fire when matching events occur:
- Filter events by type/category
- Receive signed payloads
- Automatic retry on failure
- Full delivery history

### Queries
Saved analytics queries you can:
- Execute on demand
- Share with team members
- Include in reports
- Schedule for regular runs

---

## 🆘 Common Issues

### "Connection refused" error
```bash
# Check if containers are running
docker-compose ps

# Restart containers
docker-compose down && docker-compose up -d

# Wait 30 seconds before retrying
```

### Admin panel won't load
```bash
# Check logs
docker-compose logs aegion

# Look for database migration errors
# If found, run migrations manually
docker-compose exec aegion aegion migrate
```

### No data showing up
```bash
# Check if sync layer is running
docker-compose logs aegion | grep sync

# Seed test data
docker-compose exec aegion aegion analytics seed --count 100

# Check DuckDB status
docker-compose exec aegion aegion analytics health
```

### Performance is slow
```bash
# Check DuckDB memory usage
docker-compose exec aegion aegion analytics stats

# Restart to clear cache
docker-compose restart aegion

# See Performance Tuning guide for optimization
```

---

## 📚 Common Tasks

### Export Events to CSV

```bash
curl -H "Authorization: Bearer $AEGION_TOKEN" \
  'http://localhost:8080/api/v1/analytics/events/export/csv?category=authentication' \
  > events.csv
```

### Query Last 24 Hours

```bash
curl -H "Authorization: Bearer $AEGION_TOKEN" \
  'http://localhost:8080/api/v1/analytics/events?after=2026-04-22T14:30:00Z'
```

### Get Event Count by Type

```bash
curl -X POST \
  -H "Authorization: Bearer $AEGION_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "aggregation": "count",
    "group_by": ["event_type"]
  }' \
  http://localhost:8080/api/v1/analytics/events/aggregate
```

### Subscribe to Login Failures

Create a webhook matching:
- **Event Type**: `authentication.login_failed`
- **Category**: `authentication`

Your endpoint receives:
```json
{
  "id": "webhook_evt_123",
  "event_type": "authentication.login_failed",
  "data": {
    "user_id": "usr_456",
    "reason": "invalid_password",
    "attempts": 3
  },
  "timestamp": "2026-04-23T14:30:00Z",
  "signatures": {
    "sha256": "sha256=..."
  }
}
```

---

## 🔐 Security Basics

### API Authentication

All requests require a Bearer token:

```bash
# Get token (from login or admin panel)
export AEGION_TOKEN="your-token-here"

# Include in requests
curl -H "Authorization: Bearer $AEGION_TOKEN" \
  http://localhost:8080/api/v1/analytics/events
```

### Rate Limiting

Default limits:
- **REST API**: 1000 requests/minute per user
- **GraphQL**: 100 queries/minute per user
- **Webhook Delivery**: 100 concurrent deliveries

See [Security Guide](security.md) for details.

### Data Privacy

- All data is encrypted at rest (AES-256)
- Communication uses HTTPS in production
- Audit logs track all access
- Retention policies can auto-delete old data

---

## 🚀 Next Steps

1. **[Read the Full Setup Guide](setup.md)** for production deployment
2. **[Review Configuration Reference](config.md)** to customize settings
3. **[Check Integration Guide](integration.md)** for code examples
4. **[Explore Webhook Integration](webhooks.md)** for event subscriptions
5. **[Review Security Guide](security.md)** for security best practices

---

## 📞 Need Help?

- **Issues**: https://github.com/NeerajCodz/Aegion/issues
- **Discussions**: https://github.com/NeerajCodz/Aegion/discussions
- **Troubleshooting**: See [Troubleshooting Guide](troubleshooting.md)
- **FAQ**: See [FAQ](faq.md) for common questions

---

**What are you building? We'd love to hear about it!** 🚀

Bookmark this page: `docs/analytics/quickstart.md`
