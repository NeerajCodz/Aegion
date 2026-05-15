# Analytics Security Model

**Version:** 1.0  
**Last Updated:** 2026-04-24  
**Module:** `modules/analytics`  
**Classification:** Internal

## Overview

The Aegion analytics system implements defense-in-depth security across multiple layers:

```
┌────────────────────────────────────────────────┐
│              Request Validation                │
│  (Input sanitization, schema validation)       │
└────────────────┬─────────────────────────────┘
                 │
┌────────────────▼─────────────────────────────┐
│          Authentication Layer                 │
│  (Token/session validation, identity)        │
└────────────────┬─────────────────────────────┘
                 │
┌────────────────▼─────────────────────────────┐
│         Authorization Layer (RBAC)            │
│  (Role-based access control, resource check) │
└────────────────┬─────────────────────────────┘
                 │
┌────────────────▼─────────────────────────────┐
│       Query Security Layer                    │
│  (Injection prevention, resource limits)     │
└────────────────┬─────────────────────────────┘
                 │
┌────────────────▼─────────────────────────────┐
│       Encryption & Rate Limiting              │
│  (TLS, field encryption, DDoS protection)    │
└────────────────────────────────────────────────┘
```

---

## Authentication

### Supported Methods

#### 1. Bearer Token (JWT)

```
Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
```

Token structure:
```json
{
  "header": {
    "alg": "HS256",
    "typ": "JWT"
  },
  "payload": {
    "sub": "user_123",
    "iat": 1234567890,
    "exp": 1234571490,
    "email": "user@example.com",
    "org_id": "org_456",
    "roles": ["viewer", "analyst"],
    "permissions": ["read:events", "write:dashboards"]
  }
}
```

**Validation:**
1. Token signature verified against public key
2. Expiry checked (`exp` claim)
3. Token not revoked (checked against revocation list)

**Duration:** 24 hours (configurable)

```yaml
analytics:
  security:
    jwt:
      enabled: true
      secret_key: ${JWT_SECRET_KEY}
      algorithm: HS256
      expires_in_hours: 24
      refresh_token_expires_in_days: 7
```

#### 2. Session Cookie

```
Cookie: X-Session-ID=sess_abc123def456
```

Session storage:
- Redis (distributed): `sessions:{session_id}`
- In-memory (development): Local map

**Session data:**
```go
type Session struct {
  ID          string
  UserID      string
  OrgID       string
  Roles       []string
  Permissions []string
  CreatedAt   time.Time
  ExpiresAt   time.Time
  IPAddress   string
}
```

#### 3. mTLS (Production, gRPC only)

```
Client certificate validation on port 50051
```

Configuration:
```yaml
analytics:
  grpc:
    tls:
      enabled: true
      cert_file: /etc/aegion/certs/server.crt
      key_file: /etc/aegion/certs/server.key
      ca_cert: /etc/aegion/certs/ca.crt
      client_auth: REQUIRE
```

### Token Refresh

```
POST /api/v1/analytics/auth/refresh
Body: { "refresh_token": "ref_..." }

Response:
{
  "access_token": "new_jwt_token",
  "refresh_token": "new_refresh_token",
  "expires_in": 86400
}
```

---

## Authorization (RBAC)

### Role Hierarchy

```
┌─────────────┐
│   Admin     │  Full access, system config
├─────────────┤
│  Analyst    │  Read+write dashboards, queries, webhooks
├─────────────┤
│   Viewer    │  Read-only dashboards, events
└─────────────┘
```

### Permission Matrix

| Action | Admin | Analyst | Viewer |
|--------|-------|---------|--------|
| List events | ✓ | ✓ | ✓ |
| Create dashboard | ✓ | ✓ | ✗ |
| Edit dashboard | ✓ | ✓* | ✗ |
| Delete dashboard | ✓ | ✗ | ✗ |
| Save query | ✓ | ✓ | ✗ |
| Execute ad-hoc query | ✓ | ✓ | ✗ |
| Create webhook | ✓ | ✓ | ✗ |
| Manage config | ✓ | ✗ | ✗ |
| View audit log | ✓ | ✗ | ✗ |

*Analyst can only edit own dashboards

### Resource Ownership

Resources are scoped to:
1. **User** - Personal dashboards, queries
2. **Organization** - Shared dashboards, shared queries
3. **Public** - System-wide resources (read-only)

```go
type Dashboard struct {
  ID          string
  Name        string
  OwnerUserID string      // Individual owner
  OrgID       string      // Organization scope
  IsPublic    bool        // Accessible to all authenticated users
  Permissions map[string][]string  // {role: [actions]}
}
```

### Authorization Enforcement

```go
// Example: Check permission before reading dashboard
func (h *Handler) GetDashboard(w http.ResponseWriter, r *http.Request) {
  user := r.Context().Value("user").(User)
  dashboard, _ := h.store.GetDashboard(dashboardID)
  
  // Check ownership or public access
  if dashboard.OwnerUserID != user.ID && 
     dashboard.OrgID != user.OrgID && 
     !dashboard.IsPublic {
    http.Error(w, "Forbidden", http.StatusForbidden)
    return
  }
  
  // Proceed with request
  json.NewEncoder(w).Encode(dashboard)
}
```

---

## Query Security

### SQL Injection Prevention

**Approach:** Parameterized queries + query validation

```go
// ✗ UNSAFE - String interpolation
query := fmt.Sprintf("SELECT * FROM events WHERE userId = '%s'", userID)

// ✓ SAFE - Parameterized queries
query := "SELECT * FROM events WHERE userId = ?"
rows, err := db.Query(query, userID)
```

All queries use prepared statements with parameter binding.

### Query Validation

```go
type QueryValidator struct {
  allowedTables   []string
  blockedPatterns []string  // e.g., DROP, DELETE, TRUNCATE
}

func (v *QueryValidator) Validate(sql string) error {
  // 1. Parse SQL
  ast, err := parser.Parse(sql)
  if err != nil {
    return fmt.Errorf("invalid SQL: %w", err)
  }
  
  // 2. Check for blocked operations
  for _, pattern := range v.blockedPatterns {
    if strings.Contains(strings.ToUpper(sql), pattern) {
      return fmt.Errorf("operation blocked: %s", pattern)
    }
  }
  
  // 3. Verify table access
  for _, table := range extractTables(ast) {
    if !contains(v.allowedTables, table) {
      return fmt.Errorf("table not allowed: %s", table)
    }
  }
  
  return nil
}
```

### Resource Limits

```yaml
analytics:
  security:
    query_limits:
      max_query_duration_seconds: 30
      max_rows_returned: 1000000
      max_memory_per_query_mb: 2048
      max_joins: 10
      max_unions: 5
```

Enforcement:
```go
ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()

rows, err := db.QueryContext(ctx, query)
if err == context.DeadlineExceeded {
  return nil, fmt.Errorf("query timeout")
}
```

---

## Encryption

### In Transit (TLS 1.3)

All APIs require TLS:
```yaml
analytics:
  tls:
    enabled: true
    min_version: "1.3"
    cert_file: /etc/aegion/certs/server.crt
    key_file: /etc/aegion/certs/server.key
    client_auth: optional
```

Certificate validation:
- Self-signed: Development only
- Let's Encrypt: Production

```bash
# Generate self-signed cert for development
openssl req -x509 -newkey rsa:4096 \
  -keyout key.pem -out cert.pem -days 365 -nodes
```

### At Rest (Field-Level)

Sensitive fields can be encrypted:
```yaml
analytics:
  security:
    encryption:
      enabled: true
      algorithm: AES-256-GCM
      key_management: "kms"  # or "local"
      encrypted_fields:
        - "events.user_email"
        - "webhooks.auth_token"
        - "users.password_hash"
```

Encryption in code:
```go
type Encrypter interface {
  Encrypt(plaintext string) (string, error)
  Decrypt(ciphertext string) (string, error)
}

// Usage
encryptedEmail, err := encrypter.Encrypt(userEmail)
dashboard.UserEmail = encryptedEmail
```

### Key Management

```yaml
analytics:
  security:
    keys:
      provider: "aws-kms"  # or "local", "vault"
      master_key_id: "arn:aws:kms:us-east-1:123:key/12345"
      key_rotation_days: 90
```

---

## Rate Limiting

### Per-User Rate Limits

```yaml
analytics:
  security:
    rate_limiting:
      enabled: true
      default_per_minute: 600
      burst_size: 100
      
      # Role-based limits
      limits_by_role:
        admin: 2000
        analyst: 1000
        viewer: 100
```

### Implementation

```go
type RateLimiter interface {
  Allow(userID string) bool
  GetRemaining(userID string) int
  Reset(userID string)
}

// Token bucket algorithm
type TokenBucket struct {
  capacity    int
  tokens      int
  refillRate  int  // tokens per minute
  lastRefill  time.Time
}

func (tb *TokenBucket) Allow() bool {
  tb.refill()
  if tb.tokens > 0 {
    tb.tokens--
    return true
  }
  return false
}
```

### Response Headers

```
X-RateLimit-Limit: 600
X-RateLimit-Remaining: 599
X-RateLimit-Reset: 1234567890
```

When limit exceeded:
```
HTTP/1.1 429 Conflict
X-RateLimit-Retry-After: 30
```

---

## Audit Logging

All sensitive operations logged:

```go
type AuditLog struct {
  ID          string
  UserID      string
  Action      string              // "create_dashboard", "execute_query", etc.
  ResourceType string
  ResourceID  string
  Status      string              // "success", "failure"
  Error       string
  IPAddress   string
  UserAgent   string
  Timestamp   time.Time
  Duration    time.Duration
  Details     map[string]interface{}  // Context-specific data
}
```

### Logged Actions

- **Authentication:** Login, logout, token refresh
- **Authorization:** Permission denied, access denied
- **Resources:** Create, read, update, delete dashboards/queries/webhooks
- **Queries:** Ad-hoc query execution, saved query execution
- **Config:** Configuration changes by admins
- **Webhooks:** Webhook deliveries, retries, failures

### Retention

```yaml
analytics:
  security:
    audit:
      retention_days: 90
      archive_after_days: 30
      archive_location: "s3://audit-archive/"
```

### Access to Audit Logs

```bash
# Only admins can access
curl -H "Authorization: Bearer ${AEGION_ADMIN_TOKEN}" \
  http://localhost:8080/api/v1/analytics/audit-logs
```

---

## Sensitive Data Protection

### Logging Guidelines

**Never log:**
- Passwords, API keys, tokens
- PII (email, phone, SSN)
- Payment information
- OAuth secrets

**Example: Safe logging**
```go
// ✗ UNSAFE
log.Infof("User login: %v", user)  // Might include email

// ✓ SAFE
log.Infof("User login: userID=%s", user.ID)

// ✗ UNSAFE
log.Debugf("Query executed: %s", query)  // Might contain sensitive data

// ✓ SAFE
log.Debugf("Query executed: duration=%dms", duration)
```

### Webhook Payload Sanitization

Webhooks automatically sanitize sensitive fields:
```json
{
  "id": "evt_123",
  "data": {
    "email": "***REDACTED***",
    "password": "***REDACTED***"
  }
}
```

### Database Scrubbing

Sensitive fields are automatically hashed:
```go
type Event struct {
  UserID        string  // Hashed in database
  SessionID     string  // Hashed in database
  UserEmail     string  `db:"encrypted=true"`  // Field-level encryption
}
```

---

## Data Retention & Deletion

### Retention Policies

```yaml
analytics:
  retention:
    policies:
      - category: user_action
        hot_days: 7      # DuckDB
        warm_days: 30    # S3
        cold_days: 365   # Iceberg
      - category: system_event
        hot_days: 1
        warm_days: 90
        cold_days: 2555  # 7 years for audit
```

### GDPR Right to Deletion

```bash
# Delete all events for a user (GDPR)
POST /api/v1/analytics/users/{userId}/delete
Authorization: Bearer ${AEGION_ADMIN_TOKEN}

Response:
{
  "deleted_records": 15234,
  "request_id": "del_xyz123"
}
```

Implementation:
```go
// Mark events as deleted (soft delete first)
func (h *Handler) DeleteUserData(userID string) error {
  // 1. Soft delete from hot storage (DuckDB)
  _, err := h.duckdb.Exec(
    "UPDATE events SET deleted=true WHERE userId=?",
    userID)
  
  // 2. Queue hard delete for warm/cold tiers
  h.asyncQueue.Enqueue(DeleteUserDataTask{
    UserID: userID,
    StorageTiers: []string{"s3", "iceberg"},
  })
  
  return err
}
```

---

## Compliance & Standards

### Standards Implemented

- **OWASP Top 10:** Defense against common vulnerabilities
- **GDPR:** Data protection, right to deletion, privacy
- **HIPAA:** Encryption, audit logging, access controls (when configured)
- **SOC2:** Logging, monitoring, incident response

### Security Headers

```
Strict-Transport-Security: max-age=31536000; includeSubDomains
Content-Security-Policy: default-src 'self'
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
X-XSS-Protection: 1; mode=block
```

### Regular Security Audits

- Quarterly code review
- Annual penetration testing
- Vulnerability scanning (OWASP ZAP, trivy)
- Dependency auditing (go list, npm audit)

---

## Security Checklist

Before production deployment:

- [ ] TLS 1.3 enabled with valid certificate
- [ ] JWT secret key changed from default
- [ ] Database passwords strong and unique
- [ ] Rate limiting configured
- [ ] Audit logging enabled
- [ ] CORS properly configured (not `*`)
- [ ] gRPC mTLS enabled (if exposed)
- [ ] Sensitive fields configured for encryption
- [ ] Webhook URLs validated and whitelisted
- [ ] Dead letter queue monitoring enabled
- [ ] Regular backups scheduled
- [ ] Incident response plan documented

---

## Related Documentation

- [API Reference](./api.md)
- [Setup Guide](./setup.md)
- [Architecture](./architecture.md)
- [Troubleshooting](./troubleshooting.md)
