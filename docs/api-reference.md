# API Reference

This document provides comprehensive API documentation for Aegion's REST endpoints.

## Base URL

All API endpoints are relative to your Aegion instance:
```
https://your-aegion-instance.com
```

For local development:
```
http://localhost:8080
```

## Authentication

Aegion uses session-based authentication with secure HTTP-only cookies. Include the session cookie in requests to authenticated endpoints.

### Error Responses

All endpoints return consistent error responses:

```json
{
  "error": {
    "code": "invalid_credentials",
    "message": "The provided credentials are invalid",
    "details": {
      "field": "password",
      "reason": "password_mismatch"
    }
  }
}
```

## Self-Service Endpoints

These endpoints are used by end-users for registration, login, and account management.

### Registration

#### Initialize Registration Flow

```http
GET /self-service/registration/api
```

**Response:**
```json
{
  "id": "f8b3c1a2-d4e5-4f6a-8b9c-1a2b3c4d5e6f",
  "type": "api",
  "expires_at": "2024-01-15T12:00:00Z",
  "issued_at": "2024-01-15T11:00:00Z",
  "request_url": "http://localhost:8080/self-service/registration/api",
  "ui": {
    "action": "/self-service/registration/api",
    "method": "POST",
    "nodes": [
      {
        "type": "input",
        "group": "default",
        "attributes": {
          "name": "traits.email",
          "type": "email",
          "required": true
        }
      },
      {
        "type": "input",
        "group": "password",
        "attributes": {
          "name": "password",
          "type": "password",
          "required": true
        }
      }
    ]
  }
}
```

#### Submit Registration

```http
POST /self-service/registration/api
Content-Type: application/json

{
  "method": "password",
  "password": "secure-password-123",
  "traits": {
    "email": "user@example.com",
    "name": {
      "first": "John",
      "last": "Doe"
    }
  }
}
```

**Response (Success):**
```json
{
  "session": {
    "id": "a1b2c3d4-e5f6-7a8b-9c0d-1e2f3a4b5c6d",
    "active": true,
    "expires_at": "2024-01-22T12:00:00Z",
    "authenticated_at": "2024-01-15T12:00:00Z",
    "identity": {
      "id": "9f8e7d6c-5b4a-3928-1706-f5e4d3c2b1a0",
      "schema_id": "default",
      "traits": {
        "email": "user@example.com",
        "name": {
          "first": "John",
          "last": "Doe"
        }
      },
      "created_at": "2024-01-15T12:00:00Z",
      "updated_at": "2024-01-15T12:00:00Z"
    }
  }
}
```

### Login

#### Initialize Login Flow

```http
GET /self-service/login/api
```

#### Submit Login

```http
POST /self-service/login/api
Content-Type: application/json

{
  "method": "password",
  "password_identifier": "user@example.com",
  "password": "secure-password-123"
}
```

**Response:**
```json
{
  "session": {
    "id": "a1b2c3d4-e5f6-7a8b-9c0d-1e2f3a4b5c6d",
    "active": true,
    "expires_at": "2024-01-22T12:00:00Z",
    "authenticated_at": "2024-01-15T12:00:00Z",
    "identity": {
      "id": "9f8e7d6c-5b4a-3928-1706-f5e4d3c2b1a0",
      "schema_id": "default",
      "traits": {
        "email": "user@example.com",
        "name": {
          "first": "John",
          "last": "Doe"
        }
      }
    }
  }
}
```

### Account Recovery

#### Initialize Recovery Flow

```http
GET /self-service/recovery/api
```

#### Submit Recovery Email

```http
POST /self-service/recovery/api
Content-Type: application/json

{
  "method": "link",
  "email": "user@example.com"
}
```

### Session Management

#### Get Current Session

```http
GET /sessions/whoami
Cookie: aegion_session=<session_cookie>
```

#### Logout

```http
DELETE /self-service/logout/api
Cookie: aegion_session=<session_cookie>
```

## Admin Endpoints

These endpoints require admin/operator authentication.

### Identity Management

#### List Identities

```http
GET /admin/identities?page=1&per_page=100
Authorization: Bearer <admin_token>
```

**Response:**
```json
{
  "identities": [
    {
      "id": "9f8e7d6c-5b4a-3928-1706-f5e4d3c2b1a0",
      "schema_id": "default",
      "state": "active",
      "traits": {
        "email": "user@example.com",
        "name": {
          "first": "John",
          "last": "Doe"
        }
      },
      "created_at": "2024-01-15T12:00:00Z",
      "updated_at": "2024-01-15T12:00:00Z"
    }
  ],
  "pagination": {
    "page": 1,
    "per_page": 100,
    "total": 1,
    "total_pages": 1
  }
}
```

#### Get Identity

```http
GET /admin/identities/{id}
Authorization: Bearer <admin_token>
```

#### Create Identity

```http
POST /admin/identities
Authorization: Bearer <admin_token>
Content-Type: application/json

{
  "schema_id": "default",
  "traits": {
    "email": "newuser@example.com",
    "name": {
      "first": "Jane",
      "last": "Smith"
    }
  }
}
```

#### Update Identity

```http
PUT /admin/identities/{id}
Authorization: Bearer <admin_token>
Content-Type: application/json

{
  "traits": {
    "email": "updated@example.com",
    "name": {
      "first": "Jane",
      "last": "Doe"
    }
  }
}
```

#### Delete Identity

```http
DELETE /admin/identities/{id}
Authorization: Bearer <admin_token>
```

### Session Management

#### List Sessions

```http
GET /admin/sessions?identity_id={identity_id}
Authorization: Bearer <admin_token>
```

#### Revoke Session

```http
DELETE /admin/sessions/{id}
Authorization: Bearer <admin_token>
```

### Operator Management

#### List Operators

```http
GET /admin/operators
Authorization: Bearer <admin_token>
```

#### Create Operator

```http
POST /admin/operators
Authorization: Bearer <admin_token>
Content-Type: application/json

{
  "email": "admin@example.com",
  "password": "secure-admin-password",
  "role": "operator"
}
```

## WebHooks

Configure webhooks to receive real-time notifications:

### Registration Completed

```json
{
  "event": "identity.created",
  "data": {
    "identity": {
      "id": "9f8e7d6c-5b4a-3928-1706-f5e4d3c2b1a0",
      "traits": {
        "email": "user@example.com"
      }
    }
  },
  "occurred_at": "2024-01-15T12:00:00Z"
}
```

### Login Completed

```json
{
  "event": "session.created",
  "data": {
    "session": {
      "id": "a1b2c3d4-e5f6-7a8b-9c0d-1e2f3a4b5c6d",
      "identity_id": "9f8e7d6c-5b4a-3928-1706-f5e4d3c2b1a0"
    }
  },
  "occurred_at": "2024-01-15T12:00:00Z"
}
```

## Error Codes

| Code | Description | Common Causes |
|------|-------------|---------------|
| `invalid_credentials` | Login credentials are incorrect | Wrong email/password |
| `identity_not_found` | Identity does not exist | Invalid identity ID |
| `session_expired` | Session has expired | User needs to log in again |
| `validation_error` | Request validation failed | Missing required fields |
| `email_already_exists` | Email is already registered | User trying to register existing email |
| `flow_expired` | Registration/login flow expired | Flow took too long to complete |
| `flow_not_found` | Invalid flow ID | Flow ID doesn't exist or expired |
| `unauthorized` | Authentication required | Missing or invalid session |
| `forbidden` | Insufficient permissions | User lacks required permissions |
| `rate_limit_exceeded` | Too many requests | Client is being rate limited |
| `internal_error` | Server error | Something went wrong on server |

## Rate Limiting

API endpoints are rate limited to prevent abuse:

- **Registration**: 5 attempts per hour per IP
- **Login**: 10 attempts per hour per IP
- **Recovery**: 3 attempts per hour per IP
- **Admin endpoints**: 1000 requests per hour per token

Rate limit headers are included in responses:
```http
X-RateLimit-Limit: 10
X-RateLimit-Remaining: 7
X-RateLimit-Reset: 1642249200
```

## SDKs and Examples

### cURL Examples

See the [Getting Started guide](getting-started.md) for basic cURL examples.

### JavaScript/TypeScript

```javascript
// Using fetch API
const response = await fetch('/self-service/login/api', {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json',
  },
  body: JSON.stringify({
    method: 'password',
    password_identifier: 'user@example.com',
    password: 'secure-password-123'
  })
});

const data = await response.json();
```

### Go

```go
// Using net/http
client := &http.Client{}
body := strings.NewReader(`{
  "method": "password",
  "password_identifier": "user@example.com", 
  "password": "secure-password-123"
}`)

req, _ := http.NewRequest("POST", "http://localhost:8080/self-service/login/api", body)
req.Header.Add("Content-Type", "application/json")

resp, err := client.Do(req)
```

For more examples and SDKs, see the [GitHub repository](https://github.com/NeerajCodz/Aegion).