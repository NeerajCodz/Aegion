# Aegion — Policy Engine Specification

Aegion policy combines three models in one system:

- RBAC (roles + permissions)
- ABAC (attribute rules)
- ReBAC (relationship graph)

This allows simple role-based control, context-aware decisions, and deep ownership graph permissions in the same platform.

---

## Why this module exists

Authentication answers **who** the subject is.

Policy answers **what** the subject can do.

Aegion policy is designed to work for:

- straightforward role systems (admin/editor/viewer)
- business rules (owner can edit if status = draft)
- enterprise graph access (team/folder/project inheritance)

---

## Decision surface

Policy checks can be called by:

- application APIs directly
- Aegion proxy authorization stage
- admin operations requiring capability validation

Canonical check shape:

```json
{
  "subject": "user:alice",
  "resource": "document:spec-123",
  "action": "read",
  "context": {
    "ip": "203.0.113.10",
    "time": "2026-03-23T21:37:17Z"
  }
}
```

---

## RBAC

RBAC maps identities to roles and roles to permissions.

### Model

- Permission: (`resource`, `action`) pair
- Role: named set of permissions
- Assignment: identity -> role

### Best use

- stable organizational access patterns
- low-complexity application permissions

### Example

- `role:editor` -> `posts.read`, `posts.write`
- `role:admin` -> `*.*`

---

## ABAC

ABAC evaluates rule expressions against subject, resource, and request context.

### Engine

- CEL expressions
- ordered by priority
- enable/disable at runtime

### Best use

- conditional rules (time/IP/tenant/state-dependent)
- ownership-aware checks without deep graph traversal

### Example rule

```cel
subject.roles.exists(r, r == "admin") || resource.owner_id == subject.id
```

---

## ReBAC

ReBAC uses a Zanzibar-style tuple graph to answer relationship-based access.

### Model

Tuples express facts such as:

- (`user:alice`, `member`, `group:engineering`)
- (`group:engineering`, `viewer`, `project:alpha`)
- (`project:alpha`, `reader`, `document:spec`)

The check engine expands subject sets and relation inheritance.

### Best use

- collaboration platforms
- nested organizational permissions
- shared resources and delegated access

---

## Evaluation strategy

Policy evaluation can combine models with explicit precedence.

Recommended high-level order:

1. explicit deny rules (if configured)
2. RBAC allow
3. ABAC allow
4. ReBAC allow
5. default deny

Exact precedence should remain explicit in implementation to avoid ambiguous outcomes.

---

## Namespaces and schemas

ReBAC namespaces define:

- object types
- relations
- relation expansion behavior

Namespace definitions should be versioned and validated before activation.

---

## Performance model (Go + Rust)

### Go handles

- API boundary and orchestration
- RBAC and ABAC control flow
- persistence coordination

### Rust can handle hot path graph evaluation

When ReBAC traversal becomes deep/high-volume, Rust engine integration is ideal for:

- tuple graph traversal
- expansion cache operations
- deterministic high-throughput checks

This aligns with Aegion's control-plane-in-Go, critical-engine-in-Rust pattern.

---

## Admin operations

Policy admin surface should include:

- role CRUD
- permission CRUD
- identity-role assignment
- ABAC rule CRUD + priority
- namespace config management
- relation tuple management
- policy simulation/testing tools

---

## API expectations

Core check endpoint:

- `POST /relation-tuples/check` (or equivalent policy check route)
- response includes:
  - `allow`/`deny`
  - model path used (RBAC/ABAC/ReBAC)
  - optional evaluation trace for debugging/audit

---

## Security and governance

- default deny when no rule grants access
- full audit records for policy mutations
- staged rollout support for risky rule changes
- safe validation before namespace/rule activation

---

## Summary

Policy in Aegion is not one model forced everywhere. It is a layered authorization system where RBAC, ABAC, and ReBAC coexist and are selected by use case.
